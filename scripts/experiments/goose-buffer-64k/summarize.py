#!/usr/bin/env python3
"""Summarize frozen parser outputs and complete wholePG lifecycles without reruns."""
import argparse
import hashlib
import json
from pathlib import Path
import re

p=argparse.ArgumentParser();p.add_argument('--root',type=Path,required=True);p.add_argument('--output',type=Path,required=True);p.add_argument('--baseline-cwd-error',action='store_true',help='Annotate the historical first run cwd error; never enable for a correctly prepared replay')
a=p.parse_args();r=a.root;o=a.output;o.mkdir(parents=True,exist_ok=True)
def sha(b):return hashlib.sha256(b).hexdigest()
def write(name,obj):(o/name).write_text(json.dumps(obj,indent=2)+'\n')
b=(r/'baseline-parsed.jsonl').read_bytes();c=(r/'candidate-parsed.jsonl').read_bytes()
assert b==c,'STOP: parser semantic counterexample'
rows=[json.loads(x) for x in b.splitlines()]
compact=[]
for x in rows:
 e=x['Error'];statements=x['Statements']
 compact.append({'id':x['ID'],'direction':x['Direction'],'use_tx':x['UseTx'],
 'statements':None if statements is None else [{'utf8_bytes':len(s.encode()),'sha256':sha(s.encode())} for s in statements],
 'error_bytes':len(e.encode()),'error_sha256':sha(e.encode()),'error_prefix':e[:140],
 **({'ends_with_semicolon':x['EndSemicolon']} if 'EndSemicolon' in x else {})})
write('parser-cases.json',compact)
write('parser-summary.json',{'comparison':'full raw JSONL bytewise equality before hashing','equal':True,
 'raw_bytes':len(b),'sha256':sha(b),'result_rows':len(rows),'migration_files':19,
 'migration_direction_rows':sum('.sql/' in x['ID'] for x in rows),'error_rows':sum(bool(x['Error']) for x in rows),
 'frozen_parser_environment':{'GODEBUG':'asynctimerchan=0','GOOSE_EXPERIMENT_VALUE':'FROZEN_NON_SECRET','all_other_variables':'absent'},
 'baseline':json.loads((r/'baseline-diag-summary.json').read_text()),'candidate':json.loads((r/'candidate-diag-summary.json').read_text()),
 'allocation_scope':'one serial parse of 19 migrations in both directions under race; not wholePG improvement'})
inventory=(r/'baseline-tests.txt').read_text().splitlines();assert inventory==(r/'candidate-tests.txt').read_text().splitlines()
for arm in ('baseline','candidate'):
 raw=(r/(arm+'-wholepg.jsonl')).read_bytes();events=[json.loads(x) for x in raw.splitlines()]
 started={x['Test'] for x in events if x['Action']=='run' and 'Test' in x}
 terminal={x['Test']:x['Action'] for x in events if x['Action'] in ('pass','skip','fail') and 'Test' in x}
 fail=[x for x in events if x['Action']=='fail' and 'Test' in x]
 failures=[]
 for x in fail:
  name=x['Test'];out=''.join(e.get('Output','') for e in events if e.get('Test')==name and e['Action']=='output')
  failures.append({'test':name,'elapsed':x.get('Elapsed'),'output':out})
 timedout=any('test timed out after' in x.get('Output','') for x in events)
 run=json.loads((r/(arm+'-run.json')).read_text())
 # Historical baseline cwd error is recorded explicitly; never hide or rerun it.
 if arm=='baseline' and a.baseline_cwd_error:run['working_directory']='/workspace/tabmail';run['runner_error']='precompiled binary executed from repo root instead of package directory'
 result={**run,'test_inventory':len(inventory),'top_level_started':sum(t in started for t in inventory),
 'all_test_and_subtest_started':len(started),'passed':sum(v=='pass' for v in terminal.values()),
 'skipped':[t for t,v in terminal.items() if v=='skip'],'failed':failures,
 'unstarted_top_level_ids':[t for t in inventory if t not in started],
 'started_without_terminal_ids':sorted(started-terminal.keys()),'timeout':timedout,
 'package_terminals':[x for x in events if x['Action'] in ('pass','fail','skip') and 'Test' not in x],
 'raw_log_sha256':sha(raw),'wholepg_product_qualified':False,'baseline_candidate_comparable':not a.baseline_cwd_error}
 write(arm+'-result.json',result)
 # Local-only DSNs are nonsecret, but remove URI literals from public logs.
 public=re.sub(r'postgres(?:ql)?://[^\s"\\]+','<OWNED_LOOPBACK_DSN>',raw.decode()).encode()
 (o/(arm+'-wholepg.jsonl')).write_bytes(public)
 write(arm+'-log-redaction.json',{'original_sha256':sha(raw),'public_sha256':sha(public),
 'rule':'replace postgres/postgresql URI literals with OWNED_LOOPBACK_DSN; all fixture data synthetic; no other content changes'})
 for suffix in ('wholepg.stderr','settings.txt','databases-after.txt','compile.log'):
  (o/(arm+'-'+suffix)).write_bytes((r/(arm+'-'+suffix)).read_bytes())
write('inventory.json',inventory)
print('Preserved all failures, skips, unfinished and unstarted IDs. No product qualification granted.')
