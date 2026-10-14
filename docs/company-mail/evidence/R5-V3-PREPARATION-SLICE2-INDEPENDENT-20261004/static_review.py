import ast,hashlib,json,subprocess
from pathlib import Path
BASE='632d55de566254c618bca9a54c5dbae7b355fcf9';DELIVERY='7262e78e0120c5a203bb588807ca876b82c4d5bc';SOURCE='cad567ff2bb224b94fbf20c359383edcd0fce59b';DESIGN='42f49538f7bceb6eb8e698d716c1418f85343413'
def git(*args):return subprocess.check_output(['git',*args])
def tree(commit):
    result={}
    for row in git('ls-tree','-r','-z',commit).split(b'\0'):
        if row:
            metadata,path=row.split(b'\t');mode,kind,oid=metadata.decode().split();result[path.decode()]={'mode':mode,'kind':kind,'git_blob':oid}
    return result
old,new=tree(BASE),tree(DELIVERY)
changed=sorted(p for p in set(old)|set(new) if old.get(p)!=new.get(p))
assert changed==sorted(['scripts/r5_source_runner_prepare.py','scripts/run_r5_source_version_tests.py','scripts/tests/r5_source_preparation_v3_checks.py','docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-20261004-cad567f.json'])
source_tree=tree(SOURCE)
assert [p for p in set(source_tree)|set(new) if source_tree.get(p)!=new.get(p)]==['docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-20261004-cad567f.json']
functions={
 'scripts/r5_source_runner_prepare.py':['digest','exclusive','source_identity','typescript_members','prepare_typescript','verify_typescript','go_selection','execute_binary'],
 'scripts/run_r5_source_version_tests.py':['flatten','discover','partition','verify','git','checkout','git_go_env','child']}
checks=[]
for path,names in functions.items():
    def defs(commit):return {n.name:ast.dump(n,include_attributes=False) for n in ast.parse(git('show',commit+':'+path)).body if isinstance(n,ast.FunctionDef)}
    a,b=defs(BASE),defs(DELIVERY)
    for name in names:assert a[name]==b[name];checks.append({'path':path,'function':name,'ast_unchanged':True})
for path,names in [('scripts/run_r5_source_version_tests.py',['BASELINE','HISTORICAL_IDS','FRESH_CLASS']),('scripts/r5_source_runner_prepare.py',['LOCK_SHA256','TS_URL','TS_SHA256','TS_INTEGRITY','TEST_NAME'])]:
    def constants(commit):
        return {n.targets[0].id:ast.dump(n,include_attributes=False) for n in ast.parse(git('show',commit+':'+path)).body if isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name)}
    a,b=constants(BASE),constants(DELIVERY)
    for name in names:assert a[name]==b[name];checks.append({'path':path,'constant':name,'ast_unchanged':True})
sha=lambda raw:hashlib.sha256(raw).hexdigest()
critical=['scripts/r5_selected_binding_consumer.py','scripts/r5_selected_source_binding_v2.py','scripts/r5_selected_source_binding_v3.py','scripts/r5_source_inventory.py','scripts/r5_archive_boundary.py','scripts/r5_external_batch.py','scripts/r5_go_environment.py','scripts/tests/test_r5_source_version_runner.py','scripts/r5_source_runner_prepare.py','scripts/run_r5_source_version_tests.py','scripts/tests/r5_source_preparation_v3_checks.py']
critical=[p for p in critical if p in new]+[p for p in new if ('schema' in p or 'registry' in p) and p.startswith('scripts/')]
output=Path('docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-INDEPENDENT-20261004')
manifest={p:row for p,row in old.items() if p not in changed}
(output/'preserved-tree.json').write_text(json.dumps({'base_commit':BASE,'delivery_commit':DELIVERY,'unchanged_path_count':len(manifest),'unchanged_git_blobs_and_modes':manifest},indent=2)+'\n')
record=json.loads(git('show',DELIVERY+':docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-20261004-cad567f.json'))
assert record['source_commit']==SOURCE and record['base_commit']==BASE
replay=json.loads((output/'author-replay.json').read_bytes());assert record['result']['controls']==replay['controls'] and replay['count']==64
for p,value in replay['source_sha256'].items():assert value==sha(git('show',SOURCE+':'+p))
static={'delivery_commit':DELIVERY,'tested_source_commit':SOURCE,'base_commit':BASE,'design_commit':DESIGN,'candidate_changed_paths':changed,'delivery_vs_tested_source':'author JSON only; executable source identical','preserved_path_count':len(manifest),'preserved_tree_byte_sha256':sha((output/'preserved-tree.json').read_bytes()),'unchanged_AST_checks':checks,'critical_path_sha256':{p:sha(git('show',DELIVERY+':'+p)) for p in sorted(set(critical))},'author_report_replayed_exact_controls_and_source_hashes':True,'scope':'Git object and AST metadata review only; not process-boundary control results'}
(output/'static-review.json').write_text(json.dumps(static,indent=2)+'\n')
print(json.dumps({'unchanged_paths':len(manifest),'AST_checks':len(checks),'changed_paths':changed},indent=2))
