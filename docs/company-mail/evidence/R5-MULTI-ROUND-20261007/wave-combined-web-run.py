from __future__ import annotations

import datetime as dt
import hashlib
import json
import os
from pathlib import Path
import subprocess
import time

root = Path('/workspace/scratch/458de7991ac3/tabmail-wave-compose')
web = root / 'web'
output = root.parent / 'validation'
prefix = 'wave-combined-web'
summary_path = output / f'{prefix}-summary.json'
results_path = output / f'{prefix}-vitest.results.json'
env = os.environ.copy()
env['PATH'] = '/workspace/scratch/458de7991ac3/toolchains/go/bin:' + env['PATH']
env['GOTOOLCHAIN'] = 'local'
env['GOCACHE'] = '/workspace/scratch/458de7991ac3/go-cache/build'
env['GOMODCACHE'] = '/workspace/scratch/458de7991ac3/go-cache/mod'


def text_command(command: list[str], cwd: Path = root) -> str:
    return subprocess.check_output(command, cwd=cwd, env=env, text=True, stderr=subprocess.DEVNULL).strip()


def now() -> str:
    return dt.datetime.now(dt.timezone.utc).isoformat()


def source() -> dict:
    names = [
        'web/package.json', 'web/package-lock.json', 'web/vitest.config.ts',
        'web/next.config.ts', 'web/lib/api/base.ts',
        'web/app/(dashboard)/company/recovery/page.tsx',
        'web/lib/refresh-scope.test.ts',
        'web/features/company/recovery-reinspection.consumer.test.tsx',
        'web/features/company/recovery-queue-state.consumer.test.tsx',
        'web/r5-external-batch-probe.test.tsx',
        'web/components/company/r5-protocol.test.tsx',
    ]
    return {
        'commit': text_command(['git', 'rev-parse', 'HEAD']),
        'tree': text_command(['git', 'rev-parse', 'HEAD^{tree}']),
        'web_tree': text_command(['git', 'rev-parse', 'HEAD:web']),
        'status_porcelain': text_command(['git', 'status', '--porcelain']),
        'sha256': {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in names},
    }


summary = {
    'started_at': now(),
    'worktree': str(root),
    'before': source(),
    'environment': {
        'node': text_command(['node', '--version']),
        'npm': text_command(['npm', '--version']),
        'go': text_command(['go', 'version']),
        'GOTOOLCHAIN': env['GOTOOLCHAIN'],
        'GOCACHE': env['GOCACHE'],
        'GOMODCACHE': env['GOMODCACHE'],
        'protocol_component_fixture_configured': bool(env.get('TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE')),
        'protocol_observations_configured': bool(env.get('TABMAIL_R5_PROTOCOL_OBSERVATIONS')),
        'dependencies': {
            'path': str(web / 'node_modules'),
            'mode': 'Independent copy of existing lock-installed packages; original read only',
            'source': '/workspace/scratch/458de7991ac3/tabmail-deps/web/node_modules',
            'next': json.loads((web / 'node_modules/next/package.json').read_text())['version'],
            'react': json.loads((web / 'node_modules/react/package.json').read_text())['version'],
            'vitest': json.loads((web / 'node_modules/vitest/package.json').read_text())['version'],
        },
    },
    'selection': {
        'mode': 'Default Vitest selection; no added filters, exclusions, or skips',
        'max_workers': 2,
        'existing_config_exclusion': 'components/company/r5-protocol-shared-components.test.tsx',
        'fixture_policy': 'Preserve provided environment; no generated substitute fixture',
    },
    'steps': [],
}


def save() -> None:
    summary_path.write_text(json.dumps(summary, ensure_ascii=False, indent=2) + '\n')


commands = [
    ('vitest', ['npm', 'test', '--', '--maxWorkers=2', '--reporter=default', '--reporter=json', f'--outputFile={results_path}']),
    ('lint', ['npm', 'run', 'lint']),
    ('build', ['npm', 'run', 'build']),
]
save()
for name, command in commands:
    log_path = output / f'{prefix}-{name}.log'
    step = {'name': name, 'command': command, 'cwd': str(web), 'log': str(log_path), 'started_at': now()}
    summary['steps'].append(step)
    save()
    print(json.dumps({'event': 'start', 'step': name, 'command': command}), flush=True)
    started = time.monotonic()
    with log_path.open('w') as log:
        result = subprocess.run(command, cwd=web, env=env, stdout=log, stderr=subprocess.STDOUT)
    step.update(exit_code=result.returncode, duration_seconds=round(time.monotonic() - started, 3), finished_at=now())
    if name == 'vitest' and results_path.exists():
        raw = json.loads(results_path.read_text())
        files = raw.get('testResults', [])
        step['results'] = {
            'raw_json': str(results_path),
            'total_tests': raw.get('numTotalTests'),
            'passed_tests': raw.get('numPassedTests'),
            'failed_tests': raw.get('numFailedTests'),
            'pending_tests': raw.get('numPendingTests'),
            'todo_tests': raw.get('numTodoTests'),
            'total_files': len(files),
            'passed_files': sum(item.get('status') == 'passed' for item in files),
            'failed_files': sum(item.get('status') == 'failed' for item in files),
            'failures': [
                {'file': item['name'], 'title': assertion.get('fullName'), 'messages': assertion.get('failureMessages', [])}
                for item in files for assertion in item.get('assertionResults', []) if assertion.get('status') == 'failed'
            ],
            'go_receipt_component': [
                {'file': item['name'], 'status': item['status'], 'tests': len(item.get('assertionResults', []))}
                for item in files if item['name'].endswith('/components/company/r5-protocol.test.tsx')
            ],
        }
    save()
    print(json.dumps({'event': 'complete', 'step': name, 'exit_code': result.returncode, 'duration_seconds': step['duration_seconds']}), flush=True)

summary['after'] = source()
summary['source_unchanged'] = summary['before'] == summary['after']
summary['finished_at'] = now()
save()
print(json.dumps({'event': 'finished', 'summary': str(summary_path), 'source_unchanged': summary['source_unchanged']}), flush=True)
