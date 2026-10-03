"""Record independent real Go capture/validation/build evidence in own sandbox.

This is a command, not an automatically discovered test. It loads helpers only
from --root, records every actual subprocess stdout/stderr (including hydration),
and never starts services or runs production Go tests.
"""
import argparse
import gzip
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import traceback
from unittest import mock

p = argparse.ArgumentParser()
p.add_argument('--root', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
p.add_argument('--go', type=Path, required=True)
p.add_argument('--modules', type=Path, required=True)
a = p.parse_args()
a.root = a.root.resolve()
a.output.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(a.root/'scripts'))
import r5_selected_source_binding_v2 as binding

actual = subprocess.run
commands = []
stage = 'initial'

def record(argv, *args, **kwargs):
    result = actual(argv, *args, **kwargs)
    index = len(commands)
    entry = dict(stage=stage, argv=[str(v) for v in argv], exit=result.returncode)
    for field in ('stdout', 'stderr'):
        raw = getattr(result, field) or b''
        if isinstance(raw, str):
            raw = raw.encode()
        name = f'{index:03d}.{field}.gz'
        (a.output/name).write_bytes(gzip.compress(raw, mtime=0))
        entry[field] = dict(file=name, bytes=len(raw), sha256=hashlib.sha256(raw).hexdigest())
    commands.append(entry)
    (a.output/'commands.json').write_text(json.dumps(commands, indent=2)+'\n')
    print(json.dumps(dict(stage=stage, command=index, exit=result.returncode)), flush=True)
    return result

summary = dict(source_sha=subprocess.check_output(['git', '-C', str(a.root), 'rev-parse', 'HEAD'], text=True).strip(),
               initial_status=subprocess.check_output(['git', '-C', str(a.root), 'status', '--porcelain'], text=True),
               module_cache_initially_empty=not a.modules.exists() or not any(a.modules.iterdir()),
               contexts={}, build={})
assert not summary['initial_status'], 'capture root must be a clean Git checkout'
a.modules.mkdir(parents=True, exist_ok=True)
for name, context in [('default', binding.DEFAULT_CONTEXT), ('race-r5protocol', binding.CONTEXT)]:
    cache = a.output.parent/(a.output.name+'-cache-'+name)
    assert not cache.exists() or not any(cache.iterdir()), 'cold cache must be empty'
    cache.mkdir(parents=True, exist_ok=True)
    row = dict(build_cache_initially_empty=True)
    summary['contexts'][name] = row
    try:
        with mock.patch.object(binding.subprocess, 'run', side_effect=record):
            stage = name+'-cold-capture'
            cold = binding.capture(a.root, a.go, cache=cache, modulecache=a.modules, context=context)
            (a.output/(name+'-cold-receipt.json.gz')).write_bytes(gzip.compress(binding.inventory.canonical(cold), mtime=0))
            stage = name+'-warm-validate'
            warm = binding.validate(cold, a.root, a.go, cache=cache, modulecache=a.modules)
            (a.output/(name+'-warm-receipt.json.gz')).write_bytes(gzip.compress(binding.inventory.canonical(warm), mtime=0))
        row.update(status='pass', cold_identity=cold['attestation_sha256'], warm_identity=warm['attestation_sha256'],
                   full_receipt_equal=cold == warm, selected_local=len(cold['selected_local']),
                   historical_files=len(cold['archive_static']), historical_selected_overlap=sorted(set(cold['archive_static'])&set(cold['selected_local'])),
                   root_explicit_equal=cold['production_coverage']['root_packages'] == cold['production_coverage']['explicit_packages'],
                   raw_observations=len(cold['commands']), hydration=cold.get('hydration_diagnostics'),
                   variant_directory_records=len(cold['production_coverage'].get('variant_directory_records', [])))
    except Exception:
        row.update(status='failed', error=traceback.format_exc())
        print(row['error'], flush=True)
    (a.output/'summary.json').write_text(json.dumps(summary, indent=2)+'\n')

env = dict(PATH=os.environ.get('PATH', '/usr/bin:/bin'), HOME=str(a.output.parent),
           GODEBUG='asynctimerchan=0', GOWORK='off', GOENV='off', GOTOOLCHAIN='local',
           GOFLAGS='', GOOS='linux', GOARCH='amd64', CGO_ENABLED='1',
           GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org',
           GOPATH=str(a.modules.parent), GOMODCACHE=str(a.modules),
           GOCACHE=str(a.output.parent/(a.output.name+'-build-cache')))
env.update({k:os.environ[k] for k in ('HTTPS_PROXY', 'HTTP_PROXY', 'ALL_PROXY', 'NO_PROXY') if k in os.environ})
for cmd in [('build', '-mod=readonly', './...'), ('vet', '-mod=readonly', './...')]:
    stage = 'real-'+cmd[0]
    result = record([str(a.go), *cmd], cwd=a.root, env=env, capture_output=True, timeout=240)
    summary['build'][cmd[0]] = dict(argv=list(cmd), exit=result.returncode)
summary['final_status'] = subprocess.check_output(['git', '-C', str(a.root), 'status', '--porcelain'], text=True)
(a.output/'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2))
