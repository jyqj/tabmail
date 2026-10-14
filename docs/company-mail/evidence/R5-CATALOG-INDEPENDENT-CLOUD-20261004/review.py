"""Independent exact-object provenance and fresh disposable mutation review."""
import copy
import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
from unittest import mock

import check_r5_transactions as tx
import check_r5_compatibility as gate

ROOT = Path(__file__).resolve().parents[4]
BASE = '3f34c31ed51a721b318a76512805e4a14dc292c3'
HEAD = '4540fb91ba742443dcf0a872b80261e58af7b3ab'
AUTHOR = ROOT / 'docs/company-mail/evidence/R5-CATALOG-RECONCILIATION-20261004'
OUT = Path(__file__).parent


def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)


def digest(data):
    return hashlib.sha256(data).hexdigest()


def reject(label, callback, expected):
    try:
        callback()
    except ValueError as error:
        assert expected in str(error), str(error)
        controls.append({'control': label, 'result': 'rejected', 'reason': expected})
    else:
        raise AssertionError(label + ' accepted')


paths = git('diff', '--name-only', BASE, HEAD).decode().splitlines()
assert len(paths) == 10
assert all(p.startswith('docs/company-mail/evidence/') or p in {
    'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_catalog_reconciliation.py'
} for p in paths)
snapshots = {}
for name, catalog in [('transaction', tx.CATALOG), ('compatibility', gate.MAP)]:
    current = json.loads(catalog.read_bytes())
    revision = current['inventory_revision']
    assert revision['revision'] == 2 and revision['source_commit'] == BASE
    original = git('show', BASE + ':' + str(catalog.relative_to(ROOT)))
    snapshot = (ROOT / revision['previous_snapshot']).read_bytes()
    assert original == snapshot
    assert digest(original) == revision['previous_sha256']
    snapshots[name] = digest(original)

chain = json.loads((AUTHOR / 'source-change-chain.json').read_text())['chain']
history = {}
for row in chain:
    path, commit = row['path'], row['commit']
    assert digest(git('show', commit + '^:' + path)) == row['parent_file_sha256']
    assert digest(git('show', commit + ':' + path)) == row['file_sha256']
    assert digest(git('show', BASE + ':' + row['review_document'])) == row['review_document_sha256_at_pinned_base']
    assert subprocess.run(['git', 'merge-base', '--is-ancestor', commit, BASE], cwd=ROOT).returncode == 0
    history.setdefault(path, []).append(row)
for path, rows in history.items():
    assert digest(git('show', HEAD + ':' + path)) == rows[-1]['file_sha256']
    for previous, following in zip(rows, rows[1:]):
        assert previous['file_sha256'] == following['parent_file_sha256']
    start = '006ff6939edbe5f9fa5732cd1ab1a2efb8ece4cf' if path.endswith('_test.go') else rows[0]['commit'] + '^'
    all_commits = git('log', '--format=%H', start + '..' + BASE, '--', path).decode().splitlines()
    # Merges may restate an accepted side-branch blob; every changed blob must be in the chain.
    known = {r['file_sha256'] for r in rows} | {rows[0]['parent_file_sha256']}
    for commit in all_commits:
        assert digest(git('show', commit + ':' + path)) in known
    history[path] = {'reviewed_changes': len(rows), 'all_path_commits': all_commits}

data = json.loads(tx.CATALOG.read_text())
compat = json.loads(gate.MAP.read_text())
old_data = json.loads(git('show', BASE + ':' + str(tx.CATALOG.relative_to(ROOT))))
old_compat = json.loads(git('show', BASE + ':' + str(gate.MAP.relative_to(ROOT))))
old_entries = {e['id']: e for e in old_data['entries']}
entries = {e['id']: e for e in data['entries']}
assert old_entries.keys() == entries.keys()
deltas = {key: sorted(field for field in old_entries[key] if old_entries[key][field] != entries[key][field])
          for key in entries if old_entries[key] != entries[key]}
claimed = json.loads((AUTHOR / 'reconciliation.json').read_text())
assert deltas == {r['id']: sorted(r['changed_fields']) for r in claimed['transaction_entry_deltas']}
assert len(deltas) == 19
assert sum(old_entries[k]['syntax'] != entries[k]['syntax'] for k in entries) == 11
assert sum(old_entries[k]['callers'] != entries[k]['callers'] for k in entries) == 16
assert [k for k in entries if old_entries[k]['syntax']['sha256'] != entries[k]['syntax']['sha256']] == [tx.PG + 'employee_disposition.go::offboardingSubjectsTx']
assert {k for k in old_data if old_data[k] != data[k]} == {
    'baseline_commit', 'postgres_files', 'entries', 'last_review_base_commit', 'current_review_boundary'
}
assert set(data) - set(old_data) == {'inventory_revision'}
assert {k for k in old_compat if old_compat[k] != compat[k]} == {'source_closure'}
assert set(compat) - set(old_compat) == {'inventory_revision'}
ast, migrations = tx.extract(), tx.migration_inventory()
routes, clients = gate.collect()
controls = []
with tempfile.TemporaryDirectory(prefix='R5-CATALOG-INDEPENDENT-CLOUD-20261004-source-') as directory:
    root = Path(directory)
    for group in ('internal', 'cmd'):
        for source in (ROOT / group).rglob('*.go'):
            if source.name.endswith('_test.go') or source.is_relative_to(ROOT / 'cmd/r5txinventory'):
                continue
            target = root / source.relative_to(ROOT)
            target.parent.mkdir(parents=True, exist_ok=True)
            shutil.copyfile(source, target)
    assert tx.extract(root) == ast
    path = root / 'internal/store/postgres/employee_disposition.go'
    original = path.read_text()
    check = 'if !authz.CanManageTenantMember(a, a.TenantID, role) {'
    assert original.count(check) == 1
    path.write_text(original.replace(check, 'if false {'))
    reject('actual successor role-policy bypass', lambda: tx.validate(data, tx.extract(root), migrations), 'function syntax drift: internal/store/postgres/employee_disposition.go::offboardingSubjectsTx')
    path.write_text(original + '\n// independent unreviewed comment\n')
    reject('actual file-only bytes', lambda: tx.validate(data, tx.extract(root), migrations), 'postgres file set/content drift')

changed = copy.deepcopy(ast)
function = next(f for f in changed['functions'] if f['name'] == 'offboardingSubjectsTx')
function['calls'].append({'line': function['end'], 'expr': 'tx.Exec', 'name': 'Exec', 'sql_expr': '`UPDATE users SET is_active=true`'})
reject('extra AST SQL operation', lambda: tx.validate(data, changed, migrations), 'function syntax drift')
changed_routes = copy.deepcopy(routes)
changed_routes[0]['path'] += '/independent-unreviewed'
reject('unreviewed route fact', lambda: gate.validate(compat, changed_routes, clients), 'route producer differs')
with tempfile.TemporaryDirectory(prefix='R5-CATALOG-INDEPENDENT-CLOUD-20261004-closure-') as directory:
    root = Path(directory)
    for relative in compat['source_closure']:
        target = root / relative
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copyfile(ROOT / relative, target)
    path = root / 'internal/api/handlers/r5_protocol_component_observations_test.go'
    path.write_bytes(path.read_bytes() + b'\n// independent unreviewed closure bytes\n')
    with mock.patch.object(gate, 'ROOT', root):
        actual = gate.closure(gate.source_facts(routes, clients), clients)
    with mock.patch.object(gate, 'closure', return_value=actual):
        reject('actual component closure bytes', lambda: gate.validate(compat, routes, clients), 'source hash drift')

result = {'base': BASE, 'reviewed_head': HEAD, 'verdict': 'accept_static_reconciliation',
          'delta_paths': paths, 'revision1_byte_preserved_sha256': snapshots,
          'source_history': history, 'provenance_rows_verified': len(chain),
          'transaction_entry_deltas_verified': deltas,
          'fresh_controls': controls, 'transaction': tx.validate(data, ast, migrations),
          'compatibility': gate.validate(compat, routes, clients),
          'runtime_qualified': False, 'central_progress': '10/171 unchanged'}
(OUT / 'review.json').write_text(json.dumps(result, indent=2) + '\n')
print(json.dumps({'verdict': result['verdict'], 'provenance_rows': len(chain), 'fresh_controls': len(controls)}))
