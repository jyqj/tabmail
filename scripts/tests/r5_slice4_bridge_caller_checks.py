#!/usr/bin/env python3
"""Explicit synthetic caller controls; never discovered and never runs Go/runtime."""
import sys
PROCESS_EVENTS = []
def audit(event, args):
    if event.startswith(('subprocess.', 'os.exec', 'os.spawn', 'os.posix_spawn')) or event in ('os.fork', 'os.forkpty', 'os.system', 'pty.spawn'):
        PROCESS_EVENTS.append(event)
        raise AssertionError('OS execution forbidden: ' + event)
sys.addaudithook(audit)

import ast
import hashlib
import json
import os
from pathlib import Path
import tempfile
from types import ModuleType, SimpleNamespace
from unittest.mock import Mock, patch

ROOT = Path(__file__).resolve().parents[2]
RESULTS = []
def control(name, fn):
    fn()
    RESULTS.append(name)
def reject(fn, expected=ValueError):
    try:
        fn()
    except expected as error:
        return error
    raise AssertionError('expected rejection')
def load_functions(path, names, namespace):
    tree = ast.parse(path.read_text())
    selected = [n for n in tree.body if isinstance(n, ast.FunctionDef) and n.name in names]
    assert {n.name for n in selected} == set(names)
    exec(compile(ast.Module(body=selected, type_ignores=[]), str(path), 'exec'), namespace)

runtime = ModuleType('r5_external_runtime')
env_namespace = {'os': os}
load_functions(ROOT/'scripts/preparation/r5_external_runtime.py', ['environment_version'], env_namespace)
runtime.environment_version = env_namespace['environment_version']

def exercise(selection=None, failure=None, go_failure=False, layer='components', drift=False):
    calls = []
    namespace = {'sys':sys, 'os':os, 'json':json, 'hashlib':hashlib, 'ROOT':ROOT}
    manifest = {'node':{'path':'/synthetic/node'}, 'cli':{'path':'/synthetic/cli'}}
    def admission(source, *, selected_binding_version):
        calls.append(('load', selected_binding_version))
        if failure == 'load': raise ValueError('load rejected')
        if drift: os.environ['TABMAIL_R5_SELECTED_BINDING_VERSION'] = '2' if selected_binding_version == 3 else '3'
        return manifest
    def validate(value, *, selected_binding_version):
        assert value is manifest
        calls.append(('validate', selected_binding_version))
        if failure == 'validate' or failure == 'postcheck' and len([c for c in calls if c[0]=='validate']) == 2:
            raise ValueError('validation rejected')
    def launch(value, argv, env, *, selected_binding_version):
        assert value is manifest
        calls.append(('launch', selected_binding_version))
        assert argv == [ '/synthetic/node', '/synthetic/cli', 'run', '--cache=false', '--experimental.fsModuleCache=false', 'components/company/r5-protocol.test.tsx', '--reporter=json', '--outputFile='+str((output/'vitest.json').resolve()) ]
        if failure == 'launch': raise ValueError('launch rejected')
        (output/'vitest.json').write_text('{}')
        return SimpleNamespace(stdout='synthetic',stderr='',returncode=0)
    runtime.from_environment = Mock(side_effect=admission)
    runtime.validate = Mock(side_effect=validate)
    runtime.launch = Mock(side_effect=launch)
    child = Mock(return_value=SimpleNamespace(stdout='',stderr='',returncode=1 if go_failure else 0))
    namespace.update(subprocess=SimpleNamespace(run=child), source_closure=lambda:'frozen',
        protocol_source_metadata=lambda:{'source_sha':'synthetic'}, current_protocol_environment=lambda _:dict(os.environ),
        shared_command=lambda *args:['synthetic-go-never-executed'],
        classify_shared_events=lambda *args:{'errors':['synthetic failure'] if go_failure else []},
        classify_go_component_packets=lambda *args:([],set()), classify_components=lambda *args:{'errors':[]}, summary=lambda _: {})
    load_functions(ROOT/'scripts/check_r5_protocol.py', ['run_shared','external_component_command'],namespace)
    data={'cases':[{'id':'RC01','required_layers':[], 'shared_adapters':[
        {'package':'./synthetic','test':'SyntheticOnly','layers':[layer]}, {'runner':'vitest','layers':['components']}]}]}
    with tempfile.TemporaryDirectory() as directory, patch.dict(os.environ, {}, clear=True), patch.dict(sys.modules, {'r5_external_runtime':runtime}):
        if selection is not None: os.environ['TABMAIL_R5_SELECTED_BINDING_VERSION'] = selection
        namespace['CASES'] = Path(directory)/'cases.json'
        namespace['CASES'].write_text('{}')
        output = Path(directory)/'out'
        old_path = sys.path[:]
        try:
            if failure or selection is not None and selection not in ('2','3'):
                reject(lambda:namespace['run_shared'](data,layer,output))
            else:
                namespace['run_shared'](data,layer,output)
        finally:
            sys.path[:] = old_path
    if layer != 'components':
        assert not calls
    elif selection is not None and selection not in ('2','3'):
        assert not calls and child.call_count == 0
    else:
        version = 3 if selection == '3' else 2
        assert all(c[1] == version for c in calls)
        if failure in ('load','validate'):
            assert child.call_count == 0 and runtime.launch.call_count == 0
        elif failure == 'launch':
            assert [c[0] for c in calls] == ['load','validate','launch']
        elif go_failure:
            assert [c[0] for c in calls] == ['load','validate','validate']
        else:
            assert [c[0] for c in calls] == ['load','validate','launch','validate']
        if child.call_count:
            assert child.call_args.kwargs['timeout'] == 180
            assert child.call_args.args[0] == ['synthetic-go-never-executed']

for selector in (None,'2','3'):
    control('caller-selector-'+repr(selector), lambda s=selector:exercise(s))
    control('stable-negotiation-after-environment-drift-'+repr(selector), lambda s=selector:exercise(s,drift=True))
    control('go-failure-no-launch-'+repr(selector), lambda s=selector:exercise(s,go_failure=True))
    for failure in ('load','validate','launch','postcheck'):
        control(failure+'-'+repr(selector), lambda s=selector,f=failure:exercise(s,f))
for selector in ('','1','4','true','false','3.0','03',' 3','3 ','null'):
    control('invalid-environment-zero-dispatch-'+repr(selector),lambda s=selector:exercise(s))
control('unit-does-not-load-runtime', lambda:exercise('3',layer='unit'))

# A data-only decision table mirrors the source-verified Go lexical gate.
# It is deliberately not represented as execution of Go or its JSON decoder.
def bridge_gate(env, schema, policy, selected, admitted=False):
    if env is None: version=2
    elif env in ('2','3'): version=int(env)
    else: return False
    return (schema == str(version) and policy == 'r5_external_dependency_runtime_v'+str(version)
            and (selected is None and not admitted if version == 2 else selected == '3'))
def matrix():
    total=accepted=0
    for env in (None,'2','3','','1','4','true','3.0','03'):
        for schema in (None,'1','2','3','true','"3"','3.0','null','4'):
            for policy in (None,'r5_external_dependency_runtime_v1','r5_external_dependency_runtime_v2','r5_external_dependency_runtime_v3','unknown'):
                for selected in (None,'1','2','3','true','"3"','3.0','null','4'):
                    for admitted in (False,True):
                        got=bridge_gate(env,schema,policy,selected,admitted)
                        expected=(env in (None,'2') and schema=='2' and policy=='r5_external_dependency_runtime_v2' and selected is None and not admitted or env=='3' and schema=='3' and policy=='r5_external_dependency_runtime_v3' and selected=='3')
                        assert got == expected
                        total+=1;accepted+=got
    return total,accepted
MATRIX = matrix()
control('data-only-Go-lexical-version-pairing-matrix',lambda:None)
assert PROCESS_EVENTS == []
print(json.dumps({'scope':'Synthetic actual Python caller and data-only Go model; no runtime qualification',
    'controls':RESULTS,'count':len(RESULTS),'go_data_matrix':{'rows':MATRIX[0],'accepted_rows':MATRIX[1]},
    'os_process_events':PROCESS_EVENTS},indent=2))
