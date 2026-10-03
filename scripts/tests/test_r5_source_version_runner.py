import sys
from pathlib import Path
import unittest
from unittest import mock
sys.path.insert(0,str(Path(__file__).resolve().parents[1]))
import run_r5_source_version_tests as runner
import r5_source_runner_prepare as preparation
import tempfile
import json
import os

class SourceVersionRunnerTests(unittest.TestCase):
    def setUp(self):
        self.ids=sorted(runner.HISTORICAL_IDS|{runner.FRESH_CLASS+'test_roundtrip','test_other.Class.test_x'})

    def test_exact_partition(self):
        current,frozen=runner.partition(self.ids)
        self.assertFalse(set(current)&set(frozen));self.assertEqual(set(current+frozen),set(self.ids))

    def test_missing_overlap_duplicate_wrong_baseline(self):
        current,frozen=runner.partition(self.ids)
        for a,b,c in [(current[1:],frozen,runner.BASELINE),(current+frozen[:1],frozen,runner.BASELINE),(current+current[:1],frozen,runner.BASELINE),(current,frozen,'a'*40)]:
            with self.subTest(a=a,b=b,c=c),self.assertRaises(ValueError):runner.verify(self.ids,a,b,c)

    def test_no_fresh_actual_root_class(self):
        with self.assertRaises(ValueError):runner.partition(list(runner.HISTORICAL_IDS))

    def test_preparation_failure_reports_all_ids_missing_and_dispatches_none(self):
        with tempfile.TemporaryDirectory() as temp,mock.patch.object(runner,'git',return_value='a'*40),mock.patch.object(runner,'checkout'),mock.patch.object(runner,'discover',return_value=self.ids),mock.patch.object(preparation,'prepare',side_effect=ValueError('integrity failure')),mock.patch.object(runner.subprocess,'run') as dispatch:
            output=Path(temp)/'report.json'
            self.assertEqual(runner.run(temp,output),1)
            data=json.loads(output.read_text())
            self.assertEqual(data['status'],'failed');self.assertEqual(data['groups'],[])
            self.assertEqual(data['missing_test_ids'],self.ids);self.assertFalse(data['no_missing'])
            self.assertEqual(data['preparation_error'],'integrity failure');dispatch.assert_not_called()

    def test_child_failure_and_skip_not_green(self):
        import tempfile,json
        class Bad(unittest.TestCase):
            def test_fail(self):self.fail('intentional runner negative')
        class Skip(unittest.TestCase):
            @unittest.skip('intentional runner negative')
            def test_skip(self):pass
        class Expected(unittest.TestCase):
            @unittest.expectedFailure
            def test_expected(self):self.fail('intentional expected failure negative')
        for case in [Bad('test_fail'),Skip('test_skip'),Expected('test_expected')]:
            import types,importlib
            with tempfile.TemporaryDirectory() as temp,mock.patch.object(importlib,'import_module',return_value=types.SimpleNamespace()),mock.patch.object(runner.unittest.TestLoader,'loadTestsFromNames',return_value=unittest.TestSuite([case])),mock.patch.object(runner,'git',return_value=runner.BASELINE),mock.patch.object(runner,'git_go_env',return_value='/tmp/r5-runner-cache'):
                before=Path.cwd()
                try:
                    self.assertEqual(runner.child(temp,[case.id()],Path(temp)/'report.json'),1)
                    report=json.loads((Path(temp)/'report.json').read_text())
                    self.assertEqual(report['actual_test_ids'],[case.id()])
                    self.assertEqual(report['expected_failures'],[case.id()] if isinstance(case,Expected) else [])
                finally:
                    import os
                    os.chdir(before)

class PreparationBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(prefix='r5-prep-unit-')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root/'web').mkdir()

    def test_unknown_lock_and_existing_dependency_tree_refused_before_write(self):
        lock = Path(__file__).resolve().parents[2]/'web/package-lock.json'
        (self.root/'web/package-lock.json').write_bytes(lock.read_bytes()+b' ')
        with self.assertRaisesRegex(ValueError,'lock hash'):
            preparation.prepare_typescript(self.root,b'bad')
        self.assertFalse((self.root/'web/node_modules').exists())
        (self.root/'web/package-lock.json').write_bytes(lock.read_bytes())
        (self.root/'web/node_modules').mkdir()
        (self.root/'web/node_modules/flatted.go').write_text('package flatted')
        with self.assertRaisesRegex(ValueError,'absent node_modules'):
            preparation.prepare_typescript(self.root,b'bad')
        self.assertEqual((self.root/'web/node_modules/flatted.go').read_text(),'package flatted')

    def test_wrong_archive_hash_refused_before_any_mkdir(self):
        lock = Path(__file__).resolve().parents[2]/'web/package-lock.json'
        (self.root/'web/package-lock.json').write_bytes(lock.read_bytes())
        with mock.patch.object(Path,'mkdir',side_effect=AssertionError('write before verification')) as mkdir:
            with self.assertRaisesRegex(ValueError,'hash/integrity'):
                preparation.prepare_typescript(self.root,b'not the official package')
            mkdir.assert_not_called()

    def test_authenticated_archive_still_checks_member_and_dependency_boundary(self):
        # Re-sign synthetic archives locally to exercise the structural layer
        # after transport integrity. Production constants are never relaxed.
        import io,tarfile,base64,hashlib
        for mutation in ('link','traversal','native','duplicate','extra','dependency','lifecycle'):
            buffer=io.BytesIO()
            package={'name':'typescript','version':'5.9.3'}
            if mutation=='dependency':package['dependencies']={'unknown':'1.0.0'}
            if mutation=='lifecycle':package['scripts']={'postinstall':'touch unexpected'}
            with tarfile.open(fileobj=buffer,mode='w:gz') as tar:
                for i in range(132):
                    name='package/package.json' if i==0 else f'package/lib/{i}.js'
                    if i==1:
                        name={'traversal':'package/../escape.js','native':'package/lib/native.so',
                              'duplicate':'package/package.json'}.get(mutation,name)
                    member=tarfile.TarInfo(name)
                    data=json.dumps(package).encode() if i==0 else b'fixture'
                    if i==1 and mutation=='link':member.type=tarfile.SYMTYPE;member.linkname='elsewhere'
                    else:member.size=len(data)
                    tar.addfile(member,io.BytesIO(data) if member.isfile() else None)
                if mutation=='extra':
                    member=tarfile.TarInfo('package/extra.js');member.size=1;tar.addfile(member,io.BytesIO(b'x'))
            archive=buffer.getvalue()
            integrity='sha512-'+base64.b64encode(hashlib.sha512(archive).digest()).decode()
            with self.subTest(mutation=mutation),mock.patch.object(preparation,'TS_SHA256',preparation.digest(archive)),mock.patch.object(preparation,'TS_INTEGRITY',integrity):
                with self.assertRaises(ValueError):preparation.typescript_members(archive)

    def test_extra_tampered_link_and_native_prepared_files_rejected(self):
        target=self.root/'web/node_modules/typescript'
        target.mkdir(parents=True)
        (target/'index.js').write_bytes(b'official')
        expected={'index.js':b'official'}
        preparation.verify_typescript(self.root,expected)
        for name in ('extra.js','injected.go','native.so','.bin/tsc','empty-directory'):
            path=target.parent/name
            if name=='empty-directory':path.mkdir()
            else:
                path.parent.mkdir(exist_ok=True,parents=True);path.write_bytes(b'unknown')
            with self.subTest(name=name),self.assertRaises(ValueError):
                preparation.verify_typescript(self.root,expected)
            if path.is_dir():path.rmdir()
            else:path.unlink()
            if name.startswith('.bin/'):path.parent.rmdir()
        (target/'index.js').write_bytes(b'tampered')
        with self.assertRaises(ValueError):preparation.verify_typescript(self.root,expected)
        (target/'index.js').unlink();(target/'index.js').symlink_to(target/'missing')
        with self.assertRaisesRegex(ValueError,'unsafe'):preparation.verify_typescript(self.root,expected)

    def test_stale_source_fixture_and_receipt_tamper_rejected(self):
        fixture=self.root/'wire.json';fixture.write_text('{"fixtures":[]}')
        receipt=self.root/'preparation.json'
        data=dict(source_sha='a'*40,run_id='fresh',fixture=str(fixture),fixture_sha256=preparation.digest(fixture.read_bytes()))
        receipt.write_text(json.dumps(data));sha=preparation.digest(receipt.read_bytes())
        with mock.patch.object(preparation,'source_identity',return_value='a'*40):
            with self.assertRaisesRegex(ValueError,'stale'):
                preparation.validate_wire(self.root,fixture,receipt,sha,'previous-run')
        with mock.patch.object(preparation,'source_identity',return_value='b'*40):
            with self.assertRaisesRegex(ValueError,'wrong source'):
                preparation.validate_wire(self.root,fixture,receipt,sha,'fresh')
        fixture.write_text('tampered')
        with mock.patch.object(preparation,'source_identity',return_value='a'*40):
            with self.assertRaisesRegex(ValueError,'tampered fixture'):
                preparation.validate_wire(self.root,fixture,receipt,sha,'fresh')
        receipt.write_text('tampered')
        with self.assertRaisesRegex(ValueError,'receipt tampered'):
            preparation.validate_wire(self.root,fixture,receipt,sha,'fresh')

if __name__=='__main__':unittest.main()
