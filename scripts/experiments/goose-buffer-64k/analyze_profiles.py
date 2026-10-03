#!/usr/bin/env python3
"""Analyze existing, hash-bound profiles only. Never launches a test or database.

Uses official installed Go pprof with a complete owned-cache environment. Raw
pprof totals are sampled/scaled estimates, not exact runtime.TotalAlloc.
"""
import argparse
from datetime import datetime
import hashlib
import json
from pathlib import Path
import re
import subprocess

p=argparse.ArgumentParser()
p.add_argument('--root',type=Path,required=True)
p.add_argument('--evidence',type=Path,required=True)
p.add_argument('--tool-root',type=Path,required=True)
a=p.parse_args();r=a.root.resolve();o=a.evidence.resolve();tool=a.tool_root.resolve()
sha=lambda b:hashlib.sha256(b).hexdigest()
env={'PATH':str(tool/'tools/go/bin')+':/usr/bin:/bin','GOCACHE':str(tool/'gocache'),
 'GOMODCACHE':str(tool/'gomod'),'GOPATH':str(r/'gopath'),'GOTOOLCHAIN':'local',
 'GOWORK':'off','GOENV':'off','GODEBUG':'asynctimerchan=0','GOMAXPROCS':'2',
 'TMPDIR':'/tmp','LANG':'C','LC_ALL':'C','LD_LIBRARY_PATH':str(tool/'sysroot/usr/lib/x86_64-linux-gnu')}
blocker=json.loads((o/'allocation-analysis-blocker.json').read_text())
records={}
for arm in ('baseline','candidate'):
 binding=json.loads((o/(arm+'-source-binding.json')).read_text())
 profile=r/(arm+'-alloc.prof');binary=r/(arm+'-pg.test')
 assert sha(profile.read_bytes())==blocker[arm+'_profile_sha256'],'profile mismatch'
 assert sha(binary.read_bytes())==binding['binary_sha256'],'binary mismatch'
 assert len(binding['selected_files'])==3995,'selected source count mismatch'
 for source in binding['selected_files']:
  assert sha(Path(source['path']).read_bytes())==source['sha256'],source['path']
 commands=[]
 for mode,flags in [('alloc-space',['-top','-nodecount=50','-nodefraction=0','-unit=bytes','-sample_index=alloc_space']),
                    ('alloc-objects',['-top','-nodecount=30','-nodefraction=0','-sample_index=alloc_objects']),
                    ('profile-raw',['-raw'])]:
  cmd=[str(tool/'tools/go/bin/go'),'tool','pprof',*flags,str(binary),str(profile)]
  done=subprocess.run(cmd,env=env,text=True,capture_output=True)
  (o/(arm+'-'+mode+'.txt')).write_text(done.stdout)
  (o/(arm+'-'+mode+'.stderr')).write_text(done.stderr)
  commands.append({'command':cmd,'exit':done.returncode,'stderr':done.stderr})
  if done.returncode:raise RuntimeError('STOP: pprof failed: '+done.stderr)
 raw=(o/(arm+'-profile-raw.txt')).read_text()
 samples_part=raw.split('Samples:\n',1)[1].split('Locations\n',1)[0]
 samples=[]
 for match in re.finditer(r'^\s*(\d+)\s+(\d+)\s+(\d+)\s+(\d+):\s*([0-9 ]+)\n(?:\s*bytes:\[(\d+)\]\n)?',samples_part,re.M):
  samples.append({'values':[int(match[i]) for i in range(1,5)],'locations':[int(x) for x in match[5].split()],
                  'allocation_bucket_bytes':int(match[6]) if match[6] else None})
 assert samples
 locations_part=raw.split('Locations\n',1)[1].split('Mappings\n',1)[0]
 locations={}
 for match in re.finditer(r'^\s*(\d+):\s+(.+?)(?=^\s*\d+:|\Z)',locations_part,re.M|re.S):locations[int(match[1])]=match[2]
 pool_ids={i for i,text in locations.items() if 'sqlparser.init.func1 ' in text}
 totals=[sum(sample['values'][i] for sample in samples) for i in range(4)]
 pool_samples=[s for s in samples if any(i in pool_ids for i in s['locations'])]
 pool_totals=[sum(sample['values'][i] for sample in pool_samples) for i in range(4)]
 buckets={}
 for sample in pool_samples:
  key=str(sample['allocation_bucket_bytes']);value=buckets.setdefault(key,{'weighted_alloc_objects':0,'weighted_alloc_bytes':0,'stack_sample_records':0})
  value['weighted_alloc_objects']+=sample['values'][0];value['weighted_alloc_bytes']+=sample['values'][1];value['stack_sample_records']+=1
 top=(o/(arm+'-alloc-space.txt')).read_text()
 reported_total=int(re.search(r'of (\d+)B total',top)[1]);assert reported_total==totals[1]
 reported_objects=int(re.search(r'of (\d+) total',(o/(arm+'-alloc-objects.txt')).read_text())[1]);assert reported_objects==totals[0]
 gnu_id=re.search(r'^Build ID: (\w+)$',top,re.M)[1]
 mapping=re.search(r'^1: .+$',raw.split('Mappings\n',1)[1],re.M)[0]
 assert str(binary) in mapping and gnu_id in mapping
 note=subprocess.run(['/usr/bin/readelf','-n',str(binary)],env=env,text=True,capture_output=True,check=True)
 assert 'Build ID: '+gnu_id in note.stdout
 events=[json.loads(x) for x in (o/(arm+'-wholepg.jsonl')).read_text().splitlines()]
 output=''.join(e.get('Output','') for e in events if e['Action']=='output')
 panic=output[output.index('panic: test timed out'):]
 header=panic.split('\ngoroutine ',1)[0]
 stack_blocks=re.split(r'(?=goroutine \d+ \[)',panic)
 active_blocks=[s for s in stack_blocks if ('r5_permission_authority_matrix_test.go:' in s if arm=='candidate' else 'r5_outbound_inspection_test.go:' in s)]
 if arm=='candidate':assert any('NewPostgres' in s and 'fixture.go:44' in s for s in active_blocks)
 finished=sorted([e for e in events if e['Action'] in ('pass','fail','skip') and 'Test' in e and '/' not in e['Test']],key=lambda e:e.get('Elapsed',0),reverse=True)
 start=datetime.fromisoformat(events[0]['Time'].replace('Z','+00:00'))
 last_runs=[{'test':e['Test'],'time_utc':e['Time'],'seconds_since_package_start':(datetime.fromisoformat(e['Time'].replace('Z','+00:00'))-start).total_seconds()} for e in events if e['Action']=='run'][-8:]
 env_run=json.loads((r/(arm+'-environment.json')).read_text());env_public=dict(env_run);env_public['TABMAIL_TEST_DB_DSN']='<OWNED '+arm.upper()+' ADMIN DATABASE; loopback '+('55437' if arm=='baseline' else '55438')+'>'
 records[arm]={'profile_sha256':sha(profile.read_bytes()),'binary_sha256':sha(binary.read_bytes()),
  'elf_gnu_build_id':gnu_id,'profile_mapping_matches_binary_path_and_elf_build_id':True,
  'source_binding_file':arm+'-source-binding.json','source_binding_file_sha256':sha((o/(arm+'-source-binding.json')).read_bytes()),
  'all_3995_selected_file_hashes_revalidated':True,'selected_source_binding_sha256':binding['source_binding_sha256'],
  'commands':commands,'sampling_period_bytes':int(re.search(r'^Period: (\d+)',raw,re.M)[1]),
  'profile_time_utc':re.search(r'^Time: (.+)$',raw,re.M)[1],'sample_stack_records':len(samples),
  'weighted_alloc_objects':totals[0],'weighted_alloc_bytes':totals[1],'weighted_inuse_objects':totals[2],'weighted_inuse_bytes':totals[3],
  'pool_init_weighted_alloc_objects':pool_totals[0],'pool_init_weighted_alloc_bytes':pool_totals[1],
  'pool_init_alloc_space_percent':100*pool_totals[1]/totals[1],'pool_init_allocation_buckets':buckets,
  'exact_TotalAlloc_or_Mallocs':'NOT_COLLECTED; cannot recover exact MemStats from default sampled protobuf profile',
  'actual_working_directory':'/workspace/tabmail' if arm=='baseline' else '/workspace/tabmail/internal/store/postgres',
  'only_package_that_executed':sorted({e['Package'] for e in events}),
  'top_level_slowest_completed':[{'test':e['Test'],'elapsed_seconds':e.get('Elapsed'),'terminal':e['Action']} for e in finished[:15]],
  'timeout_header':header,'timeout_active_test_stack_blocks':active_blocks,'last_run_events':last_runs,
  'pause_events':sum(e['Action']=='pause' for e in events),'cont_events':sum(e['Action']=='cont' for e in events),
  'runtime_environment_public':env_public}
# Compare the complete allowlists, not just the declared flags.
ba=json.loads((r/'baseline-environment.json').read_text());ca=json.loads((r/'candidate-environment.json').read_text())
differences=[k for k in sorted(ba.keys()|ca.keys()) if ba.get(k)!=ca.get(k)]
assert differences==['TABMAIL_TEST_DB_DSN']
record={'followup_authorization':'Analyze existing profiles with owned caches; no wholePG rerun; earlier default-cache OSError is not a tool review rejection',
 'earlier_failure_record_preserved':'allocation-analysis-blocker.json','official_pprof_success':True,
 'owned_analysis_environment':env,'wholepg_rerun':False,'runtime_environment_differences':differences,
 'baseline_candidate_comparable':False,'wholepg_improvement_claim':False,'formal_third_replace_eligible':False,
 'allocation_semantics':'pprof weighted sampled cumulative allocation estimates at 524288-byte period; exact runtime.TotalAlloc not recorded',
 'baseline_timeout_stage':'demote subtest initial fixture migration, in Goose Provider.runSQL -> database/sql/pgx I/O via testpg.NewPostgres fixture.go:55; not yet current-authority-after-wait body',
 'baseline_cwd_impact':[
  {'test':'TestR3CoordinatedSnapshotRestoresIntoEmptyTargets','baseline_terminal':'fail','candidate_terminal':'pass','incorrect_path':'/scripts/company_snapshot.py','correct_path':'/workspace/tabmail/scripts/company_snapshot.py','source':'internal/store/postgres/company_backup_test.go:31','effect':'backup/restore rehearsal terminates before backup; different executed work'},
  {'test':'TestP0GooseRestartAndHistoricalGrantSafety/upgrade_and_restart','baseline_terminal':'fail','candidate_terminal':'pass','incorrect_path':'/workspace/tabmail/migrations/00001_baseline.sql','correct_path':'/workspace/tabmail/internal/store/postgres/migrations/00001_baseline.sql','source':'internal/store/postgres/company_integration_test.go:157','effect':'early os.ReadFile failure after fresh fixture migration and DROP SCHEMA, before historical baseline provider/upgrade/restart assertions'},
  {'test':'TestP0GooseRestartAndHistoricalGrantSafety/preserve_incompatible_grants','baseline_terminal':'fail','candidate_terminal':'pass','incorrect_path':'/workspace/tabmail/migrations/00001_baseline.sql','correct_path':'/workspace/tabmail/internal/store/postgres/migrations/00001_baseline.sql','source':'internal/store/postgres/company_integration_test.go:157','effect':'early os.ReadFile failure before historical conflicting-grant provider/assertions'},
  {'test':'TestArchitectureUpgradeBackfillsExistingEmployeeAssets','baseline_terminal':'fail','candidate_terminal':'pass','incorrect_path':'/workspace/tabmail/migrations/0000*.sql','correct_path':'/workspace/tabmail/internal/store/postgres/migrations/0000*.sql','source':'internal/store/postgres/company_upgrade_test.go:31','effect':'glob returned 0 rather than 9 files; old-schema provider/backfill/upgrade assertions never executed'}],
 'candidate_timeout_stage':'freeze/assignment subtest initial fixture, runnable in uuid.NewString at testpg.NewPostgres fixture.go:44 before CREATE DATABASE; not yet in assignment lock-wait operation',
 'candidate_timeout_is':'cumulative package 180s alarm; current top-level about 2s, current subtest about 0s; not a per-test 180s stall',
 'arms':records}
(o/'allocation-analysis-followup.json').write_text(json.dumps(record,indent=2)+'\n')
for arm,s in records.items():print(arm,'weighted_alloc_bytes',s['weighted_alloc_bytes'],'pool_share',s['pool_init_alloc_space_percent'],'pool_buckets',s['pool_init_allocation_buckets'])
