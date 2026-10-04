#!/usr/bin/env python3
"""Read-only Git provenance and independent complete source-delta review.
Git metadata subprocesses are explicit and outside synthetic zero-process counts.
"""
import ast
import hashlib
import json
from pathlib import Path
import subprocess
ROOT=Path(__file__).resolve().parents[4]
HERE=Path(__file__).resolve().parent
BASE='03f2f19b5e01f0f033405e7a84cde03794d491ca'
TESTED='13ac32bbb8576a8a237dfbed46596ede8f3276ad'
DELIVERY='4643de904ff171b9b14189f63ff5e28470a73c72'
CHECKS=[]
GIT_CALLS=[]
def git(*args):
    GIT_CALLS.append(['git',*args])
    return subprocess.check_output(['git',*args],cwd=ROOT)
def check(name,ok):assert ok,name;CHECKS.append(name)
def tree(ref):
    out={}
    for row in git('ls-tree','-rz',ref).split(b'\0'):
        if not row:continue
        header,name=row.split(b'\t',1);mode,kind,blob=header.decode().split()
        assert kind=='blob'
        out[name.decode()]={'mode':mode,'blob':blob}
    return out
old=tree(BASE);tested=tree(TESTED);delivery=tree(DELIVERY)
owned={'internal/api/handlers/r5_protocol_component_observations_test.go','internal/api/handlers/r5_external_runtime_probe_test.go','scripts/check_r5_protocol.py'}
new='scripts/tests/r5_slice4_bridge_caller_checks.py'
check('implementation-exact-existing-three-plus-one-new',set(tested)-set(old)=={new} and not set(old)-set(tested) and {p for p in old if old[p]!=tested[p]}==owned)
check('delivery-report-evidence-only',all(delivery[p]==tested[p] for p in tested) and all(p=='docs/company-mail/R5-SLICE4-BRIDGE-CALLER-20261004.md' or p.startswith('docs/company-mail/evidence/R5-SLICE4-BRIDGE-CALLER-20261004/') for p in set(delivery)-set(tested)))
check('all-original-Git-modes-unchanged',all(old[p]['mode']==tested[p]['mode'] for p in old))
check('2257-original-blobs-preserved',sum(old[p]==tested[p] for p in old)==2257)
for p in owned|{new}:
    check('tested-delivery-working-byte-identity-'+p,git('show',TESTED+':'+p)==git('show',DELIVERY+':'+p)==(ROOT/p).read_bytes())
author=ROOT/'docs/company-mail/evidence/R5-SLICE4-BRIDGE-CALLER-20261004'
records=json.loads((author/'frozen-tree.json').read_text())
check('author-frozen-tree-exact-base-mode-blob-manifest',{r['path']:{'mode':r['mode'],'blob':r['blob']} for r in records}==old)
# SHA256 independent authentication of all author records using base Git blobs.
for r in records:
    check('base-SHA256-'+r['path'],hashlib.sha256(git('cat-file','blob',r['blob'])).hexdigest()==r['sha256'])
for p,snap in [('internal/api/handlers/r5_protocol_component_observations_test.go','frozen-component.go.txt'),('internal/api/handlers/r5_external_runtime_probe_test.go','frozen-probe.go.txt'),('scripts/check_r5_protocol.py','frozen-caller.py.txt')]:
    check('snapshot-actual-base-'+p,git('show',BASE+':'+p)==(author/snap).read_bytes())
component='internal/api/handlers/r5_protocol_component_observations_test.go'
text=(ROOT/component).read_text()
check('exact-Go-validate-argv','exec.Command(manifest.Python.Path, helper, "validate", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion))' in text)
check('exact-Go-launch-argv','exec.CommandContext(ctx, launcher, filepath.Join(root, "scripts/preparation/r5_external_runtime.py"), "launch", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion), "--", node, cli, "run", "--cache=false", "--experimental.fsModuleCache=false", "--config", config, "--reporter=json", "--outputFile", report)' in text)
# Reverse independently enumerated allowed Go source changes, compare full files.
text=text.replace('(string, string, string, int)', '(string, string, string)')
selector='\tselectedVersion := 2\n\tif value, present := os.LookupEnv("TABMAIL_R5_SELECTED_BINDING_VERSION"); present {\n\t\tswitch value {\n\t\tcase "2":\n\t\tcase "3":\n\t\t\tselectedVersion = 3\n\t\tdefault:\n\t\t\tt.Fatal("external runtime selector must be integer 2 or 3")\n\t\t}\n\t}\n'
check('Go-selector-exact-omission-and-spellings',text.count(selector)==1)
text=text.replace(selector,'')
fields='\t\tHelperSHA256      string          `json:"helper_sha256"`\n\t\tSchemaVersion     json.RawMessage `json:"schema_version"`\n\t\tSelectedVersion   json.RawMessage `json:"selected_binding_version"`\n\t\tAdmittedSelection json.RawMessage `json:"admitted_selection"`\n'
text=text.replace(fields,'\t\tHelperSHA256      string `json:"helper_sha256"`\n')
text=text.replace('string(manifest.SchemaVersion) != fmt.Sprint(selectedVersion) || manifest.Policy != fmt.Sprintf("r5_external_dependency_runtime_v%d", selectedVersion)','manifest.Policy != "r5_external_dependency_runtime_v2"')
text=text.replace('\tif selectedVersion == 2 && (len(manifest.SelectedVersion) != 0 || len(manifest.AdmittedSelection) != 0) || selectedVersion == 3 && string(manifest.SelectedVersion) != "3" {\n\t\tt.Fatal("external runtime selected version differs")\n\t}\n','')
text=text.replace(', "--selected-binding-version", fmt.Sprint(selectedVersion)','')
text=text.replace('return manifest.Python.Path, manifest.Node.Path, manifest.CLI.Path, selectedVersion','return manifest.Python.Path, manifest.Node.Path, manifest.CLI.Path')
text=text.replace('report string, selectedVersion int)','report string)')
text=text.replace('launcher, node, cli, selectedVersion :=','launcher, node, cli :=')
text=text.replace('report, selectedVersion)','report)')
check('complete-Go-file-identical-after-only-reviewed-plumbing-reversed',text.encode()==git('show',BASE+':'+component))
p='internal/api/handlers/r5_external_runtime_probe_test.go'
probe=(ROOT/p).read_text().replace('launcher, node, cli, selectedVersion :=','launcher, node, cli :=').replace('report, selectedVersion)','report)')
check('complete-probe-file-only-two-selector-bindings',probe.encode()==git('show',BASE+':'+p))
p='scripts/check_r5_protocol.py'
py=(ROOT/p).read_text()
py=py.replace('        selected_binding_version = r5_external_runtime.environment_version()\n','')
py=py.replace(', selected_binding_version=selected_binding_version','').replace(',selected_binding_version=selected_binding_version','')
check('complete-Python-file-only-selector-plumbing',py.encode()==git('show',BASE+':'+p))
check('Python-AST-parse',bool(ast.parse((ROOT/p).read_text())))
check('diff-whitespace-check',git('diff','--check',DELIVERY).strip()==b'')
print(json.dumps({'checks':CHECKS,'count':len(CHECKS),'base':BASE,'tested':TESTED,'delivery':DELIVERY,'trees':{ref:git('rev-parse',ref+'^{tree}').decode().strip() for ref in (BASE,TESTED,DELIVERY)},'preserved_paths':2257,'metadata_Git_process_count':len(GIT_CALLS),'source_sha256':{p:hashlib.sha256((ROOT/p).read_bytes()).hexdigest() for p in sorted(owned|{new})},'limits':['Git metadata processes are outside guarded synthetic zero-process claim','Go full-text reconstruction and argv inspection are not Go syntax/compile/runtime proof']},indent=2))
