"""Independent pure adversarial classifier-contract review; no author fixture imports."""
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
import os
import runpy
from pathlib import Path
from unittest.mock import patch
import hashlib
OLD_DIR=Path('/tmp/r5-third-run/docs/company-mail/evidence/R5-ADAPTER-DELTA-INDEPENDENT-20261004')
assert hashlib.sha256((OLD_DIR/'independent_replay.py').read_bytes()).hexdigest()=='d8a2151eba65efe1d6cfcd302ae36c6f7102526338a6893b6103eaa781f7d869'
with contextlib.redirect_stdout(io.StringIO()):n=runpy.run_path(str(OLD_DIR/'independent_replay.py'))
v=n['v'];c=n['c'];fresh=n['fresh'];publish=n['publish'];verify=n['verify'];SLOT=n['SLOTS'][0]
RESULTS=[]
def sync(r):
    p=sorted(copy.deepcopy(r['selected_local_packages']),key=n['wire'])
    r['production_coverage']['root_packages']=p
    r['production_coverage']['explicit_packages']=copy.deepcopy(p)
    r['package_records']=len(p)
def native(r):
    r['base_source']['files']['internal/z.c']='6'*64
    r['selected_local']['internal/z.c']=dict(sha256='6'*64,fields=['CFiles'])
    r['selected_local_packages'][0]['fields']['CFiles']=['z.c']
    r['native_inputs']=[dict(package='tabmail/internal',field='CFiles',path='internal/z.c',qualification='unknown')]
    sync(r)
def generated(r):
    cache=r['environment']['GOCACHE'];r['go_env']['GOCACHE']=cache
    r['selected_local_packages'].append(dict(directory='internal',import_path='tabmail/internal.test',for_test=None,module='tabmail',fields={'GoFiles':[cache+'/aa/unit-d']}))
    r['generated_testmain']=[dict(package='tabmail/internal.test',field='GoFiles',path='aa/unit-d',classification='generated_testmain',qualification='unknown')]
    sync(r)
def nonlocal_record(r,domain,field='GoFiles'):
    native_domain=field in v.v2.NATIVE_FIELDS
    suffix='c' if native_domain else 'go'
    if domain=='toolchain_source':
        r['go_env']['GOROOT']='/independent/toolchain'
        rec=dict(package='runtime',field=field,path='src/runtime/a.'+suffix,classification=domain,qualification='unknown')
        container='native_inputs' if native_domain else 'toolchain_source_inputs'
    else:
        r['go_env']['GOMODCACHE']=r['environment']['GOMODCACHE']
        r['root_mvs'].append(dict(Path='example.org/module',Version='v1.0.0'))
        r['root_mvs'].sort(key=lambda x:x['Path'])
        rec=dict(package='example.org/module/pkg',module='example.org/module',version='v1.0.0',field=field,path='example.org/module@v1.0.0/pkg/a.'+suffix,classification=domain,qualification='unknown')
        container='native_inputs' if native_domain else 'external_modulecache_inputs'
    r[container]=[rec];r['package_records']+=1
    return container
snapshot=dict(v.classify.__globals__);modules=set(sys.modules)
def probe(name,change,expect='reject'):
    r=fresh();change(r[SLOT]);b,raw=publish(r)
    try:verify(b,raw)
    except ValueError as e:actual='reject';detail=str(e)
    else:actual='accept';detail='verified'
    assert set(v.classify.__globals__)==set(snapshot)
    assert all(v.classify.__globals__[k] is value for k,value in snapshot.items())
    assert set(sys.modules)==modules
    RESULTS.append(dict(name=name,expected=expect,actual=actual,detail=detail))
# All file/process/import operations here are forbidden during verification.
with patch.object(v,'capture',side_effect=AssertionError('capture')),patch.object(v,'validate',side_effect=AssertionError('validate')),patch.object(v,'Retention',side_effect=AssertionError('retention')),patch('os.getenv',side_effect=AssertionError('env')),patch('builtins.open',side_effect=AssertionError('open')),patch.object(Path,'read_bytes',side_effect=AssertionError('read')),patch.object(Path,'read_text',side_effect=AssertionError('read')),patch('os.open',side_effect=AssertionError('os.open')),patch('os.stat',side_effect=AssertionError('stat')),patch('os.lstat',side_effect=AssertionError('lstat')),patch('os.scandir',side_effect=AssertionError('scandir')),patch('os.listdir',side_effect=AssertionError('listdir')),patch('os.readlink',side_effect=AssertionError('readlink')):
    probe('unchanged-positive-isolated',lambda r:None,'accept')
    probe('native-positive',native,'accept')
    for defect in ('missing','duplicate','package','field','path','qualification'):
        def change(r,d=defect):
            native(r)
            if d=='missing':r['native_inputs']=[]
            elif d=='duplicate':r['native_inputs']*=2
            else:r['native_inputs'][0][d]={'package':'foreign','field':'GoFiles','path':'internal/other.c','qualification':'attested'}[d]
        probe('local-native-'+defect,change)
    def two_occurrences(r):
        native(r)
        r['selected_local_packages'].append(dict(directory='internal',import_path='tabmail/internal [tabmail/internal.test]',for_test='tabmail/internal',module='tabmail',fields={'GoFiles':['a.go'],'CFiles':['z.c']}))
        r['native_inputs'].append(dict(package='tabmail/internal [tabmail/internal.test]',field='CFiles',path='internal/z.c',qualification='unknown'))
        sync(r)
    probe('native-two-package-occurrences-positive',two_occurrences,'accept')
    probe('native-two-package-occurrences-reordered',lambda r:(two_occurrences(r),r['native_inputs'].reverse()))
    probe('generated-cache-positive',generated,'accept')
    def borrowed_local(r):
        generated(r)
        r['base_source']['files']['internal/borrowed.txt']='7'*64
        r['selected_local']['internal/borrowed.txt']=dict(sha256='7'*64,fields=['GoFiles'])
        r['selected_local_packages'][1]['fields']['GoFiles']=[n['ROOT']+'/internal/borrowed.txt']
        r['generated_testmain']=[];sync(r)
    probe('generated-exception-borrowed-local-non-Go',borrowed_local)
    for defect in ('wrong-package','wrong-path','missing','duplicate','cache-env','relative-field','CgoFiles','wrong-suffix','count'):
        def change(r,d=defect):
            generated(r)
            if d=='wrong-package':r['generated_testmain'][0]['package']='foreign.test'
            elif d=='wrong-path':r['generated_testmain'][0]['path']='bb/other-d'
            elif d=='missing':r['generated_testmain']=[]
            elif d=='duplicate':r['generated_testmain']*=2
            elif d=='cache-env':r['go_env']['GOCACHE']='/different'
            elif d=='relative-field':r['selected_local_packages'][1]['fields']['GoFiles']=['aa/unit-d'];sync(r)
            elif d=='CgoFiles':r['selected_local_packages'][1]['fields']={'CgoFiles':[r['environment']['GOCACHE']+'/aa/unit-d']};r['generated_testmain'][0]['field']='CgoFiles';sync(r)
            elif d=='wrong-suffix':r['selected_local_packages'][1]['fields']['GoFiles']=[r['environment']['GOCACHE']+'/aa/unit.txt'];r['generated_testmain'][0]['path']='aa/unit.txt';sync(r)
            else:r['package_records']=1
        probe('generated-'+defect,change)
    for domain in ('external_modulecache','toolchain_source'):
        for field in ('GoFiles','CgoFiles','TestGoFiles','XTestGoFiles','CFiles'):
            probe(domain+'-'+field+'-positive',lambda r,d=domain,f=field:nonlocal_record(r,d,f),'accept')
        for defect in ('wrong-extension','local-package','count','path-escape','qualification','version-or-goroot'):
            def change(r,d=domain,bad=defect):
                container=nonlocal_record(r,d);rec=r[container][0]
                if bad=='wrong-extension':rec['path']=rec['path'][:-3]+'.txt'
                elif bad=='local-package':rec['package']='tabmail/internal'
                elif bad=='count':r['package_records']=1
                elif bad=='path-escape':rec['path']='../outside.go'
                elif bad=='qualification':rec['qualification']='captured'
                elif d=='external_modulecache':rec['version']='v2.0.0'
                else:r['go_env']['GOROOT']='relative'
            probe(domain+'-'+defect,change)
    # Same-bundle-pin remains fixed across all resealed classifier tampering.
    r=fresh();b,raw=publish(r);fixed=n['sha'](b)
    for mutation in (native,generated,lambda r:nonlocal_record(r,'toolchain_source')):
        r=fresh();mutation(r[SLOT]);b,raw=publish(r)
        try:verify(b,raw,trusted_bundle_byte_sha256=fixed)
        except ValueError as e:assert 'bundle byte pin mismatch' in str(e)
        else:raise AssertionError('counterfeit accepted')
# Mock context restored every global to its original identity.
assert all(v.classify.__globals__[k] is value for k,value in snapshot.items())
assert not EVENTS and not n['EVENTS']
print(json.dumps(dict(candidate='0c9adb17559e347ed99b3661d789b80ba59762aa',controls=RESULTS,unexpected=[x for x in RESULTS if x['expected']!=x['actual']],os_process_events=EVENTS,isolation='helper globals/module set unchanged; filesystem/env/capture/validation/retention mocks untouched',fixed_pin_controls=3),indent=2))
assert all(x['expected']==x['actual'] for x in RESULTS)
