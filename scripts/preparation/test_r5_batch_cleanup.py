"""G1: independent physical reaping inside the lease, including persistent faults."""
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time
import unittest
from unittest.mock import patch

import r5_external_batch as b
import test_r5_external_batch as author


class CleanupBarrierControls(unittest.TestCase):
    def harness(self):
        h=author.BatchFinalizationControls();h.setUp();self.addCleanup(h.doCleanups)
        return h

    def assert_released_after_join(self, h, owner, roots, original):
        self.assertIs(owner.primary_error,original)
        self.assertTrue(owner.resources_joined)
        self.assertFalse(owner.lease_held)
        self.assertFalse(owner.leased)
        self.assertFalse(owner.published)
        self.assertFalse((h.root/'.r5-runtime-owner').exists())
        self.assertFalse((owner.output/'receipt.json').exists())
        self.assertFalse(b._OWNED_PROCESSES)
        self.assertFalse(b._PROCESS_ROOTS)
        for child in roots:
            self.assertTrue(child.joined and child.direct_reaped and child.pipes_closed)
            with self.assertRaises(ChildProcessError):os.waitpid(child.process.pid,os.WNOHANG)
            with self.assertRaises(ProcessLookupError):os.kill(child.process.pid,0)
        facts=json.loads((owner.output/'cleanup.json').read_bytes())
        self.assertEqual(facts['state'],'JOINED')
        self.assertEqual(facts['primary_error'],'OSError')
        self.assertFalse(facts['lease_held'])
        self.assertTrue(all(all(p.values()) for p in facts['processes'].values()))

    def test_preflight_single_finish_error_has_independent_reap(self):
        self.preflight_fault(False)

    def test_preflight_persistent_finish_error_has_independent_reap(self):
        self.preflight_fault(True)

    def preflight_fault(self, persistent):
        h=self.harness();owner=b.Batch(h.contract,h.root/'fault',{})
        constructor=b.OwnedProcess;roots=[];original=OSError('original synthetic drain failure')
        def spawn(argv,**kwargs):
            child=constructor([sys.executable,'-c','import time;time.sleep(30)'],**kwargs)
            roots.append(child);return child
        real_cleanup=constructor.cleanup_physical
        def cleanup(child):
            self.assertTrue((h.root/'.r5-runtime-owner').exists())
            return real_cleanup(child)
        finish=constructor.finish
        calls=[]
        def fail(child,*args):
            calls.append(True)
            if persistent or len(calls)==1:raise original
            return finish(child,*args)
        with patch.object(b,'OwnedProcess',side_effect=spawn),patch.object(constructor,'finish',new=fail),patch.object(constructor,'cleanup_physical',new=cleanup):
            with self.assertRaises(OSError) as caught:owner.run()
        self.assertIs(caught.exception,original)
        self.assertEqual(len(calls),1) # cleanup does not retry the drain routine
        self.assert_released_after_join(h,owner,roots,original)

    def test_unknown_cleanup_blocks_return_and_retains_lease(self):
        h=self.harness();owner=b.Batch(h.contract,h.root/'blocked',{})
        constructor=b.OwnedProcess;roots=[];original=OSError('original persistent finish failure')
        def spawn(argv,**kwargs):
            child=constructor([sys.executable,'-c','import time;time.sleep(30)'],**kwargs)
            roots.append(child);return child
        allow_reap=threading.Event();blocked=threading.Event();finished=threading.Event();errors=[]
        real_waitpid=os.waitpid
        def waitpid(pid,options):
            if roots and pid==roots[0].process.pid and not allow_reap.is_set():
                blocked.set()
                raise OSError('injected independent wait failure')
            self.assertTrue((h.root/'.r5-runtime-owner').exists())
            return real_waitpid(pid,options)
        def run():
            try:owner.run()
            except BaseException as error:errors.append(error)
            finally:finished.set()
        with patch.object(b,'OwnedProcess',side_effect=spawn),patch.object(constructor,'finish',side_effect=original),patch.object(b.os,'waitpid',side_effect=waitpid):
            thread=threading.Thread(target=run);thread.start()
            try:
                self.assertTrue(blocked.wait(2))
                deadline=time.monotonic()+2
                while owner.cleanup_facts['state']!='BLOCKED' and time.monotonic()<deadline:time.sleep(.01)
                self.assertEqual(owner.cleanup_facts['state'],'BLOCKED')
                owner.request_cancel();owner.deadline=time.monotonic()-1
                self.assertFalse(finished.wait(.15))
                self.assertTrue((h.root/'.r5-runtime-owner').exists())
                self.assertTrue(owner.lease_held)
                self.assertFalse(owner.resources_joined)
                self.assertFalse((owner.output/'receipt.json').exists())
                self.assertIn(roots[0].process.pid,b._OWNED_PROCESSES)
                facts=json.loads((owner.output/'cleanup.json').read_bytes())
                self.assertEqual(facts['state'],'BLOCKED')
                self.assertEqual(facts['primary_error'],'OSError')
                self.assertTrue(facts['lease_held'])
            finally:
                allow_reap.set();thread.join(3)
            self.assertFalse(thread.is_alive())
        self.assertEqual(errors,[original])
        self.assert_released_after_join(h,owner,roots,original)
        self.assertIn(dict(resource='process_reap',error='OSError'),owner.cleanup_facts['failures'])

    def test_server_start_failure_closes_socket_inside_lease(self):
        h=self.harness();owner=b.Batch(h.contract,h.root/'setup',{})
        original=OSError('injected serving thread start failure')
        real_close=b.socketserver.ThreadingUnixStreamServer.server_close
        def close(server):
            self.assertTrue((h.root/'.r5-runtime-owner').exists())
            return real_close(server)
        with patch.object(owner,'validate_inventory',side_effect=lambda:h.validate(owner.contract)),patch.object(b.threading.Thread,'start',side_effect=original),patch.object(b.socketserver.ThreadingUnixStreamServer,'server_close',new=close):
            with self.assertRaises(OSError) as caught:owner.run()
        self.assertIs(caught.exception,original)
        self.assertFalse(owner.socket.exists())
        self.assert_released_after_join(h,owner,[],original)

    def test_setup_failure_after_process_registration_reaps_owned_root(self):
        h=self.harness();owner=b.Batch(h.contract,h.root/'setup-root',{})
        roots=[];original=OSError('setup failure after owned spawn')
        def validate():
            roots.append(b.OwnedProcess([sys.executable,'-c','import time;time.sleep(30)'],cwd=h.root,env={}))
            raise original
        with patch.object(owner,'validate_inventory',side_effect=validate):
            with self.assertRaises(OSError) as caught:owner.run()
        self.assertIs(caught.exception,original)
        self.assert_released_after_join(h,owner,roots,original)

    def test_execution_persistent_finish_error_is_reaped_before_receipt(self):
        h=self.harness();owner=b.Batch(h.contract,h.root/'execution',{})
        owner.contract['argv']['go_commands']=[[sys.executable,'-c','import time;time.sleep(30)']]
        roots=[];constructor=b.OwnedProcess
        def spawn(*args,**kwargs):
            child=constructor(*args,**kwargs);roots.append(child);return child
        with patch.object(owner,'validate_inventory',side_effect=lambda:h.validate(owner.contract)),patch.object(b,'OwnedProcess',side_effect=spawn),patch.object(constructor,'finish',side_effect=OSError('persistent execution drain failure')):
            receipt=owner.run()
        self.assertEqual(receipt['status'],'BATCH_REJECTED')
        self.assertTrue(receipt['resources_joined'])
        self.assertTrue(roots[0].joined and roots[0].direct_reaped and roots[0].pipes_closed)
        with self.assertRaises(ChildProcessError):os.waitpid(roots[0].process.pid,os.WNOHANG)
        self.assertFalse((h.root/'.r5-runtime-owner').exists())

    def test_postflight_persistent_finish_error_is_reaped_inside_lease(self):
        h=self.harness();original=h.validate;created=[];constructor=b.OwnedProcess
        def validate(contract):
            original(contract)
            if len(h.events)==2:
                child=constructor([sys.executable,'-c','import time;time.sleep(30)'],cwd=h.root,env={})
                created.append(child)
                child.finish(threading.Event(),time.monotonic()+1)
        h.validate=validate
        with patch.object(constructor,'finish',side_effect=OSError('persistent postflight drain failure')):
            receipt=h.run_synthetic_owner()
        self.assertEqual(receipt['status'],'BATCH_REJECTED')
        self.assertFalse(receipt['terminal_postcheck'])
        self.assertTrue(receipt['resources_joined'])
        self.assertTrue(created[0].joined and created[0].direct_reaped and created[0].pipes_closed)
        with self.assertRaises(ChildProcessError):os.waitpid(created[0].process.pid,os.WNOHANG)

    def test_normal_success_proves_resources_joined_before_publication(self):
        h=self.harness();original=b.Batch.cleanup_resources;observed=[]
        def cleanup(owner):
            observed.append((owner.published,(h.root/'.r5-runtime-owner').exists()))
            return original(owner)
        with patch.object(b.Batch,'cleanup_resources',new=cleanup):receipt=h.run_synthetic_owner()
        self.assertEqual(receipt['status'],'BATCH_QUALIFIED')
        self.assertTrue(receipt['resources_joined'])
        self.assertEqual(observed[0],(False,True))
        self.assertFalse(b._OWNED_PROCESSES)


if __name__=='__main__':unittest.main()
