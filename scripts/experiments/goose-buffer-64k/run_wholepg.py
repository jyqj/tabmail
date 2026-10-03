#!/usr/bin/env python3
"""One run per arm, on prepared race binaries and an experiment-owned PG cluster.

This does not compile, seed, tune deadlines, select tests, or qualify product deps.
All raw output is synthetic loopback evidence. A per-arm exclusive marker forbids
an accidental second execution. Memory profiling is the sole instrumentation.
"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import time

p=argparse.ArgumentParser()
p.add_argument('arm',choices=['baseline','candidate'])
p.add_argument('--root',type=Path,required=True)
p.add_argument('--repo',type=Path,required=True)
p.add_argument('--tool-root',type=Path,required=True)
p.add_argument('--port',type=int,default=55437)
a=p.parse_args();r=a.root.resolve();tool=a.tool_root.resolve()
with (r/(a.arm+'-once.json')).open('x') as f:
 json.dump({'arm':a.arm,'started_unix':time.time()},f)
# A complete allowlist: no inherited app, browser, benchmark or secret settings.
env={'PATH':str(tool/'tools/go/bin')+':'+str(tool/'sysroot/usr/lib/postgresql/17/bin')+':/usr/bin:/bin',
 'LD_LIBRARY_PATH':str(tool/'sysroot/usr/lib/x86_64-linux-gnu'),
 'GOMAXPROCS':'2','GODEBUG':'asynctimerchan=0','GOTOOLCHAIN':'local',
 'GOENV':'off','GOWORK':'off','GOFLAGS':'-mod=readonly -p=1',
 'GOMODCACHE':str(tool/'gomod'),'GOCACHE':str(tool/'gocache'),
 'GOPATH':str(r/'gopath'),'TMPDIR':'/tmp','LANG':'C','LC_ALL':'C',
 'TABMAIL_TEST_DB_DSN':f'postgres://goose_exp@127.0.0.1:{a.port}/{a.arm}_admin?sslmode=disable'}
pg=str(tool/'sysroot/usr/lib/postgresql/17/bin/psql')
pgargs=[pg,'-h','127.0.0.1','-p',str(a.port),'-U','goose_exp','-d','postgres','-X','-v','ON_ERROR_STOP=1']
# Each arm's fresh empty admin DB, followed by fixture-owned fresh databases.
subprocess.run(pgargs+['-c','CREATE DATABASE '+a.arm+'_admin'],env=env,check=True,capture_output=True)
with (r/(a.arm+'-settings.txt')).open('x') as f:
 subprocess.run(pgargs+['-At','-c',"SELECT name,setting FROM pg_settings WHERE name IN ('server_version','fsync','full_page_writes','synchronous_commit','max_connections','shared_buffers') ORDER BY name"],env=env,check=True,stdout=f)
cmd=[str(tool/'tools/go/bin/go'),'tool','test2json','-t','-p','tabmail/internal/store/postgres',
 str(r/(a.arm+'-pg.test')),'-test.v=test2json','-test.count=1','-test.timeout=180s',
 '-test.memprofile='+str(r/(a.arm+'-alloc.prof'))]
with (r/(a.arm+'-environment.json')).open('x') as f: json.dump(env,f,indent=2)
start=time.monotonic()
with (r/(a.arm+'-wholepg.jsonl')).open('x') as f, (r/(a.arm+'-wholepg.stderr')).open('x') as err:
 done=subprocess.run(cmd,cwd=a.repo/"internal/store/postgres",env=env,stdout=f,stderr=err)
wall=time.monotonic()-start
with (r/(a.arm+'-databases-after.txt')).open('x') as f:
 subprocess.run(pgargs+['-At','-c',"SELECT datname FROM pg_database WHERE datname NOT IN ('template0','template1','postgres') ORDER BY datname"],env=env,stdout=f,check=True)
result={'arm':a.arm,'command':cmd,'test_timeout_seconds':180,'process_exit':done.returncode,
 'wall_seconds_excluding_preparation_and_compilation':wall,'memprofile_exists':(r/(a.arm+'-alloc.prof')).exists(),
 'working_directory':str(a.repo/'internal/store/postgres'),
 'package_scope':'entire unfiltered tabmail/internal/store/postgres; default build tags',
 'equivalent_test_flags':['-race (compiled)','-count=1','-timeout=180s'],
 'profiling':'default-rate allocation profile; sampled estimates, not exact TotalAlloc'}
with (r/(a.arm+'-run.json')).open('x') as f:json.dump(result,f,indent=2)
print(json.dumps(result))
