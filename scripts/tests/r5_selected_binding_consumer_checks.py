"""Explicit pure synthetic adapter checks; deliberately outside test_* discovery.

Invoke: PYTHONDONTWRITEBYTECODE=1 python3 scripts/tests/r5_selected_binding_consumer_checks.py
No captures or formal cases. Audit guard is installed before adapter/helper import.
Synthetic pins here model a trusted controller; they do not bless actual receipts.
"""
import sys

PROCESS_EVENTS = []


def process_guard(event, args):
    if event in ('subprocess.Popen', 'os.system', 'os.fork', 'os.forkpty',
                 'os.posix_spawn', 'os.posix_spawnp') or event.startswith(('os.exec', 'os.spawn')):
        PROCESS_EVENTS.append(event)
        raise AssertionError('OS process forbidden in pure adapter checks: ' + event)


sys.addaudithook(process_guard)

import copy
import hashlib
import json
from pathlib import Path
import unittest
from unittest import mock

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_selected_binding_consumer as consumer

# Verify lazy/default behavior before importing fixture-only v3.
assert 'r5_selected_source_binding_v3' not in sys.modules
assert consumer.selected_version() == 2
assert 'r5_selected_source_binding_v3' not in sys.modules
import r5_selected_source_binding_v3 as v3

ROOT = '/controller/source'
COMMIT = '4' * 40
PRODUCER = dict(path='/controller/tools/go', sha256=v3.GO_SHA256, version='go1.25.7')


def wire(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':'), allow_nan=False).encode()


def sha(raw):
    return hashlib.sha256(raw).hexdigest()


def source_seal(base):
    payload = {k: v for k, v in base.items() if k not in
               {'schema_version', 'snapshot_root', 'source_identity_kind', 'source_sha', 'source_closure_sha256'}}
    base['source_sha'] = hashlib.sha1(wire(payload)).hexdigest()
    base['source_closure_sha256'] = sha(wire(payload))


def fixture_receipt(context):
    static = dict(policy='synthetic-static', registry_sha256='a' * 64, modules=[],
                  archive_static={}, archive_markers={}, production_go={'cmd/main.go': 'b' * 64},
                  production_variant_directories=['cmd'], compile_roots=['./...'],
                  explicit_production_roots=['./cmd/...', './internal/...'],
                  budgets=dict(entries=100000, depth=64, bytes=67108864))
    base = dict(schema_version=4, snapshot_root=ROOT, source_identity_kind=v3.inventory.ARCHIVE_KIND,
                policy=v3.inventory.ARCHIVE_POLICY, purpose='selected', build_context=copy.deepcopy(context),
                replacements=[], module_resolution={}, inventory_implementation_sha256='a' * 64,
                boundary='synthetic', excluded_directories=[], embed_inputs={}, files={}, archive_static={},
                archive_boundary=static, version_test_contract={})
    source_seal(base)
    selected = v3.selection_argv(context, ['./...', 'github.com/jhillyerd/enmime/v2/...', 'github.com/emersion/go-smtp/...'])
    root = v3.selection_argv(context, ['./...'])
    explicit = v3.selection_argv(context, ['./cmd/...', './internal/...'])
    variant = v3.variant_argv(context, static)
    envelope = []
    for index, argv in enumerate([['env', '-json'], selected, selected, v3.MVS_ARGV, root,
                                  explicit, variant, selected, v3.MVS_ARGV, root, explicit, variant]):
        envelope.append(dict(role='unbound_dependency_hydration_not_attested' if index == 1 else 'attested_observation',
            argv=[PRODUCER['path'], *argv], exit=0, raw_stdout_sha256='c' * 64, raw_stdout_bytes=10,
            stderr_sha256=sha(b''), stderr_bytes=0, binding_stdout_sha256='c' * 64,
            binding_stdout_domain=consumer._ENV_DOMAIN if index == 0 else 'raw_MVS' if index in (3, 8) else consumer._PACKAGE_DOMAIN))
    return v3._seal(dict(schema_version=3, policy=v3.POLICY, incompatible_with='synthetic v3 only',
        observation_order='synthetic twelve fixed descriptors', base_source=base, archive_static={},
        production_coverage={'all_variant_directories': ['cmd']},
        environment=dict(PATH='/usr/bin:/bin', HOME='/controller', GODEBUG='asynctimerchan=0',
            GOWORK='off', GOENV='off', GOTOOLCHAIN='local', GOFLAGS='', GOOS='linux', GOARCH='amd64',
            CGO_ENABLED='1', GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org',
            GOPATH='/controller/cache', GOMODCACHE='/controller/cache/mod', GOCACHE='/controller/cache/build'),
        go_env=dict(GOVERSION='go1.25.7', GOOS='linux', GOARCH='amd64', GOHOSTOS='linux',
            GOHOSTARCH='amd64', CGO_ENABLED='1', GOWORK='off', GOENV='', GOFLAGS='', GOEXPERIMENT='',
            GOAMD64='v1', GOTOOLCHAIN='local', GOGCCFLAGS='-fdebug-prefix-map=/tmp/go-build<TEMP>='),
        observation_envelope=envelope, v3_source={v3._V3_FILES[0]: v3._IMPLEMENTATION_SHA256,
        v3.SCHEMA_PATH: sha(v3._REGISTRY_BYTES), v3._V3_FILES[2]: 'd' * 64},
        metadata_producer=dict(version=PRODUCER['version'], executable_sha256=PRODUCER['sha256'],
            qualification='exact_metadata_producer_only_not_compilation_attestation'),
        root_mvs=[], selected_local={}, generated_testmain=[], external_modulecache_inputs=[], native_inputs=[],
        toolchain_source_inputs=[], selected_local_packages=[], package_records=1, qualification={}, boundary='synthetic'))


def fixture(slots=consumer.PREPARATION_SLOTS):
    receipts = {s: fixture_receipt(v3.DEFAULT_CONTEXT if s in ('default', 'before-default', 'after-default') else v3.CONTEXT) for s in slots}
    bundle = dict(schema_version=1, policy=consumer.PIN_POLICY, run_id='owned-run', source_commit=COMMIT,
                  source_root=ROOT, selected_binding_version=3, producer=copy.deepcopy(PRODUCER), observations={})
    rebind(bundle, receipts)
    return bundle, receipts


def rebind(bundle, receipts):
    for slot, receipt in receipts.items():
        source_seal(receipt['base_source'])
        try:
            receipt.update(v3._seal(receipt))
        except (KeyError, TypeError):
            # A malformed envelope may not be projectable even by the helper.
            # Still authenticate its bytes to reach the adapter's shape gate.
            receipt['binding_sha256'] = 'f' * 64
            receipt['observation_sha256'] = v3.observation_digest(receipt)
        bundle['observations'][slot] = dict(receipt_path='/controller/evidence/' + slot + '.json',
            receipt_byte_sha256=sha(wire(receipt)), observation_sha256=receipt['observation_sha256'],
            context=copy.deepcopy(receipt['base_source']['build_context']))


def verify(bundle, receipts, **overrides):
    bundle_raw = wire(bundle)
    arguments = dict(trusted_bundle_byte_sha256=sha(bundle_raw), run_id='owned-run', source_commit=COMMIT,
                     source_root=ROOT, producer=copy.deepcopy(PRODUCER), allowed_slots=tuple(receipts), selected_binding_version=3)
    arguments.update(overrides)
    return consumer.verify_bundle(bundle_raw, {s: wire(r) for s, r in receipts.items()}, **arguments)


class VersionChecks(unittest.TestCase):
    def test_omission_is_v2_without_v3_import(self):
        with mock.patch.object(consumer.importlib, 'import_module') as load:
            consumer.selected_helper()
        load.assert_called_once_with('r5_selected_source_binding_v2')

    def test_invalid_versions_never_import(self):
        for value in (None, True, False, 1, 4, -1, 0, 3.0, 2.0, '3', '2', [], {}):
            with self.subTest(value=value), mock.patch.object(consumer.importlib, 'import_module') as load:
                with self.assertRaises(ValueError): consumer.selected_helper(value)
                load.assert_not_called()

    def test_explicit_three_imports_only_v3(self):
        with mock.patch.object(consumer.importlib, 'import_module') as load:
            consumer.selected_helper(3)
        load.assert_called_once_with('r5_selected_source_binding_v3')

    def test_v3_pure_operation_requires_explicit_opt_in(self):
        b, r = fixture()
        arguments = dict(trusted_bundle_byte_sha256=sha(wire(b)), run_id='owned-run', source_commit=COMMIT,
                         source_root=ROOT, producer=PRODUCER, allowed_slots=tuple(r))
        with mock.patch.object(consumer.importlib, 'import_module') as load:
            with self.assertRaises(ValueError): consumer.verify_bundle(wire(b), {s: wire(v) for s, v in r.items()}, **arguments)
            load.assert_not_called()

    def test_all_selector_bundle_receipt_version_pairings(self):
        for selector in (1, 2, 3, True):
            for bundle_version in (1, 2, 3, True):
                for receipt_version in (1, 2, 3, True):
                    b, r = fixture(consumer.ADMISSION_SLOTS)
                    b['selected_binding_version'] = bundle_version
                    for receipt in r.values(): receipt['schema_version'] = receipt_version
                    rebind(b, r)
                    with self.subTest(selector=selector, bundle=bundle_version, receipt=receipt_version):
                        if type(selector) is int and type(bundle_version) is int and type(receipt_version) is int and (selector, bundle_version, receipt_version) == (3, 3, 3):
                            verify(b, r, selected_binding_version=selector)
                        else:
                            with self.assertRaises(ValueError): verify(b, r, selected_binding_version=selector)

    def test_bundle_policy_schema_strict(self):
        for field, value in (('schema_version', True), ('schema_version', 2), ('policy', v3.POLICY), ('selected_binding_version', '3')):
            b, r = fixture(); b[field] = value
            with self.subTest(field=field, value=value), self.assertRaises(ValueError): verify(b, r)


class TrustChecks(unittest.TestCase):
    def test_valid_two_and_four_slot_bundles(self):
        for slots in (consumer.ADMISSION_SLOTS, consumer.PREPARATION_SLOTS):
            b, r = fixture(slots)
            self.assertEqual(verify(b, r), r)

    def test_forged_resealed_replacement_against_original_controller_pin(self):
        for index in range(12):
            for field, replacement in (('raw_stdout_sha256', 'e' * 64), ('raw_stdout_bytes', 99),
                    ('stderr_sha256', 'e' * 64), ('stderr_bytes', 99), ('role', 'other'),
                    ('argv', ['/other', 'list']), ('exit', 7), ('binding_stdout_sha256', 'e' * 64)):
                b, r = fixture(); original_pin = sha(wire(b))
                r['before-default']['observation_envelope'][index][field] = replacement
                rebind(b, r)
                with self.subTest(index=index, field=field), self.assertRaisesRegex(ValueError, 'bundle byte pin mismatch'):
                    verify(b, r, trusted_bundle_byte_sha256=original_pin)

    def test_receipt_bytes_not_canonical_digest(self):
        b, r = fixture()
        raw = {s: wire(v) for s, v in r.items()}
        raw['before-default'] += b'\n'
        with self.assertRaisesRegex(ValueError, 'receipt byte pin mismatch'):
            consumer.verify_bundle(wire(b), raw, trusted_bundle_byte_sha256=sha(wire(b)), run_id='owned-run',
                source_commit=COMMIT, source_root=ROOT, producer=PRODUCER, allowed_slots=tuple(r), selected_binding_version=3)

    def test_wrong_expected_identifiers(self):
        for overrides in ({'run_id': 'different'}, {'source_commit': '5' * 40}, {'source_root': '/other'},
                          {'producer': dict(PRODUCER, path='/other/go')}, {'producer': dict(PRODUCER, sha256='e' * 64)},
                          {'producer': dict(PRODUCER, version='go1.25.8')}):
            b, r = fixture()
            with self.subTest(overrides=overrides), self.assertRaises(ValueError): verify(b, r, **overrides)

    def test_missing_and_malformed_trust_inputs(self):
        for overrides in ({'trusted_bundle_byte_sha256': None}, {'trusted_bundle_byte_sha256': 'a' * 63},
                {'trusted_bundle_byte_sha256': True}, {'run_id': ''}, {'source_commit': 'HEAD'},
                {'source_root': 'relative'}, {'source_root': '/a/../b'},
                {'producer': dict(PRODUCER, path='go')}, {'producer': dict(PRODUCER, extra=True)},
                {'allowed_slots': None}, {'allowed_slots': []}):
            b, r = fixture()
            with self.subTest(overrides=overrides), self.assertRaises(ValueError): verify(b, r, **overrides)
        with self.assertRaises(TypeError): consumer.verify_bundle(b'{}', {})

    def test_missing_extra_duplicate_slots_and_wrong_phase(self):
        for kind in ('missing', 'extra', 'duplicate-input', 'wrong-phase', 'swapped'):
            b, r = fixture()
            kwargs = {}
            if kind == 'missing': del b['observations']['before-default']
            if kind == 'extra': b['observations']['other'] = b['observations']['before-default']
            if kind == 'duplicate-input': kwargs['allowed_slots'] = (*r, 'before-default')
            if kind == 'wrong-phase':
                b['observations']['after-other'] = b['observations'].pop('after-default')
            if kind == 'swapped':
                b['observations']['before-default'], b['observations']['before-race-r5protocol'] = b['observations']['before-race-r5protocol'], b['observations']['before-default']
            with self.subTest(kind=kind), self.assertRaises(ValueError): verify(b, r, **kwargs)

    def test_swapped_phases_with_distinct_pinned_observations(self):
        b, r = fixture()
        r['after-default']['observation_envelope'][2]['raw_stdout_sha256'] = 'f' * 64
        rebind(b, r)
        r['before-default'], r['after-default'] = r['after-default'], r['before-default']
        with self.assertRaisesRegex(ValueError, 'receipt byte pin mismatch'): verify(b, r)

    def test_duplicate_receipt_path_rejected(self):
        b, r = fixture()
        b['observations']['after-default']['receipt_path'] = b['observations']['before-default']['receipt_path']
        with self.assertRaises(ValueError): verify(b, r)

    def test_identical_complete_observations_may_share_digests(self):
        b, r = fixture()
        self.assertEqual(b['observations']['before-default']['observation_sha256'], b['observations']['after-default']['observation_sha256'])
        verify(b, r)

    def test_wrong_inner_observation_pin_or_binding_seal(self):
        for kind in ('observation', 'binding'):
            b, r = fixture()
            if kind == 'observation': b['observations']['before-default']['observation_sha256'] = 'f' * 64
            else:
                r['before-default']['binding_sha256'] = 'f' * 64
                r['before-default']['observation_sha256'] = v3.observation_digest(r['before-default'])
                b['observations']['before-default']['receipt_byte_sha256'] = sha(wire(r['before-default']))
                b['observations']['before-default']['observation_sha256'] = r['before-default']['observation_sha256']
            with self.subTest(kind=kind), self.assertRaises(ValueError): verify(b, r)


class ShapeChecks(unittest.TestCase):
    def reject_receipt(self, change):
        b, r = fixture()
        change(r['before-default'])
        rebind(b, r)
        with self.assertRaises(ValueError): verify(b, r)

    def test_missing_extra_descriptor_at_every_position(self):
        for index in range(12):
            for extra in (False, True):
                def change(r):
                    e = r['observation_envelope']
                    e.insert(index, copy.deepcopy(e[index])) if extra else e.pop(index)
                with self.subTest(index=index, extra=extra): self.reject_receipt(change)

    def test_order_role_argv_exit_and_domain_at_every_position(self):
        for index in range(12):
            for field, bad in (('role', 'other'), ('argv', ['go', 'list']), ('exit', True), ('exit', 1),
                              ('binding_stdout_domain', 'package')):
                with self.subTest(index=index, field=field, bad=bad):
                    self.reject_receipt(lambda r: r['observation_envelope'][index].update({field: bad}))
        self.reject_receipt(lambda r: r['observation_envelope'].reverse())

    def test_command_shape_and_field_types(self):
        for field in consumer._COMMAND_FIELDS:
            with self.subTest(missing=field): self.reject_receipt(lambda r: r['observation_envelope'][0].pop(field))
        for field, value in (('stderr_bytes', -1), ('stderr_bytes', True), ('raw_stdout_bytes', '10'),
                             ('raw_stdout_bytes', 0), ('stderr_sha256', 'e' * 64),
                             ('stderr_sha256', 'G' * 64), ('raw_stdout_sha256', None), ('binding_stdout_sha256', []),
                             ('extra', False)):
            with self.subTest(field=field): self.reject_receipt(lambda r: r['observation_envelope'][0].update({field: value}))

    def test_receipt_top_level_shape(self):
        # Fields needed for resealing cannot be removed by the synthetic publisher;
        # check their raw pinned shape separately below.
        for field in consumer._RECEIPT_FIELDS - {'base_source', 'observation_envelope', 'binding_sha256', 'observation_sha256'}:
            with self.subTest(field=field): self.reject_receipt(lambda r: r.pop(field))
        self.reject_receipt(lambda r: r.update(extra=True))

    def test_swapped_context_root_source_and_producer(self):
        mutations = [lambda r: r['base_source'].update(snapshot_root='/other'),
            lambda r: r['base_source'].update(build_context=copy.deepcopy(v3.CONTEXT)),
            lambda r: r['base_source'].update(policy='other'),
            lambda r: r['base_source'].update(schema_version=True),
            lambda r: r['metadata_producer'].update(version='go1.25.8'),
            lambda r: r['metadata_producer'].update(executable_sha256='f' * 64),
            lambda r: r['metadata_producer'].update(extra=True),
            lambda r: r['v3_source'].update({v3._V3_FILES[0]: 'f' * 64}),
            lambda r: r['v3_source'].update({v3.SCHEMA_PATH: 'f' * 64})]
        for index, mutation in enumerate(mutations):
            with self.subTest(index=index): self.reject_receipt(mutation)

    def test_bool_context_cannot_equal_integer(self):
        self.reject_receipt(lambda r: r['base_source']['build_context'].update(cgo_enabled=True))

    def test_environment_and_source_nested_type_mismatches(self):
        for field, value in (('GOOS', 'darwin'), ('GOFLAGS', '-changed'), ('GODEBUG', 'changed'),
                             ('GOCACHE', 'relative'), ('extra', 'unexpected')):
            with self.subTest(environment=field): self.reject_receipt(lambda r: r['environment'].update({field: value}))
        for field, value in (('GOVERSION', 'go1.25.8'), ('GOHOSTARCH', 'arm64'), ('GOGCCFLAGS', None)):
            with self.subTest(go_env=field): self.reject_receipt(lambda r: r['go_env'].update({field: value}))
        for field, value in (('replacements', 'wrong'), ('module_resolution', []), ('embed_inputs', []),
                             ('excluded_directories', [False]), ('version_test_contract', 'wrong')):
            with self.subTest(source=field): self.reject_receipt(lambda r: r['base_source'].update({field: value}))

    def test_variant_directory_inconsistency_and_repeat_mismatch(self):
        self.reject_receipt(lambda r: r['base_source']['archive_boundary'].update(production_variant_directories=['cmd', 'cmd']))
        self.reject_receipt(lambda r: r['observation_envelope'][7].update(binding_stdout_sha256='f' * 64))
        self.reject_receipt(lambda r: r['observation_envelope'][3].update(binding_stdout_sha256='f' * 64))

    def test_bundle_and_pin_missing_extra_fields(self):
        for location in ('bundle', 'pin'):
            b, r = fixture()
            target = b if location == 'bundle' else b['observations']['before-default']
            for field in tuple(target):
                changed = copy.deepcopy(b)
                (changed if location == 'bundle' else changed['observations']['before-default']).pop(field)
                with self.subTest(location=location, field=field), self.assertRaises(ValueError): verify(changed, r)
            target['extra'] = True
            with self.assertRaises(ValueError): verify(b, r)

    def test_duplicate_malformed_json_at_nested_boundaries(self):
        b, r = fixture()
        bundle_raw = wire(b)
        bundle_bad = [b'{', b'[]', b'{}junk', b'\xff', bundle_raw.replace(b'"schema_version":1', b'"schema_version":1,"schema_version":1'),
                      bundle_raw.replace(b'"run_id":"owned-run"', b'"run_id":NaN'),
                      bundle_raw.replace(b'"run_id":"owned-run"', b'"run_id":1e999'),
                      bundle_raw.replace(b'"before-default":', b'"before-default":{},"before-default":', 1),
                      bundle_raw.replace(b'"receipt_path":', b'"receipt_path":"/duplicate","receipt_path":', 1),
                      bundle_raw.replace(b'"version":"go1.25.7"', b'"version":"go1.25.7","version":"go1.25.7"', 1),
                      bundle_raw.replace(b'"source_root":', b'"source_root":"/duplicate","source_root":')]
        for raw in bundle_bad:
            with self.subTest(raw=raw[:60]), self.assertRaises(ValueError):
                consumer.verify_bundle(raw, {s: wire(v) for s, v in r.items()}, trusted_bundle_byte_sha256=sha(raw),
                    run_id='owned-run', source_commit=COMMIT, source_root=ROOT, producer=PRODUCER,
                    allowed_slots=tuple(r), selected_binding_version=3)
        receipt_raw = wire(r['before-default'])
        for raw in (b'{}', b'[]', b'\xff', receipt_raw.replace(b'"exit":0', b'"exit":0,"exit":0', 1),
                    receipt_raw.replace(b'"stderr_bytes":0', b'"stderr_bytes":Infinity', 1),
                    receipt_raw.replace(b'"race":false', b'"race":false,"race":false', 1)):
            changed = copy.deepcopy(b)
            changed['observations']['before-default']['receipt_byte_sha256'] = sha(raw)
            raws = {s: wire(v) for s, v in r.items()}; raws['before-default'] = raw
            with self.assertRaises(ValueError):
                consumer.verify_bundle(wire(changed), raws, trusted_bundle_byte_sha256=sha(wire(changed)),
                    run_id='owned-run', source_commit=COMMIT, source_root=ROOT, producer=PRODUCER,
                    allowed_slots=tuple(r), selected_binding_version=3)


class ProjectionAndIsolationChecks(unittest.TestCase):
    def test_only_existing_hydration_and_raw_stdout_exclusions(self):
        b, r = fixture()
        original = copy.deepcopy(r['before-default'])
        # Hydration stderr/argv/domain included in observation, excluded in binding.
        r['after-default']['observation_envelope'][1].update(raw_stdout_sha256='f' * 64,
            raw_stdout_bytes=123, stderr_sha256='e' * 64, stderr_bytes=25, binding_stdout_sha256='e' * 64)
        # Retained raw package stdout may differ with same normalized projection.
        r['after-default']['observation_envelope'][2].update(raw_stdout_sha256='f' * 64, raw_stdout_bytes=99)
        rebind(b, r)
        verified = verify(b, r)
        payloads = consumer.binding_payloads(verified, selected_binding_version=3)
        self.assertEqual(payloads['before-default'], payloads['after-default'])
        self.assertNotEqual(verified['before-default']['observation_sha256'], verified['after-default']['observation_sha256'])
        self.assertEqual(consumer.binding_payloads({'one': original}, selected_binding_version=3)['one'], v3.binding_payload(original))

    def test_retained_command_stderr_hash_and_length_remain_bound(self):
        for index in (0, *range(2, 12)):
            for field, changed in (('stderr_sha256', 'e' * 64), ('stderr_bytes', 9)):
                b, r = fixture()
                r['after-default']['observation_envelope'][index][field] = changed
                if field == 'stderr_sha256':
                    # Both candidate receipts describe nonempty stderr, so the
                    # hash-only mutation is structurally valid independently.
                    r['before-default']['observation_envelope'][index]['stderr_bytes'] = 9
                    r['after-default']['observation_envelope'][index]['stderr_bytes'] = 9
                rebind(b, r); verified = verify(b, r)
                with self.subTest(index=index, field=field):
                    self.assertNotEqual(v3.binding_payload(verified['before-default']), v3.binding_payload(verified['after-default']))

    def test_verification_never_calls_capture_validate_or_retention(self):
        b, r = fixture()
        with mock.patch.object(v3, 'capture', side_effect=AssertionError('capture forbidden')) as capture, \
             mock.patch.object(v3, 'validate', side_effect=AssertionError('validate forbidden')) as validate, \
             mock.patch.object(v3, 'Retention', side_effect=AssertionError('retention forbidden')) as retention:
            verify(b, r)
            b['run_id'] = 'wrong'
            with self.assertRaises(ValueError): verify(b, r)
        capture.assert_not_called(); validate.assert_not_called(); retention.assert_not_called()

    def test_adapter_does_not_read_environment_or_candidate_paths(self):
        b, r = fixture()
        with mock.patch('os.getenv', side_effect=AssertionError('environment forbidden')), \
             mock.patch('builtins.open', side_effect=AssertionError('candidate IO forbidden')), \
             mock.patch.object(Path, 'read_bytes', side_effect=AssertionError('candidate IO forbidden')):
            verify(b, r)

    def test_os_process_guard_has_observed_zero_events(self):
        self.assertEqual(PROCESS_EVENTS, [])


if __name__ == '__main__':
    result = unittest.main(exit=False, verbosity=2).result
    print('OS-process audit events: ' + str(len(PROCESS_EVENTS)))
    sys.exit(0 if result.wasSuccessful() and not PROCESS_EVENTS else 1)
