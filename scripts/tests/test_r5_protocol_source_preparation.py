"""Preparation controls only; synthetic receipts here never qualify Go/PG tests."""
import contextlib
import hashlib
import importlib.util
import io
import json
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

import yaml

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location('r5_protocol_source_preparation', ROOT / 'scripts/prepare_r5_protocol_source.py')
preparation = importlib.util.module_from_spec(spec)
spec.loader.exec_module(preparation)


class ProtocolSourcePreparationTests(unittest.TestCase):
    def setUp(self):
        temporary = tempfile.TemporaryDirectory()
        self.addCleanup(temporary.cleanup)
        self.base = Path(temporary.name)
        self.root, self.output = self.base / 'source', self.base / 'receipt'
        self.root.mkdir()
        (self.root / 'go.mod').write_text('module fixture\n\ngo 1.25.7\n')
        self.context = dict(goos='linux', goarch='amd64', cgo_enabled=1,
                            build_tag_sets=[['r5protocol']], race=True, go_work='off',
                            go_flags='', selection='all_local_variants_superset')
        self.receipt = dict(source_sha='a' * 40, boundary='synthetic preparation control only',
                            schema_version=4, policy=preparation.POLICY, files={'owned.go': 'b' * 64})
        self.go = dict(GOVERSION='go1.25.7', GOHOSTOS='linux', GOHOSTARCH='amd64',
                       GOOS='linux', GOARCH='amd64', CGO_ENABLED='1', GOENV='', GOWORK='off', GOFLAGS='')
        for target, name, value in (
            (preparation, 'build_context', self.context),
            (preparation.inventory, 'capture_current_source', self.receipt),
            (preparation.inventory, 'validate_current_source', self.receipt),
            (preparation.subprocess, 'check_output', json.dumps(self.go).encode()),
        ):
            patcher = mock.patch.object(target, name, return_value=value)
            setattr(self, name, patcher.start())
            self.addCleanup(patcher.stop)
        environment = mock.patch.dict(os.environ, {}, clear=True)
        environment.start()
        self.addCleanup(environment.stop)

    def prepare(self):
        return preparation.prepare(self.root, self.output)

    def test_exact_written_bytes_are_pinned_and_validated_before_publication(self):
        result = self.prepare()
        raw = (self.output / 'source-manifest.json').read_bytes()
        digest = hashlib.sha256(raw).hexdigest()
        self.assertEqual(raw, preparation.inventory.canonical(self.receipt))
        self.assertEqual((self.output / 'source-manifest.sha256').read_text(), digest + '\n')
        self.assertEqual(result['source_manifest_sha256'], digest)
        self.capture_current_source.assert_called_once_with(
            self.root, purpose='protocol', policy=preparation.inventory.ARCHIVE_POLICY,
            build_context=self.context)
        self.validate_current_source.assert_called_once_with(
            self.receipt, self.root, purpose='protocol', policy=preparation.inventory.ARCHIVE_POLICY)
        self.assertEqual(result['status'], 'source_prepared')
        self.assertFalse(result['task_complete'])
        self.assertFalse(result['product_green'])
        self.assertEqual(stat.S_IMODE(self.output.stat().st_mode), 0o700)
        for path in self.output.iterdir():
            self.assertEqual(stat.S_IMODE(path.stat().st_mode), 0o600)
        command = self.check_output.call_args
        self.assertEqual(command.args[0], ['go', 'env', '-json', *preparation.GO_ENV_KEYS])
        self.assertEqual(command.kwargs['timeout'], 30)
        self.assertEqual(command.kwargs['env']['GOWORK'], 'off')
        self.assertEqual(command.kwargs['env']['GOENV'], 'off')

    def test_source_directory_or_existing_receipt_is_never_reused(self):
        for output in (self.root / 'generated', self.base):
            with self.subTest(output=str(output)), self.assertRaises(ValueError):
                preparation.prepare(self.root, output)
        self.output.mkdir()
        marker = self.output / 'source-manifest.sha256'
        marker.write_text('old immutable receipt')
        with self.assertRaisesRegex(ValueError, 'fresh'):
            self.prepare()
        self.assertEqual(marker.read_text(), 'old immutable receipt')
        self.check_output.assert_not_called()
        self.capture_current_source.assert_not_called()

    def test_symlinked_output_ancestor_is_rejected_before_tool_execution(self):
        real = self.base / 'actual'
        real.mkdir()
        link = self.base / 'alias'
        link.symlink_to(real, target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'symlinks'):
            preparation.prepare(self.root, link / 'receipt')
        self.check_output.assert_not_called()

    def test_unbound_build_overrides_are_rejected_not_removed(self):
        for key, value in (('GOFLAGS', '-overlay=unreviewed.json'), ('GOTOOLCHAIN', 'go1.26.0'),
                           ('GOROOT', '/unreviewed/toolchain'), ('GOPATH', '/unreviewed/modules'),
                           ('CGO_CFLAGS', '-include unreviewed.h'), ('CC', 'other-compiler')):
            with self.subTest(key=key), mock.patch.dict(os.environ, {key: value}, clear=True):
                with self.assertRaisesRegex(ValueError, 'unbound build environment'):
                    self.prepare()
        self.check_output.assert_not_called()
        self.capture_current_source.assert_not_called()

    def test_actual_version_host_and_build_context_must_match(self):
        for key, value in (('GOVERSION', 'go1.25.8'), ('GOHOSTOS', 'darwin'),
                           ('GOHOSTARCH', 'arm64'), ('GOOS', 'windows'), ('CGO_ENABLED', '0'),
                           ('GOFLAGS', '-tags=other')):
            self.check_output.return_value = json.dumps({**self.go, key: value}).encode()
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, 'actual Go'):
                self.prepare()
        self.capture_current_source.assert_not_called()
        self.assertFalse(self.output.exists())

    def test_failed_capture_never_selects_an_older_policy(self):
        self.capture_current_source.side_effect = ValueError('controlled source rejection')
        with self.assertRaisesRegex(ValueError, 'source rejection'):
            self.prepare()
        self.capture_current_source.assert_called_once()
        self.assertEqual(self.capture_current_source.call_args.kwargs['policy'], preparation.inventory.ARCHIVE_POLICY)
        self.assertFalse(self.output.exists())

    def test_manifest_write_failure_never_publishes_a_hash(self):
        with mock.patch.object(preparation, 'exclusive', side_effect=OSError('controlled disk failure')) as write:
            with self.assertRaises(OSError):
                self.prepare()
        write.assert_called_once()
        self.validate_current_source.assert_not_called()
        self.assertEqual(list(self.output.iterdir()), [])

    def test_written_manifest_tamper_is_rejected_by_the_original_hash_check(self):
        original_write = preparation.exclusive

        def tamper(path, raw):
            original_write(path, raw + b' ')

        with mock.patch.object(preparation, 'exclusive', side_effect=tamper):
            with self.assertRaisesRegex(ValueError, 'receipt byte hash differs'):
                self.prepare()
        self.validate_current_source.assert_not_called()
        self.assertFalse((self.output / 'source-manifest.sha256').exists())
        self.assertFalse((self.output / 'preparation.json').exists())

    def test_source_changed_after_capture_cannot_publish_a_prepared_receipt(self):
        self.validate_current_source.side_effect = ValueError('controlled source drift')
        with self.assertRaisesRegex(ValueError, 'source drift'):
            self.prepare()
        self.assertTrue((self.output / 'source-manifest.json').is_file())
        self.assertFalse((self.output / 'source-manifest.sha256').exists())
        self.assertFalse((self.output / 'preparation.json').exists())
        self.capture_current_source.assert_called_once()

    def test_go_environment_failure_exits_without_capture_or_fallback(self):
        self.check_output.side_effect = subprocess.TimeoutExpired(['go', 'env'], 30)
        with contextlib.redirect_stderr(io.StringIO()) as stderr:
            status = preparation.main(['--root', str(self.root), '--output-dir', str(self.output)])
        self.assertEqual(status, 1)
        self.assertIn('TimeoutExpired', stderr.getvalue())
        self.capture_current_source.assert_not_called()
        self.assertFalse(self.output.exists())


class ProtocolCIWiringTests(unittest.TestCase):
    def test_current_catalog_derives_the_original_shared_db_tag_set(self):
        self.assertEqual(preparation.build_context(ROOT)['build_tag_sets'], [['r5protocol']])

    def test_preparation_failure_cannot_launch_the_original_runner(self):
        workflow = yaml.load((ROOT / '.github/workflows/company-p0.yml').read_text(), Loader=yaml.BaseLoader)
        step = next(s for s in workflow['jobs']['backend']['steps']
                    if s.get('name') == 'Real protocol baseline with explicit target failures')
        command = step['run']
        self.assertIn('set -euo pipefail\n', command)
        self.assertLess(command.index('prepare_r5_protocol_source.py'), command.index('check_r5_protocol.py'))
        self.assertIn('--run shared-db --output-dir protocol-baseline-evidence', command)
        self.assertIn('--source-policy ' + preparation.inventory.ARCHIVE_POLICY, command)
        self.assertIn('--source-manifest "$RUNNER_TEMP/tabmail-protocol-source/source-manifest.json"', command)
        self.assertIn('--source-manifest-sha256 "$(cat "$RUNNER_TEMP/tabmail-protocol-source/source-manifest.sha256")"', command)
        self.assertEqual(step['env'], {'GOOS': 'linux', 'GOARCH': 'amd64', 'CGO_ENABLED': '1',
                                       'GOENV': 'off', 'GOWORK': 'off', 'GOFLAGS': ''})
        for bypass in ('|| true', '--allow', '--skip', 'continue-on-error', 'source_sha='):
            self.assertNotIn(bypass, command)


if __name__ == '__main__':
    unittest.main()
