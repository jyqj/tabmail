#!/usr/bin/env python3
"""Coordinated filesystem/PG snapshot. Never stops services or restores over data.
Requires PG client tools matching the server, and explicit writer-stop acknowledgement.
S3 deployments must use a coordinated versioned object-store snapshot instead.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile


def run(*args: str, capture: bool = False) -> str:
    if args[0] in ("pg_dump", "psql"):
        args = (*args, "--dbname=" + os.environ["PGDATABASE"])
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE if capture else None).stdout or ""


def digest(path: Path) -> str:
    h = hashlib.sha256()
    with path.open("rb") as f:
        for part in iter(lambda: f.read(1024 * 1024), b""):
            h.update(part)
    return h.hexdigest()


def main() -> None:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("operation", choices=["backup", "restore"])
    parser.add_argument("snapshot", type=Path)
    args = parser.parse_args()
    if os.environ.get("TABMAIL_WRITERS_STOPPED") != "yes":
        parser.error("stop all API/SMTP/worker/retention writers; set TABMAIL_WRITERS_STOPPED=yes")
    if os.environ.get("TABMAIL_OBJECT_STORE", os.environ.get("TABMAIL_OBJECTSTORE", "fs")) != "fs":
        parser.error("this script only supports filesystem object storage")
    dsn = os.environ.get("TABMAIL_DB_DSN", "")
    data = os.environ.get("TABMAIL_DATADIR", "")
    if not dsn or not data:
        parser.error("TABMAIL_DB_DSN and TABMAIL_DATADIR are required")
    root, snapshot = Path(data).resolve(), args.snapshot.resolve()
    if snapshot == root or snapshot.is_relative_to(root) or root.is_relative_to(snapshot):
        parser.error("snapshot and object directory must not overlap")
    # Credentials are never written into the manifest or echoed by this script.
    # Use a libpq service definition for production to keep secrets out of DSNs.
    os.environ["PGDATABASE"] = dsn
    if args.operation == "backup":
        if not root.is_dir() or any(p.is_symlink() for p in root.rglob("*")):
            parser.error("object directory must exist and contain no symlinks")
        snapshot.mkdir(parents=True, exist_ok=False, mode=0o700)
        run("pg_dump", "--format=custom", "--no-owner", "--no-acl", "--file=" + str(snapshot / "database.dump"))
        with tarfile.open(snapshot / "objects.tar.gz", "w:gz") as archive:
            for path in sorted(root.iterdir()):
                archive.add(path, arcname=path.name, recursive=True)
        manifest = {"format": 1, "writer_stop_acknowledged": True,
                    "sha256": {name: digest(snapshot / name) for name in ("database.dump", "objects.tar.gz")}}
        (snapshot / "manifest.json").write_text(json.dumps(manifest, indent=2) + "\n")
        print("Coordinated snapshot created:", snapshot)
        return
    manifest = json.loads((snapshot / "manifest.json").read_text())
    if manifest.get("format") != 1 or set(manifest.get("sha256", {})) != {"database.dump", "objects.tar.gz"}:
        parser.error("invalid snapshot manifest")
    for name, expected in manifest["sha256"].items():
        if digest(snapshot / name) != expected:
            parser.error("snapshot checksum mismatch: " + name)
    if root.exists() and any(root.iterdir()):
        parser.error("restore target object directory is not empty")
    count = run("psql", "-X", "-A", "-t", "-v", "ON_ERROR_STOP=1", "-c",
                "SELECT count(*) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace "
                "WHERE n.nspname NOT IN ('pg_catalog','information_schema') AND n.nspname NOT LIKE 'pg_toast%'",
                capture=True).strip()
    if count != "0":
        parser.error("restore target database must be empty; no destructive --clean restore is performed")
    with tarfile.open(snapshot / "objects.tar.gz", "r:gz") as archive:
        for member in archive.getmembers():
            target = (root / member.name).resolve()
            if not target.is_relative_to(root) or not (member.isfile() or member.isdir()):
                parser.error("unsafe archive member")
        # Restore atomically at the SQL layer. A filesystem failure leaves an
        # incomplete isolated target, never a silently promoted production restore.
        run("pg_restore", "--exit-on-error", "--single-transaction", "--no-owner", "--no-acl", "--dbname=" + os.environ.get("PGDATABASE", ""),
            str(snapshot / "database.dump"))
        root.mkdir(parents=True, exist_ok=True)
        archive.extractall(root, filter="data")
    print("Restored into empty isolated targets. Verify mail, permissions, objects and queues before promotion.")


if __name__ == "__main__":
    try:
        main()
    except subprocess.CalledProcessError:
        raise SystemExit("PostgreSQL command failed; snapshot/restore was not completed") from None
