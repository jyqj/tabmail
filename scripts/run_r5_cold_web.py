#!/usr/bin/env python3
"""Private, bounded cold Next build; never a shipping-image/browser acceptance.

Preparation is the default and runs no Node process. --dirty-file explicitly
reviews every changed/untracked admissible web path (including deletions). The
runner copies HEAD via git archive, never the workspace .next/node_modules.
Execution requires the runtime owner's separately reserved window label:

  python3 -B scripts/run_r5_cold_web.py --output-dir /tmp/new-cold-web \
    --dirty-file web/path.tsx ...
  # Use a NEW output directory for execution; do not reuse prepared evidence.
  python3 -B scripts/run_r5_cold_web.py --output-dir /tmp/new-cold-web-run \
    --dirty-file web/path.tsx ... --execute --runtime-window OWNER-LABEL \
    --node-bin /absolute/trusted/node22/bin --expected-source-identity PREPARED_SHA256

The label records scheduling, not an OS lock or proof that another owner is idle.
Dependencies/install scripts run as the current user, with a private HOME/cache
and allowlisted environment; this is source isolation, not a container sandbox.
Admission also requires an outer controller exit-0/complete proof. SIGKILL and
unrecoverable OS/filesystem failure cannot be promised a Python terminal receipt.
"""
import argparse
from contextlib import contextmanager
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import signal
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
from datetime import datetime, timezone

MAX_FILE_BYTES = 32 * 1024 * 1024
MAX_SOURCE_BYTES = 128 * 1024 * 1024
MAX_LOG_BYTES = 16 * 1024 * 1024
DENIED_PARTS = {'.git', '.next', 'node_modules', '.npmrc', '.yarnrc', '.yarnrc.yml',
                '.ssh', '.aws', '.gnupg', '.netrc', '.npm', '.cache', 'credentials', 'secrets',
                'id_rsa', 'id_ed25519'}


class GateError(ValueError):
    pass


class DeadlineExpired(GateError):
    pass


class HardDeadlineExpired(BaseException):
    """Never caught as ValueError or converted into an unbounded failure phase."""
    pass


_ACTIVE_GUARD = None


def remaining_timeout(ceiling, deadline, stage, reserve=0):
    if deadline is None:
        return ceiling
    remaining = deadline - time.monotonic() - reserve
    if remaining <= 0:
        raise DeadlineExpired('total cold-build deadline exhausted during ' + stage)
    return min(ceiling, remaining)


def check_deadline(deadline, stage):
    remaining_timeout(float('inf'), deadline, stage)


class LifecycleGuard:
    def __init__(self, deadline):
        self.hard_deadline = deadline
        self.work_deadline = deadline
        self.phase = 'hard'
        self.hard_expired = False
        self.output = None
        self.admission_staging = None
        self.commit_deadline = None

    def check_hard(self):
        if self.hard_expired or time.monotonic() >= self.hard_deadline:
            self.hard_expired = True
            raise HardDeadlineExpired('absolute cold-build lifecycle hard deadline expired')

    def arm_hard(self):
        self.phase = 'hard'
        self.check_hard()
        signal.setitimer(signal.ITIMER_REAL, self.hard_deadline - time.monotonic())

    def arm_commit(self):
        # Reserve original-budget time to revoke a publication on Python failure.
        self.commit_deadline = self.hard_deadline - min(0.1, (self.hard_deadline - time.monotonic()) / 4)
        self.phase = 'commit'
        self.check_commit()
        signal.setitimer(signal.ITIMER_REAL, self.commit_deadline - time.monotonic())

    def check_commit(self):
        self.check_hard()
        if self.commit_deadline is not None and time.monotonic() >= self.commit_deadline:
            self.arm_hard()
            raise DeadlineExpired('atomic admission cutoff expired; bounded revoke only')

    def configure_work(self, started, total_timeout):
        self.hard_deadline = min(self.hard_deadline, started + total_timeout)
        self.work_deadline = self.hard_deadline - min(1, total_timeout / 4)
        self.check_hard()
        if time.monotonic() >= self.work_deadline:
            self.arm_hard()
            raise DeadlineExpired('work deadline exhausted; bounded terminal phase only')
        self.phase = 'work'
        signal.setitimer(signal.ITIMER_REAL, self.work_deadline - time.monotonic())

    def expired(self, signum, frame):
        if self.phase in ('work', 'commit'):
            expired_phase = self.phase
            self.arm_hard()  # Rearm BEFORE propagation; one-shot expiry cannot unbound I/O.
            raise DeadlineExpired('cold-build ' + expired_phase + ' deadline expired; bounded terminal phase only')
        self.hard_expired = True
        raise HardDeadlineExpired('absolute cold-build lifecycle hard deadline expired')


@contextmanager
def lifecycle_alarm(deadline):
    """Arm before parser/path work. Expired failure emission shares the hard end."""
    global _ACTIVE_GUARD
    previous_handler = signal.getsignal(signal.SIGALRM)
    previous_timer = signal.getitimer(signal.ITIMER_REAL)
    if previous_timer[0] or previous_timer[1] or _ACTIVE_GUARD is not None:
        raise GateError('existing process alarm refused; lifecycle deadline not armed')
    guard = LifecycleGuard(deadline)
    signal.signal(signal.SIGALRM, guard.expired)
    _ACTIVE_GUARD = guard
    try:
        guard.arm_hard()
        yield guard
    finally:
        signal.setitimer(signal.ITIMER_REAL, 0)
        signal.signal(signal.SIGALRM, previous_handler)
        _ACTIVE_GUARD = None


def prescan_total_timeout(argv):
    """Only raw argv, already under max-600 alarm; no parser or path resolution."""
    total = 600
    for index, value in enumerate(argv):
        text = None
        if value == '--total-timeout-seconds' and index + 1 < len(argv):
            text = argv[index + 1]
        elif value.startswith('--total-timeout-seconds='):
            text = value.split('=', 1)[1]
        if text is not None:
            try:
                number = int(text)
                if 1 <= number <= 600:
                    total = number
            except ValueError:
                pass  # The bounded authoritative parser reports invalid input.
    return total


def revoke_admission(guard):
    """Prewritten non-PASS backup: one atomic revoke, no read/hash/serialization."""
    if guard is not None and guard.output is not None:
        guard.check_hard()
        try:
            os.replace(str(guard.output / '.cold-build-not-admitted'),
                       str(guard.output / 'cold-build-receipt.json'))
        except OSError:
            pass


def utc_now():
    return datetime.now(timezone.utc).isoformat()


def sha256(data):
    return hashlib.sha256(data).hexdigest()


def admissible(path):
    """Only web regular source; never configs/caches bearing host secrets."""
    p = PurePosixPath(path)
    if not path or p.is_absolute() or '..' in p.parts or str(p) != path:
        raise GateError('non-canonical relative path: ' + path)
    if not p.parts or p.parts[0] != 'web':
        raise GateError('outside web write/snapshot scope: ' + path)
    for part in p.parts:
        lower = part.lower()
        if (lower in DENIED_PARTS or lower.startswith(('.env', 'credentials.', 'secrets.')) or
                lower.endswith(('.pem', '.key', '.p12', '.pfx', '.tsbuildinfo'))):
            return False
    return True


def git(repo, *args, deadline=None):
    result = subprocess.check_output(['git', '-C', str(repo), *args],
                                     timeout=remaining_timeout(30, deadline, 'source git'))
    check_deadline(deadline, 'source git completion')
    return result


def dirty_paths(repo, deadline=None):
    changed = git(repo, 'diff', '--name-only', '-z', 'HEAD', '--', 'web', deadline=deadline)
    untracked = git(repo, 'ls-files', '--others', '--exclude-standard', '-z', '--', 'web', deadline=deadline)
    return sorted(set(os.fsdecode(p) for p in (changed + untracked).split(b'\0') if p))


def write_json(path, value):
    guard = _ACTIVE_GUARD
    if guard is not None:
        guard.check_hard()
        if value.get('status') == 'cold_next_build_passed' and Path(path) != guard.admission_staging:
            value = {**value, 'status': 'cold_build_completed_pending_lifecycle_admission',
                     'cold_next_build_passed': False, 'admitted': False}
    data = (json.dumps(value, indent=2, sort_keys=True) + '\n').encode()
    fd = os.open(str(path), os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(data)
    if guard is not None:
        guard.check_hard()
        if Path(path).name == 'cold-build-receipt.json':
            write_json(Path(path).parent / '.cold-build-not-admitted', {
                'status': 'not_admitted', 'cold_next_build_passed': False,
                'admitted': False, 'shipping_image_passed': False,
                'browser_passed': False, 'G0_passed': False})


def private_output(output, repo):
    output = Path(output)
    if not output.is_absolute():
        raise GateError('output directory must be absolute')
    parent = output.parent.resolve(strict=True)
    allowed_roots = {Path(tempfile.gettempdir()).resolve(), Path('/tmp').resolve()}
    if not any(parent == root or root in parent.parents for root in allowed_roots):
        raise GateError('output directory must be under temporary storage')
    target = parent / output.name
    repo = repo.resolve()
    if target == repo or repo in target.parents:
        raise GateError('output directory must not be in the checkout')
    target.mkdir(mode=0o700)  # Existing evidence is never overwritten.
    return target


def regular_bytes(path, root):
    """Refuse symlink ancestors as well as links/devices at the leaf."""
    relative = path.relative_to(root)
    current = root
    for part in relative.parts:
        current = current / part
        if current.is_symlink():
            raise GateError('symlink source refused: ' + str(relative))
    flags = os.O_RDONLY | getattr(os, 'O_NOFOLLOW', 0)
    fd = os.open(str(path), flags)
    with os.fdopen(fd, 'rb') as stream:
        before = os.fstat(stream.fileno())
        if not stat.S_ISREG(before.st_mode) or before.st_size > MAX_FILE_BYTES:
            raise GateError('nonregular/oversized source: ' + str(relative))
        data = stream.read(MAX_FILE_BYTES + 1)
        after = os.fstat(stream.fileno())
    if (len(data) > MAX_FILE_BYTES or before.st_size != len(data) or
            (before.st_mtime_ns, before.st_size) != (after.st_mtime_ns, after.st_size)):
        raise GateError('source changed while reading: ' + str(relative))
    return data, 0o700 if before.st_mode & 0o111 else 0o600


def put_file(root, relative, data, mode):
    target = root / relative
    target.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
    fd = os.open(str(target), os.O_WRONLY | os.O_CREAT | os.O_TRUNC,
                 mode)
    with os.fdopen(fd, 'wb') as stream:
        stream.write(data)
    target.chmod(mode)


def prepare_snapshot(repo, output, reviewed_dirty, deadline=None):
    capture_started = time.monotonic()
    check_deadline(deadline, 'source capture start')
    repo = repo.resolve()
    head = git(repo, 'rev-parse', 'HEAD', deadline=deadline).decode().strip()
    current_dirty = dirty_paths(repo, deadline=deadline)
    allowed_dirty = {p for p in current_dirty if admissible(p)}
    if 'web/package-lock.json' in allowed_dirty:
        raise GateError('original tracked lockfile required; dirty lockfile refused')
    reviewed = set(reviewed_dirty)
    if len(reviewed) != len(reviewed_dirty):
        raise GateError('duplicate dirty-file review')
    for path in reviewed:
        if not admissible(path):
            raise GateError('forbidden dirty-file review: ' + path)
    if reviewed != allowed_dirty:
        raise GateError('explicit dirty review mismatch: ' + json.dumps({
            'missing': sorted(allowed_dirty - reviewed),
            'unexpected': sorted(reviewed - allowed_dirty)}))
    archive = git(repo, 'archive', '--format=tar', head, '--', 'web', deadline=deadline)
    if len(archive) > MAX_SOURCE_BYTES:
        raise GateError('tracked archive exceeds source byte budget')
    source = output / 'source'
    source.mkdir(mode=0o700)
    excluded, count, total = [], 0, 0
    # No extractall: refuse links/devices/path traversal before creating files.
    with tarfile.open(fileobj=io.BytesIO(archive), mode='r:') as tar:
        for member in tar:
            check_deadline(deadline, 'tracked source extraction')
            name = member.name.rstrip('/')
            if not admissible(name):
                excluded.append(name)
                continue
            if member.isdir():
                (source / name).mkdir(parents=True, exist_ok=True, mode=0o700)
                continue
            if not member.isfile() or member.size > MAX_FILE_BYTES:
                raise GateError('unsafe archive member: ' + name)
            data = tar.extractfile(member).read(MAX_FILE_BYTES + 1)
            total += len(data)
            if total > MAX_SOURCE_BYTES or len(data) != member.size:
                raise GateError('archive source byte budget/integrity failure')
            put_file(source, name, data, 0o700 if member.mode & 0o111 else 0o600)
            check_deadline(deadline, 'tracked source extraction completion')
            count += 1
    delta = []
    for path in sorted(reviewed):
        check_deadline(deadline, 'dirty source capture')
        origin, target = repo / path, source / path
        if origin.exists() or origin.is_symlink():
            data, mode = regular_bytes(origin, repo)
            total += len(data)
            if total > MAX_SOURCE_BYTES:
                raise GateError('dirty source exceeds byte budget')
            put_file(source, path, data, mode)
            delta.append({'path': path, 'action': 'overlay', 'sha256': sha256(data),
                          'bytes': len(data), 'mode': oct(mode)})
        else:
            if not target.is_file():
                raise GateError('deleted dirty source absent from tracked snapshot: ' + path)
            target.unlink()
            delta.append({'path': path, 'action': 'delete'})
    # Only the reviewed delta is reread, not a per-file full-tree hash sweep.
    for entry in delta:
        check_deadline(deadline, 'dirty source freshness')
        origin = repo / entry['path']
        if entry['action'] == 'delete':
            if origin.exists() or origin.is_symlink():
                raise GateError('deleted source reappeared: ' + entry['path'])
        else:
            data, mode = regular_bytes(origin, repo)
            if sha256(data) != entry['sha256'] or oct(mode) != entry['mode']:
                raise GateError('dirty source changed during capture: ' + entry['path'])
    if git(repo, 'rev-parse', 'HEAD', deadline=deadline).decode().strip() != head or dirty_paths(repo, deadline=deadline) != current_dirty:
        raise GateError('HEAD/dirty inventory changed during capture')
    web = source / 'web'
    for required in ('package.json', 'package-lock.json', 'Dockerfile'):
        if not (web / required).is_file():
            raise GateError('required tracked build input missing: web/' + required)
    package = json.loads((web / 'package.json').read_text())
    if package.get('scripts', {}).get('build') != 'next build':
        raise GateError('real next build script required; wrapper/bypass refused')
    if (web / 'node_modules').exists() or (web / '.next').exists():
        raise GateError('non-cold snapshot')
    identity = {'head': head, 'tracked_web_archive_sha256': sha256(archive), 'dirty_delta': delta,
                'excluded_tracked_paths': sorted(excluded),
                'excluded_dirty_paths': sorted(set(current_dirty) - allowed_dirty)}
    receipt = {**identity, 'source_identity_sha256': sha256(json.dumps(identity, sort_keys=True).encode()),
               'source_root': str(source), 'captured_at': utc_now(),
               'tracked_regular_file_count': count, 'capture_bytes_upper_bound': total,
               'package_json_sha256': sha256((web / 'package.json').read_bytes()),
               'lockfile_sha256': sha256((web / 'package-lock.json').read_bytes()),
               'dockerfile_sha256': sha256((web / 'Dockerfile').read_bytes()),
               'cold_before_install': True,
               'scope': 'tracked web HEAD plus exact explicitly reviewed working-tree delta'}
    check_deadline(deadline, 'source capture receipt')
    receipt['capture_elapsed_seconds'] = round(time.monotonic() - capture_started, 3)
    receipt['lifecycle_deadline_monotonic'] = deadline
    write_json(output / 'source-receipt.json', receipt)
    check_deadline(deadline, 'source capture completion')
    return receipt


def build_environment(output, node_bin):
    node_bin = Path(node_bin).resolve(strict=True)
    if not node_bin.is_dir() or not (node_bin / 'node').is_file() or not (node_bin / 'npm').is_file():
        raise GateError('node-bin must contain trusted node and npm executables')
    for name in ('home', 'npm-cache', 'tmp'):
        (output / name).mkdir(mode=0o700)
    userconfig = output / 'empty-npmrc'
    userconfig.touch(mode=0o600)
    globalconfig = output / 'empty-global-npmrc'
    globalconfig.touch(mode=0o600)
    return {'PATH': str(node_bin) + ':/usr/bin:/bin', 'HOME': str(output / 'home'),
            'TMPDIR': str(output / 'tmp'), 'CI': '1', 'LC_ALL': 'C', 'TZ': 'UTC',
            'NEXT_TELEMETRY_DISABLED': '1', 'INTERNAL_API_URL': 'http://tabmail:8080',
            'npm_config_cache': str(output / 'npm-cache'),
            'npm_config_userconfig': str(userconfig), 'npm_config_globalconfig': str(globalconfig),
            'npm_config_audit': 'false', 'npm_config_fund': 'false',
            'npm_config_registry': 'https://registry.npmjs.org/'}


def unknown_group(pgid, error, deadline=None):
    return {'observed_at': utc_now(), 'pgid': pgid, 'group_absent': False,
            'members': [], 'live_pids': [], 'ownership_confirmed': False,
            'unknown': True, 'error': str(error), 'deadline_monotonic': deadline}


def observe_group(pgid, deadline=None):
    """No independent observation budget: ps consumes only cleanup's remainder."""
    try:
        timeout = remaining_timeout(2, deadline, 'owned group observation')
        probe = subprocess.run(['/bin/ps', '-axo', 'pid=,pgid=,stat='],
                               stdout=subprocess.PIPE, stderr=subprocess.DEVNULL,
                               env={'PATH': '/usr/bin:/bin', 'LC_ALL': 'C'}, timeout=timeout, check=True)
        check_deadline(deadline, 'owned group observation completion')
        members = []
        for line in probe.stdout.decode('ascii', errors='replace').splitlines():
            check_deadline(deadline, 'owned group observation parse')
            fields = line.split()
            if len(fields) != 3 or int(fields[1]) != pgid:
                continue
            pid = int(fields[0])
            try:
                sid = os.getsid(pid)
            except ProcessLookupError:
                continue
            members.append({'pid': pid, 'pgid': pgid, 'sid': sid, 'state': fields[2]})
        try:
            os.killpg(pgid, 0)
            exists = True
        except ProcessLookupError:
            exists = False
        check_deadline(deadline, 'owned group final absence observation')
        return {'observed_at': utc_now(), 'pgid': pgid, 'group_absent': not exists,
                'members': members, 'live_pids': [m['pid'] for m in members if not m['state'].startswith('Z')],
                'ownership_confirmed': all(m['sid'] == pgid for m in members),
                'unknown': exists and not members, 'probe_timeout_seconds': timeout,
                'deadline_monotonic': deadline}
    except DeadlineExpired:
        raise  # Work expiry must reach the bounded terminal phase, never be swallowed.
    except (OSError, ValueError, subprocess.SubprocessError) as exc:
        return unknown_group(pgid, exc, deadline)


def emergency_owned_stop(process, signals):
    """Nonblocking stop on observation expiry; never claim that it succeeded.

    Only a still-live leader with its original PID/PGID/SID is safe to signal
    without ps. If the leader is gone or identity is unknown, leave cleanup red.
    """
    try:
        process.poll()
        if process.returncode is None and os.getsid(process.pid) == process.pid and os.getpgid(process.pid) == process.pid:
            os.killpg(process.pid, signal.SIGKILL)
            signals.append({'signal': int(signal.SIGKILL), 'at': utc_now(),
                            'pgid': process.pid, 'reason': 'unverified observation deadline stop'})
            process.poll()
    except DeadlineExpired:
        raise
    except (OSError, ValueError):
        pass


def cleanup_group(process, budget=5, deadline=None):
    """One absolute end covers every observation/sleep; no fresh final 2s probe."""
    pgid = process.pid
    end = time.monotonic() + max(0, budget)
    if deadline is not None:
        end = min(end, deadline)
    observations, signals = [], []
    sent_term, sent_kill = False, False
    final = unknown_group(pgid, 'cleanup not observed', end)
    while True:
        process.poll()
        if time.monotonic() >= end:
            final = unknown_group(pgid, 'cleanup deadline exhausted; absence unobserved', end)
            observations.append(final)
            emergency_owned_stop(process, signals)
            break
        observed = observe_group(pgid, deadline=end)
        observations.append(observed)
        final = observed
        if time.monotonic() >= end:
            final = unknown_group(pgid, 'cleanup observation exhausted deadline; absence unobserved', end)
            observations.append(final)
        if final['unknown'] or not final['ownership_confirmed']:
            if final['unknown']:
                emergency_owned_stop(process, signals)
            break
        if not final['live_pids']:
            break
        remaining = end - time.monotonic()
        sig = None
        if not sent_term:
            sig, sent_term = signal.SIGTERM, True
        elif not sent_kill and remaining <= min(2, budget / 2):
            sig, sent_kill = signal.SIGKILL, True
        if sig is not None:
            try:
                os.killpg(pgid, sig)
                signals.append({'signal': int(sig), 'at': utc_now(), 'pgid': pgid})
            except ProcessLookupError:
                pass
        if remaining > 0:
            time.sleep(min(0.05, remaining))
    process.poll()
    return {'pgid': pgid, 'sid': pgid, 'deadline_monotonic': end,
            'signals': signals, 'observations': observations, 'final': final,
            'verified_no_live_group': not final['unknown'] and final['ownership_confirmed'] and
            not final['live_pids'] and process.returncode is not None and time.monotonic() < end}


def run_command(output, stage, argv, cwd, env, timeout, cleanup_budget=5, deadline=None):
    """Bound lifetime/log bytes; persist exact command, owned group and red logs."""
    record = {'stage': stage, 'argv': argv, 'cwd': str(cwd), 'env_allowlist': dict(env),
              'timeout_seconds': timeout, 'started_at': utc_now(), 'ended_at': None,
              'exit_code': None, 'timed_out': False, 'log_limit_exceeded': False,
              'pid': None, 'pgid': None, 'sid': None, 'cleanup_verified': False,
              'lifecycle_deadline_monotonic': deadline}
    receipt_path = output / (stage + '.json')
    write_json(receipt_path, record)
    started = time.monotonic()
    paths = [output / (stage + suffix) for suffix in ('.stdout', '.stderr')]
    with paths[0].open('xb') as stdout, paths[1].open('xb') as stderr:
        for path in paths:
            path.chmod(0o600)
        process = None
        try:
            check_deadline(deadline, 'command spawn ' + stage)
            process = subprocess.Popen(argv, cwd=str(cwd), env=env,
                                       stdout=stdout, stderr=stderr, start_new_session=True)
            record.update(pid=process.pid, pgid=process.pid, sid=process.pid)
            write_json(receipt_path, record)  # Interrupted parent still leaves group identity.
            while process.poll() is None:
                if any(p.stat().st_size > MAX_LOG_BYTES for p in paths):
                    record['log_limit_exceeded'] = True
                    break
                remaining = timeout - (time.monotonic() - started)
                if deadline is not None:
                    remaining = min(remaining, deadline - time.monotonic() - cleanup_budget)
                if remaining <= 0:
                    record['timed_out'] = True
                    break
                time.sleep(min(0.1, remaining))
        except BaseException as exc:
            record['error'] = type(exc).__name__ + ': ' + str(exc)
            raise
        finally:
            cleanup_interruption = None
            if _ACTIVE_GUARD is not None and _ACTIVE_GUARD.hard_expired:
                if process is not None:
                    emergency_owned_stop(process, [])
                raise HardDeadlineExpired('hard expiry: command evidence finalization forbidden')
            if process is not None:
                try:
                    cleanup = cleanup_group(process, cleanup_budget, deadline=deadline)
                except HardDeadlineExpired:
                    emergency_owned_stop(process, [])
                    raise
                except BaseException as exc:
                    # The CLI alarm can arrive between ps calls/poll, not just in ps.
                    signals = []
                    emergency_owned_stop(process, signals)
                    cleanup = {'pgid': process.pid, 'sid': process.pid, 'signals': signals,
                               'observations': [], 'final': unknown_group(process.pid, exc, deadline),
                               'verified_no_live_group': False}
                    cleanup_interruption = exc
                    record['cleanup_error'] = type(exc).__name__ + ': ' + str(exc)
                record['group_cleanup'] = cleanup
                record['cleanup_verified'] = cleanup['verified_no_live_group']
                record['exit_code'] = process.returncode
            record['ended_at'] = utc_now()
            record['elapsed_seconds'] = round(time.monotonic() - started, 3)
            record['lifecycle_deadline_exceeded'] = deadline is not None and time.monotonic() >= deadline
            stdout.flush()
            stderr.flush()
            for label, path in zip(('stdout', 'stderr'), paths):
                if _ACTIVE_GUARD is not None:
                    _ACTIVE_GUARD.check_hard()
                record[label + '_bytes'] = path.stat().st_size
                record[label + '_sha256'] = sha256(path.read_bytes())
            record['log_limit_exceeded'] |= any(p.stat().st_size > MAX_LOG_BYTES for p in paths)
            write_json(receipt_path, record)
            if cleanup_interruption is not None:
                raise cleanup_interruption
    return record


def command_passed(record):
    return (record['exit_code'] == 0 and not record['timed_out'] and
            not record['log_limit_exceeded'] and not record.get('lifecycle_deadline_exceeded', False) and
            record.get('cleanup_verified', False) and
            'error' not in record)


def execute_build(output, source, node_bin, window, timeout, total_timeout=600,
                  deadline=None, lifecycle_started=None):
    started = time.monotonic() if lifecycle_started is None else lifecycle_started
    deadline = started + total_timeout if deadline is None else deadline
    check_deadline(deadline, 'environment setup')
    cleanup_reserve = min(5, total_timeout / 10)
    env = build_environment(output, node_bin)
    web = Path(source['source_root']) / 'web'
    result = {'status': 'running', 'runtime_window': window, 'commands': [],
              'total_timeout_seconds': total_timeout, 'cleanup_reserve_seconds': cleanup_reserve,
              'budget_exhausted': False, 'lifecycle_deadline_monotonic': deadline,
              'lifecycle_includes_capture': lifecycle_started is not None,
              'shipping_image_passed': False, 'browser_passed': False, 'G0_passed': False,
              'isolation': 'private native source/HOME/cache, not a container sandbox',
              'platform': os.uname().sysname + '/' + os.uname().machine,
              'source_identity_sha256': source['source_identity_sha256'],
              'started_at': utc_now(), 'ended_at': None}
    def stage_command(stage, argv, ceiling):
        try:
            stage_timeout = remaining_timeout(ceiling, deadline, stage, cleanup_reserve)
        except DeadlineExpired:
            result['budget_exhausted'] = True
            raise
        try:
            return run_command(output, stage, argv, web, env, stage_timeout, cleanup_reserve, deadline=deadline)
        finally:
            if _ACTIVE_GUARD is not None:
                _ACTIVE_GUARD.check_hard()
            persisted = output / (stage + '.json')
            if persisted.is_file():
                result['commands'].append(json.loads(persisted.read_text()))

    try:
        for stage, argv in (('node-version', ['node', '--version']),
                            ('npm-version', ['npm', '--version'])):
            command = stage_command(stage, argv, 15)
            if not command_passed(command):
                raise GateError('tool version command failed: ' + stage)
        node_version = (output / 'node-version.stdout').read_text().strip()
        result['node_version'] = node_version
        result['npm_version'] = (output / 'npm-version.stdout').read_text().strip()
        if not node_version.startswith('v22.'):
            raise GateError('Node 22 required by shipping Dockerfile/CI')
        for stage, argv in (('npm-ci', ['npm', 'ci']), ('npm-build', ['npm', 'run', 'build'])):
            if stage == 'npm-build' and (web / '.next').exists():
                raise GateError('install produced .next: refuse non-cold build')
            command = stage_command(stage, argv, timeout)
            if not command_passed(command):
                result['status'] = stage + '_failed'
                break
        else:
            artifacts = {name: (web / name).is_file() for name in
                         ('.next/BUILD_ID', '.next/standalone/server.js')}
            artifacts['.next/static'] = (web / '.next/static').is_dir()
            result['artifact_presence'] = artifacts
            result['status'] = 'cold_next_build_passed' if all(artifacts.values()) else 'build_artifacts_missing'
        result['lockfile_unchanged'] = sha256((web / 'package-lock.json').read_bytes()) == source['lockfile_sha256']
        result['package_json_unchanged'] = sha256((web / 'package.json').read_bytes()) == source['package_json_sha256']
        if not result['lockfile_unchanged'] or not result['package_json_unchanged']:
            result['status'] = 'dependency_input_mutated'
    except BaseException as exc:
        result['budget_exhausted'] |= isinstance(exc, DeadlineExpired)
        result['status'] = 'execution_error'
        result['error'] = type(exc).__name__ + ': ' + str(exc)
        raise
    finally:
        result['ended_at'] = utc_now()
        result['elapsed_seconds'] = round(time.monotonic() - started, 3)
        result['total_deadline_exceeded'] = time.monotonic() > deadline
        result['all_owned_groups_no_live'] = bool(result['commands']) and all(
            c.get('cleanup_verified', False) for c in result['commands'])
        if result['total_deadline_exceeded'] or not result['all_owned_groups_no_live']:
            if result['status'] == 'cold_next_build_passed':
                result['status'] = 'deadline_or_cleanup_failed'
        write_json(output / 'cold-build-receipt.json', result)
    return result


def atomic_admit(output, result, guard):
    """Canonical PASS is the final atomic publication, never a phase receipt."""
    guard.arm_commit()
    staged = output / '.cold-build-admission-stage'
    guard.admission_staging = staged
    try:
        admitted = {**result, 'admitted': True,
                    'cold_next_build_passed': result['status'] == 'cold_next_build_passed',
                    'lifecycle_completed_before_deadline': True,
                    'hard_deadline_monotonic': guard.hard_deadline,
                    'publication_started_monotonic': time.monotonic(),
                    'publication_cutoff_monotonic': guard.commit_deadline,
                    'requires_outer_exit_zero': True, 'requires_controller_complete_proof': True,
                    'shipping_image_passed': False, 'browser_passed': False, 'G0_passed': False}
        write_json(staged, admitted)
    finally:
        guard.admission_staging = None
    guard.check_commit()
    os.replace(str(staged), str(output / 'cold-build-receipt.json'))
    guard.check_commit()


def main(argv=None):
    started = time.monotonic()
    guard = None
    try:
        # The maximum guard precedes argv conversion, argparse and Path.resolve.
        with lifecycle_alarm(started + 600) as guard:
            try:
                raw_argv = sys.argv[1:] if argv is None else list(argv)
                guard.configure_work(started, prescan_total_timeout(raw_argv))
                parser = argparse.ArgumentParser(description=__doc__, formatter_class=argparse.RawDescriptionHelpFormatter, allow_abbrev=False)
                parser.add_argument('--repo', type=Path, default=Path(__file__).resolve().parents[1])
                parser.add_argument('--output-dir', type=Path, required=True)
                parser.add_argument('--dirty-file', action='append', default=[])
                parser.add_argument('--execute', action='store_true')
                parser.add_argument('--runtime-window')
                parser.add_argument('--node-bin', type=Path)
                parser.add_argument('--expected-source-identity')
                parser.add_argument('--timeout-seconds', type=int, default=300)
                parser.add_argument('--total-timeout-seconds', type=int, default=600)
                args = parser.parse_args(raw_argv)
                if args.execute and (not args.runtime_window or not args.node_bin or not args.expected_source_identity):
                    parser.error('--execute requires runtime-owner window, trusted --node-bin and --expected-source-identity')
                if not 1 <= args.total_timeout_seconds <= 600:
                    parser.error('total timeout must be 1..600 seconds, including terminal reserve')
                if not 1 <= args.timeout_seconds <= 600:
                    parser.error('per-stage timeout must be 1..600 seconds')
                return run_lifecycle(args, started, guard)
            except BaseException:
                if not guard.hard_expired:
                    guard.arm_hard()
                    revoke_admission(guard)
                raise
    except HardDeadlineExpired:
        return 1  # No more file read/hash/write/stdout after the hard deadline.
    except (GateError, OSError, subprocess.SubprocessError) as exc:
        # A parser/setup failure has no admission; avoid unguarded failure I/O.
        return 1


def run_lifecycle(args, started, guard):
    output = None
    try:
        deadline = guard.work_deadline
        check_deadline(deadline, 'private output creation')
        output = private_output(args.output_dir, args.repo)
        guard.output = output
        source = prepare_snapshot(args.repo, output, args.dirty_file, deadline=deadline)
        check_deadline(deadline, 'source admission')
        if args.execute:
            if source['source_identity_sha256'] != args.expected_source_identity:
                raise GateError('prepared source identity drift; build refused before npm')
            result = execute_build(output, source, args.node_bin, args.runtime_window,
                                   args.timeout_seconds, args.total_timeout_seconds,
                                   deadline=deadline, lifecycle_started=started)
        else:
            result = {'status': 'prepared_not_executed', 'cold_next_build_passed': False,
                      'source_identity_sha256': source['source_identity_sha256'],
                      'shipping_image_passed': False, 'browser_passed': False, 'G0_passed': False}
        guard.arm_hard()  # All final receipts share the remaining original hard end.
        write_json(output / 'cold-build-receipt.json', result)  # Always non-PASS pending.
        write_json(output / 'lifecycle-receipt.json', {
            'status': 'completed_pending_atomic_admission', 'admitted': False,
            'total_timeout_seconds': args.total_timeout_seconds,
            'elapsed_seconds': round(time.monotonic() - started, 6),
            'work_deadline_monotonic': guard.work_deadline,
            'hard_deadline_monotonic': guard.hard_deadline,
            'scope': 'CLI argv/parser/path/output/capture/install/build/cleanup/final receipts',
            'deadline_failure_never_proves_group_absence': True,
            'shipping_image_passed': False, 'browser_passed': False, 'G0_passed': False})
        guard.check_hard()
        # Stdout is before the commit and explicitly not an acceptance claim.
        print(json.dumps({'status': 'finalization_prepared_not_admitted', 'evidence': str(output)}), flush=True)
        if result['status'] not in ('cold_next_build_passed', 'prepared_not_executed'):
            return 1
        atomic_admit(output, result, guard)
        return 0  # No later receipt serialization or stdout can strand a PASS.
    except HardDeadlineExpired:
        raise
    except (GateError, OSError, subprocess.SubprocessError) as exc:
        revoke_admission(guard)
        guard.arm_hard()  # Remaining original budget, never a new timeout allowance.
        if output is not None:
            write_json(output / 'error.json', {'error': str(exc), 'at': utc_now(), 'passed': False})
            write_json(output / 'cold-build-receipt.json', {
                'status': 'lifecycle_failed', 'cold_next_build_passed': False, 'admitted': False,
                'error': str(exc), 'shipping_image_passed': False, 'browser_passed': False, 'G0_passed': False})
            write_json(output / 'lifecycle-receipt.json', {
                'status': 'failed', 'admitted': False, 'deadline_exceeded': isinstance(exc, DeadlineExpired),
                'elapsed_seconds': round(time.monotonic() - started, 6),
                'hard_deadline_monotonic': guard.hard_deadline,
                'deadline_failure_never_proves_group_absence': True})
        return 1
    except BaseException:
        guard.arm_hard()
        revoke_admission(guard)
        raise


if __name__ == '__main__':
    raise SystemExit(main())
