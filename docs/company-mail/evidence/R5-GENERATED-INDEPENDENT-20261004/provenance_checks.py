"""Independent generated-provenance cross-field controls; pure and non-discovered."""
import sys
EVENTS=[]
def guard(event,args):
    if event in ('subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp') or event.startswith(('os.exec','os.spawn')):
        EVENTS.append(event);raise AssertionError(event)
sys.addaudithook(guard)
import contextlib
import copy
import hashlib
import io
import json
import runpy
from pathlib import Path
from unittest.mock import patch
p=Path('/tmp/r5-generated-adversarial.py')
assert hashlib.sha256(p.read_bytes()).hexdigest()=='165355274becac87f3e741aa1e26e1f61d911c73462a5807643cfc08e4c34db8'
with contextlib.redirect_stdout(io.StringIO()): a=runpy.run_path(str(p))
n=a['n'];v=a['v'];c=a['c'];fresh=a['fresh'];publish=a['publish'];verify=a['verify'];generated=a['generated'];sync=a['sync'];slot=a['SLOT'];root=n['ROOT']
RESULTS=[]
globals_before=dict(v.classify.__globals__);module_set=set(sys.modules)
def probe(name,change,expected='reject'):
    r=fresh();change(r[slot]);b,raw=publish(r)
    try:verify(b,raw)
    except ValueError as e: actual='reject';detail=str(e)
    else:actual='accept';detail='verified'
    assert set(v.classify.__globals__)==set(globals_before)
    assert all(v.classify.__globals__[k] is value for k,value in globals_before.items())
    assert set(sys.modules)==module_set
    RESULTS.append(dict(name=name,expected=expected,actual=actual,detail=detail))
def owned_input(r,field,suffix,absolute=True,cache_below_source=False):
    generated(r)
    r['generated_testmain']=[]
    if cache_below_source:
        cache=root+'/internal/cache';r['environment']['GOCACHE']=cache;r['go_env']['GOCACHE']=cache
        name='cache/owned'+suffix
    else:name='owned'+suffix
    rel='internal/'+name
    r['base_source']['files'][rel]='9'*64
    r['selected_local'][rel]=dict(sha256='9'*64,fields=[field])
    r['selected_local_packages'][1]['fields']={field:[root+'/'+rel if absolute else name]}
    if cache_below_source:
        r['generated_testmain']=[dict(package='tabmail/internal.test',field='GoFiles',path='owned'+suffix,classification='generated_testmain',qualification='unknown')]
    sync(r)
def cache_case(r,cache):
    r['environment']['GOCACHE']=cache;generated(r)
def mixed_good(r):
    generated(r);r['selected_local_packages'][1]['fields']['GoFiles'].append(root+'/internal/a.go');sync(r)
def fork_good(r):
    generated(r);replacement=v.inventory.CURRENT_REPLACEMENTS[0]
    pkg=r['selected_local_packages'][1];pkg['directory']=replacement['path'];pkg['module']=replacement['module'];pkg['import_path']=replacement['module']+'.test'
    r['generated_testmain'][0]['package']=pkg['import_path'];sync(r)
    r['production_coverage']['root_packages']=[copy.deepcopy(r['selected_local_packages'][0])]
    r['production_coverage']['explicit_packages']=copy.deepcopy(r['production_coverage']['root_packages'])
with patch.object(v,'capture',side_effect=AssertionError('capture')),patch.object(v,'validate',side_effect=AssertionError('validate')),patch.object(v,'Retention',side_effect=AssertionError('retention')),patch('os.getenv',side_effect=AssertionError('env')),patch('builtins.open',side_effect=AssertionError('open')),patch.object(Path,'read_bytes',side_effect=AssertionError('read')),patch.object(Path,'read_text',side_effect=AssertionError('read')),patch('os.open',side_effect=AssertionError('os.open')),patch('os.stat',side_effect=AssertionError('stat')),patch('os.lstat',side_effect=AssertionError('lstat')),patch('os.scandir',side_effect=AssertionError('scandir')),patch('os.listdir',side_effect=AssertionError('listdir')),patch('os.readlink',side_effect=AssertionError('readlink')):
    probe('ordinary-positive',lambda r:None,'accept')
    probe('generated-positive',generated,'accept')
    probe('mixed-legitimate-source-and-generated-positive',mixed_good,'accept')
    probe('fork-generated-positive',fork_good,'accept')
    for field in v.FIELDS[:4]:
        for suffix in ('.txt','-d'):
            for absolute in (True,False):
                probe('source-owned-%s-%s-%s'%(field,suffix,absolute),lambda r,f=field,s=suffix,abs=absolute:owned_input(r,f,s,abs))
        def ordinary_go_test(r,f=field):
            generated(r);r['generated_testmain']=[]
            r['selected_local_packages'][1]['fields']={f:[root+'/internal/a.go']}
            r['selected_local']['internal/a.go']['fields']=sorted(set(['GoFiles',f]));sync(r)
        probe('ordinary-source-go-in-test-package-'+field,ordinary_go_test,'accept')
    probe('cache-beneath-source-owned-minus-d',lambda r:owned_input(r,'GoFiles','-d',True,True))
    probe('cache-is-source-root',lambda r:cache_case(r,root))
    probe('cache-ancestor-input-outside-source-positive',lambda r:cache_case(r,str(Path(root).parent)),'accept')
    for defect in ('path','package','field','category','qualification','missing','duplicate'):
        def change(r,d=defect):
            generated(r);rec=r['generated_testmain'][0]
            if d=='missing':r['generated_testmain']=[]
            elif d=='duplicate':r['generated_testmain']*=2
            else:rec[dict(category='classification').get(d,d)]={'path':'aa/other-d','package':'other.test','field':'CgoFiles','category':'external_modulecache','qualification':'captured'}[d]
        probe('generated-record-'+defect,change)
    for defect in ('cache-prefix-lookalike','source-prefix-lookalike','cache-equals-file','outside-cache','parent-component','wrong-suffix','go-extension','package-suffix','relative'):
        def change(r,d=defect):
            generated(r);cache=r['environment']['GOCACHE'];pkg=r['selected_local_packages'][1];rec=r['generated_testmain'][0]
            if d=='source-prefix-lookalike':
                r['environment']['GOCACHE']=root+'-peer/cache';r['go_env']['GOCACHE']=r['environment']['GOCACHE'];pkg['fields']['GoFiles']=[r['environment']['GOCACHE']+'/aa/unit-d']
            elif d=='cache-prefix-lookalike':pkg['fields']['GoFiles']=[cache+'-peer/aa/unit-d']
            elif d=='cache-equals-file':r['environment']['GOCACHE']=cache+'/unit-d';r['go_env']['GOCACHE']=cache+'/unit-d';pkg['fields']['GoFiles']=[cache+'/unit-d'];rec['path']='.'
            elif d=='outside-cache':pkg['fields']['GoFiles']=['/unrelated/aa/unit-d']
            elif d=='parent-component':pkg['fields']['GoFiles']=[cache+'/aa/../unit-d'];rec['path']='aa/../unit-d'
            elif d=='wrong-suffix':pkg['fields']['GoFiles']=[cache+'/aa/unit.txt'];rec['path']='aa/unit.txt'
            elif d=='go-extension':pkg['fields']['GoFiles']=[cache+'/aa/unit.go'];rec['path']='aa/unit.go'
            elif d=='package-suffix':pkg['import_path']='tabmail/internal.unit';rec['package']=pkg['import_path']
            else:pkg['fields']['GoFiles']=['aa/unit-d']
            sync(r)
        probe('generated-path-'+defect,change,'accept' if defect=='source-prefix-lookalike' else 'reject')
    for cache in ('/independent/new-cache','/independent/cache/build/subcache'):
        probe('distinct-valid-cache-'+cache,lambda r,p=cache:cache_case(r,p),'accept')
    for defect in ('observed-cache-missing','observed-cache-mismatch','extra-orphan','mixed-invalid-source'):
        def change(r,d=defect):
            generated(r)
            if d=='observed-cache-missing':del r['go_env']['GOCACHE']
            elif d=='observed-cache-mismatch':r['go_env']['GOCACHE']='/other'
            elif d=='extra-orphan':r['generated_testmain'].append(dict(r['generated_testmain'][0],package='other.test'))
            else:
                r['base_source']['files']['internal/hidden.txt']='8'*64
                r['selected_local']['internal/hidden.txt']=dict(sha256='8'*64,fields=['GoFiles'])
                r['selected_local_packages'][1]['fields']['GoFiles'].append(root+'/internal/hidden.txt');sync(r)
        probe(defect,change)
    # All new replacement bundles are tested with the original independently held pin.
    r=fresh();b,raw=publish(r);pin=n['sha'](b)
    for mutation in (generated,mixed_good,fork_good):
        r=fresh();mutation(r[slot]);b,raw=publish(r)
        try:verify(b,raw,trusted_bundle_byte_sha256=pin)
        except ValueError as e:assert 'bundle byte pin mismatch' in str(e)
        else:raise AssertionError('changed pin accepted')
assert not EVENTS and not a['EVENTS'] and not n['EVENTS']
assert all(v.classify.__globals__[k] is value for k,value in globals_before.items())
print(json.dumps(dict(candidate='632d55de566254c618bca9a54c5dbae7b355fcf9',controls=RESULTS,unexpected=[x for x in RESULTS if x['expected']!=x['actual']],os_process_events=EVENTS,fixed_pin_controls=3,helper_global_and_module_identity='unchanged'),indent=2))
assert all(x['expected']==x['actual'] for x in RESULTS)
