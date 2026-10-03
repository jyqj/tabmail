"""Independent fixed-head observations, including reproducible known defects.

Run explicitly: python3 -B -m unittest discover -s scripts/tests
  -p review_archive_checkpoint0c1f4f6.py -v
Passing counterexample tests mean the documented defect was reproduced.
All adversarial bodies and links belong to TemporaryDirectory fixtures.
"""
import hashlib
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

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_archive_boundary as boundary
import r5_source_inventory as inventory
import test_r5_archive_boundary as fixtures
import run_r5_source_version_tests as runner

ROOT = Path(__file__).resolve().parents[2]


class IndependentArchiveObservations(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='independent-r5-')
        self.addCleanup(self.temp.cleanup)
        self.sandbox = Path(self.temp.name)
        self.root = self.sandbox/'parent'/'root'
        self.root.mkdir(parents=True)
        fixtures.fixture(self.root)

    def test_git_blob_authority_36_originals_and_21_go(self):
        contract = boundary.registry(ROOT)
        self.assertEqual(len(contract['historical_files']), 36)
        self.assertEqual(len(contract['protected_go_files']), 21)
        for item in contract['historical_files']:
            raw = subprocess.check_output(['git', '-C', str(ROOT), 'show',
                                          boundary.BASELINE+':'+item['path']])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), item['sha256'])
            self.assertEqual(len(raw), item['size'])
            self.assertEqual(raw, (ROOT/item['path']).read_bytes())
        self.assertEqual(len(boundary.check(self.root)['modules']), 6)

    def test_mutable_registry_cannot_self_authorize(self):
        path = self.root/boundary.REGISTRY
        value = json.loads(path.read_bytes())
        value['production_roots'].append('newroot')
        path.write_text(json.dumps(value))
        with self.assertRaisesRegex(ValueError, 'registry version/bytes differ'):
            boundary.check(self.root)

    def test_own_symlinks_fifo_and_read_budget_refused(self):
        outside = self.sandbox/'outside.txt'
        outside.write_bytes(b'OWN_OUTSIDE_SENTINEL')
        p = self.root/'internal/link.go'
        p.symlink_to(outside)
        with self.assertRaises(ValueError):
            boundary.read(self.root, 'internal/link.go')
        p.unlink()
        os.mkfifo(p)
        with self.assertRaises(ValueError):
            boundary.read(self.root, 'internal/link.go')
        p.unlink()
        p.write_bytes(b'12345')
        with mock.patch.object(boundary, 'MAX_BYTES', 4):
            with self.assertRaisesRegex(ValueError, 'byte budget'):
                boundary.read(self.root, 'internal/link.go')
        for name in ['', '../outside.txt', '/own.txt', 'internal//link.go', 'internal/./link.go']:
            with self.subTest(name=name), self.assertRaises(ValueError):
                boundary.read(self.root, name)

    def test_entry_depth_budgets_include_ignored_non_go_metadata(self):
        p = self.root/'web/node_modules/a/b/own.bin'
        p.parent.mkdir(parents=True)
        p.write_bytes(b'OWN_BODY_NEVER_READ')
        with mock.patch.object(boundary, 'read', side_effect=AssertionError('body read')):
            boundary.topology(self.root)
        with mock.patch.object(boundary, 'MAX_ENTRIES', 1):
            with self.assertRaisesRegex(ValueError, 'entry budget'):
                boundary.topology(self.root)
        with mock.patch.object(boundary, 'MAX_DEPTH', 1):
            with self.assertRaisesRegex(ValueError, 'depth budget'):
                boundary.topology(self.root)

    def test_unclassified_and_hidden_production_rejected(self):
        for name in ['newroot/no_dependencies.go', 'internal/build/hidden.go',
                     'web/node_modules/own/tagged_windows.go', 'internal/hidden/go.mod',
                     'third_party/go-smtp/out/hidden/go.mod']:
            p = self.root/name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text('module hidden\ngo 1.25.7\n' if name.endswith('go.mod') else
                         '//go:build windows\n\npackage own\n')
            with self.subTest(name=name), self.assertRaises(ValueError):
                boundary.check(self.root)
            p.unlink()

    def test_counterexample_root_ancestor_swap_reads_actual_outside_bytes(self):
        outside = self.sandbox/'outside'
        (outside/'root').mkdir(parents=True)
        (outside/'root/own.txt').write_bytes(b'OWN_OUTSIDE_ROOT_SENTINEL')
        (self.root/'own.txt').write_bytes(b'OWN_INSIDE_ROOT_SENTINEL')
        ancestor = self.root.parent
        saved = self.sandbox/'saved-parent'
        original = os.open
        swapped = []
        def open_after_check(path, flags, *args, **kwargs):
            if Path(path) == self.root and not swapped:
                ancestor.rename(saved)
                ancestor.symlink_to(outside, target_is_directory=True)
                try:
                    fd = original(path, flags, *args, **kwargs)
                    swapped.append(os.fstat(fd).st_ino == (outside/'root').stat().st_ino)
                finally:
                    ancestor.unlink()
                    saved.rename(ancestor)
                return fd
            return original(path, flags, *args, **kwargs)
        with mock.patch.object(boundary.os, 'open', side_effect=open_after_check):
            observed = boundary.read(self.root, 'own.txt')
        self.assertEqual(swapped, [True])
        self.assertEqual(observed, b'OWN_OUTSIDE_ROOT_SENTINEL')
        self.assertEqual((self.root/'own.txt').read_bytes(), b'OWN_INSIDE_ROOT_SENTINEL')

    def test_counterexample_complete_guard_reads_outside_module_and_passes(self):
        # The production root grammar still uses checked_path(...).read_text().
        # Open a distinct own module body after check, restore before later scans.
        path = self.root/'go.mod'
        saved = self.root/'own-saved-mod'
        raw = path.read_bytes()
        outside = self.sandbox/'own-outside-mod'
        outside.write_bytes(raw+b'\n// OWN_OUTSIDE_MODULE_SENTINEL\n')
        checked = inventory.checked_path
        original_open = io.open
        armed = []
        reads = []
        def checked_then_swap(root, name, **kwargs):
            result = checked(root, name, **kwargs)
            if name == 'go.mod' and not armed:
                path.rename(saved)
                path.symlink_to(outside)
                armed.append(True)
            return result
        def open_and_restore(file, *args, **kwargs):
            stream = original_open(file, *args, **kwargs)
            if file == path and saved.exists():
                reads.append(os.fstat(stream.fileno()).st_ino == outside.stat().st_ino)
                path.unlink()
                saved.rename(path)
            return stream
        with mock.patch.object(inventory, 'checked_path', side_effect=checked_then_swap), \
                mock.patch.object(io, 'open', side_effect=open_and_restore):
            observed = boundary.check(self.root)
        self.assertEqual(reads, [True])
        self.assertEqual(len(observed['modules']), 6)
        self.assertEqual(path.read_bytes(), raw)

    def test_counterexample_expected_failure_child_is_green_and_unreported(self):
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
        observed = json.loads(report.read_text())
        self.assertEqual(code, 0)
        self.assertNotIn('expected_failures', observed)
        self.assertEqual(observed['tests_run'], 1)
        self.assertEqual(observed['failures'], [])


if __name__ == '__main__':
    unittest.main(verbosity=2)
