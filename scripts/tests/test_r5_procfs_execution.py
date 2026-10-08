"""The held executable must be addressed in procfs's visible PID namespace."""
from __future__ import annotations

import errno
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_source_runner_prepare as preparation


class ProcfsExecutableTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix="r5-procfs-execution-")
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / "internal/api/handlers").mkdir(parents=True)
        self.binary = self.root / "ordinary-receipt.test"
        self.binary.write_bytes(b"the held original inode")
        self.digest = preparation.digest(self.binary.read_bytes())
        self.fixture = self.root / "fresh-wire.json"
        self.source = "a" * 40
        self.source_patch = mock.patch.object(preparation, "source_identity", return_value=self.source)
        self.source_patch.start()
        self.addCleanup(self.source_patch.stop)

    def execute(self):
        return preparation.execute_binary(
            self.root, "go", self.binary, self.digest, self.source, self.fixture, {})

    def test_procfs_visible_identity_survives_namespace_pid_mismatch(self):
        visible_pid = os.readlink("/proc/self")

        def run(argv, **kwargs):
            self.assertTrue(argv[6].startswith(f"/proc/{visible_pid}/fd/"))
            self.assertNotEqual(argv[6], str(self.binary))
            self.assertEqual(Path(argv[6]).read_bytes(), self.binary.read_bytes())
            self.assertEqual(kwargs["cwd"], self.root / "internal/api/handlers")
            return subprocess.CompletedProcess(argv, 0, b"", b"")

        with mock.patch.object(preparation.os, "getpid", return_value=2147483647), \
                mock.patch.object(preparation.subprocess, "run", side_effect=run) as child:
            result, receipt = self.execute()
        child.assert_called_once()
        self.assertEqual(result.returncode, 0)
        self.assertEqual(receipt["execution_binding"], "linux_parent_proc_fd_pinned_inode")

    def test_malformed_procfs_process_identity_is_rejected(self):
        for target in ("", "0", "01", "../1", "/proc/1", "self", "1/fd/2", "١"):
            with self.subTest(target=target), \
                    mock.patch.object(preparation.os, "readlink", return_value=target), \
                    mock.patch.object(preparation.subprocess, "run") as child:
                with self.assertRaisesRegex(ValueError, "procfs process identity"):
                    self.execute()
                child.assert_not_called()

    def test_unavailable_procfs_keeps_the_original_cause(self):
        cause = OSError(errno.ENOENT, "procfs unavailable")
        with mock.patch.object(preparation.os, "readlink", side_effect=cause), \
                mock.patch.object(preparation.subprocess, "run") as child:
            with self.assertRaisesRegex(ValueError, "pinned executable FD unavailable") as error:
                self.execute()
        self.assertIs(error.exception.__cause__, cause)
        child.assert_not_called()

    def test_resolved_descriptor_must_match_the_held_inode(self):
        replacement = self.root / "different-inode"
        replacement.write_bytes(self.binary.read_bytes())
        different_identity = replacement.stat()
        original_stat = os.stat

        def substitute(path, *args, **kwargs):
            if str(path).startswith("/proc/") and "/fd/" in str(path):
                return different_identity
            return original_stat(path, *args, **kwargs)

        with mock.patch.object(preparation.os, "stat", side_effect=substitute), \
                mock.patch.object(preparation.subprocess, "run") as child:
            with self.assertRaisesRegex(ValueError, "pinned executable FD identity differs"):
                self.execute()
        child.assert_not_called()


if __name__ == "__main__":
    unittest.main()
