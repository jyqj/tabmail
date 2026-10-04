"""PROPOSED ONLY: parent-reviewed, separately authorized minimum v3 batch probe.
Never import or execute as part of bounded slice5 verification.
Owned controller return values originate all pins; candidate files grant no authority.
"""
import argparse, hashlib, json, os, sys, uuid
from pathlib import Path
p=argparse.ArgumentParser(description=__doc__)
for key in ('root','source','go','node','cache','modulecache','evidence'):
    p.add_argument('--'+key,type=Path,required=True)
p.add_argument('--source-commit',required=True)
a=p.parse_args()
for value in (a.root,a.source,a.go,a.node,a.cache,a.modulecache,a.evidence):
    if not value.is_absolute() or str(value)!=os.path.abspath(value):raise ValueError('absolute canonical paths required')
if a.source!=a.root/'source':raise ValueError('ownedroot/source required')
if any(a.evidence.is_relative_to(tree) for tree in (a.root,a.cache,a.modulecache)):
    raise ValueError('new evidence must be outside owned root and caches')
a.evidence.mkdir(mode=0o700)  # Fresh private root only, never overwrite.
sys.path[:0]=[str(a.source/'scripts'),str(a.source/'scripts/preparation')]
import r5_source_inventory as inventory
import r5_selected_source_binding_v3 as selected
import r5_selected_binding_consumer as consumer
import r5_external_runtime as runtime
import r5_external_batch as batch

def publish(name,value):
    raw=inventory.canonical(value);pin=hashlib.sha256(raw).hexdigest();path=a.evidence/name
    with path.open('xb') as f:f.write(raw)
    if path.read_bytes()!=raw:raise ValueError('publication bytes differ')
    return path,pin
run_id=uuid.uuid4().hex
context=dict(selected.CONTEXT,build_tag_sets=[[],['r5protocol']])
archive=inventory.capture_current_source(a.source,purpose='protocol',policy=inventory.ARCHIVE_POLICY,build_context=context)
archive_path,archive_pin=publish('archive-v4.json',archive)
observations={}
for slot,context in (('default',selected.DEFAULT_CONTEXT),('race-r5protocol',selected.CONTEXT)):
    receipt=selected.capture(a.source,a.go,cache=a.cache,modulecache=a.modulecache,context=context)
    observation_pin=selected.observation_digest(receipt)
    selected._verify_receipt(receipt,observation_pin)
    path,pin=publish(slot+'.json',receipt)
    observations[slot]=dict(receipt_path=str(path),receipt_byte_sha256=pin,
        observation_sha256=observation_pin,context=receipt['base_source']['build_context'])
bundle=dict(schema_version=1,policy=consumer.PIN_POLICY,run_id=run_id,
    source_commit=a.source_commit,source_root=str(a.source),selected_binding_version=3,
    producer=dict(path=str(a.go),sha256=selected.GO_SHA256,version='go1.25.7'),observations=observations)
bundle_path,bundle_pin=publish('controller-selection-pins.json',bundle)
manifest=runtime.capture(a.source,a.root,archive_path,
    Path(observations['default']['receipt_path']),Path(observations['race-r5protocol']['receipt_path']),
    a.node,a.go,cache=a.cache,modulecache=a.modulecache,selected_binding_version=3,
    bundle_path=bundle_path,bundle_byte_sha256=bundle_pin,run_id=run_id,
    source_commit=a.source_commit,evidence_parent=a.evidence)
manifest_path,manifest_pin=publish('runtime-v3.json',manifest)
# Validate serialized publication against the independent controller-memory pin.
loaded=runtime.load_pinned(manifest_path,manifest_pin,selected_binding_version=3)
if loaded!=manifest:raise ValueError('published manifest differs from owned return')
runtime.validate(loaded,selected_binding_version=3)
contract=batch.capture(manifest,a.go,'probe',selected_binding_version=3,
    runtime_manifest_path=str(manifest_path),runtime_manifest_sha256=manifest_pin)
contract_path,contract_pin=publish('batch-v2-probe.json',contract)
loaded=batch.load(contract_path,contract_pin,selected_binding_version=3)
batch.validate_contract(loaded,selected_binding_version=3)
executor=batch.Batch(loaded,a.evidence/'probe-output',selected_binding_version=3)
# Preserve sole-executor CLI cancellation semantics for the proposed in-process controller.
import signal
for sig in (signal.SIGINT,signal.SIGTERM):
    signal.signal(sig,lambda signum,frame:executor.request_cancel())
receipt=executor.run();receipt_path,receipt_pin=publish('batch-result.json',receipt)
publish('controller-pins.json',dict(source_commit=a.source_commit,run_id=run_id,
    archive=archive_pin,bundle=bundle_pin,runtime=manifest_pin,batch=contract_pin,result=receipt_pin))
print(json.dumps(dict(status=receipt['status'],receipt=str(receipt_path),receipt_sha256=receipt_pin)))
raise SystemExit(0 if receipt['status']=='BATCH_QUALIFIED' else 1)
