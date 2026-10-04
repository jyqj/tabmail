import copy, hashlib, json, os, pathlib, subprocess, sys, time
from unittest import mock
root=pathlib.Path('/workspace/tabmail-v3-reviewed-experiment');private=pathlib.Path('/tmp/r5-selected-v3-reviewed-20261004')
sys.path.insert(0,str(root/'scripts'))
import r5_selected_source_binding_v3 as b
os.environ['R5_DIAGNOSTIC_ROOT']=str(private/'raw')
go=pathlib.Path('/workspace/tabmail-cloud/tools/go/bin/go');cache=private/'cache';mod=private/'gomod'
summary={'frozen_sha':subprocess.check_output(['git','rev-parse','HEAD'],cwd=root,text=True).strip(),'root':str(root),'stages':[],'diffs':{},'rejections':[]}
actual=b.subprocess.run;captured={}; current='';raws={};events=[]
def observe(argv,**kw):
 start=time.monotonic();result=actual(argv,**kw)
 if argv[0]==str(go):
  events.append({'stage':current,'argv':argv,'exit':result.returncode,'stdout_bytes':len(result.stdout),'stderr_bytes':len(result.stderr),'raw_stdout_sha256':b._sha(result.stdout),'stderr_sha256':b._sha(result.stderr),'seconds':round(time.monotonic()-start,3)})
  if len(argv)>1 and argv[1] in ('env','list'):raws.setdefault(current,[]).append((argv,result.stdout))
 return result

def save(name,value):
 path=private/name;path.write_text(json.dumps(value,indent=2,sort_keys=True)+'\n');path.chmod(0o600)

def diff(a,z,path=''):
 if type(a)!=type(z):return [{'path':path,'before':a,'after':z}]
 if type(a) is dict:
  out=[]
  for key in sorted(set(a)|set(z)):
   p=path+'/'+key
   if key not in a or key not in z:out.append({'path':p,'before_present':key in a,'after_present':key in z,'before':a.get(key),'after':z.get(key)})
   else:out+=diff(a[key],z[key],p)
  return out
 if type(a) is list:
  if len(a)!=len(z):return [{'path':path,'before':a,'after':z}]
  return sum((diff(x,y,path+'/'+str(i)) for i,(x,y) in enumerate(zip(a,z))),[])
 return [] if a==z else [{'path':path,'before':a,'after':z}]

try:
 with mock.patch.object(b.subprocess,'run',side_effect=observe):
  for name,ctx in [('default',b.DEFAULT_CONTEXT),('race-r5protocol',b.CONTEXT)]:
   current=name+'-before';start=time.monotonic()
   receipt=b.capture(root,go,cache=cache,modulecache=mod,context=ctx)
   captured[name]=receipt;save(current+'.receipt.json',receipt)
   # Independent trusted local capture channel: store pin separately before any compile.
   (private/(name+'.trusted-pin')).write_text(b.observation_digest(receipt)+'\n');(private/(name+'.trusted-pin')).chmod(0o600)
   summary['stages'].append({'stage':current,'binding_sha256':receipt['binding_sha256'],'observation_sha256':receipt['observation_sha256'],'commands':len(receipt['observation_envelope']),'package_records':receipt['package_records'],'seconds':round(time.monotonic()-start,3)})
   print(current+' captured',flush=True)
  env=dict(PATH=os.environ.get('PATH','/usr/bin:/bin'),HOME='/tmp',GODEBUG='asynctimerchan=0',GOWORK='off',GOENV='off',GOTOOLCHAIN='local',GOFLAGS='',GOOS='linux',GOARCH='amd64',CGO_ENABLED='1',GOPROXY='off',GOSUMDB='sum.golang.org',GOPATH=str(mod.parent),GOMODCACHE=str(mod),GOCACHE=str(cache))
  for name,flags in [('default',[]),('race-r5protocol',['-race','-tags=r5protocol'])]:
   current=name+'-compile';out=private/(name+'-binaries');out.mkdir(mode=0o700)
   result=observe([str(go),'test','-c','-mod=readonly','-o',str(out)+'/',*flags,'./internal/api/handlers','./internal/architecture'],cwd=root,env=env,capture_output=True,timeout=180)
   (private/(current+'.stdout')).write_bytes(result.stdout);(private/(current+'.stderr')).write_bytes(result.stderr)
   if result.returncode:raise ValueError('compile failed')
   for p in out.iterdir():p.chmod(0o600)
   summary['stages'].append({'stage':current,'exit':result.returncode,'test_binaries_executed':0});print(current+' passed',flush=True)
  for name in captured:
   current=name+'-after';start=time.monotonic();pin=(private/(name+'.trusted-pin')).read_text().strip()
   observed=b.validate(captured[name],root,go,cache=cache,modulecache=mod,trusted_observation_sha256=pin)
   save(current+'.receipt.json',observed)
   summary['stages'].append({'stage':current,'binding_sha256':observed['binding_sha256'],'observation_sha256':observed['observation_sha256'],'strict_postvalidation':True,'seconds':round(time.monotonic()-start,3)})
   full=[]
   for i,((argv,a),(argv2,z)) in enumerate(zip(raws[name+'-before'],raws[current])):
    assert argv==argv2
    if argv[1]=='list' and '-m' not in argv:
     differences=diff(b.stream(a),b.stream(z))
     for d in differences:d['command_index']=i
     full+=differences
     if b.project_package_stdout(a)!=b.project_package_stdout(z):raise ValueError('outside allowlist package diff')
    elif '-m' in argv:
     if a!=z:raise ValueError('MVS raw changed')
    else:
     ae=b.inventory.strict_json(a);ze=b.inventory.strict_json(z)
     for e in (ae,ze):e['GOGCCFLAGS']=b.re.sub(r'/tmp/go-build[0-9]+=', '/tmp/go-build<TEMP>=',e['GOGCCFLAGS'])
     if ae!=ze:raise ValueError('env outside normalization changed')
   save(name+'.full-raw-json-diffs.json',full)
   from collections import Counter
   summary['diffs'][name]={'full_diff_private_file':str(private/(name+'.full-raw-json-diffs.json')),'locations':len(full),'fields':dict(Counter(d['path'].split('/')[-1] for d in full)),'outside_allowlist':0,'binding_equal':observed['binding_sha256']==captured[name]['binding_sha256'],'observations_equal':observed['observation_sha256']==captured[name]['observation_sha256']}
   print(current+' strictly validated',flush=True)
 summary['result']='bounded_compile_only_v3_postvalidation_passed'
except Exception as e:
 summary['result']='rejected';summary['rejections'].append({'stage':current,'type':type(e).__name__,'message':str(e)});print(summary['rejections'][-1],flush=True)
finally:
 save('summary.json',summary);save('command-summary.json',events)
 facts=[]
 for p in sorted(private.rglob('*')):
  if p.is_file() and '/gomod/' not in str(p) and '/cache/' not in str(p):
   raw=p.read_bytes();p.chmod(0o600);facts.append({'path':str(p.relative_to(private)),'bytes':len(raw),'sha256':hashlib.sha256(raw).hexdigest()})
 save('private-evidence-hashes.json',facts)
