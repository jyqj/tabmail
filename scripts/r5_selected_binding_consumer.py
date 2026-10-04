"""Pure, explicitly opted-in selected-v3 consumer boundary.

No capture, publisher, file reader, environment lookup or runtime integration.
The controller must supply the bundle BYTE pin and expected identities through
its owned channel. A pin computed from an arbitrary candidate is not authority.
Receipt paths are labels only: receipt bytes are supplied by slot, never opened.
The private v3._verify_receipt API at base 412f875 is deliberately reused only
after independently checking shape and all twelve command descriptors. Projection
is exclusively v3.binding_payload; retained-command stderr remains bound.
"""
from __future__ import annotations

import hashlib
import importlib
import json
import posixpath
import re

PIN_POLICY = 'r5_selected_observation_pins_v1'
ADMISSION_SLOTS = ('default', 'race-r5protocol')
PREPARATION_SLOTS = ('before-default', 'before-race-r5protocol',
                     'after-default', 'after-race-r5protocol')
_RECEIPT_FIELDS = frozenset('schema_version policy incompatible_with observation_order '
    'base_source archive_static production_coverage environment go_env observation_envelope '
    'v3_source metadata_producer root_mvs selected_local generated_testmain '
    'external_modulecache_inputs native_inputs toolchain_source_inputs selected_local_packages '
    'package_records qualification boundary binding_sha256 observation_sha256'.split())
_BASE_FIELDS = frozenset('schema_version snapshot_root source_identity_kind source_sha '
    'source_closure_sha256 policy purpose build_context replacements module_resolution '
    'inventory_implementation_sha256 boundary excluded_directories embed_inputs files '
    'archive_static archive_boundary version_test_contract'.split())
_COMMAND_FIELDS = frozenset('role argv exit raw_stdout_sha256 raw_stdout_bytes '
    'stderr_sha256 stderr_bytes binding_stdout_sha256 binding_stdout_domain'.split())
_PACKAGE_DOMAIN = 'complete_PackagePublic_stream_except_top_level_Stale_StaleReason'
_ENV_DOMAIN = 'canonical_go_env_with_only_GOGCCFLAGS_temp_prefix_normalized'


def selected_version(selected_binding_version=2):
    """Omission is v2; only exact integer 2/3 is a current-version selector.

    Historical v1 dispatch is outside this adapter and remains unchanged.
    Explicit None/bool/string/float/unknown values never trigger fallback.
    """
    if type(selected_binding_version) is not int or selected_binding_version not in (2, 3):
        raise ValueError('selected binding version must be integer 2 or 3')
    return selected_binding_version


def selected_helper(selected_binding_version=2):
    """Lazy helper selection only; does not execute or wrap producer actions."""
    version = selected_version(selected_binding_version)
    return importlib.import_module('r5_selected_source_binding_v' + str(version))


def _sha(raw):
    return hashlib.sha256(raw).hexdigest()


def _canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate JSON field')
        result[key] = value
    return result


def _number(value):
    raise ValueError('noninteger/nonfinite JSON number')


def _decode(raw):
    if type(raw) is not bytes:
        raise ValueError('caller-provided bytes required')
    try:
        return json.loads(raw.decode('utf-8'), object_pairs_hook=_unique,
                          parse_float=_number, parse_constant=_number)
    except (UnicodeError, RecursionError) as error:
        raise ValueError('malformed JSON bytes') from error


def _shape(value, fields, label):
    if type(value) is not dict or set(value) != set(fields):
        raise ValueError('missing/extra/malformed ' + label + ' fields')


def _digest(value, size=64):
    if type(value) is not str or not re.fullmatch('[0-9a-f]{' + str(size) + '}', value):
        raise ValueError('malformed digest')


def _text(value):
    if type(value) is not str or not value or '\x00' in value:
        raise ValueError('nonempty string required')


def _absolute(value):
    _text(value)
    if not value.startswith('/') or value.startswith('//') or posixpath.normpath(value) != value:
        raise ValueError('canonical absolute path required')


def _relative(value):
    _text(value)
    if value.startswith('/') or '\\' in value or posixpath.normpath(value) != value or value in ('.', '..') or value.startswith('../'):
        raise ValueError('canonical relative path required')


def _equal(actual, expected, label):
    # JSON canonical comparison also separates integer 1 from boolean true.
    if _canonical(actual) != _canonical(expected):
        raise ValueError(label + ' mismatch')


def _hash_map(value):
    if type(value) is not dict:
        raise ValueError('file hash map required')
    for path, digest in value.items():
        _relative(path)
        _digest(digest)


def _receipt(receipt, context, source_root, producer, v3):
    _shape(receipt, _RECEIPT_FIELDS, 'receipt')
    if type(receipt['schema_version']) is not int or receipt['schema_version'] != 3 or receipt['policy'] != v3.POLICY:
        raise ValueError('mixed/incompatible selected receipt version')
    for key in ('incompatible_with', 'observation_order', 'boundary'):
        _text(receipt[key])
    for key in ('binding_sha256', 'observation_sha256'):
        _digest(receipt[key])
    for key in ('archive_static', 'production_coverage', 'environment', 'go_env',
                'v3_source', 'selected_local', 'qualification'):
        if type(receipt[key]) is not dict:
            raise ValueError('malformed receipt ' + key)
    for key in ('root_mvs', 'generated_testmain', 'external_modulecache_inputs',
                'native_inputs', 'toolchain_source_inputs', 'selected_local_packages'):
        if type(receipt[key]) is not list or any(type(row) is not dict for row in receipt[key]):
            raise ValueError('malformed receipt ' + key)
    if type(receipt['package_records']) is not int or receipt['package_records'] < 1:
        raise ValueError('invalid package record count')
    if any(type(value) is not str for key in ('environment', 'go_env') for value in receipt[key].values()):
        raise ValueError('malformed environment metadata')
    _shape(receipt['environment'], 'PATH HOME GODEBUG GOWORK GOENV GOTOOLCHAIN GOFLAGS '
           'GOOS GOARCH CGO_ENABLED GOPROXY GOSUMDB GOPATH GOMODCACHE GOCACHE'.split(), 'environment')
    for key, expected in dict(GODEBUG='asynctimerchan=0', GOWORK='off', GOENV='off',
            GOTOOLCHAIN='local', GOFLAGS='', GOOS=context['goos'], GOARCH=context['goarch'],
            CGO_ENABLED=str(context['cgo_enabled']), GOPROXY='https://proxy.golang.org',
            GOSUMDB='sum.golang.org').items():
        _equal(receipt['environment'][key], expected, 'environment ' + key)
    for key in ('GOPATH', 'GOMODCACHE', 'GOCACHE'):
        _absolute(receipt['environment'][key])
    for key, expected in dict(GOVERSION='go1.25.7', GOOS='linux', GOARCH='amd64',
            GOHOSTOS='linux', GOHOSTARCH='amd64', CGO_ENABLED='1', GOWORK='off', GOENV='',
            GOFLAGS='', GOEXPERIMENT='', GOAMD64='v1', GOTOOLCHAIN='local').items():
        _equal(receipt['go_env'].get(key), expected, 'Go environment ' + key)
    _text(receipt['go_env'].get('GOGCCFLAGS'))
    _equal(receipt['metadata_producer'], dict(version=producer['version'],
        executable_sha256=producer['sha256'],
        qualification='exact_metadata_producer_only_not_compilation_attestation'), 'producer')
    _shape(receipt['v3_source'], v3._V3_FILES, 'v3 source')
    _hash_map(receipt['v3_source'])
    _equal(receipt['v3_source'][v3._V3_FILES[0]], v3._IMPLEMENTATION_SHA256, 'v3 helper')
    _equal(receipt['v3_source'][v3.SCHEMA_PATH], _sha(v3._REGISTRY_BYTES), 'v3 schema')
    base = receipt['base_source']
    _shape(base, _BASE_FIELDS, 'base source')
    if type(base['schema_version']) is not int or base['schema_version'] != v3.inventory.ARCHIVE_SCHEMA_VERSION:
        raise ValueError('incompatible source version')
    _equal(base['policy'], v3.inventory.ARCHIVE_POLICY, 'source policy')
    _equal(base['source_identity_kind'], v3.inventory.ARCHIVE_KIND, 'source kind')
    _equal(base['purpose'], 'selected', 'source purpose')
    _equal(base['snapshot_root'], source_root, 'source root')
    _equal(base['build_context'], context, 'source context')
    _digest(base['source_sha'], 40)
    _digest(base['source_closure_sha256'])
    _digest(base['inventory_implementation_sha256'])
    for key in ('module_resolution', 'embed_inputs', 'version_test_contract'):
        if type(base[key]) is not dict:
            raise ValueError('malformed source ' + key)
    if type(base['replacements']) is not list or any(type(row) is not dict for row in base['replacements']):
        raise ValueError('malformed source replacements')
    if type(base['excluded_directories']) is not list or any(type(d) is not str for d in base['excluded_directories']):
        raise ValueError('malformed excluded directories')
    _text(base['boundary'])
    _hash_map(base['files'])
    _hash_map(base['archive_static'])
    _equal(receipt['archive_static'], base['archive_static'], 'archive static')
    source_payload = {k: v for k, v in base.items() if k not in
        {'schema_version', 'snapshot_root', 'source_identity_kind', 'source_sha', 'source_closure_sha256'}}
    raw = _canonical(source_payload)
    _equal(base['source_sha'], hashlib.sha1(raw).hexdigest(), 'source seal')
    _equal(base['source_closure_sha256'], _sha(raw), 'source closure seal')
    static = base['archive_boundary']
    _shape(static, 'policy registry_sha256 modules archive_static archive_markers production_go '
           'production_variant_directories compile_roots explicit_production_roots budgets'.split(), 'archive boundary')
    _text(static['policy'])
    _digest(static['registry_sha256'])
    _hash_map(static['archive_static'])
    _hash_map(static['archive_markers'])
    _equal(static['archive_static'], base['archive_static'], 'boundary archive static')
    if type(static['modules']) is not list or any(type(m) is not str for m in static['modules']):
        raise ValueError('malformed archive module labels')
    _equal(static['compile_roots'], ['./...'], 'compile roots')
    _equal(static['explicit_production_roots'], ['./cmd/...', './internal/...'], 'explicit roots')
    _equal(static['budgets'], dict(entries=100000, depth=64, bytes=64 * 1024 * 1024), 'archive budgets')
    _hash_map(static['production_go'])
    directories = static['production_variant_directories']
    if type(directories) is not list or not directories or any(type(d) is not str for d in directories):
        raise ValueError('invalid variant directories')
    for directory in directories:
        _relative(directory)
    _equal(directories, sorted({posixpath.dirname(p) for p in static['production_go']}), 'variant directories')
    _equal(receipt['production_coverage'].get('all_variant_directories'), directories, 'coverage directories')
    selection = v3.selection_argv(context, ['./...', 'github.com/jhillyerd/enmime/v2/...', 'github.com/emersion/go-smtp/...'])
    root = v3.selection_argv(context, ['./...'])
    explicit = v3.selection_argv(context, ['./cmd/...', './internal/...'])
    variant = v3.variant_argv(context, static)
    commands = [['env', '-json'], selection, selection, v3.MVS_ARGV, root, explicit,
                variant, selection, v3.MVS_ARGV, root, explicit, variant]
    envelope = receipt['observation_envelope']
    if type(envelope) is not list or len(envelope) != 12:
        raise ValueError('exactly twelve command descriptors required')
    for index, (descriptor, argv) in enumerate(zip(envelope, commands)):
        _shape(descriptor, _COMMAND_FIELDS, 'command')
        _equal(descriptor['argv'], [producer['path'], *argv], 'command argv/order')
        _equal(descriptor['role'], 'unbound_dependency_hydration_not_attested' if index == 1
               else 'attested_observation', 'command role')
        if type(descriptor['exit']) is not int or descriptor['exit'] != 0:
            raise ValueError('successful integer command exit required')
        for key in ('raw_stdout_sha256', 'stderr_sha256', 'binding_stdout_sha256'):
            _digest(descriptor[key])
        for key in ('raw_stdout_bytes', 'stderr_bytes'):
            if type(descriptor[key]) is not int or descriptor[key] < 0:
                raise ValueError('nonnegative integer command length required')
        if descriptor['raw_stdout_bytes'] == 0:
            raise ValueError('empty successful metadata stdout')
        if descriptor['stderr_bytes'] == 0:
            _equal(descriptor['stderr_sha256'], _sha(b''), 'empty stderr hash')
        _equal(descriptor['binding_stdout_domain'], _ENV_DOMAIN if index == 0 else
               'raw_MVS' if index in (3, 8) else _PACKAGE_DOMAIN, 'stdout domain')
    # Capture checks equality of projected package outputs and raw MVS outputs.
    for a, b in ((2, 7), (3, 8), (4, 9), (5, 10), (6, 11)):
        _equal(envelope[a]['binding_stdout_sha256'], envelope[b]['binding_stdout_sha256'], 'repeated metadata')
    for index in (3, 8):
        _equal(envelope[index]['binding_stdout_sha256'], envelope[index]['raw_stdout_sha256'], 'raw MVS binding')
    _equal(envelope[3]['raw_stdout_bytes'], envelope[8]['raw_stdout_bytes'], 'repeated MVS length')


def verify_bundle(bundle_bytes, receipt_bytes, *, trusted_bundle_byte_sha256,
                  run_id, source_commit, source_root, producer, allowed_slots,
                  selected_binding_version=2):
    """Verify and return decoded v3 receipts, keyed by exact approved slots.

    All keyword trust inputs are required and separately controller-supplied.
    No version is inferred from candidate bytes. This v3-only operation rejects
    omitted selection; use selected_helper() for the unchanged v2 default.
    Neither run ID nor Git commit is present in the old receipt schema: they are
    checked in the authenticated bundle, which pins each complete receipt byte
    string. Phases are encoded by the fixed slot names, never inferred from paths.
    No producer action or error-retention wrapper exists in this pure slice.
    """
    if selected_version(selected_binding_version) != 3:
        raise ValueError('explicit selected binding integer version 3 required')
    _digest(trusted_bundle_byte_sha256)
    if type(bundle_bytes) is not bytes or _sha(bundle_bytes) != trusted_bundle_byte_sha256:
        raise ValueError('trusted bundle byte pin mismatch')
    _text(run_id)
    _digest(source_commit, 40)
    _absolute(source_root)
    _shape(producer, ('path', 'sha256', 'version'), 'expected producer')
    _absolute(producer['path'])
    _digest(producer['sha256'])
    if type(allowed_slots) not in (tuple, list) or any(type(s) is not str for s in allowed_slots) or len(set(allowed_slots)) != len(allowed_slots):
        raise ValueError('unique caller-supplied slots required')
    if set(allowed_slots) not in (set(ADMISSION_SLOTS), set(PREPARATION_SLOTS)):
        raise ValueError('unsupported slot set')
    bundle = _decode(bundle_bytes)
    _shape(bundle, 'schema_version policy run_id source_commit source_root selected_binding_version producer observations'.split(), 'bundle')
    if type(bundle['schema_version']) is not int or bundle['schema_version'] != 1 or bundle['policy'] != PIN_POLICY:
        raise ValueError('incompatible pin bundle')
    if selected_version(bundle['selected_binding_version']) != 3:
        raise ValueError('mixed selected versions')
    for key, expected in (('run_id', run_id), ('source_commit', source_commit),
                          ('source_root', source_root), ('producer', producer)):
        _equal(bundle[key], expected, key)
    _shape(bundle['observations'], allowed_slots, 'observation slots')
    _shape(receipt_bytes, allowed_slots, 'receipt byte slots')
    v3 = selected_helper(3)
    _equal(producer['sha256'], v3.GO_SHA256, 'approved producer hash')
    _equal(producer['version'], 'go1.25.7', 'approved producer version')
    receipts = {}
    paths = set()
    for slot in allowed_slots:
        pin = bundle['observations'][slot]
        _shape(pin, ('receipt_path', 'receipt_byte_sha256', 'observation_sha256', 'context'), 'observation pin')
        _absolute(pin['receipt_path'])
        if pin['receipt_path'] in paths:
            raise ValueError('duplicate receipt path')
        paths.add(pin['receipt_path'])
        _digest(pin['receipt_byte_sha256'])
        _digest(pin['observation_sha256'])
        context = v3.DEFAULT_CONTEXT if slot in ('default', 'before-default', 'after-default') else v3.CONTEXT
        _equal(pin['context'], context, 'slot phase/context')
        raw = receipt_bytes[slot]
        if type(raw) is not bytes or _sha(raw) != pin['receipt_byte_sha256']:
            raise ValueError('receipt byte pin mismatch')
        receipt = _decode(raw)
        _receipt(receipt, context, source_root, producer, v3)
        v3._verify_receipt(receipt, pin['observation_sha256'])
        receipts[slot] = receipt
    return receipts


def binding_payloads(receipts, *, selected_binding_version=2):
    """Project already verified receipts using the reviewed helper, without IO.

    Caller controls any cross-slot equality requirement; observations may differ.
    This is projection, not verification or an authority-bearing publication API.
    """
    if selected_version(selected_binding_version) != 3:
        raise ValueError('explicit selected binding integer version 3 required')
    v3 = selected_helper(3)
    return {slot: v3.binding_payload(receipt) for slot, receipt in receipts.items()}
