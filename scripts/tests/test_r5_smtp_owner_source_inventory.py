"""Pure SOURCE v3 fixtures; no Go/MVS/build/test/PG/runtime is executed here.

The two tiny modules model complete declared source trees and both actual
metadata layouts. They are not upstream forks or runtime-qualified sources.
"""
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

SCRIPTS=Path(__file__).resolve().parents[1]

def load(name,file):
    spec=importlib.util.spec_from_file_location(name,SCRIPTS/file)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

inventory=load('smtp_inventory_v3','r5_source_inventory.py')
protocol=load('smtp_protocol_v3','check_r5_protocol.py')
bench=load('smtp_benchmark_v3','run_r5_benchmark.py')


def context(purpose='protocol'):
    return {'goos':'linux','goarch':'amd64','cgo_enabled':1 if purpose=='protocol' else 0,
            'build_tag_sets':[['r5protocol']] if purpose=='protocol' else [['r5benchmark']],
            'race':purpose=='protocol','go_work':'off','go_flags':'','selection':'all_local_variants_superset'}


def fixture(root, *, dual=True):
    replacements=inventory.CURRENT_REPLACEMENTS if dual else (inventory.REPLACEMENT,)
    paths=set(inventory.ROOT_FILES)|set().union(*inventory.FIXED.values())|{
        'cmd/main.go','internal/api/router.go','internal/api/docsassets/a.js','internal/api/docsassets/a.css',
        'internal/api/openapi.yaml','web/package.json','web/package-lock.json',
        'scripts/tests/test_r5_current_source_inventory.py','scripts/tests/test_r5_protocol.py',
        'scripts/tests/test_r5_protocol_source_inventory.py','scripts/tests/test_r5_protocol_component_evidence.py',
        'scripts/tests/test_r5_benchmark.py'}
    for name in paths:
        path=root/name;path.parent.mkdir(parents=True,exist_ok=True)
        path.write_text('package fixture\n' if name.endswith('.go') else 'synthetic SOURCE fixture\n')
    for name in ['r5_source_inventory.py','check_r5_protocol.py','run_r5_benchmark.py','tests/test_r5_smtp_owner_source_inventory.py']:
        dest=root/'scripts'/name;dest.parent.mkdir(parents=True,exist_ok=True);dest.write_bytes((SCRIPTS/name).read_bytes())
    requires='\n'.join(' '+item['module']+' '+item['version'] for item in replacements)
    replaces='\n'.join('replace '+item['module']+' '+item['version']+' => ./'+item['path'] for item in replacements)
    (root/'go.sum').write_text('')
    (root/'go.mod').write_text('module tabmail\ngo 1.25.7\nrequire (\n'+requires+'\n)\n'+replaces+'\n')
    (root/'cmd/main.go').write_text('package main\nimport _ "tabmail/internal/api"\n')
    imports='\n'.join(' _ "'+item['module']+'"' for item in replacements)
    (root/'internal/api/router.go').write_text('package api\nimport (\n "embed"\n'+imports+'\n)\n//go:embed docsassets/*.js docsassets/*.css\nvar docs embed.FS\n')
    (root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').write_text('{"cases":[]}')
    for item in replacements:
        base=root/item['path'];base.mkdir(parents=True,exist_ok=True)
        (base/'go.mod').write_text('module '+item['module']+'\ngo 1.25.7\n')
        (base/'go.sum').write_text('')
        (base/'LICENSE').write_text('Synthetic license fixture, not upstream evidence\n')
        (base/'README.md').write_text('Complete tiny SOURCE fixture\n')
        (base/'assets').mkdir();(base/'assets/data.txt').write_text('local added embedded fixture\n')
        (base/'module.go').write_text('package fixture\nimport "embed"\n//go:embed assets/*.txt\nvar input embed.FS\n')
        # This is a local addition; it must still be captured, not only the
        # upstream declared names or Go-suffix files.
        (base/'local-extra.bin').write_bytes(b'local non-Go source input')
        (base/'source-fixture.log').write_bytes(b'declared local source; not an assumed runtime log')
        for name in inventory.MODULE_METADATA[item['module']]:
            if name!='LICENSE':
                content=json.dumps({'fixture_only':True,'module':item['module'],'version':item['version'],'role':'synthetic SOURCE fixture, not provenance qualification'}) if name.endswith('.json') else 'Synthetic metadata '+name+'\n'
                (base/name).write_text(content)
        names=['go.mod','go.sum','LICENSE','README.md','module.go']
        hashes={name:hashlib.sha256((base/name).read_bytes()).hexdigest() for name in names}
        mapping={name:{'bytes':(base/name).stat().st_size,'sha256':digest} for name,digest in hashes.items()} if item['module']==inventory.REPLACEMENT['module'] else hashes
        field='files' if item['module']==inventory.REPLACEMENT['module'] else 'files_sha256'
        (base/'UPSTREAM-MANIFEST.json').write_text(json.dumps({'module':item['module'],'version':item['version'],field:mapping}))


class SMTPSourceInventoryV3Tests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve()/'src';self.root.mkdir();fixture(self.root)

    def capture(self,purpose='protocol'):
        return inventory.capture_current_source(self.root,purpose=purpose,policy=inventory.CURRENT_POLICY,build_context=context(purpose))

    def validate(self,receipt,purpose='protocol'):
        return inventory.validate_current_source(receipt,self.root,purpose=purpose,policy=inventory.CURRENT_POLICY)

    def positive(self,purpose='protocol'):
        receipt=self.capture(purpose);self.assertEqual(self.validate(receipt,purpose),receipt)
        self.assertEqual(receipt['schema_version'],3);self.assertEqual(receipt['policy'],inventory.CURRENT_POLICY)
        return receipt

    def test_exact_two_forks_full_inputs_and_actual_metadata_layouts(self):
        receipt=self.positive()
        self.assertEqual(receipt['replacements'],list(inventory.CURRENT_REPLACEMENTS))
        self.assertNotIn('replacement',receipt)
        self.assertEqual(receipt['source_identity_kind'],inventory.CURRENT_KIND)
        self.assertEqual(receipt['inventory_implementation_sha256'],receipt['files']['scripts/r5_source_inventory.py'])
        for item in inventory.CURRENT_REPLACEMENTS:
            base=item['path']
            for name in ['go.mod','go.sum','LICENSE','README.md','module.go','assets/data.txt','local-extra.bin','source-fixture.log',*inventory.MODULE_METADATA[item['module']]]:
                self.assertIn(base+'/'+name,receipt['files'])
            record=next(row for row in receipt['module_resolution']['local_modules'] if row['module']==item['module'])
            self.assertEqual(record['go_mod_sha256'],receipt['files'][base+'/go.mod'])
            self.assertEqual(record['go_sum_sha256'],receipt['files'][base+'/go.sum'])
            self.assertEqual(record['metadata_sha256']['LICENSE'],receipt['files'][base+'/LICENSE'])
            self.assertIn(base+'/module.go:assets/*.txt',receipt['embed_inputs'])
        resolution=receipt['module_resolution']
        self.assertFalse(resolution['effective_root_MVS_observed'])
        self.assertFalse(resolution['go_selected_inputs_observed'])
        self.assertFalse(resolution['external_module_cache_verified'])
        self.assertEqual(resolution['root_go_mod_sha256'],receipt['files']['go.mod'])
        self.assertEqual(resolution['root_go_sum_sha256'],receipt['files']['go.sum'])

    def test_unknown_third_replace_and_extra_local_tree_fail_closed(self):
        self.positive();path=self.root/'go.mod';original=path.read_text()
        path.write_text(original+'replace example.invalid/third v1.0.0 => ./third_party/third\n')
        with self.assertRaisesRegex(ValueError,'exactly the two pinned'):self.capture()
        path.write_text(original);self.positive();(self.root/'third_party/third').mkdir()
        with self.assertRaisesRegex(ValueError,'unknown extra local module entry'):self.capture()

    def test_wrong_root_versions_paths_and_dynamic_forms_rejected(self):
        path=self.root/'go.mod';original=path.read_text()
        replacements=[('require_version',original.replace(' github.com/emersion/go-smtp v0.24.0',' github.com/emersion/go-smtp v0.25.0',1),'root required local module version'),
                      ('replace_version',original.replace('replace github.com/emersion/go-smtp v0.24.0','replace github.com/emersion/go-smtp v0.25.0'),'exactly the two pinned'),
                      ('escape',original.replace('./third_party/go-smtp','../outside'),'exactly the two pinned'),
                      ('absolute',original.replace('./third_party/go-smtp','/outside'),'exactly the two pinned'),
                      ('dynamic',original.replace('./third_party/go-smtp','${SMTP_FORK}'),'exactly the two pinned'),
                      ('block',original+'replace (\n)\n','unsupported dynamic module directive')]
        for name,changed,reason in replacements:
            self.positive();path.write_text(changed)
            with self.subTest(name=name),self.assertRaisesRegex(ValueError,reason):self.capture()
            path.write_text(original)

    def test_duplicate_or_MVS_override_declarations_do_not_bypass_exact_binding(self):
        path=self.root/'go.mod';original=path.read_text()
        for text,reason in [('require github.com/emersion/go-smtp v0.24.0\n','duplicate module requirement'),('exclude github.com/emersion/go-smtp v0.24.0\n','unknown module directive'),('tool example.invalid/tool\n','unknown module directive')]:
            self.positive();path.write_text(original+text)
            with self.assertRaisesRegex(ValueError,reason):self.capture()
            path.write_text(original)

    def test_each_fork_mod_sum_license_and_required_metadata_are_mandatory(self):
        for item in inventory.CURRENT_REPLACEMENTS:
            for name in ['go.mod','go.sum',*inventory.MODULE_METADATA[item['module']]]:
                self.positive();path=self.root/item['path']/name;original=path.read_bytes();path.unlink()
                with self.subTest(module=item['module'],name=name),self.assertRaisesRegex(ValueError,'missing or escaped local source input'):self.capture()
                path.write_bytes(original)

    def test_local_module_identity_version_dependency_and_nested_replace_rejected(self):
        path=self.root/'third_party/go-smtp/go.mod';original=path.read_text()
        for changed,reason in [(original.replace('github.com/emersion/go-smtp','example.invalid/wrong'),'local module identity'),
                               (original+'replace example.invalid/other v1.0.0 => ../other\n','nested replacement differs'),
                               (original+'require github.com/jhillyerd/enmime/v2 v2.4.0\n','unadmitted local module version')]:
            self.positive();path.write_text(changed)
            with self.assertRaisesRegex(ValueError,reason):self.capture()
            path.write_text(original)

    def test_both_fork_embeds_and_nongo_upstream_inputs_are_required(self):
        for item in inventory.CURRENT_REPLACEMENTS:
            for name,reason in [('assets/data.txt','missing or excluded embed input'),('README.md','missing or escaped local source input')]:
                self.positive();path=self.root/item['path']/name;original=path.read_bytes();path.unlink()
                with self.subTest(module=item['module'],name=name),self.assertRaisesRegex(ValueError,reason):self.capture()
                path.write_bytes(original)

    def test_both_forks_root_modsum_and_metadata_drift_reject_pinned_receipt(self):
        names=['go.mod','go.sum']+[item['path']+'/'+name for item in inventory.CURRENT_REPLACEMENTS for name in ['go.mod','go.sum','module.go','assets/data.txt','local-extra.bin','LICENSE']]
        for name in names:
            before=self.positive();path=self.root/name;original=path.read_bytes();path.write_bytes(original+b'\n')
            with self.subTest(name=name),self.assertRaisesRegex(ValueError,'missing, extra, or drifted'):self.validate(before)
            path.write_bytes(original)

    def test_symlink_escape_workfile_and_unknown_build_condition_fail_closed(self):
        for name in ['third_party/go-smtp/go.sum','third_party/enmime-v2.3.0/LICENSE']:
            self.positive();path=self.root/name;original=path.read_bytes();path.unlink();path.symlink_to(self.root/'go.sum')
            with self.assertRaisesRegex(ValueError,'symlinked local source'):self.capture()
            path.unlink();path.write_bytes(original)
        self.positive();work=self.root/'go.work';work.write_text('go 1.25.7\nuse ../other\n')
        with self.assertRaisesRegex(ValueError,'workspace/vendor'):self.capture()
        work.unlink();self.positive();source=self.root/'third_party/go-smtp/unknown.go';source.write_text('//go:build unknown_owner_tag\n\npackage fixture\n')
        with self.assertRaisesRegex(ValueError,'unknown conditional build input'):self.capture()

    def test_unlisted_nested_module_and_local_import_omission_are_rejected(self):
        self.positive();nested=self.root/'third_party/go-smtp/nested/go.mod';nested.parent.mkdir();nested.write_text('module example.invalid/nested\ngo 1.25.7\n')
        with self.assertRaisesRegex(ValueError,'unknown nested module'):self.capture()
        nested.unlink();self.positive();path=self.root/'cmd/main.go';path.write_text('package main\nimport _ "github.com/emersion/go-smtp/omitted"\n')
        with self.assertRaisesRegex(ValueError,'local replacement import outside captured module inputs'):self.capture()

    def test_unknown_extra_or_omitted_receipt_fields_cannot_be_resealed(self):
        before=self.positive()
        for change in [lambda r:r['files'].pop('third_party/go-smtp/LICENSE'),lambda r:r['files'].__setitem__('../escape.go','a'*64),lambda r:r.__setitem__('unknown',True),lambda r:r['module_resolution'].__setitem__('effective_root_MVS_observed',True),lambda r:r['module_resolution'].__setitem__('effective_root_MVS_observed',0),lambda r:r['replacements'].pop()]:
            self.validate(before);changed=copy.deepcopy(before);change(changed)
            with self.assertRaisesRegex(ValueError,'missing, extra, or drifted'):self.validate(changed)

    def test_actual_loaded_helper_is_bound_not_a_different_snapshot_implementation(self):
        self.positive();path=self.root/'scripts/r5_source_inventory.py';path.write_bytes(path.read_bytes()+b'\n# drift\n')
        with self.assertRaisesRegex(ValueError,'implementation differs from snapshot helper bytes'):self.capture()

    def test_v2_single_fork_snapshot_keeps_old_domain_but_cannot_cover_dual_root(self):
        self.positive()
        with self.assertRaisesRegex(ValueError,'only exact versioned local replacement'):
            inventory.capture_current_source(self.root,purpose='protocol',policy=inventory.POLICY,build_context=context())
        single=Path(self.temp.name).resolve()/'legacy';single.mkdir();fixture(single,dual=False)
        old=inventory.capture_current_source(single,purpose='protocol',policy=inventory.POLICY,build_context=context())
        self.assertEqual(old['schema_version'],2);self.assertEqual(old['source_identity_kind'],'policy_bound_local_input_superset_sha1_v2')
        self.assertEqual(old['policy'],'r5_current_local_inputs_v2');self.assertIn('replacement',old);self.assertNotIn('replacements',old)
        self.assertEqual(inventory.validate_current_source(old,single,purpose='protocol',policy=inventory.POLICY),old)
        with self.assertRaisesRegex(ValueError,'exactly the two pinned'):
            inventory.capture_current_source(single,purpose='protocol',policy=inventory.CURRENT_POLICY,build_context=context())
        with self.assertRaisesRegex(ValueError,'policy/receipt version match'):
            inventory.validate_current_source(old,single,purpose='protocol',policy=inventory.CURRENT_POLICY)

    def test_explicit_v3_pin_and_policy_are_required_without_auto_upgrade(self):
        receipt=self.positive();path=self.root.parent/'v3.json';path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
        self.assertEqual(inventory.load_current_source(path,pin,self.root,purpose='protocol',policy=inventory.CURRENT_POLICY),receipt)
        with self.assertRaisesRegex(ValueError,'byte hash differs'):
            inventory.load_current_source(path,'0'*64,self.root,purpose='protocol',policy=inventory.CURRENT_POLICY)
        for policy in [None,'unknown',inventory.POLICY]:
            with self.subTest(policy=policy),self.assertRaisesRegex(ValueError,'policy/receipt version match'):
                inventory.load_current_source(path,pin,self.root,purpose='protocol',policy=policy)

    def test_protocol_producer_validator_and_prepost_use_v3_without_Git(self):
        receipt=self.positive();path=self.root.parent/'protocol.json';path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
        with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'SOURCE_POLICY',inventory.CURRENT_POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',path),mock.patch.object(protocol,'SOURCE_MANIFEST_SHA256',pin),mock.patch.object(protocol.subprocess,'run') as execute:
            self.assertEqual(protocol.source_closure(),receipt['files'])
            metadata=protocol.protocol_source_metadata();self.assertEqual(metadata['policy'],inventory.CURRENT_POLICY);self.assertEqual(metadata['schema_version'],3);self.assertEqual(metadata['replacements'],receipt['replacements']);self.assertNotIn('boundary',metadata)
            self.assertEqual(protocol.manifest_source_closure(path,pin,self.root)[0],receipt['files'])
            (self.root/'third_party/go-smtp/local-extra.bin').write_bytes(b'after source changed')
            with self.assertRaisesRegex(ValueError,'missing, extra, or drifted'):protocol.source_closure()
            execute.assert_not_called()

    def test_benchmark_source_producer_accepts_v3_but_no_METHOD_live_or_tool_admission(self):
        receipt=self.positive('benchmark')
        with mock.patch.object(bench,'ROOT',self.root):
            self.assertEqual(bench.current_source_receipt(policy=inventory.CURRENT_POLICY,build_context=context('benchmark')),receipt)
            self.assertEqual(bench.source_closure(policy=inventory.CURRENT_POLICY,build_context=context('benchmark')),receipt['files'])
            with self.assertRaisesRegex(ValueError,'METHOD'):
                bench.current_source_receipt(policy=inventory.CURRENT_POLICY,build_context=context('benchmark'),preparation_method=bench.LOOKAHEAD_METHOD)
        path=self.root.parent/'benchmark.json';path.write_bytes(inventory.canonical(receipt));pin=hashlib.sha256(path.read_bytes()).hexdigest()
        for action in [['--execute-approved-budget'],['--validate-tool-result',str(self.root.parent/'never-opened.json')]]:
            args=['run_r5_benchmark.py',*action,'--source-policy',inventory.CURRENT_POLICY,'--source-manifest',str(path),'--source-manifest-sha256',pin,'--source-sha',receipt['source_sha']]
            with mock.patch.object(sys,'argv',args),mock.patch.object(bench,'run_owned_process') as execute,mock.patch.object(bench,'preflight') as preflight,mock.patch.object(bench.source_inventory,'load_current_source') as read,mock.patch('sys.stdout',new_callable=io.StringIO) as output:
                self.assertEqual(bench.main(),1)
                self.assertEqual(json.loads(output.getvalue())['error'],'SMTP-owner source policy v3 is SOURCE-only here: no admitted benchmark METHOD or schema18/19-to-schema16 fallback')
                execute.assert_not_called();preflight.assert_not_called();read.assert_not_called()


if __name__=='__main__':unittest.main()
