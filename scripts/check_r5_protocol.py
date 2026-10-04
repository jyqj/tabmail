#!/usr/bin/env python3
"""Check R5 target cases and run real named Go evidence (not protocol completion).

The shared JSON describes target inputs. Existing Go tests are reference adapters:
they do NOT consume those inputs. Passing them never freezes P0-080. --run needs
an explicitly supplied disposable TABMAIL_TEST_DB_DSN for DB/HTTP/components.
"""
from __future__ import annotations
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys

# Absolute file loading also supports archived importlib-based validator tests.
import importlib.util
_inventory_spec = importlib.util.spec_from_file_location(
    "r5_source_inventory", Path(__file__).with_name("r5_source_inventory.py"))
source_inventory = importlib.util.module_from_spec(_inventory_spec)
_inventory_spec.loader.exec_module(source_inventory)

ROOT = Path(__file__).resolve().parents[1]
CASES = ROOT / 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'
EXPECTED_IDS = {f'{prefix}{i:02d}' for prefix, n in [('CT',10),('RC',5),('OP',4),('RT',7),('BC',6),('GC',2),('LF',7),('PE',5)] for i in range(1,n+1)}


def validate_adapter_targets(row, adapter):
    targets = adapter.get('target_failures', {})
    if not isinstance(targets, dict):
        raise ValueError('adapter targets must be an exact path mapping')
    capability = adapter.get('evidence_scope') == 'current_contract_capability'
    capability_paths = {
        'BC02': {'TestR5LegacyBCCContractCapabilities/BC02/trusted_structured_source'},
        'BC03': {'TestR5LegacyBCCContractCapabilities/BC03/missing_source',
                 'TestR5LegacyBCCContractCapabilities/BC03/same_id_foreign_source'},
    }
    if adapter.get('evidence_scope') is not None and not capability:
        raise ValueError('unknown adapter evidence scope')
    if capability:
        if (row['id'] not in capability_paths
                or adapter.get('source') != 'internal/store/postgres/r5_legacy_bcc_contract_baseline_test.go'
                or adapter.get('package') != './internal/store/postgres'
                or adapter.get('test') != 'TestR5LegacyBCCContractCapabilities'
                or adapter.get('build_tag') != 'r5protocol'
                or adapter.get('layers') != ['db', 'http']
                or adapter.get('runner') is not None
                or adapter.get('component_source') is not None
                or adapter.get('consumes_shared_input') is not True
                or set(adapter.get('runtime_test_paths', [])) != capability_paths[row['id']]
                or len(adapter.get('runtime_test_paths', [])) != len(capability_paths[row['id']])
                or set(targets) != capability_paths[row['id']]):
            raise ValueError('current capability requires source-bound exact BC02/BC03 adapter')
    for path, target in targets.items():
        if path not in adapter.get('runtime_test_paths', []):
            raise ValueError('adapter target path is not a declared runtime path')
        marker_ok = (isinstance(target, dict) and (
            target.get('marker') == 'R5_PROTOCOL_CAPABILITY_TARGET_' + row['id'] if capability
            else bool(re.fullmatch(r'R5_PROTOCOL_UI_TARGET_[A-Z0-9_]+', target.get('marker', '')))))
        if not marker_ok:
            raise ValueError('adapter requires exact scoped target marker')
        if target.get('audit') not in row['audit'] or target.get('acceptance') not in row['acceptance'] or not re.fullmatch(r'R5-P[0-9]+-[0-9]+',target.get('task','')):
            raise ValueError('adapter target needs row audit/acceptance and successor task')
    return targets


def load_cases(path=CASES, root=ROOT):
    data = json.loads(Path(path).read_text())
    if data.get('schema_version') != 1 or data.get('task_complete') is not False:
        raise ValueError('schema v1 and explicit task_complete=false required')
    rows = data.get('cases', [])
    ids = [row.get('id') for row in rows]
    if len(ids) != len(set(ids)) or set(ids) != EXPECTED_IDS:
        raise ValueError('missing, unexpected or duplicate protocol case')
    for row in rows:
        if not isinstance(row.get('input'), dict) or not row['input']:
            raise ValueError(f"{row['id']}: structured input required")
        if not row.get('acceptance') or not row.get('required_layers'):
            raise ValueError(f"{row['id']}: acceptance/layers required")
        output = row.get('expected', {})
        status = output.get('http_status')
        if output.get('http_status_kind') not in {'exact','class','variant_exact','not_applicable','ordered_outcome_exact','unresolved'}:
            raise ValueError('unknown HTTP applicability/status mode')
        if output.get('http_status_kind') == 'not_applicable' and status is not None:
            raise ValueError('non-HTTP operation cannot assert HTTP status')
        if output.get('http_status_kind') == 'ordered_outcome_exact':
            outcomes = output.get('ordered_outcomes', [])
            if {o.get('order') for o in outcomes} != {'save_before_revoke','revoke_before_save'} or any(o.get('http_status') not in {200,403} for o in outcomes):
                raise ValueError('ordered outcomes must freeze both exact safe HTTP orders')
        if status is not None and (type(status) is not int or status < 200 or status > 599):
            raise ValueError(f"{row['id']}: invalid status")
        if not output.get('assertion') or not output.get('semantic_code'):
            raise ValueError(f"{row['id']}: output assertion/rejection category required")
        wire = output.get('wire_error', {})
        mode = wire.get('reason_mode')
        if mode not in {'absent','optional','exact','not_applicable'}:
            raise ValueError(f"{row['id']}: explicit reason presence policy required")
        if status is not None and status >= 400:
            if not wire.get('code') or wire.get('message_mode') != 'required_nonempty':
                raise ValueError(f"{row['id']}: error.code and error.message are required")
            if mode == 'not_applicable':
                raise ValueError(f"{row['id']}: error reason presence policy missing")
        if mode == 'exact' and not wire.get('reason'):
            raise ValueError(f"{row['id']}: exact reason value required")
        if mode in {'absent','not_applicable'} and wire.get('reason') is not None:
            raise ValueError(f"{row['id']}: absent reason cannot have a value")
        if output.get('http_status_kind') == 'variant_exact':
            variants = output.get('variants', [])
            names = [v.get('name') for v in variants]
            if not variants or len(names) != len(set(names)) or set(names) != {v['name'] for v in row['input'].get('variants', [])}:
                raise ValueError('variant-exact response requires the precise input variant set')
            if any(type(v.get('status')) is not int or not 400 <= v['status'] <= 599 or not v.get('code') for v in variants):
                raise ValueError('variant-exact rejection needs status and error.code')
        target = row.get('baseline_target_failure')
        if target and (not target.get('marker') or not target.get('task') or not target.get('acceptance') or target.get('audit') not in row['audit']):
            raise ValueError('target red requires exact marker, audit, acceptance and successor task')
        if target and (not target.get('test_paths') or any(not re.fullmatch(r'Test[A-Za-z0-9_]+/'+re.escape(row['id'])+r'(?:/(?:[A-Za-z0-9_-]+|\[\]))*', path) for path in target['test_paths'])):
            raise ValueError('target red requires exact named runtime test paths')
        for adapter in row.get('reference_adapters', []) + row.get('shared_adapters', []):
            shared = adapter in row.get('shared_adapters', [])
            if adapter.get('consumes_shared_input') is not shared:
                raise ValueError('adapter shared-input declaration contradicts kind')
            source = (root / adapter['source']).resolve()
            if not source.is_relative_to(root.resolve()) or not source.is_file():
                raise ValueError('adapter source missing or escapes root')
            if shared and adapter.get('runner') != 'vitest':
                paths = adapter.get('runtime_test_paths', [])
                prefix = adapter['test']+'/'+row['id']
                if not paths or len(paths) != len(set(paths)) or any(path != prefix and not path.startswith(prefix+'/') for path in paths):
                    raise ValueError('shared adapter requires exact per-case runtime paths')
            validate_adapter_targets(row,adapter)
            if adapter.get('component_source'):
                component = (root / adapter['component_source']).resolve()
                if not shared or adapter.get('runner') == 'vitest' or adapter['layers'] != ['components'] or not component.is_relative_to(root.resolve()) or component.suffix != '.tsx' or not component.is_file():
                    raise ValueError('invalid Go-owned component adapter/source')
            if adapter.get('runner') == 'vitest':
                if not shared or source.suffix != '.tsx' or adapter['layers'] != ['components']:
                    raise ValueError('invalid shared component adapter')
                continue
            package = './' + str(source.parent.relative_to(root))
            if adapter['package'] != package or not re.fullmatch(r'Test[A-Za-z0-9_]+', adapter['test']):
                raise ValueError('adapter package/test mismatch')
            pattern = r'^func ' + re.escape(adapter['test']) + r'\(t \*testing\.T\)'
            if not re.search(pattern, source.read_text(), re.M):
                raise ValueError(f"missing Go test symbol {adapter['test']}")
    protocol_file = root / 'docs/company-mail/R5-PROTOCOL.md'
    markdown_cases = dict(re.findall(r'^\| ((?:CT|RC|OP|RT|BC|GC|LF|PE)\d\d) \| .*? \| (.*?) \| .*? \|$', protocol_file.read_text(), re.M))
    if set(markdown_cases) != EXPECTED_IDS:
        raise ValueError('Markdown and machine case sets differ')
    for row in rows:
        if row['expected']['assertion'] != markdown_cases[row['id']]:
            raise ValueError(f"{row['id']}: Markdown/machine output drift")
    return data


SOURCE_MANIFEST = None
SOURCE_MANIFEST_SHA256 = None
SOURCE_IDENTITY = {}
SOURCE_POLICY = None
PROTOCOL_FIXED_SOURCES = {
    'go.mod', 'go.sum', 'docs/company-mail/R5-PROTOCOL.md',
    'docs/company-mail/evidence/R5-PROTOCOL-CASES.json',
    'scripts/check_r5_protocol.py', 'scripts/tests/test_r5_protocol.py',
    'scripts/tests/test_r5_protocol_component_evidence.py',
    'scripts/tests/test_r5_protocol_source_inventory.py',
}
PROTOCOL_ARTIFACT_DIRS = {'node_modules', '.next', 'out', '.vercel', 'coverage', 'build', '__pycache__', '.git'}


def protocol_source_paths(root):
    """Archived v1 path scope; never a fallback for current v2 evidence.

    This never supplies a fallback inventory or source identity. Runtime/build
    artifacts and secret env files are excluded; .env.example remains source.
    """
    names = set(PROTOCOL_FIXED_SOURCES)
    def walk_error(error):
        raise error
    for directory in ['cmd', 'internal', 'web']:
        base = root / directory
        if not base.is_dir() or base.is_symlink():
            raise ValueError('required protocol source directory missing or symlinked: '+directory)
        for current, dirs, files in os.walk(base, followlinks=False, onerror=walk_error):
            dirs[:] = sorted(d for d in dirs if d not in PROTOCOL_ARTIFACT_DIRS)
            for d in dirs:
                if (Path(current)/d).is_symlink():
                    raise ValueError('symlinked protocol source directory')
            for filename in files:
                if (filename == '.DS_Store' or filename == 'next-env.d.ts'
                        or filename.endswith(('.log', '.tsbuildinfo'))
                        or filename.startswith('.env') and filename != '.env.example'):
                    continue
                names.add((Path(current)/filename).relative_to(root).as_posix())
    return names


def strict_manifest_json(raw):
    def unique(pairs):
        result = {}
        for key, value in pairs:
            if key in result: raise ValueError('duplicate manifest key: '+key)
            result[key] = value
        return result
    return json.loads(raw, object_pairs_hook=unique)


def manifest_source_closure(path, pinned_sha256, root):
    """Read-only archived v1 scope, or explicitly matched v2/v3 source policy."""
    if not isinstance(pinned_sha256, str) or not re.fullmatch('[0-9a-f]{64}', pinned_sha256):
        raise ValueError('external manifest SHA256 required')
    raw = Path(path).read_bytes()
    if hashlib.sha256(raw).hexdigest() != pinned_sha256:
        raise ValueError('explicit manifest byte hash differs from supplied proof')
    manifest = strict_manifest_json(raw)
    if isinstance(manifest, dict) and manifest.get('schema_version') in {2, source_inventory.CURRENT_SCHEMA_VERSION}:
        receipt = source_inventory.validate_current_source(manifest, root, purpose='protocol', policy=SOURCE_POLICY)
        return receipt['files'], current_source_metadata(receipt, pinned_sha256)
    keys = {'schema_version','snapshot_root','source_identity_kind','source_sha','files'}
    if not isinstance(manifest, dict) or set(manifest) != keys or type(manifest['schema_version']) is not int or manifest['schema_version'] != 1:
        raise ValueError('exact protocol manifest schema v1 required')
    if manifest['snapshot_root'] != str(root.resolve()) or root.is_symlink():
        raise ValueError('manifest belongs to another actual snapshot root')
    if manifest['source_identity_kind'] != 'canonical_protocol_dependency_closure_sha256':
        raise ValueError('canonical protocol closure identity required, not Git HEAD/commit/deployment')
    files = manifest['files']
    if not isinstance(files, dict) or not files:
        raise ValueError('explicit nonempty source mapping required')
    for name, digest in files.items():
        if (not isinstance(name,str) or not name or Path(name).is_absolute()
                or '..' in Path(name).parts or Path(name).as_posix() != name
                or any(part in {'','.'} for part in name.split('/'))
                or not isinstance(digest,str) or not re.fullmatch('[0-9a-f]{64}',digest)):
            raise ValueError('noncanonical source path/hash in explicit manifest')
        actual = root / name
        if not actual.resolve().is_relative_to(root.resolve()) or not actual.is_file():
            raise ValueError('manifest source missing or escapes actual root: '+name)
        current = actual
        while current != root:
            if current.is_symlink(): raise ValueError('symlinked manifest source: '+name)
            current = current.parent
    expected = protocol_source_paths(root)
    if set(files) != expected:
        raise ValueError('protocol source manifest has missing or extra important paths')
    cases = strict_manifest_json((root/'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').read_bytes())
    for row in cases.get('cases', []):
        for adapter in row.get('reference_adapters', []) + row.get('shared_adapters', []):
            for field in ['source','component_source']:
                if adapter.get(field) and adapter[field] not in files:
                    raise ValueError('adapter dependency omitted from explicit manifest')
    observed = {name:hashlib.sha256((root/name).read_bytes()).hexdigest() for name in sorted(files)}
    if observed != files:
        raise ValueError('actual protocol source bytes drifted from explicit manifest')
    digest = hashlib.sha256(json.dumps(files,sort_keys=True,separators=(',',':')).encode()).hexdigest()
    if manifest['source_sha'] != digest:
        raise ValueError('canonical protocol source identity differs from closure')
    return observed, {'source_sha':digest,'source_identity_kind':manifest['source_identity_kind'],
                      'source_manifest_sha256':pinned_sha256,'snapshot_root':str(root.resolve()),
                      'source_identity_boundary':'Archived v1 scoped inventory only; excludes undeclared local replacements and is not current complete certification'}


def current_source_metadata(receipt, pinned_sha256):
    # A source scope is not the runtime/target-red report boundary. Never let
    # receipt metadata overwrite the independently classified execution scope.
    identity = {key:value for key,value in receipt.items() if key not in {'files','boundary'}}
    identity['source_identity_boundary'] = receipt['boundary']
    identity['source_manifest_sha256'] = pinned_sha256
    return identity


def protocol_source_metadata():
    if SOURCE_POLICY not in source_inventory.SUPPORTED_POLICIES or not SOURCE_IDENTITY or SOURCE_IDENTITY.get('policy') != SOURCE_POLICY:
        raise ValueError('protocol evidence requires an explicit matching source policy and receipt')
    return dict(SOURCE_IDENTITY)


def source_closure():
    global SOURCE_IDENTITY
    if SOURCE_POLICY not in source_inventory.SUPPORTED_POLICIES or SOURCE_MANIFEST is None:
        raise ValueError('current protocol requires explicit source policy and manifest; no Git/v1 fallback')
    receipt = source_inventory.load_current_source(SOURCE_MANIFEST, SOURCE_MANIFEST_SHA256, ROOT,
                                                  purpose='protocol', policy=SOURCE_POLICY)
    SOURCE_IDENTITY = current_source_metadata(receipt, SOURCE_MANIFEST_SHA256)
    return receipt['files']


def current_protocol_environment(tag_sets):
    context = SOURCE_IDENTITY.get('build_context')
    if not context or [list(tags) for tags in sorted(tag_sets)] != context['build_tag_sets']:
        raise ValueError('selected protocol adapters differ from pinned build tags')
    return source_inventory.bound_environment(context, os.environ)


def classify_events(text, package, expected, exit_code):
    """Require exact named executions, reject skip/missing/setup failures."""
    states = {test: [] for test in expected}
    errors = []
    package_states = []
    for line in text.splitlines():
        if not line.strip():
            continue
        event = json.loads(line)
        if not isinstance(event, dict) or event.get('Package') != package:
            errors.append('unexpected event/package')
            continue
        test, action = event.get('Test'), event.get('Action')
        if test:
            parent = test.split('/')[0]
            if parent not in states:
                errors.append(f'unexpected test: {test}')
            elif test == parent and action in {'run','pass','fail','skip'}:
                states[parent].append(action)
            elif action in {'fail','skip'}:
                errors.append(f'subtest {action}: {test}')
        elif action in {'pass','fail','skip'}:
            package_states.append(action)
    if exit_code != 0:
        errors.append(f'Go exit {exit_code}; target failure is not protocol success')
    if package_states != ['pass']:
        errors.append('missing/duplicate/nonpassing package completion')
    for test, actions in states.items():
        if actions != ['run','pass']:
            errors.append(f'{test}: expected exact run/pass, got {actions}')
    return {'status':'reference_tests_passed' if not errors else 'invalid_or_failing_reference_evidence',
            'task_complete':False, 'errors':errors, 'tests':states,
            'log_sha256':hashlib.sha256(text.encode()).hexdigest()}


def summary(data):
    rows = data['cases']
    return {'status':'structure_checked_only', 'task_complete':False,
            'cases':len(rows), 'cases_with_reference_adapters':sum(bool(x['reference_adapters']) for x in rows),
            'shared_input_verified_cases':0,
            'cases_with_shared_input_adapters':sum(bool(x.get('shared_adapters')) for x in rows),
            'missing_shared_input_adapters':[x['id'] for x in rows if not x.get('shared_adapters')],
            'unresolved_codes':[x['id'] for x in rows if x['expected'].get('http_status_kind') == 'unresolved'],
            'declared_applicability':{x['id']:x.get('applicability',{}) for x in rows if x.get('applicability')},
            'remaining_semantic_or_layer_gaps':{x['id']:x.get('unresolved',[]) for x in rows if x.get('unresolved')},
            'boundary':'Structure checks are not execution evidence. Declared shared consumers require fresh scoped unit/DB/HTTP/component execution and explicit target-red classification.'}


def run_references(data, layer, output):
    selected = {}
    for row in data['cases']:
        for a in row['reference_adapters']:
            if layer in a['layers']:
                selected[(a['package'], a.get('build_tag',''))] = selected.get((a['package'], a.get('build_tag','')), set()) | {a['test']}
    if not selected:
        raise ValueError('no reference adapters selected')
    if layer != 'unit' and not os.environ.get('TABMAIL_TEST_DB_DSN'):
        raise ValueError('explicit disposable TABMAIL_TEST_DB_DSN required; no silent skip')
    output.mkdir(parents=True, exist_ok=False)
    tested_closure = source_closure()
    identity = protocol_source_metadata()
    source = identity['source_sha']
    dirty = None  # v2 source identity is manifest-bound, never a Git fallback.
    env = current_protocol_environment(sorted({(tag,) if tag else () for _, tag in selected}))
    reports = []
    for index, ((package, tag), tests) in enumerate(sorted(selected.items())):
        cmd = ['go','test','-json','-race','-count=1','-timeout=240s']
        if tag:
            cmd += ['-tags='+tag]
        cmd += ['-run','^('+ '|'.join(sorted(tests)) + ')$',package]
        env['TABMAIL_R5_COMPONENT_EVIDENCE'] = str((output/'components').resolve())
        result = subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,text=True,timeout=300)
        (output/f'{index}.jsonl').write_text(result.stdout)
        (output/f'{index}.stderr').write_text(result.stderr)
        report = classify_events(result.stdout,'tabmail/' + package.removeprefix('./'),tests,result.returncode)
        report.update(command=cmd,package=package)
        reports.append(report)
    final_closure = source_closure()
    if tested_closure != final_closure:
        reports.append({'errors':['source changed during reference execution; rerun after integration'],'task_complete':False})
    report = summary(data)
    report.update(source_closure_before=tested_closure,source_closure_after=final_closure,status='reference_run_passed' if all(not x['errors'] for x in reports) else 'reference_run_failed',
                  source_sha=source,dirty_paths=dirty,layer=layer,reports=reports,
                  cases_sha256=hashlib.sha256(CASES.read_bytes()).hexdigest(),
                  adapter_source_sha256={a['source']:hashlib.sha256((ROOT/a['source']).read_bytes()).hexdigest() for row in data['cases'] for a in row['reference_adapters']})
    report.update(identity)
    (output/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    return report


def classify_components(data, expected, exit_code):
    errors = []
    rows = [a for suite in data.get('testResults', []) for a in suite.get('assertionResults', [])]
    states = {name: [] for name in expected}
    if exit_code != 0 or data.get('success') is not True:
        errors.append('component process/report failed')
    for row in rows:
        name = row.get('fullName')
        if name not in states:
            errors.append(f'unexpected component assertion {name}')
        else:
            states[name].append(row.get('status'))
    for name, statuses in states.items():
        if statuses != ['passed']:
            errors.append(f'{name}: missing/duplicate/skipped/failing shared component: {statuses}')
    return {'status':'shared_components_passed' if not errors else 'shared_components_failed',
            'errors':errors,'tests':states,'task_complete':False}


def classify_shared_events(text, package, expected, exit_code, data):
    """Admit only exact, documented target assertions; never setup failures."""
    allowed = {}
    for row in data['cases']:
        for adapter in row.get('shared_adapters', []):
            if adapter.get('package') != './'+package.removeprefix('tabmail/') or adapter.get('test') not in expected:
                continue
            targets = validate_adapter_targets(row,adapter)
            original = row.get('baseline_target_failure')
            if original:
                for path in original.get('test_paths', []):
                    if path in adapter.get('runtime_test_paths', []):
                        if path in allowed and allowed[path] != original['marker']:
                            raise ValueError('conflicting exact target markers')
                        allowed[path] = original['marker']
            for path,target in targets.items():
                if path in allowed and allowed[path] != target['marker']:
                    raise ValueError('conflicting exact target markers')
                allowed[path] = target['marker']
    events = [json.loads(line) for line in text.splitlines() if line.strip()]
    infrastructure = ['WARNING: DATA RACE', 'panic:', 'runtime error:', 'fatal error:', 'test timed out', '[build failed]', 'context deadline exceeded', 'deadline exceeded']
    if any(value in ''.join(e.get('Output','') for e in events) for value in infrastructure):
        result = classify_events(text, package, expected, exit_code)
        result['errors'].append('infrastructure/race/panic/timeout failure is never target red')
        result.update(product_green=False, process_exit_code=exit_code)
        return result
    failures = {e.get('Test') for e in events if e.get('Package') == package and e.get('Test') and e.get('Action') == 'fail'}
    red = {}
    errors = []
    required_paths = {path for row in data['cases'] for a in row.get('shared_adapters', [])
                      if a.get('runner') != 'vitest' and a.get('package') == './'+package.removeprefix('tabmail/')
                      and a['test'] in expected for path in a.get('runtime_test_paths', [])}
    if not required_paths:
        errors.append('no matching declared runtime shared consumers')
    for path in sorted(required_paths):
        lifecycle = [e['Action'] for e in events if e.get('Test') == path and e.get('Action') in {'run','pass','fail','skip'}]
        if lifecycle not in [['run','pass'], ['run','fail']]:
            errors.append('missing/duplicate/skipped case variant: '+path+': '+str(lifecycle))
    # A failing leaf never launders a fatal assertion in its parent, cleanup,
    # or package output. Inspect diagnostics across ALL scopes first.
    for event in events:
        if event.get('Action') != 'output':
            continue
        name = event.get('Test')
        marker = allowed.get(name)
        diagnostics = re.findall(r'[\w.-]+\.go:\d+: ([^\n]*)', event.get('Output',''))
        if diagnostics and (name in failures or not name) and (not marker or any(not d.startswith(marker+':') for d in diagnostics)):
            errors.append('non-target assertion diagnostics: '+str(name))
        if not name and re.search(r'(^|\n)(?:# |.*\.go:\d+:|FAIL\s+.*\[)', event.get('Output','')):
            errors.append('package/build diagnostics outside admitted target test')
    for name in sorted(failures):
        descendants = [f for f in failures if f.startswith(name+'/')]
        if descendants:
            continue  # Aggregate failures are admitted only through all leaves.
        marker = allowed.get(name)
        output = ''.join(e.get('Output','') for e in events if e.get('Test') == name and e.get('Action') == 'output')
        diagnostics = re.findall(r'[\w.-]+\.go:\d+: ([^\n]*)', output)
        if not marker or not diagnostics or any(not d.startswith(marker+':') for d in diagnostics):
            errors.append('non-target failure: '+name)
        else:
            red[name] = marker
    if errors or not red:
        result = classify_events(text, package, expected, exit_code)
        result['errors'] += errors
        result['target_red'] = {}
        result.update(product_green=not result['errors'], process_exit_code=exit_code)
        return result
    # Transform admitted target failures only for structural lifecycle checks.
    # Original JSONL remains immutable and the result explicitly records red.
    for e in events:
        if e.get('Action') == 'fail' and (not e.get('Test') or e.get('Test') in failures):
            e['Action'] = 'pass'
    result = classify_events('\n'.join(json.dumps(e) for e in events), package, expected, 0 if exit_code == 1 else exit_code)
    result['log_sha256'] = hashlib.sha256(text.encode()).hexdigest()
    result['target_red'] = red
    result['status'] = 'shared_baseline_target_red' if not result['errors'] else 'shared_baseline_invalid'
    result['product_green'] = False
    result['process_exit_code'] = exit_code
    return result


def shared_command(package, tag, tests):
    if tag and not re.fullmatch(r'[A-Za-z0-9_]+', tag):
        raise ValueError('invalid shared build tag')
    cmd = ['go','test','-json','-race','-count=1','-timeout=120s']
    if tag:
        cmd.append('-tags='+tag)
    return cmd + ['-run','^('+ '|'.join(sorted(tests)) + ')$',package]


def classify_go_component_packets(data, selected, output, cases_hash):
    # Import the tested pure validator; it never supplies response data.
    import importlib.util
    module_path = ROOT/'scripts/tests/test_r5_protocol_component_evidence.py'
    spec = importlib.util.spec_from_file_location('r5_component_packet_validation',module_path)
    validator = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(validator)
    reports=[];verified=set()
    for row in data['cases']:
        for adapter in row.get('shared_adapters', []):
            if not adapter.get('component_source') or adapter['test'] not in selected.get((adapter['package'],adapter.get('build_tag','')),set()):
                continue
            prefix = adapter['test']+'/'+row['id']+'/'
            for path in adapter['runtime_test_paths']:
                variant = path.removeprefix(prefix)
                directory = output/'http-pg-components'/row['id']/variant.replace('[]','empty_array')
                try:
                    observed=json.loads((directory/'observations.json').read_text())
                    vitest=json.loads((directory/'vitest.json').read_text())
                    report=validator.classify_packet(observed,vitest,observed.get('component_process_exit_code'),row['id'],variant,cases_hash)
                    declared=adapter.get('target_failures',{}).get(path,{}).get('marker')
                    if report.get('target_marker') and report['target_marker'] != declared:
                        report['errors'].append('component marker is not declared for this exact adapter path')
                except (OSError,ValueError,TypeError,KeyError) as error:
                    report={'errors':['missing/invalid real Go-owned component packet: '+type(error).__name__],'status':'invalid','product_green':False,'task_complete':False}
                report.update(case_id=row['id'],variant=variant,runtime_test_path=path)
                reports.append(report)
            case_reports=[r for r in reports if r.get('case_id')==row['id']]
            if case_reports and all(not r['errors'] for r in case_reports):verified.add(row['id'])
    return reports,verified


def external_component_command(manifest, report, *, probe=False):
    prefix = [manifest['node']['path'], manifest['cli']['path'], 'run', '--cache=false', '--experimental.fsModuleCache=false']
    if probe:
        return prefix + ['--config','vitest.r5external-probe.config.ts','--reporter=json','--outputFile',str(report)]
    return prefix + ['components/company/r5-protocol.test.tsx','--reporter=json','--outputFile='+str(report)]


def run_shared(data, layer, output):
    selected = {}
    for row in data['cases']:
        for a in row.get('shared_adapters', []):
            if a.get('runner') != 'vitest' and (layer in a['layers'] or layer == 'components' and 'unit' in a['layers']):
                selected.setdefault((a['package'],a.get('build_tag','')), set()).add(a['test'])
    if not selected:
        raise ValueError('missing shared Go consumers')
    if layer in {'db','http'} and not os.environ.get('TABMAIL_TEST_DB_DSN'):
        raise ValueError('explicit disposable TABMAIL_TEST_DB_DSN required; no silent skip')
    output.mkdir(parents=True, exist_ok=False)
    tested_closure = source_closure()
    cases_hash = hashlib.sha256(CASES.read_bytes()).hexdigest()
    identity = protocol_source_metadata()
    source = identity['source_sha']
    env = current_protocol_environment(sorted({(tag,) if tag else () for _, tag in selected}))
    external_runtime = None
    if layer == 'components':
        sys.path.insert(0, str(ROOT/'scripts/preparation'))
        import r5_external_runtime
        selected_binding_version = r5_external_runtime.environment_version()
        external_runtime = r5_external_runtime.from_environment(ROOT, selected_binding_version=selected_binding_version)
        r5_external_runtime.validate(external_runtime, selected_binding_version=selected_binding_version)
    env['TABMAIL_R5_PROTOCOL_OBSERVATIONS'] = str((output/'observations.json').resolve())
    env['TABMAIL_R5_PROTOCOL_COMPONENT_EVIDENCE'] = str((output/'http-pg-components').resolve())
    reports = []
    for index, ((package, tag), tests) in enumerate(sorted(selected.items())):
        cmd = shared_command(package,tag,tests)
        result = subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,text=True,timeout=180)
        (output/f'go-{index}.jsonl').write_text(result.stdout)
        (output/f'go-{index}.stderr').write_text(result.stderr)
        report = classify_shared_events(result.stdout,'tabmail/'+package.removeprefix('./'),tests,result.returncode,data)
        report.update(command=cmd,package=package)
        reports.append(report)
    go_pass = all(not r['errors'] for r in reports)
    go_component_verified=set()
    if layer == 'components':
        packets,go_component_verified=classify_go_component_packets(data,selected,output,cases_hash)
        reports.extend(packets)
    component_pass = False
    if layer == 'components' and go_pass:
        component_file = output/'vitest.json'
        cmd = external_component_command(external_runtime,component_file.resolve())
        result = r5_external_runtime.launch(external_runtime,cmd,env,selected_binding_version=selected_binding_version)
        (output/'vitest.stdout').write_text(result.stdout)
        (output/'vitest.stderr').write_text(result.stderr)
        names = {'R5 shared receipt '+row['id'] for row in data['cases']
                 if any(a.get('runner')=='vitest' for a in row.get('shared_adapters', []))}
        component = classify_components(json.loads(component_file.read_text()),names,result.returncode)
        component.update(command=cmd)
        reports.append(component)
        component_pass = not component['errors']
    if external_runtime is not None:
        r5_external_runtime.validate(external_runtime, selected_binding_version=selected_binding_version)
    final_closure = source_closure()
    if tested_closure != final_closure or hashlib.sha256(CASES.read_bytes()).hexdigest()!=cases_hash:
        reports.append({'errors':['source/shared input changed during execution; rerun after integration'],'task_complete':False})
    verified = {}
    stable = tested_closure == final_closure and hashlib.sha256(CASES.read_bytes()).hexdigest()==cases_hash
    for row in data['cases']:
        layers = set()
        for adapter in row.get('shared_adapters', []):
            if stable and go_pass and adapter.get('runner')!='vitest' and adapter['test'] in selected.get((adapter['package'],adapter.get('build_tag','')),set()) and (not adapter.get('component_source') or row['id'] in go_component_verified): layers.update(adapter['layers'])
            if stable and component_pass and adapter.get('runner')=='vitest': layers.add('components')
        if layers: verified[row['id']] = sorted(layers)
    report = summary(data)
    report.update(status='shared_scoped_evidence_passed' if all(not r['errors'] for r in reports) else 'shared_scoped_evidence_failed',
                  task_complete=False,product_green=bool(reports) and stable and go_pass and all(not r['errors'] for r in reports) and not any(r.get('target_red') for r in reports),source_sha=source,layer=layer,source_closure_before=tested_closure,
                  source_closure_after=final_closure,cases_sha256=cases_hash,reports=reports,
                  shared_input_verified_cases=len(verified),shared_input_verified_layers=verified,
                  missing_required_layers={row['id']:sorted(set(row['required_layers'])-set(verified.get(row['id'],[]))) for row in data['cases']},
                  boundary='Only the recorded scoped layers and exact runtime paths are verified. Accepted target red is not product green; missing component journeys and future backfill/migration implementation remain separate gaps.')
    report.update(identity)
    (output/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    return report


def main():
    global SOURCE_MANIFEST, SOURCE_MANIFEST_SHA256, SOURCE_POLICY
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', choices=['unit','db','http','components','shared-unit','shared-components','shared-db','shared-http'])
    parser.add_argument('--output-dir',type=Path)
    parser.add_argument('--source-policy', choices=source_inventory.SUPPORTED_POLICIES)
    parser.add_argument('--source-manifest',type=Path)
    parser.add_argument('--source-manifest-sha256')
    args = parser.parse_args()
    if (args.source_manifest is None) != (args.source_manifest_sha256 is None):
        parser.error('--source-manifest and --source-manifest-sha256 must be supplied together')
    SOURCE_MANIFEST, SOURCE_MANIFEST_SHA256 = args.source_manifest, args.source_manifest_sha256
    SOURCE_POLICY = args.source_policy
    if args.run and (SOURCE_POLICY not in source_inventory.SUPPORTED_POLICIES or SOURCE_MANIFEST is None):
        parser.error('execution requires explicit --source-policy and matching versioned source manifest')
    if SOURCE_MANIFEST is not None:
        source_closure()
    if args.run and args.output_dir is None:
        parser.error('--run requires fresh --output-dir')
    data = load_cases()
    if args.run and args.run.startswith('shared-'):
        report = run_shared(data,args.run.removeprefix('shared-'),args.output_dir)
    else:
        report = run_references(data,args.run,args.output_dir) if args.run else summary(data)
    print(json.dumps(report,ensure_ascii=False,indent=2))
    return int(report['status'] in {'reference_run_failed','shared_scoped_evidence_failed'})

if __name__=='__main__':
    try:
        raise SystemExit(main())
    except (OSError,ValueError,subprocess.SubprocessError) as exc:
        print(f'protocol check failed: {type(exc).__name__}: {exc}',file=sys.stderr)
        raise SystemExit(1)
