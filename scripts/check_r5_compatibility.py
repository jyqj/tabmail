#!/usr/bin/env python3
"""P0-110 source inventory and release-plan gate; never product/runtime approval."""
from __future__ import annotations
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path
import re
import subprocess
import sys
import tempfile
import r5_go_environment
from r5_private_diagnostics import Retention

ROOT=Path(__file__).resolve().parents[1]
MAP=ROOT/'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json'
HISTORY=Path('docs/company-mail/evidence/R5-COMPATIBILITY-CURRENT-20261003/historical-map-v1.json')
CURRENT_CLIENTS=HISTORY.parent/'clients.json'
HISTORICAL_WIRE_SUMMARY_SHA='6421a985979e1e61e187d7b51ac30d75c0ac11c1d8082904fa113037bd352e33'
HISTORY_SHA='61b039486bc7804366012298fe87203882b6fb52ba1b160eb8ee77d76989dc22'

def contracts():
    spec=importlib.util.spec_from_file_location('r5_compat_contracts',ROOT/'scripts/check_contract_drift.py')
    module=importlib.util.module_from_spec(spec);sys.modules[spec.name]=module;spec.loader.exec_module(module)
    return module

def digest(path):return hashlib.sha256(path.read_bytes()).hexdigest()

def strict_json(text):
    def pairs(items):
        result={}
        for key,value in items:
            if key in result:raise ValueError('duplicate JSON key: '+key)
            result[key]=value
        return result
    def constant(value):raise ValueError('non-finite JSON number: '+value)
    return json.loads(text,object_pairs_hook=pairs,parse_constant=constant)

def diagnostic_run(argv, **kwargs):
    with Retention('compatibility') as diagnostics:
        try:
            result = subprocess.run(argv, **kwargs)
        except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
            diagnostics.failed_command(0, error.stdout, error.stderr, getattr(error, 'returncode', None), error)
            raise
        diagnostics.command(0, result.stdout, result.stderr, result.returncode)
        return result


def collect():
    # Fresh producers read syntax only; neither starts a server nor touches PostgreSQL.
    with tempfile.TemporaryDirectory(prefix='tabmail-r5-compat-') as tmp:
        import os
        out=Path(tmp)/'routes.json'
        go, env = r5_go_environment.selected()
        env['TABMAIL_ROUTE_INVENTORY_OUTPUT'] = str(out)
        diagnostic_run([go,'test','-mod=readonly','-count=1','./internal/architecture','-run','^TestR5RouteInventory$'],cwd=ROOT,env=env,check=True,capture_output=True,text=True,timeout=60)
        routes=strict_json(out.read_text())
    clients=strict_json(diagnostic_run(['node','scripts/collect_api_calls.cjs'],cwd=ROOT,check=True,capture_output=True,text=True,timeout=60).stdout)
    return routes,clients

def norm(path):return re.sub(r'\{[^{}]*\}','{}',path.split('?')[0])

def refs(node):
    if isinstance(node,dict):
        return sorted({node.get('$ref','').removeprefix('#/components/schemas/')} - {''} | {v for x in node.values() for v in refs(x)})
    if isinstance(node,list):return sorted({v for x in node for v in refs(x)})
    return []

def release_batch(route):
    path=route['path']
    if '/permissions' in path or '/permission-editor' in path or path.endswith('/permission') or path.endswith('/send-policy') or ('/mailboxes/' in path and path.endswith('/grants')):return 'P1'
    if '/offboard' in path or any(x in path for x in ['/transfer','/convert','/activate']):return 'P4'
    if '/templates' in path or '/drafts' in path:return 'P5'
    if path.startswith('/api/v1/company/') and any(x in path for x in ['/events','/search','/index','/conversation']):return 'P7'
    if '/webhook' in path:return 'P7'
    if any(x in path for x in ['/recovery','/reconcile','/inspect','/retry','/attempts']):return 'P6'
    if path.startswith('/api/v1/outbound') or '/submissions' in path:return 'P2'
    if '/actions' in path:return 'P3'
    if path.startswith('/api/v1/company/'):return 'P8'
    return 'stable'

def source_facts(routes,clients):
    if not isinstance(routes,list) or not routes or not isinstance(clients,list) or not clients:
        raise ValueError('nonempty producer inventories required')
    if len({(r.get('method'),r.get('path')) for r in routes})!=len(routes):
        raise ValueError('duplicate producer route')
    for r in routes:
        if r.get('source') not in {'internal/api/router.go','internal/api/handlers/company_routes.go'} or r.get('method') not in {'GET','POST','PUT','PATCH','DELETE','HEAD','OPTIONS','*'} or not str(r.get('path','')).startswith('/'):
            raise ValueError('invalid source-bound route fact')
    for x in clients:
        source=x.get('source','')
        if not source.startswith('web/') or Path(source).suffix not in {'.ts','.tsx'} or not (ROOT/source).resolve().is_relative_to(ROOT/'web') or not (ROOT/source).is_file():
            raise ValueError('invalid client producer source')
    if clients!=strict_json((ROOT/CURRENT_CLIENTS).read_text()):
        raise ValueError('client producer differs from reviewed current source inventory')
    matrix=strict_json((ROOT/'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
    keys=['method','path','handler','middleware','conditions','source','line']
    if [{k:r.get(k) for k in keys} for r in routes]!=[{k:r.get(k) for k in keys} for r in matrix]:
        raise ValueError('route producer differs from reviewed current source inventory')
    c=contracts();doc,errors=c.parse_openapi_text((ROOT/'internal/api/openapi.yaml').read_text())
    if errors:raise ValueError('; '.join(errors))
    result=[]
    for r in routes:
        key=r['method']+' '+r['path'];op=doc.get('paths',{}).get(r['path'],{}).get(r['method'].lower())
        matched=[x for x in clients if not x['forwarding'] and r['method'] in x['methods'] and norm(r['path'])==norm(x['path'])]
        schema=refs(op or {})
        dto=[{'go':go,'ts':c.company_pairs.get(go),'schema':component} for go,component in c.OPENAPI_COMPANY_COMPONENTS.items() if component in schema]
        # Explicit reviewed permission DTOs; response envelopes are recorded separately.
        permission_bindings={
            'PermissionEditorResponse':('PermissionEditorSnapshot','PermissionEditorSnapshot','data'),
            'PermissionEditorCommand':('PermissionEditorCommand','PermissionEditorCommand',None),
            'PermissionEditorAssignmentCommand':('PermissionAssignmentCommand','PermissionAssignmentCommand',None),
            'PermissionProfileDeletionPreviewResponse':('PermissionProfileDeletionPreview','PermissionProfileDeletionPreview','data'),
        }
        for component,(go,ts,envelope) in permission_bindings.items():
            if component not in schema:continue
            go_source='internal/company/permission_editor.go';ts_source='web/lib/api/permission-editor-types.ts'
            if not re.search(r'type '+go+r' struct', (ROOT/go_source).read_text()) or not re.search(r'export interface '+ts+r'\b',(ROOT/ts_source).read_text()):
                raise ValueError('missing reviewed permission DTO symbol')
            dto.append({'go':go,'ts':ts,'schema':component,'go_source':go_source,'ts_source':ts_source,
                        'response_envelope':envelope,'limitation':'Reviewed named source binding; field/runtime compatibility requires independent contract consumers.'})
        shared=[{'go':name,'ts':name,'schema':name,'limitation':'storage model is not an allow-list wire DTO'} for name in c.SHARED_TYPES if name in schema]
        binding=c.COMPANY_RESPONSES.get(r['handler'])
        tests=[{'source':'internal/architecture/route_inventory_test.go','name':'TestR5RouteInventory','scope':'source Go AST route registration'}]
        if r['path'].startswith('/api/v1/company/'):
            tests.append({'source':'internal/api/http_contract_test.go','name':'TestCompanyHTTPContract','scope':'actual HTTP fixture declared; live DNS verification/status are excluded, not certified here'})
        if r['path'] in ['/api/v1/outbound','/api/v1/outbound/{id}']:
            tests.append({'source':'internal/api/handlers/r5_protocol_component_observations_test.go','name':'TestR5ProtocolComponentObservations','scope':'RC02 expired receipt and lost-response replay; not every state or API Key'})
        if r['handler'] in {'perm.SetUserPermissionOverride','perm.UpdateProfile'}:
            tests.append({'source':'internal/store/postgres/r5_protocol_shared_test.go',
                          'name':'TestR5ProtocolPermissionIntentSharedCases' if r['handler']=='perm.SetUserPermissionOverride' else 'TestR5ProtocolPermissionStaleSharedCases',
                          'scope':'PE omission or stale profile actual HTTP/PG target baseline, not all legacy writer behavior'})
        runtime_registration=('declared_company_http_fixture' if r['path'].startswith('/api/v1/company/') else
                              'scoped_protocol_fixture' if len(tests)>1 else
                              'no_route_specific_runtime_fixture_recorded_by_this_map')
        if (r['method'],r['path']) in {('GET','/api/v1/company/domains/{id}/verification'),('POST','/api/v1/company/domains/{id}/verify')}:runtime_registration='live_DNS_excluded_from_company_HTTP_fixture'
        fields=(op or {}).get('requestBody',{})
        result.append({'route':key,'release_batch':release_batch(r),'handler':r['handler'],'route_source':r['source'],'route_line':r['line'],
                       'middleware':r['middleware'],'conditions':r['conditions'],
                       'openapi_operation_present':isinstance(op,dict),'schema_components':schema,
                       'request_schema_refs':refs(fields),
                       'request_contract':None if not fields else {'required':fields.get('required',False), 'media_type_schemas':{media:body.get('schema') for media,body in fields.get('content',{}).items()}},
                       'response_schema_refs':refs((op or {}).get('responses',{})),
                       'go_ts_dto_bindings':dto+shared,'reviewed_response_binding':list(binding) if binding else None,
                       'clients':matched,'test_bindings':tests,'runtime_test_registration':runtime_registration,
                       'api_key_authority':[m for m in r['middleware'] if any(v in m for v in ['Key','Scope','RequireAdmin','RequireSuperAdmin'])],
                       'coverage_limits':['Syntax/field/schema presence is not actual authorization, CAS, nil slice normalization, or dynamic request-body proof.',
                           'No known shipped web caller' if not matched else 'Client AST identifies paths/methods; dynamic payload fields need real HTTP consumers.',
                           'No explicit reviewed Go/TS DTO binding' if not dto and not shared else 'Go/TS names mapped; current field checker must independently execute.']})
    return result

def closure(facts,clients):
    paths={'internal/api/openapi.yaml','scripts/check_contract_drift.py','scripts/collect_api_calls.cjs',
           'internal/models/models.go','internal/models/mailbox_grants.go','web/lib/types.ts',
           'internal/architecture/route_inventory_test.go','internal/api/http_contract_test.go',
           'docs/company-mail/evidence/R5-API-MATRIX.json',str(CURRENT_CLIENTS)}
    paths|={b[k] for r in facts for b in r['go_ts_dto_bindings'] for k in ['go_source','ts_source'] if k in b}
    paths|={r['route_source'] for r in facts}|{x['source'] for x in clients}|{t['source'] for r in facts for t in r['test_bindings']}
    paths|={str(p.relative_to(ROOT)) for p in (ROOT/'internal/company').glob('*.go') if not p.name.endswith('_test.go')}
    # Bind handler implementation and middleware bytes as structure, never runtime proof.
    paths|={str(p.relative_to(ROOT)) for folder in ['internal/api/handlers','internal/api/middleware']
            for p in (ROOT/folder).glob('*.go') if not p.name.endswith('_test.go')}
    return {p:digest(ROOT/p) for p in sorted(paths)}

def canonical_bytes(value):return (json.dumps(value,ensure_ascii=False,indent=2)+'\n').encode()

def validate_wire(data,facts,artifact_root=None):
    evidence=data.get('wire_source_evidence',{})
    summary=evidence.get('summary',{})
    if set(summary)!={'schema_version','source_identity','source_closure_sha256','inputs','observations','excluded_http_operations','boundary'} or summary.get('schema_version')!=1:
        raise ValueError('missing/malformed safe wire summary')
    if hashlib.sha256(canonical_bytes(summary)).hexdigest()!=evidence.get('summary_sha256'):
        raise ValueError('wire summary hash mismatch')
    if data.get('schema_version')!=1 or evidence.get('summary_sha256')!=HISTORICAL_WIRE_SUMMARY_SHA:
        raise ValueError('unreviewed/re-signed historical wire summary')
    identity=summary['source_identity']
    if identity!=evidence.get('expected_source_identity') or set(identity)!={'frozen_tree','validation_commit'} or any(not re.fullmatch('[0-9a-f]{40}',str(v)) for v in identity.values()):
        raise ValueError('wrong wire report source identity')
    if summary['source_closure_sha256']!=evidence.get('expected_source_closure_sha256') or not re.fullmatch('[0-9a-f]{64}',str(summary['source_closure_sha256'])):
        raise ValueError('wire source closure hash mismatch')
    spec=digest(ROOT/'internal/api/openapi.yaml');cases=digest(ROOT/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json')
    if evidence.get('expected_spec_sha256')!=spec or evidence.get('expected_cases_sha256')!=cases:
        raise ValueError('wire spec/case differs from current source')
    exclusions={(x.get('method'),x.get('path')) for x in summary['excluded_http_operations']}
    if exclusions!={('GET','/api/v1/company/domains/{id}/verification'),('POST','/api/v1/company/domains/{id}/verify')} or len(summary['excluded_http_operations'])!=2:
        raise ValueError('wrong exact DNS method/path exclusions')
    if any(set(x)!={'method','path','reason'} for x in summary['excluded_http_operations']):raise ValueError('unknown/private DNS exclusion fields')
    inputs={}
    for ref in summary['inputs']:
        if not isinstance(ref,dict) or set(ref)-{'kind','artifact_ref','sha256','source_sha','spec_sha256','cases_sha256'} or any(not isinstance(ref.get(k),str) for k in ['kind','artifact_ref','sha256','source_sha']):
            raise ValueError('private/unknown/malformed wire input fields')
        path=ref['artifact_ref'];kind=ref['kind']
        if path in inputs or not re.fullmatch('[0-9a-f]{64}',ref['sha256']):raise ValueError('duplicate/malformed artifact input')
        fixed={'http_contract':'final-http/result.json','protocol_db':'final-protocol-db/report.json','protocol_components':'final-protocol-components/report.json'}
        packet=bool(re.fullmatch(r'final-protocol-components/http-pg-components/[A-Z]{2}[0-9]{2}/(?:[A-Za-z0-9_-]+|\[\])/observations\.json',path))
        if kind not in fixed or (path!=fixed[kind] and not (kind=='protocol_components' and packet)):
            raise ValueError('unapproved artifact (private captures are forbidden)')
        expected_source=identity['frozen_tree'] if kind=='http_contract' else identity['validation_commit']
        if ref['source_sha']!=expected_source:raise ValueError('wrong input report source hash')
        if kind=='http_contract' and ref.get('spec_sha256')!=spec:raise ValueError('wrong HTTP report spec hash')
        if kind!='http_contract' and ref.get('cases_sha256')!=cases:raise ValueError('wrong protocol report case hash')
        inputs[path]=ref
    if not all(path in inputs for path in ['final-http/result.json','final-protocol-db/report.json','final-protocol-components/report.json']):
        raise ValueError('missing wire source reports')
    known={row['id']:row for row in strict_json((ROOT/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').read_text())['cases']}
    joined={r['route']:[] for r in facts};seen=set()
    for observation in summary['observations']:
        if not isinstance(observation,dict) or set(observation)-{'route','actual_status','aspect','case_id','variant','target_marker','response_sha256','input_ref'}:
            raise ValueError('private/unknown wire observation fields')
        route=observation.get('route');aspect=observation.get('aspect');ref=observation.get('input_ref')
        if route not in joined or ref not in inputs or type(observation.get('actual_status')) is not int or not 200<=observation['actual_status']<=599 or not isinstance(aspect,str) or not aspect:
            raise ValueError('unknown route/input/status/aspect')
        if not re.fullmatch('[0-9a-f]{64}',str(observation.get('response_sha256',''))):raise ValueError('missing actual response digest')
        if ref=='final-http/result.json':
            if aspect!='http_schema_status:'+str(observation.get('case_id')) or 'target_marker' in observation or 'variant' in observation:raise ValueError('wrong HTTP observation aspect')
        else:
            case=observation.get('case_id');variant=observation.get('variant')
            path_variant='empty_array' if variant=='[]' else variant
            if case not in known or ref!=f'final-protocol-components/http-pg-components/{case}/{path_variant}/observations.json' or not re.fullmatch(re.escape(f'real_component_http:{case}:{variant}:')+'[0-9]+',aspect):raise ValueError('wrong component case/variant/aspect')
            marker=observation.get('target_marker')
            allowed={t['marker'] for adapter in known[case].get('shared_adapters',[]) for path,t in adapter.get('target_failures',{}).items() if path==f'TestR5ProtocolComponentObservations/{case}/{variant}'}
            if marker is not None and marker not in allowed:raise ValueError('unknown/wrong-case component target marker')
        fingerprint=json.dumps(observation,sort_keys=True)
        if fingerprint in seen:raise ValueError('duplicate wire observation')
        seen.add(fingerprint);joined[route].append(observation)
    if not summary['observations']:raise ValueError('empty wire observations')
    if artifact_root is not None:
        root=Path(artifact_root).resolve()
        manifest=root/'final-source.json'
        if not manifest.is_file() or digest(manifest)!=summary['source_closure_sha256']:raise ValueError('actual frozen source manifest mismatch')
        frozen=strict_json(manifest.read_text())
        if any(frozen.get(path)!=sha for path,sha in data['source_closure'].items()):raise ValueError('wire frozen source differs from declared source closure')
        for path,ref in inputs.items():
            artifact=(root/path).resolve()
            if not artifact.is_relative_to(root) or not artifact.is_file() or digest(artifact)!=ref['sha256']:raise ValueError('actual wire artifact hash mismatch: '+path)
            packet=strict_json(artifact.read_text())
            if path.endswith('/observations.json'):
                if packet.get('case_sha256')!=cases:raise ValueError('actual packet case mismatch')
            elif packet.get('source_sha')!=ref['source_sha']:raise ValueError('actual report source mismatch')
    for row in data['routes']:
        actual=joined[row['route']]
        if row.get('wire_observations')!=actual or row.get('wire_evidence')!=('observed_metadata_not_complete_behavior' if actual else 'explicit_not_observed'):
            raise ValueError('wire row observation/unknown status laundering')
    return {'wire_observations':len(summary['observations']),'wire_observed_routes':sum(bool(v) for v in joined.values()),
            'wire_explicit_not_observed_routes':sum(not v for v in joined.values()),
            'wire_validation_scope':'actual_safe_artifacts_rehashed' if artifact_root is not None else 'embedded_safe_metadata_and_pinned_hashes_only',
            'wire_source_identity':identity}


def validate(data,routes,clients,artifact_root=None):
    if data.get('schema_version')!=2 or data.get('scope')!='source_inventory_and_upgrade_plan' or data.get('task_complete') is not False or data.get('product_green') is not False:
        raise ValueError('explicit source-only non-completion/non-product scope required')
    if data.get('dependencies')!=['R5-P0-020','R5-P0-050','R5-P0-080'] or data.get('dependency_acceptance')!='operator_review_required':
        raise ValueError('P0-110 dependency acceptance cannot be inferred by source gate')
    actual=source_facts(routes,clients);stored=data.get('routes',[])
    if len(stored)!=len(actual) or len({x.get('route') for x in stored})!=len(stored):raise ValueError('missing/duplicate routes')
    batches=data.get('release_batches',{});expected={'P1','P2','P3','P4','P5','P6','P7','P8','stable'}
    if set(batches)!=expected:raise ValueError('unknown or missing release batch')
    for name,batch in batches.items():
        if not all(isinstance(batch.get(k),str) and batch[k].strip() for k in ['old_write_rejection','coordinated_upgrade','current_implementation','successor_tasks']):raise ValueError('batch needs explicit rejection/upgrade/current/task strategy')
        if batch.get('implemented_by_this_map') is not False:raise ValueError('map cannot certify future changes implemented')
        if name!='stable':
            task_list=re.fullmatch(r'(R5-P[0-9]+-)([0-9]+(?:/[0-9]+)*)',batch['successor_tasks'])
            known=set(re.findall(r'\*\*(R5-P[0-9]+-[0-9]+)｜',(ROOT/'docs/company-mail/R5-TODO.md').read_text()))
            if not task_list or any(task_list[1]+n not in known for n in task_list[2].split('/')):
                raise ValueError('existing successor task bindings required')
            required_tasks={'P5':{'R5-P5-080','R5-P5-100','R5-P5-110'},
                            'P6':{'R5-P6-020','R5-P6-070','R5-P6-130'},
                            'P7':{'R5-P7-070','R5-P7-090','R5-P7-100','R5-P7-120'}}
            bound={task_list[1]+n for n in task_list[2].split('/')}
            if not required_tasks.get(name,set()).issubset(bound):
                raise ValueError('batch does not bind original template/recovery/events task semantics')

    for row,fact in zip(stored,actual):
        if any(row.get(k)!=v for k,v in fact.items()):raise ValueError('route/schema/client/test source drift: '+fact['route'])
        if row.get('release_batch') not in batches:raise ValueError('route has unknown upgrade batch')
        if row.get('legacy_client_disposition') not in ['shipped_client_upgrade_and_external_usage_unknown','external_usage_unknown_no_shipped_client']:
            raise ValueError('legacy clients cannot silently be declared absent')
        if row.get('runtime_evidence_scope')!='not_executed_by_this_gate':raise ValueError('source-only gate cannot claim runtime pass')
        for test in row['test_bindings']:
            if not re.search(r'func '+re.escape(test['name'])+r'\(', (ROOT/test['source']).read_text()):raise ValueError('missing actual test symbol')
    if data.get('source_closure')!=closure(actual,clients):raise ValueError('source hash drift')
    reference={'artifact_ref':str(HISTORY),'sha256':HISTORY_SHA,'qualification':'historical_metadata_only_not_current_wire'}
    if data.get('historical_wire_reference')!=reference or digest(ROOT/HISTORY)!=HISTORY_SHA:
        raise ValueError('historical wire archive/reference drift')
    if 'wire_source_evidence' in data or any('wire_observations' in r or 'wire_evidence' in r for r in stored):
        raise ValueError('current source map cannot re-sign historical runtime evidence')
    if artifact_root is not None:
        # Keep the actual wire validator strict; archive bytes cannot qualify current source.
        validate_wire(data,actual,artifact_root)
    wire={'wire_validation_scope':'not_checked_current_wire_required',
          'historical_wire_reference':reference}

    return {**wire,'status':'source_inventory_and_upgrade_plan_checked','task_complete':False,'product_green':False,
            'routes':len(actual),'client_branches':len(clients),'source_files':len(data['source_closure']),
            'openapi_missing':[r['route'] for r in actual if not r['openapi_operation_present']],
            'no_shipped_client':[r['route'] for r in actual if not r['clients']],
            'runtime_boundary':'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency review remain required.'}

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--map',type=Path,default=MAP);parser.add_argument('--wire-evidence-root',type=Path)
    parser.add_argument('--route-inventory',type=Path);parser.add_argument('--client-inventory',type=Path);args=parser.parse_args()
    try:
        if bool(args.route_inventory)!=bool(args.client_inventory):raise ValueError('both fresh inventories must be supplied together')
        routes,clients=(strict_json(args.route_inventory.read_text()),strict_json(args.client_inventory.read_text())) if args.route_inventory else collect()
        report=validate(strict_json(args.map.read_text()),routes,clients,args.wire_evidence_root);print(json.dumps(report,ensure_ascii=False,indent=2));return 0
    except (ValueError,KeyError,TypeError,OSError,subprocess.SubprocessError) as exc:
        print(json.dumps({'status':'rejected','product_green':False,'task_complete':False,'error':str(exc)},ensure_ascii=False));return 1
if __name__=='__main__':raise SystemExit(main())
