"""Source/selection controls only; these do not qualify frontend or PG runtime."""
import copy
import hashlib
import json
from pathlib import Path
import re
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
import prepare_r5_protocol_source as preparation
import r5_source_inventory as inventory

OWNER = 'TestR5FrontendOwnedFixture'
RUNTIME_INPUTS = (
    'internal/testpg/r5_frontend_fixture_test.go',
    'internal/testpg/r5_loopback_probe.go',
    'internal/testpg/fixture.go',
    'scripts/run_r5_frontend_fixture.py',
    'scripts/required_frontend_fixture_tests.json',
    'scripts/check_go_test_evidence.py',
    'scripts/preparation/r5_external_batch.py',
    'scripts/preparation/r5_external_runtime.py',
    'scripts/check_r5_protocol.py',
    'scripts/r5_source_inventory.py',
    'scripts/tests/test_r5_frontend_fixture.py',
    'scripts/tests/test_r5_frontend_fixture_source_inputs.py',
    '.github/workflows/company-p0.yml',
    'web/package.json',
    'web/package-lock.json',
    'web/vitest.config.ts',
    'web/vitest.setup.ts',
    'web/r5-external-batch-probe.test.tsx',
    'docs/company-mail/evidence/R5-PROTOCOL-CASES.json',
)


class FrontendFixtureSourceInputsTests(unittest.TestCase):
    def test_existing_fixture_tag_does_not_admit_unknown_tags_or_command_contexts(self):
        self.assertNotIn('r5frontendfixture', inventory.TAGS)
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            path = root / 'fixture_test.go'
            path.write_text('//go:build r5fixtures\n\npackage fixture\n')
            self.assertEqual(inventory._check_go_inputs(root, {path.name}), {})
            for tag in ('r5frontendfixture', 'unregistered_fixture_input'):
                with self.subTest(tag=tag):
                    path.write_text('//go:build ' + tag + '\n\npackage fixture\n')
                    with self.assertRaisesRegex(ValueError, 'unknown conditional build input'):
                        inventory._check_go_inputs(root, {path.name})
                    context = preparation.build_context(ROOT)
                    context['build_tag_sets'] = [[tag]]
                    with self.assertRaisesRegex(ValueError, 'supported command build tag sets'):
                        inventory.validate_context(context, 'protocol')

    def test_original_twelve_fixture_tests_and_frontend_owner_have_disjoint_selectors(self):
        suites = json.loads((ROOT / 'scripts/required_go_tests.json').read_text())['suites']
        original = [name for package, name in suites['r5fixtures']['required_tests']]
        self.assertEqual(len(original), 12)
        self.assertEqual(len(set(original)), 12)
        self.assertTrue(all(re.search('^TestR5Fixture', name) for name in original))
        self.assertIsNone(re.search('^TestR5Fixture', OWNER))
        self.assertFalse(any(re.search('^' + OWNER + '$', name) for name in original))
        current = json.loads((ROOT / 'scripts/required_frontend_fixture_tests.json').read_text())['suites']
        self.assertEqual(current['frontend-owned-fixture'], {
            'required_tests': [['tabmail/internal/testpg', OWNER]], 'allowed_skips': []})
        self.assertEqual((ROOT / RUNTIME_INPUTS[0]).read_text().splitlines()[0], '//go:build r5fixtures')

    def test_actual_archive_superset_binds_runtime_inputs_and_rejects_an_omission(self):
        receipt = inventory.capture_current_source(
            ROOT, purpose='protocol', policy=inventory.ARCHIVE_POLICY,
            build_context=preparation.build_context(ROOT))
        # Existing complete source-directory capture covers all variants,
        # shared helpers, migrations, frontend and checked runtime commands.
        # External module/toolchain bytes remain outside this local SOURCE claim.
        for name in RUNTIME_INPUTS:
            with self.subTest(input=name):
                self.assertEqual(receipt['files'][name], hashlib.sha256((ROOT / name).read_bytes()).hexdigest())
        migrations = sorted((ROOT / 'internal/store/postgres/migrations').glob('*.sql'))
        self.assertTrue(migrations)
        for path in migrations:
            self.assertEqual(receipt['files'][path.relative_to(ROOT).as_posix()], hashlib.sha256(path.read_bytes()).hexdigest())
        missing = copy.deepcopy(receipt)
        del missing['files']['scripts/required_frontend_fixture_tests.json']
        with self.assertRaisesRegex(ValueError, 'files missing, extra, or drifted'):
            inventory.validate_current_source(missing, ROOT, purpose='protocol', policy=inventory.ARCHIVE_POLICY)


if __name__ == '__main__':
    unittest.main()
