"""Independent tooling checks against frozen df6ffe0. No formal batch dispatch.

Run from the checkout root with PYTHONPATH=scripts:scripts/tests python3 -B
<this file>. Requirement tests intentionally fail on the candidate.
Raw outputs belong under an owned private /tmp root, never in this directory.
"""
import ast
import copy
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

ROOT = Path(__file__).resolve().parents[4]
sys.path[:0] = [str(ROOT / 'scripts'), str(ROOT / 'scripts/tests')]
import r5_go_environment as environment
import r5_private_diagnostics as private
import r5_selected_source_binding_v2 as binding
import r5_source_runner_prepare as preparation
import check_r5_transactions as transactions
import check_r5_compatibility as compatibility
import run_r5_source_version_tests as runner

BASE = 'f52f8cbbf31dcbfb72ba062af6623248334a2e99'
CANDIDATE = 'df6ffe046611f5b2121f4d48d5114a2ececd2c24'


class EnvironmentChecks(unittest.TestCase):
    def test_explicit_no_path_go_and_caller_isolation(self):
        original = dict(R5_TEST_GO='/selected/bin/go', R5_GO='/legacy/go', PATH='/no-go',
                        R5_TEST_CACHE='/owned/build', R5_TEST_MODULECACHE='/owned/modules',
                        GOOS='darwin', GOARCH='arm64', GOAMD64='v4', GOEXPERIMENT='swissmap',
                        CGO_ENABLED='0', CGO_CFLAGS='bad', CGO_LDFLAGS_ALLOW='bad',
                        CC='bad', CXX='bad', GOROOT='bad', GODEBUG='bad', GOFLAGS='-race',
                        GOENV='bad', GOWORK='bad', GOTOOLCHAIN='auto', GOTMPDIR='bad',
                        GOCACHEPROG='bad', GOFIPS140='bad', GOCOVERDIR='bad',
                        DATABASE_URL='synthetic-preserved', HTTPS_PROXY='synthetic-preserved')
        before = copy.deepcopy(original)
        go, child = environment.selected(original)
        self.assertEqual(original, before)
        self.assertEqual(go, '/selected/bin/go')
        self.assertEqual(child['PATH'], '/selected/bin:/no-go')
        self.assertEqual(child['GOCACHE'], '/owned/build')
        self.assertEqual(child['GOMODCACHE'], '/owned/modules')
        self.assertEqual(child['GOPATH'], '/owned')
        self.assertEqual({k:child[k] for k in ('GOENV','GOWORK','GOFLAGS','GOTOOLCHAIN')},
                         dict(GOENV='off', GOWORK='off', GOFLAGS='', GOTOOLCHAIN='local'))
        for key in ('GOOS','GOARCH','GOAMD64','GOEXPERIMENT','CGO_ENABLED','CGO_CFLAGS',
                    'CGO_LDFLAGS_ALLOW','CC','CXX','GOROOT','GODEBUG','GOTMPDIR',
                    'GOCACHEPROG','GOFIPS140','GOCOVERDIR'):
            self.assertNotIn(key, child)
        self.assertEqual(child['DATABASE_URL'], before['DATABASE_URL'])
        self.assertEqual(child['HTTPS_PROXY'], before['HTTPS_PROXY'])

    def test_no_explicit_tool_keeps_legacy_environment(self):
        original = dict(PATH='/bin', GOFLAGS='-tags=legacy', CGO_ENABLED='0',
                        CC='legacycc', GOWORK='legacy', GODEBUG='legacy')
        with mock.patch.object(environment.shutil, 'which', return_value='/bin/go'):
            go, child = environment.selected(original)
        self.assertEqual(go, '/bin/go')
        self.assertEqual(child, original)
        self.assertIsNot(child, original)

    def test_legacy_selector_and_empty_primary(self):
        go, child = environment.selected(dict(R5_TEST_GO='', R5_GO='/legacy/go', PATH='/bin'))
        self.assertEqual(go, '/legacy/go')
        self.assertEqual(child['R5_TEST_GO'], go)
        self.assertEqual(child['R5_GO'], go)

    def test_transaction_fallback_does_not_scrub_flags(self):
        original = dict(PATH='/bin', GOFLAGS='-tags=legacy', CGO_ENABLED='0', CC='legacy')
        with mock.patch.dict(os.environ, original, clear=True), \
             mock.patch.object(transactions.shutil, 'which', return_value='/bin/go'), \
             mock.patch.object(transactions.Path, 'exists', return_value=False), \
             mock.patch.object(transactions.subprocess, 'run', return_value=subprocess.CompletedProcess([],0,'{}','')) as run:
            self.assertEqual(transactions.extract(), {})
        args, kwargs = run.call_args
        self.assertEqual(args[0][0], '/bin/go')
        self.assertEqual(kwargs['env'], dict(original, GOTOOLCHAIN='local', GOPROXY='off'))
        self.assertEqual(kwargs['timeout'], 90)

    def test_transaction_existing_cached_fallback_precedence(self):
        cached = '/Users/jin/.cache/go-mod/golang.org/toolchain@v0.0.1-go1.25.7.darwin-arm64/bin/go'
        with mock.patch.dict(os.environ, {'PATH':'/bin'}, clear=True), \
             mock.patch.object(transactions.shutil, 'which', side_effect=lambda name, **kw:'/bin/go' if name=='go' else None), \
             mock.patch.object(transactions.Path, 'exists', return_value=True), \
             mock.patch.object(transactions.subprocess, 'run', return_value=subprocess.CompletedProcess([],0,'{}','')) as run:
            transactions.extract()
        self.assertEqual(run.call_args.args[0][0], cached)

    def test_preparation_uses_passed_tool_and_preserves_initial_settings(self):
        class Stop(Exception): pass
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, dict(R5_TEST_GO='/wrong/go', R5_GO='/wrong/go',
                   R5_TEST_CACHE='/cache', R5_TEST_MODULECACHE='/modules', PATH='/no-go', CGO_ENABLED='0'), clear=True), \
             mock.patch.object(preparation, 'source_identity', return_value=CANDIDATE), \
             mock.patch.object(preparation.subprocess, 'check_output', return_value='go1.25.7\n') as version, \
             mock.patch.object(preparation, 'go_selection', side_effect=Stop) as selected:
            with self.assertRaises(Stop): preparation.prepare(tmp, Path(tmp)/'out', '/passed/go')
        self.assertEqual(version.call_args.args[0], ['/passed/go','env','GOVERSION'])
        child = selected.call_args.args[2]
        self.assertEqual(child['R5_GO'], '/passed/go')
        self.assertEqual(child['GOCACHE'], '/cache')
        self.assertEqual(child['GOMODCACHE'], '/modules')
        self.assertNotIn('CGO_ENABLED', child)

    def test_runner_both_groups_preserve_settings_and_isolate_current_fixture(self):
        ids = sorted(runner.HISTORICAL_IDS | {runner.FRESH_CLASS+'test_roundtrip'})
        children = []
        def run(argv, **kwargs):
            children.append(kwargs['env'])
            request = json.loads(Path(argv[argv.index('--ids')+1]).read_text())
            Path(argv[argv.index('--output')+1]).write_text(json.dumps(dict(actual_test_ids=request, source_sha=CANDIDATE)))
            return subprocess.CompletedProcess(argv, 0)
        with tempfile.TemporaryDirectory() as tmp, \
             mock.patch.dict(os.environ, dict(R5_GO='/legacy/go', PATH='/no-go',
                 R5_TEST_CACHE='/cache',R5_TEST_MODULECACHE='/modules',CGO_ENABLED='0',
                 ORDINARY_RECEIPT_WIRE_FIXTURE='stale',R5_SOURCE_RUN_ID='stale',
                 DATABASE_URL='synthetic-preserved'),clear=True), \
             mock.patch.object(runner,'git',return_value=CANDIDATE), \
             mock.patch.object(runner,'checkout'), mock.patch.object(runner,'discover',return_value=ids), \
             mock.patch.object(runner.preparation,'prepare',return_value=({},dict(R5_SOURCE_RUN_ID='fresh',ORDINARY_RECEIPT_WIRE_FIXTURE='fresh'))), \
             mock.patch.object(runner.subprocess,'run',side_effect=run):
            self.assertEqual(runner.run(tmp,Path(tmp)/'report.json'),0)
        self.assertEqual(len(children),2)
        for child in children:
            self.assertEqual(child['R5_TEST_GO'], '/legacy/go')
            self.assertEqual(child['GOCACHE'], '/cache')
            self.assertEqual(child['GOMODCACHE'], '/modules')
            self.assertEqual(child['DATABASE_URL'], 'synthetic-preserved')
            self.assertNotIn('CGO_ENABLED', child)
        self.assertEqual(children[0]['ORDINARY_RECEIPT_WIRE_FIXTURE'], 'fresh')
        self.assertNotIn('ORDINARY_RECEIPT_WIRE_FIXTURE',children[1])

    def test_context_and_receipt_semantics_unchanged_by_ast(self):
        names = ('CONTEXT','DEFAULT_CONTEXT','ARGV','MVS_ARGV','POLICY','FIELDS','NATIVE_FIELDS','STATIC_EVIDENCE')
        def read(ref):
            return ast.parse(subprocess.check_output(['git','show',ref+':scripts/r5_selected_source_binding_v2.py'],cwd=ROOT,text=True))
        a,b = read(BASE),read(CANDIDATE)
        def constants(tree):
            return {n.targets[0].id:ast.dump(n.value) for n in tree.body if isinstance(n,ast.Assign) and isinstance(n.targets[0],ast.Name) and n.targets[0].id in names}
        self.assertEqual(constants(a), constants(b))
        functions = ('selection_argv','classify','normalize_mvs','compare_coverage','compare_variants','stream','digest','topology')
        for name in functions:
            left = next(n for n in a.body if isinstance(n,ast.FunctionDef) and n.name==name)
            right = next(n for n in b.body if isinstance(n,ast.FunctionDef) and n.name==name)
            self.assertEqual(ast.dump(left),ast.dump(right),name)
        # Remove only retention wrappers/statements and compare complete old capture/validate bodies.
        class StripRetention(ast.NodeTransformer):
            def visit_With(self,n):
                if any(isinstance(i.context_expr,ast.Call) and isinstance(i.context_expr.func,ast.Name) and i.context_expr.func.id=='Retention' for i in n.items):return None
                return self.generic_visit(n)
            def visit_Expr(self,n):
                if isinstance(n.value,ast.Call) and isinstance(n.value.func,ast.Attribute) and isinstance(n.value.func.value,ast.Name) and n.value.func.value.id=='diagnostics':return None
                return self.generic_visit(n)
            def visit_Try(self,n):
                if len(n.handlers)==1 and isinstance(n.handlers[0].type,ast.Attribute) and n.handlers[0].type.attr=='TimeoutExpired':return n.body
                return self.generic_visit(n)
        old = next(n for n in a.body if isinstance(n,ast.FunctionDef) and n.name=='capture')
        new = next(n for n in b.body if isinstance(n,ast.FunctionDef) and n.name=='_capture')
        self.assertEqual(ast.dump(ast.Module(body=old.body,type_ignores=[])),ast.dump(StripRetention().visit(ast.Module(body=copy.deepcopy(new.body),type_ignores=[]))))
        old = next(n for n in a.body if isinstance(n,ast.FunctionDef) and n.name=='validate')
        new = next(n for n in b.body if isinstance(n,ast.FunctionDef) and n.name=='validate')
        self.assertEqual(ast.dump(old), ast.dump(StripRetention().visit(copy.deepcopy(new))))


class RetentionChecks(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory(prefix='R5-ENV-EVIDENCE-INDEPENDENT-20261004-',dir='/tmp')
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        patch = mock.patch.dict(os.environ, {'R5_DIAGNOSTIC_ROOT':str(self.root)})
        patch.start();self.addCleanup(patch.stop)

    def test_absent_opt_in_preserves_completed_failed_and_timed_out_return(self):
        result = subprocess.CompletedProcess(['go'],7,b'raw',b'error')
        errors = [subprocess.CalledProcessError(7,['go'],output=b'raw',stderr=b'error'),subprocess.TimeoutExpired(['go'],60,output=b'partial',stderr=b'error')]
        with mock.patch.dict(os.environ, {}, clear=True):
            with mock.patch.object(compatibility.subprocess,'run',return_value=result):self.assertIs(compatibility.diagnostic_run(['go']),result)
            for error in errors:
                with mock.patch.object(compatibility.subprocess,'run',side_effect=error),self.assertRaises(type(error)) as caught:compatibility.diagnostic_run(['go'])
                self.assertIs(caught.exception,error)
        self.assertEqual(list(self.root.iterdir()),[])

    def test_git_checkout_ancestor_and_symlink_markers_rejected(self):
        repo=self.root/'repo';repo.mkdir(mode=0o700)
        git=repo/'.git';git.mkdir();(git/'HEAD').write_text('ref: refs/heads/main\n')
        child=repo/'child';child.mkdir(mode=0o700)
        with mock.patch.dict(os.environ,{'R5_DIAGNOSTIC_ROOT':str(child)}),self.assertRaisesRegex(ValueError,'outside Git'):private.Retention('test')
        (git/'HEAD').unlink();git.rmdir();git.symlink_to(self.root/'missing')
        with mock.patch.dict(os.environ,{'R5_DIAGNOSTIC_ROOT':str(repo)}),self.assertRaisesRegex(ValueError,'outside Git'):private.Retention('test')

    def test_root_parent_escape_rejected(self):
        with mock.patch.dict(os.environ,{'R5_DIAGNOSTIC_ROOT':str(self.root/'..'/'escape')}),self.assertRaisesRegex(ValueError,'escape'):private.Retention('test')

    def test_root_replacement_does_not_redirect_opened_sink(self):
        with private.Retention('test') as sink:
            old=sink.path
            moved=self.root/'moved'
            old.rename(moved)
            old.symlink_to('/tmp')
            sink.write('unique-review-output',b'private')
            self.assertEqual((moved/'unique-review-output').read_bytes(),b'private')
            self.assertFalse(Path('/tmp/unique-review-output').exists())

    def test_count_and_total_bounds_accumulate_and_preserve_existing_files(self):
        with mock.patch.multiple(private,MAX_FILE_BYTES=4,MAX_TOTAL_BYTES=5,MAX_FILES=2),private.Retention('test') as sink:
            sink.write('a',b'123')
            with self.assertRaisesRegex(ValueError,'bound'):sink.write('b',b'123')
            sink.write('b',b'12')
            with self.assertRaisesRegex(ValueError,'bound'):sink.write('c',b'')
            self.assertEqual((sink.count,sink.total),(2,5))
            self.assertEqual({p.name:p.read_bytes() for p in sink.path.iterdir()},{'a':b'123','b':b'12'})

    def test_production_limits_and_exclusive_count_boundary(self):
        self.assertEqual((private.MAX_FILE_BYTES,private.MAX_TOTAL_BYTES,private.MAX_FILES),
                         (64*1024*1024,512*1024*1024,64))
        with private.Retention('test') as sink:
            with self.assertRaisesRegex(ValueError,'bound'):sink.write('oversize',b'x'*(private.MAX_FILE_BYTES+1))
            self.assertEqual(list(sink.path.iterdir()),[])
            for i in range(64):sink.write(str(i),b'')
            with self.assertRaisesRegex(ValueError,'bound'):sink.write('65',b'')
            self.assertEqual(len(list(sink.path.iterdir())),64)

    def test_operation_uuid_collision_exclusive(self):
        with mock.patch.object(private.uuid,'uuid4',return_value=type('UUID',(),{'hex':'fixed'})()):
            with private.Retention('test') as sink:sink.write('a',b'original')
            with self.assertRaises(FileExistsError):private.Retention('test')
        self.assertEqual((self.root/'test-fixed'/'a').read_bytes(),b'original')

    def test_constructor_fd_cleanup_for_bad_root_and_git_rejection(self):
        before=len(list(Path('/proc/self/fd').iterdir()))
        self.root.chmod(0o755)
        for _ in range(20):
            with self.assertRaises(ValueError):private.Retention('test')
        self.root.chmod(0o700)
        (self.root/'.git').write_text('gitdir: other')
        for _ in range(20):
            with self.assertRaises(ValueError):private.Retention('test')
        self.assertEqual(len(list(Path('/proc/self/fd').iterdir())),before)

    def test_partial_write_failure_unlinks_partial_and_resets_counters(self):
        original=os.fdopen
        class Broken:
            def __init__(self,fd):self.file=original(fd,'wb')
            def __enter__(self):return self
            def write(self,data):self.file.write(data[:2]);self.file.flush();raise OSError(errno.ENOSPC,'simulated full disk')
            def __exit__(self,*args):self.file.close()
        with private.Retention('test') as sink,mock.patch.object(private.os,'fdopen',side_effect=lambda fd,*a:Broken(fd)):
            before=len(list(Path('/proc/self/fd').iterdir()))
            with self.assertRaises(OSError):sink.write('partial',b'abcdef')
            self.assertEqual(list(sink.path.iterdir()),[])
            self.assertEqual((sink.count,sink.total),(0,0))
            self.assertEqual(len(list(Path('/proc/self/fd').iterdir())),before)

    def test_same_uid_operation_replacement_is_documented_trust_boundary(self):
        # An actor with our uid and directory authority can move files anywhere.
        # Record the mkdir/open gap without claiming protection from that actor.
        actual=os.mkdir
        def replace(name,*args,**kwargs):
            actual(name,*args,**kwargs)
            fd=kwargs['dir_fd']
            os.rmdir(name,dir_fd=fd)
            actual(name,0o755,dir_fd=fd)
            os.chmod(name,0o755,dir_fd=fd)
        with mock.patch.object(private.os,'mkdir',side_effect=replace),private.Retention('test') as sink:
            self.assertEqual(sink.path.stat().st_mode & 0o777,0o755)


class RequirementRegressions(RetentionChecks):
    """Do not change requirements to make candidate defects green."""
    # Restrict inherited test discovery to these methods via the custom suite below.
    def test_compatibility_bound_must_preserve_original_exception(self):
        for error in (subprocess.CalledProcessError(7,['go'],output=b'abc',stderr=b'e'),subprocess.TimeoutExpired(['go'],60,output=b'abc',stderr=b'e')):
            with self.subTest(kind=type(error).__name__),mock.patch.object(private,'MAX_FILE_BYTES',2),mock.patch.object(compatibility.subprocess,'run',side_effect=error):
                try:compatibility.diagnostic_run(['go'])
                except BaseException as observed:self.assertIs(observed,error)
                else:self.fail('original failure not raised')

    def test_compatibility_write_error_must_preserve_original_timeout(self):
        error=subprocess.TimeoutExpired(['go'],60,output=b'partial',stderr=b'e')
        with mock.patch.object(private.Retention,'write',side_effect=OSError(errno.ENOSPC,'simulated full disk')),mock.patch.object(compatibility.subprocess,'run',side_effect=error):
            try:compatibility.diagnostic_run(['go'])
            except BaseException as observed:self.assertIs(observed,error)
            else:self.fail('original timeout not raised')

    def test_compatibility_production_bound_must_preserve_original_failure(self):
        error=subprocess.CalledProcessError(7,['go'],output=b'x'*(private.MAX_FILE_BYTES+1),stderr=b'e')
        with mock.patch.object(compatibility.subprocess,'run',side_effect=error):
            try:compatibility.diagnostic_run(['go'])
            except BaseException as observed:self.assertIs(observed,error)
            else:self.fail('original failure not raised')

    def test_selected_bound_must_preserve_metadata_failure_and_timeout(self):
        base={'files':{'scripts/r5_selected_source_binding_v2.py':{}},'archive_static':{}}
        for result in (subprocess.CompletedProcess([],7,b'abc',b'e'),subprocess.TimeoutExpired(['go'],180,output=b'abc',stderr=b'e')):
            kwargs={'side_effect':result} if isinstance(result,Exception) else {'return_value':result}
            with self.subTest(kind=type(result).__name__),mock.patch.object(private,'MAX_FILE_BYTES',2), \
                 mock.patch.object(binding.inventory,'capture_current_source',return_value=base), \
                 mock.patch.object(binding,'topology',return_value=set()), \
                 mock.patch.object(binding,'digest',return_value={'scripts/r5_selected_source_binding_v2.py':binding._IMPLEMENTATION_SHA256}), \
                 mock.patch.object(binding.subprocess,'run',**kwargs):
                try:binding.capture(self.root,Path('/go'),cache=self.root,modulecache=self.root)
                except BaseException as observed:
                    if isinstance(result,Exception):self.assertIs(observed,result)
                    else:
                        self.assertIsInstance(observed,binding.MetadataCommandFailure)
                        self.assertEqual(observed.command['exit'],7)
                        self.assertEqual(observed.stdout,b'abc')
                else:self.fail('original failure not raised')

    def test_write_failure_must_not_close_reused_descriptor(self):
        actual=os.fdopen
        reuse=[]
        class ReuseAfterClose:
            def __init__(self,fd):self.fd=fd;self.file=actual(fd,'wb')
            def __enter__(self):return self
            def write(self,data):self.file.write(data[:2]);self.file.flush();raise OSError(errno.ENOSPC,'simulated full disk')
            def __exit__(self,*args):
                self.file.close()
                # Deterministic scheduling of another thread allocating after close.
                reuse.append(os.open('/dev/null',os.O_RDONLY))
                assert reuse[-1]==self.fd
        try:
            with private.Retention('test') as sink,mock.patch.object(private.os,'fdopen',side_effect=lambda fd,*a:ReuseAfterClose(fd)):
                with self.assertRaises(OSError):sink.write('partial',b'abcdef')
                try:os.fstat(reuse[0])
                except OSError:self.fail('diagnostic cleanup closed an unrelated reused descriptor')
        finally:
            for fd in reuse:
                try:os.close(fd)
                except OSError:pass


if __name__=='__main__':
    suite=unittest.TestSuite()
    loader=unittest.TestLoader()
    suite.addTests(loader.loadTestsFromTestCase(EnvironmentChecks))
    suite.addTests(loader.loadTestsFromTestCase(RetentionChecks))
    for name in RequirementRegressions.__dict__:
        if name.startswith('test_'):suite.addTest(RequirementRegressions(name))
    result=unittest.TextTestRunner(verbosity=2).run(suite)
    report=dict(source_sha=CANDIDATE,base_sha=BASE,tests_run=result.testsRun,
                failures=[t.id() for t,_ in result.failures],errors=[t.id() for t,_ in result.errors],
                skipped=[t.id() for t,_ in result.skipped],expected_failures=[t.id() for t,_ in result.expectedFailures])
    print(json.dumps(report,indent=2))
    raise SystemExit(0 if result.wasSuccessful() else 1)
