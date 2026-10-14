"""Pure v2 source-policy bindings. Fixtures are not product/Go/PG evidence."""
import copy
import hashlib
import importlib.util
import json
import io
import sys
from pathlib import Path
import tempfile
import unittest
from unittest import mock

SCRIPTS = Path(__file__).resolve().parents[1]

def load(name, path):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module

inventory = load('current_inventory', SCRIPTS/'r5_source_inventory.py')
protocol = load('current_protocol', SCRIPTS/'check_r5_protocol.py')
bench = load('current_benchmark', SCRIPTS/'run_r5_benchmark.py')


def context(purpose='benchmark'):
    return {'goos':'linux', 'goarch':'amd64', 'cgo_enabled':1 if purpose=='protocol' else 0,
            'build_tag_sets':[['r5protocol']] if purpose=='protocol' else [['r5benchmark']],
            'race':purpose=='protocol', 'go_work':'off', 'go_flags':'',
            'selection':'all_local_variants_superset'}


def fixture(root):
    fork = inventory.REPLACEMENT['path']
    files = inventory.ROOT_FILES | set().union(*inventory.FIXED.values()) | {
        'scripts/r5_source_inventory.py', 'scripts/tests/test_r5_current_source_inventory.py',
        'scripts/check_r5_protocol.py', 'scripts/run_r5_benchmark.py',
        'scripts/tests/test_r5_benchmark.py', 'scripts/tests/test_r5_protocol.py',
        'scripts/tests/test_r5_protocol_component_evidence.py',
        'scripts/tests/test_r5_protocol_source_inventory.py',
        'internal/api/router.go', 'internal/api/docsassets/a.js', 'internal/api/docsassets/a.css',
        'internal/api/openapi.yaml', 'cmd/main.go', 'web/package.json', 'web/package-lock.json',
        fork+'/go.mod', fork+'/go.sum', fork+'/PROVENANCE.json', fork+'/UPSTREAM-MANIFEST.json',
        fork+'/UPSTREAM-DELTA.patch', fork+'/LICENSE', fork+'/budget.go', fork+'/data/sample.eml',
    }
    for name in files:
        path = root/name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text('package fixture\n' if name.endswith('.go') else 'fixture bytes\n')
    (root/'go.mod').write_text('module tabmail\n\ngo 1.25.7\n\nrequire github.com/jhillyerd/enmime/v2 v2.3.0\nreplace github.com/jhillyerd/enmime/v2 v2.3.0 => ./'+fork+'\n')
    (root/fork/'go.mod').write_text('module github.com/jhillyerd/enmime/v2\n\ngo 1.24.3\n')
    (root/'internal/api/router.go').write_text('package api\n//go:embed docsassets/*.js docsassets/*.css\nvar docs string\n//go:embed openapi.yaml\nvar spec string\n')
    (root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').write_text('{"cases":[]}')


class CurrentSourceInventoryTests(unittest.TestCase):
    def setUp(self):
        temp = tempfile.TemporaryDirectory()
        self.addCleanup(temp.cleanup)
        self.root = Path(temp.name).resolve()/'src'
        self.root.mkdir()
        fixture(self.root)

    def capture(self, purpose='benchmark', **kwargs):
        return inventory.capture_current_source(self.root, purpose=purpose, policy=inventory.POLICY,
                                                build_context=kwargs.get('build_context', context(purpose)))

    def validate(self, receipt, purpose='benchmark'):
        return inventory.validate_current_source(receipt, self.root, purpose=purpose, policy=inventory.POLICY)

    def test_superset_includes_fork_embed_metadata_helper_and_new_identity_domain(self):
        receipt = self.capture()
        self.assertEqual(self.validate(receipt), receipt)
        required = {'third_party/enmime-v2.3.0/budget.go','third_party/enmime-v2.3.0/PROVENANCE.json',
                    'internal/api/docsassets/a.js','internal/api/docsassets/a.css',
                    'scripts/r5_source_inventory.py','scripts/tests/test_r5_current_source_inventory.py','Dockerfile'}
        self.assertTrue(required.issubset(receipt['files']))
        legacy = hashlib.sha1(inventory.canonical(receipt['files'])).hexdigest()
        self.assertNotEqual(receipt['source_sha'], legacy)
        self.assertEqual(receipt['source_identity_kind'], inventory.KIND)
        self.assertIn('not Go-selected', receipt['boundary'])
        other = context(); other['goos']='darwin'; other['goarch']='arm64'
        self.assertNotEqual(receipt['source_sha'], self.capture(build_context=other)['source_sha'])

    def test_fork_embed_and_build_metadata_drift_are_rejected_before_after(self):
        for name in ['third_party/enmime-v2.3.0/budget.go','third_party/enmime-v2.3.0/data/sample.eml',
                     'internal/api/docsassets/a.js','internal/api/docsassets/a.css','Dockerfile',
                     'scripts/r5_source_inventory.py']:
            before = self.capture(); path = self.root/name; original=path.read_bytes()
            path.write_bytes(original+b'\n')
            with self.subTest(name=name), self.assertRaisesRegex(ValueError,'drifted'):
                self.validate(before)
            self.assertNotEqual(before['source_sha'], self.capture()['source_sha'])
            path.write_bytes(original)

    def test_omitted_fork_extra_escape_and_unknown_policy_cannot_reseal(self):
        receipt = self.capture()
        mutations = [lambda r:r['files'].pop('third_party/enmime-v2.3.0/budget.go'),
                     lambda r:r['files'].__setitem__('../escape.go','a'*64),
                     lambda r:r['files'].__setitem__('internal/extra.go','a'*64),
                     lambda r:r.__setitem__('policy','unknown'), lambda r:r.pop('policy'),
                     lambda r:r.__setitem__('schema_version',1), lambda r:r.__setitem__('unknown',True)]
        for change in mutations:
            altered = copy.deepcopy(receipt); change(altered)
            with self.subTest(receipt=altered), self.assertRaises(ValueError):self.validate(altered)
        with self.assertRaises(ValueError):inventory.capture_current_source(self.root,purpose='benchmark',policy=None,build_context=context())

    def test_replacement_path_version_dynamic_workfile_and_extra_directory_fail_closed(self):
        path=self.root/'go.mod'; original=path.read_text()
        for replacement in ['./third_party/other','../outside','/outside','${FORK}','github.com/other/mod v1.0.0']:
            path.write_text(original.replace('./third_party/enmime-v2.3.0',replacement))
            with self.subTest(replacement=replacement),self.assertRaises(ValueError):self.capture()
        path.write_text(original.replace('replace github.com/jhillyerd/enmime/v2 v2.3.0','replace github.com/jhillyerd/enmime/v2 v2.3.1'))
        with self.assertRaises(ValueError):self.capture()
        path.write_text(original+'replace example.org/other => ./other\n')
        with self.assertRaises(ValueError):self.capture()
        path.write_text(original)
        (self.root/'go.work').write_text('go 1.25.7\nuse ../outside')
        with self.assertRaises(ValueError):self.capture()
        (self.root/'go.work').unlink()
        (self.root/'third_party/extra').mkdir()
        with self.assertRaises(ValueError):self.capture()

    def test_missing_symlink_and_new_disk_input_are_not_silently_ignored(self):
        before=self.capture(); name='third_party/enmime-v2.3.0/budget.go'; path=self.root/name; data=path.read_bytes()
        path.unlink()
        with self.assertRaises(ValueError):self.validate(before)
        path.symlink_to(self.root/'cmd/main.go')
        with self.assertRaises(ValueError):self.capture()
        path.unlink();path.write_bytes(data)
        (self.root/'internal/new.go').write_text('package internal\n')
        with self.assertRaises(ValueError):self.validate(before)
        (self.root/'internal/new.go').unlink()
        (self.root/'web/node_modules').symlink_to(self.root/'internal',target_is_directory=True)
        with self.assertRaises(ValueError):self.capture()

    def test_symlinked_root_external_local_import_and_adapter_omission_rejected(self):
        alias=self.root.parent/'alias';alias.symlink_to(self.root,target_is_directory=True)
        with self.assertRaises(ValueError):inventory.capture_current_source(alias,purpose='benchmark',policy=inventory.POLICY,build_context=context())
        (self.root/'cmd/main.go').write_text('package main\nimport ( // closing ) inside comment is not a delimiter\n _ "tabmail/omitted"\n)\n')
        with self.assertRaises(ValueError):self.capture()
        (self.root/'cmd/main.go').write_text('package main\n')
        cases=self.root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'
        cases.write_text(json.dumps({'cases':[{'shared_adapters':[{'source':'outside/adapter.go'}]}]}))
        with self.assertRaises(ValueError):self.capture('protocol')

    def test_output_exclusions_cannot_hide_embed_inputs(self):
        before=self.capture()
        for name in ['web/.next/output.js','web/node_modules/dependency/a.js','scripts/__pycache__/a.pyc']:
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('output')
        self.assertEqual(before,self.capture())
        (self.root/'internal/api/router.go').write_text('package api\n//go:embed missing/*.js\nvar bad string\n')
        with self.assertRaises(ValueError):self.capture()
        (self.root/'internal/api/docsassets/generated.log').write_text('embedded input')
        (self.root/'internal/api/router.go').write_text('package api\n//go:embed docsassets\nvar bad string\n')
        with self.assertRaises(ValueError):self.capture()

    def test_unknown_build_conditions_and_environment_are_rejected(self):
        for key,value in [('build_tag_sets',[['unknown']]),('selection','selected_compiler_inputs'),
                          ('go_flags','-overlay=/elsewhere'),('go_work','auto'),('cgo_enabled',True),('goos','unknown')]:
            candidate=context();candidate[key]=value
            with self.subTest(key=key),self.assertRaises(ValueError):self.capture(build_context=candidate)
        (self.root/'cmd/main.go').write_text('//go:build unknown_dynamic_tag\n\npackage main\n')
        with self.assertRaises(ValueError):self.capture()
        for key in ['GOFLAGS','GOWORK','CC','GOEXPERIMENT']:
            with self.subTest(key=key),self.assertRaises(ValueError):inventory.bound_environment(context(),{key:'unbound'})
        env=inventory.bound_environment(context(),{})
        self.assertEqual((env['GOOS'],env['GOARCH'],env['GOWORK'],env['GOENV']),('linux','amd64','off','off'))

    def test_pinned_v2_protocol_and_benchmark_current_entrypoints(self):
        for purpose,module in [('protocol',protocol),('benchmark',bench)]:
            receipt=self.capture(purpose); path=self.root.parent/(purpose+'.json');path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
            self.assertEqual(inventory.load_current_source(path,pin,self.root,purpose=purpose,policy=inventory.POLICY),receipt)
            with self.assertRaises(ValueError):inventory.load_current_source(path,'0'*64,self.root,purpose=purpose,policy=inventory.POLICY)
            if purpose=='protocol':
                with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'SOURCE_POLICY',inventory.POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',path),mock.patch.object(protocol,'SOURCE_MANIFEST_SHA256',pin),mock.patch.object(protocol.subprocess,'run') as execute,mock.patch.dict(protocol.os.environ,{},clear=True):
                    self.assertEqual(protocol.source_closure(),receipt['files'])
                    self.assertEqual(protocol.protocol_source_metadata()['source_sha'],receipt['source_sha'])
                    protocol.current_protocol_environment([('r5protocol',)])
                    with self.assertRaises(ValueError):protocol.current_protocol_environment([()])
                    execute.assert_not_called()
            else:
                with mock.patch.object(bench,'ROOT',self.root):
                    self.assertEqual(bench.source_closure(policy=inventory.POLICY,build_context=context()),receipt['files'])
                    with self.assertRaises(ValueError):bench.source_closure()
                    with self.assertRaisesRegex(ValueError,'METHOD'):
                        bench.source_closure(policy=inventory.POLICY,build_context=context(),preparation_method=bench.LOOKAHEAD_METHOD)

    def test_current_tool_cli_binds_v2_source_and_rejects_live_fork_drift(self):
        receipt=self.capture(); path=self.root.parent/'manifest.json'; path.write_bytes(inventory.canonical(receipt))
        pin=hashlib.sha256(path.read_bytes()).hexdigest()
        result=self.root.parent/'tool-result.json';result.write_text('{}')
        argv=['run_r5_benchmark.py','--validate-tool-result',str(result),'--source-sha',receipt['source_sha'],
              '--source-policy',inventory.POLICY,'--source-manifest',str(path),'--source-manifest-sha256',pin]
        with mock.patch.object(bench,'ROOT',self.root),mock.patch.object(sys,'argv',argv),mock.patch.object(bench,'run_owned_process') as run,mock.patch.object(bench,'validate_tool_result',return_value={'status':'fixture_validated'}) as validate,mock.patch('sys.stdout',new_callable=io.StringIO) as output:
            self.assertEqual(bench.main(),0)
            self.assertEqual(json.loads(output.getvalue())['source_identity_kind'],inventory.KIND)
            validate.assert_called_once();run.assert_not_called()
            (self.root/'third_party/enmime-v2.3.0/budget.go').write_text('package drift\n')
            self.assertEqual(bench.main(),1);validate.assert_called_once();run.assert_not_called()

    def test_protocol_manifest_revalidation_rejects_after_run_drift_without_git(self):
        receipt=self.capture('protocol');path=self.root.parent/'protocol-manifest.json';path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
        with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'SOURCE_POLICY',inventory.POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',path),mock.patch.object(protocol,'SOURCE_MANIFEST_SHA256',pin),mock.patch.object(protocol.subprocess,'run') as run:
            self.assertEqual(protocol.source_closure(),receipt['files'])
            (self.root/'internal/api/docsassets/a.css').write_text('body { color:red }')
            with self.assertRaises(ValueError):protocol.source_closure()
            run.assert_not_called()

    def test_c_map_keys_strings_and_comments_are_not_native_imports(self):
        source=self.root/'third_party/enmime-v2.3.0/reader_fixture_test.go'
        source.write_text('package enmime\nvar want = map[string][]string{\n "A": {"0"},\n "C": {"2 3 4"},\n}\nvar label = "C"\nvar quoted = `import "C"`\n// import "C"\n/* import ( "C" ) */\n')
        receipt=self.capture()
        self.assertIn(source.relative_to(self.root).as_posix(),receipt['files'])
        self.assertEqual(self.validate(receipt),receipt)

    def test_real_native_import_and_linker_inputs_still_fail_closed(self):
        source=self.root/'third_party/enmime-v2.3.0/native_fixture.go'
        for native in ['import "C"', 'import (\n "C"\n)', 'import c "C"',
                       '//go:linkname fixture native', '//go:cgo_import_dynamic fixture native',
                       '/*\n#cgo CFLAGS: -I/outside\n*/\nimport "C"']:
            source.write_text('package enmime\n'+native+'\n')
            with self.subTest(native=native),self.assertRaisesRegex(ValueError,'unbound native'):
                self.capture()

    def test_protocol_metadata_preserves_shared_and_reference_runtime_boundaries(self):
        # Synthetic runner/classifier events test only metadata composition;
        # source capture and source manifest validation use the real fixture tree.
        receipt=self.capture('protocol');path=self.root.parent/'boundary-manifest.json'
        path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
        adapter={'source':'cmd/main.go','package':'./cmd','test':'TestFixture','layers':['unit'],'build_tag':'r5protocol'}
        data={'cases':[{'id':'FIXTURE','required_layers':['unit'],'reference_adapters':[adapter],
                        'shared_adapters':[adapter],'expected':{}}]}
        process=mock.Mock(stdout='',stderr='',returncode=0)
        red={'errors':[],'target_red':{'TestFixture/FIXTURE':'synthetic_target_red'}}
        with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'CASES',self.root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'),mock.patch.object(protocol,'SOURCE_POLICY',inventory.POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',path),mock.patch.object(protocol,'SOURCE_MANIFEST_SHA256',pin),mock.patch.dict(protocol.os.environ,{},clear=True),mock.patch.object(protocol.subprocess,'run',return_value=process) as execute,mock.patch.object(protocol,'classify_shared_events',return_value=red),mock.patch.object(protocol,'classify_events',return_value={'errors':[]}):
            shared=protocol.run_shared(data,'unit',self.root.parent/'shared-boundary')
            reference=protocol.run_references(data,'unit',self.root.parent/'reference-boundary')
            self.assertEqual(execute.call_count,2)
            self.assertIn('Only the recorded scoped layers',shared['boundary'])
            self.assertIn('Accepted target red is not product green',shared['boundary'])
            self.assertFalse(shared['product_green']);self.assertFalse(shared['task_complete'])
            self.assertEqual(reference['boundary'],protocol.summary(data)['boundary'])
            for report in [shared,reference]:
                self.assertEqual(report['source_identity_boundary'],receipt['boundary'])
                self.assertNotEqual(report['boundary'],report['source_identity_boundary'])
            _,metadata=protocol.manifest_source_closure(path,pin,self.root)
            self.assertNotIn('boundary',metadata)
            self.assertEqual(metadata['source_identity_boundary'],receipt['boundary'])

    def test_current_policy_never_enters_frozen_legacy_identity(self):
        receipt=self.capture();files=dict(receipt['files'])
        # Satisfy every frozen validator precondition through the kind gate.
        files['internal/store/postgres/r5_benchmark_test.go']='a'*64
        files.update({f'internal/store/postgres/migrations/{version:05d}_fixture.sql':'b'*64
                      for version in range(1,bench.PIPELINE_SCHEMA_VERSION+1)})
        canonical=inventory.canonical(files);digest=hashlib.sha256(canonical).hexdigest()
        source=hashlib.sha1(canonical).hexdigest()
        row={'source_tree':source,'source_identity_kind':inventory.KIND,'actual_source_closure_sha256':digest}
        review=dict(row,source_sha_semantics=bench.CANONICAL_SOURCE_SEMANTICS)
        closure={'declared_source_sha':source,'files':files,'actual_source_closure_sha256':digest,
                 'source_identity_kind':inventory.KIND}
        with self.assertRaisesRegex(ValueError,'^exact schema16 canonical closure identity required; not Git tree or commit$'):
            bench.validate_source_identity(row,review,closure,{})
        # Positive gate-position control: only kind changes, so validation now
        # reaches the later missing archival-proof check, not digest/schema gates.
        for value in [row,review,closure]:value['source_identity_kind']=bench.CANONICAL_SOURCE_KIND
        with self.assertRaisesRegex(ValueError,'^explicit archived source identity proof member required$'):
            bench.validate_source_identity(row,review,closure,{})
        with mock.patch.object(protocol,'SOURCE_POLICY',None),mock.patch.object(protocol,'SOURCE_MANIFEST',None),mock.patch.object(protocol.subprocess,'run') as execute:
            with self.assertRaises(ValueError):protocol.source_closure()
            execute.assert_not_called()


if __name__ == '__main__':
    unittest.main()
