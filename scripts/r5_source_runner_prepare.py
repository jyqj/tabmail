"""Minimal source-runner preparation; never installs npm or alters Go policy.

Only the original locked official TypeScript archive is admitted. Every archive
member is checked before writing; no lifecycle, .bin, symlink or native/Go input
is admitted. Typed wire evidence is freshly compiled/run in the same checkout.
"""
from __future__ import annotations
import base64
import hashlib
import io
import json
import os
from pathlib import Path, PurePosixPath
import subprocess
import stat
import tarfile
import urllib.request
import uuid
import r5_go_environment
import r5_selected_binding_consumer as consumer

LOCK_SHA256 = 'b839b59e9aa06133819adca60659e0f807ca1e321fbdc35fe55afe1c7b52eba3'
TS_URL = 'https://registry.npmjs.org/typescript/-/typescript-5.9.3.tgz'
TS_SHA256 = '10e108c9cf7d5f2879053dff18515fb405abf2ccef63eaaf017d9c571687a1d3'
TS_INTEGRITY = 'sha512-jl1vZzPDinLr9eUt3J/t7V6FgNEw9QjvBPdysz9KfQDD41fQrC2Y4vKQdiaUpFT4bXlb1RHhLpp8wtm6M5TgSw=='
TEST_NAME = 'TestOrdinaryReceiptOpenAPIWireFixtures'


def digest(data):
    return hashlib.sha256(data).hexdigest()


def exclusive(path, data):
    with Path(path).open('xb') as file:
        os.chmod(path, 0o600)
        file.write(data)


def source_identity(root):
    sha = subprocess.check_output(['git', '-C', str(root), 'rev-parse', 'HEAD'], text=True).strip()
    if subprocess.check_output(['git', '-C', str(root), 'diff', 'HEAD', '--']):
        raise ValueError('tracked source differs from HEAD')
    return sha


def typescript_members(archive):
    if digest(archive) != TS_SHA256 or 'sha512-' + base64.b64encode(hashlib.sha512(archive).digest()).decode() != TS_INTEGRITY:
        raise ValueError('TypeScript archive hash/integrity mismatch')
    files = {}
    with tarfile.open(fileobj=io.BytesIO(archive), mode='r:gz') as tar:
        for member in tar.getmembers():
            path = PurePosixPath(member.name)
            if (not member.isfile() or path.is_absolute() or '..' in path.parts or
                    str(path) != member.name or path.parts[0] != 'package' or len(path.parts) < 2):
                raise ValueError('unsafe TypeScript archive member')
            name = path.relative_to('package').as_posix()
            if (name in files or '.bin' in path.parts or
                    (path.suffix not in {'.js', '.ts', '.json', '.md', '.txt'} and name not in {'bin/tsc', 'bin/tsserver'})):
                raise ValueError('unknown/duplicate TypeScript content')
            files[name] = tar.extractfile(member).read()
    package = json.loads(files['package.json'])
    if (len(files) != 132 or package['name'] != 'typescript' or package['version'] != '5.9.3' or
            package.get('dependencies') or package.get('optionalDependencies') or
            any(k in package.get('scripts', {}) for k in ('preinstall', 'install', 'postinstall', 'prepare'))):
        raise ValueError('unknown TypeScript dependency/lifecycle/content')
    return files


def prepare_typescript(root, archive=None):
    root = Path(root)
    lock = (root / 'web/package-lock.json').read_bytes()
    if digest(lock) != LOCK_SHA256:
        raise ValueError('original complete lock hash required')
    package = json.loads(lock)['packages']['node_modules/typescript']
    if (package['version'], package['resolved'], package['integrity']) != ('5.9.3', TS_URL, TS_INTEGRITY):
        raise ValueError('unknown TypeScript lock entry')
    target = root / 'web/node_modules'
    if target.exists() or target.is_symlink():
        raise ValueError('TypeScript preparation requires absent node_modules')
    if archive is None:
        with urllib.request.urlopen(TS_URL, timeout=60) as response:
            if response.geturl() != TS_URL:
                raise ValueError('TypeScript download redirect refused')
            archive = response.read(8 * 1024 * 1024 + 1)
    files = typescript_members(archive)  # Validate the entire archive before mkdir.
    target.mkdir(mode=0o700)
    destination = target / 'typescript'
    destination.mkdir(mode=0o700)
    for name, data in sorted(files.items()):
        path = destination / name
        path.parent.mkdir(mode=0o700, parents=True, exist_ok=True)
        exclusive(path, data)
    verify_typescript(root, files)
    return dict(lock_sha256=LOCK_SHA256, archive_sha256=TS_SHA256, integrity=TS_INTEGRITY,
                files={name: digest(data) for name, data in sorted(files.items())})


def verify_typescript(root, files):
    target = Path(root) / 'web/node_modules'
    expected = {'typescript/' + name: digest(data) for name, data in files.items()}
    observed = {}
    for path in target.rglob('*'):
        if path.is_symlink() or not (path.is_file() or path.is_dir()):
            raise ValueError('unsafe prepared TypeScript tree')
        if path.is_file():
            observed[path.relative_to(target).as_posix()] = digest(path.read_bytes())
        elif not any(name.startswith(path.relative_to(target).as_posix() + '/') for name in expected):
            raise ValueError('extra prepared TypeScript directory')
    if target.is_symlink() or observed != expected:
        raise ValueError('extra/missing/tampered prepared TypeScript content')


def go_selection(root, go, env, output, phase):
    import r5_selected_source_binding_v2 as binding
    import r5_source_inventory as inventory
    result = {}
    cache = env.get('GOCACHE') or subprocess.check_output([go,'env','GOCACHE'],env=env,text=True).strip()
    modulecache = env.get('GOMODCACHE') or subprocess.check_output([go,'env','GOMODCACHE'],env=env,text=True).strip()
    for name, context in (('default', binding.DEFAULT_CONTEXT), ('race-r5protocol', binding.CONTEXT)):
        receipt = binding.capture(root, Path(go), cache=Path(cache), modulecache=Path(modulecache), context=context)
        exclusive(output / (phase + '-' + name + '-selection.json'), (json.dumps(receipt,indent=2)+'\n').encode())
        # Exactly the established binding.validate identity domain: hydration
        # and diagnostic stderr are retained evidence, not selection identity.
        identity = {k:v for k,v in receipt.items() if k not in ('attestation_sha256','hydration_diagnostics')}
        identity['commands'] = [{k:v for k,v in command.items() if k != 'stderr'} for command in receipt['commands']]
        result[name] = dict(identity_sha256=digest(inventory.canonical(identity)),
                            selected_local=receipt['selected_local'], root_mvs=receipt['root_mvs'],
                            package_records=receipt['package_records'], production_coverage=receipt['production_coverage'])
    return result


def read_regular(path):
    """Read retained evidence through a held regular, non-following descriptor."""
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        initial = os.fstat(fd)
        if not stat.S_ISREG(initial.st_mode):
            raise ValueError('regular preparation evidence required')
        with os.fdopen(os.dup(fd), 'rb') as file:
            raw = file.read()
        final, current = os.fstat(fd), Path(path).lstat()
        identity = lambda row: (row.st_dev, row.st_ino, row.st_size, row.st_mtime_ns, row.st_ctime_ns)
        if identity(initial) != identity(final) or identity(initial) != identity(current):
            raise ValueError('preparation evidence changed while reading')
        return raw
    finally:
        os.close(fd)


def capture_owned_v3(root, go, env, output, phase, observations):
    """Pins originate only from these successful capture returns, never files."""
    helper = consumer.selected_helper(3)
    cache = env.get('GOCACHE') or subprocess.check_output([go, 'env', 'GOCACHE'], env=env, text=True).strip()
    modulecache = env.get('GOMODCACHE') or subprocess.check_output([go, 'env', 'GOMODCACHE'], env=env, text=True).strip()
    if any(output.is_relative_to(Path(tree).resolve()) for tree in (root, cache, modulecache)):
        raise ValueError('v3 evidence must be outside source and dependency/cache trees')
    for name, context in (('default', helper.DEFAULT_CONTEXT), ('race-r5protocol', helper.CONTEXT)):
        receipt = helper.capture(root, Path(go), cache=Path(cache), modulecache=Path(modulecache), context=context)
        observation_pin = helper.observation_digest(receipt)
        helper._verify_receipt(receipt, observation_pin)
        raw = (json.dumps(receipt, indent=2, allow_nan=False) + '\n').encode()
        path = output / (phase + '-' + name + '-selection.json')
        pin = dict(receipt_path=str(path), receipt_byte_sha256=digest(raw),
                   observation_sha256=observation_pin, context=dict(context))
        exclusive(path, raw)
        if read_regular(path) != raw:
            raise ValueError('owned capture return persistence mismatch')
        exclusive(output / (phase + '-' + name + '-observation-pin.json'),
                  (json.dumps(pin, indent=2) + '\n').encode())
        observations[phase + '-' + name] = pin


def verify_v3_selection(bundle_path, bundle_hash, *, root, sha, run_id, producer, observations):
    bundle_raw = read_regular(bundle_path)
    for slot, pin in observations.items():
        pin_path = Path(bundle_path).parent / (slot + '-observation-pin.json')
        if consumer._decode(read_regular(pin_path)) != pin:
            raise ValueError('separately persisted observation pin mismatch')
        if pin['receipt_path'] != str(Path(bundle_path).parent / (slot + '-selection.json')):
            raise ValueError('observation slot path mismatch')
    receipts = {slot: read_regular(pin['receipt_path']) for slot, pin in observations.items()}
    verified = consumer.verify_bundle(bundle_raw, receipts, trusted_bundle_byte_sha256=bundle_hash,
        run_id=run_id, source_commit=sha, source_root=str(root), producer=producer,
        allowed_slots=consumer.PREPARATION_SLOTS, selected_binding_version=3)
    bundle = consumer._decode(bundle_raw)
    if bundle['observations'] != observations:
        raise ValueError('preparation observation references mismatch')
    payloads = consumer.binding_payloads(verified, selected_binding_version=3)
    for name in ('default', 'race-r5protocol'):
        if payloads['before-' + name] != payloads['after-' + name]:
            raise ValueError('TypeScript preparation changed complete Go binding payload')
    return verified, payloads


def execute_binary(root, go, binary, expected_hash, source_sha, fixture, env):
    """Execute the pinned inode, even if its pathname is replaced midflight.

    The selected-source policy already requires Linux. test2json opens the
    parent's live proc FD path, so it cannot execute a substituted pathname.
    The FD stays open until completion; both inode bytes and the original path
    are checked again before any execution evidence is admitted.
    """
    if source_identity(root) != source_sha:
        raise ValueError('typed binary wrong source before execution')
    if fixture.exists() or fixture.is_symlink():
        raise ValueError('stale typed wire fixture before execution')
    fd = os.open(binary, os.O_RDONLY | os.O_NOFOLLOW)
    try:
        identity = os.fstat(fd)
        def pinned_hash():
            with os.fdopen(os.dup(fd), 'rb') as file:
                file.seek(0)
                return hashlib.file_digest(file, 'sha256').hexdigest()
        if not stat.S_ISREG(identity.st_mode) or pinned_hash() != expected_hash:
            raise ValueError('typed binary replaced/tampered before execution')
        executable = f'/proc/{os.getpid()}/fd/{fd}'
        if not Path(executable).exists():
            raise ValueError('Linux pinned executable FD unavailable')
        argv = [go, 'tool', 'test2json', '-t', '-p', 'tabmail/internal/api/handlers', executable,
                '-test.v=test2json', '-test.run=^' + TEST_NAME + '$', '-test.count=1']
        cwd = Path(root) / 'internal/api/handlers'
        run = subprocess.run(argv, cwd=cwd, env={**env, 'ORDINARY_RECEIPT_WIRE_FIXTURE': str(fixture)}, capture_output=True)
        current = binary.lstat()
        if ((current.st_dev, current.st_ino) != (identity.st_dev, identity.st_ino) or
                pinned_hash() != expected_hash or source_identity(root) != source_sha):
            raise ValueError('typed binary/source replaced/tampered during execution')
        return run, dict(run_argv=argv, run_cwd=str(cwd), executed_binary_source_path=str(binary),
                         executed_binary_sha256=expected_hash, execution_binding='linux_parent_proc_fd_pinned_inode')
    finally:
        os.close(fd)


def prepare(root, output, go, *, selected_binding_version=2):
    selected_binding_version = consumer.selected_version(selected_binding_version)
    if selected_binding_version == 3:
        # Reject PATH selection before the legacy environment resolver runs.
        consumer.selected_helper(3)._absolute_producer_path(go)
    root, output = Path(root).resolve(), Path(output).resolve()
    if selected_binding_version == 3 and output.is_relative_to(root):
        raise ValueError('v3 evidence must be outside source tree')
    output.mkdir(mode=0o700)  # New unique runner-owned directory only.
    sha = source_identity(root)
    go, env = r5_go_environment.selected({**os.environ, 'R5_TEST_GO': str(go)})
    version = subprocess.check_output([go, 'env', 'GOVERSION'], env=env, text=True).strip()
    if version != 'go1.25.7':
        raise ValueError('Go1.25.7 required for source-runner preparation')
    if selected_binding_version == 2:
        before = go_selection(root, go, env, output, 'before')
        typescript = prepare_typescript(root)
        after = go_selection(root, go, env, output, 'after')
        if before != after:
            raise ValueError('TypeScript preparation changed Go selection')
    else:
        run_id = uuid.uuid4().hex
        observations = {}
        capture_owned_v3(root, go, env, output, 'before', observations)
        typescript = prepare_typescript(root)
        capture_owned_v3(root, go, env, output, 'after', observations)
        producer = dict(path=go, sha256=consumer.selected_helper(3).GO_SHA256, version=version)
        bundle = dict(schema_version=1, policy=consumer.PIN_POLICY, run_id=run_id,
            source_commit=sha, source_root=str(root), selected_binding_version=3,
            producer=producer, observations=observations)
        bundle_raw = (json.dumps(bundle, indent=2, allow_nan=False) + '\n').encode()
        bundle_hash = digest(bundle_raw)  # Controller memory, computed before publication.
        bundle_path = output / 'selected-observation-pins.json'
        exclusive(bundle_path, bundle_raw)
        verified, payloads = verify_v3_selection(bundle_path, bundle_hash, root=root, sha=sha,
            run_id=run_id, producer=producer, observations=observations)
        before = {name: payloads['before-' + name] for name in ('default', 'race-r5protocol')}
        after = {name: payloads['after-' + name] for name in ('default', 'race-r5protocol')}
    # Compile/run under exactly the checked selection environment, preserving
    # only platform proxy transport settings in addition to that environment.
    env = (json.loads((output / 'after-default-selection.json').read_bytes())['environment']
           if selected_binding_version == 2 else dict(verified['after-default']['environment']))
    env.update({k:os.environ[k] for k in ('HTTPS_PROXY','HTTP_PROXY','ALL_PROXY','NO_PROXY') if k in os.environ})
    binary = output / 'ordinary-receipt.test'
    build_argv = [go, 'test', '-c', '-o', str(binary), './internal/api/handlers']
    build = subprocess.run(build_argv, cwd=root, env=env, capture_output=True, check=True)
    fixture = output / 'ordinary-receipt-wire.json'
    build_hash = digest(binary.read_bytes())
    run, execution = execute_binary(root, go, binary, build_hash, sha, fixture, env)
    exclusive(output / 'build.stdout', build.stdout)
    exclusive(output / 'build.stderr', build.stderr)
    exclusive(output / 'run.stdout', run.stdout)
    exclusive(output / 'run.stderr', run.stderr)
    events = [json.loads(line) for line in run.stdout.splitlines()]
    if (run.returncode or [e.get('Test') for e in events if e.get('Action') == 'run'] != [TEST_NAME] or
            [e.get('Test') for e in events if e.get('Action') == 'pass' and 'Test' in e] != [TEST_NAME]):
        raise ValueError('fresh typed wire test did not actually run and pass')
    if source_identity(root) != sha or not fixture.is_file() or fixture.is_symlink():
        raise ValueError('fresh typed wire source/output mismatch')
    if selected_binding_version == 2:
        run_id = uuid.uuid4().hex
    else:
        # Recheck retained selection after build/run before publishing admission.
        verify_v3_selection(bundle_path, bundle_hash, root=root, sha=sha, run_id=run_id,
                            producer=producer, observations=observations)
    receipt = dict(source_sha=sha, run_id=run_id, go_version=version, typescript=typescript, go_selection_before=before, go_selection_after=after,
                   go_selection_unchanged=True, fixture=str(fixture), fixture_sha256=digest(fixture.read_bytes()),
                   build_argv=build_argv, build_sha256=build_hash,
                   build_stdout_sha256=digest(build.stdout), build_stderr_sha256=digest(build.stderr),
                   **execution, run_stdout_sha256=digest(run.stdout), run_stderr_sha256=digest(run.stderr),
                   actual_started_test_ids=[TEST_NAME], actual_passed_test_ids=[TEST_NAME], exit=run.returncode)
    if selected_binding_version == 3:
        receipt.update(schema_version=3, policy='r5_source_preparation_v3', selected_binding_version=3,
            source_root=str(root), selected_observation_bundle=str(bundle_path),
            selected_observation_bundle_sha256=bundle_hash, selected_producer=producer,
            selected_observations=observations)
    receipt['run_sha256'] = digest(json.dumps(receipt, sort_keys=True).encode())
    path = output / 'preparation.json'
    exclusive(path, (json.dumps(receipt, indent=2) + '\n').encode())
    return receipt, {**({'R5_SELECTED_BINDING_VERSION': '3',
        'R5_SELECTED_OBSERVATION_BUNDLE': str(bundle_path),
        'R5_SELECTED_OBSERVATION_BUNDLE_SHA256': bundle_hash} if selected_binding_version == 3 else {}), 'ORDINARY_RECEIPT_WIRE_FIXTURE': str(fixture), 'R5_SOURCE_PREPARATION': str(path),
                     'R5_SOURCE_PREPARATION_SHA256': digest(path.read_bytes()), 'R5_SOURCE_RUN_ID': run_id}


_DEFAULT_VERSION = object()


def validate_wire(root, fixture, receipt_path, receipt_hash, run_id, *, selected_binding_version=_DEFAULT_VERSION):
    """Bind complete wire bytes to the current source and fresh build/run evidence."""
    if selected_binding_version is _DEFAULT_VERSION:
        selector = os.environ.get('R5_SELECTED_BINDING_VERSION', '2')
        if selector not in ('2', '3'):
            raise ValueError('invalid selected binding environment selector')
        selected_binding_version = int(selector)
    selected_binding_version = consumer.selected_version(selected_binding_version)
    path = Path(receipt_path)
    raw = path.read_bytes() if selected_binding_version == 2 else read_regular(path)
    if path.is_symlink() or digest(raw) != receipt_hash:
        raise ValueError('typed wire preparation receipt tampered')
    receipt = json.loads(raw) if selected_binding_version == 2 else consumer._decode(raw)
    if selected_binding_version == 3:
        consumer._shape(receipt, 'source_sha run_id go_version typescript go_selection_before '
            'go_selection_after go_selection_unchanged fixture fixture_sha256 build_argv build_sha256 '
            'build_stdout_sha256 build_stderr_sha256 run_argv run_cwd executed_binary_source_path '
            'executed_binary_sha256 execution_binding run_stdout_sha256 run_stderr_sha256 '
            'actual_started_test_ids actual_passed_test_ids exit schema_version policy selected_binding_version '
            'source_root selected_observation_bundle selected_observation_bundle_sha256 '
            'selected_producer selected_observations run_sha256'.split(), 'v3 preparation')
        if (type(receipt.get('schema_version')) is not int or receipt['schema_version'] != 3 or
                receipt.get('policy') != 'r5_source_preparation_v3' or
                type(receipt.get('selected_binding_version')) is not int or receipt['selected_binding_version'] != 3 or
                receipt.get('source_root') != str(Path(root).resolve()) or
                receipt.get('go_version') != 'go1.25.7' or
                receipt.get('go_selection_unchanged') is not True or
                type(receipt.get('exit')) is not int):
            raise ValueError('explicit v3 preparation required')
        _, payloads = verify_v3_selection(receipt['selected_observation_bundle'],
            receipt['selected_observation_bundle_sha256'], root=Path(root).resolve(),
            sha=receipt['source_sha'], run_id=run_id, producer=receipt['selected_producer'],
            observations=receipt['selected_observations'])
        for phase in ('before', 'after'):
            expected = {name: payloads[phase + '-' + name] for name in ('default', 'race-r5protocol')}
            if receipt['go_selection_' + phase] != expected:
                raise ValueError('complete preparation binding payload mismatch')
    elif any(key in receipt for key in ('schema_version', 'policy', 'selected_binding_version',
            'selected_observation_bundle', 'selected_observations')):
        raise ValueError('v2 preparation rejects mixed selected policy')
    fixture = Path(fixture)
    if (receipt['run_id'] != run_id or receipt['source_sha'] != source_identity(root) or str(fixture.resolve()) != receipt['fixture'] or
            fixture.is_symlink() or digest(fixture.read_bytes()) != receipt['fixture_sha256']):
        raise ValueError('typed wire wrong source/stale/tampered fixture')
    for name, key in (('ordinary-receipt.test', 'build_sha256'), ('run.stdout', 'run_stdout_sha256'),
                      ('run.stderr', 'run_stderr_sha256'), ('build.stdout', 'build_stdout_sha256'),
                      ('build.stderr', 'build_stderr_sha256')):
        artifact = path.parent / name
        if artifact.is_symlink() or digest(artifact.read_bytes()) != receipt[key]:
            raise ValueError('typed wire build/run evidence tampered')
    binary = path.parent / 'ordinary-receipt.test'
    if (receipt['executed_binary_sha256'] != receipt['build_sha256'] or
            receipt['executed_binary_source_path'] != str(binary) or
            receipt['execution_binding'] != 'linux_parent_proc_fd_pinned_inode' or
            receipt['run_cwd'] != str(Path(root) / 'internal/api/handlers') or
            receipt['run_argv'][1:6] != ['tool','test2json','-t','-p','tabmail/internal/api/handlers'] or
            receipt['run_argv'][7:] != ['-test.v=test2json','-test.run=^' + TEST_NAME + '$','-test.count=1'] or
            not receipt['run_argv'][6].startswith('/proc/')):
        raise ValueError('typed wire executed binary identity mismatch')
    run_hash = receipt.pop('run_sha256')
    if (digest(json.dumps(receipt, sort_keys=True).encode()) != run_hash or receipt['exit'] != 0 or
            receipt['actual_started_test_ids'] != [TEST_NAME] or receipt['actual_passed_test_ids'] != [TEST_NAME]):
        raise ValueError('typed wire run identity mismatch')
    return json.loads(fixture.read_bytes())
