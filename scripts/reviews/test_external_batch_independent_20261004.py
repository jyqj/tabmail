"""Review-only counterexamples. Run with --source pointing at frozen f80b37f.

Assertions document observed defects; passing here means reproduction, never
qualification. All children/markers are owned synthetic temporary fixtures.
"""
import argparse
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from unittest.mock import patch

parser = argparse.ArgumentParser()
parser.add_argument('--source', type=Path, required=True)
args, rest = parser.parse_known_args()
sys.path.insert(0, str(args.source / 'scripts/preparation'))
import r5_external_batch as b
import test_r5_external_batch as author


class ReviewControls(unittest.TestCase):
    def test_detached_tail_with_inherited_pipes_outlives_finish_deadline(self):
        with tempfile.TemporaryDirectory() as root:
            marker = Path(root) / 'owned-tail.pid'
            detached = "import os,time;open(" + repr(str(marker)) + ",'w').write(str(os.getpid()));time.sleep(30)"
            direct = "import subprocess,sys;subprocess.Popen([sys.executable,'-c'," + repr(detached) + "],start_new_session=True)"
            supervisor = (
                "import sys,time,threading;sys.path.insert(0," + repr(str(args.source / 'scripts/preparation')) + ");"
                "import r5_external_batch as b;b.become_subreaper();"
                "p=b.OwnedProcess([sys.executable,'-c'," + repr(direct) + "],cwd=" + repr(root) + ",env={});"
                "r=p.finish(threading.Event(),time.monotonic()+.2);"
                "b.join_adopted_tails();print('returned')"
            )
            proc = subprocess.Popen([sys.executable, '-c', supervisor], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            tail = None
            try:
                for _ in range(100):
                    if marker.exists():
                        tail = int(marker.read_text())
                        break
                    time.sleep(.01)
                self.assertIsNotNone(tail)
                time.sleep(.6)
                self.assertIsNone(proc.poll(), 'finish unexpectedly obeyed its .2s deadline')
                # Identity recorded by our own child; never scan/kill host processes.
                os.kill(tail, signal.SIGKILL)
                out, err = proc.communicate(timeout=3)
                self.assertEqual(proc.returncode, 0, err.decode())
                self.assertIn(b'returned', out)
            finally:
                if tail is not None:
                    try: os.kill(tail, signal.SIGKILL)
                    except ProcessLookupError: pass
                if proc.poll() is None:
                    proc.kill()
                proc.communicate(timeout=3)

    def test_late_case_cancel_is_acknowledged_but_does_not_reject_staged_group(self):
        harness = author.BatchFinalizationControls()
        harness.setUp()
        original = harness.validate
        def validate(contract):
            original(contract)
            if len(harness.events) == 2:
                owner = harness.last_owner
                # All child results already exist; no active child to kill.
                reply = owner.rpc(dict(nonce=owner.nonce, contract_sha256=owner.pin,
                                       operation='cancel', key='probe/0'))
                self.assertTrue(reply['cancelled'])
        harness.validate = validate
        try:
            receipt = harness.run_synthetic_owner()
            self.assertEqual(receipt['status'], 'BATCH_QUALIFIED')
            self.assertIn('probe/0', harness.last_owner.cancelled_keys)
            self.assertTrue(receipt['children']['probe/0']['qualified'])
        finally:
            harness.doCleanups()

    def test_preflight_walk_is_not_interrupted_at_deadline(self):
        harness = author.BatchFinalizationControls()
        harness.setUp()
        original = harness.validate
        exceeded = []
        def validate(contract):
            if not harness.events:
                owner = harness.last_owner
                owner.deadline = time.monotonic() + .02
                time.sleep(.1)  # Deterministic slow-inventory fault; no hostile mutation.
                exceeded.append(time.monotonic() > owner.deadline)
            original(contract)
        harness.validate = validate
        try:
            receipt = harness.run_synthetic_owner()
            self.assertEqual(exceeded, [True])
            self.assertTrue(receipt['preflight'])
            self.assertEqual(receipt['status'], 'BATCH_REJECTED')
        finally:
            harness.doCleanups()

    def test_run_process_finish_exception_leaves_direct_child_unjoined(self):
        with tempfile.TemporaryDirectory() as root:
            source = Path(root)/'source'
            source.mkdir()
            manifest = dict(source=dict(path=str(source)), dependency_root=dict(path=root), execution=dict(cwd=str(source)))
            owner = b.Batch(dict(runtime=manifest, mode='probe', required={}), Path(root)/'out', {})
            owner.deadline = time.monotonic() + 2
            created = []
            constructor = b.OwnedProcess
            def capture(*a, **kw):
                p = constructor(*a, **kw)
                created.append(p)
                return p
            try:
                with patch.object(b, 'OwnedProcess', side_effect=capture), patch.object(constructor, 'finish', side_effect=OSError('synthetic pipe I/O error')):
                    with self.assertRaises(OSError):
                        owner.run_process([sys.executable, '-c', 'import time;time.sleep(30)'], Path(root), {})
                self.assertIsNone(created[0].process.poll())
                self.assertFalse(created[0].joined)
            finally:
                for p in created:
                    p.kill()
                    p.finish(threading.Event(), time.monotonic())

    def test_idle_rpc_join_waits_80_seconds_after_injected_deadline(self):
        harness = author.BatchFinalizationControls()
        harness.setUp()
        import socket
        original = b.protocol.classify_events
        connections = []
        started = []
        def classify(*a, **kw):
            owner = harness.last_owner
            connection = socket.socket(socket.AF_UNIX)
            connection.connect(str(owner.socket))
            connection.sendall(b'{')  # Owned incomplete line, no foreign processes.
            connections.append(connection)
            owner.deadline = time.monotonic() + .02
            time.sleep(.1)  # Ensure accept/read began before shutdown.
            started.append(time.monotonic())
            return original(*a, **kw)
        try:
            with patch.object(b.protocol, 'classify_events', side_effect=classify):
                receipt = harness.run_synthetic_owner()
            elapsed = time.monotonic() - started[0]
            self.assertGreater(elapsed, 78)
            self.assertEqual(receipt['status'], 'BATCH_REJECTED')
            print('idle_rpc_join_seconds=' + str(round(elapsed, 3)), flush=True)
        finally:
            for connection in connections:
                connection.close()
            harness.doCleanups()

    def test_outer_batch_reaps_direct_child_after_finish_exception(self):
        harness = author.BatchFinalizationControls()
        harness.setUp()
        owner = b.Batch(harness.contract, harness.root/'outer', {})
        owner.contract['argv']['go_commands'] = [[sys.executable, '-c', 'import time;time.sleep(30)']]
        created = []
        constructor = b.OwnedProcess
        def capture(*a, **kw):
            child = constructor(*a, **kw)
            created.append(child)
            return child
        try:
            with patch.object(b, 'validate_contract', side_effect=harness.validate), patch.object(b, 'OwnedProcess', side_effect=capture), patch.object(constructor, 'finish', side_effect=OSError('synthetic pipe I/O error')):
                receipt = owner.run()
            self.assertEqual(receipt['status'], 'BATCH_REJECTED')
            self.assertEqual(len(created), 1)
            with self.assertRaises(ProcessLookupError): os.kill(created[0].process.pid, 0)
            self.assertFalse(owner.leased)
            self.assertFalse((harness.root/'.r5-runtime-owner').exists())
        finally:
            for child in created:
                child.kill()
                child.process.communicate(timeout=3)
            harness.doCleanups()


if __name__ == '__main__':
    unittest.main(argv=[sys.argv[0], *rest])
