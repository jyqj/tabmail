"""Independent scoped runtime candidate; never upgrades archive/selected qualification.

Trust boundary: one executor's physically isolated workspace and official locked
packages. Descriptor checks detect observed drift, not transient hostile writes.
No chmod/chown, source-local dependency links, or NODE_PATH resolution tricks.
"""
from __future__ import annotations
import argparse
import contextlib
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import stat
import subprocess
import sys

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
import r5_source_inventory as source_inventory
import r5_selected_source_binding_v2 as selected
import r5_external_dependencies as preparation

POLICY = 'r5_external_dependency_runtime_v2'
BOUNDARY = 'not_qualified_for_hostile_concurrent_mutation'

def canonical(value):
    return source_inventory.canonical(value)

def digest(raw):
    return hashlib.sha256(raw).hexdigest()

def clean_environment(environment=None):
    environment = os.environ if environment is None else environment
    for key in ('NODE_OPTIONS', 'NODE_PATH'):
        if environment.get(key):
            raise ValueError('polluted runtime environment: ' + key)
    for key in environment:
        if key.lower().startswith('npm_config_') and environment[key]:
            raise ValueError('unbound npm environment: ' + key)

class Descriptors:
    """Anchor ancestors and read every entry with directory-relative NOFOLLOW."""
    def __init__(self):
        self.fds = []
        self.anchors = {}
    def close(self):
        for fd in reversed(self.fds):
            os.close(fd)
        self.fds = []
    def directory(self, path):
        path = Path(path)
        if not path.is_absolute() or '..' in path.parts or str(path) != os.path.abspath(path):
            raise ValueError('canonical absolute directory required')
        fd = os.open('/', os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        self.fds.append(fd)
        current = Path('/')
        self.anchors[str(current)] = os.fstat(fd)
        for name in path.parts[1:]:
            before = os.stat(name, dir_fd=fd, follow_symlinks=False)
            child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            self.fds.append(child)
            if preparation.identity(before) != preparation.identity(os.fstat(child)):
                raise ValueError('directory binding changed')
            fd = child
            current = current / name
            self.anchors[str(current)] = os.fstat(fd)
        return fd
    def read(self, parent, name):
        before = os.stat(name, dir_fd=parent, follow_symlinks=False)
        if not stat.S_ISREG(before.st_mode):
            raise ValueError('regular file required')
        fd = os.open(name, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK, dir_fd=parent)
        try:
            if preparation.identity(before) != preparation.identity(os.fstat(fd)):
                raise ValueError('file binding changed')
            with os.fdopen(os.dup(fd), 'rb') as stream:
                raw = stream.read(preparation.MAX_BYTES + 1)
            if len(raw) != before.st_size or preparation.identity(before) != preparation.identity(os.fstat(fd)) or preparation.identity(before) != preparation.identity(os.stat(name, dir_fd=parent, follow_symlinks=False)):
                raise ValueError('file changed during read')
            return raw
        finally:
            os.close(fd)
    def file(self, path):
        path = Path(path)
        return self.read(self.directory(path.parent), path.name)
    def tree(self, root, *, source=False):
        rootfd = self.directory(root)
        if os.fstat(rootfd).st_uid != os.getuid():
            raise ValueError('foreign owned root')
        result = {}
        total = 0
        def visit(fd, prefix):
            nonlocal total
            before = preparation.identity(os.fstat(fd))
            for name in sorted(os.listdir(fd)):
                rel = prefix + name
                info = os.stat(name, dir_fd=fd, follow_symlinks=False)
                if source and name in ('.git', '__pycache__'):
                    continue
                if source and name == 'node_modules':
                    raise ValueError('shadow source node_modules')
                row = dict(mode=stat.S_IMODE(info.st_mode),device=info.st_dev,inode=info.st_ino,uid=info.st_uid)
                if info.st_uid != os.getuid():
                    raise ValueError('foreign owned entry')
                if stat.S_ISDIR(info.st_mode):
                    row['type'] = 'directory'
                    child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                    try:
                        if preparation.identity(info) != preparation.identity(os.fstat(child)):
                            raise ValueError('directory changed')
                        visit(child, rel + '/')
                    finally:
                        os.close(child)
                elif stat.S_ISREG(info.st_mode):
                    raw = self.read(fd, name)
                    total += len(raw)
                    if total > preparation.MAX_BYTES:
                        raise ValueError('tree size budget exceeded')
                    row.update(type='file', sha256=digest(raw), size=len(raw))
                elif stat.S_ISLNK(info.st_mode) and not source:
                    row.update(type='link', target=os.readlink(name, dir_fd=fd))
                    if preparation.identity(info) != preparation.identity(os.stat(name, dir_fd=fd, follow_symlinks=False)):
                        raise ValueError('link changed')
                else:
                    raise ValueError('source link or special entry')
                result[rel] = row
                if len(result) > preparation.MAX_ENTRIES:
                    raise ValueError('tree entry budget exceeded')
            if preparation.identity(os.fstat(fd)) != before:
                raise ValueError('directory changed during walk')
        visit(rootfd, '')
        return result
    def binding(self, path):
        info = os.fstat(self.directory(path))
        if info.st_uid != os.getuid():
            raise ValueError('foreign owned root')
        ancestors = {str(p):dict(device=self.anchors[str(p)].st_dev,inode=self.anchors[str(p)].st_ino,uid=self.anchors[str(p)].st_uid,mode=stat.S_IMODE(self.anchors[str(p)].st_mode)) for p in [*Path(path).parents,Path(path)]}
        return dict(path=str(path), device=info.st_dev, inode=info.st_ino, uid=info.st_uid, mode=stat.S_IMODE(info.st_mode),ancestors=ancestors)

@contextlib.contextmanager
def descriptors():
    value = Descriptors()
    try:
        yield value
    finally:
        value.close()

def dependency_records(tree, lock, *, dependency_root=None):
    declared = {}
    packages = lock['packages']
    missing = []
    for name, row in packages.items():
        if not name:
            continue
        key = name.removeprefix('node_modules/') + '/package.json'
        if key not in tree:
            if not row.get('optional'):
                raise ValueError('missing nonoptional locked package: ' + name)
            missing.append(name)
            continue
        # All installed package metadata must be a regular bound file.
        if dependency_root is None:
            raise ValueError("explicit dependency root required")
        root = Path(dependency_root) / name
        with descriptors() as files:
            package = source_inventory.strict_json(files.file(root / 'package.json'))
        if package.get('version') != row.get('version'):
            raise ValueError('installed lock version differs')
        bins = package.get('bin', {})
        if isinstance(bins, str):
            bins = {package['name'].split('/')[-1]: bins}
        modules = root.parent
        if modules.name.startswith('@'):
            modules = modules.parent
        for command, target in bins.items():
            resolved = os.path.normpath(str(root / target))
            relative = os.path.relpath(resolved, Path(dependency_root) / 'node_modules')
            if relative.startswith('../') or tree.get(relative, {}).get('type') != 'file':
                raise ValueError('unknown bin executable')
            key = (modules / '.bin' / command).relative_to(Path(dependency_root) / 'node_modules').as_posix()
            declared.setdefault(key, set()).add(relative)
    for name, row in tree.items():
        if row['type'] == 'link':
            resolved = os.path.normpath(str(Path(name).parent / row['target']))
            if Path(row['target']).is_absolute() or resolved not in declared.get(name, set()) or tree.get(resolved, {}).get('type') != 'file':
                raise ValueError('escaping or undeclared dependency link')
        if name.endswith('/package.json'):
            package_root = 'node_modules/' + name.removesuffix('/package.json')
            if re.search(r'(?:^|/)node_modules/(?:@[^/]+/)?[^/]+$', package_root) and package_root not in packages:
                raise ValueError('unknown installed package root')
    return dict(locked=packages, absent_optional=missing,
                sri='official lock SRI verified by lifecycle-enabled npm ci; installed outputs are observed bytes',
                lifecycle_outputs='not independently attributed to registry tarball members')


def observe(source, root, archive, default, race, node):
    clean_environment()
    source, root, node = Path(source), Path(root), Path(node)
    if source != root / 'source':
        raise ValueError('exact ownedroot/source layout required')
    for ancestor in root.parents:
        if (ancestor / 'node_modules').exists() or (ancestor / 'node_modules').is_symlink():
            raise ValueError('shadow ancestor node_modules')
    with descriptors() as files:
        receipts = {name:files.file(path) for name,path in [('archive',archive),('default',default),('race',race)]}
        archived = source_inventory.strict_json(receipts['archive'])
        source_inventory.validate_current_source(archived, source, purpose='protocol', policy=source_inventory.ARCHIVE_POLICY)
        if archived.get('build_context') != dict(selected.CONTEXT,build_tag_sets=[[],['r5protocol']]):
            raise ValueError('shared-components archive context must bind both consumer tag sets')
        for name, context in [('default', selected.DEFAULT_CONTEXT),('race',selected.CONTEXT)]:
            receipt = source_inventory.strict_json(receipts[name])
            if receipt.get('policy') != selected.POLICY or receipt.get('base_source',{}).get('build_context') != context or receipt.get('base_source',{}).get('snapshot_root') != str(source):
                raise ValueError('selected context/root differs')
            if receipt['base_source']['files'] != archived['files']:
                raise ValueError('selected/archive source files differ')
        git = str(Path(shutil.which('git')).resolve())
        if not (source/'.git').is_dir() or (source/'.git').is_symlink():
            raise ValueError('real Git checkout required')
        if subprocess.check_output([git,'status','--porcelain','--untracked-files=all'],cwd=source):
            raise ValueError('clean Git source required')
        commit = subprocess.check_output([git,'rev-parse','HEAD'],cwd=source,text=True).strip()
        source_tree = files.tree(source, source=True)
        installed = files.tree(root / 'node_modules')
        package_hashes = {}
        for name in ('package.json','package-lock.json'):
            raw = files.file(root / name)
            if raw != files.file(source / 'web' / name):
                raise ValueError('package lock/source differs')
            package_hashes[name] = digest(raw)
        lock = preparation.check_lock(files.file(root / 'package-lock.json'))
        records = dependency_records(installed, lock, dependency_root=root)
        cli = root / 'node_modules/vitest/vitest.mjs'
        return dict(schema_version=2, policy=POLICY, status='UNADOPTED',
            source=files.binding(source), dependency_root=files.binding(root), git_commit=commit,
            git=dict(path=git,sha256=digest(files.file(Path(git)))),
            source_tree=source_tree, installed=installed, installed_content_root=digest(canonical(installed)),
            receipt_paths=dict(archive=str(archive),default=str(default),race=str(race)),
            receipt_hashes={k:digest(v) for k,v in receipts.items()}, archive_policy=source_inventory.ARCHIVE_POLICY,
            package_hashes=package_hashes, dependency_records=records,
            node=dict(path=str(node),sha256=digest(files.file(node))),
            python=dict(path=str(Path(shutil.which('python3')).resolve()),sha256=digest(files.file(Path(shutil.which('python3')).resolve()))),
            cli=dict(path=str(cli),sha256=digest(files.file(cli))),
            helper_sha256=digest(files.file(Path(__file__).resolve())),
            configs={name:digest(files.file(source/'web'/name)) for name in ('vitest.config.ts','vitest.r5protocol.config.ts','vitest.r5external-probe.config.ts','vitest.setup.ts')},
            platform=dict(os=platform.system(),arch=platform.machine(),node_version=subprocess.check_output([str(node),'--version'],text=True).strip()),
            execution=dict(cwd=str(source/'web'),go_race_seconds=120,process_seconds=180,
                cache_policy=dict(results=False,fs_module_cache=False,optimizer_default_enabled=False,on_disk_runtime_cache_root=None,config_loader='bundle',cli_options=['--cache=false','--experimental.fsModuleCache=false']),
                probe_argv=['run','--cache=false','--experimental.fsModuleCache=false','--config','vitest.r5external-probe.config.ts','--reporter=json','--outputFile','<fresh-report>'],
                python_argv=['run','--cache=false','--experimental.fsModuleCache=false','components/company/r5-protocol.test.tsx','--reporter=json','--outputFile=<fresh-report>'],
                go_argv=['run','--cache=false','--experimental.fsModuleCache=false','--config','vitest.r5protocol.config.ts','--reporter=json','--outputFile','<fresh-report>']),
            resolution=dict(layout='normal ancestor lookup; no source node_modules',dynamic_loader_coverage='unknown; runtime observations do not enumerate all dynamic resolutions'),
            concurrency_boundary=BOUNDARY,task_complete=False,product_green=False)

def load_pinned(path, pin):
    with descriptors() as files:
        raw = files.file(Path(path))
    if digest(raw) != pin:
        raise ValueError('external manifest byte pin differs')
    manifest = source_inventory.strict_json(raw)
    if manifest.get('policy') != POLICY or manifest.get('status') != 'UNADOPTED':
        raise ValueError('manifest/status promotion forbidden')
    return manifest

def validate(manifest):
    paths = manifest['receipt_paths']
    actual = observe(manifest['source']['path'],manifest['dependency_root']['path'],
                     paths['archive'],paths['default'],paths['race'],manifest['node']['path'])
    if actual != manifest:
        raise ValueError('runtime source/dependency/descriptor/tool drift')

@contextlib.contextmanager
def owner(manifest):
    token = Path(manifest['dependency_root']['path']) / '.r5-runtime-owner'
    fd = os.open(token, os.O_WRONLY | os.O_CREAT | os.O_EXCL | os.O_NOFOLLOW, 0o600)
    nonce = secrets.token_hex(24)
    info = os.fstat(fd)
    try:
        os.write(fd, canonical(dict(pid=os.getpid(),nonce=nonce)))
        info = os.fstat(fd)
        yield nonce
        if preparation.identity(info) != preparation.identity(token.lstat()):
            raise ValueError('exclusive owner token changed')
    finally:
        os.close(fd)
        try:
            terminal = token.lstat()
        except FileNotFoundError:
            terminal = None
        if terminal is not None and (terminal.st_dev,terminal.st_ino)==(info.st_dev,info.st_ino):
            token.unlink()

def launch(manifest, argv, env=None):
    clean_environment(env)
    if argv[:2] != [manifest['node']['path'], manifest['cli']['path']]:
        raise ValueError('unknown executable/CLI')
    tail = argv[2:]
    if not ((len(tail)==6 and tail[:5]==['run','--cache=false','--experimental.fsModuleCache=false','components/company/r5-protocol.test.tsx','--reporter=json'] and tail[5].startswith('--outputFile=')) or
            (len(tail)==8 and tail[:4]==['run','--cache=false','--experimental.fsModuleCache=false','--config'] and tail[4] in ('vitest.r5protocol.config.ts','vitest.r5external-probe.config.ts') and tail[5:7]==['--reporter=json','--outputFile'])):
        raise ValueError('unbound runtime argv')
    report = Path(tail[-1].removeprefix('--outputFile='))
    if not report.is_absolute() or report.exists() or report.is_relative_to(Path(manifest['source']['path'])) or report.is_relative_to(Path(manifest['dependency_root']['path'])/'node_modules'):
        raise ValueError('fresh external report path required')
    with owner(manifest), descriptors() as held:
        held.directory(Path(manifest['source']['path']))
        held.directory(Path(manifest['execution']['cwd']))
        held.directory(Path(manifest['dependency_root']['path']))
        validate(manifest)
        try:
            return subprocess.run(argv,cwd=manifest['execution']['cwd'],env=env,capture_output=True,text=True,timeout=180)
        finally:
            validate(manifest)

def from_environment(source):
    path = os.environ.get('TABMAIL_R5_EXTERNAL_MANIFEST','')
    pin = os.environ.get('TABMAIL_R5_EXTERNAL_MANIFEST_SHA256','')
    if not path or not pin:
        raise ValueError('explicit external runtime manifest and independent pin required')
    manifest = load_pinned(path,pin)
    if manifest['source']['path'] != str(source):
        raise ValueError('external runtime source differs')
    return manifest

if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['capture','validate','launch'])
    parser.add_argument('--source',type=Path,required=True)
    parser.add_argument('--root',type=Path)
    parser.add_argument('--archive',type=Path)
    parser.add_argument('--default',type=Path)
    parser.add_argument('--race',type=Path)
    parser.add_argument('--node',type=Path)
    parser.add_argument('--go',type=Path)
    parser.add_argument('--cache',type=Path)
    parser.add_argument('--modulecache',type=Path)
    split = sys.argv.index('--') if '--' in sys.argv else len(sys.argv)
    args = parser.parse_args(sys.argv[1:split])
    runtime_argv = sys.argv[split+1:]
    try:
        if args.action == 'capture':
            for path in (args.default,args.race):
                selected.validate(source_inventory.strict_json(preparation.read_regular(path)),args.source,args.go,cache=args.cache,modulecache=args.modulecache)
            print(canonical(observe(args.source,args.root,args.archive,args.default,args.race,args.node)).decode())
        else:
            manifest = from_environment(args.source)
            if args.action == 'validate':
                validate(manifest)
                print(canonical(dict(node=manifest['node']['path'],cli=manifest['cli']['path'])).decode())
            else:
                result = launch(manifest,runtime_argv)
                sys.stdout.write(result.stdout); sys.stderr.write(result.stderr)
                raise SystemExit(result.returncode)
    except (OSError,ValueError,subprocess.SubprocessError) as error:
        print('external runtime rejected: '+str(error),file=sys.stderr)
        raise SystemExit(1)
