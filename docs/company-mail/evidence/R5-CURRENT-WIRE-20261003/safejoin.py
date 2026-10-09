"""Read-only diagnostic for the proposed v2 join; no production validator changes."""
import hashlib
import json
from pathlib import Path


def validate(root):
    root = Path(root)
    doc = json.loads((root / 'join-proposal.json').read_text())
    if doc['format_version'] != 'r5_current_wire_join_proposal_v2' or doc['adoption'] != 'operator_review_required':
        raise ValueError('version/adoption differs')
    if doc['task_complete'] is not False or doc['product_green'] is not False:
        raise ValueError('proposal cannot claim completion or product green')
    if any(k in doc for k in ['frozen_tree', 'validation_commit', 'wire_summary']):
        raise ValueError('historical identity forbidden')
    refs = {r['path']: r for r in doc['input_refs']}
    if len(refs) != len(doc['input_refs']):
        raise ValueError('duplicate reference')
    for name, ref in refs.items():
        if Path(name).name != name or name in {'responses.json', 'join-proposal.json'}:
            raise ValueError('unsafe path')
        if hashlib.sha256((root / name).read_bytes()).hexdigest() != ref['sha256']:
            raise ValueError('actual byte hash differs')
    if doc['packets'] != [r for r in doc['input_refs'] if r['kind'] == 'actual_safe_packet']:
        raise ValueError('incomplete actual packet set')
    manifest = json.loads((root / 'manifest-db-clean.json').read_text())
    for key in ['source_sha', 'source_identity_kind', 'source_closure_sha256']:
        if doc[key] != manifest[key]:
            raise ValueError('current source differs')
    if doc['source_policy'] != manifest['policy'] or doc['source_manifest_sha256'] != refs['manifest-db-clean.json']['sha256']:
        raise ValueError('policy/manifest pin differs')
    http = json.loads((root / 'http-result.json').read_text())
    db = json.loads((root / 'db-result-metadata.json').read_text())
    if http['status'] != 'pass' or http['spec_sha256'] != doc['current_spec_sha256'] or db['cases_sha256'] != doc['cases_sha256'] or db['source_sha'] != doc['source_sha']:
        raise ValueError('formal result identity differs')
    expected = [('http', 'PASS_HTTP_SHAPE', True), ('shared-db', 'FAIL', True), ('shared-components', 'NOTRUN_POLICY_BLOCKED', False)]
    if [(p['name'], p['status'], p['actualstarted']) for p in doc['producers']] != expected or db['status'] != 'shared_scoped_evidence_failed':
        raise ValueError('terminal status promotion')
    packet = json.loads((root / 'http-status-packet.json').read_text())
    if set(packet) != {'producer', 'spec_sha256', 'raw_private_capture_sha256', 'observations'} or packet['raw_private_capture_sha256'] != http['capture_sha256'] or packet['spec_sha256'] != doc['current_spec_sha256']:
        raise ValueError('HTTP packet identity/private fields')
    for row in packet['observations']:
        if set(row) != {'case_id', 'method', 'route', 'status', 'response_sha256', 'aspect'} or row['aspect'] != 'http_shape_status' or type(row['status']) is not int:
            raise ValueError('unsafe HTTP metadata')
    if len(packet['observations']) != http['responses']:
        raise ValueError('missing HTTP observations')
    packet = json.loads((root / 'db-status-packet.json').read_text())
    if set(packet) != {'producer', 'source_sha', 'raw_private_log_sha256', 'events'} or packet['source_sha'] != doc['source_sha']:
        raise ValueError('unsafe DB packet')
    for row in packet['events']:
        if not set(row).issubset({'Time', 'Action', 'Package', 'Test', 'Elapsed'}) or row['Action'] == 'output':
            raise ValueError('unsafe DB metadata')
    return {'diagnostic': 'pins_and_safe_metadata_match', 'current_wire_complete': False, 'product_green': False, 'task_complete': False}

if __name__ == '__main__':
    print(json.dumps(validate(Path(__file__).parent), indent=2))
