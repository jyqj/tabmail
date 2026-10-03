"""Tiny v3 boundary negatives; synthetic fixtures only, no Go/process/runtime.

Two artifact-descendant cases intentionally require the requested fail-closed
boundary. If current v3 admits them, retain the failures for policy-owner review;
do not weaken the assertions or infer runtime qualification from this suite.
"""
import tempfile
from pathlib import Path
import unittest

import test_r5_smtp_owner_source_inventory as fixtures

inventory = fixtures.inventory


class SourceV3BoundaryNegativeTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name).resolve() / 'src'
        self.root.mkdir()
        fixtures.fixture(self.root)

    def capture(self):
        return inventory.capture_current_source(
            self.root, purpose='protocol', policy=inventory.CURRENT_POLICY,
            build_context=fixtures.context())

    def test_nested_module_in_excluded_artifact_directory_is_rejected(self):
        self.capture()
        for replacement in inventory.CURRENT_REPLACEMENTS:
            with self.subTest(module=replacement['module']):
                path = self.root / replacement['path'] / 'build' / 'go.mod'
                path.parent.mkdir()
                path.write_text('module example.invalid/third\ngo 1.25.7\n')
                try:
                    with self.assertRaisesRegex(ValueError, 'nested module'):
                        self.capture()
                finally:
                    path.unlink()
                    path.parent.rmdir()

    def test_symlink_descendant_in_excluded_artifact_directory_is_rejected(self):
        self.capture()
        for replacement in inventory.CURRENT_REPLACEMENTS:
            with self.subTest(module=replacement['module']):
                path = self.root / replacement['path'] / 'build' / 'alias.go'
                path.parent.mkdir()
                path.symlink_to(self.root / 'cmd' / 'main.go')
                try:
                    with self.assertRaisesRegex(ValueError, 'symlink'):
                        self.capture()
                finally:
                    path.unlink()
                    path.parent.rmdir()

    def test_excluded_artifact_directory_alias_is_rejected(self):
        self.capture()
        path = self.root / 'third_party/go-smtp/build'
        path.symlink_to(self.root / 'cmd', target_is_directory=True)
        with self.assertRaisesRegex(ValueError, 'symlink'):
            self.capture()

    def test_upstream_manifest_traversal_and_duplicate_keys_are_rejected(self):
        self.capture()
        path = self.root / 'third_party/go-smtp/UPSTREAM-MANIFEST.json'
        original = path.read_bytes()
        cases = [
            (b'{"module":"github.com/emersion/go-smtp","version":"v0.24.0",'
             b'"files_sha256":{"../go.mod":"' + b'a' * 64 + b'"}}',
             'noncanonical local source path'),
            (original[:-1] + b',"module":"github.com/emersion/go-smtp"}',
             'duplicate source receipt key'),
        ]
        for data, reason in cases:
            path.write_bytes(data)
            with self.subTest(reason=reason), self.assertRaisesRegex(ValueError, reason):
                self.capture()
        path.write_bytes(original)

    def test_workspace_alias_and_overlay_environment_are_rejected(self):
        self.capture()
        (self.root / 'go.work').symlink_to(self.root / 'absent-workfile')
        with self.assertRaisesRegex(ValueError, 'workspace/vendor'):
            self.capture()
        for key, value in [('GOWORK', '../go.work'), ('GOFLAGS', '-overlay=fixture.json')]:
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, 'unbound build environment'):
                inventory.bound_environment(fixtures.context(), {key: value})


if __name__ == '__main__':
    unittest.main()
