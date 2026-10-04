"""UNEXECUTED PROPOSAL. Requires a separately reviewed owning supervisor.
No PostgreSQL provisioning/drop/stop or outer descendant cleanup is implemented here.
Parent-held approval SHA authenticates paths, byte pins and already provisioned scope.
"""
import argparse, hashlib, json, os, re, shutil, signal, stat, subprocess, sys, traceback, uuid
from pathlib import Path
from urllib.parse import urlencode
SOURCE='593bbc6a3ac91b5dbf8917602e45900bff094e27'
TREE='b5248dfb96e9478c5720ea7d5734433d3ddc6941'
GO_PIN='76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8'
p=argparse.ArgumentParser(description=__doc__)
p.add_argument('--approval',type=Path,required=True)
p.add_argument('--approval-sha256',required=True)
a=p.parse_args()
os.umask(0o077)
def digest(raw): return hashlib.sha256(raw).hexdigest()
def require(condition,label):
    if not condition:raise ValueError(label)
def regular(path):
    path=Path(path);info=path.lstat()
    require(stat.S_ISREG(info.st_mode) and not path.is_symlink(),'regular file required')
    return path.read_bytes()
def directory(path,private=False):
    path=Path(path)
    require(path.is_absolute() and path==path.resolve(strict=True),'canonical existing directory required')
    for part in (path,*path.parents):require(part.is_dir() and not part.is_symlink(),'directory ancestor differs')
    require(path.stat().st_uid==os.getuid(),'directory owner differs')
    if private:require(stat.S_IMODE(path.stat().st_mode)==0o700,'private directory mode differs')
    return path
raw=regular(a.approval)
require(re.fullmatch('[0-9a-f]{64}',a.approval_sha256) and digest(raw)==a.approval_sha256,'independent approval pin differs')
def unique(pairs):
    value={}
    for key,item in pairs:
        require(key not in value,'duplicate approval key');value[key]=item
    return value
approved=json.loads(raw,object_pairs_hook=unique)
require(approved['schema_version']==2 and approved['source_commit']==SOURCE and approved['source_tree']==TREE,'exact frozen source approval required')
require(approved['authorized_single_probe'] is True,'parent single-probe authorization required')
require(approved['controller_sha256']==digest(regular(Path(__file__).resolve())),'controller bytes differ')
require(sys.flags.isolated and sys.flags.no_site and sys.flags.dont_write_bytecode,'use -I -S -B')
tools=approved['tools']
for name,row in tools.items():
    path=Path(row['path'])
    require(path.is_absolute() and path==path.resolve(strict=True),'canonical tool path required')
    require(digest(regular(path))==row['sha256'],'tool byte pin differs: '+name)
require(tools['go']['sha256']==GO_PIN,'Go producer pin differs')
require(Path(sys.executable).resolve()==Path(tools['python']['path']),'controller Python differs')
expected_env=approved['environment']
allowed_env={'PATH','HOME','TMPDIR','LANG','LC_ALL','GOCACHE','GOMODCACHE','GOPATH','GOENV','GOWORK','GOFLAGS','GOTOOLCHAIN','GOPROXY','GOSUMDB','R5_DIAGNOSTIC_ROOT','LD_LIBRARY_PATH'}
require(set(expected_env)<=allowed_env and dict(os.environ)==expected_env,'inherited environment differs')
for name,key in (('python','python3'),('git','git'),('node','node'),('go','go'),('cc','cc')):
    resolved=shutil.which(key)
    require(resolved is not None and Path(resolved).resolve()==Path(tools[name]['path']),'PATH resolution differs: '+key)
require(expected_env.get('GOENV')=='off' and expected_env.get('GOWORK')=='off' and expected_env.get('GOFLAGS')=='' and expected_env.get('GOTOOLCHAIN')=='local','Go environment differs')
root=directory(approved['root'],True);source=directory(approved['source']);evidence=directory(approved['evidence'],True)
cache=directory(expected_env['GOCACHE']);modulecache=directory(expected_env['GOMODCACHE'])
diag=directory(expected_env['R5_DIAGNOSTIC_ROOT'],True)
directory(expected_env['HOME'],True);directory(expected_env['TMPDIR'],True)
require(source==root/'source' and (source/'.git').is_dir() and not (source/'.git').is_symlink(),'real ownedroot/source Git clone required')
for path in (evidence,diag,cache,modulecache):
    require(not path.is_relative_to(root),'evidence/cache/diagnostics must be outside runtime root')
for path in (evidence,diag):
    require(not any(path.is_relative_to(tree) for tree in (cache,modulecache)),'evidence/diagnostics outside caches required')
require(not any(evidence.iterdir()),'new empty evidence required')
for ancestor in root.parents:require(not (ancestor/'node_modules').exists(),'ancestor dependency shadow')

def cg(pid):
    lines=Path('/proc')/str(pid)/'cgroup'
    rows=lines.read_text().splitlines()
    require(len(rows)==1 and rows[0].startswith('0::'),'unified cgroup required')
    return rows[0][3:]
containment=approved['containment']
require(containment['probe_cgroup']!='/' and cg('self')==containment['probe_cgroup'],'review-owned probe cgroup required')
for namespace in ('net','pid'):
    require(os.readlink('/proc/self/ns/'+namespace)==containment[namespace+'_namespace'],'namespace identity differs')
require(re.fullmatch('[0-9a-f]{40}',containment['supervisor_review_commit'] or '') and re.fullmatch('[0-9a-f]{64}',containment['supervisor_sha256'] or ''),'reviewed supervisor required')
supervisor_path=Path(containment['supervisor_path'])
require(supervisor_path.is_absolute() and supervisor_path==supervisor_path.resolve(strict=True) and digest(regular(supervisor_path))==containment['supervisor_sha256'],'reviewed supervisor bytes differ')
pg=approved['postgres'];pgdata=directory(pg['data_directory'],True)
require(not any(pgdata.is_relative_to(tree) for tree in (root,evidence,diag,cache,modulecache)),'PGDATA outside probe/runtime/cache trees required')
require(pg['host']=='127.0.0.1' and type(pg['port']) is int and 1024<=pg['port']<=65535,'owned loopback endpoint required')
require(re.fullmatch('r5_slice5_[a-z0-9_]{8,40}',pg['role']) and pg['database']=='postgres','fresh task role/admin database required')
require(type(pg['pid']) is int and pg['pid']>1,'postmaster identity required')
pidfile=regular(pgdata/'postmaster.pid').decode().splitlines()
require(len(pidfile)>=4 and int(pidfile[0])==pg['pid'] and Path(pidfile[1])==pgdata and int(pidfile[2])==pg['start_epoch'] and int(pidfile[3])==pg['port'],'postmaster pidfile differs')
require(cg(pg['pid'])==containment['pg_cgroup'] and containment['pg_cgroup']!=containment['probe_cgroup'],'separate owned PG cgroup required')
require(Path(os.readlink('/proc/'+str(pg['pid'])+'/exe'))==Path(tools['postgres']['path']),'postmaster executable differs')
require(digest(regular(pgdata/'pg_hba.conf'))==pg['pg_hba_sha256'],'approved loopback-only HBA bytes required')
for namespace in ('net','pid'):
    require(os.readlink('/proc/'+str(pg['pid'])+'/ns/'+namespace)==containment[namespace+'_namespace'],'PG namespace differs')

def retain_command(argv,stdout,stderr,status):
    target=diag/('controller-command-'+uuid.uuid4().hex);target.mkdir(mode=0o700)
    for name,value in (('stdout',stdout or b''),('stderr',stderr or b'')):
        require(isinstance(value,bytes) and len(value)<=64*1024*1024,'command retention bound exceeded')
        with (target/(name+'.private')).open('xb') as f:f.write(value)
    with (target/'status.private.json').open('x') as f:
        json.dump(dict(argv=argv,status=status),f)

def run(argv,env=None):
    try:
        value=subprocess.run(argv,env=env,capture_output=True,check=True,timeout=10)
    except (subprocess.CalledProcessError,subprocess.TimeoutExpired) as error:
        try:retain_command(argv,error.stdout,error.stderr,type(error).__name__)
        except BaseException as retention_error:
            error.retention_error=retention_error
            error.add_note('private command retention failed: '+type(retention_error).__name__)
        raise
    retain_command(argv,value.stdout,value.stderr,'returncode0')
    return value.stdout

def authenticate_source():
    git=tools['git']['path'];base=[git,'-C',str(source)]
    require(run(base+['rev-parse','HEAD']).decode().strip()==SOURCE,'clone commit differs')
    require(run(base+['rev-parse','HEAD^{tree}']).decode().strip()==TREE,'clone tree differs')
    require(run(base+['status','--porcelain','--ignored','--untracked-files=all'])==b'','clean exact clone required')
    count=0
    for row in run(base+['ls-tree','-rz','HEAD']).split(b'\0'):
        if not row:continue
        meta,name=row.split(b'\t');mode,kind,blob=meta.decode().split();name=name.decode()
        require(kind=='blob' and mode in ('100644','100755'),'regular tracked input required')
        path=source/name
        require(path.resolve(strict=True)==path,'source symlink ancestor rejected')
        raw=regular(path)
        require(hashlib.sha1(b'blob '+str(len(raw)).encode()+b'\0'+raw).hexdigest()==blob,'tracked bytes differ')
        require(('100755' if path.stat().st_mode & 0o111 else '100644')==mode,'tracked mode differs');count+=1
    require(count==2331,'frozen path count differs')

def publish(name,value):
    raw=(json.dumps(value,sort_keys=True,separators=(',',':'))+'\n').encode();path=evidence/name
    with path.open('xb') as f:f.write(raw)
    require(regular(path)==raw,'published bytes differ');return path,digest(raw)

QUERY="""SELECT json_build_object('data_directory',current_setting('data_directory'),'listen_addresses',current_setting('listen_addresses'),'port',current_setting('port')::int,'role',current_user,'database',current_database(),'version',current_setting('server_version_num')::int,'system_identifier',(pg_control_system()).system_identifier::text,'databases',(SELECT coalesce(json_agg(datname ORDER BY datname),'[]'::json) FROM pg_database),'other_clients',(SELECT count(*) FROM pg_stat_activity WHERE backend_type='client backend' AND pid<>pg_backend_pid()))"""
def pg_snapshot():
    env=dict(expected_env,PGHOST='127.0.0.1',PGPORT=str(pg['port']),PGUSER=pg['role'],PGDATABASE='postgres',PGSSLMODE='disable',PGCONNECT_TIMEOUT='5',PGPASSFILE='/dev/null',PGSERVICEFILE='/dev/null',PGOPTIONS='-c statement_timeout=5000')
    value=json.loads(run([tools['psql']['path'],'-X','-w','-A','-t','-v','ON_ERROR_STOP=1','-c',QUERY],env=env))
    require(value['data_directory']==str(pgdata) and value['listen_addresses']=='127.0.0.1' and value['port']==pg['port'] and value['role']==pg['role'] and value['database']=='postgres','live cluster identity differs')
    require(170000<=value['version']<180000 and value['system_identifier']==pg['system_identifier'],'fresh approved PG17 cluster differs')
    return value
executor=None;stage='preflight';failed=False

def cancel(signum,frame):
    if executor is not None:executor.request_cancel()
    else:raise KeyboardInterrupt('controller cancelled before batch ownership')
for sig in (signal.SIGINT,signal.SIGTERM):signal.signal(sig,cancel)
try:
    authenticate_source()
    before=pg_snapshot();publish('pg-before.private.json',before)
    require(before['databases']==['postgres','template0','template1'] and before['other_clients']==0,'nonempty/shared cluster rejected')
    # Only this independently identified task cluster can become the inherited Go fixture DSN.
    os.environ['TABMAIL_TEST_DB_DSN']='postgresql://'+pg['role']+'@127.0.0.1:'+str(pg['port'])+'/postgres?'+urlencode(dict(sslmode='disable',connect_timeout='5'))
    sys.path[:0]=[str(source/'scripts'),str(source/'scripts/preparation')]
    import r5_source_inventory as inventory
    import r5_selected_source_binding_v3 as selected
    import r5_selected_binding_consumer as consumer
    import r5_external_runtime as runtime
    import r5_external_batch as batch
    stage='archive';run_id=uuid.uuid4().hex
    archive=inventory.capture_current_source(source,purpose='protocol',policy=inventory.ARCHIVE_POLICY,build_context=dict(selected.CONTEXT,build_tag_sets=[[],['r5protocol']]))
    archive_path,archive_pin=publish('archive-v4.json',archive)
    stage='selected';observations={}
    for slot,context in (('default',selected.DEFAULT_CONTEXT),('race-r5protocol',selected.CONTEXT)):
        receipt=selected.capture(source,Path(tools['go']['path']),cache=cache,modulecache=modulecache,context=context)
        pin=selected.observation_digest(receipt);selected._verify_receipt(receipt,pin)
        path,byte_pin=publish(slot+'.json',receipt)
        observations[slot]=dict(receipt_path=str(path),receipt_byte_sha256=byte_pin,observation_sha256=pin,context=receipt['base_source']['build_context'])
    bundle=dict(schema_version=1,policy=consumer.PIN_POLICY,run_id=run_id,source_commit=SOURCE,source_root=str(source),selected_binding_version=3,producer=dict(path=tools['go']['path'],sha256=GO_PIN,version='go1.25.7'),observations=observations)
    bundle_path,bundle_pin=publish('controller-selection-pins.json',bundle)
    stage='admission'
    manifest=runtime.capture(source,root,archive_path,Path(observations['default']['receipt_path']),Path(observations['race-r5protocol']['receipt_path']),Path(tools['node']['path']),Path(tools['go']['path']),cache=cache,modulecache=modulecache,selected_binding_version=3,bundle_path=bundle_path,bundle_byte_sha256=bundle_pin,run_id=run_id,source_commit=SOURCE,evidence_parent=evidence)
    manifest_path,manifest_pin=publish('runtime-v3.json',manifest)
    require(runtime.load_pinned(manifest_path,manifest_pin,selected_binding_version=3)==manifest,'manifest persistence differs')
    runtime.validate(manifest,selected_binding_version=3)
    stage='contract'
    contract=batch.capture(manifest,Path(tools['go']['path']),'probe',selected_binding_version=3,runtime_manifest_path=str(manifest_path),runtime_manifest_sha256=manifest_pin)
    contract_path,contract_pin=publish('batch-v2-probe.json',contract)
    loaded=batch.load(contract_path,contract_pin,selected_binding_version=3);batch.validate_contract(loaded,selected_binding_version=3)
    authenticate_source()
    stage='one-probe';executor=batch.Batch(loaded,evidence/'probe-output',selected_binding_version=3)
    receipt=executor.run();receipt_path,receipt_pin=publish('batch-result.json',receipt)
    publish('controller-pins.json',dict(source_commit=SOURCE,approval_sha256=a.approval_sha256,run_id=run_id,archive=archive_pin,bundle=bundle_pin,runtime=manifest_pin,batch=contract_pin,result=receipt_pin))
    require(receipt['status']=='BATCH_QUALIFIED','batch did not qualify')
    stage='postcheck';authenticate_source()
except BaseException as error:
    failed=True
    with (evidence/'controller-failure.private.txt').open('x') as f:traceback.print_exc(file=f)
    publish('controller-failure.json',dict(stage=stage,error_type=type(error).__name__,gate='FAILED',cleanup_required=True))
finally:
    # Read-only observation. The external supervisor must contain/reap descendants,
    # check DBs again after containment, stop the cluster and attest final cleanup.
    try:
        after=pg_snapshot();publish('pg-after.private.json',after)
        require(after['databases']==['postgres','template0','template1'] and after['other_clients']==0,'fixture database/backend cleanup incomplete')
    except BaseException as error:
        failed=True;publish('pg-cleanup-incomplete.json',dict(error_type=type(error).__name__,gate='FAILED',supervisor_cleanup_required=True))
publish('controller-terminal.json',dict(status='FAILED' if failed else 'PROBE_RETURNED',physical_acceptance='requires_independent_supervisor_cleanup',stage=stage))
raise SystemExit(1 if failed else 0)
