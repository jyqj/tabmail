#!/usr/bin/env python3
"""Independent data-only review. No Go parsing/tool execution or runtime dispatch."""
import sys
EVENTS=[]
def audit(event,args):
    if event.startswith(('subprocess.','os.exec','os.spawn','os.posix_spawn')) or event in ('os.fork','os.forkpty','os.system','pty.spawn'):
        EVENTS.append(event)
        raise AssertionError('OS process forbidden: '+event)
sys.addaudithook(audit)
import ast
import contextlib
import hashlib
import itertools
import json
import os
from pathlib import Path
import tempfile
from types import ModuleType, SimpleNamespace
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'scripts/preparation'))
import r5_external_runtime as runtime
RESULTS=[]
def check(name,fn):
    fn(); RESULTS.append(name)
def rejects(fn):
    try: fn()
    except (ValueError,TypeError,KeyError,AttributeError,UnicodeError): return
    raise AssertionError('expected rejection')
def extract(path,names,ns):
    body=[n for n in ast.parse(path.read_text()).body if isinstance(n,ast.FunctionDef) and n.name in names]
    assert len(body)==len(names)
    exec(compile(ast.Module(body=body,type_ignores=[]),str(path),'exec'),ns)

def manifest_matrix():
    rows=0; accepts=0
    for selector,schema,policy in itertools.product((2,3),(None,True,False,'2','3',2.0,3.0,1,2,3,4),('r5_external_dependency_runtime_v2','r5_external_dependency_runtime_v3','unknown')):
        m={'schema_version':schema,'policy':policy,'status':'UNADOPTED'}
        # For v3, test only version boundary: a valid complete manifest is separately
        # mocked below. Missing v3 envelope must reject even with matching header.
        expected=selector==2 and type(schema) is int and schema==2 and policy=='r5_external_dependency_runtime_v2'
        if expected: assert runtime._manifest_version(m,selector)==2; accepts+=1
        else: rejects(lambda:runtime._manifest_version(m,selector))
        rows+=1
    return rows,accepts
MATRIX=manifest_matrix()
check('actual-runtime-header-matrix-66',lambda:None)
for selector in (None,True,False,2.0,3.0,'2','3',1,4,[],{}):
    check('actual-runtime-API-selector-reject-'+repr(selector),lambda s=selector:rejects(lambda:runtime._manifest_version({},s)))
for key in ('selected_binding_version','admitted_selection'):
    for value in (None,False,2,3,{},[]):
        check('v2-reject-field-presence-'+key+'-'+repr(value),lambda k=key,v=value:rejects(lambda:runtime._manifest_version({'schema_version':2,'policy':runtime.POLICY,'status':'UNADOPTED',k:v},2)))

def v3_boundary():
    fields=('schema_version policy status source dependency_root git_commit git source_tree installed installed_content_root receipt_paths receipt_hashes archive_policy package_hashes dependency_records node python cli helper_sha256 configs platform execution resolution concurrency_boundary task_complete product_green selected_binding_version admitted_selection').split()
    m=dict.fromkeys(fields)
    m.update(schema_version=3,policy=runtime.V3_POLICY,status='UNADOPTED',source={'path':'/synthetic/source'},dependency_root={'path':'/synthetic/deps'},git_commit='a'*40,task_complete=False,product_green=False,selected_binding_version=3,admitted_selection={'source_commit':'a'*40},receipt_paths={'archive':'/synthetic/a','default':'/synthetic/d','race':'/synthetic/r'},receipt_hashes={'archive':'a'*64,'default':'b'*64,'race':'c'*64})
    bundle={'observations':{'default':{'receipt_byte_sha256':'b'*64},'race-r5protocol':{'receipt_byte_sha256':'c'*64}}}
    with patch.object(runtime,'_selection',return_value=(bundle,None)) as selection:
        assert runtime._manifest_version(m,3)==3 and selection.call_count==1
        selection.reset_mock()
        for value in (None,True,False,'3',3.0,2,4):
            candidate=dict(m,selected_binding_version=value)
            rejects(lambda:runtime._manifest_version(candidate,3))
        assert selection.call_count==0
check('actual-v3-boundary-strict-selector-before-mocked-envelope',v3_boundary)

# Exercise real descriptor reading + strict JSON at independently pinned bytes.
# Minimal v2 objects here test load's header gate only, never full validate/observe.
def raw_load(raw,accepted=False):
    with tempfile.TemporaryDirectory() as directory:
        p=Path(directory)/'manifest.json';p.write_bytes(raw)
        pin=hashlib.sha256(raw).hexdigest()
        if accepted: assert runtime.load_pinned(p,pin)==json.loads(raw)
        else: rejects(lambda:runtime.load_pinned(p,pin))
        rejects(lambda:runtime.load_pinned(p,'0'*64))
base=b'{"schema_version":2,"policy":"r5_external_dependency_runtime_v2","status":"UNADOPTED"}'
check('real-pinned-load-canonical-v2-and-bad-pin',lambda:raw_load(base,True))
check('real-pinned-load-whitespace-integer-v2',lambda:raw_load(base.replace(b':2',b': 2 '),True))
for token in (b'true',b'false',b'"2"',b'2.0',b'2e0',b'null',b'NaN',b'Infinity',b'1',b'3',b'[]',b'{}'):
    check('real-load-schema-token-'+token.decode(),lambda t=token:raw_load(base.replace(b':2',b':'+t)))
for raw in (b'{',b'[]',b'null',b'\xff',base+b' {}',base.replace(b'"status":"UNADOPTED"',b'"status":false'),base.replace(b'"schema_version":2',b'"schema_version":3,"schema_version":2'),base.replace(b'"schema_version":2',b'"schema_version":2,"schema_version":2'),base[:-1]+b',"source":{"path":"/x","path":"/x"}}',base[:-1]+b',"selected_binding_version":null}',base.replace(b'"schema_version":2',b'"Schema_Version":2')):
    check('malformed-duplicate-case-or-presence-'+hashlib.sha256(raw).hexdigest()[:12],lambda r=raw:raw_load(r))

for selector in (None,'2','3','','03','+3','3.0','3e0',' 3','3 ','true','null','1','4'):
    def environment(s=selector):
        with patch.dict(os.environ,{},clear=True):
            if s is not None:os.environ['TABMAIL_R5_SELECTED_BINDING_VERSION']=s
            if s in (None,'2','3'): assert runtime.environment_version()==(3 if s=='3' else 2)
            else: rejects(runtime.environment_version)
    check('actual-env-selector-'+repr(selector),environment)

def explicit_beats_contamination():
    with patch.dict(os.environ,{'TABMAIL_R5_SELECTED_BINDING_VERSION':'garbage','TABMAIL_R5_EXTERNAL_MANIFEST':'/synthetic/manifest','TABMAIL_R5_EXTERNAL_MANIFEST_SHA256':'a'*64},clear=True):
        for v in (2,3):
            with patch.object(runtime,'load_pinned',return_value={'source':{'path':str(ROOT)}}) as load:
                runtime.from_environment(ROOT,selected_binding_version=v)
                assert load.call_args.kwargs=={'selected_binding_version':v}
        rejects(lambda:runtime.from_environment(ROOT))
check('actual-from-environment-explicit-beats-contamination',explicit_beats_contamination)

def caller(selector,drift=False,layer='components',go_failure=False):
    calls=[];child_envs=[];manifest={'node':{'path':'/synthetic/node'},'cli':{'path':'/synthetic/cli'}}
    fake=ModuleType('r5_external_runtime');fake.environment_version=runtime.environment_version
    def load(root,*,selected_binding_version):
        calls.append(('load',selected_binding_version))
        if drift:os.environ['TABMAIL_R5_SELECTED_BINDING_VERSION']='invalid-after-resolution'
        return manifest
    def validate(m,*,selected_binding_version):assert m is manifest;calls.append(('validate',selected_binding_version))
    def launch(m,argv,env,*,selected_binding_version):
        calls.append(('launch',selected_binding_version));child_envs.append(dict(env))
        assert argv==['/synthetic/node','/synthetic/cli','run','--cache=false','--experimental.fsModuleCache=false','components/company/r5-protocol.test.tsx','--reporter=json','--outputFile='+str((out/'vitest.json').resolve())]
        (out/'vitest.json').write_text('{}')
        return SimpleNamespace(returncode=0,stdout='',stderr='')
    fake.from_environment=load;fake.validate=validate;fake.launch=launch
    def run(argv,**kw):
        child_envs.append(dict(kw['env']))
        assert argv==['go','test','-json','-race','-count=1','-timeout=120s','-tags=r5protocol','-run','^(SyntheticOnly)$','./synthetic']
        assert kw['timeout']==180
        return SimpleNamespace(returncode=1 if go_failure else 0,stdout='',stderr='')
    ns=dict(ROOT=ROOT,sys=sys,os=os,json=json,hashlib=hashlib,subprocess=SimpleNamespace(run=run),source_closure=lambda:'same',protocol_source_metadata=lambda:{'source_sha':'synthetic'},SOURCE_IDENTITY={'build_context':{'build_tag_sets':[['r5protocol']],'goos':'linux','goarch':'amd64','cgo_enabled':1}},source_inventory=runtime.source_inventory, re=__import__('re'),classify_shared_events=lambda *a:{'errors':['synthetic'] if go_failure else []},classify_go_component_packets=lambda *a:([],set()),classify_components=lambda *a:{'errors':[]},summary=lambda d:{})
    extract(ROOT/'scripts/check_r5_protocol.py',{'run_shared','external_component_command','current_protocol_environment','shared_command'},ns)
    data={'cases':[{'id':'SYNTHETIC','required_layers':[],'shared_adapters':[{'package':'./synthetic','build_tag':'r5protocol','test':'SyntheticOnly','layers':[layer]},{'runner':'vitest','layers':['components']}]}]}
    with tempfile.TemporaryDirectory() as d,patch.dict(os.environ,{},clear=True),patch.dict(sys.modules,{'r5_external_runtime':fake}):
        if selector is not None:os.environ['TABMAIL_R5_SELECTED_BINDING_VERSION']=selector
        ns['CASES']=Path(d)/'cases';ns['CASES'].write_text('{}');out=Path(d)/'out'
        before=list(sys.path)
        try:
            if layer=='components' and selector not in (None,'2','3'):rejects(lambda:ns['run_shared'](data,layer,out))
            else:ns['run_shared'](data,layer,out)
        finally:sys.path[:]=before
    if layer!='components':assert not calls
    elif selector not in (None,'2','3'):assert not calls and not child_envs
    else:
        v=3 if selector=='3' else 2
        assert calls==[(n,v) for n in (['load','validate','validate'] if go_failure else ['load','validate','launch','validate'])]
    for env in child_envs:
        # Actual bound_environment snapshots prior to runtime admission. Drift in
        # this harness occurs later, and does not contaminate either child env.
        assert env.get('TABMAIL_R5_SELECTED_BINDING_VERSION')==selector
        assert env['GOENV']=='off' and env['GOWORK']=='off'
for selector in (None,'2','3'):
    for drift,failed in ((False,False),(True,False),(False,True)):
        check('actual-caller-bound-env-'+repr((selector,drift,failed)),lambda s=selector,d=drift,f=failed:caller(s,d,go_failure=f))
for selector in ('','garbage','03','3.0'):
    check('invalid-caller-no-dispatch-'+repr(selector),lambda s=selector:caller(s))
check('noncomponent-selector-isolation',lambda:caller('garbage',layer='unit'))
assert EVENTS==[]
print(json.dumps({'controls':RESULTS,'count':len(RESULTS),'actual_runtime_header_matrix':{'rows':MATRIX[0],'accepted_minimal_v2_rows':MATRIX[1]},'os_process_events':EVENTS,'limits':['Go is read as text, never parsed/compiled/executed','Runtime load tests validate header and byte-pin only; full v3 selection is mocked','Caller process/runtime boundaries mocked; no physical or lifecycle qualification']},indent=2))
