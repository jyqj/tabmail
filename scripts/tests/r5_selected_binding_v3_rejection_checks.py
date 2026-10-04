"""Root independent controls: diagnostic faults must not hide binding rejection.

Pure synthetic receipts; no Go, repository runtime, database, or service runs.
Invoked explicitly, without changing formal test discovery or routing.
"""
import copy
import errno
import unittest
from unittest import mock

import r5_selected_source_binding_v3 as v3


class RejectionEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.receipt = v3._seal(dict(
            policy=v3.POLICY, schema_version=3,
            base_source={'build_context': v3.DEFAULT_CONTEXT},
            observation_envelope=[dict(
                role='attested_observation', argv=['go', 'list'], exit=0,
                raw_stdout_sha256='a' * 64, raw_stdout_bytes=1,
                binding_stdout_sha256='b' * 64, binding_stdout_domain='package',
                stderr_sha256='c' * 64, stderr_bytes=0)]))
        self.observed = copy.deepcopy(self.receipt)
        self.observed['changed_source'] = 'synthetic drift'
        self.observed = v3._seal(self.observed)
        self.pin = self.receipt['observation_sha256']

    def validate(self):
        return v3.validate(self.receipt, 'unused', 'unused', cache='unused',
                           modulecache='unused', trusted_observation_sha256=self.pin)

    def check_secondary(self, failure, target):
        with mock.patch.object(v3, 'capture', return_value=self.observed), \
                mock.patch.object(v3.Retention, target, side_effect=failure), \
                self.assertRaisesRegex(ValueError, 'drifted/context mismatch') as caught:
            self.validate()
        self.assertEqual(caught.exception.retention_errors, (failure,))
        self.assertIs(caught.exception.retention_errors[0], failure)
        return caught.exception

    def test_disk_full_keeps_binding_mismatch(self):
        failure = OSError(errno.ENOSPC, 'private synthetic path must not leak')
        error = self.check_secondary(failure, 'json')
        self.assertNotIn('private synthetic path', '\n'.join(error.__notes__))
        self.assertIn('errno=28', '\n'.join(error.__notes__))

    def test_retention_limit_keeps_binding_mismatch(self):
        self.check_secondary(ValueError('diagnostic retention bound exceeded'), 'json')

    def test_constructor_failure_keeps_binding_mismatch(self):
        self.check_secondary(PermissionError(errno.EACCES, 'private root'), '__init__')

    def test_context_cleanup_failure_keeps_binding_mismatch(self):
        self.check_secondary(OSError(errno.EIO, 'synthetic close failure'), 'close')

    def test_successful_retention_records_observed_then_rejects(self):
        with mock.patch.object(v3, 'capture', return_value=self.observed), \
                mock.patch.object(v3.Retention, 'json') as save, \
                self.assertRaisesRegex(ValueError, 'drifted/context mismatch') as caught:
            self.validate()
        save.assert_called_once_with('observed.json', self.observed)
        self.assertFalse(hasattr(caught.exception, 'retention_errors'))

    def test_capture_failure_object_is_unchanged(self):
        failure = RuntimeError('synthetic capture rejection')
        with mock.patch.object(v3, 'capture', side_effect=failure), \
                mock.patch.object(v3.Retention, 'json') as save, \
                self.assertRaises(RuntimeError) as caught:
            self.validate()
        self.assertIs(caught.exception, failure)
        save.assert_not_called()

    def test_altered_envelope_rejects_before_capture(self):
        self.receipt['observation_envelope'][0]['raw_stdout_sha256'] = 'd' * 64
        self.receipt = v3._seal(self.receipt)
        with mock.patch.object(v3, 'capture') as capture, \
                self.assertRaisesRegex(ValueError, 'pin mismatch'):
            self.validate()
        capture.assert_not_called()

    def test_matching_binding_keeps_normal_success(self):
        observed = copy.deepcopy(self.receipt)
        observed['observation_envelope'][0]['raw_stdout_sha256'] = 'd' * 64
        observed = v3._seal(observed)
        with mock.patch.object(v3, 'capture', return_value=observed), \
                mock.patch.object(v3.Retention, 'json') as save:
            self.assertIs(self.validate(), observed)
        save.assert_not_called()


if __name__ == '__main__':
    unittest.main()
