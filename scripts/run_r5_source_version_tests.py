"""Run every discovered test exactly once in its declared source checkout.

Current tests run in an independent clean Git clone at HEAD, before npm artifacts
can affect Go discovery. Only the four original selected v1 actual-root IDs run
in their frozen baseline clone. Preserve failures and skips as non-green evidence.
"""
from __future__ import annotations
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

BASELINE = '41b015c30c66b3ba58a3c1395e8559ebcd27a65f'
HISTORICAL_IDS = frozenset('test_r5_selected_source_binding.ActualRootBindingTests.'+name for name in (
    'test_actual_full_scope_fresh_hashes','test_real_receipt_roundtrip',
    'test_omission_extra_hash_flags_abi_and_historical_rejected','test_capture_midflight_drift_refused'))
FRESH_CLASS = 'test_r5_selected_source_binding_v2.ActualRootBindingV2Tests.'


def flatten(suite):
    for test in suite:
        if isinstance(test,unittest.TestSuite):
            yield from flatten(test)
        else:
            yield test


def discover(root):
    suite = unittest.TestLoader().discover(str(Path(root)/'scripts/tests'),pattern='test_*.py')
    tests = list(flatten(suite)); ids = [t.id() for t in tests]
    if any(i.startswith('unittest.loader._FailedTest') for i in ids):
        raise ValueError('test discovery import failure')
    if len(ids) != len(set(ids)):
        raise ValueError('duplicate discovered test ID')
    return sorted(ids)


def partition(ids):
    all_ids = set(ids)
    if not HISTORICAL_IDS.issubset(all_ids) or not any(i.startswith(FRESH_CLASS) for i in ids):
        raise ValueError('missing frozen or fresh actual-root tests')
    current = sorted(all_ids - HISTORICAL_IDS); frozen = sorted(HISTORICAL_IDS)
    verify(ids,current,frozen,BASELINE)
    return current,frozen



def verify(ids,current,frozen,baseline):
    if baseline != BASELINE or set(frozen) != HISTORICAL_IDS:
        raise ValueError('wrong frozen baseline or historical test set')
    if len(current+frozen) != len(set(current+frozen)):
        raise ValueError('overlap or duplicate test dispatch')
    if set(current+frozen) != set(ids):
        raise ValueError('missing or extra test dispatch')


def git(root,*args):
    return subprocess.check_output(['git','-C',str(root),*args],text=True).strip()


def checkout(root,destination,sha):
    subprocess.run(['git','clone','--quiet','--no-hardlinks','--no-checkout',str(root),str(destination)],check=True)
    subprocess.run(['git','-C',str(destination),'checkout','--quiet','--detach',sha],check=True)
    if git(destination,'rev-parse','HEAD') != sha or git(destination,'status','--porcelain','--untracked-files=all'):
        raise ValueError('independent checkout not exact/clean')


def git_go_env(go,key):
    return subprocess.check_output([go,'env',key],text=True).strip()


def child(root,ids,output):
    os.chdir(root);sys.path.insert(0,str(Path(root)/'scripts/tests'))
    # Frozen suite has historical cloud constants. Configure invocation paths
    # explicitly in memory; source/helper bytes and policy stay frozen.
    import importlib
    legacy = importlib.import_module('test_r5_selected_source_binding')
    go = os.environ.get('R5_TEST_GO','go')
    import shutil
    legacy.GO = Path(shutil.which(go) or go).resolve()
    legacy.CACHE = Path(os.environ.get('R5_TEST_CACHE') or git_go_env(go,'GOCACHE'))
    legacy.MODULECACHE = Path(os.environ.get('R5_TEST_MODULECACHE') or git_go_env(go,'GOMODCACHE'))
    suite = unittest.TestLoader().loadTestsFromNames(ids)
    actual = [t.id() for t in flatten(suite)]
    if actual != ids:
        raise ValueError('child actual test IDs differ')
    class ObservedResult(unittest.TextTestResult):
        def __init__(self,*args,**kwargs):
            super().__init__(*args,**kwargs); self.actual_ids=[]
        def startTest(self,test):
            self.actual_ids.append(test.id());super().startTest(test)
    result = unittest.TextTestRunner(verbosity=2,resultclass=ObservedResult).run(suite)
    report = dict(source_sha=git(root,'rev-parse','HEAD'),loaded_test_ids=actual,actual_test_ids=result.actual_ids,missing_test_ids=sorted(set(ids)-set(result.actual_ids)),tests_run=result.testsRun,
        failures=[t.id() for t,_ in result.failures],errors=[t.id() for t,_ in result.errors],
        execution_paths=dict(go=str(legacy.GO),cache=str(legacy.CACHE),modulecache=str(legacy.MODULECACHE)), skips=[t.id() for t,_ in result.skipped],expected_failures=[t.id() for t,_ in result.expectedFailures],unexpected_successes=[t.id() for t in result.unexpectedSuccesses])
    Path(output).write_text(json.dumps(report,indent=2)+'\n')
    return 0 if result.wasSuccessful() and not result.skipped and not result.expectedFailures and result.testsRun == len(ids) else 1


def run(root,output):
    root = Path(root).resolve(); output = Path(output).resolve(); output.parent.mkdir(parents=True,exist_ok=True)
    sha = git(root,'rev-parse','HEAD'); groups=[]
    with tempfile.TemporaryDirectory(prefix='r5-version-') as temp:
        current = Path(temp)/'current'
        checkout(root,current,sha)
        # The clone is the discovery authority; a dirty invocation root (npm ci)
        # neither deletes third-party files nor grants them Go authority.
        ids = discover(current)
        current_ids,frozen_ids=partition(ids)
        specs=[('current',None,current_ids),('frozen-v1',BASELINE,frozen_ids)]
        for name,pin,selection in specs:
            source=current if pin is None else Path(temp)/name
            if pin is not None:checkout(root,source,pin)
            request = Path(temp)/(name+'-ids.json'); reportfile = Path(temp)/(name+'-report.json')
            request.write_text(json.dumps(selection))
            # Dispatch through the NEW runner for both groups, but load tests
            # from the exact clone. The frozen helper itself is never replaced.
            argv = [sys.executable,'-B',str(Path(__file__).resolve()),'--child-root',str(source),
                    '--ids',str(request),'--output',str(reportfile)]
            result = subprocess.run(argv,cwd=source,env={**os.environ,'R5_TEST_GO':os.environ.get('R5_TEST_GO','go')})
            report = json.loads(reportfile.read_text()) if reportfile.exists() else dict(error='child report missing')
            groups.append(dict(group=name,source_sha=git(source,'rev-parse','HEAD'),requested_test_ids=selection,argv=argv,exit=result.returncode,report=report))
    good = all(g['exit']==0 and g['report'].get('actual_test_ids')==g['requested_test_ids'] and g['report'].get('source_sha')==g['source_sha'] for g in groups)
    executed = [i for g in groups for i in g['report'].get('actual_test_ids',[])]
    report = dict(schema_version=1,discovered_test_ids=ids,groups=groups,
        dispatch_no_missing=True,dispatch_no_overlap=True,
        no_missing=set(executed)==set(ids),no_overlap=len(executed)==len(set(executed)),
        missing_test_ids=sorted(set(ids)-set(executed)),status='pass' if good else 'failed')
    output.write_text(json.dumps(report,indent=2)+'\n')
    return 0 if good else 1


def main():
    p=argparse.ArgumentParser(description=__doc__);p.add_argument('--root',type=Path);p.add_argument('--output',type=Path,required=True)
    p.add_argument('--child-root',type=Path);p.add_argument('--ids',type=Path);a=p.parse_args()
    return child(a.child_root,json.loads(a.ids.read_text()),a.output) if a.child_root else run(a.root,a.output)

if __name__=='__main__':
    raise SystemExit(main())
