#!/usr/bin/env python3
"""Bounded full Go race coverage, with original per-invocation evidence rules.

Discover all default tests from the actual compiled packages, then partition by
top-level name (not package) so a -run expression cannot duplicate a same-named
test in another package. Each raw process log is validated separately. Never
concatenate multiple process lifecycles into a purported single invocation.
"""
from concurrent.futures import ThreadPoolExecutor
from contextlib import contextmanager
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import shutil
import subprocess
import sys
import tempfile
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
from check_go_test_evidence import evaluate, load_suite, unique_object

TOP = re.compile(r'(?:Test|Example|Fuzz)[A-Za-z0-9_]*\Z')
SHARDS = 16
WORKERS = 4
MATRIX_PARTITIONS = 4
PROCESS_SECONDS = 180
TOTAL_SECONDS = 660  # Leave 60 seconds before the 12-minute step timeout for cleanup.


@contextmanager
def owned_ui_dependencies(output, deadline):
    """Give real PG/UI probes dependencies without traversable Go npm sources.

    A source-local npm install contains flatted's unrelated Go package. Keep
    dependencies in a private external directory, expose only a temporary
    node_modules symlink, and remove exactly our link before later source gates.
    """
    link = ROOT / 'web/node_modules'
    if link.exists() or link.is_symlink():
        raise ValueError('backend UI preparation requires absent node_modules')
    with tempfile.TemporaryDirectory(prefix='tabmail-backend-ui-', dir=os.environ.get('RUNNER_TEMP')) as temp:
        directory = Path(temp)
        for name in ('package.json', 'package-lock.json'):
            shutil.copyfile(ROOT / 'web' / name, directory / name)
        installed = run(['npm', 'ci'], output / 'npm-ci', deadline, cwd=directory, suffix='.log')
        if installed['exit_code'] or installed['timeout']:
            raise ValueError('owned backend UI dependency installation failed')
        target = directory / 'node_modules'
        link.symlink_to(target, target_is_directory=True)
        try:
            yield
        finally:
            if not link.is_symlink() or Path(os.readlink(link)) != target:
                raise ValueError('owned node_modules link was replaced')
            link.unlink()


def discover(lines, expected_packages):
    states, tests = {}, set()
    for line in lines:
        event = json.loads(line, object_pairs_hook=unique_object)
        package, action = event.get('Package'), event.get('Action')
        if package not in expected_packages or event.get('FailedBuild'):
            raise ValueError('unknown package or failed discovery build')
        if action == 'start':
            if package in states:
                raise ValueError('duplicate discovery package')
            states[package] = 'running'
        elif action == 'output':
            if states.get(package) != 'running':
                raise ValueError('discovery output outside running package')
            name = event['Output'].strip()
            if TOP.fullmatch(name):
                identity = (package, name)
                if identity in tests:
                    raise ValueError('duplicate discovered test')
                tests.add(identity)
        elif action in ('pass', 'skip') and not event.get('Test'):
            if states.get(package) != 'running':
                raise ValueError('invalid discovery package terminal')
            if action == 'skip' and any(p == package for p, _ in tests):
                raise ValueError('nonempty discovery package skipped')
            states[package] = action
        else:
            raise ValueError('discovery failed or unexpectedly executed tests')
    if set(states) != expected_packages or 'running' in states.values() or not tests:
        raise ValueError('incomplete/empty package discovery')
    return tests


def partition(tests, count=SHARDS):
    names = sorted({name for _, name in tests})
    if not names or count < 1:
        raise ValueError('empty test partition')
    count = min(count, len(names))
    owners = {name: index % count for index, name in enumerate(names)}
    return [{identity for identity in tests if owners[identity[1]] == index} for index in range(count)]


def validate(lines, planned, allowed, exit_code):
    report = evaluate(lines, planned - set(allowed), allowed, exit_code)
    actual = []
    for line in lines:
        event = json.loads(line, object_pairs_hook=unique_object)
        name = event.get('Test', '')
        if event.get('Action') == 'run' and '/' not in name and name:
            actual.append((event['Package'], name))
    if len(actual) != len(set(actual)) or set(actual) != planned:
        report['status'] = 'fail'
        report.setdefault('errors', []).append('actual top-level execution differs from exact discovery partition')
    report['planned_top_level_tests'] = sorted(planned)
    report['actual_top_level_tests'] = sorted(actual)
    return report


def test_argv(part):
    expression = '^(' + '|'.join(re.escape(name) for name in sorted({name for _, name in part})) + ')$'
    # Empty packages were compiled during whole-repository discovery. Do not
    # run 53 empty race test processes again in every shard (each has exit cost).
    return ['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', '-run', expression,
            *sorted({package for package, _ in part})]


def matrix_indices(index, count=SHARDS):
    if not 0 <= index < MATRIX_PARTITIONS:
        raise ValueError('invalid matrix partition')
    return [i for i in range(count) if i % MATRIX_PARTITIONS == index]


def run(argv, prefix, deadline, *, cwd=ROOT, suffix='.jsonl'):
    started = time.monotonic()
    remaining = deadline - started
    if remaining <= 0:
        raise TimeoutError('backend total lifecycle budget exhausted')
    with prefix.with_suffix(suffix).open('wb') as stdout, prefix.with_suffix('.stderr').open('wb') as stderr:
        child = subprocess.Popen(argv, cwd=cwd, stdout=stdout, stderr=stderr, start_new_session=True)
        timed_out = False
        try:
            child.wait(timeout=min(PROCESS_SECONDS + 15, remaining))
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


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--source-sha', required=True)
    parser.add_argument('--output-dir', type=Path, default=Path('backend-shards'))
    parser.add_argument('--matrix-index', type=int, choices=range(MATRIX_PARTITIONS))
    parser.add_argument('--verify-matrix', type=Path)
    args = parser.parse_args()
    if args.verify_matrix is not None and args.matrix_index is not None:
        raise ValueError('execution and verification modes are mutually exclusive')
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() != args.source_sha:
        raise ValueError('exact source SHA required')
    if subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT):
        raise ValueError('tracked source must be clean')
    output = args.output_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    deadline = time.monotonic() + TOTAL_SECONDS
    if args.verify_matrix is not None:
        return execute_suite(args, output, deadline)
    for flag in ('TABMAIL_PAIR_REVIEW', 'TABMAIL_SUCCESSOR_AUTHZ_OWNED', 'TABMAIL_LF01_OWNED_PROBE'):
        if os.environ.get(flag) != '1':
            raise ValueError('explicit owned PG/UI probe flag required: ' + flag)
    with owned_ui_dependencies(output, deadline):
        return execute_suite(args, output, deadline)


def execute_suite(args, output, deadline):
    required, allowed = load_suite(ROOT / 'scripts/required_go_tests.json', 'backend')
    # Package enumeration is independent of the executable test listing.
    packages = set(subprocess.check_output(['go', 'list', './...'], cwd=ROOT, text=True, timeout=180).splitlines())
    process = run(['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', '-list', '.', './...'], output / 'discovery', deadline)
    if process['exit_code'] or process['timeout']:
        raise ValueError('compiled test discovery failed')
    planned = discover((output / 'discovery.jsonl').read_text().splitlines(), packages)
    if not required.issubset(planned):
        raise ValueError('mandatory tests missing from compiled discovery')
    parts = partition(planned)
    plan = dict(source_sha=args.source_sha, packages=sorted(packages),
        tests=[list(identity) for identity in sorted(planned)],
        shards=[[list(identity) for identity in sorted(part)] for part in parts],
        workers=1 if args.matrix_index is not None or args.verify_matrix is not None else WORKERS,
        process_seconds=PROCESS_SECONDS, total_seconds=TOTAL_SECONDS)
    (output / 'plan.json').write_text(json.dumps(plan, indent=2) + '\n')
    if args.verify_matrix is not None:
        return verify_matrix(args, output, plan, parts, allowed, required)
    indices = list(range(len(parts))) if args.matrix_index is None else matrix_indices(args.matrix_index, len(parts))

    def execute(item):
        index, part = item
        prefix = output / f'shard-{index:02d}'
        try:
            process = run(test_argv(part), prefix, deadline)
            raw = prefix.with_suffix('.jsonl').read_bytes()
            result = validate(raw.decode().splitlines(), part, allowed, process['exit_code'])
            if process['timeout']:
                result['status'] = 'fail'
            result.update(process=process, log_sha256=hashlib.sha256(raw).hexdigest(), source_sha=args.source_sha)
        except Exception as error:
            result = dict(status='fail', errors=[f'{type(error).__name__}: {error}'], source_sha=args.source_sha)
        prefix.with_suffix('.evidence.json').write_text(json.dumps(result, indent=2) + '\n')
        print(json.dumps(dict(shard=index, status=result['status'],
                              process=result.get('process'), errors=result.get('errors', []))), flush=True)
        return result

    with ThreadPoolExecutor(max_workers=WORKERS if args.matrix_index is None else 1) as workers:
        reports = list(workers.map(execute, [(i, parts[i]) for i in indices]))
    seen = [tuple(identity) for report in reports for identity in report.get('actual_top_level_tests', [])]
    passed = all(report['status'] == 'pass' for report in reports)
    assigned = set().union(*(parts[i] for i in indices))
    passed = passed and len(seen) == len(set(seen)) and set(seen) == assigned
    source_unchanged = not subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT)
    source_unchanged = source_unchanged and subprocess.check_output(
        ['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() == args.source_sha
    passed = passed and source_unchanged
    result = dict(status='pass' if passed else 'fail', mode='independent_bounded_shards', source_sha=args.source_sha,
        matrix_index=args.matrix_index, shard_indices=indices,
        source_unchanged=source_unchanged, discovered_top_level_tests=len(planned), actual_top_level_tests=len(seen),
        mandatory_total=len(required), mandatory_assigned=len(required & assigned), shards=reports)
    (ROOT / 'go-test-evidence.json').write_text(json.dumps(result, indent=2) + '\n')
    (output / 'summary.json').write_text(json.dumps(result, indent=2) + '\n')
    return 0 if passed else 1


def verify_matrix(args, output, plan, parts, allowed, required):
    """Revalidate all raw logs against a fresh independent compiled inventory."""
    folders = {f'company-backend-shards-{i}' for i in range(MATRIX_PARTITIONS)}
    if {p.name for p in args.verify_matrix.iterdir()} != folders:
        raise ValueError('missing or extra matrix artifacts')
    seen, reports = [], []
    for index in range(MATRIX_PARTITIONS):
        folder = args.verify_matrix / f'company-backend-shards-{index}'
        read = lambda name: json.loads((folder / name).read_text(), object_pairs_hook=unique_object)
        if read('plan.json') != plan:
            raise ValueError('matrix discovery/source differs from independent inventory')
        summary = read('summary.json')
        indices = matrix_indices(index, len(parts))
        if (summary['source_sha'] != args.source_sha or summary['matrix_index'] != index or
                summary['shard_indices'] != indices or summary['status'] != 'pass' or
                summary['source_unchanged'] is not True):
            raise ValueError('incomplete, wrong-source or failed matrix partition')
        if {p.name for p in folder.glob('shard-*.jsonl')} != {f'shard-{i:02d}.jsonl' for i in indices}:
            raise ValueError('missing or duplicate shard files')
        for shard in indices:
            process = read(f'shard-{shard:02d}.process.json')
            if process['argv'] != test_argv(parts[shard]) or process['timeout'] or process['exit_code'] != 0:
                raise ValueError('changed command, failed or timed-out shard')
            raw = (folder / f'shard-{shard:02d}.jsonl').read_bytes()
            recorded = read(f'shard-{shard:02d}.evidence.json')
            if recorded['source_sha'] != args.source_sha or recorded['log_sha256'] != hashlib.sha256(raw).hexdigest():
                raise ValueError('shard source/log identity mismatch')
            result = validate(raw.decode().splitlines(), parts[shard], allowed, process['exit_code'])
            if result['status'] != 'pass':
                raise ValueError('original event validator rejected shard: ' + str(result['errors']))
            seen.extend(tuple(identity) for identity in result['actual_top_level_tests'])
            reports.append(result)
    planned = set().union(*parts)
    if len(seen) != len(set(seen)) or set(seen) != planned or not required.issubset(set(seen)):
        raise ValueError('matrix failed exact unique whole-suite coverage')
    if (subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT) or
            subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() != args.source_sha):
        raise ValueError('source changed during matrix verification')
    result = dict(status='pass', source_sha=args.source_sha, mode='independently_verified_matrix',
                  top_level_tests=len(seen), mandatory_tests=len(required), shards=reports)
    (output / 'go-test-evidence.json').write_text(json.dumps(result, indent=2) + '\n')
    print(json.dumps(dict(status='pass', top_level_tests=len(seen), mandatory_tests=len(required))), flush=True)
    return 0


if __name__ == '__main__':
    raise SystemExit(main())
