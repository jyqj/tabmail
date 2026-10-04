"""Explicit batch v1. Sole cooperating executor, never hostile-write protection.

Old runtime v2 entry points remain unchanged. Private RPC carries case identity,
never executable/argv. Only this leased supervisor starts and reaps Vitest groups.
"""
from __future__ import annotations
import argparse
import concurrent.futures
import contextlib
import hmac
import json
import os
from pathlib import Path
import secrets
import signal
import socketserver
import subprocess
import sys
import threading
import time

import r5_external_runtime as runtime
sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import check_r5_protocol as protocol

POLICY = 'r5_external_batch_validation_v1'
GO_OLD = 'TestR5ProtocolComponentObservations'
GO_NEW = 'TestR5ProtocolBatchComponentObservations'
PROBE = 'TestR5ExternalBatchIsolationProbe'
CATALOG = 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'
HELPER = 'scripts/preparation/r5_external_batch.py'
BRIDGE = 'internal/api/handlers/r5_external_batch_test.go'
PROBE_CONFIG = 'vitest.r5batch-probe.config.ts'
MAX_WORKERS = 4


def derive(data):
    """Same scope selection as the existing component consumer; no guessed cases."""
    groups, paths, children, python = {}, set(), {}, []
    for row in data['cases']:
        for adapter in row.get('shared_adapters', []):
            if adapter.get('runner') == 'vitest':
                python.append('R5 shared receipt ' + row['id'])
                continue
            if 'components' not in adapter['layers'] and 'unit' not in adapter['layers']:
                continue
            group = (adapter['package'], adapter.get('build_tag', ''))
            groups.setdefault(group, set()).add(adapter['test'])
            for path in adapter.get('runtime_test_paths', []):
                if path in paths:
                    raise ValueError('duplicate required Go path')
                paths.add(path)
                if adapter.get('component_source'):
                    prefix = GO_OLD + '/' + row['id'] + '/'
                    if not path.startswith(prefix):
                        raise ValueError('unknown component path')
                    key = path.removeprefix(GO_OLD + '/')
                    children[key] = dict(case_id=row['id'], variant=path[len(prefix):],
                                         assertion='R5 protocol component ' + row['id'] + ' ' + path[len(prefix):] + ' secure behavior',
                                         input_sha256=runtime.digest(runtime.canonical(row['input'])))
    if len(python) != len(set(python)) or not paths or not children:
        raise ValueError('duplicate/missing required terminals')
    return dict(groups=[dict(package=p, tag=t, tests=sorted(v)) for (p,t),v in sorted(groups.items())],
                go_paths=sorted(paths), children=children, python_assertions=sorted(python))


def file_digest(path):
    with runtime.descriptors() as files:
        return runtime.digest(files.file(Path(path)))


def capture(manifest, go, mode='components'):
    source = Path(manifest['source']['path'])
    data = protocol.load_cases(source / CATALOG, source)
    contract = dict(schema_version=1, policy=POLICY, status='UNADOPTED', runtime=manifest,
                    runtime_sha256=runtime.digest(runtime.canonical(manifest)),
                    catalog_sha256=file_digest(source/CATALOG), required=derive(data),
                    mode=mode, workers=4, budgets=dict(go=120, process=180, case=75),
                    go=dict(path=str(go), sha256=file_digest(go), version=subprocess.check_output([str(go),'version'],text=True).strip()),
                    build_context=dict(runtime.selected.CONTEXT,build_tag_sets=[[],['r5protocol']]),
                    argv=dict(go_commands=[ [str(go)]+protocol.shared_command(g['package'],g['tag'],[GO_NEW if name==GO_OLD else name for name in g['tests']])[1:] for g in derive(data)['groups']] if mode=='components' else [[str(go)]+protocol.shared_command('./internal/api/handlers','r5protocol',[PROBE])[1:]],
                              child=[manifest['node']['path'],manifest['cli']['path'],'run','--cache=false','--experimental.fsModuleCache=false','--config','vitest.r5protocol.config.ts' if mode=='components' else PROBE_CONFIG,'--reporter=json','--outputFile','<fresh-report>'],
                              python=protocol.external_component_command(manifest,'<fresh-report>',probe=mode=='probe')),
                    helper_sha256=file_digest(source/HELPER), bridge_sha256=file_digest(source/BRIDGE),
                    probe_config_sha256=file_digest(source/'web'/PROBE_CONFIG),
                    concurrency_boundary=runtime.BOUNDARY, product_green=False, task_complete=False)
    if contract['go']['version']!='go version go1.25.7 linux/amd64':
        raise ValueError('fixed Go1.25.7 required')
    if mode not in ('components', 'probe'):
        raise ValueError('unknown batch mode')
    return contract


def validate_contract(contract):
    if contract.get('policy') != POLICY or contract.get('status') != 'UNADOPTED':
        raise ValueError('old policy/promotion cannot authorize batch')
    if Path(__file__).resolve()!=Path(contract['runtime']['source']['path'])/HELPER:
        raise ValueError('batch helper must execute from bound source')
    if Path(sys.executable).resolve()!=Path(contract['runtime']['python']['path']):
        raise ValueError('batch interpreter identity differs')
    if capture(contract['runtime'], Path(contract['go']['path']), contract['mode']) != contract:
        raise ValueError('batch contract/required inputs drift')
    runtime.validate(contract['runtime'])


def load(path, pin):
    with runtime.descriptors() as files:
        raw = files.file(Path(path))
    if runtime.digest(raw) != pin:
        raise ValueError('independent batch pin differs')
    result = runtime.source_inventory.strict_json(raw)
    if result.get('policy') != POLICY or result.get('status') != 'UNADOPTED':
        raise ValueError('old policy/promotion cannot authorize batch')
    return result


@contextlib.contextmanager
def lease(manifest, cancelled, deadline):
    """Serialize cooperating batches; never steal a stale or foreign token."""
    with contextlib.ExitStack() as stack:
        while True:
            if cancelled.is_set() or time.monotonic()>=deadline:
                raise ValueError('batch cancelled/deadline while acquiring lease')
            try:
                nonce=stack.enter_context(runtime.owner(manifest))
                break
            except FileExistsError:
                cancelled.wait(0.05)
        yield nonce


class OwnedProcess:
    """Owned process group; communicate joins pipe readers, wait reaps direct child.

    Linux subreaper additionally reaps orphaned owned descendants. No group ID
    is accepted from RPC or any external input.
    """
    def __init__(self, argv, *, cwd, env):
        self.process = subprocess.Popen(argv, cwd=cwd, env=env, stdout=subprocess.PIPE,
                                        stderr=subprocess.PIPE, start_new_session=True)
        self.group = self.process.pid
        self.joined = False
        self.tail = False

    def kill(self):
        try:
            os.killpg(self.group, signal.SIGKILL)
        except ProcessLookupError:
            pass

    def finish(self, cancelled, deadline):
        timed_out = False
        while True:
            if cancelled.is_set() or time.monotonic() >= deadline:
                timed_out = True
                self.kill()
            try:
                stdout, stderr = self.process.communicate(timeout=0.05)
                break
            except subprocess.TimeoutExpired:
                if self.process.poll() is not None:
                    try:
                        os.killpg(self.group,0)
                        self.tail = True
                        self.kill()
                    except ProcessLookupError:
                        pass
                continue
        # A daemon can close inherited pipes and outlive the direct process.
        try:
            os.killpg(self.group, 0)
            self.tail = True
        except ProcessLookupError:
            pass
        if self.tail:
            self.kill()
        # All orphan descendants of this session are ours, never unrelated PIDs.
        while True:
            try:
                pid, _ = os.waitpid(-self.group, 0)
                if not pid:
                    break
            except ChildProcessError:
                break
        try:
            os.killpg(self.group, 0)
        except ProcessLookupError:
            self.joined = True
        if not self.joined:
            raise ValueError('owned process group not physically joined')
        return dict(exit_code=self.process.returncode, timeout=timed_out, tail=self.tail,
                    stdout=stdout, stderr=stderr)


def become_subreaper():
    import ctypes
    if sys.platform != 'linux' or ctypes.CDLL(None, use_errno=True).prctl(36, 1, 0, 0, 0) != 0:
        raise ValueError('Linux owned descendant subreaper required')


def join_adopted_tails():
    """Final subreaper barrier after all registered roots/RPC owners joined.

    An owned daemon may create a new session. Such orphans are adopted by this
    isolated supervisor; no external PID or process group is accepted as input.
    Any tail rejects the batch, even when it is successfully terminated/reaped.
    """
    found=False
    while True:
        children=[]
        for entry in Path('/proc').glob('[0-9]*/stat'):
            try:
                fields=entry.read_text().rsplit(') ',1)[1].split()
                if int(fields[1])==os.getpid():children.append(int(entry.parent.name))
            except (FileNotFoundError,ProcessLookupError):pass
        children=sorted(set(children))
        if not children:return found
        found=True
        for pid in children:
            try:os.kill(pid,signal.SIGKILL)
            except ProcessLookupError:pass
        for pid in children:
            try:os.waitpid(pid,0)
            except ChildProcessError:pass


def exact_assertions(report, expected, exit_code):
    assertions = [a for result in report.get('testResults', []) for a in result.get('assertionResults', [])]
    names = [a.get('fullName') for a in assertions]
    counters=('numTotalTests','numPassedTests','numFailedTests','numPendingTests')
    return (all(type(report.get(key)) is int for key in counters)
            and exit_code == 0 and report.get('success') is True and len(names) == len(set(names))
            and set(names) == set(expected) and all(a.get('status') == 'passed' for a in assertions)
            and report.get('numTotalTests') == report.get('numPassedTests') == len(expected)
            and report.get('numFailedTests') == report.get('numPendingTests') == 0
            and report.get('numRuntimeErrorTestSuites', 0) == 0)


class Batch:
    def __init__(self, contract, output, environment=None):
        self.contract = contract
        self.manifest = contract['runtime']
        self.source = Path(self.manifest['source']['path'])
        self.output = Path(output)
        if not self.output.is_absolute() or '..' in self.output.parts or str(self.output)!=os.path.abspath(self.output):
            raise ValueError('canonical absolute private output required')
        if self.output.is_relative_to(self.source) or self.output.is_relative_to(Path(self.manifest['dependency_root']['path'])/'node_modules'):
            raise ValueError('private output outside runtime trees required')
        self.env = dict(os.environ if environment is None else environment)
        runtime.clean_environment(self.env)
        if contract.get('build_context'):
            self.env=runtime.source_inventory.bound_environment(contract['build_context'],self.env)
            self.env['GOTOOLCHAIN']='local'
        self.nonce = secrets.token_hex(32)
        self.pin=runtime.digest(runtime.canonical(contract))
        self.cancelled = threading.Event()
        self.guard = threading.Lock()
        self.slots = threading.BoundedSemaphore(MAX_WORKERS)
        self.active = {}
        self.seen = set()
        self.results = {}
        self.ack = None
        self.closed = False
        self.max_active = 0
        self.invalid = False
        self.published = False
        self.started = False
        self.leased = False
        self.socket = self.output/'channel.sock'
        self.cancelled_keys = set()
        self.deadline = None
        self.children = contract['required']['children'] if contract['mode'] == 'components' else {
            'probe/'+str(i):dict(case_id='probe',variant=str(i),assertion='R5 batch independent realm loopback PostgreSQL') for i in range(8)}

    def authenticate(self, request):
        if not self.leased:
            raise ValueError('batch capability is outside a validated owned lease')
        if (not isinstance(request, dict) or not isinstance(request.get('nonce'), str)
                or not hmac.compare_digest(request['nonce'], self.nonce)
                or request.get('contract_sha256')!=self.pin):
            raise ValueError('invalid owned batch capability')

    def rpc(self, request):
        self.authenticate(request)
        operation = request.get('operation')
        if operation == 'cancel':
            if set(request) != {'nonce','contract_sha256','operation','key'} or request['key'] not in self.children:
                raise ValueError('unbound cancel request')
            with self.guard:
                self.cancelled_keys.add(request['key'])
                child = self.active.get(request['key'])
                if child:
                    child.kill()
            return dict(cancelled=True)
        if operation == 'ack':
            if set(request) != {'nonce','contract_sha256','operation','completed'}:
                raise ValueError('unbound owner acknowledgement')
            completed = request['completed']
            with self.guard:
                if (self.ack is not None or self.active or not isinstance(completed,list)
                        or len(completed)!=len(set(completed)) or set(completed)!=set(self.children)
                        or self.seen!=set(self.children) or set(self.results)!=set(self.children)):
                    self.invalid = True
                    raise ValueError('missing/duplicate/nonterminal owner acknowledgement')
                self.ack = sorted(completed)
            return dict(acknowledged=True)
        if operation != 'case' or set(request) != {'nonce','contract_sha256','operation','key','fixture'}:
            raise ValueError('unbound batch operation/argv')
        key = request['key']
        with self.guard:
            if key not in self.children or key in self.seen or self.closed or self.cancelled.is_set():
                self.invalid = True
                raise ValueError('duplicate/unknown/closed case')
            self.seen.add(key)
        with self.slots:
            if self.cancelled.is_set() or time.monotonic() >= self.deadline:
                raise ValueError('batch deadline/cancellation')
            fixture = Path(request['fixture'])
            if (not fixture.is_absolute() or fixture.is_relative_to(self.source)
                    or fixture.is_relative_to(Path(self.manifest['dependency_root']['path'])/'node_modules')):
                raise ValueError('private fixture required')
            info=fixture.lstat()
            if info.st_uid!=os.getuid() or info.st_mode&0o077:
                raise ValueError('owned private fixture mode required')
            with runtime.descriptors() as files:
                fixture_bytes=files.file(fixture)
                content = runtime.source_inventory.strict_json(fixture_bytes)
            fixture_hash=runtime.digest(fixture_bytes)
            row = self.children[key]
            if self.contract['mode']=='components':
                if (content.get('case_id') != row['case_id'] or content.get('variant') != row['variant']
                        or content.get('case_sha256') != self.contract['catalog_sha256']
                        or runtime.digest(runtime.canonical(content.get('input')))!=row['input_sha256']):
                    raise ValueError('fixture/case/catalog binding differs')
            elif content.get('case_id') != key:
                raise ValueError('probe fixture identity differs')
            report = self.output/'staged'/key.replace('[]','empty_array')/'vitest.json'
            report.parent.mkdir(parents=True, exist_ok=True, mode=0o700)
            if report.exists():
                raise ValueError('fresh staged report required')
            argv = [str(report) if arg=='<fresh-report>' else arg for arg in self.contract['argv']['child']]
            env = dict(self.env, TABMAIL_R5_PROTOCOL_COMPONENT_FIXTURE=str(fixture))
            owned = OwnedProcess(argv,cwd=self.manifest['execution']['cwd'],env=env)
            with self.guard:
                self.active[key] = owned
                if key in self.cancelled_keys:
                    owned.kill()
                self.max_active = max(self.max_active,len(self.active))
            try:
                result = owned.finish(self.cancelled,min(self.deadline,time.monotonic()+75))
                # Keep child stdout and error bodies private; RPC returns only exit/lifecycle.
                (report.parent/'stdout').write_bytes(result.pop('stdout'))
                (report.parent/'stderr').write_bytes(result.pop('stderr'))
                with runtime.descriptors() as fixture_files:
                    if runtime.digest(fixture_files.file(fixture))!=fixture_hash or runtime.preparation.identity(fixture.lstat())!=runtime.preparation.identity(info):
                        raise ValueError('private fixture content/descriptor drift')
                raw = report.read_bytes()
                passed = exact_assertions(json.loads(raw),[row['assertion']],result['exit_code'])
                packet = dict(result,passed=passed,report_sha256=runtime.digest(raw),report=str(report))
                with self.guard:
                    self.results[key] = packet
                return packet
            finally:
                if not owned.joined:
                    owned.kill()
                    owned.finish(self.cancelled,time.monotonic())
                with self.guard:
                    self.active.pop(key,None)

    def run_process(self, argv, cwd, env, seconds=180):
        if self.cancelled.is_set() or time.monotonic()>=self.deadline:
            raise ValueError('batch closed to further dispatch')
        owned = OwnedProcess(argv,cwd=cwd,env=env)
        return owned.finish(self.cancelled,min(self.deadline,time.monotonic()+seconds))

    def run(self):
        if self.started:
            raise ValueError('batch instance is single-use; previous receipts are immutable')
        self.started=True
        try:
            return self._run()
        except Exception as error:
            # An observed lease-finalization failure must invalidate a receipt
            # already staged before context-manager release. Never touch a
            # previous attempt's existing output or any historical receipt.
            if not self.published:
                raise
            path=self.output/'receipt.json'
            receipt=runtime.source_inventory.strict_json(path.read_bytes())
            receipt['status']='BATCH_REJECTED'
            receipt['errors'].append('lease finalization rejected: '+type(error).__name__)
            for child in receipt['children'].values():child['qualified']=False
            pending=self.output/'receipt.pending'
            pending.write_bytes(runtime.canonical(receipt));os.chmod(pending,0o600)
            pending.replace(path)
            return receipt
        finally:
            self.leased=False

    def _run(self):
        self.output.mkdir(mode=0o700,parents=True,exist_ok=False)
        os.chmod(self.output,0o700)
        if self.output.is_relative_to(self.source) or self.output.is_relative_to(Path(self.manifest['dependency_root']['path'])/'node_modules'):
            raise ValueError('private output outside runtime trees required')
        become_subreaper()
        self.deadline = time.monotonic()+180
        errors, terminals = [], []
        before = after = False
        with lease(self.manifest,self.cancelled,self.deadline) as nonce, runtime.descriptors() as anchors:
            self.nonce=nonce
            token=Path(self.manifest['dependency_root']['path'])/'.r5-runtime-owner'
            token_identity=runtime.preparation.identity(token.lstat())
            for path in (self.source,Path(self.manifest['execution']['cwd']),Path(self.manifest['dependency_root']['path'])):
                anchors.directory(path)
            validate_contract(self.contract)
            before = True
            self.leased=True
            batch = self
            class Handler(socketserver.StreamRequestHandler):
                def handle(self):
                    try:
                        self.connection.settimeout(80)
                        raw = self.rfile.readline(65537)
                        if len(raw)>65536: raise ValueError('bounded RPC required')
                        response = batch.rpc(runtime.source_inventory.strict_json(raw))
                        response['ok'] = True
                    except Exception as error:
                        batch.invalid = True
                        response = dict(ok=False,error=type(error).__name__)
                    self.wfile.write(runtime.canonical(response)+b'\n')
            class Server(socketserver.ThreadingUnixStreamServer):
                daemon_threads = False
                block_on_close = True
            server = Server(str(self.socket),Handler)
            os.chmod(self.socket,0o600)
            serving = threading.Thread(target=server.serve_forever)
            serving.start()
            try:
                env = dict(self.env,TABMAIL_R5_BATCH_SOCKET=str(self.socket),TABMAIL_R5_BATCH_NONCE=self.nonce,
                           TABMAIL_R5_BATCH_CATALOG_SHA256=self.contract['catalog_sha256'],
                           TABMAIL_R5_BATCH_CONTRACT_SHA256=self.pin,
                           TABMAIL_R5_PROTOCOL_COMPONENT_EVIDENCE=str(self.output/'http-pg-components'),
                           TABMAIL_R5_PROTOCOL_OBSERVATIONS=str(self.output/'observations.json'))
                if self.contract['mode']=='probe':
                    groups = [dict(package='./internal/api/handlers',tag='r5protocol',tests=[PROBE])]
                else:
                    groups = self.contract['required']['groups']
                for index,group in enumerate(groups):
                    tests = [GO_NEW if name==GO_OLD else name for name in group['tests']]
                    command = self.contract['argv']['go_commands'][index]
                    result = self.run_process(command,self.source,env)
                    (self.output/f'go-{index}.jsonl').write_bytes(result['stdout'])
                    (self.output/f'go-{index}.stderr').write_bytes(result['stderr'])
                    if result['timeout'] or result['tail']:
                        errors.append('Go owner timeout/tail')
                    events = [json.loads(line) for line in result['stdout'].decode().splitlines() if line.strip()]
                    # Versioned mapping changes only the dedicated new parent name.
                    events=[event for event in events if event.get('Test')!=GO_NEW+'/owners']
                    for event in events:
                        if event.get('Test','').split('/')[0]==GO_NEW:
                            event['Test']=GO_OLD+event['Test'][len(GO_NEW):].removeprefix('/owners')
                    expected = group['tests']
                    text = '\n'.join(json.dumps(event) for event in events)
                    if self.contract['mode']=='probe':
                        checked=protocol.classify_events(text,'tabmail/internal/api/handlers',[PROBE],result['exit_code'])
                    else:
                        data=protocol.load_cases(self.source/CATALOG,self.source)
                        checked=protocol.classify_shared_events(text,'tabmail/'+group['package'][2:],expected,result['exit_code'],data)
                    if checked['errors'] or result['exit_code']!=0:
                        errors.append('Go required terminal/assertion failure')
                    terminals.extend(event['Test'] for event in events if event.get('Action')=='pass' and event.get('Test'))
                    if (PROBE in tests or GO_NEW in tests) and self.ack is None:
                        self.cancelled.set()
                        with self.guard:
                            for child in self.active.values():child.kill()
                        raise ValueError('Go owner exited without physical fixture acknowledgement')
                if self.contract['mode']=='components':
                    selected={(g['package'],g['tag']):set(g['tests']) for g in groups}
                    packets,_=protocol.classify_go_component_packets(data,selected,self.output,self.contract['catalog_sha256'])
                    if len(packets)!=len(self.children) or any(packet['errors'] for packet in packets):
                        errors.append('missing/invalid real Go-owned observation packets')
                report=self.output/'python-vitest.json'
                command=[str(report) if arg=='<fresh-report>' else arg.replace('--outputFile=<fresh-report>','--outputFile='+str(report)) for arg in self.contract['argv']['python']]
                result=self.run_process(command,Path(self.manifest['execution']['cwd']),self.env)
                (self.output/'python.stdout').write_bytes(result['stdout'])
                (self.output/'python.stderr').write_bytes(result['stderr'])
                raw=report.read_bytes()
                expected_python=self.contract['required']['python_assertions'] if self.contract['mode']=='components' else ['R5 external runtime real TSX CJS ESM worker jsdom']
                if result['timeout'] or result['tail'] or not exact_assertions(json.loads(raw),expected_python,result['exit_code']):
                    errors.append('Python required terminal/assertion failure')
                else:terminals.extend(expected_python)

            except Exception as error:
                errors.append('execution rejected: '+type(error).__name__)
            finally:
                self.cancelled.set()
                with self.guard:
                    self.closed=True
                    for child in self.active.values():child.kill()
                server.shutdown()
                serving.join()
                server.server_close() # joins every RPC owner/child before postcheck
                self.socket.unlink()
                if join_adopted_tails():
                    errors.append('unregistered/detached owned descendant tail')
            if self.invalid or self.ack!=sorted(self.children) or self.seen!=set(self.children) or set(self.results)!=set(self.children):
                errors.append('owner acknowledgement/queue incomplete or invalid')
            if not all(r['passed'] and not r['timeout'] and not r['tail'] for r in self.results.values()):
                errors.append('staged child assertion/lifecycle failure')
            required = self.contract['required']['go_paths']+self.contract['required']['python_assertions'] if self.contract['mode']=='components' else [PROBE]+[PROBE+'/owners/probe/'+str(i) for i in range(8)]+['R5 external runtime real TSX CJS ESM worker jsdom']
            if any(terminals.count(name)!=1 for name in required):
                errors.append('missing/duplicate/nonpassing required terminal')
            try:
                if self.ack!=sorted(self.children):
                    raise ValueError('fixture owners did not acknowledge complete cleanup')
                validate_contract(self.contract)
                after=True
            except Exception as error:
                errors.append('terminal inventory rejected: '+type(error).__name__)
            try:
                if runtime.preparation.identity(token.lstat())!=token_identity:
                    raise ValueError('lease token identity drift')
                with runtime.descriptors() as token_files:
                    if runtime.source_inventory.strict_json(token_files.file(token))!=dict(pid=os.getpid(),nonce=self.nonce):
                        raise ValueError('lease token capability drift')
            except Exception as error:
                errors.append('lease token validation rejected: '+type(error).__name__)
            if time.monotonic()>=self.deadline:
                errors.append('batch process180 deadline exceeded')
            receipt=dict(schema_version=1,policy=POLICY,status='BATCH_QUALIFIED' if not errors else 'BATCH_REJECTED',
                         contract_sha256=runtime.digest(runtime.canonical(self.contract)),preflight=before,terminal_postcheck=after,
                         owners_joined=self.ack==sorted(self.children),max_live_children=self.max_active,
                         eligibility_scope='component_adapter_terminals' if self.contract['mode']=='components' else 'infrastructure_probe_only',
                         required_terminal_count=len(required),terminal_count=sum(terminals.count(n)==1 for n in required),
                         errors=errors,product_green=False,task_complete=False,
                         concurrency_boundary=runtime.BOUNDARY,
                         children={key:dict(report_sha256=value['report_sha256'],exit_code=value['exit_code'],
                                            timeout=value['timeout'],tail=value['tail'],qualified=not errors) for key,value in sorted(self.results.items())})
            pending=self.output/'receipt.pending'
            pending.write_bytes(runtime.canonical(receipt));os.chmod(pending,0o600)
            pending.replace(self.output/'receipt.json')
            self.published=True
        return receipt


def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['capture','run'])
    parser.add_argument('--source',type=Path)
    parser.add_argument('--go',type=Path)
    parser.add_argument('--mode',choices=['components','probe'],default='components')
    parser.add_argument('--contract',type=Path)
    parser.add_argument('--pin')
    parser.add_argument('--output',type=Path)
    args=parser.parse_args()
    if args.action=='capture':
        manifest=runtime.from_environment(args.source)
        validate_contract_candidate= capture(manifest,args.go,args.mode)
        validate_contract(validate_contract_candidate)
        print(runtime.canonical(validate_contract_candidate).decode())
    else:
        contract=load(args.contract,args.pin)
        batch=Batch(contract,args.output)
        def cancel(signum,frame):batch.cancelled.set()
        for sig in (signal.SIGINT,signal.SIGTERM):signal.signal(sig,cancel)
        receipt=batch.run()
        print(runtime.canonical(receipt).decode())
        return 0 if receipt['status']=='BATCH_QUALIFIED' else 1
    return 0

if __name__=='__main__':
    try:sys.exit(main())
    except (OSError,ValueError,KeyError) as error:
        print('batch rejected: '+type(error).__name__,file=sys.stderr);sys.exit(1)
