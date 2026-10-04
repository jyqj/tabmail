"""Focused tooling controls; no product/formal dispatch or DB access."""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_go_environment as goenv
import r5_private_diagnostics as private
import check_r5_compatibility as compatibility
import check_r5_transactions as transactions
import r5_selected_source_binding_v2 as binding
import run_r5_source_version_tests as runner


class EnvironmentTests(unittest.TestCase):
    def test_explicit_tool_caches_and_no_inherited_flags(self):
        original = dict(R5_TEST_GO='/owned/go', R5_GO='/wrong/go', R5_TEST_CACHE='/owned/cache',
                        R5_TEST_MODULECACHE='/owned/mod', GOCACHE='/wrong/cache', GOFLAGS='-tags=wrong',
                        CGO_CFLAGS='-wrong', CGO_CFLAGS_ALLOW='.*', GOROOT='/wrong/root',
                        GOCACHEPROG='/wrong/cache-program', GOFIPS140='wrong', GOEXPERIMENT='wrong', PATH='/usr/bin', DATABASE_URL='preserved')
        go, env = goenv.selected(original)
        self.assertEqual(go, '/owned/go')
        self.assertEqual(env['R5_GO'], go)
        self.assertEqual(env['GOCACHE'], '/owned/cache')
        self.assertEqual(env['GOMODCACHE'], '/owned/mod')
        self.assertEqual(env['GOPATH'], '/owned')
        self.assertEqual(env['PATH'], '/owned:/usr/bin')
        self.assertEqual(env['GOFLAGS'], '')
        self.assertEqual(env['DATABASE_URL'], 'preserved')
        self.assertNotIn('CGO_CFLAGS', env)
        self.assertNotIn('GOEXPERIMENT', env)
        for key in ('CGO_CFLAGS_ALLOW','GOROOT','GOCACHEPROG','GOFIPS140'):
            self.assertNotIn(key,env)
        self.assertEqual(original['GOFLAGS'], '-tags=wrong')

    def test_legacy_selector_and_default_cache_preserved(self):
        go, env = goenv.selected(dict(R5_GO='/legacy/go', GOCACHE='/legacy/cache'))
        self.assertEqual(go, '/legacy/go')
        self.assertEqual(env['GOCACHE'], '/legacy/cache')

    def test_transaction_uses_selected_tool_and_offline_policy(self):
        with mock.patch.dict(os.environ, {'R5_TEST_GO':'/owned/go','R5_TEST_CACHE':'/owned/cache'}, clear=True), mock.patch.object(transactions.shutil, 'which', return_value=None), mock.patch.object(transactions.subprocess, 'run', return_value=subprocess.CompletedProcess([],0,'{}','')) as run:
            self.assertEqual(transactions.extract(), {})
        args, kwargs = run.call_args
        self.assertEqual(args[0][0], '/owned/go')
        self.assertEqual(kwargs['env']['GOCACHE'], '/owned/cache')
        self.assertEqual(kwargs['env']['GOPROXY'], 'off')
        self.assertEqual(kwargs['timeout'], 90)

    def test_compatibility_uses_selected_tool_and_original_command(self):
        def run(argv, **kwargs):
            if argv[0] == '/owned/go':
                Path(kwargs['env']['TABMAIL_ROUTE_INVENTORY_OUTPUT']).write_text('{}')
            return subprocess.CompletedProcess(argv,0,'{}','')
        with mock.patch.dict(os.environ, {'R5_TEST_GO':'/owned/go','R5_TEST_CACHE':'/owned/cache'}, clear=True), mock.patch.object(compatibility.subprocess, 'run', side_effect=run) as child:
            self.assertEqual(compatibility.collect(), ({},{}))
        args, kwargs = child.call_args_list[0]
        self.assertEqual(args[0], ['/owned/go','test','-mod=readonly','-count=1','./internal/architecture','-run','^TestR5RouteInventory$'])
        self.assertEqual(kwargs['env']['GOCACHE'], '/owned/cache')
        self.assertEqual(kwargs['timeout'], 60)

    def test_runner_dispatches_owned_environment_to_both_groups(self):
        ids = sorted(runner.HISTORICAL_IDS | {runner.FRESH_CLASS+'test_roundtrip'})
        environments = []
        def run(argv, **kwargs):
            environments.append(kwargs['env'])
            requested = json.loads(Path(argv[argv.index('--ids')+1]).read_text())
            Path(argv[argv.index('--output')+1]).write_text(json.dumps(dict(actual_test_ids=requested,source_sha='a'*40)))
            return subprocess.CompletedProcess(argv,0)
        with tempfile.TemporaryDirectory() as temp, mock.patch.dict(os.environ, {'R5_TEST_GO':'/owned/go','R5_TEST_CACHE':'/owned/cache','R5_TEST_MODULECACHE':'/owned/mod'}, clear=True), mock.patch.object(runner,'git',return_value='a'*40), mock.patch.object(runner,'checkout'), mock.patch.object(runner,'discover',return_value=ids), mock.patch.object(runner.preparation,'prepare',return_value=({}, {'R5_SOURCE_RUN_ID':'fresh'})), mock.patch.object(runner.subprocess,'run',side_effect=run):
            self.assertEqual(runner.run(temp,Path(temp)/'report.json'),0)
        self.assertEqual(len(environments),2)
        for env in environments:
            self.assertEqual(env['R5_GO'],'/owned/go')
            self.assertEqual(env['GOCACHE'],'/owned/cache')
            self.assertEqual(env['GOMODCACHE'],'/owned/mod')
        self.assertEqual(environments[0]['R5_SOURCE_RUN_ID'],'fresh')
        self.assertNotIn('R5_SOURCE_RUN_ID',environments[1])


class DiagnosticTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory(dir='/tmp')
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.env = mock.patch.dict(os.environ, {'R5_DIAGNOSTIC_ROOT':str(self.root)})
        self.env.start()
        self.addCleanup(self.env.stop)

    def test_disabled_does_not_create_files(self):
        with mock.patch.dict(os.environ, {}, clear=True), private.Retention('test') as sink:
            sink.command(0,b'raw',b'private',1)
            self.assertIsNone(sink.fd)
        self.assertEqual(list(self.root.iterdir()),[])

    def test_private_modes_hashes_and_fd_cleanup(self):
        with private.Retention('test') as sink:
            fd = sink.fd
            sink.command(0,b'raw',b'secret',7)
            directory = sink.path
            self.assertEqual(directory.stat().st_mode & 0o777,0o700)
        with self.assertRaises(OSError): os.fstat(fd)
        for path in directory.iterdir(): self.assertEqual(path.stat().st_mode & 0o777,0o600)
        summary = json.loads((directory/'0.summary.json').read_text())
        self.assertEqual(summary['returncode'],7)
        self.assertEqual(summary['stdout_sha256'],hashlib.sha256(b'raw').hexdigest())
        self.assertNotIn('secret',json.dumps(summary))

    def test_symlink_ancestor_and_git_roots_refused(self):
        target = self.root/'target';target.mkdir(mode=0o700)
        link = self.root/'link';link.symlink_to(target)
        for path in (link, link/'child'):
            with mock.patch.dict(os.environ, {'R5_DIAGNOSTIC_ROOT':str(path)}), self.assertRaises(OSError): private.Retention('test')
        (target/'.git').write_text('worktree')
        with mock.patch.dict(os.environ, {'R5_DIAGNOSTIC_ROOT':str(target)}), self.assertRaisesRegex(ValueError,'outside Git'): private.Retention('test')

    def test_unowned_and_public_root_refused(self):
        self.root.chmod(0o755)
        with self.assertRaisesRegex(ValueError,'owned mode700'): private.Retention('test')
        self.root.chmod(0o700)
        with mock.patch.object(private.os,'getuid',return_value=os.getuid()+1), self.assertRaises(ValueError): private.Retention('test')

    def test_escape_duplicate_and_symlink_file_refused(self):
        with private.Retention('test') as sink:
            for name in ('../escape','/escape','.', 'a/b'):
                with self.assertRaises(ValueError): sink.write(name,b'raw')
            sink.write('once',b'first')
            with self.assertRaises(FileExistsError): sink.write('once',b'replacement')
            (sink.path/'link').symlink_to(self.root/'outside')
            with self.assertRaises(FileExistsError): sink.write('link',b'raw')
            self.assertFalse((self.root/'outside').exists())
            self.assertEqual((sink.path/'once').read_bytes(),b'first')

    def test_file_total_and_count_bounds_refuse_new_output(self):
        for limits in ({'MAX_FILE_BYTES':2},{'MAX_TOTAL_BYTES':2},{'MAX_FILES':0}):
            with mock.patch.multiple(private,**limits), private.Retention('test') as sink:
                with self.assertRaisesRegex(ValueError,'bound'): sink.write('oversize',b'123')
                self.assertEqual(list(sink.path.iterdir()),[])

    def test_write_failure_removes_partial_and_closes_descriptor(self):
        with private.Retention('test') as sink:
            with mock.patch.object(private.os,'fdopen',side_effect=OSError('write failed')):
                with self.assertRaises(OSError): sink.write('partial',b'raw')
            self.assertFalse((sink.path/'partial').exists())

    def test_failed_compatibility_preserves_exception_and_raw(self):
        for error in (subprocess.CalledProcessError(7,['go'],output='raw',stderr='private'),subprocess.TimeoutExpired(['go'],60,output=b'raw',stderr=b'private')):
            with mock.patch.object(compatibility.subprocess,'run',side_effect=error):
                with self.assertRaises(type(error)) as caught: compatibility.diagnostic_run(['go'],check=True)
            self.assertIs(caught.exception,error)
        for directory in self.root.iterdir():
            self.assertEqual((directory/'0.stdout').read_bytes(),b'raw')
            self.assertEqual((directory/'0.stderr').read_bytes(),b'private')

    def test_rejected_observation_is_diagnostic_and_still_rejected(self):
        receipt = dict(schema_version=2,policy=binding.POLICY,base_source={'build_context':binding.DEFAULT_CONTEXT},commands=[],attestation_sha256='invalid',caller_replacement='forbidden')
        observed = dict(receipt,attestation_sha256='observed',commands=[dict(stdout_sha256='changed')])
        with mock.patch.object(binding,'capture',return_value=observed), self.assertRaisesRegex(ValueError,'missing/extra'): binding.validate(receipt,self.root,Path('/go'),cache=self.root,modulecache=self.root)
        saved = list(self.root.glob('rejected-selection-*/observed.json'))
        self.assertEqual(len(saved),1)
        self.assertEqual(json.loads(saved[0].read_text()),observed)

    def test_command_failure_and_timeout_retained_by_capture(self):
        base = {'files': {'scripts/r5_selected_source_binding_v2.py': {}}, 'archive_static': {}}
        errors = [subprocess.CompletedProcess([],7,b'raw list',b'private'),
                  subprocess.TimeoutExpired(['go'],180,output=b'partial',stderr=b'timed out')]
        for result in errors:
            patch = dict(side_effect=result) if isinstance(result,Exception) else dict(return_value=result)
            with mock.patch.object(binding.inventory,'capture_current_source',return_value=base), mock.patch.object(binding,'topology',return_value=set()), mock.patch.object(binding,'digest',return_value={'scripts/r5_selected_source_binding_v2.py':binding._IMPLEMENTATION_SHA256}), mock.patch.object(binding.subprocess,'run',**patch):
                with self.assertRaises((binding.MetadataCommandFailure,subprocess.TimeoutExpired)):
                    binding.capture(self.root,Path('/go'),cache=self.root,modulecache=self.root)
        self.assertEqual(sorted(p.read_bytes() for p in self.root.glob('selection-*/0.stdout')),[b'partial',b'raw list'])

    def test_valid_and_extra_or_changed_hash_receipts(self):
        observed = dict(schema_version=2,policy=binding.POLICY,base_source={'build_context':binding.DEFAULT_CONTEXT},commands=[dict(stdout_sha256='original')])
        def sign(value):
            return dict(value,attestation_sha256=hashlib.sha256(binding.inventory.canonical(value)).hexdigest())
        observed = sign(observed)
        with mock.patch.object(binding,'capture',return_value=observed):
            self.assertEqual(binding.validate(observed,self.root,Path('/go'),cache=self.root,modulecache=self.root),observed)
            for extra in (dict(caller_replacement='forbidden'),dict(commands=[dict(stdout_sha256='changed')])):
                candidate = sign({**{k:v for k,v in observed.items() if k!='attestation_sha256'},**extra})
                with self.assertRaisesRegex(ValueError,'missing/extra'):
                    binding.validate(candidate,self.root,Path('/go'),cache=self.root,modulecache=self.root)
        self.assertEqual(len(list(self.root.glob('rejected-selection-*/observed.json'))),2)

    def test_capture_sink_is_closed_after_failure(self):
        def capture(*args,diagnostics,**kwargs):
            self.fd = diagnostics.fd
            diagnostics.command(0,b'failed raw',b'private',1)
            raise ValueError('capture failure')
        with mock.patch.object(binding,'_capture',side_effect=capture), self.assertRaisesRegex(ValueError,'capture failure'): binding.capture(self.root,Path('/go'),cache=self.root,modulecache=self.root)
        with self.assertRaises(OSError): os.fstat(self.fd)
        self.assertEqual(len(list(self.root.glob('selection-*/0.stdout'))),1)


if __name__ == '__main__':
    unittest.main()
