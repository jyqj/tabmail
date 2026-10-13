#!/usr/bin/env python3
"""Run the original whole-repository Go race command exactly once, never shard.

Temporary UI dependencies serve the existing owned PG/UI probes. All Go events
are retained in one original-format log and validated by the unchanged checker.
The Go -timeout=180s budget is unchanged; the outer bound also includes compiling
the test binaries and dependency preparation, within the existing CI job budget.
"""
from contextlib import contextmanager
import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import signal
import subprocess
import sys
import tempfile
import time
from urllib.parse import urlparse

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
from check_go_test_evidence import evaluate, load_suite

GO_ARGV = ['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', './...']


def run(argv, prefix, *, cwd=ROOT, timeout):
    started = time.monotonic()
    with prefix.with_suffix('.stdout').open('wb') as out, prefix.with_suffix('.stderr').open('wb') as err:
        child = subprocess.Popen(argv, cwd=cwd, stdout=out, stderr=err, start_new_session=True)
        timed_out = False
        try:
            child.wait(timeout=max(0.01, timeout))
        except subprocess.TimeoutExpired:
            timed_out = True
            os.killpg(child.pid, signal.SIGTERM)
            try:
                child.wait(timeout=2)
            except subprocess.TimeoutExpired:
                os.killpg(child.pid, signal.SIGKILL)
                child.wait()
    result = dict(argv=argv, exit_code=child.returncode, timeout=timed_out,
                  elapsed_seconds=time.monotonic() - started)
    prefix.with_suffix('.process.json').write_text(json.dumps(result, indent=2) + '\n')
    return result


@contextmanager
def owned_ui_dependencies(output, deadline):
    link = ROOT / 'web/node_modules'
    if link.exists() or link.is_symlink():
        raise ValueError('owned UI preparation requires absent node_modules')
    with tempfile.TemporaryDirectory(prefix='tabmail-single-ui-', dir=os.environ.get('RUNNER_TEMP')) as temp:
        directory = Path(temp)
        for name in ('package.json', 'package-lock.json'):
            shutil.copyfile(ROOT / 'web' / name, directory / name)
        result = run(['npm', 'ci'], output / 'npm-ci', cwd=directory,
                     timeout=min(180, deadline - time.monotonic()))
        if result['exit_code'] or result['timeout']:
            raise ValueError('owned UI installation failed')
        target = directory / 'node_modules'
        link.symlink_to(target, target_is_directory=True)
        try:
            yield
        finally:
            if not link.is_symlink() or Path(os.readlink(link)) != target:
                raise ValueError('owned dependency link changed')
            link.unlink()


def source_identity():
    head = subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip()
    if subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT):
        raise ValueError('tracked source differs from HEAD')
    return head


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--output-dir', type=Path, required=True)
    args = parser.parse_args(argv)
    if source_identity() != args.source_sha:
        raise ValueError('exact source SHA required')
    target = urlparse(os.environ.get('TABMAIL_TEST_DB_DSN', ''))
    if target.hostname != '127.0.0.1' or target.port != 55447:
        raise ValueError('explicit owned loopback PostgreSQL cluster required')
    for flag in ('TABMAIL_PAIR_REVIEW', 'TABMAIL_SUCCESSOR_AUTHZ_OWNED', 'TABMAIL_LF01_OWNED_PROBE'):
        if os.environ.get(flag) != '1':
            raise ValueError('explicit owned PG/UI probe flag required: ' + flag)
    output = args.output_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    deadline = time.monotonic() + 660
    required, allowed = load_suite(ROOT / 'scripts/required_go_tests.json', 'backend')
    with owned_ui_dependencies(output, deadline):
        process = run(GO_ARGV, output / 'go-test', timeout=min(480, deadline - time.monotonic()))
    raw = (output / 'go-test.stdout').read_bytes()
    # No transformation, concatenation or selection: this is the one original
    # Go invocation's exact JSONL, including all failures and original skips.
    (output / 'go-test.jsonl').write_bytes(raw)
    report = evaluate(raw.decode().splitlines(), required, allowed, process['exit_code'])
    if process['timeout'] or source_identity() != args.source_sha:
        report['status'] = 'fail'
        report['errors'].append('outer process timeout or changed source')
    report.update(source_sha=args.source_sha, process=process, mode='single_original_go_invocation',
                  log_sha256=hashlib.sha256(raw).hexdigest())
    (output / 'go-test-evidence.json').write_text(json.dumps(report, indent=2) + '\n')
    print(json.dumps(report, indent=2), flush=True)
    return 0 if report['status'] == 'pass' else 1


if __name__ == '__main__':
    raise SystemExit(main())
