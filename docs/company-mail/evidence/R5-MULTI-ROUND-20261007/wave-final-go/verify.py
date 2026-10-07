#!/usr/bin/env python3
"""Run the authorized final Go combination once; never edit repository files."""
import collections
import datetime
import hashlib
import json
import os
from pathlib import Path
import shlex
import subprocess
import time

ROOT = Path('/workspace/scratch/458de7991ac3')
WORKTREE = ROOT / 'tabmail-wave-verify'
OUT = ROOT / 'validation/wave-final-go'
GO = ROOT / 'toolchains/go/bin/go'
EXPECTED = 'e32564304c0c84aa80b1184e72129fb0316d989d'
PACKAGES = ['./internal/config', './internal/outbound', './internal/app/companymail', './internal/mailcontent']
HANDLERS = './internal/api/handlers'
SELECTED = [
    'TestOrdinaryReceiptOpenAPIWireFixtures',
    'TestOutboundJobAccessCheckCoversGetRetryAndAttempts',
    'TestOutboundListJobsScopesRegularPrincipalsAndTenantContext',
    'TestSubmissionReceiptCapabilities',
    'TestAtomicRetryCommittedResponseStaysSuccessfulAndRestricted',
    'TestRetryJobConflictReasonMapping',
    'TestR5InboundAttachmentHTTPSourceFailureIsNotMissing',
    'TestRespondAppErrorContract',
]
FILTER = '^(' + '|'.join(SELECTED) + ')$'
OVERRIDES = {
    'GOTOOLCHAIN': 'local',
    'GOMODCACHE': str(ROOT / 'go-cache/mod'),
    'GOCACHE': str(ROOT / 'go-cache/build'),
    'GOPROXY': 'off',
}
ENV = os.environ.copy()
ENV.update(OVERRIDES)
OUT.mkdir(parents=True, exist_ok=True)


def now():
    return datetime.datetime.now(datetime.timezone.utc).isoformat()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def git(*args):
    return subprocess.check_output(['git', *args], cwd=WORKTREE).decode().strip()


def dump(path, value):
    path.write_text(json.dumps(value, ensure_ascii=False, indent=2) + '\n')


summary = {
    'schema_version': 1,
    'started_at': now(),
    'worktree': str(WORKTREE),
    'source_commit': git('rev-parse', 'HEAD'),
    'source_tree': git('rev-parse', 'HEAD^{tree}'),
    'initial_git_status': git('status', '--porcelain=v1', '--untracked-files=all'),
    'environment_overrides': OVERRIDES,
    'test_timeout_seconds_per_package': 180,
    'full_packages': PACKAGES,
    'handler_selected_tests': SELECTED,
    'commands': [],
    'scope_notes': [
        'Fresh execution on the fixed combined source; no earlier worker result is inherited.',
        'All four named packages run in full; only the eight explicitly listed handler tests run.',
        'Selected handler controls use real handlers/services and synthetic repository/object seams, not PostgreSQL.',
        'The ordinary wire fixture is typed projection/envelope evidence, not live router or PostgreSQL admission.',
        'Any test skip is counted separately; unselected PostgreSQL or external probes have no qualification here.',
        'No repository source, test, budget, validator, exclusion, or module file is changed by this runner.',
    ],
}
if summary['source_commit'] != EXPECTED or summary['initial_git_status']:
    raise SystemExit('Expected fixed clean combined worktree was not present')


def run(label, args, extra_env=None):
    command = [str(GO), *args]
    env = ENV.copy()
    if extra_env:
        env.update(extra_env)
    log = OUT / (label + ('.jsonl' if '-json' in args and args[0] == 'test' else '.log'))
    start = time.monotonic()
    record = {'label': label, 'argv': command, 'shell_command': shlex.join(command),
              'cwd': str(WORKTREE), 'started_at': now(), 'log': log.name}
    if extra_env:
        record['additional_environment'] = extra_env
    print('START ' + label, flush=True)
    with log.open('wb') as output:
        result = subprocess.run(command, cwd=WORKTREE, env=env, stdout=output, stderr=subprocess.STDOUT)
    record.update(exit_code=result.returncode, finished_at=now(), elapsed_seconds=round(time.monotonic()-start, 3),
                  log_sha256=digest(log.read_bytes()), log_bytes=log.stat().st_size)
    summary['commands'].append(record)
    dump(OUT / 'summary.json', summary)
    print('END ' + label + ' exit=' + str(result.returncode) + ' seconds=' + str(record['elapsed_seconds']), flush=True)
    return record, log


version, version_log = run('go-version', ['version'])
summary['go_version'] = version_log.read_text().strip()
if version['exit_code'] or 'go1.25.7 ' not in summary['go_version']:
    raise SystemExit('Pinned Go 1.25.7 is unavailable')

manifest_run, manifest_log = run('package-list', ['list', '-mod=readonly', '-json', *PACKAGES, HANDLERS])
if manifest_run['exit_code']:
    raise SystemExit('Package source manifest could not be read')
decoder = json.JSONDecoder()
remaining = manifest_log.read_text().lstrip()
manifest = []
while remaining:
    package, end = decoder.raw_decode(remaining)
    remaining = remaining[end:].lstrip()
    files = {}
    for kind in ['GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles', 'EmbedFiles', 'TestEmbedFiles', 'XTestEmbedFiles']:
        files[kind] = []
        for name in package.get(kind, []):
            path = Path(package['Dir']) / name
            files[kind].append({'path': str(path.relative_to(WORKTREE)), 'sha256': digest(path.read_bytes())})
    manifest.append({'import_path': package['ImportPath'], 'files': files})
module_paths = git('ls-files', 'go.mod', 'go.sum', '**/go.mod', '**/go.sum').splitlines()
module_hashes = {p: digest((WORKTREE/p).read_bytes()) for p in module_paths}
dump(OUT / 'source-manifest.json', {'source_commit': EXPECTED, 'source_tree': summary['source_tree'],
     'go_version': summary['go_version'], 'packages': manifest, 'tracked_module_files': module_hashes})
summary['source_manifest'] = 'source-manifest.json'
summary['source_manifest_sha256'] = digest((OUT/'source-manifest.json').read_bytes())

listing, listing_log = run('handler-test-list', ['test', '-mod=readonly', '-race', '-count=1', '-timeout=180s', '-list', FILTER, HANDLERS])
listed = [line for line in listing_log.read_text().splitlines() if line.startswith('Test')]
summary['handler_listed_tests'] = listed
summary['handler_preflight_matches_exact_selection'] = sorted(listed) == sorted(SELECTED)
if listing['exit_code'] or not summary['handler_preflight_matches_exact_selection']:
    dump(OUT/'summary.json', summary)
    raise SystemExit('Handler test listing does not exactly match the nonempty selection')

run('four-full-packages', ['test', '-mod=readonly', '-race', '-count=1', '-timeout=180s', '-json', *PACKAGES])
run('handler-controls', ['test', '-mod=readonly', '-race', '-count=1', '-timeout=180s', '-json', '-run', FILTER, HANDLERS],
    {'ORDINARY_RECEIPT_WIRE_FIXTURE': str(OUT/'ordinary-receipt-wire.json')})
run('build-all', ['build', '-mod=readonly', './...'])
run('vet-related', ['vet', '-mod=readonly', *PACKAGES, HANDLERS])


def counts(values):
    counter = collections.Counter(values)
    return {key: counter[key] for key in ['pass', 'fail', 'skip']}


test_results = []
for command in summary['commands']:
    if command['label'] not in ['four-full-packages', 'handler-controls']:
        continue
    packages = {}
    non_json = []
    for line in (OUT/command['log']).read_text(errors='replace').splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            non_json.append(line)
            continue
        name = event.get('Package')
        if not name:
            continue
        package = packages.setdefault(name, {'terminal': {}, 'ran': [], 'status': 'unknown'})
        test = event.get('Test')
        action = event.get('Action')
        if test and action == 'run':
            package['ran'].append(test)
        if test and action in ['pass', 'fail', 'skip']:
            package['terminal'][test] = action
        if not test and action in ['pass', 'fail', 'skip']:
            package['status'] = action
            package['package_elapsed_seconds'] = event.get('Elapsed')
    command['non_json_output_lines'] = len(non_json)
    for name, package in sorted(packages.items()):
        terminal = package['terminal']
        names = set(package['ran']) | set(terminal)
        leaves = {n for n in names if not any(other.startswith(n + '/') for other in names)}
        top = {n for n in names if '/' not in n}
        result = {'package': name, 'command_label': command['label'], 'package_status': package['status'],
                  'package_elapsed_seconds': package.get('package_elapsed_seconds'),
                  'top_level': counts(terminal.get(n) for n in top),
                  'leaf': counts(terminal.get(n) for n in leaves),
                  'all_test_terminal_events': counts(terminal.values()),
                  'run_events': len(package['ran']),
                  'top_level_test_names': sorted(top),
                  'failed_tests': sorted(n for n,s in terminal.items() if s == 'fail'),
                  'skipped_tests': sorted(n for n,s in terminal.items() if s == 'skip'),
                  'tests_without_terminal_event': sorted(n for n in names if n not in terminal)}
        test_results.append(result)
summary['test_results'] = test_results
summary['handler_execution_matches_exact_selection'] = any(
    r['package'] == 'tabmail/internal/api/handlers' and r['top_level_test_names'] == sorted(SELECTED)
    for r in test_results)
summary['source_hashes_unchanged_after'] = all(
    digest((WORKTREE/f['path']).read_bytes()) == f['sha256']
    for p in manifest for group in p['files'].values() for f in group)
summary['module_hashes_unchanged_after'] = all(digest((WORKTREE/p).read_bytes()) == h for p,h in module_hashes.items())
summary['final_git_status'] = git('status', '--porcelain=v1', '--untracked-files=all')
summary['final_source_commit'] = git('rev-parse', 'HEAD')
summary['finished_at'] = now()
summary['all_requested_commands_passed'] = all(c['exit_code'] == 0 for c in summary['commands'])
summary['all_executed_tests_passed_without_skip'] = len(test_results) == 5 and all(
    r['package_status'] == 'pass' and r['top_level']['pass'] > 0 and not r['failed_tests']
    and not r['skipped_tests'] and not r['tests_without_terminal_event'] for r in test_results)
summary['qualification_passed'] = all([
    summary['all_requested_commands_passed'], summary['all_executed_tests_passed_without_skip'],
    summary['handler_execution_matches_exact_selection'], summary['source_hashes_unchanged_after'],
    summary['module_hashes_unchanged_after'], not summary['final_git_status'],
    summary['final_source_commit'] == EXPECTED,
])
summary['artifacts'] = [{'path': p.name, 'sha256': digest(p.read_bytes()), 'bytes': p.stat().st_size}
                        for p in sorted(OUT.iterdir()) if p.is_file() and p.name not in ['summary.json', 'summary.md']]
dump(OUT/'summary.json', summary)

lines = ['# Final combined Go verification — 2026-10-07', '',
         'Result: **' + ('PASS' if summary['qualification_passed'] else 'FAIL / incomplete; inspect JSON and raw logs') + '**.', '',
         f"Source commit: `{EXPECTED}`; tree: `{summary['source_tree']}`.", '',
         f"Toolchain: `{summary['go_version']}`. All tests used `-mod=readonly -race -count=1 -timeout=180s`.", '',
         '| Package / scope | Top-level P/F/S | Leaf P/F/S | All terminal test events P/F/S | Package seconds |',
         '| --- | ---: | ---: | ---: | ---: |']
for result in test_results:
    def cell(key):
        return '/'.join(str(result[key][s]) for s in ['pass', 'fail', 'skip'])
    label = result['package'] + (' (8 selected controls)' if result['package'].endswith('/handlers') else ' (complete)')
    lines.append(f"| `{label}` | {cell('top_level')} | {cell('leaf')} | {cell('all_test_terminal_events')} | {result['package_elapsed_seconds']} |")
lines += ['', '## Commands', '']
for command in summary['commands']:
    lines += [f"- `{command['shell_command']}` — exit {command['exit_code']}; log `{command['log']}`; elapsed {command['elapsed_seconds']} s."]
lines += ['', 'The handler command additionally exported `ORDINARY_RECEIPT_WIRE_FIXTURE` to the new evidence file shown in `summary.json`.',
          '', '## Boundaries and source identity', '']
lines += ['- ' + note for note in summary['scope_notes']]
lines += ['', 'The complete selected Go source/test/embed file hashes and tracked module-file hashes are in `source-manifest.json`. '
          'The runner compared all of those bytes after execution and checked the repository remained on the same clean source commit. '
          'The machine summary records exact listed/executed handler names, commands, exit codes, timestamps, raw-log hashes, '
          'per-package pass/fail/skip counts, and any missing terminal events. Counts describe this combined tree only.', '']
(OUT/'summary.md').write_text('\n'.join(lines))
print(json.dumps({'qualification_passed': summary['qualification_passed'], 'summary': str(OUT/'summary.json'),
                  'packages': [{k:r[k] for k in ['package','top_level','leaf','all_test_terminal_events']} for r in test_results]}, indent=2), flush=True)
