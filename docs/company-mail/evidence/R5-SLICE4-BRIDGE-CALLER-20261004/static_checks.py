#!/usr/bin/env python3
"""Read-only source-data review. No Git subprocess, Go tool, or consumer import."""
import sys
EVENTS=[]
def audit(event,args):
    if event.startswith(('subprocess.', 'os.exec', 'os.spawn', 'os.posix_spawn')) or event in ('os.fork','os.forkpty','os.system','pty.spawn'):
        EVENTS.append(event)
        raise AssertionError('OS execution forbidden: '+event)
sys.addaudithook(audit)
import ast
import hashlib
import json
from pathlib import Path
import stat
ROOT=Path(__file__).resolve().parents[4]
HERE=Path(__file__).resolve().parent
controls=[]
def check(name, condition):
    assert condition,name
    controls.append(name)
def sha(raw):return hashlib.sha256(raw).hexdigest()
delta=json.loads((HERE/'reviewed-delta.json').read_text())
records=json.loads((HERE/'frozen-tree.json').read_text())
check('exact-three-existing-owned-paths',set(delta)=={
    'internal/api/handlers/r5_protocol_component_observations_test.go',
    'internal/api/handlers/r5_external_runtime_probe_test.go','scripts/check_r5_protocol.py'})
preserved=[]
for record in records:
    path=record['path']
    info=(ROOT/path).lstat()
    check('preserved-mode-'+path,stat.S_ISREG(info.st_mode) and bool(info.st_mode & 0o111)==(record['mode']=='100755'))
    if path in delta:
        check('frozen-snapshot-'+path,sha((HERE/delta[path]['snapshot']).read_bytes())==record['sha256'])
    else:
        check('preserved-'+path,sha((ROOT/path).read_bytes())==record['sha256'])
        preserved.append(record)
for path,entry in delta.items():
    old=(HERE/entry['snapshot']).read_text().splitlines(keepends=True)
    for change in reversed(entry['changes']):
        check('reviewed-old-fragment-'+path+':'+str(change['start']),''.join(old[change['start']:change['end']])==change['old'])
        old[change['start']:change['end']]=[change['new']]
    check('exact-reviewed-delta-'+path,''.join(old)==(ROOT/path).read_text())

py_path='scripts/check_r5_protocol.py'
before=ast.parse((HERE/delta[py_path]['snapshot']).read_text())
after=ast.parse((ROOT/py_path).read_text())
def normalize(tree):
    for node in ast.walk(tree):
        if isinstance(node,ast.FunctionDef) and node.name=='run_shared':
            for nested in ast.walk(node):
                if isinstance(nested,ast.If):
                    nested.body[:]=[n for n in nested.body if not (isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name) and n.targets[0].id=='selected_binding_version')]
                if isinstance(nested,ast.Call) and isinstance(nested.func,ast.Attribute) and isinstance(nested.func.value,ast.Name) and nested.func.value.id=='r5_external_runtime':
                    nested.keywords[:]=[kw for kw in nested.keywords if kw.arg!='selected_binding_version']
    return ast.dump(tree,include_attributes=False)
check('caller-AST-identical-except-version-propagation',normalize(after)==ast.dump(before,include_attributes=False))
for path in delta:
    if path.endswith('.go'):
        text=(ROOT/path).read_text()
        check('Go-source-data-only-balanced-braces-'+path,text.count('{')==text.count('}'))
component=(ROOT/'internal/api/handlers/r5_protocol_component_observations_test.go').read_text()
frozen=(HERE/'frozen-component.go.txt').read_text()
check('all-Go-assertion-fixture-tail-bytes-preserved',component[component.index('// Shared assertions remain unchanged;'):]==frozen[frozen.index('// Shared assertions remain unchanged;'):])
check('all-Go-fixture-prefix-bytes-preserved',component[:component.index('func r5UIExternalRuntime(')]==frozen[:frozen.index('func r5UIExternalRuntime(')])
for token in ('selectedVersion := 2','os.LookupEnv("TABMAIL_R5_SELECTED_BINDING_VERSION")','case "2":','case "3":',
    'SchemaVersion     json.RawMessage `json:"schema_version"`','SelectedVersion   json.RawMessage `json:"selected_binding_version"`',
    'string(manifest.SchemaVersion) != fmt.Sprint(selectedVersion)',
    'fmt.Sprintf("r5_external_dependency_runtime_v%d", selectedVersion)',
    'selectedVersion == 2 && (len(manifest.SelectedVersion) != 0 || len(manifest.AdmittedSelection) != 0)',
    'selectedVersion == 3 && string(manifest.SelectedVersion) != "3"',
    '"validate", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion)',
    '"launch", "--source", root, "--selected-binding-version", fmt.Sprint(selectedVersion), "--"'):
    check('Go-structural-'+token,token in component)
check('Go-pins-before-validation-dispatch',component.index('external runtime manifest pin differs')<component.index('selectedVersion := 2')<component.index('external runtime selected version differs')<component.index('external runtime executable drift')<component.index('cmd := exec.Command(manifest.Python.Path'))
for token in ('if !filepath.IsAbs(file.Path)', 'os.Lstat(file.Path)', 'if !info.Mode().IsRegular()', 'sha256.Sum256(bytes)', 'hex.EncodeToString(digest[:]) != file.SHA256'):
    check('Go-retained-tool-check-'+token,token in component)
for path in ('internal/api/handlers/r5_external_runtime_probe_test.go','internal/api/handlers/r5_protocol_component_observations_test.go'):
    check('Go-caller-negotiated-selector-'+path,'launcher, node, cli, selectedVersion := r5UIExternalRuntime(t, root)' in (ROOT/path).read_text())
check('zero-OS-process-events',EVENTS==[])
print(json.dumps({'scope':'Data-only source and frozen-tree preservation; no Go parse/compile/test/runtime',
    'controls':controls,'count':len(controls),'unchanged_frozen_paths':len(preserved),'os_process_events':EVENTS,
    'base_commit':'03f2f19b5e01f0f033405e7a84cde03794d491ca',
    'source_sha256':{path:sha((ROOT/path).read_bytes()) for path in delta},
    'check_sha256':{str(path.relative_to(ROOT)):sha(path.read_bytes()) for path in (Path(__file__),ROOT/'scripts/tests/r5_slice4_bridge_caller_checks.py')},
    'limitations':['Go structural observations and decision table do not execute Go or its JSON decoder','Physical and formal qualification remain separately authorized']},indent=2))
