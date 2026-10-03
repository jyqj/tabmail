"""Independent adversarial runtime checks; no fake product-green controls."""
import copy
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('runtime',Path(__file__).with_name('r5_external_runtime.py'))
runtime = importlib.util.module_from_spec(spec)
spec.loader.exec_module(runtime)

class RuntimeControls(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(); self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.source = self.root/'source'; self.source.mkdir()
        self.dependencies = self.root/'node_modules'; self.dependencies.mkdir()
        (self.source/'file').write_text('bound bytes')
    def test_descriptor_content_and_mode_drift(self):
        with runtime.descriptors() as d:
            before = d.tree(self.source)
        (self.source/'file').write_text('changed bytes')
        with runtime.descriptors() as d:
            self.assertNotEqual(before,d.tree(self.source))
    def test_source_shadow(self):
        (self.source/'node_modules').mkdir()
        with runtime.descriptors() as d, self.assertRaisesRegex(ValueError,'shadow'):
            d.tree(self.source,source=True)
    def test_directory_link(self):
        (self.source/'linked').symlink_to(self.dependencies,target_is_directory=True)
        with runtime.descriptors() as d, self.assertRaises(ValueError):
            d.tree(self.source,source=True)
    def test_file_link(self):
        (self.source/'link').symlink_to('file')
        with runtime.descriptors() as d, self.assertRaises(ValueError):
            d.file(self.source/'link')
    def test_ancestor_link(self):
        (self.root/'alias').symlink_to(self.source,target_is_directory=True)
        with runtime.descriptors() as d, self.assertRaises(OSError):
            d.file(self.root/'alias/file')
    def test_special_fifo(self):
        os.mkfifo(self.source/'fifo')
        with runtime.descriptors() as d, self.assertRaises(ValueError):
            d.tree(self.source)
    def test_node_poison(self):
        for key in ('NODE_PATH','NODE_OPTIONS','npm_config_registry'):
            with patch.dict(os.environ,{key:'pollution'}),self.assertRaises(ValueError):
                runtime.clean_environment()
    def test_exclusive_owner(self):
        manifest = dict(dependency_root=dict(path=str(self.root)))
        with runtime.owner(manifest):
            with self.assertRaises(FileExistsError):
                with runtime.owner(manifest): pass
        self.assertFalse((self.root/'.r5-runtime-owner').exists())
    def test_independent_pin_rejects_rehash_promotion(self):
        path=self.root/'manifest.json'
        receipt=dict(policy=runtime.POLICY,status='UNADOPTED')
        raw=runtime.canonical(receipt); path.write_bytes(raw)
        runtime.load_pinned(path,runtime.digest(raw))
        receipt['status']='scoped_pass'; path.write_bytes(runtime.canonical(receipt))
        with self.assertRaisesRegex(ValueError,'byte pin'):
            runtime.load_pinned(path,runtime.digest(raw))
        with self.assertRaisesRegex(ValueError,'promotion'):
            runtime.load_pinned(path,runtime.digest(path.read_bytes()))
    def test_unknown_executable(self):
        manifest=dict(node=dict(path='/fixed/node'),cli=dict(path='/fixed/cli'))
        with self.assertRaisesRegex(ValueError,'unknown executable'):
            runtime.launch(manifest,['/unknown/node','/fixed/cli'])
    def test_unknown_argv(self):
        manifest=dict(node=dict(path='/fixed/node'),cli=dict(path='/fixed/cli'))
        with self.assertRaisesRegex(ValueError,'unbound runtime argv'):
            runtime.launch(manifest,['/fixed/node','/fixed/cli','--eval','code'])
    def test_escaping_declared_link(self):
        runtime._DEPENDENCY_ROOT=str(self.root)
        with self.assertRaisesRegex(ValueError,'undeclared'):
            runtime.dependency_records({'bad':dict(type='link',target='/outside')},dict(packages={'':{}}))
    def test_missing_required_lock_package(self):
        runtime._DEPENDENCY_ROOT=str(self.root)
        with self.assertRaisesRegex(ValueError,'nonoptional'):
            runtime.dependency_records({},dict(packages={'node_modules/tool':dict(version='1')}))
    def test_optional_absence_explicit(self):
        runtime._DEPENDENCY_ROOT=str(self.root)
        receipt=runtime.dependency_records({},dict(packages={'node_modules/tool':dict(version='1',optional=True,os=['darwin'])}))
        self.assertEqual(receipt['absent_optional'],['node_modules/tool'])
    def test_manifest_drift_rejected(self):
        fake=dict(source=dict(path=str(self.source)),dependency_root=dict(path=str(self.root)),receipt_paths=dict(archive='a',default='b',race='c'),node=dict(path='/node'))
        with patch.object(runtime,'observe',return_value=dict(fake,installed_content_root='changed')):
            with self.assertRaisesRegex(ValueError,'drift'): runtime.validate(fake)

if __name__=='__main__': unittest.main()
