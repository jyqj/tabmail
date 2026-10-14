"""Bounded fork artifact metadata regressions through public capture/CLI."""
import builtins
from contextlib import ExitStack, contextmanager
import io
import mmap
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


@contextmanager
def artifact_body_guard(test, excluded):
    """Allow exact metadata acquisition; fail before any artifact body I/O.

    Resolve dir_fd paths and track identities too: FD aliases and /proc reopen
    must not bypass a basename-only assertion. Closing removes reused FD IDs.
    """
    original_open, original_close = os.open, os.close
    identities = {(p.stat().st_dev, p.stat().st_ino) for p in excluded}
    fds, observations = set(), []

    def artifact(value, dir_fd=None):
        if isinstance(value, int):
            if value in fds: return True
            try: info = os.fstat(value)
            except OSError: return False
        else:
            # Also retain path membership if an artifact is replaced with a new
            # inode. Relative names are resolved through their actual parent FD.
            candidate = Path(value)
            if not candidate.is_absolute() and dir_fd is not None:
                candidate = Path(os.readlink('/proc/self/fd/' + str(dir_fd))) / candidate
            if candidate.absolute() in excluded: return True
            try: info = os.stat(value, dir_fd=dir_fd, follow_symlinks=True)
            except (OSError, TypeError): return False
        return (info.st_dev, info.st_ino) in identities

    def open_guard(path, flags, *args, **kwargs):
        is_artifact = artifact(path, kwargs.get('dir_fd'))
        if is_artifact:
            # O_RDONLY == 0: testing flags & O_RDONLY cannot detect body opens.
            test.assertEqual(flags, os.O_PATH | os.O_NOFOLLOW, 'artifact body open flags')
            observations.append(flags)
        fd = original_open(path, flags, *args, **kwargs)
        if is_artifact: fds.add(fd)
        return fd

    def close_guard(fd):
        fds.discard(fd)
        return original_close(fd)

    def body_guard(reader):
        def guarded(value, *args, **kwargs):
            test.assertFalse(artifact(value), 'artifact body read API')
            return reader(value, *args, **kwargs)
        return guarded

    with ExitStack() as stack:
        stack.enter_context(mock.patch.object(os, 'open', open_guard))
        stack.enter_context(mock.patch.object(os, 'close', close_guard))
        for owner, names in [(Path, ['read_bytes', 'read_text', 'open']),
                             (builtins, ['open']), (io, ['open', 'open_code', 'FileIO']),
                             (os, ['read', 'pread', 'readv', 'preadv', 'readinto', 'fdopen', 'copy_file_range', 'splice']),
                             (mmap, ['mmap'])]:
            for name in names:
                if hasattr(owner, name):
                    stack.enter_context(mock.patch.object(owner, name, body_guard(getattr(owner, name))))
        if hasattr(os, 'sendfile'):
            original_sendfile = os.sendfile
            def sendfile_guard(out_fd, in_fd, *args, **kwargs):
                test.assertFalse(artifact(in_fd), 'artifact body read API')
                return original_sendfile(out_fd, in_fd, *args, **kwargs)
            stack.enter_context(mock.patch.object(os, 'sendfile', sendfile_guard))
        yield observations
        test.assertFalse(fds, 'artifact metadata FD leaked')


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
        with artifact_body_guard(self, excluded) as observations:
            self.assertEqual(before, self.capture())
            self.assertEqual(before, inventory.validate_current_source(before, self.root,
                purpose='protocol', policy=inventory.CURRENT_POLICY))
            # Two handles per file, two topology passes per capture, then
            # capture and validate. No body-readable file acquisition.
            self.assertEqual(len(observations), len(excluded) * 8)
        self.assertFalse(any('/build/' in name or '/node_modules/' in name for name in before['files']))
        result = self.cli('capture', '--build-context', json.dumps(fixtures.context()))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertEqual(json.loads(result.stdout), before)

    def test_body_guard_rejects_readable_flags_and_every_body_api(self):
        path = self.root / 'third_party/go-smtp/build/opaque.bin'
        path.parent.mkdir(); path.write_bytes(b'synthetic body')
        flags = [os.O_RDONLY, os.O_WRONLY, os.O_RDWR, os.O_NOFOLLOW,
                 os.O_RDONLY | os.O_NONBLOCK, os.O_RDONLY | os.O_DIRECTORY,
                 os.O_PATH, os.O_PATH | os.O_NOFOLLOW | os.O_RDWR,
                 os.O_PATH | os.O_NOFOLLOW | os.O_TRUNC,
                 os.O_PATH | os.O_NOFOLLOW | os.O_APPEND,
                 os.O_PATH | os.O_NOFOLLOW | os.O_CREAT]
        # Readable and metadata FDs both must be rejected by read API guards.
        for mode in [os.O_RDONLY, os.O_PATH | os.O_NOFOLLOW]:
            fd = os.open(path, mode)
            try:
                with artifact_body_guard(self, {path}):
                    for flag in flags:
                        with self.subTest(flags=flag), self.assertRaisesRegex(AssertionError, 'body open flags'):
                            os.open(path, flag)
                    for reader, args in [(Path.read_bytes, (path,)), (Path.read_text, (path,)),
                                         (Path.open, (path, 'rb')), (builtins.open, (path, 'rb')),
                                         (io.open, (path, 'rb')), (io.FileIO, (path, 'rb')),
                                         (io.FileIO, (fd, 'rb')), (builtins.open, (fd, 'rb')),
                                         (io.open, (fd, 'rb')), (os.fdopen, (fd, 'rb')),
                                         (os.read, (fd, 1)), (mmap.mmap, (fd, 1))]:
                        with self.subTest(api=reader, mode=mode), self.assertRaisesRegex(AssertionError, 'body read API'):
                            reader(*args)
                    for name, args in [('pread', (fd, 1, 0)), ('readv', (fd, [bytearray(1)])),
                                       ('preadv', (fd, [bytearray(1)], 0)),
                                       ('readinto', (fd, bytearray(1))), ('open_code', (str(path),)),
                                       ('copy_file_range', (fd, -1, 1)), ('splice', (fd, -1, 1)),
                                       ('sendfile', (-1, fd, 0, 1))]:
                        owner = io if name == 'open_code' else os
                        if hasattr(owner, name):
                            with self.subTest(api=name, mode=mode), self.assertRaisesRegex(AssertionError, 'body read API'):
                                getattr(owner, name)(*args)
                    with self.assertRaisesRegex(AssertionError, 'body open flags'):
                        os.open('/proc/self/fd/' + str(fd), os.O_RDONLY)
                    metadata = os.open(path, os.O_PATH | os.O_NOFOLLOW)
                    os.close(metadata)
            finally:
                os.close(fd)

    def test_metadata_no_follow_rejects_excluded_ancestor_alias(self):
        # O_PATH allowance cannot authorize following a directory alias.
        fork = self.root / 'third_party/go-smtp'
        outside = self.root.parent / 'outside'; outside.mkdir()
        (outside / 'opaque.bin').touch()
        (fork / 'build').symlink_to(outside, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.capture()
        with self.assertRaisesRegex(ValueError, 'metadata unavailable'):
            inventory._check_excluded_metadata(self.root, fork / 'build/opaque.bin', [0])

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
