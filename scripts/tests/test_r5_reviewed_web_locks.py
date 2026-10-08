"""Reviewed dependency updates keep full-lock and TypeScript admission strict."""
import ast
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_source_runner_prepare as preparation


ROOT = Path(__file__).resolve().parents[2]
PR55 = 'cbc8c17ebd3599d5c92711d28088b0bf4edc3f8f'
PRE_NEXT_PATCH = '1322c9284d5dad8984adb61d2b4627b331c3740f'
NEXT_PATCH_BASE = '3a103cda4363cb8f32839eaaa73ab065eb967f79'
OLD_HASH = 'b839b59e9aa06133819adca60659e0f807ca1e321fbdc35fe55afe1c7b52eba3'
NEW_HASH = 'b1c85223bc1171b16a5994c2f069ada51a5574719445142c31a213e9fda84e89'
NEXT_PATCH_HASH = 'c4f70934466c07c0a2589ead49f120d1b2278d9775e13fdbf7386913b1658869'


class ReviewedWebLockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Use the real previous lock from preserved Git history, not a fixture
        # whose digest or dependency identity has been mocked into acceptance.
        cls.old = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{PR55}:web/package-lock.json'])
        # This original suite proves the earlier b839 -> b1 update. Keep its
        # three assertions bound to those immutable locks as current advances.
        cls.current = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{PRE_NEXT_PATCH}:web/package-lock.json'])

    def test_previous_and_updated_complete_locks_report_their_actual_hash(self):
        for lock, expected in ((self.old, OLD_HASH), (self.current, NEW_HASH)):
            with self.subTest(lock=expected), tempfile.TemporaryDirectory() as temp:
                self.assertEqual(hashlib.sha256(lock).hexdigest(), expected)
                root = Path(temp)
                (root / 'web').mkdir()
                (root / 'web/package-lock.json').write_bytes(lock)
                # This suite isolates lock admission. The unchanged preparation
                # boundary suite separately tests official archive authentication,
                # all 132 members, unsafe paths, native files and lifecycle rules.
                with mock.patch.object(preparation, 'typescript_members',
                                       return_value={'lib/fixture.js': b'fixture'}) as archive:
                    result = preparation.prepare_typescript(root, b'archive fixture')
                archive.assert_called_once_with(b'archive fixture')
                self.assertEqual(result['lock_sha256'], expected)
                self.assertEqual(result['archive_sha256'], preparation.TS_SHA256)
                self.assertEqual(result['integrity'], preparation.TS_INTEGRITY)

    def test_unreviewed_lock_bytes_reject_before_archive_or_filesystem_changes(self):
        altered_ts = json.loads(self.current)
        altered_ts['packages']['node_modules/typescript']['version'] = '0.0.0'
        locks = (self.old + b' ', self.current + b' ',
                 json.dumps(json.loads(self.current), sort_keys=True).encode(),
                 json.dumps(altered_ts).encode())
        for lock in locks:
            with self.subTest(digest=hashlib.sha256(lock).hexdigest()), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                (root / 'web').mkdir()
                (root / 'web/package-lock.json').write_bytes(lock)
                with mock.patch.object(preparation, 'typescript_members') as archive:
                    with self.assertRaisesRegex(ValueError, 'lock hash'):
                        preparation.prepare_typescript(root, b'archive fixture')
                archive.assert_not_called()
                self.assertFalse((root / 'web/node_modules').exists())

    def test_update_preserves_direct_requirements_and_typescript_identity(self):
        old, current = json.loads(self.old), json.loads(self.current)
        self.assertEqual(old['packages'][''], current['packages'][''])
        self.assertEqual(old['packages']['node_modules/typescript'],
                         current['packages']['node_modules/typescript'])
        ts = current['packages']['node_modules/typescript']
        self.assertEqual((ts['version'], ts['resolved'], ts['integrity']),
                         ('5.9.3', preparation.TS_URL, preparation.TS_INTEGRITY))


class NextSecurityPatchLockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.previous = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{PRE_NEXT_PATCH}:web/package-lock.json'])
        cls.current = (ROOT / 'web/package-lock.json').read_bytes()

    def test_only_reviewed_direct_pair_and_matching_release_packages_change(self):
        old, current = json.loads(self.previous), json.loads(self.current)
        self.assertEqual(hashlib.sha256(self.previous).hexdigest(), NEW_HASH)
        self.assertEqual(hashlib.sha256(self.current).hexdigest(), NEXT_PATCH_HASH)
        expected_root = json.loads(json.dumps(old['packages']['']))
        expected_root['dependencies']['next'] = '16.3.8'
        expected_root['devDependencies']['eslint-config-next'] = '16.3.8'
        self.assertEqual(current['packages'][''], expected_root)
        manifest = json.loads((ROOT / 'web/package.json').read_bytes())
        for kind in ('dependencies', 'devDependencies'):
            self.assertEqual(manifest[kind], expected_root[kind])
        release_packages = {
            'node_modules/next', 'node_modules/eslint-config-next',
            'node_modules/@next/env', 'node_modules/@next/eslint-plugin-next',
            *('node_modules/@next/swc-' + platform for platform in (
                'darwin-arm64', 'darwin-x64', 'linux-arm64-gnu', 'linux-arm64-musl',
                'linux-x64-gnu', 'linux-x64-musl', 'win32-arm64-msvc', 'win32-x64-msvc')),
        }
        self.assertEqual(set(old['packages']), set(current['packages']))
        changed = {name for name in old['packages'] if old['packages'][name] != current['packages'][name]}
        self.assertEqual(changed, release_packages | {''})
        for name in release_packages:
            with self.subTest(package=name):
                self.assertEqual(old['packages'][name]['version'], '16.3.6')
                self.assertEqual(current['packages'][name]['version'], '16.3.8')
        self.assertEqual(old['packages']['node_modules/typescript'],
                         current['packages']['node_modules/typescript'])
        ts = current['packages']['node_modules/typescript']
        self.assertEqual((ts['version'], ts['resolved'], ts['integrity']),
                         ('5.9.3', preparation.TS_URL, preparation.TS_INTEGRITY))

    def test_exact_new_complete_lock_passes_original_preparation_boundary(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            (root / 'web').mkdir()
            (root / 'web/package-lock.json').write_bytes(self.current)
            with mock.patch.object(preparation, 'typescript_members',
                                   return_value={'lib/fixture.js': b'fixture'}) as archive:
                result = preparation.prepare_typescript(root, b'archive fixture')
            archive.assert_called_once_with(b'archive fixture')
            self.assertEqual(result['lock_sha256'], NEXT_PATCH_HASH)
            self.assertEqual(result['archive_sha256'], preparation.TS_SHA256)
            self.assertEqual(result['integrity'], preparation.TS_INTEGRITY)

    def test_new_lock_mutations_reject_before_archive_or_filesystem_changes(self):
        changed_ts = json.loads(self.current)
        changed_ts['packages']['node_modules/typescript']['version'] = '0.0.0'
        changed_next = json.loads(self.current)
        changed_next['packages']['node_modules/next']['version'] = '16.3.6'
        for lock in (self.current + b' ', json.dumps(changed_ts).encode(),
                     json.dumps(changed_next).encode(),
                     json.dumps(json.loads(self.current), sort_keys=True).encode()):
            with self.subTest(digest=hashlib.sha256(lock).hexdigest()), tempfile.TemporaryDirectory() as temp:
                root = Path(temp)
                (root / 'web').mkdir()
                (root / 'web/package-lock.json').write_bytes(lock)
                with mock.patch.object(preparation, 'typescript_members') as archive:
                    with self.assertRaisesRegex(ValueError, 'lock hash'):
                        preparation.prepare_typescript(root, b'archive fixture')
                archive.assert_not_called()
                self.assertFalse((root / 'web/node_modules').exists())

    def test_all_existing_preparation_functions_and_original_assertions_are_preserved(self):
        old_source = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{NEXT_PATCH_BASE}:scripts/r5_source_runner_prepare.py'])
        def functions(source):
            return {node.name: ast.dump(node, include_attributes=False)
                    for node in ast.parse(source).body
                    if isinstance(node, (ast.FunctionDef, ast.AsyncFunctionDef))}
        self.assertEqual(functions(old_source), functions((ROOT / 'scripts/r5_source_runner_prepare.py').read_bytes()))
        old_tests = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{NEXT_PATCH_BASE}:scripts/tests/test_r5_reviewed_web_locks.py'])
        def original_assertions(source):
            suite = next(node for node in ast.parse(source).body
                         if isinstance(node, ast.ClassDef) and node.name == 'ReviewedWebLockTests')
            return {node.name: ast.dump(node, include_attributes=False) for node in suite.body
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        self.assertEqual(original_assertions(old_tests), original_assertions(Path(__file__).read_bytes()))
        self.assertEqual(preparation.REVIEWED_LOCK_SHA256S,
                         frozenset({OLD_HASH, NEW_HASH, NEXT_PATCH_HASH}))


if __name__ == '__main__':
    unittest.main()
