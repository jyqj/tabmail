"""Independent, non-discovered pure review controls; no author fixture imports."""
import sys
EVENTS = []
def guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty', 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        EVENTS.append(event)
        raise AssertionError(event)
sys.addaudithook(guard)
import copy
import hashlib
import json
from pathlib import Path
from unittest.mock import patch
sys.path.insert(0, str(Path(__file__).resolve().parents[4] / 'scripts'))
if '--baseline' in sys.argv:
    import importlib.util
    spec=importlib.util.spec_from_file_location('r5_selected_binding_consumer','/tmp/r5-rejected-consumer.py')
    module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
    sys.modules['r5_selected_binding_consumer']=module
import r5_selected_binding_consumer as c
assert 'r5_selected_source_binding_v3' not in sys.modules
with patch.object(c.importlib, 'import_module') as load:
    c.selected_helper()
    load.assert_called_once_with('r5_selected_source_binding_v2')
for bad in (True, False, None, 1, 0, 4, 3.0, '3'):
    with patch.object(c.importlib, 'import_module') as load:
        try: c.selected_helper(bad)
        except ValueError: pass
        else: raise AssertionError('selector accepted')
        load.assert_not_called()
import r5_selected_source_binding_v3 as v
ROOT = '/independent/controller/source'
COMMIT = '9' * 40
RUN = 'independent-review-run'
PRODUCER = {'path': '/independent/pinned/go', 'sha256': v.GO_SHA256, 'version': 'go1.25.7'}
def wire(x): return json.dumps(x, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()
def sha(x): return hashlib.sha256(x).hexdigest()
def seal(r):
    b = r['base_source']
    p = {k:x for k,x in b.items() if k not in ('schema_version','snapshot_root','source_identity_kind','source_sha','source_closure_sha256')}
    b['source_sha'] = hashlib.sha1(wire(p)).hexdigest()
    b['source_closure_sha256'] = sha(wire(p))
    # Independent implementation solely to publish synthetic controlled objects.
    p = {k:x for k,x in r.items() if k not in ('binding_sha256','observation_sha256','observation_envelope')}
    p['commands'] = [{k:x for k,x in d.items() if k not in ('raw_stdout_sha256','raw_stdout_bytes')} for i,d in enumerate(r['observation_envelope']) if i != 1]
    r['binding_sha256'] = sha(wire(p))
    r['observation_sha256'] = sha(wire({k:x for k,x in r.items() if k != 'observation_sha256'}))
def receipt(context):
    static = dict(policy='r5_immutable_archive_boundary_v1', registry_sha256=v.boundary.REGISTRY_SHA256, modules=[], archive_static={}, archive_markers={}, production_go={'internal/a.go':'a'*64}, production_variant_directories=['internal'], compile_roots=['./...'], explicit_production_roots=['./cmd/...','./internal/...'], budgets=dict(entries=100000, depth=64, bytes=67108864))
    base = dict(schema_version=4, snapshot_root=ROOT, source_identity_kind=v.inventory.ARCHIVE_KIND, policy=v.inventory.ARCHIVE_POLICY, purpose='selected', build_context=copy.deepcopy(context), replacements=[], module_resolution={}, inventory_implementation_sha256='b'*64, boundary='synthetic controlled source', excluded_directories=[], embed_inputs={}, files={}, archive_static={}, archive_boundary=static, version_test_contract={})
    selection = v.selection_argv(context, ['./...', 'github.com/jhillyerd/enmime/v2/...','github.com/emersion/go-smtp/...'])
    root = v.selection_argv(context, ['./...']); explicit = v.selection_argv(context,['./cmd/...','./internal/...']); variant = v.variant_argv(context,static)
    commands = []
    for i, argv in enumerate([['env','-json'],selection,selection,v.MVS_ARGV,root,explicit,variant,selection,v.MVS_ARGV,root,explicit,variant]):
        commands.append(dict(role='unbound_dependency_hydration_not_attested' if i == 1 else 'attested_observation', argv=[PRODUCER['path'],*argv], exit=0, raw_stdout_sha256='c'*64, raw_stdout_bytes=7, stderr_sha256=sha(b''), stderr_bytes=0, binding_stdout_sha256='c'*64, binding_stdout_domain='canonical_go_env_with_only_GOGCCFLAGS_temp_prefix_normalized' if i==0 else 'raw_MVS' if i in (3,8) else 'complete_PackagePublic_stream_except_top_level_Stale_StaleReason'))
    r = dict(schema_version=3,policy=v.POLICY,incompatible_with='selected v1/v2 receipts never authorize v3; v3 never authorizes v2', observation_order='env; unbound hydration; selected; MVS; coverage; selected; MVS; repeated coverage (projected package equality; raw MVS equality)',base_source=base,archive_static={},production_coverage=dict(all_variant_directories=['internal']), environment=dict(PATH='/usr/bin',HOME='/independent',GODEBUG='asynctimerchan=0',GOWORK='off',GOENV='off',GOTOOLCHAIN='local',GOFLAGS='',GOOS='linux',GOARCH='amd64',CGO_ENABLED='1',GOPROXY='https://proxy.golang.org',GOSUMDB='sum.golang.org',GOPATH='/independent/cache',GOMODCACHE='/independent/cache/mod',GOCACHE='/independent/cache/build'),go_env=dict(GOVERSION='go1.25.7',GOOS='linux',GOARCH='amd64',GOHOSTOS='linux',GOHOSTARCH='amd64',CGO_ENABLED='1',GOWORK='off',GOENV='',GOFLAGS='',GOEXPERIMENT='',GOAMD64='v1',GOTOOLCHAIN='local',GOGCCFLAGS='-fPIC'),observation_envelope=commands,v3_source={v._V3_FILES[0]:v._IMPLEMENTATION_SHA256,v.SCHEMA_PATH:sha(v._REGISTRY_BYTES),v._V3_FILES[2]:'d'*64},metadata_producer=dict(version='go1.25.7',executable_sha256=v.GO_SHA256,qualification='exact_metadata_producer_only_not_compilation_attestation'),root_mvs=[],selected_local={},generated_testmain=[],external_modulecache_inputs=[],native_inputs=[],toolchain_source_inputs=[],selected_local_packages=[],package_records=1,qualification={},boundary='synthetic review')
    seal(r); return r
# Independent completion of the original deliberately partial positive fixture.
partial_receipt = receipt
def receipt(context):
    r = partial_receipt(context)
    b = r['base_source']; inv = v.inventory
    b['replacements'] = copy.deepcopy(list(inv.CURRENT_REPLACEMENTS))
    b['inventory_implementation_sha256'] = inv._IMPLEMENTATION_SHA256
    b['boundary'] = 'Versioned archive static bytes and production variant superset; selected metadata/runtime are separate'
    b['excluded_directories'] = sorted(inv.EXCLUDED_DIRS)
    b['version_test_contract'] = dict(frozen_v1=v.boundary.BASELINE, frozen_test_ids=sorted('test_r5_selected_source_binding.ActualRootBindingTests.'+n for n in ('test_actual_full_scope_fresh_hashes','test_real_receipt_roundtrip','test_omission_extra_hash_flags_abi_and_historical_rejected','test_capture_midflight_drift_refused')),current_actual_class='test_r5_selected_source_binding_v2.ActualRootBindingV2Tests.',expected_failures_are_green=False)
    files=b['files'];files.update({'go.mod':'1'*64,'go.sum':'2'*64,'internal/a.go':'a'*64})
    modules=[]
    for replacement in inv.CURRENT_REPLACEMENTS:
        files[replacement['path']+'/go.mod']='3'*64
        files[replacement['path']+'/go.sum']='4'*64
        metadata={name:'5'*64 for name in inv.MODULE_METADATA[replacement['module']]}
        files.update({replacement['path']+'/'+name:h for name,h in metadata.items()})
        modules.append(dict(replacement,declared_go='1.25.7',declared_toolchain=None,declared_requires={},go_mod_sha256=files[replacement['path']+'/go.mod'],go_sum_sha256=files[replacement['path']+'/go.sum'],metadata_sha256=metadata,upstream_file_inventory=['go.mod']))
    b['module_resolution']=dict(main_module='tabmail',root_go_mod_sha256=files['go.mod'],root_go_sum_sha256=files['go.sum'],declared_go='1.25.7',declared_toolchain=None,declared_requires={x['module']:x['version'] for x in inv.CURRENT_REPLACEMENTS},local_modules=modules,local_module_file_selection='all_regular_files_not_suffix_filtered_except_explicit_artifact_directories_and_DS_Store',excluded_local_module_metadata=dict(inspection='names_and_lstat_types_only_no_regular_file_reads_no_symlink_following',reject=['symlinks','nested_go.mod','special_or_unknown_types','unavailable_metadata','budget_exceeded'],max_entries_per_local_module_per_pass=4096,max_depth_including_excluded_root=32),effective_root_MVS_observed=False,go_selected_inputs_observed=False,external_module_cache_verified=False,boundary='Declared root/local module metadata and complete local input superset only; actual root MVS/go-list/build/external-cache qualification is separate')
    b['archive_boundary']['modules']=sorted(['go.mod',*[x['path']+'/go.mod' for x in inv.CURRENT_REPLACEMENTS]])
    package=dict(directory='internal',import_path='tabmail/internal',for_test=None,module='tabmail',fields={'GoFiles':['a.go']})
    r['selected_local_packages']=[package]
    r['selected_local']={'internal/a.go':dict(sha256='a'*64,fields=['GoFiles'])}
    r['root_mvs']=sorted([dict(Path='tabmail',Main=True,GoVersion='1.25.7'),*[dict(Path=x['module'],Version=x['version'],Replace=dict(x)) for x in inv.CURRENT_REPLACEMENTS]],key=lambda x:x['Path'])
    r['qualification']=dict(overall='blocked',excluded_metadata_boundary='descriptor_checked',local_static_binding='captured',generated='unknown',external_modulecache='unknown',compiler_native='unknown',Method19='unknown',SML='unknown',wholeCI='unknown')
    r['production_coverage'].update(root_packages=[copy.deepcopy(package)],explicit_packages=[copy.deepcopy(package)],no_missing=True,no_overlap=True,other_contexts='static_only_not_executed',variant_directory_records=[dict(directory='internal',import_path='tabmail/internal',module='tabmail',fields={'GoFiles':['internal/a.go']},selected_context=True,other_variants='static_only')])
    r['boundary']='Metadata/static local bytes only; no product tests, cold build or runtime qualification'
    seal(r)
    return r

SLOTS = ('before-default','before-race-r5protocol','after-default','after-race-r5protocol')
def publish(receipts):
    for r in receipts.values(): seal(r)
    b = dict(schema_version=1,policy='r5_selected_observation_pins_v1',run_id=RUN,source_commit=COMMIT,source_root=ROOT,selected_binding_version=3,producer=copy.deepcopy(PRODUCER), observations={s:dict(receipt_path='/independent/evidence/'+s+'.json',receipt_byte_sha256=sha(wire(r)),observation_sha256=r['observation_sha256'],context=copy.deepcopy(r['base_source']['build_context'])) for s,r in receipts.items()})
    return wire(b), {s:wire(r) for s,r in receipts.items()}
def fresh(): return {s:receipt(v.DEFAULT_CONTEXT if s.endswith('default') else v.CONTEXT) for s in SLOTS}
def verify(b,raw,**override):
    args = dict(trusted_bundle_byte_sha256=sha(b),run_id=RUN,source_commit=COMMIT,source_root=ROOT,producer=PRODUCER,allowed_slots=SLOTS,selected_binding_version=3); args.update(override)
    return c.verify_bundle(b,raw,**args)
RESULTS=[]
def check(name,b,raw,expect_reject=True,**args):
    try: verify(b,raw,**args)
    except (ValueError,TypeError) as e: rejected=True; detail=str(e)
    else: rejected=False; detail='accepted'
    RESULTS.append(dict(name=name,expected='reject' if expect_reject else 'accept',actual='reject' if rejected else 'accept',detail=detail,passed=rejected==expect_reject))
r=fresh(); b,raw=publish(r); check('controlled-two-context-four-slot-positive',b,raw,False)
# Untrusted replacements must never change the independently retained expected pin.
fixedpin=sha(b)
with patch.object(v,'capture',side_effect=AssertionError('capture')), patch.object(v,'validate',side_effect=AssertionError('validate')), patch.object(v,'Retention',side_effect=AssertionError('Retention')), patch('os.getenv',side_effect=AssertionError('getenv')), patch('builtins.open',side_effect=AssertionError('candidate open')), patch.object(Path,'read_bytes',side_effect=AssertionError('candidate read')):
    for i in range(12):
        for field,bad in [('role','foreign'),('argv',['go']),('exit',True),('binding_stdout_domain','foreign'),('stderr_bytes',True)]:
            r=fresh();r[SLOTS[0]]['observation_envelope'][i][field]=bad;b,raw=publish(r)
            check('descriptor-%d-%s'%(i,field),b,raw)
        r=fresh();r[SLOTS[0]]['observation_envelope'][i]['raw_stdout_bytes']=33;b,raw=publish(r)
        check('counterfeit-resealed-fixedpin-%d'%i,b,raw,trusted_bundle_byte_sha256=fixedpin)
    for arg,bad in [('run_id','other'),('source_commit','8'*40),('source_root','/other'),('producer',dict(PRODUCER,path='/other/go')),('allowed_slots',('default','race-r5protocol')),('selected_binding_version',2),('selected_binding_version',True)]:
        b,raw=publish(fresh());check('expected-'+arg,b,raw,**{arg:bad})
    for field,bad in [('schema_version',True),('policy','foreign'),('selected_binding_version',2)]:
        b,raw=publish(fresh()); obj=json.loads(b);obj[field]=bad;check('bundle-'+field,wire(obj),raw)
    b,raw=publish(fresh()); duplicate=b.replace(b'"schema_version":1',b'"schema_version":1,"schema_version":1');check('duplicate-bundle',duplicate,raw)
    for i in range(12):
        r=fresh();r[SLOTS[0]]['observation_envelope'].pop(i);b,raw=publish(r);check('missing-descriptor-%d'%i,b,raw)
    for bad in (b'{', b'[]', b'\xff', b'{"schema_version":1.0}', b'{"schema_version":NaN}'):
        _,raw=publish(fresh());check('malformed-bundle-'+repr(bad),bad,raw)
    for change in ('duplicate-key','float','wrong-context','wrong-root','missing-slot','duplicate-path','swap-phases'):
        r=fresh()
        if change=='wrong-context': r[SLOTS[0]]['base_source']['build_context']=copy.deepcopy(v.CONTEXT)
        if change=='wrong-root': r[SLOTS[0]]['base_source']['snapshot_root']='/foreign'
        if change=='swap-phases': r['after-default']['observation_envelope'][2]['raw_stdout_bytes']=44
        b,raw=publish(r);obj=json.loads(b)
        if change in ('duplicate-key','float'):
            raw[SLOTS[0]]=raw[SLOTS[0]].replace(b'"exit":0',b'"exit":0,"exit":0' if change=='duplicate-key' else b'"exit":0.0',1)
            obj['observations'][SLOTS[0]]['receipt_byte_sha256']=sha(raw[SLOTS[0]])
        if change=='missing-slot': del obj['observations'][SLOTS[0]]
        if change=='duplicate-path': obj['observations'][SLOTS[2]]['receipt_path']=obj['observations'][SLOTS[0]]['receipt_path']
        if change=='swap-phases': raw[SLOTS[0]],raw[SLOTS[2]]=raw[SLOTS[2]],raw[SLOTS[0]]
        check('receipt-context-slot-'+change,wire(obj),raw)
    # Contract-invalid records, resealed and newly pinned by this synthetic publisher.
    mutations = [
        ('archive-policy',lambda r:r['base_source']['archive_boundary'].update(policy='incompatible_archive_v999')),
        ('archive-registry',lambda r:r['base_source']['archive_boundary'].update(registry_sha256='e'*64)),
        ('nested-root-mvs-bool',lambda r:r.update(root_mvs=[{'Path':True,'Main':'wrong'}])),
        ('selected-local-schema',lambda r:r.update(selected_local={'internal/a.go':{'sha256':False,'fields':7}})),
        ('qualification-false-claim',lambda r:r.update(qualification={'overall':'passed','compiler_native':'attested'})),
        ('production-coverage-types',lambda r:r['production_coverage'].update(no_missing='yes',no_overlap=False,root_packages=True)),
    ]
    for name,change in mutations:
        r=fresh();change(r[SLOTS[0]]);b,raw=publish(r);check('schema-edge-'+name,b,raw)
# Binding projection equality and stderr preservation.
r=fresh(); b,raw=publish(r); original=c.binding_payloads(verify(b,raw),selected_binding_version=3)
r['after-default']['observation_envelope'][2].update(raw_stdout_sha256='e'*64,raw_stdout_bytes=99)
b,raw=publish(r); new=c.binding_payloads(verify(b,raw),selected_binding_version=3)
assert original['before-default']==new['after-default']
for i in [0,*range(2,12)]:
    r=fresh();r['after-default']['observation_envelope'][i].update(stderr_sha256='e'*64,stderr_bytes=8)
    b,raw=publish(r); verified=verify(b,raw);projected=c.binding_payloads(verified,selected_binding_version=3)
    assert projected['before-default'] != projected['after-default']
    assert projected['after-default']==v.binding_payload(verified['after-default'])
assert not EVENTS
print(json.dumps(dict(candidate=('3ff6db72b579419a7145d2bc2a3b888c111c9b62' if '--baseline' in sys.argv else '116d44a2a9c0b5d7ac6a09187bf3fae22c1a1e50'),base='412f875985854527f5e7d74040f18954c3a352bf',os_process_events=EVENTS,selector_and_projection_assertions='passed',controls=RESULTS,unexpected_acceptances=[x['name'] for x in RESULTS if not x['passed']]),indent=2))
assert all(x['passed'] for x in RESULTS if not x['name'].startswith('schema-edge-'))
assert len([x for x in RESULTS if not x['passed']])==(6 if '--baseline' in sys.argv else 0)
