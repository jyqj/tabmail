"""Explicit non-discovery slice4 batch checks; synthetic boundaries only."""
import sys
EVENTS = []
def guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty',
                 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        EVENTS.append(event)
        raise AssertionError('OS process forbidden: ' + event)
sys.addaudithook(guard)
import ast
import contextlib
import copy
import json
import os
from pathlib import Path
import time
import types
import unittest
from unittest import mock
SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS / 'preparation'))
import r5_external_batch as batch
runtime = batch.runtime
# Reuse only the frozen synthetic fixture plumbing, never its test methods.
tree = ast.parse((SCRIPTS/'tests/r5_external_runtime_v3_checks.py').read_text())
for node in tree.body:
    if isinstance(node, ast.ClassDef) and node.name == 'RuntimeV3Checks':
        node.body = [n for n in node.body if not isinstance(n, ast.FunctionDef) or not n.name.startswith('test_')]
tree.body = [n for n in tree.body if not isinstance(n, ast.If)]
fixture = types.ModuleType('synthetic_runtime_fixture')
fixture.__file__ = str(SCRIPTS/'tests/r5_external_runtime_v3_checks.py')
exec(compile(tree, fixture.__file__, 'exec'), fixture.__dict__)

class BatchChecks(unittest.TestCase):
    def setUp(self):
        self.fixture = fixture.RuntimeV3Checks()
        self.fixture.setUp()
        self.addCleanup(self.fixture.doCleanups)
        self.manifest = self.fixture.manifest()
        self.fixture.producer.reset_mock(); self.fixture.v2producer.reset_mock(); self.fixture.process.reset_mock()
        self.files = self.fixture.files
        self.manifest_path = '/evidence/runtime.json'
        self.files[self.manifest_path] = runtime.canonical(self.manifest) + b'\n'
        self.pin = runtime.digest(self.files[self.manifest_path])
        self.stack = contextlib.ExitStack(); self.addCleanup(self.stack.close)
        self.catalog = json.loads((SCRIPTS.parent/batch.CATALOG).read_text())
        self.catalog_mock = self.stack.enter_context(mock.patch.object(batch.protocol, 'load_cases', return_value=self.catalog))
        self.stack.enter_context(mock.patch.object(batch, 'file_digest', return_value='a'*64))
        old_output = self.fixture.process.side_effect
        self.fixture.process.side_effect = lambda argv, **kwargs: ('go version go1.25.7 linux/amd64' if argv == ['/controller/tools/go', 'version'] else old_output(argv, **kwargs))
        self.options = dict(selected_binding_version=3, runtime_manifest_path=self.manifest_path, runtime_manifest_sha256=self.pin)
        self.contract = batch.capture(self.manifest, Path('/controller/tools/go'), **self.options)
        self.catalog_mock.reset_mock(); self.fixture.process.reset_mock()
        self.stack.enter_context(mock.patch.object(batch, '__file__', fixture.ROOT+'/'+batch.HELPER))
        self.stack.enter_context(mock.patch.object(sys, 'executable', '/controller/tools/python3'))
    def no_dispatch(self):
        self.fixture.producer.assert_not_called(); self.fixture.v2producer.assert_not_called()
        self.fixture.process.assert_not_called(); self.catalog_mock.assert_not_called()
    def store(self, contract=None):
        raw = runtime.canonical(self.contract if contract is None else contract)
        self.files['/evidence/contract.json'] = raw
        return '/evidence/contract.json', runtime.digest(raw)
    def test_default_v1_exact_route(self):
        manifest = copy.deepcopy(self.manifest)
        manifest.update(schema_version=2, policy=runtime.POLICY)
        del manifest['selected_binding_version']; del manifest['admitted_selection']
        contract = batch.capture(manifest, Path('/controller/tools/go'))
        self.assertEqual(set(contract), batch.CONTRACT_FIELDS)
        self.assertEqual(contract['schema_version'], 1); self.assertEqual(contract['policy'], batch.POLICY)
        self.assertEqual(batch.contract_gate(contract), 2)
        with mock.patch.object(runtime, 'validate') as validate:
            batch.validate_contract(contract)
            validate.assert_called_once_with(manifest, selected_binding_version=2)
        self.assertEqual(contract['required'], self.contract['required'])
        self.assertEqual(contract['argv'], self.contract['argv'])
        self.assertEqual(contract['build_context'], self.contract['build_context'])
    def test_explicit_v3_load_validate_and_no_producer(self):
        path, pin = self.store()
        self.assertEqual(batch.load(path, pin, selected_binding_version=3), self.contract)
        batch.validate_contract(self.contract, selected_binding_version=3)
        self.fixture.producer.assert_not_called(); self.fixture.v2producer.assert_not_called()
        self.assertEqual(self.fixture.process.call_count, 4) # mocked status/HEAD/Node plus Go version
    def test_selector_matrix_zero_dispatch(self):
        for version in (None, True, False, 1, 4, '3', 3.0, 2):
            with self.subTest(version=version), self.assertRaises(ValueError):
                batch.capture(self.manifest, Path('/controller/tools/go'), **dict(self.options, selected_binding_version=version))
            with self.assertRaises(ValueError): batch.load(*self.store(), selected_binding_version=version)
            self.no_dispatch()
        with self.assertRaises(ValueError): batch.load(*self.store())
        self.no_dispatch()
    def test_all_policy_schema_runtime_pairings(self):
        for schema in (None, True, 1, 2, 3, '2', 2.0):
            for policy in (None, batch.POLICY, batch.V2_POLICY, 'unknown'):
                if type(schema) is int and schema == 2 and policy == batch.V2_POLICY: continue
                value = copy.deepcopy(self.contract); value.update(schema_version=schema, policy=policy)
                with self.subTest(schema=schema,policy=policy), self.assertRaises(ValueError):
                    batch.load(*self.store(value), selected_binding_version=3)
                self.no_dispatch()
        for version in (1, 2, 3):
            for policy in (runtime.POLICY, runtime.V3_POLICY, 'unknown'):
                if version == 3 and policy == runtime.V3_POLICY: continue
                value = copy.deepcopy(self.manifest); value.update(schema_version=version, policy=policy)
                with self.assertRaises(ValueError): batch.capture(value, Path('/controller/tools/go'), **self.options)
                self.no_dispatch()
    def test_v1_closed_to_explicit_v3_and_mixed_runtime(self):
        manifest=copy.deepcopy(self.manifest)
        manifest.update(schema_version=2,policy=runtime.POLICY)
        del manifest['selected_binding_version']; del manifest['admitted_selection']
        contract=batch.capture(manifest,Path('/controller/tools/go'))
        self.fixture.process.reset_mock();self.catalog_mock.reset_mock()
        with self.assertRaises(ValueError):batch.load(*self.store(contract),selected_binding_version=3)
        for key,value in [('selected_binding_version',2),('admitted_selection',{})]:
            candidate=copy.deepcopy(manifest);candidate[key]=value
            with self.assertRaises(ValueError):batch.capture(candidate,Path('/controller/tools/go'))
            self.no_dispatch()
        for version in (1,True,3.0,'3'):
            candidate=copy.deepcopy(self.manifest);candidate['selected_binding_version']=version
            with self.assertRaises(ValueError):batch.capture(candidate,Path('/controller/tools/go'),**self.options)
            self.no_dispatch()
    def test_missing_bad_references_mode_and_strict_json_before_dispatch(self):
        for options in (dict(self.options,runtime_manifest_path=None),
                        dict(self.options,runtime_manifest_sha256=None),
                        dict(self.options,runtime_manifest_sha256='bad'),
                        dict(self.options,runtime_manifest_sha256='f'*64)):
            with self.assertRaises(ValueError):batch.capture(self.manifest,Path('/controller/tools/go'),**options)
            self.no_dispatch()
        with self.assertRaises(ValueError):batch.capture(self.manifest,Path('/controller/tools/go'),'unknown',**self.options)
        for raw in (b'{"policy":1,"policy":2}',b'{"policy":NaN}',b'{'):
            self.files['/evidence/contract.json']=raw
            with self.assertRaises(ValueError):batch.load('/evidence/contract.json',runtime.digest(raw),selected_binding_version=3)
            self.no_dispatch()

    def test_contract_fields_and_promotion_reject(self):
        for key, value in [('selected_binding_version', True), ('selected_binding_version', '3'),
                           ('selected_binding_version', 3.0), ('workers', True), ('workers', 5),
                           ('budgets', dict(go=120.0,process=180,case=75)), ('mode','unknown'),
                           ('status','BATCH_QUALIFIED'), ('product_green',True), ('task_complete',True),
                           ('concurrency_boundary','qualified'), ('runtime_sha256','f'*64)]:
            candidate=copy.deepcopy(self.contract); candidate[key]=value
            with self.subTest(key=key,value=value), self.assertRaises(ValueError):
                batch.load(*self.store(candidate), selected_binding_version=3)
            self.no_dispatch()
        for key in self.contract:
            candidate=copy.deepcopy(self.contract); del candidate[key]
            with self.assertRaises(ValueError): batch.load(*self.store(candidate), selected_binding_version=3)
        candidate=copy.deepcopy(self.contract); candidate['extra']=1
        with self.assertRaises(ValueError): batch.load(*self.store(candidate), selected_binding_version=3)
        self.no_dispatch()
    def test_manifest_outer_and_byte_pins(self):
        path, pin = self.store()
        with self.assertRaises(ValueError): batch.load(path,'f'*64,selected_binding_version=3)
        self.files[self.manifest_path] = runtime.canonical(self.manifest) # equal JSON, distinct bytes
        with self.assertRaises(ValueError): batch.load(path,pin,selected_binding_version=3)
        self.no_dispatch()
    def test_forged_manifest_self_hash_cannot_delegate(self):
        candidate=copy.deepcopy(self.contract)
        candidate['runtime']['admitted_selection']['bundle_byte_sha256']='f'*64
        candidate['runtime_sha256']=runtime.digest(runtime.canonical(candidate['runtime']))
        with self.assertRaises(ValueError): batch.load(*self.store(candidate),selected_binding_version=3)
        self.no_dispatch()
    def test_complete_receipt_envelope_and_bundle_transitive(self):
        admitted=self.manifest['admitted_selection']
        bundle=runtime.source_inventory.strict_json(self.files[admitted['bundle_path']])
        for slot,pin in bundle['observations'].items():
            path=pin['receipt_path']; original=self.files[path]
            for index in range(12):
                for key,value in [('raw_stdout_bytes',99),('raw_stdout_sha256','f'*64),('stderr_bytes',9),
                                  ('stderr_sha256','f'*64),('role','wrong'),('argv',['/wrong/go']),('exit',1)]:
                    receipt=runtime.source_inventory.strict_json(original)
                    receipt['observation_envelope'][index][key]=value
                    receipt=fixture.v3._seal(receipt)
                    self.files[path]=runtime.canonical(receipt)
                    with self.subTest(slot=slot,index=index,key=key), self.assertRaises(ValueError):
                        batch.load(*self.store(),selected_binding_version=3)
                    self.no_dispatch()
            self.files[path]=original
        saved=self.files[admitted['bundle_path']]
        bundle['run_id']='forged';self.files[admitted['bundle_path']]=runtime.canonical(bundle)
        with self.assertRaises(ValueError):batch.load(*self.store(),selected_binding_version=3)
        self.files[admitted['bundle_path']]=saved
        self.no_dispatch()
    def test_authenticated_incompatible_admission_before_dispatch(self):
        original=copy.deepcopy(self.manifest)
        mutations=[('source_root','/wrong'),('source_commit','f'*40),('run_id','wrong'),
                   ('bundle_byte_sha256','f'*64),('selected_binding_version',2)]
        for key,value in mutations:
            candidate=copy.deepcopy(self.contract)
            candidate['runtime']['admitted_selection'][key]=value
            self.files[self.manifest_path]=runtime.canonical(candidate['runtime'])
            candidate['runtime_manifest_sha256']=runtime.digest(self.files[self.manifest_path])
            candidate['runtime_sha256']=runtime.digest(runtime.canonical(candidate['runtime']))
            with self.subTest(key=key),self.assertRaises(ValueError):
                batch.load(*self.store(candidate),selected_binding_version=3)
            self.no_dispatch()
        self.files[self.manifest_path]=runtime.canonical(original)+b'\n'

    def test_owned_environment_and_killable_worker_selector_deadline(self):
        env=dict(TABMAIL_R5_SELECTED_BINDING_VERSION='2',TABMAIL_R5_EXTERNAL_MANIFEST='/forged',
                 TABMAIL_R5_EXTERNAL_MANIFEST_SHA256='f'*64)
        instance=batch.Batch(self.contract, '/private/output', env, selected_binding_version=3)
        self.assertEqual(instance.env['TABMAIL_R5_SELECTED_BINDING_VERSION'],'3')
        self.assertEqual(instance.env['TABMAIL_R5_EXTERNAL_MANIFEST'],self.manifest_path)
        self.assertEqual(instance.env['TABMAIL_R5_EXTERNAL_MANIFEST_SHA256'],self.pin)
        self.assertEqual(instance.slots._value,4)
        instance.deadline=time.monotonic()+50; deadline=instance.deadline
        with mock.patch.object(batch, 'OwnedProcess') as owned, mock.patch.object(Path, 'exists', return_value=True):
            owned.return_value.finish.return_value=dict(exit_code=0,timeout=False,tail=False)
            instance.validate_inventory()
            argv=owned.call_args.args[0]
            self.assertEqual(argv[-2:],['--selected-binding-version','3'])
            self.assertEqual(argv[argv.index('--pin')+1], instance.pin)
            owned.return_value.finish.assert_called_once_with(instance.abort,deadline)
            self.assertEqual(instance.deadline,deadline)
        self.no_dispatch()
    def test_worker_rejection_cancellation_and_remaining_deadline(self):
        instance=batch.Batch(self.contract,'/private/output',{},selected_binding_version=3)
        instance.deadline=time.monotonic()+30
        for result in (dict(exit_code=1,timeout=False,tail=False),dict(exit_code=0,timeout=True,tail=False),dict(exit_code=0,timeout=False,tail=True)):
            with mock.patch.object(batch,'OwnedProcess') as owned, mock.patch.object(Path, 'exists', return_value=True):
                owned.return_value.finish.return_value=result
                with self.assertRaises(ValueError):instance.validate_inventory()
        instance.abort.set()
        with mock.patch.object(batch,'OwnedProcess') as owned, mock.patch.object(Path, 'exists', return_value=True):
            with self.assertRaises(ValueError):instance.validate_inventory()
            owned.assert_not_called()
        instance.abort.clear();instance.deadline=time.monotonic()-1
        with mock.patch.object(batch,'OwnedProcess') as owned, mock.patch.object(Path, 'exists', return_value=True):
            with self.assertRaises(ValueError):instance.validate_inventory()
            owned.assert_not_called()
    def test_cli_explicit_capture_validate_run_selection(self):
        for action in ('capture','validate','run'):
            args=['batch',action,'--selected-binding-version','3','--source',fixture.ROOT,
                  '--go','/controller/tools/go','--contract','/evidence/contract.json','--pin','a'*64,'--output','/private/output']
            with mock.patch.object(sys,'argv',args), mock.patch.object(runtime,'from_environment',return_value=self.manifest) as source, mock.patch.object(batch,'capture',return_value=self.contract) as capture, mock.patch.object(batch,'validate_contract') as validate, mock.patch.object(batch,'load',return_value=self.contract) as load, mock.patch.object(batch,'Batch') as constructor, mock.patch.object(batch.signal,'signal'), mock.patch('builtins.print'), mock.patch.dict(os.environ,{'TABMAIL_R5_EXTERNAL_MANIFEST':self.manifest_path,'TABMAIL_R5_EXTERNAL_MANIFEST_SHA256':self.pin}):
                constructor.return_value.run.return_value=dict(status='BATCH_REJECTED')
                batch.main()
                if action=='capture':
                    self.assertEqual(source.call_args.kwargs['selected_binding_version'],3)
                    self.assertEqual(capture.call_args.kwargs,self.options)
                    self.assertEqual(validate.call_args.kwargs['selected_binding_version'],3)
                else:
                    self.assertEqual(load.call_args.kwargs['selected_binding_version'],3)
                    target=validate if action=='validate' else constructor
                    self.assertEqual(target.call_args.kwargs['selected_binding_version'],3)

if __name__ == '__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(BatchChecks))
    print(json.dumps(dict(tests=result.testsRun,failures=len(result.failures),errors=len(result.errors),os_process_events=EVENTS)))
    sys.exit(0 if result.wasSuccessful() and not EVENTS else 1)
