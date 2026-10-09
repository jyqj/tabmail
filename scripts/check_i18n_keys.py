#!/usr/bin/env python3
"""Validate actual JSON catalogs and literal catalog calls in production TS/TSX.

Requires Node and the TypeScript version installed by `cd web && npm ci`.
The AST collector distinguishes useText(zh, en) from useI18n().t(key).
Computed keys are counted, not guessed: this is a static-key gate, not a proof
of complete runtime localization. Missing tools or empty scans fail closed.
"""
from __future__ import annotations

import argparse
import json
import os
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
SOURCE_DIRS = ("app", "components", "contexts", "features", "hooks", "lib")
EXCLUDED_DIRS = {"node_modules", ".next", ".git", "__tests__", "__mocks__"}


class CheckError(ValueError):
    """Malformed input or unavailable validation dependency."""


def unique_object(pairs: list[tuple[str, object]]) -> dict[str, object]:
    result: dict[str, object] = {}
    for key, value in pairs:
        if key in result:
            raise CheckError(f"duplicate JSON key: {key}")
        result[key] = value
    return result


def catalog_keys(root: Path, locale: str) -> set[str]:
    path = root / "web" / "locales" / f"{locale}.json"
    try:
        data = json.loads(path.read_text(encoding="utf-8"), object_pairs_hook=unique_object)
    except (OSError, UnicodeError, ValueError) as exc:
        raise CheckError(f"{path}: invalid or missing catalog: {exc}") from exc
    if not isinstance(data, dict) or not data:
        raise CheckError(f"{path}: catalog must be a nonempty object")
    for key, value in data.items():
        if not key.strip() or not isinstance(value, str) or not value.strip():
            raise CheckError(f"{path}: {key!r} must have a nonempty string translation")
    return set(data)


def source_files(root: Path) -> list[Path]:
    result: list[Path] = []
    for directory in SOURCE_DIRS:
        base = root / "web" / directory
        if not base.is_dir():
            raise CheckError(f"required source directory missing: {base}")
        for current, dirs, files in os.walk(base, followlinks=False):
            dirs[:] = sorted(d for d in dirs if d not in EXCLUDED_DIRS)
            for name in sorted(files):
                path = Path(current) / name
                if path.suffix not in {".ts", ".tsx"} or name.endswith(".d.ts"):
                    continue
                if any(mark in name for mark in (".test.", ".spec.", ".r5audit.")):
                    continue
                if path.is_symlink() or not path.resolve().is_relative_to(root.resolve()):
                    raise CheckError(f"source outside validation root: {path}")
                # Strict decoding: do not silently drop malformed source bytes.
                path.read_text(encoding="utf-8")
                result.append(path.resolve())
    if not result:
        raise CheckError("no production TS/TSX sources scanned")
    return sorted(result)


def collect_keys(root: Path, files: list[Path], node: str) -> dict:
    collector = ROOT / "scripts" / "collect_i18n_keys.cjs"
    try:
        process = subprocess.run(
            [node, str(collector)],
            input=json.dumps({"root": str(root.resolve()), "files": [str(p) for p in files]}),
            capture_output=True, text=True, encoding="utf-8", timeout=90,
        )
    except (OSError, subprocess.TimeoutExpired) as exc:
        raise CheckError(f"cannot run TypeScript key collector: {exc}") from exc
    if process.returncode:
        raise CheckError(f"TypeScript key collector failed: {process.stderr.strip()[:2000]}")
    try:
        data = json.loads(process.stdout, object_pairs_hook=unique_object)
    except ValueError as exc:
        raise CheckError("invalid TypeScript collector response") from exc
    if not isinstance(data, dict) or data.get("files") != len(files):
        raise CheckError("TypeScript collector did not account for every source file")
    if not isinstance(data.get("keys"), list) or not data["keys"]:
        raise CheckError("no literal catalog keys found; refusing an empty successful scan")
    for item in data["keys"]:
        if not isinstance(item, dict) or not isinstance(item.get("key"), str) or not isinstance(item.get("location"), str):
            raise CheckError("invalid key entry from TypeScript collector")
    for field in ("dynamic_calls", "inline_calls"):
        if type(data.get(field)) is not int or data[field] < 0:
            raise CheckError(f"invalid collector counter: {field}")
    return data


def validate(root: Path, node: str = "node") -> dict:
    zh, en = catalog_keys(root, "zh"), catalog_keys(root, "en")
    if zh != en:
        raise CheckError(f"catalog mismatch: missing en={sorted(zh-en)}; missing zh={sorted(en-zh)}")
    files = source_files(root)
    data = collect_keys(root, files, node)
    missing = [entry for entry in data["keys"] if entry["key"] not in zh]
    if missing:
        raise CheckError("missing catalog keys:\n" + "\n".join(f"  {v['key']!r} at {v['location']}" for v in missing))
    return {"catalog_keys": len(zh), "source_files": len(files),
            "literal_keys": len({entry["key"] for entry in data["keys"]}),
            "dynamic_calls": data["dynamic_calls"], "inline_calls": data["inline_calls"]}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=ROOT)
    parser.add_argument("--node", default=os.environ.get("NODE_BINARY", "node"))
    args = parser.parse_args()
    try:
        result = validate(args.root.resolve(), args.node)
    except (CheckError, OSError, UnicodeError) as exc:
        print(f"i18n check FAILED: {exc}", file=sys.stderr)
        return 1
    print("i18n check PASS: " + json.dumps(result, sort_keys=True))
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
