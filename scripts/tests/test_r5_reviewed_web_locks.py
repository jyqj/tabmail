"""Reviewed dependency updates keep full-lock and TypeScript admission strict."""
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
OLD_HASH = 'b839b59e9aa06133819adca60659e0f807ca1e321fbdc35fe55afe1c7b52eba3'
NEW_HASH = 'b1c85223bc1171b16a5994c2f069ada51a5574719445142c31a213e9fda84e89'


class ReviewedWebLockTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        # Use the real previous lock from preserved Git history, not a fixture
        # whose digest or dependency identity has been mocked into acceptance.
        cls.old = subprocess.check_output(
            ['git', '-C', str(ROOT), 'show', f'{PR55}:web/package-lock.json'])
        cls.current = (ROOT / 'web/package-lock.json').read_bytes()

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


if __name__ == '__main__':
    unittest.main()
