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

ROOT=Path(__file__).resolve().parents[1]
MAP=ROOT/'docs/company-mail/evidence/R5-COMPATIBILITY-GATES.json'

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

def collect():
    # Fresh producers read syntax only; neither starts a server nor touches PostgreSQL.
    with tempfile.TemporaryDirectory(prefix='tabmail-r5-compat-') as tmp:
        import os
        out=Path(tmp)/'routes.json';env=dict(os.environ,TABMAIL_ROUTE_INVENTORY_OUTPUT=str(out))
        subprocess.run(['go','test','-mod=readonly','-count=1','./internal/architecture','-run','^TestR5RouteInventory$'],cwd=ROOT,env=env,check=True,capture_output=True,text=True,timeout=60)
        routes=strict_json(out.read_text())
    clients=strict_json(subprocess.run(['node','scripts/collect_api_calls.cjs'],cwd=ROOT,check=True,capture_output=True,text=True,timeout=60).stdout)
    return routes,clients

def norm(path):return re.sub(r'\{[^{}]*\}','{}',path.split('?')[0])

def refs(node):
    if isinstance(node,dict):
        return sorted({node.get('$ref','').removeprefix('#/components/schemas/')} - {''} | {v for x in node.values() for v in refs(x)})
    if isinstance(node,list):return sorted({v for x in node for v in refs(x)})
    return []

def release_batch(route):
    path=route['path']
    if '/permissions' in path or path.endswith('/permission') or path.endswith('/send-policy') or ('/mailboxes/' in path and path.endswith('/grants')):return 'P1'
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
    c=contracts();doc,errors=c.parse_openapi_text((ROOT/'internal/api/openapi.yaml').read_text())
    if errors:raise ValueError('; '.join(errors))
    result=[]
    for r in routes:
        key=r['method']+' '+r['path'];op=doc.get('paths',{}).get(r['path'],{}).get(r['method'].lower())
        matched=[x for x in clients if not x['forwarding'] and r['method'] in x['methods'] and norm(r['path'])==norm(x['path'])]
        schema=refs(op or {})
        dto=[{'go':go,'ts':c.company_pairs.get(go),'schema':component} for go,component in c.OPENAPI_COMPANY_COMPONENTS.items() if component in schema]
        shared=[{'go':name,'ts':name,'schema':name,'limitation':'storage model is not an allow-list wire DTO'} for name in c.SHARED_TYPES if name in schema]
        binding=c.COMPANY_RESPONSES.get(r['handler'])
        tests=[{'source':'internal/architecture/route_inventory_test.go','name':'TestR5RouteInventory','scope':'source Go AST route registration'}]
        if r['path'].startswith('/api/v1/company/'):
            tests.append({'source':'internal/api/http_contract_test.go','name':'TestCompanyHTTPContract','scope':'actual HTTP fixture declared; live DNS verification/create are excluded, not certified here'})
        if r['path'] in ['/api/v1/outbound','/api/v1/outbound/{id}']:
            tests.append({'source':'internal/api/handlers/r5_protocol_component_observations_test.go','name':'TestR5ProtocolComponentObservations','scope':'RC02 expired receipt and lost-response replay; not every state or API Key'})
        if r['handler'] in {'perm.SetUserPermissionOverride','perm.UpdateProfile'}:
            tests.append({'source':'internal/store/postgres/r5_protocol_shared_test.go',
                          'name':'TestR5ProtocolPermissionIntentSharedCases' if r['handler']=='perm.SetUserPermissionOverride' else 'TestR5ProtocolPermissionStaleSharedCases',
                          'scope':'PE omission or stale profile actual HTTP/PG target baseline, not all legacy writer behavior'})
        runtime_registration=('declared_company_http_fixture' if r['path'].startswith('/api/v1/company/') else
                              'scoped_protocol_fixture' if len(tests)>1 else
                              'no_route_specific_runtime_fixture_recorded_by_this_map')
        if r['handler'] in {'c.Domains.Create','c.Domains.Verify'}:runtime_registration='live_DNS_excluded_from_company_HTTP_fixture'
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
           'docs/company-mail/evidence/R5-API-MATRIX.json'}
    paths|={r['route_source'] for r in facts}|{x['source'] for x in clients}|{t['source'] for r in facts for t in r['test_bindings']}
    paths|={str(p.relative_to(ROOT)) for p in (ROOT/'internal/company').glob('*.go') if not p.name.endswith('_test.go')}
    return {p:digest(ROOT/p) for p in sorted(paths)}

def validate(data,routes,clients):
    if data.get('schema_version')!=1 or data.get('scope')!='source_inventory_and_upgrade_plan' or data.get('task_complete') is not False or data.get('product_green') is not False:
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
    return {'status':'source_inventory_and_upgrade_plan_checked','task_complete':False,'product_green':False,
            'routes':len(actual),'client_branches':len(clients),'source_files':len(data['source_closure']),
            'openapi_missing':[r['route'] for r in actual if not r['openapi_operation_present']],
            'no_shipped_client':[r['route'] for r in actual if not r['clients']],
            'runtime_boundary':'No HTTP/DB/old-client upgrade execution; fresh scoped evidence and dependency review remain required.'}

def main():
    parser=argparse.ArgumentParser(description=__doc__);parser.add_argument('--map',type=Path,default=MAP)
    parser.add_argument('--route-inventory',type=Path);parser.add_argument('--client-inventory',type=Path);args=parser.parse_args()
    try:
        if bool(args.route_inventory)!=bool(args.client_inventory):raise ValueError('both fresh inventories must be supplied together')
        routes,clients=(strict_json(args.route_inventory.read_text()),strict_json(args.client_inventory.read_text())) if args.route_inventory else collect()
        report=validate(strict_json(args.map.read_text()),routes,clients);print(json.dumps(report,ensure_ascii=False,indent=2));return 0
    except (ValueError,KeyError,TypeError,OSError,subprocess.SubprocessError) as exc:
        print(json.dumps({'status':'rejected','product_green':False,'task_complete':False,'error':str(exc)},ensure_ascii=False));return 1
if __name__=='__main__':raise SystemExit(main())
