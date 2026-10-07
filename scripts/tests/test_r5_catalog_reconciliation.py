"""Frozen revision-1/2/3/4 reviews and current revision-5 facts; original validators."""
import copy
from collections import Counter
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
REVISION3_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261007'
REVISION4_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION4-20261007'
CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION5-20261007'
SOURCE_COMMIT = '79738c17d185f1cd5cd1c8d550a9f15a5501f31c'
REVISION4_SOURCE_COMMIT = 'e32564304c0c84aa80b1184e72129fb0316d989d'
REVISION4_COMMIT = '6b6163dc8941aa36bb9fa2b75f40c101b8f662fe'
REVISION3_SOURCE_COMMIT = '7b7dbfeaad5c87e875bf19e5a9e213867fda2db3'
REVISION3_COMMIT = '4065c4909c8f21a401a9a1af6370fa3f72670b99'
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


REVISION3_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': '2dbd71b6b9f2ccd6eb1bf141f439b6ff83a1f26e',
        'sha256': '3a49928f35308951340025741ba0c308e8fecf20fc7792bc5b45115175f3f009',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '6b14b9ec4e8aac5caac5d74f47847b16e25e9e5e',
        'sha256': '0817aa5c77142849401dafe8e5acf3194feedc489539442a3cdcf9686cfd7f0d',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': 'f9f82391f73b3e8a5680c015a860bf880a429d2d',
        'sha256': '842ade963ab45a9215123929ed1db529da5d94c4a8c2462f612df9c615e3399c',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '1d375226040c4edb3b4571eaddf1396f875643b1',
        'sha256': '8e00ecf63e11b46062add84f062ad7f7d780949ae842127e5aed055db05fb6de',
    },
}


REVISION4_SNAPSHOTS = {
    'transaction': {
        'path': 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json',
        'blob': 'bd50c385e9c6c385a650f68ce5c5857ee69986ea',
        'sha256': '5fc5fb70255b5e6986b9aab38157fbb915b6012377d76d5b2a1956fc2907de2c',
    },
    'compatibility': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json',
        'blob': '23a227e3eae3aacc44d2c2be97c35bfa55bcc8a6',
        'sha256': '56452854110d0c33b5d2ae9eaf1abe43a7f77681a59307fc2470796dbdc18f56',
    },
    'clients': {
        'path': 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json',
        'blob': '9989cd7fe881f23eb2fe5b08e1a390e3128033e3',
        'sha256': '83eeb4a43fa3a6c762353c1597c00e998ca64bf68138a7eb165984095b110e3d',
    },
    'client_routes': {
        'path': 'docs/company-mail/evidence/R5-CLIENT-CALLS.json',
        'blob': '6a9cac1e365ce2664c6cddb170009d52fec2184c',
        'sha256': '0877601e0a825ba28f2e912bc057cf92e7d607ca568f5d1b29621e2798dc5139',
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


def revision3_snapshot(name):
    pin = REVISION3_SNAPSHOTS[name]
    ref = REVISION3_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-3 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-3 complete snapshot bytes differ: ' + name)
    return json.loads(raw)


def revision4_snapshot(name):
    pin = REVISION4_SNAPSHOTS[name]
    ref = REVISION4_COMMIT + ':' + pin['path']
    if git('rev-parse', ref).decode().strip() != pin['blob']:
        raise ValueError('revision-4 Git blob differs: ' + name)
    raw = git('show', ref)
    if hashlib.sha256(raw).hexdigest() != pin['sha256']:
        raise ValueError('revision-4 complete snapshot bytes differ: ' + name)
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
        cls.revision3 = {name: revision3_snapshot(name) for name in REVISION3_SNAPSHOTS}
        cls.rev3_tx = cls.revision3['transaction']
        cls.rev3_compat = cls.revision3['compatibility']
        cls.revision4 = {name: revision4_snapshot(name) for name in REVISION4_SNAPSHOTS}
        cls.rev4_tx = cls.revision4['transaction']
        cls.rev4_compat = cls.revision4['compatibility']
        cls.ast = tx.extract()
        cls.migrations = tx.migration_inventory()
        cls.routes, cls.clients = gate.collect()

    def test_old_pins_reject_and_current_revision_passes_same_actual_facts(self):
        with self.assertRaisesRegex(ValueError, 'function syntax drift:.*employee_disposition'):
            tx.validate(self.old_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev2_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev3_tx, self.ast, self.migrations)
        with self.assertRaisesRegex(ValueError, '^caller drift:'):
            tx.validate(self.rev4_tx, self.ast, self.migrations)
        # The old rows fail before the closure hash check because
        # reviewed client locations changed. The original hash-only rev1->rev2
        # assertion remains verified unchanged in the fixed historical checkout.
        for old in (self.old_compat, self.rev2_compat, self.rev3_compat, self.rev4_compat):
            with self.assertRaisesRegex(ValueError, '^route/schema/client/test source drift:'):
                gate.validate(old, self.routes, self.clients)
        self.assertFalse(tx.validate(self.tx, self.ast, self.migrations)['runtime_verified'])
        self.assertFalse(gate.validate(self.compat, self.routes, self.clients)['product_green'])
        for name, current in (('transaction', self.tx), ('compatibility', self.compat)):
            revision = current['inventory_revision']
            pin = REVISION4_SNAPSHOTS[name]
            self.assertEqual(revision['revision'], 5)
            self.assertEqual(revision['source_commit'], SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION4_COMMIT)
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
        current_entries = {entry['id']: entry for entry in self.rev3_tx['entries']}
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
                         {k: v for k, v in self.rev3_tx.items() if k not in mutable})
        self.assertEqual(self.rev3_tx['baseline_commit'], REVISION3_SOURCE_COMMIT)
        self.assertEqual(self.rev3_tx['last_review_base_commit'], REVISION3_SOURCE_COMMIT)

        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev2_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev3_compat.items() if k not in mutable})
        self.assertEqual(self.rev3_compat['acquisition'], {**self.rev2_compat['acquisition'], 'base_commit': REVISION3_SOURCE_COMMIT})
        self.assertEqual(len(self.rev2_compat['routes']), len(self.rev3_compat['routes']))
        for before, after in zip(self.rev2_compat['routes'], self.rev3_compat['routes']):
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
        self.assertEqual(set(self.rev2_compat['source_closure']), set(self.rev3_compat['source_closure']))
        self.assertEqual({p for p, sha in self.rev3_compat['source_closure'].items() if self.rev2_compat['source_closure'][p] != sha}, closure_changes)
        review = json.loads((REVISION3_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['source_commit'], REVISION3_SOURCE_COMMIT)
        self.assertEqual(review['revision2_snapshots'], {name: dict(commit=REVISION2_COMMIT, **pin) for name, pin in REVISION2_SNAPSHOTS.items()})
        reviewed_paths = {row['path'] for row in review['source_changes']}
        self.assertEqual(len(review['source_changes']), len(reviewed_paths))
        self.assertEqual(reviewed_paths, (closure_changes - {str(gate.CURRENT_CLIENTS)}) | sources)
        for row in review['source_changes']:
            path = row['path']
            for prefix, commit in [('before', REVISION2_COMMIT), ('after', REVISION3_SOURCE_COMMIT)]:
                raw = git('show', commit + ':' + path)
                self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                self.assertEqual(git('rev-parse', commit + ':' + path).decode().strip(), row[prefix + '_blob'])
            self.assertEqual(git('show', REVISION3_COMMIT + ':' + path), git('show', REVISION3_SOURCE_COMMIT + ':' + path))
        original = review['unchanged_validators_and_collectors']
        self.assertEqual(set(original), {'scripts/check_r5_transactions.py', 'scripts/check_r5_compatibility.py',
            'scripts/collect_api_calls.cjs', 'cmd/r5txinventory/main.go', 'internal/architecture/route_inventory_test.go'})
        for path, sha in original.items():
            raw = git('show', REVISION2_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), sha)

    def test_revision3_client_inventory_preserves_routes_and_forwarders(self):
        documented = self.revision3['client_routes']
        self.assertEqual(len(self.revision3['clients']), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision3['clients']), 7)
        line_changes = 0
        owner_changes = []
        for old, now, previous_doc, current_doc in zip(self.revision2['clients'], self.revision3['clients'], self.revision2['client_routes'], documented):
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

    def test_revision4_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['source_commit'], REVISION4_SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], git('rev-parse', REVISION4_SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision3_snapshots'], {name: dict(commit=REVISION3_COMMIT, **pin) for name, pin in REVISION3_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev3_tx), ('compatibility', self.rev3_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 3)
            self.assertEqual(revision['source_commit'], REVISION3_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION2_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION2_SNAPSHOTS[name][pin_field])
        old = {entry['id']: entry for entry in self.rev3_tx['entries']}
        current = {entry['id']: entry for entry in self.rev4_tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(set(changes), {tx.PG + suffix for suffix in (
            'company_mail.go:*PgStore:FinishMailAttachment', 'company_mail.go:*PgStore:GetWorkAttachment',
            'company_mail.go:*PgStore:ReserveMailAttachment', 'company_members.go:*PgStore:GetWorkMailbox',
            'outbound.go:*PgStore:CreateOutboundAttempt', 'outbound.go:*PgStore:IsSuppressed',
            'outbound.go:*PgStore:MarkOutboundJobFailed', 'outbound.go:*PgStore:MarkOutboundJobSent',
            'outbound_recipients.go:*PgStore:BeginOutboundRecipient', 'outbound_recipients.go:*PgStore:CompleteOutboundRecipient',
            'postgres.go:*PgStore:Close', 'postgres.go::New', 'submissions.go:*PgStore:GetSubmissionAttachment',
            'submissions.go:*PgStore:GetSubmissionContent', 'submissions.go:*PgStore:ListSubmissionAttachments')})
        additions, removals = [], []
        locations = 0
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                matches[0]['line'] = row['after_line']
                locations += 1
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, 29)
        self.assertEqual(additions, [(tx.PG + 'postgres.go:*PgStore:Close', 'internal/app/companymail/attachment_read.go::readOwnedAttachment', 'input.Close')])
        self.assertEqual(removals, [(tx.PG + 'postgres.go:*PgStore:Close', 'internal/app/companymail/service.go:*Service:verifiedFile', 'r.Close'),
                                    (tx.PG + 'postgres.go::New', 'internal/mailcontent/parser.go:*Parser:Attachment', 'errors.New')])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev3_tx.items() if k not in mutable},
                         {k: v for k, v in self.rev4_tx.items() if k not in mutable})
        self.assertEqual(self.rev4_tx['baseline_commit'], REVISION4_SOURCE_COMMIT)
        self.assertEqual(self.rev4_tx['last_review_base_commit'], REVISION4_SOURCE_COMMIT)

    def test_revision4_client_locations_and_source_hashes_are_explicitly_bound(self):
        review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        documented = self.revision4['client_routes']
        self.assertEqual(len(self.revision4['clients']), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.revision4['clients']), 7)
        changed_indices = []
        for index, (before, after, previous_doc, current_doc) in enumerate(zip(self.revision3['clients'], self.revision4['clients'], self.revision3['client_routes'], documented)):
            self.assertEqual({k: v for k, v in before.items() if k != 'line'}, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(previous_doc['routes'], current_doc['routes'])
            self.assertEqual({k: v for k, v in current_doc.items() if k != 'routes'}, after)
            if before != after:
                changed_indices.append(index)
        self.assertEqual(changed_indices, [0, 1, 2, 3, 4, 5, 93, 94, 95])
        self.assertEqual([row['index'] for row in review['client_locations']], changed_indices)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev3_compat.items() if k not in mutable},
                         {k: v for k, v in self.rev4_compat.items() if k not in mutable})
        self.assertEqual(self.rev4_compat['acquisition'], dict(self.rev3_compat['acquisition'], base_commit=REVISION4_SOURCE_COMMIT))
        self.assertEqual(len(self.rev3_compat['routes']), len(self.rev4_compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev3_compat['routes'], self.rev4_compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append({'route': after['route'], 'changed_fields': ['clients']})
        self.assertEqual(len(changed_routes), 7)
        self.assertEqual(review['compatibility_routes'], changed_routes)
        closure_changes = {str(gate.CURRENT_CLIENTS), 'web/app/(dashboard)/company/recovery/page.tsx', 'web/lib/api/base.ts'}
        self.assertEqual(set(self.rev3_compat['source_closure']), set(self.rev4_compat['source_closure']))
        self.assertEqual({p for p, sha in self.rev4_compat['source_closure'].items() if self.rev3_compat['source_closure'][p] != sha}, closure_changes)
        paths = (closure_changes - {str(gate.CURRENT_CLIENTS)}) | {
            'internal/app/companymail/attachment_read.go', 'internal/app/companymail/service.go', 'internal/mailcontent/parser.go',
            'internal/outbound/delivery.go', 'internal/outbound/recipient_delivery.go'}
        self.assertEqual({row['path'] for row in review['source_changes']}, paths)
        self.assertEqual(len(review['source_changes']), len(paths))
        for row in review['source_changes']:
            path = row['path']
            if path == 'internal/app/companymail/attachment_read.go':
                self.assertEqual(git('ls-tree', REVISION3_COMMIT, '--', path), b'')
                self.assertIsNone(row['before_sha256'])
                self.assertIsNone(row['before_blob'])
            else:
                self.assertEqual(hashlib.sha256(git('show', REVISION3_COMMIT + ':' + path)).hexdigest(), row['before_sha256'])
                self.assertEqual(git('rev-parse', REVISION3_COMMIT + ':' + path).decode().strip(), row['before_blob'])
            raw = git('show', REVISION4_SOURCE_COMMIT + ':' + path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual(git('rev-parse', REVISION4_SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(git('show', REVISION4_COMMIT + ':' + path), raw)
        historical_review = json.loads((REVISION3_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['unchanged_validators_and_collectors'], historical_review['unchanged_validators_and_collectors'])

    def test_revision5_preserves_reviews_and_records_exact_caller_changes(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['inventory_revision'], 5)
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], '1621d5fefb443d9d314abc2f9e0fab94914ce4c4')
        self.assertEqual(review['source_tree'], git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip())
        self.assertEqual(review['revision4_snapshots'], {name: dict(commit=REVISION4_COMMIT, **pin) for name, pin in REVISION4_SNAPSHOTS.items()})
        for name, historical in [('transaction', self.rev4_tx), ('compatibility', self.rev4_compat)]:
            revision = historical['inventory_revision']
            self.assertEqual(revision['revision'], 4)
            self.assertEqual(revision['source_commit'], REVISION4_SOURCE_COMMIT)
            self.assertEqual(revision['previous_snapshot_commit'], REVISION3_COMMIT)
            for field, pin_field in [('previous_snapshot', 'path'), ('previous_snapshot_blob', 'blob'), ('previous_sha256', 'sha256')]:
                self.assertEqual(revision[field], REVISION3_SNAPSHOTS[name][pin_field])

        old = {entry['id']: entry for entry in self.rev4_tx['entries']}
        current = {entry['id']: entry for entry in self.tx['entries']}
        self.assertEqual(set(old), set(current))
        changes = {row['id']: row for row in review['transaction_callers']}
        self.assertEqual(len(changes), len(review['transaction_callers']))
        self.assertEqual(set(changes), {tx.PG + suffix for suffix in (
            'company_mail.go:*PgStore:OutboundAttachments', 'outbound.go:*PgStore:IsSuppressed',
            'outbound_content.go:*PgStore:CanReadOutboundContent', 'outbound_retry.go:*PgStore:RequeueOutboundJobAuthorized',
            'postgres.go:*PgStore:Close', 'postgres.go::New')})
        additions, removals, locations = [], [], 0
        for name, before in old.items():
            after = current[name]
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            if name not in changes:
                self.assertEqual(before['callers'], after['callers'])
                continue
            delta = changes[name]
            for prefix, value in [('before', before['callers']), ('after', after['callers'])]:
                self.assertEqual(delta[prefix + '_count'], len(value))
                self.assertEqual(delta[prefix + '_sha256'], hashlib.sha256(gate.canonical_bytes(value)).hexdigest())
            transformed = copy.deepcopy(before['callers'])
            for row in delta['removed']:
                transformed.remove(row)
                removals.append((name, row['caller_id'], row['expression']))
            for row in delta['locations']:
                matches = [c for c in transformed if c['caller_id'] == row['caller_id'] and c['file'] == row['source']
                           and c['expression'] == row['expression'] and c['line'] == row['before_line']]
                self.assertEqual(len(matches), 1)
                matches[0]['line'] = row['after_line']
                locations += 1
            for row in delta['added']:
                transformed.append(row)
                additions.append((name, row['caller_id'], row['expression']))
            self.assertEqual(Counter(json.dumps(row, sort_keys=True) for row in transformed),
                             Counter(json.dumps(row, sort_keys=True) for row in after['callers']))
        self.assertEqual(locations, 8)
        self.assertEqual(additions, [
            (tx.PG + 'postgres.go:*PgStore:Close', 'internal/outbound/queued_attachment_read.go::readQueuedAttachment', 'input.Close'),
            (tx.PG + 'postgres.go::New', 'internal/app/recovery/verify.go::Verify', 'sha256.New'),
            (tx.PG + 'postgres.go::New', 'internal/outbound/queued_attachment_read.go::readQueuedAttachment', 'errors.New')])
        self.assertEqual(removals, [])
        mutable = {'entries', 'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary'}
        self.assertEqual({k: v for k, v in self.rev4_tx.items() if k not in mutable},
                         {k: v for k, v in self.tx.items() if k not in mutable})
        self.assertEqual(self.tx['baseline_commit'], SOURCE_COMMIT)
        self.assertEqual(self.tx['last_review_base_commit'], SOURCE_COMMIT)
        self.assertFalse(review['transaction']['task_complete'])
        self.assertFalse(review['transaction']['runtime_verified'])
        self.assertFalse(review['compatibility']['task_complete'])
        self.assertFalse(review['compatibility']['product_green'])

    def test_revision5_client_moves_and_source_hashes_are_explicitly_bound(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        documented = json.loads((ROOT / REVISION4_SNAPSHOTS['client_routes']['path']).read_text())
        self.assertEqual(len(self.clients), 134)
        self.assertEqual(len(documented), 134)
        self.assertEqual(sum(row['forwarding'] for row in self.clients), 7)
        # Publish moves from the page into save; preview moves ahead of save in
        # the editor. Preserve the original route identities through both moves.
        previous_indices = list(range(8)) + list(range(9, 21)) + [23, 21, 22, 8] + list(range(24, 134))
        self.assertEqual(sorted(previous_indices), list(range(134)))
        client_changes, reordered = [], []
        for index, previous_index in enumerate(previous_indices):
            before, after = self.revision4['clients'][previous_index], self.clients[index]
            expected = {k: v for k, v in before.items() if k != 'line'}
            if previous_index == 8:
                expected.update(source='web/components/company/templates/editor.tsx', owner='save',
                                expression='`/templates/${res.id}/publish`')
            if previous_index == 21:
                expected['expression'] = '`/templates/${snapshot.id}`'
            if previous_index == 23:
                expected['owner'] = 'value'
            self.assertEqual(expected, {k: v for k, v in after.items() if k != 'line'})
            self.assertEqual(self.revision4['client_routes'][previous_index]['routes'], documented[index]['routes'])
            self.assertEqual({k: v for k, v in documented[index].items() if k != 'routes'}, after)
            if before != after:
                client_changes.append(dict(before_index=previous_index, after_index=index, path=after['path'], methods=after['methods'],
                    changes={k: dict(before=before[k], after=after[k]) for k in before if before[k] != after[k]}))
            if previous_index != index:
                reordered.append(dict(before_index=previous_index, after_index=index))
        self.assertEqual([(row['before_index'], row['after_index']) for row in client_changes],
                         [(6, 6), (7, 7), (23, 20), (21, 21), (22, 22), (8, 23), (27, 27)])
        self.assertEqual(review['client_changes'], client_changes)
        self.assertEqual(review['client_reorder'], reordered)
        mutable = {'routes', 'source_closure', 'inventory_revision', 'acquisition'}
        self.assertEqual({k: v for k, v in self.rev4_compat.items() if k not in mutable},
                         {k: v for k, v in self.compat.items() if k not in mutable})
        self.assertEqual(self.compat['acquisition'], dict(self.rev4_compat['acquisition'], base_commit=SOURCE_COMMIT))
        self.assertEqual(len(self.rev4_compat['routes']), len(self.compat['routes']))
        changed_routes = []
        for before, after in zip(self.rev4_compat['routes'], self.compat['routes']):
            self.assertEqual({k: v for k, v in before.items() if k != 'clients'}, {k: v for k, v in after.items() if k != 'clients'})
            if before != after:
                changed_routes.append(dict(route=after['route'], changed_fields=['clients']))
        self.assertEqual({row['route'] for row in changed_routes}, {
            'GET /api/v1/company/templates', 'POST /api/v1/company/templates', 'POST /api/v1/company/templates/preview',
            'POST /api/v1/company/templates/{id}/publish', 'POST /api/v1/company/templates/{id}/retire',
            'PUT /api/v1/company/templates/{id}', 'GET /api/v1/company/templates/{id}/versions'})
        self.assertEqual(review['compatibility_routes'], changed_routes)
        closure_changes = {str(gate.CURRENT_CLIENTS), 'internal/company/templates.go',
            'web/app/(dashboard)/company/templates/page.tsx', 'web/components/company/templates/editor.tsx',
            'web/components/company/templates/versions.tsx'}
        self.assertEqual(set(self.rev4_compat['source_closure']), set(self.compat['source_closure']))
        self.assertEqual({p for p, sha in self.compat['source_closure'].items() if self.rev4_compat['source_closure'][p] != sha}, closure_changes)
        paths = (closure_changes - {str(gate.CURRENT_CLIENTS)}) | {
            'internal/app/recovery/verify.go', 'internal/app/submissions/service.go',
            'internal/outbound/company.go', 'internal/outbound/queued_attachment_read.go'}
        self.assertEqual({row['path'] for row in review['source_changes']}, paths)
        self.assertEqual(len(review['source_changes']), len(paths))
        for row in review['source_changes']:
            path = row['path']
            if path == 'internal/outbound/queued_attachment_read.go':
                self.assertEqual(git('ls-tree', REVISION4_COMMIT, '--', path), b'')
                self.assertIsNone(row['before_blob'])
                self.assertIsNone(row['before_sha256'])
            else:
                self.assertEqual(git('rev-parse', REVISION4_COMMIT + ':' + path).decode().strip(), row['before_blob'])
                self.assertEqual(hashlib.sha256(git('show', REVISION4_COMMIT + ':' + path)).hexdigest(), row['before_sha256'])
            raw = git('show', SOURCE_COMMIT + ':' + path)
            self.assertEqual(git('rev-parse', SOURCE_COMMIT + ':' + path).decode().strip(), row['after_blob'])
            self.assertEqual(hashlib.sha256(raw).hexdigest(), row['after_sha256'])
            self.assertEqual((ROOT / path).read_bytes(), raw)
        self.assertEqual(set(review['generated_catalogs']), set(REVISION4_SNAPSHOTS))
        for name, value in review['generated_catalogs'].items():
            self.assertEqual(value['path'], REVISION4_SNAPSHOTS[name]['path'])
            self.assertEqual(hashlib.sha256((ROOT / value['path']).read_bytes()).hexdigest(), value['sha256'])
        previous_review = json.loads(git('show', REVISION4_COMMIT + ':' + str((REVISION4_EVIDENCE / 'reconciliation.json').relative_to(ROOT))))
        self.assertEqual(review['unchanged_validators_and_collectors'], previous_review['unchanged_validators_and_collectors'])
        # All existing historical packets remain byte-for-byte pinned, including
        # the revision-4 review now read directly from its public Git object.
        for directory in (EVIDENCE, REVISION3_EVIDENCE, REVISION4_EVIDENCE):
            names = git('ls-tree', '-r', '--name-only', REVISION4_COMMIT, '--', str(directory.relative_to(ROOT))).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(p.relative_to(ROOT)) for p in directory.rglob('*') if p.is_file()}, set(names))
            for path in names:
                self.assertEqual((ROOT / path).read_bytes(), git('show', REVISION4_COMMIT + ':' + path))

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
