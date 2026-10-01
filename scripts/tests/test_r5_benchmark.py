import copy
import hashlib
import importlib.util
from pathlib import Path
import tempfile
import unittest

spec=importlib.util.spec_from_file_location('benchmark',Path(__file__).resolve().parents[1]/'run_r5_benchmark.py')
bench=importlib.util.module_from_spec(spec);spec.loader.exec_module(bench)

class BenchmarkContractTests(unittest.TestCase):
 def setUp(self):self.data=bench.read_json(bench.DATASET)
 def reject(self,modify):
  data=copy.deepcopy(self.data);modify(data)
  with self.assertRaises(ValueError):bench.validate_contract(data)
 def test_original_scales_and_no_runtime_registry(self):
  bench.validate_contract(self.data)
  self.assertEqual(bench.SCALES['S'],(100,500,100000));self.assertEqual(bench.SCALES['M'],(1000,5000,1000000));self.assertEqual(bench.SCALES['L'][2],10000000)
  registry=bench.read_json(bench.ROOT/'docs/company-mail/evidence/R5-BENCHMARK-RUNS.json');self.assertEqual(registry['runs'],[]);self.assertFalse(registry['S_executed']);self.assertFalse(registry['M_executed'])
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
 def test_duplicate_json_rejected(self):
  with tempfile.TemporaryDirectory() as root:
   p=Path(root)/'bad.json';p.write_text('{"seed":1,"seed":2}')
   with self.assertRaises(ValueError):bench.read_json(p)

 def test_nonfinite_json_rejected(self):
  with tempfile.TemporaryDirectory() as root:
   p=Path(root)/'bad.json'
   for value in ['NaN','Infinity','-Infinity','1e999']:
    p.write_text('{"metric":'+value+'}')
    with self.subTest(value=value),self.assertRaises(ValueError):bench.read_json(p)

 def test_integer_contract_fields_reject_float_and_boolean_aliases(self):
  for field in ['schema_version','seed','concurrency']:
   for value in [float(self.data[field]), True]:
    with self.subTest(field=field,value=value):self.reject(lambda d:d.__setitem__(field,value))
  for scale in bench.SCALES:
   for field in ['employees','mailboxes','messages']:
    with self.subTest(scale=scale,field=field):self.reject(lambda d:d['scales'][scale].__setitem__(field,float(d['scales'][scale][field])))
  self.reject(lambda d:d['message_sizes'][0].__setitem__('bytes',4096.0))
  self.reject(lambda d:d['population'].__setitem__('personal_per_employee',True))
  self.reject(lambda d:d['candidate_thresholds'].__setitem__('S_list_p95_ms',300.0))

class BenchmarkEvidenceTests(unittest.TestCase):
 def setUp(self):
  self.contract=bench.read_json(bench.DATASET)
  # Pure validator data, NOT simulated product HTTP/DB results or S evidence.
  self.result={'scale':'S','employees':100,'mailboxes':500,'messages':100000,'seed':3893945,'concurrency':20,'source_sha':'a'*40,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'index_ready_count':100000,'sql_tracer_calibration_count':20,'sql_tracer_calibration_expected':20,'pool_size':24,'hardware':{'os':'darwin','arch':'arm64','cpu':16},'schema_version':15,'dataset_fingerprint':'validator-only','safety_assertions':[w+':foreign_and_revoked_current_source_denied' for w in bench.WORKLOADS],'safety_assertions_passed':True,'task_complete':False,'product_green':False,'measurements':[{'workload':w,'cache_mode':c,'samples':200,'sql_count':201,'p50_ms':1,'p95_ms':2,'p99_ms':3,'alloc_bytes':4,'rss_bytes':5,'disk_bytes':6} for w in bench.WORKLOADS for c in self.contract['cache_modes']]}
  self.result.update(self.observations(100,100000,'S'))
 @staticmethod
 def observations(employees,messages,scale):
  return {'actual_distribution':{'employees':employees,'mailboxes':employees*5,'messages':messages,'index_ready_count':messages,'personal_mailboxes':employees,'shared_mailboxes':employees*4,'shared_grant_rows':employees*16,'min_distinct_shared_grantees':4,'max_distinct_shared_grantees':4,'lifecycle_counts':{'inbox':messages*80//100,'archived':messages*10//100,'trash_expired':messages*5//100,'hard_expired':messages*5//100},'mime_size_counts':{'4096':messages*800//1000,'32768':messages*180//1000,'262144':messages*19//1000,'2097152':messages//1000},'tenant_message_counts':{'bench-0.test':messages*80//100,'bench-1.test':messages*20//100}},'dataset_fingerprint':'1'*64,'parameter_fingerprint':'2'*64,'resource_scope':copy.deepcopy(bench.RESOURCE_SCOPE),'system_memory_observation':'not_measured','sql_plans':[{'workload':'inbox_list','origin':'captured_shipping_query_same_parameters','scale':scale,'source_sha':'a'*40,'schema_version':15,'index_ready_count':messages,'query_sha256':'3'*64,'parameters_sha256':'4'*64,'plan':[{'Plan':{'Node Type':'Seq Scan'}}]}]}
 def reject(self,modify):
  data=copy.deepcopy(self.result);modify(data)
  with self.assertRaises(ValueError):bench.validate_result(data,self.contract,'S','a'*40)
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
 def test_actual_plan_metadata_and_resource_scope_rejected(self):
  for field,value in [('origin','handwritten_SQL'),('scale','M'),('source_sha','b'*40),('schema_version',14),('index_ready_count',0),('query_sha256',''),('parameters_sha256',''),('plan',[])]:
   with self.subTest(field=field):self.reject(lambda d:d['sql_plans'][0].__setitem__(field,value))
  self.reject(lambda d:d.__setitem__('sql_plans',[]))
  self.reject(lambda d:d['resource_scope'].__setitem__('sql','all_server_SQL'))
  self.reject(lambda d:d.__setitem__('system_memory_observation','all_system_measured'))
 def test_tool_only_shape_never_certifies_original_scale(self):
  result={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'seed':3893945,'rss_bytes':123,'disk_bytes':456,'source_sha':'a'*40,'schema_version':15,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'task_complete':False,'product_green':False,'S_M_L_executed':False,**self.observations(20,1000,'tool_only')}
  observed=bench.validate_tool_result(result,self.contract,'a'*40)
  self.assertFalse(observed['S_M_L_executed'])
  with self.assertRaises(ValueError):bench.validate_result(result,self.contract,'S','a'*40)
  for field,value in [('S_M_L_executed',True),('messages',100),('source_sha','b'*40),('task_complete',True)]:
   altered=copy.deepcopy(result);altered[field]=value
   with self.subTest(field=field),self.assertRaises(ValueError):bench.validate_tool_result(altered,self.contract,'a'*40)
 def test_validated_shape_still_not_task_or_product_approval(self):
  result=bench.validate_result(self.result,self.contract,'S','a'*40)
  self.assertFalse(result['task_complete']);self.assertFalse(result['product_green'])

 def test_integer_result_fields_reject_float_and_boolean_aliases(self):
  for field in ['employees','mailboxes','messages','seed','concurrency','index_ready_count','sql_tracer_calibration_count','sql_tracer_calibration_expected','pool_size']:
   with self.subTest(field=field):self.reject(lambda d:d.__setitem__(field,float(d[field])))
  for value in [1,'true',[True],{'passed':True}]:
   with self.subTest(value=value):self.reject(lambda d:d.__setitem__('safety_assertions_passed',value))

 def test_tool_calibration_requires_strict_seed_and_positive_resources(self):
  result={'mode':'TOOL_ONLY_DATASET_CALIBRATION','scale':'tool_only','employees':20,'mailboxes':100,'messages':1000,'seed':3893945,'rss_bytes':123,'disk_bytes':456,'source_sha':'a'*40,'schema_version':15,'dataset_sha256':hashlib.sha256(bench.DATASET.read_bytes()).hexdigest(),'task_complete':False,'product_green':False,'S_M_L_executed':False,**self.observations(20,1000,'tool_only')}
  bench.validate_tool_result(result,self.contract,'a'*40)
  for field,values in [('seed',[None,1,3893945.0,True]),('rss_bytes',[None,0,-1,1.0,True]),('disk_bytes',[None,0,-1,1.0,True]),('employees',[20.0]),('mailboxes',[100.0]),('messages',[1000.0])]:
   for value in values:
    altered=copy.deepcopy(result);altered[field]=value
    with self.subTest(field=field,value=value),self.assertRaises(ValueError):bench.validate_tool_result(altered,self.contract,'a'*40)

if __name__=='__main__':unittest.main()
