"""Static path/hash inventory only; no consumer import or closure capture."""
import ast
import hashlib
import json
from pathlib import Path
import subprocess

ROOT=Path(__file__).resolve().parents[4]
OUT=Path(__file__).resolve().parent
BASE='03f2f19b5e01f0f033405e7a84cde03794d491ca'
ADAPTER='632d55de566254c618bca9a54c5dbae7b355fcf9'
FREEZE='73777de3f51ff185d02ab2e1193421fa91cde9d4'

def git(*args):return subprocess.check_output(['git',*args],cwd=ROOT)
def sha(raw):return hashlib.sha256(raw).hexdigest()
def old_sha(ref,path):
    rows=git('ls-tree',ref,'--',path)
    return sha(git('show',ref+':'+path)) if rows else None
def location(path,needle):
    return next(i for i,line in enumerate((ROOT/path).read_text().splitlines(),1) if needle in line)

registry_path='scripts/contracts/r5-archive-boundary-v1.json'
registry=json.loads((ROOT/registry_path).read_bytes())
inventory='scripts/r5_source_inventory.py'
boundary='scripts/r5_archive_boundary.py'
v3='scripts/r5_selected_source_binding_v3.py'
consumer='scripts/r5_selected_binding_consumer.py'
assert 'REGISTRY_SHA256 = '+repr(sha((ROOT/registry_path).read_bytes())) in (ROOT/boundary).read_text()
# Authenticate static membership rules without importing or executing capture.
iv=ast.parse((ROOT/inventory).read_bytes())
for name in ('_capture_legacy_v2','_current_source_paths'):
    node=next(n for n in iv.body if isinstance(n,ast.FunctionDef) and n.name==name)
    assert any(isinstance(n,ast.Constant) and n.value=='scripts' for n in ast.walk(node))
assert 'names = set(base[\'files\'])' in (ROOT/inventory).read_text()
assert "_shape(receipt['v3_source'], v3._V3_FILES, 'v3 source')" in (ROOT/consumer).read_text()
refs=[('slice4',BASE),('slices23',ADAPTER)]
paths={}
for label,ref in refs:
    for p in git('diff','--name-only',ref,FREEZE,'--','scripts','internal').decode().splitlines():
        row=paths.setdefault(p,dict(path=p,sha256=sha((ROOT/p).read_bytes()),
            git_blob=git('rev-parse',FREEZE+':'+p).decode().strip(),
            containing_source_domain='base_source.files via complete scripts walk' if p.startswith('scripts/') else 'base_source.files and archive_boundary.production_go via internal production root',
            selected_v3_source_domain='not a new v3_source key; full base_source already covers this input',
            historical_registry_record=p in registry['protected_go_files'] or p in {r['path'] for r in registry['historical_files']},
            allowed_new_control_files_listed=p in registry['allowed_new_control_files']))
        row[label+'_baseline_sha256']=old_sha(ref,p)
assert len(paths)==14
assert all(not r['historical_registry_record'] for r in paths.values())
retained=[inventory,boundary,registry_path,consumer,v3,'scripts/r5_selected_source_binding_v2.py',
    'scripts/r5_selected_source_binding.py','scripts/contracts/r5-selected-binding-v3-producer.json',
    'scripts/tests/r5_selected_source_binding_v3_checks.py','scripts/tests/r5_selected_binding_consumer_checks.py',
    'go.mod','go.sum','web/package.json','web/package-lock.json','docs/company-mail/R5-TODO.md']
retained=[p for p in retained if (ROOT/p).is_file()]
for p in retained:assert old_sha(BASE,p)==sha((ROOT/p).read_bytes())
value=dict(scope='proposed slice5 only; no closure capture, receipt refresh, registry or authority mutation',
    combined_source=FREEZE,combined_tree=git('rev-parse',FREEZE+'^{tree}').decode().strip(),
    slice4_base=BASE,slices23_adapter_base=ADAPTER,
    actual_changed_consumer_control_paths=paths,
    retained_authority_paths={p:dict(sha256=sha((ROOT/p).read_bytes()),git_blob=git('rev-parse',FREEZE+':'+p).decode().strip()) for p in retained},
    authority_locations=[dict(path=p,line=location(p,s),meaning=m) for p,s,m in [
        (inventory,'def _current_source_paths','v3 current scope walks scripts and internal; inherited by v4'),
        (inventory,'def _capture_archive_v4','v4 keeps complete base.files and adds frozen authority inputs'),
        (inventory,'def validate_current_source','exact policy/version and full canonical recapture equality'),
        (boundary,'def registry','hard-coded immutable registry byte pin; no candidate authority'),
        (boundary,'def check','historical hashes plus owned production Go; two forks/topology/budgets'),
        (v3,'_V3_FILES =','exact separate three-key helper/schema/check identity domain'),
        (consumer,"_shape(receipt['v3_source']",'exact v3_source keys and helper/schema checks'),
        (consumer,"_equal(static['registry_sha256']",'consumer checks frozen registry pin'),
        ('scripts/preparation/r5_external_batch.py','def runtime_gate','contract pin delegates serialized runtime pin and complete admitted bundle'),
        ('scripts/check_r5_protocol.py','def manifest_source_closure','archived v1 explicit membership, never current complete fallback')]],
    historical_registry=dict(path=registry_path,sha256=sha((ROOT/registry_path).read_bytes()),
        baseline=registry['baseline_commit'],historical_file_count=len(registry['historical_files']),
        protected_go_count=len(registry['protected_go_files']),markers=registry['modules'],
        allowed_new_control_files=registry['allowed_new_control_files'],
        usage='allowed_new_control_files is parsed as a required registry key; no membership enforcement reads it in current scripts'),
    minimal_proposed_write_set=['new docs/company-mail closure coverage report and exact-head evidence only, if unchanged membership is sufficient'],
    new_identity_requirements=[
        'Fresh exact combined source closure receipt/file pins; changed bytes change source_sha and source_closure_sha256 under existing membership.',
        'No old runtime/preparation/selection/batch receipt is current evidence. Future authorized controller must publish fresh independently pinned serialized bytes and preserve old receipts.',
        'No policy/schema version bump merely for changed input hashes with the same admitted domain.',
        'If a new consumer authority subset, membership, grammar, or trust rule is necessary, create a separately versioned contract/dispatch and reviewed explicit consumer changes; retain historical v1/v2/v3/v4 rules and registry bytes.',
        'Do not append keys to exact v3._V3_FILES/v3_source or broaden archived v1 membership as a shortcut.'],
    required_negative_controls=[
        'Mutate each of the 14 consumer/control inputs: old independently held source/receipt pins reject; missing/extra/renamed inputs reject.',
        'Mutate registry bytes, hard-coded pin, historical file/hash/size, archive module/marker/sum: reject without refreshing historical hashes.',
        'Unknown nested module/fork, vendor/workspace, archive import/embed/generate, unclassified Go/native/build file, symlink/special entry, midflight source/topology/registry drift: reject.',
        'Retain root versus explicit production/variant/file coverage, exact two forks, lock/provenance checks and 100000/depth64/64MiB boundary budgets.',
        'Mixed v1/v2/v3/v4 policy/schema, selected selector bool/string/float, missing byte pin, forged/resealed receipt or replacement bundle against original controller pin: reject before mocked producer/child.',
        'Preserve all twelve ordered envelope positions, four preparation slots and two admission slots; full binding equality and raw diagnostics retain distinct meanings.',
        'Pure controls use an OS guard; no new discovery IDs, lifecycle or physical provenance claim. Physical qualification remains separately authorized.'],
    ownership='Parent must confirm specific slice5 integration/authority owner; shared TODO remains unchanged and substeps proposed only',
    central='10/171 unchanged',F52='historical failures unchanged')
(OUT/'slice5-authority-inventory.json').write_text(json.dumps(value,indent=2,sort_keys=True)+'\n')
print('14 exact changed paths; historical registry preserved; proposed slice5 inventory written')
