"""Independent synthetic slice2 review. No test discovery or physical producer work."""
import sys
EVENTS=[]
def guard(event,args):
    if event in ('subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp') or event.startswith(('os.exec','os.spawn')):
        EVENTS.append(event); raise AssertionError('OS process forbidden: '+event)
    if event in ('socket.connect','socket.bind'):
        raise AssertionError('network forbidden')
sys.addaudithook(guard)
import contextlib, copy, hashlib, json, os, tempfile
from pathlib import Path
from unittest.mock import patch
ROOT=Path(__file__).resolve().parents[4]
sys.path[:0]=[str(ROOT/'scripts'),str(ROOT/'scripts/tests')]
# Only accepted adapter fixture construction is reused; candidate checks are not imported.
import r5_selected_binding_consumer_checks as f
import r5_source_runner_prepare as p
import run_r5_source_version_tests as runner
v=f.v3
RESULTS=[]
def test(name,fn):
    fn(); RESULTS.append({'name':name,'passed':True})
def reject(fn):
    try: fn()
    except (ValueError,OSError,KeyError,TypeError): return
    raise AssertionError('unexpected acceptance')
def encoded(value): return (json.dumps(value,allow_nan=False)+'\n').encode()
def seal(value):
    f.source_seal(value['base_source']); return v._seal(value)
class Harness:
    def __init__(self,d):
        self.root=d/'source'; self.root.mkdir();self.out=d/'owned';self.captures=[];self.builds=[];self.returns=[];self.mutate=lambda r,i:None
    def capture(self,root,producer,**kwargs):
        i=len(self.captures);self.captures.append(copy.deepcopy(kwargs))
        r=f.fixture_receipt(kwargs['context']);r['base_source']['snapshot_root']=str(root)
        r['observation_envelope'][1]['raw_stdout_bytes']+=i+10
        self.mutate(r,i);r=seal(r);self.returns.append(copy.deepcopy(r));return r
    def build(self,argv,**kwargs):
        self.builds.append((argv,kwargs));Path(argv[4]).write_bytes(b'pure compiled fixture');return p.subprocess.CompletedProcess(argv,0,b'build-out',b'build-err')
    def execute(self,root,go,binary,pin,sha,fixture,env):
        fixture.write_bytes(b'{}');argv=[go,'tool','test2json','-t','-p','tabmail/internal/api/handlers','/proc/123/fd/4','-test.v=test2json','-test.run=^'+p.TEST_NAME+'$','-test.count=1']
        raw=b'\n'.join(encoded({'Action':a,'Test':p.TEST_NAME}).strip() for a in ('run','pass'))
        return p.subprocess.CompletedProcess(argv,0,raw,b'run-err'),dict(run_argv=argv,run_cwd=str(root/'internal/api/handlers'),executed_binary_source_path=str(binary),executed_binary_sha256=pin,execution_binding='linux_parent_proc_fd_pinned_inode')
    @contextlib.contextmanager
    def boundary(self):
        with contextlib.ExitStack() as s:
            for obj,name,kw in [(p,'source_identity',dict(return_value=f.COMMIT)),(p.r5_go_environment,'selected',dict(return_value=(f.PRODUCER['path'],{'GOCACHE':'/controller/build','GOMODCACHE':'/controller/modules'}))),(p.subprocess,'check_output',dict(return_value='go1.25.7')),(v,'capture',dict(side_effect=self.capture)),(p,'prepare_typescript',dict(return_value={'locked':'synthetic'})),(p.subprocess,'run',dict(side_effect=self.build)),(p,'execute_binary',dict(side_effect=self.execute))]:s.enter_context(patch.object(obj,name,**kw))
            yield
    def prepare(self):
        with self.boundary():return p.prepare(self.root,self.out,f.PRODUCER['path'],selected_binding_version=3)
    def validate(self,env,version=3):
        with patch.object(p,'source_identity',return_value=f.COMMIT):return p.validate_wire(self.root,env['ORDINARY_RECEIPT_WIRE_FIXTURE'],env['R5_SOURCE_PREPARATION'],env['R5_SOURCE_PREPARATION_SHA256'],env['R5_SOURCE_RUN_ID'],selected_binding_version=version)
with tempfile.TemporaryDirectory(prefix='independent-slice2-') as tmp:
    base=Path(tmp);counter=0
    def fresh():
        global counter
        counter+=1;d=base/str(counter);d.mkdir();return Harness(d)
    def owned():
        h=fresh();data,env=h.prepare();assert h.validate(env)=={};assert len(h.captures)==4 and len(h.builds)==1
        bundle=json.loads((h.out/'selected-observation-pins.json').read_bytes())
        for slot,r in zip(p.consumer.PREPARATION_SLOTS,h.returns):
            expected=hashlib.sha256(json.dumps({k:value for k,value in r.items() if k!='observation_sha256'},sort_keys=True,separators=(',',':'),ensure_ascii=True,allow_nan=False).encode()).hexdigest()
            assert bundle['observations'][slot]['observation_sha256']==expected
            raw=(h.out/(slot+'-selection.json')).read_bytes();assert bundle['observations'][slot]['receipt_byte_sha256']==hashlib.sha256(raw).hexdigest()
            assert json.loads(raw)==r
        assert data['go_selection_before']==data['go_selection_after']
        assert h.builds[0][1]['env']['GOENV']=='off'
        assert env['R5_SOURCE_PREPARATION_SHA256']==p.digest((h.out/'preparation.json').read_bytes())
    test('owned-returns-independent-complete-canonical-digests-byte-pins-and-environment',owned)
    for name in ('go','./go','bin/go','../go',''):
        def selector(name=name):
            h=fresh()
            with patch.object(p.r5_go_environment,'selected') as resolver,patch.object(p,'source_identity') as source,patch.object(v,'capture') as capture:
                reject(lambda:p.prepare(h.root,h.out,name,selected_binding_version=3));resolver.assert_not_called();source.assert_not_called();capture.assert_not_called();assert not h.out.exists()
        test('producer-before-boundary-'+repr(name),selector)
    for selector in (None,True,False,1,0,4,'3',3.0):
        test('strict-selector-'+repr(selector),lambda selector=selector:reject(lambda:p.prepare(base,base/'invalid','go',selected_binding_version=selector)))
    for index in range(4):
        def failed(index=index):
            h=fresh();original=h.capture
            def capture(*a,**kw):
                if len(h.captures)==index:raise ValueError('synthetic capture failure')
                return original(*a,**kw)
            h.capture=capture;reject(h.prepare);assert not h.builds and not (h.out/'preparation.json').exists()
        test('failed-owned-capture-'+str(index),failed)
    # Every publication boundary, both exclusive collision and simulated ENOSPC.
    names=[slot+suffix for slot in p.consumer.PREPARATION_SLOTS for suffix in ('-selection.json','-observation-pin.json')]+['selected-observation-pins.json','build.stdout','build.stderr','run.stdout','run.stderr','preparation.json']
    for name in names:
        for mode in ('collision','ENOSPC'):
            def publication(name=name,mode=mode):
                h=fresh();real=p.exclusive
                def write(path,raw):
                    if Path(path).name==name:
                        if mode=='collision':Path(path).write_bytes(b'foreign');return real(path,raw)
                        raise OSError(28,'synthetic ENOSPC')
                    return real(path,raw)
                with patch.object(p,'exclusive',side_effect=write):reject(h.prepare)
                if name in names[:9]:assert not h.builds
                if name!='preparation.json' or mode=='ENOSPC':assert not (h.out/'preparation.json').exists()
                if mode=='collision':assert (h.out/name).read_bytes()==b'foreign'
            test('publication-'+mode+'-'+name,publication)
    for slot in p.consumer.PREPARATION_SLOTS:
        for kind in ('reseal','replace-bundle','swap-context','symlink'):
            def substitution(slot=slot,kind=kind):
                h=fresh();data,env=h.prepare();path=h.out/(slot+'-selection.json');r=json.loads(path.read_bytes())
                if kind=='symlink':
                    target=h.out/'replacement';target.write_bytes(path.read_bytes());path.unlink();path.symlink_to(target)
                else:
                    r['observation_envelope'][1]['raw_stdout_bytes']+=77
                    if kind=='swap-context':r['base_source']['build_context']=v.CONTEXT if r['base_source']['build_context']==v.DEFAULT_CONTEXT else v.DEFAULT_CONTEXT
                    raw=encoded(seal(r));path.write_bytes(raw)
                    if kind=='replace-bundle':
                        bp=h.out/'selected-observation-pins.json';b=json.loads(bp.read_bytes());pin=b['observations'][slot];pin['observation_sha256']=v.observation_digest(r);pin['receipt_byte_sha256']=p.digest(raw)
                        (h.out/(slot+'-observation-pin.json')).write_bytes(encoded(pin));bp.write_bytes(encoded(b));(h.out/'selected-observation-pins.sha256').write_text(p.digest(bp.read_bytes()))
                reject(lambda:h.validate(env))
            test('fixed-controller-pin-'+slot+'-'+kind,substitution)
    for key in ('package_records','qualification','root_mvs','selected_local_packages','environment','go_env'):
        def drift(key=key):
            h=fresh()
            def mutate(r,i):
                if i!=2:return
                if key=='package_records':r[key]+=1
                elif key=='qualification':r[key]['foreign']='retained'
                elif key=='root_mvs':r[key][0]['foreign']='retained'
                elif key=='selected_local_packages':r[key][0]['foreign']='retained'
                else:r[key]['foreign']='retained'
            h.mutate=mutate;reject(h.prepare);assert not h.builds
        test('full-binding-prebuild-'+key,drift)
    for key in ('source_root','source_commit','run_id','producer','phase'):
        def identity(key=key):
            h=fresh();data,env=h.prepare();bp=h.out/'selected-observation-pins.json';b=json.loads(bp.read_bytes())
            if key=='producer':b[key]['path']='/foreign/go'
            elif key=='phase':b['observations']['before-default'],b['observations']['after-default']=b['observations']['after-default'],b['observations']['before-default']
            else:b[key]='/foreign' if key=='source_root' else 'f'*40
            bp.write_bytes(encoded(b))
            with patch.object(v,'capture') as capture:
                reject(lambda:p.verify_v3_selection(bp,p.digest(bp.read_bytes()),root=h.root,sha=f.COMMIT,run_id=data['run_id'],producer=data['selected_producer'],observations=data['selected_observations']));capture.assert_not_called()
        test('authenticated-wrong-identity-'+key,identity)
    for target in ('source','build-cache','module-cache'):
        def placement(target=target):
            h=fresh();build=h.root.parent/'build-cache';modules=h.root.parent/'module-cache'
            h.out=(h.root if target=='source' else build if target=='build-cache' else modules)/'evidence'
            with h.boundary(),patch.object(p.r5_go_environment,'selected',return_value=(f.PRODUCER['path'],{'GOCACHE':str(build),'GOMODCACHE':str(modules)})):
                reject(lambda:p.prepare(h.root,h.out,f.PRODUCER['path'],selected_binding_version=3))
            assert not h.captures and not h.builds
        test('pin-publication-outside-'+target,placement)
    for name in ('before-default-selection.json','before-default-observation-pin.json','selected-observation-pins.json'):
        def retained_write(name=name):
            h=fresh();real=p.exclusive
            def write(path,raw):
                real(path,raw)
                if Path(path).name==name:Path(path).write_bytes(b'{}')
            with patch.object(p,'exclusive',side_effect=write):reject(h.prepare)
            assert not h.builds and not (h.out/'preparation.json').exists()
        test('persisted-substitution-before-build-'+name,retained_write)
    for key in ('HOME','PATH','GOGCCFLAGS'):
        def valid_drift(key=key):
            h=fresh()
            def mutate(r,i):
                if i==2:
                    r['go_env' if key=='GOGCCFLAGS' else 'environment'][key]+='-independent-drift'
            h.mutate=mutate
            try:h.prepare()
            except ValueError as error:assert str(error)=='TypeScript preparation changed complete Go binding payload'
            else:raise AssertionError('valid full binding drift admitted')
            assert not h.builds and not (h.out/'preparation.json').exists()
        test('schema-valid-complete-binding-drift-'+key,valid_drift)
    def omitted_v2():
        h=fresh()
        def legacy(root,go,env,output,phase):
            p.exclusive(output/(phase+'-default-selection.json'),encoded({'environment':{}}))
            return {'default':{'unchanged':'v2'},'race-r5protocol':{'unchanged':'v2'}}
        with h.boundary(),patch.object(p,'go_selection',side_effect=legacy),patch.object(v,'capture') as capture:
            data,env=p.prepare(h.root,h.out,f.PRODUCER['path'])
            capture.assert_not_called();assert 'schema_version' not in data;assert not any(k.startswith('R5_SELECTED_') for k in env)
        with patch.dict(os.environ,{},clear=True):assert h.validate(env,2)=={}
        reject(lambda:h.validate(env,3))
    test('omitted-preparation-selector-v2-retains-legacy-shape-and-domain',omitted_v2)
    for selector in ('1','4','true','', '03'):
        def bad_environment(selector=selector):
            h=fresh();data,env=h.prepare()
            with patch.dict(os.environ,{'R5_SELECTED_BINDING_VERSION':selector}),patch.object(p,'source_identity',return_value=f.COMMIT):
                reject(lambda:p.validate_wire(h.root,env['ORDINARY_RECEIPT_WIRE_FIXTURE'],env['R5_SOURCE_PREPARATION'],env['R5_SOURCE_PREPARATION_SHA256'],env['R5_SOURCE_RUN_ID']))
        test('environment-selector-reject-'+repr(selector),bad_environment)
    for version in (2,3):
        def dispatch(version=version):
            h=fresh();data,prepared_env=h.prepare();output=base/('runner-'+str(version)+'.json');calls=[]
            ids=sorted([*runner.HISTORICAL_IDS,runner.FRESH_CLASS+'independent'])
            if version==2:prepared_env={k:value for k,value in prepared_env.items() if not k.startswith('R5_SELECTED_')}
            contaminated={**prepared_env,'R5_SELECTED_BINDING_VERSION':'3','R5_SELECTED_OBSERVATION_BUNDLE':'/foreign','R5_SELECTED_OBSERVATION_BUNDLE_SHA256':'f'*64}
            def child(argv,**kw):
                calls.append((argv,kw));requested=json.loads(Path(argv[argv.index('--ids')+1]).read_bytes());sha=f.COMMIT if len(calls)==1 else runner.BASELINE
                Path(argv[argv.index('--output')+1]).write_bytes(encoded({'source_sha':sha,'actual_test_ids':requested}));return p.subprocess.CompletedProcess(argv,0)
            with patch.object(runner,'git',side_effect=lambda root,*a:runner.BASELINE if str(root).endswith('frozen-v1') else f.COMMIT),patch.object(runner,'checkout'),patch.object(runner,'discover',return_value=ids),patch.object(p,'prepare',return_value=(data,prepared_env)) as prepare,patch.object(runner.r5_go_environment,'selected',return_value=(f.PRODUCER['path'],contaminated)),patch.object(runner.subprocess,'run',side_effect=child):
                assert runner.run(h.root,output,selected_binding_version=version)==0
                assert len(calls)==2;assert not any(k.startswith('R5_SELECTED_') for k in calls[1][1]['env']);assert '--selected-binding-version' not in calls[1][0]
                assert prepare.call_args.kwargs==({'selected_binding_version':3} if version==3 else {})
            report=json.loads(output.read_bytes());assert report['discovered_test_ids']==ids;assert report['groups'][1]['requested_test_ids']==sorted(runner.HISTORICAL_IDS)
        test('current-only-frozen-child-contamination-v'+str(version),dispatch)
assert not EVENTS
print(json.dumps({'scope':'independent pure synthetic preparation boundaries; no runtime qualification','count':len(RESULTS),'controls':RESULTS,'os_process_events':EVENTS},indent=2))
