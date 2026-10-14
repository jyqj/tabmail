"""Synthetic safe metadata probes; these never impersonate producer runtime JSON."""
import copy
import hashlib
import json
import tempfile
import unittest
from pathlib import Path
from safejoin import validate

class NegativeProbes(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        # Minimal explicitly synthetic diagnostic fixture, no runtime output.
        objects = {
            'manifest-db-clean.json': dict(source_sha='s', source_identity_kind='i', source_closure_sha256='c', policy='p'),
            'http-result.json': dict(status='pass', spec_sha256='spec', capture_sha256='capture', responses=0),
            'db-result-metadata.json': dict(status='shared_scoped_evidence_failed', cases_sha256='cases', source_sha='s'),
            'http-status-packet.json': dict(producer='synthetic probe', spec_sha256='spec', raw_private_capture_sha256='capture', observations=[]),
            'db-status-packet.json': dict(producer='synthetic probe', source_sha='s', raw_private_log_sha256='log', events=[]),
        }
        refs=[]
        for name, value in objects.items():
            self.write(name,value)
            refs.append(dict(path=name, sha256=hashlib.sha256((self.root/name).read_bytes()).hexdigest(),kind='actual_safe_packet' if 'packet' in name else 'probe'))
        self.doc=dict(format_version='r5_current_wire_join_proposal_v2',adoption='operator_review_required',task_complete=False,product_green=False,input_refs=refs,packets=[r for r in refs if r['kind']=='actual_safe_packet'],source_sha='s',source_identity_kind='i',source_closure_sha256='c',source_policy='p',source_manifest_sha256=refs[0]['sha256'],current_spec_sha256='spec',cases_sha256='cases',producers=[dict(name=n,status=s,actualstarted=a) for n,s,a in [('http','PASS_HTTP_SHAPE',True),('shared-db','FAIL',True),('shared-components','NOTRUN_POLICY_BLOCKED',False)]])
        self.save()
    def write(self,name,value): (self.root/name).write_text(json.dumps(value))
    def save(self): self.write('join-proposal.json',self.doc)
    def rejected(self):
        self.save()
        with self.assertRaises(ValueError): validate(self.root)
    def test_synthetic_control(self): self.assertFalse(validate(self.root)['current_wire_complete'])
    def test_hash_drift(self): self.write('http-result.json',{});self.rejected()
    def test_promoted_status(self): self.doc['producers'][1]['status']='PASS';self.rejected()
    def test_missing_packet(self): self.doc['packets'].pop();self.rejected()
    def test_historical_identity(self): self.doc['frozen_tree']='old';self.rejected()
    def test_private_field_even_rehashed(self):
        p=self.root/'http-status-packet.json';x=json.loads(p.read_text());x['body']='synthetic secret probe';self.write(p.name,x)
        for ref in self.doc['input_refs']:
            if ref['path']==p.name:ref['sha256']=hashlib.sha256(p.read_bytes()).hexdigest()
        self.rejected()
if __name__=='__main__': unittest.main()
