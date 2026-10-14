"""Read-only independent review audit; never changes the reviewed repository."""
from pathlib import Path
import ast
from collections import Counter
import hashlib
import json
import re
import subprocess

ROOT = Path('/workspace/scratch/4f09dfb9aa92/tabmail-frontend-review-catalog12-public')
OUTPUT = Path(__file__).with_name('source-review.json')
CURRENT = 'b87df7a77656457549d6f2d678c56d321ae0d869'
OLD = '58d0c9cf274569258319ff4b5dcab7912b0174f4'
PRODUCT = 'ed81ee2fcadc9e8cfcd165947561f8b4b65589e6'

def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def canonical(value):
    return (json.dumps(value, ensure_ascii=False, indent=2) + '\n').encode()

def raw_at(commit, path):
    return git('show', commit + ':' + path)

def json_at(commit, path):
    return json.loads(raw_at(commit, path))

def pin_at(commit, path):
    raw = raw_at(commit, path)
    return dict(path=path, blob=git('rev-parse', commit + ':' + path).decode().strip(),
                sha256=sha(raw), bytes=len(raw))

review = json_at(CURRENT, 'docs/company-mail/evidence/R5-CATALOG-REVISION12-20261008/reconciliation.json')
out = dict(source_commit=CURRENT, source_tree=git('rev-parse', CURRENT + '^{tree}').decode().strip(),
           product_source=PRODUCT, previous_catalog_source=OLD)
historical = []
for directory in review['historical_directories']:
    paths = git('ls-tree', '-r', '--name-only', OLD, '--', directory).decode().splitlines()
    assert set(paths) == {str(p.relative_to(ROOT)) for p in (ROOT / directory).rglob('*') if p.is_file()}
    for path in paths:
        assert (ROOT / path).read_bytes() == raw_at(OLD, path), path
        historical.append(pin_at(OLD, path))
assert len(historical) == 65 and historical == review['historical_manifest']
assert sha(canonical(historical)) == '3f0fd50e1b86ff3e470b5beca0399cf72791cb88d90e130e63b4f3119d47c53a'
out['historical_preservation'] = dict(files=65, directories=10, manifest_sha256=sha(canonical(historical)), entries=historical)

mutable = {'scripts/tests/test_r5_catalog_reconciliation.py', 'scripts/tests/test_r5_transactions.py', 'scripts/tests/test_r5_compatibility.py'}
paths = git('ls-tree', '-r', '--name-only', PRODUCT, '--', 'scripts', '.github/workflows', 'cmd/r5txinventory', 'internal/architecture/route_inventory_test.go').decode().splitlines()
protected = []
for path in paths:
    if path in mutable:
        continue
    assert (ROOT / path).read_bytes() == raw_at(PRODUCT, path), path
    protected.append(pin_at(PRODUCT, path))
assert len(protected) == 167 and protected == review['protected_source_manifest']
assert sha(canonical(protected)) == 'd9e16753906d1372bbfdfca9f7d1352c9b7c98511e30b970c3c53752d1402f1c'
out['protected_preservation'] = dict(files=167, manifest_sha256=sha(canonical(protected)), entries=protected)
assert len(review['unchanged_validators_and_collectors']) == 5
for path, expected in review['unchanged_validators_and_collectors'].items():
    assert (ROOT / path).read_bytes() == raw_at(OLD, path)
    assert sha((ROOT / path).read_bytes()) == expected
out['unchanged_original_collectors'] = review['unchanged_validators_and_collectors']

path = 'scripts/tests/test_r5_catalog_reconciliation.py'
before_raw, after_raw = raw_at(OLD, path).decode(), (ROOT / path).read_text()
def methods(raw):
    return {n.name: n for n in ast.walk(ast.parse(raw)) if isinstance(n, ast.FunctionDef) and n.name.startswith('test_')}
before, after = methods(before_raw), methods(after_raw)
assert len(before) == 25 and len(after) == 27 and set(before) <= set(after)
frozen = {'test_revision11_reconciles_actual_functions_clients_routes_and_source', 'test_revision11_preserves_history_and_all_unapproved_rejection_guards'}
current_pins = 'test_old_pins_reject_and_current_revision_passes_same_actual_facts'
identical = []
for name, method in before.items():
    if name in frozen:
        assert len(after[name].body) == 1
        wrapper = after[name].body[0]
        assert isinstance(wrapper, ast.With) and len(wrapper.items) == 1
        assert ast.unparse(wrapper.items[0].context_expr) == 'self.revision11_context()'
        assert [ast.dump(n, include_attributes=False) for n in method.body] == [ast.dump(n, include_attributes=False) for n in wrapper.body]
    elif name != current_pins:
        assert ast.get_source_segment(before_raw, method) == ast.get_source_segment(after_raw, after[name]), name
        assert ast.dump(method, include_attributes=False) == ast.dump(after[name], include_attributes=False), name
        identical.append(name)
guards = [name for name in before if name.startswith('test_unapproved_')]
assert len(guards) == 5 and set(guards) <= set(identical)
guard_pins = {name: dict(source_sha256=sha(ast.get_source_segment(after_raw, after[name]).encode()), ast_sha256=sha(ast.dump(after[name], include_attributes=False).encode())) for name in guards}
assert guard_pins == review['original_unapproved_methods']
out['old_tests_preserved'] = dict(before_count=25, after_count=27, all_old_names_retained=True,
    byte_and_ast_identical_methods=identical, frozen_public_revision11_same_assertion_ast=sorted(frozen),
    current_pins_method_advanced=current_pins, new_methods=sorted(set(after) - set(before)), original_unapproved_methods=guard_pins)
def assignments(raw):
    return {target.id: node.value for node in ast.parse(raw).body if isinstance(node, ast.Assign) for target in node.targets if isinstance(target, ast.Name)}
old_values, new_values = assignments(before_raw), assignments(after_raw)
constant_names = []
for name, value in old_values.items():
    if name.startswith('REVISION'):
        assert ast.dump(value, include_attributes=False) == ast.dump(new_values[name], include_attributes=False), name
        constant_names.append(name)
out['historical_revision_constants_unchanged'] = constant_names

path = 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json'
old_tx, new_tx = json_at(OLD, path), json_at(CURRENT, path)
old_entries, new_entries = ({row['id']: row for row in catalog['entries']} for catalog in (old_tx, new_tx))
assert set(old_entries) == set(new_entries) and len(old_entries) == 401
caller_changes = []
for identity, old in old_entries.items():
    new = new_entries[identity]
    assert {k: v for k, v in old.items() if k != 'callers'} == {k: v for k, v in new.items() if k != 'callers'}, identity
    if old['callers'] != new['callers']:
        caller_changes.append(dict(id=identity, before_count=len(old['callers']), after_count=len(new['callers'])))
assert len(caller_changes) == 11
for key in ('postgres_files', 'reviewed_file_types', 'migrations', 'historical_review_metadata'):
    assert old_tx[key] == new_tx[key], key
new_id = 'internal/store/postgres/postgres.go::New'
def logical_caller(row):
    return tuple((k, json.dumps(v, sort_keys=True)) for k, v in row.items() if k != 'line')
old_candidates = Counter(logical_caller(row) for row in old_entries[new_id]['callers'])
new_candidates = Counter(logical_caller(row) for row in new_entries[new_id]['callers'])
assert not (old_candidates - new_candidates) and sum((new_candidates - old_candidates).values()) == 1
candidate = [{k: json.loads(v) for k, v in row} for row in (new_candidates - old_candidates).elements()]
assert candidate[0]['expression'] == 'list.New' and candidate[0]['status'] == 'name-match-candidate-not-dispatch-proof'
out['transaction_review_preservation'] = dict(functions=401, only_caller_arrays_changed=caller_changes,
    new_name_candidate=candidate, all_other_function_fields_unchanged=True,
    boundary='New is a name match, not proof of PostgreSQL dispatch; syntax, classification, manual review, risk, and evidence fields are unchanged.')

path = 'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json'
old_gate, new_gate = json_at(OLD, path), json_at(CURRENT, path)
old_routes, new_routes = ({row['route']: row for row in catalog['routes']} for catalog in (old_gate, new_gate))
assert set(old_routes) == set(new_routes) and len(old_routes) == 133
route_changes = []
for route, old in old_routes.items():
    new = new_routes[route]
    assert {k: v for k, v in old.items() if k != 'clients'} == {k: v for k, v in new.items() if k != 'clients'}, route
    if old['clients'] != new['clients']:
        route_changes.append(route)
assert len(route_changes) == 7
assert set(old_gate['source_closure']) == set(new_gate['source_closure']) and len(old_gate['source_closure']) == 95
closure_changes = [p for p in old_gate['source_closure'] if old_gate['source_closure'][p] != new_gate['source_closure'][p]]
assert len(closure_changes) == 10
out['compatibility_review_preservation'] = dict(routes=133, only_client_rows_changed=route_changes,
    closure_membership_unchanged=95, closure_hash_changes=sorted(closure_changes))
path = 'docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/clients.json'
old_clients, new_clients = json_at(OLD, path), json_at(CURRENT, path)
def logical_client(row):
    return (row['source'], row['branch'], row['callee'], re.sub(r'\{[^}]+\}', '{}', row['path']), tuple(row['methods']), row['forwarding'])
old_calls, new_calls = Counter(map(logical_client, old_clients)), Counter(map(logical_client, new_clients))
assert len(old_clients) == 135 and len(new_clients) == 136 and not (old_calls - new_calls)
addition = list((new_calls - old_calls).elements())
assert addition == [('web/features/company/domain-settings.tsx', 0, 'company', '/api/v1/company/settings', ('GET',), False)]
old_forwarders, new_forwarders = ([row for row in clients if row['forwarding']] for clients in (old_clients, new_clients))
assert old_forwarders == new_forwarders and len(old_forwarders) == 7
out['client_logical_preservation'] = dict(before=135, after=136, removed=[], added=addition, identical_forwarders=7,
    normalized_fields=['source', 'branch', 'callee', 'path parameter placeholders normalized', 'methods', 'forwarding'],
    nonlogical_fields_not_compared=['line', 'owner', 'expression'],
    note='Invitation DELETE expression now encodes its id and moved owner; same route/method. Original producers separately check exact raw call rows.')

previous_product = review['previous_product_source_commit']
changed = git('diff', '--name-only', previous_product, PRODUCT, '--', 'internal', 'cmd', 'web').decode().splitlines()
product_paths = [p for p in changed if (p.endswith('.go') and not p.endswith('_test.go')) or (p.endswith(('.ts', '.tsx', '.css')) and '.test.' not in p) or (p.startswith('web/locales/') and p.endswith('.json'))]
assert product_paths == [row['path'] for row in review['source_changes']] and len(product_paths) == 18
inside = [p for p in product_paths if p in new_gate['source_closure']]
outside = [p for p in product_paths if p not in new_gate['source_closure']]
assert len(inside) == 8 and len(outside) == 10 and outside == review['excluded_closure_product_paths']
assert len(review['additional_api_schema_changes']) == 1 and review['additional_api_schema_changes'][0]['path'] == 'internal/api/openapi.yaml'
for row in review['source_changes'] + review['additional_api_schema_changes']:
    for prefix, commit in [('before', previous_product), ('after', PRODUCT)]:
        assert row[prefix + '_commit'] == commit
        if row[prefix + '_blob'] is None:
            assert not git('ls-tree', commit, '--', row['path'])
            assert row[prefix + '_sha256'] is None and row[prefix + '_bytes'] is None
        else:
            raw = raw_at(commit, row['path'])
            assert sha(raw) == row[prefix + '_sha256'] and len(raw) == row[prefix + '_bytes']
            assert git('rev-parse', commit + ':' + row['path']).decode().strip() == row[prefix + '_blob']
    assert (ROOT / row['path']).read_bytes() == raw_at(PRODUCT, row['path'])
assert review['additional_build_metadata_changes'] == []
for path in ('web/package.json', 'web/package-lock.json'):
    assert raw_at(previous_product, path) == raw_at(PRODUCT, path)
out['source_scope'] = dict(original_filter_product_paths=product_paths, in_closure=inside, outside_closure=outside,
    additional_api_schema_changes=review['additional_api_schema_changes'], build_metadata_changes=[])
assert not review['runtime_verified'] and not review['product_green'] and not review['task_complete'] and review['implementation_todos_completed'] == 0
assert review['parent_tasks'] == dict(accepted=10, total=171, remaining=161)
out['scope_claims'] = {k: review[k] for k in ('runtime_verified', 'product_green', 'task_complete', 'implementation_todos_completed', 'parent_tasks')}
out['no_material_issue_found'] = True
OUTPUT.write_text(json.dumps(out, ensure_ascii=False, indent=2) + '\n')
print('PASS: 65 historical files, 167 protected source files, 25 old test methods, 5 original negative guards, 401 manual function reviews; only reviewed catalog facts changed.')
