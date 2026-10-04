"""Explicit synthetic preparation controls; never part of test_* discovery.
Run with python3 -B scripts/tests/r5_source_preparation_v3_checks.py.
All producer/build/execution/checkout/discovery boundaries are synthetic.
"""
import sys
PROCESS_EVENTS = []
def guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty', 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        PROCESS_EVENTS.append(event)
        raise AssertionError('OS process forbidden: ' + event)
sys.addaudithook(guard)
import copy
import hashlib
import json
import tempfile
from pathlib import Path
from unittest import mock
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
# Frozen explicit scaffolding, imported without executing its checks.
import r5_selected_binding_consumer_checks as fixtures
import r5_source_runner_prepare as p
import run_r5_source_version_tests as runner
v3 = fixtures.v3
RESULTS = []
def check(name, function):
    function()
    RESULTS.append(dict(name=name, passed=True))
def rejects(function):
    try:
        function()
    except (ValueError, OSError, KeyError, TypeError):
        return
    raise AssertionError('unexpected acceptance')
def seal(receipt):
    fixtures.source_seal(receipt['base_source'])
    return v3._seal(receipt)

class Harness:
    def __init__(self, directory, mutation=None):
        self.root = directory / 'source'; self.root.mkdir()
        self.output = directory / 'evidence'
        self.mutation = mutation
        self.calls = []; self.builds = []
    def capture(self, root, go, **kwargs):
        index = len(self.calls); self.calls.append(kwargs['context'])
        receipt = fixtures.fixture_receipt(kwargs['context'])
        receipt['base_source']['snapshot_root'] = str(root)
        # Hydration and raw selected diagnostics may differ with stable binding.
        receipt['observation_envelope'][1]['raw_stdout_bytes'] += index
        receipt['observation_envelope'][2]['raw_stdout_bytes'] += index
        if self.mutation:
            self.mutation(self, receipt, index)
        return seal(receipt)
    def build(self, argv, **kwargs):
        self.builds.append(argv)
        Path(argv[4]).write_bytes(b'synthetic binary')
        return p.subprocess.CompletedProcess(argv, 0, b'build', b'')
    def execute(self, root, go, binary, expected_hash, sha, fixture, env):
        fixture.write_text('{}')
        argv = [go,'tool','test2json','-t','-p','tabmail/internal/api/handlers','/proc/123/fd/8',
            '-test.v=test2json','-test.run=^'+p.TEST_NAME+'$','-test.count=1']
        raw = b'\n'.join(json.dumps(dict(Action=action, Test=p.TEST_NAME)).encode() for action in ('run','pass'))
        return p.subprocess.CompletedProcess(argv,0,raw,b''), dict(run_argv=argv,
            run_cwd=str(root/'internal/api/handlers'),executed_binary_source_path=str(binary),
            executed_binary_sha256=expected_hash,execution_binding='linux_parent_proc_fd_pinned_inode')
    def prepare(self, version=3):
        with mock.patch.object(p,'source_identity',return_value=fixtures.COMMIT), \
             mock.patch.object(p.r5_go_environment,'selected',return_value=(fixtures.PRODUCER['path'],dict(GOCACHE='/controller/cache/build',GOMODCACHE='/controller/cache/mod'))), \
             mock.patch.object(p.subprocess,'check_output',return_value='go1.25.7'), \
             mock.patch.object(v3,'capture',side_effect=self.capture), \
             mock.patch.object(p,'prepare_typescript',return_value={'synthetic':True}), \
             mock.patch.object(p.subprocess,'run',side_effect=self.build), \
             mock.patch.object(p,'execute_binary',side_effect=self.execute):
            return p.prepare(self.root,self.output,fixtures.PRODUCER['path'],selected_binding_version=version)
    def validate(self, receipt, env, version=3):
        with mock.patch.object(p,'source_identity',return_value=fixtures.COMMIT):
            return p.validate_wire(self.root,env['ORDINARY_RECEIPT_WIRE_FIXTURE'],env['R5_SOURCE_PREPARATION'],
                env['R5_SOURCE_PREPARATION_SHA256'],env['R5_SOURCE_RUN_ID'],selected_binding_version=version)

with tempfile.TemporaryDirectory(prefix='r5-v3-preparation-pure-') as temporary:
    base = Path(temporary)
    def fresh(name, mutation=None):
        directory = base/name; directory.mkdir()
        return Harness(directory, mutation)
    h = fresh('positive'); receipt, env = h.prepare()
    def positive():
        assert len(h.calls)==4 and len(h.builds)==1
        assert h.validate(receipt,env)=={}
        bundle = json.loads(Path(receipt['selected_observation_bundle']).read_bytes())
        assert set(bundle['observations']) == set(p.consumer.PREPARATION_SLOTS)
        pins = bundle['observations']
        assert len({pin['observation_sha256'] for pin in pins.values()}) == 4
        assert receipt['go_selection_before']==receipt['go_selection_after']
        assert all((h.output/(slot+'-observation-pin.json')).exists() for slot in pins)
    check('four-owned-returns-distinct-persisted-pins-complete-equality', positive)
    for version in (None,True,False,1,4,'3',3.0):
        check('invalid-selector-'+repr(version),lambda version=version: rejects(lambda:p.prepare(h.root,base/'unused','go',selected_binding_version=version)))
    for name in ('go','./go'):
        def bare(name=name):
            with mock.patch.object(p.r5_go_environment,'selected') as resolver, mock.patch.object(p,'source_identity') as source:
                rejects(lambda:p.prepare(h.root,base/'bare',name,selected_binding_version=3))
                resolver.assert_not_called();source.assert_not_called()
        check('bare-producer-before-environment-'+name,bare)
    check('v2-consumer-rejects-v3',lambda:rejects(lambda:h.validate(receipt,env,2)))
    check('wrong-run-id',lambda:rejects(lambda:h.validate(receipt,{**env,'R5_SOURCE_RUN_ID':'foreign'})))
    check('wrong-preparation-byte-pin',lambda:rejects(lambda:h.validate(receipt,{**env,'R5_SOURCE_PREPARATION_SHA256':'f'*64})))
    def prebuild_mutation(kind):
        def mutate(owner,r,index):
            if index != 2:return
            if kind=='binding':r['package_records']+=1
            elif kind=='root':r['base_source']['snapshot_root']='/foreign/root'
            elif kind=='context':r['base_source']['build_context']=v3.CONTEXT
            elif kind=='role':r['observation_envelope'][0]['role']='unbound_dependency_hydration_not_attested'
            elif kind=='argv':r['observation_envelope'][0]['argv'][0]='/foreign/go'
            elif kind=='exit':r['observation_envelope'][0]['exit']=1
            elif kind=='order':r['observation_envelope'].reverse()
            elif kind=='persisted':(owner.output/'before-default-selection.json').write_text('{}')
            elif kind=='pin':(owner.output/'before-default-observation-pin.json').write_text('{}')
        owner=fresh('prebuild-'+kind,mutate)
        rejects(owner.prepare)
        assert not owner.builds
        assert not (owner.output/'preparation.json').exists()
    for kind in ('binding','root','context','role','argv','exit','order','persisted','pin'):
        check('reject-before-build-'+kind,lambda kind=kind:prebuild_mutation(kind))
    for kind in ('hydration','stdout-length','stdout-hash','stderr','role','argv','exit','order','bundle','pin','fixture','binary','run-log'):
        def retained(kind=kind):
            owner=fresh('retained-'+kind); data,e=owner.prepare()
            path=owner.output/'before-default-selection.json'
            r=json.loads(path.read_bytes())
            if kind=='hydration':r['observation_envelope'][1]['raw_stdout_bytes']+=1
            elif kind=='stdout-length':r['observation_envelope'][2]['raw_stdout_bytes']+=1
            elif kind=='stdout-hash':r['observation_envelope'][2]['raw_stdout_sha256']='f'*64
            elif kind=='stderr':r['observation_envelope'][2].update(stderr_bytes=1,stderr_sha256='f'*64)
            elif kind=='role':r['observation_envelope'][0]['role']='foreign'
            elif kind=='argv':r['observation_envelope'][0]['argv']=['/foreign/go']
            elif kind=='exit':r['observation_envelope'][0]['exit']=1
            elif kind=='order':r['observation_envelope'].reverse()
            if kind in ('hydration','stdout-length','stdout-hash','stderr','role','argv','exit','order'):
                path.write_text(json.dumps(seal(r)))  # Even resealed both hashes cannot replace controller pin.
            elif kind=='bundle':Path(data['selected_observation_bundle']).write_text('{}')
            elif kind=='pin':(owner.output/'before-default-observation-pin.json').write_text('{}')
            elif kind=='fixture':Path(e['ORDINARY_RECEIPT_WIRE_FIXTURE']).write_text('{"foreign":true}')
            elif kind=='binary':(owner.output/'ordinary-receipt.test').write_bytes(b'foreign')
            elif kind=='run-log':(owner.output/'run.stdout').write_bytes(b'foreign')
            rejects(lambda:owner.validate(data,e))
        check('pinned-retained-reject-'+kind,retained)
    def replacement_bundle():
        owner=fresh('replacement-bundle');data,e=owner.prepare()
        bundle_path=Path(data['selected_observation_bundle'])
        bundle=json.loads(bundle_path.read_bytes())
        path=owner.output/'before-default-selection.json'
        r=json.loads(path.read_bytes());r['observation_envelope'][1]['raw_stdout_bytes']+=1
        raw=(json.dumps(seal(r))+'\n').encode();path.write_bytes(raw)
        pin=bundle['observations']['before-default']
        pin.update(receipt_byte_sha256=p.digest(raw),observation_sha256=v3.observation_digest(r))
        (owner.output/'before-default-observation-pin.json').write_text(json.dumps(pin))
        bundle_path.write_text(json.dumps(bundle))
        (owner.output/'selected-observation-pins.sha256').write_text(p.digest(bundle_path.read_bytes()))
        rejects(lambda:owner.validate(data,e))
    check('forged-resealed-receipt-replaced-bundle-colocated-hash-rejected',replacement_bundle)
    for kind in ('missing','extra','swapped','producer','source','run-id'):
        def bundle_gate(kind=kind):
            owner=fresh('bundle-gate-'+kind);data,e=owner.prepare()
            path=Path(data['selected_observation_bundle']);bundle=json.loads(path.read_bytes())
            if kind=='missing':bundle['observations'].pop('before-default')
            elif kind=='extra':bundle['observations']['foreign']=copy.deepcopy(bundle['observations']['before-default'])
            elif kind=='swapped':
                a,b='before-default','after-default'
                bundle['observations'][a],bundle['observations'][b]=bundle['observations'][b],bundle['observations'][a]
            elif kind=='producer':bundle['producer']['path']='/foreign/go'
            elif kind=='source':bundle['source_root']='/foreign/source'
            elif kind=='run-id':bundle['run_id']='foreign'
            path.write_text(json.dumps(bundle))
            # This models a controller-authenticated malformed bundle: identity/slot checks still apply.
            with mock.patch.object(v3,'capture') as producer:
                rejects(lambda:p.verify_v3_selection(path,p.digest(path.read_bytes()),root=owner.root,
                    sha=fixtures.COMMIT,run_id=data['run_id'],producer=data['selected_producer'],
                    observations=data['selected_observations']))
                producer.assert_not_called()
        check('bundle-contract-zero-producer-'+kind,bundle_gate)
    def postrun_mutation():
        owner=fresh('postrun');original=owner.execute
        def execute(*args,**kwargs):
            result=original(*args,**kwargs)
            (owner.output/'after-default-selection.json').write_text('{}')
            return result
        owner.execute=execute
        rejects(owner.prepare)
        assert len(owner.builds)==1 and not (owner.output/'preparation.json').exists()
    check('retained-postrun-mutation-no-publication',postrun_mutation)
    def default_v2():
        owner=fresh('default-v2')
        def selection(root,go,env,output,phase):
            p.exclusive(output/(phase+'-default-selection.json'),json.dumps({'environment':{}}).encode())
            return {'default':{'legacy':'unchanged'},'race-r5protocol':{'legacy':'unchanged'}}
        with mock.patch.object(p,'go_selection',side_effect=selection),mock.patch.object(v3,'capture') as capture:
            data,e=owner.prepare(2)
            assert 'schema_version' not in data and not any(k.startswith('R5_SELECTED_') for k in e)
            capture.assert_not_called()
            assert owner.validate(data,e,2)=={}
            rejects(lambda:owner.validate(data,e,3))
    check('default-v2-shape-no-v3-capture-and-v3-rejects-v2',default_v2)
    for kind in ('extra','missing','wrong-policy','bool-schema','bool-version'):
        def schema(kind=kind):
            owner=fresh('schema-'+kind);data,e=owner.prepare()
            if kind=='extra':data['foreign']=True
            elif kind=='missing':data.pop('go_version')
            elif kind=='wrong-policy':data['policy']='r5_source_preparation_v2'
            elif kind=='bool-schema':data['schema_version']=True
            else:data['selected_binding_version']=True
            path=Path(e['R5_SOURCE_PREPARATION']);path.write_text(json.dumps(data))
            e['R5_SOURCE_PREPARATION_SHA256']=p.digest(path.read_bytes())
            rejects(lambda:owner.validate(data,e))
        check('v3-strict-preparation-schema-'+kind,schema)
    for kind in ('missing-run','missing-pass','wrong-test','nonzero'):
        def wire_failure(kind=kind):
            owner=fresh('wire-failure-'+kind);original=owner.execute
            def execute(*args,**kwargs):
                run,e=original(*args,**kwargs)
                if kind=='missing-run':run.stdout=json.dumps(dict(Action='pass',Test=p.TEST_NAME)).encode()
                elif kind=='missing-pass':run.stdout=json.dumps(dict(Action='run',Test=p.TEST_NAME)).encode()
                elif kind=='wrong-test':run.stdout=run.stdout.replace(p.TEST_NAME.encode(),b'ForeignTest')
                else:run.returncode=1
                return run,e
            owner.execute=execute
            rejects(owner.prepare)
            assert not (owner.output/'preparation.json').exists()
        check('exact-wire-test-publication-reject-'+kind,wire_failure)
    for kind in ('extra','symlink','tampered','extra-directory'):
        def typescript(kind=kind):
            directory=base/('typescript-'+kind);target=directory/'web/node_modules/typescript'
            target.mkdir(parents=True);(target/'index.js').write_bytes(b'locked')
            expected={'index.js':b'locked'};p.verify_typescript(directory,expected)
            if kind=='extra':(target/'foreign.js').write_bytes(b'foreign')
            elif kind=='symlink':(target/'index.js').unlink();(target/'index.js').symlink_to('/foreign')
            elif kind=='tampered':(target/'index.js').write_bytes(b'foreign')
            else:(target/'foreign').mkdir()
            rejects(lambda:p.verify_typescript(directory,expected))
        check('preserved-typescript-boundary-'+kind,typescript)
    for kind in ('wrong-source','stale-fixture','binary-tamper','midflight-inode'):
        def fd_boundary(kind=kind):
            directory=base/('fd-'+kind);directory.mkdir()
            binary=directory/'ordinary-receipt.test';binary.write_bytes(b'original')
            fixture=directory/'wire.json';pin=p.digest(binary.read_bytes())
            if kind=='stale-fixture':fixture.write_bytes(b'old')
            elif kind=='binary-tamper':binary.write_bytes(b'foreign')
            def process(argv,**kwargs):
                assert Path(argv[6]).read_bytes()==b'original'
                binary.unlink();binary.write_bytes(b'foreign')
                assert Path(argv[6]).read_bytes()==b'original'
                return p.subprocess.CompletedProcess(argv,0,b'',b'')
            with mock.patch.object(p,'source_identity',return_value=('f'*40 if kind=='wrong-source' else fixtures.COMMIT)), \
                 mock.patch.object(p.subprocess,'run',side_effect=process) as execution:
                rejects(lambda:p.execute_binary(directory,'/controller/tools/go',binary,pin,fixtures.COMMIT,fixture,{}))
                if kind!='midflight-inode':execution.assert_not_called()
        check('preserved-linux-fd-boundary-'+kind,fd_boundary)
    def runner_dispatch(version):
        root=base/('runner-source-'+str(version));root.mkdir()
        out=base/('runner-'+str(version)+'.json')
        ids=sorted([*runner.HISTORICAL_IDS,runner.FRESH_CLASS+'synthetic'])
        calls=[]
        prepared_env={**env} if version==3 else {key:value for key,value in env.items() if not key.startswith('R5_SELECTED_')}
        def dispatch(argv,**kwargs):
            calls.append((argv,kwargs['env']))
            requested=json.loads(Path(argv[argv.index('--ids')+1]).read_text())
            source=fixtures.COMMIT if len(calls)==1 else runner.BASELINE
            Path(argv[argv.index('--output')+1]).write_text(json.dumps(dict(source_sha=source,actual_test_ids=requested)))
            return p.subprocess.CompletedProcess(argv,0)
        def identity(root,*args):return runner.BASELINE if str(root).endswith('frozen-v1') else fixtures.COMMIT
        contaminated={**prepared_env,'R5_SELECTED_BINDING_VERSION':'3','R5_SELECTED_OBSERVATION_BUNDLE':'/foreign','R5_SELECTED_OBSERVATION_BUNDLE_SHA256':'f'*64}
        with mock.patch.object(runner,'checkout'),mock.patch.object(runner,'discover',return_value=ids), \
             mock.patch.object(runner,'git',side_effect=identity),mock.patch.object(p,'prepare',return_value=(receipt,prepared_env)) as prep, \
             mock.patch.object(runner.r5_go_environment,'selected',return_value=('/controller/tools/go',contaminated)), \
             mock.patch.object(runner.subprocess,'run',side_effect=dispatch):
            assert runner.run(root,out,selected_binding_version=version)==0
            assert len(calls)==2
            assert not any(k.startswith('R5_SELECTED_') for k in calls[1][1])
            assert prep.call_args.kwargs == ({'selected_binding_version':3} if version==3 else {})
            if version==3:assert calls[0][1]['R5_SELECTED_BINDING_VERSION']=='3'
            else:assert not any(k.startswith('R5_SELECTED_') for k in calls[0][1])
        report=json.loads(out.read_text())
        assert report['groups'][1]['requested_test_ids']==sorted(runner.HISTORICAL_IDS)
        assert report['discovered_test_ids']==ids
    for version in (2,3):check('current-only-dispatch-v'+str(version),lambda version=version:runner_dispatch(version))
    def failure_dispatch():
        root=base/'failure-runner';root.mkdir();out=base/'failure.json'
        ids=sorted([*runner.HISTORICAL_IDS,runner.FRESH_CLASS+'synthetic'])
        with mock.patch.object(runner,'checkout'),mock.patch.object(runner,'discover',return_value=ids), \
             mock.patch.object(runner,'git',return_value=fixtures.COMMIT), \
             mock.patch.object(p,'prepare',side_effect=ValueError('selection rejected')), \
             mock.patch.object(runner.subprocess,'run') as child:
            assert runner.run(root,out,selected_binding_version=3)==1
            child.assert_not_called()
        assert json.loads(out.read_bytes())['missing_test_ids']==ids
    check('failed-selection-zero-child-dispatch',failure_dispatch)

assert not PROCESS_EVENTS
print(json.dumps(dict(scope='synthetic preparation only; no runtime qualification', controls=RESULTS,
    count=len(RESULTS),os_process_events=PROCESS_EVENTS, source_sha256={str(path.relative_to(Path(__file__).resolve().parents[2])):hashlib.sha256(path.read_bytes()).hexdigest() for path in
        (Path(p.__file__),Path(runner.__file__),Path(p.consumer.__file__),Path(__file__))}),indent=2))
