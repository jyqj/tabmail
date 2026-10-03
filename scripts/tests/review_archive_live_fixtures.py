"""Real selected/build coverage for self-authored production variants in own clone."""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
from unittest import mock

p = argparse.ArgumentParser()
p.add_argument('--source', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
p.add_argument('--go', type=Path, required=True)
p.add_argument('--modules', type=Path, required=True)
a = p.parse_args()
a.output.mkdir(parents=True, exist_ok=True)
records = []
actual = subprocess.run
def run(argv, *args, **kwargs):
    result = actual(argv, *args, **kwargs)
    row = dict(argv=[str(v) for v in argv], exit=result.returncode)
    for field in ('stdout', 'stderr'):
        raw = getattr(result, field) or b''
        if isinstance(raw, str): raw = raw.encode()
        file = f'{len(records):03d}.{field}.gz'
        (a.output/file).write_bytes(gzip.compress(raw, mtime=0))
        row[field] = dict(file=file, sha256=hashlib.sha256(raw).hexdigest())
    records.append(row)
    (a.output/'commands.json').write_text(json.dumps(records, indent=2)+'\n')
    return result

summary = {}
with tempfile.TemporaryDirectory(prefix='r5-live-own-', dir=a.output.parent) as temp:
    root = Path(temp)/'clone'
    subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', str(a.source), str(root)], check=True)
    summary['source_sha'] = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    assert not subprocess.check_output(['git', '-C', str(root), 'status', '--porcelain'], text=True)
    sys.path[:0] = [str(root/'scripts')]
    import r5_selected_source_binding_v2 as binding
    sources = {
        'internal/r5reviewstandalone/standalone.go': 'package r5reviewstandalone\nconst Visible = 1\n',
        'internal/r5reviewtagonly/tagged.go': '//go:build r5fixtures\n\npackage r5reviewtagonly\nconst Tagged = 1\n',
        'internal/r5reviewwindowsonly/own_windows.go': 'package r5reviewwindowsonly\nconst Windows = 1\n',
        'internal/r5reviewtestonly/own_test.go': 'package r5reviewtestonly\nimport "testing"\nfunc TestOwn(t *testing.T) {}\n',
    }
    for name, text in sources.items():
        path = root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(text)
    cache = a.output.parent/'live-fixture-cache'
    with mock.patch.object(binding.subprocess, 'run', side_effect=run):
        receipt = binding.capture(root, a.go, cache=cache, modulecache=a.modules, context=binding.DEFAULT_CONTEXT)
    summary['all_own_files_in_static'] = set(sources).issubset(receipt['base_source']['archive_boundary']['production_go'])
    ownrecords = [r for r in receipt['production_coverage']['variant_directory_records'] if r['directory'].startswith('internal/r5review')]
    summary['own_variant_records'] = ownrecords
    summary['own_selected_files'] = sorted(set(sources)&set(receipt['selected_local']))
    assert summary['all_own_files_in_static'] and len(ownrecords) == 4
    assert 'internal/r5reviewstandalone/standalone.go' in receipt['selected_local']
    assert 'internal/r5reviewtestonly/own_test.go' in receipt['selected_local']
    assert 'internal/r5reviewtagonly/tagged.go' not in receipt['selected_local']
    assert 'internal/r5reviewwindowsonly/own_windows.go' not in receipt['selected_local']
    env = dict(receipt['environment'])
    env.update({k:os.environ[k] for k in ('HTTPS_PROXY', 'HTTP_PROXY', 'ALL_PROXY', 'NO_PROXY') if k in os.environ})
    good = run([str(a.go), 'build', '-mod=readonly', './...'], cwd=root, env=env, capture_output=True, timeout=240)
    summary['standalone_build_exit'] = good.returncode
    assert good.returncode == 0, good.stderr
    (root/'internal/r5reviewstandalone/standalone.go').write_text('package r5reviewstandalone\nvar Broken = undefinedIndependentReviewSentinel\n')
    bad = run([str(a.go), 'build', '-mod=readonly', './...'], cwd=root, env=env, capture_output=True, timeout=240)
    summary['undefined_build_exit'] = bad.returncode
    assert bad.returncode != 0 and b'undefinedIndependentReviewSentinel' in bad.stderr
    for name in sources:
        path = root/name
        path.unlink()
        path.parent.rmdir()
    marker = root/binding.boundary.registry(root)['modules'][0]['path']
    marker.unlink()
    exposed = run([str(a.go), 'build', '-mod=readonly', './...'], cwd=root, env=env, capture_output=True, timeout=240)
    summary['marker_deleted_build_exit'] = exposed.returncode
    assert exposed.returncode != 0 and b'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/' in exposed.stderr
    summary['status'] = 'pass'
(a.output/'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2))
