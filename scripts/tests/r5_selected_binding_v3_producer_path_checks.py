"""Explicit pure controls for the v3 absolute metadata-producer path contract.

Only Python fixtures and mocked subprocesses are used. No Go or formal producer
is executed, and this filename does not change existing test discovery.
"""
import contextlib
import io
from pathlib import Path
import sys
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_selected_source_binding_v3 as v3
import r5_selected_source_binding_v3_checks as existing


class ProducerPathTests(unittest.TestCase):
    def assert_early_rejection(self, go, *, receipt=None):
        with mock.patch.object(v3.inventory, 'capture_current_source') as source, \
                mock.patch.object(v3.subprocess, 'run') as run, \
                self.assertRaisesRegex(ValueError, 'absolute.*metadata producer'):
            if receipt is None:
                v3.capture('/unused-source', go, cache='/unused-cache',
                           modulecache='/unused-modules')
            else:
                v3.validate(receipt, '/unused-source', go, cache='/unused-cache',
                            modulecache='/unused-modules',
                            trusted_observation_sha256=receipt['observation_sha256'])
        source.assert_not_called()
        run.assert_not_called()

    def test_bare_names_reject_before_source_or_command(self):
        for go in ('go', 'producer', Path('go')):
            with self.subTest(go=go):
                self.assert_early_rejection(go)

    def test_relative_paths_reject_before_source_or_command(self):
        for go in ('bin/go', './bin/go', '../bin/go', '', '.', Path('bin/go')):
            with self.subTest(go=go):
                self.assert_early_rejection(go)

    def test_validate_rejects_nonabsolute_after_valid_external_pin(self):
        receipt = existing.EnvelopeTests().receipt()
        for go in ('go', Path('bin/go')):
            with self.subTest(go=go):
                self.assert_early_rejection(go, receipt=receipt)

    def test_cli_rejects_bare_and_relative_before_source_or_command(self):
        for go in ('go', 'bin/go'):
            argv = ['r5_selected_source_binding_v3.py', '--root', '/unused-source',
                    '--go', go, '--cache', '/unused-cache',
                    '--modulecache', '/unused-modules']
            with self.subTest(go=go), mock.patch.object(sys, 'argv', argv), \
                    mock.patch.object(v3.inventory, 'capture_current_source') as source, \
                    mock.patch.object(v3.subprocess, 'run') as run, \
                    self.assertRaisesRegex(ValueError, 'absolute.*metadata producer'):
                v3.main()
            source.assert_not_called()
            run.assert_not_called()

    def test_cli_help_explains_absolute_path_requirement(self):
        output = io.StringIO()
        with mock.patch.object(sys, 'argv', ['v3', '--help']), \
                contextlib.redirect_stdout(output), self.assertRaises(SystemExit) as got:
            v3.main()
        self.assertEqual(got.exception.code, 0)
        self.assertIn('absolute path', output.getvalue())
        self.assertIn('metadata producer', output.getvalue())

    def test_absolute_capture_keeps_all_twelve_commands_and_pin(self):
        receipt, calls = existing.CaptureOrderingTests().exercise()
        self.assertEqual(len(calls), 12)
        producer = calls[0][0]
        self.assertTrue(Path(producer).is_absolute())
        self.assertTrue(all(argv[0] == producer for argv in calls))
        self.assertTrue(all(command['argv'][0] == producer
                            for command in receipt['observation_envelope']))
        self.assertEqual(receipt['metadata_producer']['executable_sha256'],
                         v3._sha(b'fixture producer'))
        self.assertEqual(receipt['qualification']['overall'], 'blocked')
        self.assertEqual(receipt['qualification']['compiler_native'], 'unknown')

    def test_production_sha256_pin_is_unchanged(self):
        self.assertEqual(v3.GO_SHA256,
                         '76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8')


if __name__ == '__main__':
    unittest.main()
