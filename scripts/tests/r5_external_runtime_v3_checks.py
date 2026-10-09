"""Explicit synthetic slice-3 checks; no discovery, captures or real processes.

All runtime producer, filesystem and process boundaries are mocked. The reviewed
adapter's complete synthetic fixture is loaded as AST functions only; its suites
are not executed. OS audit guard precedes runtime/helper imports.
"""
import sys
EVENTS = []
def guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty',
                 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        EVENTS.append(event)
        raise AssertionError('OS process forbidden: ' + event)
sys.addaudithook(guard)

import ast
import hashlib
import contextlib
import copy
import json
import os
from pathlib import Path
import subprocess
import types
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1]
sys.path.insert(0, str(SCRIPTS / 'preparation'))
import r5_external_runtime as runtime
consumer = runtime.consumer
EXCLUSIVE = runtime._exclusive
import r5_selected_source_binding_v3 as v3
ROOT = '/controller/source'
COMMIT = '4' * 40
PRODUCER = dict(path='/controller/tools/go', sha256=v3.GO_SHA256, version='go1.25.7')
wire = runtime.canonical
sha = runtime.digest
fixture_source = (SCRIPTS / 'tests/r5_selected_binding_consumer_checks.py').read_text()
functions = '\n\n'.join(ast.get_source_segment(fixture_source, node)
    for node in ast.parse(fixture_source).body if isinstance(node, ast.FunctionDef)
    and node.name in ('source_seal', 'fixture_receipt'))
exec(compile(functions, '<frozen-complete-synthetic-fixture>', 'exec'))

class MemoryDescriptors:
    def __init__(self, files): self.files = files
    def file(self, path): return self.files[str(path)]
    def directory(self, path): return 101
    def tree(self, root, *, source=False):
        return copy.deepcopy(self.files['source_tree' if source else 'installed_tree'])
    def binding(self, path): return dict(path=str(path), inode=42, uid=1000)

class RuntimeV3Checks(unittest.TestCase):
    def setUp(self):
        self.files = {'source_tree': {'bound': dict(type='file', sha256='a'*64)}, 'installed_tree': {}}
        self.source = Path(ROOT); self.root = self.source.parent
        self.default = '/evidence/input/default.json'; self.race = '/evidence/input/race.json'
        self.bundle_path = '/evidence/input/pins.json'
        self.receipts = {slot: fixture_receipt(v3.DEFAULT_CONTEXT if slot == 'default' else v3.CONTEXT)
                         for slot in consumer.ADMISSION_SLOTS}
        self.bundle = dict(schema_version=1, policy=consumer.PIN_POLICY, run_id='owned-run',
            source_commit=COMMIT, source_root=ROOT, selected_binding_version=3,
            producer=copy.deepcopy(PRODUCER), observations={})
        for slot, path in (('default',self.default), ('race-r5protocol',self.race)):
            raw = wire(self.receipts[slot]); self.files[path] = raw
            self.bundle['observations'][slot] = dict(receipt_path=path, receipt_byte_sha256=sha(raw),
                observation_sha256=v3.observation_digest(self.receipts[slot]),
                context=copy.deepcopy(self.receipts[slot]['base_source']['build_context']))
        self.repin()
        archive = dict(build_context=dict(runtime.selected.CONTEXT,build_tag_sets=[[],['r5protocol']]),
            files=self.receipts['default']['base_source']['files'])
        self.files['/evidence/archive.json'] = wire(archive)
        for path in ('/controller/package.json','/controller/package-lock.json',
                     ROOT+'/web/package.json',ROOT+'/web/package-lock.json'):
            self.files[path] = b'{}'
        for name in ('vitest.config.ts','vitest.r5protocol.config.ts','vitest.r5external-probe.config.ts','vitest.setup.ts'):
            self.files[ROOT+'/web/'+name] = b'config'
        for path in ('/controller/tools/git', '/controller/tools/python3', '/controller/tools/node',
            '/controller/node_modules/vitest/vitest.mjs',str(Path(runtime.__file__).resolve())):
            self.files[path] = b'bound tool'
        self.stack = contextlib.ExitStack(); self.addCleanup(self.stack.close)
        self.stack.enter_context(mock.patch.dict(os.environ, {}, clear=True))
        def patch(*args, **kw): return self.stack.enter_context(mock.patch(*args, **kw))
        self.desc = MemoryDescriptors(self.files)
        @contextlib.contextmanager
        def descriptors(): yield self.desc
        patch.object = lambda *a, **kw: self.stack.enter_context(mock.patch.object(*a, **kw))
        patch.object(runtime, 'descriptors', descriptors)
        patch.object(runtime.source_inventory, 'validate_current_source')
        patch.object(runtime.preparation, 'read_regular', side_effect=lambda path:self.files[str(path)])
        patch.object(runtime.preparation, 'check_lock', return_value=dict(packages={}))
        patch.object(runtime.shutil, 'which', side_effect=lambda name:'/controller/tools/'+name)
        patch.object(Path, 'exists', return_value=False)
        patch.object(Path, 'is_symlink', return_value=False)
        patch.object(Path, 'is_dir', return_value=True)
        self.process = patch.object(runtime.subprocess, 'check_output', side_effect=self.process_output)
        self.producer = patch.object(v3, 'validate', side_effect=self.fresh)
        self.v2producer = patch.object(runtime.selected, 'validate')
        self.make_dir = patch.object(runtime.tempfile, 'mkdtemp', return_value='/evidence/fresh/admission')
        self.publisher = patch.object(runtime, '_exclusive', side_effect=self.publish)
    def repin(self):
        self.files[self.bundle_path] = wire(self.bundle)
        self.input_pin = sha(self.files[self.bundle_path])
    def publish(self, path, raw):
        if str(path) in self.files: raise FileExistsError(str(path))
        self.files[str(path)] = raw
    def process_output(self, argv, **kwargs):
        if argv[1:] == ['status','--porcelain','--untracked-files=all']: return b''
        if argv[1:] == ['rev-parse','HEAD']: return COMMIT+'\n'
        if argv == ['/controller/tools/node','--version']: return 'v22.synthetic\n'
        raise AssertionError('unexpected process boundary: '+repr(argv))
    def fresh(self, receipt, source, go, **kwargs):
        self.assertEqual(go, Path(PRODUCER['path']))
        slot = 'default' if receipt['base_source']['build_context'] == v3.DEFAULT_CONTEXT else 'race-r5protocol'
        self.assertEqual(kwargs['trusted_observation_sha256'], self.bundle['observations'][slot]['observation_sha256'])
        value = copy.deepcopy(receipt)
        value['observation_envelope'][1]['raw_stdout_bytes'] += 7
        value['observation_envelope'][1]['raw_stdout_sha256'] = 'e'*64
        return v3._seal(value)
    def capture(self, **overrides):
        options = dict(cache=Path('/cache/build'), modulecache=Path('/cache/mod'), selected_binding_version=3,
            bundle_path=self.bundle_path,bundle_byte_sha256=self.input_pin,
            run_id='owned-run',source_commit=COMMIT,evidence_parent=Path('/evidence/fresh'))
        options.update(overrides)
        go = options.pop('go', Path(PRODUCER['path']))
        return runtime.capture(self.source,self.root,'/evidence/archive.json',self.default,self.race,
                               '/controller/tools/node',go,**options)
    def no_producer(self):
        self.producer.assert_not_called(); self.v2producer.assert_not_called(); self.process.assert_not_called()
    def manifest(self): return self.capture()
    def test_fresh_origin_and_immutable_inputs(self):
        inputs = {p:self.files[p] for p in (self.default,self.race,self.bundle_path)}
        manifest = self.manifest(); admitted = manifest['admitted_selection']
        self.assertEqual(manifest['schema_version'],3); self.assertEqual(manifest['policy'],runtime.V3_POLICY)
        self.assertEqual(manifest['selected_binding_version'],3)
        self.assertEqual(inputs, {p:self.files[p] for p in inputs})
        self.assertEqual(self.producer.call_count,2)
        fresh_bundle = consumer._decode(self.files[admitted['bundle_path']])
        self.assertEqual(admitted['bundle_byte_sha256'],sha(self.files[admitted['bundle_path']]))
        for slot, pin in fresh_bundle['observations'].items():
            raw = self.files[pin['receipt_path']]; receipt = consumer._decode(raw)
            self.assertEqual(pin['receipt_byte_sha256'],sha(raw))
            self.assertEqual(pin['observation_sha256'],v3.observation_digest(receipt))
            self.assertNotEqual(pin['observation_sha256'],self.bundle['observations'][slot]['observation_sha256'])
            self.assertEqual(v3.binding_payload(receipt),v3.binding_payload(self.receipts[slot]))
        self.producer.reset_mock(); self.process.reset_mock()
        runtime.validate(manifest,selected_binding_version=3)
        self.producer.assert_not_called()
        self.assertEqual(self.process.call_count,3)  # Git status/HEAD + mocked Node version only.
    def test_fresh_equal_digests_allowed(self):
        self.producer.side_effect=lambda receipt,*a,**k: copy.deepcopy(receipt)
        manifest=self.manifest()
        bundle=consumer._decode(self.files[manifest['admitted_selection']['bundle_path']])
        self.assertNotEqual(manifest['admitted_selection']['bundle_path'],self.bundle_path)
        for slot,pin in bundle['observations'].items():
            self.assertEqual(pin['observation_sha256'],self.bundle['observations'][slot]['observation_sha256'])
    def test_input_bundle_replacement_fails_before_producer(self):
        self.bundle['run_id']='forged'; self.files[self.bundle_path]=wire(self.bundle)
        with self.assertRaisesRegex(ValueError,'byte pin'): self.capture()
        self.no_producer()
    def test_complete_envelope_mutations_original_pin(self):
        original=copy.deepcopy(self.receipts['race-r5protocol'])
        mutations=[('raw_stdout_bytes',99),('raw_stdout_sha256','f'*64),('stderr_bytes',5),
            ('stderr_sha256','f'*64),('role','other'),('argv',['/bad/go']),('exit',1)]
        for index in range(12):
            for key,value in mutations:
                with self.subTest(index=index,key=key):
                    receipt=copy.deepcopy(original);receipt['observation_envelope'][index][key]=value
                    try: receipt=v3._seal(receipt)
                    except (KeyError,TypeError): pass
                    self.files[self.race]=wire(receipt)
                    with self.assertRaises(ValueError): self.capture()
                    self.no_producer()
        self.files[self.race]=wire(original)
    def test_forged_receipts_and_colocated_resealed_bundle(self):
        receipt=copy.deepcopy(self.receipts['default'])
        receipt['observation_envelope'][1]['raw_stdout_bytes']+=1; receipt=v3._seal(receipt)
        raw=wire(receipt);self.files[self.default]=raw
        self.bundle['observations']['default'].update(receipt_byte_sha256=sha(raw),observation_sha256=v3.observation_digest(receipt))
        self.files[self.bundle_path]=wire(self.bundle)
        with self.assertRaisesRegex(ValueError,'byte pin'):self.capture()
        self.no_producer()
    def test_authenticated_wrong_context_slot_and_producer(self):
        for field in ('context','receipt_path'):
            saved=copy.deepcopy(self.bundle)
            self.bundle['observations']['default'][field]=self.bundle['observations']['race-r5protocol'][field]
            self.repin()
            with self.assertRaises(ValueError):self.capture()
            self.no_producer();self.bundle=saved
        for field,value in (('path','/wrong/go'),('sha256','f'*64),('version','go1.0')):
            saved=copy.deepcopy(self.bundle);self.bundle['producer'][field]=value;self.repin()
            with self.assertRaises(ValueError):self.capture()
            self.no_producer();self.bundle=saved
    def test_wrong_controller_identity(self):
        for kwargs in (dict(run_id='wrong'),dict(source_commit='5'*40),dict(bundle_byte_sha256=None),
                       dict(source_commit=None),dict(run_id=None)):
            with self.subTest(kwargs=kwargs),self.assertRaises(ValueError):self.capture(**kwargs)
            self.no_producer()
    def test_slot_omission_extra_and_duplicate_json(self):
        for operation in ('missing','extra','duplicate'):
            b=copy.deepcopy(self.bundle)
            if operation=='missing':b['observations'].pop('race-r5protocol')
            if operation=='extra':b['observations']['before-default']=b['observations']['default']
            raw=wire(b)
            if operation=='duplicate':raw=raw.replace(b'"schema_version":1',b'"schema_version":1,"schema_version":1')
            self.files[self.bundle_path]=raw
            with self.assertRaises(ValueError):self.capture(bundle_byte_sha256=sha(raw))
            self.no_producer()
    def test_no_relative_or_in_tree_evidence(self):
        for path in (Path('relative'),Path(ROOT+'/evidence'),Path('/controller/evidence')):
            with self.subTest(path=path),self.assertRaises(ValueError):self.capture(evidence_parent=path)
            self.no_producer()
        with self.assertRaises(ValueError):self.capture(bundle_path=ROOT+'/pins.json')
        self.no_producer()
    def test_absolute_producer_and_selected_receipt_pairings(self):
        for go in ('go',Path('relative/go'),Path('/tools/../go')):
            with self.subTest(go=go),self.assertRaises(ValueError):self.capture(go=go)
            self.no_producer()
        original_bundle=copy.deepcopy(self.bundle)
        for version in (1,2,True):
            for slot,path in (('default',self.default),('race-r5protocol',self.race)):
                self.bundle=copy.deepcopy(original_bundle)
                self.files[self.default]=wire(self.receipts['default'])
                self.files[self.race]=wire(self.receipts['race-r5protocol'])
                receipt=copy.deepcopy(self.receipts[slot]);receipt['schema_version']=version
                raw=wire(v3._seal(receipt));self.files[path]=raw
                self.bundle['observations'][slot].update(receipt_byte_sha256=sha(raw),
                    observation_sha256=v3.observation_digest(consumer._decode(raw)))
                self.repin()
                with self.assertRaises(ValueError):self.capture()
                self.no_producer()
    def test_invalid_versions_zero_producer(self):
        for version in (None,True,False,1,4,3.0,'3'):
            with self.subTest(version=version),self.assertRaises(ValueError):self.capture(selected_binding_version=version)
            self.no_producer()
        with self.assertRaises(ValueError):self.capture(selected_binding_version=2)
        self.no_producer()
    def test_v2_default_and_legacy_selected_export(self):
        self.assertIs(runtime.selected,sys.modules['r5_selected_source_binding_v2'])
        self.assertEqual(runtime.selected.CONTEXT,v3.CONTEXT)
        for path,context in ((self.default,runtime.selected.DEFAULT_CONTEXT),(self.race,runtime.selected.CONTEXT)):
            self.files[path]=wire(dict(schema_version=2,policy=runtime.selected.POLICY,
                base_source=dict(build_context=context,snapshot_root=ROOT,files=self.receipts['default']['base_source']['files'])))
        value=runtime.capture(self.source,self.root,'/evidence/archive.json',self.default,self.race,
            '/controller/tools/node','go',cache='/cache/build',modulecache='/cache/mod')
        self.assertEqual(value['schema_version'],2);self.assertEqual(value['policy'],runtime.POLICY)
        self.assertNotIn('admitted_selection',value);self.producer.assert_not_called()
        self.assertEqual(self.v2producer.call_count,2)
    def test_manifest_independent_byte_pin_and_opt_in(self):
        manifest=self.manifest();raw=wire(manifest);self.files['/evidence/runtime.json']=raw
        self.producer.reset_mock();self.process.reset_mock()
        with self.assertRaises(ValueError):runtime.load_pinned('/evidence/runtime.json',sha(raw))
        self.assertEqual(runtime.load_pinned('/evidence/runtime.json',sha(raw),selected_binding_version=3),manifest)
        changed=dict(manifest,git_commit='f'*40);self.files['/evidence/runtime.json']=wire(changed)
        with self.assertRaisesRegex(ValueError,'byte pin'):runtime.load_pinned('/evidence/runtime.json',sha(raw),selected_binding_version=3)
        self.no_producer()
    def test_environment_pins_and_explicit_selector(self):
        manifest=self.manifest();raw=wire(manifest);path='/evidence/runtime.json';self.files[path]=raw
        self.producer.reset_mock();self.process.reset_mock()
        with mock.patch.dict(os.environ,dict(TABMAIL_R5_EXTERNAL_MANIFEST=path,
                TABMAIL_R5_EXTERNAL_MANIFEST_SHA256=sha(raw)),clear=True):
            with self.assertRaises(ValueError):runtime.from_environment(ROOT)
            self.assertEqual(runtime.from_environment(ROOT,selected_binding_version=3),manifest)
            with self.assertRaises(ValueError):runtime.from_environment(ROOT,selected_binding_version=None)
            with mock.patch.dict(os.environ,dict(TABMAIL_R5_SELECTED_BINDING_VERSION='3')):
                self.assertEqual(runtime.from_environment(ROOT),manifest)
                with self.assertRaises(ValueError):runtime.from_environment(ROOT,selected_binding_version=2)
        self.no_producer()
    def test_policy_schema_selector_pairings(self):
        manifest=self.manifest();self.producer.reset_mock();self.process.reset_mock()
        for selector in (1,2,3,True):
            for schema in (1,2,3,True):
                for policy in ('old',runtime.POLICY,runtime.V3_POLICY):
                    value=copy.deepcopy(manifest);value.update(schema_version=schema,policy=policy)
                    if schema==2 and type(schema) is int:
                        value.pop('admitted_selection');value.pop('selected_binding_version')
                    allowed=(type(selector) is int and selector==schema and type(schema) is int
                             and policy==(runtime.POLICY if selector==2 else runtime.V3_POLICY) and selector in (2,3))
                    with self.subTest(selector=selector,schema=schema,policy=policy):
                        if allowed:self.assertEqual(runtime._manifest_version(value,selector),selector)
                        else:
                            with self.assertRaises(ValueError):runtime._manifest_version(value,selector)
        self.no_producer()
    def test_manifest_strict_fields_and_promotion(self):
        manifest=self.manifest();self.producer.reset_mock();self.process.reset_mock()
        for field in ('schema_version','selected_binding_version','admitted_selection','receipt_paths','git_commit'):
            value=copy.deepcopy(manifest);value.pop(field)
            with self.subTest(field=field),self.assertRaises(ValueError):runtime._manifest_version(value,3)
        for field,value in (('task_complete',True),('product_green',True),('selected_binding_version',True),('extra',1)):
            bad=dict(manifest);bad[field]=value
            with self.assertRaises(ValueError):runtime._manifest_version(bad,3)
        self.no_producer()
    def test_later_metadata_tamper_stops_before_filesystem_process(self):
        manifest=self.manifest();self.producer.reset_mock();self.process.reset_mock()
        path=manifest['receipt_paths']['default'];receipt=consumer._decode(self.files[path])
        receipt['observation_envelope'][1]['raw_stdout_bytes']+=8
        self.files[path]=wire(v3._seal(receipt))
        with self.assertRaises(ValueError):runtime.validate(manifest,selected_binding_version=3)
        self.no_producer()
    def test_later_bundle_replacement_rejects_manifest_delegation(self):
        manifest=self.manifest();self.producer.reset_mock();self.process.reset_mock()
        path=manifest['admitted_selection']['bundle_path'];b=consumer._decode(self.files[path]);b['run_id']='other'
        self.files[path]=wire(b)
        with self.assertRaises(ValueError):runtime.validate(manifest,selected_binding_version=3)
        self.no_producer()
    def test_filesystem_and_actual_commit_drift(self):
        manifest=self.manifest();self.producer.reset_mock()
        self.files['source_tree']['bound']['inode']=99
        with self.assertRaisesRegex(ValueError,'drift'):runtime.validate(manifest,selected_binding_version=3)
        self.producer.assert_not_called()
        self.files['source_tree'].pop('bound')
        with mock.patch.object(runtime.subprocess,'check_output',side_effect=lambda argv,**k:'5'*40 if argv[1]=='rev-parse' else self.process_output(argv,**k)):
            with self.assertRaisesRegex(ValueError,'commit'):runtime.validate(manifest,selected_binding_version=3)
    def test_retention_and_publication_fail_closed(self):
        for fail_at in (1,2,3):
            self.publisher.reset_mock();self.producer.reset_mock()
            for path in list(self.files):
                if path.startswith('/evidence/fresh/admission/'):del self.files[path]
            calls=[]
            def fail(path,raw):
                calls.append(path)
                if len(calls)==fail_at:raise OSError(28,'synthetic ENOSPC')
                self.publish(path,raw)
            self.publisher.side_effect=fail
            with self.assertRaises(OSError):self.capture()
            self.assertEqual(self.producer.call_count,min(fail_at,2))
            self.assertNotIn('/evidence/fresh/admission/pins.json',self.files)
    def test_directory_construction_fails_before_producer(self):
        self.make_dir.side_effect=OSError(28,'ENOSPC')
        with self.assertRaises(OSError):self.capture()
        self.no_producer()
    def test_second_producer_failure_retains_first_and_preserves_exception(self):
        error=subprocess.TimeoutExpired(['/absolute/go'],1,output=b'out',stderr=b'err')
        self.producer.side_effect=[self.fresh(self.receipts['default'],self.source,Path(PRODUCER['path']),
            trusted_observation_sha256=self.bundle['observations']['default']['observation_sha256']),error]
        with self.assertRaises(subprocess.TimeoutExpired) as caught:self.capture()
        self.assertIs(caught.exception,error)
        self.assertIn('/evidence/fresh/admission/default.json',self.files)
        self.assertNotIn('/evidence/fresh/admission/pins.json',self.files)
    def test_binding_mismatch_primary_with_retention_failure(self):
        changed=copy.deepcopy(self.receipts['default']);changed['observation_envelope'][0]['stderr_bytes']=1
        changed['observation_envelope'][0]['stderr_sha256']='f'*64
        self.producer.side_effect=None;self.producer.return_value=v3._seal(changed)
        secondary=OSError(28,'ENOSPC');self.publisher.side_effect=secondary
        with self.assertRaisesRegex(ValueError,'binding differs') as caught:self.capture()
        self.assertIs(caught.exception.retention_errors[0],secondary)
    def test_corrupt_fresh_return_seal_never_published(self):
        value=copy.deepcopy(self.receipts['default']);value['observation_sha256']='f'*64
        self.producer.side_effect=None;self.producer.return_value=value
        with self.assertRaises(ValueError):self.capture()
        self.publisher.assert_not_called()
    def test_clean_environment_and_dependency_boundaries(self):
        for key in ('NODE_PATH','NODE_OPTIONS','npm_config_registry'):
            with mock.patch.dict(os.environ,{key:'polluted'}),self.assertRaises(ValueError):runtime.clean_environment()
        with self.assertRaisesRegex(ValueError,'nonoptional'):
            runtime.dependency_records({},dict(packages={'node_modules/tool':dict(version='1')}))
        result=runtime.dependency_records({},dict(packages={'node_modules/tool':dict(version='1',optional=True)}))
        self.assertEqual(result['absent_optional'],['node_modules/tool'])
        with self.assertRaisesRegex(ValueError,'undeclared'):
            runtime.dependency_records({'bad':dict(type='link',target='/outside')},dict(packages={}))
        with self.assertRaisesRegex(ValueError,'unknown installed'):
            runtime.dependency_records({'tool/node_modules/unknown/package.json':dict(type='file')},dict(packages={}))
    def test_launch_prepost_success_and_errors(self):
        manifest=self.manifest();self.producer.reset_mock()
        command=[manifest['node']['path'],manifest['cli']['path'],*manifest['execution']['probe_argv'][:-1],'/reports/new.json']
        @contextlib.contextmanager
        def owner(value):yield 'nonce'
        with mock.patch.object(runtime,'owner',owner),mock.patch.object(runtime,'validate') as validate,\
             mock.patch.object(runtime.subprocess,'run') as run:
            result=subprocess.CompletedProcess(command,0,'out','err');run.return_value=result
            self.assertIs(runtime.launch(manifest,command,env={},selected_binding_version=3),result)
            self.assertEqual(validate.call_count,2)
            self.assertEqual(run.call_args.kwargs['timeout'],180)
            for error in (subprocess.TimeoutExpired(command,180,output='out',stderr='err'),OSError('execution failed')):
                post=ValueError('postcheck');validate.reset_mock();validate.side_effect=[None,post];run.side_effect=error
                with self.assertRaises(type(error)) as caught:runtime.launch(manifest,command,env={},selected_binding_version=3)
                self.assertIs(caught.exception,error);self.assertIs(error.postcheck_error,post)
            validate.side_effect=[None,ValueError('postcheck')];run.side_effect=None
            run.return_value=subprocess.CompletedProcess(command,7,'out','err')
            with self.assertRaises(subprocess.CalledProcessError) as caught:runtime.launch(manifest,command,env={},selected_binding_version=3)
            self.assertEqual((caught.exception.returncode,caught.exception.stdout,caught.exception.stderr),(7,'out','err'))
            self.assertIsInstance(caught.exception.postcheck_error,ValueError)
            validate.side_effect=ValueError('precheck');run.reset_mock()
            with self.assertRaises(ValueError):runtime.launch(manifest,command,env={},selected_binding_version=3)
            run.assert_not_called()
        self.producer.assert_not_called()
    def test_exclusive_publication_descriptor_flags_and_readback(self):
        info=types.SimpleNamespace(st_dev=1,st_ino=2,st_mode=0o100400,st_size=5,st_mtime_ns=6,st_ctime_ns=7)
        @contextlib.contextmanager
        def descriptors():
            yield types.SimpleNamespace(directory=lambda path:101,
                read=lambda parent,name:b'owned',file=lambda path:b'owned')
        for failure in (None,'write','identity','readback','open'):
            stream=mock.MagicMock();stream.__enter__.return_value=stream;stream.fileno.return_value=8
            if failure=='write':stream.write.side_effect=OSError(28,'ENOSPC')
            after=copy.copy(info)
            if failure=='identity':after.st_ino+=1
            with mock.patch.object(runtime,'descriptors',descriptors), \
                 mock.patch.object(runtime.os,'open',return_value=7) as opened, \
                 mock.patch.object(runtime.os,'dup',return_value=8), \
                 mock.patch.object(runtime.os,'fdopen',return_value=stream), \
                 mock.patch.object(runtime.os,'fsync'), \
                 mock.patch.object(runtime.os,'fstat',return_value=info), \
                 mock.patch.object(runtime.os,'stat',return_value=after), \
                 mock.patch.object(runtime.os,'close') as closed:
                if failure=='open':opened.side_effect=FileExistsError('existing immutable receipt')
                if failure=='readback':
                    @contextlib.contextmanager
                    def wrong():
                        yield types.SimpleNamespace(directory=lambda path:101,
                            read=lambda parent,name:b'wrong',file=lambda path:b'owned')
                    with mock.patch.object(runtime,'descriptors',wrong),self.assertRaises(ValueError):EXCLUSIVE('/evidence/new.json',b'owned')
                elif failure:
                    with self.assertRaises((OSError,ValueError)):EXCLUSIVE('/evidence/new.json',b'owned')
                else:EXCLUSIVE('/evidence/new.json',b'owned')
                self.assertEqual(opened.call_args.args[0],'new.json')
                self.assertTrue(opened.call_args.args[1] & os.O_EXCL)
                self.assertTrue(opened.call_args.args[1] & os.O_NOFOLLOW)
                self.assertEqual(opened.call_args.args[2],0o400)
                self.assertEqual(opened.call_args.kwargs,dict(dir_fd=101))
                if failure=='open':closed.assert_not_called()
                else:closed.assert_called_once_with(7)
    def test_no_follow_descriptor_file_and_directory_invariants(self):
        directory=types.SimpleNamespace(st_dev=1,st_ino=1,st_mode=0o40700,st_size=0,st_mtime_ns=1,st_ctime_ns=1)
        regular=types.SimpleNamespace(st_dev=1,st_ino=2,st_mode=0o100600,st_size=5,st_mtime_ns=1,st_ctime_ns=1)
        with mock.patch.object(runtime.os,'open',return_value=101) as opened, \
             mock.patch.object(runtime.os,'stat',return_value=directory), \
             mock.patch.object(runtime.os,'fstat',return_value=directory), \
             mock.patch.object(runtime.os,'close'):
            d=runtime.Descriptors();d.directory('/owned/source');d.close()
            for call in opened.call_args_list:
                self.assertTrue(call.args[1] & os.O_NOFOLLOW)
                self.assertTrue(call.args[1] & os.O_DIRECTORY)
            for bad in ('relative','/owned/../source'):
                with self.assertRaises(ValueError):runtime.Descriptors().directory(bad)
        for failure in (None,'symlink','replacement','changed','short'):
            before=copy.copy(regular);after=copy.copy(regular)
            if failure=='symlink':before.st_mode=0o120777
            if failure=='replacement':after.st_ino=99
            stream=mock.MagicMock();stream.__enter__.return_value=stream
            stream.read.return_value=b'x' if failure=='short' else b'owned'
            states=[before,after] if failure=='changed' else [before,before]
            if failure=='changed':after.st_ctime_ns=99
            with mock.patch.object(runtime.os,'stat',side_effect=states), \
                 mock.patch.object(runtime.os,'open',return_value=101) as opened, \
                 mock.patch.object(runtime.os,'fstat',return_value=after), \
                 mock.patch.object(runtime.os,'dup',return_value=102), \
                 mock.patch.object(runtime.os,'fdopen',return_value=stream), \
                 mock.patch.object(runtime.os,'close'):
                if failure:
                    with self.assertRaises(ValueError):runtime.Descriptors().read(7,'receipt.json')
                else:self.assertEqual(runtime.Descriptors().read(7,'receipt.json'),b'owned')
                if failure=='symlink':opened.assert_not_called()
                else:self.assertTrue(opened.call_args.args[1] & os.O_NOFOLLOW)
    def test_environment_selector_strict_default(self):
        with mock.patch.dict(os.environ,{},clear=True):self.assertEqual(runtime.environment_version(),2)
        for value in ('3','2','true','1','03','3.0',''):
            with mock.patch.dict(os.environ,{'TABMAIL_R5_SELECTED_BINDING_VERSION':value}):
                if value in ('2','3'):self.assertEqual(runtime.environment_version(),int(value))
                else:
                    with self.assertRaises(ValueError):runtime.environment_version()
    def test_cli_controls_are_explicit_and_disjoint(self):
        for name in ('r5_external_runtime.py','run_r5_external_scoped.py','probe_r5_external_runtime.py'):
            tree=ast.parse((SCRIPTS/'preparation'/name).read_text())
            choices=[n for n in ast.walk(tree) if isinstance(n,ast.Call) and isinstance(n.func,ast.Attribute)
                and n.func.attr=='add_argument' and n.args and isinstance(n.args[0],ast.Constant)
                and n.args[0].value=='--selected-binding-version']
            self.assertEqual(len(choices),1)
            self.assertEqual({k.arg:ast.literal_eval(k.value) for k in choices[0].keywords if k.arg in ('choices','default')},dict(choices=(2,3),default=2))

if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(RuntimeV3Checks))
    print(json.dumps(dict(tests=result.testsRun,failures=len(result.failures),errors=len(result.errors),
                         os_process_events=EVENTS,synthetic_only=True),sort_keys=True))
    if EVENTS or not result.wasSuccessful():raise SystemExit(1)
