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

ROOT = Path(__file__).resolve().parents[1]
CASES = ROOT / 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'
EXPECTED_IDS = {f'{prefix}{i:02d}' for prefix, n in [('CT',10),('RC',5),('OP',4),('RT',7),('BC',6),('GC',2),('LF',7),('PE',5)] for i in range(1,n+1)}


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
        if status is not None and (type(status) is not int or status < 200 or status > 599):
            raise ValueError(f"{row['id']}: invalid status")
        if not output.get('assertion') or not output.get('semantic_code'):
            raise ValueError(f"{row['id']}: output assertion/rejection category required")
        if output.get('error_reason') is None and not row.get('unresolved'):
            raise ValueError(f"{row['id']}: unresolved literal reason must remain explicit")
        for adapter in row.get('reference_adapters', []) + row.get('shared_adapters', []):
            shared = adapter in row.get('shared_adapters', [])
            if adapter.get('consumes_shared_input') is not shared:
                raise ValueError('adapter shared-input declaration contradicts kind')
            source = (root / adapter['source']).resolve()
            if not source.is_relative_to(root.resolve()) or not source.is_file():
                raise ValueError('adapter source missing or escapes root')
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


def source_closure():
    names = subprocess.run(['git','ls-files','--cached','--others','--exclude-standard',
                            'cmd','internal','web','go.mod','go.sum'],cwd=ROOT,
                           capture_output=True,text=True,check=True).stdout.splitlines()
    return {name:hashlib.sha256((ROOT/name).read_bytes()).hexdigest()
            for name in sorted(set(names)) if (ROOT/name).is_file()}


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
            'missing_shared_input_adapters':[x['id'] for x in rows],
            'unresolved_codes':[x['id'] for x in rows if x['expected']['error_reason'] is None],
            'boundary':'Structure checks are not runtime evidence; shared adapters cover declared pure-policy/component scopes only, not DB/HTTP.'}


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
    source = subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True,text=True,check=True).stdout.strip()
    dirty = subprocess.run(['git','status','--porcelain'],cwd=ROOT,capture_output=True,text=True,check=True).stdout.splitlines()
    reports = []
    tested_closure = source_closure()
    for index, ((package, tag), tests) in enumerate(sorted(selected.items())):
        cmd = ['go','test','-json','-race','-count=1','-timeout=240s']
        if tag:
            cmd += ['-tags='+tag]
        cmd += ['-run','^('+ '|'.join(sorted(tests)) + ')$',package]
        env = dict(os.environ)
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


def run_shared(data, layer, output):
    selected = {}
    for row in data['cases']:
        for a in row.get('shared_adapters', []):
            if a.get('runner') != 'vitest':
                selected.setdefault(a['package'], set()).add(a['test'])
    if not selected:
        raise ValueError('missing shared Go consumers')
    output.mkdir(parents=True, exist_ok=False)
    tested_closure = source_closure()
    cases_hash = hashlib.sha256(CASES.read_bytes()).hexdigest()
    source = subprocess.run(['git','rev-parse','HEAD'],cwd=ROOT,capture_output=True,text=True,check=True).stdout.strip()
    env = dict(os.environ)
    env['TABMAIL_R5_PROTOCOL_OBSERVATIONS'] = str((output/'observations.json').resolve())
    reports = []
    for index, (package, tests) in enumerate(sorted(selected.items())):
        cmd = ['go','test','-json','-race','-count=1','-timeout=120s','-run','^('+ '|'.join(sorted(tests)) + ')$',package]
        result = subprocess.run(cmd,cwd=ROOT,env=env,capture_output=True,text=True,timeout=180)
        (output/f'go-{index}.jsonl').write_text(result.stdout)
        (output/f'go-{index}.stderr').write_text(result.stderr)
        report = classify_events(result.stdout,'tabmail/'+package.removeprefix('./'),tests,result.returncode)
        report.update(command=cmd,package=package)
        reports.append(report)
    go_pass = all(not r['errors'] for r in reports)
    component_pass = False
    if layer == 'components' and go_pass:
        component_file = output/'vitest.json'
        cmd = ['node','node_modules/vitest/vitest.mjs','run','components/company/r5-protocol.test.tsx',
               '--reporter=json','--outputFile='+str(component_file.resolve())]
        result = subprocess.run(cmd,cwd=ROOT/'web',env=env,capture_output=True,text=True,timeout=180)
        (output/'vitest.stdout').write_text(result.stdout)
        (output/'vitest.stderr').write_text(result.stderr)
        names = {'R5 shared receipt '+row['id'] for row in data['cases']
                 if any(a.get('runner')=='vitest' for a in row.get('shared_adapters', []))}
        component = classify_components(json.loads(component_file.read_text()),names,result.returncode)
        component.update(command=cmd)
        reports.append(component)
        component_pass = not component['errors']
    final_closure = source_closure()
    if tested_closure != final_closure or hashlib.sha256(CASES.read_bytes()).hexdigest()!=cases_hash:
        reports.append({'errors':['source/shared input changed during execution; rerun after integration'],'task_complete':False})
    verified = {}
    stable = tested_closure == final_closure and hashlib.sha256(CASES.read_bytes()).hexdigest()==cases_hash
    for row in data['cases']:
        layers = set()
        for adapter in row.get('shared_adapters', []):
            if stable and go_pass and adapter.get('runner')!='vitest': layers.add('unit')
            if stable and component_pass and adapter.get('runner')=='vitest': layers.add('components')
        if layers: verified[row['id']] = sorted(layers)
    report = summary(data)
    report.update(status='shared_scoped_evidence_passed' if all(not r['errors'] for r in reports) else 'shared_scoped_evidence_failed',
                  task_complete=False,source_sha=source,layer=layer,source_closure_before=tested_closure,
                  source_closure_after=final_closure,cases_sha256=cases_hash,reports=reports,
                  shared_input_verified_cases=len(verified),shared_input_verified_layers=verified,
                  missing_required_layers={row['id']:sorted(set(row['required_layers'])-set(verified.get(row['id'],[]))) for row in data['cases']},
                  boundary='Actual JSON-fed production policy and rendered component evidence. PostgreSQL/HTTP, credential admission, frozen reasons and other cases remain unverified.')
    (output/'report.json').write_text(json.dumps(report,ensure_ascii=False,indent=2)+'\n')
    return report


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--run', choices=['unit','db','http','components','shared-unit','shared-components'])
    parser.add_argument('--output-dir',type=Path)
    args = parser.parse_args()
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
