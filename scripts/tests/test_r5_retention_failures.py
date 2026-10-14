"""P2 completion controls: secondary failures and raw descriptor ownership."""
import errno
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import check_r5_compatibility as compatibility
import r5_private_diagnostics as private
import r5_selected_source_binding_v2 as binding


class RetentionFailureTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir='/tmp')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        patch = mock.patch.dict(os.environ, {'R5_DIAGNOSTIC_ROOT': str(self.root)})
        patch.start()
        self.addCleanup(patch.stop)

    def test_compatibility_limit_records_secondary_and_preserves_original_fields(self):
        error = subprocess.CalledProcessError(7, ['go'], output=b'original', stderr=b'private')
        with mock.patch.object(private, 'MAX_FILE_BYTES', 2), mock.patch.object(compatibility.subprocess, 'run', side_effect=error):
            with self.assertRaises(subprocess.CalledProcessError) as caught:
                compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception, error)
        self.assertEqual((error.returncode,error.cmd,error.stdout,error.stderr), (7,['go'],b'original',b'private'))
        self.assertIsInstance(error.retention_errors[0], ValueError)
        self.assertIn('diagnostic retention failed: ValueError', error.__notes__[0])
        self.assertNotIn('private', str(error.__notes__))

    def test_compatibility_timeout_records_write_fault_without_replacement(self):
        error = subprocess.TimeoutExpired(['go'], 60, output=b'original', stderr=b'private')
        fault = OSError(errno.ENOSPC, 'sensitive path not surfaced')
        before = len(list(Path('/proc/self/fd').iterdir()))
        with mock.patch.object(private.Retention, 'write', side_effect=fault), mock.patch.object(compatibility.subprocess, 'run', side_effect=error):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:
                compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception, error)
        self.assertEqual(error.retention_errors, (fault,))
        self.assertEqual((error.cmd,error.timeout,error.stdout,error.stderr), (['go'],60,b'original',b'private'))
        self.assertNotIn('sensitive', str(error.__notes__))
        self.assertEqual(len(list(Path('/proc/self/fd').iterdir())), before)

    def test_successful_compatibility_remains_fail_closed_on_retention_failure(self):
        result = subprocess.CompletedProcess(['go'], 0, b'original', b'')
        fault = OSError(errno.ENOSPC, 'synthetic write fault')
        with mock.patch.object(private.Retention, 'write', side_effect=fault), mock.patch.object(compatibility.subprocess, 'run', return_value=result):
            with self.assertRaises(OSError) as caught:
                compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception, fault)

    def selected_result(self, result):
        base = {'files': {'scripts/r5_selected_source_binding_v2.py': {}}, 'archive_static': {}}
        kwargs = {'side_effect': result} if isinstance(result, Exception) else {'return_value': result}
        with mock.patch.object(binding.inventory, 'capture_current_source', return_value=base), mock.patch.object(binding, 'topology', return_value=set()), mock.patch.object(binding, 'digest', return_value={'scripts/r5_selected_source_binding_v2.py':binding._IMPLEMENTATION_SHA256}), mock.patch.object(binding.subprocess, 'run', **kwargs):
            binding.capture(self.root, Path('/go'), cache=self.root, modulecache=self.root)

    def test_metadata_failure_constructed_before_secondary_write_fault(self):
        result = subprocess.CompletedProcess([], 7, b'original', b'private')
        fault = OSError(errno.ENOSPC, 'synthetic write fault')
        before = len(list(Path('/proc/self/fd').iterdir()))
        with mock.patch.object(private.Retention, 'write', side_effect=fault):
            with self.assertRaises(binding.MetadataCommandFailure) as caught:
                self.selected_result(result)
        error = caught.exception
        self.assertEqual((error.command['exit'],error.command['argv'],error.stdout,error.stderr), (7,['/go','env','-json'],b'original',b'private'))
        self.assertEqual(error.retention_errors, (fault,))
        self.assertEqual(len(list(Path('/proc/self/fd').iterdir())), before)

    def test_selected_timeout_preserves_object_and_records_secondary_limit(self):
        error = subprocess.TimeoutExpired(['/go'], 180, output=b'original', stderr=b'private')
        with mock.patch.object(private, 'MAX_FILE_BYTES', 2):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:
                self.selected_result(error)
        self.assertIs(caught.exception, error)
        self.assertIsInstance(error.retention_errors[0], ValueError)

    def test_successful_selected_command_remains_fail_closed(self):
        with mock.patch.object(private, 'MAX_FILE_BYTES', 2):
            with self.assertRaisesRegex(ValueError, 'retention bound'):
                self.selected_result(subprocess.CompletedProcess([],0,b'original',b''))

    def test_fdopen_failure_closes_only_still_owned_raw_descriptor(self):
        opened = []
        def fail(fd, *args):
            opened.append(fd)
            raise OSError(errno.ENOSPC, 'synthetic fdopen failure')
        with private.Retention('test') as sink, mock.patch.object(private.os, 'fdopen', side_effect=fail):
            with self.assertRaises(OSError):
                sink.write('partial', b'original')
            with self.assertRaises(OSError):
                os.fstat(opened[0])
            self.assertEqual((sink.count,sink.total), (0,0))
            self.assertFalse((sink.path/'partial').exists())

    def test_secondary_records_accumulate_without_replacing_existing_note(self):
        error = subprocess.CalledProcessError(7, ['go'])
        error.add_note('original producer note')
        fault = OSError(errno.ENOSPC, 'synthetic fault')
        with private.Retention('test') as sink, mock.patch.object(sink, 'command', side_effect=fault):
            sink.failed_command(0,b'',b'',7,error)
            sink.failed_command(0,b'',b'',7,error)
        self.assertEqual(error.retention_errors, (fault,fault))
        self.assertEqual(error.__notes__[0], 'original producer note')
        self.assertEqual(len(error.__notes__), 3)


if __name__ == '__main__':
    unittest.main()
