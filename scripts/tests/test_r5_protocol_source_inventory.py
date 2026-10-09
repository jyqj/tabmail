"""Pure source identity boundary tests; no Go, PostgreSQL or protocol execution."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import tempfile
import unittest
from unittest import mock

spec = importlib.util.spec_from_file_location('protocol_inventory', Path(__file__).resolve().parents[1]/'check_r5_protocol.py')
protocol = importlib.util.module_from_spec(spec)
spec.loader.exec_module(protocol)


class ProtocolSourceInventoryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)/'src'
        self.root.mkdir()
        for directory in ['cmd','internal','web']:
            (self.root/directory).mkdir()
        for name in protocol.PROTOCOL_FIXED_SOURCES | {'internal/a.go','cmd/main.go','web/package.json','web/package-lock.json','web/a.tsx'}:
            path = self.root/name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text('{"cases":[]}' if name.endswith('R5-PROTOCOL-CASES.json') else 'fixture bytes')
        self.manifest_path = Path(self.temp.name)/'source-manifest.json'
        self.manifest = {
            'schema_version':1, 'snapshot_root':str(self.root.resolve()),
            'source_identity_kind':'canonical_protocol_dependency_closure_sha256',
            'source_sha':'', 'files':{},
        }
        self.refresh()

    def refresh(self):
        self.manifest['files'] = {name:hashlib.sha256((self.root/name).read_bytes()).hexdigest() for name in protocol.protocol_source_paths(self.root)}
        return self.seal()

    def seal(self):
        self.manifest['source_sha'] = hashlib.sha256(json.dumps(self.manifest['files'],sort_keys=True,separators=(',',':')).encode()).hexdigest()
        self.manifest_path.write_text(json.dumps(self.manifest))
        self.pin = hashlib.sha256(self.manifest_path.read_bytes()).hexdigest()
        return self.pin

    def validate(self):
        return protocol.manifest_source_closure(self.manifest_path,self.pin,self.root)

    def test_explicit_canonical_closure_not_head_commit(self):
        files, identity = self.validate()
        self.assertEqual(files,self.manifest['files'])
        self.assertEqual(identity['source_sha'],self.manifest['source_sha'])
        self.assertEqual(identity['source_identity_kind'],'canonical_protocol_dependency_closure_sha256')
        self.assertNotIn('head',identity)
        self.assertIn('Archived v1 scoped',identity['source_identity_boundary'])
        # Read-only v1 validation remains available, but cannot enter a current run.
        with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'SOURCE_POLICY',protocol.source_inventory.POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',self.manifest_path),mock.patch.object(protocol,'SOURCE_MANIFEST_SHA256',self.pin),mock.patch.object(protocol.subprocess,'run') as git:
            with self.assertRaises(ValueError):protocol.source_closure()
            git.assert_not_called()

    def test_current_missing_policy_and_git_inventory_are_not_fallback(self):
        with mock.patch.object(protocol,'ROOT',self.root),mock.patch.object(protocol,'SOURCE_POLICY',None),mock.patch.object(protocol,'SOURCE_MANIFEST',None),mock.patch.object(protocol.subprocess,'run') as git:
            with self.assertRaises(ValueError):protocol.source_closure()
            git.assert_not_called()
        with mock.patch.object(protocol,'SOURCE_POLICY',protocol.source_inventory.POLICY),mock.patch.object(protocol,'SOURCE_MANIFEST',None),mock.patch.object(protocol.subprocess,'run') as git:
            with self.assertRaises(ValueError):protocol.source_closure()
            git.assert_not_called()

    def test_missing_extra_important_source_or_actual_byte_drift_rejected(self):
        original = json.loads(json.dumps(self.manifest))
        for name in ['go.mod','web/package-lock.json','internal/a.go','scripts/check_r5_protocol.py']:
            self.manifest = json.loads(json.dumps(original))
            self.manifest['files'].pop(name)
            self.seal()
            with self.subTest(name=name),self.assertRaises(ValueError):self.validate()
        self.manifest = original
        self.seal()
        (self.root/'internal/new.go').write_text('new important source')
        with self.assertRaises(ValueError):self.validate()
        (self.root/'internal/new.go').unlink()
        (self.root/'internal/a.go').write_text('changed bytes')
        with self.assertRaises(ValueError):self.validate()

    def test_manifest_pin_kind_root_digest_and_schema_cannot_be_inferred(self):
        with self.assertRaises(ValueError):protocol.manifest_source_closure(self.manifest_path,'0'*64,self.root)
        for key,value in [('snapshot_root','/another-root'),('source_identity_kind','git_commit'),('source_sha','0'*64),('schema_version',True),('unknown',1)]:
            saved = json.loads(json.dumps(self.manifest))
            self.manifest[key] = value
            self.manifest_path.write_text(json.dumps(self.manifest))
            self.pin = hashlib.sha256(self.manifest_path.read_bytes()).hexdigest()
            with self.subTest(key=key),self.assertRaises(ValueError):self.validate()
            self.manifest = saved

    def test_path_traversal_absolute_noncanonical_and_symlink_rejected(self):
        for name in ['../escaped.go','/absolute.go','internal/../a.go','internal//a.go','./internal/a.go']:
            self.manifest['files'][name] = '0'*64
            self.seal()
            with self.subTest(name=name),self.assertRaises(ValueError):self.validate()
            self.manifest['files'].pop(name)
        self.seal()
        target = self.root/'internal/a.go'
        target.unlink()
        target.symlink_to(self.root/'go.mod')
        with self.assertRaises(ValueError):self.validate()

    def test_duplicate_json_keys_and_missing_adapter_dependency_rejected(self):
        raw = self.manifest_path.read_text().replace('"schema_version": 1','"schema_version": 1,"schema_version": 1')
        self.manifest_path.write_text(raw)
        self.pin = hashlib.sha256(self.manifest_path.read_bytes()).hexdigest()
        with self.assertRaises(ValueError):self.validate()
        (self.root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').write_text(json.dumps({'cases':[{'shared_adapters':[{'source':'outside/source.go'}]}]}))
        self.refresh()
        with self.assertRaises(ValueError):self.validate()

    def test_known_runtime_artifacts_are_not_source_inventory_fallback(self):
        for name in ['web/node_modules/dependency/a.js','web/.next/a.js','web/.env.local','web/a.tsbuildinfo']:
            path = self.root/name
            path.parent.mkdir(parents=True,exist_ok=True)
            path.write_text('not source')
        self.assertEqual(self.validate()[0],self.manifest['files'])


if __name__ == '__main__':
    unittest.main()
