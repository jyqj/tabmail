"""Independent assertions, synthetic only. Reuse author fixture plumbing via AST.

No author test methods execute here; fixture source and tested files are pinned
in identities.json. All producer/filesystem/process boundaries are mocked.
"""
import sys
EVENTS = []
def guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty',
                 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        EVENTS.append(event)
        raise AssertionError('forbidden OS process: ' + event)
sys.addaudithook(guard)
import ast
import copy
import contextlib
import json
import os
import subprocess
import types
from pathlib import Path
import unittest
from unittest import mock

ROOT_REPO = Path(__file__).resolve().parents[4]
fixture = ROOT_REPO / 'scripts/tests/r5_external_runtime_v3_checks.py'
tree = ast.parse(fixture.read_text())
tree.body = [node for node in tree.body if not isinstance(node, ast.If)]
for node in tree.body:
    if isinstance(node, ast.ClassDef) and node.name == 'RuntimeV3Checks':
        node.body = [method for method in node.body
                     if not isinstance(method, ast.FunctionDef) or not method.name.startswith('test_')]
namespace = {'__file__': str(fixture), '__name__': 'fixture_plumbing'}
exec(compile(tree, str(fixture), 'exec'), namespace)
runtime, consumer, v3 = (namespace[name] for name in ('runtime', 'consumer', 'v3'))
wire, sha = runtime.canonical, runtime.digest

class IndependentChecks(namespace['RuntimeV3Checks']):
    def reset_input(self):
        self.bundle = copy.deepcopy(self.saved_bundle)
        self.files.update(self.saved_files)
        self.producer.reset_mock(); self.process.reset_mock()

    def test_fixed_pin_against_resealed_all_envelopes_and_bundle(self):
        self.saved_bundle = copy.deepcopy(self.bundle); self.saved_files = dict(self.files)
        for slot, path in (('default', self.default), ('race-r5protocol', self.race)):
            for index in range(12):
                for field, value in (('raw_stdout_bytes', 1234), ('raw_stdout_sha256', 'b'*64),
                                     ('stderr_bytes', 12), ('stderr_sha256', 'c'*64),
                                     ('role', 'substitution'), ('exit', 2), ('argv', ['/alien/go'])):
                    with self.subTest(slot=slot, index=index, field=field):
                        self.reset_input()
                        receipt = copy.deepcopy(self.receipts[slot])
                        receipt['observation_envelope'][index][field] = value
                        receipt = v3._seal(receipt); raw = wire(receipt)
                        self.files[path] = raw
                        self.bundle['observations'][slot].update(receipt_byte_sha256=sha(raw),
                            observation_sha256=v3.observation_digest(receipt))
                        self.files[self.bundle_path] = wire(self.bundle)
                        with self.assertRaises(ValueError): self.capture()
                        self.no_producer()

    def test_authenticated_malformed_policy_source_context_phase_producer(self):
        self.saved_bundle = copy.deepcopy(self.bundle); self.saved_files = dict(self.files)
        alterations = [('policy', 'wrong'), ('schema_version', True), ('source_root', '/alien/source'),
                       ('source_commit', 'f'*40), ('run_id', 'alien'), ('selected_binding_version', 2)]
        for field, value in alterations:
            with self.subTest(field=field):
                self.reset_input(); self.bundle[field] = value; self.repin()
                with self.assertRaises(ValueError): self.capture()
                self.no_producer()
        for kind in ('phase', 'context', 'producer', 'duplicate-path', 'missing-slot', 'receipt-policy', 'receipt-source'):
            with self.subTest(kind=kind):
                self.reset_input()
                if kind == 'phase':
                    self.bundle['observations']['before-default'] = self.bundle['observations'].pop('default')
                elif kind == 'context': self.bundle['observations']['default']['context'] = v3.CONTEXT
                elif kind == 'producer': self.bundle['producer']['path'] = '/alien/go'
                elif kind == 'duplicate-path': self.bundle['observations']['default']['receipt_path'] = self.race
                elif kind == 'missing-slot': self.bundle['observations'].pop('race-r5protocol')
                else:
                    receipt = copy.deepcopy(self.receipts['default'])
                    if kind == 'receipt-policy': receipt['policy'] = runtime.POLICY
                    else: receipt['base_source']['snapshot_root'] = '/alien/source'
                    receipt = v3._seal(receipt); raw = wire(receipt); self.files[self.default] = raw
                    self.bundle['observations']['default'].update(receipt_byte_sha256=sha(raw),
                        observation_sha256=v3.observation_digest(receipt))
                self.repin()
                with self.assertRaises(ValueError): self.capture()
                self.no_producer()

    def test_duplicate_and_malformed_json_even_with_matching_byte_pin(self):
        for raw in (b'{', b'null', b'[]', b'{"schema_version":1,"schema_version":1}', b'{"x":NaN}'):
            self.files[self.bundle_path] = raw
            with self.assertRaises(ValueError): self.capture(bundle_byte_sha256=sha(raw))
            self.no_producer()

    def test_first_return_retained_before_second_producer_and_new_pins(self):
        original = {p:self.files[p] for p in (self.default, self.race, self.bundle_path)}
        calls = []
        def fresh(receipt, *args, **kwargs):
            if calls:
                first = consumer._decode(self.files['/evidence/fresh/admission/default.json'])
                self.assertEqual(first, calls[0])
            value = self.fresh(receipt, *args, **kwargs); calls.append(copy.deepcopy(value)); return value
        self.producer.side_effect = fresh
        manifest = self.capture()
        self.assertEqual(original, {p:self.files[p] for p in original})
        admission = manifest['admitted_selection']; bundle = consumer._decode(self.files[admission['bundle_path']])
        self.assertNotEqual(admission['bundle_path'], self.bundle_path)
        self.assertEqual(admission['bundle_byte_sha256'], sha(self.files[admission['bundle_path']]))
        for slot, value in zip(consumer.ADMISSION_SLOTS, calls):
            pin = bundle['observations'][slot]
            self.assertEqual(pin['observation_sha256'], v3.observation_digest(value))
            self.assertNotEqual(pin['observation_sha256'], self.bundle['observations'][slot]['observation_sha256'])
            self.assertEqual(self.files[pin['receipt_path']], wire(value))
            self.assertEqual(manifest['receipt_hashes']['default' if slot=='default' else 'race'], sha(wire(value)))

    def test_equal_observations_require_new_paths_without_inequality_rule(self):
        self.producer.side_effect = lambda receipt,*a,**k: copy.deepcopy(receipt)
        manifest = self.capture(); bundle = consumer._decode(self.files[manifest['admitted_selection']['bundle_path']])
        for slot, pin in bundle['observations'].items():
            self.assertEqual(pin['observation_sha256'], self.bundle['observations'][slot]['observation_sha256'])
            self.assertNotEqual(pin['receipt_path'], self.bundle['observations'][slot]['receipt_path'])

    def test_failure_at_every_publish_stops_admission(self):
        for fail_at in (1,2,3):
            with self.subTest(fail_at=fail_at):
                self.producer.reset_mock()
                for path in list(self.files):
                    if path.startswith('/evidence/fresh/admission/'): del self.files[path]
                calls = []
                def publish(path, raw):
                    calls.append(path)
                    if len(calls)==fail_at: raise OSError(28, 'independent ENOSPC')
                    self.publish(path,raw)
                self.publisher.side_effect = publish
                with self.assertRaises(OSError): self.capture()
                self.assertEqual(self.producer.call_count, min(fail_at,2))
                self.assertNotIn('/evidence/fresh/admission/pins.json', self.files)

    def test_observe_rechecks_envelopes_after_filesystem_work(self):
        manifest = self.capture(); self.producer.reset_mock(); self.process.reset_mock()
        path = manifest['receipt_paths']['race']
        def mutate(argv, **kwargs):
            if argv[-1]=='--version':
                receipt = consumer._decode(self.files[path]); receipt['observation_envelope'][1]['raw_stdout_bytes'] += 1
                self.files[path] = wire(v3._seal(receipt))
            return self.process_output(argv, **kwargs)
        self.process.side_effect = mutate
        with self.assertRaises(ValueError): runtime.validate(manifest, selected_binding_version=3)
        self.producer.assert_not_called()
        self.assertEqual(self.process.call_count,3)

    def test_later_byte_reencoding_rejected_before_process(self):
        manifest = self.capture(); self.producer.reset_mock(); self.process.reset_mock()
        path = manifest['receipt_paths']['default']
        self.files[path] += b'\n'
        with self.assertRaises(ValueError): runtime.validate(manifest, selected_binding_version=3)
        self.no_producer()

    def test_fixed_manifest_pin_blocks_forged_delegated_pin(self):
        manifest = self.capture(); path = '/evidence/runtime.json'; raw = wire(manifest)
        self.files[path] = raw; fixed = sha(raw)
        bundle_path = manifest['admitted_selection']['bundle_path']
        self.files[bundle_path] += b'\n'
        candidate = copy.deepcopy(manifest); candidate['admitted_selection']['bundle_byte_sha256'] = sha(self.files[bundle_path])
        self.files[path] = wire(candidate)
        self.producer.reset_mock(); self.process.reset_mock()
        with self.assertRaisesRegex(ValueError,'manifest byte pin'): runtime.load_pinned(path,fixed,selected_binding_version=3)
        self.no_producer()

    def test_explicit_selector_matrix(self):
        manifest = self.capture(); self.producer.reset_mock(); self.process.reset_mock()
        for selector in (None, True, False, '3', 3.0, 1, 4, 2):
            with self.subTest(selector=selector), self.assertRaises(ValueError):
                runtime.validate(manifest,selected_binding_version=selector)
            self.no_producer()
        self.assertIs(runtime.selected,sys.modules['r5_selected_source_binding_v2'])
        self.assertEqual(runtime.selected.CONTEXT,v3.CONTEXT)
        with self.assertRaises(ValueError): runtime.validate(manifest)

    def test_same_byte_inode_replacement_and_nofollow(self):
        info = types.SimpleNamespace(st_dev=1,st_ino=2,st_mode=0o100400,
            st_size=5,st_mtime_ns=6,st_ctime_ns=7)
        replacement = copy.copy(info); replacement.st_ino=99
        stream = mock.MagicMock(); stream.__enter__.return_value=stream; stream.read.return_value=b'owned'
        with mock.patch.object(runtime.os,'stat',side_effect=[info,replacement]), \
             mock.patch.object(runtime.os,'open',return_value=7) as opened, \
             mock.patch.object(runtime.os,'fstat',return_value=info), \
             mock.patch.object(runtime.os,'dup',return_value=8), \
             mock.patch.object(runtime.os,'fdopen',return_value=stream), \
             mock.patch.object(runtime.os,'close') as closed:
            with self.assertRaisesRegex(ValueError,'changed during read'): runtime.Descriptors().read(101,'receipt.json')
            self.assertTrue(opened.call_args.args[1] & os.O_NOFOLLOW)
            closed.assert_called_once_with(7)

    def test_real_publisher_fsync_and_independent_readback_failure(self):
        info = types.SimpleNamespace(st_dev=1,st_ino=2,st_mode=0o100400,
            st_size=5,st_mtime_ns=6,st_ctime_ns=7)
        @contextlib.contextmanager
        def descriptors():
            yield types.SimpleNamespace(directory=lambda path:101,read=lambda *args:b'owned',file=lambda path:b'wrong')
        for fail_fsync in (False,True):
            stream=mock.MagicMock(); stream.__enter__.return_value=stream; stream.fileno.return_value=8
            with self.subTest(fail_fsync=fail_fsync), \
                 mock.patch.object(runtime,'descriptors',descriptors), \
                 mock.patch.object(runtime.os,'open',return_value=7) as opened, \
                 mock.patch.object(runtime.os,'dup',return_value=8), \
                 mock.patch.object(runtime.os,'fdopen',return_value=stream), \
                 mock.patch.object(runtime.os,'fsync',side_effect=OSError('fsync failed') if fail_fsync else None) as synced, \
                 mock.patch.object(runtime.os,'fstat',return_value=info), \
                 mock.patch.object(runtime.os,'stat',return_value=info), \
                 mock.patch.object(runtime.os,'close') as closed:
                with self.assertRaises(OSError if fail_fsync else ValueError): namespace['EXCLUSIVE']('/evidence/new.json',b'owned')
                flags=opened.call_args.args[1]
                self.assertEqual(flags & (os.O_EXCL|os.O_NOFOLLOW),os.O_EXCL|os.O_NOFOLLOW)
                self.assertEqual(opened.call_args.kwargs,dict(dir_fd=101))
                self.assertEqual(opened.call_args.args[2],0o400)
                synced.assert_called_once_with(8); closed.assert_called_once_with(7)

    def test_execution_timeout_exit_and_postcheck_preserve_primary(self):
        manifest=self.capture()
        command=[manifest['node']['path'],manifest['cli']['path'],*manifest['execution']['probe_argv'][:-1],'/reports/fresh.json']
        @contextlib.contextmanager
        def owner(manifest): yield 'independent-owner'
        with mock.patch.object(runtime,'owner',owner),mock.patch.object(runtime,'validate') as validate, \
             mock.patch.object(runtime.subprocess,'run') as run:
            timeout=subprocess.TimeoutExpired(command,180,output=b'out',stderr=b'err')
            secondary=ValueError('postcheck')
            validate.side_effect=[None,secondary]; run.side_effect=timeout
            with self.assertRaises(subprocess.TimeoutExpired) as caught: runtime.launch(manifest,command,env={},selected_binding_version=3)
            self.assertIs(caught.exception,timeout); self.assertIs(timeout.postcheck_error,secondary)
            self.assertEqual((timeout.stdout,timeout.stderr),(b'out',b'err'))
            validate.side_effect=[None,secondary]; run.side_effect=None
            run.return_value=subprocess.CompletedProcess(command,9,'output','errors')
            with self.assertRaises(subprocess.CalledProcessError) as caught: runtime.launch(manifest,command,env={},selected_binding_version=3)
            self.assertEqual((caught.exception.returncode,caught.exception.stdout,caught.exception.stderr),(9,'output','errors'))
            self.assertIs(caught.exception.postcheck_error,secondary)
            validate.side_effect=ValueError('precheck'); run.reset_mock()
            with self.assertRaises(ValueError): runtime.launch(manifest,command,env={},selected_binding_version=3)
            run.assert_not_called()

if __name__ == '__main__':
    result = unittest.TextTestRunner(verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(IndependentChecks))
    events = EVENTS + namespace['EVENTS']
    print(json.dumps(dict(tests=result.testsRun, failures=len(result.failures), errors=len(result.errors),
                         os_process_events=events, synthetic_only=True),sort_keys=True))
    raise SystemExit(0 if result.wasSuccessful() and not events else 1)
