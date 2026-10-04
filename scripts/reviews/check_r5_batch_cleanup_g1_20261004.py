"""G1 fixed-source actual-helper controls; injected faults, no business execution."""
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
for phase in ('single-finish-error','persistent-finish-error','persistent-finish-and-wait-error'):
    owner=b.Batch(contract,a.output/phase,dict(os.environ,PYTHONDONTWRITEBYTECODE='1'))
    token=Path(contract['runtime']['dependency_root']['path'])/'.r5-runtime-owner'
    constructor=b.OwnedProcess;finish=constructor.finish;waitpid=os.waitpid
    roots=[];drains=[];primary=OSError('original G1 controlled drain fault')
    allow=threading.Event();blocked=threading.Event();done=threading.Event();errors=[]
    if phase!='persistent-finish-and-wait-error':allow.set()
    def spawn(argv,**kwargs):
        assert argv[1]==str(a.source/b.HELPER) and argv[2]=='validate'
        child=constructor(argv,**kwargs);roots.append(child);return child
    def drain(child,*args):
        drains.append(True)
        if phase!='single-finish-error' or len(drains)==1:raise primary
        return finish(child,*args)
    def wait(pid,options):
        if roots and pid==roots[0].process.pid:
            assert token.exists(), 'wait must precede lease release'
            if not allow.is_set():
                blocked.set();raise OSError('controlled independent wait failure')
        return waitpid(pid,options)
    def run():
        try:owner.run()
        except BaseException as error:errors.append(error)
        finally:done.set()
    with patch.object(b,'OwnedProcess',side_effect=spawn),patch.object(constructor,'finish',new=drain),patch.object(b.os,'waitpid',side_effect=wait):
        if allow.is_set():run()
        else:
            thread=threading.Thread(target=run);thread.start()
            try:
                assert blocked.wait(3)
                deadline=time.monotonic()+3
                while owner.cleanup_facts['state']!='BLOCKED' and time.monotonic()<deadline:time.sleep(.01)
                assert owner.cleanup_facts['state']=='BLOCKED'
                owner.request_cancel();owner.deadline=time.monotonic()-1
                assert not done.wait(.15) and token.exists() and owner.lease_held
                assert not owner.resources_joined and not owner.published
                assert not (owner.output/'receipt.json').exists()
                observations.append(dict(phase=phase,blocking_state_observed=True,unknown_cleanup_retains_lease=True,qualification=False))
            finally:allow.set();thread.join(5)
            assert not thread.is_alive()
    assert errors==[primary] and len(drains)==1
    assert owner.primary_error is primary and owner.resources_joined
    assert not token.exists() and not owner.leased and not owner.lease_held and not owner.published
    assert not (owner.output/'receipt.json').exists()
    child=roots[0]
    assert child.joined and child.direct_reaped and child.pipes_closed
    try:os.waitpid(child.process.pid,os.WNOHANG)
    except ChildProcessError:pass
    else:raise AssertionError('worker was not physically reaped')
    assert not b._PROCESS_ROOTS and not b._OWNED_PROCESSES
    facts=json.loads((owner.output/'cleanup.json').read_bytes())
    assert facts['state']=='JOINED' and facts['primary_error']=='OSError' and not facts['lease_held']
    assert all(all(row.values()) for row in facts['processes'].values())
    observations.append(dict(phase=phase,original_error_preserved=True,drain_calls=len(drains),physical_join_before_lease_release=True,independent_wait=True,pipes_closed=True,qualification=False))
b.validate_contract(contract)
print(json.dumps(dict(fixed_source=contract['runtime']['git_commit'],status='PASS',observations=observations,full_initial_and_restoration_validation=True,execution='actual pinned inventory-helper argv; injected errors; no business execution'),indent=2))
