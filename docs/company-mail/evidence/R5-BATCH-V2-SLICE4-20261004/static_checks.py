"""Read-only Git/AST preservation checks; never imports or executes consumers."""
import ast
import hashlib
import json
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parents[4]
BASE='03f2f19b5e01f0f033405e7a84cde03794d491ca'
HELPER='scripts/preparation/r5_external_batch.py'
OUT=Path(__file__).resolve().parent
def git(*args):return subprocess.check_output(['git','-C',str(ROOT),*args])
def functions(source):
    tree=ast.parse(source); result={}
    for node in tree.body:
        if isinstance(node,(ast.FunctionDef,ast.ClassDef)):
            if isinstance(node,ast.ClassDef) and node.name=='Batch':
                for method in node.body:
                    if isinstance(method,ast.FunctionDef):result['Batch.'+method.name]=method
            else:result[node.name]=node
    return result
old=git('show',BASE+':'+HELPER).decode();new=(ROOT/HELPER).read_text()
a,b=functions(old),functions(new)
allowed={'capture','validate_contract','load','Batch.__init__','Batch.validate_inventory','Batch._run','main'}
unchanged=[]
for name,node in a.items():
    if name not in allowed:
        assert ast.get_source_segment(old,node)==ast.get_source_segment(new,b[name]),name
        unchanged.append(name)
# Inventory worker only adds explicit CLI selection; finish keeps original abort/deadline.
x=ast.get_source_segment(new,b['Batch.validate_inventory']).replace(
    "self.pin,\n                              '--selected-binding-version', str(self.selected_binding_version)", 'self.pin')
assert ast.dump(ast.parse(x))==ast.dump(ast.parse(ast.get_source_segment(old,a['Batch.validate_inventory'])))
# Execution lifecycle differs only in receipt schema/policy and its explicit v3 selector.
x=ast.get_source_segment(new,b['Batch._run']).replace(
    "schema_version=self.contract['schema_version'],policy=self.contract['policy']",'schema_version=1,policy=POLICY').replace(
    "            if self.selected_binding_version == 3:\n                receipt['selected_binding_version'] = 3\n",'')
assert ast.dump(ast.parse(x))==ast.dump(ast.parse(ast.get_source_segment(old,a['Batch._run'])))
# The constructor's v1 shape, catalogs, budgets and every command remain equivalent.
old_contract=next(n for n in ast.walk(a['capture']) if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='contract' for t in n.targets))
new_contract=next(n for n in ast.walk(b['capture']) if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='contract' for t in n.targets))
for keyword in new_contract.value.keywords:
    if keyword.arg in ('schema_version','policy'):keyword.value=keyword.value.body
assert ast.dump(old_contract)==ast.dump(new_contract)
for path in ('scripts/contracts/r5-external-batch-v1.schema.json','scripts/preparation/r5_external_runtime.py',
             'scripts/check_r5_protocol.py','internal/api/handlers/r5_protocol_component_observations_test.go',
             'docs/company-mail/R5-TODO.md'):
    assert (ROOT/path).read_bytes()==git('show',BASE+':'+path),path
preserved=[];changed=[]
for line in git('ls-tree','-r',BASE).decode().splitlines():
    meta,path=line.split('\t');mode,kind,blob=meta.split()
    local=ROOT/path
    assert local.exists(),path
    current=hashlib.sha1(b'blob '+str(len(local.read_bytes())).encode()+b'\0'+local.read_bytes()).hexdigest()
    actual_mode='100755' if local.stat().st_mode & 0o111 else '100644'
    if blob==current and mode==actual_mode:preserved.append(dict(path=path,mode=mode,blob=blob))
    else:changed.append(path)
assert changed==[HELPER],changed
schema=json.loads((ROOT/'scripts/contracts/r5-external-batch-v2.schema.json').read_text())
assert set(schema['required'])==set(schema['properties'])
assert set(schema['required'])==set(('runtime required budgets go build_context argv schema_version policy status mode workers concurrency_boundary product_green task_complete runtime_sha256 catalog_sha256 helper_sha256 bridge_sha256 probe_config_sha256 selected_binding_version runtime_manifest_path runtime_manifest_sha256').split())
result=dict(base=BASE,head=git('rev-parse','HEAD').decode().strip(),unchanged_definitions=unchanged,
    inventory_worker_only_selector_change=True,execution_lifecycle_only_result_version_change=True,
    v1_constructor_equivalent=True,v1_schema_byte_identical=True,changed_existing_paths=changed,
    preserved_existing_paths=len(preserved),source_hashes={path:hashlib.sha256((ROOT/path).read_bytes()).hexdigest()
        for path in (HELPER,'scripts/contracts/r5-external-batch-v2.schema.json','scripts/tests/r5_external_batch_v2_checks.py')})
(OUT/'static-results.json').write_text(json.dumps(result,indent=2)+'\n')
(OUT/'preserved-tree.json').write_text(json.dumps(preserved,indent=2)+'\n')
print(json.dumps(result,indent=2))
