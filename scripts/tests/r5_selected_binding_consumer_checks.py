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

if len(sys.argv) > 1 and sys.argv[1] == '--replay-delta':
    import argparse
    import contextlib
    import importlib.util
    import io
    import runpy
    parser = argparse.ArgumentParser()
    parser.add_argument('--replay-delta', required=True)
    parser.add_argument('--adapter')
    parser.add_argument('--expected-gaps', type=int, choices=(0, 5), required=True)
    args = parser.parse_args()
    directory = Path(args.replay_delta)
    for name, expected in (
        ('independent_replay.py', 'd8a2151eba65efe1d6cfcd302ae36c6f7102526338a6893b6103eaa781f7d869'),
        ('new_edge_checks.py', '9f284ec994c9a9e164aef0103ae546bae79b24a7ab6be70c34dea4b1ae3963d9')):
        assert hashlib.sha256((directory / name).read_bytes()).hexdigest() == expected
    adapter_path = Path(args.adapter) if args.adapter else Path(__file__).resolve().parents[1] / 'r5_selected_binding_consumer.py'
    adapter_sha = hashlib.sha256(adapter_path.read_bytes()).hexdigest()
    if args.adapter:
        spec = importlib.util.spec_from_file_location('r5_selected_binding_consumer', args.adapter)
        baseline = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(baseline)
        sys.modules['r5_selected_binding_consumer'] = baseline
    # Execute both exact independent programs, including their own fixtures,
    # assertions, publisher, classifier parity probes and process guards.
    with contextlib.redirect_stdout(io.StringIO()):
        namespace = runpy.run_path(str(directory / 'new_edge_checks.py'))
    prior = namespace['n']['RESULTS']
    delta = namespace['RESULTS']
    gaps = [r['name'] for r in delta if r['actual'] != r['expected']]
    assert len(prior) == 114 and all(r['passed'] for r in prior)
    assert len(delta) == 18 and len(gaps) == args.expected_gaps
    assert not PROCESS_EVENTS and not namespace['EVENTS'] and not namespace['n']['EVENTS']
    print(json.dumps(dict(adapter_sha256=adapter_sha, prior_controls=prior, delta_controls=delta,
        helper_parity=namespace['helper_results'], unexpected_acceptances=gaps,
        os_process_events=PROCESS_EVENTS), indent=2))
    sys.exit(0)

if len(sys.argv) > 1 and sys.argv[1] == '--replay-review':
    # Preserve all 114 externally specified controls. Only replace their earlier
    # incomplete positive schema fixture, plus the obsolete six-gap assertion.
    # Evidence stays immutable; the complete fixture is explicit author scaffolding.
    import argparse
    import ast
    import importlib.util
    parser = argparse.ArgumentParser()
    parser.add_argument('--replay-review', required=True)
    parser.add_argument('--adapter')
    parser.add_argument('--expected-gaps', type=int, choices=(0, 6), required=True)
    args = parser.parse_args()
    reviewer_source = Path(args.replay_review).read_bytes()
    assert hashlib.sha256(reviewer_source).hexdigest() == '9c4743698351ea2f6f1f51381a680e49011d750e857dfec3882dfa91d4d86b53'
    if args.adapter:
        spec = importlib.util.spec_from_file_location('r5_selected_binding_consumer', args.adapter)
        baseline = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(baseline)
        sys.modules['r5_selected_binding_consumer'] = baseline
    own_source = Path(__file__).read_text()
    fixture_source = '\n\n'.join(ast.get_source_segment(own_source, node) for node in ast.parse(own_source).body
        if isinstance(node, ast.FunctionDef) and node.name in ('source_seal', 'fixture_receipt'))
    source = reviewer_source.decode()
    adapter_path = Path(args.adapter) if args.adapter else Path(__file__).resolve().parents[1] / 'r5_selected_binding_consumer.py'
    adapter_sha = hashlib.sha256(adapter_path.read_bytes()).hexdigest()
    source = source.replace("candidate='3ff6db72b579419a7145d2bc2a3b888c111c9b62'", "candidate='adapter-sha256:" + adapter_sha + "'")
    hook = "consumer = c\nv3 = v\n" + fixture_source + '\nreceipt = fixture_receipt\n'
    anchor = "r=fresh(); b,raw=publish(r); check('controlled-two-context-four-slot-positive',b,raw,False)"
    assert source.count(anchor) == 1
    source = source.replace(anchor, hook + anchor)
    final_assert = "assert len([x for x in RESULTS if not x['passed']])==6"
    assert source.count(final_assert) == 1
    source = source.replace(final_assert, "assert len([x for x in RESULTS if not x['passed']])==" + str(args.expected_gaps))
    namespace = {'__file__': str(Path(__file__).resolve().parents[2] /
        'docs/company-mail/evidence/R5-ADAPTER-INDEPENDENT-20261004/independent_checks.py')}
    exec(compile(source, '<preserved-independent-controls-with-complete-fixture>', 'exec'), namespace)
    assert len(namespace['RESULTS']) == 114
    assert not PROCESS_EVENTS
    print('Replay: 114 controls; unexpected acceptances=' + str(args.expected_gaps) + '; OS-process events=0')
    sys.exit(0)

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
    files = {'cmd/main.go': 'b' * 64, 'go.mod': 'a' * 64, 'go.sum': 'a' * 64}
    local_modules = []
    for replacement in v3.inventory.CURRENT_REPLACEMENTS:
        prefix = replacement['path'] + '/'
        metadata = {name: 'a' * 64 for name in v3.inventory.MODULE_METADATA[replacement['module']]}
        files.update({prefix + name: 'a' * 64 for name in ('go.mod', 'go.sum', *metadata)})
        local_modules.append(dict(replacement, declared_go='1.25.7', declared_toolchain=None,
            declared_requires={}, go_mod_sha256='a' * 64, go_sum_sha256='a' * 64,
            metadata_sha256=metadata, upstream_file_inventory=['go.mod']))
    resolution = dict(main_module='tabmail', root_go_mod_sha256='a' * 64, root_go_sum_sha256='a' * 64,
        declared_go='1.25.7', declared_toolchain=None,
        declared_requires={r['module']: r['version'] for r in v3.inventory.CURRENT_REPLACEMENTS},
        local_modules=local_modules,
        local_module_file_selection='all_regular_files_not_suffix_filtered_except_explicit_artifact_directories_and_DS_Store',
        excluded_local_module_metadata=dict(
            inspection='names_and_lstat_types_only_no_regular_file_reads_no_symlink_following',
            reject=['symlinks', 'nested_go.mod', 'special_or_unknown_types', 'unavailable_metadata', 'budget_exceeded'],
            max_entries_per_local_module_per_pass=4096, max_depth_including_excluded_root=32),
        effective_root_MVS_observed=False, go_selected_inputs_observed=False, external_module_cache_verified=False,
        boundary='Declared root/local module metadata and complete local input superset only; '
                 'actual root MVS/go-list/build/external-cache qualification is separate')
    static = dict(policy='r5_immutable_archive_boundary_v1', registry_sha256=v3.boundary.REGISTRY_SHA256,
        modules=sorted(['go.mod', *[r['path'] + '/go.mod' for r in v3.inventory.CURRENT_REPLACEMENTS]]),
        archive_static={}, archive_markers={}, production_go={'cmd/main.go': 'b' * 64},
        production_variant_directories=['cmd'], compile_roots=['./...'],
        explicit_production_roots=['./cmd/...', './internal/...'], budgets=dict(entries=100000, depth=64, bytes=67108864))
    frozen_ids = sorted('test_r5_selected_source_binding.ActualRootBindingTests.' + name for name in
        ('test_actual_full_scope_fresh_hashes', 'test_real_receipt_roundtrip',
         'test_omission_extra_hash_flags_abi_and_historical_rejected', 'test_capture_midflight_drift_refused'))
    base = dict(schema_version=4, snapshot_root=ROOT, source_identity_kind=v3.inventory.ARCHIVE_KIND,
        policy=v3.inventory.ARCHIVE_POLICY, purpose='selected', build_context=copy.deepcopy(context),
        replacements=[dict(r) for r in v3.inventory.CURRENT_REPLACEMENTS], module_resolution=resolution,
        inventory_implementation_sha256=v3.inventory._IMPLEMENTATION_SHA256,
        boundary='Versioned archive static bytes and production variant superset; selected metadata/runtime are separate',
        excluded_directories=sorted(v3.inventory.EXCLUDED_DIRS), embed_inputs={}, files=files, archive_static={},
        archive_boundary=static, version_test_contract=dict(frozen_v1=v3.boundary.BASELINE,
            frozen_test_ids=frozen_ids, current_actual_class='test_r5_selected_source_binding_v2.ActualRootBindingV2Tests.',
            expected_failures_are_green=False))
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
    package = dict(directory='cmd', import_path='tabmail/cmd', for_test=None, module='tabmail', fields={'GoFiles': ['main.go']})
    coverage = dict(root_packages=[copy.deepcopy(package)], explicit_packages=[copy.deepcopy(package)],
        no_missing=True, no_overlap=True, all_variant_directories=['cmd'], other_contexts='static_only_not_executed',
        variant_directory_records=[dict(directory='cmd', import_path='tabmail/cmd', module='tabmail',
            fields={'GoFiles': ['cmd/main.go']}, selected_context=True, other_variants='static_only')])
    mvs = [dict(Path='tabmail', Main=True, GoVersion='1.25.7')]
    mvs += [dict(Path=r['module'], Version=r['version'], Replace=dict(r)) for r in v3.inventory.CURRENT_REPLACEMENTS]
    qualification = dict(overall='blocked', excluded_metadata_boundary='descriptor_checked', local_static_binding='captured',
        generated='unknown', external_modulecache='unknown', compiler_native='unknown', Method19='unknown', SML='unknown', wholeCI='unknown')
    return v3._seal(dict(schema_version=3, policy=v3.POLICY,
        incompatible_with='selected v1/v2 receipts never authorize v3; v3 never authorizes v2',
        observation_order='env; unbound hydration; selected; MVS; coverage; selected; MVS; repeated coverage (projected package equality; raw MVS equality)',
        base_source=base, archive_static={}, production_coverage=coverage,
        environment=dict(PATH='/usr/bin:/bin', HOME='/controller', GODEBUG='asynctimerchan=0',
            GOWORK='off', GOENV='off', GOTOOLCHAIN='local', GOFLAGS='', GOOS='linux', GOARCH='amd64',
            CGO_ENABLED='1', GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org',
            GOPATH='/controller/cache', GOMODCACHE='/controller/cache/mod', GOCACHE='/controller/cache/build'),
        go_env=dict(GOVERSION='go1.25.7', GOOS='linux', GOARCH='amd64', GOHOSTOS='linux',
            GOHOSTARCH='amd64', CGO_ENABLED='1', GOWORK='off', GOENV='', GOFLAGS='', GOEXPERIMENT='',
            GOAMD64='v1', GOTOOLCHAIN='local', GOROOT='/controller/toolchain', GOGCCFLAGS='-fdebug-prefix-map=/tmp/go-build<TEMP>='),
        observation_envelope=envelope, v3_source={v3._V3_FILES[0]: v3._IMPLEMENTATION_SHA256,
        v3.SCHEMA_PATH: sha(v3._REGISTRY_BYTES), v3._V3_FILES[2]: 'd' * 64},
        metadata_producer=dict(version=PRODUCER['version'], executable_sha256=PRODUCER['sha256'],
            qualification='exact_metadata_producer_only_not_compilation_attestation'),
        root_mvs=sorted(mvs, key=lambda r: r['Path']), selected_local={'cmd/main.go': {'sha256': 'b' * 64, 'fields': ['GoFiles']}},
        generated_testmain=[], external_modulecache_inputs=[], native_inputs=[], toolchain_source_inputs=[],
        selected_local_packages=[package], package_records=1, qualification=qualification,
        boundary='Metadata/static local bytes only; no product tests, cold build or runtime qualification'))


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


class CompleteNestedSchemaChecks(unittest.TestCase):
    def reject_mutation(self, mutation):
        b, r = fixture()
        mutation(r['before-default'])
        rebind(b, r)
        with self.assertRaises(ValueError): verify(b, r)

    def test_each_required_nested_field_and_extra_key(self):
        _, receipts = fixture()
        sample = receipts['before-default']
        paths = [('root_mvs', 0), ('root_mvs', 0, 'Replace'),
            ('selected_local', 'cmd/main.go'), ('selected_local_packages', 0),
            ('production_coverage',), ('production_coverage', 'root_packages', 0),
            ('production_coverage', 'variant_directory_records', 0), ('qualification',),
            ('base_source', 'module_resolution'), ('base_source', 'module_resolution', 'local_modules', 0),
            ('base_source', 'module_resolution', 'excluded_local_module_metadata'),
            ('base_source', 'version_test_contract')]
        for path in paths:
            row = sample
            for key in path: row = row[key]
            for field in (*row, '__extra__'):
                def mutation(r):
                    obj = r
                    for key in path: obj = obj[key]
                    if field == '__extra__': obj[field] = 'extra'
                    else: obj.pop(field)
                # MVS Version/Replace are optional schema fields, but required
                # for this pinned fork. GoVersion/Main on main are also tested
                # through the separate missing-root controls below.
                with self.subTest(path=path, field=field): self.reject_mutation(mutation)

    def test_all_nested_leaf_type_errors(self):
        _, receipts = fixture()
        sample = receipts['before-default']
        paths = []
        def walk(value, path):
            if type(value) is dict:
                for key, item in value.items(): walk(item, (*path, key))
            elif type(value) is list:
                for index, item in enumerate(value): walk(item, (*path, index))
            else: paths.append((path, value))
        for field in ('root_mvs', 'selected_local', 'selected_local_packages', 'production_coverage',
                      'qualification', 'base_source'):
            walk(sample[field], (field,))
        # Source seals are publisher-derived and will be regenerated below.
        for path, value in paths:
            if path in (('base_source', 'source_sha'), ('base_source', 'source_closure_sha256')): continue
            def mutation(r):
                obj = r
                for key in path[:-1]: obj = obj[key]
                obj[path[-1]] = 1 if type(value) is bool else True if value is None or type(value) is int else {}
            with self.subTest(path=path): self.reject_mutation(mutation)

    def test_nested_linkage_missing_or_extra_records(self):
        mutations = [lambda r: r.update(root_mvs=[]),
            lambda r: r['root_mvs'].append(copy.deepcopy(r['root_mvs'][0])),
            lambda r: r['selected_local']['cmd/main.go'].update(sha256='e' * 64),
            lambda r: r['selected_local']['cmd/main.go'].update(fields=['CgoFiles']),
            lambda r: r.update(selected_local={}),
            lambda r: r['production_coverage'].update(explicit_packages=[]),
            lambda r: r['production_coverage'].update(variant_directory_records=[]),
            lambda r: r['production_coverage']['variant_directory_records'][0]['fields'].update(GoFiles=['cmd/other.go']),
            lambda r: r['base_source']['module_resolution']['local_modules'].pop(),
            lambda r: r['base_source']['module_resolution']['local_modules'][0].update(go_mod_sha256='e' * 64),
            lambda r: r['base_source']['module_resolution'].update(external_module_cache_verified=True)]
        for index, mutation in enumerate(mutations):
            with self.subTest(index=index): self.reject_mutation(mutation)

    def test_classified_records_positive_and_every_field_type(self):
        b, receipts = fixture()
        for r in receipts.values():
            r['root_mvs'].append(dict(Path='example.org/dependency', Version='v1.2.3'))
            r['root_mvs'].sort(key=lambda row: row['Path'])
            r['generated_testmain'] = [dict(package='tabmail/cmd.test', field='GoFiles', path='aa/synthetic-d',
                classification='generated_testmain', qualification='unknown')]
            r['selected_local_packages'].append(dict(directory='cmd', import_path='tabmail/cmd.test', for_test=None,
                module='tabmail', fields={'GoFiles': ['/controller/cache/build/aa/synthetic-d']}))
            for key in ('root_packages', 'explicit_packages'):
                r['production_coverage'][key] = sorted(copy.deepcopy(r['selected_local_packages']), key=wire)
            r['package_records'] = 4
            r['external_modulecache_inputs'] = [dict(package='example.org/dependency', field='GoFiles',
                module='example.org/dependency', version='v1.2.3', path='example.org/dependency@v1.2.3/file.go',
                classification='external_modulecache', qualification='unknown')]
            r['toolchain_source_inputs'] = [dict(package='runtime', field='GoFiles', path='src/runtime/file.go',
                classification='toolchain_source', qualification='unknown')]
            r['native_inputs'] = [dict(package='runtime', field='SFiles', path='src/runtime/asm.s',
                classification='toolchain_source', qualification='unknown')]
        rebind(b, receipts)
        verify(b, receipts)
        for container in ('generated_testmain', 'external_modulecache_inputs', 'toolchain_source_inputs', 'native_inputs'):
            for field in (*receipts['before-default'][container][0], '__extra__'):
                for kind in ('missing', 'bad-type'):
                    changed = copy.deepcopy(receipts)
                    row = changed['before-default'][container][0]
                    if kind == 'missing' and field != '__extra__': row.pop(field)
                    else: row[field] = False
                    rebind(b, changed)
                    with self.subTest(container=container, field=field, kind=kind), self.assertRaises(ValueError): verify(b, changed)


class ClassifierContractChecks(unittest.TestCase):
    @staticmethod
    def sync(r):
        for key in ('root_packages', 'explicit_packages'):
            r['production_coverage'][key] = sorted(copy.deepcopy(
                [p for p in r['selected_local_packages'] if p['module'] == 'tabmail']), key=wire)

    @staticmethod
    def add_native(r, field):
        path = 'cmd/native.input'
        r['base_source']['files'][path] = 'e' * 64
        r['selected_local'][path] = dict(sha256='e' * 64, fields=[field])
        r['selected_local_packages'][0]['fields'][field] = ['native.input']
        ClassifierContractChecks.sync(r)
        return dict(package='tabmail/cmd', field=field, path=path, qualification='unknown')

    def test_first_four_go_fields_in_every_represented_domain(self):
        for field in v3.FIELDS[:4]:
            for domain in ('local', 'toolchain', 'external'):
                b, r = fixture()
                receipt = r['before-default']
                if domain == 'local':
                    receipt['base_source']['files']['cmd/not-go.txt'] = 'e' * 64
                    receipt['selected_local_packages'][0]['fields'][field] = ['not-go.txt']
                    if field == 'GoFiles': receipt['selected_local'].pop('cmd/main.go')
                    receipt['selected_local']['cmd/not-go.txt'] = dict(sha256='e' * 64, fields=[field])
                    self.sync(receipt)
                else:
                    receipt['package_records'] = 2
                    if domain == 'toolchain':
                        receipt['toolchain_source_inputs'] = [dict(package='runtime', field=field,
                            path='src/runtime/not-go.txt', qualification='unknown', classification='toolchain_source')]
                    else:
                        receipt['root_mvs'].append(dict(Path='example.org/dependency', Version='v1.2.3'))
                        receipt['root_mvs'].sort(key=lambda x: x['Path'])
                        receipt['external_modulecache_inputs'] = [dict(package='example.org/dependency', field=field,
                            path='example.org/dependency@v1.2.3/not-go.txt', qualification='unknown',
                            classification='external_modulecache', module='example.org/dependency', version='v1.2.3')]
                rebind(b, r)
                with self.subTest(field=field, domain=domain), self.assertRaisesRegex(ValueError, 'false Go source path'):
                    verify(b, r)

    def test_all_local_native_fields_complete_package_attributed_occurrences(self):
        for field in sorted(v3.v2.NATIVE_FIELDS):
            b, receipts = fixture()
            expected = self.add_native(receipts['before-default'], field)
            receipts['before-default']['native_inputs'] = [expected]
            rebind(b, receipts)
            verify(b, receipts)
            raw = dict(Dir=ROOT + '/cmd', ImportPath='tabmail/cmd', Name='package',
                Module=dict(Path='tabmail', Main=True, Dir=ROOT), GoFiles=['main.go'], **{field: ['native.input']})
            with mock.patch.object(v3.v2, 'base_markers', return_value=[]):
                actual = v3.classify([raw], Path(ROOT), set(receipts['before-default']['base_source']['files']), {}, receipts['before-default']['root_mvs'])
            self.assertEqual(actual[3], [expected])
            for mode in ('missing', 'foreign-package', 'duplicate', 'wrong-field'):
                r = copy.deepcopy(receipts)
                records = r['before-default']['native_inputs']
                if mode == 'missing': records.clear()
                if mode == 'foreign-package': records[0]['package'] = 'foreign/unselected'
                if mode == 'duplicate': records.append(copy.deepcopy(records[0]))
                if mode == 'wrong-field': records[0]['field'] = next(f for f in v3.v2.NATIVE_FIELDS if f != field)
                rebind(b, r)
                with self.subTest(field=field, mode=mode), self.assertRaises(ValueError): verify(b, r)

    def test_native_multiplicity_order_and_shared_paths_across_packages(self):
        b, receipts = fixture()
        r = receipts['before-default']
        first = self.add_native(r, 'CFiles')
        second_package = copy.deepcopy(r['selected_local_packages'][0])
        second_package['import_path'] = 'tabmail/cmd [tabmail/cmd.test]'
        second_package['for_test'] = 'tabmail/cmd'
        r['selected_local_packages'].append(second_package)
        r['package_records'] = 2
        self.sync(r)
        second = dict(first, package=second_package['import_path'])
        r['native_inputs'] = [first, second]
        rebind(b, receipts)
        verify(b, receipts)
        r['native_inputs'].reverse()
        rebind(b, receipts)
        with self.assertRaises(ValueError): verify(b, receipts)

    def test_non_go_bytes_remain_permitted_for_bound_embeds(self):
        b, receipts = fixture()
        r = receipts['before-default']
        r['base_source']['files']['cmd/data.txt'] = 'e' * 64
        r['base_source']['embed_inputs'] = {'cmd/main.go:data.txt': ['cmd/data.txt']}
        r['selected_local_packages'][0]['fields']['EmbedFiles'] = ['data.txt']
        r['selected_local']['cmd/data.txt'] = dict(sha256='e' * 64, fields=['EmbedFiles'])
        self.sync(r)
        rebind(b, receipts)
        verify(b, receipts)

    def test_generated_cache_exception_complete_linkage_and_wrong_field_rejection(self):
        b, receipts = fixture()
        r = receipts['before-default']
        package = dict(directory='cmd', import_path='tabmail/cmd.test', for_test=None,
            module='tabmail', fields={'GoFiles': ['/controller/cache/build/aa/generated-d']})
        record = dict(package=package['import_path'], field='GoFiles', path='aa/generated-d',
            classification='generated_testmain', qualification='unknown')
        r['selected_local_packages'].append(package)
        r['generated_testmain'] = [record]
        r['package_records'] = 2
        self.sync(r)
        rebind(b, receipts)
        verify(b, receipts)
        original = copy.deepcopy(receipts)
        for mode in ('orphan', 'missing', 'duplicate', 'wrong-field', 'local-nongo'):
            r = copy.deepcopy(original)
            target = r['before-default']
            if mode == 'orphan': target['generated_testmain'][0]['package'] = 'foreign/unselected.test'
            if mode == 'missing': target['generated_testmain'] = []
            if mode == 'duplicate': target['generated_testmain'].append(copy.deepcopy(record))
            if mode == 'wrong-field':
                target['selected_local_packages'][1]['fields'] = {'CgoFiles': ['/controller/cache/build/aa/generated-d']}
                target['generated_testmain'][0]['field'] = 'CgoFiles'
                self.sync(target)
            if mode == 'local-nongo':
                target['base_source']['files']['cmd/generated.txt'] = 'e' * 64
                target['selected_local_packages'][1]['fields'] = {'GoFiles': ['generated.txt']}
                target['selected_local']['cmd/generated.txt'] = dict(sha256='e' * 64, fields=['GoFiles'])
                target['generated_testmain'] = []
                self.sync(target)
            rebind(b, r)
            with self.subTest(mode=mode), self.assertRaises(ValueError): verify(b, r)

    def test_classifier_isolation_and_nonlocal_package_count_lower_bound(self):
        b, receipts = fixture()
        before = v3.classify.__globals__['base_markers']
        with mock.patch.object(v3.v2, 'base_markers', side_effect=AssertionError('filesystem reader forbidden')) as reader:
            verify(b, receipts)
            self.assertIs(v3.classify.__globals__['base_markers'], reader)
            reader.assert_not_called()
        self.assertIs(v3.classify.__globals__['base_markers'], before)
        r = receipts['before-default']
        r['toolchain_source_inputs'] = [dict(package='runtime', field='GoFiles', path='src/runtime/runtime.go',
            classification='toolchain_source', qualification='unknown')]
        rebind(b, receipts)
        with self.assertRaisesRegex(ValueError, 'represented classification packages'): verify(b, receipts)
        r['package_records'] = 2
        rebind(b, receipts)
        verify(b, receipts)


if __name__ == '__main__':
    result = unittest.main(exit=False, verbosity=2).result
    print('OS-process audit events: ' + str(len(PROCESS_EVENTS)))
    sys.exit(0 if result.wasSuccessful() and not PROCESS_EVENTS else 1)
