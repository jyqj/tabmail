"""Independent real-contract G1 controls at f875672; injected OS faults only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import threading
import time
from unittest.mock import patch

p=argparse.ArgumentParser()
p.add_argument('--source',type=Path,required=True)
p.add_argument('--contract',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
a=p.parse_args()
for key in list(os.environ):
    if key in ('NODE_PATH','NODE_OPTIONS','GOTOOLCHAIN') or key.lower().startswith('npm_config_'):os.environ.pop(key)
sys.path.insert(0,str(a.source/'scripts/preparation'))
import r5_external_batch as b
raw=a.contract.read_bytes();contract=b.load(a.contract,hashlib.sha256(raw).hexdigest())
b.validate_contract(contract)
observations=[]
for phase in ['persistent-drain','cleanup-blocked','server-start','source-preflight']:
    owner=b.Batch(contract,a.output/phase,dict(os.environ,PYTHONDONTWRITEBYTECODE='1'))
    token=Path(contract['runtime']['dependency_root']['path'])/'.r5-runtime-owner'
    ctor=b.OwnedProcess;real_cleanup=ctor.cleanup_physical;real_wait=os.waitpid
    roots=[];drains=[];waits=[];primary=OSError('review-owned original persistent drain fault')
    allow=threading.Event();blocked=threading.Event();done=threading.Event();errors=[]
    if phase!='cleanup-blocked':allow.set()
    def spawn(argv,**kw):
        assert argv[1]==str(a.source/b.HELPER) and argv[2]=='validate'
        child=ctor(argv,**kw);roots.append(child);return child
    def drain(child,*args):drains.append(True);raise primary
    def cleanup(child):
        assert token.exists() and owner.lease_held
        if not allow.is_set():blocked.set();raise OSError('review-owned independent cleanup fault')
        real_cleanup(child)
        assert token.exists() and child.joined and child.direct_reaped and child.pipes_closed
    def wait(pid,options):
        if pid in [root.process.pid for root in roots]:
            assert token.exists(), 'physical wait must precede release'
            waits.append(pid)
        return real_wait(pid,options)
    def run():
        try:owner.run()
        except BaseException as err:errors.append(err)
        finally:done.set()
    from contextlib import ExitStack
    target=a.source/'README.md';old=target.read_bytes()
    try:
        with ExitStack() as stack:
            stack.enter_context(patch.object(b,'OwnedProcess',side_effect=spawn))
            stack.enter_context(patch.object(ctor,'cleanup_physical',new=cleanup))
            stack.enter_context(patch.object(b.os,'waitpid',side_effect=wait))
            if phase in ('persistent-drain','cleanup-blocked'):stack.enter_context(patch.object(ctor,'finish',new=drain))
            if phase=='server-start':stack.enter_context(patch.object(b.threading.Thread,'start',side_effect=primary))
            if phase=='source-preflight':target.write_bytes(old+b'\nreview-owned preflight source mutation\n')
            if allow.is_set():run()
            else:
                thread=threading.Thread(target=run);thread.start()
                try:
                    assert blocked.wait(3)
                    until=time.monotonic()+3
                    while owner.cleanup_facts['state']!='BLOCKED' and time.monotonic()<until:time.sleep(.01)
                    owner.request_cancel();owner.deadline=time.monotonic()-1
                    assert not done.wait(.15)
                    assert owner.cleanup_facts['state']=='BLOCKED' and token.exists() and owner.lease_held
                    assert roots[0].process.pid in b._OWNED_PROCESSES
                    assert not owner.resources_joined and not owner.published and not (owner.output/'receipt.json').exists()
                    observations.append(dict(phase=phase,blocked_lease_retained_through_cancel_and_expiry=True,no_receipt=True))
                finally:allow.set();thread.join(5)
                assert not thread.is_alive()
        assert len(errors)==1 and owner.primary_error is errors[0]
        if phase!='source-preflight':assert errors[0] is primary
        else:assert isinstance(errors[0],ValueError)
        if phase in ('persistent-drain','cleanup-blocked'):assert len(drains)==1 and waits
        assert owner.resources_joined and owner.channel_joined
        assert not token.exists() and not owner.lease_held and not owner.published
        assert not owner.socket.exists() and not (owner.output/'receipt.json').exists()
        for child in roots:
            assert child.joined and child.direct_reaped and child.pipes_closed
            assert child.process.stdout.closed and child.process.stderr.closed
            try:os.waitpid(child.process.pid,os.WNOHANG)
            except ChildProcessError:pass
            else:raise AssertionError('root not physically reaped')
        assert not b._PROCESS_ROOTS and not b._OWNED_PROCESSES
        facts=json.loads((owner.output/'cleanup.json').read_bytes())
        assert facts['state']=='JOINED' and not facts['lease_held']
        assert all(all(row.values()) for row in facts['processes'].values())
        observations.append(dict(phase=phase,physical_reap_and_pipe_close_before_release=True,original_error_preserved=True,no_receipt=True,independent_drain_calls=len(drains),cleanup_failures=facts['failures']))
    finally:
        if phase=='source-preflight':target.write_bytes(old)
        allow.set()
        for child in roots:
            child.kill();child.process.communicate(timeout=3)
        b.join_adopted_tails()
    b.validate_contract(contract)
print(json.dumps(dict(status='PASS',fixed_source=contract['runtime']['git_commit'],observations=observations,full_initial_and_restoration_validation=True),indent=2))
