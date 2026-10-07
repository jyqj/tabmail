"""Real owned child-process controls, not Go/PG or source qualification.

Only the dispatcher is replaced: each control really launches Python (or a
missing executable) and lets subprocess.run observe its exit/timeout. The
formal Go 120s / outer 180s arguments are asserted, never shortened in code.
"""
import contextlib
import copy
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location('r5_protocol_process_control', ROOT / 'scripts/check_r5_protocol.py')
protocol = importlib.util.module_from_spec(spec)
spec.loader.exec_module(protocol)
REAL_RUN = subprocess.run
CONTROL_TIMEOUT_SECONDS = 0.75
BOUNDARY = 'Synthetic Go-style events from a real owned Python child; no Go/PG or source qualification.'


def raw(value):
    return value.encode() if isinstance(value, str) else value or b''


class ProtocolProcessEvidenceTests(unittest.TestCase):
    def setUp(self):
        evidence = os.environ.get('TABMAIL_R5_PROCESS_CONTROL_EVIDENCE_DIR')
        if evidence:
            self.base = Path(evidence).resolve() / self._testMethodName
            self.base.mkdir(parents=True, exist_ok=False)
        else:
            temporary = tempfile.TemporaryDirectory()
            self.addCleanup(temporary.cleanup)
            self.base = Path(temporary.name)
        self.output = self.base / 'protocol-evidence'
        self.data = {'cases': [copy.deepcopy(next(row for row in protocol.load_cases()['cases'] if row['id'] == 'RT01'))]}
        self.parent = 'TestR5ProtocolRetentionSharedCases'
        self.package = 'tabmail/internal/store/postgres'
        self.leaf = self.parent + '/RT01/default'
        self.cases = self.base / 'cases.json'
        self.cases.write_bytes(protocol.CASES.read_bytes())
        self.closure = {'owned-synthetic-control': 'a' * 64}
        identity = dict(source_sha='b' * 40, policy=protocol.source_inventory.ARCHIVE_POLICY,
                        source_identity_kind=protocol.source_inventory.KIND,
                        source_identity_boundary=BOUNDARY)
        for target, name, value in (
            (protocol, 'source_closure', self.closure),
            (protocol, 'protocol_source_metadata', identity),
            (protocol, 'current_protocol_environment', dict(os.environ)),
        ):
            patcher = mock.patch.object(target, name, return_value=value)
            setattr(self, name, patcher.start())
            self.addCleanup(patcher.stop)
        for name, value in (('CASES', self.cases), ('SOURCE_MANIFEST', None),
                            ('SOURCE_MANIFEST_SHA256', None), ('SOURCE_POLICY', None)):
            patcher = mock.patch.object(protocol, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        environment = mock.patch.dict(os.environ, {'TABMAIL_TEST_DB_DSN': 'owned-control-not-connected'})
        environment.start()
        self.addCleanup(environment.stop)
        self.dispatches = 0

    def passing_bytes(self):
        rows = [dict(Package=self.package, Action='run', Test=self.parent),
                dict(Package=self.package, Action='run', Test=self.leaf),
                dict(Package=self.package, Action='pass', Test=self.leaf),
                dict(Package=self.package, Action='pass', Test=self.parent),
                dict(Package=self.package, Action='pass')]
        return ('\n'.join(map(json.dumps, rows)) + '\n').encode()

    def invoke(self, stdout=b'', stderr=b'', *, mode='timeout', exit_code=0, after_process=None, cli=False):
        # These are declared control inputs, not observations or fabricated Go evidence.
        (self.base / 'control-expected.stdout').write_bytes(stdout)
        (self.base / 'control-expected.stderr').write_bytes(stderr)
        child = self.base / 'owned_control_child.py'
        child.write_text('import sys, time\n'
                         f'sys.stdout.buffer.write({stdout!r})\nsys.stdout.buffer.flush()\n'
                         f'sys.stderr.buffer.write({stderr!r})\nsys.stderr.buffer.flush()\n'
                         + ('time.sleep(10)\n' if mode == 'timeout' else f'sys.exit({exit_code})\n'))
        actual = [str(self.base / 'missing-owned-executable')] if mode == 'start_error' else [sys.executable, str(child)]

        def dispatch(command, **kwargs):
            self.dispatches += 1
            self.assertEqual(command[:2], ['go', 'test'])
            self.assertIn('-timeout=120s', command)
            self.assertIn('-race', command)
            self.assertIn('-count=1', command)
            self.assertEqual(kwargs['timeout'], 180)
            self.assertTrue(kwargs['capture_output'])
            receipt = dict(boundary=BOUNDARY, requested_command=command,
                           requested_timeout_seconds=kwargs['timeout'], actual_command=actual,
                           actual_timeout_seconds=CONTROL_TIMEOUT_SECONDS)
            try:
                result = REAL_RUN(actual, cwd=self.base, env=kwargs['env'], capture_output=True,
                                  text=kwargs.get('text', False), timeout=CONTROL_TIMEOUT_SECONDS)
                receipt.update(observed_status='completed', observed_returncode=result.returncode)
                (self.base / 'control-observed.stdout').write_bytes(raw(result.stdout))
                (self.base / 'control-observed.stderr').write_bytes(raw(result.stderr))
                return result
            except subprocess.TimeoutExpired as error:
                receipt.update(observed_status='timed_out', observed_error_type=type(error).__name__,
                               observed_returncode=None)
                (self.base / 'control-observed.stdout').write_bytes(raw(error.output))
                (self.base / 'control-observed.stderr').write_bytes(raw(error.stderr))
                raise
            except OSError as error:
                receipt.update(observed_status='process_error', observed_error_type=type(error).__name__,
                               observed_returncode=None)
                raise
            finally:
                (self.base / 'control-execution.json').write_text(json.dumps(receipt, indent=2) + '\n')
                if after_process:
                    after_process()

        with mock.patch.object(protocol.subprocess, 'run', side_effect=dispatch):
            if not cli:
                return protocol.run_shared(self.data, 'db', self.output)
            arguments = ['check_r5_protocol.py', '--run', 'shared-db', '--output-dir', str(self.output),
                         '--source-policy', protocol.source_inventory.ARCHIVE_POLICY,
                         '--source-manifest', str(self.base / 'synthetic-unvalidated-manifest.json'),
                         '--source-manifest-sha256', 'c' * 64]
            printed = io.StringIO()
            with mock.patch.object(sys, 'argv', arguments), mock.patch.object(protocol, 'load_cases', return_value=self.data), contextlib.redirect_stdout(printed):
                code = protocol.main()
            (self.base / 'control-cli.stdout').write_text(printed.getvalue())
            return code, json.loads(printed.getvalue())

    def assert_raw_failure(self, report, stdout, stderr, status, exit_code=None):
        self.assertEqual(report['status'], 'shared_scoped_evidence_failed')
        self.assertFalse(report['product_green'])
        self.assertFalse(report['task_complete'])
        self.assertEqual(report['shared_input_verified_cases'], 0)
        self.assertEqual(report['shared_input_verified_layers'], {})
        self.assertEqual(json.loads((self.output / 'report.json').read_text()), report)
        self.assertEqual((self.output / 'go-0.jsonl').read_bytes(), stdout)
        self.assertEqual((self.output / 'go-0.stderr').read_bytes(), stderr)
        execution = json.loads((self.output / 'go-0.execution.json').read_text())
        self.assertEqual(execution['status'], status)
        self.assertEqual(execution['returncode'], exit_code)
        self.assertEqual(execution['timeout_seconds'], 180)
        self.assertEqual(execution['stdout_sha256'], hashlib.sha256(stdout).hexdigest())
        self.assertEqual(execution['stderr_sha256'], hashlib.sha256(stderr).hexdigest())
        self.assertEqual(execution['stdout_bytes'], len(stdout))
        self.assertEqual(execution['stderr_bytes'], len(stderr))
        self.assertEqual(report['reports'][0]['execution'], execution)
        self.assertEqual(report['reports'][0]['process_exit_code'], exit_code)
        self.assertTrue(report['reports'][0]['errors'])
        return execution

    def test_real_timeout_preserves_partial_streams_and_failure_report(self):
        stdout = (json.dumps(dict(Package=self.package, Action='run', Test=self.parent)) + '\n').encode()
        stderr = b'owned child diagnostic\r\n'
        report = self.invoke(stdout, stderr)
        execution = self.assert_raw_failure(report, stdout, stderr, 'timed_out')
        self.assertEqual(execution['error']['type'], 'TimeoutExpired')
        self.assertEqual(self.source_closure.call_count, 2)

    def test_real_missing_executable_keeps_child_streams_empty_and_launch_error_separate(self):
        report = self.invoke(mode='start_error')
        execution = self.assert_raw_failure(report, b'', b'', 'process_error')
        self.assertEqual(execution['error']['type'], 'FileNotFoundError')
        self.assertIn('missing-owned-executable', execution['error']['message'])

    def test_complete_pass_looking_output_followed_by_timeout_cannot_qualify(self):
        stdout = self.passing_bytes()
        report = self.invoke(stdout)
        self.assert_raw_failure(report, stdout, b'', 'timed_out')

    def test_timeout_keeps_truncated_json_bytes_without_parse_exception_losing_report(self):
        stdout, stderr = self.passing_bytes() + b'{"Action":', b'last diagnostic\x00\r\n'
        report = self.invoke(stdout, stderr)
        self.assert_raw_failure(report, stdout, stderr, 'timed_out')

    def test_timeout_keeps_invalid_utf8_bytes_without_replacement_or_report_loss(self):
        stdout, stderr = b'{"Output":"\xff', b'\xfe raw child stderr\r\n'
        report = self.invoke(stdout, stderr)
        self.assert_raw_failure(report, stdout, stderr, 'timed_out')

    def test_completed_pass_preserves_original_classifier_semantics_and_raw_line_endings(self):
        stdout, stderr = self.passing_bytes().replace(b'\n', b'\r\n'), b'owned diagnostic\r\n'
        report = self.invoke(stdout, stderr, mode='completed')
        self.assertEqual(report['status'], 'shared_scoped_evidence_passed')
        self.assertTrue(report['product_green'])
        self.assertFalse(report['task_complete'])
        self.assertEqual(report['source_identity_boundary'], BOUNDARY)
        self.assertEqual((self.output / 'go-0.jsonl').read_bytes(), stdout)
        self.assertEqual((self.output / 'go-0.stderr').read_bytes(), stderr)
        self.assertEqual(report['reports'][0]['execution']['returncode'], 0)
        self.assertEqual(report['reports'][0]['execution']['status'], 'completed')

    def test_completed_nonzero_exit_is_recorded_and_not_laundered_by_pass_events(self):
        stdout = self.passing_bytes()
        report = self.invoke(stdout, mode='completed', exit_code=7)
        self.assert_raw_failure(report, stdout, b'', 'completed', exit_code=7)

    def test_completed_invalid_json_keeps_raw_streams_and_failure_report(self):
        stdout, stderr = b'[1]\n', b'owned stderr\r\n'
        report = self.invoke(stdout, stderr, mode='completed')
        self.assert_raw_failure(report, stdout, stderr, 'completed', exit_code=0)

    def test_completed_invalid_utf8_keeps_raw_streams_and_failure_report(self):
        stdout, stderr = b'\xff invalid JSONL\n', b'\xfe stderr\r\n'
        report = self.invoke(stdout, stderr, mode='completed')
        self.assert_raw_failure(report, stdout, stderr, 'completed', exit_code=0)

    def test_post_source_rejection_after_timeout_keeps_failed_report_with_no_fallback(self):
        self.source_closure.side_effect = [self.closure, ValueError('controlled source byte pin mismatch')]
        stdout = self.passing_bytes()
        report = self.invoke(stdout)
        self.assert_raw_failure(report, stdout, b'', 'timed_out')
        self.assertIsNone(report['source_closure_after'])
        self.assertEqual(report['source_closure_before'], self.closure)
        self.assertEqual(self.source_closure.call_count, 2)
        self.assertTrue(any('controlled source byte pin mismatch' in error
                            for group in report['reports'] for error in group['errors']))

    def test_post_case_read_failure_after_timeout_keeps_failed_report(self):
        stdout = self.passing_bytes()
        report = self.invoke(stdout, after_process=self.cases.unlink)
        self.assert_raw_failure(report, stdout, b'', 'timed_out')
        self.assertIsNone(report['cases_sha256_after'])

    def test_preflight_source_rejection_never_dispatches_or_creates_a_pass_report(self):
        self.source_closure.side_effect = ValueError('controlled preflight source rejection')
        with self.assertRaisesRegex(ValueError, 'controlled preflight source rejection'):
            self.invoke(mode='start_error')
        self.assertEqual(self.dispatches, 0)
        self.assertFalse((self.output / 'report.json').exists())

    def test_existing_output_is_never_overwritten_or_reused(self):
        self.output.mkdir()
        sentinel = self.output / 'report.json'
        sentinel.write_bytes(b'owned existing evidence')
        with self.assertRaises(FileExistsError):
            self.invoke(mode='start_error')
        self.assertEqual(self.dispatches, 0)
        self.assertEqual(sentinel.read_bytes(), b'owned existing evidence')

    def test_cli_keeps_nonzero_conclusion_and_persisted_report_on_real_start_failure(self):
        code, report = self.invoke(mode='start_error', cli=True)
        self.assertEqual(code, 1)
        self.assert_raw_failure(report, b'', b'', 'process_error')
        self.assertEqual(self.source_closure.call_count, 3)


if __name__ == '__main__':
    unittest.main()
