"""Independent frontend failures must retain all executable release evidence."""
from pathlib import Path
import unittest

import yaml

ROOT = Path(__file__).resolve().parents[2]


def frontend():
    workflow = yaml.load((ROOT / '.github/workflows/company-p0.yml').read_text(), Loader=yaml.BaseLoader)
    return workflow['jobs']['frontend']


class FrontendEvidenceContinuationTests(unittest.TestCase):
    def test_each_validation_runs_after_an_unrelated_gate_failure(self):
        steps = frontend()['steps']
        installation = next(step for step in steps if step.get('run') == 'npm ci')
        install_id = installation.get('id')
        self.assertIsNotNone(install_id, 'the dependency prerequisite needs an observable outcome')
        commands = ['run_r5_source_version_tests.py', 'collect_api_calls.cjs',
                    'npm audit --json', 'd.metadata.vulnerabilities.total !== 0',
                    'npx tsc --noEmit', 'npm test', 'npm run lint', 'npm run build']
        for command in commands:
            with self.subTest(command=command):
                matches = [step for step in steps if command in step.get('run', '')]
                self.assertEqual(len(matches), 1)
                condition = matches[0].get('if', '')
                self.assertIn('!cancelled()', condition)
                self.assertIn(f"steps.{install_id}.outcome == 'success'", condition)
                # A failing sibling step must not become a prerequisite.
                self.assertNotIn('success()', condition)
                self.assertNotIn('failure()', condition)
                self.assertEqual(condition.count('steps.'), 1)

    def test_failures_still_fail_the_existing_frontend_job(self):
        job = frontend()
        self.assertNotIn('continue-on-error', job)
        for step in job['steps']:
            self.assertNotIn('continue-on-error', step)
        audit_gate = next(step for step in job['steps']
                          if step.get('name') == 'Require a complete clean dependency audit')
        self.assertIn('d.error', audit_gate['run'])
        self.assertIn('d.metadata.vulnerabilities.total !== 0', audit_gate['run'])
        test_step = next(step for step in job['steps'] if 'npm test' in step.get('run', ''))
        for bypass in ['--exclude', '--passWithNoTests', '|| true', '|| :', 'continue-on-error']:
            self.assertNotIn(bypass, test_step['run'])

    def test_test_report_and_actual_checkout_identity_are_retained_after_failure(self):
        steps = frontend()['steps']
        self.assertTrue(any('git rev-parse HEAD > frontend-source-sha.txt' in step.get('run', '')
                            for step in steps))
        test_step = next(step for step in steps if 'npm test' in step.get('run', ''))
        self.assertIn('--reporter=json', test_step['run'])
        self.assertIn('--outputFile=../frontend-vitest.json', test_step['run'])
        upload = next(step for step in steps
                      if step.get('with', {}).get('name') == 'company-frontend-evidence')
        self.assertEqual(upload['if'], 'always()')
        paths = upload['with']['path'].splitlines()
        self.assertIn('frontend-source-sha.txt', paths)
        self.assertIn('frontend-source-version-tests.json', paths)
        self.assertIn('frontend-vitest.json', paths)
        self.assertEqual(upload['with']['if-no-files-found'], 'error')


if __name__ == '__main__':
    unittest.main()
