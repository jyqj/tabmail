import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import unittest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('compatibility',ROOT/'scripts/check_r5_compatibility.py')
gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)

class CompatibilityGateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data=json.loads(gate.MAP.read_text())
        # Unit inputs are syntax facts, never canned HTTP or product outputs.
        cls.routes=json.loads((ROOT/'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
        cls.clients=json.loads(subprocess.run(['node','scripts/collect_api_calls.cjs'],cwd=ROOT,check=True,capture_output=True,text=True).stdout)

    def test_current_source_map_is_not_product_or_dependency_completion(self):
        result=gate.validate(self.data,self.routes,self.clients)
        self.assertEqual(result['routes'],127)
        self.assertFalse(result['task_complete']);self.assertFalse(result['product_green'])
        self.assertEqual(result['status'],'source_inventory_and_upgrade_plan_checked')
        self.assertTrue(result['openapi_missing'])

    def reject(self,change):
        data=copy.deepcopy(self.data);change(data)
        with self.assertRaises((ValueError,KeyError,TypeError)):gate.validate(data,self.routes,self.clients)

    def test_route_schema_client_and_test_fact_mutations_rejected(self):
        for key in ['handler','route','route_source','schema_components','request_schema_refs','request_contract','response_schema_refs','go_ts_dto_bindings','test_bindings','clients','middleware','conditions','api_key_authority','coverage_limits','release_batch']:
            with self.subTest(key=key):self.reject(lambda d:d['routes'][0].__setitem__(key,'invented'))

    def test_missing_duplicate_and_source_drift_rejected(self):
        self.reject(lambda d:d['routes'].pop())
        self.reject(lambda d:d['routes'].append(d['routes'][0]))
        self.reject(lambda d:d['source_closure'].__setitem__('internal/api/router.go','0'*64))
        self.reject(lambda d:d['source_closure'].pop('internal/api/openapi.yaml'))

    def test_future_rejection_upgrade_and_task_strategies_mandatory(self):
        for field in ['old_write_rejection','coordinated_upgrade','current_implementation','successor_tasks']:
            with self.subTest(field=field):self.reject(lambda d:d['release_batches']['P1'].__setitem__(field,''))
        self.reject(lambda d:d['release_batches']['P1'].__setitem__('successor_tasks','not a task'))
        self.reject(lambda d:d['release_batches']['P1'].__setitem__('successor_tasks','R5-P99-999'))
        self.reject(lambda d:d['release_batches']['P1'].__setitem__('implemented_by_this_map',True))
        self.reject(lambda d:d['release_batches'].pop('P2'))
        self.reject(lambda d:d['release_batches'].__setitem__('future',d['release_batches']['P1']))

    def test_cannot_waive_dependency_runtime_or_legacy_client_unknowns(self):
        self.reject(lambda d:d.__setitem__('dependency_acceptance','completed'))
        self.reject(lambda d:d['dependencies'].remove('R5-P0-080'))
        self.reject(lambda d:d.__setitem__('task_complete',True))
        self.reject(lambda d:d.__setitem__('product_green',True))
        self.reject(lambda d:d.__setitem__('scope','runtime_pass'))
        self.reject(lambda d:d['routes'][0].__setitem__('runtime_evidence_scope','all passed'))
        self.reject(lambda d:d['routes'][0].__setitem__('legacy_client_disposition','no old clients'))

    def test_malformed_and_untrusted_producer_sources_rejected(self):
        for routes,clients in [([],self.clients),(self.routes,[]),([*self.routes,self.routes[0]],self.clients)]:
            with self.subTest(routes=len(routes),clients=len(clients)),self.assertRaises(ValueError):gate.validate(self.data,routes,clients)
        routes=copy.deepcopy(self.routes);routes[0]['source']='.env'
        with self.assertRaises(ValueError):gate.validate(self.data,routes,self.clients)
        clients=copy.deepcopy(self.clients);clients[0]['source']='web/../../.env'
        with self.assertRaises(ValueError):gate.validate(self.data,self.routes,clients)

    def test_routes_and_successor_tasks_match_original_stage_semantics(self):
        expected={
            '/api/v1/company/templates/{id}':'P5',
            '/api/v1/company/templates/{id}/grants':'P5',
            '/api/v1/company/drafts/{id}/submit':'P5',
            '/api/v1/company/recovery/{id}/retry':'P6',
            '/api/v1/company/outbound/{id}/reconcile':'P6',
            '/api/v1/outbound/{id}/retry':'P6',
            '/api/v1/company/mailboxes/{id}/events':'P7',
            '/api/v1/company/index/retry':'P7',
        }
        for path,batch in expected.items():
            with self.subTest(path=path):self.assertEqual(gate.release_batch({'path':path}),batch)
        for batch,wrong_tasks in [('P5','R5-P6-030/050/080/150'),('P6','R5-P7-040/060/090/120'),('P7','R5-P5-080/090/100/110')]:
            with self.subTest(batch=batch):self.reject(lambda d:d['release_batches'][batch].__setitem__('successor_tasks',wrong_tasks))

    def test_duplicate_and_nonfinite_json_rejected(self):
        for raw in ['{"schema_version":1,"schema_version":1}','{"value":NaN}','{"value":Infinity}']:
            with self.subTest(raw=raw),self.assertRaises(ValueError):gate.strict_json(raw)

if __name__=='__main__':unittest.main()
