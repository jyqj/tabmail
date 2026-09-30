#!/usr/bin/env python3
"""Fail-closed syntax inventory drift check. Never a runtime concurrency proof."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess

ROOT = Path(__file__).resolve().parents[1]
CATALOG = ROOT / 'docs/company-mail/evidence/R5-TRANSACTION-COVERAGE.json'
PG = 'internal/store/postgres/'
MUTATION = re.compile(r'\b(?:INSERT\s+INTO|UPDATE\s+\w+(?:\s+\w+)?\s+SET|DELETE\s+FROM|ALTER\s+TABLE|CREATE\s+(?:TABLE|INDEX|FUNCTION|TRIGGER)|DROP\s+(?:TABLE|INDEX|FUNCTION|TRIGGER)|TRUNCATE)\b', re.I)
LOCK = re.compile(r'\bFOR\s+(?:UPDATE|SHARE|KEY\s+SHARE|NO\s+KEY\s+UPDATE)\b|pg_advisory|\bLOCK\s+TABLE\b', re.I)
TX = {'Begin', 'BeginTx', 'companyTx', 'companyReadTx', 'companyReferencedTx', 'companyTxScope', 'permissionOverrideTx', 'outboundPrincipalTx', 'legacyKeyContentTx'}
SQL_CALLS = {'Exec','Query','QueryRow','ExecContext','QueryContext','QueryRowContext'}

def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()

def extract(root=ROOT):
    go = os.environ.get('R5_GO') or shutil.which('go')
    cached = Path('/Users/jin/.cache/go-mod/golang.org/toolchain@v0.0.1-go1.25.7.darwin-arm64/bin/go')
    if 'R5_GO' not in os.environ and cached.exists():
        go = str(cached)
    if not go:
        raise ValueError('Go required for AST extraction; set R5_GO')
    env = dict(os.environ, GOTOOLCHAIN='local', GOPROXY='off')
    run = subprocess.run([go, 'run', './cmd/r5txinventory', str(root)], cwd=ROOT, env=env, capture_output=True, text=True, timeout=90)
    if run.returncode:
        raise ValueError('AST extraction failed: ' + run.stderr.strip())
    return json.loads(run.stdout)

def is_literal(expr):
    return re.fullmatch(r'`[^`]*`|"(?:[^"\\]|\\.)*"', expr, re.S) is not None

def classify(ast):
    funcs = [f for f in ast['functions'] if f['file'].startswith(PG)]
    by_name = {}
    for f in funcs:
        by_name.setdefault(f['name'], []).append(f['id'])
    writes = {f['id'] for f in funcs if any(MUTATION.search(s['value']) for s in f['strings'])}
    # Call-name closure deliberately over-approximates same-name methods. It is
    # coverage evidence, not resolved dispatch or a claim all callers are live.
    changed = True
    while changed:
        changed = False
        for f in funcs:
            if f['id'] not in writes and any(target in writes for c in f['calls'] for target in by_name.get(c['name'], [])):
                writes.add(f['id']); changed = True
    results = {}
    for f in funcs:
        direct = any(MUTATION.search(s['value']) for s in f['strings'])
        lock = any(LOCK.search(s['value']) for s in f['strings'])
        tx = any(c['name'] in TX for c in f['calls'])
        callback = 'func(' in f['params'][5:] or 'Validator' in f['params']
        sql = [c for c in f['calls'] if c['name'] in SQL_CALLS]
        dynamic = any(not is_literal(c.get('sql_expr','')) for c in sql)
        kind = ('direct-write' if direct else 'write-call-closure' if f['id'] in writes else 'transaction-or-callback' if tx or callback else 'lock-reader' if lock else 'dynamic-sql-review' if dynamic else 'read-or-pure')
        results[f['id']] = {'kind':kind, 'direct_write':direct, 'write_closure':f['id'] in writes, 'explicit_lock':lock, 'transaction_calls':tx, 'callback_parameter':callback, 'dynamic_sql_expression':dynamic}
    return results

def migration_inventory(root=ROOT):
    rows = []
    for path in sorted((root / PG / 'migrations').glob('*.sql')):
        up = path.read_text().split('-- +goose Down')[0]
        definitions=[]
        child='unknown'
        for n,line in enumerate(up.splitlines(),1):
            table = re.search(r'\b(?:CREATE|ALTER)\s+TABLE\s+(?:IF\s+NOT\s+EXISTS\s+)?(\w+)',line,re.I)
            if table: child=table.group(1)
            if re.search(r'REFERENCES|FOREIGN KEY|DROP CONSTRAINT|CREATE (?:FUNCTION|TRIGGER)|LOCK TABLE',line,re.I):
                definitions.append({'line':n,'child_context':child,'source':line.strip()})
        rows.append({'path':path.relative_to(root).as_posix(),'sha256':sha(path),'definitions':definitions})
    return rows

def validate(data, ast, migrations):
    errors=[]
    if data.get('schema') != 'r5-transaction-coverage-v1': errors.append('schema mismatch')
    if data.get('task_complete') is not False or data.get('runtime_verified') is not False: errors.append('inventory cannot claim task/runtime completion')
    funcs={f['id']:f for f in ast['functions'] if f['file'].startswith(PG)}
    entries=data.get('entries',[])
    ids=[e.get('id') for e in entries]
    if len(ids)!=len(set(ids)): errors.append('duplicate entry')
    if set(ids)!=set(funcs): errors.append('postgres function set drift: missing='+str(sorted(set(funcs)-set(ids)))+' obsolete='+str(sorted(set(ids)-set(funcs))))
    classes=classify(ast)
    for e in entries:
        f=funcs.get(e.get('id'))
        if not f: continue
        if e.get('syntax') != f: errors.append('function syntax drift: '+e['id'])
        if e.get('classification') != classes[e['id']]: errors.append('classification drift: '+e['id'])
        for field in ['owner','group','lock_fk_wait_fence','evidence','unverified_risks']:
            if not e.get(field): errors.append('missing review '+field+': '+e['id'])
        if not e.get('assertions') or not e.get('followup_tasks'): errors.append('missing precise assertion/followup: '+e['id'])
        assertions=e.get('assertions',{})
        expected_assertions={
            'direct_sql_effects':[{'line':x['line'],'sql_fragment':x['value']} for x in f['strings'] if MUTATION.search(x['value'])],
            'direct_lock_fragments':[{'line':x['line'],'sql_fragment':x['value']} for x in f['strings'] if LOCK.search(x['value'])],
            'transaction_helper_calls':[c for c in f['calls'] if c['name'] in TX],
            'sql_execution_expressions':[c for c in f['calls'] if c['name'] in SQL_CALLS],
        }
        for key,value in expected_assertions.items():
            if assertions.get(key)!=value: errors.append('precise assertion drift '+key+': '+e['id'])
        if not assertions.get('callback_effect'): errors.append('missing callback effect review: '+e['id'])
        if e.get('evidence_level') not in {'source-only','linked-existing-test','actual-batch-linked-relationship'}: errors.append('invalid evidence level: '+e['id'])
        if e.get('review_status') != 'static-type-review': errors.append('missing static review: '+e['id'])
        if not isinstance(e.get('callers'),list): errors.append('missing callers: '+e['id'])
        candidates=[]
        for caller in ast['functions']:
            for c in caller['calls']:
                if c['name']==f['name']:
                    candidates.append({'caller_id':caller['id'],'file':caller['file'],'function':caller['name'],'line':c['line'],'expression':c['expr'],'status':'name-match-candidate-not-dispatch-proof'})
        if e.get('callers') != candidates: errors.append('caller drift: '+e['id'])
    pgfiles=[{'path':f['path'],'sha256':f['sha256']} for f in ast['files'] if f['path'].startswith(PG)]
    if data.get('postgres_files') != pgfiles: errors.append('postgres file set/content drift including package SQL constants')
    if data.get('migrations') != migrations: errors.append('migration/FK/trigger definition drift')
    reviewed=data.get('reviewed_file_types',{})
    if set(reviewed)!=set(f['path'] for f in pgfiles): errors.append('file-type review coverage drift')
    if any(not v for v in reviewed.values()): errors.append('empty file-type review')
    if errors: raise ValueError('\n'.join(errors))
    return {'status':'PASS','postgres_files':len(pgfiles),'functions':len(entries),'direct_write_functions':sum(c['direct_write'] for c in classes.values()),'write_closure_functions':sum(c['write_closure'] for c in classes.values()),'migration_files':len(migrations),'task_complete':False,'runtime_verified':False,'meaning':'syntax inventory current; no concurrency or behavior equivalence claim'}

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--catalog',type=Path,default=CATALOG)
    args=parser.parse_args()
    try:
        data=json.loads(args.catalog.read_text())
        result=validate(data,extract(),migration_inventory())
    except (ValueError,OSError,subprocess.TimeoutExpired,json.JSONDecodeError) as exc:
        print(json.dumps({'status':'FAIL','task_complete':False,'error':str(exc)},ensure_ascii=False));return 1
    print(json.dumps(result,ensure_ascii=False));return 0

if __name__=='__main__':
    raise SystemExit(main())
