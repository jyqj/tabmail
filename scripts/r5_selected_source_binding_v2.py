"""Independent archive selected attestation v2; historical identities remain separate.

Only live, fixed root metadata commands authorize selection. No import API accepts
metadata as a file-read capability. External/generated/native bytes remain unknown.
"""
from __future__ import annotations
import hashlib
import json
import os
import re
from pathlib import Path
import subprocess
import r5_source_inventory as inventory
import r5_archive_boundary as boundary

_IMPLEMENTATION_SHA256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
POLICY = 'r5_root_selected_local_archive_attestation_v2'
ARGV = ['list', '-mod=readonly', '-deps', '-test', '-race', '-tags=r5protocol',
        '-json', './...', 'github.com/jhillyerd/enmime/v2/...', 'github.com/emersion/go-smtp/...']
MVS_ARGV = ['list', '-mod=readonly', '-m', '-json', 'all']
CONTEXT = dict(goos='linux', goarch='amd64', cgo_enabled=1, race=True,
               build_tag_sets=[['r5protocol']], go_work='off', go_flags='',
               selection='all_local_variants_superset')
DEFAULT_CONTEXT = dict(CONTEXT, race=False, build_tag_sets=[[]])

def selection_argv(context, patterns):
    result = ['list','-mod=readonly','-deps','-test']
    if context['race']:
        result.append('-race')
    tags = context['build_tag_sets']
    if len(tags) != 1:
        raise ValueError('one actual command tag context required')
    if tags[0]:
        result.append('-tags='+','.join(tags[0]))
    return result + ['-json', *patterns]


def package_identity(rows, root):
    result = []
    for row in rows:
        if row.get('Error') or row.get('DepsErrors'):
            raise ValueError('coverage metadata error')
        d = Path(row.get('Dir',''))
        if d.is_relative_to(root):
            module = row.get('Module',{})
            fork = next((r for r in inventory.CURRENT_REPLACEMENTS if d == root/r['path'] or d.is_relative_to(root/r['path'])),None)
            if fork:
                replace = module.get('Replace',{})
                if module.get('Path') != fork['module'] or module.get('Version') != fork['version'] or replace.get('Path') != './'+fork['path'] or Path(replace.get('Dir','')) != root/fork['path']:
                    raise ValueError('unexpected fork in production coverage')
                continue
            if module.get('Path') != 'tabmail' or not module.get('Main') or Path(module.get('Dir','')) != root:
                raise ValueError('unexpected local package in production coverage')
            rel = d.relative_to(root).as_posix()
            if not any(rel == p or rel.startswith(p+'/') for p in ('cmd','internal')):
                raise ValueError('unknown main production root')
            result.append(dict(directory=rel,import_path=row['ImportPath'],for_test=row.get('ForTest'),
                module='tabmail', fields={f:row[f] for f in FIELDS if f in row}))
    return sorted(result,key=lambda r:inventory.canonical(r))


def compare_coverage(root_rows, explicit_rows, root, static, local):
    a,b = package_identity(root_rows,root),package_identity(explicit_rows,root)
    if a != b:
        raise ValueError('root/explicit production package coverage differs')
    # All selected inputs must come from the independent static superset.
    selected_go = {p for p in local if p.endswith('.go')}
    if not selected_go.issubset(static['production_go']):
        raise ValueError('selected Go outside production variant superset')
    return dict(root_packages=a,explicit_packages=b,no_missing=True,no_overlap=True,
                all_variant_directories=static['production_variant_directories'],
                other_contexts='static_only_not_executed')

STATIC_EVIDENCE = frozenset(['docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/app/messages/mime_budget_boundary_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/app/messages/service.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/mime_budget.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/mime_budget_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/after/internal/mailcontent/parser.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/app/messages/mime_budget_boundary_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/app/messages/service.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/mime_budget.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/mime_budget_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V2/before/internal/mailcontent/parser.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/after/internal/mailcontent/mime_budget.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/after/internal/mailcontent/mime_budget_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/before/internal/mailcontent/mime_budget.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE-V3/before/internal/mailcontent/mime_budget_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/app/messages/mime_budget_boundary_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/app/messages/service.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/mime_budget.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/mime_budget_test.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/after/internal/mailcontent/parser.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before/internal/app/messages/service.go', 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE/before/internal/mailcontent/parser.go'])
FIELDS = ('GoFiles', 'CgoFiles', 'TestGoFiles', 'XTestGoFiles', 'EmbedFiles',
          'TestEmbedFiles', 'XTestEmbedFiles', 'CFiles', 'CXXFiles', 'MFiles',
          'HFiles', 'FFiles', 'SFiles', 'SwigFiles', 'SwigCXXFiles', 'SysoFiles')
NATIVE_FIELDS = frozenset(FIELDS[7:])

def stream(raw):
    decoder = json.JSONDecoder(object_pairs_hook=lambda pairs: _unique(pairs))
    text = raw.decode(); rows = []
    while text.strip():
        text = text.lstrip(); row, end = decoder.raw_decode(text)
        if not isinstance(row, dict):
            raise ValueError('metadata object required')
        rows.append(row); text = text[end:]
    if not rows:
        raise ValueError('empty metadata')
    return rows


def _unique(pairs):
    result = {}
    for key, value in pairs:
        if key in result:
            raise ValueError('duplicate metadata key')
        result[key] = value
    return result


def digest(root, names):
    return {name:hashlib.sha256(boundary.read(root,name)).hexdigest() for name in sorted(names)}


def topology(root):
    observed = boundary.check(root)
    return set(observed['production_go'])


def normalize_mvs(rows, root):
    result = []; seen = set(); replacements = []
    for row in rows:
        name = row.get('Path')
        if not isinstance(name, str) or name in seen or row.get('Error'):
            raise ValueError('invalid root MVS')
        seen.add(name)
        entry = {k:row[k] for k in ('Path','Version','Main','GoVersion','Sum','GoModSum') if k in row}
        if row.get('Main'):
            if name != 'tabmail' or Path(row.get('Dir','')) != root:
                raise ValueError('wrong root MVS identity')
        if 'Replace' in row:
            replace = row['Replace']; matched = [r for r in inventory.CURRENT_REPLACEMENTS if r['module']==name and r['version']==row.get('Version')]
            if len(matched)!=1 or replace.get('Path')!='./'+matched[0]['path'] or Path(replace.get('Dir','')) != root/matched[0]['path'] or replace.get('Version'):
                raise ValueError('wrong fork or third replacement in root MVS')
            entry['Replace'] = dict(matched[0]); replacements.append(matched[0])
        result.append(entry)
    if len(replacements)!=2 or not any(r.get('Main') for r in result):
        raise ValueError('missing root or replacements')
    return sorted(result, key=lambda r:r['Path'])


def base_markers(root):
    return [m['path'] for m in boundary.registry(root)['modules']]


def classify(rows, root, authorized, envinfo, mvs):
    local = {}; generated = []; external = []; native = []; toolchain = []
    modules = {r['Path']:r for r in mvs}
    for row in rows:
        if row.get('Error') or row.get('DepsErrors'):
            raise ValueError('Go selected error')
        directory = Path(row.get('Dir',''))
        if not directory.is_absolute() or '..' in directory.parts:
            raise ValueError('noncanonical package path')
        if directory.is_relative_to(root):
            relative = directory.relative_to(root).as_posix()
            expected = next((r for r in inventory.CURRENT_REPLACEMENTS if relative == r['path'] or relative.startswith(r['path']+'/')), None)
            module = row.get('Module',{})
            if expected:
                replace = module.get('Replace',{})
                if module.get('Path')!=expected['module'] or module.get('Version')!=expected['version'] or replace.get('Path')!='./'+expected['path'] or Path(replace.get('Dir',''))!=root/expected['path']:
                    raise ValueError('wrong selected fork identity')
            elif module.get('Path')!='tabmail' or not module.get('Main') or Path(module.get('Dir',''))!=root:
                raise ValueError('wrong selected main module identity')
        if directory.is_relative_to(root) and any(str(directory.relative_to(root)).startswith(str(Path(p).parent)+'/') for p in base_markers(root)):
            raise ValueError('archive package selected')
        generated_main = row.get('Name')=='main' and row.get('ImportPath','').endswith('.test')
        for field in FIELDS:
            values = row.get(field, [])
            if type(values) is not list or len(values)!=len(set(values)):
                raise ValueError('invalid selected file list')
            for name in values:
                if type(name) is not str:
                    raise ValueError('invalid selected filename')
                if field in FIELDS[:4] and not name.endswith('.go') and not generated_main:
                    raise ValueError('false Go source path')
                path = Path(name) if Path(name).is_absolute() else directory/name
                if '..' in path.parts or '\\' in name:
                    raise ValueError('noncanonical selected path')
                if path.is_relative_to(root):
                    rel = path.relative_to(root).as_posix()
                    if rel not in authorized:
                        raise ValueError('unclassified local Go input: '+rel)
                    local.setdefault(rel,set()).add(field)
                    if field in NATIVE_FIELDS:
                        native.append(dict(package=row['ImportPath'],field=field,path=rel,qualification='unknown'))
                elif generated_main:
                    if field!='GoFiles' or not Path(name).is_absolute() or not path.is_relative_to(Path(envinfo['GOCACHE'])) or not path.name.endswith('-d'):
                        raise ValueError('invalid generated testmain path')
                    generated.append(dict(package=row['ImportPath'],field=field,path=path.relative_to(Path(envinfo['GOCACHE'])).as_posix(),classification='generated_testmain',qualification='unknown'))
                elif row.get('Standard'):
                    if not path.is_relative_to(Path(envinfo['GOROOT'])/'src'):
                        raise ValueError('false standard path')
                    entry = dict(package=row['ImportPath'],field=field,path=path.relative_to(Path(envinfo['GOROOT'])).as_posix(),classification='toolchain_source',qualification='unknown')
                    (native if field in NATIVE_FIELDS else toolchain).append(entry)
                else:
                    module = row.get('Module',{}); bound = modules.get(module.get('Path'))
                    if not bound or 'Replace' in bound or module.get('Version')!=bound.get('Version') or not path.is_relative_to(Path(envinfo['GOMODCACHE'])):
                        raise ValueError('unclassified external input')
                    module_dir = Path(module.get('Dir',''))
                    if not module_dir.is_absolute() or not module_dir.is_relative_to(Path(envinfo['GOMODCACHE'])) or not path.is_relative_to(module_dir):
                        raise ValueError('false external module directory')
                    entry = dict(package=row['ImportPath'],field=field,module=module['Path'],version=module['Version'],path=path.relative_to(Path(envinfo['GOMODCACHE'])).as_posix(),classification='external_modulecache',qualification='unknown')
                    (native if field in NATIVE_FIELDS else external).append(entry)
        if row.get('CompiledGoFiles'):
            raise ValueError('unexpected compiled input metadata')
    return {p:sorted(fields) for p,fields in sorted(local.items())}, generated, external, native, toolchain


def capture(root, go, *, cache, modulecache, context=None):
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
    authorized = set(base['files'])
    archive_static = base['archive_static']
    before_go = topology(root)
    if not before_go.issubset(authorized):
        raise ValueError('unclassified repository Go source')
    before = digest(root, authorized)
    if before.get('scripts/r5_selected_source_binding_v2.py')!=_IMPLEMENTATION_SHA256:
        raise ValueError('selected binding helper differs from source')
    # Clean environment: never inherit private service settings or ABI/flags.
    env = dict(PATH=os.environ.get('PATH','/usr/bin:/bin'), HOME=os.environ.get('HOME','/tmp'),
               GODEBUG='asynctimerchan=0', GOWORK='off', GOENV='off', GOTOOLCHAIN='local',
               GOFLAGS='', GOOS=context['goos'], GOARCH=context['goarch'], CGO_ENABLED=str(context['cgo_enabled']),
               GOPROXY='https://proxy.golang.org', GOSUMDB='sum.golang.org', GOPATH=str(Path(modulecache).parent),
               GOMODCACHE=str(Path(modulecache).resolve()), GOCACHE=str(Path(cache).resolve()))
    commands = []
    def run(argv):
        result = subprocess.run([str(go),*argv],cwd=root,env={**env,**{k:os.environ[k] for k in ('HTTPS_PROXY','HTTP_PROXY','ALL_PROXY','NO_PROXY') if k in os.environ}},capture_output=True,timeout=180)
        commands.append(dict(argv=[str(go),*argv],exit=result.returncode,stdout_sha256=hashlib.sha256(result.stdout).hexdigest(),stderr=result.stderr.decode()))
        if result.returncode:
            raise ValueError('metadata command failed (stamping unchanged): '+result.stderr.decode())
        return result.stdout
    envinfo = json.loads(run(['env','-json']))
    # go env reports GOENV='' when disabled; GOGCCFLAGS has a per-query temp prefix.
    envinfo['GOGCCFLAGS'] = re.sub(r'/tmp/go-build[0-9]+=', '/tmp/go-build<TEMP>=', envinfo['GOGCCFLAGS'])
    commands[-1]['stdout_sha256'] = hashlib.sha256(inventory.canonical(envinfo)).hexdigest()
    commands[-1]['stdout_hash_domain'] = 'canonical_go_env_with_only_GOGCCFLAGS_temp_prefix_normalized'
    required = dict(GOVERSION='go1.25.7', GOOS='linux',GOARCH='amd64',GOHOSTOS='linux',GOHOSTARCH='amd64',CGO_ENABLED='1',GOWORK='off',GOENV='',GOFLAGS='',GOEXPERIMENT='',GOAMD64='v1',GOTOOLCHAIN='local')
    if any(envinfo.get(k)!=v for k,v in required.items()):
        raise ValueError('wrong Go version/flags/ABI')
    # Selection may download test dependencies and hydrate MVS Dir fields.
    # Bind both observations only after that hydration, retaining raw hashes.
    run(argv)
    commands.pop()  # dependency hydration is not formal evidence or identity
    selected_raw = run(argv)
    rows = stream(selected_raw)
    mvs_raw = run(MVS_ARGV)
    mvs = normalize_mvs(stream(mvs_raw),root)
    local, generated, external, native, toolchain = classify(rows,root,authorized,envinfo,mvs)
    admitted_embeds = {path for paths in base['embed_inputs'].values() for path in paths}
    if any(set(fields) & set(FIELDS[4:7]) and path not in admitted_embeds for path,fields in local.items()):
        raise ValueError('selected embed outside declared static directives')
    root_raw = run(selection_argv(context, ['./...']))
    explicit_raw = run(selection_argv(context, ['./cmd/...','./internal/...']))
    coverage = compare_coverage(stream(root_raw),stream(explicit_raw),root,base['archive_boundary'],local)
    fresh = digest(root,local)
    after = inventory.capture_current_source(root,purpose='selected',policy=inventory.ARCHIVE_POLICY,build_context=context)
    if base!=after or before_go!=topology(root) or before!=digest(root,authorized) or any(before[p]!=h for p,h in fresh.items()):
        raise ValueError('source/hash/topology changed during capture')
    repeated_selected_raw = run(argv)
    repeated_mvs_raw = run(MVS_ARGV)
    if selected_raw!=repeated_selected_raw or mvs_raw!=repeated_mvs_raw or root_raw!=run(selection_argv(context,['./...'])) or explicit_raw!=run(selection_argv(context,['./cmd/...','./internal/...'])):
        raise ValueError('selected metadata changed during capture')
    if before!=digest(root,authorized) or before_go!=topology(root):
        raise ValueError('source changed after final metadata')
    payload = dict(schema_version=2,policy=POLICY,incompatible_with='Historical inventory v2/v3, selected v1 and stable capture revision 2 do not authorize archive v2',
                   observation_order='env; unbound hydration; selected; MVS; coverage; selected; MVS; repeated coverage (raw equality)', base_source=base, archive_static=archive_static, production_coverage=coverage, environment=env,go_env=envinfo,commands=commands,root_mvs=mvs,
                   selected_local={p:dict(sha256=fresh[p],fields=fields) for p,fields in local.items()},
                   generated_testmain=generated,external_modulecache_inputs=external,native_inputs=native,toolchain_source_inputs=toolchain,
                   package_records=len(rows), qualification=dict(overall='blocked',excluded_metadata_boundary='descriptor_checked',local_static_binding='captured',generated='unknown',external_modulecache='unknown',compiler_native='unknown',Method19='unknown',SML='unknown',wholeCI='unknown'),
                   boundary='Metadata/static local bytes only; no product tests, cold build or runtime qualification')
    return dict(payload,attestation_sha256=hashlib.sha256(inventory.canonical(payload)).hexdigest())


def validate(receipt, root, go, *, cache, modulecache):
    if type(receipt) is not dict or receipt.get('policy')!=POLICY or type(receipt.get('schema_version')) is not int or receipt.get('schema_version')!=2:
        raise ValueError('explicit incompatible selected attestation required')
    observed = capture(root,go,cache=cache,modulecache=modulecache,context=receipt.get('base_source',{}).get('build_context'))
    def identity(value):
        value = dict(value)
        value.pop('attestation_sha256',None)
        value['commands'] = [{k:v for k,v in command.items() if k!='stderr'} for command in value['commands']]
        return inventory.canonical(value)
    payload = {k:v for k,v in receipt.items() if k!='attestation_sha256'}
    if receipt.get('attestation_sha256')!=hashlib.sha256(inventory.canonical(payload)).hexdigest() or identity(receipt)!=identity(observed):
        raise ValueError('selected attestation missing/extra/drifted/context mismatch')
    return observed


def main():
    import argparse
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--root',type=Path,required=True); parser.add_argument('--go',type=Path,required=True)
    parser.add_argument('--cache',type=Path,required=True); parser.add_argument('--modulecache',type=Path,required=True)
    parser.add_argument('--validate',type=Path)
    parser.add_argument('--context',choices=['default','race-r5protocol'],default='race-r5protocol')
    args=parser.parse_args()
    if args.validate:
        result=validate(inventory.strict_json(args.validate.read_bytes()),args.root,args.go,cache=args.cache,modulecache=args.modulecache)
    else:
        result=capture(args.root,args.go,cache=args.cache,modulecache=args.modulecache,context=DEFAULT_CONTEXT if args.context=='default' else CONTEXT)
    print(inventory.canonical(result).decode())

if __name__=='__main__':
    main()
