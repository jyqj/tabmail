#!/usr/bin/env python3
"""Static i18n key consistency checks for the web app.

Checks:
  1. zh/en message catalogs define the same keys.
  2. literal t("key") / t('key') usages are present in both catalogs.

Dynamic usages such as t(`faq.q${n}`) are intentionally not expanded here.
"""

from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
I18N_FILE = ROOT / "web" / "lib" / "i18n.tsx"
SCAN_DIRS = [
    ROOT / "web" / "app",
    ROOT / "web" / "components",
    ROOT / "web" / "contexts",
    ROOT / "web" / "lib",
]
SKIP_DIRS = {"node_modules", ".next", ".git"}
KEY_RE = re.compile(r"""["']([^"']+)["']\s*:""")
STATIC_T_RE = re.compile(r"""\bt\s*\(\s*(["'])([^"']+)\1""")


def extract_object(source: str, name: str) -> str:
    marker = re.search(rf"const\s+{name}\s*:\s*Messages\s*=\s*{{", source)
    if not marker:
        raise RuntimeError(f"missing const {name}: Messages object")

    start = marker.end() - 1
    depth = 0
    quote: str | None = None
    escaped = False
    line_comment = False
    block_comment = False

    for i in range(start, len(source)):
        ch = source[i]
        nxt = source[i + 1] if i + 1 < len(source) else ""

        if line_comment:
            if ch == "\n":
                line_comment = False
            continue
        if block_comment:
            if ch == "*" and nxt == "/":
                block_comment = False
            continue
        if quote:
            if escaped:
                escaped = False
            elif ch == "\\":
                escaped = True
            elif ch == quote:
                quote = None
            continue

        if ch in {'"', "'", "`"}:
            quote = ch
            continue
        if ch == "/" and nxt == "/":
            line_comment = True
            continue
        if ch == "/" and nxt == "*":
            block_comment = True
            continue
        if ch == "{":
            depth += 1
        elif ch == "}":
            depth -= 1
            if depth == 0:
                return source[start : i + 1]

    raise RuntimeError(f"unterminated const {name}: Messages object")


def catalog_keys(name: str) -> set[str]:
    source = I18N_FILE.read_text(encoding="utf-8")
    return set(KEY_RE.findall(extract_object(source, name)))


def iter_source_files() -> list[Path]:
    out: list[Path] = []
    for base in SCAN_DIRS:
        if not base.exists():
            continue
        for path in base.rglob("*"):
            if any(part in SKIP_DIRS for part in path.parts):
                continue
            if path.suffix in {".ts", ".tsx"} and path != I18N_FILE:
                out.append(path)
    return out


def used_static_keys() -> dict[str, list[str]]:
    used: dict[str, list[str]] = {}
    for path in iter_source_files():
        text = path.read_text(encoding="utf-8", errors="ignore")
        for match in STATIC_T_RE.finditer(text):
            used.setdefault(match.group(2), []).append(str(path.relative_to(ROOT)))
    return used


def print_key_list(title: str, keys: list[str], locations: dict[str, list[str]] | None = None) -> None:
    if not keys:
        return
    print(title, file=sys.stderr)
    for key in keys:
        suffix = ""
        if locations and key in locations:
            suffix = "  # " + ", ".join(sorted(set(locations[key]))[:5])
        print(f"  - {key}{suffix}", file=sys.stderr)


def main() -> int:
    zh = catalog_keys("zh")
    en = catalog_keys("en")
    used = used_static_keys()
    used_keys = set(used)

    missing_en = sorted(zh - en)
    missing_zh = sorted(en - zh)
    used_missing_zh = sorted(used_keys - zh)
    used_missing_en = sorted(used_keys - en)

    print_key_list("Keys present in zh but missing in en:", missing_en)
    print_key_list("Keys present in en but missing in zh:", missing_zh)
    print_key_list("Static t(...) keys missing in zh:", used_missing_zh, used)
    print_key_list("Static t(...) keys missing in en:", used_missing_en, used)

    if missing_en or missing_zh or used_missing_zh or used_missing_en:
        return 1

    print(f"i18n keys OK: zh={len(zh)} en={len(en)} static_usages={len(used_keys)}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
