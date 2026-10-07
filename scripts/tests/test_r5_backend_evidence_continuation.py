"""Keep independent backend evidence executable without bypassing prerequisites."""
from pathlib import Path
import re
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


def backend():
    workflow = yaml.load((ROOT / '.github/workflows/company-p0.yml').read_text(), Loader=yaml.BaseLoader)
    return workflow['jobs']['backend']


def by_name(name):
    return next(step for step in backend()['steps'] if step.get('name') == name)


def prerequisites(step):
    # Deliberately accept only the reviewed conjunction. Including a status
    # function avoids GitHub's implicit success() after a previous step failed.
    condition = step.get('if', '')
    if not re.fullmatch(r"\$\{\{ !cancelled\(\)(?: && steps\.[a-z_]+\.outcome == 'success')+ \}\}", condition):
        raise AssertionError(f"missing explicit continuation/prerequisite condition: {step.get('name', step.get('uses'))}")
    names = re.findall(r"steps\.([a-z_]+)\.outcome == 'success'", condition)
    if len(names) != len(set(names)):
        raise AssertionError('duplicate prerequisite')
    return set(names)


SOURCE = {'backend_source'}
GO = SOURCE | {'backend_go', 'backend_archive'}
CHECKS = {
    'Build': GO,
    'Race tests with PostgreSQL': GO | {'backend_pg_client'},
    'Template grant revocation with PostgreSQL': GO,
    'Prove missing execution cannot pass': GO,
    'Validation tool regressions': GO | {'backend_node', 'backend_contract_deps'},
    'DTO field contract': SOURCE | {'backend_contract_deps'},
    'Fresh route/client compatibility inventory and upgrade plan': GO | {
        'backend_node', 'backend_contract_deps', 'backend_typescript'},
    'Transaction syntax and review inventory drift': GO,
    'Real protocol baseline with explicit target failures': GO,
    'Actual HTTP response contracts with PostgreSQL': GO | {'backend_contract_deps'},
    'Vet': GO,
}


class BackendEvidenceContinuationTests(unittest.TestCase):
    def test_validation_failure_cannot_suppress_ready_independent_checks(self):
        actual = {name: prerequisites(by_name(name)) for name in CHECKS}
        self.assertEqual(actual, CHECKS)
        # In particular no check depends on the outcome of build, race,
        # source tests, contracts, or a previous protocol/HTTP validation.
        ids = {step['id']: step for step in backend()['steps'] if 'id' in step}
        for needed in CHECKS.values():
            for name in needed:
                self.assertIn(name, ids)

    def test_failed_or_skipped_prerequisite_blocks_only_its_dependents(self):
        actual = {name: prerequisites(by_name(name)) for name in CHECKS}
        observed = {name: 'success' for needed in CHECKS.values() for name in needed}
        for unavailable in ['backend_go', 'backend_pg_client', 'backend_contract_deps',
                            'backend_node', 'backend_typescript', 'backend_archive']:
            for status in ['failure', 'skipped', 'cancelled']:
                outcomes = {**observed, unavailable: status}
                for name, needed in CHECKS.items():
                    with self.subTest(prerequisite=unavailable, status=status, step=name):
                        ready = all(outcomes.get(value) == 'success' for value in actual[name])
                        self.assertEqual(ready, unavailable not in needed)
        # The one-use scanner preparation cannot become a requirement for
        # Go-only checks, DTO validation, or the isolated source-test runner.
        self.assertEqual([name for name, needed in CHECKS.items() if 'backend_typescript' in needed],
                         ['Fresh route/client compatibility inventory and upgrade plan'])

    def test_setups_do_not_inherit_an_unrelated_gate_failure(self):
        ids = {step['id']: step for step in backend()['steps'] if 'id' in step}
        self.assertEqual(ids['backend_checkout']['uses'], 'actions/checkout@v4')
        self.assertEqual(prerequisites(ids['backend_source']), {'backend_checkout'})
        for name in ['backend_go', 'backend_node', 'backend_pg_client', 'backend_archive',
                     'backend_contract_deps', 'backend_typescript']:
            with self.subTest(prerequisite=name):
                self.assertEqual(prerequisites(ids[name]), SOURCE)
        self.assertEqual(ids['backend_go']['uses'], 'actions/setup-go@v5')
        self.assertEqual(ids['backend_go']['with'], {'go-version-file': 'go.mod'})
        self.assertEqual(ids['backend_node']['uses'], 'actions/setup-node@v4')
        self.assertEqual(ids['backend_node']['with'], {'node-version': '22'})
        self.assertIn('-r scripts/requirements-contract.txt', ids['backend_contract_deps']['run'])

    def test_failed_jobs_and_original_race_selection_remain_failed(self):
        job = backend()
        self.assertEqual(job['timeout-minutes'], '20')
        self.assertNotIn('continue-on-error', job)
        for step in job['steps']:
            self.assertNotIn('continue-on-error', step)
        race = by_name('Race tests with PostgreSQL')['run']
        self.assertIn('set -euo pipefail', race)
        self.assertIn('set +e\n', race)
        self.assertIn('go test -json -race -count=1 -timeout=180s ./... | tee go-test.jsonl\n'
                      'test_exit=$?\nset -e\n', race)
        self.assertIn('check_go_test_evidence.py --suite backend', race)
        self.assertIn('--exit-code "$test_exit"', race)
        for bypass in ['--exclude', '-run ', '-skip ', '|| true', '|| :']:
            self.assertNotIn(bypass, race)
        self.assertEqual(by_name('Build')['run'], 'go build ./...')
        self.assertEqual(by_name('Vet')['run'], 'go vet ./...')

    def test_checkout_identity_and_all_existing_evidence_survive_failure(self):
        steps = backend()['steps']
        source = by_name('Record the tested backend checkout')
        self.assertEqual(source['run'], 'git rev-parse HEAD > go-test-source-sha.txt')
        for name in CHECKS:
            self.assertLess(steps.index(source), steps.index(by_name(name)))
        self.assertEqual(sum('> go-test-source-sha.txt' in step.get('run', '') for step in steps), 1)
        for name in ['Race tests with PostgreSQL', 'Prove missing execution cannot pass',
                     'Actual HTTP response contracts with PostgreSQL']:
            self.assertIn('--source-sha "$(cat go-test-source-sha.txt)"', by_name(name)['run'])
        upload = by_name('Upload test evidence')
        self.assertEqual(upload['if'], 'always()')
        self.assertEqual(upload['with']['name'], 'company-p0-test-evidence')
        self.assertEqual(upload['with']['retention-days'], '14')
        self.assertEqual(upload['with']['if-no-files-found'], 'error')
        self.assertEqual(set(upload['with']['path'].splitlines()), {
            'source-version-tests.json', 'go-test.jsonl', 'go-test-source-sha.txt',
            'go-test-evidence.json', 'go-test-negative-evidence', 'http-contract-evidence',
            '!http-contract-evidence/responses.json', 'protocol-baseline-evidence',
            'backend-typescript-preparation.json'})

    def test_typescript_preparation_reuses_the_reviewed_installer_and_receipt(self):
        command = by_name('Prepare the pinned TypeScript scanner dependency')['run']
        self.assertIn('from r5_source_runner_prepare import prepare_typescript', command)
        self.assertIn('prepare_typescript(Path.cwd())', command)
        self.assertIn('go-test-source-sha.txt', command)
        self.assertIn('backend-typescript-preparation.json', command)
        self.assertIn('json.dumps(receipt', command)
        for bypass in ['npm ', 'pip ', 'curl ', 'wget ', 'except ', '||']:
            self.assertNotIn(bypass, command)


if __name__ == '__main__':
    unittest.main()
