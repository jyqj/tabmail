"""Pure current18 contract grammar fixtures, never PG/Go/runtime qualification.

Every admission/measurement/hash below is synthetic. A positive grammar test
is not an approved attempt, an executed workload or a catalog/build proof.
"""
import copy
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
from pathlib import Path
import unittest
from unittest import mock

SCRIPTS=Path(__file__).resolve().parents[1]
spec=importlib.util.spec_from_file_location('current18_evidence',SCRIPTS/'r5_current_benchmark_evidence.py')
evidence=importlib.util.module_from_spec(spec);spec.loader.exec_module(evidence)


def h(label):return hashlib.sha256(label.encode()).hexdigest()


def source_fixture(contract_raw,method):
    names={'go.mod','go.sum','Dockerfile','internal/api/router.go','internal/api/openapi.yaml','scripts/r5_source_inventory.py','scripts/r5_current_benchmark_evidence.py','scripts/tests/test_r5_current_benchmark.py','scripts/contracts/r5-benchmark-current18-evidence.schema.json','third_party/enmime-v2.3.0/go.mod','third_party/enmime-v2.3.0/go.sum','third_party/enmime-v2.3.0/part.go','third_party/enmime-v2.3.0/parse_budget.go','third_party/enmime-v2.3.0/PROVENANCE.json','third_party/enmime-v2.3.0/UPSTREAM-MANIFEST.json','third_party/enmime-v2.3.0/UPSTREAM-DELTA.patch','third_party/enmime-v2.3.0/LICENSE','internal/api/docsassets/a.js','internal/api/docsassets/a.css'}
    files={name:h('synthetic '+name) for name in names}
    for key,name in [('method',evidence.METHOD_FILES[method]),('workload',evidence.WORKLOAD_FILE),('catalog',evidence.CATALOG_FILE)]:files['scripts/contracts/'+name]=evidence.sha(contract_raw[key])
    payload={'policy':evidence.POLICY,'purpose':'benchmark','build_context':{'goos':'linux','goarch':'amd64','cgo_enabled':0,'build_tag_sets':[['r5benchmark']],'race':False,'go_work':'off','go_flags':'','selection':'all_local_variants_superset'},'replacement':{'module':'github.com/jhillyerd/enmime/v2','version':'v2.3.0','path':'third_party/enmime-v2.3.0'},'boundary':'synthetic source grammar fixture; not actual source or external build attestation','excluded_directories':['.git','node_modules'],'embed_inputs':{'internal/api/router.go:docsassets/*.js':['internal/api/docsassets/a.js'],'internal/api/router.go:docsassets/*.css':['internal/api/docsassets/a.css']},'files':files}
    wire=evidence.canonical(payload)
    return {'schema_version':2,'snapshot_root':'/synthetic/source-only','source_identity_kind':evidence.SOURCE_KIND,'source_sha':hashlib.sha1(wire).hexdigest(),'source_closure_sha256':evidence.sha(wire),**payload}


def synthetic_contracts(method):
    """In-memory complete-shaped toy catalog. Never writes/upgrades actual refs."""
    values,raw=evidence.load_contracts(method)
    values=copy.deepcopy(values);catalog=values['catalog']
    nullable={'default','collation','referenced_table','fk_update_action','fk_delete_action','fk_match_type','owner_table','owner_column','ownership_dependency_kind'}
    booleans={'rls','forced_rls','validated','deferrable','initially_deferred','valid','ready','cycle','security_definer','strict','leakproof'}
    integers={'position','typmod','start','min','max','increment','cache','type_bits'}
    lists={'columns','referenced_columns','configuration'}
    rows={}
    for collection,fields in evidence.FULL_CATALOG_FIELDS.items():
        row={field:(False if field in booleans else -1 if field=='typmod' else 1 if field in integers else [] if field in lists else None if field in nullable else 'synthetic_'+field) for field in fields}
        rows[collection]=[row]
    rows['functions'].append(dict(rows['functions'][0],name='synthetic_ordinary_SQL_function',return_type='integer'))
    catalog.update(reference_kind='synthetic_complete_fixture_not_runtime_reference',reference_catalog=rows,reference_collection_scope=sorted(rows),reference_catalog_sha256=evidence.sha(evidence.canonical(rows)),reference_raw_sha256=evidence.sha(evidence.canonical(rows)),reference_catalog_sha256_scope='synthetic_fixture_bytes_only_NOT_a_PG_capture',reference_shape_complete=True,runtime_reference_ready=False,missing_coverage={},current_full_catalog_qualification_status='synthetic_grammar_fixture_NOT_runtime_qualification',informational_counts={key:len(value) for key,value in rows.items()})
    raw=dict(raw,catalog=evidence.canonical(catalog))
    values['method']['catalog_contract']['sha256']=evidence.sha(raw['catalog'])
    values['method']['runtime_qualification_status']='synthetic_fixture_NOT_runtime_qualification'
    raw['method']=evidence.canonical(values['method'])
    return values,raw


def prep_stats(messages,phase,depth,clock,method,claim_query):
    terminal=phase=='terminal';joined=phase=='batch_joined'
    written=messages if terminal else depth+20;completed=messages if terminal else depth+20 if joined else depth
    calls=messages//20+1 if terminal else completed//20
    store=written*.00005;claim=calls*.001;parse=completed*.00005
    remaining=clock['effective_deadline_unix_ns']-clock['origin_unix_ns']-3*evidence.NS-completed*1_000_000
    return {'method':method,'phase':phase,'joined_inflight':0,'parser_join_policy':evidence.PARSER_JOIN_POLICY,'parser_join_reserve_seconds':31,'parser_joined_inflight':0,'parser_joined_calls':completed,'parser_canceled_joined_calls':0,'parser_joined_call_wall_seconds_sum':parse*2,'parser_quiescent':True,'parser_min_start_remaining_ns':remaining,'parser_deadline_unix_ns':clock['effective_deadline_unix_ns'],'active_budget':20,'pending_upper_bound':20,'max_active':20,'max_window_messages':20,'joined_stored_messages':written,'joined_completed_jobs':completed,'claim_calls':calls,'max_claim_batch':20,'last_claim_batch':0 if terminal else 20,'joined_ready_before_last_claim':messages if terminal else depth if joined else depth-20,'last_claim_wall_seconds':.001,'producer_done':terminal,'final_empty_claim':terminal,'sql_ready_count':messages if terminal else 0,'company_wall_seconds':.1,'store_wall_seconds':store,'claim_wall_seconds':claim,'parse_complete_wall_seconds':parse,'pipeline_wall_seconds':store+claim+parse+.01,'max_wall_monotonic_gap_ms':0.0,'claim_query_sha256':claim_query,'claim_parameters_sha256':evidence.sha(b'[20]')}


def fixture(method=evidence.BOUNDED,*,synthetic=True):
    contracts,raw=synthetic_contracts(method) if synthetic else evidence.load_contracts(method);source=source_fixture(raw,method);source_raw=evidence.canonical(source)
    binding={'evidence_generation':evidence.GENERATION,'attempt_id':'1'*32,'scale':'tool_only','source_sha':source['source_sha'],'source_policy':evidence.POLICY,'source_identity_kind':evidence.SOURCE_KIND,'source_manifest_sha256':evidence.sha(source_raw),'method_id':method,'method_contract_sha256':evidence.sha(raw['method']),'workload_contract_sha256':evidence.sha(raw['workload']),'catalog_contract_sha256':evidence.sha(raw['catalog'])}
    origin=1_800_000_000*evidence.NS;deadline=origin+300*evidence.NS
    admission={**binding,'artifact_kind':'r5_current18_external_admission_v1','admission_schema_version':1,'not_before_unix_ns':origin,'not_after_unix_ns':deadline,'approved_deadlines':{'outer_owner':deadline},'approvals':{role:h('synthetic approval '+role) for role in ['owner','evi','root']},'qualification_receipts':{key:h('synthetic qualification '+key) for key in evidence.QUALIFICATIONS},'previous_current_S_review_sha256':None,'previous_current_S_review':None}
    clock={**binding,'artifact_kind':'r5_current18_parent_clock_v1','clock_schema_version':1,'origin_unix_ns':origin,'original_budget_seconds':300,'approved_deadlines':admission['approved_deadlines'],'effective_deadline_unix_ns':deadline,'origin_before_native_init_and_compile':True,'go_new_budget_origin_allowed':False}
    clock_hash=evidence.sha(evidence.canonical(clock))
    admission['parent_clock_descriptor_sha256']=clock_hash
    catalog={**binding,'artifact_kind':'r5_current18_catalog_attestation_v1','database_schema_version':18,'origin':'actual_owned_database_after_official_New_Migrate','catalog':copy.deepcopy(contracts['catalog']['reference_catalog']),'migration_sha256':contracts['catalog']['migration_sha256'],'restart_catalog_unchanged':True,'qualification_receipt_sha256':admission['qualification_receipts']['C18-07'],'server_version_num':190000}
    build={**binding,'artifact_kind':'r5_current18_build_attestation_v1','qualification_receipt_sha256':admission['qualification_receipts']['C18-09'],'root_module':'tabmail','root_go_mod_sha256':source['files']['go.mod'],'build_context':source['build_context'],'selected_local_inputs':{name:source['files'][name] for name in ['third_party/enmime-v2.3.0/part.go','third_party/enmime-v2.3.0/parse_budget.go','internal/api/docsassets/a.js','internal/api/docsassets/a.css']},'external_inputs_manifest_sha256':h('synthetic external cache'),'toolchain_binary_sha256':h('synthetic toolchain'),'toolchain_version':'go1.25.7','compiled_binary_sha256':h('synthetic compiler output'),'local_inventory_is_not_external_input_attestation':True}
    checkpoints=[];messages=1000;claim_query=contracts['workload']['pipeline']['claim_query_sha256']
    for depth in evidence.preparation_depths(messages)+[messages]:
        terminal=depth==messages;phase='terminal' if terminal else 'before_claim'
        row={'attempt_id':binding['attempt_id'],'method_id':method,'parent_clock_sha256':clock_hash,'origin':'captured_preparation_claim_same_parameters_observed_heap','phase':phase,'source_sha':binding['source_sha'],'database_schema_version':18,'maintenance':'no_explicit_analyze_or_planner_or_durability_override','claim_requested_limit':20,'observed_jobs_total':messages if terminal else depth+20,'observed_pending':0 if terminal else 20,'observed_processing':0,'observed_ready_jobs':depth,'observed_durable_source_bound_ready':depth,'observed_failed':0,'observed_attempts_gt1':0,'query_sha256':claim_query,'parameters_sha256':evidence.sha(b'[20]'),'explain_only_not_actual_claim_latency':True,'plan':[{'Plan':{'Node Type':'Seq Scan','Relation Name':'mail_index_jobs'}}],'preparation':prep_stats(messages,phase,depth,clock,method,claim_query)}
        if not terminal:row['actual_claim_joined']=prep_stats(messages,'batch_joined',depth,clock,method,claim_query)
        checkpoints.append(row)
    safety=[{'workload':work,'origin':'actual_shipping_HTTP_and_JWT','foreign_positive_http':200,'foreign_denied_http':404,'revoked_before_http':200,'revoked_after_http':403,'restored_http':200} for work in sorted(evidence.WORKLOADS)]
    result={**binding,'artifact_kind':'r5_current18_result_v1','result_schema_version':1,'outcome':'completed','parent_clock_sha256':clock_hash,'database_schema_version':18,'catalog_attestation_sha256':evidence.sha(evidence.canonical(catalog)),'build_attestation_sha256':evidence.sha(evidence.canonical(build)),'employees':20,'mailboxes':100,'messages':1000,'seed':3893945,'concurrency':20,'pool_size':24,'dataset_fingerprint':h('synthetic actual streams'),'parameter_fingerprint':evidence.sha(evidence.canonical({'seed':3893945,'population':[20,100,1000]})),'fingerprint_method':'six_actual_canonical_sql_streams_v1','fingerprint_streams':[{'stream_id':i,'row_count':count,'sha256':h('synthetic stream '+str(i))} for i,count in enumerate([2,1000,1000,320,22,100])],'actual_distribution':evidence.distribution((20,100,1000)),'preparation':copy.deepcopy(checkpoints[-1]['preparation']),'preparation_checkpoints':checkpoints,'preparation_input_scheduler':None,'preparation_persistent_observations':None,'identity_boundary':contracts['method']['identity_boundary'],'sql_plans':[{'attempt_id':binding['attempt_id'],'method_id':method,'parent_clock_sha256':clock_hash,'origin':'captured_shipping_query_same_parameters','workload':'inbox_list','scale':'tool_only','source_sha':binding['source_sha'],'database_schema_version':18,'index_ready_count':1000,'query_sha256':h('inbox SQL'),'parameters_sha256':h('inbox args'),'plan':[{'Plan':{'Node Type':'Seq Scan','Relation Name':'messages'}}]}],'sql_tracer_calibration_count':20,'sql_tracer_calibration_expected':20,'hardware':{'os':'linux','arch':'amd64','cpu':4},'resource_scope':contracts['workload']['resource_scope'],'system_memory_observation':'not_measured','safety_observations':safety,'measurements':[{'workload':work,'cache_mode':cache,'samples':200,'sql_count':201,'p50_ms':1,'p95_ms':2,'p99_ms':3,'alloc_bytes':4,'rss_bytes':1024,'disk_bytes':4096,'max_wall_monotonic_gap_ms':0,'safety_assertion':work+':foreign_and_revoked_current_source_denied'} for work in sorted(evidence.WORKLOADS) for cache in sorted(evidence.CACHES)],'execution':{'go_started_ns':origin+3*evidence.NS,'observations_completed_ns':origin+6*evidence.NS,'max_wall_monotonic_gap_ms':0},'task_complete':False,'product_green':False}
    if method==evidence.LOOKAHEAD:
        order=evidence.expected_scheduler_order(1000,20);boxes=[evidence.fixture_mailbox_ordinal(n,20) for n in order]
        result['preparation_input_scheduler']={'method':method,'lookahead_limit':100,'generated_inputs':1000,'emitted_inputs':1000,'ordinal_fifo_completed_jobs':1000,'max_unpersisted_buffered_inputs':100,'joined_fifo_inflight':0,'fifo_gate_call_elapsed_seconds_sum':.01,'actual_emitted_ordinal_order':order,'actual_emitted_mailbox_ordinals':boxes,'actual_emitted_window_unique_mailboxes':[len(set(boxes[i:i+20])) for i in range(0,1000,20)]}
        result['preparation_persistent_observations']={'attempt_id':binding['attempt_id'],'method_id':method,'parent_clock_sha256':clock_hash,'source_sha':binding['source_sha'],'database_schema_version':18,'origin':'actual_owned_pg_catalog_and_received_at_projection','uid_column_count':0,'projected_messages':1000,'projected_mailboxes':len(set(boxes)),'ordinal_fifo_violations':0,'index_ready_count':1000,'source_identity_bindings_valid':True,'uid_catalog_query_sha256':'2de36ca227e3140de0f200ef3e10f207d9f917061469b8b7047d2c520de14ba8','received_at_projection_query_sha256':'d56b9930a294ee2b4a00ce7ffd95e0871fc2049f474bc08900d78071943a0a9d'}
    times=dict(zip(evidence.PHASES,[origin+i*evidence.NS for i in [0,1,2,3,3,3,4,4,5,6,7,8,9,10,11]]))
    process={**binding,'artifact_kind':'r5_current18_process_receipt_v1','parent_clock_sha256':clock_hash,'origin_unix_ns':origin,'effective_deadline_unix_ns':deadline,'phase_times':times,'go_exit':0,'runner_exit':0,'exit_observation':'observed_parent_wait','timed_out':False,'clock_reversed':False,'child_deadline_unix_ns':deadline+6*evidence.NS}
    cleanup={**binding,'artifact_kind':'r5_current18_cleanup_receipt_v1','parent_clock_sha256':clock_hash,'completed_ns':times['cleanup_completed_ns'],'parser_joined_inflight':0,'worker_joined_inflight':0,'readers_open':0,'owned_connections':0,'owned_db_absent':True,'owned_pg_stopped':True,'owned_processes_absent':True,'owned_objects_absent':True,'containment_only':False}
    test='TestR5BenchmarkCurrent18ToolOnlyWorkloads'
    command={**binding,'artifact_kind':'r5_current18_command_v1','parent_clock_sha256':clock_hash,'argv':['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout=300s','./internal/store/postgres','-run','^'+test+'$'],'environment':{'TABMAIL_R5_CURRENT_GENERATION':evidence.GENERATION,'TABMAIL_R5_CURRENT_ATTEMPT_ID':binding['attempt_id'],'TABMAIL_R5_CURRENT_METHOD_ID':method,'TABMAIL_R5_CURRENT_METHOD_SHA256':binding['method_contract_sha256'],'TABMAIL_R5_CURRENT_SOURCE_SHA':binding['source_sha'],'TABMAIL_R5_CURRENT_PARENT_CLOCK_SHA256':clock_hash,'GOWORK':'off','GOFLAGS':''}}
    def stamp(seconds):return datetime.fromtimestamp(seconds,tz=timezone.utc).isoformat().replace('+00:00','Z')
    events=[{'Time':stamp(origin//evidence.NS+(3 if i<2 else 9)),'Package':'tabmail/internal/store/postgres','Action':action,**({'Test':test} if action in ['run','pass'] and i<3 else {}),**({'Elapsed':6.0} if action=='pass' else {})} for i,action in enumerate(['start','run','pass','pass'])]
    members={name:evidence.canonical(value) for name,value in [('source-before.json',source),('source-after.json',source),('clock.json',clock),('catalog-attestation.json',catalog),('build-attestation.json',build),('process.json',process),('cleanup.json',cleanup),('command.json',command),('result.json',result)]}
    members.update({'method.json':raw['method'],'workload.json':raw['workload'],'catalog-contract.json':raw['catalog'],'go.jsonl':b'\n'.join(evidence.canonical(event) for event in events),'go.stderr':b''})
    admission_raw=evidence.canonical(admission)
    index={**binding,'artifact_kind':'r5_current18_benchmark_archive_v1','archive_schema_version':1,'outcome':'success','members':{name:evidence.sha(data) for name,data in members.items()},'admission_sha256':evidence.sha(admission_raw),'task_complete':False,'product_green':False}
    return index,members,admission_raw,evidence.canonical(clock),clock_hash,raw


class Current18ContractGrammarTests(unittest.TestCase):
    def validate(self,packet):
        index,members,raw,descriptor,pin,contracts=packet
        return evidence.validate_synthetic_archive(index,members,admission_raw=raw,admission_sha256=evidence.sha(raw),trusted_descriptor_raw=descriptor,trusted_descriptor_sha256=pin,synthetic_contract_bundle=contracts)

    def admission(self,packet,raw=None):
        raw=packet[2] if raw is None else raw
        return evidence._validate_admission(raw,evidence.sha(raw),packet[5])

    def positive(self,packet):
        result=self.validate(packet)
        self.assertTrue(result['status'].startswith('synthetic_'))
        self.assertFalse(result['runtime_qualification']);self.assertFalse(result['product_green'])
        return result

    def write_member(self,packet,name,value):
        packet[1][name]=evidence.canonical(value);packet[0]['members'][name]=evidence.sha(packet[1][name])

    def mutate_member(self,packet,name,mutation):
        value=evidence.parse_json(packet[1][name]);mutation(value);self.write_member(packet,name,value)

    def reject_member(self,name,mutation,reason,method=evidence.BOUNDED):
        packet=fixture(method);self.positive(packet)
        self.mutate_member(packet,name,mutation)
        with self.assertRaisesRegex(ValueError,reason):self.validate(packet)

    def descriptor_kwargs(self,packet):
        return {'trusted_descriptor_raw':packet[3],'trusted_descriptor_sha256':packet[4]}

    def test_public_schema_matches_full_grammar_and_maintenance_literal(self):
        packet=fixture();self.positive(packet)
        schema=evidence.parse_json((evidence.CONTRACTS/'r5-benchmark-current18-evidence.schema.json').read_bytes())
        self.assertEqual(set(schema['$defs']['result']['properties']),evidence.RESULT_FIELDS)
        self.assertEqual(set(schema['$defs']['preparation']['properties']),evidence.PREPARATION_FIELDS)
        self.assertEqual(set(schema['$defs']['process']['properties']['phase_times']['properties']),set(evidence.PHASES))
        literal=evidence.load_contracts(evidence.BOUNDED)[0]['workload']['pipeline']['maintenance']
        self.assertEqual(literal,'no_explicit_analyze_or_planner_or_durability_override')
        self.assertEqual(schema['$defs']['checkpoint']['properties']['maintenance'],{'const':literal})
        self.reject_member('result.json',lambda r:r['preparation_checkpoints'][0].__setitem__('maintenance','no_explicit_analyze_or_planner_override'),'actual preparation trace/source/schema/no-maintenance boundary required')

    def test_two_methods_have_complete_synthetic_positive_controls_only(self):
        for method in [evidence.BOUNDED,evidence.LOOKAHEAD]:
            with self.subTest(method=method):
                result=self.positive(fixture(method))
                self.assertIsNone(result['candidate_threshold_passed'])
                self.assertFalse(result['relative_regression_certified'])

    def test_actual_historical_partial_catalog_is_fail_closed_despite_qualification_hashes(self):
        packet=fixture(synthetic=False);index,members,raw,descriptor,pin,_=packet
        admission,contracts,_=evidence._validate_admission(raw,evidence.sha(raw))
        catalog=contracts['catalog']
        self.assertEqual(catalog['reference_raw_sha256'],'02a5850d03a8909ee95720e56d755128b4e6f923ab43cbeffb35fc0de1f14f94')
        self.assertFalse(catalog['runtime_reference_ready']);self.assertFalse(catalog['reference_shape_complete'])
        self.assertIn('typmod',catalog['missing_coverage']['columns'])
        self.assertIn('enabled',catalog['missing_coverage']['triggers'])
        self.assertIn('cache',catalog['missing_coverage']['sequences'])
        self.assertIn('valid',catalog['missing_coverage']['indexes'])
        self.assertIn('functions',catalog['missing_coverage'])
        # SOURCE parsing/admission syntax is positive; actual runtime reference
        # is deliberately NOT a positive fixture and must never become one.
        for label in ['original','different_signed_qualification_hash']:
            candidate=copy.deepcopy(admission)
            if label!='original':candidate['qualification_receipts']['C18-07']=h(label)
            candidate_raw=evidence.canonical(candidate)
            evidence._validate_admission(candidate_raw,evidence.sha(candidate_raw))
            with self.assertRaisesRegex(ValueError,'C18-07 full catalog reference not ready'):
                evidence.validate_admission(candidate_raw,evidence.sha(candidate_raw))
            consistent=copy.deepcopy(packet);consistent[0]['admission_sha256']=evidence.sha(candidate_raw)
            self.mutate_member(consistent,'catalog-attestation.json',lambda obj:obj.__setitem__('qualification_receipt_sha256',candidate['qualification_receipts']['C18-07']))
            self.mutate_member(consistent,'result.json',lambda obj:obj.__setitem__('catalog_attestation_sha256',evidence.sha(consistent[1]['catalog-attestation.json'])))
            with self.subTest(label=label),self.assertRaisesRegex(ValueError,'C18-07 full catalog reference not ready'):
                evidence.validate_archive(consistent[0],consistent[1],admission_raw=candidate_raw,admission_sha256=evidence.sha(candidate_raw),trusted_descriptor_raw=descriptor,trusted_descriptor_sha256=pin)

    def test_declared_full_reference_flags_cannot_hide_missing_dimensions(self):
        packet=fixture();self.positive(packet)
        values=evidence._validate_contract_bundle(evidence.BOUNDED,packet[5])[0]
        for collection,field in [('columns','typmod'),('triggers','enabled'),('sequences','cache'),('indexes','ready'),('functions','identity_arguments')]:
            catalog=copy.deepcopy(values['catalog'])
            evidence.require_full_catalog_reference(catalog,synthetic=True)
            catalog['reference_catalog'][collection][0].pop(field)
            catalog['reference_catalog_sha256']=evidence.sha(evidence.canonical(catalog['reference_catalog']))
            with self.subTest(collection=collection),self.assertRaisesRegex(ValueError,'full catalog '+collection+': missing/unknown fields'):
                evidence.require_full_catalog_reference(catalog,synthetic=True)

    def test_external_admission_pin_and_qualifications_remain_required(self):
        packet=fixture();self.positive(packet)
        with self.assertRaisesRegex(ValueError,'admission byte pin'):
            evidence.validate_synthetic_archive(packet[0],packet[1],admission_raw=packet[2],admission_sha256='0'*64,synthetic_contract_bundle=packet[5],**self.descriptor_kwargs(packet))
        for key in sorted(evidence.QUALIFICATIONS):
            self.admission(packet)
            value=evidence.parse_json(packet[2]);value['qualification_receipts'].pop(key)
            with self.subTest(key=key),self.assertRaisesRegex(ValueError,'qualification prerequisites: missing/unknown fields'):
                self.admission(packet,evidence.canonical(value))

    def test_workload_mutations_reach_named_domain_gates(self):
        cases=[(lambda r:r.__setitem__('messages',999),'original population/C20/pool24/SQL20 changed'),
               (lambda r:r.__setitem__('concurrency',21),'original population/C20/pool24/SQL20 changed'),
               (lambda r:r.__setitem__('pool_size',25),'original population/C20/pool24/SQL20 changed'),
               (lambda r:r['measurements'][0].__setitem__('samples',199),'exact original 200 samples required'),
               (lambda r:r['measurements'].pop(),'all 7x2 workload/cache rows required'),
               (lambda r:r['safety_observations'][0].__setitem__('foreign_denied_http',200),'actual positive/foreign/revoked safety statuses differs')]
        for mutation,reason in cases:self.reject_member('result.json',mutation,reason)

    def test_external_descriptor_pin_is_required_and_not_archive_self_digest(self):
        packet=fixture();self.positive(packet)
        clock=evidence.parse_json(packet[1]['clock.json']);admission=self.admission(packet)[0]
        evidence.validate_clock(clock,admission,**self.descriptor_kwargs(packet))
        with self.assertRaisesRegex(ValueError,'external parent descriptor byte pin differs'):
            evidence.validate_clock(clock,admission,trusted_descriptor_raw=packet[3],trusted_descriptor_sha256='0'*64)
        moved=copy.deepcopy(packet)
        self.mutate_member(moved,'clock.json',lambda c:c.__setitem__('origin_unix_ns',c['origin_unix_ns']+1))
        with self.assertRaisesRegex(ValueError,'archive parent clock differs from independently trusted descriptor'):
            self.validate(moved)

    def test_same_admission_reorigin_and_all_internal_rehashes_cannot_replace_trusted_clock(self):
        packet=fixture();self.positive(packet);moved=copy.deepcopy(packet);delta=evidence.NS
        clock=evidence.parse_json(moved[1]['clock.json']);clock['origin_unix_ns']+=delta
        new_clock_raw=evidence.canonical(clock);new_hash=evidence.sha(new_clock_raw)
        self.write_member(moved,'clock.json',clock)
        def rebind(value):
            if type(value) is dict:
                for key,item in list(value.items()):
                    if key in {'parent_clock_sha256','TABMAIL_R5_CURRENT_PARENT_CLOCK_SHA256'}:value[key]=new_hash
                    elif key=='parser_min_start_remaining_ns':value[key]=item-delta
                    else:rebind(item)
            elif type(value) is list:
                for item in value:rebind(item)
        for name in ['result.json','process.json','cleanup.json','command.json']:
            value=evidence.parse_json(moved[1][name]);rebind(value)
            if name=='result.json':
                value['execution']['go_started_ns']+=delta;value['execution']['observations_completed_ns']+=delta
            elif name=='process.json':
                value['origin_unix_ns']+=delta
                value['phase_times']={key:stamp+delta if stamp is not None else None for key,stamp in value['phase_times'].items()}
            elif name=='cleanup.json':value['completed_ns']+=delta
            self.write_member(moved,name,value)
        events=[evidence.parse_json(line) for line in moved[1]['go.jsonl'].splitlines()]
        for event in events:
            stamp=evidence.event_time_ns(event['Time'])+delta
            event['Time']=datetime.fromtimestamp(stamp//evidence.NS,tz=timezone.utc).isoformat().replace('+00:00','Z')
        moved[1]['go.jsonl']=b'\n'.join(evidence.canonical(event) for event in events);moved[0]['members']['go.jsonl']=evidence.sha(moved[1]['go.jsonl'])
        self.assertEqual(moved[2],packet[2])
        # A new external descriptor alone must also fail under the SAME
        # pinned admission. Internally rehashing every artifact is insufficient.
        descriptor_only=(*moved[:3],new_clock_raw,new_hash,moved[5])
        # The structural positive control uses an explicitly DIFFERENT
        # synthetic admission as well; not actual attempt reuse permission.
        different_admission=evidence.parse_json(moved[2]);different_admission['parent_clock_descriptor_sha256']=new_hash
        different_raw=evidence.canonical(different_admission);different_index=copy.deepcopy(moved[0]);different_index['admission_sha256']=evidence.sha(different_raw)
        self.positive((different_index,moved[1],different_raw,new_clock_raw,new_hash,moved[5]))
        with self.assertRaisesRegex(ValueError,'external parent descriptor differs from admitted exact clock pin'):
            self.validate(descriptor_only)
        with self.assertRaisesRegex(ValueError,'archive parent clock differs from independently trusted descriptor'):
            self.validate(moved)

    def test_parent31_guard_and_invalid_earlier_deadline_descriptor(self):
        packet=fixture();self.positive(packet);admission=self.admission(packet)[0];clock=evidence.parse_json(packet[3]);deadline=clock['effective_deadline_unix_ns']
        evidence.validate_clock(clock,admission,**self.descriptor_kwargs(packet))
        for remaining,want in [(31*evidence.NS,False),(29*evidence.NS,False),(31*evidence.NS+1,True)]:
            self.assertIs(evidence.parser_admission_allowed(clock,admission,deadline-remaining,**self.descriptor_kwargs(packet)),want)
        admitted=copy.deepcopy(admission);admitted['approved_deadlines']['controller']=clock['origin_unix_ns']+100*evidence.NS
        early=copy.deepcopy(clock);early['approved_deadlines']=admitted['approved_deadlines'];early['effective_deadline_unix_ns']=admitted['approved_deadlines']['controller']
        raw=evidence.canonical(early);admitted['parent_clock_descriptor_sha256']=evidence.sha(raw)
        evidence.validate_clock(early,admitted,trusted_descriptor_raw=raw,trusted_descriptor_sha256=evidence.sha(raw))
        for key,value,reason in [('effective_deadline_unix_ns',deadline,'earliest deadline not retained'),('go_new_budget_origin_allowed',True,'later budget origin'),('original_budget_seconds',True,'parent budget: exact integer required')]:
            good_raw=evidence.canonical(early)
            evidence.validate_clock(early,admitted,trusted_descriptor_raw=good_raw,trusted_descriptor_sha256=evidence.sha(good_raw))
            bad=copy.deepcopy(early);bad[key]=value;raw=evidence.canonical(bad);bad_admission=copy.deepcopy(admitted);bad_admission['parent_clock_descriptor_sha256']=evidence.sha(raw)
            with self.subTest(key=key),self.assertRaisesRegex(ValueError,reason):
                evidence.validate_clock(bad,bad_admission,trusted_descriptor_raw=raw,trusted_descriptor_sha256=evidence.sha(raw))

    def test_parser_deadline_reserve_join_and_claim_mutations_have_valid_preconditions(self):
        cases=[('parser_deadline_unix_ns',1_800_000_306*evidence.NS,'Parser deadline differs from earliest parent deadline'),
               ('parser_min_start_remaining_ns',31*evidence.NS,'actual minimum parse-start remaining original wall must exceed31s'),
               ('parser_joined_inflight',1,'actual formal Parser joined/drained/quiescent observations required'),
               ('parser_quiescent',False,'actual formal Parser joined/drained/quiescent observations required')]
        for field,value,reason in cases:self.reject_member('result.json',lambda r:r['preparation'].__setitem__(field,value),reason)
        def wrong_claim(result):
            result['preparation']['claim_query_sha256']='f'*64
            for point in result['preparation_checkpoints']:
                point['query_sha256']='f'*64;point['preparation']['claim_query_sha256']='f'*64
                if 'actual_claim_joined' in point:point['actual_claim_joined']['claim_query_sha256']='f'*64
        self.reject_member('result.json',wrong_claim,'actual Claim20/lease90/attempt5 query differs from source contract')

    def test_go_pass_after_exit_but_before_parent_wait_is_rejected(self):
        packet=fixture();self.positive(packet)
        process=evidence.parse_json(packet[1]['process.json']);origin=process['origin_unix_ns']
        self.assertEqual(process['phase_times']['go_exit_observed_ns'],origin+9*evidence.NS)
        self.assertEqual(process['phase_times']['cleanup_completed_ns'],origin+10*evidence.NS)
        self.assertEqual(process['phase_times']['parent_wait_completed_ns'],origin+11*evidence.NS)
        events=[evidence.parse_json(line) for line in packet[1]['go.jsonl'].splitlines()]
        for event in events:
            if event['Action']=='pass':event['Time']=datetime.fromtimestamp(origin/evidence.NS+10.5,tz=timezone.utc).isoformat().replace('+00:00','Z')
        packet[1]['go.jsonl']=b'\n'.join(evidence.canonical(event) for event in events);packet[0]['members']['go.jsonl']=evidence.sha(packet[1]['go.jsonl'])
        with self.assertRaisesRegex(ValueError,'raw Go event outside declared Go start/exit phase'):self.validate(packet)

    def test_go_start_before_go_phase_and_elapsed_mismatch_are_rejected(self):
        for mode,reason in [('early_start','raw Go event outside declared Go start/exit phase'),('elapsed','raw Go elapsed exceeds its recorded lifecycle interval')]:
            packet=fixture();self.positive(packet);events=[evidence.parse_json(line) for line in packet[1]['go.jsonl'].splitlines()]
            if mode=='early_start':events[0]['Time']=datetime.fromtimestamp(1_800_000_002.5,tz=timezone.utc).isoformat().replace('+00:00','Z')
            else:events[2]['Elapsed']=7
            packet[1]['go.jsonl']=b'\n'.join(evidence.canonical(event) for event in events);packet[0]['members']['go.jsonl']=evidence.sha(packet[1]['go.jsonl'])
            with self.subTest(mode=mode),self.assertRaisesRegex(ValueError,reason):self.validate(packet)

    def test_catalog_mutations_reach_full_definition_gate(self):
        cases=[(lambda c:c['catalog']['constraints'][0].__setitem__('definition','changed_same_count'),'full catalog definitions differ'),
               (lambda c:c['catalog'].pop('columns'),'full actual catalog collections required'),
               (lambda c:c.__setitem__('database_schema_version',16),'full catalog definitions differ'),
               (lambda c:c['migration_sha256'].pop(next(iter(c['migration_sha256']))),'full catalog definitions differ')]
        for mutation,reason in cases:self.reject_member('catalog-attestation.json',mutation,reason)

    def test_cross_run_and_unknown_fields_reach_named_binding_gates(self):
        cases=[('result.json','attempt_id','2'*32,'cross-run attempt_id differs'),
               ('cleanup.json','source_sha','f'*40,'cross-run source_sha differs'),
               ('result.json','parent_clock_sha256','0'*64,'result clock/catalog/build cross-run binding differs'),
               ('build-attestation.json','qualification_receipt_sha256','0'*64,'root-MVS/external-cache/toolchain qualification required'),
               ('result.json','unknown_alias',True,'current completed result: missing/unknown fields')]
        for member,key,value,reason in cases:self.reject_member(member,lambda r:r.__setitem__(key,value),reason)

    def test_duplicate_nonfinite_and_numeric_aliases_do_not_use_early_failures(self):
        for raw,reason in [('{"n":1,"n":2}','duplicate JSON key'),('{"n":NaN}','nonfinite JSON'),('{"n":Infinity}','nonfinite JSON'),('{"n":1e999}','nonfinite JSON number')]:
            self.assertEqual(evidence.parse_json('{"n":1}'),{'n':1})
            with self.assertRaisesRegex(ValueError,reason):evidence.parse_json(raw)
        cases=[('result_schema_version',True,'result format cannot certify task/product'),('employees',True,'employees: exact integer required'),('concurrency',20.0,'concurrency: exact integer required'),('database_schema_version',16,'current DB18 required'),('source_identity_kind','canonical_source_closure_sha1','cross-run source_identity_kind differs')]
        for key,value,reason in cases:self.reject_member('result.json',lambda r:r.__setitem__(key,value),reason)

    def test_late_cleanup_unknown_exit_and_platform_mismatch_have_positive_controls(self):
        cases=[('process.json',lambda p:p['phase_times'].__setitem__('parent_wait_completed_ns',1_800_000_301*evidence.NS),'late cleanup/parent wait cannot be success'),
               ('process.json',lambda p:p.__setitem__('go_exit',None),'unknown exit must retain unknown observation status'),
               ('cleanup.json',lambda p:p.__setitem__('owned_db_absent',None),'success cleanup incomplete/unknown/containment-only'),
               ('cleanup.json',lambda p:p.__setitem__('containment_only',True),'success cleanup incomplete/unknown/containment-only'),
               ('result.json',lambda p:p['execution'].__setitem__('max_wall_monotonic_gap_ms',101),'result late or interrupted under original parent deadline'),
               ('result.json',lambda p:p['hardware'].__setitem__('arch','arm64'),'runtime platform versus admitted build context differs')]
        for member,mutation,reason in cases:self.reject_member(member,mutation,reason)

    def test_archive_extra_escape_hash_and_missing_build_inputs_reach_correct_gates(self):
        for name,reason in [('../escaped.json','noncanonical archive/source path'),('extra.json','known archive layout required')]:
            packet=fixture();self.positive(packet);packet[1][name]=b'{}';packet[0]['members'][name]=evidence.sha(b'{}')
            with self.assertRaisesRegex(ValueError,reason):self.validate(packet)
        packet=fixture();self.positive(packet);packet[1]['method.json']+=b'\n'
        with self.assertRaisesRegex(ValueError,'archive member bytes differ'):self.validate(packet)
        for name in ['third_party/enmime-v2.3.0/part.go','internal/api/docsassets/a.js']:
            self.reject_member('build-attestation.json',lambda p:p['selected_local_inputs'].pop(name),'selected compilation omitted actual fork or embeds')
        self.reject_member('build-attestation.json',lambda p:p.__setitem__('external_inputs_manifest_sha256',None),'external_inputs_manifest_sha256: exact hash required')

    def test_lookahead_FIFO_and_persistent_boundaries_use_lookahead_positive_controls(self):
        cases=[(lambda r:r['preparation_input_scheduler']['actual_emitted_ordinal_order'].__setitem__(0,999),'actual emitted ordinals must be an exact integer permutation'),
               (lambda r:r['preparation_input_scheduler'].__setitem__('lookahead_limit',101),'actual scheduler generated/emitted/FIFOcompleted N and joined buffer bounds required'),
               (lambda r:r['preparation_persistent_observations'].__setitem__('ordinal_fifo_violations',1),'actual FIFO/UID PostgreSQL observations required'),
               (lambda r:r['identity_boundary'].__setitem__('protocol_UID_certified',True),'persistent identity non-equivalence differs')]
        for mutation,reason in cases:self.reject_member('result.json',mutation,reason,evidence.LOOKAHEAD)

    def failed_packet(self,reason):
        packet=fixture();self.positive(packet);index,members,raw=packet[:3]
        admission=self.admission(packet)[0];binding={key:admission[key] for key in evidence.BINDING};clock=evidence.parse_json(packet[3]);deadline=clock['effective_deadline_unix_ns']
        index['outcome']='failed';members.pop('result.json');index['members'].pop('result.json')
        if reason=='absolute_deadline_exceeded':
            self.mutate_member(packet,'process.json',lambda p:p.update(go_exit=-15,runner_exit=1,timed_out=True,phase_times={key:None for key in evidence.PHASES}))
            self.mutate_member(packet,'cleanup.json',lambda p:p.update(completed_ns=deadline+evidence.NS,containment_only=True))
            members['go.jsonl']=b'\n'.join(members['go.jsonl'].splitlines()[:2]);observed=deadline
        else:
            self.mutate_member(packet,'process.json',lambda p:p.update(go_exit=None,runner_exit=1,exit_observation='unknown_not_observed',child_deadline_unix_ns=None,phase_times={key:None for key in evidence.PHASES}))
            self.mutate_member(packet,'cleanup.json',lambda p:p.update(completed_ns=None,parser_joined_inflight=None,worker_joined_inflight=None,readers_open=None,owned_connections=None,owned_db_absent=None,owned_pg_stopped=None,owned_processes_absent=None,owned_objects_absent=None,containment_only=True))
            members['go.jsonl']=b'';observed=clock['origin_unix_ns']+1
        index['members']['go.jsonl']=evidence.sha(members['go.jsonl'])
        failure={**binding,'artifact_kind':'r5_current18_attempt_failure_v1','failure_schema_version':1,'parent_clock_sha256':packet[4],'reason':reason,'observed_at_ns':observed,'result_present':False,'task_complete':False,'product_green':False}
        self.write_member(packet,'failure.json',failure)
        return packet

    def test_typed_deadline_failure_and_late_containment_never_become_baseline(self):
        packet=self.failed_packet('absolute_deadline_exceeded');result=self.positive(packet)
        self.assertFalse(result['baseline_complete'])
        deadline=evidence.parse_json(packet[3])['effective_deadline_unix_ns']
        self.mutate_member(packet,'failure.json',lambda f:f.__setitem__('observed_at_ns',deadline-1))
        with self.assertRaisesRegex(ValueError,'deadline failure lacks actual parent deadline observation'):self.validate(packet)

    def test_typed_unknown_exit_does_not_backfill_numeric_exit(self):
        packet=self.failed_packet('unknown_exit_observation');self.positive(packet)
        self.mutate_member(packet,'process.json',lambda p:p.__setitem__('go_exit',-15))
        with self.assertRaisesRegex(ValueError,'unknown exit observation cannot carry a numeric Go exit'):self.validate(packet)

    def test_legacy_methods_old_S_and_L_do_not_gain_current_admission(self):
        packet=fixture();self.positive(packet)
        for method in ['bounded_store_claim_complete_window_v1','lookahead100_unpersisted_inputs_distinct_mailbox_windows20_ordinal_fifo_v1']:
            evidence.load_contracts(evidence.BOUNDED)
            with self.assertRaisesRegex(ValueError,'unknown current method; no legacy fallback'):evidence.load_contracts(method)
        for scale,reason in [('L','legacy/unknown/L admission refused'),('M','new current S prerequisite: exact hash required')]:
            self.admission(packet);admission=evidence.parse_json(packet[2]);admission['scale']=scale
            with self.assertRaisesRegex(ValueError,reason):self.admission(packet,evidence.canonical(admission))
        self.admission(packet);admission=evidence.parse_json(packet[2]);admission['scale']='M'
        admission['previous_current_S_review']={'artifact_kind':'historical_original_S','source_identity_kind':'git_write_tree'}
        admission['previous_current_S_review_sha256']=evidence.sha(evidence.canonical(admission['previous_current_S_review']))
        with self.assertRaisesRegex(ValueError,'new current S review: missing/unknown fields'):self.admission(packet,evidence.canonical(admission))


if __name__=='__main__':unittest.main()
