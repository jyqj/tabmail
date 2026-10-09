"""Independent real-contract controls, with synthetic execution only.

Run after the real probe, against its unchanged frozen checkout/contract.
Temporary mutations are intentional controls, restored before returning.
"""
import argparse
import copy
import hashlib
import json
import os
from pathlib import Path
import sys
from unittest.mock import patch

p = argparse.ArgumentParser()
p.add_argument('--source', type=Path, required=True)
p.add_argument('--contract', type=Path, required=True)
p.add_argument('--output', type=Path, required=True)
args = p.parse_args()
for key in list(os.environ):
    if key in ('NODE_PATH', 'NODE_OPTIONS', 'GOTOOLCHAIN') or key.lower().startswith('npm_config_'):
        os.environ.pop(key)
sys.path.insert(0, str(args.source / 'scripts/preparation'))
import r5_external_batch as b

checks = []
def check(name, condition):
    if not condition: raise AssertionError(name)
    checks.append(name)

raw = args.contract.read_bytes()
pin = hashlib.sha256(raw).hexdigest()
contract = b.load(args.contract, pin)
try: b.load(args.contract, '0'*64)
except ValueError: checks.append('independent byte pin rejects')
else: raise AssertionError('bad pin accepted')
for field, value in [('workers', 5), ('budgets', dict(go=121, process=180, case=75)), ('required', {})]:
    changed = copy.deepcopy(contract)
    changed[field] = value
    try: b.validate_contract(changed)
    except ValueError: checks.append('semantic recapture rejects '+field)
    else: raise AssertionError('semantic recapture accepted '+field)
b.validate_contract(contract)
checks.append('actual full contract validation passes')
owner = b.Batch(contract, args.output/'rpc', {})
request = dict(nonce=owner.nonce, contract_sha256=owner.pin, operation='case', key='probe/0', fixture='/private')
for label, change in [('outside lease', {}), ('foreign nonce', dict(nonce='foreign')), ('foreign contract', dict(contract_sha256='0'*64)), ('argv injection', dict(argv=['/bin/sh']))]:
    owner.leased = label != 'outside lease'
    try: owner.rpc(dict(request, **change))
    except ValueError: checks.append('RPC rejects '+label)
    else: raise AssertionError('RPC accepted '+label)

def mutated_group(label, target=None):
    old = target.read_bytes() if target else None
    owner = b.Batch(contract, args.output/label, {})
    def synthetic(argv, cwd, env, seconds=180):
        if argv[0] == contract['go']['path']:
            keys = list(owner.children)
            owner.seen = set(keys)
            owner.results = {key: dict(passed=True, timeout=False, tail=False, exit_code=0, report_sha256='0'*64) for key in keys}
            if target:
                owner.ack = sorted(keys)
            else:
                # Use the real private socket, cancel after successful child
                # results but before physical owner ack. Execution is synthetic.
                import socket
                for request in [dict(operation='cancel', key=keys[0]), dict(operation='ack', completed=keys)]:
                    with socket.socket(socket.AF_UNIX) as connection:
                        connection.connect(str(owner.socket))
                        connection.sendall(b.runtime.canonical(dict(request, nonce=owner.nonce, contract_sha256=owner.pin))+b'\n')
                        response = json.loads(connection.makefile('rb').readline())
                        check(label+' '+request['operation']+' socket accepted', response['ok'])
            names = [b.PROBE]+[b.PROBE+'/owners/'+key for key in keys]
            events = [dict(Package='tabmail/internal/api/handlers', Test=name, Action=action) for name in names for action in ['run', 'pass']]
            events.append(dict(Package='tabmail/internal/api/handlers', Action='pass'))
            if target: target.write_bytes(old+b'\nindependent review mutation\n')
            return dict(exit_code=0, timeout=False, tail=False, stdout='\n'.join(json.dumps(e) for e in events).encode(), stderr=b'')
        report = owner.output/'python-vitest.json'
        observation = dict(success=True, numTotalTests=1, numPassedTests=1, numFailedTests=0, numPendingTests=0,
            testResults=[dict(assertionResults=[dict(fullName='R5 external runtime real TSX CJS ESM worker jsdom', status='passed')])])
        report.write_text(json.dumps(observation))
        return dict(exit_code=0, timeout=False, tail=False, stdout=b'', stderr=b'')
    try:
        with patch.object(owner, 'run_process', side_effect=synthetic): receipt = owner.run()
        check(label+' actual preflight', receipt['preflight'])
        if target:
            check(label+' terminal full validation rejects', not receipt['terminal_postcheck'] and receipt['status']=='BATCH_REJECTED')
            check(label+' invalidates every staged child', len(receipt['children'])==8 and all(not c['qualified'] for c in receipt['children'].values()))
        else:
            check(label+' reproduces qualification despite accepted late cancel', receipt['status']=='BATCH_QUALIFIED' and all(c['qualified'] for c in receipt['children'].values()))
    finally:
        if target: target.write_bytes(old)
    b.validate_contract(contract)
    checks.append(label+' restored full validation passes')

mutated_group('source-mutation', args.source/'README.md')
mutated_group('dependency-mutation', Path(contract['runtime']['dependency_root']['path'])/'node_modules/react/LICENSE')
mutated_group('late-case-cancel-before-ack')
print(json.dumps(dict(status='PASS', controls=checks, execution='synthetic; no component case credit', fixed_source=contract['runtime']['git_commit']), indent=2))
