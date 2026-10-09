#!/usr/bin/env python3
"""Explicit-budget R5 benchmark orchestration; no scale reduction or implicit DB run."""
from __future__ import annotations
import argparse
import hashlib
from datetime import datetime
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
import tarfile
import time
import importlib.util
_inventory_spec = importlib.util.spec_from_file_location("r5_source_inventory", Path(__file__).with_name("r5_source_inventory.py"))
source_inventory = importlib.util.module_from_spec(_inventory_spec)
_inventory_spec.loader.exec_module(source_inventory)

ROOT=Path(__file__).resolve().parents[1]
DATASET=ROOT/'docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'
LOOKAHEAD_METHOD_FILE=ROOT/'docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json'
LOOKAHEAD_METHOD='lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1'
DEFAULT_METHOD='bounded_store_claim_complete_window_v1'
METHOD_ENV='TABMAIL_R5_BENCHMARK_PREPARATION_METHOD'
METHOD_SHA_ENV='TABMAIL_R5_BENCHMARK_METHOD_CONTRACT_SHA256'
LOOKAHEAD_DATASET_SHA256='9e81020680dbd6e990cbb35ab1aac16413885e65306d949a5889c4bafda00b36'
BUDGETS={'S':(1800,10),'M':(10800,80),'L':(43200,450)}
SCALES={'S':(100,500,100000),'M':(1000,5000,1000000),'L':(1000,5000,10000000)}
# The only admitted pre-pipeline success is its immutable, independently
# archived S2 source + contract pair; schema15 by itself is not an exemption.
FROZEN_S2=('68cafcc03672edff848b9fd07abd95c34944a35c','0b828ec6dda5de423968115ad256746702f81d3011a40ffb2540208943c94fd8',15)
PIPELINE_SCHEMA_VERSION=16
WORKLOADS={'inbox_list','indexed_search','indexed_message','permission_mailboxes','draft_save','submit_and_loopback_worker','gc_reference_safety'}
METRICS={'hardware','source_sha','schema_version','dataset_fingerprint','seed','concurrency','pool_size','sql_count','sql_tracer_calibration','p50_ms','p95_ms','p99_ms','alloc_bytes','rss_bytes','disk_bytes','cache_mode','safety_assertions'}

def read_json(path):
 return parse_json(Path(path).read_text())

def parse_json(payload):
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
 return json.loads(payload,object_pairs_hook=pairs,parse_constant=constant,parse_float=finite_float)

def strict_frozen_value(value):
 if type(value) is dict:return all(strict_frozen_value(v) for v in value.values())
 if type(value) is list:return all(strict_frozen_value(v) for v in value)
 return type(value) in {int,str}

def validate_contract(data,*,frozen_legacy=False):
 if type(data) is not dict:raise ValueError('dataset JSON object required')
 if type(data.get('schema_version')) is not int or data.get('schema_version')!=1 or data.get('task_complete') is not False or data.get('product_green') is not False:raise ValueError('explicit uncompleted benchmark contract required')
 if type(data.get('seed')) is not int or data['seed']!=3893945 or type(data.get('concurrency')) is not int or data.get('concurrency')!=20:raise ValueError('frozen seed/20 concurrency changed')
 if type(data.get('scales')) is not dict or set(data.get('scales',{}))!=set(SCALES):raise ValueError('S/M/L scales required')
 for scale,size in SCALES.items():
  row=data['scales'][scale]
  require_integer_fields(row,['employees','mailboxes','messages'])
  if tuple(row.get(k) for k in ['employees','mailboxes','messages'])!=size:raise ValueError('original S/M/L population cannot be reduced')
  if (row.get('budget_seconds'),row.get('disk_budget_gib'))!=BUDGETS[scale]:raise ValueError('original wall-time and disk budgets cannot be changed')
  if type(row.get('budget_seconds')) is not int or row['budget_seconds']<=0 or type(row.get('disk_budget_gib')) is not int or row['disk_budget_gib']<=0:raise ValueError('scale resource budget missing')
 if type(data.get('message_sizes')) is not list:raise ValueError('message-size list required')
 for row in data.get('message_sizes',[]):require_integer_fields(row,['bytes','weight_per_1000'])
 if any(not strict_frozen_value(data.get(k)) for k in ['message_sizes','population','candidate_thresholds']):raise ValueError('frozen population/size/threshold values require exact types')
 if data.get('message_sizes')!=[{'bytes':4096,'weight_per_1000':800},{'bytes':32768,'weight_per_1000':180},{'bytes':262144,'weight_per_1000':19},{'bytes':2097152,'weight_per_1000':1}]:raise ValueError('message-size distribution changed')
 if set(data.get('workloads',[]))!=WORKLOADS or len(data['workloads'])!=len(WORKLOADS):raise ValueError('all real workloads required')
 if set(data.get('required_metrics',[]))!=METRICS:raise ValueError('required actual metrics cannot be omitted')
 if data.get('candidate_thresholds')!={'S_list_p95_ms':300,'M_list_p95_ms':500,'M_index_ready_query_p95_ms':1000,'unexplained_regression_percent':10,'attachment_and_public_SMTP':'not_list_latency_threshold'}:raise ValueError('original candidate thresholds changed')
 if data.get('population')!={'tenant_ratio':[80,20],'personal_per_employee':1,'shared_per_employee':4,'shared_grants_per_mailbox':4,'lifecycle':{'active_inbox_percent':80,'archived_percent':10,'trash_past_purge_percent':5,'shared_hard_expired_percent':5},'historical_personal_permanent':'never reinterpret anomalous expiry as shared finite retention'}:raise ValueError('ACL/lifecycle population changed')
 if data.get('cache_modes')!=['app_cold_db_os_unspecified','app_hot_db_os_unspecified']:raise ValueError('cannot invent complete DB/OS cold cache')
 if 'clock_contract' in data and (data['clock_contract']!=CLOCK_CONTRACT or not strict_frozen_value(data['clock_contract'])):raise ValueError('frozen actual clock observation contract changed')
 if frozen_legacy:
  if any(k in data for k in ['preparation_contract','clock_contract']):raise ValueError('frozen legacy contract cannot be backfilled with new protocol')
 elif data.get('preparation_contract')!=PREPARATION_CONTRACT or not strict_frozen_value(data['preparation_contract']):raise ValueError('explicit frozen current preparation contract required')
 if not frozen_legacy:
  evidence=data.get('required_evidence');required={'actual_distribution','parameter_fingerprint','dataset_fingerprint','sql_plans','resource_scope','system_memory_observation','preparation','preparation_checkpoints','fingerprint_method','fingerprint_streams'}
  if type(evidence) is not list or any(type(k) is not str for k in evidence) or len(evidence)!=len(required) or set(evidence)!=required:raise ValueError('all known actual preparation/distribution/fingerprint evidence required')
 if data['scales']['L'].get('execution')!='not_requested_this_batch' or not data['scales']['L'].get('release_necessity'):raise ValueError('L budget/release necessity required without implicit run')
 return data

def current_source_receipt(*, policy, build_context, preparation_method=DEFAULT_METHOD):
 if preparation_method != DEFAULT_METHOD:
  raise ValueError('current v2 lookahead blocked: frozen METHOD binds legacy files-only SHA1; independent method migration required')
 return source_inventory.capture_current_source(ROOT,purpose='benchmark',policy=policy,build_context=build_context)

def source_closure(*,preparation_method=DEFAULT_METHOD,policy=None,build_context=None):
 return current_source_receipt(policy=policy,build_context=build_context,preparation_method=preparation_method)['files']


def observed_git_head():
 result=subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True,text=True)
 head=result.stdout.strip()
 return head if result.returncode==0 and re.fullmatch('[0-9a-f]{40}',head) else None

def preflight(contract,scale,directory,*,preparation_method=DEFAULT_METHOD):
 validate_contract(contract)
 if scale not in SCALES:raise ValueError('unknown scale')
 if preparation_method not in {DEFAULT_METHOD,LOOKAHEAD_METHOD} or preparation_method==LOOKAHEAD_METHOD and scale not in {'S','M'}:raise ValueError('lookahead method is explicitly authorized for S/M only; L remains unauthorized')
 free=shutil.disk_usage(directory).free;required=contract['scales'][scale]['disk_budget_gib']*(1<<30)
 if free<required+(200*(1<<30) if scale=='L' else 10*(1<<30)):raise ValueError('insufficient disk safety margin')
 return {'status':'budget_preflight_only','scale':scale,'population':SCALES[scale],'free_disk_bytes':free,'disk_budget_bytes':required,'budget_seconds':contract['scales'][scale]['budget_seconds'],'hardware':{'platform':platform.platform(),'machine':platform.machine(),'logical_cpu':os.cpu_count()},'task_complete':False,'product_green':False,'actual_scale_run':False}

LATENCY_CLOCK='wall_elapsed_cross_checked_monotonic_gap_le_100ms'
CLOCK_CONTRACT={'latency_clock':LATENCY_CLOCK,'max_wall_monotonic_gap_ms':100,'run_deadline':'absolute_wall_time_includes_host_suspend','resume_policy':'reject_observed_deadline_overrun_cleanup_after_resume_no_sleep_time_kill_claim'}

RESOURCE_SCOPE={
 'sql':'application_and_worker_pool_not_server_trigger_sql',
 'memory':'go_alloc_and_process_high_water_rss_not_pg_redis_system',
 'disk':'owned_raw_objects_plus_owned_pg_database_bytes',
 'gc':'raw_reference_read_only_not_sweep_throughput',
 'cache':'app_reset_and_reused_no_os_db_cold_claim',
}

def sha256_string(value):
 return type(value) is str and re.fullmatch('[0-9a-f]{64}',value) is not None

def expected_schema_version():
 versions=[int(p.name.split('_',1)[0]) for p in (ROOT/'internal/store/postgres/migrations').glob('[0-9]*_*.sql')]
 if not versions:raise ValueError('actual migration source missing')
 return max(versions)

def require_integer_fields(value,fields):
 if type(value) is not dict:raise ValueError('JSON object with integer observations required')
 if any(type(value.get(k)) is not int for k in fields):raise ValueError('integer observations required; floats and booleans are not counts')

def reject_unknown_sampling(value):
 if type(value) is dict:
  for key,item in value.items():
   if 'sampling' in key.lower() or key in {'sample_mode','sample_method'}:raise ValueError('unknown sampling protocol declaration')
   reject_unknown_sampling(item)
 elif type(value) is list:
  for item in value:reject_unknown_sampling(item)

def plan_reads_messages(value):
 if type(value) is dict:return value.get('Relation Name')=='messages' or any(plan_reads_messages(v) for v in value.values())
 if type(value) is list:return any(plan_reads_messages(v) for v in value)
 return False

def validate_explain_plan(raw,relation):
 # Relations count only in the PostgreSQL Plan/Plans tree, not arbitrary JSON.
 if type(raw) is not list or len(raw)!=1 or type(raw[0]) is not dict or type(raw[0].get('Plan')) is not dict:raise ValueError('actual singleton JSON EXPLAIN Plan tree required')
 if any(k.startswith('Actual ') or k in {'Execution Time','Planning Time'} for k in raw[0]):raise ValueError('EXPLAIN estimates cannot masquerade as execution measurements')
 def node(value):
  if type(value) is not dict or type(value.get('Node Type')) is not str or not value['Node Type']:raise ValueError('actual EXPLAIN node type required')
  if any(k.startswith('Actual ') or k in {'Execution Time','Planning Time'} for k in value):raise ValueError('preparation EXPLAIN cannot claim ANALYZE execution timing')
  children=value.get('Plans',[])
  if type(children) is not list:raise ValueError('actual EXPLAIN child plans required')
  found=value.get('Relation Name')==relation
  for child in children:found=node(child) or found
  return found
 if not node(raw[0]['Plan']):raise ValueError('actual EXPLAIN does not read '+relation+' relation')

PREPARATION_COUNTS={'joined_inflight','parser_join_reserve_seconds','parser_joined_inflight','parser_joined_calls','parser_canceled_joined_calls','active_budget','pending_upper_bound','max_active','max_window_messages','joined_stored_messages','joined_completed_jobs','claim_calls','max_claim_batch','last_claim_batch','joined_ready_before_last_claim','sql_ready_count'}
PREPARATION_WALL={'parser_joined_call_wall_seconds_sum','company_wall_seconds','store_wall_seconds','claim_wall_seconds','last_claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds','max_wall_monotonic_gap_ms'}
PREPARATION_GUARD={'parser_min_start_remaining_wall_seconds','parser_original_deadline_unix_seconds'}
PREPARATION_FIELDS=PREPARATION_COUNTS|PREPARATION_WALL|PREPARATION_GUARD|{'method','phase','producer_done','final_empty_claim','claim_query_sha256','claim_parameters_sha256','parser_join_policy','parser_quiescent'}
PARSER_JOIN_POLICY='uncancelled_document_wait_with_31s_absolute_wall_admission_guard'
PREPARATION_CONTRACT={
 'method':'bounded_store_claim_complete_window_v1',
 'schema_version':16,
 'active_budget':20,
 'window_messages':20,
 'claim_requested_limit':20,
 'parser_concurrency':4,
 'parser_timeout_seconds':30,
 'parser_join_policy':PARSER_JOIN_POLICY,
 'parser_join_reserve_seconds':31,
 'checkpoint_policy':'before_claim_ready20_1000_10000_100000_each250000_Nminus20_then_terminalN',
 'maintenance':'no_explicit_analyze_or_planner_override',
 'fingerprint_method':'six_actual_canonical_sql_streams_v1',
}

INPUT_SCHEDULER_CONTRACT={
 'method':LOOKAHEAD_METHOD,
 'lookahead_limit':100,
 'buffer_scope':'unpersisted_inputs_only_no_database_job_backlog',
 'window_messages':20,
 'selection':'earliest_ordinal_mailbox_heads_distinct_first_then_earliest_remaining_sorted_ordinal',
 'mailbox_order':'generator_ordinal_fifo_predecessor_success_before_store_and_lifecycle',
 'fairness':'earliest_ordinal_head_selected_no_overtaking_within_mailbox',
 'failure_policy':'predecessor_error_stops_dependents_cancel_and_join_all_workers',
 'observer_dependency':'none_scheduler_explicit_opt_in_independent_of_diagnostic_observer',
}
IDENTITY_BOUNDARY={
 'generator_ordinal_mailbox_fifo_preserved':True,
 'A_random_commit_order_not_preserved_or_claimed':True,
 'received_at_and_received_order_may_change_with_B_scheduling':True,
 'all_persistent_equivalent':False,
 'protocol_UID_certified':False,
 'canonical_streams_unchanged_no_added_normalization':True,
 'all_physical_uuid_time_protocol_uid_fields_not_covered_by_six_streams':True,
 'uncovered_fields':'received_at/received-order, lifecycle clock timestamps, indexed_at, next_attempt_at, lease tokens, random actor/mailbox row UUIDs; protocol UID not certified',
}
LOOKAHEAD_CONTRACT={
 'schema_version':1,
 'task':'R5-P0-100',
 'task_complete':False,
 'product_green':False,
 'preparation_method':LOOKAHEAD_METHOD,
 'dataset_contract':'docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json',
 'dataset_contract_sha256':LOOKAHEAD_DATASET_SHA256,
 'authorized_original_scales':['S','M'],
 'tool_only_population':[20,100,1000],
 'source_binding':'selected_method_file_in_canonical_source_closure_sha1_not_git_tree_or_commit',
 'cli_binding':{'option':'--preparation-method','value':LOOKAHEAD_METHOD,'contract_option':'--method-contract','environment':METHOD_ENV,'contract_sha256_environment':METHOD_SHA_ENV,'default':DEFAULT_METHOD},
 'preparation_contract':{**PREPARATION_CONTRACT,'method':LOOKAHEAD_METHOD},
 'input_scheduler_contract':INPUT_SCHEDULER_CONTRACT,
 'preparation_identity_boundary':IDENTITY_BOUNDARY,
 'clock_contract':CLOCK_CONTRACT,
 'persistent_observation_policy':'actual_postgresql_uid_catalog_and_per_mailbox_received_at_ordinal_projection_not_scheduler_metadata',
 'six_stream_boundary':'only_original_six_defined_canonical_streams_no_normalized_digest_or_all_persistent_uid_timestamp_equivalence',
}
UID_CATALOG_SQL="SELECT count(*) FROM pg_attribute WHERE attrelid='messages'::regclass AND NOT attisdropped AND attnum>0 AND lower(attname) IN ('uid','imap_uid','message_uid')"
RECEIVED_ORDER_SQL='SELECT id,mailbox_id,subject,received_at FROM messages ORDER BY mailbox_id,received_at,id'
LOOKAHEAD_CONTRACT['persistent_query_sha256']={'uid_catalog_query_sha256':hashlib.sha256(UID_CATALOG_SQL.encode()).hexdigest(),'received_at_projection_query_sha256':hashlib.sha256(RECEIVED_ORDER_SQL.encode()).hexdigest()}
FROZEN_LOOKAHEAD_S_METHOD_SHA256='87c41f7a841399f25a9e7eb7017bee59c57d3d340d30d8569cad59f66bf30c19'
FROZEN_LOOKAHEAD_S_CONTRACT={**LOOKAHEAD_CONTRACT,'authorized_original_scales':['S']}
LOOKAHEAD_RESULT_FIELDS={'preparation_method','method_contract_sha256','preparation_input_scheduler','preparation_identity_boundary','preparation_persistent_observations'}
LOOKAHEAD_KNOWN_RESULT_FIELDS=LOOKAHEAD_RESULT_FIELDS|{'scale','employees','mailboxes','messages','seed','concurrency','pool_size','schema_version','source_sha','dataset_sha256','dataset_fingerprint','fingerprint_method','fingerprint_streams','parameter_fingerprint','actual_distribution','preparation','preparation_checkpoints','sql_plans','resource_scope','system_memory_observation','index_ready_count','sql_tracer_calibration_count','sql_tracer_calibration_expected','sql_count_scope','hardware','safety_assertions','safety_assertions_passed','measurements','cache_boundary','task_complete','product_green','mode','S_M_L_executed','rss_bytes','disk_bytes','latency_clock','execution_wall'}

def exact_frozen_equal(actual,expected):
 if type(actual) is not type(expected):return False
 if type(expected) is dict:return set(actual)==set(expected) and all(exact_frozen_equal(actual[k],v) for k,v in expected.items())
 if type(expected) is list:return len(actual)==len(expected) and all(exact_frozen_equal(a,b) for a,b in zip(actual,expected))
 return actual==expected

def validate_method_contract(contract,*,frozen_contract_digest=None):
 current=exact_frozen_equal(contract,LOOKAHEAD_CONTRACT)
 frozen_s=frozen_contract_digest==FROZEN_LOOKAHEAD_S_METHOD_SHA256 and exact_frozen_equal(contract,FROZEN_LOOKAHEAD_S_CONTRACT)
 if frozen_contract_digest==FROZEN_LOOKAHEAD_S_METHOD_SHA256 and not frozen_s:raise ValueError('historical S digest cannot be backfilled with current M contract')
 if not current and not frozen_s:raise ValueError('exact known current or explicitly digest-bound historical S-only lookahead contract required')
 return contract

def validate_method_selection(result,preparation_method,method_contract,method_contract_digest,*,frozen_legacy=False):
 if preparation_method==DEFAULT_METHOD:
  if method_contract is not None or method_contract_digest is not None or LOOKAHEAD_RESULT_FIELDS.intersection(result):raise ValueError('default or historical v1 cannot carry lookahead contract or observations')
 elif preparation_method==LOOKAHEAD_METHOD:
  if frozen_legacy:raise ValueError('historical frozen S2 cannot be relabelled as lookahead')
  if set(result)-LOOKAHEAD_KNOWN_RESULT_FIELDS:raise ValueError('unknown selected lookahead result fields')
  validate_method_contract(method_contract,frozen_contract_digest=method_contract_digest)
  if not sha256_string(method_contract_digest) or result.get('preparation_method')!=preparation_method or result.get('method_contract_sha256')!=method_contract_digest:raise ValueError('selected lookahead method and actual frozen contract digest must bind result')
  if result.get('dataset_sha256')!=LOOKAHEAD_DATASET_SHA256:raise ValueError('lookahead cannot replace original frozen dataset')
  if not exact_frozen_equal(result.get('preparation_identity_boundary'),IDENTITY_BOUNDARY):raise ValueError('explicit non-equivalence and UID boundary required')
 else:raise ValueError('unknown selected preparation method')


def fixture_mailbox_ordinal(ordinal,employees):
 second=(ordinal//20)%5==4
 personal=employees//5 if second else employees*4//5
 offset=employees*4 if second else 0
 return offset+personal+(ordinal//20)%(personal*4) if ordinal%20==19 else offset+(ordinal//5)%(personal*5)

def expected_scheduler_order(messages,employees):
 buffer=[];generated=0;emitted=[]
 while generated<messages or buffer:
  while generated<messages and len(buffer)<100:
   buffer.append(generated);generated+=1
  count=min(20,len(buffer));selected=[];seen=set()
  for i,ordinal in enumerate(buffer):
   box=fixture_mailbox_ordinal(ordinal,employees)
   if box not in seen:
    selected.append(i);seen.add(box)
    if len(selected)==count:break
  if len(selected)<count:
   for i in range(len(buffer)):
    if len(selected)==count:break
    if i not in selected:selected.append(i)
  selected.sort();chosen=set(selected)
  emitted.extend(buffer[i] for i in selected)
  buffer=[ordinal for i,ordinal in enumerate(buffer) if i not in chosen]
 return emitted

def validate_input_scheduler(result,messages,employees):
 stats=result.get('preparation_input_scheduler')
 counts={'lookahead_limit','generated_inputs','emitted_inputs','ordinal_fifo_completed_jobs','max_unpersisted_buffered_inputs','joined_fifo_inflight'}
 fields=counts|{'method','fifo_gate_call_elapsed_seconds_sum','actual_emitted_ordinal_order','actual_emitted_mailbox_ordinals','actual_emitted_window_unique_mailboxes'}
 if type(stats) is not dict or set(stats)!=fields:raise ValueError('all known actual input scheduler fields required')
 require_integer_fields(stats,counts)
 if stats['method']!=LOOKAHEAD_METHOD or stats['lookahead_limit']!=100 or any(stats[k]!=messages for k in ['generated_inputs','emitted_inputs','ordinal_fifo_completed_jobs']) or stats['joined_fifo_inflight']!=0 or not 1<=stats['max_unpersisted_buffered_inputs']<=100:raise ValueError('actual scheduler generated/emitted/FIFOcompleted N and joined buffer bounds required')
 elapsed=stats['fifo_gate_call_elapsed_seconds_sum']
 if type(elapsed) not in {int,float} or not math.isfinite(elapsed) or elapsed<0:raise ValueError('finite nonnegative actual FIFO gate call wall required')
 order=stats['actual_emitted_ordinal_order'];boxes=stats['actual_emitted_mailbox_ordinals'];windows=stats['actual_emitted_window_unique_mailboxes']
 if type(order) is not list or len(order)!=messages or any(type(n) is not int for n in order) or set(order)!=set(range(messages)):raise ValueError('actual emitted ordinals must be an exact integer permutation')
 if type(boxes) is not list or len(boxes)!=messages or any(type(n) is not int for n in boxes) or boxes!=[fixture_mailbox_ordinal(n,employees) for n in order]:raise ValueError('actual emitted mailbox ordinals must bind original generator mapping')
 if order!=expected_scheduler_order(messages,employees):raise ValueError('actual earliest ordinal head fairness and mailbox FIFO schedule required')
 unique=[len(set(boxes[i:i+20])) for i in range(0,messages,20)]
 if type(windows) is not list or len(windows)!=messages//20 or any(type(n) is not int or not 1<=n<=20 for n in windows) or windows!=unique:raise ValueError('actual emitted window mailbox diversity must match selected inputs')
 return stats

def validate_persistent_observations(result,messages,employees,expected_source):
 obs=result.get('preparation_persistent_observations')
 counts={'schema_version','uid_column_count','projected_messages','projected_mailboxes','ordinal_fifo_violations','index_ready_count'}
 fields=counts|{'origin','source_sha','uid_catalog_query_sha256','received_at_projection_query_sha256','source_identity_bindings_valid'}
 if type(obs) is not dict or set(obs)!=fields:raise ValueError('known actual PostgreSQL persistent observations required')
 require_integer_fields(obs,counts)
 projected=len({fixture_mailbox_ordinal(n,employees) for n in range(messages)})
 if obs['origin']!='actual_owned_pg_catalog_and_received_at_projection' or obs['source_sha']!=expected_source or obs['schema_version']!=result['schema_version'] or obs['uid_column_count']<0 or (obs['projected_messages'],obs['projected_mailboxes'],obs['ordinal_fifo_violations'],obs['index_ready_count'])!=(messages,projected,0,messages) or obs['source_identity_bindings_valid'] is not True:raise ValueError('actual PostgreSQL source-bound received order and ready observations required')
 if any(obs.get(k)!=v for k,v in LOOKAHEAD_CONTRACT['persistent_query_sha256'].items()):raise ValueError('exact actual PostgreSQL catalog and projection query digests required')
 return obs


def preparation_depths(messages):
 return sorted({d for d in [20,1000,10000,100000,messages-20,*range(250000,messages,250000)] if 0<d<messages})

def validate_preparation_stats(stats,messages,phase,depth,*,preparation_method=DEFAULT_METHOD):
 if type(stats) is not dict or set(stats)!=PREPARATION_FIELDS:raise ValueError('complete known preparation observation fields required')
 require_integer_fields(stats,PREPARATION_COUNTS)
 if any(stats[k]<0 for k in PREPARATION_COUNTS):raise ValueError('nonnegative preparation counts required')
 if stats['method']!=preparation_method or stats['phase']!=phase:raise ValueError('known legal fixture pipeline method/phase required')
 if stats['joined_inflight']!=0 or not sha256_string(stats['claim_query_sha256']) or stats['claim_parameters_sha256']!=hashlib.sha256(b'[20]').hexdigest():raise ValueError('joined inflight0 and actual legal20 claim digests required')
 if (stats['active_budget'],stats['pending_upper_bound'],stats['max_window_messages'],stats['max_claim_batch'])!=(20,20,20,20) or not 1<=stats['max_active']<=20:raise ValueError('preparation active/window/legal claim20 bounds required')
 if any(type(stats.get(k)) not in {int,float} or not math.isfinite(stats[k]) or stats[k]<0 for k in PREPARATION_WALL):raise ValueError('finite nonnegative preparation wall observations required')
 if stats['max_wall_monotonic_gap_ms']>100 or any(stats[k]<=0 for k in ['company_wall_seconds','store_wall_seconds','claim_wall_seconds','last_claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds']):raise ValueError('actual uninterrupted preparation phase wall required')
 if stats['last_claim_wall_seconds']>stats['claim_wall_seconds']+1e-9 or sum(stats[k] for k in ['store_wall_seconds','claim_wall_seconds','parse_complete_wall_seconds'])>stats['pipeline_wall_seconds']+0.001:raise ValueError('preparation phase wall inconsistent')
 if type(stats['producer_done']) is not bool or type(stats['final_empty_claim']) is not bool:raise ValueError('explicit preparation producer/terminal booleans required')
 terminal=phase=='terminal';joined=phase=='batch_joined'
 written=messages if terminal else depth+20
 completed=messages if terminal else depth+20 if joined else depth
 calls=messages//20+1 if terminal else completed//20
 last_depth=messages if terminal else depth if joined else depth-20
 if (stats['joined_stored_messages'],stats['joined_completed_jobs'],stats['claim_calls'],stats['last_claim_batch'],stats['joined_ready_before_last_claim'],stats['sql_ready_count'],stats['producer_done'],stats['final_empty_claim'])!=(written,completed,calls,0 if terminal else 20,last_depth,messages if terminal else 0,terminal,terminal):raise ValueError('preparation joined population/claim/producer/final-empty state inconsistent')
 if not 0<=written-completed<=20:raise ValueError('preparation observed backlog exceeds window20')
 if stats['parser_join_policy']!=PARSER_JOIN_POLICY or (stats['parser_join_reserve_seconds'],stats['parser_joined_inflight'],stats['parser_joined_calls'],stats['parser_canceled_joined_calls'])!=(31,0,completed,0) or stats['parser_quiescent'] is not True or stats['parser_joined_call_wall_seconds_sum']<=0:raise ValueError('actual formal Parser joined/drained/quiescent observations required')
 if stats['parser_joined_call_wall_seconds_sum']>20*stats['parse_complete_wall_seconds']+0.001:raise ValueError('summed formal Parser waiter wall exceeds actual20 active phase bound')
 if any(type(stats[k]) not in {int,float} or not math.isfinite(stats[k]) or stats[k]<=0 for k in PREPARATION_GUARD) or stats['parser_min_start_remaining_wall_seconds']<=31:raise ValueError('actual minimum parse-start remaining original wall must exceed31s')
 return stats

def validate_preparation(result,messages,scale,expected_source,*,preparation_method=DEFAULT_METHOD):
 terminal=validate_preparation_stats(result.get('preparation'),messages,'terminal',messages,preparation_method=preparation_method)
 budget=300 if scale=='tool_only' else BUDGETS[scale][0]
 if terminal['company_wall_seconds']+terminal['pipeline_wall_seconds']>budget:raise ValueError('preparation exceeds original absolute wall budget')
 if terminal['parser_min_start_remaining_wall_seconds']>budget:raise ValueError('actual parse admission cannot invent more than original wall budget')
 checkpoints=result.get('preparation_checkpoints');depths=preparation_depths(messages)
 if type(checkpoints) is not list or len(checkpoints)!=len(depths)+1:raise ValueError('all actual deep-ready and terminal preparation checkpoints required')
 fields={'origin','phase','source_sha','schema_version','maintenance','claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1','query_sha256','parameters_sha256','explain_only_not_actual_claim_latency','plan','preparation'}
 countfields={'schema_version','claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1'}
 previous=None;query=None
 for index,checkpoint in enumerate(checkpoints):
  final=index==len(depths);depth=messages if final else depths[index];phase='terminal' if final else 'before_claim'
  if type(checkpoint) is not dict or set(checkpoint)!=fields|({'actual_claim_joined'} if not final else set()):raise ValueError('complete known paired preparation checkpoint fields required')
  require_integer_fields(checkpoint,countfields)
  if checkpoint['origin']!='captured_preparation_claim_same_parameters_observed_heap' or checkpoint['phase']!=phase or checkpoint['source_sha']!=expected_source or checkpoint['schema_version']!=result['schema_version'] or checkpoint['maintenance']!='no_explicit_analyze_or_planner_override' or checkpoint['explain_only_not_actual_claim_latency'] is not True:raise ValueError('actual preparation trace/source/schema/no-maintenance boundary required')
  expected=(20,messages if final else depth+20,0 if final else 20,0,depth,depth,0,0)
  if tuple(checkpoint[k] for k in ['claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1'])!=expected:raise ValueError('actual source-bound heap/backlog/attempt observations inconsistent')
  if not sha256_string(checkpoint['query_sha256']) or not sha256_string(checkpoint['parameters_sha256']) or checkpoint['parameters_sha256']!=hashlib.sha256(b'[20]').hexdigest():raise ValueError('captured actual legal20 claim/parameter digests required')
  if query is not None and checkpoint['query_sha256']!=query:raise ValueError('preparation claim query changed across checkpoints')
  query=checkpoint['query_sha256'];validate_explain_plan(checkpoint['plan'],'mail_index_jobs')
  before=validate_preparation_stats(checkpoint['preparation'],messages,phase,depth,preparation_method=preparation_method)
  if before['parser_original_deadline_unix_seconds']!=terminal['parser_original_deadline_unix_seconds'] or before['parser_min_start_remaining_wall_seconds']>budget:raise ValueError('preparation must observe one unchanged original deadline')
  if (before['claim_query_sha256'],before['claim_parameters_sha256'])!=(query,checkpoint['parameters_sha256']):raise ValueError('preparation EXPLAIN does not pair with captured actual claim digests')
  if previous is not None and any(before[k]<previous[k]-1e-9 for k in ['company_wall_seconds','store_wall_seconds','claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds','max_wall_monotonic_gap_ms','parser_joined_call_wall_seconds_sum']):raise ValueError('preparation checkpoint cumulative wall regressed')
  if previous is not None and before['parser_min_start_remaining_wall_seconds']>previous['parser_min_start_remaining_wall_seconds']:raise ValueError('actual cumulative minimum remaining wall increased')
  if final:
   if before!=terminal:raise ValueError('terminal preparation differs from observed final SQL checkpoint')
   if previous is None or abs(before['claim_wall_seconds']-previous['claim_wall_seconds']-before['last_claim_wall_seconds'])>1e-9 or any(before[k]!=previous[k] for k in ['joined_stored_messages','joined_completed_jobs','store_wall_seconds','parse_complete_wall_seconds','parser_joined_calls','parser_joined_call_wall_seconds_sum','parser_min_start_remaining_wall_seconds']):raise ValueError('actual terminal empty claim must follow joined final window without new work')
   previous=before
  else:
   joined=validate_preparation_stats(checkpoint['actual_claim_joined'],messages,'batch_joined',depth,preparation_method=preparation_method)
   if joined['parser_original_deadline_unix_seconds']!=terminal['parser_original_deadline_unix_seconds'] or joined['parser_min_start_remaining_wall_seconds']>before['parser_min_start_remaining_wall_seconds']:raise ValueError('joined Parser actual original-deadline/minimum observation inconsistent')
   if (joined['claim_query_sha256'],joined['claim_parameters_sha256'])!=(query,checkpoint['parameters_sha256']):raise ValueError('joined actual claim differs from same-parameter preparation EXPLAIN')
   if abs(joined['claim_wall_seconds']-before['claim_wall_seconds']-joined['last_claim_wall_seconds'])>1e-9 or joined['store_wall_seconds']!=before['store_wall_seconds'] or joined['company_wall_seconds']!=before['company_wall_seconds']:raise ValueError('paired actual claim wall differs from its observed pre-claim window')
   if any(joined[k]<before[k]-1e-9 for k in ['company_wall_seconds','store_wall_seconds','claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds','max_wall_monotonic_gap_ms','parser_joined_call_wall_seconds_sum']) or joined['claim_wall_seconds']<=before['claim_wall_seconds'] or joined['parse_complete_wall_seconds']<=before['parse_complete_wall_seconds'] or joined['parser_joined_call_wall_seconds_sum']<=before['parser_joined_call_wall_seconds_sum']:raise ValueError('actual joined claim timing missing or inconsistent')
   previous=joined

def validate_fingerprint_streams(result,size):
 employees,mailboxes,messages=size
 if result.get('fingerprint_method')!='six_actual_canonical_sql_streams_v1':raise ValueError('actual canonical six-stream fingerprint method required')
 streams=result.get('fingerprint_streams');counts=[2,messages,messages,employees*16,employees+2,mailboxes]
 if type(streams) is not list or len(streams)!=6:raise ValueError('all six actual canonical fingerprint streams required')
 for index,(stream,count) in enumerate(zip(streams,counts)):
  if type(stream) is not dict or set(stream)!={'stream_id','row_count','sha256'}:raise ValueError('known actual canonical stream observations required')
  require_integer_fields(stream,['stream_id','row_count'])
  if stream['stream_id']!=index or stream['row_count']!=count or not sha256_string(stream['sha256']):raise ValueError('actual canonical stream identity/population/hash mismatch')

def validate_observations(result,size,scale,expected_source,*,source_schema_version=None,frozen_legacy=False,preparation_method=DEFAULT_METHOD,method_contract=None,method_contract_digest=None):
 if type(expected_source) is not str or not re.fullmatch('[0-9a-f]{40}',expected_source):raise ValueError('explicit source identity required')
 reject_unknown_sampling(result)
 if type(result.get('schema_version')) is not int or result['schema_version']!=(expected_schema_version() if source_schema_version is None else source_schema_version):raise ValueError('observed schema differs from actual migration source')
 if not frozen_legacy and result['schema_version']!=PIPELINE_SCHEMA_VERSION:raise ValueError('new preparation evidence requires exact schema16 pipeline contract')
 employees,mailboxes,messages=size
 validate_method_selection(result,preparation_method,method_contract,method_contract_digest,frozen_legacy=frozen_legacy)
 if not frozen_legacy:
  validate_preparation(result,messages,scale,expected_source,preparation_method=preparation_method)
  validate_fingerprint_streams(result,size)
  if preparation_method==LOOKAHEAD_METHOD:
   validate_input_scheduler(result,messages,employees)
   validate_persistent_observations(result,messages,employees,expected_source)
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
  if frozen_legacy:
   if not plan_reads_messages(raw):raise ValueError('captured shipping plan does not read actual messages relation')
  else:validate_explain_plan(raw,'messages')
 if not any(plan['workload']=='inbox_list' for plan in plans):raise ValueError('representative actual shipping inbox plan required')

def validate_tool_result(result,contract,expected_source,*,preparation_method=DEFAULT_METHOD,method_contract=None,method_contract_digest=None):
 validate_contract(contract)
 if type(result) is not dict or result.get('mode')!='TOOL_ONLY_DATASET_CALIBRATION' or result.get('scale')!='tool_only' or result.get('S_M_L_executed') is not False:raise ValueError('tool-only evidence cannot certify S/M/L')
 require_integer_fields(result,['employees','mailboxes','messages','seed','rss_bytes','disk_bytes'])
 if result['seed']!=contract['seed'] or result['rss_bytes']<=0 or result['disk_bytes']<=0:raise ValueError('actual tool seed/RSS/disk observations required')
 if tuple(result.get(k) for k in ['employees','mailboxes','messages'])!=(20,100,1000) or result.get('source_sha')!=expected_source:raise ValueError('wrong tool-only source/population')
 if result.get('dataset_sha256')!=hashlib.sha256(DATASET.read_bytes()).hexdigest():raise ValueError('tool dataset contract mismatch')
 if result.get('task_complete') is not False or result.get('product_green') is not False:raise ValueError('tool-only evidence cannot approve a task/product')
 validate_observations(result,(20,100,1000),'tool_only',expected_source,preparation_method=preparation_method,method_contract=method_contract,method_contract_digest=method_contract_digest)
 if 'measurements' in result:
  require_integer_fields(result,['concurrency','pool_size','sql_tracer_calibration_count','sql_tracer_calibration_expected'])
  if (result['concurrency'],result['pool_size'],result['sql_tracer_calibration_count'],result['sql_tracer_calibration_expected'])!=(20,24,20,20):raise ValueError('complete tool workload sampling/SQL calibration required')
  if result.get('safety_assertions_passed') is not True or type(result.get('safety_assertions')) is not list or len(result['safety_assertions'])!=len(WORKLOADS) or set(result['safety_assertions'])!={w+':foreign_and_revoked_current_source_denied' for w in WORKLOADS}:raise ValueError('complete tool workload source-safety assertions required')
  validate_measurements(result,contract,'tool_only',require_wall_clock='clock_contract' in contract)
 return {'status':'tool_only_observations_validated','S_M_L_executed':False,'task_complete':False,'product_green':False}

def validate_measurements(result,contract,scale,*,require_wall_clock=False):
 budget=300 if scale=='tool_only' else BUDGETS[scale][0]
 rows=result.get('measurements',[])
 if type(rows) is not list or any(type(r) is not dict for r in rows):raise ValueError('measurement objects required')
 required={(w,c) for w in WORKLOADS for c in contract['cache_modes']}
 if any(type(r.get('workload')) is not str or type(r.get('cache_mode')) is not str for r in rows):raise ValueError('workload/cache identifiers required')
 if {(r.get('workload'),r.get('cache_mode')) for r in rows}!=required or len(rows)!=len(required):raise ValueError('missing/duplicate real cold/hot workloads')
 for row in rows:
  if type(row.get('samples')) is not int or row['samples']!=200 or type(row.get('sql_count')) is not int or row['sql_count']<=0:raise ValueError('zero/unknown samples or SQL count')
  if not all(type(row.get(k)) in {int,float} for k in ['p50_ms','p95_ms','p99_ms']):raise ValueError('numeric percentiles required')
  if not row['p50_ms']<=row['p95_ms']<=row['p99_ms']:raise ValueError('ordered percentiles required')
  require_integer_fields(row,['alloc_bytes','rss_bytes','disk_bytes'])
  for k in ['p50_ms','p95_ms','p99_ms','alloc_bytes','rss_bytes','disk_bytes']:
   if type(row.get(k)) not in {int,float} or not math.isfinite(row[k]) or row[k]<0 or (k in {'rss_bytes','disk_bytes'} and row[k]==0):raise ValueError('missing actual latency/memory/disk metrics')
 if require_wall_clock or 'latency_clock' in result or 'execution_wall' in result:
  if result.get('latency_clock')!=LATENCY_CLOCK:raise ValueError('known actual wall latency clock required')
  execution=result.get('execution_wall')
  if type(execution) is not dict:raise ValueError('actual run wall observation required')
  fields=['started_unix_seconds','completed_unix_seconds','elapsed_wall_seconds','budget_seconds']
  if any(type(execution.get(k)) not in {int,float} or not math.isfinite(execution[k]) for k in fields):raise ValueError('finite wall clock observations required')
  elapsed=execution['completed_unix_seconds']-execution['started_unix_seconds']
  if type(execution['budget_seconds']) is not int:raise ValueError('integer execution wall budget required')
  if elapsed<0 or elapsed> budget or abs(elapsed-execution['elapsed_wall_seconds'])>0.001 or execution['budget_seconds']!=budget:raise ValueError('original absolute wall budget exceeded or inconsistent')
  if 'preparation' in result and result['preparation']['company_wall_seconds']+result['preparation']['pipeline_wall_seconds']>elapsed+0.001:raise ValueError('preparation phase wall escapes actual execution wall interval')
  if 'preparation' in result and abs(result['preparation']['parser_original_deadline_unix_seconds']-execution['started_unix_seconds']-budget)>0.000002:raise ValueError('actual parser deadline is not original execution start plus unchanged budget')
  for row in rows:
   gap=row.get('max_wall_monotonic_gap_ms')
   if type(gap) not in {int,float} or not math.isfinite(gap) or not 0<=gap<=100:raise ValueError('unknown or interrupted sample wall/monotonic clock')
 return rows

def validate_result(result,contract,scale,expected_source,*,dataset_digest=None,source_schema_version=None,require_wall_clock=None,frozen_legacy=False,preparation_method=DEFAULT_METHOD,method_contract=None,method_contract_digest=None):
 validate_contract(contract,frozen_legacy=frozen_legacy)
 if type(result) is not dict:raise ValueError('result JSON object required')
 if scale not in {'S','M'}:raise ValueError('only authorized original S/M evidence accepted')
 if preparation_method==LOOKAHEAD_METHOD:
  validate_method_contract(method_contract,frozen_contract_digest=method_contract_digest)
  if scale not in method_contract['authorized_original_scales']:raise ValueError('selected frozen method contract does not authorize this original scale')
 if frozen_legacy:
  if scale!='S' or (expected_source,dataset_digest,source_schema_version)!=FROZEN_S2 or any(k in result for k in ['preparation','preparation_checkpoints','fingerprint_method','fingerprint_streams','latency_clock','execution_wall']) or any(k in contract for k in ['preparation_contract','clock_contract']):raise ValueError('only exact unmodified frozen S2 legacy contract admitted')
 elif source_schema_version is not None and (type(source_schema_version) is not int or source_schema_version!=PIPELINE_SCHEMA_VERSION):raise ValueError('new source cannot substitute a historical schema contract')
 if 'mode' in result or 'S_M_L_executed' in result:raise ValueError('tool-only markers cannot certify original scale')
 require_integer_fields(result,['employees','mailboxes','messages','seed','concurrency','index_ready_count','sql_tracer_calibration_count','sql_tracer_calibration_expected','pool_size'])
 if result.get('scale')!=scale or tuple(result.get(k) for k in ['employees','mailboxes','messages'])!=SCALES[scale] or result.get('source_sha')!=expected_source:raise ValueError('wrong scale/source evidence')
 if result.get('seed')!=contract['seed'] or result.get('concurrency')!=20:raise ValueError('wrong seed/concurrency')
 if result.get('dataset_sha256')!=(dataset_digest or hashlib.sha256(DATASET.read_bytes()).hexdigest()):raise ValueError('dataset contract hash mismatch')
 if result.get('index_ready_count')!=SCALES[scale][2]:raise ValueError('index-ready prerequisite not satisfied')
 if result.get('sql_tracer_calibration_count')!=20 or result.get('sql_tracer_calibration_expected')!=20:raise ValueError('real SQL tracer calibration required')
 if not isinstance(result.get('hardware'),dict) or not result['hardware'] or type(result.get('schema_version')) is not int or not result.get('dataset_fingerprint') or not result.get('safety_assertions'):raise ValueError('actual hardware/schema/fingerprint/safety observations required')
 if result.get('safety_assertions_passed') is not True or result.get('task_complete') is not False or result.get('product_green') is not False:raise ValueError('safety/policy evidence is not overall task approval')
 hardware=result['hardware']
 if not isinstance(hardware.get('os'),str) or not hardware['os'] or not isinstance(hardware.get('arch'),str) or not hardware['arch'] or type(hardware.get('cpu')) is not int or hardware['cpu']<=0:raise ValueError('observed runtime hardware required')
 if result.get('pool_size')!=24:raise ValueError('frozen actual application pool size required')
 safety={w+':foreign_and_revoked_current_source_denied' for w in WORKLOADS}
 if type(result.get('safety_assertions')) is not list or len(result['safety_assertions'])!=len(safety) or any(type(x) is not str for x in result['safety_assertions']) or set(result['safety_assertions'])!=safety:raise ValueError('each shipping workload safety control required')
 validate_observations(result,SCALES[scale],scale,expected_source,source_schema_version=source_schema_version,frozen_legacy=frozen_legacy,preparation_method=preparation_method,method_contract=method_contract,method_contract_digest=method_contract_digest)
 rows=validate_measurements(result,contract,scale,require_wall_clock=(not frozen_legacy or 'clock_contract' in contract or require_wall_clock is True))
 thresholds=[]
 for row in rows:
  limit=contract['candidate_thresholds'][scale+'_list_p95_ms'] if row['workload']=='inbox_list' else contract['candidate_thresholds']['M_index_ready_query_p95_ms'] if scale=='M' and row['workload'] in {'indexed_message','indexed_search'} else None
  if limit is not None:thresholds.append({'workload':row['workload'],'cache_mode':row['cache_mode'],'p95_ms':row['p95_ms'],'candidate_limit_ms':limit,'passed':row['p95_ms']<=limit})
 return {'status':'actual_scale_evidence_validated','task_complete':False,'product_green':False,'scale':scale,'candidate_thresholds':thresholds,'candidate_threshold_passed':all(x['passed'] for x in thresholds),'relative_regression':'no_reference_baseline_yet_not_certified'}

class OwnedProcessTimeout(ValueError):
 def __init__(self,returncode,receipt):
  self.returncode=returncode;self.receipt=receipt
  super().__init__('absolute wall budget exceeded: cleanup observations recorded; original output preserved')

class OwnedProcessExecutionError(ValueError):
 def __init__(self,receipt):
  self.receipt=receipt;self.returncode=receipt.get('process_returncode')
  super().__init__('owned process observation failed; execution/cleanup receipt preserved')

def owned_group_observation(group):
 # Read-only membership evidence. This is never used to select a group to kill.
 try:
  result=subprocess.run(['ps','-axo','pid=,pgid=,stat='],capture_output=True,text=True,timeout=5,check=True)
  members=[]
  for line in result.stdout.splitlines():
   parts=line.split()
   if len(parts)!=3:raise ValueError('unparseable process membership row')
   pid,pgid=int(parts[0]),int(parts[1])
   if pgid==group:members.append({'pid':pid,'group':pgid,'state':parts[2]})
  return {'status':'observed','members':members,'non_zombie_live_pids':[m['pid'] for m in members if not m['state'].startswith('Z')]}
 except Exception as exc:return {'status':'unknown','error':repr(exc)}

def cleanup_owned_process(process,receipt,*,already_reaped=False,leader_unreaped=False):
 # Never signal a PID/group after reaping its leader: it could have been reused.
 # TERM grace deliberately does not poll/wait, retaining our unreaped child as
 # the ownership anchor through the final group signal. No broad process search.
 actions=[];errors=[]
 def send(sig):
  action={'signal':sig.name,'owned_pid':process.pid}
  try:
   if not leader_unreaped or process.returncode is not None or signal.getsignal(signal.SIGCHLD)!=signal.SIG_DFL:
    action['status']='untrusted_leader_anchor_no_signal';errors.append(dict(action));actions.append(action);return
   group=os.getpgid(process.pid)
   action['observed_group']=group
   if group!=process.pid:
    action['status']='identity_mismatch_no_signal';errors.append(dict(action))
   else:
    os.killpg(group,sig);action['status']='sent'
  except ProcessLookupError:
   # Darwin getpgid rejects a zombie even before waitpid has released its PID.
   # Keep the original Popen wait ownership as the identity anchor; only use
   # this fallback with default SIGCHLD (no automatic reaping) and an observed
   # surviving member of this exact original group. Never signal after join.
   observation=owned_group_observation(process.pid)
   action['missing_leader_group_observation']=observation
   if leader_unreaped and process.returncode is None and signal.getsignal(signal.SIGCHLD)==signal.SIG_DFL and observation['status']=='observed' and observation['non_zombie_live_pids']:
    try:
     os.killpg(process.pid,sig);action.update(status='sent_unreaped_leader_anchor',observed_group=process.pid)
    except OSError as exc:
     action.update(status='signal_error',errno=exc.errno,error=str(exc));errors.append(dict(action))
   else:action['status']='owned_leader_missing_no_signal'
  except OSError as exc:
   action.update(status='signal_or_identity_error',errno=exc.errno,error=str(exc));errors.append(dict(action))
  actions.append(action)
 if already_reaped:
  actions.append({'status':'already_reaped_no_group_signal'})
 else:
  send(signal.SIGTERM)
  # Preserve leader identity even if TERM exits it or the entire group disappears.
  try:time.sleep(5)
  except BaseException as exc:errors.append({'status':'grace_interrupted','error':repr(exc)})
  send(signal.SIGKILL)
 try:
  if signal.getsignal(signal.SIGCHLD)!=signal.SIG_DFL:raise ValueError('unknown child wait ownership: SIGCHLD changed')
  returncode=process.wait(timeout=5)
  receipt.update(process_returncode=returncode,process_returncode_status='observed_parent_wait')
 except Exception as exc:
  receipt.update(process_returncode=None,process_returncode_status='unknown_join_failed')
  errors.append({'status':'parent_join_error','error':repr(exc)})
 observation=owned_group_observation(process.pid)
 verified_gone=observation['status']=='observed' and not observation['non_zombie_live_pids']
 receipt.update(cleanup_actions=actions,cleanup_errors=errors,cleanup_group_observation=observation,
  cleanup_live_processes_absent=verified_gone,
  owned_process_group_cleanup='observed_no_non_zombie_members' if verified_gone else 'error_or_unverified',
  cleanup_observed_wall_time=time.time())
 return receipt.get('process_returncode')

def run_owned_process(command,*,env,stdout,stderr,timeout,receipt=None):
 if type(timeout) not in {int,float} or not math.isfinite(timeout) or timeout<=0:raise ValueError('positive finite wall budget required')
 receipt={} if receipt is None else receipt
 started=time.time();deadline=started+timeout;last_wall=started
 receipt.update(started_wall_time=started,absolute_deadline_wall_time=deadline,budget_seconds=timeout,process_returncode=None,process_returncode_status='not_started',timed_out=None,scope='absolute wall deadline including host suspension; cleanup is observed after resume, never guaranteed during sleep')
 process=None;reaped=False
 try:
  if signal.getsignal(signal.SIGCHLD)!=signal.SIG_DFL:raise ValueError('default SIGCHLD required for exclusive owned child wait identity')
  process=subprocess.Popen(command,cwd=ROOT,env=env,stdout=stdout,stderr=stderr,start_new_session=True)
  receipt.update(owned_process_group=process.pid,process_returncode_status='running_unobserved')
  while True:
   now=time.time();receipt.update(observed_wall_time=now,elapsed_wall_seconds=now-started)
   if now<last_wall:
    receipt['wall_clock_reversed']=True
    raise subprocess.TimeoutExpired(command,timeout)
   last_wall=now
   if now>=deadline:raise subprocess.TimeoutExpired(command,timeout)
   if signal.getsignal(signal.SIGCHLD)!=signal.SIG_DFL:raise ValueError('unknown child wait ownership: SIGCHLD changed')
   returncode=process.poll()
   if returncode is not None:
    reaped=True
    receipt.update(process_returncode=returncode,process_returncode_status='observed_parent_poll')
    completed=time.time();receipt.update(observed_wall_time=completed,elapsed_wall_seconds=completed-started)
    if completed<last_wall:
     receipt['wall_clock_reversed']=True
     raise subprocess.TimeoutExpired(command,timeout)
    if completed>=deadline:raise subprocess.TimeoutExpired(command,timeout)
    receipt['timed_out']=False
    return returncode
   # Unlike Popen.wait, this does not reap the leader before the next wall check.
   time.sleep(min(0.1,deadline-now))
 except subprocess.TimeoutExpired:
  receipt['timed_out']=True
  cleanup_owned_process(process,receipt,already_reaped=reaped,leader_unreaped=not reaped)
  raise OwnedProcessTimeout(receipt.get('process_returncode'),receipt)
 except BaseException as exc:
  receipt.update(execution_error=repr(exc))
  if process is not None:cleanup_owned_process(process,receipt,already_reaped=reaped,leader_unreaped=not reaped)
  else:receipt.update(process_returncode_status='unknown_spawn_failed',cleanup_actions=[],cleanup_errors=[],owned_process_group_cleanup='not_started_no_signal')
  if isinstance(exc,(KeyboardInterrupt,SystemExit)):raise
  raise OwnedProcessExecutionError(receipt) from exc

def write_execution_receipt(out,receipt):
 # A missing observation stays unknown. Never manufacture a numeric Go exit.
 (out/'execution-budget.json').write_text(json.dumps(receipt,sort_keys=True,indent=2)+'\n')
 returncode=receipt.get('process_returncode')
 if type(returncode) is int:(out/'go.exit').write_text(str(returncode)+'\n')
 else:(out/'go-exit-status.json').write_text(json.dumps({'status':'unknown','returncode':None,'reason':receipt.get('process_returncode_status','missing_observation'),'task_complete':False,'product_green':False},sort_keys=True)+'\n')
 if receipt.get('timed_out') is True:(out/'timeout.json').write_text(json.dumps(dict(receipt,task_complete=False,product_green=False),sort_keys=True)+'\n')
 if receipt.get('execution_error') or receipt.get('cleanup_errors'):(out/'execution-error.json').write_text(json.dumps(dict(receipt,task_complete=False,product_green=False),sort_keys=True)+'\n')

def validate_go_events(path):
 return validate_go_event_text(Path(path).read_text())

def validate_go_event_text(payload):
 events=[]
 for line in payload.splitlines():
  if line.strip():
   event=parse_json(line)
   if type(event) is not dict:raise ValueError('Go event object required')
   events.append(event)
 target='TestR5BenchmarkOriginalScale';package='tabmail/internal/store/postgres'
 observed=[e.get('Action') for e in events if e.get('Test')==target and e.get('Action') in {'run','pass','fail','skip'}]
 if observed!=['run','pass'] or any(e.get('Package')!=package for e in events if e.get('Test')==target):raise ValueError('scale test did not actually run/pass in benchmark package')
 if any(e.get('Action') in {'fail','skip'} for e in events) or len([e for e in events if e.get('Action')=='pass' and e.get('Package')==package and 'Test' not in e])!=1:raise ValueError('benchmark package did not pass without failures/skips')
 return events


def go_event_timestamp(event):
 value=event.get('Time')
 if type(value) is not str:raise ValueError('actual Go RFC3339 wall timestamp required')
 normalized=value.replace('Z','+00:00')
 normalized=re.sub(r'\.(\d+)(?=[+-]\d{2}:\d{2}$)',lambda m:'.'+(m.group(1)+'000000')[:6],normalized)
 parsed=datetime.fromisoformat(normalized)
 if parsed.tzinfo is None:raise ValueError('Go timestamp timezone required')
 return parsed.timestamp()

def validate_run_wall(events,budget):
 starts=[e for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action')=='run']
 ends=[e for e in events if e.get('Action')=='pass' and e.get('Package')=='tabmail/internal/store/postgres' and 'Test' not in e]
 if len(starts)!=1 or len(ends)!=1:raise ValueError('actual original test/package wall interval required')
 elapsed=go_event_timestamp(ends[0])-go_event_timestamp(starts[0])
 if not 0<=elapsed<=budget:raise ValueError('original test/package absolute wall budget exceeded')
 return elapsed

def validate_package_elapsed_observation(events,review,layout,budget):
 packages=[e for e in events if e.get('Action')=='pass' and e.get('Package')=='tabmail/internal/store/postgres' and 'Test' not in e]
 if len(packages)!=1:raise ValueError('actual unique Go package PASS elapsed observation required')
 elapsed=packages[0].get('Elapsed')
 if type(elapsed) not in {int,float} or not math.isfinite(elapsed) or not 0<=elapsed<=budget:raise ValueError('actual finite bounded nonboolean package PASS Elapsed required')
 if layout=='embedded_archive':
  tests=[e for e in events if e.get('Action')=='pass' and e.get('Test')=='TestR5BenchmarkOriginalScale']
  if len(tests)!=1 or not exact_frozen_equal(review.get('go_test_elapsed_seconds'),tests[0].get('Elapsed')) or not exact_frozen_equal(review.get('package_elapsed_seconds'),elapsed):raise ValueError('historical S2 declared test/package elapsed observations must each bind original raw boundary')
 return elapsed

def validate_run_elapsed_summary(row,review,result,events,layout,scale):
 passed=[e for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action')=='pass']
 starts=[e for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action')=='run']
 if len(passed)!=1 or len(starts)!=1:raise ValueError('actual unique Go test run/pass elapsed source required')
 go_elapsed=passed[0].get('Elapsed');summary=row.get('elapsed_seconds');budget=BUDGETS[scale][0]
 if any(type(v) not in {int,float} or not math.isfinite(v) or not 0<=v<=budget for v in [go_elapsed,summary]):raise ValueError('actual finite bounded Go/summary elapsed observations required')
 if layout=='embedded_archive':
  if summary!=go_elapsed:raise ValueError('historical S2 elapsed summary must match original GoTest PASS')
  return 'original_go_test_pass_elapsed_seconds'
 if layout not in {'pipeline_s_v1_delivered','lookahead_s_receipt_native'}:raise ValueError('unknown elapsed summary clock version')
 wall=result.get('execution_wall');review_wall=review.get('execution_wall')
 if type(wall) is not dict or not exact_frozen_equal(wall,review_wall) or summary!=wall.get('elapsed_wall_seconds'):raise ValueError('historical delivered elapsed summary must match source-bound result execution wall, not different GoTest scope')
 start=go_event_timestamp(starts[0]);end=go_event_timestamp(passed[0])
 if wall['started_unix_seconds']<start-0.001 or wall['completed_unix_seconds']>end+0.001 or summary>go_elapsed+0.001:raise ValueError('result execution wall escapes actual Go test run/pass interval')
 return 'original_source_bound_result_execution_wall_elapsed_seconds'


def evidence_path(name,directory):
 if type(name) is not str:raise ValueError('evidence path required')
 path=(directory/name).resolve()
 if not path.is_relative_to(directory.resolve()) or not path.is_file():raise ValueError('missing or out-of-scope original evidence')
 return path

def delivered_archive_binding(row,review,directory):
 if type(row) is not dict or type(review) is not dict:raise ValueError('delivered archive row/review objects required')
 population=review.get('original_requested_population') if row.get('failure_variant')==M_TIMEOUT_VARIANT else review.get('population')
 if review.get('source_tree')!=row.get('source_tree') or review.get('scale')!=row.get('scale') or not exact_frozen_equal(population,row.get('population')):raise ValueError('delivered original review source/population differs')
 name=row.get('archive');digest=row.get('archive_sha256')
 if type(name) is not str or Path(name).name!=name or not name or not sha256_string(digest):raise ValueError('explicit delivered archive name/SHA required')
 delivery=read_json(evidence_path(row.get('delivery_receipt'),directory))
 if row.get('failure_variant')==M_TIMEOUT_VARIANT:
  m_failure_review_layout(row,review)
  required={'schema_version':1,'source_sha':FROZEN_M_TIMEOUT_SOURCE,'review':row['evidence'],'archive':name,'archive_sha256_from_author_receipt':digest,'payload_bytes_changed':False,'original_raw_review_and_archive_unchanged':True,'M_attempted':True,'M_baseline_complete':False,'M_retry_started':False,'L_started':False,'task_complete':False,'product_green':False,'payload_file_count':34}
  if type(delivery) is not dict or any(not exact_frozen_equal(delivery.get(k),v) for k,v in required.items()) or type(delivery.get('payload_members')) is not list or len(delivery['payload_members'])!=34 or any(type(n) is not str for n in delivery['payload_members']) or len(set(delivery['payload_members']))!=34 or type(delivery.get('source_archive')) is not str:raise ValueError('known unchanged original M single-record delivery and exact payload identity required')
  return {'path':name,'sha256':digest},delivery
 if type(delivery) is not dict or type(delivery.get('schema_version')) is not int or delivery['schema_version']!=1 or type(delivery.get('deliveries')) is not list:raise ValueError('known version1 delivery receipt required')
 entries=[entry for entry in delivery['deliveries'] if type(entry) is dict and entry.get('review')==row.get('evidence')]
 if len(entries)!=1:raise ValueError('unique delivery original-review binding required')
 entry=entries[0]
 if 'safe_archive' in entry:
  if 'archive' in entry or ('copied_archive_bytes_changed' in entry and entry['copied_archive_bytes_changed'] is not True):raise ValueError('ambiguous or contradictory safe-repack archive shape')
  actual_name=entry.get('safe_archive');actual_digest=entry.get('safe_archive_sha256')
 elif 'archive' in entry and type(entry.get('copied_archive_bytes_changed')) is bool:
  actual_name=entry.get('archive');actual_digest=entry.get('archive_sha256') if entry['copied_archive_bytes_changed'] else entry.get('archive_sha256_from_author_receipt')
  if not entry['copied_archive_bytes_changed'] and 'archive_sha256' in entry and entry['archive_sha256']!=actual_digest:raise ValueError('unchanged author archive cannot have a contradictory safe digest')
 else:raise ValueError('unknown delivered archive proof shape')
 if (actual_name,actual_digest)!=(name,digest):raise ValueError('delivery/registry actual archive binding differs')
 return {'path':name,'sha256':digest},entry

def archive_evidence(review,directory,archive_name=None,*,delivered_binding=None):
 info=review.get('archive')
 if delivered_binding is None:
  if type(info) is not dict or not sha256_string(info.get('sha256')) or type(info.get('members')) is not dict:raise ValueError('original archive SHA and member manifest required')
 else:
  if 'archive' in review or type(delivered_binding) is not dict or set(delivered_binding)!={'path','sha256'} or not sha256_string(delivered_binding.get('sha256')):raise ValueError('explicit unambiguous delivered archive binding required')
  info=delivered_binding
 archive_path=info.get('path')
 if type(archive_path) is not str or Path(archive_path).is_absolute() or '..' in Path(archive_path).parts:raise ValueError('safe original archive path required')
 name=Path(archive_path).name
 if not name or (archive_name is not None and name!=archive_name):raise ValueError('review/registry archive mismatch')
 path=evidence_path(name,directory)
 if hashlib.sha256(path.read_bytes()).hexdigest()!=info['sha256']:raise ValueError('original evidence archive hash mismatch')
 members={};seen=set();total=0
 with tarfile.open(path,'r:gz') as archive:
  for member in archive:
   parts=Path(member.name).parts
   if not member.name or not parts or member.name.rstrip('/') in seen or member.name.startswith('/') or '..' in parts or Path(member.name).as_posix()!=(member.name[:-1] if member.name.endswith('/') else member.name) or member.size>64*(1<<20):raise ValueError('duplicate, unsafe or oversized archive member')
   seen.add(member.name.rstrip('/'))
   if member.isdir():
    if member.size!=0:raise ValueError('directory archive member cannot carry payload')
    continue
   if not member.isfile():raise ValueError('only regular files and safe empty directories admitted')
   total+=member.size
   if total>128*(1<<20):raise ValueError('original archive exceeds bounded validation memory')
   members[member.name]=archive.extractfile(member).read()
 if not members:raise ValueError('empty original evidence archive')
 if 'members' in info and {name:hashlib.sha256(payload).hexdigest() for name,payload in members.items()}!=info['members']:raise ValueError('original archive member closure mismatch')
 return members

FROZEN_PIPELINE_S_SOURCE='3d07b72b3732cf289410aefaaba49f8d8423f746'
FROZEN_LOOKAHEAD_S_SOURCE='4074282496c0c60e3b6e0c6f6c1407858939c95c'
FROZEN_M_TIMEOUT_SOURCE='8529007132f1d61a2571e43aaabfe15b8452baa1'
FROZEN_M_METHOD_SHA256='d286ae9f5f4b68033ba9739a1276de22f1e0f7ef88e15642d52633da943b49a7'
M_TIMEOUT_VARIANT='observed_absolute10800_timeout_incomplete_preparation'
M_TIMEOUT_REVIEW_FIELDS={'L_executed','M_attempted','M_executed','M_result_sha256','absolute_deadline_UTC','absolute_deadline_wall_time','actual_captured_ready_checkpoints','actual_population_counts_after_original_Go_join_and_before_native_stop','actual_source_closure_sha256','baseline_complete','budget_and_owned_process_cleanup_receipt','captured_preparation_incomplete_stats','checkpoint_logging_boundary','completed_workload_samples_evidenced','concurrency','created_utc','dataset_contract_sha256','disk_RSS_boundary','disk_budget_bytes','full_population_1M_not_reached','full_six_stream_fingerprint_not_computed','go_exit_semantics','matrix14by200_completed','max_observed_active20','max_window20','meaning_of_M_executed_flag','method_contract_sha256','native_cleanup','no_explicit_analyze_or_planner_or_durability_override','no_scale_shrink_budget_extension_retry_or_L','observed_budget_elapsed_seconds','observed_outer_Go_process_returncode','original_requested_population','outer_budget_seconds','parser_actual_min_start_remaining31guard_seconds','parser_joined_calls','parser_quiescent','pending_upper_bound20','pool_size','preparation_method','preparation_stats_named_TERMINAL_not_completion','product_green','raw_go_sha256','result_JSON_absent','runner_exit','scale','source_identity_kind','source_prepost_proof','source_sha_semantics','source_tree','status','task_complete','timed_out'}

def m_failure_review_layout(row,review):
 if type(row) is not dict or type(review) is not dict:raise ValueError('typed M failure row/review objects required')
 expected={'source_tree':FROZEN_M_TIMEOUT_SOURCE,'scale':'M','failure_variant':M_TIMEOUT_VARIANT,'status':'failed','preparation_method':LOOKAHEAD_METHOD,'method_contract_sha256':FROZEN_M_METHOD_SHA256,'dataset_contract_sha256':LOOKAHEAD_DATASET_SHA256,'schema_version':16,'source_identity_kind':CANONICAL_SOURCE_KIND,'source_sha_semantics':CANONICAL_SOURCE_SEMANTICS,'budget_seconds':10800,'disk_budget_bytes':80*(1<<30),'go_exit':-15,'runner_exit':1}
 if any(not exact_frozen_equal(row.get(k),v) for k,v in expected.items()) or not exact_frozen_equal(row.get('population'),list(SCALES['M'])) or set(review)!=M_TIMEOUT_REVIEW_FIELDS or review.get('status')!='once_original_M_failed_absolute10800_wall_budget_incomplete_not_baseline':raise ValueError('exact source852900/schema16/methodd286/absolute10800 failed version required')
 claims={'source_tree':FROZEN_M_TIMEOUT_SOURCE,'scale':'M','preparation_method':LOOKAHEAD_METHOD,'method_contract_sha256':FROZEN_M_METHOD_SHA256,'dataset_contract_sha256':LOOKAHEAD_DATASET_SHA256,'source_identity_kind':CANONICAL_SOURCE_KIND,'source_sha_semantics':CANONICAL_SOURCE_SEMANTICS,'original_requested_population':list(SCALES['M']),'outer_budget_seconds':10800,'disk_budget_bytes':80*(1<<30),'observed_outer_Go_process_returncode':-15,'runner_exit':1,'concurrency':20,'pool_size':24}
 if any(not exact_frozen_equal(review.get(k),v) for k,v in claims.items()):raise ValueError('typed M original requested contract and observed exit/source must bind exact evidence')
 for k,v in {'baseline_complete':False,'M_executed':False,'L_executed':False,'M_attempted':True,'task_complete':False,'product_green':False,'timed_out':True,'result_JSON_absent':True,'matrix14by200_completed':False,'full_population_1M_not_reached':True,'full_six_stream_fingerprint_not_computed':True,'completed_workload_samples_evidenced':0,'M_result_sha256':None}.items():
  if not exact_frozen_equal(review.get(k),v):raise ValueError('typed M timeout cannot claim completion, result, metrics or samples')
 return 'm_absolute_timeout_receipt_v1'

def validate_typed_m_failure_summary(row,stats):
 expected={'status':'failed','baseline_complete':False,'go_exit_status':'observed_parent_wait_original_owned_process_SIGTERM','go_test_terminal_status':'absent_not_fabricated_PASS_or_FAIL','strict_validation_status':'rejected_no_result_incomplete_budget_timeout','preparation_TERMINAL_marker_is_complete':False,'preparation_phase':stats['phase'],'producer_done':stats['producer_done'],'final_empty_claim':stats['final_empty_claim'],'worker_joined_inflight':stats['joined_inflight'],'parser_joined_inflight':stats['parser_joined_inflight'],'parser_quiescent':stats['parser_quiescent'],'parser_min_start_remaining_wall_seconds':stats['parser_min_start_remaining_wall_seconds']}
 if any(not exact_frozen_equal(row.get(key),value) for key,value in expected.items()):raise ValueError('typed M registry status/completion/phase/joins/parser reserve must bind trusted incomplete raw observations')
 for key in ['task_complete','product_green']:
  if key in row and row[key] is not False:raise ValueError('typed M failed attempt cannot claim task or product completion')

S2_REVIEW_FIELDS={'task','batch','status','base_commit','source_tree','source_sha_semantics','scale','population','seed','budget_seconds','go_test_elapsed_seconds','package_elapsed_seconds','go_exit','runner_exit','actual_test_lifecycle','source_closure_files_verified','source_closure_mismatches','completed_workload_cache_pairs','samples_each','concurrency','pool','actual_distribution','actual_dataset_fingerprint','parameter_fingerprint','sql_tracer_calibration_actual_queries','safety_assertions_count','safety_assertions_passed','original_thresholds','observed_S_list_p95_ms','S_list_candidate_passed','observed_S_indexed_search_p95_ms','S_search_candidate','performance_gaps','relative_regression','baseline_complete','overall_performance_green','M_baseline_complete','task100_complete','G0_accepted','resource_scope','host_supplementary_observation','result_sha256','cleanup','archive','historical_dataset_contract'}
PIPELINE_REVIEW_FIELDS={'L_executed','M_executed','actual_distribution','actual_same_parameter_deep_claim_pairs','actual_source_closure_sha256','baseline_complete','candidate_result','comparison_boundary','created_utc','dataset_fingerprint','dataset_sha256','disk_max_bytes','execution_wall','fingerprint_method','fingerprint_streams','full_workload_sample_count','go_exit','go_test_to_package_wall_seconds','original_outer_process_budget_receipt','owned_process_group_observation','population','preparation','product_green','raw_go_sha256','result_sha256','review_role','rss_max_bytes','runner_exit','safety_assertions','safety_assertions_passed','scale','schema_version','source_identity_kind','source_identity_proof_member','source_sha_semantics','source_tree','status','task_complete','terminal_empty_claim_at_exact_ready100k_seconds','workloads_rows'}
LOOKAHEAD_S_REVIEW_FIELDS={'Go_exit','M_L_executed','M_planning_only','RSS_max_bytes','actual_UID_received_order_observations','actual_same_parameter_Claim20_at_ready99980_seconds','actual_sample_count','actual_scheduler','actual_source_closure_sha256','actual_sourcebound_ready','all7safety','baseline_complete','created_utc','dataset_sha256','disk_max_bytes','execution_wall','final_empty_claim_at_ready100000_seconds','fingerprint','fingerprint_streams','go_to_package_wall_seconds','measurements','method_contract_sha256','native_cleanup','old_S_source_sha','old_new_source_comparison_boundary','original_S_reexecuted','owned_go_group_observation','persistent_equivalence_disclosure','population','preparation','preparation_method','product_green','runner_exit','scale','six_streams_aggregate_parameter_exact_equal_original_S','source_identity_kind','source_sha_semantics','source_tree','status','strict_validator_result','task_complete'}

def run_review_layout(row,review):
 if type(row) is not dict or type(review) is not dict:raise ValueError('run review/version objects required')
 if review.get('source_tree')!=row.get('source_tree') or review.get('scale')!=row.get('scale') or not exact_frozen_equal(review.get('population'),row.get('population')):raise ValueError('run review version must bind original source/scale/population')
 observed=(row.get('source_tree'),row.get('scale'),row.get('dataset_contract_sha256'),row.get('preparation_method',DEFAULT_METHOD),row.get('method_contract_sha256'))
 if 'archive' in review:
  schema=row.get('schema_version',15)
  if observed!=(FROZEN_S2[0],'S',FROZEN_S2[1],DEFAULT_METHOD,None) or type(schema) is not int or schema!=FROZEN_S2[2] or row.get('source_identity_kind')!='git_write_tree' or set(review)!=S2_REVIEW_FIELDS or review.get('status')!='original_S_complete_initial_baseline_not_overall_performance_green' or review.get('source_sha_semantics')!='git_write_tree_label_not_commit' or type(review['archive']) is not dict:raise ValueError('only exact frozen historical S2 embedded run-review version admitted; unknown/future run version requires independent admission')
  require_integer_fields(review,['seed','budget_seconds','samples_each','concurrency','pool','completed_workload_cache_pairs'])
  if (review['seed'],review['budget_seconds'],review['samples_each'],review['concurrency'],review['pool'],review['completed_workload_cache_pairs'])!=(3893945,1800,200,20,24,14) or review.get('task')!='R5-P0-100' or review.get('batch')!='B01-AJ' or review.get('actual_test_lifecycle')!=['run','pass']:raise ValueError('exact historical S2 review budget/population/sample/proof-role envelope required')
  return 'embedded_archive'
 if observed==(FROZEN_PIPELINE_S_SOURCE,'S',LOOKAHEAD_DATASET_SHA256,DEFAULT_METHOD,None) and set(review)==PIPELINE_REVIEW_FIELDS and review.get('status')=='complete_original_S_runtime_validated':return 'pipeline_s_v1_delivered'
 if observed==(FROZEN_LOOKAHEAD_S_SOURCE,'S',LOOKAHEAD_DATASET_SHA256,LOOKAHEAD_METHOD,FROZEN_LOOKAHEAD_S_METHOD_SHA256) and set(review)==LOOKAHEAD_S_REVIEW_FIELDS and review.get('status')=='complete_once_selected_lookahead_original_S_runtime_strict_validated_and_cleaned':return 'lookahead_s_receipt_native'
 raise ValueError('unknown or incomplete original run review/version/source tuple')

def validate_review_result_binding(row,review,members,prefix,result_digest,layout,directory,delivery_entry):
 go_member=prefix+'/go.jsonl'
 if any(type(members.get(name)) is not bytes or not members[name] for name in [prefix+'/result.json',go_member]):raise ValueError('original result and Go raw members required for byte binding')
 go_digest=hashlib.sha256(members[go_member]).hexdigest()
 if row.get('result_sha256')!=result_digest or not sha256_string(row.get('result_sha256')):raise ValueError('original registry result hash mismatch')
 if layout!='lookahead_s_receipt_native':
  if review.get('result_sha256')!=result_digest:raise ValueError('original review result hash mismatch')
  if layout=='pipeline_s_v1_delivered' and (row.get('raw_go_sha256')!=go_digest or review.get('raw_go_sha256')!=go_digest):raise ValueError('delivered original Go lifecycle hash mismatch')
  return
 binding=read_json(evidence_path(row.get('result_binding_receipt'),directory))
 fields={'created_utc','source_sha','result_member','result_sha256','raw_go_member','raw_go_sha256','original_archive','original_archive_sha256','source_raw_archive_unchanged','scope'}
 if type(binding) is not dict or set(binding)!=fields or type(binding['created_utc']) is not str or not binding['created_utc'] or type(binding['scope']) is not str or not binding['scope'] or binding['source_raw_archive_unchanged'] is not True:raise ValueError('known explicit unchanged-source result binding receipt required')
 origin=delivery_entry.get('source_archive') if type(delivery_entry) is dict else None
 if type(origin) is not str or not origin or delivery_entry.get('copied_archive_bytes_changed') is not False:raise ValueError('receipt-native binding requires actual unchanged author archive delivery')
 expected={'source_sha':row['source_tree'],'result_member':prefix+'/result.json','result_sha256':result_digest,'raw_go_member':go_member,'raw_go_sha256':go_digest,'original_archive':Path(origin).name,'original_archive_sha256':row['archive_sha256']}
 if any(binding.get(k)!=v for k,v in expected.items()) or row.get('raw_go_sha256')!=go_digest:raise ValueError('result sidecar differs from actual archived result/Go/source bytes')


def archived_json(members,name):
 if name not in members:raise ValueError('missing original raw evidence: '+name)
 return parse_json(members[name].decode('utf-8'))

CANONICAL_SOURCE_KIND='canonical_source_closure_sha1'
CANONICAL_SOURCE_SEMANTICS='canonical_source_closure_sha1_not_git_tree_or_commit'

def validate_source_identity(row,review,closure,members):
 if any(type(obj) is not dict for obj in [row,review,closure,members]):raise ValueError('source identity objects and archived raw members required')
 source=row.get('source_tree');kind=row.get('source_identity_kind');files=closure.get('files')
 if type(source) is not str or not re.fullmatch('[0-9a-f]{40}',source) or review.get('source_tree')!=source or closure.get('declared_source_sha')!=source:raise ValueError('frozen raw/review/registry source label differs')
 if type(files) is not dict or not files or any(type(k) is not str or not k or '\x00' in k or not sha256_string(v) or k.startswith('/') or '..' in Path(k).parts or Path(k).as_posix()!=k for k,v in files.items()):raise ValueError('original canonical relative-path source closure required')
 canonical=json.dumps(files,sort_keys=True,separators=(',',':')).encode()
 digest=hashlib.sha256(canonical).hexdigest()
 if closure.get('actual_source_closure_sha256')!=digest:raise ValueError('frozen source closure digest mismatch')
 dataset='docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'
 protected={dataset,'go.mod','go.sum','scripts/run_r5_benchmark.py','scripts/tests/test_r5_benchmark.py','internal/store/postgres/r5_benchmark_test.go'}
 if not protected.issubset(files):raise ValueError('incomplete benchmark dependency source closure')
 migrations=[int(m.group(1)) for path in files if (m:=re.fullmatch(r'internal/store/postgres/migrations/([0-9]+)_.*\.sql',path))]
 if not migrations or set(migrations)!=set(range(1,max(migrations)+1)) or len(migrations)!=len(set(migrations)):raise ValueError('actual unique frozen migration/schema closure required')
 schema=max(migrations)
 if kind=='git_write_tree':
  if (source,files[dataset],schema)!=FROZEN_S2 or review.get('source_sha_semantics')!='git_write_tree_label_not_commit':raise ValueError('Git identity is the exact historical frozen S2 only, never a canonical label')
  if any('source_identity_kind' in obj and obj['source_identity_kind']!=kind for obj in [review,closure]):raise ValueError('historical source kind cannot be relabelled')
  if any('actual_source_closure_sha256' in obj and obj['actual_source_closure_sha256']!=digest for obj in [row,review]) or any('source_identity_proof_member' in obj for obj in [row,review]):raise ValueError('historical Git closure cannot accept false hashes or a new canonical proof')
  return files,digest,schema
 if kind!=CANONICAL_SOURCE_KIND or review.get('source_identity_kind')!=kind or review.get('source_sha_semantics')!=CANONICAL_SOURCE_SEMANTICS or schema!=PIPELINE_SCHEMA_VERSION:raise ValueError('exact schema16 canonical closure identity required; not Git tree or commit')
 if 'source_identity_kind' in closure and closure['source_identity_kind']!=kind:raise ValueError('original source closure kind differs from canonical proof')
 if hashlib.sha1(canonical).hexdigest()!=source or any(obj.get('actual_source_closure_sha256')!=digest for obj in [row,review]):raise ValueError('canonical SHA1 source label and SHA256 closure must derive from the same actual files')
 if source==FROZEN_M_TIMEOUT_SOURCE and row.get('failure_variant')==M_TIMEOUT_VARIANT:
  m_failure_review_layout(row,review)
  if members.get('canonical-source-closure.json')!=canonical or members.get('source-label')!=(source+'\n').encode():raise ValueError('typed failed M requires actual compact sorted canonical bytes and original source label')
  receipt=archived_json(members,'source-receipt.json')
  expected={'source_sha':source,'source_identity_kind':kind,'source_sha_semantics':CANONICAL_SOURCE_SEMANTICS,'actual_source_closure_sha256':digest,'preparation_method':LOOKAHEAD_METHOD,'method_contract_sha256':FROZEN_M_METHOD_SHA256}
  if type(receipt) is not dict or receipt.get('files')!=files or any(receipt.get(k)!=v for k,v in expected.items()):raise ValueError('typed failed M original receipt/source/method differs from frozen actual files')
  signed=[{'path':'scripts/run_r5_benchmark.py','sha256':'33c70c07807977644bf52f025b6f872a2a55f4a5170be675bdd523b2ad071dd4'},{'path':'scripts/tests/test_r5_benchmark.py','sha256':'2a494eec68c65465c1160e990cb566e6509e56a0431504d9b11a5dc6494399bf'},{'path':'internal/store/postgres/r5_preparation_optin_test.go','sha256':'4cada7fa575e7572febda45eff1bcfc831f14f010f9e67b0afaa3d13c847016f'},{'path':'docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json','sha256':FROZEN_M_METHOD_SHA256}]
  if not exact_frozen_equal(receipt.get('actual_latest_signed_method_delta'),signed) or any(files.get(item['path'])!=item['sha256'] for item in signed) or files[dataset]!=LOOKAHEAD_DATASET_SHA256:raise ValueError('typed failed M original jointly signed method delta/contract closure required, never current-source refill')
  proof=archived_json(members,'source-post-execution-proof.json')
  expected_post={'source_sha':source,'actual_source_closure_sha256':digest,'selected_files_pre_post_identical':True,'no_live_main_WIP_added_to_M':True}
  if not exact_frozen_equal(proof,expected_post) or row.get('source_prepost_proof')!='source-post-execution-proof.json' or review.get('source_prepost_proof')!='source-post-execution-proof.json':raise ValueError('typed failed M actual unchanged pre/post source proof required')
  return files,digest,schema
 if row.get('source_tree')==FROZEN_LOOKAHEAD_S_SOURCE and run_review_layout(row,review)=='lookahead_s_receipt_native':
  proof_members=['source-receipt.json','source-label','canonical-source-closure.json','S/source-closure.json','S/method-selection.json']
  if not exact_frozen_equal(row.get('source_identity_proof_members'),proof_members) or 'source_identity_proof_member' in row or 'source_identity_proof_member' in review:raise ValueError('exact receipt-native proof member version required; no invented legacy proof')
  if members.get('canonical-source-closure.json')!=canonical or members.get('source-label')!=(source+'\n').encode():raise ValueError('actual receipt-native compact sorted canonical bytes/label required')
  receipt=archived_json(members,'source-receipt.json')
  if type(receipt) is not dict or receipt.get('files')!=files or any(receipt.get(k)!=v for k,v in {'source_sha':source,'source_identity_kind':kind,'source_sha_semantics':CANONICAL_SOURCE_SEMANTICS,'actual_source_closure_sha256':digest,'dataset_sha256':files[dataset],'preparation_method':LOOKAHEAD_METHOD,'method_contract_sha256':FROZEN_LOOKAHEAD_S_METHOD_SHA256}.items()):raise ValueError('actual receipt-native source/method/dataset bindings differ')
  method_path='docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json'
  validator_paths=[dataset,method_path,'scripts/run_r5_benchmark.py','scripts/tests/test_r5_benchmark.py']
  go_paths=['internal/store/postgres/r5_benchmark_test.go','internal/store/postgres/r5_preparation_optin_test.go','internal/store/postgres/r5_store_diagnostic_test.go']
  if any(path not in files for path in validator_paths+go_paths) or files[method_path]!=FROZEN_LOOKAHEAD_S_METHOD_SHA256 or receipt.get('validator_final_protected_hashes_matched')!={path:files[path] for path in validator_paths} or receipt.get('Go_final_author_hashes_matched')!={path:files[path] for path in go_paths}:raise ValueError('actual receipt-native original protected source hashes required')
  env=receipt.get('environment')
  if type(env) is not dict or env.get(METHOD_ENV)!=LOOKAHEAD_METHOD or env.get(METHOD_SHA_ENV)!=FROZEN_LOOKAHEAD_S_METHOD_SHA256:raise ValueError('actual source receipt selected-method environment binding required')
  return files,digest,schema
 proof_member=review.get('source_identity_proof_member')
 if type(proof_member) is not str or not proof_member:raise ValueError('explicit archived source identity proof member required')
 proof=archived_json(members,proof_member)
 if type(proof) is not dict or any(proof.get(k)!=v for k,v in {'source_sha':source,'source_identity_kind':kind,'source_sha_semantics':CANONICAL_SOURCE_SEMANTICS,'actual_source_closure_sha256':digest}.items()):raise ValueError('archived canonical identity proof differs from actual frozen closure')
 form=proof.get('canonicalization')
 expected={'canonical_bytes_length':len(canonical),'canonical_bytes_member':'canonical-source-closure.json','closure_algorithm':'SHA256(canonical_bytes)','encoding':'utf-8','implementation':"json.dumps(files,sort_keys=True,separators=(',',':')).encode()",'input':'original source-receipt.json files object of relative path -> individual file SHA256','label_algorithm':'SHA1(canonical_bytes)','python_ensure_ascii':True,'trailing_newline':False}
 if type(form) is not dict or form!=expected or type(form.get('canonical_bytes_length')) is not int or form.get('python_ensure_ascii') is not True or form.get('trailing_newline') is not False:raise ValueError('exact archived canonical byte algorithm required')
 if members.get(form['canonical_bytes_member'])!=canonical:raise ValueError('archived canonical bytes do not match actual source closure')
 original=proof.get('original_source_receipt')
 if type(original) is not dict or type(original.get('member')) is not str or original['member'] not in members or not sha256_string(original.get('sha256')) or hashlib.sha256(members[original['member']]).hexdigest()!=original['sha256']:raise ValueError('canonical proof requires original unchanged source-receipt member/hash')
 receipt=archived_json(members,original['member'])
 if type(receipt) is not dict or receipt.get('files')!=files or receipt.get('source_sha')!=source or receipt.get('actual_source_closure_sha256')!=digest:raise ValueError('original source receipt differs from measured source closure')
 return files,digest,schema

def receipt_native_method_review(row,review):
 if row.get('source_tree')==FROZEN_LOOKAHEAD_S_SOURCE:return run_review_layout(row,review)=='lookahead_s_receipt_native'
 if row.get('source_tree')==FROZEN_M_TIMEOUT_SOURCE and row.get('failure_variant')==M_TIMEOUT_VARIANT:return m_failure_review_layout(row,review)=='m_absolute_timeout_receipt_v1'
 return False


def validate_archived_method_selection(row,review,closure,members,prefix,files,source,scale):
 method=row.get('preparation_method',DEFAULT_METHOD)
 marker={'preparation_method','method_contract','method_contract_sha256'}
 if method==DEFAULT_METHOD:
  if marker.intersection(row) or marker.intersection(review) or {'preparation_method','method_contract_sha256'}.intersection(closure) or any(name in members for name in [prefix+'/method-selection.json',prefix+'/method-contract.json']):raise ValueError('historical/default v1 source cannot be backfilled with selected lookahead metadata')
  return method,None,None
 if method!=LOOKAHEAD_METHOD or scale not in {'S','M'}:raise ValueError('unknown or unauthorized original selected preparation method')
 rel='docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json';digest=row.get('method_contract_sha256')
 if any(obj.get('preparation_method')!=method or obj.get('method_contract_sha256')!=digest for obj in [review,closure]) or row.get('method_contract')!=Path(rel).name or (review.get('method_contract')!=row['method_contract'] and not receipt_native_method_review(row,review)) or not sha256_string(digest) or files.get(rel)!=digest:raise ValueError('selected method must bind registry/review/original source closure')
 raw=members.get(prefix+'/method-contract.json')
 if type(raw) is not bytes or hashlib.sha256(raw).hexdigest()!=digest:raise ValueError('actual archived method contract must match selected source digest')
 contract=validate_method_contract(parse_json(raw.decode()),frozen_contract_digest=digest)
 if scale not in contract['authorized_original_scales']:raise ValueError('historical frozen S-only method cannot authorize M')
 receipt=archived_json(members,prefix+'/method-selection.json')
 fields={'preparation_method','method_contract','method_contract_sha256','source_sha','source_identity_kind','actual_source_closure_sha256','argv','environment'}
 expected={'preparation_method':method,'method_contract':rel,'method_contract_sha256':digest,'source_sha':source,'source_identity_kind':CANONICAL_SOURCE_KIND,'actual_source_closure_sha256':closure['actual_source_closure_sha256']}
 if type(receipt) is not dict or set(receipt)!=fields or any(receipt.get(k)!=v for k,v in expected.items()):raise ValueError('actual selected method receipt source/contract mismatch')
 env={METHOD_ENV:method,METHOD_SHA_ENV:digest,'TABMAIL_R5_BENCHMARK_SOURCE_SHA':source,'TABMAIL_R5_BENCHMARK_SCALE':scale}
 if not exact_frozen_equal(receipt['environment'],env):raise ValueError('actual selected method environment binding required')
 argv=receipt['argv'];values={};flags=set()
 if type(argv) is not list or not argv or any(type(x) is not str or not x for x in argv) or Path(argv[0]).name!='run_r5_benchmark.py':raise ValueError('actual runner argv required')
 i=1
 while i<len(argv):
  arg=argv[i];i+=1
  if arg=='--execute-approved-budget':
   if arg in flags:raise ValueError('duplicate selected method argv flag')
   flags.add(arg);continue
  parts=arg.split('=',1);key=parts[0]
  if key not in {'--scale','--source-sha','--output-dir','--preparation-method','--method-contract'} or key in values:raise ValueError('unknown or duplicate selected method argv')
  if len(parts)==2:value=parts[1]
  else:
   if i>=len(argv):raise ValueError('missing selected method argv value')
   value=argv[i];i+=1
  if not value:raise ValueError('empty selected method argv value')
  values[key]=value
 if set(values)!={'--scale','--source-sha','--output-dir','--preparation-method','--method-contract'} or flags!={'--execute-approved-budget'} or values['--scale']!=scale or values['--source-sha']!=source or values['--preparation-method']!=method or Path(values['--method-contract']).name!=Path(rel).name or not Path(values['--output-dir']).is_absolute():raise ValueError('actual opt-in runner argv differs from selected method/source/scale contract')
 return method,contract,digest


def validate_delivered_review_payload(review_raw,members):
 if type(review_raw) is not bytes or not review_raw or members.get('S-runtime-review.json')!=review_raw:raise ValueError('delivered review bytes do not match original author payload inside bound archive')


def raw_exit_receipt(payload):
 if type(payload) is not bytes or re.fullmatch(rb'(?:0|-?[1-9][0-9]*)\n?',payload) is None:raise ValueError('actual raw exit requires canonical signed integer bytes; no float/bool/alias')
 return int(payload)


def register_run_identity(row,scale,source,schema,contract_hash,method,method_digest,result_hash,identities,observed_runs,run_ids):
 run_id=row.get('run_id')
 if run_id is not None and (type(run_id) is not str or re.fullmatch('[A-Za-z0-9][A-Za-z0-9._-]{0,127}',run_id) is None or run_id in run_ids):raise ValueError('unique unambiguous declared original run identifier required')
 identity=(scale,row['source_identity_kind'],source,schema,contract_hash,method,method_digest,run_id or result_hash)
 observed=(scale,source,schema,contract_hash,method,method_digest,result_hash)
 if identity in identities or observed in observed_runs:raise ValueError('duplicate or relabelled original method/source/dataset/schema/run identity')
 identities.add(identity);observed_runs.add(observed)
 if run_id is not None:run_ids.add(run_id)

def validate_typed_m_failure(row,review,directory):
 m_failure_review_layout(row,review)
 info,delivery=delivered_archive_binding(row,review,directory)
 members=archive_evidence(review,directory,row['archive'],delivered_binding=info)
 if set(members)!=set(delivery['payload_members']) or members.get('M-runtime-review.json')!=evidence_path(row['evidence'],directory).read_bytes():raise ValueError('typed M original 34 payloads and original review bytes required')
 closure=archived_json(members,'M/source-closure.json')
 files,digest,schema=validate_source_identity(row,review,closure,members)
 method,contract,method_digest=validate_archived_method_selection(row,review,closure,members,'M',files,row['source_tree'],'M')
 if row.get('frozen_method_contract_member')!='M/method-contract.json' or evidence_path(row.get('frozen_method_contract'),directory).read_bytes()!=members['M/method-contract.json']:raise ValueError('typed M original frozen method bytes must bind source and signed requested scale')
 if contract['dataset_contract_sha256']!=LOOKAHEAD_DATASET_SHA256 or contract['authorized_original_scales']!=['S','M']:raise ValueError('typed M failed attempt cannot borrow another dataset/scale contract')
 preflight=archived_json(members,'M/preflight.json')
 for key,value in {'scale':'M','population':list(SCALES['M']),'budget_seconds':10800,'disk_budget_bytes':80*(1<<30)}.items():
  if not exact_frozen_equal(preflight.get(key),value):raise ValueError('typed M original full requested population/resource budget must bind preflight')
 command=archived_json(members,'M/command.json')
 expected_command=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout=10800s','./internal/store/postgres','-run','^TestR5BenchmarkOriginalScale$']
 selection=archived_json(members,'M/method-selection.json');actual_command=archived_json(members,'actual-runner-command.json')
 if command!=expected_command or type(actual_command) is not list or len(actual_command)<3 or actual_command[1]!='-B' or not exact_frozen_equal(actual_command[2:],selection['argv']):raise ValueError('typed M actual original Go/runner argv and unchanged 10800 timeout required')
 if raw_exit_receipt(members.get('M/go.exit'))!=-15 or raw_exit_receipt(members.get('runner/M-runner.exit'))!=1:raise ValueError('typed M original parent-observed SIGTERM/runner rejection exits required')
 if any(name.endswith('/result.json') for name in members):raise ValueError('typed M timeout cannot carry a completed result')
 rejection=archived_json(members,'runner/M-runner.stdout')
 if not exact_frozen_equal(rejection,{'status':'rejected','task_complete':False,'product_green':False,'error':'absolute wall budget exceeded: cleanup observations recorded; original output preserved'}):raise ValueError('typed M original absolute-budget rejection required')
 receipt=archived_json(members,'M/execution-budget.json');timeout=archived_json(members,'M/timeout.json')
 if not exact_frozen_equal(timeout,{**receipt,'task_complete':False,'product_green':False}) or not exact_frozen_equal(receipt,review['budget_and_owned_process_cleanup_receipt']):raise ValueError('typed M original timeout/owned-process receipts must agree byte-derived observations')
 wall=['started_wall_time','absolute_deadline_wall_time','observed_wall_time','elapsed_wall_seconds','cleanup_observed_wall_time']
 if any(type(receipt.get(k)) not in {int,float} or not math.isfinite(receipt[k]) for k in wall):raise ValueError('typed M finite original absolute wall observations required')
 if receipt.get('cleanup_live_processes_absent') is not True or not exact_frozen_equal(receipt.get('cleanup_group_observation'),{'status':'observed','members':[],'non_zombie_live_pids':[]}) or receipt.get('cleanup_errors')!=[]:raise ValueError('typed M known owned process cleanup must remain actual observed, never unknown->success')
 if receipt.get('budget_seconds')!=10800 or type(receipt.get('budget_seconds')) is not int or receipt.get('timed_out') is not True or receipt.get('process_returncode')!=-15 or type(receipt.get('process_returncode')) is not int or receipt.get('process_returncode_status')!='observed_parent_wait' or abs(receipt['absolute_deadline_wall_time']-receipt['started_wall_time']-10800)>0.001 or abs(receipt['elapsed_wall_seconds']-receipt['observed_wall_time']+receipt['started_wall_time'])>0.001 or receipt['elapsed_wall_seconds']<10800:raise ValueError('typed M must be failed at original absolute10800 budget, never extended or counted as success')
 if row.get('elapsed_wall_seconds')!=receipt['elapsed_wall_seconds'] or review.get('observed_budget_elapsed_seconds')!=receipt['elapsed_wall_seconds'] or review.get('absolute_deadline_wall_time')!=receipt['absolute_deadline_wall_time']:raise ValueError('typed M timeout summary must bind original runner clock')
 raw=members['M/go.jsonl'];raw_digest=hashlib.sha256(raw).hexdigest()
 if row.get('raw_go_sha256')!=raw_digest or review.get('raw_go_sha256')!=raw_digest:raise ValueError('typed M actual raw Go lifecycle hash required')
 events=[parse_json(line) for line in raw.decode().splitlines() if line.strip()]
 if any(type(e) is not dict or e.get('Package')!='tabmail/internal/store/postgres' or e.get('Action') not in {'start','run','output'} for e in events) or len([e for e in events if e.get('Action')=='run' and e.get('Test')=='TestR5BenchmarkOriginalScale'])!=1:raise ValueError('typed M has original run only, no fabricated GoJSON PASS/FAIL/SKIP terminal')
 output=''.join(e.get('Output','') for e in events)
 parsed=[]
 for line in output.splitlines():
  for marker in ['PREPARATION_CHECKPOINT','PREPARATION_ACTUAL_CLAIM_JOINED','PREPARATION_TERMINAL']:
   if marker+' ' in line:parsed.append((marker,parse_json(line.split(marker+' ',1)[1])))
 pairs=[item for item in parsed if item[0]!='PREPARATION_TERMINAL'];terminal=[item[1] for item in parsed if item[0]=='PREPARATION_TERMINAL']
 if len(terminal)!=1 or len(pairs)!=14 or [x[0] for x in pairs]!=['PREPARATION_CHECKPOINT','PREPARATION_ACTUAL_CLAIM_JOINED']*7:raise ValueError('typed M original seven paired deep-ready claims and one incomplete terminal-labelled marker required')
 for index,depth in enumerate([20,1000,10000,100000,250000,500000,750000]):
  checkpoint=pairs[index*2][1];joined=pairs[index*2+1][1]
  if checkpoint.get('source_sha')!=row['source_tree'] or type(checkpoint.get('schema_version')) is not int or checkpoint['schema_version']!=16 or checkpoint.get('origin')!='captured_preparation_claim_same_parameters_observed_heap' or checkpoint.get('maintenance')!='no_explicit_analyze_or_planner_override' or checkpoint.get('observed_ready_jobs')!=depth or checkpoint.get('observed_durable_source_bound_ready')!=depth or checkpoint.get('observed_jobs_total')!=depth+20 or checkpoint.get('observed_pending')!=20 or checkpoint.get('observed_processing')!=0 or checkpoint.get('observed_failed')!=0 or checkpoint.get('observed_attempts_gt1')!=0 or checkpoint.get('claim_requested_limit')!=20 or checkpoint.get('explain_only_not_actual_claim_latency') is not True:raise ValueError('typed M actual source-bound legal Claim20 SQL checkpoint required')
  validate_explain_plan(checkpoint.get('plan'),'mail_index_jobs')
  before=validate_preparation_stats(checkpoint.get('preparation'),1000000,'before_claim',depth,preparation_method=LOOKAHEAD_METHOD)
  validate_preparation_stats(joined,1000000,'batch_joined',depth,preparation_method=LOOKAHEAD_METHOD)
  if (before['claim_query_sha256'],before['claim_parameters_sha256'])!=(checkpoint.get('query_sha256'),checkpoint.get('parameters_sha256')) or (joined['claim_query_sha256'],joined['claim_parameters_sha256'])!=(checkpoint.get('query_sha256'),checkpoint.get('parameters_sha256')) or abs(joined['claim_wall_seconds']-before['claim_wall_seconds']-joined['last_claim_wall_seconds'])>1e-9:raise ValueError('typed M paired actual claim timing/digests must not be substituted by EXPLAIN estimates')
 stats=terminal[0]
 if type(stats) is not dict or set(stats)!=PREPARATION_FIELDS or not exact_frozen_equal(stats,review['captured_preparation_incomplete_stats']):raise ValueError('typed M incomplete stats must bind original Go marker, never infer completion from TERMINAL label')
 require_integer_fields(stats,PREPARATION_COUNTS)
 if any(type(stats.get(k)) not in {int,float} or not math.isfinite(stats[k]) or stats[k]<0 for k in PREPARATION_WALL|PREPARATION_GUARD):raise ValueError('typed M finite bounded prep clock observations required')
 completed=stats['joined_completed_jobs'];stored=stats['joined_stored_messages']
 expected_stats={'method':LOOKAHEAD_METHOD,'phase':'before_claim','joined_inflight':0,'parser_joined_inflight':0,'parser_quiescent':True,'producer_done':False,'final_empty_claim':False,'sql_ready_count':0,'active_budget':20,'pending_upper_bound':20,'max_active':20,'max_window_messages':20,'max_claim_batch':20,'last_claim_batch':20,'parser_join_policy':PARSER_JOIN_POLICY,'parser_join_reserve_seconds':31,'parser_canceled_joined_calls':0,'parser_joined_calls':completed,'joined_ready_before_last_claim':completed,'claim_calls':completed//20+1}
 if any(not exact_frozen_equal(stats.get(k),v) for k,v in expected_stats.items()) or not 0<completed<1000000 or stored!=completed+20 or stored>=1000000 or stats['parser_min_start_remaining_wall_seconds']<=31 or stats['max_wall_monotonic_gap_ms']>100 or stats['company_wall_seconds']+stats['pipeline_wall_seconds']>10800:raise ValueError('typed M source partial joined/quiescent window20 must remain incomplete and budget bounded')
 validate_typed_m_failure_summary(row,stats)
 run=next(e for e in events if e.get('Action')=='run' and e.get('Test')=='TestR5BenchmarkOriginalScale')
 if abs(stats['parser_original_deadline_unix_seconds']-go_event_timestamp(run)-10800)>0.001:raise ValueError('typed M parser deadline must remain original Go test start+10800; outer runner remains authoritative earlier deadline')
 if 'fixture parser admission requires original absolute wall remaining>31s: context deadline exceeded' not in output or row.get('method_admission_claimed_no_failure_until_deadline') is not False:raise ValueError('typed M pre-deadline parser guard error must be disclosed, not a no-failure-until-timeout claim')
 sql=members.get('partial-actualSQL-counts.txt',b'').decode().splitlines()
 if len(sql)!=5 or sql[0]!='16' or re.fullmatch(r'[0-9]+\|[0-9]+\|[0-9]+\|[0-9]+',sql[1]) is None or re.fullmatch(r'processing\|[0-9]+',sql[2]) is None or re.fullmatch(r'ready\|[0-9]+',sql[3]) is None or re.fullmatch(r'[0-9]+',sql[4]) is None:raise ValueError('typed M actual owned SQL partial population checkpoint required, not requested full population')
 users,mailboxes,messages,documents=map(int,sql[1].split('|'));processing=int(sql[2].split('|')[1]);ready=int(sql[3].split('|')[1]);source_ready=int(sql[4])
 actual={'actual_schema':16,'actual_users_including_admins':users,'actual_mailboxes':mailboxes,'actual_stored_messages':messages,'actual_documents':documents,'actual_processing_jobs':processing,'actual_ready_jobs':ready,'actual_durable_current_sourcebound_SQL_ready':source_ready,'employees_requested_and_created':users-2}
 if (users,mailboxes,messages,documents,processing,ready,source_ready)!=(1002,5000,stored,completed,20,completed,completed) or not exact_frozen_equal(row.get('actual_partial_population'),actual) or not exact_frozen_equal(review['actual_population_counts_after_original_Go_join_and_before_native_stop'],actual):raise ValueError('typed M actual SQL partial counts must bind signed1000/5000/1M request without falsely claiming full 1M or schema17')
 corrections=read_json(evidence_path(row.get('readonly_reporting_corrections'),directory))
 if corrections.get('schema_version')!=1 or type(corrections.get('schema_version')) is not int or corrections.get('source_sha')!=row['source_tree'] or corrections.get('original_review')!=row['evidence'] or corrections.get('original_review_bytes_unchanged') is not True or corrections.get('raw_source')!=row['archive']+':M/go.jsonl' or corrections.get('Go_test_terminal')!='not_observed' or corrections.get('outer_process_exit')!=-15 or corrections.get('task_complete') is not False or corrections.get('product_green') is not False:raise ValueError('typed M readonly reporting corrections must bind original source/review/raw and failed-only role')
 corrected=corrections.get('actual_checkpoint_claims')
 if type(corrected) is not list or len(corrected)!=7:raise ValueError('typed M actual seven joined-claim reporting corrections required')
 for index,item in enumerate(corrected):
  checkpoint=pairs[index*2][1];joined=pairs[index*2+1][1]
  values={'ready':checkpoint['observed_ready_jobs'],'actual_claim_joined_seconds':joined['last_claim_wall_seconds'],'actual_claim_requested_limit':20,'actual_claim_batch':20,'joined_ready_before_last_claim':checkpoint['observed_ready_jobs'],'joined_inflight':0,'phase':'batch_joined','claim_query_sha256':checkpoint['query_sha256'],'claim_parameters_sha256':checkpoint['parameters_sha256']}
  if type(item) is not dict or any(not exact_frozen_equal(item.get(k),v) for k,v in values.items()):raise ValueError('typed M correction actual claim values must match paired raw Go, not null summary or plan estimates')
 cleanup=archived_json(members,'cleanup-receipt.json')
 if not exact_frozen_equal(cleanup,review['native_cleanup']) or cleanup.get('status')!='original_M_budget_timeout_all_owned_live_resources_stopped_residual_synthetic_state_preserved' or raw_exit_receipt(members.get('pg-final-stop.exit'))!=0 or cleanup.get('owned_postmaster_pid_absent') is not True or cleanup.get('owned_socket_absent') is not True or cleanup.get('budget_never_extended') is not True or cleanup.get('M_retried') is not False or cleanup.get('L_started') is not False or not cleanup.get('residual_DB') or not cleanup.get('residual_object_files') or members.get('terminal-owned-test-databases.txt')!=cleanup.get('terminal_DB_report','').encode():raise ValueError('typed M stopped native processes with privately retained partial DB/objects required; never claim DB0/objects removed')
 for key,value in {'baseline_complete':False,'result_JSON_absent':True,'result_sha256':None,'matrix14by200_completed':False,'completed_workload_samples':0,'full_six_stream_fingerprint_computed':False,'timed_out':True}.items():
  if not exact_frozen_equal(row.get(key),value):raise ValueError('typed M failure registry cannot imply baseline, fingerprint or performance samples')
 return {'failure_variant':M_TIMEOUT_VARIANT,'source_tree':row['source_tree'],'schema_version':schema,'method_contract_sha256':method_digest,'raw_go_sha256':raw_digest,'baseline_complete':False,'task_complete':False,'product_green':False,'actual_partial_population':actual,'elapsed_clock':'original_runner_absolute_wall_timeout_not_completed_execution_or_GoJSON_terminal'}


def validate_registry(registry,*,directory=None):
 directory=DATASET.parent if directory is None else Path(directory)
 if type(registry) is not dict or type(registry.get('schema_version')) is not int or registry['schema_version']!=1 or type(registry.get('task_complete')) is not bool or registry.get('product_green') is not False or registry.get('L_executed') is not False:raise ValueError('strict baseline registry state required')
 runs=registry.get('runs');failed=registry.get('failed_attempts',[])
 if type(runs) is not list or type(failed) is not list:raise ValueError('run and failed-attempt lists required')
 scales=set();identities=set();observed_runs=set();run_ids=set();typed_failed=[];failed_identities=set();elapsed_clocks=[]
 for row in runs:
  if type(row) is not dict or row.get('scale') not in {'S','M'}:raise ValueError('original S/M run identities required; TOOL_ONLY is not a scale')
  scale=row['scale'];scales.add(scale);source=row.get('source_tree')
  require_integer_fields(row,['budget_seconds'])
  if type(source) is not str or not re.fullmatch('[0-9a-f]{40}',source) or row.get('source_identity_kind') not in {'git_write_tree',CANONICAL_SOURCE_KIND}:raise ValueError('explicit immutable source identity required, not an arbitrary40hex/current HEAD claim')
  if type(row.get('population')) is not list or any(type(v) is not int for v in row['population']) or tuple(row['population'])!=SCALES[scale] or row['budget_seconds']!=BUDGETS[scale][0] or row.get('baseline_complete') is not True or row.get('performance_green') is not False:raise ValueError('original complete population/budget and non-product baseline required')
  review_path=evidence_path(row.get('evidence'),directory);review_raw=review_path.read_bytes();review=parse_json(review_raw.decode());layout=run_review_layout(row,review)
  go_review=review.get('Go_exit') if layout=='lookahead_s_receipt_native' else review.get('go_exit')
  if review.get('scale')!=scale or review.get('source_tree')!=source or not exact_frozen_equal(review.get('population'),row['population']) or go_review!=0 or type(go_review) is not int or review.get('runner_exit')!=0 or type(review.get('runner_exit')) is not int or review.get('baseline_complete') is not True:raise ValueError('independent original baseline review differs from registry')
  delivery_entry=None;delivered_binding=None
  if layout!='embedded_archive':delivered_binding,delivery_entry=delivered_archive_binding(row,review,directory)
  members=archive_evidence(review,directory,row.get('archive'),delivered_binding=delivered_binding)
  if layout!='embedded_archive':validate_delivered_review_payload(review_raw,members)
  results=[name for name in members if name.endswith('/result.json')]
  if len(results)!=1:raise ValueError('exactly one original result required')
  result_member=results[0];prefix=result_member.rsplit('/',1)[0]
  result=archived_json(members,result_member);closure=archived_json(members,prefix+'/source-closure.json')
  files,digest,schema=validate_source_identity(row,review,closure,members)
  if 'schema_version' in row and (type(row['schema_version']) is not int or row['schema_version']!=schema):raise ValueError('declared run schema differs from actual frozen migration closure')
  if layout=='pipeline_s_v1_delivered' and (type(review['schema_version']) is not int or review['schema_version']!=schema):raise ValueError('delivered v1 review schema differs from actual frozen migration closure')
  if layout!='embedded_archive' and (review['task_complete'] is not False or review['product_green'] is not False or review['dataset_sha256']!=files['docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json']):raise ValueError('delivered review cannot claim completion or replace frozen dataset')
  dataset_path='docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'
  frozen=evidence_path(row.get('dataset_contract'),directory);contract_hash=hashlib.sha256(frozen.read_bytes()).hexdigest()
  if not sha256_string(row.get('dataset_contract_sha256')) or contract_hash!=row['dataset_contract_sha256'] or files[dataset_path]!=contract_hash:raise ValueError('historical contract must match actual frozen source, never current metadata')
  legacy=(source,contract_hash,schema)==FROZEN_S2
  method,method_contract,method_digest=validate_archived_method_selection(row,review,closure,members,prefix,files,source,scale)
  if method_digest and 'frozen_method_contract' in row:
   if row.get('frozen_method_contract_member')!=prefix+'/method-contract.json' or evidence_path(row['frozen_method_contract'],directory).read_bytes()!=members[prefix+'/method-contract.json']:raise ValueError('explicit historical frozen method copy differs from original archived bytes')
  if method_digest and method_digest!=FROZEN_LOOKAHEAD_S_METHOD_SHA256 and hashlib.sha256(evidence_path(row['method_contract'],directory).read_bytes()).hexdigest()!=method_digest:raise ValueError('frozen selected method file differs from original archived source')
  contract=validate_contract(read_json(frozen),frozen_legacy=legacy)
  verdict=validate_result(result,contract,scale,source,dataset_digest=contract_hash,source_schema_version=schema,frozen_legacy=legacy,preparation_method=method,method_contract=method_contract,method_contract_digest=method_digest)
  result_hash=hashlib.sha256(members[result_member]).hexdigest()
  validate_review_result_binding(row,review,members,prefix,result_hash,layout,directory,delivery_entry)
  register_run_identity(row,scale,source,schema,contract_hash,method,method_digest,result_hash,identities,observed_runs,run_ids)
  if row.get('candidate_threshold_passed') is not verdict['candidate_threshold_passed']:raise ValueError('candidate threshold red/green must match unmodified real values')
  if layout=='pipeline_s_v1_delivered':
   claims={'preparation':'preparation','actual_distribution':'actual_distribution','fingerprint_streams':'fingerprint_streams','dataset_fingerprint':'dataset_fingerprint','execution_wall':'execution_wall','safety_assertions':'safety_assertions'}
   if any(not exact_frozen_equal(review[k],result[v]) for k,v in claims.items()) or not exact_frozen_equal(review['candidate_result'],verdict):raise ValueError('delivered v1 author review differs from archived actual observations')
  elif layout=='lookahead_s_receipt_native':
   claims={'preparation':'preparation','actual_scheduler':'preparation_input_scheduler','actual_UID_received_order_observations':'preparation_persistent_observations','persistent_equivalence_disclosure':'preparation_identity_boundary','fingerprint_streams':'fingerprint_streams','fingerprint':'dataset_fingerprint','execution_wall':'execution_wall','all7safety':'safety_assertions','measurements':'measurements'}
   if any(not exact_frozen_equal(review[k],result[v]) for k,v in claims.items()) or not exact_frozen_equal(review['strict_validator_result'],verdict):raise ValueError('receipt-native author review differs from actual selected-method observations')
  try:
   go_exit=raw_exit_receipt(members[prefix+'/go.exit'])
   events=validate_go_event_text(members[prefix+'/go.jsonl'].decode())
   runner_members=[name for name in members if name.startswith('runner/') and name.endswith('-runner.exit')]
   if len(runner_members)!=1 or raw_exit_receipt(members[runner_members[0]])!=0 or go_exit!=0:raise ValueError('original Go/runner process must actually pass')
  except KeyError as exc:raise ValueError('missing original Go/runner raw exit') from exc
  validate_run_wall(events,BUDGETS[scale][0])
  validate_package_elapsed_observation(events,review,layout,BUDGETS[scale][0])
  if 'clock_contract' in contract:
   receipt=archived_json(members,prefix+'/execution-budget.json')
   wallkeys=['started_wall_time','absolute_deadline_wall_time','observed_wall_time','elapsed_wall_seconds','budget_seconds']
   if any(type(receipt.get(k)) not in {int,float} or not math.isfinite(receipt[k]) for k in wallkeys):raise ValueError('actual runner wall-budget receipt required')
   start=receipt['started_wall_time'];end=receipt['observed_wall_time'];budget=BUDGETS[scale][0]
   if type(receipt['budget_seconds']) is not int:raise ValueError('integer original runner wall budget required')
   if receipt.get('timed_out') is not False or type(receipt.get('process_returncode')) is not int or receipt['process_returncode']!=0 or receipt['budget_seconds']!=budget or abs(receipt['absolute_deadline_wall_time']-start-budget)>0.001 or abs(receipt['elapsed_wall_seconds']-end+start)>0.001 or not 0<=end-start<budget:raise ValueError('whole-process wall budget exceeded or false completion receipt')
   execution=result['execution_wall']
   if execution['started_unix_seconds']<start-0.001 or execution['completed_unix_seconds']>end+0.001:raise ValueError('actual Go execution escapes recorded runner wall interval')
  clock=validate_run_elapsed_summary(row,review,result,events,layout,scale)
  elapsed_clocks.append({'source_tree':source,'scale':scale,'summary_clock':clock,'row_elapsed_seconds':row['elapsed_seconds'],'actual_go_test_pass_elapsed_seconds':next(e['Elapsed'] for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action')=='pass'),'actual_result_execution_wall_elapsed_seconds':result.get('execution_wall',{}).get('elapsed_wall_seconds')})
  command=archived_json(members,prefix+'/command.json')
  expected_command=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout='+str(BUDGETS[scale][0])+'s','./internal/store/postgres','-run','^TestR5BenchmarkOriginalScale$']
  if command!=expected_command:raise ValueError('original Go command/timeout/run scope differs from frozen scale contract')
  preflight=archived_json(members,prefix+'/preflight.json')
  if preflight.get('scale')!=scale or preflight.get('population')!=list(SCALES[scale]) or preflight.get('budget_seconds')!=BUDGETS[scale][0] or preflight.get('disk_budget_bytes')!=BUDGETS[scale][1]*(1<<30):raise ValueError('original raw preflight population/resource budget mismatch')
 for scale in ['S','M']:
  if type(registry.get(scale+'_executed')) is not bool or registry[scale+'_executed']!=(scale in scales):raise ValueError('completion flags must match validated actual baselines, not attempts')
 for row in failed:
  if type(row) is not dict or row.get('scale') not in {'S','M'}:raise ValueError('failed attempt original scale required')
  scale=row['scale'];review=read_json(evidence_path(row.get('evidence'),directory));require_integer_fields(row,['budget_seconds'])
  if 'failure_variant' in row:
   if row['failure_variant']!=M_TIMEOUT_VARIANT:raise ValueError('unknown failed attempt version')
   proof=validate_typed_m_failure(row,review,directory)
   identity=(proof['source_tree'],proof['schema_version'],proof['method_contract_sha256'],proof['raw_go_sha256'])
   if identity in failed_identities:raise ValueError('duplicate typed original M failed attempt identity')
   failed_identities.add(identity);typed_failed.append(proof);continue
  if 'go_exit' not in row or 'go_exit' not in review:raise ValueError('explicit failed Go exit observation required')
  unknown=row.get('go_exit') is None
  if not unknown:require_integer_fields(row,['go_exit'])
  if type(row.get('population')) is not list or any(type(v) is not int for v in row['population']) or type(row.get('source_tree')) is not str or not re.fullmatch('[0-9a-f]{40}',row['source_tree']):raise ValueError('strict original failed population/source required')
  if row['go_exit']==0 or row['budget_seconds']!=BUDGETS[scale][0] or tuple(row.get('population',[]))!=SCALES[scale] or row.get('source_tree')!=review.get('source_tree') or row['go_exit']!=review.get('go_exit') or review.get('runner_exit')==0:raise ValueError('failed attempts cannot be passed baselines or relabelled sources')
  if type(review.get('runner_exit')) is not int or review['runner_exit']==0:raise ValueError('failed benchmark actual runner rejection required')
  members=archive_evidence(review,directory);exits=[name for name in members if name.endswith('/go.exit')]
  if unknown:
   if any(key in obj and obj[key] is not False for obj in [row,review] for key in ['baseline_complete','task_complete','product_green']):raise ValueError('unknown failed observations cannot claim completion')
   if scale!='M':raise ValueError('unknown EPERM failure variant is the original M attempt only')
   missing=['go.exit','execution-budget.json','timeout.json','result.json']
   if row.get('status')!='failed' or review.get('status')!='failed' or row.get('go_exit_status')!='missing_observation' or review.get('go_exit_status')!='missing_observation' or row.get('missing_observations')!=missing or review.get('missing_observations')!=missing:raise ValueError('unknown Go exit requires explicit failed/missing-observation schema')
   if exits or any(name.endswith('/'+missing_file) for name in members for missing_file in missing):raise ValueError('declared missing observations actually present')
   raw_go=[name for name in members if name.endswith('/go.jsonl')]
   if len(raw_go)!=1:raise ValueError('unknown failed attempt requires unique original Go lifecycle')
   prefix=raw_go[0].rsplit('/',1)[0]
   if prefix+'/go.stderr' not in members:raise ValueError('unknown failed attempt requires original Go stderr even if empty')
   raw_runner=[name for name in members if name.startswith('runner/') and name.endswith('-runner.json')]
   if len(raw_runner)!=1:raise ValueError('unknown failed attempt requires raw runner rejection')
   rejection=archived_json(members,raw_runner[0])
   if rejection.get('status')!='rejected' or rejection.get('task_complete') is not False or rejection.get('product_green') is not False or rejection.get('error')!='[Errno 1] Operation not permitted':raise ValueError('unknown failed attempt requires exact original EPERM rejection')
   if review['runner_exit']!=1 or type(row.get('runner_exit')) is not int or row['runner_exit']!=1:raise ValueError('unknown failed EPERM attempt original runner exit is one')
   if raw_runner[0][:-5]+'.stderr' not in members:raise ValueError('unknown failed original runner stderr required')
   closure=archived_json(members,prefix+'/source-closure.json');files=closure.get('files')
   if type(files) is not dict or any(type(k) is not str or not sha256_string(v) or k.startswith('/') or '..' in Path(k).parts for k,v in files.items()):raise ValueError('unknown failed source closure required')
   digest=hashlib.sha256(json.dumps(files,sort_keys=True,separators=(',',':')).encode()).hexdigest()
   protected={'docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json','go.mod','go.sum','scripts/run_r5_benchmark.py','scripts/tests/test_r5_benchmark.py','internal/store/postgres/r5_benchmark_test.go'}
   if closure.get('actual_source_closure_sha256')!=digest or not protected.issubset(files):raise ValueError('unknown failed exact original dependency provenance required')
   migrations=[int(m.group(1)) for path in files if (m:=re.fullmatch(r'internal/store/postgres/migrations/([0-9]+)_.*\.sql',path))]
   if not migrations or set(migrations)!=set(range(1,max(migrations)+1)):raise ValueError('unknown failed frozen migration closure required')
   preflight=archived_json(members,prefix+'/preflight.json')
   if preflight.get('scale')!=scale or preflight.get('population')!=list(SCALES[scale]) or type(preflight.get('budget_seconds')) is not int or preflight['budget_seconds']!=BUDGETS[scale][0] or preflight.get('disk_budget_bytes')!=BUDGETS[scale][1]*(1<<30):raise ValueError('unknown failed original resource budget required')
   command=archived_json(members,prefix+'/command.json')
   expected_command=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout='+str(BUDGETS[scale][0])+'s','./internal/store/postgres','-run','^TestR5BenchmarkOriginalScale$']
   if command!=expected_command:raise ValueError('unknown failed original Go command scope required')
  else:
   if len(exits)!=1 or raw_exit_receipt(members[exits[0]])!=row['go_exit']:raise ValueError('failed attempt original raw exit required')
   prefix=exits[0].rsplit('/',1)[0]
  runners=[name for name in members if name.startswith('runner/') and name.endswith('-runner.exit')]
  if len(runners)!=1 or raw_exit_receipt(members[runners[0]])!=review['runner_exit'] or ('runner_exit' in row and (type(row['runner_exit']) is not int or row['runner_exit']!=review['runner_exit'])):raise ValueError('failed attempt raw runner exit differs from review')
  if any(name.endswith('/result.json') for name in members):raise ValueError('failed attempt cannot carry a completed result')
  closure=archived_json(members,prefix+'/source-closure.json')
  if closure.get('declared_source_sha')!=row['source_tree']:raise ValueError('failed original raw source label differs from registry')
  events=[parse_json(line) for line in members.get(prefix+'/go.jsonl',b'').decode().splitlines() if line.strip()]
  if len([e for e in events if e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action')=='run'])!=1 or any(e.get('Test')=='TestR5BenchmarkOriginalScale' and e.get('Action') in {'pass','skip'} for e in events):raise ValueError('failed attempt requires raw original run without pass/skip')
  if unknown and (any(e.get('Action') in {'pass','fail','skip'} for e in events) or any(e.get('Package')!='tabmail/internal/store/postgres' for e in events)):raise ValueError('unknown original EPERM failure has no Go terminal outcome')
 if registry['task_complete']:
  if scales!={'S','M'}:raise ValueError('task approval requires both original baselines and independent fresh gates')
  reference=registry.get('fresh_gate_evidence')
  if type(reference) is not dict or not sha256_string(reference.get('sha256')):raise ValueError('actual independent fresh gate reference required')
  path=evidence_path(reference.get('review'),directory)
  if hashlib.sha256(path.read_bytes()).hexdigest()!=reference['sha256']:raise ValueError('fresh gate review hash mismatch')
  fresh=read_json(path)
  if fresh.get('source_tree')!=reference.get('source_tree') or fresh.get('unknown_errors')!=0 or type(fresh.get('unknown_errors')) is not int or fresh.get('independent_contract_review') is not True:raise ValueError('fresh gate source/unknown errors/independent approval required')
  fresh_members=archive_evidence(fresh,directory)
  gates=fresh.get('required_gate_results');required=fresh.get('required_gates')
  if type(gates) is not list or not gates or type(required) is not list or not required or any(type(g) is not dict for g in gates) or {g.get('gate') for g in gates}!=set(required) or len(gates)!=len(set(required)):raise ValueError('all explicitly required fresh gate results required')
  for gate in gates:
   classifier=archived_json(fresh_members,gate.get('classifier_member'))
   if classifier.get('source_tree')!=reference.get('source_tree') or classifier.get('unknown_errors')!=0 or type(classifier.get('unknown_errors')) is not int or classifier.get('status') not in {'passed','expected_target_red'}:raise ValueError('fresh gate classifier must prove source and unknown-error boundary')
   if gate.get('raw_member') not in fresh_members or gate.get('exit_member') not in fresh_members or type(gate.get('expected_exit')) is not int or raw_exit_receipt(fresh_members[gate['exit_member']])!=gate['expected_exit']:raise ValueError('fresh classifier requires matching original raw/exit')
 return {'status':'baseline_registry_evidence_validated','validated_scales':sorted(scales),'validated_elapsed_clock_boundaries':elapsed_clocks,'validated_typed_failed_attempts':typed_failed,'validated_run_count':len(identities),'validated_run_identities':[dict(zip(['scale','source_identity_kind','source_tree','schema_version','dataset_contract_sha256','preparation_method','method_contract_sha256','run_identity'],identity)) for identity in sorted(identities,key=lambda item:json.dumps(item))],'task_complete':False,'product_green':False,'boundary':'Original immutable baseline evidence validated; registry task approval is separately owned, never inferred from current HEAD or this validator'}

def main():
 parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--scale',choices=SCALES,default='S');parser.add_argument('--output-dir',type=Path);parser.add_argument('--execute-approved-budget',action='store_true');parser.add_argument('--source-sha');parser.add_argument('--validate-tool-result',type=Path);parser.add_argument('--validate-registry',type=Path);parser.add_argument('--preparation-method',choices=[DEFAULT_METHOD,LOOKAHEAD_METHOD],default=DEFAULT_METHOD);parser.add_argument('--method-contract',type=Path);parser.add_argument('--source-policy',choices=source_inventory.SUPPORTED_POLICIES);parser.add_argument('--source-manifest',type=Path);parser.add_argument('--source-manifest-sha256');args=parser.parse_args()
 try:
  for option in ['--scale','--output-dir','--execute-approved-budget','--source-sha','--validate-tool-result','--validate-registry','--preparation-method','--method-contract','--source-policy','--source-manifest','--source-manifest-sha256']:
   if sum(arg.split('=',1)[0]==option for arg in sys.argv[1:])>1:raise ValueError('duplicate runner argv option: '+option)
  contract=validate_contract(read_json(DATASET));method=args.preparation_method;method_contract=None;method_digest=None
  if args.validate_registry:
   if args.execute_approved_budget or args.validate_tool_result or method!=DEFAULT_METHOD or args.method_contract or args.source_policy or args.source_manifest or args.source_manifest_sha256:raise ValueError('registry validation never executes or injects a selected method')
   print(json.dumps(validate_registry(read_json(args.validate_registry))));return 0
  if method==LOOKAHEAD_METHOD:
   if not args.method_contract or args.method_contract.resolve()!=LOOKAHEAD_METHOD_FILE.resolve():raise ValueError('explicit canonical lookahead method contract path required')
   method_contract=validate_method_contract(read_json(args.method_contract));method_digest=hashlib.sha256(args.method_contract.read_bytes()).hexdigest()
   if hashlib.sha256(DATASET.read_bytes()).hexdigest()!=LOOKAHEAD_DATASET_SHA256:raise ValueError('lookahead original dataset digest differs from frozen method')
   if not args.validate_tool_result and args.scale not in {'S','M'}:raise ValueError('lookahead is not authorized for original L')
  elif args.method_contract:raise ValueError('method contract requires explicit lookahead opt-in')
  current = None
  if args.validate_tool_result or args.execute_approved_budget:
   if args.source_policy==source_inventory.CURRENT_POLICY:
    raise ValueError('SMTP-owner source policy v3 is SOURCE-only here: no admitted benchmark METHOD or schema18/19-to-schema16 fallback')
   if args.source_policy!=source_inventory.POLICY or not args.source_manifest or not args.source_manifest_sha256:
    raise ValueError('current tool/execution requires explicit v2 source policy and pinned manifest; no legacy fallback')
   if method!=DEFAULT_METHOD:
    raise ValueError('current v2 lookahead blocked: frozen METHOD binds legacy files-only SHA1; independent method migration required')
   current=source_inventory.load_current_source(args.source_manifest,args.source_manifest_sha256,ROOT,purpose='benchmark',policy=args.source_policy)
   if args.source_sha!=current['source_sha']:raise ValueError('current source label must bind policy/context/full local input superset')
  elif args.source_policy or args.source_manifest or args.source_manifest_sha256:
   raise ValueError('source receipt options require current tool validation or execution')
  if args.validate_tool_result:
   if args.execute_approved_budget:raise ValueError('tool evidence validation never executes a benchmark')
   result=validate_tool_result(read_json(args.validate_tool_result),contract,args.source_sha,preparation_method=method,method_contract=method_contract,method_contract_digest=method_digest)
   result.update(source_policy=current['policy'],source_identity_kind=current['source_identity_kind'],source_closure_sha256=current['source_closure_sha256'],source_identity_boundary=current['boundary'])
   print(json.dumps(result));return 0
  report=preflight(contract,args.scale,Path(os.getenv('TMPDIR','/tmp')),preparation_method=method)
  if not args.execute_approved_budget:print(json.dumps(report,ensure_ascii=False,indent=2));return 0
  if contract.get('clock_contract')!=CLOCK_CONTRACT:raise ValueError('new execution requires explicit absolute wall and sample clock contract')
  if args.scale=='L':raise ValueError('L execution is not authorized in this batch')
  if not args.output_dir or not args.source_sha or not re.fullmatch('[0-9a-f]{40}',args.source_sha) or not os.getenv('TABMAIL_TEST_DB_DSN'):raise ValueError('explicit fresh output/source identity and owned DSN required')
  out=args.output_dir.resolve();out.mkdir(mode=0o700,parents=True,exist_ok=False)
  env=source_inventory.bound_environment(current['build_context'],os.environ)
  env.update(TABMAIL_R5_BENCHMARK_SCALE=args.scale,TABMAIL_R5_BENCHMARK_OUTPUT=str(out/'result.json'),TABMAIL_R5_BENCHMARK_SOURCE_SHA=args.source_sha)
  env[METHOD_ENV]=method
  if method_digest:env[METHOD_SHA_ENV]=method_digest
  else:env.pop(METHOD_SHA_ENV,None)
  command=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout='+str(report['budget_seconds'])+'s','./internal/store/postgres','-run','^TestR5BenchmarkOriginalScale$']
  git_head=observed_git_head()
  closure=dict(current,declared_source_sha=args.source_sha,observed_git_head=git_head,source_manifest_sha256=args.source_manifest_sha256)
  (out/'source-closure.json').write_text(json.dumps(closure,sort_keys=True,indent=2)+'\n')
  (out/'preflight.json').write_text(json.dumps(report,indent=2)+'\n');(out/'command.json').write_text(json.dumps(command)+'\n')
  receipt={}
  try:
   with (out/'go.jsonl').open('w') as stdout,(out/'go.stderr').open('w') as stderr:returncode=run_owned_process(command,env=env,stdout=stdout,stderr=stderr,timeout=report['budget_seconds'],receipt=receipt)
  finally:
   write_execution_receipt(out,receipt)
  after=current_source_receipt(policy=args.source_policy,build_context=current['build_context'],preparation_method=method)
  (out/'source-closure-after.json').write_text(json.dumps(after,sort_keys=True,indent=2)+'\n')
  if returncode!=0 or after!=current:raise ValueError('benchmark failed or source drifted; original output preserved')
  validate_run_wall(validate_go_events(out/'go.jsonl'),report['budget_seconds'])
  result=validate_result(read_json(out/'result.json'),contract,args.scale,args.source_sha,require_wall_clock=True,preparation_method=method,method_contract=method_contract,method_contract_digest=method_digest);print(json.dumps(result));return 0
 except (ValueError,KeyError,TypeError,OSError,tarfile.TarError,subprocess.SubprocessError) as exc:print(json.dumps({'status':'rejected','task_complete':False,'product_green':False,'error':str(exc)}));return 1
if __name__=='__main__':raise SystemExit(main())
