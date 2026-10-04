"""Read-only Git/AST identity checks. No runtime/producer/tool execution.

Git subprocesses here are static repository reads, outside the guarded synthetic
runtime tests; they are not included in the zero-process synthetic claim.
"""
import ast
import hashlib
import json
from pathlib import Path
import subprocess

ROOT=Path(__file__).resolve().parents[4]
BASE='632d55de566254c618bca9a54c5dbae7b355fcf9'
PARENT='6d7a3979ccea031f28f12e94f058731339044f19'
TESTED='dcaf2bf91355d4c0524f34f0c767b7997a994833'
DELIVERY='97c54dc8dad6ab2da6d4dd29e43c20b7091db73f'
DESIGN='42f49538f7bceb6eb8e698d716c1418f85343413'
def git(*args): return subprocess.check_output(['git',*args],cwd=ROOT)
def blob(commit,path): return git('show',commit+':'+path)
def sha(raw): return hashlib.sha256(raw).hexdigest()
paths=git('diff','--name-only',PARENT,TESTED).decode().splitlines()
expected=['scripts/preparation/probe_r5_external_runtime.py','scripts/preparation/r5_external_runtime.py',
          'scripts/preparation/run_r5_external_scoped.py','scripts/tests/r5_external_runtime_v3_checks.py']
assert sorted(paths)==sorted(expected)
tracked=git('ls-tree','-r','--name-only',BASE).decode().splitlines()
unchanged=[p for p in tracked if blob(BASE,p)==blob(TESTED,p)]
runtime='scripts/preparation/r5_external_runtime.py'
old=blob(BASE,runtime).decode(); new=blob(TESTED,runtime).decode()
def defs(source):
    return {n.name:ast.get_source_segment(source,n) for n in ast.parse(source).body
            if isinstance(n,(ast.FunctionDef,ast.ClassDef))}
olddefs,newdefs=defs(old),defs(new)
preserved=['Descriptors','descriptors','dependency_records','clean_environment','owner','canonical','digest']
assert all(olddefs[n]==newdefs[n] for n in preserved)
def constructor(source):
    return next(n for n in ast.walk(ast.parse(source)) if isinstance(n,ast.Call)
                and isinstance(n.func,ast.Name) and n.func.id=='dict'
                and any(k.arg=='concurrency_boundary' for k in n.keywords))
prior,current=constructor(old),constructor(new)
for keyword in current.keywords:
    if keyword.arg=='schema_version': keyword.value=ast.Constant(value=2)
    if keyword.arg=='policy': keyword.value=ast.Name(id='POLICY',ctx=ast.Load())
assert ast.dump(prior)==ast.dump(current)
frozen=['scripts/r5_selected_binding_consumer.py','scripts/r5_selected_source_binding_v2.py',
        'scripts/r5_selected_source_binding_v3.py','scripts/r5_source_runner_prepare.py',
        'scripts/run_r5_source_version_tests.py','scripts/check_r5_protocol.py',
        'scripts/preparation/r5_external_batch.py','scripts/r5_source_inventory.py']
assert all(blob(BASE,p)==blob(TESTED,p) for p in frozen)
identities={}
for path in expected+frozen:
    raw=blob(TESTED,path)
    ast.parse(raw,filename=path)
    assert raw==blob(DELIVERY,path)==(ROOT/path).read_bytes()
    identities[path]=dict(sha256=sha(raw),git_blob=git('rev-parse',TESTED+':'+path).decode().strip())
evidence=Path(__file__).resolve().parent
for name in ['independent_checks.py','independent.log','author-replay.log','static_review.py']:
    identities[str((evidence/name).relative_to(ROOT))]=dict(sha256=sha((evidence/name).read_bytes()))
design_path='docs/company-mail/R5-V3-CONSUMER-MIGRATION-DESIGN-20261004.md'
report_path='docs/company-mail/R5-RUNTIME-V3-SLICE3-20261004.md'
result=dict(decision='ACCEPT_SCOPED_SYNTHETIC_IMPLEMENTATION',accepted_adapter_base=BASE,
    immediate_parent=PARENT,tested_implementation=TESTED,delivery=DELIVERY,design=DESIGN,
    design_sha256=sha(blob(DESIGN,design_path)),delivery_report_sha256=sha(blob(DELIVERY,report_path)),
    exact_implementation_delta=paths,unchanged_base_paths=len(unchanged),
    unchanged_immediate_parent_paths=len(git('ls-tree','-r','--name-only',PARENT).decode().splitlines())-3,
    unchanged_base_paths_manifest_sha256=sha(('\n'.join(unchanged)+'\n').encode()),
    base_to_tested_delta=git('diff','--name-only',BASE,TESTED).decode().splitlines(),
    tested_to_delivery_delta=git('diff','--name-only',TESTED,DELIVERY).decode().splitlines(),
    source_byte_preserved_definitions=preserved,v2_manifest_constructor_ast_equal=True,
    frozen_paths=frozen,identities=identities,
    independent=json.loads((evidence/'independent.log').read_text().splitlines()[-1]),
    author_replay=json.loads((evidence/'author-replay.log').read_text().splitlines()[-1]))
(evidence/'identities.json').write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
print(json.dumps({k:result[k] for k in ('decision','unchanged_base_paths','v2_manifest_constructor_ast_equal','independent','author_replay')},sort_keys=True))
