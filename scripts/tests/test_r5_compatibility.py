import copy
import importlib.util
import json
from pathlib import Path
from unittest import mock
import unittest

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location('compatibility',ROOT/'scripts/check_r5_compatibility.py')
gate=importlib.util.module_from_spec(spec);spec.loader.exec_module(gate)

class CompatibilityGateTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data=json.loads(gate.MAP.read_text())
        # Unit inputs are syntax facts, never canned HTTP or product outputs.
        cls.routes,cls.clients=gate.collect()

    def test_current_source_map_is_not_product_or_dependency_completion(self):
        result=gate.validate(self.data,self.routes,self.clients)
        self.assertEqual(result['routes'],132)
        self.assertEqual(result['client_branches'],134)
        self.assertEqual(sum(x['forwarding'] for x in self.clients),7)
        self.assertEqual(result['wire_validation_scope'],'not_checked_current_wire_required')
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

    def test_dns_exclusions_match_real_HTTP_methods_not_create(self):
        facts={r['route']:r for r in gate.source_facts(self.routes,self.clients)}
        for route in ['GET /api/v1/company/domains/{id}/verification','POST /api/v1/company/domains/{id}/verify']:
            self.assertEqual(facts[route]['runtime_test_registration'],'live_DNS_excluded_from_company_HTTP_fixture')
        self.assertEqual(facts['POST /api/v1/company/domains']['runtime_test_registration'],'declared_company_http_fixture')
        self.assertNotEqual(facts['GET /api/v1/company/domains']['runtime_test_registration'],'live_DNS_excluded_from_company_HTTP_fixture')

    def test_duplicate_and_nonfinite_json_rejected(self):
        for raw in ['{"schema_version":1,"schema_version":1}','{"value":NaN}','{"value":Infinity}']:
            with self.subTest(raw=raw),self.assertRaises(ValueError):gate.strict_json(raw)



class SafeWireJoinTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        cls.data=json.loads((ROOT/gate.HISTORY).read_text())
        cls.historical_root=ROOT/gate.HISTORY.parent/'historical-source'
        cls.facts=[{'route':r['route']} for r in cls.data['routes']]

    def validate(self,data):
        # Independent historical source bytes, never patch a digest or current validator.
        with mock.patch.object(gate,'ROOT',self.historical_root):
            return gate.validate_wire(data,self.facts)
    def reject(self,modify,repin=False):
        data=copy.deepcopy(self.data);modify(data)
        if repin:data['wire_source_evidence']['summary_sha256']=__import__('hashlib').sha256(gate.canonical_bytes(data['wire_source_evidence']['summary'])).hexdigest()
        with self.assertRaises((ValueError,KeyError,TypeError)):self.validate(data)

    def test_exact_safe_join_remains_metadata_not_product_approval(self):
        result=self.validate(self.data)
        self.assertEqual((result['wire_observations'],result['wire_observed_routes'],result['wire_explicit_not_observed_routes']),(193,75,52))
        self.assertEqual(result['wire_validation_scope'],'embedded_safe_metadata_and_pinned_hashes_only')
        self.assertFalse(self.data['product_green']);self.assertFalse(self.data['task_complete'])

    def test_wrong_tree_report_spec_case_and_closure_hashes_rejected(self):
        for key in ['frozen_tree','validation_commit']:
            with self.subTest(key=key):self.reject(lambda d:d['wire_source_evidence']['summary']['source_identity'].__setitem__(key,'0'*40),True)
        self.reject(lambda d:d['wire_source_evidence']['summary'].__setitem__('source_closure_sha256','0'*64),True)
        self.reject(lambda d:d['wire_source_evidence'].__setitem__('summary_sha256','0'*64))
        for field,length in [('source_sha',40),('spec_sha256',64)]:
            with self.subTest(field=field):self.reject(lambda d:d['wire_source_evidence']['summary']['inputs'][0].__setitem__(field,'0'*length),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['inputs'][1].__setitem__('cases_sha256','0'*64),True)
        self.reject(lambda d:d['wire_source_evidence'].__setitem__('expected_cases_sha256','0'*64))
        self.reject(lambda d:d['wire_source_evidence'].__setitem__('expected_spec_sha256','0'*64))

    def test_unknown_private_fields_bad_status_and_aspects_rejected(self):
        for field in ['body','payload','token','headers','private_fixture','product_green']:
            with self.subTest(field=field):self.reject(lambda d:d['wire_source_evidence']['summary']['observations'][0].__setitem__(field,'private-or-unsupported'),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['inputs'][0].__setitem__('authorization','private'),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['observations'][0].__setitem__('route','GET /api/v1/not-real'),True)
        for status in [None,True,0,199,600,'200']:
            with self.subTest(status=status):self.reject(lambda d:d['wire_source_evidence']['summary']['observations'][0].__setitem__('actual_status',status),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['observations'][0].__setitem__('aspect','all_policy_pass'),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['observations'].append(d['wire_source_evidence']['summary']['observations'][0]),True)

    def test_not_observed_cannot_be_laundered_as_runtime_pass(self):
        unobserved=next(i for i,r in enumerate(self.data['routes']) if r['wire_evidence']=='explicit_not_observed')
        self.reject(lambda d:d['routes'][unobserved].__setitem__('wire_evidence','observed_metadata_not_complete_behavior'))
        self.reject(lambda d:d['routes'][unobserved].__setitem__('wire_observations',[d['wire_source_evidence']['summary']['observations'][0]]))
        self.reject(lambda d:d['wire_source_evidence']['summary']['excluded_http_operations'][0].__setitem__('method','POST'),True)
        self.reject(lambda d:d['wire_source_evidence']['summary']['excluded_http_operations'][0].__setitem__('path','/api/v1/company/domains'),True)
        with __import__('tempfile').TemporaryDirectory() as root:
            with self.assertRaises(ValueError):gate.validate_wire(self.data,self.facts,root)


class CurrentHistoricalBoundaryTests(unittest.TestCase):
    setUpClass=classmethod(CompatibilityGateTests.setUpClass.__func__)
    reject=CompatibilityGateTests.reject
    def test_current_source_rejects_historical_wire(self):
        historical=json.loads((ROOT/gate.HISTORY).read_text())
        with self.assertRaisesRegex(ValueError,'wire spec/case differs from current source'):
            gate.validate_wire(historical,[{'route':r['route']} for r in historical['routes']])

    def test_re_signing_history_and_current_artifact_claims_rejected(self):
        self.reject(lambda d:d.__setitem__('wire_source_evidence',json.loads((ROOT/gate.HISTORY).read_text())['wire_source_evidence']))
        self.reject(lambda d:d['historical_wire_reference'].__setitem__('sha256','0'*64))
        self.reject(lambda d:d['routes'][0].__setitem__('wire_evidence','observed_metadata_not_complete_behavior'))
        with self.assertRaises(ValueError):gate.validate(self.data,self.routes,self.clients,ROOT)

    def test_future_and_wrong_handler_producer_routes_rejected(self):
        for field,value in [('path','/api/v1/future'),('handler','invented.Handler'),('middleware',[]),('source','internal/api/router.go')]:
            routes=copy.deepcopy(self.routes)
            # A different allowed source must also be rejected, not only path traversal.
            if field=='source':value='internal/api/handlers/company_routes.go'
            routes[0][field]=value
            with self.subTest(field=field),self.assertRaises(ValueError):gate.validate(self.data,routes,self.clients)

    def test_old_runtime_cannot_be_re_signed_with_current_spec(self):
        historical=json.loads((ROOT/gate.HISTORY).read_text())
        evidence=historical['wire_source_evidence'];current_spec=gate.digest(ROOT/'internal/api/openapi.yaml')
        evidence['expected_spec_sha256']=current_spec
        for ref in evidence['summary']['inputs']:
            if ref['kind']=='http_contract':ref['spec_sha256']=current_spec
        evidence['summary_sha256']=__import__('hashlib').sha256(gate.canonical_bytes(evidence['summary'])).hexdigest()
        with self.assertRaisesRegex(ValueError,'re-signed historical wire'):
            gate.validate_wire(historical,[{'route':r['route']} for r in historical['routes']])

    def test_fabricated_client_producer_not_accepted_as_source(self):
        clients=copy.deepcopy(self.clients);clients[0]['owner']='inventedClient'
        with self.assertRaisesRegex(ValueError,'client producer differs'):
            gate.validate(self.data,self.routes,clients)

    def test_new_routes_preserve_release_semantics(self):
        rows={r['route']:r for r in self.data['routes']}
        for route in ['GET /api/v1/admin/users/{id}/permission-editor','PATCH /api/v1/admin/users/{id}/permission-editor','POST /api/v1/admin/users/{id}/permission-editor/assignment','GET /api/v1/admin/permissions/{id}/deletion-preview']:
            self.assertEqual(rows[route]['release_batch'],'P1')
        self.assertEqual(rows['GET /api/v1/company/events']['release_batch'],'P7')

if __name__=='__main__':unittest.main()
