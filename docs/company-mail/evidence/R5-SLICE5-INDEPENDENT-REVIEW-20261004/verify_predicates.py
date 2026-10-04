"""Guarded selected membership/coverage/version predicates; all rows synthetic."""
import copy, io, json, sys, tempfile, unittest
from pathlib import Path
from unittest import mock
ROOT=Path('/workspace/scratch/slice5-independent-review-20261004/repo');OUT=Path(__file__).resolve().parent
events=[]
def guard(event,args):
    if event in {'subprocess.Popen','os.system','os.fork','os.forkpty','os.posix_spawn','os.posix_spawnp'} or event.startswith(('os.exec','os.spawn')):
        events.append(event);raise RuntimeError('OS process forbidden: '+event)
sys.addaudithook(guard)
sys.path[:0]=[str(ROOT/'scripts'),str(ROOT/'scripts/preparation'),str(ROOT/'scripts/tests')]
import r5_selected_source_binding_v3 as v3
import r5_selected_binding_consumer as consumer
import r5_external_batch as batch
import r5_archive_boundary as boundary
import test_r5_archive_boundary as fixtures
rows=[]
class Predicates(unittest.TestCase):
    def reject(self,label,fn):
        with self.assertRaises(ValueError) as caught:fn()
        rows.append(dict(label=label,error=str(caught.exception)))
    def test_exact_v3_source_membership(self):
        original={p:'a'*64 for p in v3._V3_FILES}
        consumer._shape(original,v3._V3_FILES,'v3 source')
        for p in v3._V3_FILES:
            changed=dict(original);del changed[p]
            self.reject('v3 missing '+p,lambda:consumer._shape(changed,v3._V3_FILES,'v3 source'))
            changed[p+'.renamed']='a'*64
            self.reject('v3 rename '+p,lambda:consumer._shape(changed,v3._V3_FILES,'v3 source'))
        changed=dict(original);changed['scripts/new-control.py']='a'*64
        self.reject('v3 extra',lambda:consumer._shape(changed,v3._V3_FILES,'v3 source'))
    def test_selected_coverage_and_variants(self):
        root=Path('/tmp/slice5-synthetic-authority')
        row=dict(Dir=str(root/'cmd'),ImportPath='tabmail/cmd',Name='main',Module=dict(Path='tabmail',Main=True,Dir=str(root)),GoFiles=['main.go'],IgnoredGoFiles=['tagged.go'])
        static=dict(production_go={'cmd/main.go':'a'*64,'cmd/tagged.go':'b'*64},production_variant_directories=['cmd'])
        v3.compare_variants([row],root,static)
        v3.compare_coverage([row],[row],root,static,{'cmd/main.go':['GoFiles']})
        self.reject('root explicit omission',lambda:v3.compare_coverage([row],[],root,static,{}))
        self.reject('selected file outside static',lambda:v3.compare_coverage([row],[row],root,static,{'cmd/extra.go':['GoFiles']}))
        for label,changed in [('missing variant',dict(row,IgnoredGoFiles=[])),('extra variant',dict(row,GoFiles=['extra.go'])),('invalid variant',dict(row,InvalidGoFiles=['main.go'])),('unknown directory',dict(row,Dir=str(root/'unknown'))),('wrong module',dict(row,Module={'Path':'other'}))]:
            self.reject(label,lambda:v3.compare_variants([changed],root,static))
        fork=copy.deepcopy(row);fork.update(Dir=str(root/'third_party/go-smtp'),GoFiles=['main.go'],IgnoredGoFiles=[])
        fs=dict(production_go={'third_party/go-smtp/main.go':'a'*64},production_variant_directories=['third_party/go-smtp'])
        self.reject('variant fork identity',lambda:v3.compare_variants([fork],root,fs))
    def test_selector_and_batch_versions_before_boundary(self):
        for selector in (True,False,1,4,'3',3.0,None):
            self.reject('selector '+repr(selector),lambda:consumer.selected_version(selector))
        manifest=dict(schema_version=3,policy=batch.runtime.V3_POLICY,status='UNADOPTED',selected_binding_version=3)
        with mock.patch.object(batch.runtime,'load_pinned') as load:
            self.reject('runtime missing independent serialized pin',lambda:batch.runtime_gate(manifest,3))
            load.assert_not_called()
        for version in (True,2,4):
            changed=dict(manifest,schema_version=version)
            self.reject('runtime mixed version '+repr(version),lambda:batch.runtime_gate(changed,3))
        contract={p:None for p in batch.CONTRACT_FIELDS|{'selected_binding_version','runtime_manifest_path','runtime_manifest_sha256'}}
        contract.update(schema_version=1,policy=batch.V2_POLICY,status='UNADOPTED',selected_binding_version=3)
        self.reject('batch v1 schema mixed with selected3',lambda:batch.contract_gate(contract,3))
    def test_native_and_unclassified_build_inputs(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);fixtures.fixture(root)
            for name in ('newroot/input.c','internal/build/input.h','newroot/input.syso'):
                p=root/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(b'unknown')
                self.reject('unclassified build '+name,lambda:boundary.check(root));p.unlink()
            p=root/'cmd/native.go'
            for text in ('package main\nimport "C"\n','package main\n//go:linkname local remote\n'):
                p.write_text(text)
                self.reject('native directive '+repr(text),lambda:boundary.inventory._check_go_inputs(root,{'cmd/native.go'}))
            p.unlink()
    def test_byte_budget_actual_read(self):
        with tempfile.TemporaryDirectory() as temp:
            root=Path(temp);p=root/'input';p.write_bytes(b'ab')
            with mock.patch.object(boundary,'MAX_BYTES',1):
                self.reject('archive byte budget reduced synthetic boundary',lambda:boundary.read(root,'input'))
stream=io.StringIO();result=unittest.TextTestRunner(stream=stream,verbosity=2).run(unittest.defaultTestLoader.loadTestsFromTestCase(Predicates))
report=dict(tests_run=result.testsRun,failures=len(result.failures),errors=len(result.errors),skips=len(result.skipped),os_process_events=events,executed_negatives=rows,scope='actual predicates on synthetic data; no producer/capture/process')
(OUT/'predicate-results.json').write_text(json.dumps(report,indent=2)+'\n');(OUT/'predicate-checks.txt').write_text(stream.getvalue())
print(json.dumps({k:report[k] for k in ('tests_run','failures','errors','skips','os_process_events')}))
if not result.wasSuccessful() or events:sys.exit(1)
