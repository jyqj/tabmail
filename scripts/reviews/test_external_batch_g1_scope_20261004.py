"""Independent execution/post error controls; synthetic terminal observations."""
import argparse
import os
from pathlib import Path
import sys
import threading
import time
import unittest
from unittest.mock import patch

p=argparse.ArgumentParser();p.add_argument('--source',type=Path,required=True)
a,rest=p.parse_known_args();sys.path.insert(0,str(a.source/'scripts/preparation'))
import r5_external_batch as b
import test_r5_external_batch as author

class ScopeControls(unittest.TestCase):
    def verify(self,owner,roots,primary,token):
        self.assertIs(owner.primary_error,primary)
        self.assertTrue(owner.resources_joined and owner.channel_joined)
        self.assertFalse(token.exists())
        self.assertFalse(b._OWNED_PROCESSES or b._PROCESS_ROOTS)
        for root in roots:
            self.assertTrue(root.direct_reaped and root.joined and root.pipes_closed)
            self.assertTrue(root.process.stdout.closed and root.process.stderr.closed)
            with self.assertRaises(ChildProcessError):os.waitpid(root.process.pid,os.WNOHANG)

    def test_execution_drain_error_independently_reaped_before_rejected_receipt(self):
        h=author.BatchFinalizationControls();h.setUp()
        owner=b.Batch(h.contract,h.root/'execution',{})
        owner.contract['argv']['go_commands']=[[sys.executable,'-c','import time;time.sleep(30)']]
        primary=OSError('review-owned execution drain failure');ctor=b.OwnedProcess;physical=ctor.cleanup_physical;roots=[]
        def capture(*args,**kw):
            child=ctor(*args,**kw);roots.append(child);return child
        def cleanup(child):
            self.assertTrue((h.root/'.r5-runtime-owner').exists());self.assertFalse(owner.published)
            return physical(child)
        try:
            with patch.object(owner,'validate_inventory',side_effect=lambda:h.validate(owner.contract)),patch.object(b,'OwnedProcess',side_effect=capture),patch.object(ctor,'finish',side_effect=primary),patch.object(ctor,'cleanup_physical',new=cleanup):receipt=owner.run()
            self.assertEqual(receipt['status'],'BATCH_REJECTED');self.assertTrue(receipt['resources_joined'])
            self.assertFalse(receipt['owners_joined'])
            self.verify(owner,roots,primary,h.root/'.r5-runtime-owner')
        finally:b.join_adopted_tails();h.doCleanups()

    def test_post_drain_error_independently_reaped_before_all_or_none_rejection(self):
        h=author.BatchFinalizationControls();h.setUp()
        original=h.validate;ctor=b.OwnedProcess;physical=ctor.cleanup_physical;roots=[];primary=OSError('review-owned postcheck drain failure')
        def validate(contract):
            original(contract)
            if len(h.events)==2:
                child=ctor([sys.executable,'-c','import time;time.sleep(30)'],cwd=h.root,env={});roots.append(child)
                child.finish(threading.Event(),time.monotonic()+1)
        h.validate=validate
        def cleanup(child):
            self.assertTrue((h.root/'.r5-runtime-owner').exists());self.assertFalse(h.last_owner.published)
            return physical(child)
        try:
            with patch.object(ctor,'finish',side_effect=primary),patch.object(ctor,'cleanup_physical',new=cleanup):receipt=h.run_synthetic_owner()
            self.assertEqual(receipt['status'],'BATCH_REJECTED');self.assertFalse(receipt['terminal_postcheck'])
            self.assertTrue(receipt['resources_joined'] and receipt['owners_joined'])
            self.assertTrue(all(not child['qualified'] for child in receipt['children'].values()))
            self.verify(h.last_owner,roots,primary,h.root/'.r5-runtime-owner')
        finally:b.join_adopted_tails();h.doCleanups()

if __name__=='__main__':unittest.main(argv=[sys.argv[0],*rest])
