#!/usr/bin/env python3
"""Prepare revision-19 catalogs from exact public fixture source and original producers.

This maintenance records a lexical caller candidate and the existing protocol
fixture closure. It grants no runtime, release, parent TODO or CI qualification.
Historical snapshots, review bodies, negative controls and producer bytes remain.
"""
from __future__ import annotations

import argparse
import ast
import copy
import json
from pathlib import Path
import pprint
import re
import subprocess
import sys
import tempfile

import prepare_r5_catalog_revision17 as prior
from prepare_r5_catalog_revision17 import (
    assignments, blob, callers, canonical, derived_assertions, digest,
    exact_manifest, exact_replace, fingerprint, full_method, git, git_text,
    identity, load_module, method_nodes, pin_bytes,
    record_command, require, source_paths, source_pin, wrap_method, write_raw,
)

FINAL_SOURCE = '311593e6a837287f58d8f10d559dc0b8991be55e'
FINAL_TREE = '6ff0a4fca8116e1f4955e443f03007f7bf83c7bc'
CATALOG_BASE = 'af1444807e1018c95b963b8c5b74e2b2e5060b9a'
CATALOG_TREE = 'ddd5ae6fef29012af00642ae60fd31126ca32a74'
PREVIOUS_SOURCE = '177ee571b194836678cc9a473f178c655326ba7c'
PREVIOUS_TREE = '2d5d2c44680f6a6cb2b2d1166e2923806beafcc8'
RECONCILIATION = 'scripts/tests/test_r5_catalog_reconciliation.py'
BASE_TEST_SHA256 = '0686f1c5b5bd3764f8d04a4549aff27c173d2770de3b33bc5bfd1e0fc52b6ebc'
PRIOR_UTILITY = 'scripts/prepare_r5_catalog_revision18.py'
PRIOR_UTILITY_SHA256 = '1ab15802bd4252c6abcc26c592c4e7d94cf3bc4f6d3423dad3aaee8e46f8b716'
UTILITY = 'scripts/prepare_r5_catalog_revision19.py'
REVIEW_DIRECTORY = 'docs/company-mail/evidence/R5-CATALOG-REVISION19-20261012'
REVIEW_PATH = REVIEW_DIRECTORY + '/reconciliation.json'
README_PATH = REVIEW_DIRECTORY + '/README.md'
MUTABLE_TEST_PATHS = [RECONCILIATION]
PROTECTED_ROOTS = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
PRODUCT_ROOTS = ('internal', 'cmd', 'web', 'third_party')
BUILD_PATHS = ('go.mod', 'go.sum', 'web/package.json', 'web/package-lock.json')
SOURCE_BOUNDARY = prior.SOURCE_BOUNDARY
ORIGINAL_PRODUCERS = prior.ORIGINAL_PRODUCERS
MANUAL_FIELDS = prior.MANUAL_FIELDS
SNAPSHOTS = {
    'transaction': dict(path='docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json', blob='b88a287a63f6d5900ec18f1f8621089b26aa423c', sha256='4663cac24efe0ec255214f125bea6afc71df8e808b445cf5c6313419e7f07483', bytes=3009391),
    'compatibility': dict(path='docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json', blob='9662cf37febae2516ea690815d052e2b4ea6a9df', sha256='8e6083d08e32fcf382c66599c28e72aed091db9e3e6e62359509311d083377f7', bytes=412337),
    'clients': dict(path='docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json', blob='379f012dcbb48be2a5ff0903d9f4c19d0efb2528', sha256='d1308904b464dd1a2dcb04039f47a82c0d3526e8fa3f0b0413f0b0d6d5eac47a', bytes=41772),
    'client_routes': dict(path='docs/company-mail/evidence/R5-CLIENT-CALLS.json', blob='ef8b727980b55c9254e76ed4a4a3ecc7ae18d84c', sha256='3a3519aa7a59dcff9febde16bb770c784013eaaee614b4ab01825d266ca3fa94', bytes=50753),
}
SCAN = 'internal/store/postgres/outbound_receipts.go:outboundReceiptRow:Scan'
PROBE = 'internal/testpg/r5_loopback_probe.go::NewR5LoopbackProbe'
PROBE_FILE = 'internal/testpg/r5_loopback_probe.go'
PROTOCOL_FILE = 'internal/store/postgres/r5_protocol_shared_test.go'
VALIDATION_PATHS = [
    '.github/workflows/company-p0.yml',
    'internal/api/handlers/r5_external_batch_test.go',
    'internal/store/postgres/company_http_test.go',
    'internal/store/postgres/company_safety_contracts_test.go',
    PROTOCOL_FILE,
    'internal/testpg/r5_frontend_fixture_test.go',
    PROBE_FILE,
    'scripts/required_frontend_fixture_tests.json',
    'scripts/run_r5_frontend_fixture.py',
    'scripts/tests/test_r5_frontend_fixture.py',
    'scripts/tests/test_r5_frontend_fixture_source_inputs.py',
]
FROZEN_METHODS = (
    'test_revision18_binds_actual_source_facts_and_preserves_manual_reviews',
    'test_revision18_preserves_public_history_collectors_and_original_rejections',
)
NEW_METHODS = {name.replace('revision18', 'revision19') for name in FROZEN_METHODS}
CURRENT_METHOD = prior.CURRENT_METHOD
CURRENT_REPLACEMENTS = (
    ('REVISION18_EXPECTED', 'REVISION19_EXPECTED'),
    ("('revision17', self.rev17_tx, self.rev17_compat)]", "('revision17', self.rev17_tx, self.rev17_compat), ('revision18', self.rev18_tx, self.rev18_compat)]"),
    ('pin = REVISION17_SNAPSHOTS[name]', 'pin = REVISION18_SNAPSHOTS[name]'),
    ("revision['revision'], 18", "revision['revision'], 19"),
    ("revision['previous_snapshot_commit'], REVISION17_COMMIT", "revision['previous_snapshot_commit'], REVISION18_COMMIT"),
)


def inventory_revision(name, qualification):
    pin = SNAPSHOTS[name]
    return dict(revision=19, source_commit=FINAL_SOURCE, previous_snapshot=pin['path'],
        previous_snapshot_commit=CATALOG_BASE, previous_snapshot_blob=pin['blob'],
        previous_sha256=pin['sha256'], qualification=qualification, review=README_PATH)

def reconcile_transactions(root, tx, previous, syntax, migrations):
    functions = {f['id']: f for f in syntax['functions'] if f['file'].startswith(tx.PG)}
    old = {e['id']: e for e in previous['entries']}
    classes = tx.classify(syntax)
    require(set(functions) == set(old), 'unreviewed PostgreSQL function set change')
    require(migrations == previous['migrations'], 'unreviewed migration/FK/trigger change')
    files = [dict(path=f['path'], sha256=f['sha256']) for f in syntax['files'] if f['path'].startswith(tx.PG)]
    require(files == previous['postgres_files'], 'unreviewed PostgreSQL production file change')
    current = copy.deepcopy(previous)
    changed = []
    for entry in current['entries']:
        function = functions[entry['id']]
        require(entry['syntax'] == function, 'unreviewed PostgreSQL syntax change: ' + entry['id'])
        require(entry['classification'] == classes[entry['id']], 'unreviewed classification change: ' + entry['id'])
        require(all(entry['assertions'][key] == value for key, value in derived_assertions(tx, function).items()),
                'unreviewed derived assertion change: ' + entry['id'])
        actual = callers(syntax, function)
        if entry['callers'] != actual:
            changed.append(entry['id'])
            require(entry['id'] == SCAN, 'unreviewed caller delta')
            added = [row for row in actual if row not in entry['callers']]
            require(len(added) == 1 and all(row in actual for row in entry['callers']), 'unreviewed caller removal/multiplicity')
            require(added[0]['caller_id'] == PROBE and added[0]['file'] == PROBE_FILE
                    and added[0]['expression'] == 'pool.QueryRow(r.Context(), "SELECT identity FROM r5_batch_probe").Scan'
                    and added[0]['status'] == 'name-match-candidate-not-dispatch-proof', 'unreviewed lexical Scan candidate')
            entry['callers'] = actual
    require(changed == [SCAN], 'expected owned fixture Scan candidate absent')
    current.update(baseline_commit=FINAL_SOURCE, last_review_base_commit=FINAL_SOURCE,
        current_review_boundary='Revision 19 records exactly one lexical Scan name-match candidate in the owned loopback fixture. '
        'This is not a dispatch edge to outboundReceiptRow.Scan. PostgreSQL production syntax, classifications, '
        'assertions and all manual/file reviews remain byte-equivalent as JSON values. ' + SOURCE_BOUNDARY,
        inventory_revision=inventory_revision('transaction', previous['inventory_revision']['qualification']))
    return current, {}, {}, {}


def verify_preservation(before_raw, after_raw, *, final):
    before, after = method_nodes(before_raw), method_nodes(after_raw)
    require(len(before) == 39 and len(after) == (41 if final else 39), 'test count drift')
    require(set(after) == set(before) | (NEW_METHODS if final else set()), 'test method set drift')
    exceptions = set(FROZEN_METHODS) | ({CURRENT_METHOD} if final else set())
    for name, node in before.items():
        if name not in exceptions:
            require(ast.dump(node, include_attributes=False) == ast.dump(after[name], include_attributes=False)
                and ast.get_source_segment(before_raw, node) == ast.get_source_segment(after_raw, after[name]), 'old test changed: ' + name)
    for name in FROZEN_METHODS:
        node = after[name]
        require(len(node.body) == 1 and isinstance(node.body[0], ast.With), 'historical wrapper changed')
        wrapper = node.body[0]
        require(ast.unparse(wrapper.items[0].context_expr) == 'self.revision18_context()', 'wrong historical context')
        require([ast.dump(n, include_attributes=False) for n in before[name].body]
                == [ast.dump(n, include_attributes=False) for n in wrapper.body], 'historical AST changed')
        old_body = ''.join(before_raw.splitlines(keepends=True)[before[name].lineno:before[name].end_lineno])
        body = ''.join(after_raw.splitlines(keepends=True)[node.lineno + 1:node.end_lineno])
        require(''.join(line[4:] if line.strip() else line for line in body.splitlines(keepends=True)) == old_body, 'historical body bytes changed')
    if final:
        expected = ast.get_source_segment(before_raw, before[CURRENT_METHOD])
        for old, new in CURRENT_REPLACEMENTS:
            expected = exact_replace(expected, old, new)
        require(ast.get_source_segment(after_raw, after[CURRENT_METHOD]) == expected, 'unapproved current positive change')
    old_values, new_values = assignments(before_raw), assignments(after_raw)
    require(len(old_values) == 102 and set(old_values).issubset(new_values), 'old assignment set drift')
    for name, value in old_values.items():
        if final and name in {'CURRENT_EVIDENCE', 'SOURCE_COMMIT', 'SOURCE_TREE'}:
            continue
        require(ast.dump(value, include_attributes=False) == ast.dump(new_values[name], include_attributes=False)
                and ast.get_source_segment(before_raw, value) == ast.get_source_segment(after_raw, new_values[name]), 'old assignment changed: ' + name)
    guards = {name for name in before if name.startswith('test_unapproved_')}
    require(len(guards) == 5, 'original negative guard count changed')
    return {name: dict(source_sha256=digest(ast.get_source_segment(before_raw, before[name]).encode()),
        ast_sha256=digest(ast.dump(before[name], include_attributes=False).encode())) for name in sorted(guards)}

def scaffold(original):
    require(digest(original.encode()) == BASE_TEST_SHA256, 'public revision18 test source mismatch')
    pins = '\n'.join(name + ' = ' + pprint.pformat(value, width=120, sort_dicts=False) for name, value in (
        ('REVISION18_COMMIT', CATALOG_BASE), ('REVISION18_TREE', CATALOG_TREE),
        ('REVISION18_SOURCE_COMMIT', PREVIOUS_SOURCE), ('REVISION18_SOURCE_TREE', PREVIOUS_TREE),
        ('REVISION18_SNAPSHOTS', SNAPSHOTS))) + '\n\n\n'
    advance = lambda text: text.replace('revision16', 'revision18').replace('REVISION16', 'REVISION18').replace('rev16', 'rev18').replace('REVISION18-20261009', 'REVISION18-20261010')
    current = exact_replace(original, 'def revision17_snapshot(name):', pins + advance(prior.REVISION16_SNAPSHOT_TEXT) + 'def revision17_snapshot(name):')
    current = exact_replace(current, '        cls.ast = tx.extract()\n',
        "        cls.revision18 = {name: revision18_snapshot(name) for name in REVISION18_SNAPSHOTS}\n"
        "        cls.rev18_tx = cls.revision18['transaction']\n        cls.rev18_compat = cls.revision18['compatibility']\n        cls.ast = tx.extract()\n")
    current = exact_replace(current, '    def ' + CURRENT_METHOD + '(self):\n', advance(prior.REVISION16_CONTEXT_TEXT) + '    def ' + CURRENT_METHOD + '(self):\n')
    for name in FROZEN_METHODS:
        current = wrap_method(current, name, 'revision18_context')
    verify_preservation(original, current, final=False)
    return current

CURRENT_FACTS_TEST = r'''    def test_revision19_binds_actual_source_facts_and_preserves_manual_reviews(self):
        review = json.loads((CURRENT_EVIDENCE / 'reconciliation.json').read_text())
        fingerprint = lambda value: hashlib.sha256(gate.canonical_bytes(value)).hexdigest()
        self.assertEqual(review['inventory_revision'], 19)
        self.assertEqual(review['source_commit'], SOURCE_COMMIT)
        self.assertEqual(review['source_tree'], SOURCE_TREE)
        self.assertEqual(git('rev-parse', SOURCE_COMMIT + '^{tree}').decode().strip(), SOURCE_TREE)
        self.assertEqual(review['previous_catalog_commit'], REVISION18_COMMIT)
        self.assertEqual(review['previous_product_source_commit'], REVISION18_SOURCE_COMMIT)
        self.assertEqual(review['revision18_snapshots'], {name: dict(commit=REVISION18_COMMIT, **pin) for name, pin in REVISION18_SNAPSHOTS.items()})
        for field, expected in REVISION19_EXPECTED['review_field_sha256'].items():
            self.assertEqual(fingerprint(review[field]), expected)
        self.assertEqual(tx.validate(self.tx, self.ast, self.migrations), review['transaction'])
        self.assertEqual(gate.validate(self.compat, self.routes, self.clients), review['compatibility'])
        self.assertEqual(self.tx['postgres_files'], self.rev18_tx['postgres_files'])
        self.assertEqual(self.tx['reviewed_file_types'], self.rev18_tx['reviewed_file_types'])
        self.assertEqual(self.tx['migrations'], self.rev18_tx['migrations'])
        self.assertEqual(self.migrations, self.rev18_tx['migrations'])
        mutable_tx = {'inventory_revision', 'baseline_commit', 'last_review_base_commit', 'current_review_boundary', 'entries'}
        self.assertEqual({k: v for k, v in self.tx.items() if k not in mutable_tx},
                         {k: v for k, v in self.rev18_tx.items() if k not in mutable_tx})
        self.assertEqual(self.tx['baseline_commit'], SOURCE_COMMIT)
        self.assertEqual(self.tx['last_review_base_commit'], SOURCE_COMMIT)
        functions = {row['id']: row for row in self.ast['functions'] if row['file'].startswith(tx.PG)}
        classes = tx.classify(self.ast)
        old = {row['id']: row for row in self.rev18_tx['entries']}
        current = {row['id']: row for row in self.tx['entries']}
        self.assertEqual(set(old), set(current))
        self.assertEqual(set(current), set(functions))
        def actual_callers(function):
            return [dict(caller_id=item['id'], file=item['file'], function=item['name'],
                line=call['line'], expression=call['expr'], status='name-match-candidate-not-dispatch-proof')
                for item in self.ast['functions'] for call in item['calls'] if call['name'] == function['name']]
        caller_changes, entry_changes = [], []
        for identity, before in old.items():
            after, function = current[identity], functions[identity]
            self.assertEqual(before['syntax'], function)
            self.assertEqual(after['syntax'], function)
            self.assertEqual(before['classification'], classes[identity])
            self.assertEqual(after['classification'], classes[identity])
            self.assertEqual({k: v for k, v in before.items() if k != 'callers'},
                             {k: v for k, v in after.items() if k != 'callers'})
            self.assertEqual(after['callers'], actual_callers(function))
            if before['callers'] != after['callers']:
                caller_changes.append(dict(id=identity, before=before['callers'], after=after['callers'],
                    before_sha256=fingerprint(before['callers']), after_sha256=fingerprint(after['callers'])))
                entry_changes.append(dict(id=identity, changed_fields=['callers'], before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
        self.assertEqual(caller_changes, review['transaction_callers'])
        self.assertEqual(entry_changes, review['transaction_entry_changes'])
        self.assertEqual([row['id'] for row in caller_changes], ['internal/store/postgres/outbound_receipts.go:outboundReceiptRow:Scan'])
        delta = caller_changes[0]
        self.assertTrue(all(row in delta['after'] for row in delta['before']))
        added = [row for row in delta['after'] if row not in delta['before']]
        self.assertEqual(len(added), 1)
        self.assertEqual(added[0]['caller_id'], 'internal/testpg/r5_loopback_probe.go::NewR5LoopbackProbe')
        self.assertEqual(added[0]['file'], 'internal/testpg/r5_loopback_probe.go')
        self.assertEqual(added[0]['expression'], 'pool.QueryRow(r.Context(), "SELECT identity FROM r5_batch_probe").Scan')
        self.assertEqual(added[0]['status'], 'name-match-candidate-not-dispatch-proof')
        for field in ('transaction_body_changes', 'transaction_classification_changes', 'transaction_added_entries',
                      'transaction_removed_entries', 'migration_changes', 'compatibility_route_changes'):
            self.assertEqual(review[field], [])
        for field in ('manual_review_fields', 'transaction_file_review_additions', 'transaction_new_file_reviews'):
            self.assertEqual(review[field], {})
        self.assertEqual(self.clients, self.revision18['clients'])
        self.assertEqual(review['client_before'], self.clients)
        self.assertEqual(review['client_after'], self.clients)
        self.assertEqual(review['client_before_sha256'], fingerprint(self.clients))
        self.assertEqual(review['client_after_sha256'], fingerprint(self.clients))
        for name in ('clients', 'client_routes'):
            pin = REVISION18_SNAPSHOTS[name]
            raw = (ROOT / pin['path']).read_bytes()
            self.assertEqual(raw, git('show', REVISION18_COMMIT + ':' + pin['path']))
            self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
        self.assertEqual(self.compat['routes'], self.rev18_compat['routes'])
        fresh = {row['route']: row for row in gate.source_facts(self.routes, self.clients)}
        self.assertEqual(set(fresh), {row['route'] for row in self.compat['routes']})
        for row in self.compat['routes']:
            self.assertEqual({key: row[key] for key in fresh[row['route']]}, fresh[row['route']])
        old_closure, new_closure = self.rev18_compat['source_closure'], self.compat['source_closure']
        self.assertEqual(set(old_closure), set(new_closure))
        self.assertEqual(new_closure, gate.closure(list(fresh.values()), self.clients))
        closure_changes = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
                           for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
        self.assertEqual(closure_changes, review['closure_changes'])
        self.assertEqual([row['path'] for row in closure_changes], ['internal/store/postgres/r5_protocol_shared_test.go'])
        mutable_compat = {'inventory_revision', 'acquisition', 'source_closure'}
        self.assertEqual({k: v for k, v in self.compat.items() if k not in mutable_compat},
                         {k: v for k, v in self.rev18_compat.items() if k not in mutable_compat})
        self.assertEqual(self.compat['acquisition'], dict(self.rev18_compat['acquisition'], base_commit=SOURCE_COMMIT))
        for name, pin in review['generated_catalogs'].items():
            self.assertEqual(pin['path'], REVISION18_SNAPSHOTS[name]['path'])
            raw = (ROOT / pin['path']).read_bytes()
            self.assertEqual(hashlib.sha256(raw).hexdigest(), pin['sha256'])
            self.assertEqual(len(raw), pin['bytes'])
        self.assertEqual(set(review['generated_catalogs']), set(REVISION18_SNAPSHOTS))
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in self.routes}
        documented = json.loads((ROOT / REVISION18_SNAPSHOTS['client_routes']['path']).read_text())
        self.assertEqual(documented, [dict(row, routes=[] if row['forwarding'] else
                         [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in self.clients])
        changed = git('diff', '--name-only', REVISION18_SOURCE_COMMIT, SOURCE_COMMIT, '--', 'internal', 'cmd', 'web', 'third_party').decode().splitlines()
        product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or
                         (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or
                         (p.startswith('web/locales/') and p.endswith('.json'))]
        self.assertEqual(product_paths, [row['path'] for row in review['source_changes']])
        self.assertEqual(product_paths, REVISION19_EXPECTED['product_paths'])
        self.assertEqual(review['excluded_closure_product_paths'], [path for path in product_paths if path not in new_closure])
        build_paths = [path for path in ('go.mod', 'go.sum', 'web/package.json', 'web/package-lock.json')
                       if git('show', REVISION18_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(build_paths, [row['path'] for row in review['additional_build_metadata_changes']])
        schema_paths = [path for path in ('internal/api/openapi.yaml',)
                        if git('show', REVISION18_SOURCE_COMMIT + ':' + path) != git('show', SOURCE_COMMIT + ':' + path)]
        self.assertEqual(schema_paths, [row['path'] for row in review['additional_api_schema_changes']])
        validation_paths = ['.github/workflows/company-p0.yml', 'internal/api/handlers/r5_external_batch_test.go',
            'internal/store/postgres/company_http_test.go', 'internal/store/postgres/company_safety_contracts_test.go',
            'internal/store/postgres/r5_protocol_shared_test.go', 'internal/testpg/r5_frontend_fixture_test.go',
            'internal/testpg/r5_loopback_probe.go', 'scripts/required_frontend_fixture_tests.json',
            'scripts/run_r5_frontend_fixture.py', 'scripts/tests/test_r5_frontend_fixture.py',
            'scripts/tests/test_r5_frontend_fixture_source_inputs.py']
        self.assertEqual(git('diff', '--name-only', REVISION18_COMMIT, SOURCE_COMMIT).decode().splitlines(), validation_paths)
        self.assertEqual([row['path'] for row in review['validation_input_changes']], validation_paths)
        for records, previous in ((review['source_changes'] + review['additional_build_metadata_changes'] + review['additional_api_schema_changes'], REVISION18_SOURCE_COMMIT),
                                  (review['validation_input_changes'], REVISION18_COMMIT)):
            for row in records:
                self.assertEqual(row['before_commit'], previous)
                self.assertEqual(row['after_commit'], SOURCE_COMMIT)
                for prefix, commit in (('before', previous), ('after', SOURCE_COMMIT)):
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
        baseline = review['baseline_validation']
        self.assertEqual(baseline['source'], dict(commit=SOURCE_COMMIT, tree=SOURCE_TREE, status=''))
        self.assertEqual([(row['label'], row['exit_code']) for row in baseline['commands']], [('transactions', 1), ('compatibility', 1), ('clients', 0)])
        for row in baseline['commands']:
            self.assertEqual(row['before'], baseline['source'])
            self.assertEqual(row['after'], baseline['source'])
        self.assertFalse(review['runtime_verified'])
        self.assertFalse(review['product_green'])
        self.assertFalse(review['task_complete'])
        self.assertEqual(review['implementation_todos_completed'], 0)
        self.assertEqual(review['parent_tasks'], dict(accepted=10, total=171, remaining=161))
'''


def current_test_template(original):
    # Reuse the previous preservation audit as an independent current audit.
    # Historical bodies in scaffold are never replaced by this derived copy.
    text = full_method(original, FROZEN_METHODS[1])
    for prefix in ('REVISION', 'revision', 'rev'):
        text = re.sub(prefix + r'(16|17|18)(?!\d)', lambda m: prefix + str(int(m[1]) + 1), text)
    text = exact_replace(text,
        '("revision[\'revision\'], 17", "revision[\'revision\'], 18")',
        '("revision[\'revision\'], 18", "revision[\'revision\'], 19")')
    text = exact_replace(text, "mutable = ['scripts/tests/test_r5_catalog_reconciliation.py', 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']",
                         "mutable = ['scripts/tests/test_r5_catalog_reconciliation.py']")
    start = text.index('        for path, replacements in (')
    end = text.index("        path = 'scripts/tests/test_r5_catalog_reconciliation.py'", start)
    text = text[:start] + "        for path in ('scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py'):\n            self.assertEqual((ROOT / path).read_bytes(), git('show', REVISION18_COMMIT + ':' + path))\n" + text[end:]
    for before, after in [('(len(before), len(after)), (37, 39)', '(len(before), len(after)), (39, 41)'), ('len(old_values), 96', 'len(old_values), 102')]:
        text = exact_replace(text, before, after)
    return CURRENT_FACTS_TEST + '\n' + text


def candidate_test_sources(root, expected):
    original = git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode()
    current = scaffold(original)
    current = exact_replace(current,
        '"""Frozen revision-1 through revision-17 reviews and current revision-18 facts."""',
        '"""Frozen revision-1 through revision-18 reviews and current revision-19 facts."""')
    current = exact_replace(current,
        "CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION18-20261010'",
        "CURRENT_EVIDENCE = ROOT / '" + REVIEW_DIRECTORY + "'")
    current = exact_replace(current, "\nSOURCE_COMMIT = '" + PREVIOUS_SOURCE + "'\n", "\nSOURCE_COMMIT = '" + FINAL_SOURCE + "'\n")
    current = exact_replace(current, "\nSOURCE_TREE = '" + PREVIOUS_TREE + "'\n", "\nSOURCE_TREE = '" + FINAL_TREE + "'\n")
    old = full_method(current, CURRENT_METHOD)
    replacement = old
    for before, after in CURRENT_REPLACEMENTS:
        replacement = exact_replace(replacement, before, after)
    current = exact_replace(current, old, replacement)
    current = exact_replace(current, 'def revision18_snapshot(name):',
        'REVISION19_EXPECTED = ' + pprint.pformat(expected, width=120, sort_dicts=False) + '\n\n\ndef revision18_snapshot(name):')
    current = current.rstrip() + '\n\n' + current_test_template(original)
    verify_preservation(original, current, final=True)
    compile(current, RECONCILIATION, 'exec')
    return {RECONCILIATION: current.encode()}


def source_change(root, path, before=PREVIOUS_SOURCE):
    return dict(path=path, before_commit=before, after_commit=FINAL_SOURCE,
        **{'before_' + key: value for key, value in source_pin(root, before, path).items()},
        **{'after_' + key: value for key, value in source_pin(root, FINAL_SOURCE, path).items()})

def original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous):
    require(digest((root / RECONCILIATION).read_bytes()) == BASE_TEST_SHA256, 'original rejection module drift')
    module = load_module(root / RECONCILIATION, 'r5_catalog19_original_tests')
    old = [('revision1', json.loads((module.EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text()),
        json.loads((module.EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text()))]
    for number in range(2, 18):
        snapshot = getattr(module, 'revision%d_snapshot' % number)
        old.append(('revision%d' % number, snapshot('transaction'), snapshot('compatibility')))
    old.append(('revision18', previous['transaction'], previous['compatibility']))
    result = dict(transaction={}, compatibility={})
    for revision, transaction, compatibility in old:
        for name, invoke in (('transaction', lambda: tx.validate(transaction, syntax, migrations)),
                             ('compatibility', lambda: gate.validate(compatibility, routes, clients))):
            try:
                invoke()
            except ValueError as error:
                result[name][revision] = str(error)
            else:
                raise ValueError('historical catalog unexpectedly accepted current facts: ' + name + ' ' + revision)
    return result

def reconciliation(root, invocation, previous, current, tx, gate, syntax, migrations, routes, clients,
                   manual, file_additions, new_file_reviews, baseline):
    require(git(root, 'diff', '--name-only', CATALOG_BASE, FINAL_SOURCE).decode().splitlines() == VALIDATION_PATHS, 'unreviewed validation source boundary')
    old = {entry['id']: entry for entry in previous['transaction']['entries']}
    now = {entry['id']: entry for entry in current['transaction']['entries']}
    report = dict(schema_version=1, inventory_revision=19, source_commit=FINAL_SOURCE, source_tree=FINAL_TREE,
        previous_catalog_commit=CATALOG_BASE, previous_product_source_commit=PREVIOUS_SOURCE,
        issue='Owned frontend/PG fixture static source catalog maintenance; zero additional implementation TODOs.',
        revision18_snapshots={name: dict(commit=CATALOG_BASE, **pin) for name, pin in SNAPSHOTS.items()},
        transaction=tx.validate(current['transaction'], syntax, migrations),
        compatibility=gate.validate(current['compatibility'], routes, clients),
        transaction_callers=[], transaction_body_changes=[], transaction_classification_changes=[],
        transaction_entry_changes=[], transaction_added_entries=[], transaction_removed_entries=[], migration_changes=[],
        manual_review_fields=manual, transaction_file_review_additions=file_additions, transaction_new_file_reviews=new_file_reviews,
        client_before_sha256=fingerprint(previous['clients']), client_after_sha256=fingerprint(clients),
        client_before=previous['clients'], client_after=clients, compatibility_route_changes=[], closure_changes=[])
    for name, before in old.items():
        after = now[name]
        if before['callers'] != after['callers']:
            report['transaction_callers'].append(dict(id=name, before=before['callers'], after=after['callers'],
                before_sha256=fingerprint(before['callers']), after_sha256=fingerprint(after['callers'])))
        if before['syntax']['sha256'] != after['syntax']['sha256']:
            report['transaction_body_changes'].append(dict(id=name, before_sha256=before['syntax']['sha256'], after_sha256=after['syntax']['sha256']))
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
        if before != after:
            report['compatibility_route_changes'].append(dict(route=name,
                changed_fields=[key for key in sorted(set(before) | set(after)) if before.get(key) != after.get(key)],
                before_sha256=fingerprint(before), after_sha256=fingerprint(after)))
    old_closure, new_closure = previous['compatibility']['source_closure'], current['compatibility']['source_closure']
    report['closure_changes'] = [dict(path=path, before_sha256=old_closure.get(path), after_sha256=new_closure.get(path))
        for path in sorted(set(old_closure) | set(new_closure)) if old_closure.get(path) != new_closure.get(path)]
    require(report['closure_changes'] and [row['path'] for row in report['closure_changes']] == [PROTOCOL_FILE], 'unreviewed compatibility closure drift')
    require(not report['compatibility_route_changes'] and clients == previous['clients'], 'unreviewed route/client source change')
    old_review_path = 'docs/company-mail/evidence/R5-CATALOG-REVISION18-20261010/reconciliation.json'
    old_review = json.loads(git(root, 'show', CATALOG_BASE + ':' + old_review_path))
    directories = [*old_review['historical_directories'], str(Path(old_review_path).parent)]
    historical = []
    for directory in directories:
        paths = source_paths(root, CATALOG_BASE, directory)
        require(paths and {p.relative_to(root).as_posix() for p in (root / directory).rglob('*') if p.is_file()} == set(paths), 'historical directory membership drift: ' + directory)
        historical.extend(exact_manifest(root, CATALOG_BASE, paths))
    protected_paths = source_paths(root, FINAL_SOURCE, *PROTECTED_ROOTS)
    require(UTILITY not in protected_paths, 'new maintenance utility already in product source')
    protected = exact_manifest(root, FINAL_SOURCE, [p for p in protected_paths if p not in MUTABLE_TEST_PATHS])
    raw = git(invocation, 'show', 'HEAD:' + UTILITY)
    require((invocation / UTILITY).read_bytes() == raw, 'uncommitted author utility')
    maintenance = {UTILITY: pin_bytes(UTILITY, raw)}
    protected.append(maintenance[UTILITY])
    changes = git(root, 'diff', '--name-only', PREVIOUS_SOURCE, FINAL_SOURCE, '--', *PRODUCT_ROOTS).decode().splitlines()
    product = [p for p in changes if (p.endswith('.go') and not p.endswith('_test.go')) or
        (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or (p.startswith('web/locales/') and p.endswith('.json'))]
    report.update(historical_directories=directories, historical_manifest=historical,
        historical_manifest_sha256=fingerprint(historical), protected_source_manifest=protected,
        protected_source_manifest_sha256=fingerprint(protected), mutable_current_positive_test_paths=MUTABLE_TEST_PATHS,
        unchanged_validators_and_collectors=ORIGINAL_PRODUCERS,
        original_unapproved_methods=verify_preservation(git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode(),
            scaffold(git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode()), final=False),
        implementation_todos_completed=0, runtime_verified=False, product_green=False, task_complete=False,
        parent_tasks=dict(accepted=10, total=171, remaining=161), baseline_validation=baseline,
        source_changes=[source_change(root, p) for p in product],
        validation_input_changes=[source_change(root, p, before=CATALOG_BASE) for p in VALIDATION_PATHS],
        excluded_closure_product_paths=[p for p in product if p not in new_closure],
        additional_build_metadata_changes=[source_change(root, p) for p in BUILD_PATHS if git(root, 'show', PREVIOUS_SOURCE + ':' + p) != git(root, 'show', FINAL_SOURCE + ':' + p)],
        additional_api_schema_changes=[source_change(root, p) for p in ('internal/api/openapi.yaml',) if git(root, 'show', PREVIOUS_SOURCE + ':' + p) != git(root, 'show', FINAL_SOURCE + ':' + p)],
        current_rejections=original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous),
        generated_catalogs={name: dict(path=pin['path'], sha256=digest((root / pin['path']).read_bytes()), bytes=len((root / pin['path']).read_bytes())) for name, pin in SNAPSHOTS.items()})
    fields = ['transaction_callers', 'compatibility_route_changes', 'closure_changes', 'source_changes', 'current_rejections',
        'generated_catalogs', 'additional_api_schema_changes', 'additional_build_metadata_changes', 'transaction_body_changes',
        'transaction_classification_changes', 'transaction_entry_changes', 'transaction_added_entries', 'transaction_removed_entries',
        'validation_input_changes', 'manual_review_fields', 'transaction_file_review_additions', 'transaction_new_file_reviews', 'baseline_validation', 'original_unapproved_methods']
    expected = dict(transaction=report['transaction'], compatibility=report['compatibility'],
        review_field_sha256={field: fingerprint(report[field]) for field in fields},
        historical_manifest_sha256=report['historical_manifest_sha256'], protected_source_manifest_sha256=report['protected_source_manifest_sha256'],
        maintenance_source_pins=maintenance, file_review_additions=file_additions, new_file_reviews=new_file_reviews,
        added_entries={name: now[name] for name in sorted(set(now) - set(old))}, body_changes=report['transaction_body_changes'],
        classification_changes=report['transaction_classification_changes'], manual_review_fields=manual, product_paths=product)
    report.update({field + '_sha256': value for field, value in expected['review_field_sha256'].items()})
    return report, expected

def proposal_readme(report):
    tx, compatibility = report['transaction'], report['compatibility']
    return (f'''# R5 catalog revision 19: owned fixture source reconciliation

Public product source: {FINAL_SOURCE}; exact tree: {FINAL_TREE}.
Previous public catalog: {CATALOG_BASE}; previous reviewed product source: {PREVIOUS_SOURCE}.

This is an author proposal. Later committed-candidate qualification requires the three unchanged CLIs, the five original modules and the complete original source-version runner. External raw receipts identify the exact source for each command. This static report does not certify the complete Company workflow or PostgreSQL execution.

Maintenance completes 0 original implementation TODOs. Parent acceptance remains 10/171, with 161 remaining. runtime_verified, product_green and task_complete remain false.

## Exact reviewed change and original producer facts

The new internal/testpg/r5_loopback_probe.go shared fixture calls pgx QueryRow(...).Scan. The original syntax collector adds that lexical name match to outboundReceiptRow.Scan's caller candidates. This is not a resolved call to that receipt reader. Exactly one existing entry changes its callers only. All PostgreSQL function bodies, classifications, derived assertions, file hashes, migrations and manual/file reviews remain identical to revision 18.

The compatibility finite closure changes exactly internal/store/postgres/r5_protocol_shared_test.go, whose two setup maps now supply the explicit false booleans required by the already approved four-boolean grant contract. Route/client facts, route semantics, wire evidence and both client catalog byte sequences remain identical. The complete source closure is regenerated by the original producer and pinned, never relaxed.

The separate eleven-file validation-input manifest binds all three updated PostgreSQL fixture files, the shared probe and owner, the frontend wrapper and its tests/registry, the source-input regression tests, the former batch fixture consumer and Company workflow. The owner reuses the already declared r5fixtures build tag with an exact owner selector; the original conditional-input policy and unknown-input rejection remain unchanged. This includes inputs outside the finite compatibility closure. Cancellation/deadline behavior and PostgreSQL runtime assertions require their own actual execution evidence.

Original clean-source CLI exits are transactions=1 (one caller candidate), compatibility=1 (source hash drift), clients=0 (unchanged). Proposals must pass all three original CLIs before being emitted.

Files/functions: {tx['postgres_files']}/{tx['functions']}; SQL execution calls: {tx['sql_execution_calls']}; direct writes/write closures: {tx['direct_write_functions']}/{tx['write_closure_functions']}; migrations: {tx['migration_files']}; routes/client branches: {compatibility['routes']}/{compatibility['client_branches']}; finite closure: {compatibility['source_files']}.

## Historical preservation

All 39 old methods remain: 36 keep full source bytes and AST; the two revision 18 positives run their identical bodies in the exact clean historical public snapshot; the current positive receives only five checked pin/list substitutions. All five original unapproved negative controls remain unchanged and executable. Of 102 existing assignments, only CURRENT_EVIDENCE, SOURCE_COMMIT and SOURCE_TREE advance. Two strict current positives bring the module to 41. The original transaction and compatibility test modules do not change. All historical directories/manifests and original producer bytes remain exact.

{SOURCE_BOUNDARY}
''').encode()


def run(invocation, output, typescript_archive=None):
    invocation, output = invocation.resolve(), output.resolve()
    require(not output.is_relative_to(invocation) and not output.exists(), 'output must be a new external directory')
    output.mkdir(parents=True)
    invocation_before = identity(invocation)
    write_raw(output, 'invocation-before.json', canonical(invocation_before))
    require(not invocation_before['status'], 'invocation checkout is not clean')
    require(digest((invocation / PRIOR_UTILITY).read_bytes()) == PRIOR_UTILITY_SHA256, 'shared revision18 utility changed')
    for commit, tree in ((FINAL_SOURCE, FINAL_TREE), (CATALOG_BASE, CATALOG_TREE), (PREVIOUS_SOURCE, PREVIOUS_TREE)):
        require(git_text(invocation, 'rev-parse', commit + '^{tree}') == tree, 'fixed public tree mismatch')
    for ancestor, descendant in ((CATALOG_BASE, FINAL_SOURCE), (FINAL_SOURCE, 'HEAD')):
        subprocess.run(['git', '-C', str(invocation), 'merge-base', '--is-ancestor', ancestor, descendant], check=True)
    with tempfile.TemporaryDirectory(prefix='r5-catalog19-source-') as temp:
        root = Path(temp) / 'source'
        subprocess.run(['git', 'clone', '--quiet', '--no-hardlinks', '--no-checkout', str(invocation), str(root)], check=True)
        subprocess.run(['git', '-C', str(root), 'checkout', '--quiet', '--detach', FINAL_SOURCE], check=True)
        require((root / '.git').is_dir() and not (root / '.git/objects/info/alternates').exists()
                and (root / git_text(root, 'rev-parse', '--git-common-dir')).resolve() == root / '.git', 'ordinary independent clone required')
        source_before = identity(root)
        require(source_before == dict(commit=FINAL_SOURCE, tree=FINAL_TREE, status=''), 'source clone not exact/clean')
        write_raw(output, 'source-before.json', canonical(source_before))
        for path, expected in {**ORIGINAL_PRODUCERS, PRIOR_UTILITY: PRIOR_UTILITY_SHA256}.items():
            raw = git(root, 'show', CATALOG_BASE + ':' + path)
            require(digest(raw) == expected and (root / path).read_bytes() == raw, 'original producer/helper drift: ' + path)
        previous = {}
        for name, pin in SNAPSHOTS.items():
            raw = git(root, 'show', CATALOG_BASE + ':' + pin['path'])
            require(pin_bytes(pin['path'], raw) == pin and (root / pin['path']).read_bytes() == raw, 'public revision18 snapshot drift: ' + name)
            previous[name] = json.loads(raw)
        require(digest((root / RECONCILIATION).read_bytes()) == BASE_TEST_SHA256, 'original current test module drift')
        sys.path.insert(0, str(root / 'scripts'))
        import r5_source_runner_prepare as preparation
        write_raw(output, 'typescript-preparation.json', canonical(preparation.prepare_typescript(root,
            archive=typescript_archive.read_bytes() if typescript_archive else None)))
        import check_r5_transactions as tx
        import check_r5_compatibility as gate
        require(Path(tx.__file__).resolve() == root / 'scripts/check_r5_transactions.py' and tx.ROOT == root
                and Path(gate.__file__).resolve() == root / 'scripts/check_r5_compatibility.py' and gate.ROOT == root, 'wrong original collector root')
        commands = [('transactions', [sys.executable, '-B', 'scripts/check_r5_transactions.py']),
            ('compatibility', [sys.executable, '-B', 'scripts/check_r5_compatibility.py']),
            ('clients', ['node', 'scripts/collect_api_calls.cjs', '--check'])]
        baseline_runs = [record_command(root, output / 'baseline', label, argv) for label, argv in commands]
        require([run.returncode for run, _ in baseline_runs] == [1, 1, 0], 'original CLI baseline is not exact expected 1/1/0')
        transaction_error = json.loads(baseline_runs[0][0].stdout)
        require(transaction_error.get('status') == 'FAIL' and 'caller drift:' in transaction_error.get('error', '')
                and SCAN in transaction_error['error'], 'transaction rejection is not actual owned fixture candidate')
        compatibility_error = json.loads(baseline_runs[1][0].stdout)
        require(compatibility_error.get('status') == 'rejected' and compatibility_error.get('error') == 'source hash drift',
                'compatibility rejection is not exact closure source drift')
        syntax, migrations = tx.extract(), tx.migration_inventory()
        routes, clients = gate.collect()
        for name, value in (('syntax', syntax), ('migrations', migrations), ('routes', routes), ('clients', clients)):
            write_raw(output / 'original-facts', name + '.json', canonical(value))
        require(identity(root) == source_before, 'source changed during original collection')
        write_raw(output, 'source-after-original-collection.json', canonical(identity(root)))
        baseline = dict(source=source_before, boundary='Three unchanged CLIs in exact clean public final source with original revision18 catalogs.', commands=[record for _, record in baseline_runs])
        transaction, manual, additions, new_reviews = reconcile_transactions(root, tx, previous['transaction'], syntax, migrations)
        require(canonical(clients) == (root / SNAPSHOTS['clients']['path']).read_bytes(), 'unreviewed client inventory change')
        facts = gate.source_facts(routes, clients)
        old_routes = {row['route']: row for row in previous['compatibility']['routes']}
        require(set(old_routes) == {row['route'] for row in facts}, 'unreviewed route set change')
        compatibility = copy.deepcopy(previous['compatibility'])
        compatibility.update(routes=[dict(old_routes[row['route']], **row) for row in facts], source_closure=gate.closure(facts, clients),
            acquisition=dict(previous['compatibility']['acquisition'], base_commit=FINAL_SOURCE),
            inventory_revision=inventory_revision('compatibility', previous['compatibility']['inventory_revision']['qualification']))
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in routes}
        client_routes = [dict(row, routes=[] if row['forwarding'] else [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in clients]
        require(client_routes == previous['client_routes'], 'unreviewed client route mapping change')
        current = dict(transaction=transaction, compatibility=compatibility, clients=clients, client_routes=client_routes)
        for name, value in current.items():
            write_raw(root, SNAPSHOTS[name]['path'], canonical(value))
        changed = {SNAPSHOTS[name]['path'] for name in ('transaction', 'compatibility')}
        require(set(git(root, 'diff', '--name-only').decode().splitlines()) == changed, 'generation did not change exactly the two reviewed current catalogs')
        report, expected = reconciliation(root, invocation, previous, current, tx, gate, syntax, migrations, routes, clients, manual, additions, new_reviews, baseline)
        proposals = {path: (root / path).read_bytes() for path in changed}
        proposals.update({REVIEW_PATH: canonical(report), README_PATH: proposal_readme(report)})
        proposals.update(candidate_test_sources(root, expected))
        write_raw(output, 'expected-current-pins.json', canonical(expected))
        for path, raw in sorted(proposals.items()):
            write_raw(output / 'files', path, raw)
        pins = {path: pin_bytes(path, raw) for path, raw in sorted(proposals.items())}
        write_raw(output, 'proposal-files.json', canonical(pins))
        generated = [record_command(root, output / 'generated', label, argv)[1] for label, argv in commands]
        write_raw(output, 'generated-cli-validation.json', canonical(dict(source=identity(root), boundary='Only dirty two-catalog proposal clone; not committed-candidate qualification.', commands=generated)))
        require(all(record['exit_code'] == 0 for record in generated), 'original CLI rejects proposed catalogs')
        require(set(git(root, 'diff', '--name-only').decode().splitlines()) == changed, 'generated validation changed other source')
        for path in changed:
            require((root / path).read_bytes() == proposals[path], 'catalog changed during validation')
        for name in ('clients', 'client_routes'):
            require(pin_bytes(SNAPSHOTS[name]['path'], (root / SNAPSHOTS[name]['path']).read_bytes()) == SNAPSHOTS[name], 'unchanged client bytes drifted')
        require(identity(invocation) == invocation_before, 'invocation source changed')
        write_raw(output, 'invocation-after.json', canonical(identity(invocation)))
        result = dict(status='proposals_generated_and_original_clis_passed', invocation=invocation_before, product_source=source_before,
            proposal_files=pins, generated_cli_exits=[record['exit_code'] for record in generated], catalog_module_methods=41,
            committed_candidate_tests_executed=False, implementation_todos_completed=0, runtime_verified=False, product_green=False, task_complete=False)
        write_raw(output, 'result.json', canonical(result))
        print(json.dumps({key: value for key, value in result.items() if key != 'proposal_files'}), flush=True)
        return 0

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root', type=Path, default=Path.cwd())
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--typescript-archive', type=Path)
    args = parser.parse_args()
    output_existed = args.output.exists()
    try:
        return run(args.root, args.output, args.typescript_archive)
    except Exception as error:
        failure = dict(status='failed', error=repr(error), implementation_todos_completed=0, runtime_verified=False, product_green=False, task_complete=False)
        if not output_existed and args.output.is_dir() and not args.output.resolve().is_relative_to(args.root.resolve()):
            write_raw(args.output, 'author-failure.json', canonical(failure))
        print(json.dumps(failure), flush=True)
        raise

if __name__ == '__main__':
    raise SystemExit(main())
