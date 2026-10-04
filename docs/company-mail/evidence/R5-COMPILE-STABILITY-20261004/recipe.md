# Bounded compile-only experiment recipe

This is a diagnostic proposal in evidence documentation. It changes no preparation/runner/tool/test source. The executed attempt stopped during the race-r5protocol binding; the repeat/postvalidation block below was not reached. Do not rerun in the failed checkout or describe it as stabilized.

Start from a new actual Git clone (HTTPS clone or --no-hardlinks for a local source), detach at df6ffe046611f5b2121f4d48d5114a2ececd2c24, and verify `git status --porcelain` is empty. Use a new owned mode700 checkout and private root outside any checkout, with an empty private `cache`, mode700 `raw`, and `gopath` directories. Copy an owned prehydrated module cache into `gopath/pkgmod` (the executed attempt copied `/workspace/tabmail-cloud/gomod` with `cp -a`, without sourcing its service environment). Do not reuse a shared build cache. Use the official pinned Go1.25.7 executable; compare its SHA256 to control-summary.json. Preserve the exact root and both fork module locks and web package lock; capture enforces exact two-fork identity and locked source inventories. No TypeScript preparation is needed for this experiment.

Set R5_EXPERIMENT_PRIVATE, R5_EXPERIMENT_CHECKOUT and R5_EXPERIMENT_GO to those absolute owned paths. Save the following Python block as `experiment.py` inside the private root, mode600. Run `PYTHONDONTWRITEBYTECODE=1 python "$R5_EXPERIMENT_PRIVATE/experiment.py" > "$R5_EXPERIMENT_PRIVATE/experiment.stdout" 2> "$R5_EXPERIMENT_PRIVATE/experiment.stderr"`. The script creates compiler outputs outside the checkout, captures raw stdout/stderr and hashes, compares JSON fields without changing the raw receipt hash domain, and stops on any nonzero exit or capture/validation rejection. The metadata observations before prewarming are not binding receipts. Leave every failed attempt and raw operation private.

The selections cover the root production build; default typed-wire handlers and compatibility architecture compiler contexts; CI untagged race root tests (also covering untagged protocol adapters); race r5protocol handlers/postgres; and race r5audit postgres. Root Go-list observations explicitly include both fork module package trees. Fork production dependencies are compiled through the root graph; their own test binaries are not part of this recipe. Runtime-only JSON/count/run/timeout arguments are absent from compile-only transforms; no race/tag/ABI semantic flags are added beyond the declared contexts. Benchmark/r5benchmark, nonrace root tests, mutation-created clones and fork test execution remain outside coverage. The compile output directory differs per phase, but each repeated selection's action, flags and packages is identical.

The two formal binding contexts are the only contexts accepted by the existing binding API. Untagged race and race r5audit receive raw metadata observations only. A complete recipe attempt would require both captures, repetition of all five compiles, raw equality in all four contexts and both unmodified strict validators. Any early rejection stops the attempt; no extra warmup, normalization, source edits or receipt waiver follows.

```python
import os,sys,json,hashlib,subprocess,time,stat
from pathlib import Path
P=Path(os.environ['R5_EXPERIMENT_PRIVATE']); R=Path(os.environ['R5_EXPERIMENT_CHECKOUT']); G=Path(os.environ['R5_EXPERIMENT_GO']); C=P/'cache'; M=P/'gopath/pkgmod'
os.umask(0o077); sys.path.insert(0,str(R/'scripts')); os.environ['R5_DIAGNOSTIC_ROOT']=str(P/'raw'); os.environ['PYTHONDONTWRITEBYTECODE']='1'
import r5_selected_source_binding_v2 as b, r5_source_inventory as inv
H=lambda v:hashlib.sha256(v).hexdigest()
def save(n,v):
 data=v if isinstance(v,bytes) else json.dumps(v,indent=2,sort_keys=True).encode()+b'\n'
 (P/n).write_bytes(data)
def clean():
 assert not subprocess.check_output(['git','-C',str(R),'status','--porcelain','--untracked-files=no'])
 assert subprocess.check_output(['git','-C',str(R),'rev-parse','HEAD']).decode().strip()=='df6ffe046611f5b2121f4d48d5114a2ececd2c24'
clean()
env=dict(PATH=str(G.parent)+':/usr/bin:/bin',HOME=os.environ['HOME'],GODEBUG='asynctimerchan=0',GOWORK='off',GOENV='off',GOTOOLCHAIN='local',GOFLAGS='',GOOS='linux',GOARCH='amd64',CGO_ENABLED='1',GOPROXY='https://proxy.golang.org',GOSUMDB='sum.golang.org',GOPATH=str(M.parent),GOMODCACHE=str(M),GOCACHE=str(C))
env.update({k:os.environ[k] for k in ('HTTPS_PROXY','HTTP_PROXY','ALL_PROXY','NO_PROXY') if k in os.environ})
commands=[]; diffs=[]
def run(args,label,timeout=600):
 start=time.monotonic(); q=subprocess.run([str(G),*args],cwd=R,env=env,capture_output=True,timeout=timeout)
 save(label+'.stdout',q.stdout);save(label+'.stderr',q.stderr)
 rec=dict(label=label,argv=[str(G),*args],exit=q.returncode,seconds=round(time.monotonic()-start,3),stdout_sha256=H(q.stdout),stderr_sha256=H(q.stderr),execution='compiler_or_metadata_only')
 commands.append(rec);save('command-receipts.json',commands)
 if q.returncode:raise RuntimeError(label+' exit '+str(q.returncode))
 return q.stdout
patterns=['./...','github.com/jhillyerd/enmime/v2/...','github.com/emersion/go-smtp/...']
ctxs={'default':b.DEFAULT_CONTEXT,'race-r5protocol':b.CONTEXT,'race':dict(b.DEFAULT_CONTEXT,race=True),'race-r5audit':dict(b.CONTEXT,build_tag_sets=[['r5audit']])}
static=inv.capture_current_source(R,purpose='selected',policy=inv.ARCHIVE_POLICY,build_context=b.DEFAULT_CONTEXT)['archive_boundary']
metadata={}
for name,ctx in ctxs.items():
 metadata[name]=[b.selection_argv(ctx,patterns),b.selection_argv(ctx,['./...']),b.selection_argv(ctx,['./cmd/...','./internal/...']),b.variant_argv(ctx,static),b.MVS_ARGV]
save('plan.json',dict(source='df6ffe046611f5b2121f4d48d5114a2ececd2c24',go_sha256=H(G.read_bytes()),contexts=ctxs,metadata_commands=metadata,lock_hashes={p:H((R/p).read_bytes()) for p in ['go.mod','go.sum','web/package-lock.json','third_party/enmime-v2.3.0/go.mod','third_party/go-smtp/go.mod']},replacements=inv.CURRENT_REPLACEMENTS))
def snapshot(label):
 out={}
 for ctx,args in metadata.items():
  for i,a in enumerate(args):out[(ctx,i)]=run(a,label+'-'+ctx+'-'+str(i),180)
 return out
def compare(a,z,label):
 def delta(x,y,path=''):
  if type(x)!=type(y):return [path]
  if isinstance(x,dict):return [p for k in sorted(set(x)|set(y)) for p in ([path+'/'+k] if k not in x or k not in y else delta(x[k],y[k],path+'/'+k))]
  if isinstance(x,list):return [path+'/length'] if len(x)!=len(y) else [p for i,(u,v) in enumerate(zip(x,y)) for p in delta(u,v,path+'/'+str(i))]
  return [] if x==y else [path]
 for key in a:
  fields=delta(b.stream(a[key]),b.stream(z[key]))
  if fields:diffs.append(dict(intervention=label,context=key[0],command_index=key[1],raw_before_sha256=H(a[key]),raw_after_sha256=H(z[key]),changed_fields=fields))
 save('raw-differences.json',diffs)
# Hydrate before the raw baseline; these are observations, not binding receipts.
snapshot('hydration'); previous=snapshot('before')
selections=[('root-build',['build','-mod=readonly','./...']),('default-selected',['test','-c','./internal/api/handlers','./internal/architecture']),('race-root',['test','-c','-race','./...']),('race-protocol',['test','-c','-race','-tags=r5protocol','./internal/api/handlers','./internal/store/postgres']),('race-audit',['test','-c','-race','-tags=r5audit','./internal/store/postgres'])]
save('compile-selections.json',selections)
def compile_one(name,args,phase):
 out=P/(phase+'-'+name);out.mkdir(mode=0o700)
 actual=args if args[0]=='build' else [*args[:2],'-o',str(out)+'/',*args[2:]]
 run(actual,phase+'-'+name)
 for artifact in out.iterdir():artifact.chmod(0o600)
 clean()
for name,args in selections:
 compile_one(name,args,'prewarm'); current=snapshot('after-'+name);compare(previous,current,name);previous=current
receipts={}
for name,ctx in [('default',b.DEFAULT_CONTEXT),('race-r5protocol',b.CONTEXT)]:
 receipts[name]=b.capture(R,G,cache=C,modulecache=M,context=ctx);save('binding-'+name+'.json',receipts[name])
post_before=snapshot('post-binding-before')
for name,args in selections:compile_one(name,args,'repeat')
post_after=snapshot('post-binding-after');compare(post_before,post_after,'repeat-all')
results={}
for name,receipt in receipts.items():
 try:
  observed=b.validate(receipt,R,G,cache=C,modulecache=M);save('post-'+name+'.json',observed);results[name]=dict(status='strict_identity_unchanged',attestation_sha256=receipt['attestation_sha256'])
 except Exception as e:
  results[name]=dict(status='rejected',error=str(e));save('validation-status.json',results);raise
save('validation-status.json',results);clean()
save('outcome.json',dict(status='bounded_compile_only_stable',strict_validation=results,tracked_bytes_clean=True,test_binaries_executed=0,raw_post_lists_equal=post_before==post_after,not_run=['formal default','sharedDB','HTTP','components','PG','mail','business services','Method19','SML','wholeCI','source runner','typed wire fixture execution']))
print('bounded compile-only experiment completed; private outcome saved',flush=True)
```
