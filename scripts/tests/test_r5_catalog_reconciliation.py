"""Revision-2 reviewed deltas; original validators and real syntax producers."""
import copy
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest
from unittest import mock

import check_r5_transactions as tx
import r5_go_environment
import check_r5_compatibility as gate

ROOT = Path(__file__).resolve().parents[2]
EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261004'


class ReviewedCatalogReconciliationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tx = json.loads(tx.CATALOG.read_text())
        cls.compat = json.loads(gate.MAP.read_text())
        cls.old_tx = json.loads((EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text())
        cls.old_compat = json.loads((EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text())
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()
        cls.routes, cls.clients = gate.collect()

    def test_old_pins_reject_and_current_revision_passes_same_actual_facts(self):
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*employee_disposition'):
            tx.validate(self.old_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^source hash drift$'):
            gate.validate(self.old_compat, self.routes, self.clients)
        self.assertFalse(tx.validate(self.tx, self.ast, self.migrations)['runtime_verified'])
        self.assertFalse(gate.validate(self.compat, self.routes, self.clients)['product_green'])
        for current in (self.tx, self.compat):
            revision = current['inventory_revision']
            self.assertEqual(revision['revision'], 2)
            self.assertEqual(hashlib.sha256((ROOT / revision['previous_snapshot']).read_bytes()).hexdigest(), revision['previous_sha256'])

    def test_only_reviewed_body_and_two_closure_hashes_change(self):
        old = {e['id']: e for e in self.old_tx['entries']}
        current = {e['id']: e for e in self.tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual([k for k in old if old[k]['syntax']['sha256'] != current[k]['syntax']['sha256']],
                         ['internal/store/postgres/employee_disposition.go::offboardingSubjectsTx'])
        self.assertEqual(self.old_tx['migrations'], self.tx['migrations'])
        self.assertEqual(self.old_tx['historical_review_metadata'], self.tx['historical_review_metadata'])
        self.assertEqual(self.old_tx['reviewed_file_types'], self.tx['reviewed_file_types'])
        for key in old:
            self.assertEqual(old[key]['classification'], current[key]['classification'])
            self.assertEqual(old[key]['evidence_level'], current[key]['evidence_level'])
        self.assertEqual(set(self.old_compat['source_closure']), set(self.compat['source_closure']))
        changed = {k for k in self.compat['source_closure'] if self.compat['source_closure'][k] != self.old_compat['source_closure'][k]}
        self.assertEqual(changed, {'internal/api/handlers/r5_protocol_component_observations_test.go', 'internal/store/postgres/r5_protocol_shared_test.go'})
        for key in self.old_compat:
            if key != 'source_closure':
                self.assertEqual(self.old_compat[key], self.compat[key])

    def test_unapproved_actual_source_and_ast_changes_rejected(self):
        # Copy only real producer inputs; no product binary, service or source edit.
        with tempfile.TemporaryDirectory(prefix='r5-catalog-negative-') as directory:
            root = Path(directory)
            for group in ('internal', 'cmd'):
                for source in (ROOT / group).rglob('*.go'):
                    if source.name.endswith('_test.go') or source.is_relative_to(ROOT / 'cmd/r5txinventory'):
                        continue
                    target = root / source.relative_to(ROOT)
                    target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(source, target)
            self.assertEqual(tx.extract(root), self.ast)
            path = root / 'internal/store/postgres/employee_disposition.go'
            source = path.read_text()
            self.assertEqual(source.count('successor == a.ID ||'), 1)
            path.write_text(source.replace('successor == a.ID ||', ''))
            with self.assertRaisesRegex(ValueError, 'function syntax drift:.*offboardingSubjectsTx'):
                tx.validate(self.tx, tx.extract(root), self.migrations)
            path.write_text(source + '\n// unreviewed file-only delta\n')
            with self.assertRaisesRegex(ValueError, 'postgres file set/content drift'):
                tx.validate(self.tx, tx.extract(root), self.migrations)

    def test_unapproved_actual_closure_bytes_rejected(self):
        # Clone the exact closure, alter actual bytes, and use the real hasher.
        with tempfile.TemporaryDirectory(prefix='r5-closure-negative-') as directory:
            root = Path(directory)
            for relative in self.compat['source_closure']:
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / relative, path)
            path = root / 'internal/store/postgres/r5_protocol_shared_test.go'
            path.write_bytes(path.read_bytes() + b'\n// unreviewed closure delta\n')
            actual_facts = gate.source_facts(self.routes, self.clients)
            with mock.patch.object(gate, 'ROOT', root):
                changed = gate.closure(actual_facts, self.clients)
            # Only root location is substituted; all digests derive from real bytes.
            with mock.patch.object(gate, 'closure', return_value=changed):
                with self.assertRaisesRegex(ValueError, '^source hash drift$'):
                    gate.validate(self.compat, self.routes, self.clients)

    def test_unapproved_route_and_ast_fact_changes_rejected(self):
        routes = copy.deepcopy(self.routes)
        routes[0]['handler'] = 'unapproved.handler'
        with self.assertRaisesRegex(ValueError, 'route producer differs'):
            gate.validate(self.compat, routes, self.clients)
        ast = copy.deepcopy(self.ast)
        function = next(f for f in ast['functions'] if f['name'] == 'offboardingSubjectsTx')
        function['calls'].append({'line': function['end'], 'expr': 'tx.Exec', 'name': 'Exec', 'sql_expr': '`DELETE FROM users`'})
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*offboardingSubjectsTx'):
            tx.validate(self.tx, ast, self.migrations)

    def test_unapproved_actual_route_source_rejected_by_original_collector(self):
        with tempfile.TemporaryDirectory(prefix='r5-route-source-negative-') as directory:
            root = Path(directory)
            for name in ('go.mod', 'go.sum', 'internal/architecture/route_inventory_test.go',
                         'internal/api/router.go', 'internal/api/handlers/company_routes.go',
                         'docs/company-mail/evidence/R5-API-MATRIX.json'):
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / name, path)
            go, env = r5_go_environment.selected()
            env['GOPROXY'] = 'off'
            argv = [go, 'test', '-mod=readonly', '-count=1', './internal/architecture', '-run', '^TestR5RouteInventory$']
            before = subprocess.run(argv, cwd=root, env=env, capture_output=True, text=True, timeout=60)
            self.assertEqual(before.returncode, 0, before.stdout + before.stderr)
            path = root / 'internal/api/router.go'
            source = path.read_text()
            self.assertEqual(source.count('"/docs-assets/*"'), 1)
            path.write_text(source.replace('"/docs-assets/*"', '"/unreviewed-docs-assets/*"'))
            after = subprocess.run(argv, cwd=root, env=env, capture_output=True, text=True, timeout=60)
            self.assertNotEqual(after.returncode, 0)
            self.assertIn('undocumented route GET /unreviewed-docs-assets/*', after.stdout)
