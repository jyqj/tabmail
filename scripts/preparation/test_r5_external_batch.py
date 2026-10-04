"""Independent controls for the explicit batch contract; synthetic owned children."""
import copy
import json
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

import r5_external_batch as batch

class ContractControls(unittest.TestCase):
    def test_actual_catalog_derived_complete_scope(self):
        data=batch.protocol.load_cases()
        required=batch.derive(data)
        self.assertEqual(len(data['cases']),46)
        self.assertEqual(len(required['go_paths']),40)
        self.assertEqual(len(required['children']),26)
        self.assertEqual(len({v['case_id'] for v in required['children'].values()}),17)
        self.assertEqual(required['python_assertions'],['R5 shared receipt RC03','R5 shared receipt RC04','R5 shared receipt RC05'])
    def test_duplicate_catalog_paths_reject(self):
        data=batch.protocol.load_cases()
        adapter=next(a for row in data['cases'] for a in row.get('shared_adapters',[]) if a.get('component_source'))
        adapter['runtime_test_paths'].append(adapter['runtime_test_paths'][0])
        with self.assertRaisesRegex(ValueError,'duplicate'):batch.derive(data)
    def test_old_v2_or_rehashed_promotion_reject(self):
        with tempfile.TemporaryDirectory() as root:
            path=Path(root)/'contract'
            for policy,status in [(batch.runtime.POLICY,'UNADOPTED'),(batch.POLICY,'BATCH_QUALIFIED')]:
                raw=batch.runtime.canonical(dict(policy=policy,status=status));path.write_bytes(raw)
                with self.assertRaisesRegex(ValueError,'promotion'):batch.load(path,batch.runtime.digest(raw))
    def test_independent_pin_required(self):
        with tempfile.TemporaryDirectory() as root:
            path=Path(root)/'contract';path.write_text('{}')
            with self.assertRaisesRegex(ValueError,'pin'):batch.load(path,'0'*64)
    def test_false_pass_and_duplicate_assertions_rejected(self):
        report=dict(success=True,numTotalTests=1,numPassedTests=1,numFailedTests=0,numPendingTests=0,testResults=[dict(assertionResults=[dict(fullName='expected',status='passed')])])
        self.assertTrue(batch.exact_assertions(report,['expected'],0))
        for field,value in [('success',False),('numPendingTests',1),('numPassedTests',0),('numRuntimeErrorTestSuites',1)]:
            bad=copy.deepcopy(report);bad[field]=value
            self.assertFalse(batch.exact_assertions(bad,['expected'],0))
        self.assertFalse(batch.exact_assertions(report,['expected'],1))
        self.assertFalse(batch.exact_assertions(report,['missing'],0))
        report['testResults'][0]['assertionResults']*=2
        self.assertFalse(batch.exact_assertions(report,['expected'],0))

class RPCControls(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        root=Path(self.tmp.name)
        runtime=dict(source=dict(path=str(root/'source')),dependency_root=dict(path=str(root)),node=dict(path='/fixed/node'),cli=dict(path='/fixed/cli'),execution=dict(cwd=str(root/'source/web')))
        contract=dict(runtime=runtime,mode='probe',required={},catalog_sha256='0'*64)
        self.batch=batch.Batch(contract,root/'out',{})
    def rpc(self,operation,**kwargs):return self.batch.rpc(dict(nonce=self.batch.nonce,contract_sha256=self.batch.pin,operation=operation,**kwargs))
    def test_capability_required_and_no_argv_escape(self):
        with self.assertRaisesRegex(ValueError,'capability'):self.batch.rpc(dict(nonce='foreign',operation='ack',completed=[]))
        with self.assertRaisesRegex(ValueError,'operation'):self.rpc('case',key='probe/0',fixture='/private',argv=['/bin/sh'])
    def test_missing_duplicate_nonterminal_ack(self):
        for completed in [[],['probe/0']*8,list(self.batch.children)]:
            with self.assertRaisesRegex(ValueError,'acknowledgement'):self.rpc('ack',completed=completed)
    def test_exact_joined_ack_only_once(self):
        self.batch.seen=set(self.batch.children);self.batch.results={key:{} for key in self.batch.children}
        self.assertTrue(self.rpc('ack',completed=list(self.batch.children))['acknowledged'])
        with self.assertRaises(ValueError):self.rpc('ack',completed=list(self.batch.children))
    def test_active_child_prevents_owner_ack(self):
        self.batch.seen=set(self.batch.children);self.batch.results={key:{} for key in self.batch.children};self.batch.active={'probe/0':object()}
        with self.assertRaises(ValueError):self.rpc('ack',completed=list(self.batch.children))
    def test_duplicate_unknown_or_closed_case(self):
        self.batch.seen.add('probe/0')
        for key in ['probe/0','foreign-case']:
            with self.assertRaisesRegex(ValueError,'case'):self.rpc('case',key=key,fixture='/private')
        self.batch.closed=True
        with self.assertRaises(ValueError):self.rpc('case',key='probe/1',fixture='/private')
    def test_cancel_before_child_start_is_retained(self):
        self.rpc('cancel',key='probe/0')
        self.assertEqual(self.batch.cancelled_keys,{'probe/0'})

class PhysicalLifecycleControls(unittest.TestCase):
    @classmethod
    def setUpClass(cls):batch.become_subreaper()
    def run_child(self,program,cancel=None,seconds=2):
        with tempfile.TemporaryDirectory() as root:
            owned=batch.OwnedProcess([sys.executable,'-c',program],cwd=root,env={})
            result=owned.finish(cancel or threading.Event(),time.monotonic()+seconds)
            self.assertTrue(owned.joined)
            with self.assertRaises(ProcessLookupError):os.killpg(owned.group,0)
            return result
    def test_real_normal_child_physical_join(self):
        result=self.run_child('print("synthetic")')
        self.assertEqual(result['exit_code'],0);self.assertFalse(result['timeout']);self.assertFalse(result['tail'])
    def test_real_falsepass_exit_is_retained(self):
        result=self.run_child('raise SystemExit(1)')
        self.assertEqual(result['exit_code'],1)
    def test_real_deadline_child_and_descendant_join(self):
        program='import subprocess,sys,time;subprocess.Popen([sys.executable,"-c","import time;time.sleep(60)"]);time.sleep(60)'
        result=self.run_child(program,seconds=.15)
        self.assertTrue(result['timeout']);self.assertNotEqual(result['exit_code'],0)
    def test_real_cancellation_child_and_descendant_join(self):
        cancelled=threading.Event();timer=threading.Timer(.15,cancelled.set);timer.start()
        try:result=self.run_child('import subprocess,sys,time;subprocess.Popen([sys.executable,"-c","import time;time.sleep(60)"]);time.sleep(60)',cancel=cancelled)
        finally:timer.cancel();timer.join()
        self.assertTrue(result['timeout'])
    def test_real_closed_pipe_descendant_tail_rejects(self):
        result=self.run_child('import subprocess,sys;subprocess.Popen([sys.executable,"-c","import time;time.sleep(60)"],stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)')
        self.assertTrue(result['tail'])

if __name__=='__main__':unittest.main()

class BatchFinalizationControls(unittest.TestCase):
    def setUp(self):
        self.tmp=tempfile.TemporaryDirectory();self.addCleanup(self.tmp.cleanup)
        self.root=Path(self.tmp.name)
        self.source=self.root/'source';self.source.mkdir()
        (self.source/'bound').write_text('same content')
        self.manifest=dict(source=dict(path=str(self.source)),dependency_root=dict(path=str(self.root)),execution=dict(cwd=str(self.source)))
        self.contract=dict(runtime=self.manifest,mode='probe',required={},catalog_sha256='0'*64,go=dict(path=sys.executable),argv=dict(go_commands=[[sys.executable,'synthetic-control']],python=[sys.executable,'synthetic-python-probe']))
        with batch.runtime.descriptors() as files:self.before=files.tree(self.source,source=True)
        self.events=[]
    def validate(self,contract):
        self.events.append('validate')
        self.assertTrue((self.root/'.r5-runtime-owner').exists())
        with batch.runtime.descriptors() as files:
            if files.tree(self.source,source=True)!=self.before:raise ValueError('real source content drift')
    def run_synthetic_owner(self,mutate=False,missing=False,duplicate=False,noack=False,falsepass=False):
        owner=batch.Batch(self.contract,self.root/'out',{})
        case_keys=list(owner.children)
        test=self
        def synthetic(argv,cwd,env,seconds=180):
            if argv[-1]=='synthetic-python-probe':
                (owner.output/'python-vitest.json').write_text(json.dumps(dict(success=True,numTotalTests=1,numPassedTests=1,numFailedTests=0,numPendingTests=0,testResults=[dict(assertionResults=[dict(fullName='R5 external runtime real TSX CJS ESM worker jsdom',status='passed')])])))
                return dict(exit_code=0,timeout=False,tail=False,stdout=b'',stderr=b'')
            # This control supplies synthetic terminal events only. It never runs
            # or claims business cases; physical-process tests are independent.
            owner.max_active=4
            owner.seen=set(case_keys)
            owner.results={key:dict(passed=not falsepass,timeout=False,tail=False,exit_code=0,report_sha256='0'*64) for key in case_keys}
            if not noack:owner.ack=sorted(case_keys)
            if mutate:(test.source/'bound').write_text('mutated')
            names=[batch.PROBE]+[batch.PROBE+'/owners/'+key for key in case_keys]
            if missing:names.pop()
            if duplicate:names.append(names[-1])
            events=[dict(Package='tabmail/internal/api/handlers',Test=name,Action=action) for name in names for action in ['run','pass']]
            events.append(dict(Package='tabmail/internal/api/handlers',Action='pass'))
            return dict(exit_code=0,timeout=False,tail=False,stdout='\n'.join(json.dumps(e) for e in events).encode(),stderr=b'')
        with patch.object(batch,'validate_contract',side_effect=self.validate),patch.object(owner,'run_process',side_effect=synthetic):
            receipt=owner.run()
        self.assertEqual(self.events,['validate','validate'])
        self.assertFalse((self.root/'.r5-runtime-owner').exists())
        self.assertFalse(receipt['product_green'])
        return receipt
    def test_terminal_postmutation_rejects_all_staged_passes(self):
        receipt=self.run_synthetic_owner(mutate=True)
        self.assertEqual(receipt['status'],'BATCH_REJECTED')
        self.assertFalse(receipt['terminal_postcheck'])
        self.assertTrue(all(not r['qualified'] for r in receipt['children'].values()))
    def test_missing_duplicate_nonterminal_falsepass_whole_group_reject(self):
        for kwargs in [dict(missing=True),dict(duplicate=True),dict(noack=True),dict(falsepass=True)]:
            self.events=[]
            import shutil
            shutil.rmtree(self.root/'out',ignore_errors=True)
            receipt=self.run_synthetic_owner(**kwargs)
            self.assertEqual(receipt['status'],'BATCH_REJECTED')
            self.assertTrue(all(not r['qualified'] for r in receipt['children'].values()))
    def test_receipt_finalized_before_lock_release(self):
        receipt=self.run_synthetic_owner()
        self.assertEqual(receipt['status'],'BATCH_QUALIFIED')
        self.assertEqual(receipt['eligibility_scope'],'infrastructure_probe_only')
        self.assertTrue((self.root/'out/receipt.json').is_file())
    def test_exclusive_second_consumer_cannot_interleave(self):
        with batch.runtime.owner(self.manifest):
            second=batch.Batch(self.contract,self.root/'second',{})
            second.cancelled.set()
            with self.assertRaisesRegex(ValueError,'acquiring lease'):second.run()
        self.assertFalse((self.root/'.r5-runtime-owner').exists())


class LeaseSerializationControls(unittest.TestCase):
    def test_second_batch_waits_until_terminal_lease_release(self):
        with tempfile.TemporaryDirectory() as root:
            manifest=dict(dependency_root=dict(path=root))
            acquired=threading.Event();finished=threading.Event();cancelled=threading.Event()
            def second():
                with batch.lease(manifest,cancelled,time.monotonic()+2):
                    acquired.set()
                finished.set()
            with batch.runtime.owner(manifest):
                thread=threading.Thread(target=second);thread.start()
                self.assertFalse(acquired.wait(.1))
            self.assertTrue(acquired.wait(1));thread.join()
            self.assertTrue(finished.is_set())


class BoundedRealChildrenControls(unittest.TestCase):
    def test_fixed_queue_once_and_maximum_four_real_children(self):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root);source=root/'source';source.mkdir()
            cli=root/'synthetic_cli.py'
            cli.write_text("import sys,json,time;time.sleep(.2);report=sys.argv[sys.argv.index('--outputFile')+1];json.dump(dict(success=True,numTotalTests=1,numPassedTests=1,numFailedTests=0,numPendingTests=0,testResults=[dict(assertionResults=[dict(fullName='R5 batch independent realm loopback PostgreSQL',status='passed')])]),open(report,'w'))")
            manifest=dict(source=dict(path=str(source)),dependency_root=dict(path=str(root)),execution=dict(cwd=str(source)))
            contract=dict(runtime=manifest,mode='probe',required={},argv=dict(child=[sys.executable,str(cli),'--outputFile','<fresh-report>']))
            owner=batch.Batch(contract,root/'out',{});owner.output.mkdir();owner.deadline=time.monotonic()+10
            fixtures={}
            for key in owner.children:
                fixture=root/(key.replace('/','-')+'.json');fixture.write_text(json.dumps(dict(case_id=key)));fixtures[key]=str(fixture)
            batch.become_subreaper()
            with batch.concurrent.futures.ThreadPoolExecutor(max_workers=8) as workers:
                results=list(workers.map(lambda key:owner.rpc(dict(nonce=owner.nonce,contract_sha256=owner.pin,operation='case',key=key,fixture=fixtures[key])),owner.children))
            self.assertEqual(owner.max_active,4)
            self.assertEqual(owner.seen,set(owner.children));self.assertFalse(owner.active)
            self.assertTrue(all(r['passed'] and not r['tail'] and not r['timeout'] for r in results))
            ack=owner.rpc(dict(nonce=owner.nonce,contract_sha256=owner.pin,operation='ack',completed=list(owner.children)))
            self.assertTrue(ack['acknowledged'])


class DetachedTailControls(unittest.TestCase):
    def test_detached_owned_daemon_is_adopted_and_joined_before_postcheck(self):
        batch.become_subreaper()
        with tempfile.TemporaryDirectory() as root:
            marker=Path(root)/'tail.pid'
            child="import os,time;open("+repr(str(marker))+",'w').write(str(os.getpid()));time.sleep(60)"
            program="import subprocess,sys;subprocess.Popen([sys.executable,'-c',"+repr(child)+"],start_new_session=True,stdout=subprocess.DEVNULL,stderr=subprocess.DEVNULL)"
            owner=batch.OwnedProcess([sys.executable,'-c',program],cwd=root,env={})
            owner.finish(threading.Event(),time.monotonic()+2)
            self.assertTrue(owner.joined)
            self.assertTrue(batch.join_adopted_tails())
            self.assertFalse(batch.join_adopted_tails())

class ConsumerIdentityControls(unittest.TestCase):
    def test_both_builders_share_pinned_cli_and_cache_policy(self):
        source=Path(batch.protocol.ROOT)
        manifest=dict(source=dict(path=str(source)),node=dict(path='/fixed/node'),cli=dict(path='/fixed/cli'))
        with patch.object(batch,'file_digest',return_value='0'*64),patch.object(batch.subprocess,'check_output',return_value='go version go1.25.7 linux/amd64'):
            contract=batch.capture(manifest,Path('/fixed/go'),'components')
        prefix=['/fixed/node','/fixed/cli','run','--cache=false','--experimental.fsModuleCache=false']
        self.assertEqual(contract['argv']['child'][:5],prefix)
        self.assertEqual(contract['argv']['python'][:5],prefix)
        self.assertEqual(len(contract['required']['go_paths'])+len(contract['required']['python_assertions']),43)
        self.assertEqual(contract['budgets'],dict(go=120,process=180,case=75))
    def test_two_dependency_observers_do_not_share_mutable_root_identity(self):
        with tempfile.TemporaryDirectory() as root:
            root=Path(root)
            for name,version in [('a','1'),('b','2')]:
                folder=root/name/'node_modules/tool';folder.mkdir(parents=True)
                (folder/'package.json').write_text(json.dumps(dict(name='tool',version=version)))
            def observe(pair):
                name,version=pair
                tree={'tool/package.json':dict(type='file')}
                lock=dict(packages={'':{},'node_modules/tool':dict(version=version)})
                result=batch.runtime.dependency_records(tree,lock,dependency_root=root/name)
                self.assertEqual(result['locked']['node_modules/tool']['version'],version)
            with batch.concurrent.futures.ThreadPoolExecutor(max_workers=4) as workers:
                list(workers.map(observe,[('a','1'),('b','2')]*20))


class OutputBoundaryControls(unittest.TestCase):
    def test_source_dependency_relative_and_parent_outputs_rejected_before_write(self):
        runtime=dict(source=dict(path='/owned/source'),dependency_root=dict(path='/owned'),execution=dict(cwd='/owned/source/web'))
        contract=dict(runtime=runtime,mode='probe',required={})
        for path in ['relative','/owned/source/output','/owned/node_modules/output','/owned/../output']:
            with self.assertRaises(ValueError):batch.Batch(contract,Path(path),{})
