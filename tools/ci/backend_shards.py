#!/usr/bin/env python3
"""Bounded full Go race coverage, with original per-invocation evidence rules.

Discover all default tests from the actual compiled packages, then partition by
top-level name (not package) so a -run expression cannot duplicate a same-named
test in another package. Each raw process log is validated separately. Never
concatenate multiple process lifecycles into a purported single invocation.
"""
from concurrent.futures import ThreadPoolExecutor
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
from check_go_test_evidence import evaluate, load_suite, unique_object

TOP = re.compile(r'(?:Test|Example|Fuzz)[A-Za-z0-9_]*\Z')
SHARDS = 16
WORKERS = 4
PROCESS_SECONDS = 180
TOTAL_SECONDS = 720


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
    report = evaluate(lines, planned, allowed, exit_code)
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


def run(argv, prefix, deadline):
    started = time.monotonic()
    remaining = deadline - started
    if remaining <= 0:
        raise TimeoutError('backend total lifecycle budget exhausted')
    with prefix.with_suffix('.jsonl').open('wb') as stdout, prefix.with_suffix('.stderr').open('wb') as stderr:
        child = subprocess.Popen(argv, cwd=ROOT, stdout=stdout, stderr=stderr, start_new_session=True)
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
    args = parser.parse_args()
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip() != args.source_sha:
        raise ValueError('exact source SHA required')
    if subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT):
        raise ValueError('tracked source must be clean')
    output = args.output_dir.resolve()
    output.mkdir(parents=True, exist_ok=False)
    deadline = time.monotonic() + TOTAL_SECONDS
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
    (output / 'plan.json').write_text(json.dumps(dict(source_sha=args.source_sha, packages=sorted(packages),
        tests=sorted(planned), shards=[sorted(part) for part in parts], workers=WORKERS,
        process_seconds=PROCESS_SECONDS, total_seconds=TOTAL_SECONDS), indent=2) + '\n')

    def execute(item):
        index, part = item
        prefix = output / f'shard-{index:02d}'
        expression = '^(' + '|'.join(re.escape(name) for name in sorted({name for _, name in part})) + ')$'
        try:
            process = run(['go', 'test', '-json', '-race', '-count=1', '-timeout=180s', '-run', expression, './...'], prefix, deadline)
            raw = prefix.with_suffix('.jsonl').read_bytes()
            result = validate(raw.decode().splitlines(), part, allowed, process['exit_code'])
            if process['timeout']:
                result['status'] = 'fail'
            result.update(process=process, log_sha256=hashlib.sha256(raw).hexdigest(), source_sha=args.source_sha)
        except Exception as error:
            result = dict(status='fail', errors=[f'{type(error).__name__}: {error}'], source_sha=args.source_sha)
        prefix.with_suffix('.evidence.json').write_text(json.dumps(result, indent=2) + '\n')
        print(f'shard {index}: {result["status"]}', flush=True)
        return result

    with ThreadPoolExecutor(max_workers=WORKERS) as workers:
        reports = list(workers.map(execute, enumerate(parts)))
    seen = [tuple(identity) for report in reports for identity in report.get('actual_top_level_tests', [])]
    passed = all(report['status'] == 'pass' for report in reports)
    passed = passed and len(seen) == len(set(seen)) and set(seen) == planned
    source_unchanged = not subprocess.check_output(['git', 'diff', 'HEAD', '--'], cwd=ROOT)
    passed = passed and source_unchanged
    result = dict(status='pass' if passed else 'fail', mode='independent_bounded_shards', source_sha=args.source_sha,
        source_unchanged=source_unchanged, discovered_top_level_tests=len(planned), actual_top_level_tests=len(seen),
        mandatory_tests=len(required), shards=reports)
    (ROOT / 'go-test-evidence.json').write_text(json.dumps(result, indent=2) + '\n')
    return 0 if passed else 1


if __name__ == '__main__':
    raise SystemExit(main())
