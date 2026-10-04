"""Independent batch admission challenges. No real lifecycle or OS processes."""
import sys
EVENTS=[]
def guard(event,args):
    if event in ('subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp') or event.startswith(('os.exec','os.spawn')):
        EVENTS.append(event); raise AssertionError('OS process forbidden: '+event)
sys.addaudithook(guard)
import ast, copy, json, time, types, unittest
from pathlib import Path
from unittest import mock
ROOT=Path(__file__).resolve().parents[4]
# Retain fixture plumbing only; no author test method is compiled.
source=ROOT/'scripts/tests/r5_external_batch_v2_checks.py'
tree=ast.parse(source.read_text())
for node in tree.body:
    if isinstance(node,ast.ClassDef):
        node.body=[n for n in node.body if not isinstance(n,ast.FunctionDef) or not n.name.startswith('test_')]
tree.body=[n for n in tree.body if not isinstance(n,ast.If)]
f=types.ModuleType('batch_fixture'); f.__file__=str(source)
exec(compile(tree,str(source),'exec'),f.__dict__)
b=f.batch; r=b.runtime
MUTATIONS=0
class Independent(f.BatchChecks):
    def reject(self,candidate,version=3):
        with self.assertRaises(ValueError): b.load(*self.store(candidate),selected_binding_version=version)
        self.no_dispatch()
    def test_omission_and_both_valid_routes(self):
        old=copy.deepcopy(self.manifest);old.update(schema_version=2,policy=r.POLICY)
        old.pop('selected_binding_version');old.pop('admitted_selection')
        contract=b.capture(old,Path('/controller/tools/go'))
        self.fixture.process.reset_mock();self.catalog_mock.reset_mock()
        self.assertEqual(b.load(*self.store(contract)),contract)
        with mock.patch.object(r,'validate') as validate:
            b.validate_contract(contract);validate.assert_called_once_with(old,selected_binding_version=2)
        self.assertEqual(contract['required'],self.contract['required'])
        self.assertEqual(contract['argv'],self.contract['argv'])
        self.fixture.process.reset_mock();self.catalog_mock.reset_mock()
        self.reject(self.contract,2);self.reject(contract,3)
        self.assertEqual(b.load(*self.store(),selected_binding_version=3),self.contract)
    def test_strict_selector_at_all_public_boundaries(self):
        for selector in (None,False,True,'2','3',2.0,3.0,0,1,4,[],{}):
            with self.subTest(selector=selector):
                for action in (lambda:b.capture(self.manifest,Path('/controller/tools/go'),**dict(self.options,selected_binding_version=selector)),
                               lambda:b.load(*self.store(),selected_binding_version=selector),
                               lambda:b.validate_contract(self.contract,selected_binding_version=selector),
                               lambda:b.Batch(self.contract,'/private/out',selected_binding_version=selector)):
                    with self.assertRaises(ValueError):action()
                    self.no_dispatch()
    def test_contract_runtime_policy_schema_cross_product(self):
        for schema in (None,True,False,'2',2.0,1,2,3,4):
            for policy in (None,b.POLICY,b.V2_POLICY,r.POLICY,r.V3_POLICY,'unknown'):
                if type(schema)is int and schema==2 and policy==b.V2_POLICY:continue
                c=copy.deepcopy(self.contract);c.update(schema_version=schema,policy=policy);self.reject(c)
        for schema in (None,True,'3',3.0,1,2,3,4):
            for policy in (r.POLICY,r.V3_POLICY,'unknown'):
                if type(schema)is int and schema==3 and policy==r.V3_POLICY:continue
                c=copy.deepcopy(self.contract);c['runtime'].update(schema_version=schema,policy=policy)
                c['runtime_sha256']=r.digest(r.canonical(c['runtime']));self.reject(c)
    def test_outer_manifest_pins_and_recaptured_hash(self):
        path,pin=self.store()
        for raw in (b'{',b'{"x":1,"x":2}',b'{"x":Infinity}',b'[]'):
            self.files[path]=raw
            with self.assertRaises(ValueError):b.load(path,r.digest(raw),selected_binding_version=3)
        self.store();self.files[path]+=b' '
        with self.assertRaises(ValueError):b.load(path,pin,selected_binding_version=3)
        self.store();self.files[self.manifest_path]+=b' '
        self.reject(self.contract)
        c=copy.deepcopy(self.contract);c['runtime']['source']['commit']='f'*40
        c['runtime_sha256']=r.digest(r.canonical(c['runtime']));self.reject(c)
    def test_resealed_receipts_and_replaced_bundle_original_manifest_pin(self):
        global MUTATIONS
        admitted=self.manifest['admitted_selection']; bp=admitted['bundle_path'];original=self.files[bp]
        bundle=r.source_inventory.strict_json(original)
        for slot,pin in bundle['observations'].items():
            rp=pin['receipt_path']; raw=self.files[rp]
            for index in range(12):
                for key,value in [('raw_stdout_bytes',123),('raw_stdout_sha256','f'*64),('stderr_bytes',77),('stderr_sha256','f'*64),('role','forged'),('argv',['/forged/go']),('exit',1)]:
                    receipt=r.source_inventory.strict_json(raw);receipt['observation_envelope'][index][key]=value
                    receipt=f.fixture.v3._seal(receipt);changed=r.canonical(receipt);self.files[rp]=changed
                    swapped=copy.deepcopy(bundle);swapped['observations'][slot].update(receipt_byte_sha256=r.digest(changed),observation_sha256=f.fixture.v3.observation_digest(receipt))
                    self.files[bp]=r.canonical(swapped)
                    c=copy.deepcopy(self.contract);c['runtime']['admitted_selection']['bundle_byte_sha256']=r.digest(self.files[bp])
                    c['runtime_sha256']=r.digest(r.canonical(c['runtime']))
                    self.reject(c);MUTATIONS+=1
            self.files[rp]=raw
        self.files[bp]=original
    def test_authenticated_incompatible_admission(self):
        for key,value in [('source_commit','f'*40),('source_root','/forged'),('run_id','forged'),('selected_binding_version',True),('bundle_byte_sha256','f'*64)]:
            c=copy.deepcopy(self.contract);c['runtime']['admitted_selection'][key]=value
            self.files[self.manifest_path]=r.canonical(c['runtime'])
            c['runtime_manifest_sha256']=r.digest(self.files[self.manifest_path]);c['runtime_sha256']=r.digest(r.canonical(c['runtime']))
            self.reject(c)
    def test_env_worker_deadline_and_gate_order(self):
        events=[]
        original=r.load_pinned
        def authenticate(*args,**kw):events.append('authenticate');return original(*args,**kw)
        with mock.patch.object(r,'load_pinned',side_effect=authenticate),mock.patch.object(b.protocol,'load_cases',side_effect=lambda *a:(events.append('catalog') or self.catalog)):
            b.capture(self.manifest,Path('/controller/tools/go'),**self.options)
        self.assertEqual(events[:2],['authenticate','catalog'])
        instance=b.Batch(self.contract,'/private/out',dict(TABMAIL_R5_SELECTED_BINDING_VERSION='2',TABMAIL_R5_EXTERNAL_MANIFEST='/forged',TABMAIL_R5_EXTERNAL_MANIFEST_SHA256='f'*64),selected_binding_version=3)
        self.assertEqual(instance.env['TABMAIL_R5_SELECTED_BINDING_VERSION'],'3')
        self.assertEqual(instance.env['TABMAIL_R5_EXTERNAL_MANIFEST'],self.manifest_path)
        self.assertEqual(instance.env['TABMAIL_R5_EXTERNAL_MANIFEST_SHA256'],self.pin)
        instance.deadline=time.monotonic()+12;deadline=instance.deadline
        with mock.patch.object(b,'OwnedProcess') as owned,mock.patch.object(Path,'exists',return_value=True):
            owned.return_value.finish.return_value=dict(exit_code=0,timeout=False,tail=False)
            instance.validate_inventory()
            self.assertEqual(owned.call_args.args[0][-2:],['--selected-binding-version','3'])
            owned.return_value.finish.assert_called_once_with(instance.abort,deadline)
        self.assertEqual(instance.deadline,deadline)
    def test_cli_default_explicit_and_environment_cannot_upgrade(self):
        for action in ('capture','validate','run'):
            for version in (2,3):
                argv=['batch',action,'--source','/controller/source','--go','/controller/tools/go','--contract','/evidence/contract.json','--pin','a'*64,'--output','/private/out']
                if version==3:argv+=['--selected-binding-version','3']
                with mock.patch.object(sys,'argv',argv),mock.patch.object(r,'from_environment',return_value=self.manifest) as source,mock.patch.object(b,'capture',return_value=self.contract) as capture,mock.patch.object(b,'load',return_value=self.contract) as load,mock.patch.object(b,'validate_contract') as validate,mock.patch.object(b,'Batch') as constructor,mock.patch.object(b.signal,'signal'),mock.patch('builtins.print'),mock.patch.dict(f.os.environ,{'TABMAIL_R5_SELECTED_BINDING_VERSION':'3','TABMAIL_R5_EXTERNAL_MANIFEST':self.manifest_path,'TABMAIL_R5_EXTERNAL_MANIFEST_SHA256':self.pin}):
                    constructor.return_value.run.return_value=dict(status="BATCH_REJECTED")
                    b.main()
                    if action=='capture':
                        self.assertEqual(source.call_args.kwargs['selected_binding_version'],version)
                        self.assertEqual(capture.call_args.kwargs['selected_binding_version'],version)
                        self.assertEqual(validate.call_args.kwargs['selected_binding_version'],version)
                        self.assertEqual('runtime_manifest_path'in capture.call_args.kwargs,version==3)
                    else:
                        self.assertEqual(load.call_args.kwargs['selected_binding_version'],version)
                        self.assertEqual((validate if action=='validate' else constructor).call_args.kwargs['selected_binding_version'],version)
    def test_fixed_budgets_closed_fields_and_worker_rejection(self):
        for key,value in [('workers',True),('workers',4.0),('workers',5),('budgets',dict(go=120.0,process=180,case=75)),('budgets',dict(go=120,process=181,case=75)),('selected_binding_version',2),('mode','bad'),('product_green',1),('task_complete',True),('status','BATCH_QUALIFIED')]:
            c=copy.deepcopy(self.contract);c[key]=value;self.reject(c)
        for key in self.contract:
            c=copy.deepcopy(self.contract);c.pop(key);self.reject(c)
        c=copy.deepcopy(self.contract);c['extra']=False;self.reject(c)
        instance=b.Batch(self.contract,'/private/out',selected_binding_version=3);instance.deadline=time.monotonic()+10
        for result in (dict(exit_code=1,timeout=False,tail=False),dict(exit_code=0,timeout=True,tail=False),dict(exit_code=0,timeout=False,tail=True)):
            with mock.patch.object(b,'OwnedProcess') as owned,mock.patch.object(Path,'exists',return_value=True):
                owned.return_value.finish.return_value=result
                with self.assertRaises(ValueError):instance.validate_inventory()
        for cancelled in (True,False):
            if cancelled:instance.abort.set()
            else:instance.abort.clear();instance.deadline=time.monotonic()-1
            with mock.patch.object(b,'OwnedProcess') as owned,mock.patch.object(Path,'exists',return_value=True):
                with self.assertRaises(ValueError):instance.validate_inventory()
                owned.assert_not_called()
    def test_publication_fragment_failures_and_result_selector(self):
        # Execute only receipt construction/publication AST, never _run or lifecycle.
        tree=ast.parse((ROOT/b.HELPER).read_text());method=next(n for n in ast.walk(tree) if isinstance(n,ast.FunctionDef) and n.name=='_run')
        body=next(n for n in method.body if isinstance(n,ast.With)).body
        index=next(i for i,n in enumerate(body) if isinstance(n,ast.Assign) and any(isinstance(t,ast.Name) and t.id=='receipt' for t in n.targets))
        fragment=ast.fix_missing_locations(ast.Module(body=body[index:],type_ignores=[]))
        for version in (2,3):
            instance=types.SimpleNamespace(contract=dict(self.contract,schema_version=1 if version==2 else 2,policy=b.POLICY if version==2 else b.V2_POLICY),selected_binding_version=version,ack=[],children={},resources_joined=True,max_active=0,results={},output=mock.Mock(),published=False)
            for failure in ('write','chmod','replace',None):
                instance.published=False;pending=mock.Mock();instance.output.__truediv__=mock.Mock(return_value=pending)
                pending.write_bytes.side_effect=OSError('ENOSPC') if failure=='write' else None
                pending.replace.side_effect=OSError('replace') if failure=='replace' else None
                scope=dict(self=instance,runtime=r,POLICY=b.POLICY,before=True,after=True,required=[],terminals=[],errors=[])
                with mock.patch.object(b.os,'chmod',side_effect=OSError('chmod') if failure=='chmod' else None):
                    if failure:
                        with self.assertRaises(OSError):exec(compile(fragment,'<receipt-only>','exec'),dict(b.__dict__,**scope),scope)
                        self.assertFalse(instance.published)
                    else:
                        exec(compile(fragment,'<receipt-only>','exec'),dict(b.__dict__,**scope),scope)
                        receipt=scope['receipt'];self.assertTrue(instance.published)
                        self.assertEqual(receipt['schema_version'],1 if version==2 else 2)
                        self.assertEqual('selected_binding_version'in receipt,version==3)
    def test_finalization_barrier_only_with_mocked_execution(self):
        instance=b.Batch(self.contract,'/private/out',selected_binding_version=3)
        receipt=dict(status='BATCH_QUALIFIED',errors=[],children={'x':dict(qualified=True)},schema_version=2,policy=b.V2_POLICY,selected_binding_version=3)
        instance.published=True;instance.deadline=time.monotonic()+10
        with mock.patch.object(instance,'_run',side_effect=OSError('lease release')),mock.patch.object(Path,'read_bytes',return_value=r.canonical(receipt)),mock.patch.object(Path,'write_bytes') as write,mock.patch.object(Path,'replace'),mock.patch.object(b.os,'chmod'):
            result=instance.run();self.assertEqual(result['status'],'BATCH_REJECTED');self.assertFalse(result['children']['x']['qualified']);self.assertEqual(result['selected_binding_version'],3)
            self.assertIn('OSError',result['errors'][0]);write.assert_called_once()
        with self.assertRaises(ValueError):instance.run()
        instance=b.Batch(self.contract,'/private/out',selected_binding_version=3)
        with mock.patch.object(instance,'_run',side_effect=OSError('not published')),mock.patch.object(Path,'write_bytes') as write:
            with self.assertRaises(OSError):instance.run()
            write.assert_not_called()
if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Independent))
    print(json.dumps(dict(tests=result.testsRun,failures=len(result.failures),errors=len(result.errors),resealed_envelope_bundle_mutations=MUTATIONS,os_process_events=EVENTS)))
    sys.exit(0 if result.wasSuccessful() and not EVENTS else 1)
