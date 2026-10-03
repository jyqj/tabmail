"""Observe frozen selected-v1 cold roundtrip without replacing its algorithm."""
import argparse
import gzip
import hashlib
import json
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
a.output.mkdir(parents=True, exist_ok=True)
sys.path.insert(0, str(a.root/'scripts'))
import r5_selected_source_binding as binding
actual = subprocess.run
records = []
stage = 'cold-capture'
def run(argv, *args, **kwargs):
    value = actual(argv, *args, **kwargs)
    row = dict(stage=stage, argv=[str(v) for v in argv], exit=value.returncode)
    for field in ('stdout', 'stderr'):
        raw = getattr(value, field) or b''
        file = f'{len(records):03d}.{field}.gz'
        (a.output/file).write_bytes(gzip.compress(raw, mtime=0))
        row[field] = dict(file=file, sha256=hashlib.sha256(raw).hexdigest())
    records.append(row)
    (a.output/'commands.json').write_text(json.dumps(records, indent=2)+'\n')
    return value
cache = a.output.parent/'legacy-cold-cache'
assert not cache.exists() and not a.modules.exists()
summary = dict(source_sha=subprocess.check_output(['git', '-C', str(a.root), 'rev-parse', 'HEAD'], text=True).strip(),
               build_cache_initially_empty=True, module_cache_initially_empty=True)
with mock.patch.object(binding.subprocess, 'run', side_effect=run):
    try:
        receipt = binding.capture(a.root, a.go, cache=cache, modulecache=a.modules)
        summary['cold_capture'] = 'pass'
        (a.output/'cold-receipt.json.gz').write_bytes(gzip.compress(binding.inventory.canonical(receipt), mtime=0))
        stage = 'cold-receipt-warm-validate'
        try:
            binding.validate(receipt, a.root, a.go, cache=cache, modulecache=a.modules)
            summary['cold_receipt_warm_validate'] = 'pass'
        except Exception:
            summary['cold_receipt_warm_validate'] = 'failed'
            summary['cold_roundtrip_error'] = traceback.format_exc()
        stage = 'warm-capture'
        warm = binding.capture(a.root, a.go, cache=cache, modulecache=a.modules)
        stage = 'warm-validate'
        binding.validate(warm, a.root, a.go, cache=cache, modulecache=a.modules)
        summary['warm_recapture_roundtrip'] = 'pass'
    except Exception:
        summary['error'] = traceback.format_exc()
(a.output/'summary.json').write_text(json.dumps(summary, indent=2)+'\n')
print(json.dumps(summary, indent=2))
