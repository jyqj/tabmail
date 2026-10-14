"""Negative admission controls; synthetic reports are never runtime evidence."""
import copy
from contextlib import redirect_stdout
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import run_r5_frontend_fixture as runner


def report():
    return {
        'success': True, 'numTotalTests': 2, 'numPassedTests': 2,
        'numFailedTests': 0, 'numPendingTests': 0, 'numTodoTests': 0,
        'testResults': [
            {'name': '/source/web/r5-external-batch-probe.test.tsx', 'status': 'passed',
             'assertionResults': [{'fullName': runner.PROBE, 'status': 'passed'}]},
            {'name': '/source/web/ordinary.test.tsx', 'status': 'passed',
             'assertionResults': [{'fullName': 'ordinary test', 'status': 'passed'}]},
        ],
    }


class FrontendFixtureAdmissionTests(unittest.TestCase):
    def test_complete_results_are_required_even_when_probe_passes(self):
        self.assertEqual(runner.validate_vitest(report(), 0), [])
        for key, value in [('success', False), ('numPassedTests', 1),
                           ('numFailedTests', 1), ('numPendingTests', 1),
                           ('numTodoTests', 1), ('numRuntimeErrorTestSuites', 1),
                           ('numTotalTests', True)]:
            candidate = report()
            candidate[key] = value
            with self.subTest(field=key):
                self.assertTrue(runner.validate_vitest(candidate, 0))
        self.assertTrue(runner.validate_vitest(report(), 1))

    def test_missing_duplicate_wrong_file_and_skipped_probe_are_rejected(self):
        missing, duplicate, wrong_file, skipped = [report() for _ in range(4)]
        missing['testResults'][0]['assertionResults'][0]['fullName'] = 'unrelated'
        duplicate['testResults'].append(copy.deepcopy(duplicate['testResults'][0]))
        duplicate['numTotalTests'] = duplicate['numPassedTests'] = 3
        wrong_file['testResults'][0]['name'] = '/source/web/other.test.tsx'
        skipped['testResults'][0]['assertionResults'][0]['status'] = 'skipped'
        for candidate in (missing, duplicate, wrong_file, skipped):
            with self.subTest(report=candidate):
                self.assertTrue(runner.validate_vitest(candidate, 0))

    def test_malformed_empty_or_inconsistent_results_are_rejected(self):
        for candidate in (None, {}, dict(report(), testResults=[]),
                          dict(report(), numTotalTests=0, numPassedTests=0),
                          dict(report(), numTotalTests=3, numPassedTests=3)):
            with self.subTest(report=candidate):
                self.assertTrue(runner.validate_vitest(candidate, 0))
        candidate = report()
        candidate['testResults'][1]['status'] = 'failed'
        self.assertTrue(runner.validate_vitest(candidate, 0))

    def test_missing_dsn_and_injected_fixture_fail_before_dispatch(self):
        for environment in ({}, {'TABMAIL_TEST_DB_DSN': 'synthetic-control',
                                 'TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE': '/injected.json'}):
            with self.subTest(environment=environment), patch.dict(os.environ, environment, clear=True), \
                    patch.object(runner, 'run_owned') as dispatch, tempfile.TemporaryDirectory() as temporary:
                with self.assertRaises(ValueError):
                    runner.run(Path(temporary), 'a' * 40)
                dispatch.assert_not_called()

    def test_altered_command_and_uncommitted_source_fail_before_dispatch(self):
        with tempfile.TemporaryDirectory() as temporary, patch.object(runner, 'run_owned') as dispatch:
            output = Path(temporary)
            with self.assertRaises(ValueError):
                runner.run(output, 'a' * 40, runner.FRONTEND_COMMAND + ['--passWithNoTests'])
            with patch.dict(os.environ, {'TABMAIL_TEST_DB_DSN': 'synthetic-control'}, clear=True), \
                    patch.object(runner, 'source_state', return_value={'head': 'a' * 40, 'tracked_clean': False}):
                with self.assertRaises(ValueError):
                    runner.run(output, 'a' * 40)
            dispatch.assert_not_called()

    def test_source_drift_invalidates_otherwise_passing_synthetic_results(self):
        package = 'tabmail/internal/testpg'
        events = [dict(Action='start', Package=package),
                  dict(Action='run', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package)]
        raw = ('\n'.join(json.dumps(event) for event in events) + '\n').encode()
        before = dict(head='a' * 40, tree='b' * 40, tracked_clean=True, tracked_diff_sha256='c' * 64)
        changed = dict(before, tracked_clean=False, tracked_diff_sha256='d' * 64)
        execution = dict(stdout=raw, stderr=b'', exit_code=0, timeout=False, tail=False, processes_joined=True)
        with tempfile.TemporaryDirectory() as temporary, \
                patch.dict(os.environ, {'TABMAIL_TEST_DB_DSN': 'synthetic-control'}, clear=True), \
                patch.object(runner, 'source_state', side_effect=[before, changed]), \
                patch.object(runner, 'run_owned', return_value=execution), redirect_stdout(io.StringIO()):
            output = Path(temporary)
            runner.write_json(output / 'frontend-result.json', {'status': 'pass'})
            runner.write_json(output / 'frontend-vitest.json', report())
            (output / 'vitest.exit').write_text('0\n')
            self.assertEqual(runner.run(output, before['head']), 1)
            result = json.loads((output / 'go-test-evidence.json').read_text())
            self.assertFalse(result['source_unchanged'])
            self.assertIn('frontend source changed during execution', result['errors'])

    def test_go_dispatch_keeps_exact_owner_selection_under_existing_fixture_tag(self):
        package = 'tabmail/internal/testpg'
        events = [dict(Action='start', Package=package),
                  dict(Action='run', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package)]
        raw = ('\n'.join(json.dumps(event) for event in events) + '\n').encode()
        source = dict(head='a' * 40, tree='b' * 40, tracked_clean=True, tracked_diff_sha256='c' * 64)
        execution = dict(stdout=raw, stderr=b'', exit_code=0, timeout=False, tail=False, processes_joined=True)
        with tempfile.TemporaryDirectory() as temporary, \
                patch.dict(os.environ, {'TABMAIL_TEST_DB_DSN': 'synthetic-control'}, clear=True), \
                patch.object(runner, 'source_state', return_value=source), \
                patch.object(runner, 'run_owned', return_value=execution) as dispatch, redirect_stdout(io.StringIO()):
            output = Path(temporary)
            runner.write_json(output / 'frontend-result.json', {'status': 'pass'})
            runner.write_json(output / 'frontend-vitest.json', report())
            (output / 'vitest.exit').write_text('0\n')
            self.assertEqual(runner.run(output, source['head']), 0)
            self.assertEqual(dispatch.call_args.args[0], [
                'go', 'test', '-mod=readonly', '-json', '-race', '-count=1',
                '-timeout=360s', '-tags=r5fixtures', '-run', '^TestR5FrontendOwnedFixture$',
                './internal/testpg'])
            self.assertEqual(dispatch.call_args.kwargs['seconds'], 390)

    def test_original_go_checker_rejects_empty_skipped_and_nonzero_owner(self):
        required, allowed = runner.evidence.load_suite(runner.SUITE, 'frontend-owned-fixture')
        self.assertFalse(allowed)
        package = 'tabmail/internal/testpg'
        events = [dict(Action='start', Package=package),
                  dict(Action='run', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package, Test=runner.TEST),
                  dict(Action='pass', Package=package)]
        lines = [json.dumps(event) for event in events]
        self.assertEqual(runner.evidence.evaluate(lines, required, allowed, 0)['status'], 'pass')
        self.assertEqual(runner.evidence.evaluate([], required, allowed, 0)['status'], 'fail')
        self.assertEqual(runner.evidence.evaluate(lines, required, allowed, 1)['status'], 'fail')
        events[2]['Action'] = 'skip'
        self.assertEqual(runner.evidence.evaluate([json.dumps(e) for e in events], required, allowed, 0)['status'], 'fail')

    def test_owned_process_preserves_real_failure_and_joins_timeout(self):
        # A dedicated process owns the subreaper, just like the runtime CLI.
        program = '''
import json, os, sys
from pathlib import Path
import run_r5_frontend_fixture as runner
code, seconds = sys.argv[1], float(sys.argv[2])
result = runner.run_owned([sys.executable, '-c', code], cwd=Path.cwd(), env=dict(os.environ), seconds=seconds)
result.pop('stdout'); result.pop('stderr')
print(json.dumps(result))
'''
        environment = dict(os.environ, PYTHONPATH=str(runner.ROOT / 'scripts'), PYTHONDONTWRITEBYTECODE='1')
        for code, seconds, expected in [('raise SystemExit(7)', '3', 7),
                                        ('import time; time.sleep(30)', '0.1', None)]:
            completed = subprocess.run([sys.executable, '-B', '-c', program, code, seconds],
                                       cwd=runner.ROOT, env=environment, capture_output=True,
                                       text=True, timeout=10, check=True)
            result = json.loads(completed.stdout)
            self.assertTrue(result['processes_joined'])
            if expected is None:
                self.assertTrue(result['timeout'])
                self.assertNotEqual(result['exit_code'], 0)
            else:
                self.assertFalse(result['timeout'])
                self.assertEqual(result['exit_code'], expected)

    def terminal_control(self, mode):
        # A real child and cleanup finish successfully. Only the final signal
        # or clock boundary is controlled; this is not PG/runtime evidence.
        program = '''
import json, os, signal, sys
from pathlib import Path
import run_r5_frontend_fixture as runner
sys.path.insert(0, str(runner.ROOT / 'scripts/preparation'))
import r5_external_batch as processes
mode = sys.argv[1]
terminal = {}
original_finish = processes.OwnedProcess.finish
original_join = processes.join_adopted_tails
def finish(self, cancelled, deadline):
    result = original_finish(self, cancelled, deadline)
    terminal.update(cancelled=cancelled, deadline=deadline, finish_timeout=result['timeout'])
    return result
def join():
    tails = original_join()
    if mode == 'signal':
        os.kill(os.getpid(), signal.SIGTERM)
    elif mode == 'deadline':
        runner.time.monotonic = lambda: terminal['deadline'] + 0.01
    else:
        raise AssertionError('unknown terminal control')
    return tails
processes.OwnedProcess.finish = finish
processes.join_adopted_tails = join
result = runner.run_owned([sys.executable, '-I', '-S', '-c', 'pass'], cwd=Path.cwd(), env=dict(os.environ), seconds=5)
result.pop('stdout'); result.pop('stderr')
result.update(finish_timeout=terminal['finish_timeout'], cancellation_observed=terminal['cancelled'].is_set())
print(json.dumps(result))
'''
        environment = dict(os.environ, PYTHONPATH=str(runner.ROOT / 'scripts'), PYTHONDONTWRITEBYTECODE='1')
        completed = subprocess.run([sys.executable, '-B', '-c', program, mode],
                                   cwd=runner.ROOT, env=environment, capture_output=True,
                                   text=True, timeout=10, check=True, start_new_session=True)
        result = json.loads(completed.stdout)
        self.assertEqual(result['exit_code'], 0)
        self.assertTrue(result['processes_joined'])
        self.assertFalse(result['tail'], result)
        self.assertFalse(result['finish_timeout'])
        self.assertTrue(result['timeout'])
        return result

    def test_signal_during_final_join_rejects_successful_child(self):
        self.assertTrue(self.terminal_control('signal')['cancellation_observed'])

    def test_deadline_crossed_during_final_join_rejects_successful_child(self):
        self.assertFalse(self.terminal_control('deadline')['cancellation_observed'])


if __name__ == '__main__':
    unittest.main()
