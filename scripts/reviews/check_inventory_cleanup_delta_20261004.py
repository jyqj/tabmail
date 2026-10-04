"""Real pinned-contract preflight error control; all child ownership is local.

The repeated finish fault is injected; no spontaneous operating-system I/O
failure or D-state behavior is claimed. Never publishes business qualification.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import sys
import time
from unittest.mock import patch

p=argparse.ArgumentParser()
p.add_argument('--source',type=Path,required=True)
p.add_argument('--contract',type=Path,required=True)
p.add_argument('--output',type=Path,required=True)
args=p.parse_args()
for key in list(os.environ):
    if key in ('NODE_PATH','NODE_OPTIONS','GOTOOLCHAIN') or key.lower().startswith('npm_config_'):
        os.environ.pop(key)
sys.path.insert(0,str(args.source/'scripts/preparation'))
import r5_external_batch as b

raw=args.contract.read_bytes()
contract=b.load(args.contract,hashlib.sha256(raw).hexdigest())
b.validate_contract(contract)
observations=[]
for phase in ['single-finish-error','repeated-finish-error']:
    owner=b.Batch(contract,args.output/phase,dict(os.environ,PYTHONDONTWRITEBYTECODE='1'))
    constructor=b.OwnedProcess
    finish=constructor.finish
    children=[]
    calls=[]
    def capture(argv,**kwargs):
        assert argv[1]==str(args.source/b.HELPER) and argv[2]=='validate'
        child=constructor(argv,**kwargs);children.append(child);return child
    def fail(child,*a,**kw):
        calls.append(True)
        if phase=='repeated-finish-error' or len(calls)==1:
            raise OSError('review-owned injected pipe-drain failure')
        return finish(child,*a,**kw)
    try:
        with patch.object(b,'OwnedProcess',side_effect=capture),patch.object(constructor,'finish',new=fail):
            try:owner.run()
            except OSError:pass
            else:raise AssertionError('fault unexpectedly returned a receipt')
        assert not owner.published and not owner.leased
        assert not (Path(contract['runtime']['dependency_root']['path'])/'.r5-runtime-owner').exists()
        assert not (owner.output/'receipt.json').exists()
        child=children[0]
        if phase=='single-finish-error':
            assert child.joined
            try:os.kill(child.process.pid,0)
            except ProcessLookupError:pass
            else:raise AssertionError('single-error worker remains')
            observations.append(dict(phase=phase,qualification=False,joined=True,lease_released=True))
        else:
            assert not child.joined
            # A waitable PID proves it was not reaped before lease release.
            pid,_=os.waitpid(child.process.pid,0)
            assert pid==child.process.pid
            observations.append(dict(phase=phase,qualification=False,joined_before_lease_release=False,review_had_to_reap=True,lease_released=True))
    finally:
        for child in children:
            child.kill();child.process.communicate(timeout=3)
        b.join_adopted_tails()
b.validate_contract(contract)
print(json.dumps(dict(fixed_source=contract['runtime']['git_commit'],observations=observations,source_restored_and_full_validation=True),indent=2))
