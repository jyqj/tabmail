#!/usr/bin/env python3
from __future__ import annotations

import re
import sys
from pathlib import Path


ROOT = Path(__file__).resolve().parents[1]
GO_MODELS = ROOT / "internal/models/models.go"
TS_TYPES = ROOT / "web/lib/types.ts"

SHARED_TYPES = [
    "Plan",
    "Tenant",
    "TenantOverride",
    "TenantAPIKey",
    "EffectiveConfig",
    "DomainZone",
    "DomainRoute",
    "Mailbox",
    "Message",
    "MonitorEvent",
    "SMTPPolicy",
]


def parse_go_struct_fields(text: str) -> dict[str, set[str]]:
    structs: dict[str, set[str]] = {}
    for match in re.finditer(r"type\s+(\w+)\s+struct\s*\{(.*?)\n\}", text, flags=re.S):
        name, body = match.groups()
        fields: set[str] = set()
        for line in body.splitlines():
            line = line.strip()
            if not line or line.startswith("//"):
                continue

            json_match = re.search(r'json:"([^"]+)"', line)
            if not json_match:
                continue

            json_name = json_match.group(1).split(",", 1)[0]
            if json_name == "-":
                continue
            fields.add(json_name)

        structs[name] = fields
    return structs


def parse_ts_interface_fields(text: str) -> dict[str, set[str]]:
    interfaces: dict[str, set[str]] = {}
    pattern = re.compile(r"export interface\s+(\w+)\s*(?:extends\s+\w+\s*)?\{(.*?)\n\}", flags=re.S)
    for match in pattern.finditer(text):
        name, body = match.groups()
        fields: set[str] = set()
        depth = 0

        for raw_line in body.splitlines():
            line = raw_line.strip()
            if not line or line.startswith("//"):
                continue

            if depth == 0 and ":" in line:
                key = line.split(":", 1)[0].strip().rstrip("?")
                if re.match(r"^[A-Za-z_][A-Za-z0-9_]*$", key):
                    fields.add(key)

            depth += line.count("{") - line.count("}")

        interfaces[name] = fields
    return interfaces


def main() -> int:
    go_structs = parse_go_struct_fields(GO_MODELS.read_text())
    ts_interfaces = parse_ts_interface_fields(TS_TYPES.read_text())

    errors: list[str] = []
    for type_name in SHARED_TYPES:
      go_fields = go_structs.get(type_name)
      ts_fields = ts_interfaces.get(type_name)

      if go_fields is None:
          errors.append(f"[missing-go] {type_name}")
          continue
      if ts_fields is None:
          errors.append(f"[missing-ts] {type_name}")
          continue

      only_go = sorted(go_fields - ts_fields)
      only_ts = sorted(ts_fields - go_fields)
      if only_go or only_ts:
          errors.append(
              f"[drift] {type_name}\n"
              f"  only in Go: {only_go or '[]'}\n"
              f"  only in TS: {only_ts or '[]'}"
          )

    if errors:
        print("Contract drift detected between internal/models/models.go and web/lib/types.ts:\n")
        print("\n".join(errors))
        return 1

    print(f"Contract check passed for {len(SHARED_TYPES)} shared model types.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
