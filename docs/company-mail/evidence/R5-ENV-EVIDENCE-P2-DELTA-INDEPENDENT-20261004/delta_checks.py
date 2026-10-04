"""Fresh bounded P2 delta counterexamples. Synthetic children; no product batches."""
import ast
import errno
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'scripts'))
import check_r5_compatibility as compatibility
import r5_private_diagnostics as private
import r5_selected_source_binding_v2 as binding

OLD='df6ffe046611f5b2121f4d48d5114a2ececd2c24'
FIXED='3f34c31ed51a721b318a76512805e4a14dc292c3'


class DeltaChecks(unittest.TestCase):
    def setUp(self):
        self.temp=tempfile.TemporaryDirectory(prefix='R5-ENV-EVIDENCE-P2-DELTA-INDEPENDENT-20261004-',dir='/tmp')
        self.addCleanup(self.temp.cleanup)
        self.root=Path(self.temp.name)
        patch=mock.patch.dict(os.environ,{'R5_DIAGNOSTIC_ROOT':str(self.root)})
        patch.start();self.addCleanup(patch.stop)
        self.fd_before=len(list(Path('/proc/self/fd').iterdir()))

    def assert_fd_count(self):
        self.assertEqual(len(list(Path('/proc/self/fd').iterdir())),self.fd_before)

    def test_real_child_nonzero_retains_original_object_and_binary_output(self):
        actual=subprocess.run
        observed=[]
        def run(*args,**kwargs):
            try:return actual(*args,**kwargs)
            except subprocess.CalledProcessError as error:observed.append(error);raise
        argv=[sys.executable,'-c','import os; os.write(1,b"\\xffabc"); os.write(2,b"synthetic-stderr"); raise SystemExit(7)']
        with mock.patch.object(private,'MAX_FILE_BYTES',2),mock.patch.object(compatibility.subprocess,'run',side_effect=run):
            with self.assertRaises(subprocess.CalledProcessError) as caught:compatibility.diagnostic_run(argv,check=True,capture_output=True)
        error=caught.exception
        self.assertIs(error,observed[0])
        self.assertEqual((error.cmd,error.returncode,error.stdout,error.stderr),(argv,7,b'\xffabc',b'synthetic-stderr'))
        self.assertIsInstance(error.retention_errors[0],ValueError)
        self.assertEqual(error.__notes__,['diagnostic retention failed: ValueError; errno=None'])
        self.assert_fd_count()

    def test_real_child_timeout_retains_original_object_and_partial_output(self):
        actual=subprocess.run
        observed=[]
        def run(*args,**kwargs):
            try:return actual(*args,**kwargs)
            except subprocess.TimeoutExpired as error:observed.append(error);raise
        argv=[sys.executable,'-c','import os,time; os.write(1,b"partial"); os.write(2,b"synthetic-stderr"); time.sleep(5)']
        fault=OSError(errno.ENOSPC,'synthetic-private-error-message')
        with mock.patch.object(private.Retention,'write',side_effect=fault),mock.patch.object(compatibility.subprocess,'run',side_effect=run):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:compatibility.diagnostic_run(argv,check=True,capture_output=True,timeout=0.3)
        error=caught.exception
        self.assertIs(error,observed[0])
        self.assertEqual((error.cmd,error.timeout,error.stdout,error.stderr),(argv,0.3,b'partial',b'synthetic-stderr'))
        self.assertEqual(error.retention_errors,(fault,))
        self.assertEqual(error.__notes__,['diagnostic retention failed: OSError; errno=28'])
        self.assert_fd_count()

    def test_stderr_and_summary_faults_keep_raw_files_and_original_exception(self):
        actual=private.Retention.write
        for stage in ('stderr','summary.json'):
            for cls in (subprocess.CalledProcessError,subprocess.TimeoutExpired):
                with self.subTest(stage=stage,kind=cls.__name__):
                    error=cls(7,['go'],output='synthetic-out',stderr='synthetic-err') if cls is subprocess.CalledProcessError else cls(['go'],60,output='synthetic-out',stderr='synthetic-err')
                    fault=OSError(errno.EACCES,'synthetic-private-path')
                    def write(sink,name,data):
                        if name.endswith('.'+stage):raise fault
                        return actual(sink,name,data)
                    before=set(self.root.iterdir())
                    with mock.patch.object(private.Retention,'write',write),mock.patch.object(compatibility.subprocess,'run',side_effect=error):
                        with self.assertRaises(cls) as caught:compatibility.diagnostic_run(['go'])
                    self.assertIs(caught.exception,error)
                    self.assertEqual((error.stdout,error.stderr),('synthetic-out','synthetic-err'))
                    self.assertEqual(error.retention_errors,(fault,))
                    directory=(set(self.root.iterdir())-before).pop()
                    self.assertEqual((directory/'0.stdout').read_bytes(),b'synthetic-out')
                    if stage=='summary.json':self.assertEqual((directory/'0.stderr').read_bytes(),b'synthetic-err')
                    self.assertFalse((directory/'0.summary.json').exists())
                    self.assert_fd_count()

    def test_count_exhausted_mid_command_does_not_replace_original_failure(self):
        error=subprocess.CalledProcessError(9,['go'],output=b'out',stderr=b'err')
        with mock.patch.object(private,'MAX_FILES',1),mock.patch.object(compatibility.subprocess,'run',side_effect=error):
            with self.assertRaises(subprocess.CalledProcessError) as caught:compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception,error)
        directory=next(self.root.iterdir())
        self.assertEqual([p.name for p in directory.iterdir()],['0.stdout'])
        self.assertIsInstance(error.retention_errors[0],ValueError)
        self.assert_fd_count()

    def test_successful_retention_leaves_failure_notes_and_fields_unchanged(self):
        error=subprocess.TimeoutExpired(['go'],42,output=None,stderr=None)
        error.add_note('producer-original-note')
        with mock.patch.object(compatibility.subprocess,'run',side_effect=error):
            with self.assertRaises(subprocess.TimeoutExpired) as caught:compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception,error)
        self.assertEqual((error.cmd,error.timeout,error.stdout,error.stderr),(['go'],42,None,None))
        self.assertEqual(error.__notes__,['producer-original-note'])
        self.assertFalse(hasattr(error,'retention_errors'))
        directory=next(self.root.iterdir())
        self.assertEqual((directory/'0.stdout').read_bytes(),b'')
        self.assertEqual((directory/'0.stderr').read_bytes(),b'')
        self.assert_fd_count()

    def test_encoding_fault_is_secondary_and_keeps_original_text(self):
        error=subprocess.CalledProcessError(7,['go'],output='synthetic\ud800',stderr='err')
        with mock.patch.object(compatibility.subprocess,'run',side_effect=error):
            with self.assertRaises(subprocess.CalledProcessError) as caught:compatibility.diagnostic_run(['go'])
        self.assertIs(caught.exception,error)
        self.assertEqual(error.stdout,'synthetic\ud800')
        self.assertIsInstance(error.retention_errors[0],UnicodeEncodeError)
        self.assertEqual(error.__notes__,['diagnostic retention failed: UnicodeEncodeError; errno=None'])
        self.assert_fd_count()

    def test_metadata_hydration_failure_preserves_complete_command_interface(self):
        env=dict(GOVERSION='go1.25.7',GOOS='linux',GOARCH='amd64',GOHOSTOS='linux',GOHOSTARCH='amd64',
                 CGO_ENABLED='1',GOWORK='off',GOENV='',GOFLAGS='',GOEXPERIMENT='',GOAMD64='v1',GOTOOLCHAIN='local',GOGCCFLAGS='')
        base={'files':{'scripts/r5_selected_source_binding_v2.py':{}},'archive_static':{}}
        fault=OSError(errno.ENOSPC,'synthetic-private-path')
        actual=private.Retention.write
        def write(sink,name,data):
            if name=='3.stdout':raise fault
            return actual(sink,name,data)
        outputs=[subprocess.CompletedProcess([],0,json.dumps(env).encode(),b''),subprocess.CompletedProcess([],13,b'hydration-partial',b'hydration-stderr')]
        with mock.patch.object(binding.inventory,'capture_current_source',return_value=base), \
             mock.patch.object(binding,'topology',return_value=set()), \
             mock.patch.object(binding,'digest',return_value={'scripts/r5_selected_source_binding_v2.py':binding._IMPLEMENTATION_SHA256}), \
             mock.patch.object(binding.subprocess,'run',side_effect=outputs),mock.patch.object(private.Retention,'write',write):
            with self.assertRaises(binding.MetadataCommandFailure) as caught:binding.capture(self.root,Path('/go'),cache=self.root,modulecache=self.root)
        error=caught.exception
        self.assertEqual(error.command,dict(role='unbound_dependency_hydration_not_attested',argv=['/go',*binding.ARGV],exit=13,
            stdout_sha256=hashlib.sha256(b'hydration-partial').hexdigest(),stderr='hydration-stderr'))
        self.assertEqual((error.stdout,error.stderr),(b'hydration-partial',b'hydration-stderr'))
        self.assertEqual(error.retention_errors,(fault,))
        self.assert_fd_count()

    def test_context_exit_failure_with_reused_descriptor_keeps_new_owner_alive(self):
        actual=os.fdopen
        reuse=[]
        fault=OSError(errno.ENOSPC,'synthetic flush/close fault')
        class ExitFailure:
            def __init__(self,fd):self.file=actual(fd,'wb');self.fd=fd
            def __enter__(self):return self.file
            def __exit__(self,*args):
                self.file.close()
                reuse.append(os.open('/dev/null',os.O_RDONLY))
                assert reuse[-1]==self.fd
                raise fault
        try:
            with private.Retention('test') as sink,mock.patch.object(private.os,'fdopen',side_effect=lambda fd,*a:ExitFailure(fd)):
                with self.assertRaises(OSError) as caught:sink.write('partial',b'out')
                self.assertIs(caught.exception,fault)
                self.assertEqual((sink.count,sink.total),(0,0))
                self.assertFalse((sink.path/'partial').exists())
                os.fstat(reuse[0])
        finally:
            for fd in reuse:os.close(fd)
        self.assert_fd_count()

    def test_raw_fdopen_failure_then_retry_has_no_leak_or_partial_file(self):
        fault=OSError(errno.ENOSPC,'synthetic fdopen failure')
        with private.Retention('test') as sink:
            with mock.patch.object(private.os,'fdopen',side_effect=fault):
                with self.assertRaises(OSError) as caught:sink.write('retry',b'out')
            self.assertIs(caught.exception,fault)
            self.assertEqual((sink.count,sink.total),(0,0))
            self.assertEqual(list(sink.path.iterdir()),[])
            sink.write('retry',b'out')
            self.assertEqual((sink.count,sink.total),(1,3))
            self.assertEqual((sink.path/'retry').read_bytes(),b'out')
        self.assert_fd_count()

    def test_delta_identity_and_catalog_domains_unchanged(self):
        def tree(ref,path):return ast.parse(subprocess.check_output(['git','show',ref+':'+path],cwd=ROOT,text=True))
        path='scripts/r5_selected_source_binding_v2.py'
        before,after=tree(OLD,path),tree(FIXED,path)
        def signature(node):return ast.dump(node)
        def unchanged_nodes(module):return [signature(n) for n in module.body if not (isinstance(n,ast.FunctionDef) and n.name=='_capture')]
        self.assertEqual(unchanged_nodes(before),unchanged_nodes(after))
        a=next(n for n in before.body if isinstance(n,ast.FunctionDef) and n.name=='_capture')
        b=next(n for n in after.body if isinstance(n,ast.FunctionDef) and n.name=='_capture')
        for capture in (a,b):
            capture.body=[n for n in capture.body if not (isinstance(n,ast.FunctionDef) and n.name=='run')]
        self.assertEqual(signature(a),signature(b))
        for path in ('scripts/r5_go_environment.py','scripts/r5_source_runner_prepare.py','scripts/run_r5_source_version_tests.py',
                     'scripts/check_r5_transactions.py','docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
                     'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json','go.mod','go.sum','web/package.json','web/package-lock.json'):
            self.assertEqual(subprocess.check_output(['git','show',OLD+':'+path],cwd=ROOT),subprocess.check_output(['git','show',FIXED+':'+path],cwd=ROOT),path)


if __name__=='__main__':
    result=unittest.TextTestRunner(verbosity=2).run(unittest.TestLoader().loadTestsFromTestCase(DeltaChecks))
    print(json.dumps(dict(tested_source_sha=FIXED,repair_base_sha=OLD,tests_run=result.testsRun,
        failures=[t.id() for t,_ in result.failures],errors=[t.id() for t,_ in result.errors],
        skips=[t.id() for t,_ in result.skipped],expected_failures=[t.id() for t,_ in result.expectedFailures]),indent=2))
    raise SystemExit(0 if result.wasSuccessful() else 1)
