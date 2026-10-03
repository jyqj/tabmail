"""Synthetic descriptor metadata checks; no services or permission changes."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import sys
import unittest
from unittest import mock

ORACLE = Path(__file__).parent / 'checkpoints/r5_excluded_descriptor_20261003/oracle.py'
spec = importlib.util.spec_from_file_location('descriptor_oracle', ORACLE)
oracle = importlib.util.module_from_spec(spec)
spec.loader.exec_module(oracle)
inventory = oracle.inventory

# A separate interpreter calls unchanged helper main with only a synthetic
# scheduling/availability hook. Its stdout is the real CLI receipt/error.
WRAPPER = '''import importlib.util, json, os, pathlib, sys
spec = importlib.util.spec_from_file_location("target_inventory", sys.argv[1])
module = importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
target, fault, phase = pathlib.Path(sys.argv[2]), sys.argv[3], int(sys.argv[4])
original = os.stat
calls = 0
def injected(path, *args, **kwargs):
    global calls
    result = original(path, *args, **kwargs)
    if path == "switch" and kwargs.get("dir_fd") is not None and kwargs.get("follow_symlinks") is False:
        calls += 1
        if fault == "unreadable": raise PermissionError("synthetic metadata denial")
        if fault == "unknown":
            from types import SimpleNamespace
            return SimpleNamespace(st_mode=0)
        if calls == phase:
            target.unlink()
            if fault == "directory": target.mkdir()
            elif fault == "fifo": os.mkfifo(target)
            elif fault == "regular": target.touch()
            elif fault == "symlink": target.symlink_to(target.parent / "absent")
    return result
os.stat = injected
if fault == "unsupported": sys.platform = "synthetic-unsupported"
if fault == "missing_primitive": del os.O_PATH
status = module.main(sys.argv[5:])
print("METADATA_OBSERVATION", fault, phase, calls, file=sys.stderr)
raise SystemExit(status)
'''


class DescriptorMetadataTests(oracle.IndependentExcludedOracle):
    # Inherit the original nine tests with their exact rejection expectations.
    def test_bounded_replacements_api_and_cli(self):
        baseline = self.capture()
        receipt = self.root.parent / 'receipt.json'
        receipt.write_bytes(inventory.canonical(baseline))
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        for _, _, fork, _ in oracle.FORKS:
            base = self.root / fork / 'build'
            base.mkdir()
            target = base / 'switch'
            for fault in ['symlink', 'directory', 'fifo', 'regular', 'unknown', 'unreadable', 'unsupported', 'missing_primitive']:
                for phase in ([1, 2] if fault in ['symlink', 'directory', 'fifo', 'regular'] else [1]):
                    for action in ['capture', 'validate']:
                        target.touch()
                        try:
                            # The same wrapper runs the real API or real CLI.
                            args = [action, '--root', str(self.root), '--purpose', 'protocol', '--policy', oracle.POLICY]
                            args += ['--build-context', json.dumps(oracle.CONTEXT)] if action == 'capture' else ['--receipt', str(receipt), '--receipt-sha256', pin]
                            for entry in ['cli', 'api']:
                                wrapper = WRAPPER
                                if entry == 'api':
                                    wrapper = wrapper.replace('status = module.main(sys.argv[5:])', '''try:
    if sys.argv[5] == "capture":
        module.capture_current_source(target.parents[3], purpose="protocol", policy=module.CURRENT_POLICY, build_context=json.loads(sys.argv[-1]))
    else:
        module.validate_current_source(json.loads(pathlib.Path(sys.argv[-3]).read_text()), target.parents[3], purpose="protocol", policy=module.CURRENT_POLICY)
    status = 0
except (ValueError, OSError) as error:
    print(json.dumps({"error": str(error)}))
    status = 1''')
                                if target.is_dir(): target.rmdir()
                                else: target.unlink()
                                target.touch()
                                result = subprocess.run([sys.executable, '-B', '-c', wrapper, str(oracle.SOURCE), str(target), fault, str(phase), *args], capture_output=True, text=True, timeout=10)
                                with self.subTest(fork=fork, fault=fault, phase=phase, action=action, entry=entry):
                                    self.assertEqual(result.returncode, 1, result.stdout + result.stderr)
                                    self.assertIn('error', json.loads(result.stdout))
                                    if fault in ['symlink', 'directory', 'fifo', 'regular']:
                                        self.assertIn(f'{fault} {phase} {phase}', result.stderr)
                        finally:
                            if target.is_dir(): target.rmdir()
                            else: target.unlink()
            base.rmdir()

    def test_cli_legal_artifacts_and_static_budget_rejections(self):
        baseline = self.capture()
        receipt = self.root.parent / 'receipt.json'
        receipt.write_bytes(inventory.canonical(baseline))
        pin = hashlib.sha256(receipt.read_bytes()).hexdigest()
        def both(expected):
            for action in ['capture', 'validate']:
                args = [action, '--root', str(self.root), '--purpose', 'protocol', '--policy', oracle.POLICY]
                args += ['--build-context', json.dumps(oracle.CONTEXT)] if action == 'capture' else ['--receipt', str(receipt), '--receipt-sha256', pin]
                result = subprocess.run([sys.executable, '-B', str(oracle.SOURCE), *args], capture_output=True, text=True, timeout=10)
                self.assertEqual(result.returncode, expected, result.stdout + result.stderr)
                if expected == 0: self.assertEqual(json.loads(result.stdout), baseline)
                else: self.assertIn('budget', json.loads(result.stdout)['error'])
        for _, _, fork, _ in oracle.FORKS:
            base = self.root / fork / 'dist'
            base.mkdir(); (base / 'synthetic-opaque').touch()
        both(0)
        for _, _, fork, _ in oracle.FORKS:
            base = self.root / fork / 'dist'
            for index in range(4095): (base / str(index)).touch()
            both(1)  # excluded root + artifact + 4095 entries
            for index in range(4095): (base / str(index)).unlink()
            current = base
            for _ in range(32):
                current /= 'level'; current.mkdir()
            both(1)
            while current != base:
                parent = current.parent; current.rmdir(); current = parent
        both(0)

    def test_only_metadata_handles_for_artifacts_and_no_body_reads(self):
        baseline = self.capture()
        for _, _, fork, _ in oracle.FORKS:
            base = self.root / fork / 'dist'
            base.mkdir(); (base / 'switch').touch()
        original = os.open
        artifact_fds = set()
        original_close = os.close
        def guarded_open(path, flags, *args, **kwargs):
            if path == 'switch':
                self.assertEqual(flags, os.O_PATH | os.O_NOFOLLOW)
            fd = original(path, flags, *args, **kwargs)
            if path == 'switch': artifact_fds.add(fd)
            return fd
        def guarded_close(fd):
            artifact_fds.discard(fd)
            return original_close(fd)
        original_read, original_fdopen = os.read, os.fdopen
        def read(fd, *args):
            self.assertNotIn(fd, artifact_fds)
            return original_read(fd, *args)
        def fdopen(fd, *args, **kwargs):
            self.assertNotIn(fd, artifact_fds)
            return original_fdopen(fd, *args, **kwargs)
        with mock.patch.object(os, 'open', guarded_open), mock.patch.object(os, 'close', guarded_close), mock.patch.object(os, 'read', read), mock.patch.object(os, 'fdopen', fdopen):
            self.assertEqual(self.capture(), baseline)
            self.assertEqual(self.validate(baseline), baseline)
            self.assertFalse(artifact_fds)


if __name__ == '__main__': unittest.main()
