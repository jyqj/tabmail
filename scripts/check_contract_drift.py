#!/usr/bin/env python3
"""AR07 contract drift checker: Go DTO structs vs TS interfaces.

Beyond field-name equality, the Go->TS wire projection below is verified:

    uuid.UUID / time.Time / string       -> string (string-literal unions and
                                            aliases of them are accepted)
    named `type X string`                -> string-compatible TS member
    int/int8..64/uint*/float32/float64   -> number
    bool                                 -> boolean
    []T                                  -> TS type must contain an array
                                            member (`X[]` or `Array<X>`)
    map[K]V / json.RawMessage / structs
    without a declared mapping           -> opaque (name-level checks only)

Wire nullability follows what encoding/json actually emits, not Go nil
semantics:
    non-pointer, no omitempty -> always present, never null: TS must not say null
    pointer, no omitempty     -> explicit null on the wire: TS must include null
    pointer, omitempty        -> absent or value: TS must be optional or nullable
    non-pointer, omitempty    -> omitted when empty, never null: TS null is drift
A slice/map without omitempty can marshal nil as null; that residual gap is
documented here and deliberately not enforced.

Fail-closed structure checks: a missing Go/TS endpoint of a declared mapping,
duplicate struct/interface definitions across the scanned files, and duplicate
field names inside one struct/interface all fail the check.

OpenAPI: internal/api/openapi.yaml is deliberately NOT compiled here. The
Python stdlib has no YAML parser and a hand-rolled indentation parser would
silently mis-read the schema; envelope/schema drift inside the spec remains a
documented blind spot of this checker.
"""
from __future__ import annotations

import re
import sys
from dataclasses import dataclass
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
    "Mailbox",
    "Message",
    "MonitorEvent",
    "SMTPPolicy",
    "SystemSetting",
    "AuditEntry",
    "WebhookDelivery",
    "IngestJob",
    "SystemStats",
    "OutboundJob",
]

company_pairs = {
    "ArchivedMail": "ArchivedMail", "ContentIndexStatus": "ContentIndexStatus",
    "OffboardingOptions": "OffboardingOptions", "OffboardingImpact": "OffboardingImpact",
    "OffboardingPlan": "OffboardingPlan", "Overview": "CompanyOverview",
    "AdminAudit": "CompanyAdminAudit", "AccessExplanation": "AccessExplanation",
    "Settings": "CompanySettings", "Invitation": "Invitation",
    "MailboxGrantSnapshot": "MailboxGrantSnapshot",
    "DraftTemplateVersion": "DraftTemplateVersion",
    "SubmissionCapabilities": "SubmissionCapabilities",
    "MailboxAccess": "WorkMailbox", "Variable": "TemplateVariable",
    "TemplateDraft": "TemplateDraft", "Template": "MailTemplate",
    "TemplateVersion": "TemplateVersion", "DraftPayload": "DraftPayload",
    "Draft": "MailDraft", "Attachment": "MailAttachment",
    "Recipient": "RecipientResult", "RecoveryTarget": "RecoveryTarget",
    "RecoveryReceipt": "Receipt",
    "Submission": "Submission", "SubmissionRecipient": "SubmissionRecipient",
    "SubmissionContent": "SubmissionContent", "SubmissionAttachment": "SubmissionAttachment",
    "Domain": "CompanyDomain", "DNSCheck": "DomainDNSCheck",
    "DomainVerificationChecks": "DomainVerificationChecks",
    "DomainVerification": "DomainVerification",
}

SHARED_PAIRS = {name: name for name in SHARED_TYPES}
# Nested Go struct references resolve through the same projection table, so a
# struct-typed field must serialize as the mapped TS name or as an inline object.
NESTED_MAP: dict[str, str] = {**SHARED_PAIRS, **company_pairs}

# Unambiguous Go scalar -> TS base-type projection. Anything absent stays opaque.
SCALAR_PROJECTION = {
    "uuid.UUID": "string",
    "time.Time": "string",
    "string": "string",
    "bool": "boolean",
    "int": "number", "int8": "number", "int16": "number", "int32": "number",
    "int64": "number", "uint": "number", "uint8": "number", "uint16": "number",
    "uint32": "number", "uint64": "number",
    "float32": "number", "float64": "number",
}


@dataclass
class GoField:
    name: str
    go_type: str
    pointer: bool
    omitempty: bool


@dataclass
class GoSource:
    structs: dict[str, dict[str, GoField]]
    string_types: set[str]


@dataclass
class TsField:
    optional: bool
    ts_type: str


@dataclass
class TsSource:
    interfaces: dict[str, dict[str, TsField]]
    aliases: dict[str, str]


def parse_go_source(text: str) -> tuple[GoSource, list[str]]:
    string_types = set(re.findall(r"^type\s+(\w+)\s+string\s*$", text, flags=re.M))
    structs: dict[str, dict[str, GoField]] = {}
    problems: list[str] = []
    for match in re.finditer(r"type\s+(\w+)\s+struct\s*\{(.*?)\n\}", text, flags=re.S):
        name, body = match.groups()
        if name in structs:
            problems.append(f"[duplicate-go] {name}")
            continue
        fields: dict[str, GoField] = {}
        for raw in body.splitlines():
            line = raw.strip()
            if not line or line.startswith("//"):
                continue
            tag = re.search(r"`([^`]*)`", line)
            if not tag:
                continue
            json_tag = re.search(r'json:"([^"]+)"', tag.group(1))
            if not json_tag:
                continue
            head = re.match(r"(\w+)\s+(.+?)\s*$", line[: tag.start()])
            if not head:
                continue
            json_name, _, opts = json_tag.group(1).partition(",")
            if json_name == "-":
                continue
            if json_name in fields:
                problems.append(f"[duplicate-go-field] {name}.{json_name}")
                continue
            go_type = head.group(2).strip()
            fields[json_name] = GoField(
                name=json_name,
                go_type=go_type,
                pointer=go_type.startswith("*"),
                omitempty="omitempty" in opts.split(","),
            )
        structs[name] = fields
    return GoSource(structs, string_types), problems


def merge_go_sources(texts: list[str], problems: list[str]) -> GoSource:
    merged = GoSource({}, set())
    for text in texts:
        src, parse_problems = parse_go_source(text)
        problems.extend(parse_problems)
        for name, fields in src.structs.items():
            if name in merged.structs:
                problems.append(f"[duplicate-go] {name}")
                continue
            merged.structs[name] = fields
        merged.string_types |= src.string_types
    return merged


def split_ts_statements(body: str) -> list[str]:
    stream: list[str] = []
    for raw in body.splitlines():
        line = re.sub(r"//.*$", "", raw).strip()
        if not line or line.startswith("*") or line.startswith("/*"):
            continue
        stream.append(line)
    statements: list[str] = []
    depth = 0
    current = ""
    for char in " ".join(stream):
        if char == "{":
            depth += 1
        elif char == "}":
            depth -= 1
        if char == ";" and depth == 0:
            if current.strip():
                statements.append(current.strip())
            current = ""
        else:
            current += char
    if current.strip():
        statements.append(current.strip())
    return statements


def parse_ts_source(text: str) -> tuple[TsSource, list[str]]:
    aliases: dict[str, str] = {}
    interfaces: dict[str, dict[str, TsField]] = {}
    problems: list[str] = []
    lines = text.splitlines()
    i = 0
    while i < len(lines):
        alias_match = re.match(r"\s*export\s+type\s+(\w+)\s*=\s*(.*)$", lines[i])
        if alias_match:
            name, buf = alias_match.group(1), alias_match.group(2)
            while not buf.rstrip().endswith(";") and i + 1 < len(lines):
                i += 1
                buf += " " + re.sub(r"//.*$", "", lines[i]).strip()
            aliases[name] = buf.strip().rstrip(";").strip()
        i += 1
    for match in re.finditer(
        r"export\s+interface\s+(\w+)\s*(?:extends\s+[\w.,\s<>]+?)?\s*\{(.*?)\n\}",
        text,
        flags=re.S,
    ):
        name, body = match.group(1), match.group(2)
        if name in interfaces:
            problems.append(f"[duplicate-ts] {name}")
            continue
        fields: dict[str, TsField] = {}
        for part in split_ts_statements(body):
            field_match = re.match(r"^(?:readonly\s+)?(\w+)(\?)?\s*:\s*(.+)$", part)
            if not field_match:
                problems.append(f"[unparsed-ts] {name}: {part}")
                continue
            field_name, optional, ts_type = field_match.groups()
            if field_name in fields:
                problems.append(f"[duplicate-ts-field] {name}.{field_name}")
                continue
            fields[field_name] = TsField(optional=bool(optional), ts_type=ts_type.strip())
        interfaces[name] = fields
    return TsSource(interfaces, aliases), problems


def merge_ts_sources(texts: list[str], problems: list[str]) -> TsSource:
    merged = TsSource({}, {})
    for text in texts:
        src, parse_problems = parse_ts_source(text)
        problems.extend(parse_problems)
        for name, fields in src.interfaces.items():
            if name in merged.interfaces:
                problems.append(f"[duplicate-ts] {name}")
                continue
            merged.interfaces[name] = fields
        merged.aliases.update(src.aliases)
    return merged


def ts_members(ts_type: str) -> list[str]:
    """Split only top-level unions, never pipes in literals or nested types."""
    members: list[str] = []
    stack: list[str] = []
    pairs = {"(": ")", "[": "]", "{": "}", "<": ">"}
    quote = ""
    escaped = False
    start = 0
    for index, char in enumerate(ts_type):
        if quote:
            if escaped:
                escaped = False
            elif char == "\\":
                escaped = True
            elif char == quote:
                quote = ""
            continue
        if char in "\"'`":
            quote = char
        elif char in pairs:
            stack.append(pairs[char])
        elif char in pairs.values():
            if not stack or stack.pop() != char:
                raise ValueError(f"unbalanced TypeScript type: {ts_type}")
        elif char == "|" and not stack:
            members.append(ts_type[start:index].strip())
            start = index + 1
    if quote or stack:
        raise ValueError(f"unbalanced TypeScript type: {ts_type}")
    members.append(ts_type[start:].strip())
    # A leading pipe is valid TypeScript formatting for multiline unions;
    # empty interior or trailing members still fail the type checks.
    if members and not members[0] and ts_type.lstrip().startswith("|"):
        members = members[1:]
    return members


def ts_member_string_like(member: str, aliases: dict[str, str], seen: frozenset[str] = frozenset()) -> bool:
    member = member.strip()
    if member == "string":
        return True
    if len(member) >= 2 and member[0] == member[-1] and member[0] in "\"'":
        return True
    if re.fullmatch(r"\w+", member):
        if member in seen:
            return False
        definition = aliases.get(member)
        if definition is None:
            return False
        return all(
            ts_member_string_like(m, aliases, seen | {member})
            for m in ts_members(definition)
        )
    return False


def ts_member_number_like(member: str) -> bool:
    member = member.strip()
    return member == "number" or bool(re.fullmatch(r"-?\d+(\.\d+)?", member))


def array_elem_ts(member: str) -> str:
    member = member.strip()
    if member.startswith("Array<") and member.endswith(">"):
        return member[6:-1].strip()
    return re.sub(r"(\[\])+$", "", member).strip()


def check_projection(
    label: str,
    field_name: str,
    go_type: str,
    ts_type: str,
    nested_map: dict[str, str],
    go_string_types: set[str],
    ts_aliases: dict[str, str],
) -> list[str]:
    errors: list[str] = []
    bare = go_type.lstrip("*")
    projection = SCALAR_PROJECTION.get(bare) or SCALAR_PROJECTION.get(bare.split(".")[-1])
    relevant = [m for m in ts_members(ts_type) if m != "null"]
    if not relevant or any(not m for m in relevant):
        return [f"[type-drift] {label}.{field_name}: TS must represent a non-null Go value"]
    if projection == "string" or (
        projection is None and bare.split(".")[-1] in go_string_types
    ):
        if not all(ts_member_string_like(m, ts_aliases) for m in relevant):
            errors.append(
                f"[type-drift] {label}.{field_name}: Go `{go_type}` projects to string; "
                f"TS `{ts_type}` is not string-compatible"
            )
    elif projection == "number":
        if not all(ts_member_number_like(m) for m in relevant):
            errors.append(
                f"[type-drift] {label}.{field_name}: Go `{go_type}` projects to number; "
                f"TS `{ts_type}` is not number-compatible"
            )
    elif projection == "boolean":
        if any(m != "boolean" for m in relevant):
            errors.append(
                f"[type-drift] {label}.{field_name}: Go `{go_type}` projects to boolean; "
                f"TS `{ts_type}` is not boolean"
            )
    elif projection is None and not go_type.startswith("map["):
        expected = nested_map.get(bare.split(".")[-1])
        if expected is not None:
            if any(member != expected for member in relevant):
                errors.append(
                    f"[nested-drift] {label}.{field_name}: Go `{go_type}` maps to TS "
                    f"`{expected}`; TS says `{ts_type}`"
                )
    return errors


def compare_struct(
    go_name: str,
    ts_name: str,
    go_fields: dict[str, GoField],
    ts_fields: dict[str, TsField],
    nested_map: dict[str, str],
    go_string_types: set[str],
    ts_aliases: dict[str, str],
) -> list[str]:
    errors: list[str] = []
    label = f"{go_name}->{ts_name}"
    go_names, ts_names = set(go_fields), set(ts_fields)
    if go_names != ts_names:
        errors.append(
            f"[drift] {label}\n"
            f"  Go-only={sorted(go_names - ts_names)}\n"
            f"  TS-only={sorted(ts_names - go_names)}"
        )
    for field_name in sorted(go_names & ts_names):
        go_field, ts_field = go_fields[field_name], ts_fields[field_name]
        members = ts_members(ts_field.ts_type)
        non_null = [m for m in members if m != "null"]
        has_null = len(non_null) < len(members)
        go_type = go_field.go_type
        is_array = go_type.startswith("[]")
        is_map = go_type.startswith("map[")

        if is_array and (not non_null or not all(m.endswith("[]") or (m.startswith("Array<") and m.endswith(">")) for m in non_null)):
            errors.append(
                f"[array-drift] {label}.{field_name}: Go `{go_type}` vs TS `{ts_field.ts_type}`"
            )
        # Wire nullability: what encoding/json emits decides, not Go nil.
        if go_field.pointer and not go_field.omitempty and not has_null:
            errors.append(
                f"[missing-null] {label}.{field_name}: Go `{go_type}` without omitempty "
                f"marshals explicit null; TS `{ts_field.ts_type}` must include null"
            )
        if go_field.omitempty and not ts_field.optional:
            errors.append(
                f"[optional-drift] {label}.{field_name}: Go `{go_type}` with omitempty may "
                f"be absent; TS must be optional (nullable is not optional)"
            )
        if not go_field.pointer and not go_field.omitempty and has_null and not is_array and not is_map:
            errors.append(
                f"[null-drift] {label}.{field_name}: Go `{go_type}` never marshals null; "
                f"TS `{ts_field.ts_type}` claims null"
            )
        if not go_field.pointer and go_field.omitempty and has_null and not is_array and not is_map:
            errors.append(
                f"[null-drift] {label}.{field_name}: omitempty omits empty values and never "
                f"emits null; TS `{ts_field.ts_type}` claims null"
            )

        if is_array:
            array_members = [m for m in non_null if m.endswith("[]") or (m.startswith("Array<") and m.endswith(">"))]
            projections = [(go_type[2:], array_elem_ts(m)) for m in array_members]
        elif is_map:
            continue
        else:
            projections = [(go_type, ts_field.ts_type)]
        for elem_go, elem_ts in projections:
            errors.extend(
                check_projection(
                    label, field_name, elem_go, elem_ts,
                    nested_map, go_string_types, ts_aliases,
                )
            )
    return errors


def check_pairs(
    pairs: dict[str, str],
    go_texts: list[str],
    ts_texts: list[str],
    nested_map: dict[str, str],
) -> list[str]:
    problems: list[str] = []
    go = merge_go_sources(go_texts, problems)
    ts = merge_ts_sources(ts_texts, problems)
    errors = list(problems)
    for go_name, ts_name in pairs.items():
        go_fields = go.structs.get(go_name)
        ts_fields = ts.interfaces.get(ts_name)
        if go_fields is None:
            errors.append(f"[missing-go] {go_name}")
            continue
        if ts_fields is None:
            errors.append(f"[missing-ts] {ts_name}")
            continue
        errors.extend(
            compare_struct(
                go_name, ts_name, go_fields, ts_fields,
                nested_map, go.string_types, ts.aliases,
            )
        )
    return errors


def check_shared(go_text: str, ts_text: str) -> list[str]:
    return check_pairs(SHARED_PAIRS, [go_text], [ts_text], NESTED_MAP)


def check_company(go_texts: list[str], ts_texts: list[str]) -> list[str]:
    return check_pairs(company_pairs, go_texts, ts_texts, NESTED_MAP)


def main() -> int:
    problems = check_shared(GO_MODELS.read_text(), TS_TYPES.read_text())
    company_go = [
        path.read_text()
        for path in sorted((ROOT / "internal/company").glob("*.go"))
        if not path.name.endswith("_test.go")
    ]
    company_ts = [
        (ROOT / "web/lib/company.ts").read_text(),
        (ROOT / "web/features/mail/api.ts").read_text(),
        (ROOT / "web/features/company/api.ts").read_text(),
    ]
    problems += check_company(company_go, company_ts)

    if problems:
        print("Contract drift detected between Go DTOs and the TypeScript projections:\n")
        print("\n".join(problems))
        return 1

    print(
        f"Contract check passed: {len(SHARED_TYPES)} shared models and "
        f"{len(company_pairs)} company DTOs verified for fields, wire nullability, "
        "scalar projection, arrays and nested references."
    )
    print(
        "OpenAPI (internal/api/openapi.yaml) is not compiled: stdlib Python has no "
        "YAML parser; spec-level drift remains a documented blind spot."
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
