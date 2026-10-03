"""Independent negative fixtures plus fresh actual root metadata; never Go tests."""
import copy
import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import r5_selected_source_binding as binding
import test_r5_smtp_owner_source_inventory as fixtures

ROOT = Path(__file__).resolve().parents[2]
GO = Path('/workspace/tabmail-cloud/tools/go/bin/go')
CACHE = Path('/workspace/tabmail-cloud/gocache')
MODULECACHE = Path('/workspace/tabmail-cloud/gomod')

class SelectedNegativeTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(); self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name).resolve(); fixtures.fixture(self.root)
        self.module=dict(Path='tabmail',Main=True,Dir=str(self.root))
        self.row=dict(Dir=str(self.root/'cmd'),Name='main',ImportPath='tabmail/cmd',Module=self.module,GoFiles=['main.go'])
        self.env=dict(GOCACHE='/tmp/cache',GOROOT='/tmp/go',GOMODCACHE='/tmp/mod')
        self.mvs=[dict(Path='tabmail',Main=True)]

    def classify(self,row=None,authorized=None):
        return binding.classify([row or self.row],self.root,authorized or {'cmd/main.go'},self.env,self.mvs)

    def test_forged_metadata_does_not_authorize_secret_or_evidence(self):
        for name in ['private.env','../go.mod','/etc/passwd',str(self.root/'docs/company-mail/evidence/raw.log')]:
            row=copy.deepcopy(self.row);row['GoFiles']=[name]
            with self.subTest(name=name),self.assertRaises(ValueError):self.classify(row)

    def test_missing_and_symlink_static_source(self):
        (self.root/'cmd/main.go').unlink()
        with self.assertRaises(ValueError):binding.digest(self.root,{'cmd/main.go'})
        (self.root/'cmd/main.go').symlink_to(self.root/'go.mod')
        with self.assertRaises(ValueError):binding.digest(self.root,{'cmd/main.go'})

    def test_unknown_nested_module_and_extra_go(self):
        (self.root/'cmd/nested').mkdir();(self.root/'cmd/nested/go.mod').write_text('module third\ngo 1.25.7\n')
        with self.assertRaisesRegex(ValueError,'nested'):binding.topology(self.root)
        (self.root/'cmd/nested/go.mod').unlink();(self.root/'cmd/new.go').write_text('package main\n')
        row=copy.deepcopy(self.row);row['GoFiles'].append('new.go')
        with self.assertRaisesRegex(ValueError,'unclassified'):self.classify(row)

    def test_wrongfork_and_third_replace(self):
        for suffix in ['', '\nreplace example.invalid/third v1.0.0 => ./third_party/go-smtp\n']:
            path=self.root/'go.mod';original=path.read_text()
            path.write_text(original.replace('v0.24.0 =>','v0.23.0 =>') if not suffix else original+suffix)
            with self.subTest(suffix=suffix),self.assertRaises(ValueError):binding.inventory._module_binding(self.root)
            path.write_text(original)
        row=copy.deepcopy(self.row);row['Dir']=str(self.root/'third_party/go-smtp');row['GoFiles']=['server.go']
        with self.assertRaisesRegex(ValueError,'fork'):self.classify(row,{'third_party/go-smtp/server.go'})

    def test_generated_and_external_are_not_local_read_capabilities(self):
        row=dict(Dir=str(self.root/'cmd'),Name='main',ImportPath='tabmail/cmd.test',Module=self.module,GoFiles=['/tmp/cache/aa/hash-d'])
        local,generated,external,native,toolchain=self.classify(row)
        self.assertEqual(local,{})
        self.assertEqual(generated[0]['qualification'],'unknown')
        row['GoFiles']=['/etc/passwd']
        with self.assertRaises(ValueError):self.classify(row)
        row=dict(Dir='/tmp/mod/evil',ImportPath='evil',Name='evil',Module={'Path':'evil','Version':'v1.0.0'},GoFiles=['file.go'])
        with self.assertRaises(ValueError):self.classify(row)

    def test_duplicate_metadata_and_unknown_compiled(self):
        with self.assertRaises(ValueError):binding.stream(b'{"Path":"a","Path":"b"}')
        row=copy.deepcopy(self.row);row['CompiledGoFiles']=['arbitrary.go']
        with self.assertRaises(ValueError):self.classify(row)

    def test_nested_module_in_excluded_static_output_is_rejected(self):
        path=self.root/'cmd/build';path.mkdir();(path/'go.mod').write_text('module hidden\ngo 1.25.7\n')
        with self.assertRaisesRegex(ValueError,'nested'):binding.topology(self.root)

    def test_source_hash_drift_detected(self):
        first=binding.digest(self.root,{'cmd/main.go'})
        (self.root/'cmd/main.go').write_text('package changed\n')
        self.assertNotEqual(first,binding.digest(self.root,{'cmd/main.go'}))

class ActualRootBindingTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.receipt=binding.capture(ROOT,GO,cache=CACHE,modulecache=MODULECACHE)

    def test_actual_full_scope_fresh_hashes(self):
        receipt=self.receipt
        self.assertEqual(receipt['commands'][2]['argv'][1:],binding.ARGV)
        self.assertEqual(receipt['environment']['GODEBUG'],'asynctimerchan=0')
        self.assertEqual(receipt['go_env']['GOVERSION'],'go1.25.7')
        local=receipt['selected_local']
        self.assertTrue(binding.STATIC_EVIDENCE.issubset(local))
        self.assertEqual({p:info['sha256'] for p,info in local.items()},binding.digest(ROOT,local))
        self.assertTrue(any(p.startswith('third_party/go-smtp/') for p in local))
        self.assertTrue(any(p.startswith('third_party/enmime-v2.3.0/') for p in local))
        self.assertTrue(receipt['generated_testmain']);self.assertTrue(receipt['external_modulecache_inputs'])

    def test_real_receipt_roundtrip(self):
        self.assertEqual(binding.validate(self.receipt,ROOT,GO,cache=CACHE,modulecache=MODULECACHE),self.receipt)

    def test_omission_extra_hash_flags_abi_and_historical_rejected(self):
        # Actual independently obtained root binding, mutated receipts; a live recapture
        # is shared here to avoid repeated identical metadata dispatch per negative.
        observed=binding.capture(ROOT,GO,cache=CACHE,modulecache=MODULECACHE)
        cases=[]
        for change in ('missing','extra','hash','flags','abi','legacy'):
            value=copy.deepcopy(observed)
            if change=='missing':value['selected_local'].pop(next(iter(value['selected_local'])))
            if change=='extra':value['selected_local']['private.env']={'sha256':'a'*64,'fields':['GoFiles']}
            if change=='hash':value['selected_local'][next(iter(value['selected_local']))]['sha256']='a'*64
            if change=='flags':value['environment']['GOFLAGS']='-buildvcs=false'
            if change=='abi':value['go_env']['GOAMD64']='v3'
            if change=='legacy':value['policy']=binding.inventory.CURRENT_POLICY
            payload={k:v for k,v in value.items() if k!='attestation_sha256'}
            value['attestation_sha256']=hashlib.sha256(json.dumps(payload,sort_keys=True,separators=(',',':')).encode()).hexdigest()
            cases.append((change,value))
        with mock.patch.object(binding,'capture',return_value=observed):
            for change,value in cases:
                with self.subTest(change=change),self.assertRaises(ValueError):binding.validate(value,ROOT,GO,cache=CACHE,modulecache=MODULECACHE)

    def test_capture_midflight_drift_refused(self):
        actual=binding.digest; calls=0
        def drift(root,names):
            nonlocal calls
            result=actual(root,names);calls+=1
            if calls==3:result[next(iter(result))]='a'*64
            return result
        with mock.patch.object(binding,'digest',side_effect=drift),self.assertRaisesRegex(ValueError,'changed'):
            binding.capture(ROOT,GO,cache=CACHE,modulecache=MODULECACHE)

if __name__=='__main__':unittest.main()
