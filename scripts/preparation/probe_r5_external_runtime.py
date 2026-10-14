"""Real Python consumer probe; never credits product cases or complete loader scope."""
import argparse
import json
from pathlib import Path
import sys
import r5_external_runtime as runtime

parser=argparse.ArgumentParser(description=__doc__)
parser.add_argument('--source',type=Path,required=True)
parser.add_argument('--report',type=Path,required=True)
parser.add_argument('--selected-binding-version',type=int,choices=(2,3),default=2)
args=parser.parse_args()
sys.path.insert(0,str(args.source/'scripts'))
import check_r5_protocol as consumer
manifest=runtime.from_environment(args.source,selected_binding_version=args.selected_binding_version)
command=consumer.external_component_command(manifest,args.report,probe=True)
process=runtime.launch(manifest,command,selected_binding_version=args.selected_binding_version)
report=json.loads(args.report.read_text())
assert process.returncode==0 and report.get('success') is True
assert report.get('numTotalTests')==report.get('numPassedTests')==1
assert report.get('numFailedTests')==report.get('numPendingTests')==0
print(json.dumps(dict(entry='check_r5_protocol.external_component_command',status='probe_pass',report_sha256=runtime.digest(args.report.read_bytes()),source_and_dependencies='full_no_follow_pre_post_equal',dynamic_loader_completeness='unknown')))
