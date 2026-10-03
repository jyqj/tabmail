"""Fresh archive v2 actual commands and adversarial selected metadata fixtures."""
import copy
import hashlib
import os
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest
from unittest import mock
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import r5_selected_source_binding_v2 as binding
import r5_selected_source_binding as historical
import test_r5_archive_boundary as fixtures
ROOT=Path(__file__).resolve().parents[2]
GO=Path(shutil.which(os.environ.get('R5_TEST_GO','go')) or '/workspace/tabmail-cloud/tools/go/bin/go')
CACHE=Path(os.environ.get('R5_TEST_CACHE') or subprocess.check_output([str(GO),'env','GOCACHE'],text=True).strip())
MODULECACHE=Path(os.environ.get('R5_TEST_MODULECACHE') or subprocess.check_output([str(GO),'env','GOMODCACHE'],text=True).strip())

class SelectedV2NegativeTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);fixtures.fixture(self.root)
        self.row=dict(Dir=str(self.root/'cmd'),Name='main',ImportPath='tabmail/cmd',Module=dict(Path='tabmail',Main=True,Dir=str(self.root)),GoFiles=['main.go'])

    def test_metadata_never_authorizes_private_archive_or_unknown_body(self):
        for name in ['private.env','../go.mod','/etc/passwd',str(self.root/next(iter(binding.STATIC_EVIDENCE)))]:
            row=copy.deepcopy(self.row);row['GoFiles']=[name]
            with self.subTest(name=name),self.assertRaises(ValueError):binding.classify([row],self.root,{'cmd/main.go'},{'GOCACHE':'/tmp/cache'},[])

    def test_selected_archive_package_rejected(self):
        row=copy.deepcopy(self.row);row['Dir']=str((self.root/next(iter(binding.STATIC_EVIDENCE))).parent)
        with self.assertRaises(ValueError):binding.classify([row],self.root,set(binding.STATIC_EVIDENCE),{},[])

    def test_cross_identity_rejected_before_commands(self):
        for policy in [historical.POLICY,binding.inventory.POLICY,binding.inventory.CURRENT_POLICY,'r5_root_selected_local_attestation_v1_stable_capture_rev2']:
            with self.subTest(policy=policy),self.assertRaises(ValueError):binding.validate({'policy':policy,'schema_version':2},self.root,GO,cache=CACHE,modulecache=MODULECACHE)
        with self.assertRaises(ValueError):binding.inventory.validate_current_source({'schema_version':3},self.root,purpose='selected',policy=binding.inventory.ARCHIVE_POLICY)

    def test_coverage_omission_fork_mismatch_unknown_root(self):
        with self.assertRaises(ValueError):binding.compare_coverage([self.row],[],self.root,{'production_go':{'cmd/main.go'},'production_variant_directories':['cmd']},{'cmd/main.go':['GoFiles']})
        for d in ['third_party/go-smtp','newroot']:
            row=copy.deepcopy(self.row);row['Dir']=str(self.root/d)
            with self.subTest(d=d),self.assertRaises(ValueError):binding.package_identity([row],self.root)

    def test_unsupported_context(self):
        with self.assertRaises(ValueError):binding.capture(self.root,GO,cache=CACHE,modulecache=MODULECACHE,context=dict(binding.CONTEXT,goos='darwin'))

class ActualRootBindingV2Tests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.receipts={name:binding.capture(ROOT,GO,cache=CACHE,modulecache=MODULECACHE,context=context) for name,context in [('default',binding.DEFAULT_CONTEXT),('race-r5protocol',binding.CONTEXT)]}

    def test_actual_default_and_race_production_fork_coverage(self):
        for name,r in self.receipts.items():
            with self.subTest(context=name):
                self.assertEqual(r['schema_version'],2);self.assertEqual(r['base_source']['schema_version'],4)
                self.assertEqual(len(r['archive_static']),36);self.assertFalse(set(r['archive_static'])&set(r['selected_local']))
                self.assertEqual(r['production_coverage']['root_packages'],r['production_coverage']['explicit_packages'])
                self.assertTrue(r['production_coverage']['root_packages'])
                self.assertEqual(r['go_env']['GOVERSION'],'go1.25.7')
                self.assertEqual(r['hydration_diagnostics']['role'],'unbound_dependency_hydration_not_attested')
                self.assertEqual(len(r['production_coverage']['variant_directory_records']),len(r['base_source']['archive_boundary']['production_variant_directories']))
                for fork in ['third_party/go-smtp/','third_party/enmime-v2.3.0/']:
                    self.assertTrue(any(p.startswith(fork) for p in r['selected_local']))
                self.assertEqual({p:row['sha256'] for p,row in r['selected_local'].items()},binding.digest(ROOT,r['selected_local']))

    def test_real_receipt_roundtrip(self):
        for r in self.receipts.values():
            self.assertEqual(binding.validate(r,ROOT,GO,cache=CACHE,modulecache=MODULECACHE)['attestation_sha256'],r['attestation_sha256'])

    def test_capture_marker_registry_archive_source_drift(self):
        actual=binding.digest;calls=0
        def drift(root,names):
            nonlocal calls
            result=actual(root,names);calls+=1
            if calls==3:result[next(iter(result))]='a'*64
            return result
        with mock.patch.object(binding,'digest',side_effect=drift),self.assertRaisesRegex(ValueError,'changed'):
            binding.capture(ROOT,GO,cache=CACHE,modulecache=MODULECACHE)

    def test_live_capture_control_and_archive_mutations(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)/'checkout'
            subprocess.run(['git','clone','--quiet','--no-hardlinks',str(ROOT),str(root)],check=True)
            contract=binding.boundary.registry(root)
            for name in [contract['modules'][0]['path'],binding.boundary.REGISTRY,contract['historical_files'][0]['path'],'cmd/tabmail/main.go']:
                p=root/name
                if not p.exists():
                    name=next(p for p in binding.boundary.check(root)['production_go'] if p.startswith('cmd/'));p=root/name
                raw=p.read_bytes();actual=subprocess.run;changed=False
                def mutate(argv,*args,**kwargs):
                    nonlocal changed
                    result=actual(argv,*args,**kwargs)
                    if len(argv)>1 and argv[1]=='env' and not changed:
                        changed=True;p.write_bytes(raw+b'\n')
                    return result
                try:
                    with self.subTest(name=name),mock.patch.object(binding.subprocess,'run',side_effect=mutate),self.assertRaises(ValueError):
                        binding.capture(root,GO,cache=CACHE,modulecache=MODULECACHE)
                    self.assertTrue(changed)
                finally:p.write_bytes(raw)

    def test_dirty_ignored_go_actual_selected_and_rejected(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)/'checkout';subprocess.run(['git','clone','--quiet','--no-hardlinks',str(ROOT),str(root)],check=True)
            p=root/'web/node_modules/flatted/golang/pkg/flatted/flatted.go';p.parent.mkdir(parents=True);p.write_text('package flatted\n')
            result=subprocess.run([str(GO),'list','-mod=readonly','./...'],cwd=root,env={**os.environ,'GOMODCACHE':str(MODULECACHE),'GOCACHE':str(CACHE),'GOWORK':'off','GOENV':'off','GOFLAGS':''},capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr);self.assertIn('tabmail/web/node_modules/flatted/golang/pkg/flatted',result.stdout)
            with self.assertRaises(ValueError):binding.capture(root,GO,cache=CACHE,modulecache=MODULECACHE)

    def test_real_unimported_package_and_undefined_build(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp)/'checkout'
            subprocess.run(['git','clone','--quiet','--no-hardlinks',str(ROOT),str(root)],check=True)
            self.assertEqual(subprocess.check_output(['git','-C',str(root),'status','--porcelain'],text=True),'')
            p=root/'internal/r5boundaryfixture/standalone.go';p.parent.mkdir();p.write_text('package r5boundaryfixture\nconst Visible = 1\n')
            env={**os.environ,'GOCACHE':str(CACHE),'GOMODCACHE':str(MODULECACHE),'GOWORK':'off','GOENV':'off','GOFLAGS':'','GOTOOLCHAIN':'local','GODEBUG':'asynctimerchan=0'}
            listed=subprocess.run([str(GO),'list','-mod=readonly','./...'],cwd=root,env=env,capture_output=True,text=True)
            self.assertEqual(listed.returncode,0,listed.stderr);self.assertIn('tabmail/internal/r5boundaryfixture',listed.stdout)
            result=subprocess.run([str(GO),'build','-mod=readonly','./...'],cwd=root,env=env,capture_output=True,text=True)
            self.assertEqual(result.returncode,0,result.stderr)
            self.assertIn('internal/r5boundaryfixture/standalone.go',binding.boundary.check(root)['production_go'])
            p.write_text('package r5boundaryfixture\nvar Broken = undefinedBoundarySentinel\n')
            result=subprocess.run([str(GO),'build','-mod=readonly','./...'],cwd=root,env=env,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertIn('undefinedBoundarySentinel',result.stderr)
            # Losing a marker restores actual archive candidates and red build.
            p.unlink();p.parent.rmdir();(root/'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/go.mod').unlink()
            result=subprocess.run([str(GO),'build','-mod=readonly','./...'],cwd=root,env=env,capture_output=True,text=True)
            self.assertNotEqual(result.returncode,0);self.assertIn('docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/',result.stderr)

if __name__=='__main__':unittest.main()
