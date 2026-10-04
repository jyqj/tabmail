"""Additive selected binding v3: full metadata binding and externally pinned observations.

No formal consumer uses this API. The producer executable is pinned; compiler,
native, generated and external module bytes remain unattested.
"""
from __future__ import annotations
import datetime
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import r5_selected_source_binding_v2 as v2
import r5_source_inventory as inventory
import r5_archive_boundary as boundary
from r5_private_diagnostics import Retention

POLICY = 'r5_root_selected_local_archive_attestation_v3'
GO_SHA256 = '76ac600b41ad2eceee5d39d02af78009516f8b2a29f30bcc80f765c3e4b4f5a8'
SCHEMA_PATH = 'scripts/contracts/r5-selected-binding-v3-producer.json'
_REGISTRY_BYTES = (Path(__file__).parent.parent / SCHEMA_PATH).read_bytes()
REGISTRY = inventory.strict_json(_REGISTRY_BYTES)
_IMPLEMENTATION_SHA256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
_V3_FILES = ('scripts/r5_selected_source_binding_v3.py', SCHEMA_PATH,
             'scripts/tests/test_r5_selected_source_binding_v3.py')
CONTEXT, DEFAULT_CONTEXT = v2.CONTEXT, v2.DEFAULT_CONTEXT
ARGV, MVS_ARGV = v2.ARGV, v2.MVS_ARGV
FIELDS, STATIC_EVIDENCE = v2.FIELDS, v2.STATIC_EVIDENCE
selection_argv, variant_argv = v2.selection_argv, v2.variant_argv
classify, normalize_mvs = v2.classify, v2.normalize_mvs
compare_coverage, compare_variants = v2.compare_coverage, v2.compare_variants
digest, topology = v2.digest, v2.topology
MetadataCommandFailure = v2.MetadataCommandFailure


def _nonfinite(value):
    raise ValueError('nonfinite metadata number')


def stream(raw, producer='PackagePublic'):
    """Decode the complete ordered stream and validate every producer field."""
    decoder = json.JSONDecoder(object_pairs_hook=v2._unique, parse_constant=_nonfinite)
    text = raw.decode('utf-8'); rows = []
    while text.strip():
        text = text.lstrip(); row, end = decoder.raw_decode(text)
        _object(row, producer)
        rows.append(row); text = text[end:]
    if not rows:
        raise ValueError('empty metadata')
    return rows


def _object(value, producer):
    if type(value) is not dict:
        raise ValueError('metadata object required: '+producer)
    schema = REGISTRY['types'][producer]
    if set(value) - set(schema):
        raise ValueError('unknown metadata field: '+producer)
    for key, item in value.items():
        kind = schema[key]
        if kind in ('string', 'bool'):
            if type(item) is not (str if kind == 'string' else bool):
                raise ValueError('invalid metadata type: '+producer+'.'+key)
        elif kind in ('[]string', '[]string|null'):
            if kind == '[]string|null' and item is None:
                continue
            if type(item) is not list or any(type(x) is not str for x in item):
                raise ValueError('invalid string array: '+producer+'.'+key)
        elif kind == 'map[string]string':
            if type(item) is not dict or any(type(x) is not str for x in item.values()):
                raise ValueError('invalid string map')
        elif kind == '*time.Time':
            if type(item) is not str or not re.fullmatch(r'\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d{1,9})?(?:Z|[+-]\d{2}:\d{2})', item):
                raise ValueError('invalid module Time')
            try:
                datetime.datetime.fromisoformat(item.replace('Z', '+00:00'))
            except ValueError as error:
                raise ValueError('invalid module Time') from error
        elif kind == '[]*PackageError':
            if type(item) is not list:
                raise ValueError('invalid package error array')
            for error in item:
                _object(error, 'PackageError')
        else:
            nested = {'*modinfo.ModulePublic':'ModulePublic', '*ModulePublic':'ModulePublic',
                      '*codehost.Origin':'Origin', '*PackageError':'PackageError', '*ModuleError':'ModuleError'}.get(kind)
            if nested is None:
                raise ValueError('unregistered producer type')
            _object(item, nested)


def project_package_stdout(raw):
    """Single projection API; only top-level Stale/StaleReason are removed.

    Object key order/JSON whitespace are nonidentity. Stream order, arrays,
    multiplicity, and every other field and presence remain bound.
    """
    rows = stream(raw)
    return inventory.canonical([{k:v for k,v in row.items()
                                 if k not in ('Stale', 'StaleReason')} for row in rows])


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def binding_payload(receipt):
    value = {k:v for k,v in receipt.items()
             if k not in ('binding_sha256', 'observation_sha256', 'observation_envelope')}
    envelope = receipt['observation_envelope']
    value['commands'] = [{k:v for k,v in command.items()
                          if k not in ('raw_stdout_sha256', 'raw_stdout_bytes')}
                         for command in envelope
                         if command['role'] != 'unbound_dependency_hydration_not_attested']
    return value


def observation_digest(receipt):
    """Digest of the complete receipt/envelope; supply via a trusted channel."""
    return _sha(inventory.canonical({k:v for k,v in receipt.items() if k != 'observation_sha256'}))


def _seal(payload):
    payload = dict(payload)
    payload['binding_sha256'] = _sha(inventory.canonical(binding_payload(payload)))
    payload['observation_sha256'] = observation_digest(payload)
    return payload


def _verify_receipt(receipt, trusted_observation_sha256):
    if type(receipt) is not dict or receipt.get('policy') != POLICY or type(receipt.get('schema_version')) is not int or receipt['schema_version'] != 3:
        raise ValueError('explicit incompatible selected v3 attestation required')
    if type(trusted_observation_sha256) is not str or not re.fullmatch('[0-9a-f]{64}', trusted_observation_sha256):
        raise ValueError('independently supplied trusted complete observation digest required')
    if observation_digest(receipt) != trusted_observation_sha256 or receipt.get('observation_sha256') != trusted_observation_sha256:
        raise ValueError('trusted complete observation pin mismatch')
    if receipt.get('binding_sha256') != _sha(inventory.canonical(binding_payload(receipt))):
        raise ValueError('binding digest mismatch')


def capture(root, go, *, cache, modulecache, context=None):
    with Retention('selection-v3') as diagnostics:
        return _capture(root, go, cache=cache, modulecache=modulecache, context=context, diagnostics=diagnostics)


def _capture(root, go, *, cache, modulecache, context, diagnostics):
    context = CONTEXT if context is None else context
    inventory.validate_context(context, "selected")
    if context not in (CONTEXT, DEFAULT_CONTEXT):
        raise ValueError("unsupported selected command context")
    argv = selection_argv(context, ["./...", "github.com/jhillyerd/enmime/v2/...", "github.com/emersion/go-smtp/..."])
    root = Path(root).absolute()
    if any(p.is_symlink() for p in [root,*root.parents]):
        raise ValueError('symlinked source root')
    root = root.resolve()
    base = inventory.capture_current_source(root,purpose='selected',policy=inventory.ARCHIVE_POLICY,build_context=context)
    v3_before = digest(root, _V3_FILES)
    if v3_before[_V3_FILES[0]] != _IMPLEMENTATION_SHA256 or v3_before[SCHEMA_PATH] != _sha(_REGISTRY_BYTES):
        raise ValueError('v3 implementation/schema differs from source')
    producer_before = _sha(Path(go).read_bytes())
    if producer_before != GO_SHA256:
        raise ValueError('unapproved metadata producer executable')
    authorized = set(base['files'])
    archive_static = base['archive_static']
    before_go = topology(root)
    if not before_go.issubset(authorized):
        raise ValueError('unclassified repository Go source')
    before = digest(root, authorized)
    if before.get('scripts/r5_selected_source_binding_v2.py')!=v2._IMPLEMENTATION_SHA256:
        raise ValueError('selected binding helper differs from source')
    # Clean environment: never inherit private service settings or ABI/flags.
    env = dict(PATH=os.environ.get('PATH','/usr/bin:/bin'), HOME=os.environ.get('HOME','/tmp'),
               GODEBUG='asynctimerchan=0', GOWORK='off', GOENV='off', GOTOOLCHAIN='local',
               GOFLAGS='', GOOS=context['goos'], GOARCH=context['goarch'], CGO_ENABLED=str(context['cgo_enabled']),
               GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org', GOPATH=str(Path(modulecache).parent),
               GOMODCACHE=str(Path(modulecache).resolve()), GOCACHE=str(Path(cache).resolve()))
    commands = []
    def run(argv, *, role="attested_observation"):
        try:
            result = subprocess.run([str(go),*argv],cwd=root,env={**env,**{k:os.environ[k] for k in ('HTTPS_PROXY','HTTP_PROXY','ALL_PROXY','NO_PROXY') if k in os.environ}},capture_output=True,timeout=180)
        except subprocess.TimeoutExpired as error:
            diagnostics.failed_command(diagnostics.count, error.stdout, error.stderr, None, error)
            raise
        commands.append(dict(role=role,argv=[str(go),*argv],exit=result.returncode,
                             raw_stdout_sha256=_sha(result.stdout),raw_stdout_bytes=len(result.stdout),
                             stderr_sha256=_sha(result.stderr),stderr_bytes=len(result.stderr)))
        if result.returncode:
            error = MetadataCommandFailure(commands[-1],result.stdout,result.stderr)
            diagnostics.failed_command(diagnostics.count, result.stdout, result.stderr, result.returncode, error)
            raise error
        diagnostics.command(diagnostics.count, result.stdout, result.stderr, result.returncode)
        if argv[0] == 'list' and '-m' not in argv:
            bound = project_package_stdout(result.stdout)
            domain = 'complete_PackagePublic_stream_except_top_level_Stale_StaleReason'
        elif argv[0] == 'env':
            info = inventory.strict_json(result.stdout)
            if type(info) is not dict or any(type(v) is not str for v in info.values()):
                raise ValueError('invalid Go env metadata')
            info['GOGCCFLAGS'] = re.sub(r'/tmp/go-build[0-9]+=', '/tmp/go-build<TEMP>=', info['GOGCCFLAGS'])
            bound = inventory.canonical(info)
            domain = 'canonical_go_env_with_only_GOGCCFLAGS_temp_prefix_normalized'
        else:
            stream(result.stdout, 'ModulePublic')
            bound = result.stdout
            domain = 'raw_MVS'
        commands[-1].update(binding_stdout_sha256=_sha(bound), binding_stdout_domain=domain)
        return result.stdout
    envinfo = json.loads(run(['env','-json']))
    # go env reports GOENV='' when disabled; GOGCCFLAGS has a per-query temp prefix.
    envinfo['GOGCCFLAGS'] = re.sub(r'/tmp/go-build[0-9]+=', '/tmp/go-build<TEMP>=', envinfo['GOGCCFLAGS'])
    required = dict(GOVERSION='go1.25.7', GOOS='linux',GOARCH='amd64',GOHOSTOS='linux',GOHOSTARCH='amd64',CGO_ENABLED='1',GOWORK='off',GOENV='',GOFLAGS='',GOEXPERIMENT='',GOAMD64='v1',GOTOOLCHAIN='local')
    if any(envinfo.get(k)!=v for k,v in required.items()):
        raise ValueError('wrong Go version/flags/ABI')
    # Selection may download test dependencies and hydrate MVS Dir fields.
    # Bind both observations only after that hydration, retaining raw hashes.
    run(argv,role='unbound_dependency_hydration_not_attested')
    # Hydration stays in the complete observation envelope, excluded from binding.
    selected_raw = run(argv)
    rows = stream(selected_raw)
    mvs_raw = run(MVS_ARGV)
    mvs = normalize_mvs(stream(mvs_raw, 'ModulePublic'),root)
    local, generated, external, native, toolchain = classify(rows,root,authorized,envinfo,mvs)
    admitted_embeds = {path for paths in base['embed_inputs'].values() for path in paths}
    if any(set(fields) & set(FIELDS[4:7]) and path not in admitted_embeds for path,fields in local.items()):
        raise ValueError('selected embed outside declared static directives')
    root_raw = run(selection_argv(context, ['./...']))
    explicit_raw = run(selection_argv(context, ['./cmd/...','./internal/...']))
    coverage = compare_coverage(stream(root_raw),stream(explicit_raw),root,base['archive_boundary'],local)
    variant_raw = run(variant_argv(context,base['archive_boundary']))
    coverage['variant_directory_records'] = compare_variants(stream(variant_raw),root,base['archive_boundary'])
    local_packages = [dict(import_path=r['ImportPath'],directory=Path(r['Dir']).relative_to(root).as_posix(),for_test=r.get('ForTest'),module=r['Module']['Path'],fields={f:r[f] for f in FIELDS if f in r}) for r in rows if Path(r.get('Dir','')).is_relative_to(root)]
    fresh = digest(root,local)
    after = inventory.capture_current_source(root,purpose='selected',policy=inventory.ARCHIVE_POLICY,build_context=context)
    if base!=after or before_go!=topology(root) or before!=digest(root,authorized) or any(before[p]!=h for p,h in fresh.items()):
        raise ValueError('source/hash/topology changed during capture')
    repeated_selected_raw = run(argv)
    repeated_mvs_raw = run(MVS_ARGV)
    repeated_root_raw = run(selection_argv(context,['./...']))
    repeated_explicit_raw = run(selection_argv(context,['./cmd/...','./internal/...']))
    repeated_variant_raw = run(variant_argv(context,base['archive_boundary']))
    if mvs_raw != repeated_mvs_raw or any(project_package_stdout(a) != project_package_stdout(b)
        for a,b in ((selected_raw,repeated_selected_raw),(root_raw,repeated_root_raw),
                    (explicit_raw,repeated_explicit_raw),(variant_raw,repeated_variant_raw))):
        raise ValueError('selected metadata changed during capture outside diagnostic domain')
    if producer_before != _sha(Path(go).read_bytes()) or v3_before != digest(root, _V3_FILES):
        raise ValueError('v3 source/schema/metadata producer changed during capture')
    if before!=digest(root,authorized) or before_go!=topology(root):
        raise ValueError('source changed after final metadata')
    payload = dict(schema_version=3,policy=POLICY,incompatible_with='selected v1/v2 receipts never authorize v3; v3 never authorizes v2',
                   observation_order='env; unbound hydration; selected; MVS; coverage; selected; MVS; repeated coverage (projected package equality; raw MVS equality)', base_source=base, archive_static=archive_static, production_coverage=coverage, environment=env,go_env=envinfo,observation_envelope=commands,v3_source=v3_before,metadata_producer=dict(version='go1.25.7',executable_sha256=producer_before,qualification='exact_metadata_producer_only_not_compilation_attestation'),root_mvs=mvs,
                   selected_local={p:dict(sha256=fresh[p],fields=fields) for p,fields in local.items()},
                   generated_testmain=generated,external_modulecache_inputs=external,native_inputs=native,toolchain_source_inputs=toolchain,
                   selected_local_packages=local_packages,package_records=len(rows), qualification=dict(overall='blocked',excluded_metadata_boundary='descriptor_checked',local_static_binding='captured',generated='unknown',external_modulecache='unknown',compiler_native='unknown',Method19='unknown',SML='unknown',wholeCI='unknown'),
                   boundary='Metadata/static local bytes only; no product tests, cold build or runtime qualification')
    return _seal(payload)



def validate(receipt, root, go, *, cache, modulecache, trusted_observation_sha256):
    """Verify external pin before executing; return a separately sealed fresh capture.

    trusted_observation_sha256 MUST come from an independent trusted complete
    capture/envelope channel, never from the receipt under validation.
    """
    _verify_receipt(receipt, trusted_observation_sha256)
    observed = capture(root,go,cache=cache,modulecache=modulecache,
                       context=receipt.get('base_source',{}).get('build_context'))
    if binding_payload(receipt) != binding_payload(observed):
        with Retention('rejected-selection-v3') as diagnostics:
            diagnostics.json('observed.json', observed)
        raise ValueError('selected v3 missing/extra/drifted/context mismatch')
    return observed


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True)
    parser.add_argument('--go',type=Path,required=True)
    parser.add_argument('--cache',type=Path,required=True)
    parser.add_argument('--modulecache',type=Path,required=True)
    parser.add_argument('--validate',type=Path)
    parser.add_argument('--trusted-observation-sha256')
    parser.add_argument('--context',choices=['default','race-r5protocol'],default='race-r5protocol')
    args = parser.parse_args()
    if args.validate:
        result = validate(inventory.strict_json(args.validate.read_bytes()),args.root,args.go,
                          cache=args.cache,modulecache=args.modulecache,
                          trusted_observation_sha256=args.trusted_observation_sha256)
    else:
        if args.trusted_observation_sha256:
            parser.error('trusted observation pin is a validation input only')
        result = capture(args.root,args.go,cache=args.cache,modulecache=args.modulecache,
                         context=DEFAULT_CONTEXT if args.context=='default' else CONTEXT)
    print(inventory.canonical(result).decode())

if __name__ == '__main__':
    main()
