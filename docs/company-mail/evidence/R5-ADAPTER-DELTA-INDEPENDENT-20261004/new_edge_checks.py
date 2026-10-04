"""Pure independent delta probes; no producer or author fixtures."""
import sys
EVENTS=[]
def guard(event,args):
    if event in ('subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp') or event.startswith(('os.exec','os.spawn')):
        EVENTS.append(event);raise AssertionError(event)
sys.addaudithook(guard)
import contextlib
import copy
import io
import json
import runpy
from pathlib import Path
from unittest.mock import patch
with contextlib.redirect_stdout(io.StringIO()):
    n=runpy.run_path(str(Path(__file__).with_name('independent_replay.py')))
v=n['v'];c=n['c'];fresh=n['fresh'];publish=n['publish'];verify=n['verify'];slot=n['SLOTS'][0]
RESULTS=[]
def probe(name,change,expected='reject'):
    r=fresh();change(r[slot]);b,raw=publish(r)
    try:verify(b,raw)
    except (ValueError,TypeError) as e: actual='reject';detail=str(e)
    else:actual='accept';detail='verified receipts returned'
    RESULTS.append(dict(name=name,expected=expected,actual=actual,detail=detail))
def sync_packages(r):
    packages=[p for p in r['selected_local_packages'] if p['module']=='tabmail']
    r['production_coverage']['root_packages']=sorted(copy.deepcopy(packages),key=n['wire'])
    r['production_coverage']['explicit_packages']=copy.deepcopy(r['production_coverage']['root_packages'])
def false_go(r):
    r['base_source']['files']['internal/not-go.txt']='a'*64
    r['selected_local']={'internal/not-go.txt':dict(sha256='a'*64,fields=['GoFiles'])}
    r['selected_local_packages'][0]['fields']['GoFiles']=['not-go.txt'];sync_packages(r)
def add_native(r):
    r['base_source']['files']['internal/native.c']='b'*64
    r['selected_local']['internal/native.c']=dict(sha256='b'*64,fields=['CFiles'])
    r['selected_local_packages'][0]['fields']['CFiles']=['native.c'];sync_packages(r)
def foreign_native(r):
    add_native(r)
    r['native_inputs']=[dict(package='foreign/unselected',field='CFiles',path='internal/native.c',qualification='unknown')]
def orphan_generated(r):
    r['generated_testmain']=[dict(package='foreign/unselected.test',field='GoFiles',path='ab/orphan-d',qualification='unknown',classification='generated_testmain')]
def non_go_toolchain(r):
    r['toolchain_source_inputs']=[dict(package='runtime',field='GoFiles',path='src/runtime/not-go.txt',qualification='unknown',classification='toolchain_source')]
with patch.object(v,'capture',side_effect=AssertionError('capture')),patch.object(v,'validate',side_effect=AssertionError('validate')),patch.object(v,'Retention',side_effect=AssertionError('retention')),patch('os.getenv',side_effect=AssertionError('env')),patch('builtins.open',side_effect=AssertionError('candidate read')),patch.object(Path,'read_bytes',side_effect=AssertionError('candidate read')):
    probe('non-go-local-GoFiles',false_go)
    probe('missing-local-native-classification',add_native)
    probe('foreign-package-native-classification',foreign_native)
    probe('orphan-generated-testmain',orphan_generated)
    probe('non-go-toolchain-GoFiles',non_go_toolchain)
    for path in ('../outside.go','/outside.go','internal/../outside.go'):
        probe('selected-path-'+path,lambda r,p=path:r['selected_local'].update({p:dict(sha256='a'*64,fields=['GoFiles'])}))
    for field,bad in (('sha256',True),('fields',['not-a-field'])):
        probe('selected-input-type-'+field,lambda r,f=field,b=bad:r['selected_local']['internal/a.go'].update({f:b}))
    probe('extra-qualification-field',lambda r:r['qualification'].update(extra='unknown'))
    probe('missing-qualification-field',lambda r:r['qualification'].pop('generated'))
    probe('main-MVS-boolean-type',lambda r:next(x for x in r['root_mvs'] if x['Path']=='tabmail').update(Main=1))
    probe('third-MVS-replacement',lambda r:r['root_mvs'].append(dict(Path='foreign',Version='v1.0.0',Replace={'module':'foreign','version':'v1.0.0','path':'foreign'})))
    probe('variant-extra-field',lambda r:r['production_coverage']['variant_directory_records'][0].update(extra=True))
    probe('variant-context-bool-type',lambda r:r['production_coverage']['variant_directory_records'][0].update(selected_context=1))
    probe('variant-missing-input',lambda r:r['production_coverage']['variant_directory_records'][0].update(fields={}))
    probe('resolution-boolean-type',lambda r:r['base_source']['module_resolution'].update(effective_root_MVS_observed=0))
    # Independently compare the two false-Go cases to the unchanged pure classifier.
    helper_results=[]
    for path,standard in [('/independent/controller/source/internal/not-go.txt',False),('/pinned/goroot/src/runtime/not-go.txt',True)]:
        row=dict(Dir=str(Path(path).parent),ImportPath='runtime' if standard else 'tabmail/internal',GoFiles=[Path(path).name],Standard=standard,Module=dict(Path='tabmail',Main=True,Dir=n['ROOT']))
        with patch.object(v.v2,'base_markers',return_value=[]):
            try:v.classify([row],Path(n['ROOT']),{'internal/not-go.txt'},dict(GOROOT='/pinned/goroot'),[])
            except ValueError as e:helper_results.append(dict(path=path,actual='reject',detail=str(e)))
            else:raise AssertionError('unchanged classifier accepted false Go')
    row=dict(Dir=n['ROOT']+'/internal',ImportPath='tabmail/internal',GoFiles=['a.go'],CFiles=['native.c'],Module=dict(Path='tabmail',Main=True,Dir=n['ROOT']))
    with patch.object(v.v2,'base_markers',return_value=[]):
        classified=v.classify([row],Path(n['ROOT']),{'internal/a.go','internal/native.c'},{},[])
    assert classified[3]==[dict(package='tabmail/internal',field='CFiles',path='internal/native.c',qualification='unknown')]
    helper_results.append(dict(name='local-native-classification-required',actual=classified[3]))
assert not EVENTS and not n['EVENTS']
print(json.dumps(dict(candidate='116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50',controls=RESULTS,helper_parity=helper_results,unexpected_acceptances=[x['name'] for x in RESULTS if x['expected']!=x['actual']],os_process_events=EVENTS),indent=2))
