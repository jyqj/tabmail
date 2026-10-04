"""Independently hash private trusted direct-capture records and hold pins fixed.

These files are trusted local capture outputs, not attacker-supplied receipts.
Before pins were already separately stored and supplied to live postvalidation.
After pins are independently stored for subsequent validation of those envelopes.
"""
import copy
import hashlib
import json
from pathlib import Path
import sys
ROOT=Path(__file__).resolve().parents[4]
sys.path.insert(0,str(ROOT/'scripts'))
import r5_selected_source_binding_v3 as binding
private=Path(sys.argv[1]);facts=[];checks=[]
for context in ('default','race-r5protocol'):
    for stage in ('before','after'):
        receipt=json.loads((private/(context+'-'+stage+'.receipt.json')).read_text())
        # Compute complete receipt/envelope pin directly with stdlib, excluding
        # only the self-declared observation digest, not by reading that member.
        payload={k:v for k,v in receipt.items() if k!='observation_sha256'}
        pin=hashlib.sha256(json.dumps(payload,sort_keys=True,separators=(',',':')).encode()).hexdigest()
        assert pin==receipt['observation_sha256']
        if stage=='before':assert pin==(private/(context+'.trusted-pin')).read_text().strip()
        pin_file=private/(context+'-'+stage+'.independent-trusted-pin')
        pin_file.write_text(pin+'\n');pin_file.chmod(0o600)
        binding._verify_receipt(receipt,pin)
        facts.append(dict(context=context,stage=stage,trusted_observation_sha256=pin,pin_file=str(pin_file)))
        for field in ('raw_stdout_sha256','raw_stdout_bytes','stderr_sha256','stderr_bytes','argv','exit','role'):
            altered=copy.deepcopy(receipt);command=altered['observation_envelope'][2]
            command[field]=['altered'] if field=='argv' else 999 if field in ('raw_stdout_bytes','stderr_bytes','exit') else 'altered'
            altered=binding._seal(altered)
            try:binding._verify_receipt(altered,pin)
            except ValueError as error:
                assert str(error)=='trusted complete observation pin mismatch'
                checks.append(dict(context=context,stage=stage,field=field,result='resigned_envelope_rejected_with_external_pin_fixed'))
            else:raise AssertionError('fixed pin admitted resigned observation')
print(json.dumps(dict(trusted_channel='private direct local captures/validate returns; independent stdlib complete-envelope hashing',pins=facts,actual_receipt_resigned_rejections=checks,rejections=len(checks)),indent=2,sort_keys=True))
