"""Executed adversarial filesystem fixtures; never mutate historical source."""
import hashlib
import os
from pathlib import Path
import shutil
import sys
import tempfile
import unittest
from unittest import mock
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import r5_archive_boundary as boundary
import r5_source_inventory as inventory
import test_r5_smtp_owner_source_inventory as fixtures
ROOT=Path(__file__).resolve().parents[2]


def fixture(root):
    fixtures.fixture(root)
    contract=boundary.registry(ROOT)
    for p in [boundary.REGISTRY,*[f['path'] for f in contract['historical_files']],*[m['path'] for m in contract['modules']]]:
        target=root/p;target.parent.mkdir(parents=True,exist_ok=True);shutil.copyfile(ROOT/p,target)


class ArchiveBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name);fixture(self.root)

    def test_exact_baseline_hashes_and_six_modules(self):
        value=boundary.check(self.root)
        self.assertEqual(len(value['modules']),6);self.assertEqual(len(value['archive_static']),36)
        self.assertEqual(len(boundary.registry(self.root)['protected_go_files']),21)

    def test_marker_deletion_and_module_in_all_roots(self):
        contract=boundary.registry(self.root)
        marker=self.root/contract['modules'][0]['path'];raw=marker.read_bytes();marker.unlink()
        with self.assertRaises(ValueError):boundary.check(self.root)
        marker.write_bytes(raw)
        for p in ['docs/go.mod','internal/hidden/go.mod','third_party/go-smtp/build/go.mod',str(marker.parent.relative_to(self.root))+'/nested/go.mod','web/node_modules/x/go.mod']:
            with self.subTest(path=p):
                target=self.root/p;target.parent.mkdir(parents=True,exist_ok=True);target.write_text('module hidden\ngo 1.25.7\n')
                with self.assertRaises(ValueError):boundary.check(self.root)
                shutil.rmtree(target.parent) if target.parent.name in ('hidden','nested','x','build') else target.unlink()

    def test_marker_bytes_value_case_directives(self):
        p=self.root/boundary.registry(self.root)['modules'][0]['path'];raw=p.read_bytes()
        for changed in [raw.replace(b'archive',b'Archive'),raw.replace(b'1.25.7',b'1.25.8'),raw+b'\nrequire evil v1.0.0\n',raw+b'replace evil v1.0.0 => ./evil\n',raw+b'toolchain go1.25.7\n']:
            with self.subTest(changed=changed),self.assertRaises(ValueError):
                p.write_bytes(changed);boundary.check(self.root)
        p.write_bytes(raw)

    def test_archive_extra_controls_and_files(self):
        archive=self.root/Path(boundary.registry(self.root)['modules'][0]['path']).parent
        for name in ['go.sum','go.work','vendor','extra.go','extra.json','renamed.patch']:
            p=archive/name
            with self.subTest(name=name):
                p.mkdir() if name=='vendor' else p.write_text('new')
                with self.assertRaises(ValueError):boundary.check(self.root)
                p.rmdir() if name=='vendor' else p.unlink()

    def test_every_historical_file_byte_delete_rename(self):
        for item in boundary.registry(self.root)['historical_files']:
            p=self.root/item['path'];raw=p.read_bytes()
            with self.subTest(path=item['path']):
                p.write_bytes(raw+b'\n')
                with self.assertRaises(ValueError):boundary.check(self.root)
                p.unlink()
                with self.assertRaises(ValueError):boundary.check(self.root)
                other=p.with_name(p.name+'.renamed');other.write_bytes(raw)
                with self.assertRaises(ValueError):boundary.check(self.root)
                other.unlink();p.write_bytes(raw)

    def test_registry_duplicate_traversal_forgery(self):
        p=self.root/boundary.REGISTRY;raw=p.read_bytes()
        for changed in [raw.replace(b'"schema_version": 1',b'"schema_version": 1, "schema_version": 1'),raw.replace(b'docs/company-mail',b'../company-mail'),raw.replace(b'f670a5c4',b'a670a5c4')]:
            with self.subTest(changed=changed[:60]),self.assertRaises(ValueError):
                p.write_bytes(changed);boundary.check(self.root)
        p.write_bytes(raw)

    def test_unknown_root_and_ignored_go_not_hidden(self):
        for name in ['newroot/new.go','web/node_modules/flatted/golang/pkg/flatted/flatted.go','internal/build/hidden.go']:
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_text('package standalone\n')
            with self.subTest(name=name),self.assertRaises(ValueError):boundary.check(self.root)
            p.unlink()

    def test_production_all_variants_change_static_domain(self):
        before=boundary.check(self.root)
        for name in ['standalone.go','standalone_test.go','standalone_windows.go','tagged.go']:
            p=self.root/'internal'/name;p.write_text('//go:build r5protocol\n\npackage standalone\n')
            after=boundary.check(self.root);self.assertIn('internal/'+name,after['production_go']);self.assertNotEqual(before,after);p.unlink()

    def test_import_embed_generate_archive_rejected(self):
        p=self.root/'cmd/main.go';raw=p.read_bytes()
        for directive in ['import "tabmail/archive/r5-mime-preparse-source/x"','//go:embed ../../docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/*','//go:generate cat ../../docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before.json']:
            p.write_text('package main\n'+directive+'\n')
            with self.subTest(directive=directive),self.assertRaises(ValueError):boundary.check(self.root)
        p.write_bytes(raw)

    def test_symlink_ancestors_fifo_and_metadata_only(self):
        for name in ['internal/alias','docs/alias','web/node_modules/alias']:
            p=self.root/name;p.parent.mkdir(parents=True,exist_ok=True);p.symlink_to('/etc')
            with self.subTest(name=name),self.assertRaises(ValueError):boundary.check(self.root)
            p.unlink()
        p=self.root/'internal/fifo';os.mkfifo(p)
        with self.assertRaises(ValueError):boundary.check(self.root)
        with self.assertRaises(ValueError):boundary.read(self.root,'internal/fifo')
        p.unlink()
        # Arbitrary raw/cache bytes cannot be opened by topology.
        p=self.root/'docs/raw-private.log';p.write_bytes(b'private')
        with mock.patch.object(boundary,'read',side_effect=AssertionError('body read')):
            boundary.topology(self.root)

    def test_descriptor_second_stat_replacement(self):
        target=self.root/'cmd/main.go';original=os.stat;calls=0
        def replace(name,*args,**kwargs):
            nonlocal calls
            value=original(name,*args,**kwargs)
            if name=='main.go':
                calls+=1
                if calls==2:
                    target.unlink();target.symlink_to('/etc/passwd')
            return value
        with mock.patch.object(boundary.os,'stat',side_effect=replace),self.assertRaises(ValueError):boundary.topology(self.root)

    def test_budget_and_unavailable_metadata_fail_closed(self):
        with mock.patch.object(boundary,'MAX_ENTRIES',1),self.assertRaisesRegex(ValueError,'budget'):boundary.check(self.root)
        with mock.patch.object(boundary,'MAX_DEPTH',1),self.assertRaisesRegex(ValueError,'budget'):boundary.check(self.root)
        with mock.patch.object(boundary.os,'scandir',side_effect=PermissionError('denied')),self.assertRaisesRegex(ValueError,'unavailable'):boundary.check(self.root)

    def test_third_replace(self):
        p=self.root/'go.mod';p.write_text(p.read_text()+'\nreplace example.invalid/x v1.0.0 => ./third_party/go-smtp\n')
        with self.assertRaises(ValueError):boundary.check(self.root)

if __name__=='__main__':unittest.main()
