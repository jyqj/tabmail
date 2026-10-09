#!/usr/bin/env python3
"""Prepare revision-18 proposals using the unchanged original producers.

Only this revision's reviewed deltas are authored here. Shared byte/AST/Git
helpers and historical context templates come from the pinned revision-17
utility; that file, all collectors, runners and historical reviews are unchanged.
An ordinary clone supplies the exact public product source. Generated proposals
are not a committed-candidate, runtime, release or TODO qualification.
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
    identity, load_module, local_operations, method_nodes, pin_bytes,
    record_command, require, source_paths, source_pin, wrap_method, write_raw,
)

FINAL_SOURCE = '177ee571b194836678cc9a473f178c655326ba7c'
FINAL_TREE = '2d5d2c44680f6a6cb2b2d1166e2923806beafcc8'
CATALOG_BASE = 'ffb22b64698e4ff168a8d622018d3e169b3cdf07'
CATALOG_TREE = '13f6a9825fcaac82c29dec5bbcac3677fd53371f'
PREVIOUS_SOURCE = '17be458fb9d36e135a95962638b68ce105c16518'
PREVIOUS_TREE = 'fd362eed901840f23f58b477104a642f2a6cbf51'
RECONCILIATION = 'scripts/tests/test_r5_catalog_reconciliation.py'
BASE_TEST_SHA256 = '50c3ba4166ac29a21d9c0d3e2bfb3e8aaa0788a358c9893a22f10d282a555bba'
PRIOR_UTILITY = 'scripts/prepare_r5_catalog_revision17.py'
PRIOR_UTILITY_SHA256 = 'bb4cba4ab2951c9f964e783e89c901e1fc582fd2c2dd919de74bfba12de10e31'
UTILITY = 'scripts/prepare_r5_catalog_revision18.py'
REVIEW_DIRECTORY = 'docs/company-mail/evidence/R5-CATALOG-REVISION18-20261010'
REVIEW_PATH = REVIEW_DIRECTORY + '/reconciliation.json'
README_PATH = REVIEW_DIRECTORY + '/README.md'
MUTABLE_TEST_PATHS = [RECONCILIATION, 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py']
PROTECTED_ROOTS = ('scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go')
PRODUCT_ROOTS = ('internal', 'cmd', 'web', 'third_party')
BUILD_PATHS = ('go.mod', 'go.sum', 'web/package.json', 'web/package-lock.json')
SOURCE_BOUNDARY = prior.SOURCE_BOUNDARY
ORIGINAL_PRODUCERS = prior.ORIGINAL_PRODUCERS
MANUAL_FIELDS = prior.MANUAL_FIELDS
SNAPSHOTS = {
    'transaction': dict(path='docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json', blob='487cacb05965d30ed7a8751ddaa641806e943e98', sha256='86e39488c2800ded657eda75e09ed7f3e523ec49b5faebf90f4e0867a70fe354', bytes=2970167),
    'compatibility': dict(path='docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json', blob='0a7589f5418cdd2aacf87d5928eca235094bba21', sha256='e27227e2fc60bc74dbb0524cf1d142ca6147c804caf1337e6fa95fc0eb94917d', bytes=411980),
    'clients': dict(path='docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json', blob='1959adcc9fd996a61adeedb4e4954f55cc7b2b6f', sha256='6225c9a6133e1a0204db79a78da870cf82f0fd08f633af5915cb2dfec4e84133', bytes=41493),
    'client_routes': dict(path='docs/company-mail/evidence/R5-CLIENT-CALLS.json', blob='1081a96cbfc2438b7529e3c080e7a7e647d0658a', sha256='c48d008eea62a6b7afcb223ef092164071b675f041035fec8c1724626d7aea83', bytes=50414),
}
ROTATE = 'internal/store/postgres/refresh_rotation.go:*PgStore:RotateRefreshToken'
AUDIT = 'internal/store/postgres/company_console.go:*PgStore:ListCompanyAudit'
ACTIVATE = 'internal/store/postgres/company_members.go:*PgStore:ActivateEmployee'
CLASSIFY = 'internal/store/postgres/activation_errors.go::classifyActivationInsertError'
NEW_FILE = 'internal/store/postgres/activation_errors.go'
REVIEWED_FILES = {
    NEW_FILE: 'b2cde6796f5fadabedf2d3ec5af4aef1b2f25f23',
    'internal/store/postgres/company_console.go': '8bfb99a341908f98e67a3fd2e0e43a3a6f18c713',
    'internal/store/postgres/company_members.go': 'b742f6757cc0c26d9fd29460e3b3ce8e51842367',
    'internal/store/postgres/refresh_rotation.go': '86f4d6e063122250713695e4dafd29cc9561ca17',
}
FROZEN_METHODS = (
    'test_revision17_binds_actual_source_facts_and_preserves_manual_reviews',
    'test_revision17_preserves_public_history_collectors_and_original_rejections',
)
NEW_METHODS = {name.replace('revision17', 'revision18') for name in FROZEN_METHODS}
CURRENT_METHOD = prior.CURRENT_METHOD
CURRENT_REPLACEMENTS = (
    ('REVISION17_EXPECTED', 'REVISION18_EXPECTED'),
    ("('revision16', self.rev16_tx, self.rev16_compat)]", "('revision16', self.rev16_tx, self.rev16_compat), ('revision17', self.rev17_tx, self.rev17_compat)]"),
    ('pin = REVISION16_SNAPSHOTS[name]', 'pin = REVISION17_SNAPSHOTS[name]'),
    ("revision['revision'], 17", "revision['revision'], 18"),
    ("revision['previous_snapshot_commit'], REVISION16_COMMIT", "revision['previous_snapshot_commit'], REVISION17_COMMIT"),
)
TRACES = {
    ROTATE: (
        'refresh-authentication-snapshot-writer',
        'The existing family advisory transaction lock, user SHARE lock and old-token UPDATE '
        'lock retain their family/user/token order. The user read now combines the existing '
        'userSelect constant with a token-user subquery and FOR SHARE, then scanUser captures '
        'the complete user under that existing lock. This changes the lexical dynamic-SQL '
        'classification, not the user lock level. Expiry, replay-family revocation, descendant '
        'insert and Commit remain. Only successful Commit publishes the in-memory Issuance '
        'UserID/TenantID/PasswordHash/SessionVersion proof for the subsequent HTTP identity '
        'comparison. No proof is persisted as a refresh-token column or JSON field.',
        'Existing refresh-token user/FK and family constraints remain. The literal lock '
        'fragment is only the suffix of userSelect; the resolved constant selects users. '
        'Commit uncertainty and all cross-writer schedules require separate executed evidence.'),
    AUDIT: (
        'tenant-administration-audit-reader',
        'companyReadTx(admin=true) uses companyNoTenantLock and currentMemberActor user '
        'FOR SHARE, rechecking current interactive tenant/active/session authority. A current '
        'administrator does not enter the non-admin effectivePermissionSnapshot branch; no '
        'profile SHARE lock is asserted for this path. The identical tenant-bounded filter '
        'feeds both count and ordered page reads, adding only permission.profile.create, '
        'permission.profile.update, permission.profile.delete, permission.profile.assign '
        'and permission.override.patch to the existing action families. Safe projected '
        'fields, ordering and pagination remain; no permission wildcard or direct data write '
        'is introduced. The two READ COMMITTED statements are not one repeatable snapshot.',
        'There is no direct SQL lock or new FK action in ListCompanyAudit. Its current-user '
        'lock is owned by the existing helper transaction; no forced tenant lock is added.'),
    ACTIVATE: (
        'invitation-activation-transaction-writer',
        'The independent activation transaction retains tenant UPDATE, invitation UPDATE, '
        'deadline checks, early sponsor/domain reads, profile creation, user/mailbox inserts '
        'and invitation consumption. Each INSERT now classifies only its own address unique '
        'constraint and retains the original cause; unrelated failures and late audit errors '
        'remain original errors. After required audit/outbox writes, a new current sponsor '
        'users FOR SHARE NOWAIT checks active super-admin or same-tenant admin authority and '
        'holds that row through Commit. Missing authority is forbidden; busy sponsor 55P03 '
        'is conflict with its cause. The pre-existing final company/domain NOWAIT fence '
        'and final elapsed-deadline check still follow. Any failure uses the original '
        'rollback for user, mailbox, profile, invitation, audit and outbox effects; no '
        'automatic activation retry is added.',
        'Existing user/profile/mailbox/domain foreign keys can still wait before the final '
        'sponsor fence. NOWAIT avoids adding a reverse-order wait after audit/outbox; it '
        'does not establish that all earlier FK waits are deadlock-free.'),
    CLASSIFY: (
        'activation-insert-error-classifier',
        'This pure helper is called only at the users and mailboxes INSERT failure sites. '
        'It preserves errors unless errors.As finds a non-nil PostgreSQL diagnostic. A '
        '23505 matches only the supplied address constraint; 40001 and 55P03 become retryable '
        'conflicts. Both wrappers retain Err. Unrelated constraints, 23503/23514/40P01, '
        'cancellation and nil return unchanged. There is no SQL, transaction, lock, network, '
        'callback or automatic retry. The caller, not this helper, identifies the INSERT stage.',
        'No implicit FK or database wait is performed by this error classifier.'),
}


def source_review(function):
    role, trace, _ = TRACES[function['id']]
    return dict(base_commit=FINAL_SOURCE, source_sha256=function['sha256'], role=role,
                trace=trace, unverified=SOURCE_BOUNDARY, review=README_PATH)


def manual_fields(tx, old, function):
    _, trace, implicit = TRACES[function['id']]
    return dict(lock_fk_wait_fence=dict(boundary=trace, local_operations=local_operations(tx, function),
                direct_lock_fragments=derived_assertions(tx, function)['direct_lock_fragments'], implicit_fk=implicit),
        evidence=[f"{function['file']}:{function['line']}-{function['end']} sha256={function['sha256']}",
                  'Original syntax producers; execution and CI qualification remain separately source-bound.'],
        unverified_risks=[*old.get('unverified_risks', []), SOURCE_BOUNDARY],
        file_family_context=trace, source_review=source_review(function))


def inventory_revision(name, qualification):
    pin = SNAPSHOTS[name]
    return dict(revision=18, source_commit=FINAL_SOURCE, previous_snapshot=pin['path'],
        previous_snapshot_commit=CATALOG_BASE, previous_snapshot_blob=pin['blob'],
        previous_sha256=pin['sha256'], qualification=qualification, review=README_PATH)


def reconcile_transactions(root, tx, previous, syntax, migrations):
    functions = {f['id']: f for f in syntax['functions'] if f['file'].startswith(tx.PG)}
    old = {e['id']: e for e in previous['entries']}
    classes = tx.classify(syntax)
    require(set(functions) - set(old) == {CLASSIFY} and not set(old) - set(functions), 'unreviewed PostgreSQL function set')
    changed = {name for name in old if old[name]['syntax']['sha256'] != functions[name]['sha256']}
    require(changed == {ROTATE, AUDIT, ACTIVATE}, 'unreviewed PostgreSQL body change')
    require({name for name in old if old[name]['classification'] != classes[name]} == {ROTATE}, 'unreviewed classification delta')
    require(classes[ROTATE] == dict(old[ROTATE]['classification'], dynamic_sql_expression=True)
            and old[ROTATE]['classification']['dynamic_sql_expression'] is False, 'unexpected refresh classification')
    require(classes[CLASSIFY] == dict(kind='read-or-pure', direct_write=False, write_closure=False,
        explicit_lock=False, transaction_calls=False, callback_parameter=False, dynamic_sql_expression=False),
        'new error helper has unreviewed effects')
    require(migrations == previous['migrations'], 'unreviewed migration/FK/trigger change')
    files = [dict(path=f['path'], sha256=f['sha256']) for f in syntax['files'] if f['path'].startswith(tx.PG)]
    old_files = {f['path']: f['sha256'] for f in previous['postgres_files']}
    fresh_files = {f['path']: f['sha256'] for f in files}
    require(set(fresh_files) - set(old_files) == {NEW_FILE} and not set(old_files) - set(fresh_files), 'unreviewed PostgreSQL file set')
    require({p for p in old_files if old_files[p] != fresh_files[p]} == set(REVIEWED_FILES) - {NEW_FILE}, 'unreviewed PostgreSQL file content')
    for path, expected in REVIEWED_FILES.items():
        require(git_text(root, 'rev-parse', FINAL_SOURCE + ':' + path) == expected
                and blob((root / path).read_bytes()) == expected, 'reviewed PostgreSQL blob mismatch: ' + path)
    current = copy.deepcopy(previous)
    manual = {name: manual_fields(tx, old[name], functions[name]) for name in sorted(changed)}
    for entry in current['entries']:
        function = functions[entry['id']]
        if entry['id'] in changed:
            require('historical_revision17_review' not in entry, 'historical review archive already present')
            entry['historical_revision17_review'] = {k: copy.deepcopy(entry[k]) for k in (*MANUAL_FIELDS, 'source_review') if k in entry}
            entry.update(manual[entry['id']])
        entry.update(syntax=function, classification=classes[entry['id']], callers=callers(syntax, function))
        entry['assertions'].update(derived_assertions(tx, function))
    function = functions[CLASSIFY]
    derived = derived_assertions(tx, function)
    require(all(not value for value in derived.values()), 'classifier has unreviewed SQL/transaction effects')
    added = dict(id=CLASSIFY, owner='postgres.PgStore/activation_errors.go', entry=function['name'], group='TX01',
        classification=classes[CLASSIFY], syntax=function, callers=callers(syntax, function),
        **manual_fields(tx, {}, function), review_status='static-type-review',
        followup_tasks=['R5-P0-070', 'R5-P1-030'], evidence_level='source-only',
        assertions=dict(derived, effect_status='Pure error classification; no SQL or automatic retry.',
            callback_effect='No callback; two INSERT callers retain their transaction and error stage.',
            entry_role='Preserve storage causes and classify only the supplied activation address constraint.'))
    require(len(added['callers']) == 2 and all(c['caller_id'] == ACTIVATE for c in added['callers']), 'unexpected classifier callers')
    current['entries'].append(added)
    additions = {}
    for path in sorted(set(REVIEWED_FILES) - {NEW_FILE}):
        ids = sorted(name for name in changed if functions[name]['file'] == path)
        additions[path] = dict(source_commit=FINAL_SOURCE, source_sha256=fresh_files[path], changed_functions=ids,
            added_functions=[], reviews={name: manual[name]['source_review'] for name in ids},
            note='Earlier family reviews and unknown fields remain unchanged; this addition describes the reviewed current path.', qualification='source-only')
        require('revision18_additions' not in current['reviewed_file_types'][path], 'revision18 file review already exists')
        current['reviewed_file_types'][path]['revision18_additions'] = additions[path]
    new_reviews = {NEW_FILE: dict(group='TX01', review=TRACES[CLASSIFY][1], remaining=SOURCE_BOUNDARY,
        source_commit=FINAL_SOURCE, source_sha256=fresh_files[NEW_FILE], added_functions=[CLASSIFY],
        source_review=source_review(function), qualification='source-only')}
    current['reviewed_file_types'].update(new_reviews)
    current.update(postgres_files=files, baseline_commit=FINAL_SOURCE, last_review_base_commit=FINAL_SOURCE,
        current_review_boundary='Revision 18 reviews the authentication snapshot, exact permission-audit actions, '
        'late invitation sponsor fence and INSERT-specific cause-preserving classifier. Existing manual fields '
        'and optional source_review presence are archived exactly. The new file/helper is pure, not a lock reader. '
        'The refresh user lock is pre-existing; only its full-row read and dynamic SQL form change. ' + SOURCE_BOUNDARY,
        inventory_revision=inventory_revision('transaction', previous['inventory_revision']['qualification']))
    return current, manual, additions, new_reviews


def verify_preservation(before_raw, after_raw, *, final):
    before, after = method_nodes(before_raw), method_nodes(after_raw)
    require(len(before) == 37 and len(after) == (39 if final else 37), 'test count drift')
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
        require(ast.unparse(wrapper.items[0].context_expr) == 'self.revision17_context()', 'wrong historical context')
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
    require(len(old_values) == 96 and set(old_values).issubset(new_values), 'old assignment set drift')
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
    require(digest(original.encode()) == BASE_TEST_SHA256, 'public revision17 test source mismatch')
    pins = '\n'.join(name + ' = ' + pprint.pformat(value, width=120, sort_dicts=False) for name, value in (
        ('REVISION17_COMMIT', CATALOG_BASE), ('REVISION17_TREE', CATALOG_TREE),
        ('REVISION17_SOURCE_COMMIT', PREVIOUS_SOURCE), ('REVISION17_SOURCE_TREE', PREVIOUS_TREE),
        ('REVISION17_SNAPSHOTS', SNAPSHOTS))) + '\n\n\n'
    advance = lambda text: text.replace('revision16', 'revision17').replace('REVISION16', 'REVISION17').replace('rev16', 'rev17')
    current = exact_replace(original, 'def revision16_snapshot(name):', pins + advance(prior.REVISION16_SNAPSHOT_TEXT) + 'def revision16_snapshot(name):')
    current = exact_replace(current, '        cls.ast = tx.extract()\n',
        "        cls.revision17 = {name: revision17_snapshot(name) for name in REVISION17_SNAPSHOTS}\n"
        "        cls.rev17_tx = cls.revision17['transaction']\n        cls.rev17_compat = cls.revision17['compatibility']\n        cls.ast = tx.extract()\n")
    current = exact_replace(current, '    def ' + CURRENT_METHOD + '(self):\n', advance(prior.REVISION16_CONTEXT_TEXT) + '    def ' + CURRENT_METHOD + '(self):\n')
    for name in FROZEN_METHODS:
        current = wrap_method(current, name, 'revision17_context')
    verify_preservation(original, current, final=False)
    return current


def current_test_template(original):
    # Derive only the two new current methods from the public revision17
    # positives. All old methods themselves remain in scaffold unchanged.
    text = '\n\n'.join(full_method(original, name).rstrip() for name in FROZEN_METHODS) + '\n'
    for prefix in ('REVISION', 'revision', 'rev'):
        text = re.sub(prefix + r'(15|16|17)(?!\d)', lambda m: prefix + str(int(m[1]) + 1), text)
    replacements = [
        ("review['inventory_revision'], 17", "review['inventory_revision'], 18"),
        ('self.assertEqual(set(after_files), set(before_files))',
         "self.assertEqual(set(after_files), set(before_files) | set(REVISION18_EXPECTED['new_file_reviews']))"),
        ('(len(before_files), len(after_files)), (64, 64)',
         "(len(before_files), len(after_files)), (64, REVISION18_EXPECTED['transaction']['postgres_files'])"),
        ("self.assertEqual(set(self.tx['reviewed_file_types']), set(self.rev17_tx['reviewed_file_types']))",
         "self.assertEqual(set(self.tx['reviewed_file_types']), set(self.rev17_tx['reviewed_file_types']) | set(REVISION18_EXPECTED['new_file_reviews']))"),
        ("        self.assertEqual(review['transaction_file_review_additions'], REVISION18_EXPECTED['file_review_additions'])\n",
         "        self.assertEqual(review['transaction_file_review_additions'], REVISION18_EXPECTED['file_review_additions'])\n"
         "        self.assertEqual(review['transaction_new_file_reviews'], REVISION18_EXPECTED['new_file_reviews'])\n"
         "        self.assertEqual(set(REVISION18_EXPECTED['new_file_reviews']), {'internal/store/postgres/activation_errors.go'})\n"
         "        for path, expected in REVISION18_EXPECTED['new_file_reviews'].items():\n"
         "            self.assertEqual(self.tx['reviewed_file_types'][path], expected)\n"
         "            self.assertEqual(expected['source_commit'], SOURCE_COMMIT)\n"
         "            self.assertEqual(expected['source_sha256'], after_files[path])\n"),
        ("(len(old), len(current)), (405, REVISION18_EXPECTED", "(len(old), len(current)), (406, REVISION18_EXPECTED"),
        ("self.assertEqual(added['classification']['kind'], 'lock-reader')", "self.assertEqual(added['classification']['kind'], 'read-or-pure')"),
        ("self.assertTrue(added['classification']['explicit_lock'])", "self.assertFalse(added['classification']['explicit_lock'])"),
        ("            self.assertFalse(added['classification']['direct_write'])\n",
         "            self.assertFalse(added['classification']['direct_write'])\n"
         "            self.assertFalse(added['classification']['write_closure'])\n"
         "            self.assertFalse(added['classification']['transaction_calls'])\n"
         "            self.assertFalse(added['classification']['callback_parameter'])\n"
         "            self.assertFalse(added['classification']['dynamic_sql_expression'])\n"
         "            self.assertEqual(derived_assertions(function), {key: [] for key in derived_assertions(function)})\n"
         "            self.assertEqual(len(added['callers']), 2)\n"
         "            self.assertEqual({row['caller_id'] for row in added['callers']}, {'internal/store/postgres/company_members.go:*PgStore:ActivateEmployee'})\n"),
        ("'--', 'internal', 'cmd', 'web').decode().splitlines()", "'--', 'internal', 'cmd', 'web', 'third_party').decode().splitlines()"),
        ("('web/package.json', 'web/package-lock.json')", "('go.mod', 'go.sum', 'web/package.json', 'web/package-lock.json')"),
        ('(len(before), len(after)), (35, 37)', '(len(before), len(after)), (37, 39)'),
        ("(\"revision['revision'], 16\", \"revision['revision'], 17\")", "(\"revision['revision'], 17\", \"revision['revision'], 18\")"),
        ('len(old_values), 90', 'len(old_values), 96'),
    ]
    for old, new in replacements:
        text = exact_replace(text, old, new)
    # The preceding public current-inventory count is 406. This occurrence is
    # in the exact allowed replacement tuple, not a runtime qualification.
    require(text.count("result['functions'], 405") == 1, 'prior current function count changed')
    text = text.replace("result['functions'], 405", "result['functions'], 406")
    return text


def candidate_test_sources(root, expected):
    original = git(root, 'show', CATALOG_BASE + ':' + RECONCILIATION).decode()
    current = scaffold(original)
    current = exact_replace(current,
        '"""Frozen revision-1 through revision-16 reviews and current revision-17 facts."""',
        '"""Frozen revision-1 through revision-17 reviews and current revision-18 facts."""')
    current = exact_replace(current,
        "CURRENT_EVIDENCE = ROOT / 'docs/company-mail/evidence/R5-CATALOG-REVISION17-20261009'",
        "CURRENT_EVIDENCE = ROOT / '" + REVIEW_DIRECTORY + "'")
    current = exact_replace(current, "\nSOURCE_COMMIT = '" + PREVIOUS_SOURCE + "'\n", "\nSOURCE_COMMIT = '" + FINAL_SOURCE + "'\n")
    current = exact_replace(current, "\nSOURCE_TREE = '" + PREVIOUS_TREE + "'\n", "\nSOURCE_TREE = '" + FINAL_TREE + "'\n")
    old = full_method(current, CURRENT_METHOD)
    replacement = old
    for before, after in CURRENT_REPLACEMENTS:
        replacement = exact_replace(replacement, before, after)
    current = exact_replace(current, old, replacement)
    current = exact_replace(current, 'def revision17_snapshot(name):',
        'REVISION18_EXPECTED = ' + pprint.pformat(expected, width=120, sort_dicts=False) + '\n\n\ndef revision17_snapshot(name):')
    current = current.rstrip() + '\n\n' + current_test_template(original)
    verify_preservation(original, current, final=True)
    compile(current, RECONCILIATION, 'exec')
    result = {RECONCILIATION: current.encode()}
    for path, replacements in (
        ('scripts/tests/test_r5_transactions.py', [("result['functions'], 406", "result['functions'], %d" % expected['transaction']['functions']),
            ("result['postgres_files'], 64", "result['postgres_files'], %d" % expected['transaction']['postgres_files'])]),
        ('scripts/tests/test_r5_compatibility.py', [("result['routes'],133", "result['routes'],%d" % expected['compatibility']['routes']),
            ("result['client_branches'],136", "result['client_branches'],%d" % expected['compatibility']['client_branches'])]),
    ):
        raw = git(root, 'show', CATALOG_BASE + ':' + path).decode()
        for old, new in replacements:
            raw = exact_replace(raw, old, new)
        compile(raw, path, 'exec')
        result[path] = raw.encode()
    return result


def source_change(root, path):
    return dict(path=path, before_commit=PREVIOUS_SOURCE, after_commit=FINAL_SOURCE,
        **{'before_' + key: value for key, value in source_pin(root, PREVIOUS_SOURCE, path).items()},
        **{'after_' + key: value for key, value in source_pin(root, FINAL_SOURCE, path).items()})


def original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous):
    require(digest((root / RECONCILIATION).read_bytes()) == BASE_TEST_SHA256, 'original rejection module drift')
    module = load_module(root / RECONCILIATION, 'r5_catalog18_original_tests')
    old = [('revision1', json.loads((module.EVIDENCE / 'historical-transaction-inventory-revision1.json').read_text()),
        json.loads((module.EVIDENCE / 'historical-compatibility-inventory-revision1.json').read_text()))]
    for number in range(2, 17):
        snapshot = getattr(module, 'revision%d_snapshot' % number)
        old.append(('revision%d' % number, snapshot('transaction'), snapshot('compatibility')))
    old.append(('revision17', previous['transaction'], previous['compatibility']))
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
    old = {entry['id']: entry for entry in previous['transaction']['entries']}
    now = {entry['id']: entry for entry in current['transaction']['entries']}
    report = dict(schema_version=1, inventory_revision=18, source_commit=FINAL_SOURCE, source_tree=FINAL_TREE,
        previous_catalog_commit=CATALOG_BASE, previous_product_source_commit=PREVIOUS_SOURCE,
        issue='NEXT-1010 static source catalog maintenance; zero additional implementation TODOs.',
        revision17_snapshots={name: dict(commit=CATALOG_BASE, **pin) for name, pin in SNAPSHOTS.items()},
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
    old_review_path = 'docs/company-mail/evidence/R5-CATALOG-REVISION17-20261009/reconciliation.json'
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
        excluded_closure_product_paths=[p for p in product if p not in new_closure],
        additional_build_metadata_changes=[source_change(root, p) for p in BUILD_PATHS if git(root, 'show', PREVIOUS_SOURCE + ':' + p) != git(root, 'show', FINAL_SOURCE + ':' + p)],
        additional_api_schema_changes=[source_change(root, p) for p in ('internal/api/openapi.yaml',) if git(root, 'show', PREVIOUS_SOURCE + ':' + p) != git(root, 'show', FINAL_SOURCE + ':' + p)],
        current_rejections=original_rejections(root, tx, gate, syntax, migrations, routes, clients, previous),
        generated_catalogs={name: dict(path=pin['path'], sha256=digest((root / pin['path']).read_bytes()), bytes=len((root / pin['path']).read_bytes())) for name, pin in SNAPSHOTS.items()})
    fields = ['transaction_callers', 'compatibility_route_changes', 'closure_changes', 'source_changes', 'current_rejections',
        'generated_catalogs', 'additional_api_schema_changes', 'additional_build_metadata_changes', 'transaction_body_changes',
        'transaction_classification_changes', 'transaction_entry_changes', 'transaction_added_entries', 'transaction_removed_entries',
        'manual_review_fields', 'transaction_file_review_additions', 'transaction_new_file_reviews', 'baseline_validation', 'original_unapproved_methods']
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
    return (f'''# R5 catalog revision 18: NEXT-1010 source reconciliation proposal

Public product source: {FINAL_SOURCE}; complete tree: {FINAL_TREE} (PR #256).
Previous public catalog: {CATALOG_BASE}; previous product: {PREVIOUS_SOURCE}.

This is an author proposal. Qualification of the later committed maintenance candidate requires the three unchanged CLIs, the five original modules and the complete original source-version runner. Raw command output and exact source identities remain in the external execution packet. This packet makes no claim that the pending 50-case PostgreSQL workflow has passed.

Maintenance completes 0 implementation TODOs. Parent acceptance remains 10/171, with 161 remaining. runtime_verified, product_green and task_complete remain false.

## Original producer facts

- PostgreSQL files/functions: {tx['postgres_files']}/{tx['functions']}.
- SQL execution calls: {tx['sql_execution_calls']}.
- Direct write/write closure functions: {tx['direct_write_functions']}/{tx['write_closure_functions']}.
- Migrations: {tx['migration_files']}; routes: {compatibility['routes']}; client branches: {compatibility['client_branches']}; finite closure files: {compatibility['source_files']}, including {len(report['closure_changes'])} changed paths (the complete closure remains explicitly pinned).

## Reviewed source changes

RotateRefreshToken reads the complete authentication snapshot under its existing user SHARE lock and returns it only after successful Commit. Its userSelect concatenation changes lexical dynamic_sql_expression from false to true; family/user/token lock ordering is retained. ListCompanyAudit adds exactly five governed permission actions to the tenant-bounded count/page filter. Its current-administrator path locks the current user through companyReadTx and does not take the non-admin profile path.

ActivateEmployee adds a current sponsor FOR SHARE NOWAIT fence after required audit/outbox work, before the existing final domain fence and deadline check. Its two INSERTs classify only their own address constraints and preserve original causes. The new activation_errors.go helper is read-or-pure with no SQL, locks, callbacks or automatic retry. Prior manual reviews and optional source_review presence remain exactly archived.

The source change manifest explicitly includes third_party/go-smtp/conn.go and distinguishes paths outside the finite compatibility closure. The final NEXT1010 runner and workflow are protected as ordinary product-source files; original collectors, runners and revision17 utility retain their bytes.

## Historical preservation

All 37 old methods remain: 34 whole methods retain bytes and AST, two revision17 positive bodies run unchanged in the exact public historical clone, and one current positive has five checked pin/list substitutions. All five unapproved negative controls remain unchanged. Of 96 prior assignments only CURRENT_EVIDENCE, SOURCE_COMMIT and SOURCE_TREE advance; 93 values retain bytes and AST. Two new current positives bring the module to 39. New file/helper semantics and the one reviewed classification delta are tested explicitly without changing historical lock-reader/file-set assertions.

{SOURCE_BOUNDARY}
''').encode()


def run(invocation, output, typescript_archive=None):
    invocation, output = invocation.resolve(), output.resolve()
    require(not output.is_relative_to(invocation) and not output.exists(), 'output must be a new external directory')
    output.mkdir(parents=True)
    invocation_before = identity(invocation)
    write_raw(output, 'invocation-before.json', canonical(invocation_before))
    require(not invocation_before['status'], 'invocation checkout is not clean')
    require(digest((invocation / PRIOR_UTILITY).read_bytes()) == PRIOR_UTILITY_SHA256, 'shared revision17 utility changed')
    for commit, tree in ((FINAL_SOURCE, FINAL_TREE), (CATALOG_BASE, CATALOG_TREE), (PREVIOUS_SOURCE, PREVIOUS_TREE)):
        require(git_text(invocation, 'rev-parse', commit + '^{tree}') == tree, 'fixed public tree mismatch')
    for ancestor, descendant in ((CATALOG_BASE, FINAL_SOURCE), (FINAL_SOURCE, 'HEAD')):
        subprocess.run(['git', '-C', str(invocation), 'merge-base', '--is-ancestor', ancestor, descendant], check=True)
    with tempfile.TemporaryDirectory(prefix='r5-catalog18-source-') as temp:
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
            require(pin_bytes(pin['path'], raw) == pin and (root / pin['path']).read_bytes() == raw, 'public revision17 snapshot drift: ' + name)
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
        require(all(run.returncode == 1 for run, _ in baseline_runs), 'original source drift did not fail all three CLIs')
        transaction_error = json.loads(baseline_runs[0][0].stdout)
        require(transaction_error.get('status') == 'FAIL' and 'postgres function set drift:' in transaction_error.get('error', '')
                and CLASSIFY in transaction_error['error'], 'transaction rejection is not the actual added classifier')
        compatibility_error = json.loads(baseline_runs[1][0].stdout)
        require(compatibility_error.get('status') == 'rejected' and compatibility_error.get('error') == 'client producer differs from reviewed current source inventory', 'compatibility rejection is not actual source drift')
        require(re.search(rb'Error: client (?:source|inventory count|route mapping) drift', baseline_runs[2][0].stderr), 'client rejection is not actual drift')
        syntax, migrations = tx.extract(), tx.migration_inventory()
        routes, clients = gate.collect()
        for name, value in (('syntax', syntax), ('migrations', migrations), ('routes', routes), ('clients', clients)):
            write_raw(output / 'original-facts', name + '.json', canonical(value))
        require(identity(root) == source_before, 'source changed during original collection')
        write_raw(output, 'source-after-original-collection.json', canonical(identity(root)))
        baseline = dict(source=source_before, boundary='Three unchanged CLIs in exact clean public final source with original revision17 catalogs.', commands=[record for _, record in baseline_runs])
        transaction, manual, additions, new_reviews = reconcile_transactions(root, tx, previous['transaction'], syntax, migrations)
        write_raw(root, SNAPSHOTS['clients']['path'], canonical(clients))
        facts = gate.source_facts(routes, clients)
        old_routes = {row['route']: row for row in previous['compatibility']['routes']}
        require(set(old_routes) == {row['route'] for row in facts}, 'unreviewed route set change')
        compatibility = copy.deepcopy(previous['compatibility'])
        compatibility.update(routes=[dict(old_routes[row['route']], **row) for row in facts], source_closure=gate.closure(facts, clients),
            acquisition=dict(previous['compatibility']['acquisition'], base_commit=FINAL_SOURCE),
            inventory_revision=inventory_revision('compatibility', previous['compatibility']['inventory_revision']['qualification']))
        route_map = {(row['method'], gate.norm(row['path'])): row['method'] + ' ' + row['path'] for row in routes}
        client_routes = [dict(row, routes=[] if row['forwarding'] else [route_map[(method, gate.norm(row['path']))] for method in row['methods']]) for row in clients]
        current = dict(transaction=transaction, compatibility=compatibility, clients=clients, client_routes=client_routes)
        for name, value in current.items():
            write_raw(root, SNAPSHOTS[name]['path'], canonical(value))
        changed = {pin['path'] for pin in SNAPSHOTS.values()}
        require(set(git(root, 'diff', '--name-only').decode().splitlines()) == changed, 'generation changed more than four current catalogs')
        report, expected = reconciliation(root, invocation, previous, current, tx, gate, syntax, migrations, routes, clients, manual, additions, new_reviews, baseline)
        proposals = {pin['path']: (root / pin['path']).read_bytes() for pin in SNAPSHOTS.values()}
        proposals.update({REVIEW_PATH: canonical(report), README_PATH: proposal_readme(report)})
        proposals.update(candidate_test_sources(root, expected))
        write_raw(output, 'expected-current-pins.json', canonical(expected))
        for path, raw in sorted(proposals.items()):
            write_raw(output / 'files', path, raw)
        pins = {path: pin_bytes(path, raw) for path, raw in sorted(proposals.items())}
        write_raw(output, 'proposal-files.json', canonical(pins))
        generated = [record_command(root, output / 'generated', label, argv)[1] for label, argv in commands]
        write_raw(output, 'generated-cli-validation.json', canonical(dict(source=identity(root), boundary='Only dirty four-file proposal clone; not committed-candidate qualification.', commands=generated)))
        require(all(record['exit_code'] == 0 for record in generated), 'original CLI rejects proposed catalogs')
        require(set(git(root, 'diff', '--name-only').decode().splitlines()) == changed, 'generated validation changed other source')
        for pin in SNAPSHOTS.values():
            require((root / pin['path']).read_bytes() == proposals[pin['path']], 'catalog changed during validation')
        require(identity(invocation) == invocation_before, 'invocation source changed')
        write_raw(output, 'invocation-after.json', canonical(identity(invocation)))
        result = dict(status='proposals_generated_and_original_clis_passed', invocation=invocation_before, product_source=source_before,
            proposal_files=pins, generated_cli_exits=[record['exit_code'] for record in generated], catalog_module_methods=39,
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
