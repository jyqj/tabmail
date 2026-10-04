import hashlib
import json
import os
from pathlib import Path
import runpy
import sys
import time
import unittest

ROOT = Path('/workspace/tabmail-integration')
OUT = Path('/tmp/R5-CATALOG-V3-INTEGRATION-20261004')
mode = sys.argv[1]
os.chdir(ROOT)
# Configure before importing any project or test module.
os.environ.update(R5_TEST_GO='/workspace/tabmail-cloud/tools/go/bin/go',
                  R5_TEST_CACHE=str(OUT / 'cache'),
                  R5_TEST_MODULECACHE='/workspace/tabmail-cloud/gomod',
                  GOPROXY='off', PYTHONDONTWRITEBYTECODE='1')
os.environ.pop('R5_DIAGNOSTIC_ROOT', None)
sys.path[:0] = [str(ROOT / 'scripts'), str(ROOT / 'scripts/tests')]
events = []
blocked = []
def guard(event, args):
    if event not in ('subprocess.Popen', 'os.system', 'os.posix_spawn', 'os.posix_spawnp', 'os.fork', 'os.forkpty'):
        return
    argv = args[1] if event == 'subprocess.Popen' else []
    allowed = False
    if mode != 'pure' and event == 'subprocess.Popen' and isinstance(argv, (list, tuple)):
        if argv[0] == os.environ['R5_TEST_GO']:
            allowed = (list(argv[1:]) == ['test', '-mod=readonly', '-count=1', './internal/architecture', '-run', '^TestR5RouteInventory$'] or
                       list(argv[1:4]) == ['run', './cmd/r5txinventory', str(ROOT)] or
                       (len(argv) == 4 and list(argv[1:3]) == ['run', './cmd/r5txinventory'] and str(argv[3]).startswith('/tmp/')))
        elif list(argv) == ['node', 'scripts/collect_api_calls.cjs']:
            allowed = True
        elif argv[0] == 'git' and len(argv) > 1 and argv[1] in ('diff', 'show', 'log', 'merge-base'):
            allowed = mode == 'review'
    row = {'event': event, 'argv': list(argv) if isinstance(argv, (list, tuple)) else str(argv), 'allowed': allowed}
    (events if allowed else blocked).append(row)
    if not allowed:
        raise AssertionError('OS subprocess blocked: ' + str(row))
sys.addaudithook(guard)
class Observed(unittest.TextTestResult):
    def __init__(self, *args, **kwargs):
        super().__init__(*args, **kwargs)
        self.ids = []
    def startTest(self, test):
        self.ids.append(test.id())
        super().startTest(test)
started = time.monotonic()
report = {'mode': mode, 'source_sha': '58a50ee7ff8cde0b434f8492426d2263cedf280d'}
if mode in ('catalog', 'pure'):
    names = (['test_r5_transactions', 'test_r5_compatibility', 'test_r5_catalog_reconciliation'] if mode == 'catalog' else
             ['r5_selected_binding_v3_producer_path_checks', 'r5_selected_source_binding_v3_checks',
              'r5_selected_binding_v3_rejection_checks', 'test_r5_selected_source_binding_v2.SelectedV2NegativeTests',
              'test_r5_archive_boundary', 'test_r5_env_diagnostics', 'test_r5_retention_failures'])
    result = unittest.TextTestRunner(verbosity=2, resultclass=Observed).run(unittest.defaultTestLoader.loadTestsFromNames(names))
    report.update(tests_run=result.testsRun, ids=result.ids, failures=[t.id() for t, _ in result.failures],
                  errors=[t.id() for t, _ in result.errors], skips=[t.id() for t, _ in result.skipped],
                  expected_failures=[t.id() for t, _ in result.expectedFailures], unexpected_successes=[t.id() for t in result.unexpectedSuccesses])
    okay = result.wasSuccessful() and not result.skipped and not result.expectedFailures and not blocked
elif mode == 'cli':
    reports = []
    for script in ('check_r5_transactions.py', 'check_r5_compatibility.py'):
        sys.argv = [str(ROOT / 'scripts' / script)]
        try:
            runpy.run_path(sys.argv[0], run_name='__main__')
        except SystemExit as error:
            if error.code not in (None, 0):
                raise
        reports.append({'script': script, 'exit_code': 0})
    report['commands'] = reports
    okay = not blocked
elif mode == 'review':
    path = ROOT / 'docs/company-mail/evidence/R5-CATALOG-INDEPENDENT-CLOUD-20261004/review.py'
    raw = path.read_bytes()
    # Redirect only the output destination; historical evidence stays untouched.
    code = raw.decode().replace('OUT = Path(__file__).parent', 'OUT = Path(' + repr(str(OUT / 'fresh-review')) + ')')
    (OUT / 'fresh-review').mkdir(exist_ok=True)
    (OUT / 'review-output-redirect.py').write_text(code)
    exec(compile(code, str(path), 'exec'), {'__file__': str(path), '__name__': '__main__'})
    report['original_script_sha256'] = hashlib.sha256(raw).hexdigest()
    okay = not blocked
elif mode == 'schema':
    registry = json.loads((ROOT / 'scripts/contracts/r5-selected-binding-v3-producer.json').read_bytes())
    source_root = OUT / 'producer-source'
    source_root.mkdir(exist_ok=True)
    report['sources'] = []
    for source in registry['sources']:
        path = Path('/workspace/tabmail-cloud/tools/go') / source['path']
        raw = path.read_bytes()
        assert hashlib.sha256(raw).hexdigest() == source['sha256']
        (source_root / path.name).write_bytes(raw)
        report['sources'].append({'installed_path': str(path), 'official_source_url': source['url'], 'sha256': source['sha256'], 'bytes': len(raw)})
    path = ROOT / 'docs/company-mail/evidence/R5-SELECTED-BINDING-V3-20261004/verify_producer_schema.py'
    sys.argv = [str(path), str(source_root)]
    runpy.run_path(str(path), run_name='__main__')
    report['original_script_sha256'] = hashlib.sha256(path.read_bytes()).hexdigest()
    okay = not blocked
else:
    raise ValueError(mode)
report.update(elapsed_seconds=time.monotonic() - started, allowed_subprocesses=events, blocked_subprocesses=blocked, passed=okay)
(OUT / (mode + '-summary.json')).write_text(json.dumps(report, indent=2) + '\n')
sys.exit(0 if okay else 1)
