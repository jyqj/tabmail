"""Fresh pure-suite orchestration and read-only Git/AST integration inventory.

Each child installs its OS-process audit guard before consumer imports. Python
suite launches and Git metadata reads here are outside those guarded children.
Historical evidence is never rewritten. No consumer entrypoint is executed.
"""
import ast
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys

ROOT = Path(__file__).resolve().parents[4]
OUT = Path(__file__).resolve().parent
BASE = '632d55de566254c618bca9a54c5dbae7b355fcf9'
FREEZE = '8182e40d3b1f135415e644e122791539ab75fc1b'
PREP_SOURCE = 'cad567ff2bb224b94fbf20c359383edcd0fce59b'
PREP_DELIVERY = '7262e78e0120c5a203bb588807ca876b82c4d5bc'
PREP_REVIEW = 'f15468a0c06160dc3155e386c5f0dc8d7a862f54'
RUNTIME_SOURCE = 'dcaf2bf91355d4c0524f34f0c767b7997a994833'
RUNTIME_DELIVERY = '97c54dc8dad6ab2da6d4dd29e43c20b7091db73f'
RUNTIME_REVIEW = 'd6cda3a4b458aecf44b01467970796a143e89cb4'
DESIGN = '42f49538f7bceb6eb8e698d716c1418f85343413'

def git(*args):
    return subprocess.check_output(['git', *args], cwd=ROOT)

def tree(commit):
    result = {}
    for row in git('ls-tree', '-r', '-z', commit).split(b'\0'):
        if row:
            metadata, path = row.split(b'\t')
            mode, kind, oid = metadata.decode().split()
            result[path.decode()] = dict(mode=mode, kind=kind, git_blob=oid)
    return result

def blob(commit, path):
    return git('show', commit + ':' + path)

def sha(raw):
    return hashlib.sha256(raw).hexdigest()

def write(name, value):
    (OUT / name).write_text(json.dumps(value, indent=2, sort_keys=True) + '\n')

def main():
    baseline, prep, runtime, combined = map(tree, (BASE, PREP_REVIEW, RUNTIME_REVIEW, FREEZE))
    deltas = [{p for p in set(baseline) | set(t) if baseline.get(p) != t.get(p)} for t in (prep, runtime)]
    assert not deltas[0] & deltas[1], 'ownership collision'
    expected = dict(baseline)
    for t, delta in zip((prep, runtime), deltas):
        for path in delta:
            assert path in t, 'unexpected deletion'
            expected[path] = t[path]
    assert combined == expected, 'combined tree differs from exact reviewed union'
    for commit in (BASE, PREP_SOURCE, PREP_DELIVERY, PREP_REVIEW, RUNTIME_SOURCE, RUNTIME_DELIVERY, RUNTIME_REVIEW):
        subprocess.run(['git', 'merge-base', '--is-ancestor', commit, FREEZE], cwd=ROOT, check=True)
    for path, record in combined.items():
        assert (ROOT / path).is_file()
        assert git('hash-object', str(ROOT / path)).decode().strip() == record['git_blob'], path
        mode = '100755' if (ROOT / path).stat().st_mode & 0o111 else '100644'
        assert record['mode'] == mode, path
    preserved = {p: r for p, r in baseline.items() if p not in deltas[0] | deltas[1]}
    write('preserved-tree.json', dict(base_commit=BASE, combined_source_commit=FREEZE,
        unchanged_path_count=len(preserved), unchanged_git_blobs_and_modes=preserved))
    changed = {}
    for p in sorted(deltas[0] | deltas[1]):
        origin = PREP_REVIEW if p in deltas[0] else RUNTIME_REVIEW
        changed[p] = dict(**combined[p], reviewed_origin=origin, sha256=sha((ROOT / p).read_bytes()))
    prep_paths = ['scripts/r5_source_runner_prepare.py', 'scripts/run_r5_source_version_tests.py',
                  'scripts/tests/r5_source_preparation_v3_checks.py']
    runtime_paths = ['scripts/preparation/r5_external_runtime.py', 'scripts/preparation/run_r5_external_scoped.py',
                     'scripts/preparation/probe_r5_external_runtime.py', 'scripts/tests/r5_external_runtime_v3_checks.py']
    for source, delivery, paths in ((PREP_SOURCE, PREP_DELIVERY, prep_paths), (RUNTIME_SOURCE, RUNTIME_DELIVERY, runtime_paths)):
        for p in paths:
            assert blob(source, p) == blob(delivery, p) == (ROOT / p).read_bytes()
            ast.parse((ROOT / p).read_bytes(), filename=p)
    def defs(commit, path):
        return {n.name: ast.dump(n, include_attributes=False) for n in ast.parse(blob(commit, path)).body
                if isinstance(n, (ast.FunctionDef, ast.ClassDef))}
    checks = []
    for path, names in {
        prep_paths[0]: ['digest', 'exclusive', 'source_identity', 'typescript_members', 'prepare_typescript', 'verify_typescript', 'go_selection', 'execute_binary'],
        prep_paths[1]: ['flatten', 'discover', 'partition', 'verify', 'git', 'checkout', 'git_go_env', 'child'],
        runtime_paths[0]: ['Descriptors', 'descriptors', 'dependency_records', 'clean_environment', 'owner', 'canonical', 'digest'],
    }.items():
        a, b = defs(BASE, path), defs(FREEZE, path)
        for name in names:
            assert a[name] == b[name]
            checks.append(dict(path=path, definition=name, ast_unchanged=True))
    runtime_path = runtime_paths[0]
    def constructor(commit):
        return next(n for n in ast.walk(ast.parse(blob(commit, runtime_path))) if isinstance(n, ast.Call)
                    and isinstance(n.func, ast.Name) and n.func.id == 'dict'
                    and any(k.arg == 'concurrency_boundary' for k in n.keywords))
    prior, current = constructor(BASE), constructor(FREEZE)
    for k in current.keywords:
        if k.arg == 'schema_version': k.value = ast.Constant(value=2)
        if k.arg == 'policy': k.value = ast.Name(id='POLICY', ctx=ast.Load())
    assert ast.dump(prior) == ast.dump(current)
    for path, names in [(prep_paths[1], ['BASELINE', 'HISTORICAL_IDS', 'FRESH_CLASS']),
                         (prep_paths[0], ['LOCK_SHA256', 'TS_URL', 'TS_SHA256', 'TS_INTEGRITY', 'TEST_NAME'])]:
        def constants(commit):
            return {n.targets[0].id: ast.dump(n, include_attributes=False) for n in ast.parse(blob(commit, path)).body
                    if isinstance(n, ast.Assign) and isinstance(n.targets[0], ast.Name)}
        a, b = constants(BASE), constants(FREEZE)
        for name in names:
            assert a[name] == b[name]
            checks.append(dict(path=path, constant=name, ast_unchanged=True))
    # Byte equality covers the adapter, helpers, all frozen schemas, registry,
    # closure/inventory, batch, Go bridges, cases, TODO and historical evidence.
    for path in baseline:
        if path not in deltas[0] | deltas[1]:
            assert baseline[path] == combined[path]
    write('static-review.json', dict(base_commit=BASE, combined_source_commit=FREEZE,
        combined_source_tree=git('rev-parse', FREEZE + '^{tree}').decode().strip(), design_commit=DESIGN,
        preparation=dict(source=PREP_SOURCE, delivery=PREP_DELIVERY, independent=PREP_REVIEW),
        runtime=dict(source=RUNTIME_SOURCE, delivery=RUNTIME_DELIVERY, independent=RUNTIME_REVIEW),
        source_vs_delivery='distinct commits; all reviewed executable and control bytes equal',
        ancestry_preserved=True, semantic_resolutions=[], ownership_collisions=[], exact_reviewed_union=True,
        changed_paths=changed, unchanged_base_paths=len(preserved), AST_checks=checks,
        v2_manifest_constructor_ast_equal=True,
        preserved_tree_sha256=sha((OUT / 'preserved-tree.json').read_bytes()),
        central='10/171 unchanged', F52='historical failures unchanged', TODO='byte-identical; proposed substeps only'))
    suites = [
        ('preparation-author', prep_paths[2], 64),
        ('preparation-independent', 'docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-INDEPENDENT-20261004/independent_checks.py', 90),
        ('preparation-preserved', 'docs/company-mail/evidence/R5-V3-PREPARATION-SLICE2-INDEPENDENT-20261004/preserved_pure_checks.py', 15),
        ('runtime-author', runtime_paths[3], 30),
        ('runtime-independent', 'docs/company-mail/evidence/R5-RUNTIME-V3-INDEPENDENT-20261004/independent_checks.py', 13),
    ]
    results = []
    for name, path, count in suites:
        argv = [sys.executable, '-B', path]
        proc = subprocess.run(argv, cwd=ROOT, env=dict(os.environ, PYTHONDONTWRITEBYTECODE='1'),
                              stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=60)
        (OUT / (name + '.stdout')).write_bytes(proc.stdout)
        (OUT / (name + '.stderr')).write_bytes(proc.stderr)
        assert proc.returncode == 0, (name, proc.stderr.decode())
        record = json.loads(proc.stdout if name.startswith('preparation') else proc.stdout.splitlines()[-1])
        assert record.get('count', record.get('tests')) == count
        assert record['os_process_events'] == []
        assert record.get('failures', 0) == record.get('errors', 0) == 0
        results.append(dict(name=name, argv=argv, result=record, exit_code=proc.returncode,
                            control_source_sha256=sha((ROOT / path).read_bytes()),
                            stdout_sha256=sha(proc.stdout), stderr_sha256=sha(proc.stderr)))
        print(name, count, 'PASS; OS-process events 0', flush=True)
    subprocess.run(['git', 'diff', '--check'], cwd=ROOT, check=True)
    write('fresh-results.json', dict(combined_source_commit=FREEZE, suites=results,
        total_controls_or_test_methods=sum(x[2] for x in suites), total=212,
        counting='169 preparation named/mocked controls plus 43 runtime unittest methods; subtests not added',
        OS_process_events_in_guarded_children=[], orchestration='Python suite launches and read-only Git metadata outside child guards',
        actual_producer_capture_runtime_Go_Node_wire_PG_mail_formal_runs=False,
        synthetic_only=True, central='10/171 unchanged', F52='historical failures unchanged'))

if __name__ == '__main__':
    main()
