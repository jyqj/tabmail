"""Independent synthetic oracle. No author fixtures, Go, services, or artifact reads.

R5_REVIEW_INVENTORY selects exact helper bytes for the same oracle on old/new.
python3 -B scripts/tests/checkpoints/r5_excluded_independent_20261003/oracle.py -v
"""
import builtins
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import shutil
import socket
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

SOURCE = Path(os.environ.get('R5_REVIEW_INVENTORY',
    str(Path(__file__).resolve().parents[3] / 'r5_source_inventory.py')))
spec = importlib.util.spec_from_file_location('independent_inventory', SOURCE)
inventory = importlib.util.module_from_spec(spec)
spec.loader.exec_module(inventory)
POLICY = 'r5_current_local_inputs_smtp_owner_forks_v3'
FORKS = [('github.com/jhillyerd/enmime/v2', 'v2.3.0', 'third_party/enmime-v2.3.0',
          ['LICENSE', 'PROVENANCE.json', 'UPSTREAM-MANIFEST.json', 'UPSTREAM-DELTA.patch', 'TABMAIL-FORK.md']),
         ('github.com/emersion/go-smtp', 'v0.24.0', 'third_party/go-smtp',
          ['LICENSE', 'LOCAL-PROVENANCE.md', 'LOCAL-SOURCE-MANIFEST.json', 'UPSTREAM-MANIFEST.json', 'LOCAL-PATCH.diff', 'LOCAL-REVIEW-FIX.diff'])]
EXCLUDED = ['.git', 'node_modules', '.next', '.vercel', 'coverage', '__pycache__',
            '.pytest_cache', '.mypy_cache', '.cache', 'out', 'build', 'dist']
CONTEXT = dict(goos='linux', goarch='amd64', cgo_enabled=1,
               build_tag_sets=[['r5protocol']], race=True, go_work='off',
               go_flags='', selection='all_local_variants_superset')


def make_fixture(root):
    def put(name, text):
        target = root / name
        target.parent.mkdir(parents=True, exist_ok=True)
        target.write_text(text)
    for name in ['Dockerfile', 'Makefile', '.env.example', '.gitignore', 'go.sum',
                 'docs/company-mail/R5-PROTOCOL.md', 'web/package.json',
                 'scripts/tests/test_r5_smtp_owner_source_inventory.py']:
        put(name, 'synthetic independent fixture\n')
    put('docs/company-mail/evidence/R5-PROTOCOL-CASES.json', '{"cases":[]}')
    put('cmd/main.go', 'package main\n')
    put('internal/fixture/input.go', 'package fixture\n')
    (root / 'scripts/r5_source_inventory.py').write_bytes(SOURCE.read_bytes())
    put('go.mod', 'module tabmail\ngo 1.25.7\nrequire (\n' +
        ''.join(f' {module} {version}\n' for module, version, _, _ in FORKS) + ')\n' +
        ''.join(f'replace {module} {version} => ./{base}\n' for module, version, base, _ in FORKS))
    for module, version, base, metadata in FORKS:
        put(base + '/go.mod', f'module {module}\ngo 1.25.7\n')
        put(base + '/go.sum', '')
        put(base + '/input.go', 'package fixture\n')
        put(base + '/retained.bin', 'synthetic local source\n')
        for member in metadata:
            put(base + '/' + member, '{}\n' if member.endswith('.json') else 'synthetic metadata\n')
        digest = hashlib.sha256((root / base / 'input.go').read_bytes()).hexdigest()
        field = 'files' if module.endswith('/v2') else 'files_sha256'
        value = {'bytes': (root / base / 'input.go').stat().st_size, 'sha256': digest} if field == 'files' else digest
        put(base + '/UPSTREAM-MANIFEST.json', json.dumps({'module': module, 'version': version, field: {'input.go': value}}))


class IndependentExcludedOracle(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='r5-independent-')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name) / 'source'
        self.root.mkdir()
        make_fixture(self.root)

    def capture(self):
        return inventory.capture_current_source(self.root, purpose='protocol', policy=POLICY, build_context=CONTEXT)

    def validate(self, receipt):
        return inventory.validate_current_source(receipt, self.root, purpose='protocol', policy=POLICY)

    def both_reject(self, receipt):
        for action in [self.capture, lambda: self.validate(receipt)]:
            with self.subTest(entry=action.__name__):
                with self.assertRaises((ValueError, OSError)):
                    action()

    def test_legal_artifacts_no_reads_and_stable_original_inventory_and_closure(self):
        baseline = self.capture()
        artifacts = set()
        for _, _, fork, _ in FORKS:
            for directory in EXCLUDED:
                base = self.root / fork / directory / 'first' / 'dist' / 'last'
                base.mkdir(parents=True)
                for name in ['opaque.bin', '.env.synthetic', 'ignored.go', '.DS_Store']:
                    path = base / name
                    path.write_bytes(b'synthetic only')
                    artifacts.add(path)
        original_bytes, original_text, original_open, original_os_open = Path.read_bytes, Path.read_text, builtins.open, os.open
        def guard(reader):
            def wrapped(path, *args, **kwargs):
                if not isinstance(path, int):
                    self.assertNotIn(Path(path), artifacts, 'artifact body opened')
                return reader(path, *args, **kwargs)
            return wrapped
        with mock.patch.object(Path, 'read_bytes', guard(original_bytes)), mock.patch.object(Path, 'read_text', guard(original_text)), mock.patch.object(builtins, 'open', guard(original_open)), mock.patch.object(os, 'open', guard(original_os_open)):
            self.assertEqual(self.capture(), baseline)
            self.assertEqual(self.validate(baseline), baseline)
        for path in artifacts:
            path.write_bytes(b'changed synthetic bytes')
            path.rename(path.with_name(path.name + '.renamed'))
        self.assertEqual(self.capture(), baseline)
        self.assertEqual(self.validate(baseline), baseline)
        for _, _, fork, _ in FORKS:
            for directory in EXCLUDED:
                shutil.rmtree(self.root / fork / directory)
        self.assertEqual(self.capture(), baseline)
        included = self.root / FORKS[0][2] / 'retained.bin'
        included.write_text('changed local source\n')
        changed = self.capture()
        self.assertEqual(set(changed['files']), set(baseline['files']))
        self.assertNotEqual(changed['source_closure_sha256'], baseline['source_closure_sha256'])
        with self.assertRaises(ValueError): self.validate(baseline)

    def test_nested_module_links_fifo_socket_public_entries(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            base = self.root / fork / 'build' / 'first' / '.cache' / 'last'
            base.mkdir(parents=True)
            for kind in ['module', 'file_link', 'directory_link', 'dangling_link', 'fifo', 'socket']:
                with self.subTest(fork=fork, kind=kind):
                    path = base / ('go.mod' if kind == 'module' else 'opaque')
                    sock = None
                    if kind == 'module': path.touch()
                    elif kind == 'file_link': path.symlink_to(self.root / 'cmd/main.go')
                    elif kind == 'directory_link': path.symlink_to(self.root / 'internal')
                    elif kind == 'dangling_link': path.symlink_to(base / 'absent')
                    elif kind == 'fifo': os.mkfifo(path)
                    else:
                        sock = socket.socket(socket.AF_UNIX)
                        sock.bind(str(path))
                    try: self.both_reject(receipt)
                    finally:
                        if sock: sock.close()
                        path.unlink()

    def test_budget_exact_shared_roots_per_fork_and_overflow(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            for dirname in ['build', 'dist']:
                base = self.root / fork / dirname
                base.mkdir()
                for index in range(2047): (base / str(index)).touch()
        self.assertEqual(self.capture(), receipt)  # 4096 each fork, not global.
        self.assertEqual(self.validate(receipt), receipt)
        for _, _, fork, _ in FORKS:
            extra = self.root / fork / 'dist' / 'one-over'
            extra.touch()
            self.both_reject(receipt)
            extra.unlink()

    def test_depth_exact_and_overflow(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            current = self.root / fork / 'out'
            current.mkdir()
            for _ in range(31):
                current /= 'level'
                current.mkdir()
            self.assertEqual(self.capture(), receipt)
            self.assertEqual(self.validate(receipt), receipt)
            extra = current / 'one-over'
            extra.mkdir()
            self.both_reject(receipt)
            extra.rmdir()

    def test_unknown_and_unavailable_metadata_public_entries(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            base = self.root / fork / 'coverage'
            base.mkdir()
            (base / 'opaque').touch()
            original = os.stat
            for kind in ['unknown', 'unavailable']:
                def injected(path, *args, **kwargs):
                    if path == 'opaque' and kwargs.get('dir_fd') is not None:
                        if kind == 'unknown': return mock.Mock(st_mode=0)
                        raise PermissionError('synthetic metadata denial')
                    return original(path, *args, **kwargs)
                with self.subTest(fork=fork, kind=kind), mock.patch.object(os, 'stat', injected):
                    self.both_reject(receipt)

    def test_directory_swap_nofollow_public_entries(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            base = self.root / fork / '.next'
            base.mkdir()
            target = base / 'switch'
            original = os.open
            for action in [self.capture, lambda: self.validate(receipt)]:
                target.mkdir()
                def injected(path, flags, *args, **kwargs):
                    if path == 'switch' and kwargs.get('dir_fd') is not None:
                        self.assertTrue(flags & os.O_NOFOLLOW)
                        target.rmdir()
                        target.symlink_to(self.root / 'internal')
                    return original(path, flags, *args, **kwargs)
                try:
                    with self.subTest(fork=fork, entry=action.__name__), mock.patch.object(os, 'open', injected):
                        with self.assertRaises((ValueError, OSError)): action()
                finally:
                    if target.is_symlink(): target.unlink()
                    else: target.rmdir()

    def test_regular_file_becomes_symlink_on_final_stat_public_entries(self):
        receipt = self.capture()
        for _, _, fork, _ in FORKS:
            base = self.root / fork / '.cache'
            base.mkdir()
            target = base / 'switch'
            original = os.stat
            for action in [self.capture, lambda: self.validate(receipt)]:
                target.touch()
                calls = []
                def injected(path, *args, **kwargs):
                    result = original(path, *args, **kwargs)
                    if path == 'switch' and kwargs.get('dir_fd') is not None and kwargs.get('follow_symlinks') is False:
                        calls.append(1)
                        if len(calls) == 2:
                            target.unlink()
                            target.symlink_to(self.root / 'cmd/main.go')
                    return result
                try:
                    with self.subTest(fork=fork, entry=action.__name__), mock.patch.object(os, 'stat', injected):
                        with self.assertRaises((ValueError, OSError)): action()
                finally:
                    print('RACE_OBSERVATION ' + json.dumps({'fork': fork, 'entry': action.__name__, 'nofollow_stats': len(calls), 'symlink_at_return': target.is_symlink()}), flush=True)
                    target.unlink()

    def test_actual_cli_capture_validate_reject(self):
        if not hasattr(inventory, 'main'):
            self.skipTest('old helper has no CLI; public capture/validate oracle still runs')
        helper = self.root / 'scripts/r5_source_inventory.py'
        def cli(action, *args):
            return subprocess.run([sys.executable, '-B', str(helper), action, '--root', str(self.root), '--purpose', 'protocol', '--policy', POLICY, *args], capture_output=True, text=True, timeout=10)
        result = cli('capture', '--build-context', json.dumps(CONTEXT))
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        receipt = self.root.parent / 'receipt.json'
        receipt.write_text(result.stdout)
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        self.assertEqual(cli('validate', '--receipt', str(receipt), '--receipt-sha256', pin).returncode, 0)
        self.assertEqual(cli('validate', '--receipt', str(receipt), '--receipt-sha256', '0' * 64).returncode, 1)
        for _, _, fork, _ in FORKS:
            base = self.root / fork / 'node_modules' / 'first' / 'dist' / 'last'
            base.mkdir(parents=True)
            for kind in ['module', 'link', 'fifo']:
                path = base / ('go.mod' if kind == 'module' else 'opaque')
                if kind == 'module': path.touch()
                elif kind == 'link': path.symlink_to(base / 'absent')
                else: os.mkfifo(path)
                try:
                    for action, args in [('capture', ['--build-context', json.dumps(CONTEXT)]), ('validate', ['--receipt', str(receipt), '--receipt-sha256', pin])]:
                        with self.subTest(fork=fork, kind=kind, action=action):
                            result = cli(action, *args)
                            self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                            self.assertIn('error', json.loads(result.stdout))
                finally: path.unlink()

    def test_actual_cli_regular_file_swap_must_reject(self):
        if not hasattr(inventory, 'main'):
            self.skipTest('old helper has no CLI; public capture/validate oracle still runs')
        helper = self.root / 'scripts/r5_source_inventory.py'
        baseline = self.capture()
        receipt = self.root.parent / 'receipt.json'
        receipt.write_bytes(inventory.canonical(baseline))
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        # Separate Python process loads unchanged snapshot helper, injects only
        # a synthetic filesystem schedule, and calls its real argparse main.
        wrapper = '''import importlib.util, os, pathlib, sys
spec = importlib.util.spec_from_file_location("review_target", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
target = pathlib.Path(sys.argv[2])
original = os.stat
calls = 0
def injected(path, *args, **kwargs):
    global calls
    result = original(path, *args, **kwargs)
    if path == "switch" and kwargs.get("dir_fd") is not None and kwargs.get("follow_symlinks") is False:
        calls += 1
        if calls == 2:
            target.unlink()
            target.symlink_to(target.parents[3] / "cmd/main.go")
    return result
os.stat = injected
status = module.main(sys.argv[3:])
print("CLI_RACE_OBSERVATION", calls, target.is_symlink(), file=sys.stderr)
raise SystemExit(status)
'''
        for _, _, fork, _ in FORKS:
            base = self.root / fork / '.cache'
            base.mkdir()
            target = base / 'switch'
            for action, args in [('capture', ['--build-context', json.dumps(CONTEXT)]), ('validate', ['--receipt', str(receipt), '--receipt-sha256', pin])]:
                target.touch()
                try:
                    with self.subTest(fork=fork, action=action):
                        result = subprocess.run([sys.executable, '-B', '-c', wrapper, str(helper), str(target), action, '--root', str(self.root), '--purpose', 'protocol', '--policy', POLICY, *args], capture_output=True, text=True, timeout=10)
                        print(json.dumps({'fork': fork, 'action': action, 'cli_exit': result.returncode, 'same_receipt': json.loads(result.stdout) == baseline, 'observation': result.stderr.strip()}), flush=True)
                        self.assertEqual(result.returncode, 1, 'real main admitted regular-to-link race')
                finally: target.unlink()


if __name__ == '__main__': unittest.main()
