"""Syntax-only comparison; execute with the controlled environment in README."""
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import sys

ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'scripts'))
import check_r5_transactions as current_tx
import check_r5_compatibility as current_compat

PRIVATE=Path('/tmp/R5-ENV-EVIDENCE-INDEPENDENT-20261004')
BASE=PRIVATE/'base'

def load(name,path):
    spec=importlib.util.spec_from_file_location(name,path)
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    return module

def canonical(value):return json.dumps(value,sort_keys=True).encode()
def digest(value):return hashlib.sha256(canonical(value)).hexdigest()
def rejection(call):
    try:return dict(status='accepted',result=call())
    except ValueError as error:return dict(status='rejected',error=str(error),error_sha256=hashlib.sha256(str(error).encode()).hexdigest())

base_tx=load('base_tx',BASE/'scripts/check_r5_transactions.py')
base_compat=load('base_compat',BASE/'scripts/check_r5_compatibility.py')
current_ast=current_tx.extract()
base_ast=base_tx.extract()
current_migrations=current_tx.migration_inventory()
base_migrations=base_tx.migration_inventory()
tx_current=rejection(lambda:current_tx.validate(json.loads(current_tx.CATALOG.read_text()),current_ast,current_migrations))
tx_base=rejection(lambda:base_tx.validate(json.loads(base_tx.CATALOG.read_text()),base_ast,base_migrations))
routes,clients=current_compat.collect() # exact source-only Go route/Node AST producers
facts=current_compat.source_facts(routes,clients)
base_facts=base_compat.source_facts(routes,clients)
actual_closure=current_compat.closure(facts,clients)
base_closure=base_compat.closure(base_facts,clients)
compat_current=rejection(lambda:current_compat.validate(json.loads(current_compat.MAP.read_text()),routes,clients))
compat_base=rejection(lambda:base_compat.validate(json.loads(base_compat.MAP.read_text()),routes,clients))
stored_closure=json.loads(current_compat.MAP.read_text())['source_closure']
diff={p:dict(stored=stored_closure.get(p),observed=actual_closure.get(p)) for p in set(stored_closure)|set(actual_closure) if stored_closure.get(p)!=actual_closure.get(p)}
summary=dict(source_sha='df6ffe046611f5b2121f4d48d5114a2ececd2c24',base_sha='f52f8cbbf31dcbfb72ba062af6623248334a2e99',
    current_ast_sha256=digest(current_ast),base_ast_sha256=digest(base_ast),ast_equal=current_ast==base_ast,
    migration_equal=current_migrations==base_migrations,tx_current=tx_current,tx_base=tx_base,
    routes=len(routes),clients=len(clients),compatibility_source_facts_equal=facts==base_facts,
    compatibility_closure_equal=actual_closure==base_closure,compat_current=compat_current,compat_base=compat_base,
    compatibility_closure_drift=diff)
for name,value in [('current-ast.json',current_ast),('base-ast.json',base_ast),('collector-comparison.json',summary)]:
    with (PRIVATE/name).open('xb') as file:file.write(canonical(value))
assert summary['ast_equal'] and summary['migration_equal']
assert summary['tx_current']==summary['tx_base']
assert summary['compatibility_source_facts_equal'] and summary['compatibility_closure_equal']
assert summary['compat_current']==summary['compat_base']
print(json.dumps(summary,indent=2))
