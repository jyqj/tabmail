"""Replay only the preserved mocked preparation/runner unit controls; no discovery."""
import sys
EVENTS=[]
def guard(event,args):
    if event in ('subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp') or event.startswith(('os.exec','os.spawn')):
        EVENTS.append(event);raise AssertionError('OS process forbidden: '+event)
sys.addaudithook(guard)
import io,json,unittest
from pathlib import Path
root=Path(__file__).resolve().parents[4]
sys.path[:0]=[str(root/'scripts/tests'),str(root/'scripts')]
import test_r5_source_version_runner as controls
suite=unittest.TestSuite(unittest.defaultTestLoader.loadTestsFromTestCase(c) for c in (controls.SourceVersionRunnerTests,controls.PreparationBoundaryTests,controls.ExecutedBinaryBoundaryTests))
class Observed(unittest.TextTestResult):
    def __init__(self,*a,**kw):super().__init__(*a,**kw);self.ids=[]
    def startTest(self,test):self.ids.append(test.id());super().startTest(test)
result=unittest.TextTestRunner(stream=io.StringIO(),resultclass=Observed).run(suite)
assert result.wasSuccessful() and not result.skipped and not result.expectedFailures and not EVENTS
print(json.dumps({'scope':'preserved pure mocked unit boundaries only; no actual-root or formal suites','count':result.testsRun,'test_ids':result.ids,'failures':len(result.failures),'errors':len(result.errors),'os_process_events':EVENTS},indent=2))
