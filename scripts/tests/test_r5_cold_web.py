"""Pure local fixture tests only; fake npm is not cold shipping-build evidence."""
import importlib.util
import contextlib
import io
import json
import os
import signal
from pathlib import Path
import subprocess
import sys
import tempfile
import time
import unittest
from unittest.mock import patch

SPEC = importlib.util.spec_from_file_location(
    'r5_cold_web', Path(__file__).resolve().parents[1] / 'run_r5_cold_web.py')
gate = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(gate)


class ColdWebTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory(prefix='r5-cold-unit-')
        self.addCleanup(self.temporary.cleanup)
        self.root = Path(self.temporary.name)
        self.repo = self.root / 'repo'
        self.repo.mkdir()
        self.git('init', '-q')
        self.git('config', 'user.email', 'fixture@example.invalid')
        self.git('config', 'user.name', 'fixture')
        for name, data in {
            'web/package.json': '{"scripts":{"build":"next build"}}',
            'web/package-lock.json': '{"lockfileVersion":3}',
            'web/Dockerfile': 'FROM node:22-alpine\nRUN npm ci\nRUN npm run build\n',
            'web/app/page.tsx': 'export default function Page() { return null; }\n',
            'web/.env.local': 'SECRET=fixture-never-copy\n',
            'web/credentials/token.key': 'fixture-never-copy\n',
            'web/.npmrc': '//registry.example.invalid/:_authToken=fixture\n',
            'web/node_modules/stale.js': 'stale dependency',
            'web/.next/BUILD_ID': 'stale-build',
            'web/tsconfig.tsbuildinfo': 'stale-types',
            'web/credentials.json': 'fixture-never-copy',
            'web/.netrc': 'fixture-never-copy',
        }.items():
            self.write(name, data)
        self.git('add', '.')
        self.git('commit', '-qm', 'fixture')

    def git(self, *argv):
        return subprocess.check_output(['git', '-C', str(self.repo), *argv], stderr=subprocess.DEVNULL)

    def write(self, name, data):
        path = self.repo / name
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(data)
        return path

    def output(self, name='evidence'):
        return gate.private_output(self.root / name, self.repo)

    def prepare(self, dirty=(), name='evidence'):
        output = self.output(name)
        return output, gate.prepare_snapshot(self.repo, output, list(dirty))

    def tools(self, build_exit=0, missing_artifacts=False):
        node_bin = self.root / 'tools'
        node_bin.mkdir()
        node = node_bin / 'node'
        node.write_text('#!/bin/sh\necho v22.23.2\n')
        npm = node_bin / 'npm'
        npm.write_text('''#!/bin/sh
case "$1" in
--version) echo 10.9.4 ;;
ci) mkdir node_modules; echo fresh-install ;;
run) echo fresh-build
''' + ('' if missing_artifacts else '''mkdir -p .next/standalone .next/static
printf fresh > .next/BUILD_ID
printf server > .next/standalone/server.js
''') + 'exit ' + str(build_exit) + ''' ;;
esac
''')
        node.chmod(0o700)
        npm.chmod(0o700)
        return node_bin

    def test_tracked_snapshot_excludes_credentials_and_caches_without_touching_checkout(self):
        output, receipt = self.prepare()
        web = output / 'source/web'
        self.assertTrue((web / 'app/page.tsx').is_file())
        for name in ('.env.local', '.npmrc', 'credentials', 'node_modules', '.next', 'tsconfig.tsbuildinfo', 'credentials.json', '.netrc'):
            self.assertFalse((web / name).exists(), name)
        self.assertEqual((self.repo / 'web/.next/BUILD_ID').read_text(), 'stale-build')
        self.assertEqual(receipt['head'], self.git('rev-parse', 'HEAD').decode().strip())
        self.assertEqual(receipt['dirty_delta'], [])
        self.assertTrue(receipt['cold_before_install'])
        self.assertEqual(output.stat().st_mode & 0o777, 0o700)
        self.assertEqual((output / 'source-receipt.json').stat().st_mode & 0o777, 0o600)

    def test_explicit_dirty_overlay_untracked_and_delete(self):
        self.write('web/app/page.tsx', 'new page')
        self.write('web/lib/new.ts', 'new import')
        (self.repo / 'web/Dockerfile').unlink()
        # Required Dockerfile deletion is rightly rejected rather than pretending a build input exists.
        with self.assertRaisesRegex(gate.GateError, 'required tracked build input missing'):
            self.prepare(['web/app/page.tsx', 'web/lib/new.ts', 'web/Dockerfile'])
        self.write('web/Dockerfile', 'FROM node:22-alpine\nRUN npm ci\nRUN npm run build\n')
        self.write('web/delete-me.ts', 'delete')
        self.git('add', 'web/delete-me.ts')
        self.git('commit', '-qm', 'tracked removable')
        (self.repo / 'web/delete-me.ts').unlink()
        output, receipt = self.prepare(['web/app/page.tsx', 'web/lib/new.ts', 'web/delete-me.ts'], 'valid')
        self.assertEqual((output / 'source/web/app/page.tsx').read_text(), 'new page')
        self.assertEqual((output / 'source/web/lib/new.ts').read_text(), 'new import')
        self.assertFalse((output / 'source/web/delete-me.ts').exists())
        actions = {entry['path']: entry['action'] for entry in receipt['dirty_delta']}
        self.assertEqual(actions['web/delete-me.ts'], 'delete')
        self.assertEqual(len(receipt['dirty_delta'][0]['sha256']), 64)

    def test_missing_extra_and_duplicate_review_rejected(self):
        self.write('web/app/page.tsx', 'dirty')
        for dirty in ([], ['web/app/page.tsx', 'web/unknown.ts'], ['web/app/page.tsx'] * 2):
            with self.subTest(dirty=dirty), self.assertRaises(gate.GateError):
                self.prepare(dirty, 'bad-' + str(len(list(self.root.iterdir()))))

    def test_dirty_lockfile_and_bypassed_build_script_refused(self):
        self.write('web/package-lock.json', '{\"lockfileVersion\":2}')
        with self.assertRaisesRegex(gate.GateError, 'original tracked lockfile required'):
            self.prepare(['web/package-lock.json'])
        self.git('checkout', '--', 'web/package-lock.json')
        self.write('web/package.json', '{\"scripts\":{\"build\":\"echo fake\"}}')
        with self.assertRaisesRegex(gate.GateError, 'real next build script required'):
            self.prepare(['web/package.json'], 'bypass')

    def test_traversal_foreign_and_secret_paths_rejected(self):
        for path in ('/web/x', '../web/x', 'web/../x', 'web//x', 'internal/go.go'):
            with self.subTest(path=path), self.assertRaises(gate.GateError):
                gate.admissible(path)
        for path in ('web/.env', 'web/.env.production', 'web/x/private.pem', 'web/node_modules/a'):
            self.assertFalse(gate.admissible(path))
        with self.assertRaisesRegex(gate.GateError, 'forbidden'):
            self.prepare(['web/.env.local'])

    def test_tracked_symlink_and_dirty_symlink_refused(self):
        (self.repo / 'web/link.ts').symlink_to('app/page.tsx')
        self.git('add', 'web/link.ts')
        self.git('commit', '-qm', 'unsafe tracked link')
        with self.assertRaisesRegex(gate.GateError, 'unsafe archive member'):
            self.prepare()
        self.git('rm', '-q', 'web/link.ts')
        self.git('commit', '-qm', 'remove link')
        (self.repo / 'web/app/page.tsx').unlink()
        (self.repo / 'web/app/page.tsx').symlink_to(self.repo / 'web/package.json')
        with self.assertRaisesRegex(gate.GateError, 'symlink source refused'):
            self.prepare(['web/app/page.tsx'], 'dirty-link')

    def test_dirty_capture_drift_refused(self):
        self.write('web/app/page.tsx', 'first')
        original = gate.regular_bytes
        calls = 0
        def drifting(path, root):
            nonlocal calls
            data = original(path, root)
            calls += 1
            if calls == 1:
                path.write_text('second')
            return data
        with patch.object(gate, 'regular_bytes', side_effect=drifting):
            with self.assertRaisesRegex(gate.GateError, 'changed during capture'):
                self.prepare(['web/app/page.tsx'])

    def test_output_refuses_existing_checkout_and_non_temporary_path(self):
        output = self.output()
        (output / 'untouched').write_text('original')
        with self.assertRaises(FileExistsError):
            self.output()
        self.assertEqual((output / 'untouched').read_text(), 'original')
        with self.assertRaisesRegex(gate.GateError, 'checkout'):
            gate.private_output(self.repo / 'evidence', self.repo)
        with self.assertRaisesRegex(gate.GateError, 'temporary storage'):
            gate.private_output(Path('/Users/new-cold-evidence'), self.repo)

    def test_environment_is_allowlisted_and_uses_private_empty_caches(self):
        output = self.output()
        with patch.dict(os.environ, {'SECRET': 'do-not-copy', 'NODE_OPTIONS': '--require bad',
                                     'NPM_TOKEN': 'do-not-copy', 'NEXT_PUBLIC_SECRET': 'do-not-copy'}):
            env = gate.build_environment(output, self.tools())
        for key in ('SECRET', 'NODE_OPTIONS', 'NPM_TOKEN', 'NEXT_PUBLIC_SECRET'):
            self.assertNotIn(key, env)
        self.assertEqual(Path(env['HOME']), output / 'home')
        self.assertEqual(list(Path(env['npm_config_cache']).iterdir()), [])
        self.assertEqual(Path(env['npm_config_userconfig']).read_bytes(), b'')

    def test_preparation_never_runs_node_and_is_not_build_pass(self):
        with patch.object(gate, 'execute_build', side_effect=AssertionError('must not execute')):
            self.assertEqual(gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'prepared')]), 0)
        receipt = json.loads((self.root / 'prepared/cold-build-receipt.json').read_text())
        self.assertEqual(receipt['status'], 'prepared_not_executed')
        self.assertFalse(receipt['cold_next_build_passed'])
        self.assertFalse(receipt['shipping_image_passed'])

    def test_execute_requires_window_and_explicit_tool_path(self):
        with self.assertRaises(SystemExit):
            gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'no-window'), '--execute'])
        self.assertFalse((self.root / 'no-window').exists())

    def test_execute_cli_rejects_source_identity_drift_before_npm(self):
        with patch.object(gate, 'execute_build', side_effect=AssertionError('must not execute')):
            rc = gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'drift'),
                            '--execute', '--runtime-window', 'fixture', '--node-bin', '/unused',
                            '--expected-source-identity', '0' * 64])
        self.assertEqual(rc, 1)
        self.assertIn('source identity drift', json.loads((self.root / 'drift/error.json').read_text())['error'])
        self.assertFalse((self.root / 'drift/npm-ci.json').exists())

    def test_execute_cli_passes_only_matching_prepared_identity(self):
        _, source = self.prepare()
        with patch.object(gate, 'execute_build', return_value={'status': 'cold_next_build_passed'}) as execute:
            rc = gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'match'),
                            '--execute', '--runtime-window', 'fixture', '--node-bin', '/unused',
                            '--expected-source-identity', source['source_identity_sha256']])
        self.assertEqual(rc, 0)
        execute.assert_called_once()
        self.assertEqual(execute.call_args[0][1]['source_identity_sha256'], source['source_identity_sha256'])

    def test_fake_build_command_receipts_are_exact_and_not_shipping_browser_G0(self):
        output, source = self.prepare()
        result = gate.execute_build(output, source, self.tools(), 'fixture-owner-window', 3)
        self.assertEqual(result['status'], 'cold_next_build_passed')
        self.assertEqual(result['commands'][-2]['argv'], ['npm', 'ci'])
        self.assertEqual(result['commands'][-1]['argv'], ['npm', 'run', 'build'])
        for command in result['commands']:
            self.assertEqual(command['exit_code'], 0)
            self.assertTrue(command['started_at'])
            self.assertTrue(command['ended_at'])
            self.assertEqual(len(command['stdout_sha256']), 64)
            self.assertIn('env_allowlist', command)
        for key in ('shipping_image_passed', 'browser_passed', 'G0_passed'):
            self.assertFalse(result[key])
        self.assertTrue(result['lockfile_unchanged'])
        self.assertEqual((self.repo / 'web/.next/BUILD_ID').read_text(), 'stale-build')

    def test_failed_build_preserves_red_raw_and_exit(self):
        output, source = self.prepare()
        result = gate.execute_build(output, source, self.tools(build_exit=7), 'fixture', 3)
        self.assertEqual(result['status'], 'npm-build_failed')
        self.assertEqual(result['commands'][-1]['exit_code'], 7)
        self.assertEqual((output / 'npm-build.stdout').read_text().strip(), 'fresh-build')
        self.assertEqual(json.loads((output / 'cold-build-receipt.json').read_text())['status'], 'npm-build_failed')

    def test_zero_exit_without_standalone_artifacts_is_not_pass(self):
        output, source = self.prepare()
        result = gate.execute_build(output, source, self.tools(missing_artifacts=True), 'fixture', 3)
        self.assertEqual(result['status'], 'build_artifacts_missing')

    def test_timeout_and_log_budget_are_failure_even_if_exit_zero(self):
        output = self.output()
        command = gate.run_command(output, 'timeout', ['/bin/sh', '-c', 'sleep 20'], self.repo, {'PATH': '/bin:/usr/bin'}, 0.1)
        self.assertTrue(command['timed_out'])
        self.assertFalse(gate.command_passed(command))
        with patch.object(gate, 'MAX_LOG_BYTES', 3):
            command = gate.run_command(output, 'log-limit', ['/bin/sh', '-c', 'printf too-many-bytes'], self.repo, {}, 3)
        self.assertTrue(command['log_limit_exceeded'])
        self.assertFalse(gate.command_passed(command))

    def test_owned_child_group_cleanup_even_when_leader_exits_zero(self):
        output = self.output()
        command = gate.run_command(output, 'orphan', ['/bin/sh', '-c', 'sleep 20 & echo child=$!; exit 0'],
                                   self.repo, {'PATH': '/bin:/usr/bin'}, 1)
        self.assertEqual(command['exit_code'], 0)
        self.assertEqual(command['pid'], command['pgid'])
        self.assertEqual(command['pid'], command['sid'])
        self.assertTrue(command['cleanup_verified'])
        self.assertEqual(command['group_cleanup']['final']['live_pids'], [])
        self.assertTrue(command['group_cleanup']['signals'])
        self.assertTrue(gate.command_passed(command))

    def test_unknown_or_foreign_cleanup_never_kills_or_passes(self):
        for observed in ({'unknown': True, 'ownership_confirmed': False, 'live_pids': []},
                         {'unknown': False, 'ownership_confirmed': False, 'live_pids': [123]}):
            fake = unittest.mock.Mock(pid=123, returncode=0)
            with self.subTest(observed=observed), patch.object(gate, 'observe_group', return_value=observed), patch.object(gate.os, 'killpg') as kill:
                cleanup = gate.cleanup_group(fake, 0.1)
            kill.assert_not_called()
            self.assertFalse(cleanup['verified_no_live_group'])
        self.assertFalse(gate.command_passed({'exit_code': 0, 'timed_out': False,
                                             'log_limit_exceeded': False, 'cleanup_verified': False}))

    def fake_timed_command(self, output, stage, argv, cwd, env, timeout, cleanup_budget, deadline=None):
        time.sleep(0.12)
        (output / (stage + '.stdout')).write_text('v22.23.2\n' if stage == 'node-version' else '10.9.4\n')
        record = {'stage': stage, 'argv': argv, 'exit_code': 0, 'timed_out': False,
                  'log_limit_exceeded': False, 'cleanup_verified': True, 'timeout_seconds': timeout}
        gate.write_json(output / (stage + '.json'), record)
        return record

    def test_total_deadline_clips_stage_budget_and_stops_before_ci(self):
        output, source = self.prepare()
        with patch.object(gate, 'run_command', side_effect=self.fake_timed_command) as run:
            with self.assertRaisesRegex(gate.GateError, 'deadline exhausted'):
                gate.execute_build(output, source, self.tools(), 'fixture', 300, total_timeout=0.25)
        result = json.loads((output / 'cold-build-receipt.json').read_text())
        self.assertEqual(result['status'], 'execution_error')
        self.assertTrue(result['budget_exhausted'])
        self.assertEqual(run.call_count, 2)
        self.assertLess(run.call_args_list[1].args[5], run.call_args_list[0].args[5])
        self.assertLess(run.call_args_list[0].args[5], 0.25)
        self.assertFalse((output / 'npm-ci.json').exists())

    def test_interruption_persists_current_group_cleanup_in_terminal_receipt(self):
        output = self.output()
        real_popen = gate.subprocess.Popen
        process_holder = []
        def owned_process(*args, **kwargs):
            p = real_popen(*args, **kwargs)
            process_holder.append(p)
            return p
        real_sleep = gate.time.sleep
        interrupted = False
        def once_interrupt(seconds):
            nonlocal interrupted
            if not interrupted:
                interrupted = True
                raise KeyboardInterrupt()
            real_sleep(seconds)
        with patch.object(gate.subprocess, 'Popen', side_effect=owned_process), patch.object(gate.time, 'sleep', side_effect=once_interrupt):
            with self.assertRaises(KeyboardInterrupt):
                gate.run_command(output, 'interrupted', ['/bin/sh', '-c', 'sleep 20'],
                                 self.repo, {'PATH': '/bin:/usr/bin'}, 1)
        record = json.loads((output / 'interrupted.json').read_text())
        self.assertEqual(record['error'].split(':')[0], 'KeyboardInterrupt')
        self.assertTrue(record['cleanup_verified'])
        self.assertEqual(record['group_cleanup']['final']['live_pids'], [])
        self.assertIsNotNone(record['ended_at'])
        self.assertIsNotNone(process_holder[0].poll())

    def test_user_and_global_npm_configs_are_distinct_empty_private_files(self):
        output = self.output()
        env = gate.build_environment(output, self.tools())
        user, global_file = Path(env['npm_config_userconfig']), Path(env['npm_config_globalconfig'])
        self.assertNotEqual(user, global_file)
        for config in (user, global_file):
            self.assertEqual(config.read_bytes(), b'')
            self.assertEqual(config.stat().st_mode & 0o777, 0o600)

    def test_cli_absolute_deadline_interrupts_slow_capture_without_node(self):
        before = time.monotonic()
        previous_handler = signal.getsignal(signal.SIGALRM)
        def slow_capture(*args, **kwargs):
            time.sleep(20)
            raise AssertionError('deadline must interrupt capture')
        with patch.object(gate, 'prepare_snapshot', side_effect=slow_capture), patch.object(gate, 'execute_build') as execute:
            rc = gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'slow-capture'),
                            '--execute', '--runtime-window', 'fixture', '--node-bin', '/unused',
                            '--expected-source-identity', '0' * 64, '--total-timeout-seconds', '1'])
        self.assertEqual(rc, 1)
        execute.assert_not_called()
        self.assertLess(time.monotonic() - before, 1.5)
        self.assertEqual(signal.getsignal(signal.SIGALRM), previous_handler)
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))
        receipt = json.loads((self.root / 'slow-capture/lifecycle-receipt.json').read_text())
        self.assertEqual(receipt['status'], 'failed')
        self.assertTrue(receipt['deadline_exceeded'])
        self.assertFalse((self.root / 'slow-capture/npm-ci.json').exists())

    def test_source_git_clips_remaining_budget_and_rejects_late_return(self):
        clock = [10.0]
        def late_git(*args, **kwargs):
            self.assertLessEqual(kwargs['timeout'], 0.021)
            clock[0] += 0.03
            return b'late source'
        with patch.object(gate.time, 'monotonic', side_effect=lambda: clock[0]), patch.object(gate.subprocess, 'check_output', side_effect=late_git):
            with self.assertRaisesRegex(gate.DeadlineExpired, 'source git completion'):
                gate.git(self.repo, 'archive', 'HEAD', deadline=10.02)

    def test_execute_inherits_capture_deadline_without_fresh_budget(self):
        output, source = self.prepare()
        clock = [0.8]
        def command(output, stage, argv, cwd, env, timeout, cleanup_budget, deadline=None):
            self.assertEqual(deadline, 1.0)
            self.assertLessEqual(timeout, 0.100001)
            clock[0] += 0.12
            (output / (stage + '.stdout')).write_text('v22.23.2\n')
            record = {'exit_code': 0, 'timed_out': False, 'log_limit_exceeded': False,
                      'cleanup_verified': True, 'stage': stage}
            gate.write_json(output / (stage + '.json'), record)
            return record
        with patch.object(gate.time, 'monotonic', side_effect=lambda: clock[0]), patch.object(gate, 'run_command', side_effect=command) as run:
            with self.assertRaises(gate.DeadlineExpired):
                gate.execute_build(output, source, self.tools(), 'fixture', 300,
                                   total_timeout=1, deadline=1.0, lifecycle_started=0.0)
        self.assertEqual(run.call_count, 1)
        result = json.loads((output / 'cold-build-receipt.json').read_text())
        self.assertTrue(result['lifecycle_includes_capture'])
        self.assertTrue(result['budget_exhausted'])
        self.assertEqual(result['elapsed_seconds'], 0.92)
        self.assertFalse((output / 'npm-ci.json').exists())

    def test_group_probe_timeout_clips_remaining_and_expired_is_unknown(self):
        def probe(*args, **kwargs):
            self.assertGreater(kwargs['timeout'], 0)
            self.assertLessEqual(kwargs['timeout'], 0.031)
            raise subprocess.TimeoutExpired(args[0], kwargs['timeout'])
        with patch.object(gate.subprocess, 'run', side_effect=probe):
            observed = gate.observe_group(999999, deadline=time.monotonic() + 0.03)
        self.assertTrue(observed['unknown'])
        self.assertFalse(observed['group_absent'])
        with patch.object(gate.subprocess, 'run') as probe:
            with self.assertRaises(gate.DeadlineExpired):
                gate.observe_group(999999, deadline=time.monotonic() - 1)
        probe.assert_not_called()

    def test_slow_cleanup_observation_cannot_get_fresh_final_probe_or_absence(self):
        clock = [0.0]
        fake = unittest.mock.Mock(pid=123, returncode=0)
        def late_absence(pgid, deadline=None):
            self.assertLessEqual(deadline, 0.1)
            clock[0] = 0.11
            return {'unknown': False, 'ownership_confirmed': True, 'live_pids': [], 'group_absent': True}
        with patch.object(gate.time, 'monotonic', side_effect=lambda: clock[0]), patch.object(gate, 'observe_group', side_effect=late_absence) as observe, patch.object(gate.os, 'killpg') as kill:
            cleanup = gate.cleanup_group(fake, budget=5, deadline=0.1)
        self.assertEqual(observe.call_count, 1)
        kill.assert_not_called()
        self.assertFalse(cleanup['verified_no_live_group'])
        self.assertTrue(cleanup['final']['unknown'])
        self.assertFalse(cleanup['final']['group_absent'])

    def test_unobserved_live_owned_group_stops_precisely_but_never_certifies(self):
        fake = unittest.mock.Mock(pid=123, returncode=None)
        unknown = gate.unknown_group(123, 'probe timeout')
        with patch.object(gate, 'observe_group', return_value=unknown), patch.object(gate.os, 'getsid', return_value=123), patch.object(gate.os, 'getpgid', return_value=123), patch.object(gate.os, 'killpg') as kill:
            cleanup = gate.cleanup_group(fake, budget=0.1)
        kill.assert_called_once_with(123, signal.SIGKILL)
        self.assertFalse(cleanup['verified_no_live_group'])
        self.assertTrue(cleanup['final']['unknown'])
        self.assertFalse(cleanup['final']['group_absent'])
        foreign = {**unknown, 'unknown': False, 'ownership_confirmed': False, 'live_pids': [123]}
        with patch.object(gate, 'observe_group', return_value=foreign), patch.object(gate.os, 'killpg') as kill:
            cleanup = gate.cleanup_group(fake, budget=0.1)
        kill.assert_not_called()
        self.assertFalse(cleanup['verified_no_live_group'])

    def test_existing_alarm_is_not_overwritten(self):
        with patch.object(gate.signal, 'getitimer', return_value=(2, 0)), patch.object(gate.signal, 'setitimer') as arm:
            with self.assertRaisesRegex(gate.GateError, 'existing process alarm refused'):
                with gate.lifecycle_alarm(time.monotonic() + 1):
                    raise AssertionError('must not enter')
        arm.assert_not_called()

    def test_deadline_between_cleanup_probes_persists_unknown_not_absence(self):
        output = self.output()
        with patch.object(gate, 'cleanup_group', side_effect=gate.DeadlineExpired('cleanup hard deadline')):
            with self.assertRaises(gate.DeadlineExpired):
                gate.run_command(output, 'cleanup-expiry', ['/bin/sh', '-c', 'exit 0'],
                                 self.repo, {'PATH': '/bin:/usr/bin'}, 1)
        record = json.loads((output / 'cleanup-expiry.json').read_text())
        self.assertFalse(record['cleanup_verified'])
        self.assertTrue(record['group_cleanup']['final']['unknown'])
        self.assertFalse(record['group_cleanup']['final']['group_absent'])
        self.assertIn('DeadlineExpired', record['cleanup_error'])
        self.assertIsNotNone(record['ended_at'])
        self.assertFalse(gate.command_passed(record))

    def test_lifecycle_timeout_cannot_leave_earlier_execution_pass_admitted(self):
        _, source = self.prepare()
        def slow_terminal(output, *args, **kwargs):
            gate.write_json(output / 'cold-build-receipt.json', {'status': 'cold_next_build_passed'})
            time.sleep(20)
            raise AssertionError('absolute lifecycle deadline must interrupt')
        with patch.object(gate, 'execute_build', side_effect=slow_terminal):
            rc = gate.main(['--repo', str(self.repo), '--output-dir', str(self.root / 'terminal-expiry'),
                            '--execute', '--runtime-window', 'fixture', '--node-bin', '/unused',
                            '--expected-source-identity', source['source_identity_sha256'],
                            '--total-timeout-seconds', '1'])
        self.assertEqual(rc, 1)
        receipt = json.loads((self.root / 'terminal-expiry/cold-build-receipt.json').read_text())
        self.assertEqual(receipt['status'], 'lifecycle_failed')
        self.assertFalse(receipt['admitted'])
        self.assertFalse(receipt['cold_next_build_passed'])

    def atomic_cli_args(self, name):
        return ['--repo', str(self.repo), '--output-dir', str(self.root / name),
                '--execute', '--runtime-window', 'fixture-tiny-only', '--node-bin', '/unused',
                '--expected-source-identity', '0' * 64, '--total-timeout-seconds', '1']

    def fake_atomic_execution(self, output, *args, **kwargs):
        result = {'status': 'cold_next_build_passed', 'cold_next_build_passed': True,
                  'fixture_only': True, 'shipping_image_passed': False,
                  'browser_passed': False, 'G0_passed': False}
        gate.write_json(output / 'cold-build-receipt.json', result)
        return result

    def test_atomic_final_receipt_actual_alarm_never_leaves_machine_pass(self):
        real_write = gate.write_json
        before = time.monotonic()
        previous_handler = signal.getsignal(signal.SIGALRM)
        def slow_final(path, value):
            if path.name == 'lifecycle-receipt.json':
                time.sleep(20)
            return real_write(path, value)
        captured = io.StringIO()
        with patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=self.fake_atomic_execution), patch.object(gate, 'write_json', side_effect=slow_final), contextlib.redirect_stdout(captured):
            rc = gate.main(self.atomic_cli_args('atomic-final-expiry'))
        self.assertEqual(rc, 1)
        self.assertLess(time.monotonic() - before, 1.5)
        cold = json.loads((self.root / 'atomic-final-expiry/cold-build-receipt.json').read_text())
        self.assertNotEqual(cold['status'], 'cold_next_build_passed')
        self.assertFalse(cold['cold_next_build_passed'])
        self.assertFalse(cold['admitted'])
        self.assertNotIn('cold_next_build_passed', captured.getvalue())
        self.assertEqual(signal.getsignal(signal.SIGALRM), previous_handler)
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_atomic_parser_is_under_alarm_before_constructor_path_work(self):
        before = time.monotonic()
        def slow_parser(*args, **kwargs):
            self.assertGreater(signal.getitimer(signal.ITIMER_REAL)[0], 0)
            time.sleep(20)
            raise AssertionError('parser must be interrupted')
        with patch.object(gate.argparse, 'ArgumentParser', side_effect=slow_parser), patch.object(gate, 'prepare_snapshot') as capture:
            rc = gate.main(self.atomic_cli_args('atomic-parser-expiry'))
        self.assertEqual(rc, 1)
        self.assertLess(time.monotonic() - before, 1.5)
        capture.assert_not_called()
        self.assertFalse((self.root / 'atomic-parser-expiry').exists())
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_atomic_observe_expiry_propagates_and_failure_io_stays_hard_bounded(self):
        real_write = gate.write_json
        before = time.monotonic()
        propagated = []
        def slow_probe(*args, **kwargs):
            time.sleep(20)
        def execution(output, *args, **kwargs):
            self.fake_atomic_execution(output)
            try:
                gate.observe_group(999999, deadline=gate._ACTIVE_GUARD.work_deadline)
            except gate.DeadlineExpired:
                propagated.append(True)
                raise
            raise AssertionError('work expiry must not become unknown')
        def slow_failure(path, value):
            if path.name == 'error.json':
                time.sleep(20)
            return real_write(path, value)
        with patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=execution), patch.object(gate.subprocess, 'run', side_effect=slow_probe), patch.object(gate, 'write_json', side_effect=slow_failure):
            rc = gate.main(self.atomic_cli_args('atomic-one-shot-expiry'))
        self.assertEqual(rc, 1)
        self.assertEqual(propagated, [True])
        self.assertLess(time.monotonic() - before, 1.5)
        cold = json.loads((self.root / 'atomic-one-shot-expiry/cold-build-receipt.json').read_text())
        self.assertFalse(cold['cold_next_build_passed'])
        self.assertFalse(cold['admitted'])
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_atomic_success_publishes_once_only_after_complete_receipts(self):
        real_admit = gate.atomic_admit
        observed = []
        def admit(output, result, guard):
            cold = json.loads((output / 'cold-build-receipt.json').read_text())
            self.assertFalse(cold['cold_next_build_passed'])
            self.assertTrue((output / 'lifecycle-receipt.json').is_file())
            self.assertTrue((output / '.cold-build-not-admitted').is_file())
            guard.check_hard()
            observed.append(True)
            return real_admit(output, result, guard)
        captured = io.StringIO()
        with patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=self.fake_atomic_execution), patch.object(gate, 'atomic_admit', side_effect=admit) as publish, contextlib.redirect_stdout(captured):
            rc = gate.main(self.atomic_cli_args('atomic-positive'))
        self.assertEqual(rc, 0)
        publish.assert_called_once()
        self.assertEqual(observed, [True])
        cold = json.loads((self.root / 'atomic-positive/cold-build-receipt.json').read_text())
        self.assertEqual(cold['status'], 'cold_next_build_passed')
        self.assertTrue(cold['cold_next_build_passed'])
        self.assertTrue(cold['admitted'])
        self.assertTrue(cold['lifecycle_completed_before_deadline'])
        self.assertNotIn('cold_next_build_passed', captured.getvalue())

    def test_atomic_post_publication_failure_revokes_machine_pass(self):
        real_admit = gate.atomic_admit
        def failing_admit(output, result, guard):
            real_admit(output, result, guard)
            raise OSError('fixture failure immediately after atomic publication')
        with patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=self.fake_atomic_execution), patch.object(gate, 'atomic_admit', side_effect=failing_admit):
            rc = gate.main(self.atomic_cli_args('atomic-revoke'))
        self.assertEqual(rc, 1)
        cold = json.loads((self.root / 'atomic-revoke/cold-build-receipt.json').read_text())
        self.assertNotEqual(cold['status'], 'cold_next_build_passed')
        self.assertFalse(cold['cold_next_build_passed'])
        self.assertFalse(cold['admitted'])

    def test_atomic_hard_expiry_interrupts_raw_read_without_unbounded_failure_io(self):
        real_read = Path.read_bytes
        before = time.monotonic()
        fake_process = unittest.mock.Mock(pid=999999, returncode=0)
        def slow_raw(path):
            if path.name == 'mock-command.stdout':
                time.sleep(20)
            return real_read(path)
        def execution(output, *args, **kwargs):
            self.fake_atomic_execution(output)
            gate._ACTIVE_GUARD.arm_hard()
            return gate.run_command(output, 'mock-command', ['fixture-no-spawn'],
                                    self.repo, {}, 1, deadline=gate._ACTIVE_GUARD.hard_deadline)
        cleanup = {'verified_no_live_group': True, 'final': {'unknown': False, 'live_pids': []}}
        with patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=execution), patch.object(gate.subprocess, 'Popen', return_value=fake_process), patch.object(gate, 'cleanup_group', return_value=cleanup), patch.object(Path, 'read_bytes', slow_raw), patch.object(gate, 'write_json', wraps=gate.write_json) as writes:
            rc = gate.main(self.atomic_cli_args('atomic-raw-expiry'))
        self.assertEqual(rc, 1)
        self.assertLess(time.monotonic() - before, 1.5)
        self.assertFalse(any(call.args[0].name == 'error.json' for call in writes.call_args_list))
        cold = json.loads((self.root / 'atomic-raw-expiry/cold-build-receipt.json').read_text())
        self.assertFalse(cold['cold_next_build_passed'])
        self.assertFalse(cold['admitted'])
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_atomic_post_publication_baseexceptions_revoke_without_machine_pass(self):
        real_admit = gate.atomic_admit
        for index, failure in enumerate((KeyboardInterrupt(), SystemExit(0), RuntimeError('fixture postpublish failure'))):
            name = 'atomic-baseexception-' + str(index)
            def failing_admit(output, result, guard):
                real_admit(output, result, guard)
                raise failure
            with self.subTest(failure=type(failure).__name__), patch.object(gate, 'prepare_snapshot', return_value={'source_identity_sha256': '0' * 64}), patch.object(gate, 'execute_build', side_effect=self.fake_atomic_execution), patch.object(gate, 'atomic_admit', side_effect=failing_admit):
                with self.assertRaises(type(failure)):
                    gate.main(self.atomic_cli_args(name))
            cold = json.loads((self.root / name / 'cold-build-receipt.json').read_text())
            self.assertFalse(cold['cold_next_build_passed'])
            self.assertFalse(cold['admitted'])
            self.assertNotEqual(cold['status'], 'cold_next_build_passed')
            self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_atomic_timeout_abbreviation_is_rejected_before_capture(self):
        argv = self.atomic_cli_args('atomic-abbrev')
        argv[argv.index('--total-timeout-seconds')] = '--total-timeout'
        errors = io.StringIO()
        with patch.object(gate, 'prepare_snapshot') as capture, contextlib.redirect_stderr(errors):
            with self.assertRaises(SystemExit) as outcome:
                gate.main(argv)
        self.assertEqual(outcome.exception.code, 2)
        self.assertIn('unrecognized arguments: --total-timeout 1', errors.getvalue())
        capture.assert_not_called()
        self.assertFalse((self.root / 'atomic-abbrev').exists())
        self.assertEqual(signal.getitimer(signal.ITIMER_REAL), (0.0, 0.0))

    def test_wrong_node_major_records_rejection(self):
        output, source = self.prepare()
        node_bin = self.tools()
        (node_bin / 'node').write_text('#!/bin/sh\necho v24.0.0\n')
        with self.assertRaisesRegex(gate.GateError, 'Node 22 required'):
            gate.execute_build(output, source, node_bin, 'fixture', 3)
        result = json.loads((output / 'cold-build-receipt.json').read_text())
        self.assertEqual(result['status'], 'execution_error')
        self.assertFalse((output / 'npm-ci.json').exists())


if __name__ == '__main__':
    unittest.main()
