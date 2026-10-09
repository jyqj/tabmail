"""Read-only schema verification against independently downloaded official sources.

Usage: python3 verify_producer_schema.py /tmp/r5-v3-source
No third-party code is installed or executed. The inputs are plain Go source text.
"""
import hashlib
import json
from pathlib import Path
import re
import sys
ROOT=Path(__file__).resolve().parents[4]
registry=json.loads((ROOT/'scripts/contracts/r5-selected-binding-v3-producer.json').read_text())
source_root=Path(sys.argv[1]);observed={};facts=[]
for source,producer in zip(registry['sources'],('PackagePublic','ModulePublic','Origin')):
    raw=(source_root/Path(source['path']).name).read_bytes()
    assert hashlib.sha256(raw).hexdigest()==source['sha256'],source['path']
    text=raw.decode()
    body=re.search(r'type '+producer+r' struct \{(.*?)\n\}',text,re.S).group(1)
    fields={m[1]:m[2] for m in re.finditer(r'^\s*(\w+)\s+(\S+)\s+`json:',body,re.M)}
    assert fields==registry['types'][producer],producer
    observed[producer]=fields
    facts.append(dict(producer=producer,fields=len(fields),sha256=source['sha256'],url=source['url']))
    if producer=='PackagePublic':
        marshaler=re.search(r'func \(p \*PackageError\) MarshalJSON\(\) \(\[\]byte, error\) \{.*?perr := struct \{(.*?)\n\s*\}',text,re.S).group(1)
        actual={m[1]:m[2] for m in re.finditer(r'^\s*(\w+)\s+(\S+)',marshaler,re.M)}
        assert actual=={'ImportStack':'[]string','Pos':'string','Err':'string'}
        assert registry['types']['PackageError']==dict(actual,ImportStack='[]string|null')
    if producer=='ModulePublic':
        error_body=re.search(r'type ModuleError struct \{(.*?)\n\}',text,re.S).group(1)
        assert {m[1]:m[2] for m in re.finditer(r'^\s*(\w+)\s+(\S+)',error_body,re.M)}==registry['types']['ModuleError']
assert observed['PackagePublic']['ImportMap']=='map[string]string'
assert {k:observed['PackagePublic'][k] for k in registry['package_diagnostic_fields']}=={'Stale':'bool','StaleReason':'string'}
print(json.dumps(dict(result='exact_complete_declarations_and_error_marshaler_match',source_facts=facts,retained_package_fields=len(observed['PackagePublic'])-2),indent=2,sort_keys=True))
