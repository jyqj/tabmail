#!/usr/bin/env python3
"""Prepare revision-16 catalog proposals from original producers in an ordinary clone.

This author utility never pushes, updates Git refs, changes an invocation checkout,
or qualifies product/runtime completion. Its generated files are review proposals.
The independent original CLI, five-module and source-version CI jobs remain the
qualification authority for a later committed maintenance candidate.
"""
from __future__ import annotations
import argparse
import ast
import copy
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import pprint
import re
import subprocess
import sys
import tempfile
import time

FINAL_SOURCE = 'b6516f9ac873f4464894933b5c3a0e046bc9a4a5'
FINAL_TREE = '4034b36faa89a421f2a21b525cf35e93da4a1670'
CATALOG_BASE = 'eae219951c6f9cbf17eb4062b69464c731807be7'
CATALOG_TREE = 'e4125b61d7c8aa419518414d2ddb73efcc8239dc'
PREVIOUS_SOURCE = '26a30e3c6963d553efdd8a8b33c095feb924c758'
PREVIOUS_TREE = '437de0716fc9247e188f0409654f4f178905dc33'
RECONCILIATION = 'scripts/tests/test_r5_catalog_reconciliation.py'
BASE_TEST_SHA256 = '5a5c459974c9eee7af23ba648d28f98f9d48b6045e546c07f15039f23f322943'
SCAFFOLD_SHA256 = '3d02385f12e0c74ad4d1ccb60f97ca9b599f11085d5a25abcf8c74dd2752e1e3'
REVIEW_DIRECTORY = 'docs/company-mail/evidence/R5-CATALOG-REVISION16-20261009'
REVIEW_PATH = REVIEW_DIRECTORY + '/reconciliation.json'
README_PATH = REVIEW_DIRECTORY + '/README.md'
MAINTENANCE_PATHS = ('.github/workflows/catalog-reconciliation.yml', 'scripts/prepare_r5_catalog_revision16.py')
MUTABLE_TEST_PATHS = [RECONCILIATION, 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']
PROTECTED_ROOTS = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
SAVE_ID = 'internal/store/postgres/company_templates.go:*PgStore:SaveMailTemplate'
SEED_ID = 'internal/store/postgres/settings.go:*PgStore:SeedSetting'
REVIEWED_PRODUCT_FILES = {
    'internal/store/postgres/company_templates.go': '71c2781b749d5e5fcbace9d44488475c4e3c1a8d',
    'internal/store/postgres/settings.go': '1566afe9446572f5d690d137c395e0615a8723d1',
}
FROZEN_METHODS = (
    'test_revision15_binds_actual_source_facts_and_preserves_manual_reviews',
    'test_revision15_preserves_public_history_collectors_and_original_rejections',
)
NEW_METHODS = {name.replace('revision15', 'revision16') for name in FROZEN_METHODS}
CURRENT_METHOD = 'test_old_pins_reject_and_current_revision_passes_same_actual_facts'
CURRENT_REPLACEMENTS = (
    ("REVISION15_EXPECTED", "REVISION16_EXPECTED"),
    ("('revision14', self.rev14_tx, self.rev14_compat)]", "('revision14', self.rev14_tx, self.rev14_compat), ('revision15', self.rev15_tx, self.rev15_compat)]"),
    ("pin = REVISION14_SNAPSHOTS[name]", "pin = REVISION15_SNAPSHOTS[name]"),
    ("revision['revision'], 15", "revision['revision'], 16"),
    ("revision['previous_snapshot_commit'], REVISION14_COMMIT", "revision['previous_snapshot_commit'], REVISION15_COMMIT"),
)
MANUAL_FIELDS = ('lock_fk_wait_fence', 'evidence', 'unverified_risks', 'file_family_context')
SOURCE_BOUNDARY = (
    'Original syntax producers only; SQL fragments, name-match callers and local '
    'operation order do not prove resolved dispatch, executed SQL or concurrency '
    'correctness. Separately bound EXEC product regressions retain their own '
    'source identities and limited qualification. This maintenance adds zero '
    'implementation TODOs and grants no runtime, release or parent acceptance.'
)

SNAPSHOTS = json.loads("{\"transaction\":{\"path\":\"docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json\",\"blob\":\"bcdef4ebfddc1b71073881582b48b0b687b3aecf\",\"sha256\":\"c4a3f7491e63a8b8eb9bf95649ab874548417c0810ae181e7b10be49cd885b22\",\"bytes\":2921837},\"compatibility\":{\"path\":\"docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json\",\"blob\":\"2f888d7f5779bacc7fe168824cefcd4408a08aea\",\"sha256\":\"b7bacb11a32215df279bd1bf394907ce4c11eab6c534c3a448590dcc77674db5\",\"bytes\":411900},\"clients\":{\"path\":\"docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json\",\"blob\":\"9a1d642bf678b6449fa0b2693547a4d0096c201e\",\"sha256\":\"a2aa98cfdfb99ac8a7eade5348102c1ecde26dd9032b9278e546db25ae394b47\",\"bytes\":41444},\"client_routes\":{\"path\":\"docs/company-mail/evidence/R5-CLIENT-CALLS.json\",\"blob\":\"47d35588f0e5fa84ea7c442a32296ec1c6e90490\",\"sha256\":\"5d55680a60b43c71f849738603b51f82beb0a3ad7b03c7734a5bf39856ddf166\",\"bytes\":50365}}")
ORIGINAL_PRODUCERS = json.loads("{\"scripts/check_r5_transactions.py\":\"1f14b5b4e3ef11cd0642e9d7261cde285c17e47384c90e15b56adc68323b9355\",\"scripts/check_r5_compatibility.py\":\"d3d0bd96c84a18c224160faaad96f3a928aab5224d93c31b6ec464489f740209\",\"scripts/collect_api_calls.cjs\":\"06896de9f18dda8c7fd4f1ef2d520e4bd71fa831a99f0d63863a1fa6a06682e8\",\"cmd/r5txinventory/main.go\":\"ad14b919505b23947de335dda6077d4f6745185329d911cc357e60b1001d2361\",\"internal/architecture/route_inventory_test.go\":\"609ee5489c3e021f9b1a0238b47e4a01d09c5e5eef1144df780d6456e2ed4f05\"}")
REVISION15_PINS_TEXT = r'''REVISION15_COMMIT = 'eae219951c6f9cbf17eb4062b69464c731807be7'
REVISION15_TREE = 'e4125b61d7c8aa419518414d2ddb73efcc8239dc'
REVISION15_SOURCE_COMMIT = '26a30e3c6963d553efdd8a8b33c095feb924c758'
REVISION15_SOURCE_TREE = '437de0716fc9247e188f0409654f4f178905dc33'
REVISION15_SNAPSHOTS = {
    "transaction": {
        "path": "docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json",
        "blob": "bcdef4ebfddc1b71073881582b48b0b687b3aecf",
        "sha256": "c4a3f7491e63a8b8eb9bf95649ab874548417c0810ae181e7b10be49cd885b22",
        "bytes": 2921837
    },
    "compatibility": {
        "path": "docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json",
        "blob": "2f888d7f5779bacc7fe168824cefcd4408a08aea",
        "sha256": "b7bacb11a32215df279bd1bf394907ce4c11eab6c534c3a448590dcc77674db5",
        "bytes": 411900
    },
    "clients": {
        "path": "docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json",
        "blob": "9a1d642bf678b6449fa0b2693547a4d0096c201e",
        "sha256": "a2aa98cfdfb99ac8a7eade5348102c1ecde26dd9032b9278e546db25ae394b47",
        "bytes": 41444
    },
    "client_routes": {
        "path": "docs/company-mail/evidence/R5-CLIENT-CALLS.json",
        "blob": "47d35588f0e5fa84ea7c442a32296ec1c6e90490",
        "sha256": "5d55680a60b43c71f849738603b51f82beb0a3ad7b03c7734a5bf39856ddf166",
        "bytes": 50365
    }
}


'''
REVISION15_SNAPSHOT_TEXT = r'''def revision15_snapshot(name):
    pin = REVISION15_SNAPSHOTS[name]
    ref = REVISION15_COMMIT + ':' + pin['path']
    raw = git('show', ref)
    if git('rev-parse', ref).decode().strip() != pin['blob'] or hashlib.sha256(raw).hexdigest() != pin['sha256'] or len(raw) != pin['bytes']:
        raise ValueError('revision15 public historical catalog identity drift: ' + name)
    return json.loads(raw)


'''
REVISION15_CONTEXT_TEXT = r'''    @classmethod
    def frozen_revision15_root(cls):
        if '_revision15_root' not in cls.__dict__:
            temporary = tempfile.TemporaryDirectory(prefix='r5-catalog-revision15-')
            cls.addClassCleanup(temporary.cleanup)
            root = Path(temporary.name) / 'source'
            subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '--no-checkout', str(ROOT), str(root)], check=True)
            subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', REVISION15_COMMIT], check=True)
            def frozen_git(*args):
                return subprocess.check_output(['git', '-C', str(root), *args]).decode().strip()
            if frozen_git('rev-parse', 'HEAD') != REVISION15_COMMIT or frozen_git('rev-parse', 'HEAD^{tree}') != REVISION15_TREE or frozen_git('status', '--porcelain', '--untracked-files=all'):
                raise ValueError('revision15 source checkout is not the exact clean public snapshot')
            # Reuse only the installed compiler. All historical Go/TypeScript
            # inputs and original assertion bodies come from public revision15.
            compiler = ROOT / 'web/node_modules/typescript'
            if not compiler.is_dir():
                raise ValueError('TypeScript required for historical original collection')
            (root / 'web/node_modules').mkdir()
            (root / 'web/node_modules/typescript').symlink_to(compiler.resolve(), target_is_directory=True)
            cls.revision15_ast = tx.extract(root)
            cls.revision15_migrations = tx.migration_inventory(root)
            with mock.patch.object(gate, 'ROOT', root):
                cls.revision15_routes, cls.revision15_clients = gate.collect()
            if frozen_git('rev-parse', 'HEAD') != REVISION15_COMMIT or frozen_git('rev-parse', 'HEAD^{tree}') != REVISION15_TREE or frozen_git('status', '--porcelain', '--untracked-files=all'):
                raise ValueError('revision15 public checkout changed during original collection')
            cls._revision15_root = root
        return cls._revision15_root

    @contextmanager
    def revision15_context(self):
        root = self.frozen_revision15_root()
        with ExitStack() as patches:
            patches.enter_context(mock.patch.dict(globals(), ROOT=root,
                CURRENT_EVIDENCE=root / 'docs/company-mail/evidence/R5-CATALOG-REVISION15-20261009',
                SOURCE_COMMIT=REVISION15_SOURCE_COMMIT, SOURCE_TREE=REVISION15_SOURCE_TREE))
            patches.enter_context(mock.patch.object(gate, 'ROOT', root))
            for name, value in dict(tx=self.rev15_tx, compat=self.rev15_compat,
                    ast=self.revision15_ast, migrations=self.revision15_migrations,
                    routes=self.revision15_routes, clients=self.revision15_clients).items():
                patches.enter_context(mock.patch.object(self, name, value))
            yield

'''
CURRENT_TESTS_TEXT = r'''    def test_revision16_binds_actual_source_facts_and_preserves_manual_reviews(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(review['inventory_revision'], 16)
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], SOURCE_TREE)
        self.assertEqual(git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip(), SOURCE_TREE)
        self.assertEqual(review['previous_catalog_commit'], REVISION15_COMMIT)
        self.assertEqual(review['previous_product_source_commit'], REVISION15_SOURCE_COMMIT)
        self.assertEqual(review['revision15_snapshots'],
                         {name: dict(commit=REVISION15_COMMIT, **pin) for name, pin in REVISION15_SNAPSHOTS.items()})
        for field, expected in REVISION16_EXPECTED['review_field_sha256'].items():
            self.assertEqual(hashlib.sha256(gate.canonical_bytes(review[field])).hexdigest(), expected)
        self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
        self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
        self.assertEqual(review['transaction'], REVISION16_EXPECTED['transaction'])
        self.assertEqual(review['compatibility'], REVISION16_EXPECTED['compatibility'])
        actual_files = [dict(path=item['path'], sha256=item['sha256']) for item in self.ast['files'] if item['path'].startswith(tx.PG)]
        self.assertEqual(self.tx['postgres_files'], actual_files)
        before_files = {item['path']: item['sha256'] for item in self.rev15_tx['postgres_files']}
        after_files = {item['path']: item['sha256'] for item in self.tx['postgres_files']}
        self.assertEqual(set(after_files), set(before_files))
        self.assertEqual((len(before_files), len(after_files)), (64, 64))
        changed_files = set(REVISION16_EXPECTED['file_review_additions'])
        self.assertEqual([path for path in before_files if before_files[path] != after_files[path]],
                         sorted(changed_files))
        self.assertEqual(set(self.tx['reviewed_file_types']), set(self.rev15_tx['reviewed_file_types']))
        for path, before in self.rev15_tx['reviewed_file_types'].items():
            after = self.tx['reviewed_file_types'][path]
            if path in changed_files:
                self.assertEqual(set(after), set(before) | {'revision16_additions'})
                self.assertEqual({key: value for key, value in after.items() if key != 'revision16_additions'}, before)
                self.assertEqual(after['revision16_additions'], REVISION16_EXPECTED['file_review_additions'][path])
            else:
                self.assertEqual(after, before)
        self.assertEqual(review['transaction_file_review_additions'], REVISION16_EXPECTED['file_review_additions'])
        self.assertEqual(self.tx['migrations'], self.rev15_tx['migrations'])
        self.assertEqual(self.tx['historical_review_metadata'], self.rev15_tx['historical_review_metadata'])
        mutable_transaction = {'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary', 'postgres_files', 'reviewed_file_types', 'entries'}
        self.assertEqual({key: value for key, value in self.rev15_tx.items() if key not in mutable_transaction},
                         {key: value for key, value in self.tx.items() if key not in mutable_transaction})
        functions = {item['id']: item for item in self.ast['functions'] if item['file'].startswith(tx.PG)}
        classes = tx.classify(self.ast)
        old = {entry['id']: entry for entry in self.rev15_tx['entries']}
        current = {entry['id']: entry for entry in self.tx['entries']}
        added_ids = set(REVISION16_EXPECTED['added_entries'])
        reviewed = {row['id']: row for row in REVISION16_EXPECTED['body_changes']}
        self.assertEqual(set(current), set(old) | added_ids)
        self.assertEqual(set(current), set(functions))
        self.assertEqual((len(old), len(current)), (404, REVISION16_EXPECTED['transaction']['functions']))
        fingerprint = lambda value: hashlib.sha256(gate.canonical_bytes(value)).hexdigest()
        def derived_assertions(function):
            return {
                'direct_sql_effects': [dict(line=item['line'], sql_fragment=item['value']) for item in function['strings'] if tx.MUTATION.search(item['value'])],
                'direct_lock_fragments': [dict(line=item['line'], sql_fragment=item['value']) for item in function['strings'] if tx.LOCK.search(item['value'])],
                'transaction_helper_calls': [call for call in function['calls'] if call['name'] in tx.TX],
                'sql_execution_expressions': [call for call in function['calls'] if call['name'] in tx.SQL_CALLS],
            }
        def actual_callers(function):
            return [dict(caller_id=item['id'], file=item['file'], function=item['name'],
                line=call['line'], expression=call['expr'], status='name-match-candidate-not-dispatch-proof')
                for item in self.ast['functions'] for call in item['calls'] if call['name'] == function['name']]
        caller_changes, body_changes, classification_changes, entry_changes = [], [], [], []
        for identity, before in old.items():
            after, function = current[identity], functions[identity]
            self.assertEqual(after['syntax'], function)
            self.assertEqual(after['classification'], classes[identity])
            self.assertEqual(after['callers'], actual_callers(function))
            derived = derived_assertions(function)
            self.assertEqual({key: after['assertions'][key] for key in derived}, derived)
            self.assertEqual({key: value for key, value in before['assertions'].items() if key not in derived},
                             {key: value for key, value in after['assertions'].items() if key not in derived})
            mutable = {'syntax', 'classification', 'callers', 'assertions'}
            if identity in reviewed:
                self.assertEqual(before['syntax']['sha256'], reviewed[identity]['before_sha256'])
                self.assertEqual(function['sha256'], reviewed[identity]['after_sha256'])
                self.assertNotIn('source_review', before)
                manual = ('lock_fk_wait_fence', 'evidence', 'unverified_risks', 'file_family_context')
                self.assertEqual(after['historical_revision15_review'], {key: before[key] for key in manual})
                self.assertEqual({key: after[key] for key in (*manual, 'source_review')},
                                 REVISION16_EXPECTED['manual_review_fields'][identity])
                self.assertEqual(after['source_review']['base_commit'], SOURCE_COMMIT)
                self.assertEqual(after['source_review']['source_sha256'], function['sha256'])
                mutable.update((*manual, 'historical_revision15_review', 'source_review'))
            else:
                self.assertEqual(before['syntax']['sha256'], function['sha256'])
                if before['syntax'] != function:
                    self.assertIn(function['file'], changed_files)
            self.assertEqual({key: value for key, value in before.items() if key not in mutable},
                             {key: value for key, value in after.items() if key not in mutable})
            if before['callers'] != after['callers']:
                caller_changes.append(dict(id=identity, before=before['callers'], after=after['callers'],
                    before_sha256=fingerprint(before['callers']), after_sha256=fingerprint(after['callers'])))
            if before['syntax']['sha256'] != function['sha256']:
                body_changes.append(dict(id=identity, before_sha256=before['syntax']['sha256'], after_sha256=function['sha256']))
            if before['classification'] != after['classification']:
                classification_changes.append(dict(id=identity, before=before['classification'], after=after['classification']))
            if before != after:
                entry_changes.append(dict(id=identity,
                    changed_fields=[key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)],
                    before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
        for identity in sorted(added_ids):
            added, function = current[identity], functions[identity]
            self.assertEqual(added, REVISION16_EXPECTED['added_entries'][identity])
            self.assertEqual(added['syntax'], function)
            self.assertEqual(added['classification'], classes[identity])
            self.assertEqual(added['classification']['kind'], 'direct-write')
            self.assertTrue(added['classification']['direct_write'])
            self.assertEqual(added['callers'], actual_callers(function))
            self.assertEqual({key: added['assertions'][key] for key in derived_assertions(function)}, derived_assertions(function))
            self.assertEqual(added['source_review']['base_commit'], SOURCE_COMMIT)
            self.assertEqual(added['source_review']['source_sha256'], function['sha256'])
        self.assertEqual(caller_changes, review['transaction_callers'])
        self.assertEqual(body_changes, review['transaction_body_changes'])
        self.assertEqual(classification_changes, review['transaction_classification_changes'])
        self.assertEqual(entry_changes, review['transaction_entry_changes'])
        self.assertEqual(body_changes, REVISION16_EXPECTED['body_changes'])
        self.assertEqual(classification_changes, REVISION16_EXPECTED['classification_changes'])
        self.assertEqual(review['transaction_added_entries'], [dict(id=identity, entry_sha256=fingerprint(current[identity])) for identity in sorted(added_ids)])
        self.assertEqual(review['transaction_removed_entries'], [])
        self.assertEqual(review['manual_review_fields'], REVISION16_EXPECTED['manual_review_fields'])
        self.assertEqual(review['migration_changes'], [])
        self.assertEqual(review['client_before'], self.revision15['clients'])
        self.assertEqual(review['client_after'], self.clients)
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.revision15['clients'])).hexdigest(), review['client_before_sha256'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(self.clients)).hexdigest(), review['client_after_sha256'])
        old_routes = {row['route']: row for row in self.rev15_compat['routes']}
        new_routes = {row['route']: row for row in self.compat['routes']}
        fresh_routes = {row['route']: row for row in gate.source_facts(self.routes, self.clients)}
        self.assertEqual(set(old_routes), set(new_routes))
        route_changes = []
        for identity, before in old_routes.items():
            after = new_routes[identity]
            fact_keys = set(fresh_routes[identity])
            self.assertEqual({key: value for key, value in before.items() if key not in fact_keys},
                             {key: value for key, value in after.items() if key not in fact_keys})
            changed = [key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)]
            if changed:
                route_changes.append(dict(route=identity, changed_fields=changed,
                    before_sha256=hashlib.sha256(gate.canonical_bytes(before)).hexdigest(),
                    after_sha256=hashlib.sha256(gate.canonical_bytes(after)).hexdigest()))
        self.assertEqual(route_changes, review['compatibility_route_changes'])
        old_closure, new_closure = self.rev15_compat['source_closure'], self.compat['source_closure']
        closure_changes = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
                           for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
        self.assertEqual(closure_changes, review['closure_changes'])
        mutable = {'inventory_revision', 'acquisition', 'routes', 'source_closure'}
        self.assertEqual({k: v for k, v in self.rev15_compat.items() if k not in mutable},
                         {k: v for k, v in self.compat.items() if k not in mutable})
        self.assertEqual(self.compat['acquisition'], dict(self.rev15_compat['acquisition'], base_commit=SOURCE_COMMIT))
        for name, pin in review['generated_catalogs'].items():
            self.assertEqual(pin['path'], REVISION15_SNAPSHOTS[name]['path'])
            raw = (ROOT / pin['path']).read_bytes()
            self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
            self.assertEqual(len(raw), pin['bytes'])
        self.assertEqual(set(review['generated_catalogs']), set(REVISION15_SNAPSHOTS))
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in self.routes}
        documented = json.loads((ROOT / REVISION15_SNAPSHOTS['client_routes']['path']).read_text())
        self.assertEqual(documented, [dict(row, routes=[] if row['forwarding'] else
                         [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in self.clients])
        changed = git('diff', '--name-only', REVISION15_SOURCE_COMMIT, SOURCE_COMMIT, '--', 'internal', 'cmd', 'web').decode().splitlines()
        product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                         (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or
                         (p.startswith('web/locales/') and p.endswith('.json'))]
        self.assertEqual(product_paths, [row['path'] for row in review['source_changes']])
        self.assertEqual(product_paths, REVISION16_EXPECTED['product_paths'])
        self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in new_closure])
        build_paths = [path for path in ('web/package.json', 'web/package-lock.json')
                       if git('show', REVISION15_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(build_paths, [row['path'] for row in review['additional_build_metadata_changes']])
        schema_paths = [path for path in ('internal/api/openapi.yaml',)
                        if git('show', REVISION15_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(schema_paths, [row['path'] for row in review['additional_api_schema_changes']])
        for row in review['source_changes'] + review['additional_build_metadata_changes'] + review['additional_api_schema_changes']:
            self.assertEqual(row['before_commit'], REVISION15_SOURCE_COMMIT)
            self.assertEqual(row['after_commit'], SOURCE_COMMIT)
            for prefix, commit in (('before', REVISION15_SOURCE_COMMIT), ('after', SOURCE_COMMIT)):
                if row[prefix + '_blob'] is None:
                    self.assertFalse(git('ls-tree', commit, '--', row['path']))
                    self.assertIsNone(row[prefix + '_sha256'])
                    self.assertIsNone(row[prefix + '_bytes'])
                else:
                    ref = commit + ':' + row['path']
                    raw = git('show', ref)
                    self.assertEqual(git('rev-parse', ref).decode().strip(), row[prefix + '_blob'])
                    self.assertEqual(hashlib.sha256(raw).hexdigest(), row[prefix + '_sha256'])
                    self.assertEqual(len(raw), row[prefix + '_bytes'])
            self.assertEqual((ROOT / row['path']).read_bytes(), git('show', SOURCE_COMMIT + ':' + row['path']))
        self.assertFalse(review['runtime_verified'])
        self.assertFalse(review['product_green'])
        self.assertFalse(review['task_complete'])
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))

    def test_revision16_preserves_public_history_collectors_and_original_rejections(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        self.assertEqual(git('rev-parse', REVISION15_COMMIT + '^{tree}').decode().strip(), REVISION15_TREE)
        historical = []
        for directory in review['historical_directories']:
            names = git('ls-tree', '-r', '--name-only', REVISION15_COMMIT, '--', directory).decode().splitlines()
            self.assertTrue(names)
            self.assertEqual({str(path.relative_to(ROOT)) for path in (ROOT / directory).rglob('*') if path.is_file()}, set(names))
            for path in names:
                ref = REVISION15_COMMIT + ':' + path
                raw = git('show', ref)
                self.assertEqual((ROOT / path).read_bytes(), raw)
                historical.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                       sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        self.assertEqual(historical, review['historical_manifest'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(historical)).hexdigest(), REVISION16_EXPECTED['historical_manifest_sha256'])
        roots = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
        mutable = ['scripts/tests/test_r5_catalog_reconciliation.py', 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']
        self.assertEqual(review['mutable_current_positive_test_paths'], mutable)
        paths = git('ls-tree', '-r', '--name-only', SOURCE_COMMIT, '--', *roots).decode().splitlines()
        additions = REVISION16_EXPECTED['maintenance_source_pins']
        self.assertEqual(set(additions), {'.github/workflows/catalog-reconciliation.yml', 'scripts/prepare_r5_catalog_revision16.py'})
        self.assertTrue(set(paths).isdisjoint(additions))
        self.assertEqual({str(path.relative_to(ROOT)) for folder in ('scripts', '.github/workflows', 'cmd/r5txinventory')
                          for path in (ROOT / folder).rglob('*') if path.is_file() and '__pycache__' not in str(path) and path.suffix != '.pyc'} |
                         {'internal/architecture/route_inventory_test.go'}, set(paths) | set(additions))
        protected = []
        for path in paths:
            if path in mutable:
                continue
            ref = SOURCE_COMMIT + ':' + path
            raw = git('show', ref)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            protected.append(dict(path=path, blob=git('rev-parse', ref).decode().strip(),
                                  sha256=hashlib.sha256(raw).hexdigest(), bytes=len(raw)))
        for path, pin in sorted(additions.items()):
            raw = (ROOT / path).read_bytes()
            self.assertEqual(pin['path'], path)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
            self.assertEqual(len(raw), pin['bytes'])
            self.assertEqual(hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest(), pin['blob'])
            self.assertEqual(raw, git('cat-file', 'blob', pin['blob']))
            protected.append(pin)
        self.assertEqual(protected, review['protected_source_manifest'])
        self.assertEqual(hashlib.sha256(gate.canonical_bytes(protected)).hexdigest(), REVISION16_EXPECTED['protected_source_manifest_sha256'])
        for path, expected in review['unchanged_validators_and_collectors'].items():
            raw = git('show', REVISION15_COMMIT + ':' + path)
            self.assertEqual((ROOT / path).read_bytes(), raw)
            self.assertEqual(hashlib.sha256(raw).hexdigest(), expected)
        for path, replacements in (
            ('scripts/tests/test_r5_transactions.py', [("result['functions'], 404", "result['functions'], %d" % REVISION16_EXPECTED['transaction']['functions']),
                ("result['postgres_files'], 64", "result['postgres_files'], %d" % REVISION16_EXPECTED['transaction']['postgres_files'])]),
            ('scripts/tests/test_r5_compatibility.py', [("result['routes'],133", "result['routes'],%d" % REVISION16_EXPECTED['compatibility']['routes']),
                ("result['client_branches'],136", "result['client_branches'],%d" % REVISION16_EXPECTED['compatibility']['client_branches'])]),
        ):
            expected = git('show', REVISION15_COMMIT + ':' + path).decode()
            for before, after in replacements:
                self.assertEqual(expected.count(before), 1)
                expected = expected.replace(before, after)
            self.assertEqual((ROOT / path).read_text(), expected)
        path = 'scripts/tests/test_r5_catalog_reconciliation.py'
        before_raw = git('show', REVISION15_COMMIT + ':' + path).decode()
        after_raw = (ROOT / path).read_text()
        def methods(raw):
            return {node.name: node for node in ast.walk(ast.parse(raw))
                    if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}
        before, after = methods(before_raw), methods(after_raw)
        self.assertEqual((len(before), len(after)), (33, 35))
        self.assertTrue(set(before).issubset(after))
        self.assertEqual(set(after) - set(before), {'test_revision16_binds_actual_source_facts_and_preserves_manual_reviews', 'test_revision16_preserves_public_history_collectors_and_original_rejections'})
        frozen = {'test_revision15_binds_actual_source_facts_and_preserves_manual_reviews', 'test_revision15_preserves_public_history_collectors_and_original_rejections'}
        allowed = frozen | {'test_old_pins_reject_and_current_revision_passes_same_actual_facts'}
        for name in before:
            if name not in allowed:
                self.assertEqual(ast.dump(before[name], include_attributes=False), ast.dump(after[name], include_attributes=False))
                self.assertEqual(ast.get_source_segment(before_raw, before[name]), ast.get_source_segment(after_raw, after[name]))
        for name in frozen:
            self.assertEqual(len(after[name].body), 1)
            wrapper = after[name].body[0]
            self.assertIsInstance(wrapper, ast.With)
            self.assertEqual(ast.unparse(wrapper.items[0].context_expr), 'self.revision15_context()')
            self.assertEqual([ast.dump(node, include_attributes=False) for node in before[name].body],
                             [ast.dump(node, include_attributes=False) for node in wrapper.body])
            original_body = ''.join(before_raw.splitlines(keepends=True)[before[name].lineno:before[name].end_lineno])
            wrapped_body = ''.join(after_raw.splitlines(keepends=True)[after[name].lineno + 1:after[name].end_lineno])
            self.assertEqual(''.join(line[4:] if line.strip() else line for line in wrapped_body.splitlines(keepends=True)), original_body)
        current_name = 'test_old_pins_reject_and_current_revision_passes_same_actual_facts'
        expected_current = ast.get_source_segment(before_raw, before[current_name])
        for previous, replacement in (
            ("REVISION15_EXPECTED", "REVISION16_EXPECTED"),
            ("('revision14', self.rev14_tx, self.rev14_compat)]", "('revision14', self.rev14_tx, self.rev14_compat), ('revision15', self.rev15_tx, self.rev15_compat)]"),
            ("pin = REVISION14_SNAPSHOTS[name]", "pin = REVISION15_SNAPSHOTS[name]"),
            ("revision['revision'], 15", "revision['revision'], 16"),
            ("revision['previous_snapshot_commit'], REVISION14_COMMIT", "revision['previous_snapshot_commit'], REVISION15_COMMIT"),
        ):
            self.assertEqual(expected_current.count(previous), 1)
            expected_current = expected_current.replace(previous, replacement, 1)
        self.assertEqual(ast.get_source_segment(after_raw, after[current_name]), expected_current)
        guards = {name for name in before if name.startswith('test_unapproved_')}
        self.assertEqual(len(guards), 5)
        for name in guards:
            self.assertEqual(hashlib.sha256(ast.get_source_segment(after_raw, after[name]).encode()).hexdigest(),
                             review['original_unapproved_methods'][name]['source_sha256'])
            self.assertEqual(hashlib.sha256(ast.dump(after[name], include_attributes=False).encode()).hexdigest(),
                             review['original_unapproved_methods'][name]['ast_sha256'])
        # Fixed revision15 values and all earlier review constants are retained;
        # only the fresh root/source identifiers advance to revision16.
        def assignments(raw):
            return {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
                    for target in node.targets if isinstance(target, ast.Name)}
        old_values, new_values = assignments(before_raw), assignments(after_raw)
        self.assertEqual(len(old_values), 84)
        self.assertTrue(set(old_values).issubset(new_values))
        for name, value in old_values.items():
            if name not in {'CURRENT_EVIDENCE', 'SOURCE_COMMIT', 'SOURCE_TREE'}:
                self.assertEqual(ast.dump(value, include_attributes=False), ast.dump(new_values[name], include_attributes=False))
                self.assertEqual(ast.get_source_segment(before_raw, value), ast.get_source_segment(after_raw, new_values[name]))
'''



def require(condition, message):
    if not condition:
        raise ValueError(message)


def canonical(value):
    return (json.dumps(value, ensure_ascii=False, indent=2, allow_nan=False) + '\n').encode()


def digest(raw):
    return hashlib.sha256(raw).hexdigest()


def fingerprint(value):
    return digest(canonical(value))


def blob(raw):
    return hashlib.sha1(b'blob ' + str(len(raw)).encode() + b'\0' + raw).hexdigest()


def pin_bytes(path, raw):
    return dict(path=path, blob=blob(raw), sha256=digest(raw), bytes=len(raw))


def git(root, *args):
    return subprocess.check_output(['git', '-C', str(root), *args])


def git_text(root, *args):
    return git(root, *args).decode().strip()


def identity(root):
    return dict(commit=git_text(root, 'rev-parse', 'HEAD'),
                tree=git_text(root, 'rev-parse', 'HEAD^{tree}'),
                status=git(root, 'status', '--porcelain', '--untracked-files=all').decode())


def write_raw(root, path, raw):
    target = root / path
    target.parent.mkdir(parents=True, exist_ok=True)
    target.write_bytes(raw)


def load_module(path, name):
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


def exact_replace(raw, before, after):
    require(raw.count(before) == 1, 'non-unique author replacement: ' + before[:160])
    return raw.replace(before, after, 1)


def method_nodes(raw):
    return {node.name: node for node in ast.walk(ast.parse(raw))
            if isinstance(node, ast.FunctionDef) and node.name.startswith('test_')}


def assignments(raw):
    return {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign)
            for target in node.targets if isinstance(target, ast.Name)}


def full_method(raw, name):
    node = method_nodes(raw)[name]
    return ''.join(raw.splitlines(keepends=True)[node.lineno - 1:node.end_lineno])


def wrap_method(raw, name, context):
    node = method_nodes(raw)[name]
    lines = raw.splitlines(keepends=True)
    original = ''.join(lines[node.lineno - 1:node.end_lineno])
    wrapped = lines[node.lineno - 1] + '        with self.' + context + '():\n'
    wrapped += ''.join('    ' + line if line.strip() else line
                       for line in lines[node.lineno:node.end_lineno])
    return exact_replace(raw, original, wrapped)


def scaffold(base):
    require(digest(base.encode()) == BASE_TEST_SHA256, 'original reconciliation module mismatch')
    current = exact_replace(base, 'def revision14_snapshot(name):',
                            REVISION15_PINS_TEXT + REVISION15_SNAPSHOT_TEXT + 'def revision14_snapshot(name):')
    current = exact_replace(current, '        cls.ast = tx.extract()\n',
        "        cls.revision15 = {name: revision15_snapshot(name) for name in REVISION15_SNAPSHOTS}\n"
        "        cls.rev15_tx = cls.revision15['transaction']\n"
        "        cls.rev15_compat = cls.revision15['compatibility']\n"
        "        cls.ast = tx.extract()\n")
    current = exact_replace(current, '    def ' + CURRENT_METHOD + '(self):\n',
                            REVISION15_CONTEXT_TEXT + '    def ' + CURRENT_METHOD + '(self):\n')
    for name in FROZEN_METHODS:
        current = wrap_method(current, name, 'revision15_context')
    require(digest(current.encode()) == SCAFFOLD_SHA256, 'independently reviewed historical scaffold byte drift')
    verify_preservation(base, current, final=False)
    return current


def verify_preservation(before_raw, after_raw, *, final):
    before, after = method_nodes(before_raw), method_nodes(after_raw)
    require(len(before) == 33 and len(after) == (35 if final else 33), 'test method count drift')
    require(set(after) == set(before) | (NEW_METHODS if final else set()), 'test method set drift')
    exceptions = set(FROZEN_METHODS) | ({CURRENT_METHOD} if final else set())
    for name, node in before.items():
        if name not in exceptions:
            require(ast.dump(node, include_attributes=False) == ast.dump(after[name], include_attributes=False),
                    'old test AST changed: ' + name)
            require(ast.get_source_segment(before_raw, node) == ast.get_source_segment(after_raw, after[name]),
                    'old test bytes changed: ' + name)
    for name in FROZEN_METHODS:
        wrapped = after[name]
        require(len(wrapped.body) == 1 and isinstance(wrapped.body[0], ast.With), 'historical body not wrapped exactly')
        wrapper = wrapped.body[0]
        require(ast.unparse(wrapper.items[0].context_expr) == 'self.revision15_context()',
                'wrong historical context')
        require([ast.dump(node, include_attributes=False) for node in before[name].body] ==
                [ast.dump(node, include_attributes=False) for node in wrapper.body], 'historical AST changed')
        old_body = ''.join(before_raw.splitlines(keepends=True)[before[name].lineno:before[name].end_lineno])
        wrapped_body = ''.join(after_raw.splitlines(keepends=True)[wrapped.lineno + 1:wrapped.end_lineno])
        require(''.join(line[4:] if line.strip() else line for line in wrapped_body.splitlines(keepends=True)) == old_body,
                'historical body bytes changed')
    if final:
        expected = ast.get_source_segment(before_raw, before[CURRENT_METHOD])
        for old, new in CURRENT_REPLACEMENTS:
            expected = exact_replace(expected, old, new)
        require(ast.get_source_segment(after_raw, after[CURRENT_METHOD]) == expected,
                'unapproved current positive changes')
    old_values, new_values = assignments(before_raw), assignments(after_raw)
    require(len(old_values) == 84 and set(old_values).issubset(new_values), 'original assignment set drift')
    for name, value in old_values.items():
        if final and name in {'CURRENT_EVIDENCE', 'SOURCE_COMMIT', 'SOURCE_TREE'}:
            continue
        require(ast.dump(value, include_attributes=False) == ast.dump(new_values[name], include_attributes=False),
                'old assignment AST changed: ' + name)
        require(ast.get_source_segment(before_raw, value) == ast.get_source_segment(after_raw, new_values[name]),
                'old assignment value bytes changed: ' + name)
    guards = {name for name in before if name.startswith('test_unapproved_')}
    require(len(guards) == 5, 'original rejection guard count drift')
    return {name: dict(source_sha256=digest(ast.get_source_segment(before_raw, before[name]).encode()),
                       ast_sha256=digest(ast.dump(before[name], include_attributes=False).encode()))
            for name in sorted(guards)}


def record_command(root, output, label, argv):
    before = identity(root)
    started = time.monotonic()
    run = subprocess.run(argv, cwd=root, capture_output=True)
    elapsed = time.monotonic() - started
    write_raw(output, label + '.stdout', run.stdout)
    write_raw(output, label + '.stderr', run.stderr)
    write_raw(output, label + '.exit', (str(run.returncode) + '\n').encode())
    after = identity(root)
    require(before == after, 'source identity changed during ' + label)
    record = dict(label=label, argv=argv, exit_code=run.returncode,
                  elapsed_seconds=round(elapsed, 6), before=before, after=after,
                  stdout=dict(bytes=len(run.stdout), sha256=digest(run.stdout)),
                  stderr=dict(bytes=len(run.stderr), sha256=digest(run.stderr)))
    write_raw(output, label + '.receipt.json', canonical(record))
    print(json.dumps(dict(command=record), ensure_ascii=False), flush=True)
    return run, record


def derived_assertions(tx, function):
    return {
        'direct_sql_effects': [dict(line=item['line'], sql_fragment=item['value'])
                               for item in function['strings'] if tx.MUTATION.search(item['value'])],
        'direct_lock_fragments': [dict(line=item['line'], sql_fragment=item['value'])
                                  for item in function['strings'] if tx.LOCK.search(item['value'])],
        'transaction_helper_calls': [call for call in function['calls'] if call['name'] in tx.TX],
        'sql_execution_expressions': [call for call in function['calls'] if call['name'] in tx.SQL_CALLS],
    }


def callers(syntax, function):
    return [dict(caller_id=item['id'], file=item['file'], function=item['name'],
                 line=call['line'], expression=call['expr'], status='name-match-candidate-not-dispatch-proof')
            for item in syntax['functions'] for call in item['calls'] if call['name'] == function['name']]


def source_review(function, source, role, trace):
    return dict(base_commit=source, source_sha256=function['sha256'], role=role,
                trace=trace, unverified=SOURCE_BOUNDARY, review=README_PATH)


def local_operations(tx, function):
    return [dict(line=call['line'], operation=call['expr'], sql_expression=call.get('sql_expr', ''),
                 status='lexical-order-with-branches-not-single-total-runtime-order')
            for call in function['calls'] if call['name'] in tx.TX | tx.SQL_CALLS | {'Commit', 'Rollback'}]


def reviewed_manual_fields(tx, old, function, source):
    trace = (
        'TrimSpace(name) then reject empty, over-120-byte or NUL-bearing names before '
        'ValidateTemplate, JSON encoding or SQL. The accepted branch retains '
        'companyReferencedTx: tenant KEY SHARE then actor/profile authorization, '
        'new template INSERT or tenant/id/revision CAS UPDATE, required template.save '
        'audit and outer commit. The name check adds no database operation or lock.'
    )
    return dict(
        lock_fk_wait_fence=dict(boundary=trace, local_operations=local_operations(tx, function),
            direct_lock_fragments=derived_assertions(tx, function)['direct_lock_fragments'],
            implicit_fk='Existing template tenant and version/grant foreign-key definitions are unchanged; '
                        'unique template names can wait. Static syntax does not establish a global lock order.'),
        evidence=[f"{function['file']}:{function['line']}-{function['end']} sha256={function['sha256']}",
                  'Original syntax producers; bounded product regressions are recorded by their own EXEC source receipts.'],
        unverified_risks=[*old['unverified_risks'], SOURCE_BOUNDARY],
        file_family_context=trace,
        source_review=source_review(function, source, 'input-checked-template-transaction-writer', trace),
    )


def reviewed_seed_entry(tx, syntax, function, classification, source):
    trace = (
        'A single pool.Exec issues INSERT INTO system_settings (...) VALUES (...) '
        'ON CONFLICT (key) DO NOTHING. An existing key keeps value, description and '
        'updated_at verbatim. The function returns true only when RowsAffected() == 1; '
        'an existing key returns false and database errors propagate. There is no '
        'read-then-upsert window, explicit transaction, extra lock SQL or callback.'
    )
    return dict(
        id=function['id'], owner='postgres.PgStore/settings.go', entry='SeedSetting', group='CTX37',
        classification=classification, syntax=function, callers=callers(syntax, function),
        lock_fk_wait_fence=dict(boundary=trace, local_operations=local_operations(tx, function),
            direct_lock_fragments=[],
            implicit_fk='system_settings.key is the existing primary key; this table has no tenant foreign key. '
                        'The unique-key conflict may wait for another transaction. No row is overwritten on conflict.'),
        evidence=[f"{function['file']}:{function['line']}-{function['end']} sha256={function['sha256']}",
                  'Original syntax producers; actual PostgreSQL seed regressions remain separately source-bound.'],
        unverified_risks=[
            'Multiple setting keys are separate autocommit statements, not one atomic settings transaction.',
            'Cache refresh and concurrent administrative writes outside this single insertion require their own review.',
            SOURCE_BOUNDARY,
        ],
        review_status='static-type-review', followup_tasks=['R5-P1-030'],
        assertions=dict(derived_assertions(tx, function),
            effect_status='source-fragments-not-evaluated-SQL',
            callback_effect='No function or Validator parameter and no external callback; pool.Exec is the single database operation.',
            entry_role='store-exported-interface-candidate'),
        file_family_context=trace, evidence_level='source-only',
        source_review=source_review(function, source, 'insert-only-default-statement', trace),
    )


def reconcile_transactions(root, tx, previous, syntax, migrations, source):
    functions = {row['id']: row for row in syntax['functions'] if row['file'].startswith(tx.PG)}
    old = {row['id']: row for row in previous['entries']}
    classes = tx.classify(syntax)
    require(set(functions) - set(old) == {SEED_ID}, 'unreviewed added PostgreSQL functions')
    require(not (set(old) - set(functions)), 'unreviewed removed PostgreSQL functions')
    body_ids = {name for name in old if old[name]['syntax']['sha256'] != functions[name]['sha256']}
    require(body_ids == {SAVE_ID}, 'unreviewed existing PostgreSQL body change')
    require(all(old[name]['classification'] == classes[name] for name in old), 'unreviewed existing classification change')
    require('source_review' not in old[SAVE_ID], 'old SaveMailTemplate unexpectedly has a source review')
    require(migrations == previous['migrations'], 'unreviewed migration/FK/trigger change')
    files = [dict(path=row['path'], sha256=row['sha256']) for row in syntax['files'] if row['path'].startswith(tx.PG)]
    old_files = {row['path']: row['sha256'] for row in previous['postgres_files']}
    fresh_files = {row['path']: row['sha256'] for row in files}
    require(set(old_files) == set(fresh_files), 'unreviewed PostgreSQL file addition/removal')
    require({path for path in old_files if old_files[path] != fresh_files[path]} == set(REVIEWED_PRODUCT_FILES),
            'unreviewed PostgreSQL file content change')
    for path, expected in REVIEWED_PRODUCT_FILES.items():
        require(git_text(root, 'rev-parse', source + ':' + path) == expected, 'reviewed product blob mismatch: ' + path)
        require(blob((root / path).read_bytes()) == expected, 'reviewed product worktree mismatch: ' + path)
    current = copy.deepcopy(previous)
    manual = {SAVE_ID: reviewed_manual_fields(tx, old[SAVE_ID], functions[SAVE_ID], source)}
    result = []
    for before in previous['entries']:
        function = functions[before['id']]
        after = copy.deepcopy(before)
        after['syntax'] = function
        after['classification'] = classes[function['id']]
        after['callers'] = callers(syntax, function)
        after['assertions'].update(derived_assertions(tx, function))
        if function['id'] == SAVE_ID:
            require('historical_revision15_review' not in before, 'historical manual archive already exists')
            after['historical_revision15_review'] = {name: copy.deepcopy(before[name]) for name in MANUAL_FIELDS}
            after.update(manual[SAVE_ID])
        result.append(after)
    seed = reviewed_seed_entry(tx, syntax, functions[SEED_ID], classes[SEED_ID], source)
    require(seed['classification']['kind'] == 'direct-write' and seed['classification']['direct_write'] is True,
            'unexpected SeedSetting lexical classification')
    require(len(seed['assertions']['sql_execution_expressions']) == 1
            and seed['assertions']['sql_execution_expressions'][0]['name'] == 'Exec',
            'SeedSetting is not the reviewed single SQL execution')
    result.append(seed)
    current['entries'] = result
    current['postgres_files'] = files
    current['baseline_commit'] = source
    current['last_review_base_commit'] = source
    current['current_review_boundary'] = (
        'Revision 16 binds the actual public EXEC merge. SaveMailTemplate rejects NUL '
        'names before SQL while preserving its prior manual fields in historical_revision15_review; '
        'the old entry had no source_review and that absence is preserved in the archive. '
        'SeedSetting is a new insert-only default statement with no conflict overwrite. '
        'Other existing body hashes/classifications, manual fields and unknown fields stay unchanged; '
        'fresh syntax, exact derived assertions, name-match callers and finite compatibility '
        'facts are reconciled. Existing file-level reviews remain with additive revision16 notes. '
        + SOURCE_BOUNDARY
    )
    additions = {
        functions[SAVE_ID]['file']: dict(
            source_commit=source, source_sha256=fresh_files[functions[SAVE_ID]['file']],
            changed_functions=[SAVE_ID], added_functions=[],
            review=manual[SAVE_ID]['source_review'], qualification='source-only'),
        functions[SEED_ID]['file']: dict(
            source_commit=source, source_sha256=fresh_files[functions[SEED_ID]['file']],
            changed_functions=[], added_functions=[SEED_ID],
            review=seed['source_review'],
            note='ListSettings moves in this file without a function-body change; old manual fields are retained.',
            qualification='source-only'),
    }
    for path, note in additions.items():
        require('revision16_additions' not in current['reviewed_file_types'][path], 'existing revision16 file review')
        current['reviewed_file_types'][path]['revision16_additions'] = note
    current['inventory_revision'] = inventory_revision('transaction', previous['inventory_revision']['qualification'], source)
    return current, manual, additions


def inventory_revision(name, qualification, source):
    pin = SNAPSHOTS[name]
    return dict(revision=16, source_commit=source, previous_snapshot=pin['path'],
                previous_snapshot_commit=CATALOG_BASE, previous_snapshot_blob=pin['blob'],
                previous_sha256=pin['sha256'], qualification=qualification, review=README_PATH)


def reconcile_compatibility(gate, previous, routes, clients, source):
    # source_facts deliberately requires the exact newly generated clients.json.
    facts = gate.source_facts(routes, clients)
    old_routes = {row['route']: row for row in previous['routes']}
    require(set(old_routes) == {row['route'] for row in facts}, 'unreviewed route-set change')
    current = copy.deepcopy(previous)
    current['routes'] = [dict(old_routes[row['route']], **row) for row in facts]
    current['source_closure'] = gate.closure(facts, clients)
    current['acquisition'] = dict(previous['acquisition'], base_commit=source)
    current['inventory_revision'] = inventory_revision('compatibility', previous['inventory_revision']['qualification'], source)
    return current


def source_pin(root, commit, path):
    if not git(root, 'ls-tree', commit, '--', path):
        return dict(blob=None, sha256=None, bytes=None)
    raw = git(root, 'show', commit + ':' + path)
    return dict(blob=git_text(root, 'rev-parse', commit + ':' + path), sha256=digest(raw), bytes=len(raw))


def source_change(root, source, path):
    return dict(path=path, before_commit=PREVIOUS_SOURCE, after_commit=source,
                **{'before_' + key: value for key, value in source_pin(root, PREVIOUS_SOURCE, path).items()},
                **{'after_' + key: value for key, value in source_pin(root, source, path).items()})


def source_paths(root, commit, *roots):
    return git(root, 'ls-tree', '-r', '--name-only', commit, '--', *roots).decode().splitlines()


def exact_manifest(root, commit, paths):
    result = []
    for path in paths:
        raw = git(root, 'show', commit + ':' + path)
        require((root / path).read_bytes() == raw, 'historical or source bytes changed: ' + path)
        result.append(dict(path=path, blob=git_text(root, 'rev-parse', commit + ':' + path),
                           sha256=digest(raw), bytes=len(raw)))
    return result


def original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous):
    module = load_module(root / RECONCILIATION, 'r5_catalog16_original_tests')
    require(digest((root / RECONCILIATION).read_bytes()) == BASE_TEST_SHA256,
            'product source unexpectedly changed original catalog tests')
    old = [('revision1',
            json.loads((module.EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text()),
            json.loads((module.EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text()))]
    for number in range(2, 15):
        snapshot = getattr(module, 'revision%d_snapshot' % number)
        old.append(('revision%d' % number, snapshot('transaction'), snapshot('compatibility')))
    old.append(('revision15', previous['transaction'], previous['compatibility']))
    result = dict(transaction={}, compatibility={})
    for revision, transaction, compatibility in old:
        for kind, invoke in (
            ('transaction', lambda: tx.validate(transaction, syntax, migrations)),
            ('compatibility', lambda: gate.validate(compatibility, routes, clients)),
        ):
            try:
                invoke()
            except ValueError as error:
                result[kind][revision] = str(error)
            else:
                raise ValueError('historical catalog unexpectedly accepted current source: ' + kind + ' ' + revision)
    return result


def reconciliation(root, invocation, source, tree, previous, current, tx, gate, syntax,
                   migrations, routes, clients, manual, file_additions, baseline):
    old_tx, new_tx = previous['transaction'], current['transaction']
    old = {row['id']: row for row in old_tx['entries']}
    now = {row['id']: row for row in new_tx['entries']}
    report = dict(schema_version=1, inventory_revision=16,
        source_commit=source, source_tree=tree,
        previous_catalog_commit=CATALOG_BASE, previous_product_source_commit=PREVIOUS_SOURCE,
        issue='EXEC batch static catalog maintenance; zero additional implementation TODOs.',
        revision15_snapshots={name: dict(commit=CATALOG_BASE, **pin) for name, pin in SNAPSHOTS.items()},
        transaction=tx.validate(new_tx, syntax, migrations),
        compatibility=gate.validate(current['compatibility'], routes, clients),
        transaction_callers=[], transaction_body_changes=[], transaction_classification_changes=[],
        transaction_entry_changes=[], transaction_added_entries=[], transaction_removed_entries=[],
        migration_changes=[], manual_review_fields=manual, transaction_file_review_additions=file_additions,
        client_before_sha256=fingerprint(previous['clients']), client_after_sha256=fingerprint(clients),
        client_before=previous['clients'], client_after=clients,
        compatibility_route_changes=[], closure_changes=[],
    )
    for name, before in old.items():
        after = now[name]
        if before['callers'] != after['callers']:
            report['transaction_callers'].append(dict(id=name, before=before['callers'], after=after['callers'],
                before_sha256=fingerprint(before['callers']), after_sha256=fingerprint(after['callers'])))
        if before['syntax']['sha256'] != after['syntax']['sha256']:
            report['transaction_body_changes'].append(dict(id=name, before_sha256=before['syntax']['sha256'],
                                                         after_sha256=after['syntax']['sha256']))
        if before['classification'] != after['classification']:
            report['transaction_classification_changes'].append(dict(id=name, before=before['classification'], after=after['classification']))
        if before != after:
            report['transaction_entry_changes'].append(dict(id=name,
                changed_fields=[key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)],
                before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
    report['transaction_added_entries'] = [dict(id=name, entry_sha256=fingerprint(now[name])) for name in sorted(set(now) - set(old))]
    old_routes = {row['route']: row for row in previous['compatibility']['routes']}
    new_routes = {row['route']: row for row in current['compatibility']['routes']}
    for name, before in old_routes.items():
        after = new_routes[name]
        changed = [key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)]
        if changed:
            report['compatibility_route_changes'].append(dict(route=name, changed_fields=changed,
                before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
    old_closure, new_closure = previous['compatibility']['source_closure'], current['compatibility']['source_closure']
    report['closure_changes'] = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
        for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
    old_review_path = 'docs/company-mail/evidence/R5-CATALOG-REVISION15-20261009/reconciliation.json'
    old_review = json.loads(git(root, 'show', CATALOG_BASE + ':' + old_review_path))
    directories = [*old_review['historical_directories'], str(Path(old_review_path).parent)]
    historical = []
    for directory in directories:
        names = source_paths(root, CATALOG_BASE, directory)
        require(names, 'empty historical directory: ' + directory)
        actual = {path.relative_to(root).as_posix() for path in (root / directory).rglob('*') if path.is_file()}
        require(actual == set(names), 'historical directory membership drift: ' + directory)
        historical.extend(exact_manifest(root, CATALOG_BASE, names))
    report['historical_directories'] = directories
    report['historical_manifest'] = historical
    report['historical_manifest_sha256'] = fingerprint(historical)
    protected_paths = source_paths(root, source, *PROTECTED_ROOTS)
    require(set(protected_paths).isdisjoint(MAINTENANCE_PATHS), 'maintenance path unexpectedly present in product source')
    protected = exact_manifest(root, source, [path for path in protected_paths if path not in MUTABLE_TEST_PATHS])
    maintenance = {}
    for path in sorted(MAINTENANCE_PATHS):
        raw = git(invocation, 'show', 'HEAD:' + path)
        require((invocation / path).read_bytes() == raw, 'maintenance source working bytes differ: ' + path)
        maintenance[path] = pin_bytes(path, raw)
        protected.append(maintenance[path])
    report['protected_source_manifest'] = protected
    report['protected_source_manifest_sha256'] = fingerprint(protected)
    report['mutable_current_positive_test_paths'] = MUTABLE_TEST_PATHS
    report['unchanged_validators_and_collectors'] = ORIGINAL_PRODUCERS
    original = git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode()
    report['original_unapproved_methods'] = verify_preservation(original, scaffold(original), final=False)
    report.update(implementation_todos_completed=0, runtime_verified=False, product_green=False, task_complete=False,
                  parent_tasks=dict(accepted=10, total=171, remaining=161), baseline_validation=baseline)
    changes = git(root, 'diff', '--name-only', PREVIOUS_SOURCE, source, '--', 'internal', 'cmd', 'web').decode().splitlines()
    product = [path for path in changes if (path.endswith('.go') and not path.endswith('_test.go')) or
        (path.endswith(('.ts', '.tsx', '.css')) and '.test.' not in path) or
        (path.startswith('web/locales/') and path.endswith('.json'))]
    report['source_changes'] = [source_change(root, source, path) for path in product]
    report['excluded_closure_product_paths'] = [path for path in product if path not in new_closure]
    report['additional_build_metadata_changes'] = [
        source_change(root, source, path) for path in ('web/package.json', 'web/package-lock.json')
        if git(root, 'show', PREVIOUS_SOURCE + ':' + path) != git(root, 'show', source + ':' + path)]
    report['additional_api_schema_changes'] = [
        source_change(root, source, path) for path in ('internal/api/openapi.yaml',)
        if git(root, 'show', PREVIOUS_SOURCE + ':' + path) != git(root, 'show', source + ':' + path)]
    report['current_rejections'] = original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous)
    report['generated_catalogs'] = {name: dict(path=pin['path'],
        sha256=digest((root / pin['path']).read_bytes()), bytes=len((root / pin['path']).read_bytes()))
        for name, pin in SNAPSHOTS.items()}
    fields = [
        'transaction_callers', 'compatibility_route_changes', 'closure_changes', 'source_changes',
        'current_rejections', 'generated_catalogs', 'additional_api_schema_changes', 'additional_build_metadata_changes',
        'transaction_body_changes', 'transaction_classification_changes', 'transaction_entry_changes',
        'transaction_added_entries', 'transaction_removed_entries', 'manual_review_fields',
        'transaction_file_review_additions', 'baseline_validation', 'original_unapproved_methods',
    ]
    expected = dict(
        transaction=report['transaction'], compatibility=report['compatibility'],
        review_field_sha256={field: fingerprint(report[field]) for field in fields},
        historical_manifest_sha256=report['historical_manifest_sha256'],
        protected_source_manifest_sha256=report['protected_source_manifest_sha256'],
        maintenance_source_pins=maintenance, file_review_additions=file_additions,
        added_entries={name: now[name] for name in sorted(set(now) - set(old))},
        body_changes=report['transaction_body_changes'],
        classification_changes=report['transaction_classification_changes'],
        manual_review_fields=manual, product_paths=product,
    )
    report.update({field + '_sha256': value for field, value in expected['review_field_sha256'].items()})
    return report, expected


def candidate_test_sources(root, source, tree, expected):
    original = git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode()
    current = scaffold(original)
    current = exact_replace(current, '"""Frozen revision-1 through revision-14 reviews and current revision-15 facts."""',
                            '"""Frozen revision-1 through revision-15 reviews and current revision-16 facts."""')
    current = exact_replace(current,
        "CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION15-20261009'",
        "CURRENT_EVIDENCE = ROOT / '" + REVIEW_DIRECTORY + "'")
    current = exact_replace(current, "\nSOURCE_COMMIT = '" + PREVIOUS_SOURCE + "'\n", "\nSOURCE_COMMIT = '" + source + "'\n")
    current = exact_replace(current, "\nSOURCE_TREE = '" + PREVIOUS_TREE + "'\n", "\nSOURCE_TREE = '" + tree + "'\n")
    old_method = full_method(current, CURRENT_METHOD)
    next_method = old_method
    for before, after in CURRENT_REPLACEMENTS:
        next_method = exact_replace(next_method, before, after)
    current = exact_replace(current, old_method, next_method)
    current = exact_replace(current, 'def revision15_snapshot(name):',
        'REVISION16_EXPECTED = ' + pprint.pformat(expected, width=120, sort_dicts=False) + '\n\n\n'
        + 'def revision15_snapshot(name):')
    current = current.rstrip() + '\n\n' + CURRENT_TESTS_TEXT
    verify_preservation(original, current, final=True)
    compile(current, RECONCILIATION, 'exec')
    sources = {RECONCILIATION: current.encode()}
    for path, replacements in (
        ('scripts/tests/test_r5_transactions.py', [
            ("result['functions'], 404", "result['functions'], %d" % expected['transaction']['functions']),
            ("result['postgres_files'], 64", "result['postgres_files'], %d" % expected['transaction']['postgres_files'])]),
        ('scripts/tests/test_r5_compatibility.py', [
            ("result['routes'],133", "result['routes'],%d" % expected['compatibility']['routes']),
            ("result['client_branches'],136", "result['client_branches'],%d" % expected['compatibility']['client_branches'])]),
    ):
        raw = git(root, 'show', CATALOG_BASE + ':' + path).decode()
        for before, after in replacements:
            raw = exact_replace(raw, before, after)
        compile(raw, path, 'exec')
        sources[path] = raw.encode()
    return sources


def proposal_readme(source, tree, report):
    transaction, compatibility = report['transaction'], report['compatibility']
    return (
        '# R5 catalog revision 16: EXEC source reconciliation proposal\n\n'
        f'Product source: {source}; complete tree: {tree}.\n\n'
        f'Previous public catalog: {CATALOG_BASE}; previous product: {PREVIOUS_SOURCE}.\n\n'
        'This generated packet is an author proposal. The later committed maintenance candidate '
        'must pass the three unchanged CLIs, all five original modules with two new current '
        'positive tests, and the complete original source-version runner. No unobserved '
        'CI outcome is asserted here. Original source drift failures and all raw stdout/stderr '
        'remain in the proposal artifact and are separately source-bound.\n\n'
        'Maintenance completes 0 implementation TODOs. Parent acceptance stays 10/171, remaining 161. '
        'runtime_verified, product_green and task_complete stay false.\n\n'
        '## Original producer facts\n\n'
        f"- PostgreSQL files/functions: {transaction['postgres_files']}/{transaction['functions']}.\n"
        f"- SQL execution calls: {transaction['sql_execution_calls']}.\n"
        f"- Direct write/write closure functions: {transaction['direct_write_functions']}/{transaction['write_closure_functions']}.\n"
        f"- Migrations: {transaction['migration_files']}; routes: {compatibility['routes']}; "
        f"client branches: {compatibility['client_branches']}; finite closure files: {compatibility['source_files']}.\n\n"
        '## Manual source boundary\n\n'
        'SaveMailTemplate adds the NUL name check before template validation, JSON or SQL; '
        'its accepted transaction and revision CAS paths remain. Four prior manual fields '
        'are archived exactly. The old entry had no source_review, and its archive does not invent one.\n\n'
        'SeedSetting uses one INSERT ON CONFLICT(key) DO NOTHING statement, preserves all existing '
        'row fields on conflict, returns the inserted flag from RowsAffected, and propagates errors. '
        'It supplies no multi-key transaction or cache-consistency guarantee. Its exact new source '
        'review and derived syntax are independently pinned by the new current tests.\n\n'
        '## Historical and rejection preservation\n\n'
        'All 33 old test methods remain: 30 whole methods retain their bytes/AST, two revision-15 '
        'positive bodies run unchanged in the exact public historical clone, and one current '
        'positive has five explicitly checked current-pin/list changes. All five unapproved '
        'negative controls remain unchanged. Of 84 old assignments, only CURRENT_EVIDENCE, '
        'SOURCE_COMMIT and SOURCE_TREE advance; the other 81 values retain bytes and AST. '
        'Two new current methods bring this module to 35 and the five modules to the count '
        'actually reported by unittest. New maintenance source files have separate fixed '
        'Git blob, SHA256 and byte pins within the complete protected path set.\n\n'
        + SOURCE_BOUNDARY + '\n'
    ).encode()


def export_proposals(output, proposals):
    pins = {path: pin_bytes(path, raw) for path, raw in sorted(proposals.items())}
    for path, raw in sorted(proposals.items()):
        write_raw(output / 'files', path, raw)
    write_raw(output, 'proposal-files.json', canonical(pins))
    # This bounded, escaped format survives the GitHub decoded-log API. Each
    # complete UTF-8 file is reconstructable without an executor or ZIP decoder.
    # The artifact also retains every original file, source fact and raw log.
    print('CATALOG16_PROPOSAL_FILES ' + json.dumps(pins, ensure_ascii=True), flush=True)
    for path, raw in sorted(proposals.items()):
        text = raw.decode('utf-8')
        chunks = [text[index:index + 4096] for index in range(0, len(text), 4096)]
        for index, chunk in enumerate(chunks):
            record = dict(path=path, index=index, total=len(chunks), sha256=pins[path]['sha256'],
                          bytes=pins[path]['bytes'], text=chunk)
            print('CATALOG16_FILE_JSON ' + json.dumps(record, ensure_ascii=True), flush=True)


def run(invocation, output):
    invocation = invocation.resolve()
    output = output.resolve()
    require(not output.is_relative_to(invocation), 'author output must be outside the invocation source')
    require(not output.exists(), 'author output must be a new directory')
    output.mkdir(parents=True)
    invocation_before = identity(invocation)
    write_raw(output, 'invocation-before.json', canonical(invocation_before))
    require(not invocation_before['status'], 'invocation checkout is not clean')
    require(git_text(invocation, 'rev-parse', FINAL_SOURCE + '^{tree}') == FINAL_TREE, 'final product tree mismatch')
    require(git_text(invocation, 'rev-parse', CATALOG_BASE + '^{tree}') == CATALOG_TREE, 'public catalog tree mismatch')
    require(git_text(invocation, 'rev-parse', PREVIOUS_SOURCE + '^{tree}') == PREVIOUS_TREE, 'previous product tree mismatch')
    subprocess.run(['git', '-C', str(invocation), 'merge-base', '--is-ancestor', CATALOG_BASE, FINAL_SOURCE], check=True)
    subprocess.run(['git', '-C', str(invocation), 'merge-base', '--is-ancestor', FINAL_SOURCE, 'HEAD'], check=True)
    require(len(git_text(invocation, 'rev-list', '--parents', '-n', '1', FINAL_SOURCE).split()) == 3,
            'source is not the actual two-parent product merge')
    with tempfile.TemporaryDirectory(prefix='r5-catalog16-source-') as temporary:
        root = Path(temporary) / 'source'
        subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '--no-checkout', str(invocation), str(root)], check=True)
        subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', FINAL_SOURCE], check=True)
        require((root / '.git').is_dir() and not (root / '.git/objects/info/alternates').exists(),
                'ordinary independent Git object directory required')
        require((root / git_text(root, 'rev-parse', '--git-common-dir')).resolve() == (root / '.git').resolve(),
                'unexpected shared Git directory')
        source_before = identity(root)
        require(source_before == dict(commit=FINAL_SOURCE, tree=FINAL_TREE, status=''), 'product clone not exact/clean')
        write_raw(output, 'source-before.json', canonical(source_before))
        for path, expected in ORIGINAL_PRODUCERS.items():
            original = git(root, 'show', CATALOG_BASE + ':' + path)
            require(digest(original) == expected and (root / path).read_bytes() == original,
                    'original producer bytes changed: ' + path)
        previous = {}
        for name, pin in SNAPSHOTS.items():
            raw = git(root, 'show', CATALOG_BASE + ':' + pin['path'])
            require(pin_bytes(pin['path'], raw) == pin, 'public historical catalog pin mismatch: ' + name)
            require((root / pin['path']).read_bytes() == raw, 'product source no longer has frozen revision15 catalog: ' + name)
            previous[name] = json.loads(raw)
        require(digest((root / RECONCILIATION).read_bytes()) == BASE_TEST_SHA256, 'original product module drift')
        sys.path.insert(0, str(root / 'scripts'))
        import r5_source_runner_prepare as preparation
        prepared = preparation.prepare_typescript(root)
        write_raw(output, 'typescript-preparation.json', canonical(prepared))
        import check_r5_transactions as tx
        import check_r5_compatibility as gate
        require(Path(tx.__file__).resolve() == root / 'scripts/check_r5_transactions.py', 'wrong original transaction import')
        require(Path(gate.__file__).resolve() == root / 'scripts/check_r5_compatibility.py', 'wrong original compatibility import')
        require(tx.ROOT == root and gate.ROOT == root, 'original producer root mismatch')
        commands = [
            ('transactions', [sys.executable, '-B', 'scripts/check_r5_transactions.py']),
            ('compatibility', [sys.executable, '-B', 'scripts/check_r5_compatibility.py']),
            ('clients', ['node', 'scripts/collect_api_calls.cjs', '--check']),
        ]
        baseline_records, baseline_runs = [], []
        for label, argv in commands:
            run_result, record = record_command(root, output / 'baseline', label, argv)
            baseline_records.append(record)
            baseline_runs.append(run_result)
        # Preserve every original result before deciding whether it is genuine
        # catalog drift. Toolchain failures never count as source rejection.
        require(all(result.returncode == 1 for result in baseline_runs), 'expected original revision15 catalog drift was not observed')
        transaction_error = json.loads(baseline_runs[0].stdout)
        require(transaction_error.get('status') == 'FAIL'
                and 'postgres function set drift:' in transaction_error.get('error', '')
                and SEED_ID in transaction_error['error'],
                'transaction baseline did not reject the actual new function')
        compatibility_error = json.loads(baseline_runs[1].stdout)
        require(compatibility_error.get('status') == 'rejected'
                and compatibility_error.get('error') == 'client producer differs from reviewed current source inventory',
                'compatibility baseline did not reject the actual changed current client source')
        require(re.search(rb'Error: client (?:source|inventory count|route mapping) drift', baseline_runs[2].stderr),
                'client baseline did not reject source/count/route drift')
        syntax = tx.extract()
        migrations = tx.migration_inventory()
        routes, clients = gate.collect()
        for name, value in (('syntax', syntax), ('migrations', migrations), ('routes', routes), ('clients', clients)):
            write_raw(output / 'original-facts', name + '.json', canonical(value))
        source_after = identity(root)
        write_raw(output, 'source-after-original-collection.json', canonical(source_after))
        require(source_after == source_before, 'product source changed during original collection')
        baseline = dict(source=source_before,
            boundary='Three original CLIs in the exact clean final product clone with original revision15 catalog bytes.',
            commands=baseline_records)
        write_raw(output, 'baseline-validation.json', canonical(baseline))
        transaction, manual, file_additions = reconcile_transactions(root, tx, previous['transaction'],
                                                                     syntax, migrations, FINAL_SOURCE)
        # The exact current-client file is a required input of original
        # source_facts. Only this disposable generation clone is modified.
        write_raw(root, SNAPSHOTS['clients']['path'], canonical(clients))
        compatibility = reconcile_compatibility(gate, previous['compatibility'], routes, clients, FINAL_SOURCE)
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in routes}
        client_routes = [dict(row, routes=[] if row['forwarding'] else
            [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in clients]
        current = dict(transaction=transaction, compatibility=compatibility, clients=clients, client_routes=client_routes)
        for name, value in current.items():
            write_raw(root, SNAPSHOTS[name]['path'], canonical(value))
        changed = git(root, 'diff', '--name-only').decode().splitlines()
        require(set(changed) == {pin['path'] for pin in SNAPSHOTS.values()}, 'generation changed files outside the four current catalogs')
        report, expected = reconciliation(root, invocation, FINAL_SOURCE, FINAL_TREE, previous, current,
            tx, gate, syntax, migrations, routes, clients, manual, file_additions, baseline)
        proposals = {pin['path']: (root / pin['path']).read_bytes() for pin in SNAPSHOTS.values()}
        proposals[REVIEW_PATH] = canonical(report)
        proposals[README_PATH] = proposal_readme(FINAL_SOURCE, FINAL_TREE, report)
        proposals.update(candidate_test_sources(root, FINAL_SOURCE, FINAL_TREE, expected))
        write_raw(output, 'expected-current-pins.json', canonical(expected))
        write_raw(output, 'candidate-generation-source.json', canonical(identity(root)))
        export_proposals(output, proposals)
        # These results qualify only the explicitly dirty four-file generation
        # tree. They never replace checks on a later committed candidate.
        generated_records = []
        for label, argv in commands:
            run_result, record = record_command(root, output / 'generated', label, argv)
            generated_records.append(record)
        write_raw(output, 'generated-cli-validation.json', canonical(dict(
            source=identity(root), boundary='Dirty disposable product clone with exactly four proposed catalog files.',
            commands=generated_records)))
        require(all(record['exit_code'] == 0 for record in generated_records), 'original CLI rejects generated catalog proposals')
        require(set(git(root, 'diff', '--name-only').decode().splitlines()) == set(changed),
                'generated verification changed other tracked source files')
        for name, pin in SNAPSHOTS.items():
            require((root / pin['path']).read_bytes() == proposals[pin['path']], 'generated catalog changed during validation')
        invocation_after = identity(invocation)
        write_raw(output, 'invocation-after.json', canonical(invocation_after))
        require(invocation_after == invocation_before, 'invocation source changed')
        result = dict(status='proposals_generated_and_original_clis_passed',
            invocation=invocation_before, product_source=source_before,
            proposal_files={path: pin_bytes(path, raw) for path, raw in sorted(proposals.items())},
            generated_cli_exits=[record['exit_code'] for record in generated_records],
            catalog_module_methods=len(method_nodes(proposals[RECONCILIATION].decode())),
            committed_candidate_tests_executed=False,
            implementation_todos_completed=0, runtime_verified=False, product_green=False, task_complete=False)
        write_raw(output, 'result.json', canonical(result))
        print('CATALOG16_AUTHOR_RESULT ' + json.dumps(result, ensure_ascii=True), flush=True)
        return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path.cwd())
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    try:
        return run(args.root, args.output)
    except Exception as error:
        failure = dict(status='failed', error=repr(error), implementation_todos_completed=0,
                       runtime_verified=False, product_green=False, task_complete=False)
        if args.output.is_dir() and not args.output.resolve().is_relative_to(args.root.resolve()):
            write_raw(args.output, 'author-failure.json', canonical(failure))
        print('CATALOG16_AUTHOR_FAILURE ' + json.dumps(failure, ensure_ascii=True), flush=True)
        raise


if __name__ == '__main__':
    raise SystemExit(main())
