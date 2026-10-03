"""Bounded fork artifact metadata regressions through public capture/CLI."""
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import test_r5_smtp_owner_source_inventory as fixtures

inventory = fixtures.inventory


class ExcludedMetadataTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'src'
        self.root.mkdir()
        fixtures.fixture(self.root)

    def capture(self):
        return inventory.capture_current_source(self.root, purpose='protocol',
            policy=inventory.CURRENT_POLICY, build_context=fixtures.context())

    def cli(self, action, *options):
        return subprocess.run([sys.executable, str(fixtures.SCRIPTS / 'r5_source_inventory.py'),
            action, '--root', str(self.root), '--purpose', 'protocol',
            '--policy', inventory.CURRENT_POLICY, *options],
            capture_output=True, text=True, timeout=15)

    def test_valid_excluded_files_never_read_or_enter_source(self):
        before = self.capture()
        excluded = set()
        for replacement in inventory.CURRENT_REPLACEMENTS:
            for directory in sorted(inventory.EXCLUDED_DIRS):
                base = self.root / replacement['path'] / directory / 'layer' / 'build'
                base.mkdir(parents=True)
                for name in ['.env.secret', 'output.go', 'opaque.bin', '.DS_Store']:
                    path = base / name
                    path.write_bytes(b'synthetic artifact bytes')
                    path.chmod(0)
                    excluded.add(path)
        original_bytes, original_text = Path.read_bytes, Path.read_text
        def bytes_guard(path, *args, **kwargs):
            self.assertNotIn(path, excluded)
            return original_bytes(path, *args, **kwargs)
        def text_guard(path, *args, **kwargs):
            self.assertNotIn(path, excluded)
            return original_text(path, *args, **kwargs)
        original_open = os.open
        def open_guard(path, *args, **kwargs):
            self.assertNotIn(Path(path).name, {p.name for p in excluded})
            return original_open(path, *args, **kwargs)
        with mock.patch.object(Path, 'read_bytes', bytes_guard), mock.patch.object(Path, 'read_text', text_guard), mock.patch.object(os, 'open', open_guard):
            self.assertEqual(before, self.capture())
            self.assertEqual(before, inventory.validate_current_source(before, self.root,
                purpose='protocol', policy=inventory.CURRENT_POLICY))
        self.assertFalse(any('/build/' in name or '/node_modules/' in name for name in before['files']))
        result = self.cli('capture', '--build-context', json.dumps(fixtures.context()))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(json.loads(result.stdout), before)

    def test_multilevel_modules_links_and_special_files_rejected_by_cli(self):
        before = self.capture()
        receipt = self.root.parent / 'receipt.json'
        receipt.write_bytes(inventory.canonical(before))
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        for replacement in inventory.CURRENT_REPLACEMENTS:
            base = self.root / replacement['path'] / 'build' / 'a' / 'node_modules' / 'b'
            base.mkdir(parents=True)
            for kind in ['module', 'file_link', 'directory_link', 'dangling_link', 'fifo', 'socket']:
                path = base / ('go.mod' if kind == 'module' else '.env.secret')
                sock = None
                if kind == 'module': path.write_bytes(b'never parse this module body')
                elif kind == 'file_link': path.symlink_to(self.root / 'cmd/main.go')
                elif kind == 'directory_link': path.symlink_to(self.root / 'internal', target_is_directory=True)
                elif kind == 'dangling_link': path.symlink_to(self.root / 'missing')
                elif kind == 'fifo': os.mkfifo(path)
                else:
                    sock = socket.socket(socket.AF_UNIX)
                    sock.bind(str(path))
                try:
                    reason = 'nested module' if kind == 'module' else 'symlink' if 'link' in kind else 'special or unknown'
                    with self.subTest(module=replacement['module'], kind=kind):
                        for action, args in [('capture', ['--build-context', json.dumps(fixtures.context())]),
                            ('validate', ['--receipt', str(receipt), '--receipt-sha256', pin])]:
                            result = self.cli(action, *args)
                            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                            self.assertIn(reason, json.loads(result.stdout)['error'])
                finally:
                    if sock is not None: sock.close()
                    path.unlink()

    def test_entry_budget_shared_between_excluded_roots_and_depth_limit(self):
        receipt = self.root.parent / 'budget-receipt.json'
        receipt.write_bytes(inventory.canonical(self.capture()))
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        for replacement in inventory.CURRENT_REPLACEMENTS:
            fork = self.root / replacement['path']
            first, second = fork / 'build', fork / 'dist'
            first.mkdir(); second.mkdir()
            # Each root counts once. Together exactly the budget is valid.
            for index in range(inventory.EXCLUDED_METADATA_MAX_ENTRIES - 2):
                (first if index % 2 else second).joinpath(str(index)).touch()
            self.capture()
            extra = second / 'over-budget'; extra.touch()
            with self.assertRaisesRegex(ValueError, 'entry budget'): self.capture()
            result = self.cli('capture', '--build-context', json.dumps(fixtures.context()))
            self.assertEqual(result.returncode, 1); self.assertIn('entry budget', result.stdout)
            result = self.cli('validate', '--receipt', str(receipt), '--receipt-sha256', pin)
            self.assertEqual(result.returncode, 1); self.assertIn('entry budget', result.stdout)
            for base in [first, second]:
                for path in base.iterdir(): path.unlink()
                base.rmdir()
            base = fork / 'build'; base.mkdir()
            current = base
            for _ in range(inventory.EXCLUDED_METADATA_MAX_DEPTH - 1):
                current = current / 'd'; current.mkdir()
            self.capture()
            over = current / 'over'; over.mkdir()
            with self.assertRaisesRegex(ValueError, 'depth budget'): self.capture()
            result = self.cli('capture', '--build-context', json.dumps(fixtures.context()))
            self.assertEqual(result.returncode, 1); self.assertIn('depth budget', result.stdout)
            result = self.cli('validate', '--receipt', str(receipt), '--receipt-sha256', pin)
            self.assertEqual(result.returncode, 1); self.assertIn('depth budget', result.stdout)
            over.rmdir()
            while current != fork:
                parent = current.parent; current.rmdir(); current = parent

    def test_unavailable_unknown_and_directory_swap_metadata_fail_closed(self):
        base = self.root / 'third_party/go-smtp/build'; base.mkdir()
        (base / 'd').mkdir()
        original_stat, original_open = os.stat, os.open
        def unknown(path, *args, **kwargs):
            if path == 'd' and kwargs.get('dir_fd') is not None:
                return mock.Mock(st_mode=0)
            return original_stat(path, *args, **kwargs)
        with mock.patch.object(os, 'stat', unknown):
            with self.assertRaisesRegex(ValueError, 'special or unknown'): self.capture()
        def unavailable(path, *args, **kwargs):
            if path == 'd' and kwargs.get('dir_fd') is not None: raise PermissionError('synthetic metadata denial')
            return original_stat(path, *args, **kwargs)
        with mock.patch.object(os, 'stat', unavailable):
            with self.assertRaisesRegex(ValueError, 'metadata unavailable'): self.capture()
        def swapped(path, *args, **kwargs):
            if path == 'd' and kwargs.get('dir_fd') is not None:
                (base / 'd').rmdir()
                (base / 'd').symlink_to(self.root / 'internal', target_is_directory=True)
            return original_open(path, *args, **kwargs)
        with mock.patch.object(os, 'open', swapped):
            with self.assertRaisesRegex(ValueError, 'metadata unavailable'): self.capture()

    def test_cli_receipt_pin_and_loaded_helper_hash_binding(self):
        result = self.cli('capture', '--build-context', json.dumps(fixtures.context()))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        observed = json.loads(result.stdout)
        helper = hashlib.sha256((fixtures.SCRIPTS / 'r5_source_inventory.py').read_bytes()).hexdigest()
        self.assertEqual(observed['inventory_implementation_sha256'], helper)
        self.assertEqual(observed['files']['scripts/r5_source_inventory.py'], helper)
        receipt = self.root.parent / 'receipt.json'; receipt.write_text(result.stdout)
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        result = self.cli('validate', '--receipt', str(receipt), '--receipt-sha256', pin)
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(json.loads(result.stdout), observed)
        result = self.cli('validate', '--receipt', str(receipt), '--receipt-sha256', '0' * 64)
        self.assertEqual(result.returncode, 1); self.assertIn('byte hash differs', result.stdout)
        helper_path = self.root / 'scripts/r5_source_inventory.py'
        helper_path.write_bytes(helper_path.read_bytes() + b'\n# synthetic helper drift\n')
        for action, args in [('capture', ['--build-context', json.dumps(fixtures.context())]),
            ('validate', ['--receipt', str(receipt), '--receipt-sha256', pin])]:
            result = self.cli(action, *args)
            self.assertEqual(result.returncode, 1); self.assertIn('implementation differs', result.stdout)


if __name__ == '__main__':
    unittest.main()
