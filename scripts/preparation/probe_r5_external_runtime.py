"""Real Python consumer probe; never credits product cases or complete loader scope."""
import argparse
import json
from pathlib import Path
import sys
import r5_external_runtime as runtime

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source',type=Path,required=True)
parser.add_argument('--report',type=Path,required=True)
args=parser.parse_args()
sys.path.insert(0,str(args.source/'scripts'))
import check_r5_protocol as consumer
manifest=runtime.from_environment(args.source)
command=consumer.external_component_command(manifest,args.report,probe=True)
process=runtime.launch(manifest,command)
report=json.loads(args.report.read_text())
assert process.returncode==0 and report.get('success') is True
assert report.get('numTotalTests')==report.get('numPassedTests')==1
assert report.get('numFailedTests')==report.get('numPendingTests')==0
print(json.dumps(dict(entry='check_r5_protocol.external_component_command',status='probe_pass',report_sha256=runtime.digest(args.report.read_bytes()),source_and_dependencies='full_no_follow_pre_post_equal',dynamic_loader_completeness='unknown')))
