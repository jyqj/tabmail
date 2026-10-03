import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest
import json
import os
import subprocess
import signal
import sys
import io
import shutil
import tarfile
import re
from unittest import mock

spec=importlib.util.spec_from_file_location('benchmark',Path(__file__).resolve().parents[1]/'run_r5_benchmark.py')
bench=importlib.util.module_from_spec(spec);spec.loader.exec_module(bench)

def current_inventory_fixture(root):
 spec=importlib.util.spec_from_file_location('current_inventory_fixtures',Path(__file__).with_name('test_r5_current_source_inventory.py'))
 module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
 module.fixture(root)
 return module.context()


class BenchmarkContractTests(unittest.TestCase):
 def setUp(self):self.data=bench.read_json(bench.DATASET)
 def reject(self,modify):
  data=copy.deepcopy(self.data);modify(data)
  with self.assertRaises(ValueError):bench.validate_contract(data)
 def test_original_scales_and_no_runtime_registry(self):
  bench.validate_contract(self.data)
  self.assertEqual(bench.SCALES['S'],(100,500,100000));self.assertEqual(bench.SCALES['M'],(1000,5000,1000000));self.assertEqual(bench.SCALES['L'][2],10000000)
  registry=bench.read_json(bench.ROOT/'docs/company-mail/evidence/R5-BENCHMARK-RUNS.json')
  verdict=bench.validate_registry(registry)
  self.assertEqual(verdict['validated_scales'],sorted({row['scale'] for row in registry['runs']}));self.assertFalse(verdict['product_green'])
  proofs=verdict['validated_run_identities'];self.assertEqual(verdict['validated_run_count'],len(registry['runs']));self.assertEqual(len(proofs),len(registry['runs']))
  keys=['scale','source_identity_kind','source_tree','dataset_contract_sha256','preparation_method','method_contract_sha256','run_identity']
  observed={tuple(proof[key] for key in keys) for proof in proofs}
  expected={(row['scale'],row['source_identity_kind'],row['source_tree'],row['dataset_contract_sha256'],row.get('preparation_method',bench.DEFAULT_METHOD),row.get('method_contract_sha256'),row.get('run_id') or row['result_sha256']) for row in registry['runs']}
  self.assertEqual(observed,expected)
  historical={bench.FROZEN_S2[0]:15,bench.FROZEN_PIPELINE_S_SOURCE:16,bench.FROZEN_LOOKAHEAD_S_SOURCE:16}
  actual_history=[proof for proof in proofs if proof['source_tree'] in historical]
  self.assertEqual(len(actual_history),3);self.assertEqual({proof['source_tree'] for proof in actual_history},set(historical))
  self.assertTrue(all(proof['scale']=='S' and proof['schema_version']==historical[proof['source_tree']] for proof in actual_history))
 def test_reduced_scale_seed_concurrency_and_shape_rejected(self):
  for scale in ['S','M','L']:
   for field in ['employees','mailboxes','messages']:
    with self.subTest(scale=scale,field=field):self.reject(lambda d:d['scales'][scale].__setitem__(field,1))
  self.reject(lambda d:d.__setitem__('seed',1));self.reject(lambda d:d.__setitem__('concurrency',1))
  self.reject(lambda d:d['message_sizes'][0].__setitem__('bytes',128));self.reject(lambda d:d['population'].__setitem__('shared_grants_per_mailbox',1))
 def test_metrics_workloads_thresholds_cold_and_approval_not_weakened(self):
  self.reject(lambda d:d['workloads'].pop());self.reject(lambda d:d['required_metrics'].remove('sql_count'))
  self.reject(lambda d:d['candidate_thresholds'].__setitem__('M_list_p95_ms',99999));self.reject(lambda d:d.__setitem__('cache_modes',['fully_OS_cold']))
  self.reject(lambda d:d.__setitem__('task_complete',True));self.reject(lambda d:d.__setitem__('product_green',True))
 def test_exact_original_wall_disk_budgets_and_clock_contract(self):
  for scale in ['S','M','L']:
   for field in ['budget_seconds','disk_budget_gib']:
    for value in [self.data['scales'][scale][field]+1,1.0,True]:
     with self.subTest(scale=scale,field=field,value=value):self.reject(lambda d:d['scales'][scale].__setitem__(field,value))
  self.reject(lambda d:d['clock_contract'].__setitem__('max_wall_monotonic_gap_ms',1000))
  self.reject(lambda d:d['clock_contract'].__setitem__('run_deadline','monotonic_sleep_excluded'))
 def test_synthetic_dispatch_capacity_keeps_real_disk_margin_gate(self):
  required=self.data['scales']['S']['disk_budget_gib']*(1<<30)+10*(1<<30)
  with mock.patch.object(bench.shutil,'disk_usage',return_value=shutil._ntuple_diskusage(required,1,required-1)):
   with self.assertRaisesRegex(ValueError,'^insufficient disk safety margin$'):
    bench.preflight(self.data,'S',Path('/synthetic'))
  with mock.patch.object(bench.shutil,'disk_usage',return_value=shutil._ntuple_diskusage(required,0,required)):
   report=bench.preflight(self.data,'S',Path('/synthetic'))
  self.assertEqual(report['disk_budget_bytes'],10<<30)
  self.assertFalse(report['actual_scale_run']);self.assertFalse(report['product_green'])

 def test_duplicate_json_rejected(self):
  with tempfile.TemporaryDirectory() as root:
   p=Path(root)/'bad.json';p.write_text('{"seed":1,"seed":2}')
   with self.assertRaises(ValueError):bench.read_json(p)
 def test_current_preparation_contract_cannot_be_missing_or_weakened(self):
  self.reject(lambda d:d.pop('preparation_contract'))
  for field,value in [('method','serial_unjoined'),('schema_version',15),('active_budget',21),('window_messages',100),('claim_requested_limit',100),('parser_concurrency',20),('parser_timeout_seconds',31),('parser_join_reserve_seconds',30),('parser_join_policy','cancel_waiter'),('checkpoint_policy','terminal_only'),('maintenance','analyze'),('fingerprint_method','parameter_only')]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_contract'].__setitem__(field,value))
  self.reject(lambda d:d['preparation_contract'].__setitem__('unknown_protocol',True))
  self.reject(lambda d:d['preparation_contract'].__setitem__('parser_join_reserve_seconds',31.0))
  self.reject(lambda d:d['required_evidence'].remove('preparation_checkpoints'))

 def test_nonfinite_json_rejected(self):
  with tempfile.TemporaryDirectory() as root:
   p=Path(root)/'bad.json'
   for value in ['NaN','Infinity','-Infinity','1e999']:
    p.write_text('{"metric":'+value+'}')
    with self.subTest(value=value),self.assertRaises(ValueError):bench.read_json(p)

class Schema16GrammarFixture(unittest.TestCase):
 """Model the legacy pipeline source only within synthetic grammar tests.

 Current migration discovery and current18 evidence have separate contracts.
 Never patch the production validator or label these shapes as executed runs.
 """
 def setUp(self):
  super().setUp()
  self.schema_source=mock.patch.object(bench,'expected_schema_version',return_value=16)
  self.schema_source.start()
  self.addCleanup(self.schema_source.stop)


class BenchmarkEvidenceTests(Schema16GrammarFixture):
 def setUp(self):
  super().setUp()
  self.contract=bench.read_json(bench.DATASET)
  # Pure validator data, NOT simulated product HTTP/DB results or S evidence.
  self.result={'scale':'S','employees':100,'mailboxes':500,'messages':100000,'seed':3893945,'concurrency':20,'source_sha':'a'*40,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'index_ready_count':100000,'sql_tracer_calibration_count':20,'sql_tracer_calibration_expected':20,'pool_size':24,'hardware':{'os':'darwin','arch':'arm64','cpu':16},'schema_version':16,'dataset_fingerprint':'validator-only','safety_assertions':[w+':foreign_and_revoked_current_source_denied' for w in bench.WORKLOADS],'safety_assertions_passed':True,'task_complete':False,'product_green':False,'measurements':[{'workload':w,'cache_mode':c,'samples':200,'sql_count':201,'p50_ms':1,'p95_ms':2,'p99_ms':3,'alloc_bytes':4,'rss_bytes':5,'disk_bytes':6} for w in bench.WORKLOADS for c in self.contract['cache_modes']]}
  self.result.update(self.observations(100,100000,'S'))
  elapsed=self.result['preparation']['company_wall_seconds']+self.result['preparation']['pipeline_wall_seconds']+1
  self.result.update(latency_clock=bench.LATENCY_CLOCK,execution_wall={'started_unix_seconds':100.0,'completed_unix_seconds':100.0+elapsed,'elapsed_wall_seconds':elapsed,'budget_seconds':1800})
  for row in self.result['measurements']:row['max_wall_monotonic_gap_ms']=0.0
  # Every mutation starts from admitted grammar. Contract metadata failing
  # early must never make all downstream negative cases appear green.
  bench.validate_result(self.result,self.contract,'S','a'*40)
 @staticmethod
 def preparation(messages):
  # JSON grammar fixtures only. No claimed performance or product execution.
  def stats(phase,depth):
   terminal=phase=='terminal';joined=phase=='batch_joined';written=messages if terminal else depth+20;completed=messages if terminal else depth+20 if joined else depth;calls=messages//20+1 if terminal else completed//20
   store=written*.00005;claim=calls*.001;parse=completed*.00005
   return {'method':'bounded_store_claim_complete_window_v1','phase':phase,'joined_inflight':0,'parser_join_policy':bench.PARSER_JOIN_POLICY,'parser_join_reserve_seconds':31,'parser_joined_inflight':0,'parser_joined_calls':completed,'parser_canceled_joined_calls':0,'parser_joined_call_wall_seconds_sum':parse*2,'parser_quiescent':True,'parser_min_start_remaining_wall_seconds':299.0-completed/messages,'parser_original_deadline_unix_seconds':1900.0,'claim_query_sha256':'5'*64,'claim_parameters_sha256':hashlib.sha256(b'[20]').hexdigest(),'active_budget':20,'pending_upper_bound':20,'max_active':20,'max_window_messages':20,'joined_stored_messages':written,'joined_completed_jobs':completed,'claim_calls':calls,'max_claim_batch':20,'last_claim_batch':0 if terminal else 20,'joined_ready_before_last_claim':messages if terminal else depth if joined else depth-20,'last_claim_wall_seconds':.001,'producer_done':terminal,'final_empty_claim':terminal,'sql_ready_count':messages if terminal else 0,'company_wall_seconds':.1,'store_wall_seconds':store,'claim_wall_seconds':claim,'parse_complete_wall_seconds':parse,'pipeline_wall_seconds':store+claim+parse+.01,'max_wall_monotonic_gap_ms':0.0}
  checkpoints=[]
  for depth in [*bench.preparation_depths(messages),messages]:
   terminal=depth==messages;phase='terminal' if terminal else 'before_claim'
   item={'origin':'captured_preparation_claim_same_parameters_observed_heap','phase':phase,'source_sha':'a'*40,'schema_version':16,'maintenance':'no_explicit_analyze_or_planner_override','claim_requested_limit':20,'observed_jobs_total':messages if terminal else depth+20,'observed_pending':0 if terminal else 20,'observed_processing':0,'observed_ready_jobs':depth,'observed_durable_source_bound_ready':depth,'observed_failed':0,'observed_attempts_gt1':0,'query_sha256':'5'*64,'parameters_sha256':hashlib.sha256(b'[20]').hexdigest(),'explain_only_not_actual_claim_latency':True,'plan':[{'Plan':{'Node Type':'Limit','Plans':[{'Node Type':'Seq Scan','Relation Name':'mail_index_jobs'}]}}],'preparation':stats(phase,depth)}
   if not terminal:item['actual_claim_joined']=stats('batch_joined',depth)
   checkpoints.append(item)
  return {'preparation':stats('terminal',messages),'preparation_checkpoints':checkpoints}
 @staticmethod
 def observations(employees,messages,scale):
  return {**BenchmarkEvidenceTests.preparation(messages),'fingerprint_method':'six_actual_canonical_sql_streams_v1','fingerprint_streams':[{'stream_id':i,'row_count':n,'sha256':str(i+1)*64} for i,n in enumerate([2,messages,messages,employees*16,employees+2,employees*5])],'actual_distribution':{'employees':employees,'mailboxes':employees*5,'messages':messages,'index_ready_count':messages,'personal_mailboxes':employees,'shared_mailboxes':employees*4,'shared_grant_rows':employees*16,'min_distinct_shared_grantees':4,'max_distinct_shared_grantees':4,'lifecycle_counts':{'inbox':messages*80//100,'archived':messages*10//100,'trash_expired':messages*5//100,'hard_expired':messages*5//100},'mime_size_counts':{'4096':messages*800//1000,'32768':messages*180//1000,'262144':messages*19//1000,'2097152':messages//1000},'tenant_message_counts':{'bench-0.test':messages*80//100,'bench-1.test':messages*20//100}},'dataset_fingerprint':'1'*64,'parameter_fingerprint':'2'*64,'resource_scope':copy.deepcopy(bench.RESOURCE_SCOPE),'system_memory_observation':'not_measured','sql_plans':[{'workload':'inbox_list','origin':'captured_shipping_query_same_parameters','scale':scale,'source_sha':'a'*40,'schema_version':16,'index_ready_count':messages,'query_sha256':'3'*64,'parameters_sha256':'4'*64,'plan':[{'Plan':{'Node Type':'Seq Scan','Relation Name':'messages'}}]}]}
 def reject(self,modify):
  data=copy.deepcopy(self.result);modify(data)
  with self.assertRaises(ValueError):bench.validate_result(data,self.contract,'S','a'*40)
 def test_schema16_fixture_metadata_and_source_are_explicit(self):
  self.assertEqual(bench.PIPELINE_SCHEMA_VERSION,16)
  self.assertEqual(self.result['schema_version'],16)
  self.assertEqual({p['schema_version'] for p in self.result['sql_plans']}, {16})
  self.assertEqual({p['schema_version'] for p in self.result['preparation_checkpoints']}, {16})
  bench.validate_result(self.result,self.contract,'S','a'*40)

 def test_schema16_negative_reaches_checkpoint_validation(self):
  data=copy.deepcopy(self.result);data.pop('preparation_checkpoints')
  with self.assertRaisesRegex(ValueError,'^all actual deep-ready and terminal preparation checkpoints required$'):
   bench.validate_result(data,self.contract,'S','a'*40)

 def test_current_schema_cannot_relabel_schema16_preparation(self):
  for schema in [18,19]:
   data=copy.deepcopy(self.result);data['schema_version']=schema
   for point in data['preparation_checkpoints']+data['sql_plans']:point['schema_version']=schema
   with self.subTest(schema=schema),mock.patch.object(bench,'expected_schema_version',return_value=schema):
    with self.assertRaisesRegex(ValueError,'^new preparation evidence requires exact schema16 pipeline contract$'):
     bench.validate_result(data,self.contract,'S','a'*40)

 def test_schema16_fixture_cannot_bypass_current_migration_source(self):
  for schema in [18,19]:
   with self.subTest(schema=schema),mock.patch.object(bench,'expected_schema_version',return_value=schema):
    with self.assertRaisesRegex(ValueError,'^observed schema differs from actual migration source$'):
     bench.validate_result(self.result,self.contract,'S','a'*40)

 def test_partial_or_fake_shape_does_not_certify_scale(self):
  self.reject(lambda d:d.__setitem__('messages',10));self.reject(lambda d:d.__setitem__('source_sha','b'*40));self.reject(lambda d:d.__setitem__('dataset_sha256','0'*64))
  self.reject(lambda d:d.__setitem__('index_ready_count',1));self.reject(lambda d:d.__setitem__('sql_tracer_calibration_count',0))
  self.reject(lambda d:d.__setitem__('safety_assertions_passed',False));self.reject(lambda d:d.__setitem__('hardware',{}))
 def test_missing_zero_duplicate_nonfinite_measurements_rejected(self):
  self.reject(lambda d:d['measurements'][0].__setitem__('p50_ms',99))
  self.reject(lambda d:d['measurements'].pop());self.reject(lambda d:d['measurements'].append(d['measurements'][0]))
  for field,value in [('sql_count',0),('samples',0),('samples',199),('samples',201),('rss_bytes',0),('disk_bytes',0),('p95_ms',float('nan')),('rss_bytes',float('inf')),('alloc_bytes',-1),('disk_bytes',None)]:
   with self.subTest(field=field):self.reject(lambda d:d['measurements'][0].__setitem__(field,value))
 def test_observation_and_safety_scope_rejected(self):
  self.reject(lambda d:d['hardware'].__setitem__('cpu',0))
  self.reject(lambda d:d.__setitem__('pool_size',1))
  self.reject(lambda d:d['safety_assertions'].pop())
  self.reject(lambda d:d['safety_assertions'].__setitem__(0,'validator-only'))
 def test_actual_distribution_and_fingerprints_rejected(self):
  for field in ['employees','mailboxes','messages','index_ready_count','personal_mailboxes','shared_mailboxes','shared_grant_rows','min_distinct_shared_grantees','max_distinct_shared_grantees']:
   with self.subTest(field=field):self.reject(lambda d:d['actual_distribution'].__setitem__(field,0))
  for group in ['lifecycle_counts','mime_size_counts','tenant_message_counts']:
   for field in self.result['actual_distribution'][group]:
    with self.subTest(group=group,field=field):self.reject(lambda d:d['actual_distribution'][group].__setitem__(field,0))
  self.reject(lambda d:d.__setitem__('dataset_fingerprint','parameter-only'))
  self.reject(lambda d:d.__setitem__('dataset_fingerprint',d['parameter_fingerprint']))
  self.reject(lambda d:d['actual_distribution'].__setitem__('min_distinct_shared_grantees',4.0))
 def test_actual_plan_relation_not_quota_or_parameter_substring(self):
  self.reject(lambda d:d['sql_plans'][0].__setitem__('plan',[{'Plan':{'Node Type':'Seq Scan','Relation Name':'plans'}}]))
  self.reject(lambda d:d['sql_plans'][0].__setitem__('plan',[{'Plan':{'Node Type':'Result','max_messages':'messages'}}]))
  data=copy.deepcopy(self.result);data['sql_plans'][0]['plan']=[{'Plan':{'Node Type':'Limit','Plans':[{'Node Type':'Index Scan','Relation Name':'messages'}]}}]
  bench.validate_result(data,self.contract,'S','a'*40)
 def test_actual_plan_metadata_and_resource_scope_rejected(self):
  for field,value in [('origin','handwritten_SQL'),('scale','M'),('source_sha','b'*40),('schema_version',14),('index_ready_count',0),('query_sha256',''),('parameters_sha256',''),('plan',[])]:
   with self.subTest(field=field):self.reject(lambda d:d['sql_plans'][0].__setitem__(field,value))
  self.reject(lambda d:d.__setitem__('sql_plans',[]))
  self.reject(lambda d:d['resource_scope'].__setitem__('sql','all_server_SQL'))
  self.reject(lambda d:d.__setitem__('system_memory_observation','all_system_measured'))
 def test_tool_only_shape_never_certifies_original_scale(self):
  result={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'source_sha':'a'*40,'schema_version':16,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'task_complete':False,'product_green':False,'S_M_L_executed':False,'seed':3893945,'rss_bytes':1,'disk_bytes':1,**self.observations(20,1000,'tool_only')}
  observed=bench.validate_tool_result(result,self.contract,'a'*40)
  self.assertFalse(observed['S_M_L_executed'])
  with self.assertRaises(ValueError):bench.validate_result(result,self.contract,'S','a'*40)
  for field,value in [('S_M_L_executed',True),('messages',100),('source_sha','b'*40),('task_complete',True)]:
   altered=copy.deepcopy(result);altered[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_tool_result(altered,self.contract,'a'*40)
 def test_validated_shape_still_not_task_or_product_approval(self):
  result=bench.validate_result(self.result,self.contract,'S','a'*40)
  self.assertFalse(result['task_complete']);self.assertFalse(result['product_green'])

 def test_current_pipeline_observation_absence_is_rejected(self):
  data=copy.deepcopy(self.result)
  data.pop('preparation',None);data.pop('preparation_checkpoints',None)
  with self.assertRaises(ValueError):bench.validate_result(data,self.contract,'S','a'*40)

 def test_pipeline_checkpoint_missing_duplicate_unknown_or_wrong_phase_rejected(self):
  for key in ['preparation','preparation_checkpoints']:
   self.reject(lambda d:d.pop(key))
  self.reject(lambda d:d['preparation'].__setitem__('unknown_stage',0))
  self.reject(lambda d:d['preparation_checkpoints'].pop(0))
  self.reject(lambda d:d['preparation_checkpoints'].append(copy.deepcopy(d['preparation_checkpoints'][0])))
  self.reject(lambda d:d['preparation_checkpoints'].__setitem__(1,copy.deepcopy(d['preparation_checkpoints'][0])))
  self.reject(lambda d:d['preparation_checkpoints'][0].__setitem__('phase','batch_joined'))
  self.reject(lambda d:d['preparation_checkpoints'][0].pop('actual_claim_joined'))
  self.reject(lambda d:d['preparation_checkpoints'][0].__setitem__('unknown_protocol',True))
 def test_pipeline_window_terminal_and_illegal_number_rejected(self):
  for field,value in [('active_budget',21),('pending_upper_bound',21),('max_active',21),('max_window_messages',21),('max_claim_batch',100),('joined_stored_messages',99999),('joined_completed_jobs',99999),('claim_calls',5000),('last_claim_batch',1),('joined_ready_before_last_claim',99980),('sql_ready_count',99999),('producer_done',False),('final_empty_claim',False)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation'].__setitem__(field,value))
  for field in bench.PREPARATION_COUNTS:
   for value in [True,1.0,-1,None]:
    with self.subTest(field=field,value=value):self.reject(lambda d:d['preparation'].__setitem__(field,value))
  for field in bench.PREPARATION_WALL:
   for value in [True,-1,None,float('nan'),float('inf')]:
    with self.subTest(field=field,value=value):self.reject(lambda d:d['preparation'].__setitem__(field,value))
  self.reject(lambda d:d['preparation'].__setitem__('max_wall_monotonic_gap_ms',101))
  self.reject(lambda d:d['preparation'].__setitem__('pipeline_wall_seconds',1801))
 def test_pipeline_actual_claim_counts_pairing_and_heap_rejected(self):
  for field,value in [('observed_jobs_total',999),('observed_pending',21),('observed_processing',1),('observed_ready_jobs',21),('observed_durable_source_bound_ready',19),('observed_failed',1),('observed_attempts_gt1',1),('claim_requested_limit',100),('source_sha','b'*40),('schema_version',15),('maintenance','analyze'),('explain_only_not_actual_claim_latency',False),('parameters_sha256','1'*64)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_checkpoints'][0].__setitem__(field,value))
  self.reject(lambda d:d['preparation_checkpoints'][1].__setitem__('query_sha256','6'*64))
  for field,value in [('joined_ready_before_last_claim',21),('last_claim_batch',0),('joined_completed_jobs',20),('claim_calls',1),('phase','before_claim'),('claim_wall_seconds',.001),('producer_done',True)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__(field,value))
 def test_preparation_explain_must_be_actual_mail_index_jobs_plan_tree(self):
  plans=[[],[{'Plan':{}}],[{'Plan':{'Node Type':'Result'},'payload':{'Relation Name':'mail_index_jobs'}}],[{'Plan':{'Node Type':'Result','payload':{'Relation Name':'mail_index_jobs'}}}],[{'Plan':{'Node Type':'Seq Scan','Relation Name':'messages'}}],[{'Plan':{'Node Type':'Seq Scan','Relation Name':'mail_index_jobs','Actual Total Time':1}}],[{'Plan':{'Relation Name':'mail_index_jobs'}}]]
  for plan in plans:
   with self.subTest(plan=plan):self.reject(lambda d:d['preparation_checkpoints'][0].__setitem__('plan',plan))
 def test_current_schema_cannot_select_frozen_legacy_exemption(self):
  for schema in [15,16,999,True,16.0]:
   data=copy.deepcopy(self.result);data.pop('preparation');data.pop('preparation_checkpoints');data['schema_version']=schema
   with self.subTest(schema=schema),self.assertRaises(ValueError):bench.validate_result(data,self.contract,'S','a'*40,source_schema_version=schema)
  with self.assertRaises(ValueError):bench.validate_result(self.result,self.contract,'S','a'*40,frozen_legacy=True)
 def test_actual_claim_digest_inflight_and_sixstream_evidence_required(self):
  self.reject(lambda d:d['preparation'].__setitem__('joined_inflight',1))
  self.reject(lambda d:d['preparation'].__setitem__('claim_query_sha256','parameter-echo'))
  self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__('claim_query_sha256','6'*64))
  self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__('claim_parameters_sha256','6'*64))
  for field in ['fingerprint_method','fingerprint_streams']:
   self.reject(lambda d:d.pop(field))
  self.reject(lambda d:d.__setitem__('fingerprint_method','parameter_only'))
  self.reject(lambda d:d['fingerprint_streams'].pop())
  self.reject(lambda d:d['fingerprint_streams'].__setitem__(1,copy.deepcopy(d['fingerprint_streams'][0])))
  for field,value in [('stream_id',True),('stream_id',1.0),('row_count',True),('row_count',100000.0),('row_count',99999),('sha256','parameter-only'),('unknown_sampling',True)]:
   with self.subTest(field=field):self.reject(lambda d:d['fingerprint_streams'][1].__setitem__(field,value))
 def test_pipeline_stage_wall_cannot_escape_actual_execution_wall(self):
  self.reject(lambda d:d.__setitem__('execution_wall',{'started_unix_seconds':100.0,'completed_unix_seconds':100.001,'elapsed_wall_seconds':.001,'budget_seconds':1800}))
 def test_pipeline_duplicate_json_and_cli_absence_fail_closed(self):
  payload=json.dumps(self.result).replace('"joined_inflight": 0','"joined_inflight": 0, "joined_inflight": 0',1)
  with self.assertRaises(ValueError):bench.parse_json(payload)
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory);path=root/'result.json';data={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'seed':3893945,'rss_bytes':1,'disk_bytes':1,'source_sha':'a'*40,'schema_version':16,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'task_complete':False,'product_green':False,'S_M_L_executed':False,**self.observations(20,1000,'tool_only')};data.pop('preparation_checkpoints');path.write_text(json.dumps(data))
   command=[sys.executable,'-B',str(bench.ROOT/'scripts/run_r5_benchmark.py'),'--source-sha','a'*40,'--validate-tool-result',str(path)]
   completed=subprocess.run(command,capture_output=True,text=True)
   self.assertEqual(completed.returncode,1)
   rejection=bench.parse_json(completed.stdout);self.assertEqual(rejection['status'],'rejected');self.assertFalse(rejection['task_complete']);self.assertFalse(rejection['product_green'])
 def test_direct_pipeline_shapes_tool_S_M_preserve_population_boundaries(self):
  for scale,size in [('tool_only',(20,100,1000)),('S',bench.SCALES['S']),('M',bench.SCALES['M'])]:
   with self.subTest(scale=scale):
    observations=self.observations(size[0],size[2],scale);observations['schema_version']=16
    bench.validate_observations(observations,size,scale,'a'*40)
  self.assertEqual(bench.preparation_depths(1000),[20,980])
  self.assertEqual(bench.preparation_depths(100000),[20,1000,10000,99980])
  self.assertEqual(bench.preparation_depths(1000000),[20,1000,10000,100000,250000,500000,750000,999980])
 def test_complete_tool_pipeline_and_workloads_still_not_S_M(self):
  data=copy.deepcopy(self.result);data.update(self.observations(20,1000,'tool_only'));data.update(mode='TOOL_ONLY_DATASET_CALIBRATION',scale='tool_only',employees=20,mailboxes=100,messages=1000,index_ready_count=1000,S_M_L_executed=False,rss_bytes=1,disk_bytes=1);data['execution_wall']['budget_seconds']=300
  for stats in [data['preparation'],*[c['preparation'] for c in data['preparation_checkpoints']],*[c['actual_claim_joined'] for c in data['preparation_checkpoints'][:-1]]]:stats['parser_original_deadline_unix_seconds']=400.0
  verdict=bench.validate_tool_result(data,self.contract,'a'*40)
  self.assertEqual(verdict['status'],'tool_only_observations_validated');self.assertFalse(verdict['S_M_L_executed'])
  with self.assertRaises(ValueError):bench.validate_result(data,self.contract,'M','a'*40)
 def test_formal_parser_drain_observations_cannot_be_echo_or_unjoined(self):
  for field,value in [('parser_join_policy','canceled_waiter_no_join'),('parser_join_reserve_seconds',30),('parser_joined_inflight',1),('parser_joined_calls',99999),('parser_canceled_joined_calls',1),('parser_joined_call_wall_seconds_sum',0),('parser_quiescent',False),('parser_quiescent',1)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation'].__setitem__(field,value))
  self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__('parser_joined_call_wall_seconds_sum',d['preparation_checkpoints'][0]['preparation']['parser_joined_call_wall_seconds_sum']))
 def test_parser_guard_requires_actual_minimum_and_original_deadline(self):
  for field,value in [('parser_min_start_remaining_wall_seconds',31),('parser_min_start_remaining_wall_seconds',31.0),('parser_min_start_remaining_wall_seconds',1801),('parser_min_start_remaining_wall_seconds',True),('parser_original_deadline_unix_seconds',0),('parser_original_deadline_unix_seconds',float('nan')),('parser_original_deadline_unix_seconds',True),('parser_original_deadline_unix_seconds',1901)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation'].__setitem__(field,value))
  self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__('parser_min_start_remaining_wall_seconds',300))
  for field in bench.PREPARATION_GUARD:self.reject(lambda d:d['preparation'].pop(field))
 def test_validator_required_fields_match_actual_Go_preparation_json(self):
  source=(bench.ROOT/'internal/store/postgres/r5_benchmark_test.go').read_text()
  body=re.search(r'type r5BenchPreparation struct \{(.*?)\n\}',source,re.S).group(1)
  fields=re.findall(r'`json:"([^"]+)"`',body)
  self.assertEqual(set(fields),bench.PREPARATION_FIELDS);self.assertEqual(len(fields),len(bench.PREPARATION_FIELDS))

 def test_strict_types_unknown_sampling_schema_and_tool_mode(self):
  for field in ['employees','mailboxes','messages','seed','concurrency','pool_size','index_ready_count','sql_tracer_calibration_count','sql_tracer_calibration_expected']:
   self.reject(lambda d:d.__setitem__(field,float(d[field])))
   self.reject(lambda d:d.__setitem__(field,True))
  for value in [1,'true',[],{}]:self.reject(lambda d:d.__setitem__('safety_assertions_passed',value))
  self.reject(lambda d:d.__setitem__('schema_version',bench.expected_schema_version()+1))
  self.reject(lambda d:d.__setitem__('sampling_mode','unknown'))
  self.reject(lambda d:d.__setitem__('mode','TOOL_ONLY_DATASET_CALIBRATION'))
  for field in ['alloc_bytes','rss_bytes','disk_bytes']:
   self.reject(lambda d:d['measurements'][0].__setitem__(field,1.0))
   self.reject(lambda d:d['measurements'][0].__setitem__(field,True))
 def test_tool_resources_seed_and_sampling_unknown_rejected(self):
  tool={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'seed':3893945,'rss_bytes':1,'disk_bytes':1,'source_sha':'a'*40,'schema_version':16,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'task_complete':False,'product_green':False,'S_M_L_executed':False,**self.observations(20,1000,'tool_only')}
  for field,value in [('rss_bytes',0),('disk_bytes',None),('rss_bytes',True),('seed',1),('messages',1000.0),('sampling_method','unknown')]:
   data=copy.deepcopy(tool);data[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_tool_result(data,self.contract,'a'*40)
 def test_new_execution_requires_actual_wall_and_sample_clock(self):
  data=copy.deepcopy(self.result);data['latency_clock']=bench.LATENCY_CLOCK;data['execution_wall']=copy.deepcopy(self.result['execution_wall'])
  for row in data['measurements']:row['max_wall_monotonic_gap_ms']=0.0
  bench.validate_result(data,self.contract,'S','a'*40,require_wall_clock=True)
  unknown=copy.deepcopy(self.result);unknown.pop('latency_clock');unknown.pop('execution_wall')
  with self.assertRaises(ValueError):bench.validate_result(unknown,self.contract,'S','a'*40)
  for field,value in [('max_wall_monotonic_gap_ms',101),('max_wall_monotonic_gap_ms',-1),('max_wall_monotonic_gap_ms',None),('max_wall_monotonic_gap_ms',True),('max_wall_monotonic_gap_ms',float('nan'))]:
   altered=copy.deepcopy(data);altered['measurements'][0][field]=value
   with self.subTest(value=value),self.assertRaises(ValueError):bench.validate_result(altered,self.contract,'S','a'*40)
  for field,value in [('latency_clock','unknown'),('execution_wall',{}),('execution_wall',{'started_unix_seconds':100.0,'completed_unix_seconds':2001.0,'elapsed_wall_seconds':1901.0,'budget_seconds':1800})]:
   altered=copy.deepcopy(data);altered[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_result(altered,self.contract,'S','a'*40)
 def test_M_search_threshold_preserved(self):
  data=copy.deepcopy(self.result);data.update(scale='M',employees=1000,mailboxes=5000,messages=1000000,index_ready_count=1000000)
  data.update(self.observations(1000,1000000,'M'));elapsed=data['preparation']['company_wall_seconds']+data['preparation']['pipeline_wall_seconds']+1;data['execution_wall'].update(budget_seconds=10800,completed_unix_seconds=100+elapsed,elapsed_wall_seconds=elapsed)
  for stats in [data['preparation'],*[c['preparation'] for c in data['preparation_checkpoints']],*[c['actual_claim_joined'] for c in data['preparation_checkpoints'][:-1]]]:stats['parser_original_deadline_unix_seconds']=10900.0
  for row in data['measurements']:
   if row['workload']=='indexed_search':row.update(p95_ms=1001,p99_ms=1002)
  result=bench.validate_result(data,self.contract,'M','a'*40)
  self.assertFalse(result['candidate_threshold_passed'])
  self.assertEqual(len([x for x in result['candidate_thresholds'] if x['workload']=='indexed_search']),2)

class BenchmarkProcessTests(unittest.TestCase):
 def record_owned_process_evidence(self,name,receipt,child,status,output=''):
  directory=os.getenv('TABMAIL_R5_RUNNER_TEST_EVIDENCE_DIR')
  if directory:
   target=Path(directory)/name;target.mkdir(parents=True,exist_ok=False)
   bench.write_execution_receipt(target,receipt)
   (target/'child-observation.json').write_text(json.dumps({'child_pid':child,'ps_state':status,'non_zombie_live':bool(status and not status.startswith('Z'))},sort_keys=True)+'\n')
   (target/'original-output.txt').write_text(output)

 def test_go_protocol_rejects_skips_missing_duplicate_and_wrong_package(self):
  events=[{'Action':'run','Package':'tabmail/internal/store/postgres','Test':'TestR5BenchmarkOriginalScale'},{'Action':'pass','Package':'tabmail/internal/store/postgres','Test':'TestR5BenchmarkOriginalScale'},{'Action':'pass','Package':'tabmail/internal/store/postgres'}]
  with tempfile.TemporaryDirectory() as root:
   path=Path(root)/'events.jsonl'
   def check(data):path.write_text(''.join(json.dumps(e)+'\n' for e in data));bench.validate_go_events(path)
   check(events)
   for data in [events[:-1],events+[events[1]],events+[{'Action':'skip'}],[dict(e,Package='wrong') for e in events]]:
    with self.assertRaises(ValueError):check(data)
 def test_no_git_metadata_is_not_a_fabricated_head(self):
  with tempfile.TemporaryDirectory() as root,mock.patch.object(bench,'ROOT',Path(root)):
   self.assertIsNone(bench.observed_git_head())
  with mock.patch.object(bench.subprocess,'run',return_value=subprocess.CompletedProcess([],0,'a'*40+'\n','')):
   self.assertEqual(bench.observed_git_head(),'a'*40)
 def test_source_closure_tracks_dependencies_and_content_drift(self):
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory).resolve();context=current_inventory_fixture(root)
   with mock.patch.object(bench,'ROOT',root):
    before=bench.source_closure(policy=bench.source_inventory.POLICY,build_context=context)
    self.assertIn('go.mod',before);self.assertIn('third_party/enmime-v2.3.0/budget.go',before)
    (root/'go.sum').write_text('after')
    self.assertNotEqual(before,bench.source_closure(policy=bench.source_inventory.POLICY,build_context=context))
    with self.assertRaises(ValueError):bench.source_closure()

 def test_timeout_kills_owned_descendant_and_preserves_output(self):
  with tempfile.TemporaryDirectory() as root:
   pidfile=Path(root)/'child.pid';output=Path(root)/'output'
   code="import subprocess,sys,time; p=subprocess.Popen([sys.executable,'-c','import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(60)']); open(sys.argv[1],'w').write(str(p.pid)); print('original output',flush=True); time.sleep(60)"
   with output.open('w') as stdout,open(os.devnull,'w') as stderr:
    with self.assertRaises(bench.OwnedProcessTimeout) as raised:bench.run_owned_process([sys.executable,'-c',code,str(pidfile)],env=dict(os.environ),stdout=stdout,stderr=stderr,timeout=0.5)
   self.assertLess(raised.exception.returncode,0);self.assertIn('original output',output.read_text())
   child=int(pidfile.read_text())
   # A killed orphan may briefly be a zombie; neither state is a live worker.
   status=subprocess.run(['ps','-o','stat=','-p',str(child)],capture_output=True,text=True).stdout.strip()
   self.record_owned_process_evidence('owned-descendant-timeout',raised.exception.receipt,child,status,output.read_text())
   self.assertTrue(not status or status.startswith('Z'),status)

 def test_resume_wall_jump_rejects_even_if_process_already_finished(self):
  # Deterministic OS-boundary simulation of the observed Darwin sleep/darkwake:
  # wall advances beyond budget, while the monotonic wait can report success.
  process=mock.Mock(pid=12345678,returncode=None)
  process.poll.side_effect=[None,0]
  process.wait.return_value=0
  with mock.patch.object(bench.subprocess,'Popen',return_value=process),mock.patch.object(bench.time,'time',side_effect=[100.0,100.0,11700.0,11700.0]),mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'observed','members':[],'non_zombie_live_pids':[]}):
   with self.assertRaises(bench.OwnedProcessTimeout) as raised:bench.run_owned_process(['fixture'],env={},stdout=None,stderr=None,timeout=10800)
  self.assertEqual(raised.exception.receipt['elapsed_wall_seconds'],11600)
  self.assertTrue(raised.exception.receipt['timed_out']);self.assertEqual(kill.call_count,2)
  self.assertEqual(raised.exception.receipt['absolute_deadline_wall_time'],10900)
 def test_backwards_wall_clock_rejects_instead_of_extending_budget(self):
  process=mock.Mock(pid=12345678,returncode=None);process.poll.return_value=None;process.wait.return_value=-15
  with mock.patch.object(bench.subprocess,'Popen',return_value=process),mock.patch.object(bench.time,'time',side_effect=[100.0,99.0,99.0]),mock.patch.object(bench.os,'killpg'),mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'observed','members':[],'non_zombie_live_pids':[]}):
   with self.assertRaises(bench.OwnedProcessTimeout) as raised:bench.run_owned_process(['fixture'],env={},stdout=None,stderr=None,timeout=1800)
  self.assertTrue(raised.exception.receipt['wall_clock_reversed'])
 def test_real_finished_process_success_and_receipt(self):
  receipt={}
  with tempfile.TemporaryDirectory() as root,open(os.devnull,'w') as stream:
   rc=bench.run_owned_process([sys.executable,'-c','print("done")'],env=dict(os.environ),stdout=stream,stderr=stream,timeout=10,receipt=receipt)
   self.assertEqual(rc,0);self.assertFalse(receipt['timed_out'])
   bench.write_execution_receipt(Path(root),receipt)
   self.assertEqual((Path(root)/'go.exit').read_text(),'0\n')
   self.assertLess(receipt['observed_wall_time'],receipt['absolute_deadline_wall_time'])
 def test_cleanup_eperm_keeps_actual_exit_and_error_receipt(self):
  process=mock.Mock(pid=12345678,returncode=None);process.poll.return_value=None;process.wait.return_value=-15
  receipt={}
  with mock.patch.object(bench.subprocess,'Popen',return_value=process),mock.patch.object(bench.time,'time',side_effect=[100,101,101]),mock.patch.object(bench.time,'sleep'),mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.os,'killpg',side_effect=[None,PermissionError(1,'Operation not permitted')]),mock.patch.object(bench,'owned_group_observation',return_value={'status':'observed','members':[],'non_zombie_live_pids':[]}):
   with self.assertRaises(bench.OwnedProcessTimeout):bench.run_owned_process(['fixture'],env={},stdout=None,stderr=None,timeout=1,receipt=receipt)
  self.assertEqual(receipt['process_returncode'],-15);self.assertTrue(receipt['cleanup_live_processes_absent']);self.assertEqual(receipt['cleanup_errors'][0]['errno'],1)
  with tempfile.TemporaryDirectory() as root:
   bench.write_execution_receipt(Path(root),receipt)
   self.assertEqual((Path(root)/'go.exit').read_text(),'-15\n');self.assertTrue((Path(root)/'execution-error.json').exists());self.assertTrue((Path(root)/'timeout.json').exists())
 def test_cleanup_live_eperm_or_join_unknown_never_fabricates_exit(self):
  process=mock.Mock(pid=12345678,returncode=None);process.poll.return_value=None;process.wait.side_effect=subprocess.TimeoutExpired('fixture',5)
  receipt={}
  with mock.patch.object(bench.subprocess,'Popen',return_value=process),mock.patch.object(bench.time,'time',side_effect=[100,101,101]),mock.patch.object(bench.time,'sleep'),mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.os,'killpg',side_effect=PermissionError(1,'denied')),mock.patch.object(bench,'owned_group_observation',return_value={'status':'observed','members':[{'pid':process.pid,'state':'S'}],'non_zombie_live_pids':[process.pid]}):
   with self.assertRaises(bench.OwnedProcessTimeout):bench.run_owned_process(['fixture'],env={},stdout=None,stderr=None,timeout=1,receipt=receipt)
  self.assertIsNone(receipt['process_returncode']);self.assertFalse(receipt['cleanup_live_processes_absent'])
  with tempfile.TemporaryDirectory() as root:
   bench.write_execution_receipt(Path(root),receipt)
   self.assertFalse((Path(root)/'go.exit').exists());self.assertEqual(json.loads((Path(root)/'go-exit-status.json').read_text())['status'],'unknown')
 def test_cleanup_missing_or_reused_identity_never_signals_foreign_group(self):
  for group in [98765432,ProcessLookupError()]:
   process=mock.Mock(pid=12345678,returncode=None);process.wait.return_value=-15;receipt={}
   context=mock.patch.object(bench.os,'getpgid',side_effect=group) if isinstance(group,Exception) else mock.patch.object(bench.os,'getpgid',return_value=group)
   with context,mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'unknown'}):bench.cleanup_owned_process(process,receipt,leader_unreaped=True)
   kill.assert_not_called();self.assertFalse(receipt['cleanup_live_processes_absent'])
  with mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench,'owned_group_observation',return_value={'status':'unknown'}):bench.cleanup_owned_process(process,{},already_reaped=True)
  kill.assert_not_called()
 def test_spawn_failure_preserves_unknown_receipt(self):
  receipt={}
  with mock.patch.object(bench.subprocess,'Popen',side_effect=FileNotFoundError('not found')):
   with self.assertRaises(bench.OwnedProcessExecutionError):bench.run_owned_process(['missing'],env={},stdout=None,stderr=None,timeout=10,receipt=receipt)
  self.assertIsNone(receipt['process_returncode']);self.assertEqual(receipt['owned_process_group_cleanup'],'not_started_no_signal')
 def test_real_timeout_terminal_leader_keeps_group_identity_until_kill(self):
  # Owned parent exits on TERM; cleanup must still kill its TERM-ignoring child.
  with tempfile.TemporaryDirectory() as root,open(os.devnull,'w') as stream:
   pidfile=Path(root)/'child.pid';receipt={}
   code="import subprocess,sys,time; p=subprocess.Popen([sys.executable,'-c','import signal,time; signal.signal(signal.SIGTERM,signal.SIG_IGN); time.sleep(60)']); open(sys.argv[1],'w').write(str(p.pid)); time.sleep(60)"
   with self.assertRaises(bench.OwnedProcessTimeout):bench.run_owned_process([sys.executable,'-c',code,str(pidfile)],env=dict(os.environ),stdout=stream,stderr=stream,timeout=.5,receipt=receipt)
   self.assertIsInstance(receipt['process_returncode'],int);self.assertTrue(receipt['timed_out']);self.assertTrue(receipt['cleanup_live_processes_absent'])
   self.assertEqual([a['signal'] for a in receipt['cleanup_actions']],['SIGTERM','SIGKILL'])
   self.assertEqual([a['observed_group'] for a in receipt['cleanup_actions']],[receipt['owned_process_group']]*2)
   bench.write_execution_receipt(Path(root),receipt)
   self.assertEqual(int((Path(root)/'go.exit').read_text()),receipt['process_returncode'])
   child=int(pidfile.read_text());status=subprocess.run(['ps','-o','stat=','-p',str(child)],capture_output=True,text=True).stdout.strip()
   self.record_owned_process_evidence('darwin-terminal-leader-timeout',receipt,child,status)
   self.assertTrue(not status or status.startswith('Z'),status)
 def test_nondefault_sigchld_and_untrusted_anchor_fail_closed(self):
  for handler in [signal.SIG_IGN,lambda *_:None]:
   receipt={}
   with mock.patch.object(bench.signal,'getsignal',return_value=handler),mock.patch.object(bench.subprocess,'Popen') as spawn:
    with self.assertRaises(bench.OwnedProcessExecutionError):bench.run_owned_process(['fixture'],env={},stdout=None,stderr=None,timeout=1,receipt=receipt)
   spawn.assert_not_called();self.assertIsNone(receipt['process_returncode'])
  process=mock.Mock(pid=12345678,returncode=None);process.wait.return_value=-15
  with mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'unknown'}):bench.cleanup_owned_process(process,{})
  kill.assert_not_called()
 def test_observed_exit_and_nondefault_sigchld_never_signal_reused_group(self):
  for returncode,handler in [(0,signal.SIG_DFL),(None,signal.SIG_IGN),(None,lambda *_:None)]:
   process=mock.Mock(pid=12345678,returncode=returncode);process.wait.return_value=returncode
   with mock.patch.object(bench.signal,'getsignal',return_value=handler),mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'unknown'}):bench.cleanup_owned_process(process,{},leader_unreaped=True)
   kill.assert_not_called()
 def test_changed_sigchld_cannot_turn_echild_into_fabricated_zero(self):
  process=mock.Mock(pid=12345678,returncode=None);process.wait.return_value=0
  receipt={}
  with mock.patch.object(bench.signal,'getsignal',return_value=signal.SIG_IGN),mock.patch.object(bench.os,'getpgid',return_value=process.pid),mock.patch.object(bench.os,'killpg') as kill,mock.patch.object(bench.time,'sleep'),mock.patch.object(bench,'owned_group_observation',return_value={'status':'unknown'}):bench.cleanup_owned_process(process,receipt,leader_unreaped=True)
  kill.assert_not_called();process.wait.assert_not_called();self.assertIsNone(receipt['process_returncode']);self.assertEqual(receipt['process_returncode_status'],'unknown_join_failed')
 def test_raw_rfc3339_wall_budget_includes_suspend(self):
  base=[{'Time':'2026-10-01T00:00:00.12345+00:00','Action':'run','Package':'tabmail/internal/store/postgres','Test':'TestR5BenchmarkOriginalScale'},{'Time':'2026-10-01T00:30:00.123456789+00:00','Action':'pass','Package':'tabmail/internal/store/postgres'}]
  self.assertGreater(bench.validate_run_wall(base,1801),1799)
  for value in ['2026-10-01T00:31:00+00:00','2026-09-30T23:59:00+00:00','2026-10-01T00:10:00']:
   data=copy.deepcopy(base);data[1]['Time']=value
   with self.subTest(value=value),self.assertRaises(ValueError):bench.validate_run_wall(data,1800)

class BenchmarkSourceIdentityTests(unittest.TestCase):
 def setUp(self):
  # Identity grammar fixtures only; never an executed scale/performance result.
  dataset='docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'
  files={k:'1'*64 for k in [dataset,'go.mod','go.sum','scripts/run_r5_benchmark.py','scripts/tests/test_r5_benchmark.py','internal/store/postgres/r5_benchmark_test.go']}
  files.update({f'internal/store/postgres/migrations/{i:05d}_fixture.sql':'2'*64 for i in range(1,17)})
  canonical=json.dumps(files,sort_keys=True,separators=(',',':')).encode();source=hashlib.sha1(canonical).hexdigest();digest=hashlib.sha256(canonical).hexdigest()
  self.row={'source_tree':source,'source_identity_kind':'canonical_source_closure_sha1','actual_source_closure_sha256':digest}
  self.review={**self.row,'source_sha_semantics':'canonical_source_closure_sha1_not_git_tree_or_commit','source_identity_proof_member':'canonical-source-identity-proof.json'}
  self.closure={'declared_source_sha':source,'actual_source_closure_sha256':digest,'files':files}
  receipt=json.dumps({'source_sha':source,'actual_source_closure_sha256':digest,'files':files}).encode()
  proof={'source_sha':source,'source_identity_kind':'canonical_source_closure_sha1','source_sha_semantics':self.review['source_sha_semantics'],'actual_source_closure_sha256':digest,'canonicalization':{'canonical_bytes_length':len(canonical),'canonical_bytes_member':'canonical-source-closure.json','closure_algorithm':'SHA256(canonical_bytes)','encoding':'utf-8','implementation':"json.dumps(files,sort_keys=True,separators=(',',':')).encode()",'input':'original source-receipt.json files object of relative path -> individual file SHA256','label_algorithm':'SHA1(canonical_bytes)','python_ensure_ascii':True,'trailing_newline':False},'original_source_receipt':{'member':'source-receipt.json','sha256':hashlib.sha256(receipt).hexdigest()}}
  self.members={'source-receipt.json':receipt,'canonical-source-closure.json':canonical,'canonical-source-identity-proof.json':json.dumps(proof).encode()}
  self.validate() # Negative cases must reach identity validation, not an earlier missing fixture.
 def validate(self):return bench.validate_source_identity(self.row,self.review,self.closure,self.members)
 def reject(self,modify):
  row,review,closure,members=copy.deepcopy((self.row,self.review,self.closure,self.members));modify(row,review,closure,members)
  with self.assertRaises(ValueError):bench.validate_source_identity(row,review,closure,members)
 @staticmethod
 def mutate_proof(members,modify):
  proof=bench.parse_json(members['canonical-source-identity-proof.json'].decode());modify(proof);members['canonical-source-identity-proof.json']=json.dumps(proof).encode()
 def test_canonical_identity_derived_from_actual_frozen_mapping(self):
  files,digest,schema=self.validate();self.assertEqual(schema,16);self.assertEqual(files,self.closure['files']);self.assertEqual(digest,self.row['actual_source_closure_sha256'])
 def test_unknown_kind_arbitrary_label_pseudo_Git_and_closure_hash_rejected(self):
  for obj in ['row','review','closure']:
   for field,value in [('source_identity_kind','git_write_tree'),('source_identity_kind','current_HEAD'),('actual_source_closure_sha256','0'*64),('actual_source_closure_sha256',None)]:
    with self.subTest(obj=obj,field=field):self.reject(lambda r,v,c,m:dict(row=r,review=v,closure=c)[obj].__setitem__(field,value))
  self.reject(lambda r,v,c,m:r.__setitem__('source_tree','a'*40))
  self.reject(lambda r,v,c,m:v.__setitem__('source_sha_semantics','git_write_tree_label_not_commit'))
  self.reject(lambda r,v,c,m:v.pop('source_identity_proof_member'))
 def test_canonical_identity_protected_files_actual_schema_and_mapping_required(self):
  for file in ['go.mod','go.sum','scripts/run_r5_benchmark.py','scripts/tests/test_r5_benchmark.py','internal/store/postgres/r5_benchmark_test.go','docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json','internal/store/postgres/migrations/00016_fixture.sql']:
   with self.subTest(file=file):self.reject(lambda r,v,c,m:c['files'].pop(file))
  for path,value in [('../escape','1'*64),('/absolute','1'*64),('a//b','1'*64),('unknown.py',True),('internal/store/postgres/migrations/00016_duplicate.sql','2'*64)]:
   with self.subTest(path=path):self.reject(lambda r,v,c,m:c['files'].__setitem__(path,value))
 def test_canonical_proof_requires_exact_original_algorithm_bytes_and_semantics(self):
  for field,value in [('label_algorithm','SHA1(arbitrary_label)'),('closure_algorithm','SHA256(current_checkout)'),('encoding','ascii'),('python_ensure_ascii',1),('trailing_newline',0),('canonical_bytes_length',1.0),('implementation','json.dumps(files)'),('input','current files'),('unknown_protocol',True)]:
   with self.subTest(field=field):self.reject(lambda r,v,c,m:self.mutate_proof(m,lambda p:p['canonicalization'].__setitem__(field,value)))
  self.reject(lambda r,v,c,m:m.__setitem__('canonical-source-closure.json',m['canonical-source-closure.json']+b'\n'))
  self.reject(lambda r,v,c,m:self.mutate_proof(m,lambda p:p.__setitem__('source_sha','a'*40)))
  self.reject(lambda r,v,c,m:self.mutate_proof(m,lambda p:p.__setitem__('source_identity_kind','git_write_tree')))
  self.reject(lambda r,v,c,m:m.__setitem__('canonical-source-identity-proof.json',b'[]'))
 def test_canonical_proof_binds_original_receipt_not_rewritten_metadata(self):
  self.reject(lambda r,v,c,m:m.pop('source-receipt.json'))
  self.reject(lambda r,v,c,m:m.__setitem__('source-receipt.json',m['source-receipt.json']+b'\n'))
  def changed_receipt(r,v,c,m):
   receipt=bench.parse_json(m['source-receipt.json'].decode());receipt['source_sha']='a'*40;m['source-receipt.json']=json.dumps(receipt).encode()
   self.mutate_proof(m,lambda p:p['original_source_receipt'].__setitem__('sha256',hashlib.sha256(m['source-receipt.json']).hexdigest()))
  self.reject(changed_receipt)
  self.reject(lambda r,v,c,m:self.mutate_proof(m,lambda p:p['original_source_receipt'].__setitem__('member','fabricated-source-closure.json')))

def registry_fixture_archive_reference(row,review,directory):
 """Resolve copied fixture inputs from explicit author or delivery bindings.

 This does not adapt production validator schemas or rewrite review payloads.
 """
 if type(row) is not dict or type(review) is not dict:raise ValueError('fixture row and original review objects required')
 population=review.get('original_requested_population') if row.get('failure_variant')==bench.M_TIMEOUT_VARIANT else review.get('population')
 if review.get('source_tree')!=row.get('source_tree') or review.get('scale')!=row.get('scale') or not bench.exact_frozen_equal(population,row.get('population')):raise ValueError('fixture original review source/population binding differs')
 info=review.get('archive')
 if 'archive' in review:
  if type(info) is not dict or not bench.sha256_string(info.get('sha256')) or type(info.get('members')) is not dict:raise ValueError('fixture original archive SHA/member manifest required')
  raw=info.get('path')
  if type(raw) is not str or Path(raw).is_absolute() or '..' in Path(raw).parts:raise ValueError('fixture original archive path unsafe')
  name=Path(raw).name;digest=info['sha256']
  if row.get('archive',name)!=name or row.get('archive_sha256',digest)!=digest:raise ValueError('fixture review/registry archive binding differs')
  return name,digest
 info,_=bench.delivered_archive_binding(row,review,Path(directory))
 return info['path'],info['sha256']

class BenchmarkRegistryFixtureReferenceTests(unittest.TestCase):
 """Fixture input resolution only, not real registry or baseline admission."""
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.directory=Path(self.temp.name)
  self.row={'evidence':'review.json','source_tree':'a'*40,'scale':'S','population':[100,500,100000],'archive':'raw.tar.gz','archive_sha256':'b'*64,'delivery_receipt':'delivery.json'}
  self.review={'source_tree':'a'*40,'scale':'S','population':[100,500,100000]}
 def resolve(self):return registry_fixture_archive_reference(self.row,self.review,self.directory)
 def delivery(self,entry):
  (self.directory/'delivery.json').write_text(json.dumps({'schema_version':1,'deliveries':[entry]}))
 def test_old_author_archive_manifest_binding_not_arbitrary_row(self):
  self.review['archive']={'path':'docs/evidence/raw.tar.gz','sha256':'b'*64,'members':{'S/result.json':'c'*64}}
  self.assertEqual(self.resolve(),('raw.tar.gz','b'*64))
  self.row['archive']='other.tar.gz'
  with self.assertRaises(ValueError):self.resolve()
 def test_safe_delivery_and_unchanged_author_receipt_shapes(self):
  for entry in [{'review':'review.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'b'*64},{'review':'review.json','archive':'raw.tar.gz','copied_archive_bytes_changed':False,'archive_sha256_from_author_receipt':'b'*64},{'review':'review.json','archive':'raw.tar.gz','copied_archive_bytes_changed':True,'archive_sha256':'b'*64}]:
   self.delivery(entry);self.assertEqual(self.resolve(),('raw.tar.gz','b'*64))
 def test_missing_unknown_duplicate_or_wrong_delivery_proof_rejected(self):
  entries=[{'review':'wrong.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'b'*64},{'review':'review.json','safe_archive':'other.tar.gz','safe_archive_sha256':'b'*64},{'review':'review.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'0'*64},{'review':'review.json','archive':'raw.tar.gz','copied_archive_bytes_changed':0,'archive_sha256_from_author_receipt':'b'*64},{'review':'review.json','archive':'raw.tar.gz','archive_sha256':'b'*64},{'review':'review.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'b'*64,'copied_archive_bytes_changed':False},{'review':'review.json','archive':'raw.tar.gz','copied_archive_bytes_changed':False,'archive_sha256_from_author_receipt':'b'*64,'archive_sha256':'0'*64}]
  for entry in entries:
   self.delivery(entry)
   with self.subTest(entry=entry),self.assertRaises(ValueError):self.resolve()
  entry={'review':'review.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'b'*64}
  for delivery in [{'schema_version':True,'deliveries':[entry]},{'schema_version':1,'deliveries':[entry,entry]},{'schema_version':1,'deliveries':[]}]:
   (self.directory/'delivery.json').write_text(json.dumps(delivery))
   with self.assertRaises(ValueError):self.resolve()
 def test_original_source_population_path_and_declared_archive_null_rejected(self):
  self.delivery({'review':'review.json','safe_archive':'raw.tar.gz','safe_archive_sha256':'b'*64})
  for field,value in [('source_tree','c'*40),('scale','M'),('population',[20,100,1000]),('archive',None)]:
   original=copy.deepcopy(self.review);self.review[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):self.resolve()
   self.review=original
  for field,value in [('archive','../raw.tar.gz'),('archive_sha256',True),('delivery_receipt','../delivery.json')]:
   original=copy.deepcopy(self.row);self.row[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):self.resolve()
   self.row=original

class BenchmarkRegistryAdapterTests(unittest.TestCase):
 """Explicit adapter grammar/byte proof, never new PG or S/M execution."""
 @classmethod
 def setUpClass(cls):
  base=bench.DATASET.parent;registry=bench.read_json(base/'R5-BENCHMARK-RUNS.json')
  cls.rows={row['source_tree']:row for row in registry['runs']}
  cls.row=cls.rows[bench.FROZEN_LOOKAHEAD_S_SOURCE]
  cls.review=bench.read_json(base/cls.row['evidence'])
  info,cls.delivery=bench.delivered_archive_binding(cls.row,cls.review,base)
  cls.members=bench.archive_evidence(cls.review,base,cls.row['archive'],delivered_binding=info)
  cls.closure=bench.archived_json(cls.members,'S/source-closure.json')
 def test_multiple_distinct_S_identities_and_duplicate_alias_run_rejected(self):
  identities=set();observed=set();ids=set()
  for i,source in enumerate([bench.FROZEN_S2[0],bench.FROZEN_PIPELINE_S_SOURCE,bench.FROZEN_LOOKAHEAD_S_SOURCE]):
   row=self.rows[source]
   bench.register_run_identity(row,'S',source,15 if i==0 else 16,row['dataset_contract_sha256'],row.get('preparation_method',bench.DEFAULT_METHOD),row.get('method_contract_sha256'),row['result_sha256'],identities,observed,ids)
  self.assertEqual(len(identities),3)
  row=copy.deepcopy(self.row)
  for run_id in [row['run_id'],'different_alias_for_same_actual_result',True,1.0,'../unsafe']:
   row['run_id']=run_id
   with self.subTest(run_id=run_id),self.assertRaises(ValueError):bench.register_run_identity(row,'S',row['source_tree'],16,row['dataset_contract_sha256'],bench.LOOKAHEAD_METHOD,row['method_contract_sha256'],row['result_sha256'],identities,observed,ids)
 def test_only_explicit_known_review_version_tuple_and_fields_admitted(self):
  self.assertEqual(bench.run_review_layout(self.row,self.review),'lookahead_s_receipt_native')
  for field,value in [('status','unknown_review_version'),('Go_exit',None),('source_tree','a'*40)]:
   review=dict(self.review);review[field]=value
   # Missing/value-unknown format keys must not select generic schema16 adapter.
   if field=='Go_exit':review.pop(field)
   with self.subTest(field=field),self.assertRaises(ValueError):bench.run_review_layout(self.row,review)
  for mutate in [lambda d:d.__setitem__('unknown_protocol',True),lambda d:d.__setitem__('go_exit',0),lambda d:d.__setitem__('archive',{'path':'fake.tar.gz','sha256':'b'*64,'members':{}}),lambda d:d.pop('actual_scheduler')]:
   review=dict(self.review);mutate(review)
   with self.assertRaises(ValueError):bench.run_review_layout(self.row,review)
  row=dict(self.row);row['source_tree']='a'*40
  with self.assertRaises(ValueError):bench.run_review_layout(row,self.review)
 def test_legacy_embedded_review_only_exact_S2_version_not_unknown_or_future_M(self):
  row=self.rows[bench.FROZEN_S2[0]];review=bench.read_json(bench.DATASET.parent/row['evidence'])
  self.assertEqual(bench.run_review_layout(row,review),'embedded_archive')
  for mutate in [lambda d:d.__setitem__('unknown_protocol',True),lambda d:d.__setitem__('status','unversioned_unknown_review'),lambda d:d.__setitem__('task','fresh_gate_review_not_original_run'),lambda d:d.__setitem__('budget_seconds',1800.0),lambda d:d.pop('actual_test_lifecycle')]:
   bad=copy.deepcopy(review);mutate(bad)
   with self.assertRaises(ValueError):bench.run_review_layout(row,bad)
  for change in [{'source_tree':'a'*40},{'source_tree':'a'*40,'scale':'M','population':[1000,5000,1000000]},{'schema_version':16},{'schema_version':15.0},{'dataset_contract_sha256':bench.LOOKAHEAD_DATASET_SHA256},{'source_identity_kind':bench.CANONICAL_SOURCE_KIND}]:
   bad_row=copy.deepcopy(row);bad_row.update(change);bad_review=copy.deepcopy(review)
   for field in ['source_tree','scale','population']:
    if field in change:bad_review[field]=change[field]
   with self.subTest(change=change),self.assertRaises(ValueError):bench.run_review_layout(bad_row,bad_review)
 def test_actual_receipt_native_proof_not_synthetic_old_proof_or_current_source(self):
  bench.validate_source_identity(self.row,self.review,self.closure,self.members)
  for field in ['canonical-source-closure.json','source-label','source-receipt.json']:
   members=dict(self.members);members.pop(field)
   with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_source_identity(self.row,self.review,self.closure,members)
  members=dict(self.members);members['canonical-source-closure.json']+=b'\n'
  with self.assertRaises(ValueError):bench.validate_source_identity(self.row,self.review,self.closure,members)
  members=dict(self.members);members['source-label']=('a'*40+'\n').encode()
  with self.assertRaises(ValueError):bench.validate_source_identity(self.row,self.review,self.closure,members)
  for mutate in [lambda d:d.__setitem__('method_contract_sha256',hashlib.sha256(bench.LOOKAHEAD_METHOD_FILE.read_bytes()).hexdigest()),lambda d:d.__setitem__('source_sha','a'*40),lambda d:d['Go_final_author_hashes_matched'].pop('internal/store/postgres/r5_preparation_optin_test.go'),lambda d:d['validator_final_protected_hashes_matched'].__setitem__('scripts/run_r5_benchmark.py','0'*64),lambda d:d['environment'].__setitem__(bench.METHOD_ENV,bench.DEFAULT_METHOD)]:
   receipt=bench.archived_json(self.members,'source-receipt.json');mutate(receipt);members=dict(self.members);members['source-receipt.json']=json.dumps(receipt).encode()
   with self.assertRaises(ValueError):bench.validate_source_identity(self.row,self.review,self.closure,members)
  row=dict(self.row);row['source_identity_proof_member']='canonical-source-identity-proof.json'
  with self.assertRaises(ValueError):bench.validate_source_identity(row,self.review,self.closure,self.members)
 def test_result_sidecar_exact_actual_members_source_not_archive_digest_substitute(self):
  with tempfile.TemporaryDirectory() as root:
   directory=Path(root);members={'S/result.json':b'grammar-result','S/go.jsonl':b'grammar-go-lifecycle'}
   result=hashlib.sha256(members['S/result.json']).hexdigest();go=hashlib.sha256(members['S/go.jsonl']).hexdigest()
   row={'source_tree':bench.FROZEN_LOOKAHEAD_S_SOURCE,'archive_sha256':'b'*64,'result_sha256':result,'raw_go_sha256':go,'result_binding_receipt':'binding.json'}
   binding={'created_utc':'2026-10-02T00:00:00Z','source_sha':row['source_tree'],'result_member':'S/result.json','result_sha256':result,'raw_go_member':'S/go.jsonl','raw_go_sha256':go,'original_archive':'original.tar.gz','original_archive_sha256':'b'*64,'source_raw_archive_unchanged':True,'scope':'unit grammar only'}
   delivery={'source_archive':'/author/original.tar.gz','copied_archive_bytes_changed':False}
   path=directory/'binding.json';path.write_text(json.dumps(binding))
   bench.validate_review_result_binding(row,{},members,'S',result,'lookahead_s_receipt_native',directory,delivery)
   for field,value in [('source_sha','a'*40),('result_sha256','b'*64),('raw_go_sha256','b'*64),('result_member','S/other.json'),('raw_go_member','S/other.jsonl'),('original_archive','other.tar.gz'),('source_raw_archive_unchanged',1),('unknown_proof',True)]:
    bad=dict(binding);bad[field]=value;path.write_text(json.dumps(bad))
    with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_review_result_binding(row,{},members,'S',result,'lookahead_s_receipt_native',directory,delivery)
   duplicated=json.dumps(binding).replace('"source_sha": "'+row['source_tree']+'"','"source_sha": "'+row['source_tree']+'", "source_sha": "'+row['source_tree']+'"',1);path.write_text(duplicated)
   with self.assertRaises(ValueError):bench.validate_review_result_binding(row,{},members,'S',result,'lookahead_s_receipt_native',directory,delivery)
   path.write_text(json.dumps(binding))
   for name in members:
    missing=dict(members);missing.pop(name)
    with self.assertRaises(ValueError):bench.validate_review_result_binding(row,{},missing,'S',result,'lookahead_s_receipt_native',directory,delivery)
   changed=dict(delivery);changed['copied_archive_bytes_changed']=True
   with self.assertRaises(ValueError):bench.validate_review_result_binding(row,{},members,'S',result,'lookahead_s_receipt_native',directory,changed)
 def test_delivered_review_is_original_payload_bytes_not_refilled_wrapper(self):
  raw=b'{"source":"unit grammar"}'
  bench.validate_delivered_review_payload(raw,{'S-runtime-review.json':raw})
  for members in [{},{'S-runtime-review.json':raw+b'\n'},{'S-runtime-review.json':b'{"source":"refilled"}'}]:
   with self.assertRaises(ValueError):bench.validate_delivered_review_payload(raw,members)
 def test_raw_exit_integer_bytes_do_not_accept_float_boolean_or_alias(self):
  for raw,expected in [(b'0\n',0),(b'-15\n',-15),(b'1',1)]:self.assertEqual(bench.raw_exit_receipt(raw),expected)
  for raw in [b'+0\n',b'00\n',b'-0\n',b'0.0\n',b'true\n',b' 0\n',b'0\n0\n',0,False]:
   with self.subTest(raw=raw),self.assertRaises(ValueError):bench.raw_exit_receipt(raw)
 def test_archive_safe_empty_directories_only_not_links_duplicates_or_unsafe_paths(self):
  with tempfile.TemporaryDirectory() as root:
   directory=Path(root);path=directory/'raw.tar.gz'
   def check(items):
    with tarfile.open(path,'w:gz') as archive:
     for name,kind in items:
      info=tarfile.TarInfo(name);info.type=kind
      if kind==tarfile.REGTYPE:info.size=1;archive.addfile(info,io.BytesIO(b'x'))
      else:archive.addfile(info)
    binding={'path':path.name,'sha256':hashlib.sha256(path.read_bytes()).hexdigest()}
    return bench.archive_evidence({},directory,path.name,delivered_binding=binding)
   self.assertEqual(check([('S',tarfile.DIRTYPE),('S/result.json',tarfile.REGTYPE)]),{'S/result.json':b'x'})
   for entries in [[('S/alias',tarfile.SYMTYPE)],[('S/result.json',tarfile.REGTYPE),('S/result.json',tarfile.REGTYPE)],[('../escape',tarfile.REGTYPE)],[('S',tarfile.DIRTYPE),('S/',tarfile.DIRTYPE)],[('S//',tarfile.DIRTYPE)]]:
    with self.subTest(entries=entries),self.assertRaises(ValueError):check(entries)

class BenchmarkRegistryClockAndMFailureTests(unittest.TestCase):
 """New clock/version feedback only; no benchmark or PG rerun."""
 def test_known_summary_clock_scopes_are_bound_not_forced_equal(self):
  events=[{'Action':'run','Test':'TestR5BenchmarkOriginalScale','Time':'2026-10-02T00:00:00Z'},{'Action':'pass','Test':'TestR5BenchmarkOriginalScale','Time':'2026-10-02T00:00:20Z','Elapsed':20.0}]
  execution={'started_unix_seconds':1790899201.0,'completed_unix_seconds':1790899219.0,'elapsed_wall_seconds':18.0,'budget_seconds':1800}
  # Literal timestamp above is only grammar fixture; derive the bound epoch.
  execution['started_unix_seconds']=bench.go_event_timestamp(events[0])+1;execution['completed_unix_seconds']=bench.go_event_timestamp(events[1])-1
  result={'execution_wall':execution};review=copy.deepcopy(result)
  self.assertEqual(bench.validate_run_elapsed_summary({'elapsed_seconds':18.0},review,result,events,'pipeline_s_v1_delivered','S'),'original_source_bound_result_execution_wall_elapsed_seconds')
  self.assertEqual(bench.validate_run_elapsed_summary({'elapsed_seconds':20.0},{},{},events,'embedded_archive','S'),'original_go_test_pass_elapsed_seconds')
  for row,layout in [({'elapsed_seconds':20.0},'pipeline_s_v1_delivered'),({'elapsed_seconds':18.0},'embedded_archive'),({'elapsed_seconds':True},'lookahead_s_receipt_native')]:
   with self.assertRaises(ValueError):bench.validate_run_elapsed_summary(row,review,result,events,layout,'S')
  escaped=copy.deepcopy(result);escaped['execution_wall']['started_unix_seconds']=bench.go_event_timestamp(events[0])-1
  with self.assertRaises(ValueError):bench.validate_run_elapsed_summary({'elapsed_seconds':18.0},escaped,escaped,events,'lookahead_s_receipt_native','S')
  # New package boundary mutations: independent clocks remain distinct, but
  # neither a missing package observation nor a forged S2 review is admitted.
  package={'Action':'pass','Package':'tabmail/internal/store/postgres','Time':'2026-10-02T00:00:21Z','Elapsed':21.0}
  observed=events+[package];s2={'go_test_elapsed_seconds':20.0,'package_elapsed_seconds':21.0}
  self.assertEqual(bench.validate_package_elapsed_observation(observed,s2,'embedded_archive',1800),21.0)
  self.assertEqual(bench.validate_package_elapsed_observation(observed,review,'lookahead_s_receipt_native',1800),21.0)
  for value in [None,True,-1,1801,float('inf')]:
   bad=copy.deepcopy(observed);bad[-1]['Elapsed']=value
   with self.subTest(package_elapsed=value),self.assertRaises(ValueError):bench.validate_package_elapsed_observation(bad,s2,'embedded_archive',1800)
  bad=copy.deepcopy(observed);bad[-1].pop('Elapsed')
  with self.assertRaises(ValueError):bench.validate_package_elapsed_observation(bad,s2,'embedded_archive',1800)
  for field,value in [('go_test_elapsed_seconds',21.0),('package_elapsed_seconds',20.0),('package_elapsed_seconds',True)]:
   bad=copy.deepcopy(s2);bad[field]=value
   with self.subTest(review_clock=field),self.assertRaises(ValueError):bench.validate_package_elapsed_observation(observed,bad,'embedded_archive',1800)
 def test_typed_M_failure_version_exact_failed_role_not_S_schema17_or_fake_metrics(self):
  registry=bench.read_json(bench.DATASET.parent/'R5-BENCHMARK-RUNS.json');row=next(r for r in registry['failed_attempts'] if r.get('failure_variant')==bench.M_TIMEOUT_VARIANT);review=bench.read_json(bench.DATASET.parent/row['evidence'])
  self.assertEqual(bench.m_failure_review_layout(row,review),'m_absolute_timeout_receipt_v1')
  for field,value in [('scale','S'),('source_tree',bench.FROZEN_LOOKAHEAD_S_SOURCE),('schema_version',17),('budget_seconds',10801),('budget_seconds',10800.0),('go_exit',None),('go_exit',True),('method_contract_sha256',bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256),('population',[1000,5000,753380])]:
   bad=copy.deepcopy(row);bad[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.m_failure_review_layout(bad,review)
  for field,value in [('M_executed',True),('baseline_complete',True),('result_JSON_absent',False),('completed_workload_samples_evidenced',1),('completed_workload_samples_evidenced',0.0),('matrix14by200_completed',True),('unknown_version',True)]:
   bad=copy.deepcopy(review);bad[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.m_failure_review_layout(row,bad)
  # New row completion mutations must fail without changing the historical row.
  stats=review['captured_preparation_incomplete_stats']
  bench.validate_typed_m_failure_summary(row,stats)
  for field in ['task_complete','product_green']:
   for value in [True,0]:
    bad=copy.deepcopy(row);bad[field]=value
    with self.subTest(completion_field=field,value=value),self.assertRaises(ValueError):bench.validate_typed_m_failure_summary(bad,stats)
 def test_actual_typed_M_failure_packet_positive_and_bound_negative_evidence(self):
  base=bench.DATASET.parent;registry=bench.read_json(base/'R5-BENCHMARK-RUNS.json');row=next(r for r in registry['failed_attempts'] if r.get('failure_variant')==bench.M_TIMEOUT_VARIANT)
  with tempfile.TemporaryDirectory() as root:
   directory=Path(root)
   for field in ['evidence','archive','delivery_receipt','frozen_method_contract','readonly_reporting_corrections']:shutil.copyfile(bench.evidence_path(row[field],base),directory/row[field])
   review=bench.read_json(directory/row['evidence'])
   proof=bench.validate_typed_m_failure(row,review,directory);self.assertFalse(proof['baseline_complete']);self.assertEqual(proof['actual_partial_population']['actual_stored_messages'],753380);self.assertEqual(proof['actual_partial_population']['actual_durable_current_sourcebound_SQL_ready'],753360)
   for field,value in [('result_JSON_absent',False),('matrix14by200_completed',True),('completed_workload_samples',200),('method_admission_claimed_no_failure_until_deadline',True),('elapsed_wall_seconds',10801),('raw_go_sha256','0'*64)]:
    bad=copy.deepcopy(row);bad[field]=value
    with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_typed_m_failure(bad,review,directory)
   bad=copy.deepcopy(row);bad['actual_partial_population']['actual_schema']=17
   with self.assertRaises(ValueError):bench.validate_typed_m_failure(bad,review,directory)
   bad=copy.deepcopy(row);bad['actual_partial_population']['actual_stored_messages']=1000000
   with self.assertRaises(ValueError):bench.validate_typed_m_failure(bad,review,directory)
   # New summary mutations bind every declared status/phase/join/reserve field
   # to the already authenticated raw TERMINAL-labelled incomplete marker.
   for field,value in [('status','passed'),('baseline_complete',True),('go_exit_status','missing_observation'),('go_test_terminal_status','PASS'),('strict_validation_status','actual_scale_evidence_validated'),('preparation_TERMINAL_marker_is_complete',True),('preparation_phase','complete'),('producer_done',True),('final_empty_claim',True),('worker_joined_inflight',1),('parser_joined_inflight',1),('parser_quiescent',False),('parser_min_start_remaining_wall_seconds',31.0)]:
    bad=copy.deepcopy(row);bad[field]=value
    with self.subTest(summary_field=field),self.assertRaises(ValueError):bench.validate_typed_m_failure(bad,review,directory)

class BenchmarkRegistryTests(unittest.TestCase):
 @classmethod
 def setUpClass(cls):
  # Capture and bind genuine fixture inputs once. Each test mutates only its
  # private copy; review bytes and complete registry history remain unchanged.
  cls.source_temp=tempfile.TemporaryDirectory();cls.addClassCleanup(cls.source_temp.cleanup)
  cls.source_directory=Path(cls.source_temp.name);base=bench.DATASET.parent
  cls.source_registry=bench.read_json(base/'R5-BENCHMARK-RUNS.json')
  copied=set()
  for row in cls.source_registry['runs']+cls.source_registry.get('failed_attempts',[]):
   review=bench.read_json(bench.evidence_path(row['evidence'],base))
   archive_name,digest=registry_fixture_archive_reference(row,review,base)
   names=[row['evidence'],archive_name]
   names.extend(row[field] for field in ['dataset_contract','delivery_receipt','result_binding_receipt','frozen_method_contract','method_contract','readonly_reporting_corrections'] if field in row)
   for name in names:
    source=bench.evidence_path(name,base)
    if Path(name).name!=name:raise ValueError('fixture flat original evidence filename required')
    if name not in copied:shutil.copyfile(source,cls.source_directory/name);copied.add(name)
   if hashlib.sha256((cls.source_directory/archive_name).read_bytes()).hexdigest()!=digest:raise ValueError('fixture copied archive differs from original explicit receipt')
 def setUp(self):
  self.temp=tempfile.TemporaryDirectory();self.addCleanup(self.temp.cleanup);self.directory=Path(self.temp.name)
  self.registry=copy.deepcopy(self.source_registry)
  for source in self.source_directory.iterdir():shutil.copyfile(source,self.directory/source.name)
 def validate(self):return bench.validate_registry(self.registry,directory=self.directory)
 def rewrite_first_archive(self,modify):
  row=self.registry['runs'][0];reviewpath=self.directory/row['evidence'];review=bench.read_json(reviewpath);path=self.directory/row['archive']
  with tarfile.open(path,'r:gz') as tar:members={m.name:tar.extractfile(m).read() for m in tar}
  modify(members)
  with tarfile.open(path,'w:gz') as tar:
   for name,payload in members.items():
    info=tarfile.TarInfo(name);info.size=len(payload);tar.addfile(info,io.BytesIO(payload))
  review['archive']['sha256']=hashlib.sha256(path.read_bytes()).hexdigest();review['archive']['members']={k:hashlib.sha256(v).hexdigest() for k,v in members.items()}
  resultnames=[k for k in members if k.endswith('/result.json')]
  if len(resultnames)==1:row['result_sha256']=review['result_sha256']=hashlib.sha256(members[resultnames[0]]).hexdigest()
  reviewpath.write_text(json.dumps(review))
 def mutate_result(self,modify):
  def change(members):
   name=next(k for k in members if k.endswith('/result.json'));result=bench.parse_json(members[name].decode());modify(result);members[name]=json.dumps(result).encode()
  self.rewrite_first_archive(change)
 def test_real_success_and_failed_attempts_are_not_static_empty_registry(self):
  verdict=self.validate();self.assertEqual(verdict['validated_scales'],sorted({x['scale'] for x in self.registry['runs']}));self.assertFalse(verdict['task_complete']);self.assertFalse(verdict['product_green'])
 def test_frozen_S2_must_not_be_backfilled_with_new_pipeline_metadata(self):
  self.mutate_result(lambda r:r.__setitem__('preparation',BenchmarkEvidenceTests.preparation(100000)['preparation']))
  with self.assertRaises(ValueError):self.validate()
 def test_historical_contract_and_schema_not_current_metadata(self):
  with mock.patch.object(bench,'expected_schema_version',return_value=999):self.validate()
  self.assertNotEqual(self.registry['runs'][0]['dataset_contract_sha256'],hashlib.sha256(bench.DATASET.read_bytes()).hexdigest())
  self.registry['runs'][0]['dataset_contract_sha256']=hashlib.sha256(bench.DATASET.read_bytes()).hexdigest()
  with self.assertRaises(ValueError):self.validate()
 def test_fake_flags_tool_only_and_source_identity_rejected(self):
  original=copy.deepcopy(self.registry)
  for field,value in [('scale','tool_only'),('source_tree','b'*40),('source_identity_kind','current_HEAD'),('budget_seconds',1801),('population',[20,100,1000]),('baseline_complete',1),('candidate_threshold_passed',False)]:
   self.registry=copy.deepcopy(original);self.registry['runs'][0][field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):self.validate()
  self.registry=copy.deepcopy(original);self.registry['runs']=[];self.registry['S_executed']=True
  with self.assertRaises(ValueError):self.validate()
  self.registry=copy.deepcopy(original);self.registry['task_complete']=True
  with self.assertRaises(ValueError):self.validate()
 def test_missing_original_result_not_a_green_review(self):
  self.rewrite_first_archive(lambda m:m.pop(next(k for k in m if k.endswith('/result.json'))))
  with self.assertRaises(ValueError):self.validate()
 def test_tool_only_marker_on_original_scale_result_rejected(self):
  self.mutate_result(lambda r:r.__setitem__('mode','TOOL_ONLY_DATASET_CALIBRATION'))
  with self.assertRaises(ValueError):self.validate()
 def test_raw_source_and_process_protocol_not_only_hashes(self):
  self.mutate_result(lambda r:r.__setitem__('source_sha','b'*40))
  with self.assertRaises(ValueError):self.validate()
 def test_raw_exit_failure_cannot_be_hidden_by_passed_review(self):
  self.rewrite_first_archive(lambda m:m.__setitem__(next(k for k in m if k.endswith('/go.exit')),b'1\n'))
  with self.assertRaises(ValueError):self.validate()
 def test_raw_host_suspend_budget_cannot_hide_under_go_elapsed(self):
  def change(m):
   name=next(k for k in m if k.endswith('/go.jsonl'));events=[bench.parse_json(l) for l in m[name].decode().splitlines()]
   for e in events:
    if e.get('Action')=='pass' and 'Test' not in e:e['Time']='2026-10-02T17:38:00+08:00'
   m[name]=''.join(json.dumps(e)+'\n' for e in events).encode()
  self.rewrite_first_archive(change)
  with self.assertRaises(ValueError):self.validate()
 def test_preflight_population_budget_and_artifact_hashes_rejected(self):
  def change(m):
   name=next(k for k in m if k.endswith('/preflight.json'));value=bench.parse_json(m[name].decode());value['budget_seconds']=99999;m[name]=json.dumps(value).encode()
  self.rewrite_first_archive(change)
  with self.assertRaises(ValueError):self.validate()
 def test_missing_history_or_archive_and_failed_attempt_not_success(self):
  row=self.registry['runs'][0];path=self.directory/row['dataset_contract'];path.unlink()
  with self.assertRaises(ValueError):self.validate()
 def test_failed_attempt_uses_actual_raw_exit_and_source(self):
  self.registry['failed_attempts'][0]['go_exit']=0
  with self.assertRaises(ValueError):self.validate()

 def test_missing_raw_runner_exit_cannot_be_inferred_from_go(self):
  self.rewrite_first_archive(lambda m:m.pop(next(k for k in m if k.startswith('runner/') and k.endswith('-runner.exit'))))
  with self.assertRaises(ValueError):self.validate()
 def test_original_command_cannot_extend_frozen_go_timeout(self):
  def change(m):
   name=next(k for k in m if k.endswith('/command.json'));cmd=bench.parse_json(m[name].decode());cmd=[x.replace('-timeout=1800s','-timeout=18000s') for x in cmd];m[name]=json.dumps(cmd).encode()
  self.rewrite_first_archive(change)
  with self.assertRaises(ValueError):self.validate()
 def test_source_closure_digest_not_just_declared_source(self):
  def change(m):
   name=next(k for k in m if k.endswith('/source-closure.json'));value=bench.parse_json(m[name].decode());value['actual_source_closure_sha256']='0'*64;m[name]=json.dumps(value).encode()
  self.rewrite_first_archive(change)
  with self.assertRaises(ValueError):self.validate()
 def prepare_unknown_failed_grammar_fixture(self):
  # Parser grammar fixture only: mutate a temporary copy of historical raw
  # evidence. This is never an authoritative attempt or a performance result.
  row=next(row for row in self.registry['failed_attempts'] if row['scale']=='M')
  reviewpath=self.directory/row['evidence'];review=bench.read_json(reviewpath)
  archive=self.directory/Path(review['archive']['path']).name
  with tarfile.open(archive,'r:gz') as tar:members={m.name:tar.extractfile(m).read() for m in tar}
  missing=['go.exit','execution-budget.json','timeout.json','result.json']
  for key in list(members):
   if any(key.endswith('/'+name) for name in missing):members.pop(key)
  runner=next(k for k in members if k.startswith('runner/') and k.endswith('-runner.json'))
  members[runner]=json.dumps({'status':'rejected','task_complete':False,'product_green':False,'error':'[Errno 1] Operation not permitted'}).encode()
  for obj in [row,review]:obj.update(status='failed',go_exit=None,go_exit_status='missing_observation',missing_observations=missing,runner_exit=1)
  def rewrite(modify=lambda _:None):
   modify(members)
   with tarfile.open(archive,'w:gz') as tar:
    for name,payload in members.items():
     info=tarfile.TarInfo(name);info.size=len(payload);tar.addfile(info,io.BytesIO(payload))
   review['archive']['sha256']=hashlib.sha256(archive.read_bytes()).hexdigest();review['archive']['members']={name:hashlib.sha256(payload).hexdigest() for name,payload in members.items()};reviewpath.write_text(json.dumps(review))
  rewrite();return row,review,reviewpath,rewrite
 def test_unknown_failed_variant_never_becomes_success(self):
  row,review,path,rewrite=self.prepare_unknown_failed_grammar_fixture()
  self.validate()
  original=copy.deepcopy(row)
  for key,value in [('go_exit',0),('go_exit',-15),('status','passed'),('go_exit_status','observed'),('missing_observations',[]),('scale','S'),('baseline_complete',True),('task_complete',True),('product_green',True)]:
   row.clear();row.update(original);row[key]=value
   with self.subTest(key=key),self.assertRaises(ValueError):self.validate()
  row.clear();row.update(original)
  self.registry['M_executed']=True
  with self.assertRaises(ValueError):self.validate()
 def test_unknown_failed_variant_requires_raw_rejection_and_absence(self):
  row,review,path,rewrite=self.prepare_unknown_failed_grammar_fixture();self.validate()
  rewrite(lambda members:members.__setitem__(next(k for k in members if k.endswith('/go.jsonl')).rsplit('/',1)[0]+'/go.exit',b'-15\n'))
  with self.assertRaises(ValueError):self.validate()
 def test_unknown_failed_variant_preserves_exact_original_provenance(self):
  row,review,path,rewrite=self.prepare_unknown_failed_grammar_fixture()
  def mutate(members):
   key=next(k for k in members if k.endswith('/source-closure.json'));value=bench.parse_json(members[key].decode());value['actual_source_closure_sha256']='0'*64;members[key]=json.dumps(value).encode()
  rewrite(mutate)
  with self.assertRaises(ValueError):self.validate()
 def test_unknown_failed_variant_rejection_must_be_actual_eperm(self):
  row,review,path,rewrite=self.prepare_unknown_failed_grammar_fixture()
  def mutate(members):
   key=next(k for k in members if k.startswith('runner/') and k.endswith('-runner.json'));value=bench.parse_json(members[key].decode());value['error']='other failure';members[key]=json.dumps(value).encode()
  rewrite(mutate)
  with self.assertRaises(ValueError):self.validate()
 def test_unknown_failed_variant_rejects_terminal_go_or_missing_exit_key(self):
  row,review,path,rewrite=self.prepare_unknown_failed_grammar_fixture()
  saved=row.pop('go_exit')
  with self.assertRaises(ValueError):self.validate()
  row['go_exit']=saved
  def mutate(members):
   key=next(k for k in members if k.endswith('/go.jsonl'));members[key]+=json.dumps({'Action':'pass','Package':'tabmail/internal/store/postgres'}).encode()+b'\n'
  rewrite(mutate)
  with self.assertRaises(ValueError):self.validate()
 def test_failed_population_float_and_fake_product_green_rejected(self):
  self.registry['failed_attempts'][0]['population'][0]=100.0
  with self.assertRaises(ValueError):self.validate()
  self.registry['failed_attempts'][0]['population'][0]=100;self.registry['product_green']=True
  with self.assertRaises(ValueError):self.validate()

class BenchmarkLookaheadTests(Schema16GrammarFixture):
 """Strict grammar and mocked CLI only; never actual PG/S/performance evidence."""
 def setUp(self):
  super().setUp()
  self.contract=bench.read_json(bench.DATASET)
  self.method=bench.read_json(bench.LOOKAHEAD_METHOD_FILE)
  self.digest=hashlib.sha256(bench.LOOKAHEAD_METHOD_FILE.read_bytes()).hexdigest()
  self.result={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'source_sha':'a'*40,'schema_version':16,'dataset_sha256':bench.LOOKAHEAD_DATASET_SHA256,'task_complete':False,'product_green':False,'S_M_L_executed':False,'seed':3893945,'rss_bytes':1,'disk_bytes':1,**BenchmarkEvidenceTests.observations(20,1000,'tool_only')}
  self.result.update(preparation_method=bench.LOOKAHEAD_METHOD,method_contract_sha256=self.digest,preparation_identity_boundary=copy.deepcopy(bench.IDENTITY_BOUNDARY))
  for stats in [self.result['preparation'],*[c['preparation'] for c in self.result['preparation_checkpoints']],*[c['actual_claim_joined'] for c in self.result['preparation_checkpoints'][:-1]]]:stats['method']=bench.LOOKAHEAD_METHOD
  order=bench.expected_scheduler_order(1000,20);boxes=[bench.fixture_mailbox_ordinal(n,20) for n in order]
  self.result['preparation_input_scheduler']={'method':bench.LOOKAHEAD_METHOD,'lookahead_limit':100,'generated_inputs':1000,'emitted_inputs':1000,'ordinal_fifo_completed_jobs':1000,'max_unpersisted_buffered_inputs':100,'joined_fifo_inflight':0,'fifo_gate_call_elapsed_seconds_sum':0.01,'actual_emitted_ordinal_order':order,'actual_emitted_mailbox_ordinals':boxes,'actual_emitted_window_unique_mailboxes':[len(set(boxes[i:i+20])) for i in range(0,1000,20)]}
  self.result['preparation_persistent_observations']={'origin':'actual_owned_pg_catalog_and_received_at_projection','source_sha':'a'*40,'schema_version':16,'uid_catalog_query_sha256':bench.LOOKAHEAD_CONTRACT['persistent_query_sha256']['uid_catalog_query_sha256'],'uid_column_count':0,'received_at_projection_query_sha256':bench.LOOKAHEAD_CONTRACT['persistent_query_sha256']['received_at_projection_query_sha256'],'projected_messages':1000,'projected_mailboxes':len(set(boxes)),'ordinal_fifo_violations':0,'index_ready_count':1000,'source_identity_bindings_valid':True}
  self.validate(self.result)
 def validate(self,result):return bench.validate_tool_result(result,self.contract,'a'*40,preparation_method=bench.LOOKAHEAD_METHOD,method_contract=self.method,method_contract_digest=self.digest)
 def reject(self,modify):
  data=copy.deepcopy(self.result);modify(data)
  with self.assertRaises(ValueError):self.validate(data)
 def test_explicit_contract_population_and_non_equivalence_boundary(self):
  bench.validate_method_contract(self.method)
  self.assertFalse(self.validate(self.result)['S_M_L_executed'])
  self.assertFalse(self.result['preparation_identity_boundary']['all_persistent_equivalent'])
  self.assertFalse(self.result['preparation_identity_boundary']['protocol_UID_certified'])
  self.assertLess(self.result['preparation_persistent_observations']['projected_mailboxes'],100)
  with self.assertRaises(ValueError):bench.validate_result(self.result,self.contract,'M','a'*40,preparation_method=bench.LOOKAHEAD_METHOD,method_contract=self.method,method_contract_digest=self.digest)
 def test_new_original_S_complete_grammar_preserves_all_workloads_and_not_approval(self):
  fixture=BenchmarkEvidenceTests();self.addCleanup(fixture.doCleanups);fixture.setUp();data=copy.deepcopy(fixture.result)
  data.update({k:copy.deepcopy(self.result[k]) for k in bench.LOOKAHEAD_RESULT_FIELDS})
  for stats in [data['preparation'],*[c['preparation'] for c in data['preparation_checkpoints']],*[c['actual_claim_joined'] for c in data['preparation_checkpoints'][:-1]]]:stats['method']=bench.LOOKAHEAD_METHOD
  order=bench.expected_scheduler_order(100000,100);boxes=[bench.fixture_mailbox_ordinal(n,100) for n in order]
  scheduler=data['preparation_input_scheduler'];scheduler.update(generated_inputs=100000,emitted_inputs=100000,ordinal_fifo_completed_jobs=100000,actual_emitted_ordinal_order=order,actual_emitted_mailbox_ordinals=boxes,actual_emitted_window_unique_mailboxes=[len(set(boxes[i:i+20])) for i in range(0,100000,20)])
  data['preparation_persistent_observations'].update(projected_messages=100000,projected_mailboxes=len(set(boxes)),index_ready_count=100000)
  verdict=bench.validate_result(data,self.contract,'S','a'*40,preparation_method=bench.LOOKAHEAD_METHOD,method_contract=self.method,method_contract_digest=self.digest)
  self.assertFalse(verdict['task_complete']);self.assertFalse(verdict['product_green']);self.assertEqual(len(data['measurements']),14)
 def test_unknown_missing_duplicate_and_type_alias_method_contract_rejected(self):
  mutations=[lambda d:d.pop('input_scheduler_contract'),lambda d:d.__setitem__('unknown_protocol',True),lambda d:d['preparation_contract'].__setitem__('window_messages',100),lambda d:d['input_scheduler_contract'].__setitem__('lookahead_limit',100.0),lambda d:d['input_scheduler_contract'].__setitem__('window_messages',True),lambda d:d.__setitem__('schema_version',True),lambda d:d['preparation_identity_boundary'].__setitem__('all_persistent_equivalent',0),lambda d:d['input_scheduler_contract'].__setitem__('failure_policy','continue_dependents')]
  for mutate in mutations:
   data=copy.deepcopy(self.method);mutate(data)
   with self.subTest(data=data),self.assertRaises(ValueError):bench.validate_method_contract(data)
  payload=json.dumps(self.method).replace('"lookahead_limit": 100','"lookahead_limit": 100, "lookahead_limit": 100',1)
  with self.assertRaises(ValueError):bench.parse_json(payload)
 def test_default_v1_and_history_cannot_auto_select_or_accept_new_fields(self):
  with self.assertRaises(ValueError):bench.validate_tool_result(self.result,self.contract,'a'*40)
  with self.assertRaises(ValueError):bench.validate_method_selection(self.result,bench.LOOKAHEAD_METHOD,self.method,self.digest,frozen_legacy=True)
  self.reject(lambda d:d.__setitem__('preparation_method',bench.DEFAULT_METHOD))
  self.reject(lambda d:d.__setitem__('method_contract_sha256','0'*64))
  self.reject(lambda d:d.__setitem__('unknown_preparation_protocol',True))
  self.reject(lambda d:d.__setitem__('dataset_sha256','0'*64))
  self.reject(lambda d:d.__setitem__('source_sha','b'*40))
  self.reject(lambda d:d['preparation_checkpoints'][0]['actual_claim_joined'].__setitem__('method',bench.DEFAULT_METHOD))
 def test_scheduler_complete_joined_counts_and_numeric_aliases_rejected(self):
  fields=['lookahead_limit','generated_inputs','emitted_inputs','ordinal_fifo_completed_jobs','max_unpersisted_buffered_inputs','joined_fifo_inflight']
  for field in fields:
   for value in [True,100.0,-1,None]:
    with self.subTest(field=field,value=value):self.reject(lambda d:d['preparation_input_scheduler'].__setitem__(field,value))
  for field,value in [('lookahead_limit',101),('generated_inputs',999),('emitted_inputs',999),('ordinal_fifo_completed_jobs',999),('joined_fifo_inflight',1),('max_unpersisted_buffered_inputs',101),('fifo_gate_call_elapsed_seconds_sum',True),('fifo_gate_call_elapsed_seconds_sum',-1),('fifo_gate_call_elapsed_seconds_sum',float('inf')),('method',bench.DEFAULT_METHOD)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_input_scheduler'].__setitem__(field,value))
  for field in self.result['preparation_input_scheduler']:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_input_scheduler'].pop(field))
  self.reject(lambda d:d['preparation_input_scheduler'].__setitem__('unknown_scheduler',0))
 def test_actual_permutation_fifo_fairness_mailbox_mapping_and_diversity_rejected(self):
  for field,value in [('actual_emitted_ordinal_order',[0]*1000),('actual_emitted_ordinal_order',list(range(1000))),('actual_emitted_mailbox_ordinals',[0]*1000),('actual_emitted_window_unique_mailboxes',[20]*50)]:
   with self.subTest(field=field):self.reject(lambda d:d['preparation_input_scheduler'].__setitem__(field,value))
  for field in ['actual_emitted_ordinal_order','actual_emitted_mailbox_ordinals','actual_emitted_window_unique_mailboxes']:
   self.reject(lambda d:d['preparation_input_scheduler'][field].__setitem__(0,True))
   self.reject(lambda d:d['preparation_input_scheduler'][field].__setitem__(0,float(d['preparation_input_scheduler'][field][0])))
   self.reject(lambda d:d['preparation_input_scheduler'][field].pop())
 def test_actual_pg_observation_not_metadata_or_uid_equivalence_claim(self):
  for field in ['preparation_identity_boundary','preparation_persistent_observations']:
   self.reject(lambda d:d.pop(field));self.reject(lambda d:d[field].__setitem__('unknown_observation',0))
  for field in ['all_persistent_equivalent','protocol_UID_certified']:
   self.reject(lambda d:d['preparation_identity_boundary'].__setitem__(field,True))
  for field in self.result['preparation_persistent_observations']:
   self.reject(lambda d:d['preparation_persistent_observations'].pop(field))
  for field,value in [('origin','scheduler_metadata'),('source_sha','b'*40),('schema_version',15),('projected_messages',999),('projected_mailboxes',100),('ordinal_fifo_violations',1),('index_ready_count',999),('source_identity_bindings_valid',1),('uid_catalog_query_sha256','parameter-only'),('uid_column_count',-1)]:
   self.reject(lambda d:d['preparation_persistent_observations'].__setitem__(field,value))
  for field in ['schema_version','uid_column_count','projected_messages','projected_mailboxes','ordinal_fifo_violations','index_ready_count']:
   for value in [True,1.0]:self.reject(lambda d:d['preparation_persistent_observations'].__setitem__(field,value))

 def archived_selection_fixture(self):
  source='a'*40;rel='docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json'
  row={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract':Path(rel).name,'method_contract_sha256':self.digest}
  review=copy.deepcopy(row);closure={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract_sha256':self.digest,'actual_source_closure_sha256':'b'*64}
  receipt={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract':rel,'method_contract_sha256':self.digest,'source_sha':source,'source_identity_kind':bench.CANONICAL_SOURCE_KIND,'actual_source_closure_sha256':'b'*64,'argv':['run_r5_benchmark.py','--scale','S','--source-sha',source,'--output-dir','/fixture/output','--execute-approved-budget','--preparation-method',bench.LOOKAHEAD_METHOD,'--method-contract','/fixture/'+Path(rel).name],'environment':{bench.METHOD_ENV:bench.LOOKAHEAD_METHOD,bench.METHOD_SHA_ENV:self.digest,'TABMAIL_R5_BENCHMARK_SOURCE_SHA':source,'TABMAIL_R5_BENCHMARK_SCALE':'S'}}
  members={'run/method-contract.json':bench.LOOKAHEAD_METHOD_FILE.read_bytes(),'run/method-selection.json':json.dumps(receipt).encode()}
  files={rel:self.digest}
  bench.validate_archived_method_selection(row,review,closure,members,'run',files,source,'S')
  return row,review,closure,members,files,receipt
 def test_archived_selection_exact_source_method_contract_argv_environment(self):
  for target,key,value in [('row','preparation_method',bench.DEFAULT_METHOD),('review','method_contract_sha256','0'*64),('closure','method_contract_sha256','0'*64),('closure','preparation_method',bench.DEFAULT_METHOD),('row','method_contract','other.json')]:
   row,review,closure,members,files,receipt=self.archived_selection_fixture();dict(row=row,review=review,closure=closure)[target][key]=value
   with self.subTest(target=target,key=key),self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,'a'*40,'S')
  mutations=[lambda d:d.__setitem__('source_sha','c'*40),lambda d:d.__setitem__('unknown_field',True),lambda d:d.pop('environment'),lambda d:d['environment'].__setitem__(bench.METHOD_ENV,bench.DEFAULT_METHOD),lambda d:d['argv'].extend(['--preparation-method',bench.LOOKAHEAD_METHOD]),lambda d:d['argv'].remove('--execute-approved-budget'),lambda d:d['argv'].__setitem__(d['argv'].index('S'),'M'),lambda d:d['argv'].extend(['--unknown','value'])]
  for mutate in mutations:
   row,review,closure,members,files,receipt=self.archived_selection_fixture();mutate(receipt);members['run/method-selection.json']=json.dumps(receipt).encode()
   with self.subTest(receipt=receipt),self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,'a'*40,'S')
  row,review,closure,members,files,receipt=self.archived_selection_fixture();members['run/method-contract.json']+=b'\n'
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,'a'*40,'S')
 def test_pseudo_history_and_duplicate_selection_json_rejected(self):
  row,review,closure,members,files,receipt=self.archived_selection_fixture()
  with self.assertRaises(ValueError):bench.validate_archived_method_selection({}, {}, {},members,'run',files,'a'*40,'S')
  raw=json.dumps(receipt).replace('"source_sha": "'+'a'*40+'"','"source_sha": "'+'a'*40+'", "source_sha": "'+'a'*40+'"',1);members['run/method-selection.json']=raw.encode()
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,'a'*40,'S')
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,'a'*40,'M')
 def test_cli_invalid_or_missing_optin_contract_rejected_without_go(self):
  for args in [[],['--preparation-method',bench.DEFAULT_METHOD,'--method-contract',str(bench.LOOKAHEAD_METHOD_FILE)],['--preparation-method',bench.LOOKAHEAD_METHOD],['--preparation-method',bench.LOOKAHEAD_METHOD,'--method-contract',str(bench.LOOKAHEAD_METHOD_FILE),'--scale','L'],['--preparation-method',bench.DEFAULT_METHOD,'--preparation-method',bench.DEFAULT_METHOD]]:
   # Synthetic capacity admits the real preflight; empty args never dispatches
   # an ambient opt-in. Disk rejection is tested at its exact real margin.
   with self.subTest(args=args),mock.patch.object(sys,'argv',['run_r5_benchmark.py',*args]),mock.patch.object(bench,'run_owned_process') as run,mock.patch.dict(os.environ,{bench.METHOD_ENV:bench.LOOKAHEAD_METHOD}),mock.patch.object(bench.shutil,'disk_usage',return_value=shutil._ntuple_diskusage(40<<30,10<<30,30<<30)),mock.patch('sys.stdout',new_callable=io.StringIO):
    self.assertEqual(bench.main(),0 if not args else 1);run.assert_not_called()

class BenchmarkCurrentSourcePolicyTests(unittest.TestCase):
 """Current SOURCE/CLI fixtures only; no historical schema16 observation setUp.

 These gates do not run Go/PG or certify schema18/default-method runtime.
 """
 def test_current_closure_blocks_legacy_method_identity_until_explicit_migration(self):
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory).resolve();context=current_inventory_fixture(root)
   with mock.patch.object(bench,'ROOT',root):
    current=bench.source_closure(policy=bench.source_inventory.POLICY,build_context=context)
    self.assertIn('internal/api/docsassets/a.js',current)
    with self.assertRaisesRegex(ValueError,'^current v2 lookahead blocked: frozen METHOD binds legacy files-only SHA1; independent method migration required$'):
     bench.source_closure(preparation_method=bench.LOOKAHEAD_METHOD,policy=bench.source_inventory.POLICY,build_context=context)

 def test_runner_default_overrides_ambient_optin_and_selected_source_must_match(self):
  with tempfile.TemporaryDirectory() as directory:
   root=Path(directory).resolve();context=current_inventory_fixture(root)
   with mock.patch.object(bench,'ROOT',root):
    receipt=bench.current_source_receipt(policy=bench.source_inventory.POLICY,build_context=context)
   manifest=root.parent/(root.name+'-manifest.json');self.addCleanup(lambda:manifest.unlink(missing_ok=True))
   manifest.write_text(json.dumps(receipt));pin=hashlib.sha256(manifest.read_bytes()).hexdigest()
   args=['run_r5_benchmark.py','--scale','S','--execute-approved-budget','--source-sha',receipt['source_sha'],'--output-dir',str(root/'out'),'--source-policy',bench.source_inventory.POLICY,'--source-manifest',str(manifest),'--source-manifest-sha256',pin]
   def dispatch(command,**kw):
    self.assertEqual(kw['env'][bench.METHOD_ENV],bench.DEFAULT_METHOD);self.assertNotIn(bench.METHOD_SHA_ENV,kw['env'])
    self.assertEqual(kw['env']['GOFLAGS'],'');self.assertEqual(kw['env']['GOWORK'],'off')
    actual=bench.read_json(root/'out/source-closure.json')
    self.assertEqual(actual['source_identity_kind'],bench.source_inventory.KIND)
    self.assertEqual(actual['source_sha'],receipt['source_sha'])
    raise ValueError('unit dispatch boundary only; no Go executed')
   # Synthetic host capacity lets this dispatch grammar reach its boundary
   # even on small /tmp mounts. The real preflight still enforces budgets.
   # platform.platform() may probe a subprocess on Python 3.12.
   with mock.patch.object(sys,'argv',args),mock.patch.object(bench,'ROOT',root),mock.patch.dict(os.environ,{'TABMAIL_TEST_DB_DSN':'unit-fixture-not-credential',bench.METHOD_ENV:bench.LOOKAHEAD_METHOD,bench.METHOD_SHA_ENV:'ambient'},clear=True),mock.patch.object(bench.platform,'platform',return_value='synthetic-current-source-host-metadata') as host_platform,mock.patch.object(bench.shutil,'disk_usage',return_value=shutil._ntuple_diskusage(40<<30,10<<30,30<<30)),mock.patch.object(bench,'observed_git_head',return_value=None),mock.patch.object(bench,'run_owned_process',side_effect=dispatch) as run,mock.patch('sys.stdout',new_callable=io.StringIO) as output:
    self.assertEqual(bench.main(),1);self.assertEqual(run.call_count,1)
    self.assertEqual(json.loads(output.getvalue().splitlines()[-1])['error'],'unit dispatch boundary only; no Go executed')
    host_platform.assert_called_once_with()
    self.assertEqual(bench.read_json(root/'out/preflight.json')['hardware']['platform'],'synthetic-current-source-host-metadata')
    args[args.index('--source-sha')+1]='b'*40
    self.assertEqual(bench.main(),1);self.assertEqual(run.call_count,1)
    self.assertEqual(json.loads(output.getvalue().splitlines()[-1])['error'],'current source label must bind policy/context/full local input superset')
    host_platform.assert_called_once_with()
   # No-policy dispatch is never reinterpreted as a frozen source identity.
   args=args[:args.index('--source-policy')]
   with mock.patch.object(sys,'argv',args),mock.patch.object(bench,'run_owned_process') as run,mock.patch('sys.stdout',new_callable=io.StringIO) as output:
    self.assertEqual(bench.main(),1);run.assert_not_called()
    self.assertEqual(json.loads(output.getvalue().splitlines()[-1])['error'],'current tool/execution requires explicit v2 source policy and pinned manifest; no legacy fallback')

class BenchmarkLookaheadMAdmissionTests(unittest.TestCase):
 """New M scope/CLI/archived-contract unit targets, no DB/runtime qualification."""
 def test_current_M_allowed_exact_budget_population_and_L_rejected(self):
  contract=bench.read_json(bench.DATASET);method=bench.read_json(bench.LOOKAHEAD_METHOD_FILE)
  bench.validate_method_contract(method)
  self.assertEqual(method['authorized_original_scales'],['S','M'])
  with mock.patch.object(bench.shutil,'disk_usage',return_value=type('Space',(),{'free':1000*(1<<30)})()):
   report=bench.preflight(contract,'M',Path('/tmp'),preparation_method=bench.LOOKAHEAD_METHOD)
   self.assertEqual(report['population'],(1000,5000,1000000));self.assertEqual(report['budget_seconds'],10800);self.assertEqual(report['disk_budget_bytes'],80*(1<<30))
   with self.assertRaises(ValueError):bench.preflight(contract,'L',Path('/tmp'),preparation_method=bench.LOOKAHEAD_METHOD)
  self.assertEqual(method['preparation_contract'],{**bench.PREPARATION_CONTRACT,'method':bench.LOOKAHEAD_METHOD})
  self.assertEqual(method['clock_contract'],bench.CLOCK_CONTRACT)
  self.assertEqual(method['dataset_contract_sha256'],bench.LOOKAHEAD_DATASET_SHA256)
 def test_historical_S_contract_digest_bound_not_current_or_schema_exemption(self):
  frozen=copy.deepcopy(bench.FROZEN_LOOKAHEAD_S_CONTRACT)
  raw=(json.dumps(frozen,sort_keys=True,indent=2)+'\n').encode()
  self.assertEqual(hashlib.sha256(raw).hexdigest(),bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256)
  bench.validate_method_contract(frozen,frozen_contract_digest=bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256)
  for data,digest in [(frozen,None),(frozen,'0'*64),(bench.LOOKAHEAD_CONTRACT,bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256)]:
   with self.assertRaises(ValueError):bench.validate_method_contract(data,frozen_contract_digest=digest)
  for mutate in [lambda d:d.__setitem__('authorized_original_scales',['S','M']),lambda d:d['preparation_contract'].__setitem__('schema_version',16.0),lambda d:d['preparation_contract'].__setitem__('window_messages',21),lambda d:d.__setitem__('unknown_history_alias',True)]:
   data=copy.deepcopy(frozen);mutate(data)
   with self.assertRaises(ValueError):bench.validate_method_contract(data,frozen_contract_digest=bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256)
 def test_archive_old_S_admitted_from_exact_frozen_bytes_not_current_METHOD(self):
  rel='docs/company-mail/evidence/R5-BENCHMARK-LOOKAHEAD100-METHOD.json';digest=bench.FROZEN_LOOKAHEAD_S_METHOD_SHA256;source='a'*40
  row={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract':Path(rel).name,'method_contract_sha256':digest};review=copy.deepcopy(row)
  closure={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract_sha256':digest,'actual_source_closure_sha256':'b'*64}
  receipt={'preparation_method':bench.LOOKAHEAD_METHOD,'method_contract':rel,'method_contract_sha256':digest,'source_sha':source,'source_identity_kind':bench.CANONICAL_SOURCE_KIND,'actual_source_closure_sha256':'b'*64,'argv':['run_r5_benchmark.py','--scale','S','--source-sha',source,'--output-dir','/fixture/output','--execute-approved-budget','--preparation-method',bench.LOOKAHEAD_METHOD,'--method-contract','/fixture/'+Path(rel).name],'environment':{bench.METHOD_ENV:bench.LOOKAHEAD_METHOD,bench.METHOD_SHA_ENV:digest,'TABMAIL_R5_BENCHMARK_SOURCE_SHA':source,'TABMAIL_R5_BENCHMARK_SCALE':'S'}}
  members={'run/method-contract.json':(json.dumps(bench.FROZEN_LOOKAHEAD_S_CONTRACT,sort_keys=True,indent=2)+'\n').encode(),'run/method-selection.json':json.dumps(receipt).encode()};files={rel:digest}
  selected,frozen,got=bench.validate_archived_method_selection(row,review,closure,members,'run',files,source,'S')
  self.assertEqual(frozen['authorized_original_scales'],['S']);self.assertEqual(got,digest)
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,source,'M')
  members['run/method-contract.json']=bench.LOOKAHEAD_METHOD_FILE.read_bytes()
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,source,'S')
  # Old closure cannot be retrofitted to a new contract even if metadata changes.
  newdigest=hashlib.sha256(members['run/method-contract.json']).hexdigest();row['method_contract_sha256']=newdigest;review['method_contract_sha256']=newdigest;closure['method_contract_sha256']=newdigest
  with self.assertRaises(ValueError):bench.validate_archived_method_selection(row,review,closure,members,'run',files,source,'S')
 def test_current_M_legacy_method_requires_policy_migration_without_dispatch_or_L(self):
  with tempfile.TemporaryDirectory() as root:
   argv=['run_r5_benchmark.py','--scale','M','--execute-approved-budget','--source-sha','a'*40,'--output-dir',str(Path(root)/'new-M'),'--preparation-method',bench.LOOKAHEAD_METHOD,'--method-contract',str(bench.LOOKAHEAD_METHOD_FILE),'--source-policy',bench.source_inventory.POLICY,'--source-manifest',str(Path(root)/'absent.json'),'--source-manifest-sha256','b'*64]
   with mock.patch.object(sys,'argv',argv),mock.patch.object(bench,'run_owned_process') as run,mock.patch('sys.stdout',new_callable=io.StringIO) as output:
    self.assertEqual(bench.main(),1);run.assert_not_called();self.assertIn('METHOD',output.getvalue())
    argv[argv.index('--scale')+1]='L';self.assertEqual(bench.main(),1);run.assert_not_called()

if __name__=='__main__':unittest.main()
