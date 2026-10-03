"""One fixed-identity shared-components producer; private outputs, safe status only."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import sys
import r5_external_runtime as runtime

parser = argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source',type=Path,required=True)
parser.add_argument('--manifest',type=Path,required=True)
parser.add_argument('--manifest-sha256',required=True)
parser.add_argument('--output',type=Path,required=True)
args = parser.parse_args()
args.output.mkdir(exist_ok=False)
status = dict(policy=runtime.POLICY,status='UNADOPTED',task_complete=False,product_green=False,
              manifest_sha256=args.manifest_sha256,concurrency_boundary=runtime.BOUNDARY,
              dynamic_loader_coverage='unknown',final_missing_layers='all until producer verifies exact layers')
try:
    manifest = runtime.load_pinned(args.manifest,args.manifest_sha256)
    if manifest['source']['path'] != str(args.source):
        raise ValueError('producer source differs')
    runtime.validate(manifest)
    status['status'] = 'ready_for_scoped_run'
    env = dict(os.environ, TABMAIL_R5_EXTERNAL_MANIFEST=str(args.manifest),
               TABMAIL_R5_EXTERNAL_MANIFEST_SHA256=args.manifest_sha256,PYTHONDONTWRITEBYTECODE='1')
    archive = manifest['receipt_paths']['archive']
    command = [manifest['python']['path'],str(args.source/'scripts/check_r5_protocol.py'),
               '--run','shared-components','--source-policy',manifest['archive_policy'],
               '--source-manifest',archive,'--source-manifest-sha256',manifest['receipt_hashes']['archive'],
               '--output-dir',str(args.output/'producer')]
    try:
        # Every consumer obtains its own exclusive token, and this producer has a
        # separate lifetime token. Neither is an OS-enforced immutable lease.
        with runtime.owner(dict(manifest,dependency_root=dict(manifest['dependency_root'],path=str(args.output)))):
            process = subprocess.run(command,env=env,capture_output=True,text=True,timeout=600)
        status.update(status='run_failed',process_exit_code=process.returncode,
                      stdout_sha256=runtime.digest(process.stdout.encode()),stderr_sha256=runtime.digest(process.stderr.encode()))
        (args.output/'stdout.private').write_text(process.stdout)
        (args.output/'stderr.private').write_text(process.stderr)
        report_path = args.output/'producer/report.json'
        if report_path.exists():
            report = json.loads(report_path.read_text())
            status['final_missing_layers'] = report['missing_required_layers']
            status['protocol_status'] = report['status']
            status['product_green'] = False
            if process.returncode == 0 and report['status']=='shared_scoped_evidence_passed':
                status['status'] = 'scoped_pass'
            status['report_sha256'] = runtime.digest(report_path.read_bytes())
    finally:
        runtime.validate(manifest)
except (ValueError,OSError,subprocess.SubprocessError) as error:
    status.update(status='preparation_failed' if status['status']=='UNADOPTED' else 'run_failed',error=str(error))
(args.output/'status.json').write_text(json.dumps(status,indent=2)+'\n')
print(json.dumps(status,indent=2))
raise SystemExit(0 if status['status']=='scoped_pass' else 1)
