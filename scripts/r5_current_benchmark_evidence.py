"""SOURCE-stage current18 evidence grammar; pure validation, never a runner.

The installed catalog is explicitly historical PARTIAL data. Public actual
admission is blocked pending C18-07. Synthetic full-shaped references are
accepted only by the separately named fixture API, never as runtime proof.
No current runtime is authorized by these contracts. validate_archive requires
independently supplied admission AND exact canonical parent-descriptor pins,
including owner/evi/root admission and the
still-unimplemented C18-02/05/06/07/09 qualifications. Synthetic tests establish
only grammar behavior. Historical schema15/16 validators are not imported.
"""
from __future__ import annotations
import hashlib
from datetime import datetime
import json
import math
from pathlib import Path, PurePosixPath
import re

CONTRACTS = Path(__file__).with_name('contracts')
GENERATION = 'r5_current18_fork_source_v2_parentclock_v1'
BOUNDED = 'r5_current18_fork_v2_bounded20_parentclock_v1'
LOOKAHEAD = 'r5_current18_fork_v2_lookahead100_fifo20_parentclock_v1'
POLICY = 'r5_current_local_inputs_v2'
SOURCE_KIND = 'policy_bound_local_input_superset_sha1_v2'
METHOD_FILES = {BOUNDED:'r5-benchmark-current18-bounded-method-v1.json', LOOKAHEAD:'r5-benchmark-current18-lookahead-method-v1.json'}
WORKLOAD_FILE = 'r5-benchmark-current18-workload-v1.json'
CATALOG_FILE = 'r5-benchmark-current18-catalog-v1.json'
QUALIFICATIONS = {'C18-02','C18-03','C18-05','C18-06','C18-07','C18-09'}
SCALES = {'tool_only':(20,100,1000), 'S':(100,500,100000), 'M':(1000,5000,1000000)}
BUDGETS = {'tool_only':(300,10), 'S':(1800,10), 'M':(10800,80)}
WORKLOADS = {'inbox_list','indexed_search','indexed_message','permission_mailboxes','draft_save','submit_and_loopback_worker','gc_reference_safety'}
CACHES = {'app_cold_db_os_unspecified','app_hot_db_os_unspecified'}
NS = 1_000_000_000
BINDING = {'evidence_generation','attempt_id','scale','source_sha','source_policy','source_identity_kind','source_manifest_sha256','method_id','method_contract_sha256','workload_contract_sha256','catalog_contract_sha256'}
CATALOG_COLLECTIONS = {'tables','columns','constraints','indexes','sequences','trigger_functions','triggers','functions'}
FAILURES = {'compile_failure','catalog_mismatch','source_drift','absolute_deadline_exceeded','runtime_assertion_failure','unknown_exit_observation','cleanup_unverified'}
REQUIRED_MEMBERS = {'source-before.json','source-after.json','method.json','workload.json','catalog-contract.json','clock.json','catalog-attestation.json','build-attestation.json','process.json','cleanup.json','command.json','go.jsonl','go.stderr'}


def canonical(value):
    return json.dumps(value,sort_keys=True,separators=(',',':'),allow_nan=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def parse_json(raw):
    if type(raw) not in {bytes,str}:raise ValueError('JSON text or bytes required')
    def pairs(items):
        result = {}
        for key,value in items:
            if key in result:raise ValueError('duplicate JSON key: '+key)
            result[key]=value
        return result
    def constant(value):raise ValueError('nonfinite JSON: '+value)
    def floating(value):
        number=float(value)
        if not math.isfinite(number):raise ValueError('nonfinite JSON number')
        return number
    value=json.loads(raw,object_pairs_hook=pairs,parse_constant=constant,parse_float=floating)
    finite_tree(value)
    return value


def finite_tree(value):
    if type(value) is dict:
        if any(type(key) is not str for key in value):raise ValueError('string JSON keys required')
        for item in value.values():finite_tree(item)
    elif type(value) is list:
        for item in value:finite_tree(item)
    elif type(value) is float:
        if not math.isfinite(value):raise ValueError('nonfinite evidence')
    elif value is not None and type(value) not in {str,int,bool}:raise ValueError('plain JSON values required')


def exact(value, keys, label):
    if type(value) is not dict or set(value)!=set(keys):raise ValueError(label+': missing/unknown fields')
    finite_tree(value)
    return value


def integer(value, label, minimum=0):
    if type(value) is not int or value<minimum:raise ValueError(label+': exact integer required')
    return value


def number(value,label,minimum=0):
    if type(value) not in {int,float} or not math.isfinite(value) or value<minimum:raise ValueError(label+': finite number required')
    return value


def digest(value,label='digest',length=64):
    if type(value) is not str or not re.fullmatch('[0-9a-f]{'+str(length)+'}',value):raise ValueError(label+': exact hash required')
    return value


def same(left,right,label):
    if canonical(left)!=canonical(right):raise ValueError(label+' differs')


def path_name(name):
    if type(name) is not str or not name or '\\' in name or '\x00' in name or PurePosixPath(name).is_absolute() or any(part in {'','.','..'} for part in name.split('/')):raise ValueError('noncanonical archive/source path')
    return name


METHOD_FIELDS = {'L_execution_authorized', 'activation', 'authorized_scale_without_attempt_admission', 'catalog_contract', 'clock_contract', 'contract_kind', 'contract_schema_version', 'database_schema_version', 'evidence_generation', 'identity_boundary', 'legacy_METHOD_DATASET_and_archives_unchanged', 'local_replacement', 'method_id', 'product_green', 'required_qualification_receipts', 'result_schema_path', 'runtime_qualification_status', 'scheduler', 'source_final_hash_binding', 'source_identity_kind', 'source_policy', 'task_complete', 'workload_contract'}
WORKLOAD_FIELDS = {'activation', 'cache_modes', 'candidate_thresholds', 'concurrency', 'contract_kind', 'contract_schema_version', 'evidence_generation', 'executability', 'fingerprint_method', 'historical_dataset_reference', 'identity_boundary', 'message_sizes', 'pipeline', 'pool_size', 'population', 'product_green', 'resource_scope', 'samples_per_workload_cache', 'scales', 'seed', 'shipping_paths', 'sql_tracer_calibration_queries', 'system_memory_observation', 'task_complete', 'tool_only', 'total_samples', 'workloads'}
CATALOG_FIELDS = {'current_full_catalog_qualification_status', 'reference_shape_complete', 'reference_catalog', 'informational_counts', 'comparison', 'current_runtime_attestation_required', 'reference_kind', 'missing_coverage', 'product_green', 'migration_sha256', 'reference_catalog_sha256', 'required_collections', 'reference_raw_sha256', 'contract_kind', 'reference_catalog_sha256_scope', 'contract_schema_version', 'reference_collection_scope', 'required_full_fields', 'runtime_admission_from_this_reference_alone', 'task_complete', 'evidence_generation', 'database_schema_version', 'runtime_reference_ready'}
FULL_CATALOG_FIELDS = {'columns': ['table', 'name', 'type', 'default', 'nullable', 'position', 'typmod', 'formatted_type', 'collation', 'identity', 'generated'], 'constraints': ['table', 'name', 'type', 'definition', 'validated', 'deferrable', 'initially_deferred', 'columns', 'referenced_table', 'referenced_columns', 'fk_update_action', 'fk_delete_action', 'fk_match_type'], 'functions': ['schema', 'name', 'identity_arguments', 'return_type', 'language', 'definition', 'security_definer', 'volatility', 'strict', 'parallel', 'leakproof', 'configuration'], 'indexes': ['table', 'name', 'definition', 'valid', 'ready'], 'sequences': ['name', 'type', 'start', 'min', 'max', 'increment', 'cycle', 'cache', 'owner_table', 'owner_column', 'ownership_dependency_kind'], 'tables': ['table', 'rls', 'forced_rls'], 'trigger_functions': ['name', 'definition'], 'triggers': ['table', 'name', 'function', 'definition', 'enabled', 'deferrable', 'initially_deferred', 'type_bits']}

def load_contracts(method_id):
    if type(method_id) is not str or method_id not in METHOD_FILES:raise ValueError('unknown current method; no legacy fallback')
    raw={key:(CONTRACTS/name).read_bytes() for key,name in [('method',METHOD_FILES[method_id]),('workload',WORKLOAD_FILE),('catalog',CATALOG_FILE)]}
    return _validate_contract_bundle(method_id,raw)


def _validate_contract_bundle(method_id,raw):
    if type(method_id) is not str or method_id not in METHOD_FILES:raise ValueError('unknown current method; no legacy fallback')
    if type(raw) is not dict or set(raw)!={'method','workload','catalog'} or any(type(value) is not bytes for value in raw.values()):raise ValueError('exact contract byte bundle required')
    values={key:parse_json(data) for key,data in raw.items()}
    method,workload,catalog=(values[key] for key in ['method','workload','catalog'])
    exact(method,METHOD_FIELDS,'method contract');exact(workload,WORKLOAD_FIELDS,'workload contract');exact(catalog,CATALOG_FIELDS,'catalog contract')
    for value,kind in [(method,'method'),(workload,'workload'),(catalog,'catalog')]:
        if type(value.get('contract_schema_version')) is not int or value['contract_schema_version']!=1 or value.get('contract_kind')!='r5_current18_'+kind+'_v1' or value.get('evidence_generation')!=GENERATION:raise ValueError('unsupported contract generation')
    same(method['database_schema_version'],18,'method DB schema type/version')
    same([workload[k] for k in ['seed','concurrency','pool_size','samples_per_workload_cache','total_samples','sql_tracer_calibration_queries']],[3893945,20,24,200,2800,20],'exact numeric workload invariants')
    same([workload['pipeline'][k] for k in ['window_messages','claim_limit','parser_concurrency','parser_timeout_seconds','parser_join_reserve_seconds','lease_seconds','max_attempts']],[20,20,4,30,31,90,5],'pipeline invariants')
    same(workload['message_sizes'],[{'bytes':4096,'weight_per_1000':800},{'bytes':32768,'weight_per_1000':180},{'bytes':262144,'weight_per_1000':19},{'bytes':2097152,'weight_per_1000':1}],'original MIME size distribution')
    same(workload['candidate_thresholds'],{'S_list_p95_ms':300,'M_list_p95_ms':500,'M_index_ready_query_p95_ms':1000,'unexplained_regression_percent':10,'attachment_and_public_SMTP':'not_list_latency_threshold'},'original candidate thresholds')
    if method['method_id']!=method_id or method['database_schema_version']!=18 or method['source_policy']!=POLICY or method['source_identity_kind']!=SOURCE_KIND:raise ValueError('current contract identity mismatch')
    if method['workload_contract']['sha256']!=sha(raw['workload']) or method['catalog_contract']['sha256']!=sha(raw['catalog']):raise ValueError('contract dependency byte hash mismatch')
    if workload['seed']!=3893945 or workload['concurrency']!=20 or workload['pool_size']!=24 or workload['samples_per_workload_cache']!=200 or workload['total_samples']!=2800 or set(workload['workloads'])!=WORKLOADS or set(workload['cache_modes'])!=CACHES:raise ValueError('original workload invariants changed')
    for scale in ['S','M']:
        row=workload['scales'][scale]
        same([row[k] for k in ['employees','mailboxes','messages']],list(SCALES[scale]),'original population')
        same([row['budget_seconds'],row['disk_budget_gib']],list(BUDGETS[scale]),'original budgets')
    if workload['executability']['L']!='not_authorized' or method['L_execution_authorized'] is not False or method['authorized_scale_without_attempt_admission']!=[]:raise ValueError('implicit execution authorization forbidden')
    reference=catalog['reference_catalog']
    if type(reference) is not dict or set(reference)!=set(catalog['reference_collection_scope']) or any(type(rows) is not list for rows in reference.values()):raise ValueError('declared historical reference collection scope differs')
    same(catalog['required_full_fields'],FULL_CATALOG_FIELDS,'declared C18-07 full coverage fields')
    same(catalog['required_collections'],sorted(CATALOG_COLLECTIONS),'required full catalog collections')
    if sha(canonical(reference))!=catalog['reference_catalog_sha256'] or catalog['runtime_admission_from_this_reference_alone'] is not False:raise ValueError('partial reference digest is not current runtime qualification')
    return values,raw


def require_full_catalog_reference(contract,*,synthetic=False):
    """Presence of seven arrays/counts or qualification hashes is not completeness."""
    expected_kind='synthetic_complete_fixture_not_runtime_reference' if synthetic else 'current_full_catalog_reference_C18_07_v1'
    if (contract['reference_kind']!=expected_kind or contract['reference_shape_complete'] is not True
            or contract['missing_coverage'] or (contract['runtime_reference_ready'] is not (not synthetic))):
        raise ValueError('C18-07 full catalog reference not ready; historical partial data cannot authorize runtime')
    same(contract['required_full_fields'],FULL_CATALOG_FIELDS,'full coverage cannot be weakened')
    reference=contract['reference_catalog']
    if set(reference)!=CATALOG_COLLECTIONS:raise ValueError('C18-07 full catalog collections missing')
    boolean_fields={'rls','forced_rls','validated','deferrable','initially_deferred','valid','ready','cycle','security_definer','strict','leakproof'}
    integer_fields={'position','typmod','start','min','max','increment','cache','type_bits'}
    list_fields={'columns','referenced_columns','configuration'}
    nullable_fields={'default','collation','referenced_table','fk_update_action','fk_delete_action','fk_match_type','owner_table','owner_column','ownership_dependency_kind'}
    for collection,fields in FULL_CATALOG_FIELDS.items():
        rows=reference[collection]
        if type(rows) is not list or not rows:raise ValueError('full declared catalog collection is absent: '+collection)
        for row in rows:
            exact(row,fields,'full catalog '+collection)
            for field,value in row.items():
                if field in boolean_fields and type(value) is not bool:raise ValueError('full catalog boolean required: '+field)
                elif field in integer_fields and type(value) is not int:raise ValueError('full catalog exact integer required: '+field)
                elif field in list_fields and (type(value) is not list or any(type(item) is not str for item in value)):raise ValueError('full catalog string list required: '+field)
                elif field not in boolean_fields|integer_fields|list_fields and not (type(value) is str or value is None and field in nullable_fields):raise ValueError('full catalog scalar required: '+field)
    return reference


def validate_binding(value,admission):
    for key in BINDING:same(value.get(key),admission[key],'cross-run '+key)


def _validate_admission(raw,pinned_sha256,contract_bundle=None):
    digest(pinned_sha256,'external admission pin')
    if type(raw) is not bytes or sha(raw)!=pinned_sha256:raise ValueError('independently supplied admission byte pin required')
    value=parse_json(raw)
    exact(value,BINDING|{'admission_schema_version','artifact_kind','not_before_unix_ns','not_after_unix_ns','approved_deadlines','approvals','qualification_receipts','previous_current_S_review_sha256','previous_current_S_review','parent_clock_descriptor_sha256'},'admission')
    if type(value['admission_schema_version']) is not int or value['admission_schema_version']!=1 or value['artifact_kind']!='r5_current18_external_admission_v1' or value['evidence_generation']!=GENERATION:raise ValueError('unknown current admission version')
    digest(value['attempt_id'],'attempt',32);digest(value['source_sha'],'source',40)
    if type(value['scale']) is not str or value['scale'] not in SCALES or value['source_policy']!=POLICY or value['source_identity_kind']!=SOURCE_KIND:raise ValueError('legacy/unknown/L admission refused')
    for key in BINDING-{'evidence_generation','attempt_id','scale','source_sha','source_policy','source_identity_kind','method_id'}:digest(value[key],key)
    contracts,contract_raw=load_contracts(value['method_id']) if contract_bundle is None else _validate_contract_bundle(value['method_id'],contract_bundle)
    for key in ['method','workload','catalog']:
        if value[key+'_contract_sha256']!=sha(contract_raw[key]):raise ValueError('admitted '+key+' contract mismatch')
    digest(value['parent_clock_descriptor_sha256'],'admitted exact parent descriptor pin')
    low=integer(value['not_before_unix_ns'],'admission earliest',1);high=integer(value['not_after_unix_ns'],'admission latest',1)
    if high<=low:raise ValueError('invalid admission time interval')
    deadlines=value['approved_deadlines']
    if type(deadlines) is not dict or not deadlines:raise ValueError('all approved owner deadlines required')
    for owner,deadline in deadlines.items():
        if owner not in {'outer_owner','controller','go_test'}:raise ValueError('unknown deadline owner')
        integer(deadline,'owner deadline',low+1)
    if 'outer_owner' not in deadlines:raise ValueError('outer owner deadline missing')
    exact(value['approvals'],{'owner','evi','root'},'three independent approval receipt hashes')
    for proof in value['approvals'].values():digest(proof,'approval receipt')
    if len(set(value['approvals'].values()))!=3:raise ValueError('owner/evi/root approvals require distinct receipts')
    exact(value['qualification_receipts'],QUALIFICATIONS,'unimplemented runtime qualification prerequisites')
    for proof in value['qualification_receipts'].values():digest(proof,'qualification receipt')
    if value['scale']=='M':
        digest(value['previous_current_S_review_sha256'],'new current S prerequisite')
        review=exact(value['previous_current_S_review'],{'artifact_kind','evidence_generation','scale','method_id','source_policy','source_identity_kind','source_sha','method_contract_sha256','workload_contract_sha256','catalog_contract_sha256','database_schema_version','archive_sha256','result_sha256','review_status','task_complete','product_green'},'new current S review')
        if sha(canonical(review))!=value['previous_current_S_review_sha256']:raise ValueError('new current S review digest differs')
        for key in ['evidence_generation','method_id','source_policy','source_identity_kind','source_sha','method_contract_sha256','workload_contract_sha256','catalog_contract_sha256']:same(review[key],value[key],'new current S prerequisite '+key)
        if review['artifact_kind']!='r5_current18_original_S_review_v1' or review['scale']!='S' or type(review['database_schema_version']) is not int or review['database_schema_version']!=18 or review['review_status']!='independent_source_bound_completed_S' or review['task_complete'] is not False or review['product_green'] is not False:raise ValueError('old known S cannot certify current M')
        digest(review['archive_sha256']);digest(review['result_sha256'])
    elif value['previous_current_S_review_sha256'] is not None or value['previous_current_S_review'] is not None:raise ValueError('unexpected previous-scale approval')
    return value,contracts,contract_raw


def validate_admission(raw,pinned_sha256):
    # Public actual-contract admission, not the private syntax inspector.
    value,contracts,contract_raw=_validate_admission(raw,pinned_sha256)
    require_full_catalog_reference(contracts['catalog'])
    return value,contracts,contract_raw


def _validate_clock_fields(clock,admission):
    exact(clock,BINDING|{'artifact_kind','clock_schema_version','origin_unix_ns','original_budget_seconds','approved_deadlines','effective_deadline_unix_ns','origin_before_native_init_and_compile','go_new_budget_origin_allowed'},'clock')
    validate_binding(clock,admission)
    if clock['artifact_kind']!='r5_current18_parent_clock_v1' or type(clock['clock_schema_version']) is not int or clock['clock_schema_version']!=1:raise ValueError('unknown parent clock version')
    origin=integer(clock['origin_unix_ns'],'parent origin',1)
    budget=integer(clock['original_budget_seconds'],'parent budget',1)
    if budget!=BUDGETS[admission['scale']][0] or not admission['not_before_unix_ns']<=origin<admission['not_after_unix_ns']:raise ValueError('original parent origin/budget invalid')
    same(clock['approved_deadlines'],admission['approved_deadlines'],'approved earlier deadlines')
    expected=min(origin+budget*NS,admission['not_after_unix_ns'],*clock['approved_deadlines'].values())
    if integer(clock['effective_deadline_unix_ns'],'effective deadline',1)!=expected or expected<=origin:raise ValueError('earliest deadline not retained')
    if clock['origin_before_native_init_and_compile'] is not True or clock['go_new_budget_origin_allowed'] is not False:raise ValueError('native init/compile cannot get a later budget origin')
    return origin,expected


def validate_clock(clock,admission,*,trusted_descriptor_raw,trusted_descriptor_sha256):
    digest(trusted_descriptor_sha256,'external parent descriptor pin')
    if type(trusted_descriptor_raw) is not bytes or sha(trusted_descriptor_raw)!=trusted_descriptor_sha256:
        raise ValueError('external parent descriptor byte pin differs')
    if trusted_descriptor_sha256!=admission['parent_clock_descriptor_sha256']:raise ValueError('external parent descriptor differs from admitted exact clock pin')
    descriptor=parse_json(trusted_descriptor_raw)
    if canonical(descriptor)!=trusted_descriptor_raw:raise ValueError('externally pinned parent descriptor must use canonical JSON bytes')
    if canonical(clock)!=trusted_descriptor_raw:raise ValueError('archive parent clock differs from independently trusted descriptor')
    return _validate_clock_fields(clock,admission)


def parser_admission_allowed(clock,admission,now_unix_ns,*,trusted_descriptor_raw,trusted_descriptor_sha256):
    origin,deadline=validate_clock(clock,admission,trusted_descriptor_raw=trusted_descriptor_raw,trusted_descriptor_sha256=trusted_descriptor_sha256)
    now=integer(now_unix_ns,'actual parser admission time',origin)
    return deadline-now>31*NS


def validate_catalog(attestation,admission,contract,*,require_match=True):
    exact(attestation,BINDING|{'artifact_kind','database_schema_version','origin','catalog','migration_sha256','restart_catalog_unchanged','qualification_receipt_sha256','server_version_num'},'catalog attestation')
    validate_binding(attestation,admission)
    if attestation['artifact_kind']!='r5_current18_catalog_attestation_v1' or attestation['origin']!='actual_owned_database_after_official_New_Migrate' or type(attestation['database_schema_version']) is not int or attestation['database_schema_version']<1 or attestation['restart_catalog_unchanged'] is not True:raise ValueError('actual current18 full catalog required')
    integer(attestation['server_version_num'],'PG version',1)
    if type(attestation['catalog']) is not dict or set(attestation['catalog'])!=CATALOG_COLLECTIONS or any(type(v) is not list for v in attestation['catalog'].values()):raise ValueError('full actual catalog collections required')
    if type(attestation['migration_sha256']) is not dict or not attestation['migration_sha256']:raise ValueError('actual migration hashes required')
    for name,value in attestation['migration_sha256'].items():path_name(name);digest(value)
    matched=(attestation['database_schema_version']==18 and canonical(attestation['catalog'])==canonical(contract['reference_catalog']) and canonical(attestation['migration_sha256'])==canonical(contract['migration_sha256']))
    if require_match and not matched:raise ValueError('full catalog definitions differ (counts are not proof)')
    if not require_match and matched:raise ValueError('catalog mismatch failure has matching full catalog')
    if attestation['qualification_receipt_sha256']!=admission['qualification_receipts']['C18-07']:raise ValueError('C18-07 full catalog qualification missing/mixed')


def validate_source(receipt,raw,admission,contract_raw):
    keys={'schema_version','snapshot_root','source_identity_kind','source_sha','source_closure_sha256','policy','purpose','build_context','replacement','boundary','excluded_directories','embed_inputs','files'}
    exact(receipt,keys,'source policy V2 receipt')
    if type(receipt['schema_version']) is not int or receipt['schema_version']!=2 or receipt['policy']!=POLICY or receipt['source_identity_kind']!=SOURCE_KIND or receipt['purpose']!='benchmark':raise ValueError('current benchmark source policy V2 required')
    if type(receipt['snapshot_root']) is not str or not PurePosixPath(receipt['snapshot_root']).is_absolute() or '..' in PurePosixPath(receipt['snapshot_root']).parts or str(PurePosixPath(receipt['snapshot_root']))!=receipt['snapshot_root']:raise ValueError('actual absolute snapshot root required')
    if sha(raw)!=admission['source_manifest_sha256'] or receipt['source_sha']!=admission['source_sha']:raise ValueError('source receipt not admitted')
    files=receipt['files']
    if type(files) is not dict or not files:raise ValueError('full local source mapping required')
    for name,hash_value in files.items():path_name(name);digest(hash_value)
    context=receipt['build_context']
    exact(context,{'goos','goarch','cgo_enabled','build_tag_sets','race','go_work','go_flags','selection'},'source context')
    if type(context['goos']) is not str or context['goos'] not in {'darwin','linux','windows'} or type(context['goarch']) is not str or context['goarch'] not in {'amd64','arm64'} or type(context['cgo_enabled']) is not int or context['cgo_enabled'] not in {0,1} or context['build_tag_sets']!=[['r5benchmark']] or context['race'] is not False or context['go_work']!='off' or context['go_flags']!='' or context['selection']!='all_local_variants_superset':raise ValueError('unknown source build context')
    same(receipt['replacement'],{'module':'github.com/jhillyerd/enmime/v2','version':'v2.3.0','path':'third_party/enmime-v2.3.0'},'exact fork replacement')
    required={'go.mod','go.sum','Dockerfile','internal/api/router.go','internal/api/openapi.yaml','scripts/r5_source_inventory.py','scripts/r5_current_benchmark_evidence.py','scripts/tests/test_r5_current_benchmark.py','scripts/contracts/r5-benchmark-current18-evidence.schema.json','third_party/enmime-v2.3.0/go.mod','third_party/enmime-v2.3.0/go.sum','third_party/enmime-v2.3.0/part.go','third_party/enmime-v2.3.0/parse_budget.go','third_party/enmime-v2.3.0/PROVENANCE.json','third_party/enmime-v2.3.0/UPSTREAM-MANIFEST.json','third_party/enmime-v2.3.0/UPSTREAM-DELTA.patch','third_party/enmime-v2.3.0/LICENSE'}
    if not required.issubset(files):raise ValueError('required source/fork metadata omitted')
    for key,name in [('method',METHOD_FILES[admission['method_id']]),('workload',WORKLOAD_FILE),('catalog',CATALOG_FILE)]:
        if files.get('scripts/contracts/'+name)!=sha(contract_raw[key]):raise ValueError('contract outside actual source closure')
    if type(receipt['boundary']) is not str or not receipt['boundary'] or type(receipt['excluded_directories']) is not list or any(type(name) is not str for name in receipt['excluded_directories']):raise ValueError('explicit source scope/exclusion boundary required')
    embeds=receipt['embed_inputs']
    if type(embeds) is not dict or not embeds:raise ValueError('actual embed inventory required')
    for directive,names in embeds.items():
        if ':' not in directive or directive.split(':',1)[0] not in files:raise ValueError('embed declaring Go source omitted')
        if type(names) is not list or not names or any(type(name) is not str or name not in files for name in names):raise ValueError('embed absent from source closure')
    if not any(name.startswith('internal/api/docsassets/') and name.endswith('.js') for names in embeds.values() for name in names) or not any(name.startswith('internal/api/docsassets/') and name.endswith('.css') for names in embeds.values() for name in names):raise ValueError('JS/CSS compiler embeds omitted')
    payload={key:value for key,value in receipt.items() if key not in {'schema_version','snapshot_root','source_identity_kind','source_sha','source_closure_sha256'}}
    wire=canonical(payload)
    if hashlib.sha1(wire).hexdigest()!=receipt['source_sha'] or sha(wire)!=receipt['source_closure_sha256']:raise ValueError('policy/context/source domain digest differs; not legacy files-only SHA1')


def validate_build(attestation,admission,source):
    exact(attestation,BINDING|{'artifact_kind','qualification_receipt_sha256','root_module','root_go_mod_sha256','build_context','selected_local_inputs','external_inputs_manifest_sha256','toolchain_binary_sha256','toolchain_version','compiled_binary_sha256','local_inventory_is_not_external_input_attestation'},'build attestation')
    validate_binding(attestation,admission)
    if attestation['artifact_kind']!='r5_current18_build_attestation_v1' or attestation['root_module']!='tabmail' or attestation['root_go_mod_sha256']!=source['files']['go.mod'] or attestation['qualification_receipt_sha256']!=admission['qualification_receipts']['C18-09'] or attestation['local_inventory_is_not_external_input_attestation'] is not True:raise ValueError('root-MVS/external-cache/toolchain qualification required')
    same(attestation['build_context'],source['build_context'],'actual compiler context')
    selected=attestation['selected_local_inputs']
    if type(selected) is not dict or not selected or any(source['files'].get(name)!=value for name,value in selected.items()):raise ValueError('selected compile input missing from local superset')
    if 'third_party/enmime-v2.3.0/part.go' not in selected or 'third_party/enmime-v2.3.0/parse_budget.go' not in selected or not any(name.startswith('internal/api/docsassets/') and name.endswith('.js') for name in selected) or not any(name.startswith('internal/api/docsassets/') and name.endswith('.css') for name in selected):raise ValueError('selected compilation omitted actual fork or embeds')
    for key in ['external_inputs_manifest_sha256','toolchain_binary_sha256','compiled_binary_sha256']:digest(attestation[key],key)
    if type(attestation['toolchain_version']) is not str or not re.fullmatch(r'go1\.\d+\.\d+',attestation['toolchain_version']):raise ValueError('exact toolchain version required')


# New-generation preparation grammar retains the original exact workload,
# joining and checkpoint invariants, with integer earliest-parent deadlines.
PREPARATION_COUNTS={'joined_inflight','parser_join_reserve_seconds','parser_joined_inflight','parser_joined_calls','parser_canceled_joined_calls','active_budget','pending_upper_bound','max_active','max_window_messages','joined_stored_messages','joined_completed_jobs','claim_calls','max_claim_batch','last_claim_batch','joined_ready_before_last_claim','sql_ready_count'}
PREPARATION_WALL={'parser_joined_call_wall_seconds_sum','company_wall_seconds','store_wall_seconds','claim_wall_seconds','last_claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds','max_wall_monotonic_gap_ms'}
PREPARATION_GUARD={'parser_min_start_remaining_ns','parser_deadline_unix_ns'}
PREPARATION_FIELDS=PREPARATION_COUNTS|PREPARATION_WALL|PREPARATION_GUARD|{'method','phase','producer_done','final_empty_claim','claim_query_sha256','claim_parameters_sha256','parser_join_policy','parser_quiescent'}
PARSER_JOIN_POLICY='uncancelled_document_wait_with_31s_earliest_parent_wall_admission_guard_v1'

def sha256_string(value):
 return type(value) is str and re.fullmatch("[0-9a-f]{64}",value) is not None

def require_integer_fields(value,fields):
 for key in fields:integer(value.get(key),key)

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
 if stats['method']!=LOOKAHEAD or stats['lookahead_limit']!=100 or any(stats[k]!=messages for k in ['generated_inputs','emitted_inputs','ordinal_fifo_completed_jobs']) or stats['joined_fifo_inflight']!=0 or not 1<=stats['max_unpersisted_buffered_inputs']<=100:raise ValueError('actual scheduler generated/emitted/FIFOcompleted N and joined buffer bounds required')
 elapsed=stats['fifo_gate_call_elapsed_seconds_sum']
 if type(elapsed) not in {int,float} or not math.isfinite(elapsed) or elapsed<0:raise ValueError('finite nonnegative actual FIFO gate call wall required')
 order=stats['actual_emitted_ordinal_order'];boxes=stats['actual_emitted_mailbox_ordinals'];windows=stats['actual_emitted_window_unique_mailboxes']
 if type(order) is not list or len(order)!=messages or any(type(n) is not int for n in order) or set(order)!=set(range(messages)):raise ValueError('actual emitted ordinals must be an exact integer permutation')
 if type(boxes) is not list or len(boxes)!=messages or any(type(n) is not int for n in boxes) or boxes!=[fixture_mailbox_ordinal(n,employees) for n in order]:raise ValueError('actual emitted mailbox ordinals must bind original generator mapping')
 if order!=expected_scheduler_order(messages,employees):raise ValueError('actual earliest ordinal head fairness and mailbox FIFO schedule required')
 unique=[len(set(boxes[i:i+20])) for i in range(0,messages,20)]
 if type(windows) is not list or len(windows)!=messages//20 or any(type(n) is not int or not 1<=n<=20 for n in windows) or windows!=unique:raise ValueError('actual emitted window mailbox diversity must match selected inputs')
 return stats

def preparation_depths(messages):
 return sorted({d for d in [20,1000,10000,100000,messages-20,*range(250000,messages,250000)] if 0<d<messages})

def validate_preparation_stats(stats,messages,phase,depth,*,preparation_method=BOUNDED):
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
 if any(type(stats[k]) not in {int,float} or not math.isfinite(stats[k]) or stats[k]<=0 for k in PREPARATION_GUARD) or stats['parser_min_start_remaining_ns']<=31*NS:raise ValueError('actual minimum parse-start remaining original wall must exceed31s')
 for field in PREPARATION_GUARD:integer(stats[field],field,1)
 return stats

def validate_preparation(result,messages,scale,expected_source,*,preparation_method=BOUNDED,clock):
 terminal=validate_preparation_stats(result.get('preparation'),messages,'terminal',messages,preparation_method=preparation_method)
 budget=(clock['effective_deadline_unix_ns']-clock['origin_unix_ns'])/NS
 if terminal['parser_deadline_unix_ns']!=clock['effective_deadline_unix_ns']:raise ValueError('Parser deadline differs from earliest parent deadline')
 if terminal['company_wall_seconds']+terminal['pipeline_wall_seconds']>budget:raise ValueError('preparation exceeds original absolute wall budget')
 if terminal['parser_min_start_remaining_ns']>budget*NS:raise ValueError('actual parse admission cannot invent more than original wall budget')
 checkpoints=result.get('preparation_checkpoints');depths=preparation_depths(messages)
 if type(checkpoints) is not list or len(checkpoints)!=len(depths)+1:raise ValueError('all actual deep-ready and terminal preparation checkpoints required')
 fields={'attempt_id','method_id','parent_clock_sha256','origin','phase','source_sha','database_schema_version','maintenance','claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1','query_sha256','parameters_sha256','explain_only_not_actual_claim_latency','plan','preparation'}
 countfields={'database_schema_version','claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1'}
 previous=None;query=None
 for index,checkpoint in enumerate(checkpoints):
  final=index==len(depths);depth=messages if final else depths[index];phase='terminal' if final else 'before_claim'
  if type(checkpoint) is not dict or set(checkpoint)!=fields|({'actual_claim_joined'} if not final else set()):raise ValueError('complete known paired preparation checkpoint fields required')
  require_integer_fields(checkpoint,countfields)
  if checkpoint['origin']!='captured_preparation_claim_same_parameters_observed_heap' or checkpoint['phase']!=phase or checkpoint['source_sha']!=expected_source or checkpoint['database_schema_version']!=result['database_schema_version'] or checkpoint['maintenance']!='no_explicit_analyze_or_planner_or_durability_override' or checkpoint['explain_only_not_actual_claim_latency'] is not True:raise ValueError('actual preparation trace/source/schema/no-maintenance boundary required')
  for key in ['attempt_id','method_id','parent_clock_sha256']:same(checkpoint[key],result[key],'checkpoint cross-run '+key)
  expected=(20,messages if final else depth+20,0 if final else 20,0,depth,depth,0,0)
  if tuple(checkpoint[k] for k in ['claim_requested_limit','observed_jobs_total','observed_pending','observed_processing','observed_ready_jobs','observed_durable_source_bound_ready','observed_failed','observed_attempts_gt1'])!=expected:raise ValueError('actual source-bound heap/backlog/attempt observations inconsistent')
  if not sha256_string(checkpoint['query_sha256']) or not sha256_string(checkpoint['parameters_sha256']) or checkpoint['parameters_sha256']!=hashlib.sha256(b'[20]').hexdigest():raise ValueError('captured actual legal20 claim/parameter digests required')
  if query is not None and checkpoint['query_sha256']!=query:raise ValueError('preparation claim query changed across checkpoints')
  query=checkpoint['query_sha256'];validate_explain_plan(checkpoint['plan'],'mail_index_jobs')
  before=validate_preparation_stats(checkpoint['preparation'],messages,phase,depth,preparation_method=preparation_method)
  if before['parser_deadline_unix_ns']!=terminal['parser_deadline_unix_ns'] or before['parser_min_start_remaining_ns']>budget*NS:raise ValueError('preparation must observe one unchanged original deadline')
  if (before['claim_query_sha256'],before['claim_parameters_sha256'])!=(query,checkpoint['parameters_sha256']):raise ValueError('preparation EXPLAIN does not pair with captured actual claim digests')
  if previous is not None and any(before[k]<previous[k]-1e-9 for k in ['company_wall_seconds','store_wall_seconds','claim_wall_seconds','parse_complete_wall_seconds','pipeline_wall_seconds','max_wall_monotonic_gap_ms','parser_joined_call_wall_seconds_sum']):raise ValueError('preparation checkpoint cumulative wall regressed')
  if previous is not None and before['parser_min_start_remaining_ns']>previous['parser_min_start_remaining_ns']:raise ValueError('actual cumulative minimum remaining wall increased')
  if final:
   if before!=terminal:raise ValueError('terminal preparation differs from observed final SQL checkpoint')
   if previous is None or abs(before['claim_wall_seconds']-previous['claim_wall_seconds']-before['last_claim_wall_seconds'])>1e-9 or any(before[k]!=previous[k] for k in ['joined_stored_messages','joined_completed_jobs','store_wall_seconds','parse_complete_wall_seconds','parser_joined_calls','parser_joined_call_wall_seconds_sum','parser_min_start_remaining_ns']):raise ValueError('actual terminal empty claim must follow joined final window without new work')
   previous=before
  else:
   joined=validate_preparation_stats(checkpoint['actual_claim_joined'],messages,'batch_joined',depth,preparation_method=preparation_method)
   if joined['parser_deadline_unix_ns']!=terminal['parser_deadline_unix_ns'] or joined['parser_min_start_remaining_ns']>before['parser_min_start_remaining_ns']:raise ValueError('joined Parser actual original-deadline/minimum observation inconsistent')
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

RESULT_FIELDS = BINDING|{'artifact_kind','result_schema_version','outcome','parent_clock_sha256','database_schema_version','catalog_attestation_sha256','build_attestation_sha256','employees','mailboxes','messages','seed','concurrency','pool_size','dataset_fingerprint','parameter_fingerprint','fingerprint_method','fingerprint_streams','actual_distribution','preparation','preparation_checkpoints','preparation_input_scheduler','preparation_persistent_observations','identity_boundary','sql_plans','sql_tracer_calibration_count','sql_tracer_calibration_expected','hardware','resource_scope','system_memory_observation','safety_observations','measurements','execution','task_complete','product_green'}
PHASES = ['native_init_started_ns', 'native_init_completed_ns', 'compile_started_ns', 'go_started_ns', 'go_init_completed_ns', 'population_started_ns', 'population_completed_ns', 'measurements_started_ns', 'measurements_completed_ns', 'observations_completed_ns', 'drain_completed_ns', 'result_sealed_ns', 'go_exit_observed_ns', 'cleanup_completed_ns', 'parent_wait_completed_ns']


def distribution(size):
    employees,mailboxes,messages=size
    return {'employees':employees,'mailboxes':mailboxes,'messages':messages,'index_ready_count':messages,'personal_mailboxes':employees,'shared_mailboxes':employees*4,'shared_grant_rows':employees*16,'min_distinct_shared_grantees':4,'max_distinct_shared_grantees':4,'lifecycle_counts':{'inbox':messages*80//100,'archived':messages*10//100,'trash_expired':messages*5//100,'hard_expired':messages*5//100},'mime_size_counts':{'4096':messages*800//1000,'32768':messages*180//1000,'262144':messages*19//1000,'2097152':messages//1000},'tenant_message_counts':{'bench-0.test':messages*80//100,'bench-1.test':messages//5}}


def validate_result(result,admission,contracts,clock,catalog_raw,build_raw):
    """Observation subgrammar only; never source/catalog/runtime admission."""
    exact(result,RESULT_FIELDS,'current completed result');validate_binding(result,admission)
    if result['artifact_kind']!='r5_current18_result_v1' or type(result['result_schema_version']) is not int or result['result_schema_version']!=1 or result['outcome']!='completed' or result['task_complete'] is not False or result['product_green'] is not False:raise ValueError('result format cannot certify task/product')
    if type(result['database_schema_version']) is not int or result['database_schema_version']!=18:raise ValueError('current DB18 required, not JSON schema version')
    if result['parent_clock_sha256']!=sha(canonical(clock)) or result['catalog_attestation_sha256']!=sha(catalog_raw) or result['build_attestation_sha256']!=sha(build_raw):raise ValueError('result clock/catalog/build cross-run binding differs')
    size=SCALES[admission['scale']];employees,mailboxes,messages=size;work=contracts['workload']
    for key in ['employees','mailboxes','messages','seed','concurrency','pool_size','sql_tracer_calibration_count','sql_tracer_calibration_expected']:integer(result[key],key,1)
    if tuple(result[k] for k in ['employees','mailboxes','messages'])!=size or (result['seed'],result['concurrency'],result['pool_size'],result['sql_tracer_calibration_count'],result['sql_tracer_calibration_expected'])!=(3893945,20,24,20,20):raise ValueError('original population/C20/pool24/SQL20 changed')
    same(result['actual_distribution'],distribution(size),'actual population/lifecycle/ACL/MIME observations')
    validate_fingerprint_streams(result,size)
    digest(result['dataset_fingerprint']);digest(result['parameter_fingerprint'])
    expected_parameter=sha(canonical({'seed':3893945,'population':list(size)}))
    if result['parameter_fingerprint']!=expected_parameter or result['dataset_fingerprint']==result['parameter_fingerprint']:raise ValueError('actual dataset stream cannot be a parameter echo')
    same(result['resource_scope'],work['resource_scope'],'resource/cold-cache boundaries')
    same(result['identity_boundary'],contracts['method']['identity_boundary'],'persistent identity non-equivalence')
    if result['system_memory_observation']!='not_measured':raise ValueError('unmeasured system memory cannot be certified')
    hardware=exact(result['hardware'],{'os','arch','cpu'},'hardware')
    if any(type(hardware[k]) is not str or not hardware[k] for k in ['os','arch']):raise ValueError('actual hardware names required')
    integer(hardware['cpu'],'logical CPU',1)
    validate_preparation(result,messages,admission['scale'],admission['source_sha'],preparation_method=admission['method_id'],clock=clock)
    if result['preparation']['claim_query_sha256']!=work['pipeline']['claim_query_sha256'] or result['preparation']['claim_parameters_sha256']!=work['pipeline']['claim_parameters_sha256']:raise ValueError('actual Claim20/lease90/attempt5 query differs from source contract')
    if admission['method_id']==LOOKAHEAD:
        validate_input_scheduler(result,messages,employees)
        obs=exact(result['preparation_persistent_observations'],{'attempt_id','method_id','parent_clock_sha256','source_sha','database_schema_version','origin','uid_column_count','projected_messages','projected_mailboxes','ordinal_fifo_violations','index_ready_count','source_identity_bindings_valid','uid_catalog_query_sha256','received_at_projection_query_sha256'},'persistent observations')
        for key in ['attempt_id','method_id','parent_clock_sha256','source_sha','database_schema_version']:same(obs[key],result[key],'persistent cross-run '+key)
        for key in ['uid_column_count','projected_messages','projected_mailboxes','ordinal_fifo_violations','index_ready_count']:integer(obs[key],key)
        if obs['origin']!='actual_owned_pg_catalog_and_received_at_projection' or obs['source_identity_bindings_valid'] is not True or (obs['projected_messages'],obs['projected_mailboxes'],obs['ordinal_fifo_violations'],obs['index_ready_count'])!=(messages,len({fixture_mailbox_ordinal(n,employees) for n in range(messages)}),0,messages):raise ValueError('actual FIFO/UID PostgreSQL observations required')
        expected_queries={'uid_catalog_query_sha256':'2de36ca227e3140de0f200ef3e10f207d9f917061469b8b7047d2c520de14ba8','received_at_projection_query_sha256':'d56b9930a294ee2b4a00ce7ffd95e0871fc2049f474bc08900d78071943a0a9d'}
        for key,value in expected_queries.items():same(obs[key],value,'actual persistent query')
    elif result['preparation_input_scheduler'] is not None or result['preparation_persistent_observations'] is not None:raise ValueError('bounded method cannot auto-select lookahead')
    plans=result['sql_plans']
    if type(plans) is not list or not plans:raise ValueError('actual shipping SQL plans required')
    for plan in plans:
        exact(plan,{'attempt_id','method_id','parent_clock_sha256','origin','workload','scale','source_sha','database_schema_version','index_ready_count','query_sha256','parameters_sha256','plan'},'shipping SQL plan')
        for key in ['attempt_id','method_id','parent_clock_sha256','scale','source_sha','database_schema_version']:same(plan[key],result[key],'SQL plan cross-run '+key)
        if plan['origin']!='captured_shipping_query_same_parameters' or type(plan['workload']) is not str or plan['workload'] not in WORKLOADS or type(plan['index_ready_count']) is not int or plan['index_ready_count']!=messages:raise ValueError('shipping SQL population/provenance differs')
        digest(plan['query_sha256']);digest(plan['parameters_sha256']);validate_explain_plan(plan['plan'],'messages')
    if not any(plan['workload']=='inbox_list' for plan in plans):raise ValueError('shipping inbox plan missing')
    safety=result['safety_observations']
    if type(safety) is not list or len(safety)!=7:raise ValueError('all seven actual shipping safety observations required')
    seen=set()
    for item in safety:
        exact(item,{'workload','origin','foreign_positive_http','foreign_denied_http','revoked_before_http','revoked_after_http','restored_http'},'shipping safety')
        if type(item['workload']) is not str or item['workload'] not in WORKLOADS or item['workload'] in seen or item['origin']!='actual_shipping_HTTP_and_JWT':raise ValueError('unknown/duplicate/nonshipping safety observation')
        seen.add(item['workload'])
        same([item[k] for k in ['foreign_positive_http','foreign_denied_http','revoked_before_http','revoked_after_http','restored_http']],[200,404,200,403,200],'actual positive/foreign/revoked safety statuses')
    rows=result['measurements']
    if type(rows) is not list or len(rows)!=14:raise ValueError('all 7x2 workload/cache rows required')
    pairs=set()
    for row in rows:
        exact(row,{'workload','cache_mode','samples','sql_count','p50_ms','p95_ms','p99_ms','alloc_bytes','rss_bytes','disk_bytes','max_wall_monotonic_gap_ms','safety_assertion'},'measurement')
        if type(row['workload']) is not str or type(row['cache_mode']) is not str:raise ValueError('string workload/cache identifiers required')
        pair=(row['workload'],row['cache_mode'])
        if pair in pairs or pair[0] not in WORKLOADS or pair[1] not in CACHES:raise ValueError('duplicate/unknown workload-cache pair')
        pairs.add(pair)
        if integer(row['samples'],'sample count',1)!=200:raise ValueError('exact original 200 samples required')
        for key in ['sql_count','rss_bytes','disk_bytes']:integer(row[key],key,1)
        integer(row['alloc_bytes'],'alloc bytes')
        for key in ['p50_ms','p95_ms','p99_ms','max_wall_monotonic_gap_ms']:number(row[key],key)
        if not row['p50_ms']<=row['p95_ms']<=row['p99_ms'] or row['max_wall_monotonic_gap_ms']>100 or row['disk_bytes']>BUDGETS[admission['scale']][1]*(1<<30):raise ValueError('metric clock/order/disk budget violated')
        if row['safety_assertion']!=row['workload']+':foreign_and_revoked_current_source_denied':raise ValueError('every shipping workload requires positive/foreign/revoked source safety')
    execution=exact(result['execution'],{'go_started_ns','observations_completed_ns','max_wall_monotonic_gap_ms'},'result execution')
    origin,deadline=_validate_clock_fields(clock,admission)
    start=integer(execution['go_started_ns'],'actual Go start',origin);end=integer(execution['observations_completed_ns'],'actual observations end',start)
    if end>=deadline or number(execution['max_wall_monotonic_gap_ms'],'run clock gap')>100:raise ValueError('result late or interrupted under original parent deadline')
    if result['preparation']['company_wall_seconds']+result['preparation']['pipeline_wall_seconds']>(end-start)/NS+0.001:raise ValueError('preparation wall escapes actual Go execution interval')
    latest=deadline-result['preparation']['parser_min_start_remaining_ns']
    if not start<=latest<=end:raise ValueError('observed Parser admission escapes actual Go/result interval')
    # Candidate threshold classification is separate from complete measurements.
    list_limit=300 if admission['scale']=='S' else 500
    candidate=all(row['p95_ms']<=list_limit for row in rows if row['workload']=='inbox_list')
    if admission['scale']=='M':candidate=candidate and all(row['p95_ms']<=1000 for row in rows if row['workload'] in {'indexed_search','indexed_message'})
    if admission['scale']=='tool_only':candidate=None
    return {'candidate_threshold_passed':candidate,'candidate_threshold_scope':'not_applicable_tool_only' if admission['scale']=='tool_only' else 'original_scale_absolute_candidates_only','relative_regression_certified':False,'task_complete':False,'product_green':False}


def validate_process(process,cleanup,admission,clock,*,success):
    exact(process,BINDING|{'artifact_kind','parent_clock_sha256','origin_unix_ns','effective_deadline_unix_ns','phase_times','go_exit','runner_exit','exit_observation','timed_out','clock_reversed','child_deadline_unix_ns'},'process receipt')
    validate_binding(process,admission)
    if process['artifact_kind']!='r5_current18_process_receipt_v1' or process['parent_clock_sha256']!=sha(canonical(clock)):raise ValueError('process clock version/binding differs')
    origin,deadline=_validate_clock_fields(clock,admission)
    same(process['origin_unix_ns'],origin,'parent process origin');same(process['effective_deadline_unix_ns'],deadline,'parent process deadline')
    if process['child_deadline_unix_ns'] is None:
        if success:raise ValueError('unknown child deadline cannot certify success')
    elif integer(process['child_deadline_unix_ns'],'child deadline',1)<deadline:raise ValueError('earlier child deadline omitted from approved clock')
    for key in ['timed_out','clock_reversed']:
        if type(process[key]) is not bool:raise ValueError('explicit process clock states required')
    for key in ['go_exit','runner_exit']:
        if process[key] is not None and type(process[key]) is not int:raise ValueError('known exits must be exact integers')
    if process['go_exit'] is None and process['exit_observation']!='unknown_not_observed':raise ValueError('unknown exit must retain unknown observation status')
    if process['go_exit'] is not None and process['exit_observation']!='observed_parent_wait':raise ValueError('unknown exit observation cannot carry a numeric Go exit')
    times=exact(process['phase_times'],PHASES,'phase times')
    last=origin
    for key in PHASES:
        value=times[key]
        if value is None:
            if success:raise ValueError('success requires all init/compile/drain/result/cleanup/wait observations')
        else:
            last=integer(value,'phase '+key,last)
            if success and last>=deadline:raise ValueError('late cleanup/parent wait cannot be success')
    exact(cleanup,BINDING|{'artifact_kind','parent_clock_sha256','completed_ns','parser_joined_inflight','worker_joined_inflight','readers_open','owned_connections','owned_db_absent','owned_pg_stopped','owned_processes_absent','owned_objects_absent','containment_only'},'cleanup receipt')
    validate_binding(cleanup,admission)
    if cleanup['artifact_kind']!='r5_current18_cleanup_receipt_v1' or cleanup['parent_clock_sha256']!=sha(canonical(clock)):raise ValueError('cleanup source/clock identity differs')
    for key in ['parser_joined_inflight','worker_joined_inflight','readers_open','owned_connections']:
        if cleanup[key] is not None:integer(cleanup[key],key)
        if success and cleanup[key]!=0:raise ValueError('success requires actual join/readers/connections zero')
    for key in ['owned_db_absent','owned_pg_stopped','owned_processes_absent','owned_objects_absent','containment_only']:
        if cleanup[key] is not None and type(cleanup[key]) is not bool:raise ValueError('explicit cleanup boolean/unknown required')
    if cleanup['completed_ns'] is not None:integer(cleanup['completed_ns'],'cleanup completion',origin)
    if success:
        if process['go_exit']!=0 or process['runner_exit']!=0 or process['exit_observation']!='observed_parent_wait' or process['timed_out'] is not False or process['clock_reversed'] is not False:raise ValueError('success requires actual known exits and uninterrupted budget')
        if any(cleanup[key] is not True for key in ['owned_db_absent','owned_pg_stopped','owned_processes_absent','owned_objects_absent']) or cleanup['containment_only'] is not False or cleanup['completed_ns']!=times['cleanup_completed_ns']:raise ValueError('success cleanup incomplete/unknown/containment-only')
    elif process['exit_observation'] not in {'observed_parent_wait','unknown_not_observed'}:raise ValueError('unknown failure exit-observation version')


def event_time_ns(value):
    if type(value) is not str:raise ValueError('raw Go event timestamp required')
    match=re.fullmatch(r'(\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d)(?:\.([0-9]{1,9}))?(Z|[+-]\d\d:\d\d)',value)
    if not match:raise ValueError('invalid raw Go timestamp')
    base,fraction,zone=match.groups()
    stamp=datetime.fromisoformat(base+('+00:00' if zone=='Z' else zone))
    return int(stamp.timestamp())*NS+int((fraction or '').ljust(9,'0'))


def validate_events(raw,admission,*,success,clock,process):
    expected='TestR5BenchmarkCurrent18ToolOnlyWorkloads' if admission['scale']=='tool_only' else 'TestR5BenchmarkCurrent18OriginalScale'
    states=[];packages=[];output=[];previous=None;actual_order=[];starts={}
    for line in raw.decode().splitlines():
        if not line.strip():continue
        event=parse_json(line)
        if type(event) is not dict or set(event)-{'Time','Action','Package','Test','Output','Elapsed','FailedBuild'}:raise ValueError('unknown Go event fields')
        if 'FailedBuild' in event and (success or type(event['FailedBuild']) is not str or not event['FailedBuild']):raise ValueError('failed-build metadata cannot certify success')
        if event.get('Package')!='tabmail/internal/store/postgres':raise ValueError('foreign Go package in current archive')
        action=event.get('Action');test=event.get('Test')
        observed=event_time_ns(event.get('Time'))
        if observed<clock['origin_unix_ns'] or previous is not None and observed<previous:raise ValueError('raw Go timestamp before original origin or reversed')
        previous=observed
        if success and (observed>=clock['effective_deadline_unix_ns'] or observed>process['phase_times']['parent_wait_completed_ns']):raise ValueError('raw Go terminal/output escapes original parent budget')
        # test2json Time is the recorded event timestamp, not an inferred
        # harness-start time. Every successful stream event is bounded by the
        # explicitly observed owned-Go phase, never by later cleanup/wait time.
        go_start=process['phase_times']['go_started_ns'];go_exit=process['phase_times']['go_exit_observed_ns']
        if success and not go_start<=observed<=go_exit:raise ValueError('raw Go event outside declared Go start/exit phase')
        if success and observed>=process['phase_times']['cleanup_completed_ns']:raise ValueError('raw Go event overlaps completed cleanup')
        if type(action) is not str or action not in {'start','run','pass','fail','skip','output'}:raise ValueError('unknown Go lifecycle action')
        if 'Test' in event and (type(test) is not str or not test):raise ValueError('explicit Go Test must be a nonempty name')
        if test and test!=expected:raise ValueError('unexpected current test/subtest selector')
        if action=='output':
            if type(event.get('Output')) is not str:raise ValueError('raw output string required')
            output.append(event['Output'])
        elif test and action in {'run','pass','fail','skip'}:
            states.append(action);actual_order.append(('test',action))
        elif not test and action in {'start','pass','fail','skip'}:
            packages.append(action);actual_order.append(('package',action))
        owner='test' if test else 'package'
        if action in {'start','run'}:starts[owner]=observed
        if 'Elapsed' in event:
            elapsed=number(event['Elapsed'],'Go elapsed')
            if success and elapsed>(clock['effective_deadline_unix_ns']-clock['origin_unix_ns'])/NS:raise ValueError('raw Go elapsed exceeds original parent budget')
            if success and action=='pass' and (owner not in starts or elapsed>(observed-starts[owner])/NS+0.1):raise ValueError('raw Go elapsed exceeds its recorded lifecycle interval')
        if success and action=='pass' and 'Elapsed' not in event:raise ValueError('actual terminal elapsed missing')
    if success and (states!=['run','pass'] or packages!=['start','pass'] or actual_order!=[('package','start'),('test','run'),('test','pass'),('package','pass')] or any(word in ''.join(output) for word in ['WARNING: DATA RACE','panic:','fatal error:','test timed out','[build failed]'])):raise ValueError('exact actual Go run/pass/package pass required')
    return states,packages,''.join(output)


def _scope_verdict(result,synthetic):
    if synthetic:
        result.update(status='synthetic_'+result['status'],runtime_qualification=False,boundary='Synthetic declared-full-reference grammar fixture only; no runtime, SOURCE, catalog, or execution qualification')
    return result


def _validate_archive_structure(index,members,*,admission_raw,admission_sha256,trusted_descriptor_raw,trusted_descriptor_sha256,contract_bundle=None,synthetic=False):
    """Validate one externally admitted bundle, not ownership or runtime itself.

    Archive extraction is not implemented. Caller supplies exact member bytes;
    no arbitrary files, tar extraction, subprocesses, network or live PG access.
    """
    admission,contracts,contract_raw=_validate_admission(admission_raw,admission_sha256,contract_bundle)
    require_full_catalog_reference(contracts['catalog'],synthetic=synthetic)
    exact(index,BINDING|{'artifact_kind','archive_schema_version','outcome','members','admission_sha256','task_complete','product_green'},'archive index')
    validate_binding(index,admission)
    if index['artifact_kind']!='r5_current18_benchmark_archive_v1' or type(index['archive_schema_version']) is not int or index['archive_schema_version']!=1 or type(index['outcome']) is not str or index['outcome'] not in {'success','failed'} or index['task_complete'] is not False or index['product_green'] is not False or index['admission_sha256']!=admission_sha256:raise ValueError('unknown/mixed archive version or approval claim')
    if type(members) is not dict or type(index['members']) is not dict or set(members)!=set(index['members']):raise ValueError('archive member set missing/extra')
    for name,raw in members.items():
        path_name(name)
        if type(raw) is not bytes or sha(raw)!=digest(index['members'][name],'member hash'):raise ValueError('archive member bytes differ')
    success=index['outcome']=='success'
    required=REQUIRED_MEMBERS|({'result.json'} if success else {'failure.json'})
    if not success and 'result.json' in members:required.add('result.json')
    if set(members)!=required:raise ValueError('known archive layout required; no extra or missing members')
    parsed={name:parse_json(raw) for name,raw in members.items() if name.endswith('.json')}
    for kind,name in [('method','method.json'),('workload','workload.json'),('catalog','catalog-contract.json')]:
        if members[name]!=contract_raw[kind]:raise ValueError('archived contract is not immutable admitted current version')
    clock=parsed['clock.json']
    validate_clock(clock,admission,trusted_descriptor_raw=trusted_descriptor_raw,trusted_descriptor_sha256=trusted_descriptor_sha256)
    if members['clock.json']!=trusted_descriptor_raw:raise ValueError('archive clock bytes differ from external descriptor')
    before=parsed['source-before.json'];validate_source(before,members['source-before.json'],admission,contract_raw)
    after=parsed['source-after.json']
    if type(after) is not dict:raise ValueError('explicit after-source receipt required')
    after_admission=dict(admission,source_manifest_sha256=sha(members['source-after.json']),source_sha=after.get('source_sha'))
    validate_source(after,members['source-after.json'],after_admission,contract_raw)
    if success and members['source-before.json']!=members['source-after.json']:raise ValueError('source drift before/after success')
    failure=parsed.get('failure.json')
    reason=failure.get('reason') if type(failure) is dict else None
    for kind in ['build','catalog']:
        observation=parsed[kind+'-attestation.json']
        if type(observation) is not dict:raise ValueError('explicit prerequisite observation object required')
        if observation.get('artifact_kind')=='r5_current18_unavailable_observation_v1':
            exact(observation,BINDING|{'artifact_kind','observation','status','reason','parent_clock_sha256'},'explicit unavailable observation')
            validate_binding(observation,admission)
            if success or reason not in {'compile_failure','runtime_assertion_failure','unknown_exit_observation','absolute_deadline_exceeded'} or observation['observation']!=kind or observation['status']!='not_observed' or observation['reason']!=reason or observation['parent_clock_sha256']!=sha(canonical(clock)):raise ValueError('unavailable prerequisite cannot certify success')
        elif kind=='build':validate_build(observation,admission,before)
        else:validate_catalog(observation,admission,contracts['catalog'],require_match=reason!='catalog_mismatch')
    process=parsed['process.json'];cleanup=parsed['cleanup.json']
    validate_process(process,cleanup,admission,clock,success=success)
    command=parsed['command.json']
    exact(command,BINDING|{'artifact_kind','parent_clock_sha256','argv','environment'},'command receipt');validate_binding(command,admission)
    test='TestR5BenchmarkCurrent18ToolOnlyWorkloads' if admission['scale']=='tool_only' else 'TestR5BenchmarkCurrent18OriginalScale'
    expected=['go','test','-mod=readonly','-json','-count=1','-tags=r5benchmark','-timeout='+str(BUDGETS[admission['scale']][0])+'s','./internal/store/postgres','-run','^'+test+'$']
    if command['artifact_kind']!='r5_current18_command_v1' or command['parent_clock_sha256']!=sha(canonical(clock)) or command['argv']!=expected:raise ValueError('exact current command selector/budget required')
    environment={'TABMAIL_R5_CURRENT_GENERATION':GENERATION,'TABMAIL_R5_CURRENT_ATTEMPT_ID':admission['attempt_id'],'TABMAIL_R5_CURRENT_METHOD_ID':admission['method_id'],'TABMAIL_R5_CURRENT_METHOD_SHA256':admission['method_contract_sha256'],'TABMAIL_R5_CURRENT_SOURCE_SHA':admission['source_sha'],'TABMAIL_R5_CURRENT_PARENT_CLOCK_SHA256':sha(canonical(clock)),'GOWORK':'off','GOFLAGS':''}
    same(command['environment'],environment,'nonsecret execution selection environment')
    states,packages,output=validate_events(members['go.jsonl'],admission,success=success,clock=clock,process=process)
    if success:
        result=parsed['result.json']
        verdict=validate_result(result,admission,contracts,clock,members['catalog-attestation.json'],members['build-attestation.json'])
        for key in ['go_started_ns','observations_completed_ns']:same(result['execution'][key],process['phase_times'][key],'result/process clock')
        same([result['hardware']['os'],result['hardware']['arch']],[before['build_context']['goos'],before['build_context']['goarch']],'runtime platform versus admitted build context')
        if any(word in members['go.stderr'].decode() for word in ['panic:','fatal error:','[build failed]']):raise ValueError('successful archive has infrastructure stderr')
        return _scope_verdict(dict(verdict,status='current_bundle_structure_validated',validated_scale=admission['scale'],boundary='External admission and source-bound raw grammar only; runtime execution/qualification and registry ownership remain separate'),synthetic)
    failure=parsed['failure.json']
    exact(failure,BINDING|{'artifact_kind','failure_schema_version','parent_clock_sha256','reason','observed_at_ns','result_present','task_complete','product_green'},'known failed attempt')
    validate_binding(failure,admission)
    if failure['artifact_kind']!='r5_current18_attempt_failure_v1' or type(failure['failure_schema_version']) is not int or failure['failure_schema_version']!=1 or type(failure['reason']) is not str or failure['reason'] not in FAILURES or failure['parent_clock_sha256']!=sha(canonical(clock)) or failure['task_complete'] is not False or failure['product_green'] is not False or type(failure['result_present']) is not bool or failure['result_present']!=('result.json' in members):raise ValueError('unknown/false failed attempt grammar')
    observed=integer(failure['observed_at_ns'],'failure observed time',clock['origin_unix_ns'])
    reason=failure['reason']
    if reason=='absolute_deadline_exceeded' and not (observed>=clock['effective_deadline_unix_ns'] and process['timed_out'] is True):raise ValueError('deadline failure lacks actual parent deadline observation')
    if reason=='unknown_exit_observation' and not (process['go_exit'] is None and process['exit_observation']=='unknown_not_observed'):raise ValueError('unknown exit cannot be fabricated')
    if reason=='source_drift' and members['source-before.json']==members['source-after.json']:raise ValueError('source drift failure has identical source receipts')
    if reason=='compile_failure' and not (process['go_exit'] not in {0,None} and 'run' not in states and ('fail' in packages or '[build failed]' in output)):raise ValueError('compile failure requires actual failed build without test execution')
    if reason=='runtime_assertion_failure' and not (states==['run','fail'] and process['go_exit'] not in {0,None}):raise ValueError('runtime failure requires actual run/fail')
    if process['runner_exit']==0:raise ValueError('failed attempt cannot carry successful runner exit')
    if reason=='cleanup_unverified' and all(cleanup[key] is True for key in ['owned_db_absent','owned_pg_stopped','owned_processes_absent','owned_objects_absent']) and cleanup['containment_only'] is False and cleanup['completed_ns'] is not None and cleanup['completed_ns']<clock['effective_deadline_unix_ns'] and all(cleanup[key]==0 for key in ['parser_joined_inflight','worker_joined_inflight','readers_open','owned_connections']):raise ValueError('cleanup failure has no incomplete/late cleanup observation')
    if reason=='catalog_mismatch' and ('pass' in states or process['go_exit'] in {0,None}):raise ValueError('catalog mismatch requires actual rejecting Go observation')
    if 'result.json' in parsed:
        orphan=exact(parsed['result.json'],RESULT_FIELDS,'orphan result');validate_binding(orphan,admission)
        if orphan['artifact_kind']!='r5_current18_result_v1' or type(orphan['result_schema_version']) is not int or orphan['result_schema_version']!=1 or orphan['task_complete'] is not False or orphan['product_green'] is not False:raise ValueError('orphan result cannot claim approval or unknown version')
    return _scope_verdict({'status':'current_failed_bundle_structure_validated','failure_reason':reason,'baseline_complete':False,'task_complete':False,'product_green':False,'boundary':'Known-version failed attempt only; orphan result is not validated measurements or baseline'},synthetic)


def validate_archive(index,members,*,admission_raw,admission_sha256,trusted_descriptor_raw,trusted_descriptor_sha256):
    """Actual-contract path: historical partial catalog remains fail-closed.

    Both trust pins come from outside the archive. This function has no fixture
    override and cannot upgrade readiness with a qualification receipt hash.
    """
    return _validate_archive_structure(index,members,admission_raw=admission_raw,admission_sha256=admission_sha256,trusted_descriptor_raw=trusted_descriptor_raw,trusted_descriptor_sha256=trusted_descriptor_sha256)


def validate_synthetic_archive(index,members,*,admission_raw,admission_sha256,trusted_descriptor_raw,trusted_descriptor_sha256,synthetic_contract_bundle):
    """Explicit grammar laboratory, NEVER the actual-contract admission API."""
    result=_validate_archive_structure(index,members,admission_raw=admission_raw,admission_sha256=admission_sha256,trusted_descriptor_raw=trusted_descriptor_raw,trusted_descriptor_sha256=trusted_descriptor_sha256,contract_bundle=synthetic_contract_bundle,synthetic=True)
    return result
