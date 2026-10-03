"""Versioned, fail-closed repository-local input inventory (no Go invocation).

This is a directory superset, NOT go list's selected compiler inputs, module
cache/toolchain/native-library attestation, or a cross-platform runtime result.
All build-tag variants are captured; only the declared context is admitted.
Historical files-only identities must never be computed through this policy.
"""
from __future__ import annotations
import hashlib
import json
import os
from pathlib import Path
import re
import shlex
import stat

POLICY = 'r5_current_local_inputs_v2'
KIND = 'policy_bound_local_input_superset_sha1_v2'
# v2 is retained only as its exact historical one-fork inventory domain.
CURRENT_POLICY = 'r5_current_local_inputs_smtp_owner_forks_v3'
CURRENT_KIND = 'policy_bound_local_input_superset_smtp_owner_sha1_v3'
CURRENT_SCHEMA_VERSION = 3
SUPPORTED_POLICIES = (POLICY, CURRENT_POLICY)
_IMPLEMENTATION_SHA256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
REPLACEMENT = {'module': 'github.com/jhillyerd/enmime/v2', 'version': 'v2.3.0',
               'path': 'third_party/enmime-v2.3.0'}
SMTP_REPLACEMENT = {'module': 'github.com/emersion/go-smtp', 'version': 'v0.24.0',
                    'path': 'third_party/go-smtp'}
CURRENT_REPLACEMENTS = (REPLACEMENT, SMTP_REPLACEMENT)
MODULE_METADATA = {
    REPLACEMENT['module']: ('LICENSE', 'PROVENANCE.json', 'UPSTREAM-MANIFEST.json', 'UPSTREAM-DELTA.patch', 'TABMAIL-FORK.md'),
    SMTP_REPLACEMENT['module']: ('LICENSE', 'LOCAL-PROVENANCE.md', 'LOCAL-SOURCE-MANIFEST.json',
                                 'UPSTREAM-MANIFEST.json', 'LOCAL-PATCH.diff', 'LOCAL-REVIEW-FIX.diff'),
}
CURRENT_BOUNDARY = ('Declared main-module input superset with exactly enmime/v2 v2.3.0 and '
                    'go-smtp v0.24.0 local owner forks, including full local module trees, '
                    'mod/sum/provenance/license and embed inputs; effective root MVS, Go-selected '
                    'compiler inputs, external module cache/toolchain/native libraries and '
                    'cross-platform runtime remain independently unqualified')
BOUNDARY = ('Complete declared repository-local directory input superset; includes all local '
            'replacement files and embed assets, not Go-selected inputs, external module cache, '
            'toolchain, native libraries, installed npm dependencies, or cross-platform execution')
EXCLUDED_DIRS = {'.git', 'node_modules', '.next', '.vercel', 'coverage', '__pycache__',
                 '.pytest_cache', '.mypy_cache', '.cache', 'out', 'build', 'dist'}
# Shared across all excluded subtrees of each exact local fork, per inventory
# pass. Artifact bytes and names do not enter SOURCE; these limits do.
EXCLUDED_METADATA_MAX_ENTRIES = 4096
EXCLUDED_METADATA_MAX_DEPTH = 32
ROOT_FILES = {'go.mod', 'go.sum', 'Dockerfile', 'Makefile', '.env.example', '.gitignore'}
FIXED = {
    'protocol': {'docs/company-mail/R5-PROTOCOL.md', 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json'},
    'benchmark': {'docs/company-mail/evidence/R5-BENCHMARK-DATASETS.json'},
}
TAGS = {'r5benchmark', 'r5protocol', 'r5audit', 'r5fixtures', 'r5tenantcascade'}
OS = {'darwin', 'linux', 'windows', 'freebsd', 'openbsd', 'netbsd', 'dragonfly', 'solaris', 'illumos', 'aix', 'android', 'ios', 'plan9', 'js', 'wasip1'}
ARCH = {'amd64', 'arm64', '386', 'arm', 'ppc64', 'ppc64le', 'mips', 'mipsle', 'mips64', 'mips64le', 'riscv64', 's390x', 'wasm', 'loong64'}


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(',', ':')).encode()


def strict_json(raw):
    def unique(items):
        out = {}
        for key, value in items:
            if key in out:
                raise ValueError('duplicate source receipt key: ' + key)
            out[key] = value
        return out
    return json.loads(raw, object_pairs_hook=unique)


def checked_path(root, name, *, directory=False):
    if (type(name) is not str or not name or '\x00' in name or '\\' in name
            or name.startswith('/') or any(p in {'', '.', '..'} for p in name.split('/'))):
        raise ValueError('noncanonical local source path')
    path = root / name
    for component in [path, *path.parents]:
        if component.is_symlink():
            raise ValueError('symlinked local source input: ' + name)
        if component == root:
            break
    if not path.resolve().is_relative_to(root) or not (path.is_dir() if directory else path.is_file()):
        raise ValueError('missing or escaped local source input: ' + name)
    return path


def validate_context(context, purpose):
    keys = {'goos', 'goarch', 'cgo_enabled', 'build_tag_sets', 'race', 'go_work', 'go_flags', 'selection'}
    if type(context) is not dict or set(context) != keys:
        raise ValueError('explicit exact build context required')
    tag_sets = context['build_tag_sets']
    if (type(tag_sets) is not list or not tag_sets or any(type(tags) is not list or any(type(tag) is not str for tag in tags) or tags != sorted(set(tags)) or not set(tags).issubset(TAGS) for tags in tag_sets)
            or tag_sets != sorted(tag_sets) or len({tuple(tags) for tags in tag_sets}) != len(tag_sets)):
        raise ValueError('exact supported command build tag sets required')
    if (type(context['goos']) is not str or type(context['goarch']) is not str
            or context['goos'] not in OS or context['goarch'] not in ARCH
            or type(context['cgo_enabled']) is not int or context['cgo_enabled'] not in {0, 1}
            or type(context['race']) is not bool or context['go_work'] != 'off'
            or context['go_flags'] != '' or context['selection'] != 'all_local_variants_superset'):
        raise ValueError('unsupported dynamic build context')
    if purpose == 'benchmark' and (tag_sets != [['r5benchmark']] or context['race']):
        raise ValueError('benchmark requires exact r5benchmark non-race context')
    if purpose == 'protocol' and (not context['race'] or context['cgo_enabled'] != 1):
        raise ValueError('protocol runner requires explicit race/cgo context')
    return context


def bound_environment(context, environment):
    """Pin supported switches; reject unaccounted compiler/overlay injection."""
    for key in ('GOFLAGS', 'GOWORK', 'GOTOOLCHAIN', 'GOROOT', 'GOPATH', 'GOEXPERIMENT',
                'GOAMD64', 'GOARM', 'GOARM64', 'GOMIPS', 'GOMIPS64', 'GOPPC64', 'GORISCV64',
                'CGO_CFLAGS', 'CGO_CPPFLAGS', 'CGO_CXXFLAGS', 'CGO_FFLAGS', 'CGO_LDFLAGS', 'CC', 'CXX'):
        if environment.get(key) and not (key == 'GOWORK' and environment[key] == 'off'):
            raise ValueError('unbound build environment override: ' + key)
    env = dict(environment)
    env.update(GOOS=context['goos'], GOARCH=context['goarch'], CGO_ENABLED=str(context['cgo_enabled']),
               GOFLAGS='', GOWORK='off', GOENV='off')
    return env


def _check_excluded_metadata(root, path, budget):
    """Inspect types/names only; never open a regular file or follow a link.

    Descriptor-relative opens also reject a directory replaced with a symlink
    between stat and descent. Unsupported platforms and unreadable/raced entries
    fail closed. Iteration is streaming, bounded before the next stat/descent.
    """
    def visit(parent_fd, name, depth):
        budget[0] += 1
        if budget[0] > EXCLUDED_METADATA_MAX_ENTRIES:
            raise ValueError('excluded metadata entry budget exceeded')
        if depth > EXCLUDED_METADATA_MAX_DEPTH:
            raise ValueError('excluded metadata depth budget exceeded')
        mode = os.stat(name, dir_fd=parent_fd, follow_symlinks=False).st_mode
        if stat.S_ISLNK(mode):
            raise ValueError('symlinked excluded inventory entry')
        if name == 'go.mod':
            raise ValueError('unknown nested module in excluded directory')
        if stat.S_ISREG(mode):
            return
        if not stat.S_ISDIR(mode):
            raise ValueError('special or unknown excluded inventory entry')
        fd = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW,
                     dir_fd=parent_fd)
        try:
            with os.scandir(fd) as entries:
                for entry in entries:
                    visit(fd, entry.name, depth + 1)
        finally:
            os.close(fd)
    try:
        # Resolve each repository-relative ancestor through directory FDs so
        # aliases introduced in an ancestor cannot redirect metadata descent.
        flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
        fd = os.open(root, flags)
        try:
            for component in path.relative_to(root).parts[:-1]:
                child_fd = os.open(component, flags, dir_fd=fd)
                os.close(fd)
                fd = child_fd
            visit(fd, path.name, 1)
        finally:
            os.close(fd)
    except (OSError, AttributeError, NotImplementedError) as error:
        raise ValueError('excluded metadata unavailable or changed') from error


def _walk(root, directory, *, complete_local_module=False):
    base = checked_path(root, directory, directory=True)
    names = set()
    metadata_budget = [0]
    def fail(error):
        raise error
    for current, dirs, files in os.walk(base, followlinks=False, onerror=fail):
        # Even an excluded output alias must not bypass the symlink boundary.
        for entry in dirs + files:
            if (Path(current) / entry).is_symlink():
                raise ValueError('symlinked inventory entry')
        if complete_local_module:
            for entry in dirs:
                if entry in EXCLUDED_DIRS:
                    _check_excluded_metadata(root, Path(current) / entry, metadata_budget)
        dirs[:] = sorted(d for d in dirs if d not in EXCLUDED_DIRS)
        for filename in files:
            if filename == '.DS_Store':
                continue
            if filename.startswith('.env') and filename not in {'.env.example', '.envrc'}:
                if complete_local_module:
                    raise ValueError('unclassified environment input in local module')
                continue
            if not complete_local_module and (filename.endswith(('.log', '.tsbuildinfo', '.pyc')) or filename == 'next-env.d.ts'):
                continue
            name = (Path(current) / filename).relative_to(root).as_posix()
            checked_path(root, name)
            names.add(name)
    return names


def _legacy_module_binding_v2(root):
    text = checked_path(root, 'go.mod').read_text()
    clean = re.sub(r'//[^\n]*', '', text)
    expected = 'replace ' + REPLACEMENT['module'] + ' ' + REPLACEMENT['version'] + ' => ./' + REPLACEMENT['path']
    lines = [line.strip() for line in clean.splitlines() if line.strip()]
    if 'module tabmail' not in lines:
        raise ValueError('unexpected main module identity')
    if [line for line in lines if re.match(r'replace\b', line)] != [expected]:
        raise ValueError('only exact versioned local replacement is admitted')
    if not re.search(r'^\s*(?:require\s+)?' + re.escape(REPLACEMENT['module']) + r'\s+' + re.escape(REPLACEMENT['version']) + r'\s*$', clean, re.M):
        raise ValueError('replacement must match required module version')
    fork = checked_path(root, REPLACEMENT['path'] + '/go.mod').read_text()
    if not re.search(r'^module ' + re.escape(REPLACEMENT['module']) + r'\s*$', fork, re.M) or re.search(r'^\s*replace\b', fork, re.M):
        raise ValueError('replacement module identity or nested replacement differs')
    for name in ('go.sum', 'PROVENANCE.json', 'UPSTREAM-MANIFEST.json', 'UPSTREAM-DELTA.patch', 'LICENSE'):
        checked_path(root, REPLACEMENT['path'] + '/' + name)
    return dict(REPLACEMENT)


def _local_imports(text):
    # Tokenize comments and literals together so an import hidden behind a
    # parenthesis in a comment cannot escape the admitted local-root check.
    tokens = re.findall(r'//[^\n]*|/\*[\s\S]*?\*/|"(?:\\.|[^"\\])*"|`[^`]*`|[A-Za-z_][A-Za-z_0-9]*|[^\s]', text)
    tokens = [token for token in tokens if not token.startswith(('//', '/*'))]
    imports = []
    for index, token in enumerate(tokens):
        if token != 'import':
            continue
        index += 1
        block = index < len(tokens) and tokens[index] == '('
        if block:
            index += 1
        while index < len(tokens) and (not block or tokens[index] != ')'):
            if tokens[index] == ';' and block:
                index += 1
                continue
            if re.fullmatch(r'[A-Za-z_][A-Za-z_0-9]*|\.', tokens[index]):
                index += 1
            if index >= len(tokens) or not re.fullmatch(r'"[^"\\]*"', tokens[index]):
                raise ValueError('unsupported escaped/raw/dynamic import grammar')
            imports.append(tokens[index][1:-1])
            index += 1
            if not block:
                break
        if block and (index >= len(tokens) or tokens[index] != ')'):
            raise ValueError('unterminated import declaration')
    return imports


def _check_go_inputs(root, names, *, replacements=None):
    embeds = {}
    admitted = (REPLACEMENT,) if replacements is None else replacements
    module_files = {item['path'] + '/go.mod' for item in admitted}
    for name in sorted(names):
        if name.endswith('/go.mod') and name not in module_files:
            raise ValueError('unknown nested module: ' + name)
        if not name.endswith('.go'):
            continue
        text = (root / name).read_text()
        if re.search(r'(?m)^\s*//go:(?:linkname|cgo_)|^\s*#cgo', text):
            raise ValueError('unbound native/linker input: ' + name)
        # Only local module imports affect this local-source closure. External
        # module bytes remain explicitly outside its certification boundary.
        for imported in _local_imports(text):
            if imported == 'C':
                raise ValueError('unbound native compiler input')
            if imported.startswith(('./', '../', '/')):
                raise ValueError('dynamic/relative local import is not admitted')
            if replacements is not None:
                for local in replacements:
                    prefix = local['module']
                    if imported == prefix or imported.startswith(prefix + '/'):
                        package = local['path'] + imported[len(prefix):]
                        if not any(Path(item).parent.as_posix() == package and item.endswith('.go') for item in names):
                            raise ValueError('local replacement import outside captured module inputs: ' + imported)
            if imported == 'tabmail' or imported.startswith('tabmail/'):
                package = imported.removeprefix('tabmail/').rstrip('/')
                if not any(item.startswith(package + '/') and item.endswith('.go') for item in names):
                    raise ValueError('local import outside declared input roots: ' + imported)
        for expression in re.findall(r'^\s*//(?:go:build| \+build)\s+(.+)$', text, re.M):
            tokens = re.findall(r'[A-Za-z0-9_.]+', expression)
            if any(token not in TAGS | OS | ARCH | {'cgo', 'gc', 'gccgo', 'unix', 'race'} and not re.fullmatch(r'go1\.\d+', token) for token in tokens):
                raise ValueError('unknown conditional build input: ' + name)
        for directive in re.findall(r'^\s*//go:embed\s+(.+)$', text, re.M):
            for pattern in shlex.split(directive):
                if (not pattern or pattern.startswith('/') or '\\' in pattern or ':' in pattern
                        or any(p in {'', '.', '..'} for p in pattern.split('/'))
                        or any(c in pattern for c in '[]?`') or '**' in pattern):
                    raise ValueError('unsupported embed pattern: ' + pattern)
                matched = set()
                for target in (root / name).parent.glob(pattern):
                    rel = target.relative_to(root).as_posix()
                    if target.is_symlink():
                        raise ValueError('symlinked embed input')
                    if target.is_dir():
                        for entry in target.rglob('*'):
                            if entry.is_symlink():
                                raise ValueError('symlinked embedded directory input')
                            if entry.is_file():
                                matched.add(entry.relative_to(root).as_posix())
                    else:
                        matched.add(rel)
                if not matched or not matched.issubset(names):
                    raise ValueError('missing or excluded embed input: ' + name + ':' + pattern)
                embeds[name + ':' + pattern] = sorted(matched)
    return embeds


def _capture_legacy_v2(root, *, purpose, policy, build_context):
    if policy != POLICY or purpose not in FIXED:
        raise ValueError('explicit current source policy/purpose required; no legacy fallback')
    root = Path(root).absolute()
    if any(p.is_symlink() for p in [root, *root.parents]):
        raise ValueError('symlinked snapshot root')
    root = root.resolve()
    validate_context(build_context, purpose)
    if any(root.glob('go.work*')) or (root / 'vendor').exists():
        raise ValueError('workspace/vendor replacement selection is not admitted')
    replacement = _legacy_module_binding_v2(root)
    third_party = checked_path(root, 'third_party', directory=True)
    if sorted(p.name for p in third_party.iterdir()) != ['enmime-v2.3.0']:
        raise ValueError('unknown extra local replacement directory')
    names = set(ROOT_FILES) | FIXED[purpose]
    for directory in ('cmd', 'internal', 'web', 'scripts', REPLACEMENT['path']):
        names.update(_walk(root, directory))
    for directory in ('.github', 'deploy', 'configs'):
        if (root / directory).exists():
            names.update(_walk(root, directory))
    names.update(p.name for p in root.glob('docker-compose*.yml'))
    # The policy implementation and binding regression suite are self-bound.
    names.update({'scripts/r5_source_inventory.py', 'scripts/tests/test_r5_current_source_inventory.py'})
    if purpose == 'protocol':
        cases = strict_json(checked_path(root, 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').read_bytes())
        for row in cases.get('cases', []):
            for adapter in row.get('reference_adapters', []) + row.get('shared_adapters', []):
                for field in ('source', 'component_source'):
                    if adapter.get(field) and adapter[field] not in names:
                        raise ValueError('protocol adapter outside declared local inventory')
    files = {name: hashlib.sha256(checked_path(root, name).read_bytes()).hexdigest() for name in sorted(names)}
    payload = {'policy': policy, 'purpose': purpose, 'build_context': build_context,
               'replacement': replacement, 'boundary': BOUNDARY,
               'excluded_directories': sorted(EXCLUDED_DIRS), 'embed_inputs': _check_go_inputs(root, names), 'files': files}
    wire = canonical(payload)
    return {'schema_version': 2, 'snapshot_root': str(root), 'source_identity_kind': KIND,
            'source_sha': hashlib.sha1(wire).hexdigest(), 'source_closure_sha256': hashlib.sha256(wire).hexdigest(),
            **payload}


def _module_document(root, name):
    """Bounded static Go-module grammar, not an effective MVS resolver."""
    text = checked_path(root, name).read_text()
    if '/*' in text or '*/' in text:
        raise ValueError('unsupported module comment grammar: ' + name)
    result = {'module': None, 'go': None, 'toolchain': None, 'requires': {}, 'replace_lines': []}
    block = None
    for raw in text.splitlines():
        line = raw.split('//', 1)[0].strip()
        if not line:
            continue
        tokens = line.split()
        if block is not None:
            if line == ')':
                block = None
                continue
            directive, values = block, tokens
        else:
            directive, values = tokens[0], tokens[1:]
            if values == ['(']:
                if directive != 'require':
                    raise ValueError('unsupported dynamic module directive/block: ' + name)
                block = directive
                continue
        if directive == 'require':
            if len(values) != 2 or not re.fullmatch(r'[A-Za-z0-9][A-Za-z0-9._~+/-]*', values[0]) or any(part in {'', '.', '..'} for part in values[0].split('/')) or not re.fullmatch(r'v[0-9]+\.[0-9]+\.[0-9]+(?:-[A-Za-z0-9._-]+)?(?:\+incompatible)?', values[1]):
                raise ValueError('unsupported module requirement: ' + name)
            if values[0] in result['requires']:
                raise ValueError('duplicate module requirement: ' + name)
            result['requires'][values[0]] = values[1]
        elif directive == 'replace':
            if len(values) != 4 or values[2] != '=>':
                raise ValueError('unsupported dynamic replacement declaration: ' + name)
            result['replace_lines'].append('replace ' + ' '.join(values))
        elif directive in {'module', 'go', 'toolchain'}:
            if len(values) != 1 or result[directive] is not None:
                raise ValueError('duplicate/invalid module metadata: ' + name)
            result[directive] = values[0]
        else:
            raise ValueError('unknown module directive (including workspace/MVS overrides): ' + name)
    if block is not None or result['module'] is None or result['go'] is None or not re.fullmatch(r'[0-9]+\.[0-9]+(?:\.[0-9]+)?', result['go']):
        raise ValueError('complete canonical module/go declarations required: ' + name)
    if result['toolchain'] is not None and not re.fullmatch(r'go[0-9]+\.[0-9]+(?:\.[0-9]+)?', result['toolchain']):
        raise ValueError('unsupported dynamic toolchain declaration: ' + name)
    result['requires'] = dict(sorted(result['requires'].items()))
    return result


def _module_binding(root, *, policy=CURRENT_POLICY):
    if policy == POLICY:
        return _legacy_module_binding_v2(root)
    if policy != CURRENT_POLICY:
        raise ValueError('unknown module binding policy; no inferred upgrade')
    main = _module_document(root, 'go.mod')
    expected = {'replace '+item['module']+' '+item['version']+' => ./'+item['path'] for item in CURRENT_REPLACEMENTS}
    if main['module'] != 'tabmail' or len(main['replace_lines']) != 2 or set(main['replace_lines']) != expected:
        raise ValueError('v3 requires exactly the two pinned version/path replacements; no third or dynamic replace')
    for item in CURRENT_REPLACEMENTS:
        if main['requires'].get(item['module']) != item['version']:
            raise ValueError('root required local module version differs from exact replacement')
        nested = _module_document(root, item['path']+'/go.mod')
        if nested['module'] != item['module'] or nested['replace_lines']:
            raise ValueError('local module identity or nested replacement differs')
        for known in CURRENT_REPLACEMENTS:
            declared = nested['requires'].get(known['module'])
            if declared is not None and declared != known['version']:
                raise ValueError('local declaration selects an unadmitted local module version')
        checked_path(root, item['path']+'/go.sum')
        for member in MODULE_METADATA[item['module']]:
            checked_path(root, item['path']+'/'+member)
    return [dict(item) for item in CURRENT_REPLACEMENTS]


def _current_source_paths(root, purpose, replacements):
    third_party = checked_path(root, 'third_party', directory=True)
    if sorted(item.name for item in third_party.iterdir()) != sorted(Path(item['path']).name for item in replacements):
        raise ValueError('v3 unknown extra local module entry')
    names = set(ROOT_FILES) | FIXED[purpose]
    for directory in ('cmd', 'internal', 'web', 'scripts'):
        names.update(_walk(root, directory))
    for item in replacements:
        names.update(_walk(root, item['path'], complete_local_module=True))
    for directory in ('.github', 'deploy', 'configs'):
        if (root/directory).exists() or (root/directory).is_symlink():
            names.update(_walk(root, directory))
    names.update(path.name for path in root.glob('docker-compose*.yml'))
    names.update({'scripts/r5_source_inventory.py', 'scripts/tests/test_r5_smtp_owner_source_inventory.py'})
    return names


def _upstream_file_inventory(root, replacement, names):
    path = replacement['path']+'/UPSTREAM-MANIFEST.json'
    manifest = strict_json(checked_path(root, path).read_bytes())
    if type(manifest) is not dict or manifest.get('module') != replacement['module'] or manifest.get('version') != replacement['version']:
        raise ValueError('local upstream manifest module/version differs')
    field = 'files' if replacement['module'] == REPLACEMENT['module'] else 'files_sha256'
    entries = manifest.get(field)
    if type(entries) is not dict or not entries:
        raise ValueError('complete upstream filename inventory required')
    for name, info in entries.items():
        if type(name) is not str:
            raise ValueError('noncanonical upstream filename')
        actual = checked_path(root, replacement['path']+'/'+name)
        if field == 'files' and type(info) is not dict:
            raise ValueError('upstream file size/hash object required')
        digest = info.get('sha256') if field == 'files' else info
        if type(digest) is not str or not re.fullmatch('[0-9a-f]{64}', digest):
            raise ValueError('upstream file hash metadata malformed')
        if field == 'files' and (type(info.get('bytes')) is not int or info['bytes'] < 0):
            raise ValueError('upstream file size metadata malformed')
        if actual.relative_to(root).as_posix() not in names:
            raise ValueError('upstream module file excluded from full local inventory')
    # Modified upstream files intentionally differ. This records coverage, not
    # an invented re-verification of upstream signatures or patch correctness.
    return sorted(entries)


def _capture_smtp_owner_v3(root, *, purpose, policy, build_context):
    if policy != CURRENT_POLICY or purpose not in FIXED:
        raise ValueError('explicit v3 policy/purpose required; no legacy fallback')
    root = Path(root).absolute()
    if any(path.is_symlink() for path in [root, *root.parents]):
        raise ValueError('symlinked snapshot root')
    root = root.resolve()
    validate_context(build_context, purpose)
    if any(root.glob('go.work*')) or (root/'vendor').exists() or (root/'vendor').is_symlink():
        raise ValueError('workspace/vendor replacement selection is not admitted')
    replacements = _module_binding(root, policy=policy)
    names = _current_source_paths(root, purpose, replacements)
    upstream = {item['module']:_upstream_file_inventory(root, item, names) for item in replacements}
    if purpose == 'protocol':
        cases = strict_json(checked_path(root, 'docs/company-mail/evidence/R5-PROTOCOL-CASES.json').read_bytes())
        for row in cases.get('cases', []):
            for adapter in row.get('reference_adapters', []) + row.get('shared_adapters', []):
                for field in ('source', 'component_source'):
                    if adapter.get(field) and adapter[field] not in names:
                        raise ValueError('protocol adapter outside declared local inventory')
    files = {name:hashlib.sha256(checked_path(root, name).read_bytes()).hexdigest() for name in sorted(names)}
    if files['scripts/r5_source_inventory.py'] != _IMPLEMENTATION_SHA256:
        raise ValueError('v3 inventory implementation differs from snapshot helper bytes')
    documents = {'go.mod':_module_document(root, 'go.mod')}
    local_modules = []
    for item in replacements:
        name = item['path']+'/go.mod';documents[name] = _module_document(root, name)
        local_modules.append({**item, 'declared_go':documents[name]['go'], 'declared_toolchain':documents[name]['toolchain'],
                              'declared_requires':documents[name]['requires'], 'go_mod_sha256':files[name],
                              'go_sum_sha256':files[item['path']+'/go.sum'],
                              'metadata_sha256':{member:files[item['path']+'/'+member] for member in MODULE_METADATA[item['module']]},
                              'upstream_file_inventory':upstream[item['module']]})
    # Detect changed topology or module declarations while taking the snapshot.
    if replacements != _module_binding(root, policy=policy) or names != _current_source_paths(root, purpose, replacements):
        raise ValueError('local module/source inventory changed during capture')
    metadata_names = {'go.mod','go.sum','scripts/r5_source_inventory.py'}
    for item in replacements:
        metadata_names.update(item['path']+'/'+name for name in ('go.mod','go.sum',*MODULE_METADATA[item['module']]))
    if any(hashlib.sha256(checked_path(root, name).read_bytes()).hexdigest() != files[name] for name in metadata_names):
        raise ValueError('module or provenance bytes changed during capture')
    resolution = {'main_module':'tabmail', 'root_go_mod_sha256':files['go.mod'], 'root_go_sum_sha256':files['go.sum'],
                  'declared_go':documents['go.mod']['go'], 'declared_toolchain':documents['go.mod']['toolchain'],
                  'declared_requires':documents['go.mod']['requires'], 'local_modules':local_modules,
                  'local_module_file_selection':'all_regular_files_not_suffix_filtered_except_explicit_artifact_directories_and_DS_Store',
                  'excluded_local_module_metadata':{
                      'inspection':'names_and_lstat_types_only_no_regular_file_reads_no_symlink_following',
                      'reject':['symlinks','nested_go.mod','special_or_unknown_types','unavailable_metadata','budget_exceeded'],
                      'max_entries_per_local_module_per_pass':EXCLUDED_METADATA_MAX_ENTRIES,
                      'max_depth_including_excluded_root':EXCLUDED_METADATA_MAX_DEPTH},
                  'effective_root_MVS_observed':False, 'go_selected_inputs_observed':False,
                  'external_module_cache_verified':False,
                  'boundary':'Declared root/local module metadata and complete local input superset only; actual root MVS/go-list/build/external-cache qualification is separate'}
    payload = {'policy':policy, 'purpose':purpose, 'build_context':build_context, 'replacements':replacements,
               'module_resolution':resolution, 'inventory_implementation_sha256':_IMPLEMENTATION_SHA256, 'boundary':CURRENT_BOUNDARY, 'excluded_directories':sorted(EXCLUDED_DIRS),
               'embed_inputs':_check_go_inputs(root, names, replacements=replacements), 'files':files}
    wire = canonical(payload)
    return {'schema_version':CURRENT_SCHEMA_VERSION, 'snapshot_root':str(root), 'source_identity_kind':CURRENT_KIND,
            'source_sha':hashlib.sha1(wire).hexdigest(), 'source_closure_sha256':hashlib.sha256(wire).hexdigest(), **payload}


def capture_current_source(root, *, purpose, policy, build_context):
    if policy == POLICY:
        return _capture_legacy_v2(root, purpose=purpose, policy=policy, build_context=build_context)
    if policy == CURRENT_POLICY:
        return _capture_smtp_owner_v3(root, purpose=purpose, policy=policy, build_context=build_context)
    raise ValueError('explicit known source policy required; no auto-upgrade or fallback')


def validate_current_source(receipt, root, *, purpose, policy):
    version = 2 if policy == POLICY else CURRENT_SCHEMA_VERSION if policy == CURRENT_POLICY else None
    if version is None or type(receipt) is not dict or type(receipt.get('schema_version')) is not int or receipt.get('schema_version') != version:
        raise ValueError('explicit policy/receipt version match required; no legacy downgrade')
    observed = capture_current_source(root, purpose=purpose, policy=policy, build_context=receipt.get('build_context'))
    differs = canonical(receipt) != canonical(observed) if policy == CURRENT_POLICY else receipt != observed
    if differs:
        raise ValueError('current source receipt policy/context/files missing, extra, or drifted')
    return observed


def load_current_source(path, pinned_sha256, root, *, purpose, policy):
    if type(pinned_sha256) is not str or not re.fullmatch('[0-9a-f]{64}', pinned_sha256):
        raise ValueError('explicit current source receipt byte hash required')
    raw = Path(path).read_bytes()
    if hashlib.sha256(raw).hexdigest() != pinned_sha256:
        raise ValueError('current source receipt byte hash differs')
    return validate_current_source(strict_json(raw), root, purpose=purpose, policy=policy)


def main(argv=None):
    """SOURCE-only capture/validate entry point; no build or runtime admission."""
    import argparse
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=('capture', 'validate'))
    parser.add_argument('--root', type=Path, required=True)
    parser.add_argument('--purpose', choices=sorted(FIXED), required=True)
    parser.add_argument('--policy', choices=SUPPORTED_POLICIES, required=True)
    parser.add_argument('--build-context', help='exact build context JSON for capture')
    parser.add_argument('--receipt', type=Path)
    parser.add_argument('--receipt-sha256')
    args = parser.parse_args(argv)
    try:
        if args.action == 'capture':
            if args.build_context is None or args.receipt is not None or args.receipt_sha256 is not None:
                raise ValueError('capture requires only explicit build context')
            receipt = capture_current_source(args.root, purpose=args.purpose, policy=args.policy,
                                             build_context=strict_json(args.build_context))
        else:
            if args.build_context is not None or args.receipt is None or args.receipt_sha256 is None:
                raise ValueError('validate requires only receipt and explicit byte hash')
            receipt = load_current_source(args.receipt, args.receipt_sha256, args.root,
                                          purpose=args.purpose, policy=args.policy)
        print(canonical(receipt).decode())
        return 0
    except (ValueError, OSError) as error:
        print(json.dumps({'error': str(error)}))
        return 1


if __name__ == '__main__':
    raise SystemExit(main())
