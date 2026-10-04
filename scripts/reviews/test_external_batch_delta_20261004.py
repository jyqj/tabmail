"""Independent lifecycle delta controls; use --source for fixed 52dd8b9.

Short injected execution deadlines are controls, not changed runtime budgets.
All real children belong to these isolated synthetic test supervisors.
"""
import argparse
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

p = argparse.ArgumentParser()
p.add_argument('--source', type=Path, required=True)
args, rest = p.parse_known_args()
sys.path.insert(0, str(args.source/'scripts/preparation'))
import r5_external_batch as b
import test_r5_external_batch as author


class DeltaControls(unittest.TestCase):
    def test_detached_open_pipes_are_killed_reaped_and_recorded(self):
        with tempfile.TemporaryDirectory() as root:
            marker = Path(root)/'tail.pid'
            tail = f"import os,time;open({str(marker)!r},'w').write(str(os.getpid()));time.sleep(30)"
            direct = f"import subprocess,sys,time,os;subprocess.Popen([sys.executable,'-c',{tail!r}],start_new_session=True);time.sleep(.08)"
            code = f"""
import sys,time,threading,json,os
sys.path.insert(0,{str(args.source/'scripts/preparation')!r})
import r5_external_batch as b
b.become_subreaper()
p=b.OwnedProcess([sys.executable,'-c',{direct!r}],cwd={root!r},env={{}})
t=time.monotonic();r=p.finish(threading.Event(),t+.2)
assert p.joined and r['tail']
assert b.join_adopted_tails()
assert not b.join_adopted_tails()
assert not b._PROCESS_ROOTS
print(json.dumps(dict(elapsed=time.monotonic()-t,tail=r['tail'],joined=p.joined)))
"""
            proc = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                out, err = proc.communicate(timeout=3)
                self.assertEqual(proc.returncode, 0, err.decode())
                observation = json.loads(out)
                self.assertLess(observation['elapsed'], 1.5)
                if marker.exists():
                    with self.assertRaises(ProcessLookupError): os.kill(int(marker.read_text()), 0)
                print('detached_open_pipes='+json.dumps(observation), flush=True)
            finally:
                if marker.exists():
                    try: os.kill(int(marker.read_text()), signal.SIGKILL)
                    except ProcessLookupError: pass
                if proc.poll() is None: proc.kill()
                proc.communicate(timeout=3)

    def test_near_deadline_idle_rpc_is_interrupted_and_joined(self):
        harness = author.BatchFinalizationControls(); harness.setUp()
        original = b.protocol.classify_events
        connections = []
        before = set(threading.enumerate())
        def classify(*a, **kw):
            owner = harness.last_owner
            connection = socket.socket(socket.AF_UNIX)
            connection.connect(str(owner.socket)); connection.sendall(b'{')
            connections.append(connection)
            owner.deadline = time.monotonic()+.02
            time.sleep(.1)
            return original(*a, **kw)
        try:
            start = time.monotonic()
            with patch.object(b.protocol, 'classify_events', side_effect=classify): receipt = harness.run_synthetic_owner()
            elapsed = time.monotonic()-start
            self.assertLess(elapsed, 1.5)
            self.assertEqual(receipt['status'], 'BATCH_REJECTED')
            self.assertTrue(all(not child['qualified'] for child in receipt['children'].values()))
            self.assertFalse(harness.last_owner.socket.exists())
            self.assertEqual(set(threading.enumerate()), before)
            print('idle_rpc_join_seconds='+str(round(elapsed, 6)), flush=True)
        finally:
            for connection in connections: connection.close()
            harness.doCleanups()

    def test_late_cancel_before_ack_rejects_all_staged_results(self):
        harness = author.BatchFinalizationControls(); harness.setUp()
        original = b.protocol.classify_events
        responses = []
        def classify(*a, **kw):
            owner = harness.last_owner
            owner.ack = None
            for request in [dict(operation='cancel', key='probe/0'), dict(operation='ack', completed=list(owner.children))]:
                with socket.socket(socket.AF_UNIX) as connection:
                    connection.connect(str(owner.socket))
                    connection.sendall(b.runtime.canonical(dict(request, nonce=owner.nonce, contract_sha256=owner.pin))+b'\n')
                    with connection.makefile('rb') as stream: responses.append(json.loads(stream.readline()))
            return original(*a, **kw)
        try:
            with patch.object(b.protocol, 'classify_events', side_effect=classify): receipt = harness.run_synthetic_owner()
            self.assertTrue(all(response['ok'] for response in responses))
            self.assertTrue(receipt['owners_joined'])
            self.assertEqual(receipt['status'], 'BATCH_REJECTED')
            self.assertIn('accepted case cancellation invalidates batch', receipt['errors'])
            self.assertTrue(all(not child['qualified'] for child in receipt['children'].values()))
        finally: harness.doCleanups()

    def test_full_inventory_worker_deadline_and_cancellation(self):
        for phase, cancel in [('pre',False),('post',False),('pre',True),('post',True)]:
            with self.subTest(phase=phase, cancel=cancel), tempfile.TemporaryDirectory() as root:
                root = Path(root); source = root/'source'; source.mkdir(); inventory = root/'inventory'; inventory.mkdir()
                for i in range(30): (inventory/str(i)).write_text('owned full bytes')
                owner = b.Batch(dict(runtime=dict(source=dict(path=str(source)),dependency_root=dict(path=str(root)),execution=dict(cwd=str(source))),mode='probe',required={}),root/'out',{})
                owner.output.mkdir(); owner.deadline = time.monotonic()+.25
                if phase == 'post': owner.cancelled.set()
                code = f"""
import sys,time
from pathlib import Path
sys.path.insert(0,{str(args.source/'scripts/preparation')!r})
import r5_external_batch as b
original=b.runtime.Descriptors.read
def slow(self,*a):
    time.sleep(.05)
    return original(self,*a)
b.runtime.Descriptors.read=slow
with b.runtime.descriptors() as d: d.tree(Path({str(inventory)!r}),source=True)
"""
                spawned=[]; constructor=b.OwnedProcess
                def spawn(argv, **kw):
                    self.assertEqual(argv[2], 'validate')
                    child=constructor([sys.executable,'-c',code],**kw);spawned.append(child);return child
                timer=threading.Timer(.15,owner.request_cancel) if cancel else None
                if timer: timer.start()
                start=time.monotonic()
                try:
                    with patch.object(b,'OwnedProcess',side_effect=spawn),self.assertRaises(ValueError): owner.validate_inventory()
                    self.assertLess(time.monotonic()-start,1.5)
                    self.assertTrue(spawned[0].joined)
                    with self.assertRaises(ProcessLookupError):os.kill(spawned[0].process.pid,0)
                finally:
                    if timer: timer.cancel();timer.join()
                    for child in spawned:
                        child.kill()
                        child.process.communicate(timeout=3)
                    b.join_adopted_tails()

    def test_preflight_persistent_finish_error_releases_lease_without_join(self):
        # Observation control: qualification fails closed, but the new preflight
        # process lacks an outer descendant barrier when finish itself fails twice.
        harness=author.BatchFinalizationControls();harness.setUp()
        owner=b.Batch(harness.contract,harness.root/'fault',{})
        created=[];constructor=b.OwnedProcess
        def spawn(argv, **kw):
            child=constructor([sys.executable,'-c','import time;time.sleep(30)'],**kw)
            created.append(child);return child
        try:
            with patch.object(b,'OwnedProcess',side_effect=spawn),patch.object(constructor,'finish',side_effect=OSError('synthetic repeated pipe I/O failure')):
                with self.assertRaises(OSError):owner.run()
            self.assertFalse(owner.published)
            self.assertFalse(owner.leased)
            self.assertFalse((harness.root/'.r5-runtime-owner').exists())
            self.assertFalse(created[0].joined)
            # Reap ourselves: a waitable result demonstrates no earlier physical reap.
            pid,_=os.waitpid(created[0].process.pid,0)
            self.assertEqual(pid,created[0].process.pid)
            print('preflight_finish_error=unqualified; lease released before reap',flush=True)
        finally:
            for child in created:
                child.kill();child.process.communicate(timeout=3)
            b.join_adopted_tails();harness.doCleanups()


if __name__=='__main__':unittest.main(argv=[sys.argv[0],*rest])
