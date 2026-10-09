"""Read-only preparation inventory; never authorizes a component runtime.

Candidate external dependency contract v1. No source/runner/archive-policy edits,
installation, subprocesses, or package imports. Installed bytes are observations,
not a claim that npm lifecycle outputs have independent registry provenance.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
from pathlib import Path
import stat
from urllib.parse import urlparse

POLICY = "r5_external_dependency_preparation_candidate_v1"
MAX_ENTRIES = 150000
MAX_BYTES = 1024 * 1024 * 1024


def canonical(value):
    return json.dumps(value, sort_keys=True, separators=(",", ":")).encode()


def plain_root(value):
    root = Path(value).absolute()
    for part in [*reversed(root.parents), root]:
        info = part.lstat()
        if not stat.S_ISDIR(info.st_mode) or part.is_symlink():
            raise ValueError("non-directory/symlink ancestor")
    if root.stat().st_uid != os.getuid():
        raise ValueError("dependency root must be owned by current uid")
    return root


def identity(info):
    return (info.st_dev, info.st_ino, info.st_mode, info.st_size,
            info.st_mtime_ns, info.st_ctime_ns)


def read_regular(path):
    before = path.lstat()
    if not stat.S_ISREG(before.st_mode) or before.st_size > MAX_BYTES:
        raise ValueError("nonregular/oversize input")
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    try:
        if identity(os.fstat(fd)) != identity(before):
            raise ValueError("input identity changed")
        with os.fdopen(os.dup(fd), "rb") as stream:
            raw = stream.read(MAX_BYTES + 1)
        if len(raw) != before.st_size or identity(os.fstat(fd)) != identity(before):
            raise ValueError("input changed during read")
        if identity(path.lstat()) != identity(before):
            raise ValueError("input path changed during read")
        return raw
    finally:
        os.close(fd)


def check_lock(raw):
    lock = json.loads(raw)
    if lock.get("lockfileVersion") != 3 or not isinstance(lock.get("packages"), dict):
        raise ValueError("expected exact npm lock v3")
    packages = lock["packages"]
    for name, row in packages.items():
        if not name:
            continue
        if not name.startswith("node_modules/") or any(p in ("", ".", "..") for p in name.split("/")) or row.get("link"):
            raise ValueError("unsupported package path/link")
        if row.get("inBundle"):
            # Bundled children are covered by their nearest locked registry parent.
            parents = [p for p in packages if p and name.startswith(p + "/node_modules/") and packages[p].get("resolved")]
            if not parents or row.get("resolved") or row.get("integrity"):
                raise ValueError("unbound bundled package")
            continue
        url = urlparse(row.get("resolved", ""))
        if url.scheme != "https" or url.netloc != "registry.npmjs.org" or url.query or url.fragment or not row.get("integrity", "").startswith("sha512-"):
            raise ValueError("nonofficial/missing-integrity package")
    return lock


def snapshot(root):
    """No followed directory links; only exact installed package bin targets.

    This is a bounded candidate observer, not the future production descriptor
    lease. Runtime adoption additionally needs descriptor-based resolver binding.
    """
    root = plain_root(root)
    files, directories, links = {}, [], {}
    total = 0
    def walk(directory):
        nonlocal total
        before = identity(directory.lstat())
        for path in sorted(directory.iterdir()):
            rel = path.relative_to(root).as_posix()
            info = path.lstat()
            if info.st_uid != os.getuid():
                raise ValueError("foreign-owned dependency entry")
            if stat.S_ISDIR(info.st_mode):
                directories.append(rel)
                walk(path)
            elif stat.S_ISREG(info.st_mode):
                total += info.st_size
                if total > MAX_BYTES:
                    raise ValueError("dependency byte budget exceeded")
                raw = read_regular(path)
                files[rel] = dict(sha256=hashlib.sha256(raw).hexdigest(), size=len(raw), mode=stat.S_IMODE(info.st_mode))
            elif stat.S_ISLNK(info.st_mode):
                target = os.readlink(path)
                resolved = path.resolve(strict=True)
                if Path(target).is_absolute() or not resolved.is_relative_to(root / "node_modules") or not resolved.is_file():
                    raise ValueError("escaping/nonfile dependency symlink")
                links[rel] = dict(target=target, resolved=resolved.relative_to(root).as_posix())
                if identity(path.lstat()) != identity(info):
                    raise ValueError("link changed during observation")
            else:
                raise ValueError("special dependency entry")
            if len(files) + len(directories) + len(links) > MAX_ENTRIES:
                raise ValueError("dependency entry budget exceeded")
        if identity(directory.lstat()) != before:
            raise ValueError("directory changed during observation")
    walk(root / "node_modules")
    declared_bins = {}
    lock = check_lock(read_regular(root / "package-lock.json"))
    for package_root, row in lock["packages"].items():
        name = package_root + "/package.json"
        if package_root and name in files:
            package = json.loads(read_regular(root / name))
            if package.get("version") != row.get("version"):
                raise ValueError("installed package version differs from lock")
            bins = package.get("bin", {})
            if isinstance(bins, str):
                bins = {package["name"].split("/")[-1]: bins}
            if not isinstance(bins, dict):
                raise ValueError("invalid installed bin metadata")
            for command, target in bins.items():
                candidate = (root / name).parent / target
                resolved = candidate.resolve()
                if resolved.is_relative_to(root / "node_modules"):
                    package_directory = (root / name).parent
                    modules = package_directory.parent
                    if modules.name.startswith("@"):
                        modules = modules.parent
                    link_name = (modules / ".bin" / command).relative_to(root).as_posix()
                    declared_bins.setdefault(link_name, set()).add(resolved.relative_to(root).as_posix())
    for name, link in links.items():
        path = Path(name)
        if path.parent.name != ".bin" or link["resolved"] not in declared_bins.get(name, set()):
            raise ValueError("undeclared dependency symlink")
    return dict(files=files, directories=directories, symlinks=links,
                total_regular_bytes=total)


def observe(source, dependency_root):
    source, root = plain_root(source), plain_root(dependency_root)
    if root == source or root.is_relative_to(source):
        raise ValueError("dependencies must be outside source")
    bindings = {}
    for name in ("package.json", "package-lock.json"):
        expected = read_regular(source / "web" / name)
        observed = read_regular(root / name)
        if expected != observed:
            raise ValueError("source/installed package lock identity differs")
        bindings[name] = hashlib.sha256(expected).hexdigest()
    lock = check_lock(read_regular(root / "package-lock.json"))
    first = snapshot(root)
    if first != snapshot(root):
        raise ValueError("dependency inventory changed")
    payload = dict(schema_version=1, policy=POLICY, status="PREPARED_CANDIDATE_UNADOPTED",
                   source_root=str(source), dependency_root=str(root), package_bindings=bindings,
                   lock_package_records=len(lock["packages"]), installed=first,
                   actual_component_started=False, product_green=False, task_complete=False,
                   registry_provenance="npm-ci-lock-integrity; lifecycle outputs observed only",
                   boundary="Preparation bytes only; no resolver/runtime/source-policy authorization")
    return dict(payload, manifest_sha256=hashlib.sha256(canonical(payload)).hexdigest())


def validate(receipt, source, dependency_root):
    payload = {k: v for k, v in receipt.items() if k != "manifest_sha256"}
    if receipt.get("manifest_sha256") != hashlib.sha256(canonical(payload)).hexdigest():
        raise ValueError("candidate manifest hash differs")
    if receipt != observe(source, dependency_root):
        raise ValueError("candidate inventory/identity drift")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--source", required=True, type=Path)
    parser.add_argument("--dependency-root", required=True, type=Path)
    args = parser.parse_args()
    print(canonical(observe(args.source, args.dependency_root)).decode())
