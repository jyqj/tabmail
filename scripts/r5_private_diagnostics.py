"""Opt-in bounded private diagnostics; never binding or qualification authority.

Set R5_DIAGNOSTIC_ROOT to an existing owned mode700 directory outside Git.
Each operation creates a unique private child; raw output never goes to stdout.
"""
import hashlib
import json
import os
from pathlib import Path
import stat
import uuid

MAX_FILE_BYTES = 64 * 1024 * 1024
MAX_TOTAL_BYTES = 512 * 1024 * 1024
MAX_FILES = 64


class Retention:
    def __init__(self, kind):
        if not kind.isascii() or not kind.replace('-', '').isalnum():
            raise ValueError('diagnostic operation name invalid')
        self.fd = None
        self.total = 0
        self.count = 0
        root = os.environ.get('R5_DIAGNOSTIC_ROOT')
        if not root:
            return
        path = Path(root).absolute()
        if '..' in path.parts:
            raise ValueError('diagnostic path escape')
        anchor = os.open('/', os.O_RDONLY | os.O_DIRECTORY)
        try:
            for component in path.parts[1:]:
                child = os.open(component, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=anchor)
                os.close(anchor)
                anchor = child
                try:
                    marker = os.stat('.git', dir_fd=anchor, follow_symlinks=False)
                except FileNotFoundError:
                    pass
                else:
                    if not stat.S_ISDIR(marker.st_mode):
                        raise ValueError('diagnostic root must be outside Git')
                    # Sandboxes can expose an empty protected .git mount even
                    # outside repositories. A real repository has HEAD.
                    marker_fd = os.open('.git', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=anchor)
                    try:
                        try:
                            os.stat('HEAD', dir_fd=marker_fd, follow_symlinks=False)
                        except FileNotFoundError:
                            pass
                        else:
                            raise ValueError('diagnostic root must be outside Git')
                    finally:
                        os.close(marker_fd)
            info = os.fstat(anchor)
            if info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
                raise ValueError('diagnostic root must be owned mode700 directory')
            name = kind + '-' + uuid.uuid4().hex
            os.mkdir(name, mode=0o700, dir_fd=anchor)
            self.path = path / name
            self.fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=anchor)
        finally:
            os.close(anchor)

    def write(self, name, data):
        if self.fd is None:
            return
        if not name or Path(name).name != name or name in ('.', '..'):
            raise ValueError('diagnostic path escape')
        if not isinstance(data, bytes):
            raise TypeError('diagnostic bytes required')
        if len(data) > MAX_FILE_BYTES or self.total + len(data) > MAX_TOTAL_BYTES or self.count >= MAX_FILES:
            raise ValueError('diagnostic retention bound exceeded')
        fd = os.open(name, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600, dir_fd=self.fd)
        try:
            output = os.fdopen(fd, 'wb')
            fd = None  # fdopen owns it; its context may close/reuse the number.
            with output:
                output.write(data)
        except BaseException:
            if fd is not None:  # fdopen failed before transferring ownership.
                os.close(fd)
            os.unlink(name, dir_fd=self.fd)
            raise
        self.total += len(data)
        self.count += 1

    def json(self, name, value):
        self.write(name, json.dumps(value, sort_keys=True).encode())

    def command(self, index, stdout, stderr, returncode):
        stdout = stdout.encode() if isinstance(stdout, str) else stdout or b''
        stderr = stderr.encode() if isinstance(stderr, str) else stderr or b''
        self.write(f'{index}.stdout', stdout)
        self.write(f'{index}.stderr', stderr)
        # Only internally computed facts; no caller-overridable receipt fields.
        self.json(f'{index}.summary.json', dict(diagnostic_only=True, returncode=returncode,
                  stdout_bytes=len(stdout), stderr_bytes=len(stderr),
                  stdout_sha256=hashlib.sha256(stdout).hexdigest(),
                  stderr_sha256=hashlib.sha256(stderr).hexdigest()))

    def failed_command(self, index, stdout, stderr, returncode, producer_error):
        """A secondary diagnostic failure must never replace producer evidence."""
        try:
            self.command(index, stdout, stderr, returncode)
        except Exception as retention_error:
            producer_error.retention_errors = (*getattr(producer_error, 'retention_errors', ()), retention_error)
            # Surface safe secondary facts even if disk/limits prevent writing.
            producer_error.add_note('diagnostic retention failed: ' + type(retention_error).__name__ +
                                    '; errno=' + str(getattr(retention_error, 'errno', None)))

    def close(self):
        if self.fd is not None:
            os.close(self.fd)
            self.fd = None

    def __enter__(self):
        return self

    def __exit__(self, *args):
        self.close()
