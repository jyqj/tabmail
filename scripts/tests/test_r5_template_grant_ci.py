"""Focused PG workflow controls through the original Go evidence checker.

The Go child below emits explicit synthetic lifecycle controls. It never runs
Go/PostgreSQL or supplies product runtime qualification.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]
NAME = 'Template grant revocation with PostgreSQL'
UPLOAD = 'Upload template grant PostgreSQL evidence'
SUITE = 'template-grant-revocation'
PACKAGE = 'tabmail/internal/store/postgres'
TEST = 'TestR5TemplateGrantFrozenEmployeeRevocation'
LEAVES = ['active-grant-revoke', 'frozen-revoke-survives-reactivation', 'frozen-grant-still-denied',
          'required-audit-failure-rolls-back', 'foreign-user', 'missing-user',
          'foreign-mailbox', 'foreign-template', 'non-admin']
SOURCE_SHA = 'a' * 40


def backend():
    document = yaml.load((ROOT / '.github/workflows/company-p0.yml').read_text(), Loader=yaml.BaseLoader)
    return document['jobs']['backend']


class TemplateGrantCIControls(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory(prefix='r5-template-grant-ci-control-')
        self.addCleanup(temporary.cleanup)
        self.root = Path(temporary.name)
        self.checkout = self.root / 'checkout'
        (self.checkout / 'scripts').mkdir(parents=True)
        # Execute the unmodified real checker against the checked-in suite.
        for name in ['check_go_test_evidence.py', 'required_go_tests.json']:
            (self.checkout / 'scripts' / name).write_bytes((ROOT / 'scripts' / name).read_bytes())
        (self.checkout / 'go-test-source-sha.txt').write_text(SOURCE_SHA + '\n')
        self.runner_temp = self.root / 'runner-temp'
        self.runner_temp.mkdir()
        self.step_output = self.root / 'github-output'
        self.calls = self.root / 'go-calls.jsonl'
        self.bin = self.root / 'bin'
        self.bin.mkdir()
        child = self.bin / 'go'
        child.write_text('#!' + sys.executable + '\n' + '''
import json, os
import sys
with open(os.environ['CONTROL_GO_CALLS'], 'a') as stream:
    stream.write(json.dumps(sys.argv[1:]) + '\\n')
sys.stdout.write(os.environ['CONTROL_GO_EVENTS'])
sys.stderr.write('synthetic shell control; no PostgreSQL execution\\n')
raise SystemExit(int(os.environ.get('CONTROL_GO_EXIT', '0')))
''')
        child.chmod(0o755)

    def named(self, name):
        matches = [step for step in backend()['steps'] if step.get('name') == name]
        self.assertEqual(len(matches), 1, 'one reviewed step required: ' + name)
        return matches[0]

    def events(self, variant='pass'):
        def event(action, test=None):
            value = {'Action': action, 'Package': PACKAGE}
            if test is not None:
                value['Test'] = test
            return value
        if variant == 'empty':
            values = []
        elif variant == 'no-match':
            values = [event('start'), event('pass')]
        elif variant == 'top-skip':
            values = [event('start'), event('run', TEST), event('skip', TEST), event('pass')]
        else:
            values = [event('start'), event('run', TEST)]
            for index, leaf in enumerate(LEAVES):
                name = TEST + '/' + leaf
                values.extend([event('run', name),
                               event('skip' if variant == 'child-skip' and index == 0 else 'pass', name)])
            values.extend([event('pass', TEST), event('pass')])
        return ''.join(json.dumps(value) + '\n' for value in values)

    def run_shell(self, *, variant='pass', exit_code=0, missing_dsn=False):
        env = {**os.environ, 'PATH': str(self.bin) + os.pathsep + os.environ['PATH'],
               'RUNNER_TEMP': str(self.runner_temp), 'GITHUB_OUTPUT': str(self.step_output),
               'CONTROL_GO_CALLS': str(self.calls), 'CONTROL_GO_EVENTS': self.events(variant),
               'CONTROL_GO_EXIT': str(exit_code), 'TABMAIL_TEST_DB_DSN': 'synthetic-control-never-connected'}
        if missing_dsn:
            env.pop('TABMAIL_TEST_DB_DSN')
        result = subprocess.run(['bash', '-e', '-c', self.named(NAME)['run']], cwd=self.checkout,
                                env=env, capture_output=True, text=True, timeout=10)
        self.assertTrue(self.step_output.is_file(), 'started step must retain its owned evidence directory')
        outputs = dict(line.split('=', 1) for line in self.step_output.read_text().splitlines())
        self.assertEqual(set(outputs), {'evidence_directory'})
        evidence = Path(outputs['evidence_directory'])
        return result, evidence

    def report(self, evidence):
        report = json.loads((evidence / 'go-test-evidence.json').read_text())
        self.assertEqual(report['suite'], SUITE)
        self.assertEqual(report['source_sha'], SOURCE_SHA)
        self.assertEqual(report['mandatory_tests'], 1)
        self.assertEqual(report['log_sha256'], hashlib.sha256((evidence / 'go-test.jsonl').read_bytes()).hexdigest())
        self.assertEqual(report['manifest_sha256'], hashlib.sha256((evidence / 'required_go_tests.json').read_bytes()).hexdigest())
        return report

    def test_exact_required_suite_has_no_allowed_skips(self):
        document = json.loads((ROOT / 'scripts/required_go_tests.json').read_text())
        self.assertIn(SUITE, document['suites'])
        self.assertEqual(document['suites'][SUITE], {
            'required_tests': [[PACKAGE, TEST]], 'allowed_skips': []})

    def test_independent_prerequisites_budget_and_selection_remain_bounded(self):
        step = self.named(NAME)
        self.assertEqual(step['id'], 'backend_template_grant')
        self.assertEqual(step['if'], "${{ !cancelled() && steps.backend_source.outcome == 'success' && steps.backend_go.outcome == 'success' && steps.backend_archive.outcome == 'success' }}")
        self.assertEqual(step['timeout-minutes'], '3')
        self.assertNotIn('continue-on-error', step)
        self.assertNotIn('env', step)
        steps = backend()['steps']
        self.assertLess(steps.index(self.named('Race tests with PostgreSQL')), steps.index(step))
        self.assertIn('set -euo pipefail\n', step['run'])
        self.assertIn('set +e\n', step['run'])
        for bypass in ['|| true', '--allow', '--skip', '--exclude']:
            self.assertNotIn(bypass, step['run'])
        result, evidence = self.run_shell()
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual(json.loads(self.calls.read_text()), [
            'test', '-mod=readonly', '-json', '-race', '-count=1', '-timeout=120s',
            '-run', '^' + TEST + '$', './internal/store/postgres'])
        self.assertEqual((evidence / 'go-test.exit').read_text(), '0\n')

    def test_success_uses_the_original_checker_and_retains_source_bound_evidence(self):
        result, evidence = self.run_shell()
        self.assertEqual(result.returncode, 0, result.stderr)
        report = self.report(evidence)
        self.assertEqual(report['status'], 'pass')
        self.assertEqual(report['process_exit_code'], 0)
        self.assertEqual(report['tests_started'], 10)
        self.assertEqual(report['tests_passed'], 10)
        self.assertEqual(report['tests_skipped'], 0)
        self.assertEqual((evidence / 'go-test-source-sha.txt').read_text(), SOURCE_SHA + '\n')
        self.assertEqual((evidence / 'required_go_tests.json').read_bytes(),
                         (ROOT / 'scripts/required_go_tests.json').read_bytes())
        self.assertIn('synthetic shell control', (evidence / 'go-test.stderr').read_text())

    def test_nonzero_go_exit_is_preserved_even_if_events_claim_success(self):
        for exit_code in [17, 127]:
            with self.subTest(exit_code=exit_code):
                result, evidence = self.run_shell(exit_code=exit_code)
                self.assertNotEqual(result.returncode, 0)
                self.assertEqual((evidence / 'go-test.exit').read_text(), str(exit_code) + '\n')
                report = self.report(evidence)
                self.assertEqual(report['status'], 'fail')
                self.assertEqual(report['process_exit_code'], exit_code)
                self.assertIn('go test process exit was ' + str(exit_code), '\n'.join(report['errors']))

    def test_empty_no_match_and_skipped_execution_cannot_pass(self):
        for variant in ['empty', 'no-match', 'top-skip', 'child-skip']:
            with self.subTest(variant=variant):
                result, evidence = self.run_shell(variant=variant)
                self.assertNotEqual(result.returncode, 0)
                report = self.report(evidence)
                self.assertEqual(report['status'], 'fail')
                self.assertEqual(report['process_exit_code'], 0)
                if variant in ['empty', 'no-match']:
                    self.assertEqual(report['tests_started'], 0)
                    self.assertIn('no tests actually ran', report['errors'])
                else:
                    self.assertEqual(report['tests_skipped'], 1)
                    self.assertIn('unexpected skipped test', '\n'.join(report['errors']))

    def test_missing_dsn_fails_before_go_without_inventing_a_go_exit(self):
        result, evidence = self.run_shell(missing_dsn=True)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('TABMAIL_TEST_DB_DSN', result.stderr)
        self.assertFalse(self.calls.exists())
        self.assertFalse((evidence / 'go-test.exit').exists())
        self.assertFalse((evidence / 'go-test-evidence.json').exists())

    def test_evidence_artifact_is_separate_fresh_and_retained_after_failure(self):
        upload = self.named(UPLOAD)
        self.assertEqual(upload, {
            'name': UPLOAD, 'if': "${{ always() && steps.backend_template_grant.outputs.evidence_directory != '' }}",
            'uses': 'actions/upload-artifact@v4',
            'with': {'name': 'company-template-grant-postgres-evidence',
                     'path': '${{ steps.backend_template_grant.outputs.evidence_directory }}',
                     'if-no-files-found': 'error', 'retention-days': '14'}})
        first, before = self.run_shell()
        self.assertEqual(first.returncode, 0, first.stderr)
        old = {path.name: path.read_bytes() for path in before.iterdir()}
        failed, after = self.run_shell(exit_code=23)
        self.assertNotEqual(failed.returncode, 0)
        self.assertNotEqual(before, after)
        self.assertTrue(after.is_relative_to(self.runner_temp))
        self.assertFalse(after.is_relative_to(self.checkout))
        self.assertEqual(old, {path.name: path.read_bytes() for path in before.iterdir()})
        self.assertEqual(self.report(after)['process_exit_code'], 23)
        legacy_paths = self.named('Upload test evidence')['with']['path']
        self.assertNotIn('template-grant', legacy_paths)
        self.assertNotIn('evidence_directory', legacy_paths)


if __name__ == '__main__':
    unittest.main()
