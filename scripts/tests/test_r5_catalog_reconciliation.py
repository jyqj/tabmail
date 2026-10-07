"""Frozen revision-2 review and current revision-3 facts; original validators."""
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
CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007'
SOURCE_COMMIT = '7b7dbfeaad5c87e875bf19e5a9e213867fda2db3'
REVISION2_COMMIT = '4540fb91ba742443dcf0a872b80261e58af7b3ab'
# These identities are independent of the current catalogs and review packet.
# A missing historical Git object is an error; never fetch, skip, or fall back
# to the current same-named file when verifying a historical review.
REVISION2_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': '3e78588a301512e02d718666c54f9ea4d13640e0',
        'sha256': '9fe6a4d73935b8e51e064c9afe16279187d0516f2959189dabf58bf39e9a6bef',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '4bddcd23e6d799c05a7fade19a3ceb6456576b3d',
        'sha256': '3c03f670576ecae4d260b6995c7230dde8a598288157940cd08c2921b78a5f06',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': '2d2990ca3f1672af09fedb50f6f826aa15bcea0b',
        'sha256': 'f4a06b6911bfa888d4720235759b0bd59ee1314eddfdc9fe85524eca7e9c2c4f',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '1dead7eb284dc8b2de7d56b58db40636785b4ddb',
        'sha256': '3fdaa56861ba4e524ddcdbf1d69b2b8513e89f2567500d0410effdf73ab1a617',
    },
}


def git(*args):
    return subprocess.check_output(['git', '-C', str(ROOT), *args])


def revision2_snapshot(name):
    pin = REVISION2_SNAPSHOTS[name]
    ref = REVISION2_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-2 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-2 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


class ReviewedCatalogReconciliationTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.tx = json.loads(tx.CATALOG.read_text())
        cls.compat = json.loads(gate.MAP.read_text())
        cls.old_tx = json.loads((EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text())
        cls.old_compat = json.loads((EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text())
        cls.revision2 = {name: revision2_snapshot(name) for name in REVISION2_SNAPSHOTS}
        cls.rev2_tx = cls.revision2['transaction']
        cls.rev2_compat = cls.revision2['compatibility']
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()
        cls.routes, cls.clients = gate.collect()

    def test_old_pins_reject_and_current_revision_passes_same_actual_facts(self):
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*employee_disposition'):
            tx.validate(self.old_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev2_tx, self.ast, self.migrations)
        # At revision 3 the old rows fail before the closure hash check because
        # reviewed client locations changed. The original hash-only rev1->rev2
        # assertion remains verified unchanged in the fixed historical checkout.
        for old in (self.old_compat, self.rev2_compat):
            with self.assertRaisesRegex(ValueError, '^route/schema/client/test source drift:'):
                gate.validate(old, self.routes, self.clients)
        self.assertFalse(tx.validate(self.tx, self.ast, self.migrations)['runtime_verified'])
        self.assertFalse(gate.validate(self.compat, self.routes, self.clients)['product_green'])
        for name, current in (('transaction', self.tx), ('compatibility', self.compat)):
            revision = current['inventory_revision']
            pin = REVISION2_SNAPSHOTS[name]
            self.assertEqual(revision['revision'], 3)
            self.assertEqual(revision['source_commit'], SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION2_COMMIT)
            self.assertEqual(revision['previous_snapshot'], pin['path'])
            self.assertEqual(revision['previous_snapshot_blob'], pin['blob'])
            self.assertEqual(revision['previous_sha256'], pin['sha256'])
            self.assertEqual(revision['qualification'], self.rev2_tx['inventory_revision']['qualification'])

    def test_only_reviewed_body_and_two_closure_hashes_change(self):
        old = {e['id']: e for e in self.old_tx['entries']}
        current = {e['id']: e for e in self.rev2_tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual([k for k in old if old[k]['syntax']['sha256'] != current[k]['syntax']['sha256']],
                         ['internal/store/postgres/employee_disposition.go::offboardingSubjectsTx'])
        self.assertEqual(self.old_tx['migrations'], self.rev2_tx['migrations'])
        self.assertEqual(self.old_tx['historical_review_metadata'], self.rev2_tx['historical_review_metadata'])
        self.assertEqual(self.old_tx['reviewed_file_types'], self.rev2_tx['reviewed_file_types'])
        for key in old:
            self.assertEqual(old[key]['classification'], current[key]['classification'])
            self.assertEqual(old[key]['evidence_level'], current[key]['evidence_level'])
        self.assertEqual(set(self.old_compat['source_closure']), set(self.rev2_compat['source_closure']))
        changed = {k for k in self.rev2_compat['source_closure'] if self.rev2_compat['source_closure'][k] != self.old_compat['source_closure'][k]}
        self.assertEqual(changed, {'internal/api/handlers/r5_protocol_component_observations_test.go', 'internal/store/postgres/r5_protocol_shared_test.go'})
        for key in self.old_compat:
            if key != 'source_closure':
                self.assertEqual(self.old_compat[key], self.rev2_compat[key])
        for historical in (self.rev2_tx, self.rev2_compat):
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 2)
            self.assertEqual(hashlib.sha256((ROOT / revision['previous_snapshot']).read_bytes()).hexdigest(), revision['previous_sha256'])

    def test_revision3_preserves_unmodified_reviews_and_pins_reviewed_sources(self):
        old_entries = {entry['id']: entry for entry in self.rev2_tx['entries']}
        current_entries = {entry['id']: entry for entry in self.tx['entries']}
        self.assertEqual(set(old_entries), set(current_entries))
        changed = set()
        sources = set()
        for name, before in old_entries.items():
            after = current_entries[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            self.assertEqual(len(before['callers']), len(after['callers']))
            for old, now in zip(before['callers'], after['callers']):
                self.assertEqual({k: v for k, v in old.items() if k != 'line'},
                                 {k: v for k, v in now.items() if k != 'line'})
                if old != now:
                    changed.add(name)
                    sources.add(now['file'])
        self.assertEqual(changed, {tx.PG + suffix for suffix in (
            'outbound_retry_reader.go:*outboundRetryReader:GetUser', 'postgres.go:*PgStore:Close', 'postgres.go::New',
            'refresh_rotation.go:*PgStore:RevokeRefreshTokenByHash', 'refresh_rotation.go:*PgStore:RotateRefreshToken',
            'tenants.go:*PgStore:CreateTenant', 'users.go:*PgStore:ChangePasswordAtomic', 'users.go:*PgStore:CreateRefreshToken',
            'users.go:*PgStore:CreateUser', 'users.go:*PgStore:GetUser', 'users.go:*PgStore:GetUserByEmail',
            'users.go:*PgStore:RevokeUserRefreshTokens', 'users.go:*PgStore:TouchUserLogin', 'users.go:*PgStore:UpdateUserPassword')})
        self.assertEqual(sources, {'internal/api/handlers/auth.go', 'internal/outbound/builder.go', 'internal/outbound/delivery.go'})
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev2_tx.items() if k not in mutable},
                         {k: v for k, v in self.tx.items() if k not in mutable})
        self.assertEqual(self.tx['baseline_commit'], SOURCE_COMMIT)
        self.assertEqual(self.tx['last_review_base_commit'], SOURCE_COMMIT)

        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev2_compat.items() if k not in mutable},
                         {k: v for k, v in self.compat.items() if k not in mutable})
        self.assertEqual(self.compat['acquisition'], {**self.rev2_compat['acquisition'], 'base_commit': SOURCE_COMMIT})
        self.assertEqual(len(self.rev2_compat['routes']), len(self.compat['routes']))
        for before, after in zip(self.rev2_compat['routes'], self.compat['routes']):
            before = {k: v for k, v in before.items() if k != 'clients'}
            after = {k: v for k, v in after.items() if k != 'clients'}
            if before['route'] == 'POST /api/v1/auth/refresh':
                before = copy.deepcopy(before)
                self.assertTrue(before['request_contract']['required'])
                self.assertEqual(before['request_contract']['media_type_schemas']['application/json']['required'], ['refresh_token'])
                before['request_contract']['required'] = False
                del before['request_contract']['media_type_schemas']['application/json']['required']
            self.assertEqual(before, after)

        closure_changes = {
            str(gate.CURRENT_CLIENTS), 'internal/api/handlers/auth.go',
            'internal/api/handlers/r5_protocol_component_observations_test.go', 'internal/api/openapi.yaml',
            'web/components/company/compose.tsx', 'web/features/mail/components/draft-folder.tsx',
            'web/features/mail/components/message-pane.tsx', 'web/features/mail/components/submission-content.tsx',
            'web/features/mail/workspace.tsx', 'web/lib/api/base.ts',
        }
        self.assertEqual(set(self.rev2_compat['source_closure']), set(self.compat['source_closure']))
        self.assertEqual({p for p, sha in self.compat['source_closure'].items() if self.rev2_compat['source_closure'][p] != sha}, closure_changes)
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['revision2_snapshots'], {name: dict(commit=REVISION2_COMMIT, **pin) for name, pin in REVISION2_SNAPSHOTS.items()})
        reviewed_paths = {row['path'] for row in review['source_changes']}
        self.assertEqual(len(review['source_changes']), len(reviewed_paths))
        self.assertEqual(reviewed_paths, (closure_changes - {str(gate.CURRENT_CLIENTS)}) | sources)
        for row in review['source_changes']:
            path = row['path']
            for prefix, commit in [('before', REVISION2_COMMIT), ('after', SOURCE_COMMIT)]:
                raw = git('show', commit + ':' + path)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                self.assertEqual(git('rev-parse', commit + ':' + path).decode().strip(), row[prefix + '_blob'])
            self.assertEqual((ROOT / path).read_bytes(), git('show', SOURCE_COMMIT + ':' + path))
        original = review['unchanged_validators_and_collectors']
        self.assertEqual(set(original), {'scripts/check_r5_transactions.py', 'scripts/check_r5_compatibility.py',
            'scripts/collect_api_calls.cjs', 'cmd/r5txinventory/main.go', 'internal/architecture/route_inventory_test.go'})
        for path, sha in original.items():
            raw = git('show', REVISION2_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), sha)

    def test_revision3_client_inventory_preserves_routes_and_forwarders(self):
        documented = json.loads((ROOT / REVISION2_SNAPSHOTS['client_routes']['path']).read_text())
        self.assertEqual(len(self.clients), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.clients), 7)
        line_changes = 0
        owner_changes = []
        for old, now, previous_doc, current_doc in zip(self.revision2['clients'], self.clients, self.revision2['client_routes'], documented):
            self.assertEqual({k: v for k, v in current_doc.items() if k != 'routes'}, now)
            self.assertEqual(previous_doc['routes'], current_doc['routes'])
            line_changes += old['line'] != now['line']
            old = {k: v for k, v in old.items() if k != 'line'}
            now = {k: v for k, v in now.items() if k != 'line'}
            if old['owner'] != now['owner']:
                owner_changes.append((old['source'], old['path'], old['methods'], old['owner'], now['owner']))
                old['owner'] = now['owner']
            self.assertEqual(old, now)
        self.assertEqual(line_changes, 19)
        self.assertEqual(owner_changes, [('web/components/company/compose.tsx', '/api/v1/company/mailboxes/{id}/attachments', ['POST'], 'a', 'Compose')])

    def test_unapproved_client_and_caller_locations_are_rejected(self):
        clients = copy.deepcopy(self.clients)
        clients[0]['line'] += 1
        with self.assertRaisesRegex(ValueError, '^client producer differs'):
            gate.validate(self.compat, self.routes, clients)
        ast = copy.deepcopy(self.ast)
        caller = next(f for f in ast['functions'] if f['file'] == 'internal/api/handlers/auth.go' and f['name'] == 'Login')
        next(call for call in caller['calls'] if call['name'] == 'GetUserByEmail')['line'] += 1
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.tx, ast, self.migrations)

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
