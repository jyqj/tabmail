#!/usr/bin/env python3
"""Explicit-budget R5 benchmark orchestration; no scale reduction or implicit DB run."""
from __future__ import annotations
import argparse
import hashlib
import json
import os
import math
import re
from pathlib import Path
import platform
import shutil
import signal
import subprocess
import sys
import time

ROOT=Path(__file__).resolve().parents[1]
DATASET=ROOT/'docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'
SCALES={'S':(100,500,100000),'M':(1000,5000,1000000),'L':(1000,5000,10000000)}
WORKLOADS={'inbox_list','indexed_search','indexed_message','permission_mailboxes','draft_save','submit_and_loopback_worker','gc_reference_safety'}
METRICS={'hardware','source_sha','schema_version','dataset_fingerprint','seed','concurrency','pool_size','sql_count','sql_tracer_calibration','p50_ms','p95_ms','p99_ms','alloc_bytes','rss_bytes','disk_bytes','cache_mode','safety_assertions'}

def read_json(path):
 def pairs(items):
  out={}
  for k,v in items:
   if k in out:raise ValueError('duplicate JSON key')
   out[k]=v
  return out
 def constant(value):raise ValueError('non-finite JSON number: '+value)
 def finite_float(value):
  number=float(value)
  if not math.isfinite(number):raise ValueError('non-finite JSON number')
  return number
 return json.loads(Path(path).read_text(),object_pairs_hook=pairs,parse_constant=constant,parse_float=finite_float)

def validate_contract(data):
 if type(data) is not dict:raise ValueError('dataset JSON object required')
 if data.get('schema_version')!=1 or data.get('task_complete') is not False or data.get('product_green') is not False:raise ValueError('explicit uncompleted benchmark contract required')
 if type(data.get('seed')) is not int or data['seed']!=3893945 or data.get('concurrency')!=20:raise ValueError('frozen seed/20 concurrency changed')
 if set(data.get('scales',{}))!=set(SCALES):raise ValueError('S/M/L scales required')
 for scale,size in SCALES.items():
  row=data['scales'][scale]
  if tuple(row.get(k) for k in ['employees','mailboxes','messages'])!=size:raise ValueError('original S/M/L population cannot be reduced')
  if type(row.get('budget_seconds')) is not int or row['budget_seconds']<=0 or type(row.get('disk_budget_gib')) is not int or row['disk_budget_gib']<=0:raise ValueError('scale resource budget missing')
 if data.get('message_sizes')!=[{'bytes':4096,'weight_per_1000':800},{'bytes':32768,'weight_per_1000':180},{'bytes':262144,'weight_per_1000':19},{'bytes':2097152,'weight_per_1000':1}]:raise ValueError('message-size distribution changed')
 if set(data.get('workloads',[]))!=WORKLOADS or len(data['workloads'])!=len(WORKLOADS):raise ValueError('all real workloads required')
 if set(data.get('required_metrics',[]))!=METRICS:raise ValueError('required actual metrics cannot be omitted')
 if data.get('candidate_thresholds')!={'S_list_p95_ms':300,'M_list_p95_ms':500,'M_index_ready_query_p95_ms':1000,'unexplained_regression_percent':10,'attachment_and_public_SMTP':'not_list_latency_threshold'}:raise ValueError('original candidate thresholds changed')
 if data.get('population')!={'tenant_ratio':[80,20],'personal_per_employee':1,'shared_per_employee':4,'shared_grants_per_mailbox':4,'lifecycle':{'active_inbox_percent':80,'archived_percent':10,'trash_past_purge_percent':5,'shared_hard_expired_percent':5},'historical_personal_permanent':'never reinterpret anomalous expiry as shared finite retention'}:raise ValueError('ACL/lifecycle population changed')
 if data.get('cache_modes')!=['app_cold_db_os_unspecified','app_hot_db_os_unspecified']:raise ValueError('cannot invent complete DB/OS cold cache')
 if data['scales']['L'].get('execution')!='not_requested_this_batch' or not data['scales']['L'].get('release_necessity'):raise ValueError('L budget/release necessity required without implicit run')
 return data

def source_closure():
 paths=[p for root in ['internal','cmd','web/lib'] for p in (ROOT/root).rglob('*') if p.is_file() and p.suffix in {'.go','.ts','.yaml','.sql'}]
 paths += [DATASET,ROOT/'scripts/run_r5_benchmark.py',ROOT/'scripts/tests/test_r5_benchmark.py']
 return {str(p.relative_to(ROOT)):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(set(paths))}

def preflight(contract,scale,directory):
 validate_contract(contract)
 if scale not in SCALES:raise ValueError('unknown scale')
 free=shutil.disk_usage(directory).free;required=contract['scales'][scale]['disk_budget_gib']*(1<<30)
 if free<required+(200*(1<<30) if scale=='L' else 10*(1<<30)):raise ValueError('insufficient disk safety margin')
 return {'status':'budget_preflight_only','scale':scale,'population':SCALES[scale],'free_disk_bytes':free,'disk_budget_bytes':required,'budget_seconds':contract['scales'][scale]['budget_seconds'],'hardware':{'platform':platform.platform(),'machine':platform.machine(),'logical_cpu':os.cpu_count()},'task_complete':False,'product_green':False,'actual_scale_run':False}

RESOURCE_SCOPE={
 'sql':'application_and_worker_pool_not_server_trigger_sql',
 'memory':'go_alloc_and_process_high_water_rss_not_pg_redis_system',
 'disk':'owned_raw_objects_plus_owned_pg_database_bytes',
 'gc':'raw_reference_read_only_not_sweep_throughput',
 'cache':'app_reset_and_reused_no_os_db_cold_claim',
}

def sha256_string(value):
 return type(value) is str and re.fullmatch('[0-9a-f]{64}',value) is not None

def validate_observations(result,size,scale,expected_source):
 employees,mailboxes,messages=size
 distribution=result.get('actual_distribution')
 if type(distribution) is not dict:raise ValueError('observed dataset distribution required')
 expected={'employees':employees,'mailboxes':mailboxes,'messages':messages,'index_ready_count':messages,
 'personal_mailboxes':employees,'shared_mailboxes':employees*4,'shared_grant_rows':employees*16,
 'min_distinct_shared_grantees':4,'max_distinct_shared_grantees':4,
 'lifecycle_counts':{'inbox':messages*80//100,'archived':messages*10//100,'trash_expired':messages*5//100,'hard_expired':messages*5//100},
 'mime_size_counts':{'4096':messages*800//1000,'32768':messages*180//1000,'262144':messages*19//1000,'2097152':messages//1000},
 'tenant_message_counts':{'bench-0.test':messages*80//100,'bench-1.test':messages*20//100}}
 if distribution!=expected:raise ValueError('actual database populations/lifecycle/MIME/ACL differ from frozen dataset')
 def integers(value):
  return all(integers(v) for v in value.values()) if type(value) is dict else type(value) is int
 if not integers(distribution):raise ValueError('dataset count must be an integer, not boolean or float')
 fingerprint=result.get('dataset_fingerprint');parameter=result.get('parameter_fingerprint')
 if not sha256_string(fingerprint) or not sha256_string(parameter) or fingerprint==parameter:raise ValueError('distinct actual streaming and parameter SHA256 fingerprints required')
 if result.get('resource_scope')!=RESOURCE_SCOPE or result.get('system_memory_observation')!='not_measured':raise ValueError('actual metric boundaries required; no system/cold/GC claims')
 plans=result.get('sql_plans')
 if type(plans) is not list or not plans:raise ValueError('captured shipping SQL plans required')
 for plan in plans:
  if type(plan) is not dict or plan.get('origin')!='captured_shipping_query_same_parameters' or plan.get('workload') not in WORKLOADS:raise ValueError('shipping query plan provenance required')
  if type(plan.get('schema_version')) is not int or type(plan.get('index_ready_count')) is not int:raise ValueError('integer plan schema/count observations required')
  if plan.get('scale')!=scale or plan.get('source_sha')!=expected_source or plan.get('schema_version')!=result.get('schema_version') or plan.get('index_ready_count')!=messages:raise ValueError('query plan source/schema/population mismatch')
  if not sha256_string(plan.get('query_sha256')) or not sha256_string(plan.get('parameters_sha256')):raise ValueError('captured query and parameters digests required')
  raw=plan.get('plan')
  if type(raw) is not list or not raw or any(type(node) is not dict or type(node.get('Plan')) is not dict or not node['Plan'] for node in raw):raise ValueError('actual JSON EXPLAIN plan required')
 if not any(plan['workload']=='inbox_list' for plan in plans):raise ValueError('representative actual shipping inbox plan required')

def validate_tool_result(result,contract,expected_source):
 validate_contract(contract)
 if type(result) is not dict or result.get('mode')!='TOOL_ONLY_DATASET_CALIBRATION' or result.get('scale')!='tool_only' or result.get('S_M_L_executed') is not False:raise ValueError('tool-only evidence cannot certify S/M/L')
 if tuple(result.get(k) for k in ['employees','mailboxes','messages'])!=(20,100,1000) or result.get('source_sha')!=expected_source:raise ValueError('wrong tool-only source/population')
 if result.get('dataset_sha256')!=hashlib.sha256(DATASET.read_bytes()).hexdigest() or type(result.get('schema_version')) is not int or result['schema_version']<15:raise ValueError('tool dataset/schema mismatch')
 if result.get('task_complete') is not False or result.get('product_green') is not False:raise ValueError('tool-only evidence cannot approve a task/product')
 validate_observations(result,(20,100,1000),'tool_only',expected_source)
 return {'status':'tool_only_observations_validated','S_M_L_executed':False,'task_complete':False,'product_green':False}

def validate_result(result,contract,scale,expected_source):
 validate_contract(contract)
 if type(result) is not dict:raise ValueError('result JSON object required')
 if result.get('scale')!=scale or tuple(result.get(k) for k in ['employees','mailboxes','messages'])!=SCALES[scale] or result.get('source_sha')!=expected_source:raise ValueError('wrong scale/source evidence')
 if result.get('seed')!=contract['seed'] or result.get('concurrency')!=20:raise ValueError('wrong seed/concurrency')
 if result.get('dataset_sha256')!=hashlib.sha256(DATASET.read_bytes()).hexdigest():raise ValueError('dataset contract hash mismatch')
 if result.get('index_ready_count')!=SCALES[scale][2]:raise ValueError('index-ready prerequisite not satisfied')
 if result.get('sql_tracer_calibration_count')!=20 or result.get('sql_tracer_calibration_expected')!=20:raise ValueError('real SQL tracer calibration required')
 if not isinstance(result.get('hardware'),dict) or not result['hardware'] or type(result.get('schema_version')) is not int or result['schema_version']<15 or not result.get('dataset_fingerprint') or not result.get('safety_assertions'):raise ValueError('actual hardware/schema/fingerprint/safety observations required')
 if not result.get('safety_assertions_passed') or result.get('task_complete') is not False or result.get('product_green') is not False:raise ValueError('safety/policy evidence is not overall task approval')
 hardware=result['hardware']
 if not isinstance(hardware.get('os'),str) or not hardware['os'] or not isinstance(hardware.get('arch'),str) or not hardware['arch'] or type(hardware.get('cpu')) is not int or hardware['cpu']<=0:raise ValueError('observed runtime hardware required')
 if result.get('pool_size')!=24:raise ValueError('frozen actual application pool size required')
 safety={w+':foreign_and_revoked_current_source_denied' for w in WORKLOADS}
 if type(result.get('safety_assertions')) is not list or len(result['safety_assertions'])!=len(safety) or any(type(x) is not str for x in result['safety_assertions']) or set(result['safety_assertions'])!=safety:raise ValueError('each shipping workload safety control required')
 validate_observations(result,SCALES[scale],scale,expected_source)
 rows=result.get('measurements',[])
 if type(rows) is not list or any(type(r) is not dict for r in rows):raise ValueError('measurement objects required')
 required={(w,c) for w in WORKLOADS for c in contract['cache_modes']}
 if {(r.get('workload'),r.get('cache_mode')) for r in rows}!=required or len(rows)!=len(required):raise ValueError('missing/duplicate real cold/hot workloads')
 for row in rows:
  if type(row.get('samples')) is not int or row['samples']!=200 or type(row.get('sql_count')) is not int or row['sql_count']<=0:raise ValueError('zero/unknown samples or SQL count')
  if not all(type(row.get(k)) in {int,float} for k in ['p50_ms','p95_ms','p99_ms']):raise ValueError('numeric percentiles required')
  if not row['p50_ms']<=row['p95_ms']<=row['p99_ms']:raise ValueError('ordered percentiles required')
  for k in ['p50_ms','p95_ms','p99_ms','alloc_bytes','rss_bytes','disk_bytes']:
   if type(row.get(k)) not in {int,float} or not math.isfinite(row[k]) or row[k]<0 or (k in {'rss_bytes','disk_bytes'} and row[k]==0):raise ValueError('missing actual latency/memory/disk metrics')
 thresholds=[]
 for row in rows:
  limit=contract['candidate_thresholds'][scale+'_list_p95_ms'] if row['workload']=='inbox_list' else contract['candidate_thresholds']['M_index_ready_query_p95_ms'] if scale=='M' and row['workload']=='indexed_message' else None
  if limit is not None:thresholds.append({'workload':row['workload'],'cache_mode':row['cache_mode'],'p95_ms':row['p95_ms'],'candidate_limit_ms':limit,'passed':row['p95_ms']<=limit})
 return {'status':'actual_scale_evidence_validated','task_complete':False,'product_green':False,'scale':scale,'candidate_thresholds':thresholds,'candidate_threshold_passed':all(x['passed'] for x in thresholds),'relative_regression':'no_reference_baseline_yet_not_certified'}

def run_owned_process(command,*,env,stdout,stderr,timeout):
 process=subprocess.Popen(command,cwd=ROOT,env=env,stdout=stdout,stderr=stderr,start_new_session=True)
 try:return process.wait(timeout=timeout)
 except subprocess.TimeoutExpired:
  os.killpg(process.pid,signal.SIGTERM)
  try:process.wait(timeout=5)
  except subprocess.TimeoutExpired:os.killpg(process.pid,signal.SIGKILL);process.wait()
  raise ValueError('budget timeout: owned process group terminated; original output preserved')

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--scale',choices=SCALES,default='S');parser.add_argument('--output-dir',type=Path);parser.add_argument('--execute-approved-budget',action='store_true');parser.add_argument('--source-sha');parser.add_argument('--validate-tool-result',type=Path);args=parser.parse_args()
 try:
  contract=validate_contract(read_json(DATASET))
  if args.validate_tool_result:
   if args.execute_approved_budget or not args.source_sha or not re.fullmatch('[0-9a-f]{40}',args.source_sha):raise ValueError('tool evidence validation requires explicit source and never executes a benchmark')
   print(json.dumps(validate_tool_result(read_json(args.validate_tool_result),contract,args.source_sha)));return 0
  report=preflight(contract,args.scale,Path(os.getenv('TMPDIR','/tmp')))
  if not args.execute_approved_budget:print(json.dumps(report,ensure_ascii=False,indent=2));return 0
  if args.scale=='L':raise ValueError('L execution is not authorized in this batch')
  if not args.output_dir or not args.source_sha or not re.fullmatch('[0-9a-f]{40}',args.source_sha) or not os.getenv('TABMAIL_TEST_DB_DSN'):raise ValueError('explicit fresh output/source identity and owned DSN required')
  out=args.output_dir.resolve();out.mkdir(mode=0o700,parents=True,exist_ok=False);before=source_closure()
  env=dict(os.environ,TABMAIL_R5_BENCHMARK_SCALE=args.scale,TABMAIL_R5_BENCHMARK_OUTPUT=str(out/'result.json'),TABMAIL_R5_BENCHMARK_SOURCE_SHA=args.source_sha)
  command=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout='+str(report['budget_seconds'])+'s','./internal/store/postgres','-run','^TestR5BenchmarkOriginalScale$']
  (out/'source-closure.json').write_text(json.dumps({'declared_source_sha':args.source_sha,'files':before,'boundary':'Exact benchmark/product file hashes captured before execution; declared source label is separately recorded, not inferred from a git HEAD with untracked benchmark files'},sort_keys=True,indent=2)+'\n')
  (out/'preflight.json').write_text(json.dumps(report,indent=2)+'\n');(out/'command.json').write_text(json.dumps(command)+'\n')
  with (out/'go.jsonl').open('w') as stdout,(out/'go.stderr').open('w') as stderr:returncode=run_owned_process(command,env=env,stdout=stdout,stderr=stderr,timeout=report['budget_seconds']+30)
  (out/'go.exit').write_text(str(returncode)+'\n')
  if returncode!=0 or source_closure()!=before:raise ValueError('benchmark failed or source drifted; original output preserved')
  events=[json.loads(line) for line in (out/'go.jsonl').read_text().splitlines() if line.strip()]
  if [e['Action'] for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e['Action'] in ['run','pass','fail','skip']]!=['run','pass']:raise ValueError('scale test did not actually run/pass')
  result=validate_result(read_json(out/'result.json'),contract,args.scale,args.source_sha);print(json.dumps(result));return 0
 except (ValueError,KeyError,OSError,subprocess.SubprocessError) as exc:print(json.dumps({'status':'rejected','task_complete':False,'product_green':False,'error':str(exc)}));return 1
if __name__=='__main__':raise SystemExit(main())
