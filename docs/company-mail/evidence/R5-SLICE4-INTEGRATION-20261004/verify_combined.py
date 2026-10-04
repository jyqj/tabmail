"""Exact reviewed-union preservation and fresh guarded synthetic replays.

Only Python check interpreters and read-only Git metadata are started here.
Their guarded children mock producer/runtime/Go/Node/lifecycle boundaries.
Historical evidence and source are never rewritten. Go checks are text only.
"""
import ast
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
OUT = Path(__file__).resolve().parent
BASE = '03f2f19b5e01f0f033405e7a84cde03794d491ca'
FREEZE = '73777de3f51ff185d02ab2e1193421fa91cde9d4'
BATCH_SOURCE = '7fea1edeee6bcad3e77edab767997b8757873db1'
BATCH_DELIVERY = '5ebf159302520507829b93c5502b94b0f768f94e'
BATCH_REVIEW = '3637d25c80f85993509edd825089c7af25c0c4a9'
BRIDGE_SOURCE = '13ac32bbb8576a8a237dfbed46596ede8f3276ad'
BRIDGE_DELIVERY = '4643de904ff171b9b14189f63ff5e28470a73c72'
BRIDGE_REVIEW = '53532161a1096b89f733e36c1b2b1e0ee4cde747'
DESIGN = '42f49538f7bceb6eb8e698d716c1418f85343413'

def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def tree(ref):
    result = {}
    for row in git('ls-tree', '-rz', ref).split(b'\0'):
        if row:
            header, path = row.split(b'\t')
            mode, kind, oid = header.decode().split()
            assert kind == 'blob'
            result[path.decode()] = dict(mode=mode, git_blob=oid)
    return result

def blob(ref, path):
    return git('show', ref + ':' + path)

def write(name, value):
    (OUT/name).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')

def main():
    base, batch, bridge, combined = map(tree, (BASE, BATCH_REVIEW, BRIDGE_REVIEW, FREEZE))
    deltas = [{p for p in set(base)|set(t) if base.get(p) != t.get(p)} for t in (batch, bridge)]
    assert not deltas[0] & deltas[1], 'ownership collision'
    expected = dict(base)
    for origin, delta in zip((batch, bridge), deltas):
        for path in delta:
            assert path in origin, 'unexpected deletion'
            expected[path] = origin[path]
    assert combined == expected, 'not exact reviewed union'
    for ref in (BASE, BATCH_SOURCE, BATCH_DELIVERY, BATCH_REVIEW, BRIDGE_SOURCE, BRIDGE_DELIVERY, BRIDGE_REVIEW):
        subprocess.run(['git', 'merge-base', '--is-ancestor', ref, FREEZE], cwd=ROOT, check=True)
    # Hash without invoking Git per file; full working bytes and modes checked.
    for path, record in combined.items():
        local = ROOT/path
        assert local.is_file() and not local.is_symlink(), path
        raw = local.read_bytes()
        oid = hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()
        assert oid == record['git_blob'], path
        assert record['mode'] == ('100755' if local.stat().st_mode & 0o111 else '100644'), path
    preserved = {p:r for p,r in base.items() if p not in deltas[0]|deltas[1]}
    write('preserved-tree.json', dict(base=BASE, combined_source=FREEZE,
        count=len(preserved), all_unchanged_git_modes_and_blobs=preserved))
    changed = {}
    for p in sorted(deltas[0]|deltas[1]):
        changed[p] = dict(**combined[p], reviewed_origin=BATCH_REVIEW if p in deltas[0] else BRIDGE_REVIEW,
                          sha256=sha((ROOT/p).read_bytes()), base=base.get(p))
    write('full-source-manifest.json', dict(commit=FREEZE,
        tree=git('rev-parse', FREEZE+'^{tree}').decode().strip(),
        files={p:dict(**r, sha256=sha((ROOT/p).read_bytes())) for p,r in combined.items()}))
    batch_paths = ['scripts/preparation/r5_external_batch.py',
        'scripts/contracts/r5-external-batch-v2.schema.json', 'scripts/tests/r5_external_batch_v2_checks.py']
    bridge_paths = ['internal/api/handlers/r5_protocol_component_observations_test.go',
        'internal/api/handlers/r5_external_runtime_probe_test.go', 'scripts/check_r5_protocol.py',
        'scripts/tests/r5_slice4_bridge_caller_checks.py']
    for source, delivery, paths in ((BATCH_SOURCE, BATCH_DELIVERY, batch_paths), (BRIDGE_SOURCE, BRIDGE_DELIVERY, bridge_paths)):
        assert set(git('diff', '--name-only', BASE, source).decode().splitlines()) == set(paths)
        for p in paths:
            assert blob(source,p) == blob(delivery,p) == (ROOT/p).read_bytes()
            if p.endswith('.py'): ast.parse((ROOT/p).read_bytes())
    # Reproduce the author's batch preservation block only. Its later whole-tree
    # assertions assume no bridge integration and would rewrite historical logs.
    static_path = 'docs/company-mail/evidence/R5-BATCH-V2-SLICE4-20261004/static_checks.py'
    text = (ROOT/static_path).read_text().split("for path in ('scripts/contracts/r5-external-batch-v1.schema.json'")[0]
    ns = {'__file__':str(ROOT/static_path)}
    exec(compile(text, static_path, 'exec'), ns)
    assert len(ns['unchanged']) == 17
    schema = json.loads((ROOT/batch_paths[1]).read_bytes())
    assert set(schema['required']) == set(schema['properties'])
    assert schema['properties']['schema_version']['const'] == 2
    assert schema['properties']['selected_binding_version']['const'] == 3
    schema_v1 = 'scripts/contracts/r5-external-batch-v1.schema.json'
    assert blob(BASE,schema_v1) == (ROOT/schema_v1).read_bytes()
    # Case/discovery/budget/lifecycle/registry preservation follows exact union,
    # AST-preserved batch bodies, and independent Go full-file reconstruction.
    write('static-review.json', dict(base=BASE, source=FREEZE,
        source_tree=git('rev-parse', FREEZE+'^{tree}').decode().strip(), design=DESIGN,
        batch=dict(source=BATCH_SOURCE, delivery=BATCH_DELIVERY, independent=BATCH_REVIEW),
        bridge=dict(source=BRIDGE_SOURCE, delivery=BRIDGE_DELIVERY, independent=BRIDGE_REVIEW),
        exact_union=True, ancestry_preserved=True, ownership_collisions=[], semantic_resolutions=[],
        changed_paths=changed, preserved_count=len(preserved),
        batch_source_byte_identical_definitions=ns['unchanged'],
        inventory_worker_only_selector=True, batch_run_only_receipt_version=True,
        v1_constructor_ast_equivalent=True, v1_schema_byte_identical=True,
        existing_implementation_changes=[p for p in changed if p in base],
        new_implementation_controls_and_schema=[p for p in batch_paths+bridge_paths if p not in base],
        central='10/171 unchanged', F52='historical failures unchanged', TODO='byte-identical; proposed substeps only',
        Go='text reconstruction only; no Go parse, format, compile or execution',
        full_manifest_sha256=sha((OUT/'full-source-manifest.json').read_bytes()),
        preserved_manifest_sha256=sha((OUT/'preserved-tree.json').read_bytes())))
    suites = [
        ('batch-author', batch_paths[2], 14),
        ('batch-independent', 'docs/company-mail/evidence/R5-BATCH-V2-SLICE4-INDEPENDENT-20261004/independent_checks.py', 11),
        ('bridge-author', bridge_paths[3], 33),
        ('bridge-independent', 'docs/company-mail/evidence/R5-SLICE4-BRIDGE-INDEPENDENT-20261004/independent_checks.py', 79),
        ('preparation-author', 'scripts/tests/r5_source_preparation_v3_checks.py', 64),
        ('preparation-independent', 'docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-INDEPENDENT-20261004/independent_checks.py', 90),
        ('preparation-preserved', 'docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-INDEPENDENT-20261004/preserved_pure_checks.py', 15),
        ('runtime-author', 'scripts/tests/r5_external_runtime_v3_checks.py', 30),
        ('runtime-independent', 'docs/company-mail/evidence/R5-RUNTIME-V3-INDEPENDENT-20261004/independent_checks.py', 13),
    ]
    results=[]
    for name, path, count in suites:
        argv=[sys.executable,'-B',path]
        proc=subprocess.run(argv,cwd=ROOT,env=dict(os.environ,PYTHONDONTWRITEBYTECODE='1'),
            stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
        (OUT/(name+'.stdout')).write_bytes(proc.stdout)
        (OUT/(name+'.stderr')).write_bytes(proc.stderr)
        assert proc.returncode==0,(name,proc.stderr.decode())
        try: record=json.loads(proc.stdout)
        except json.JSONDecodeError: record=json.loads(proc.stdout.splitlines()[-1])
        assert record.get('count',record.get('tests'))==count,(name,record)
        assert record['os_process_events']==[]
        assert record.get('failures',0)==record.get('errors',0)==0
        results.append(dict(name=name,argv=argv,result=record,exit_code=0,
            source_sha256=sha((ROOT/path).read_bytes()),stdout_sha256=sha(proc.stdout),stderr_sha256=sha(proc.stderr)))
        print(name,count,'PASS; guarded OS events 0',flush=True)
    # This independent checker prints results only and never writes old evidence.
    path='docs/company-mail/evidence/R5-SLICE4-BRIDGE-INDEPENDENT-20261004/static_review.py'
    proc=subprocess.run([sys.executable,'-B',path],cwd=ROOT,stdout=subprocess.PIPE,stderr=subprocess.PIPE,timeout=60)
    (OUT/'bridge-static.stdout').write_bytes(proc.stdout)
    (OUT/'bridge-static.stderr').write_bytes(proc.stderr)
    assert proc.returncode==0,proc.stderr.decode()
    record=json.loads(proc.stdout)
    assert record['count']==2280
    write('bridge-static-results.json',record)
    # Confirm every historical/source byte after all replays, including evidence.
    for path,record in combined.items():
        assert git('hash-object',str(ROOT/path)).decode().strip()==record['git_blob'],path
    subprocess.run(['git','diff','--check'],cwd=ROOT,check=True)
    write('fresh-results.json',dict(source=FREEZE,suites=results,total=349,
        counting='named controls/test methods; receipt mutation subtests and lexical Go model rows are separate',
        guard_events=[],bridge_static_assertions=2280,
        Go='static full-text reconstruction and lexical model only; no Go execution or syntax/type/compile proof',
        synthetic_only=True,actual_producer_runtime_Go_Node_PG_mail_formal_lifecycle=False,
        orchestration='Python check interpreter starts and read-only Git metadata are outside guarded child counts',
        central='10/171 unchanged',F52='historical failures unchanged'))
    print('349 pure controls/test methods and 2280 independent static assertions PASS',flush=True)

if __name__=='__main__':
    main()
