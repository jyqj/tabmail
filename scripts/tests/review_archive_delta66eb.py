"""Explicit delta checks; REVIEW_DELTA_ROOT must name the clean final checkout."""
import io
import json
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT = Path(os.environ['REVIEW_DELTA_ROOT']).resolve()
sys.path[:0] = [str(ROOT/'scripts'), str(ROOT/'scripts/tests')]
import r5_archive_boundary as boundary
import r5_source_inventory as inventory
import r5_selected_source_binding_v2 as binding
import run_r5_source_version_tests as runner
import test_r5_archive_boundary as fixtures


class IndependentFinalDelta(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='independent-r5-delta-')
        self.addCleanup(self.temp.cleanup)
        self.sandbox = Path(self.temp.name)
        self.root = self.sandbox/'parent'/'root'
        self.root.mkdir(parents=True)
        fixtures.fixture(self.root)
        for name in ['scripts/r5_archive_boundary.py', 'scripts/r5_selected_source_binding_v2.py',
                     'scripts/run_r5_source_version_tests.py', 'scripts/tests/test_r5_archive_boundary.py',
                     'scripts/tests/test_r5_selected_source_binding_v2.py', 'scripts/tests/test_r5_source_version_runner.py']:
            shutil.copyfile(ROOT/name, self.root/name)

    def test_root_ancestor_swap_before_open_refused_without_outside_read(self):
        outside = self.sandbox/'outside'
        (outside/'root').mkdir(parents=True)
        (outside/'root/own.txt').write_bytes(b'OWN_OUTSIDE_SENTINEL')
        (self.root/'own.txt').write_bytes(b'OWN_INSIDE_SENTINEL')
        parent = self.root.parent
        saved = self.sandbox/'saved-parent'
        original = os.open
        swaps = []
        def swap(path, flags, *args, **kwargs):
            if path == 'parent' and not swaps:
                parent.rename(saved)
                parent.symlink_to(outside, target_is_directory=True)
                swaps.append(True)
                try:
                    return original(path, flags, *args, **kwargs)
                finally:
                    parent.unlink()
                    saved.rename(parent)
            return original(path, flags, *args, **kwargs)
        with mock.patch.object(boundary.os, 'open', side_effect=swap):
            with self.assertRaises((ValueError, OSError)):
                boundary.read(self.root, 'own.txt')
        self.assertEqual(swaps, [True])

    def test_complete_v4_has_no_path_body_reads_from_v3(self):
        actual_open = io.open
        def reject_path_body(file, *args, **kwargs):
            if not isinstance(file, int) and Path(file).is_relative_to(self.root):
                self.fail('v4 attempted unchecked pathname body read: '+str(file))
            return actual_open(file, *args, **kwargs)
        with mock.patch.object(io, 'open', side_effect=reject_path_body):
            receipt = inventory.capture_current_source(self.root, purpose='selected',
                policy=inventory.ARCHIVE_POLICY, build_context=binding.DEFAULT_CONTEXT)
        self.assertEqual(receipt['schema_version'], 4)
        self.assertEqual(len(receipt['archive_static']), 36)
        self.assertFalse(receipt['version_test_contract']['expected_failures_are_green'])

    def test_leaf_swap_to_own_outside_reopens_original_object_then_rejects(self):
        p = self.root/'own.txt'
        p.write_bytes(b'OWN_INSIDE')
        outside = self.sandbox/'outside.txt'
        outside.write_bytes(b'OWN_OUTSIDE')
        saved = self.root/'saved-own'
        actual = os.open
        observed = []
        def swap(path, flags, *args, **kwargs):
            if str(path).startswith('/proc/self/fd/'):
                p.rename(saved)
                p.symlink_to(outside)
                fd = actual(path, flags, *args, **kwargs)
                observed.append(os.fstat(fd).st_ino == saved.stat().st_ino)
                return fd
            return actual(path, flags, *args, **kwargs)
        with mock.patch.object(boundary.os, 'open', side_effect=swap):
            with self.assertRaises(ValueError):
                boundary.read(self.root, 'own.txt')
        self.assertEqual(observed, [True])

    def test_expected_failure_child_red_and_reported(self):
        class KnownFailure(unittest.TestCase):
            @unittest.expectedFailure
            def test_own_failure(self):
                self.fail('OWN_EXPECTED_FAILURE_SENTINEL')
        case = KnownFailure('test_own_failure')
        report = self.sandbox/'child.json'
        before = Path.cwd()
        try:
            with mock.patch.object(runner.unittest.TestLoader, 'loadTestsFromNames',
                                   return_value=unittest.TestSuite([case])), \
                    mock.patch.object(runner, 'git', return_value=runner.BASELINE), \
                    mock.patch.object(runner, 'git_go_env', return_value=str(self.sandbox/'cache')):
                code = runner.child(self.root, [case.id()], report)
        finally:
            os.chdir(before)
        value = json.loads(report.read_text())
        self.assertEqual(code, 1)
        self.assertEqual(value['expected_failures'], [case.id()])
        self.assertEqual(value['actual_test_ids'], [case.id()])

    def test_empty_archive_family_directory_refused(self):
        p = self.root/'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V4-OWN'
        p.mkdir()
        with self.assertRaisesRegex(ValueError, 'directory'):
            boundary.check(self.root)

    def test_symlink_vcs_metadata_refused(self):
        own = self.sandbox/'own-vcs'
        own.mkdir()
        (self.root/'.git').symlink_to(own, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'VCS metadata'):
            boundary.check(self.root)

    def test_variant_directory_missing_ignored_file_refused(self):
        name = 'internal/own/tagged_windows.go'
        static = dict(production_go={name:'ownhash'}, production_variant_directories=['internal/own'])
        row = dict(Dir=str(self.root/'internal/own'), ImportPath='tabmail/internal/own',
                   Module=dict(Path='tabmail', Main=True, Dir=str(self.root)),
                   Error=dict(Err='build constraints exclude all Go files in own fixture'),
                   IgnoredGoFiles=['tagged_windows.go'])
        self.assertEqual(len(binding.compare_variants([row], self.root, static)), 1)
        row['IgnoredGoFiles'] = []
        with self.assertRaisesRegex(ValueError, 'coverage missing'):
            binding.compare_variants([row], self.root, static)

    def test_source_version_matrix_and_selected_v1_v2_disjoint(self):
        import r5_selected_source_binding as legacy
        policies = [(inventory.POLICY, 2), (inventory.CURRENT_POLICY, 3),
                    (inventory.ARCHIVE_POLICY, 4)]
        for receipt_policy, receipt_schema in policies:
            for requested_policy, requested_schema in policies:
                if requested_schema == receipt_schema:
                    continue
                with self.subTest(receipt=receipt_policy, requested=requested_policy), \
                        mock.patch.object(inventory, 'capture_current_source',
                                          side_effect=AssertionError('cross version dispatched')):
                    with self.assertRaises(ValueError):
                        inventory.validate_current_source(dict(policy=receipt_policy,
                            schema_version=receipt_schema), self.root,
                            purpose='protocol', policy=requested_policy)
        for validator, foreign in [(binding, legacy), (legacy, binding)]:
            with mock.patch.object(validator, 'capture', side_effect=AssertionError('cross selected dispatched')):
                with self.assertRaises(ValueError):
                    validator.validate(dict(policy=foreign.POLICY,
                        schema_version=1 if foreign is legacy else 2), self.root,
                        Path(os.environ['R5_TEST_GO']), cache=self.sandbox/'cache',
                        modulecache=self.sandbox/'modules')

    def test_hydration_failure_retains_own_stdout_stderr_and_role(self):
        original = subprocess.run
        go = Path(os.environ['R5_TEST_GO'])
        def fail(argv, *args, **kwargs):
            if argv[1:] == binding.ARGV:
                return subprocess.CompletedProcess(argv, 7, b'OWN_PARTIAL_STDOUT', b'OWN_FAILED_STDERR')
            return original(argv, *args, **kwargs)
        with mock.patch.object(binding.subprocess, 'run', side_effect=fail):
            with self.assertRaises(binding.MetadataCommandFailure) as caught:
                binding.capture(self.root, go, cache=self.sandbox/'cache', modulecache=self.sandbox/'modules')
        value = caught.exception
        self.assertEqual(value.command['role'], 'unbound_dependency_hydration_not_attested')
        self.assertEqual(value.command['exit'], 7)
        self.assertEqual(value.stdout, b'OWN_PARTIAL_STDOUT')
        self.assertEqual(value.stderr, b'OWN_FAILED_STDERR')


if __name__ == '__main__':
    unittest.main(verbosity=2)
