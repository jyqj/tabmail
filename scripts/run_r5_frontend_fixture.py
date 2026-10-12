#!/usr/bin/env python3
"""Run the complete default frontend suite under a real private PG/HTTP owner.

This is default-frontend evidence, not qualification of the external batch,
its eight independent owners, or the protocol catalog. Original test selection,
assertions and dependency-audit policy are unchanged.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import stat
import subprocess
import sys
import threading
import time

ROOT = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(ROOT / 'scripts'))
import check_go_test_evidence as evidence

TEST = 'TestR5FrontendOwnedFixture'
PROBE = 'R5 batch independent realm loopback PostgreSQL'
SUITE = ROOT / 'scripts/required_frontend_fixture_tests.json'
# The original full default suite took 206 seconds in the baseline CI and had
# only the frontend job's 15-minute ceiling. These new bounds stay within it;
# they do not change the separate external batch's case75/Go120/process180.
FRONTEND_SECONDS = 300
GO_TEST_SECONDS = 360
OWNER_SECONDS = 390
FRONTEND_COMMAND = ['npm', 'test', '--', '--reporter=default', '--reporter=json',
                    '--outputFile=../frontend-vitest.json']


def validate_vitest(report, exit_code):
    """Require all default results and the original private-fixture probe."""
    errors = []
    if not isinstance(report, dict):
        return ['frontend report is not an object']
    counters = ('numTotalTests', 'numPassedTests', 'numFailedTests',
                'numPendingTests', 'numTodoTests')
    if any(type(report.get(key)) is not int for key in counters):
        return ['frontend report lacks complete integer counters']
    if (exit_code != 0 or report.get('success') is not True
            or report['numTotalTests'] <= 0
            or report['numTotalTests'] != report['numPassedTests']
            or any(report[key] != 0 for key in ('numFailedTests', 'numPendingTests', 'numTodoTests'))
            or report.get('numRuntimeErrorTestSuites', 0) != 0):
        errors.append('complete default frontend did not pass without missing, skipped or todo tests')
    results = report.get('testResults')
    if not isinstance(results, list) or not results:
        return errors + ['frontend file results missing']
    count, probes = 0, 0
    for result in results:
        if (not isinstance(result, dict) or result.get('status') != 'passed'
                or not isinstance(result.get('name'), str) or not result['name']):
            errors.append('frontend file failed or malformed')
            continue
        assertions = result.get('assertionResults')
        if not isinstance(assertions, list) or not assertions:
            errors.append('frontend assertion results missing')
            continue
        for assertion in assertions:
            count += 1
            if not isinstance(assertion, dict) or assertion.get('status') != 'passed':
                errors.append('frontend assertion did not pass')
                continue
            if (Path(result.get('name', '')).name == 'r5-external-batch-probe.test.tsx'
                    and assertion.get('fullName') == PROBE):
                probes += 1
    if count != report['numTotalTests']:
        errors.append('frontend counters disagree with actual assertion results')
    if probes != 1:
        errors.append('original private-fixture probe must pass exactly once')
    return errors


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


def source_state():
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=ROOT)
    difference = git('diff', '--binary', 'HEAD', '--')
    return {'head': git('rev-parse', 'HEAD').decode().strip(),
            'tree': git('rev-parse', 'HEAD^{tree}').decode().strip(),
            'tracked_clean': not difference,
            'tracked_diff_sha256': hashlib.sha256(difference).hexdigest()}


def run_owned(argv, *, cwd, env, seconds):
    # These unchanged process primitives run only inside this dedicated CLI.
    # Their subreaper also catches detached descendants after a killed owner.
    sys.path.insert(0, str(ROOT / 'scripts/preparation'))
    import r5_external_batch as processes
    deadline = time.monotonic() + seconds
    processes.become_subreaper()
    cancelled = threading.Event()
    old = {}
    for signum in (signal.SIGINT, signal.SIGTERM):
        old[signum] = signal.signal(signum, lambda *_: cancelled.set())
    result = None
    try:
        owned = processes.OwnedProcess(argv, cwd=cwd, env=env)
        result = owned.finish(cancelled, deadline)
    finally:
        # Physical completion remains mandatory even if finish itself raises.
        try:
            tails = processes.join_adopted_tails()
        finally:
            for signum, previous in old.items():
                signal.signal(signum, previous)
    result['tail'] = result['tail'] or tails
    result['processes_joined'] = True
    # finish can complete between polls or cancellation can arrive during the
    # final physical join. Neither may turn a completed child into admission.
    result['timeout'] = result['timeout'] or cancelled.is_set() or time.monotonic() >= deadline
    return result


def save_execution(output, prefix, result):
    (output / (prefix + '.stdout')).write_bytes(result.pop('stdout'))
    (output / (prefix + '.stderr')).write_bytes(result.pop('stderr'))
    (output / (prefix + '.exit')).write_text(str(result['exit_code']) + '\n')
    write_json(output / (prefix + '-execution.json'), result)


def vitest_child(output, command):
    private = os.environ.get('TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE', '')
    path = Path(private)
    if not private or not path.is_absolute() or path.is_relative_to(ROOT):
        raise ValueError('explicit private fixture outside source required')
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
        raise ValueError('owned private regular fixture required')
    report_path = ROOT / 'frontend-vitest.json'
    copied_report = output / 'frontend-vitest.json'
    if report_path.exists() or copied_report.exists():
        raise ValueError('fresh full frontend report required')
    # Same complete default npm command and reporters as the original workflow.
    # There is no file filter, exclusion, alternate config or mock fixture.
    result = run_owned(command, cwd=ROOT / 'web', env=dict(os.environ), seconds=FRONTEND_SECONDS)
    save_execution(output, 'vitest', result)
    raw_report = report_path.read_bytes()
    copied_report.write_bytes(raw_report)
    errors = validate_vitest(json.loads(raw_report), result['exit_code'])
    if result['timeout'] or result['tail']:
        errors.append('frontend process deadline or descendant cleanup failure')
    write_json(output / 'frontend-result.json', {
        'status': 'pass' if not errors else 'fail', 'errors': errors,
        'scope': 'complete_default_frontend_with_one_owned_fixture',
        'external_batch_qualified': False, 'task_complete': False,
    })
    return int(bool(errors))


def run(output, source_sha, command=FRONTEND_COMMAND):
    if command != FRONTEND_COMMAND:
        raise ValueError('only the original complete frontend npm command is admitted')
    if not os.environ.get('TABMAIL_TEST_DB_DSN'):
        raise ValueError('explicit disposable TABMAIL_TEST_DB_DSN required')
    if os.environ.get('TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE'):
        raise ValueError('caller-provided fixture cannot replace the real Go owner')
    before = source_state()
    write_json(output / 'source-before.json', before)
    if source_sha != before['head'] or not before['tracked_clean']:
        raise ValueError('recorded frontend source differs from checkout')
    (output / 'source-sha.txt').write_text(source_sha + '\n')
    write_json(output / 'command.json', command)
    required, allowed = evidence.load_suite(SUITE, 'frontend-owned-fixture')
    env = dict(os.environ, TABMAIL_R5_FRONTEND_OUTPUT=str(output),
               TABMAIL_R5_FRONTEND_PYTHON=str(Path(sys.executable).resolve()))
    command = ['go', 'test', '-mod=readonly', '-json', '-race', '-count=1',
               '-timeout=' + str(GO_TEST_SECONDS) + 's', '-tags=r5frontendfixture', '-run', '^' + TEST + '$',
               './internal/testpg']
    result = run_owned(command, cwd=ROOT, env=env, seconds=OWNER_SECONDS)
    raw = result['stdout']
    save_execution(output, 'go', result)
    report = evidence.evaluate(raw.decode().splitlines(), required, allowed, result['exit_code'])
    report.update(source_sha=source_sha, log_sha256=evidence.digest(output / 'go.stdout'),
                  scope='complete_default_frontend_with_one_owned_fixture',
                  external_batch_qualified=False, task_complete=False)
    after = source_state()
    write_json(output / 'source-after.json', after)
    report['source_unchanged'] = before == after
    if before != after:
        report['errors'].append('frontend source changed during execution')
    if result['timeout'] or result['tail']:
        report['errors'].append('Go owner deadline or descendant cleanup failure')
    try:
        child = json.loads((output / 'frontend-result.json').read_text())
        if child.get('status') != 'pass':
            report['errors'].append('complete frontend child did not qualify')
        full = json.loads((output / 'frontend-vitest.json').read_text())
        report['errors'].extend(validate_vitest(full, int((output / 'vitest.exit').read_text())))
    except (OSError, ValueError, TypeError):
        report['errors'].append('fresh complete frontend report is missing or malformed')
    report['status'] = 'pass' if not report['errors'] else 'fail'
    write_json(output / 'go-test-evidence.json', report)
    print(json.dumps(report, indent=2))
    return int(report['status'] != 'pass')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output-dir', type=Path, required=True)
    parser.add_argument('--source-sha')
    parser.add_argument('--vitest-child', action='store_true')
    parser.add_argument('command', nargs=argparse.REMAINDER)
    args = parser.parse_args()
    command = args.command[1:] if args.command[:1] == ['--'] else args.command
    if command != FRONTEND_COMMAND:
        raise ValueError('only the original complete frontend npm command is admitted')
    output = args.output_dir
    if not output.is_absolute() or output.resolve() != output or output.is_relative_to(ROOT):
        raise ValueError('canonical private output outside source required')
    if args.vitest_child:
        info = output.lstat()
        if not stat.S_ISDIR(info.st_mode) or info.st_uid != os.getuid() or info.st_mode & 0o077:
            raise ValueError('owned private output required')
        return vitest_child(output, command)
    output.mkdir(mode=0o700, exist_ok=False)
    return run(output, args.source_sha, command)


if __name__ == '__main__':
    try:
        raise SystemExit(main())
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        detail = str(error) if isinstance(error, ValueError) else type(error).__name__
        print('frontend fixture runner failed: ' + detail, file=sys.stderr)
        raise SystemExit(1)
