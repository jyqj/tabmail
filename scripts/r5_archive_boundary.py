"""Exact immutable archive boundary v1. Linux descriptor-bound, no Go execution.

Enumerate the entire working tree, including ignored/excluded outputs, without
following links or opening unclassified bodies. A module never prunes the walk.
.git alone is VCS metadata. Bounds are fail-closed observation limits, not atomicity.
"""
from __future__ import annotations
import hashlib
import os
from pathlib import Path
import re
import stat
import r5_source_inventory as inventory

REGISTRY = 'scripts/contracts/r5-archive-boundary-v1.json'
REGISTRY_SHA256 = '8e04081e1f2b9d9b82b854954b56a1ac71047646f8768d1b7a7ff89af223bb15'
_IMPLEMENTATION_SHA256 = hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
BASELINE = '41b015c30c66b3ba58a3c1395e8559ebcd27a65f'
MAX_ENTRIES = 100000
MAX_DEPTH = 64
MAX_BYTES = 64 * 1024 * 1024
BUILD_SUFFIXES = ('.go', '.c', '.cc', '.cpp', '.cxx', '.h', '.m', '.f', '.f90', '.s', '.S', '.syso', '.swig', '.swigcxx')


def identity(s):
    return stat.S_IFMT(s.st_mode), s.st_dev, s.st_ino, s.st_size, s.st_mtime_ns, s.st_ctime_ns


def root_fd(root):
    root = Path(root).absolute()
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    fd = os.open('/',flags)
    try:
        for component in root.parts[1:]:
            before = os.stat(component,dir_fd=fd,follow_symlinks=False)
            if not stat.S_ISDIR(before.st_mode):
                raise ValueError('symlink/non-directory boundary ancestor')
            child = os.open(component,flags,dir_fd=fd)
            if identity(os.fstat(child)) != identity(before):
                os.close(child)
                raise ValueError('boundary root ancestor identity changed')
            os.close(fd);fd=child
        return fd
    except BaseException:
        os.close(fd);raise


def read(root, name):
    if (type(name) is not str or not name or '\\' in name or '\x00' in name
            or name.startswith('/') or any(p in ('', '.', '..') for p in name.split('/'))):
        raise ValueError('noncanonical boundary path')
    fd = root_fd(root)
    try:
        for part in name.split('/')[:-1]:
            child = os.open(part, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
            os.close(fd); fd = child
        leaf = name.split('/')[-1]
        expected = os.stat(leaf, dir_fd=fd, follow_symlinks=False)
        handle = os.open(leaf, os.O_PATH | os.O_NOFOLLOW, dir_fd=fd)
        try:
            if not stat.S_ISREG(expected.st_mode) or identity(os.fstat(handle)) != identity(expected):
                raise ValueError('nonregular or changed boundary file')
            if expected.st_size > MAX_BYTES:
                raise ValueError('boundary byte budget exceeded')
            # Reopen the already identity-bound regular object, never the
            # mutable leaf name: a leaf swapped to a device/FIFO cannot gain I/O.
            body = os.open('/proc/self/fd/'+str(handle), os.O_RDONLY | os.O_NONBLOCK)
            try:
                if identity(os.fstat(body)) != identity(expected):
                    raise ValueError('boundary read identity mismatch')
                with os.fdopen(os.dup(body), 'rb') as stream:
                    raw = stream.read(MAX_BYTES + 1)
                if len(raw) > MAX_BYTES or identity(os.fstat(body)) != identity(expected):
                    raise ValueError('boundary bytes changed during read')
            finally:
                os.close(body)
            closing = os.open(leaf, os.O_PATH | os.O_NOFOLLOW, dir_fd=fd)
            try:
                if identity(os.fstat(closing)) != identity(expected):
                    raise ValueError('boundary path changed during read')
            finally:
                os.close(closing)
            return raw
        finally:
            os.close(handle)
    except OSError as error:
        raise ValueError('boundary descriptor unavailable') from error
    finally:
        os.close(fd)


def topology(root):
    """Return names/types only; budget shared by all subtrees, excluded or not."""
    files, directories = set(), set(); budget = [0]
    def visit(fd, prefix, depth):
        if depth > MAX_DEPTH:
            raise ValueError('boundary depth budget exceeded')
        with os.scandir(fd) as entries:
            names = sorted(entry.name for entry in entries)
        for name in names:
            if not prefix and name == '.git':
                vcs = os.stat(name,dir_fd=fd,follow_symlinks=False)
                if not (stat.S_ISDIR(vcs.st_mode) or stat.S_ISREG(vcs.st_mode)):
                    raise ValueError('symlink/special VCS metadata boundary')
                continue
            budget[0] += 1
            if budget[0] > MAX_ENTRIES:
                raise ValueError('boundary entry budget exceeded')
            rel = prefix + name
            before = os.stat(name, dir_fd=fd, follow_symlinks=False)
            if not (stat.S_ISDIR(before.st_mode) or stat.S_ISREG(before.st_mode)):
                raise ValueError('symlink/special boundary entry: ' + rel)
            handle = os.open(name, os.O_PATH | os.O_NOFOLLOW, dir_fd=fd)
            try:
                if identity(os.fstat(handle)) != identity(before):
                    raise ValueError('boundary metadata identity mismatch')
                if stat.S_ISDIR(before.st_mode):
                    directories.add(rel)
                    child = os.open(name, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW, dir_fd=fd)
                    try:
                        if identity(os.fstat(child)) != identity(before):
                            raise ValueError('boundary directory identity mismatch')
                        visit(child, rel + '/', depth + 1)
                    finally:
                        os.close(child)
                else:
                    files.add(rel)
                if identity(os.stat(name, dir_fd=fd, follow_symlinks=False)) != identity(before):
                    raise ValueError('boundary metadata changed')
                closing = os.open(name, os.O_PATH | os.O_NOFOLLOW, dir_fd=fd)
                try:
                    if identity(os.fstat(closing)) != identity(before):
                        raise ValueError('boundary closing identity mismatch')
                finally:
                    os.close(closing)
            finally:
                os.close(handle)
    fd = root_fd(root)
    try:
        visit(fd, '', 0)
    except OSError as error:
        raise ValueError('boundary metadata unavailable') from error
    finally:
        os.close(fd)
    return files, directories


def registry(root):
    raw = read(root, REGISTRY)
    # This exact version is authorized by baseline Git blobs, never by mutable
    # selected metadata or a registry that can redefine its own authority.
    if hashlib.sha256(raw).hexdigest() != REGISTRY_SHA256:
        raise ValueError('registry version/bytes differ')
    document = inventory.strict_json(raw)
    if set(document) != {'schema_version','policy','baseline_commit','modules','historical_files','protected_go_files','production_roots','allowed_new_control_files'} or document['baseline_commit'] != BASELINE:
        raise ValueError('registry schema differs')
    return document


def check(root):
    root = Path(root).absolute()
    contract = registry(root)
    files, dirs = topology(root)
    archives = {str(Path(m['path']).parent) for m in contract['modules']}
    historical = {f['path']: f for f in contract['historical_files']}
    markers = {m['path']: m for m in contract['modules']}
    modules = {'go.mod', *(r['path']+'/go.mod' for r in inventory.CURRENT_REPLACEMENTS), *markers}
    if {p for p in files if Path(p).name == 'go.mod'} != modules:
        raise ValueError('unknown/missing nested module topology')
    archive_family = 'docs/company-mail/evidence/R5-MIME-PREPARSE-SOURCE'
    if {p for p in files if p.startswith(archive_family)} != set(historical)|set(markers):
        raise ValueError('archive family added/missing file')
    family_dirs = {str(parent) for p in set(historical)|set(markers) for parent in Path(p).parents if str(parent).startswith(archive_family)}
    if {p for p in dirs if p.startswith(archive_family)} != family_dirs:
        raise ValueError('archive family added/missing directory')
    sums = {'go.sum', *(r['path']+'/go.sum' for r in inventory.CURRENT_REPLACEMENTS)}
    if {p for p in files if Path(p).name=='go.sum'} != sums:
        raise ValueError('unknown/missing module sums')
    for p in files | dirs:
        if Path(p).name in {'vendor','go.work','go.work.sum'} or (Path(p).name.startswith('go.work') and Path(p).name != 'go.work.example'):
            raise ValueError('workspace/vendor boundary rejected')
    for archive in archives:
        expected = {p for p in historical if p.startswith(archive+'/')} | {archive+'/go.mod'}
        actual = {p for p in files if p.startswith(archive+'/')}
        expected_dirs = {str(parent) for p in expected for parent in Path(p).parents if str(parent).startswith(archive)}
        if actual != expected or {p for p in dirs if p == archive or p.startswith(archive+'/')} != expected_dirs:
            raise ValueError('archive added/missing/renamed input')
    archive_hashes = {}
    for p, item in historical.items():
        raw = read(root, p); digest = hashlib.sha256(raw).hexdigest()
        if len(raw) != item['size'] or digest != item['sha256']:
            raise ValueError('historical bytes changed: '+p)
        archive_hashes[p] = digest
    for p, item in markers.items():
        raw = read(root, p)
        if raw != ('module '+item['module']+'\n\ngo 1.25.7\n').encode() or hashlib.sha256(raw).hexdigest() != item['sha256']:
            raise ValueError('archive marker bytes differ')
    replacements = inventory._module_binding(root, _reader=read)
    roots = contract['production_roots'] + [r['path'] for r in replacements]
    def owned(p):
        return any(p.startswith(prefix+'/') for prefix in roots)
    go_files = {p for p in files if p.endswith('.go')}
    protected = set(contract['protected_go_files'])
    production = go_files - protected
    if any(not owned(p) or any(part in inventory.EXCLUDED_DIRS for part in Path(p).parts) for p in production):
        raise ValueError('unclassified production Go source')
    for p in files:
        if p.endswith(BUILD_SUFFIXES) and p not in protected and (not owned(p) or any(part in inventory.EXCLUDED_DIRS for part in Path(p).parts)):
            raise ValueError('unclassified native/build input: '+p)
    hashes = {}
    for p in sorted(production):
        raw = read(root, p); hashes[p] = hashlib.sha256(raw).hexdigest(); text = raw.decode()
        for imported in inventory._local_imports(text):
            if imported.startswith('tabmail/archive/') or any(a in imported for a in archives):
                raise ValueError('production import points to archive')
        for directive in re.findall(r'^\s*//go:(?:embed|generate)\s+(.+)$', text, re.M):
            if any(a in directive or Path(a).name in directive for a in archives) or 'tabmail/archive' in directive:
                raise ValueError('production directive points to archive')
    if hashlib.sha256(read(root, REGISTRY)).hexdigest() != REGISTRY_SHA256:
        raise ValueError('registry changed during check')
    if (files, dirs) != topology(root):
        raise ValueError('boundary topology changed during check')
    for p, h in {**archive_hashes, **hashes, **{p:m['sha256'] for p,m in markers.items()}}.items():
        if hashlib.sha256(read(root,p)).hexdigest() != h:
            raise ValueError('boundary source changed during check')
    return dict(policy=contract['policy'], registry_sha256=REGISTRY_SHA256,
                modules=sorted(modules), archive_static=archive_hashes,
                archive_markers={p:m['sha256'] for p,m in markers.items()},
                production_go=hashes, production_variant_directories=sorted({str(Path(p).parent) for p in production}),
                compile_roots=['./...'], explicit_production_roots=['./cmd/...','./internal/...'],
                budgets=dict(entries=MAX_ENTRIES,depth=MAX_DEPTH,bytes=MAX_BYTES))


def main():
    import argparse
    parser = argparse.ArgumentParser(description=__doc__); parser.add_argument('--root',type=Path,required=True)
    args = parser.parse_args()
    print(inventory.canonical(check(args.root)).decode())

if __name__ == '__main__':
    main()
