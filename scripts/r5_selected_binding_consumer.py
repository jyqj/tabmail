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
import types

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
_QUALIFICATION = dict(overall='blocked', excluded_metadata_boundary='descriptor_checked',
    local_static_binding='captured', generated='unknown', external_modulecache='unknown',
    compiler_native='unknown', Method19='unknown', SML='unknown', wholeCI='unknown')
_FROZEN_IDS = sorted('test_r5_selected_source_binding.ActualRootBindingTests.' + name for name in
    ('test_actual_full_scope_fresh_hashes', 'test_real_receipt_roundtrip',
     'test_omission_extra_hash_flags_abi_and_historical_rejected', 'test_capture_midflight_drift_refused'))


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


def _record(value, required, optional=()):
    if type(value) is not dict or not set(required).issubset(value) or set(value) - set(required) - set(optional):
        raise ValueError('missing/extra/malformed nested record fields')


def _strings(value, *, sorted_unique=False):
    if type(value) is not list or any(type(s) is not str or not s or '\x00' in s for s in value):
        raise ValueError('malformed string array')
    if len(set(value)) != len(value) or (sorted_unique and value != sorted(value)):
        raise ValueError('duplicate/unordered string array')


def _requires(value):
    if type(value) is not dict:
        raise ValueError('module requirement map required')
    for module, version in value.items():
        _relative(module)
        if type(version) is not str or not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9._-]+)?(?:\+incompatible)?', version):
            raise ValueError('malformed module requirement version')


def _go_declaration(go, toolchain):
    if type(go) is not str or not re.fullmatch(r'[0-9]+\.[0-9]+(?:\.[0-9]+)?', go):
        raise ValueError('malformed declared Go version')
    if toolchain is not None and (type(toolchain) is not str or not re.fullmatch(r'go[0-9]+\.[0-9]+(?:\.[0-9]+)?', toolchain)):
        raise ValueError('malformed declared toolchain')


def _source_records(base, v3):
    inventory = v3.inventory
    _equal(base['replacements'], list(inventory.CURRENT_REPLACEMENTS), 'exact local replacements')
    _equal(base['inventory_implementation_sha256'], inventory._IMPLEMENTATION_SHA256, 'inventory helper')
    _equal(base['excluded_directories'], sorted(inventory.EXCLUDED_DIRS), 'excluded directories')
    _equal(base['boundary'], 'Versioned archive static bytes and production variant superset; '
        'selected metadata/runtime are separate', 'source boundary')
    _equal(base['version_test_contract'], dict(frozen_v1=v3.boundary.BASELINE,
        frozen_test_ids=_FROZEN_IDS, current_actual_class='test_r5_selected_source_binding_v2.ActualRootBindingV2Tests.',
        expected_failures_are_green=False), 'version test contract')
    resolution = base['module_resolution']
    _shape(resolution, 'main_module root_go_mod_sha256 root_go_sum_sha256 declared_go declared_toolchain '
           'declared_requires local_modules local_module_file_selection excluded_local_module_metadata '
           'effective_root_MVS_observed go_selected_inputs_observed external_module_cache_verified boundary'.split(), 'module resolution')
    _equal(resolution['main_module'], 'tabmail', 'main module')
    for key, path in (('root_go_mod_sha256', 'go.mod'), ('root_go_sum_sha256', 'go.sum')):
        _digest(resolution[key])
        _equal(resolution[key], base['files'].get(path), 'root module file')
    _go_declaration(resolution['declared_go'], resolution['declared_toolchain'])
    _requires(resolution['declared_requires'])
    _equal(resolution['local_module_file_selection'],
        'all_regular_files_not_suffix_filtered_except_explicit_artifact_directories_and_DS_Store', 'local file selection')
    _equal(resolution['excluded_local_module_metadata'], dict(
        inspection='names_and_lstat_types_only_no_regular_file_reads_no_symlink_following',
        reject=['symlinks', 'nested_go.mod', 'special_or_unknown_types', 'unavailable_metadata', 'budget_exceeded'],
        max_entries_per_local_module_per_pass=inventory.EXCLUDED_METADATA_MAX_ENTRIES,
        max_depth_including_excluded_root=inventory.EXCLUDED_METADATA_MAX_DEPTH), 'excluded metadata contract')
    for key in ('effective_root_MVS_observed', 'go_selected_inputs_observed', 'external_module_cache_verified'):
        _equal(resolution[key], False, 'static qualification ' + key)
    _equal(resolution['boundary'], 'Declared root/local module metadata and complete local input superset only; '
        'actual root MVS/go-list/build/external-cache qualification is separate', 'module boundary')
    modules = resolution['local_modules']
    if type(modules) is not list or len(modules) != 2:
        raise ValueError('exact two local module records required')
    for row, replacement in zip(modules, inventory.CURRENT_REPLACEMENTS):
        _shape(row, 'module version path declared_go declared_toolchain declared_requires go_mod_sha256 '
               'go_sum_sha256 metadata_sha256 upstream_file_inventory'.split(), 'local module')
        for key in ('module', 'version', 'path'):
            _equal(row[key], replacement[key], 'local module ' + key)
        _equal(resolution['declared_requires'].get(row['module']), row['version'], 'declared replacement requirement')
        _go_declaration(row['declared_go'], row['declared_toolchain'])
        _requires(row['declared_requires'])
        for key, name in (('go_mod_sha256', 'go.mod'), ('go_sum_sha256', 'go.sum')):
            _digest(row[key])
            _equal(row[key], base['files'].get(row['path'] + '/' + name), 'local module file')
        _shape(row['metadata_sha256'], inventory.MODULE_METADATA[row['module']], 'local module metadata')
        for name, digest in row['metadata_sha256'].items():
            _digest(digest)
            _equal(digest, base['files'].get(row['path'] + '/' + name), 'local metadata file')
        _strings(row['upstream_file_inventory'], sorted_unique=True)
        if not row['upstream_file_inventory']:
            raise ValueError('empty upstream inventory')
        for name in row['upstream_file_inventory']:
            _relative(name)
            if row['path'] + '/' + name not in base['files']:
                raise ValueError('upstream inventory outside source files')
    for declaration, paths in base['embed_inputs'].items():
        _text(declaration)
        parts = declaration.rsplit(':', 1)
        if len(parts) != 2 or not parts[1]:
            raise ValueError('malformed embed declaration')
        source, pattern = parts
        _relative(source)
        _relative(pattern)
        if not source.endswith('.go') or source not in base['files']:
            raise ValueError('embed declaration outside source files')
        if ':' in pattern or any(c in pattern for c in '[]?`') or '**' in pattern:
            raise ValueError('unsupported embed pattern')
        _strings(paths, sorted_unique=True)
        if not paths:
            raise ValueError('empty embed input declaration')
        for path in paths:
            _relative(path)
            if path not in base['files']:
                raise ValueError('embed input outside source files')


def _field_map(fields, allowed):
    _record(fields, (), allowed)
    for values in fields.values():
        _strings(values)
        for value in values:
            _absolute(value) if value.startswith('/') else _relative(value)


def _local_module(directory, v3):
    for replacement in v3.inventory.CURRENT_REPLACEMENTS:
        if directory == replacement['path'] or directory.startswith(replacement['path'] + '/'):
            return replacement['module']
    if directory in ('cmd', 'internal') or directory.startswith(('cmd/', 'internal/')):
        return 'tabmail'
    raise ValueError('package outside local production roots')


def _packages(rows, v3, *, main_only=False):
    if type(rows) is not list:
        raise ValueError('package record array required')
    for row in rows:
        _shape(row, 'directory import_path for_test module fields'.split(), 'local package')
        _relative(row['directory'])
        _text(row['import_path'])
        if row['for_test'] is not None:
            _text(row['for_test'])
        _equal(row['module'], 'tabmail' if main_only else _local_module(row['directory'], v3), 'package module')
        if main_only and _local_module(row['directory'], v3) != 'tabmail':
            raise ValueError('fork in root coverage')
        _field_map(row['fields'], v3.FIELDS)


def _classifier_contract(receipt, source_root, v3):
    """Replay preserved pure classification on representable normalized metadata.

    Only classify's archive-marker reader is replaced, in a private globals copy,
    by this already authenticated in-memory boundary's labels. Helper globals and
    code are never modified. classify itself performs only lexical Path operations;
    the replaced base_markers function was its sole filesystem-reading dependency.
    This is validation, not reconstruction/attestation of original stdout bytes.
    """
    base = receipt['base_source']
    root = v3.v2.Path(source_root)
    classifier = types.FunctionType(v3.classify.__code__,
        {**v3.classify.__globals__, 'base_markers': lambda unused: tuple(base['archive_boundary']['archive_markers'])},
        'consumer_pure_classify', v3.classify.__defaults__, v3.classify.__closure__)
    rows = []
    for package in receipt['selected_local_packages']:
        fields = package['fields']
        generated = False
        for field in v3.FIELDS[:4]:
            for name in fields.get(field, []):
                path = v3.v2.Path(name)
                cache = v3.v2.Path(receipt['environment']['GOCACHE'])
                # Prove the exceptional occurrence's provenance before giving
                # the reconstructed row a generated Name. A .test suffix alone
                # never exempts source-owned files from the ordinary .go rule.
                exceptional = (field == 'GoFiles' and package['import_path'].endswith('.test')
                    and path.is_absolute() and not path.is_relative_to(root)
                    and path.is_relative_to(cache) and path.name.endswith('-d')
                    and any(r['package'] == package['import_path'] and r['field'] == field
                        and r['path'] == path.relative_to(cache).as_posix()
                        for r in receipt['generated_testmain']))
                if not name.endswith('.go') and not exceptional:
                    raise ValueError('false Go source path')
                generated = generated or exceptional
        replacement = next((r for r in v3.inventory.CURRENT_REPLACEMENTS if r['module'] == package['module']), None)
        module = dict(Path='tabmail', Main=True, Dir=source_root) if replacement is None else dict(
            Path=replacement['module'], Version=replacement['version'],
            Replace=dict(Path='./' + replacement['path'], Dir=source_root + '/' + replacement['path']))
        rows.append(dict(Dir=source_root + '/' + package['directory'], ImportPath=package['import_path'],
            Name='main' if generated else 'package', Module=module, **fields))
    env = dict(receipt['go_env'])
    for key, needed in (('GOCACHE', bool(receipt['generated_testmain'])),
            ('GOMODCACHE', bool(receipt['external_modulecache_inputs']) or
             any(r.get('classification') == 'external_modulecache' for r in receipt['native_inputs']))):
        if key in env or needed:
            _equal(env.get(key), receipt['environment'][key], 'classifier observed/requested ' + key)
        else:
            # Partial pure fixtures without any cache-domain records need no
            # cache observation. No path or authority is inferred for that domain.
            env[key] = receipt['environment'][key]
    local, generated, external, native, toolchain = classifier(
        rows, root, set(base['files']), env, receipt['root_mvs'])
    _equal(local, {p: row['fields'] for p, row in receipt['selected_local'].items()}, 'classifier local inputs')
    _equal(generated, receipt['generated_testmain'], 'classifier generated occurrence records')
    _equal(native, [r for r in receipt['native_inputs'] if 'classification' not in r],
           'classifier local native occurrence records')
    # Every represented local row must classify locally or as a generated testmain.
    _equal((external, toolchain), ([], []), 'local package classification domains')
    unseen_packages = set()
    local_packages = {p['import_path'] for p in receipt['selected_local_packages']}
    for container in ('external_modulecache_inputs', 'toolchain_source_inputs', 'native_inputs'):
        for record in receipt[container]:
            domain = record.get('classification')
            if domain is None:
                continue  # Complete ordered local occurrences checked above.
            if record['package'] in local_packages:
                raise ValueError('nonlocal classification attributed to local package')
            unseen_packages.add(record['package'])
            if record['field'] in v3.FIELDS[:4] and not record['path'].endswith('.go'):
                raise ValueError('false Go source path')
            if domain == 'toolchain_source':
                goroot = env.get('GOROOT')
                _absolute(goroot)
                path = goroot + '/' + record['path']
                raw = dict(Dir=posixpath.dirname(path), ImportPath=record['package'], Standard=True,
                           **{record['field']: [path]})
                expected_index = 3 if container == 'native_inputs' else 4
            else:
                path = env['GOMODCACHE'] + '/' + record['path']
                raw = dict(Dir=posixpath.dirname(path), ImportPath=record['package'],
                    Module=dict(Path=record['module'], Version=record['version'], Dir=posixpath.dirname(path)),
                    **{record['field']: [path]})
                expected_index = 3 if container == 'native_inputs' else 2
            result = classifier([raw], root, set(base['files']), env, receipt['root_mvs'])
            _equal(result[expected_index], [record], 'classifier nonlocal input record')
            for index, values in enumerate(result):
                if index != expected_index and values:
                    raise ValueError('unexpected classifier domain')
    # A row may emit many records, and rows without selected fields emit none.
    # Only the lower bound supported by represented package identities is checked.
    if receipt['package_records'] < len(rows) + len(unseen_packages):
        raise ValueError('represented classification packages exceed total record count')


def _selected_records(receipt, source_root, v3):
    base = receipt['base_source']
    mvs = receipt['root_mvs']
    seen = set()
    replacements = []
    main = 0
    for row in mvs:
        _record(row, ('Path',), ('Version', 'Main', 'GoVersion', 'Sum', 'GoModSum', 'Replace'))
        _relative(row['Path'])
        if row['Path'] in seen:
            raise ValueError('duplicate MVS module')
        seen.add(row['Path'])
        for key in ('Version', 'GoVersion', 'Sum', 'GoModSum'):
            if key in row:
                _text(row[key])
        if 'Main' in row and type(row['Main']) is not bool:
            raise ValueError('malformed MVS Main')
        if row.get('Main', False):
            main += 1
            _equal(row['Path'], 'tabmail', 'main MVS identity')
            if 'Replace' in row or 'Version' in row:
                raise ValueError('unexpected main MVS version/replacement')
        elif 'Version' not in row:
            raise ValueError('missing MVS version')
        if 'Replace' in row:
            matches = [r for r in v3.inventory.CURRENT_REPLACEMENTS if
                       r['module'] == row['Path'] and r['version'] == row.get('Version')]
            if len(matches) != 1:
                raise ValueError('unapproved MVS replacement')
            _equal(row['Replace'], matches[0], 'normalized MVS replacement')
            replacements.append(row['Replace'])
    if main != 1 or sorted(replacements, key=lambda r: r['module']) != sorted(v3.inventory.CURRENT_REPLACEMENTS, key=lambda r: r['module']):
        raise ValueError('missing root or exact two MVS replacements')
    _equal(mvs, sorted(mvs, key=lambda r: r['Path']), 'MVS order')
    _equal(receipt['qualification'], _QUALIFICATION, 'selected qualification')
    packages = receipt['selected_local_packages']
    _packages(packages, v3)
    if receipt['package_records'] < len(packages):
        raise ValueError('local package count exceeds total')
    selected = receipt['selected_local']
    observed = {}
    for row in packages:
        for field, names in row['fields'].items():
            for name in names:
                if name.startswith('/'):
                    _absolute(name)
                    if not name.startswith(source_root + '/'):
                        # Generated testmain inputs may have a local package Dir
                        # but cache-based absolute GoFiles. Require their separate
                        # classification record rather than invent local authority.
                        matches = [r for r in receipt['generated_testmain'] if
                            r.get('package') == row['import_path'] and r.get('field') == field and
                            type(r.get('path')) is str and
                            name == receipt['environment']['GOCACHE'] + '/' + r.get('path', '')]
                        if len(matches) != 1:
                            raise ValueError('unclassified absolute local-package input')
                        continue
                    path = name[len(source_root) + 1:]
                else:
                    _relative(name)
                    path = row['directory'] + '/' + name
                observed.setdefault(path, set()).add(field)
    for path, row in selected.items():
        _relative(path)
        _shape(row, ('sha256', 'fields'), 'selected local input')
        _digest(row['sha256'])
        _equal(row['sha256'], base['files'].get(path), 'selected input source hash')
        _strings(row['fields'], sorted_unique=True)
        if not row['fields'] or set(row['fields']) - set(v3.FIELDS):
            raise ValueError('invalid selected input field names')
        if set(row['fields']) & set(v3.FIELDS[4:7]) and path not in {p for paths in base['embed_inputs'].values() for p in paths}:
            raise ValueError('selected embed outside declarations')
    _equal({p: row['fields'] for p, row in selected.items()},
           {p: sorted(fields) for p, fields in observed.items()}, 'selected package/input linkage')
    coverage = receipt['production_coverage']
    _shape(coverage, 'root_packages explicit_packages no_missing no_overlap all_variant_directories '
           'other_contexts variant_directory_records'.split(), 'production coverage')
    _equal(coverage['no_missing'], True, 'no missing coverage')
    _equal(coverage['no_overlap'], True, 'no overlap coverage')
    _equal(coverage['other_contexts'], 'static_only_not_executed', 'coverage qualification')
    for key in ('root_packages', 'explicit_packages'):
        _packages(coverage[key], v3, main_only=True)
        _equal(coverage[key], sorted(coverage[key], key=_canonical), 'coverage package order')
    _equal(coverage['root_packages'], coverage['explicit_packages'], 'root/explicit package coverage')
    _equal(coverage['root_packages'], sorted([p for p in packages if p['module'] == 'tabmail'], key=_canonical),
           'selected/root package linkage')
    static = base['archive_boundary']
    if not {p for p in selected if p.endswith('.go')}.issubset(static['production_go']):
        raise ValueError('selected Go outside static production')
    variants = coverage['variant_directory_records']
    if type(variants) is not list:
        raise ValueError('variant record array required')
    observed = set()
    directories = []
    for row in variants:
        _shape(row, 'directory import_path module fields selected_context other_variants'.split(), 'variant record')
        _relative(row['directory'])
        directories.append(row['directory'])
        _text(row['import_path'])
        _equal(row['module'], _local_module(row['directory'], v3), 'variant module')
        if type(row['selected_context']) is not bool:
            raise ValueError('malformed variant context')
        _equal(row['other_variants'], 'static_only', 'variant qualification')
        _field_map(row['fields'], ('GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles', 'IgnoredGoFiles'))
        for paths in row['fields'].values():
            if not paths:
                raise ValueError('empty serialized variant field')
            for path in paths:
                _relative(path)
                if not path.endswith('.go') or posixpath.dirname(path) != row['directory'] or path not in static['production_go']:
                    raise ValueError('variant file outside static authority')
                observed.add(path)
    _equal(directories, static['production_variant_directories'], 'variant record directories')
    _equal(sorted(observed), sorted(static['production_go']), 'variant file coverage')
    bound_modules = {row['Path']: row for row in mvs}
    for container, classification in (('generated_testmain', 'generated_testmain'),
            ('external_modulecache_inputs', 'external_modulecache'), ('toolchain_source_inputs', 'toolchain_source'),
            ('native_inputs', None)):
        for row in receipt[container]:
            actual = row.get('classification')
            if classification is not None:
                _equal(actual, classification, 'input classification')
            if actual not in (None, 'generated_testmain', 'external_modulecache', 'toolchain_source') or (container == 'native_inputs' and actual == 'generated_testmain'):
                raise ValueError('unknown input classification')
            required = ['package', 'field', 'path', 'qualification']
            if actual is not None:
                required.append('classification')
            if actual == 'external_modulecache':
                required += ['module', 'version']
            _shape(row, required, 'classified input')
            _text(row['package'])
            _relative(row['path'])
            _equal(row['qualification'], 'unknown', 'input qualification')
            if type(row['field']) is not str or row['field'] not in v3.FIELDS:
                raise ValueError('unknown input field')
            if container == 'native_inputs':
                if row['field'] not in v3.v2.NATIVE_FIELDS:
                    raise ValueError('non-native input in native records')
            elif row['field'] in v3.v2.NATIVE_FIELDS:
                raise ValueError('native input in non-native records')
            if actual is None:
                if container != 'native_inputs' or row['field'] not in selected.get(row['path'], {}).get('fields', []):
                    raise ValueError('local native input outside selected inputs')
            elif actual == 'generated_testmain':
                if row['field'] != 'GoFiles' or not row['path'].endswith('-d') or not row['package'].endswith('.test'):
                    raise ValueError('invalid generated testmain record')
            elif actual == 'toolchain_source' and not row['path'].startswith('src/'):
                raise ValueError('toolchain input outside src')
            elif actual == 'external_modulecache':
                bound = bound_modules.get(row['module']) if type(row['module']) is str else None
                if not bound or 'Replace' in bound or row['version'] != bound.get('Version'):
                    raise ValueError('external input outside MVS')


def _receipt(receipt, context, source_root, producer, v3):
    _shape(receipt, _RECEIPT_FIELDS, 'receipt')
    if type(receipt['schema_version']) is not int or receipt['schema_version'] != 3 or receipt['policy'] != v3.POLICY:
        raise ValueError('mixed/incompatible selected receipt version')
    for key in ('incompatible_with', 'observation_order', 'boundary'):
        _text(receipt[key])
    _equal(receipt['incompatible_with'], 'selected v1/v2 receipts never authorize v3; v3 never authorizes v2', 'receipt incompatibility')
    _equal(receipt['observation_order'], 'env; unbound hydration; selected; MVS; coverage; selected; MVS; '
        'repeated coverage (projected package equality; raw MVS equality)', 'receipt observation order')
    _equal(receipt['boundary'], 'Metadata/static local bytes only; no product tests, cold build or runtime qualification', 'receipt boundary')
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
    _equal(static['policy'], 'r5_immutable_archive_boundary_v1', 'archive boundary policy')
    _equal(static['registry_sha256'], v3.boundary.REGISTRY_SHA256, 'archive boundary registry')
    _hash_map(static['archive_static'])
    _hash_map(static['archive_markers'])
    _equal(static['archive_static'], base['archive_static'], 'boundary archive static')
    if type(static['modules']) is not list or any(type(m) is not str for m in static['modules']):
        raise ValueError('malformed archive module labels')
    _equal(static['modules'], sorted(['go.mod', *[r['path'] + '/go.mod' for r in v3.inventory.CURRENT_REPLACEMENTS],
                                     *static['archive_markers']]), 'archive module topology')
    _equal(static['compile_roots'], ['./...'], 'compile roots')
    _equal(static['explicit_production_roots'], ['./cmd/...', './internal/...'], 'explicit roots')
    _equal(static['budgets'], dict(entries=100000, depth=64, bytes=64 * 1024 * 1024), 'archive budgets')
    _hash_map(static['production_go'])
    for path, digest in static['production_go'].items():
        if not path.endswith('.go'):
            raise ValueError('non-Go static production file')
        _local_module(posixpath.dirname(path), v3)
        _equal(digest, base['files'].get(path), 'static production/source hash')
    directories = static['production_variant_directories']
    if type(directories) is not list or not directories or any(type(d) is not str for d in directories):
        raise ValueError('invalid variant directories')
    for directory in directories:
        _relative(directory)
    _equal(directories, sorted({posixpath.dirname(p) for p in static['production_go']}), 'variant directories')
    _equal(receipt['production_coverage'].get('all_variant_directories'), directories, 'coverage directories')
    _source_records(base, v3)
    _selected_records(receipt, source_root, v3)
    _classifier_contract(receipt, source_root, v3)
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
