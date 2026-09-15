#!/usr/bin/env python3
"""Capture npm's current audit inventory without silently changing the lockfile.

This reports findings, not an assertion that all dependencies are safe. A-E
release regressions and the dependency inventory are separate evidence.
"""
from pathlib import Path
import json
import subprocess
import sys

root = Path(__file__).resolve().parents[1]
out = Path(sys.argv[1] if len(sys.argv) > 1 else '/tmp/tabmail-dependency-audit')
out.mkdir(parents=True, exist_ok=True)
for name, extra in [('all', []), ('production', ['--omit=dev'])]:
    result = subprocess.run(['npm', 'audit', '--json', *extra], cwd=root/'web',
                            capture_output=True, text=True, timeout=120, check=False)
    (out/f'npm-audit-{name}.json').write_text(result.stdout)
    (out/f'npm-audit-{name}.stderr').write_text(result.stderr)
    try:
        report = json.loads(result.stdout)
    except ValueError:
        print(f'{name}: audit unavailable (exit {result.returncode})')
        continue
    summary = report.get('metadata', {}).get('vulnerabilities')
    if summary is None:
        print(f'{name}: audit unavailable: {report.get("error", {})}')
        continue
    print(f'{name}: {json.dumps(summary, sort_keys=True)}')
    for package, finding in report.get('vulnerabilities', {}).items():
        if finding.get('severity') in ('high', 'critical'):
            print(f'  {package}: {finding["severity"]}; direct={finding.get("isDirect")}; fix={finding.get("fixAvailable")}')
