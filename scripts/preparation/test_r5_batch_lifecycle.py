"""Focused fixes for independent F1/F2/F3; no catalog/business execution."""
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

import r5_external_batch as b
import test_r5_external_batch as author


class LifecycleFixes(unittest.TestCase):
    def test_detached_inherited_pipes_are_joined_while_pending(self):
        # An isolated supervisor owns every subprocess it may discover. The
        # external watchdog owns only its explicitly recorded synthetic tail.
        with tempfile.TemporaryDirectory() as root:
            marker = Path(root)/'tail.pid'
            child = f"import os,time;open({str(marker)!r},'w').write(str(os.getpid()));time.sleep(30)"
            direct = f"import subprocess,sys;subprocess.Popen([sys.executable,'-c',{child!r}],start_new_session=True)"
            program = f"""
import sys,time,threading
sys.path.insert(0,{str(Path(b.__file__).parent)!r})
import r5_external_batch as b
b.become_subreaper()
p=b.OwnedProcess([sys.executable,'-c',{direct!r}],cwd={root!r},env={{}})
r=p.finish(threading.Event(),time.monotonic()+.2)
assert p.joined and r['tail']
assert b.join_adopted_tails()
assert not b.join_adopted_tails()
print('joined')
"""
            proc = subprocess.Popen([sys.executable,'-c',program],stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            try:
                out, err = proc.communicate(timeout=3)
                self.assertEqual(proc.returncode,0,err.decode())
                self.assertIn(b'joined',out)
                if marker.exists():
                    with self.assertRaises(ProcessLookupError):os.kill(int(marker.read_text()),0)
            finally:
                if proc.poll() is None:
                    if marker.exists():
                        try:os.kill(int(marker.read_text()),signal.SIGKILL)
                        except ProcessLookupError:pass
                    proc.kill()
                proc.communicate(timeout=3)

    def test_registered_concurrent_roots_are_not_adopted_tails(self):
        b.become_subreaper()
        with tempfile.TemporaryDirectory() as root:
            roots = [b.OwnedProcess([sys.executable,'-c','import time;time.sleep(.1);print("ok")'],cwd=root,env={}) for _ in range(4)]
            with b.concurrent.futures.ThreadPoolExecutor(max_workers=4) as workers:
                results = list(workers.map(lambda p:p.finish(threading.Event(),time.monotonic()+2),roots))
            self.assertTrue(all(r['exit_code']==0 and not r['tail'] for r in results))
            self.assertFalse(b.join_adopted_tails())

    def slow_inventory(self, phase, cancel=False):
        # Real descriptor inventory, slowed per file inside an owned worker.
        # Full validation's dispatch/termination path remains the production one.
        harness = author.BatchFinalizationControls();harness.setUp()
        self.addCleanup(harness.doCleanups)
        owner = b.Batch(harness.contract,harness.root/'out',{})
        owner.output.mkdir()
        owner.deadline = time.monotonic()+.25
        marker=harness.root/'worker.pid'
        inventory=harness.root/'inventory';inventory.mkdir()
        for i in range(100):(inventory/str(i)).write_text('controlled content')
        code=f"""
import sys,os,time
sys.path.insert(0,{str(Path(b.__file__).parent)!r})
import r5_external_batch as b
from pathlib import Path
Path({str(marker)!r}).write_text(str(os.getpid()))
original=b.runtime.Descriptors.read
def slow(self,*args):
    time.sleep(.02)
    return original(self,*args)
b.runtime.Descriptors.read=slow
with b.runtime.descriptors() as files: files.tree(Path({str(inventory)!r}),source=True)
"""
        constructor=b.OwnedProcess
        spawned=[]
        def worker(argv,**kwargs):
            self.assertEqual(argv[2],'validate')
            self.assertEqual(kwargs['env']['PYTHONDONTWRITEBYTECODE'],'1')
            child=constructor([sys.executable,'-c',code],**kwargs);spawned.append(child);return child
        if phase=='post':owner.cancelled.set() # ordinary execution shutdown is not user abort
        timer=threading.Timer(.15,owner.request_cancel) if cancel else None
        if timer:timer.start()
        started=time.monotonic()
        try:
            with patch.object(b,'OwnedProcess',side_effect=worker),self.assertRaisesRegex(ValueError,'inventory worker'):
                owner.validate_inventory()
        finally:
            if timer:timer.cancel();timer.join()
        self.assertLess(time.monotonic()-started,1.5)
        self.assertTrue(spawned[0].joined)
        with self.assertRaises(ProcessLookupError):os.kill(int(marker.read_text()),0)
        self.assertFalse(b.join_adopted_tails())

    def test_preflight_inventory_deadline(self):self.slow_inventory('pre')
    def test_postflight_inventory_deadline(self):self.slow_inventory('post')
    def test_preflight_inventory_cancel(self):self.slow_inventory('pre',True)
    def test_postflight_inventory_cancel(self):self.slow_inventory('post',True)

    def partial_socket(self, cancel):
        harness=author.BatchFinalizationControls();harness.setUp();self.addCleanup(harness.doCleanups)
        original=b.protocol.classify_events
        connections=[]
        def classify(*args,**kwargs):
            owner=harness.last_owner
            connection=socket.socket(socket.AF_UNIX);connection.connect(str(owner.socket));connection.sendall(b'{')
            connections.append(connection)
            if cancel:owner.request_cancel()
            else:owner.deadline=time.monotonic()+.02
            time.sleep(.1)
            return original(*args,**kwargs)
        started=time.monotonic()
        try:
            with patch.object(b.protocol,'classify_events',side_effect=classify):receipt=harness.run_synthetic_owner()
            self.assertEqual(receipt['status'],'BATCH_REJECTED')
            self.assertTrue(all(not c['qualified'] for c in receipt['children'].values()))
            self.assertLess(time.monotonic()-started,1.5)
            self.assertFalse(harness.last_owner.socket.exists())
        finally:
            for connection in connections:connection.close()

    def test_partial_rpc_deadline_join(self):self.partial_socket(False)
    def test_partial_rpc_cancel_join(self):self.partial_socket(True)

    def test_successful_child_cancel_before_ack_rejects_entire_receipt(self):
        harness=author.BatchFinalizationControls();harness.setUp();self.addCleanup(harness.doCleanups)
        original=b.protocol.classify_events
        replies=[]
        def classify(*args,**kwargs):
            owner=harness.last_owner
            owner.ack=None
            for request in [dict(operation='cancel',key='probe/0'),dict(operation='ack',completed=list(owner.children))]:
                with socket.socket(socket.AF_UNIX) as connection:
                    connection.connect(str(owner.socket))
                    connection.sendall(b.runtime.canonical(dict(request,nonce=owner.nonce,contract_sha256=owner.pin))+b'\n')
                    replies.append(json.loads(connection.makefile('rb').readline()))
            return original(*args,**kwargs)
        with patch.object(b.protocol,'classify_events',side_effect=classify):receipt=harness.run_synthetic_owner()
        self.assertTrue(all(r['ok'] for r in replies))
        self.assertTrue(receipt['owners_joined'])
        self.assertEqual(receipt['status'],'BATCH_REJECTED')
        self.assertIn('accepted case cancellation invalidates batch',receipt['errors'])
        self.assertTrue(all(not c['qualified'] for c in receipt['children'].values()))

    def test_closed_cancellation_explicitly_rejected(self):
        harness=author.BatchFinalizationControls();harness.setUp();self.addCleanup(harness.doCleanups)
        harness.run_synthetic_owner()
        owner=harness.last_owner
        with patch.object(owner,'leased',True),self.assertRaisesRegex(ValueError,'already finalized'):
            owner.rpc(dict(nonce=owner.nonce,contract_sha256=owner.pin,operation='cancel',key='probe/0'))
        self.assertFalse(owner.cancelled_keys)


if __name__=='__main__':unittest.main()
