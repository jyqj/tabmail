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

OpenAPI: internal/api/openapi.yaml is parsed with the pinned PyYAML dependency
using a duplicate-key-rejecting loader. The same Go/TS projection is checked
against the 32 explicit internal/company DTO component schemas for fields,
required/optional/nullability, scalar/array/map shapes, nested references and
string-literal enums. Storage-backed shared models are intentionally excluded
until R5-P8-070 gives them allow-list response DTOs; the gate must never force
internal object keys, leases or deletion timestamps into the public contract.
Local component references are also resolved fail-closed.
"""
from __future__ import annotations

import re
import json
import sys
from dataclasses import dataclass
from pathlib import Path

try:
    import yaml
except ModuleNotFoundError:  # pragma: no cover - exercised by the CLI guard
    yaml = None


ROOT = Path(__file__).resolve().parents[1]
GO_MODELS = ROOT / "internal/models/models.go"
TS_TYPES = ROOT / "web/lib/types.ts"
OPENAPI_SPEC = ROOT / "internal/api/openapi.yaml"

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
    "MailboxGrant": "WorkGrant",
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

# Go DTO name -> OpenAPI component. Existing company component names are kept
# for compatibility with the path document; missing DTOs are added under a
# stable explicit name instead of inferred by a permissive naming heuristic.
OPENAPI_COMPANY_COMPONENTS = {
    "MailboxGrant": "MailboxGrant",
    "ArchivedMail": "ArchivedMail",
    "ContentIndexStatus": "ContentIndexStatus",
    "OffboardingOptions": "OffboardingOptions",
    "OffboardingImpact": "OffboardingImpact",
    "OffboardingPlan": "OffboardingPlan",
    "Overview": "CompanyOverview",
    "AdminAudit": "CompanyAdminAudit",
    "AccessExplanation": "AccessExplanation",
    "Settings": "CompanySettings",
    "Invitation": "CompanyInvitation",
    "MailboxGrantSnapshot": "MailboxGrantSnapshot",
    "DraftTemplateVersion": "DraftTemplateVersion",
    "SubmissionCapabilities": "SubmissionCapabilities",
    "MailboxAccess": "CompanyMailboxAccess",
    "Variable": "CompanyVariable",
    "TemplateDraft": "CompanyTemplateDraft",
    "Template": "CompanyTemplate",
    "TemplateVersion": "CompanyTemplateVersion",
    "DraftPayload": "CompanyDraftPayload",
    "Draft": "CompanyDraft",
    "Attachment": "CompanyAttachment",
    "Recipient": "CompanyRecipient",
    "RecoveryTarget": "CompanyRecoveryTarget",
    "RecoveryReceipt": "CompanyRecoveryReceipt",
    "Submission": "Submission",
    "SubmissionRecipient": "SubmissionRecipient",
    "SubmissionContent": "SubmissionContent",
    "SubmissionAttachment": "SubmissionAttachment",
    "Domain": "CompanyDomain",
    "DNSCheck": "DomainDNSCheck",
    "DomainVerificationChecks": "DomainVerificationChecks",
    "DomainVerification": "DomainVerification",
}
OPENAPI_COMPONENTS = {
    **{name: name for name in SHARED_TYPES},
    **OPENAPI_COMPANY_COMPONENTS,
}

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


def _strict_yaml_loader():
    if yaml is None:
        raise RuntimeError(
            "PyYAML is required for the OpenAPI contract gate; "
            "install scripts/requirements-contract.txt"
        )

    class StrictSafeLoader(yaml.SafeLoader):
        # Contract documents are small, alias-free source files. Reject aliases
        # rather than constructing recursive or exponentially expanded objects.
        def compose_node(self, parent, index):
            if self.check_event(yaml.AliasEvent):
                raise yaml.YAMLError("aliases are not supported in contract documents")
            self.contract_nodes = getattr(self, 'contract_nodes', 0) + 1
            self.contract_depth = getattr(self, 'contract_depth', 0) + 1
            try:
                if self.contract_nodes > 50000 or self.contract_depth > 100:
                    raise yaml.YAMLError("contract document exceeds node/depth budget")
                return super().compose_node(parent, index)
            finally:
                self.contract_depth -= 1

    def construct_mapping(loader, node, deep=False):
        loader.flatten_mapping(node)
        mapping = {}
        for key_node, value_node in node.value:
            key = loader.construct_object(key_node, deep=deep)
            if not isinstance(key, (str, int)):
                raise yaml.YAMLError("contract mapping keys must be strings or integers")
            if key in mapping:
                raise yaml.constructor.ConstructorError(
                    "while constructing a mapping",
                    node.start_mark,
                    f"found duplicate key {key!r}",
                    key_node.start_mark,
                )
            mapping[key] = loader.construct_object(value_node, deep=deep)
        return mapping

    StrictSafeLoader.add_constructor(
        yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG,
        construct_mapping,
    )
    return StrictSafeLoader


def parse_openapi_text(text: str) -> tuple[dict | None, list[str]]:
    try:
        loader = _strict_yaml_loader()
    except RuntimeError as exc:
        return None, [f"[openapi-dependency] {exc}"]
    if len(text.encode('utf-8')) > 2 * 1024 * 1024:
        return None, ["[openapi-shape] document exceeds 2 MiB limit"]
    try:
        document = yaml.load(text, Loader=loader)
    except yaml.YAMLError as exc:
        return None, [f"[openapi-yaml] {exc}"]
    if not isinstance(document, dict):
        return None, ["[openapi-shape] document root must be an object"]
    if document.get("openapi") != "3.1.0":
        return document, [
            f"[openapi-version] expected 3.1.0, got {document.get('openapi')!r}"
        ]
    return document, []


def _schema_types(schema: object) -> set[str]:
    if not isinstance(schema, dict):
        return set()
    value = schema.get("type")
    if isinstance(value, str):
        return {value}
    if isinstance(value, list):
        return {str(item) for item in value if item != "null"}
    for keyword in ("anyOf", "oneOf"):
        variants = schema.get(keyword)
        if isinstance(variants, list):
            out: set[str] = set()
            for variant in variants:
                out |= _schema_types(variant)
            return out
    return set()


def _schema_nullable(schema: object) -> bool:
    if not isinstance(schema, dict):
        return False
    value = schema.get("type")
    if isinstance(value, list) and "null" in value:
        return True
    for keyword in ("anyOf", "oneOf"):
        variants = schema.get(keyword)
        if isinstance(variants, list):
            for variant in variants:
                if isinstance(variant, dict) and variant.get("type") == "null":
                    return True
    return False


def _schema_ref_component(schema: object) -> str | None:
    if not isinstance(schema, dict):
        return None
    ref = schema.get("$ref")
    prefix = "#/components/schemas/"
    if isinstance(ref, str) and ref.startswith(prefix):
        return ref[len(prefix):]
    for keyword in ("anyOf", "oneOf", "allOf"):
        variants = schema.get(keyword)
        if not isinstance(variants, list):
            continue
        non_null = [v for v in variants if v != {"type": "null"}]
        if len(non_null) == 1:
            return _schema_ref_component(non_null[0])
    return None


def _expanded_ts_members(
    ts_type: str,
    aliases: dict[str, str],
    seen: frozenset[str] = frozenset(),
) -> list[str]:
    expanded: list[str] = []
    for member in ts_members(ts_type):
        if re.fullmatch(r"\w+", member) and member in aliases and member not in seen:
            expanded.extend(
                _expanded_ts_members(aliases[member], aliases, seen | {member})
            )
        else:
            expanded.append(member)
    return expanded


def _ts_string_enum(ts_type: str, aliases: dict[str, str]) -> list[str] | None:
    members = [
        member for member in _expanded_ts_members(ts_type, aliases)
        if member != "null"
    ]
    if not members:
        return None
    values: list[str] = []
    for member in members:
        member = member.strip()
        if len(member) < 2 or member[0] != member[-1] or member[0] not in "\"'":
            return None
        value = member[1:-1]
        if value not in values:
            values.append(value)
    return values


def _expected_scalar(
    go_type: str,
    go_string_types: set[str],
) -> tuple[str | None, str | None]:
    bare = go_type.lstrip("*")
    short = bare.split(".")[-1]
    if bare == "uuid.UUID":
        return "string", "uuid"
    if bare == "time.Time":
        return "string", "date-time"
    projection = SCALAR_PROJECTION.get(bare) or SCALAR_PROJECTION.get(short)
    if projection == "number":
        if bare.startswith("float") or short.startswith("float"):
            return "number", None
        return "integer", None
    if projection is not None:
        return projection, None
    if short in go_string_types:
        return "string", None
    return None, None


def _compare_openapi_value(
    label: str,
    go_type: str,
    ts_type: str,
    schema: object,
    schemas: dict[str, object],
    nested_components: dict[str, str],
    go_string_types: set[str],
    ts_aliases: dict[str, str],
) -> list[str]:
    errors: list[str] = []
    bare = go_type.lstrip("*")
    if not isinstance(schema, dict):
        return [f"[openapi-shape] {label}: schema must be an object"]

    if "nullable" in schema:
        errors.append(f"[openapi-dialect] {label}: use JSON Schema null, not nullable")
    # We verify a deliberately bounded DTO subset, not arbitrary JSON Schema.
    # A composite is supported only as one value alternative plus literal null.
    for keyword in ('anyOf', 'oneOf', 'allOf'):
        if keyword in schema:
            variants = schema[keyword]
            if (not isinstance(variants, list) or not variants
                    or any(not isinstance(v, dict) for v in variants)):
                return errors + [f"[openapi-shape] {label}: malformed {keyword}"]
            non_null = [v for v in variants if v != {'type': 'null'}]
            if (len(non_null) != 1 or (keyword == 'allOf' and len(variants) != 1)
                    or any(k in schema for k in ('type', '$ref'))):
                return errors + [f"[openapi-shape] {label}: unsupported composite {keyword}"]
            return errors + _compare_openapi_value(
                label, go_type, ts_type, non_null[0], schemas, nested_components,
                go_string_types, ts_aliases)
    if any(k in schema for k in ('not', 'if', 'then', 'else', '$dynamicRef')):
        return errors + [f"[openapi-shape] {label}: unsupported conditional schema"]

    if bare.startswith("[]"):
        if _schema_types(schema) != {"array"}:
            return [f"[openapi-type] {label}: Go `{go_type}` requires type array"]
        items = schema.get("items")
        members = [m for m in ts_members(ts_type) if m != "null"]
        array_members = [
            m for m in members
            if m.endswith("[]") or (m.startswith("Array<") and m.endswith(">"))
        ]
        if not isinstance(items, dict) or not array_members:
            return [f"[openapi-array] {label}: array items are missing or TS is not an array"]
        errors.extend(
            _compare_openapi_value(
                f"{label}[]",
                bare[2:],
                array_elem_ts(array_members[0]),
                items,
                schemas,
                nested_components,
                go_string_types,
                ts_aliases,
            )
        )
        return errors

    map_match = re.fullmatch(r"map\[[^]]+\](.+)", bare)
    if map_match:
        if _schema_types(schema) != {"object"}:
            return [f"[openapi-type] {label}: Go `{go_type}` requires type object"]
        additional = schema.get("additionalProperties")
        if not isinstance(additional, dict):
            return [
                f"[openapi-map] {label}: typed Go map requires an explicit "
                "additionalProperties schema"
            ]
        ts_match = re.fullmatch(r"Record<\s*string\s*,\s*(.+)>", ts_type.strip())
        value_ts = ts_match.group(1).strip() if ts_match else "unknown"
        return _compare_openapi_value(
            f"{label}{{}}",
            map_match.group(1).strip(),
            value_ts,
            additional,
            schemas,
            nested_components,
            go_string_types,
            ts_aliases,
        )

    if bare == "json.RawMessage":
        if "object" not in _schema_types(schema):
            errors.append(f"[openapi-type] {label}: json.RawMessage requires type object")
        return errors

    expected_type, expected_format = _expected_scalar(bare, go_string_types)
    if expected_type is not None:
        actual_types = _schema_types(schema)
        if actual_types != {expected_type}:
            errors.append(
                f"[openapi-type] {label}: Go `{go_type}` requires {expected_type}; "
                f"OpenAPI has {sorted(actual_types)}"
            )
        if expected_format is not None and schema.get("format") != expected_format:
            errors.append(
                f"[openapi-format] {label}: Go `{go_type}` requires format "
                f"{expected_format!r}"
            )
        expected_enum = _ts_string_enum(ts_type, ts_aliases)
        actual_enum = schema.get("enum")
        if expected_enum is not None:
            if (not isinstance(actual_enum, list)
                    or any(not isinstance(v, str) for v in actual_enum)
                    or len(actual_enum) != len(set(actual_enum))
                    or set(actual_enum) != set(expected_enum)):
                errors.append(
                    f"[openapi-enum] {label}: expected {expected_enum}, got {actual_enum}"
                )
        elif actual_enum is not None:
            errors.append(
                f"[openapi-enum] {label}: OpenAPI narrows unconstrained TS `{ts_type}` "
                f"to {actual_enum}"
            )
        return errors

    short = bare.split(".")[-1]
    expected_component = nested_components.get(short)
    if expected_component is None and short in schemas:
        expected_component = short
    if expected_component is not None:
        actual_component = _schema_ref_component(schema)
        if actual_component != expected_component:
            errors.append(
                f"[openapi-ref] {label}: Go `{go_type}` requires "
                f"#/components/schemas/{expected_component}, got {actual_component!r}"
            )
        return errors

    if "object" not in _schema_types(schema):
        errors.append(
            f"[openapi-type] {label}: unmapped Go `{go_type}` must be represented "
            "as an explicit object"
        )
    return errors


def check_openapi_pairs(
    pairs: dict[str, str],
    components: dict[str, str],
    go: GoSource,
    ts: TsSource,
    document: dict,
    nested_components: dict[str, str],
) -> list[str]:
    errors: list[str] = []
    schemas = _component_schemas(document)
    if schemas is None:
        return ["[openapi-shape] components.schemas must be an object"]
    for go_name, ts_name in pairs.items():
        component_name = components[go_name]
        schema = schemas.get(component_name)
        go_fields = go.structs.get(go_name)
        ts_fields = ts.interfaces.get(ts_name)
        label = f"{go_name}->{ts_name}->{component_name}"
        if go_fields is None or ts_fields is None:
            errors.append(f"[openapi-source] {label}: Go or TS source is missing")
            continue
        if set(go_fields) != set(ts_fields):
            errors.append(f"[openapi-source] {label}: Go/TS field sets differ")
        if not isinstance(schema, dict):
            errors.append(f"[missing-openapi] {label}")
            continue
        if _schema_types(schema) != {"object"}:
            errors.append(f"[openapi-shape] {label}: component must have type object")
            continue
        properties = schema.get("properties")
        if not isinstance(properties, dict):
            errors.append(f"[openapi-shape] {label}: properties must be an object")
            continue
        expected_names = set(go_fields)
        actual_names = set(properties)
        if expected_names != actual_names:
            errors.append(
                f"[openapi-fields] {label}\n"
                f"  missing={sorted(expected_names - actual_names)}\n"
                f"  extra={sorted(actual_names - expected_names)}"
            )
        expected_required = {
            name for name, field in go_fields.items() if not field.omitempty
        }
        actual_required_value = schema.get("required", [])
        if (not isinstance(actual_required_value, list)
                or any(not isinstance(v, str) for v in actual_required_value)):
            errors.append(f"[openapi-required] {label}: required must be a string list")
            actual_required_value = []
        actual_required = set(actual_required_value)
        if len(actual_required) != len(actual_required_value):
            errors.append(f"[openapi-required] {label}: duplicate required property")
        if expected_required != actual_required:
            errors.append(
                f"[openapi-required] {label}\n"
                f"  missing={sorted(expected_required - actual_required)}\n"
                f"  extra={sorted(actual_required - expected_required)}"
            )
        for field_name in sorted(expected_names & actual_names & set(ts_fields)):
            go_field = go_fields[field_name]
            ts_field = ts_fields[field_name]
            property_schema = properties[field_name]
            collection = (
                go_field.go_type.lstrip("*").startswith("[]")
                or go_field.go_type.lstrip("*").startswith("map[")
            )
            if go_field.pointer and not go_field.omitempty:
                if not _schema_nullable(property_schema):
                    errors.append(
                        f"[openapi-null] {label}.{field_name}: required pointer must allow null"
                    )
            elif not go_field.pointer and not collection and _schema_nullable(property_schema):
                errors.append(
                    f"[openapi-null] {label}.{field_name}: non-pointer value must not allow null"
                )
            errors.extend(
                _compare_openapi_value(
                    f"{label}.{field_name}",
                    go_field.go_type,
                    ts_field.ts_type,
                    property_schema,
                    schemas,
                    nested_components,
                    go.string_types,
                    ts.aliases,
                )
            )
    return errors


def _component_schemas(document: object) -> dict | None:
    if not isinstance(document, dict):
        return None
    components = document.get('components')
    if not isinstance(components, dict):
        return None
    schemas = components.get('schemas')
    return schemas if isinstance(schemas, dict) else None


def _resolve_local_ref(document: dict, ref: object) -> object:
    if not isinstance(ref, str) or not ref.startswith('#/'):
        raise ValueError('only document-local JSON Pointer references are supported')
    node = document
    for token in ref[2:].split('/'):
        if re.search(r'~(?![01])', token):
            raise ValueError('invalid JSON Pointer escape')
        token = token.replace('~1', '/').replace('~0', '~')
        if isinstance(node, dict) and token in node:
            node = node[token]
        elif isinstance(node, list) and re.fullmatch(r'0|[1-9][0-9]*', token) and int(token) < len(node):
            node = node[int(token)]
        else:
            raise ValueError('reference target does not exist')
    return node


def _check_local_openapi_refs(document: dict) -> list[str]:
    errors: list[str] = []
    seen: set[int] = set()

    def visit(node: object, location: str) -> None:
        if not isinstance(node, (dict, list)):
            return
        if id(node) in seen:
            errors.append(f'[openapi-shape] {location}: recursive or aliased document')
            return
        seen.add(id(node))
        if isinstance(node, dict):
            if '$ref' in node:
                try:
                    _resolve_local_ref(document, node['$ref'])
                except ValueError as exc:
                    errors.append(f"[openapi-ref-missing] {location}: {node['$ref']!r}: {exc}")
            for key, value in node.items():
                # Example/extension payloads are data, not OpenAPI Reference Objects.
                if key not in ('example', 'examples', 'default', 'const', 'enum') and not str(key).startswith('x-'):
                    visit(value, f'{location}.{key}')
        else:
            for index, value in enumerate(node):
                visit(value, f'{location}[{index}]')
        seen.remove(id(node))

    visit(document, '$')
    return errors


def check_openapi_contract(
    document: dict,
    shared_go: GoSource,
    shared_ts: TsSource,
    company_go: GoSource,
    company_ts: TsSource,
) -> list[str]:
    schemas = _component_schemas(document)
    if schemas is None:
        return ["[openapi-shape] components.schemas must be an object"]
    dynamic_nested = dict(OPENAPI_COMPONENTS)
    if isinstance(schemas, dict):
        for name in schemas:
            dynamic_nested.setdefault(name, name)
    errors = _check_local_openapi_refs(document)
    # Shared models remain Go↔TS checked above, but many are persistence objects
    # whose JSON tags include object keys, lease fields and retention tombstones.
    # Binding those directly to OpenAPI before response DTOs exist would turn an
    # implementation leak into a public promise. Only the explicit company DTO
    # layer is an eligible HTTP contract source in this phase.
    errors.extend(
        check_openapi_pairs(
            company_pairs,
            OPENAPI_COMPANY_COMPONENTS,
            company_go,
            company_ts,
            document,
            dynamic_nested,
        )
    )
    return errors


# Bind reviewed handler outputs to schemas, reusing the Go-AST route inventory
# rather than adding another router parser. route_inventory_test.go proves that
# inventory matches the current Go source in the normal backend suite.
# Values: success status, data shape, referenced DTO component.
COMPANY_RESPONSES = {
    'c.Setup.Settings': ('200', 'nullable', 'CompanySettings'),
    'c.Setup.Configure': ('200', 'one', 'CompanySettings'),
    'c.Setup.Invitations': ('200', 'list', 'CompanyInvitation'),
    'c.Setup.Invite': ('200', 'one', 'CompanyInvitationIssued'),
    'c.Mailboxes.Mailboxes': ('200', 'list', 'CompanyMailboxAccess'),
    'c.Mailboxes.Grants': ('200', 'one', 'MailboxGrantSnapshot'),
    'c.Templates.Templates': ('200', 'list', 'CompanyTemplate'),
    'c.Templates.SaveTemplate': ('200', 'one', 'CompanyTemplate'),
    'c.Templates.Publish': ('200', 'one', 'CompanyTemplateVersion'),
    'c.Templates.Versions': ('200', 'list', 'CompanyTemplateVersion'),
    'c.Templates.UsableTemplates': ('200', 'list', 'CompanyTemplateVersion'),
    'c.Drafts.List': ('200', 'page', 'CompanyDraft'),
    'c.Drafts.Get': ('200', 'one', 'CompanyDraft'),
    'c.Drafts.Save': ('200', 'one', 'CompanyDraft'),
    'c.Mail.Submissions': ('200', 'page', 'Submission'),
    'c.Mail.Submission': ('200', 'one', 'Submission'),
    'c.Mail.SubmissionContent': ('200', 'one', 'SubmissionContent'),
    'c.Mail.SubmissionAttachments': ('200', 'list', 'SubmissionAttachment'),
    'c.Mail.ComposeReply': ('200', 'one', 'CompanyDraftPayload'),
    'c.Mail.UploadAttachment': ('200', 'one', 'CompanyAttachment'),
    'c.Recovery.Recovery': ('200', 'page', 'CompanyRecoveryReceipt'),
    'c.Recovery.InspectReceipt': ('200', 'one', 'CompanyRecoveryInspection'),
    'c.Recovery.Recipients': ('200', 'list', 'CompanyRecipient'),
    'c.Archive.List': ('200', 'page', 'ArchivedMail'),
    'c.Index.Status': ('200', 'one', 'ContentIndexStatus'),
    'c.Employees.Preview': ('200', 'one', 'OffboardingPlan'),
    'c.Employees.Execute': ('200', 'one', 'OffboardingPlan'),
    'c.Console.Overview': ('200', 'one', 'CompanyOverview'),
    'c.Console.Audit': ('200', 'page', 'CompanyAdminAudit'),
    'c.Console.Access': ('200', 'one', 'AccessExplanation'),
    'c.Domains.Create': ('201', 'one', 'CompanyDomain'),
    'c.Domains.List': ('200', 'list', 'CompanyDomain'),
    'c.Domains.Verify': ('200', 'one', 'DomainVerification'),
    'c.Domains.Verification': ('200', 'one', 'DomainVerification'),
    'c.Domains.Delete': ('200', 'one', 'CompanyDeleted'),
    'c.Drafts.Delete': ('200', 'one', 'CompanyDeleted'),
    'c.Setup.RevokeInvite': ('200', 'one', 'CompanyRevoked'),
    'c.Setup.Activate': ('200', 'one', 'CompanyActivated'),
    'c.Mailboxes.CreateMailbox': ('200', 'one', 'Mailbox'),
    'c.Mailboxes.Handover': ('200', 'one', 'CompanyTransferred'),
    'c.Mailboxes.ConvertShared': ('200', 'one', 'CompanyConverted'),
    'c.Mailboxes.Grant': ('200', 'one', 'CompanyRevisionUpdated'),
    'c.Mailboxes.MailboxSendPolicy': ('200', 'one', 'CompanyRevisionUpdated'),
    'c.Mail.Messages': ('200', 'page', 'Message'),
    'c.Mail.Message': ('200', 'one', 'MessageDetail'),
    'c.Mail.InboundAttachments': ('200', 'list', 'CompanyParsedAttachment'),
    'c.Mail.MessageAction': ('200', 'one', 'CompanyUpdated'),
    'c.Mail.SubmitDraft': ('201', 'one', 'OutboundJob'),
    'c.Index.Conversation': ('200', 'page', 'Message'),
    'c.Console.RetryIndex': ('200', 'one', 'CompanyIndexRetryResult'),
    'c.Archive.Change': ('200', 'one', 'CompanyRevisionUpdated'),
    'c.Recovery.InspectOutbound': ('200', 'one', 'CompanyOutboundInspection'),
    'c.Recovery.Reconcile': ('200', 'one', 'CompanyReconciled'),
    'c.Recovery.RetryReceipt': ('200', 'one', 'CompanyQueued'),
    'c.Templates.Preview': ('200', 'one', 'CompanyRenderedTemplate'),
    'c.Templates.Retire': ('200', 'one', 'CompanyUpdated'),
    'c.Templates.RevokeTemplateVersion': ('200', 'one', 'CompanyRevoked'),
    'c.Templates.TemplateGrants': ('200', 'list', 'CompanyTemplateGrant'),
    'c.Templates.TemplateGrant': ('200', 'one', 'CompanyUpdated'),
}
# A fresh submit and an idempotent replay have distinct success codes.
COMPANY_ALTERNATE_RESPONSES = {
    'c.Mail.SubmitDraft': ('200', 'one', 'OutboundJob'),
}
# Non-JSON bodies must not pass through a JSON envelope checker.
COMPANY_STREAMS = {
    'c.Mail.Attachment': 'application/octet-stream',
    'c.Mail.InboundAttachment': 'application/octet-stream',
    'c.Mail.InboundAttachmentByID': 'application/octet-stream',
    'c.Mail.Source': 'message/rfc822',
    'c.Mail.SubmissionAttachmentDownload': 'application/octet-stream',
    'c.Events.Events': 'text/event-stream',
}
COMPANY_ACK_FIELDS = {
    'CompanyDeleted': 'deleted', 'CompanyRevoked': 'revoked',
    'CompanyActivated': 'activated', 'CompanyTransferred': 'transferred',
    'CompanyConverted': 'converted', 'CompanyUpdated': 'updated',
    'CompanyReconciled': 'reconciled', 'CompanyQueued': 'queued',
}

COMPANY_REQUESTS = {
    'c.Setup.Configure': 'CompanySettingsInput',
    'c.Templates.SaveTemplate': 'CompanyTemplateInput',
    'c.Drafts.Save': 'CompanyDraftInput',
}


def _object_at(node: object, *keys: str) -> dict:
    for key in keys:
        if not isinstance(node, dict):
            return {}
        node = node.get(key)
    return node if isinstance(node, dict) else {}


def _required_names(schema: object) -> set[str]:
    value = _object_at(schema).get('required', [])
    return set(value) if isinstance(value, list) and all(isinstance(v, str) for v in value) else set()


def _is_component_ref(schema: dict, component: str, nullable: bool = False) -> bool:
    ref = {'$ref': '#/components/schemas/' + component}
    if nullable:
        return schema == {'anyOf': [ref, {'type': 'null'}]} or schema == {'anyOf': [{'type': 'null'}, ref]}
    return schema == ref


# These are request-specific shapes, not response DTOs. Incomplete drafts and
# initial settings must not acquire response-only required fields by accident.
INPUT_REQUIRED = {
    'CompanySettingsInput': {'name', 'primary_zone_id'},
    'CompanyTemplateInput': {'name', 'draft'},
    'CompanyTemplateDraftInput': {'subject'},
    'CompanyVariableInput': {'name', 'type', 'max_length'},
    'CompanyDraftInput': {'mailbox_id'},
    'CompanyDraftPayloadInput': set(),
    'CompanyRecipientOutcomeInput': {'address', 'state'},
}


def check_input_schemas(document: dict) -> list[str]:
    errors = []
    for name, required in INPUT_REQUIRED.items():
        schema = _object_at(document, 'components', 'schemas', name)
        declared = schema.get('required', [])
        if (schema.get('type') != 'object' or not isinstance(declared, list)
                or any(not isinstance(v, str) for v in declared)
                or len(declared) != len(_required_names(schema))
                or _required_names(schema) != required):
            errors.append(f'[openapi-input-required] {name}: expected {sorted(required)}')
    outcome = _object_at(document, 'components', 'schemas', 'CompanyRecipientOutcomeInput', 'properties', 'state')
    if outcome.get('enum') != ['accepted', 'temporary', 'permanent']:
        errors.append('[openapi-input-outcome] reconciliation must require a confirmed outcome')
    op = _object_at(document, 'paths', '/api/v1/company/outbound/{id}/reconcile', 'post')
    items = _object_at(op, 'requestBody', 'content', 'application/json', 'schema', 'properties', 'results', 'items')
    if not _is_component_ref(items, 'CompanyRecipientOutcomeInput'):
        errors.append('[openapi-input-outcome] reconciliation must not reuse the response ledger DTO')
    return errors


def _check_stream_response(label: str, op: dict, media_type: str) -> list[str]:
    response = _object_at(op, 'responses', '200')
    content = _object_at(response, 'content')
    errors = []
    expected_schema = {'type': 'string'} if media_type == 'text/event-stream' else {'type': 'string', 'format': 'binary'}
    if set(content) != {media_type} or _object_at(content, media_type, 'schema') != expected_schema:
        errors.append(f'[openapi-stream] {label}: requires {media_type}, not a JSON envelope')
    if media_type != 'text/event-stream':
        for name in ('Cache-Control', 'Content-Disposition', 'X-Content-Type-Options'):
            header = _object_at(response, 'headers', name)
            if header.get('required') is not True or _object_at(header, 'schema').get('type') != 'string':
                errors.append(f'[openapi-download-header] {label}: missing required {name}')
    return errors


def check_receipt_schemas(document: dict) -> list[str]:
    errors = []
    for name, field in COMPANY_ACK_FIELDS.items():
        schema = _object_at(document, 'components', 'schemas', name)
        props = _object_at(schema, 'properties')
        if (schema.get('type') != 'object' or set(props) != {field}
                or _required_names(schema) != {field}
                or props.get(field) != {'type': 'boolean', 'const': True}
                or schema.get('additionalProperties') is not False):
            errors.append(f'[openapi-receipt] {name}: closed {field}=true receipt required')
    return errors


def check_company_operations(document: dict, inventory: object) -> list[str]:
    if not isinstance(inventory, list) or not inventory:
        return ['[openapi-routes] missing nonempty Go-AST route inventory']
    rows = []
    errors: list[str] = []
    for row in inventory:
        if not isinstance(row, dict) or any(not isinstance(row.get(k), str) for k in ('method', 'path', 'handler')):
            return ['[openapi-routes] malformed Go-AST route inventory']
        if row['path'].startswith('/api/v1/company/'):
            rows.append(row)
    if not rows:
        return ['[openapi-routes] company route inventory is empty']
    keys = {(r['method'].lower(), r['path']) for r in rows}
    if len(keys) != len(rows):
        errors.append('[openapi-routes] duplicate company operation in inventory')
    paths = _object_at(document, 'paths')
    documented = {(method, path) for path, item in paths.items()
                  if isinstance(path, str) and path.startswith('/api/v1/company/')
                  and isinstance(item, dict) for method in item
                  if method in ('get', 'post', 'put', 'patch', 'delete', 'head', 'options')}
    for key in sorted(keys - documented):
        errors.append(f'[openapi-route-missing] {key}')
    for key in sorted(documented - keys):
        errors.append(f'[openapi-route-extra] {key}')
    handlers = {r['handler'] for r in rows}
    for handler in sorted((set(COMPANY_RESPONSES) | set(COMPANY_STREAMS)) - handlers):
        errors.append(f'[openapi-route-binding] reviewed handler disappeared: {handler}')
    for row in rows:
        op = _object_at(paths, row['path'], row['method'].lower())
        label = f"{row['method']} {row['path']}"
        binding = COMPANY_RESPONSES.get(row['handler'])
        stream = COMPANY_STREAMS.get(row['handler'])
        if binding is None and stream is None:
            errors.append(f'[openapi-route-binding] {label}: no reviewed response for {row["handler"]}')
        bindings = [binding] if binding is not None else []
        alternate = COMPANY_ALTERNATE_RESPONSES.get(row['handler'])
        if alternate is not None:
            bindings.append(alternate)
        expected_success = {b[0] for b in bindings} if bindings else {'200'}
        actual_success = {code for code in _object_at(op, 'responses') if str(code).startswith('2')}
        if op and actual_success != expected_success:
            errors.append(f'[openapi-status] {label}: expected success codes {sorted(expected_success)}')
        if stream is not None and op:
            errors.extend(_check_stream_response(label, op, stream))
        for status, shape, component in bindings if op else []:
            envelope = _object_at(op, 'responses', status, 'content', 'application/json', 'schema')
            if envelope.get('type') != 'object' or 'data' not in _required_names(envelope):
                errors.append(f'[openapi-envelope] {label}: required data object envelope missing')
            data = _object_at(envelope, 'properties', 'data')
            if shape in ('list', 'page'):
                if _schema_types(data) != {'array'}:
                    errors.append(f'[openapi-response] {label}: data must be an array')
                data = _object_at(data, 'items')
            elif shape.startswith('field:'):
                field = shape.split(':', 1)[1]
                if data.get('type') != 'object' or field not in _required_names(data):
                    errors.append(f'[openapi-response] {label}: missing required data.{field}')
                data = _object_at(data, 'properties', field)
            if not _is_component_ref(data, component, nullable=shape == 'nullable'):
                errors.append(f'[openapi-response] {label}: expected {component}, got {data}')
            if shape == 'page':
                meta = _object_at(envelope, 'properties', 'meta')
                if ('meta' not in _required_names(envelope) or meta.get('type') != 'object'
                        or not {'page', 'per_page', 'total'} <= _required_names(meta)
                        or any(_object_at(meta, 'properties', k).get('type') != 'integer'
                               for k in ('page', 'per_page', 'total'))):
                    errors.append(f'[openapi-pagination] {label}: pagination metadata missing')
        request_component = COMPANY_REQUESTS.get(row['handler'])
        if request_component is not None and op:
            request = _object_at(op, 'requestBody', 'content', 'application/json', 'schema')
            if not _is_component_ref(request, request_component):
                errors.append(f'[openapi-request] {label}: requires separate {request_component}')
    return errors


def check_shared(go_text: str, ts_text: str) -> list[str]:
    return check_pairs(SHARED_PAIRS, [go_text], [ts_text], NESTED_MAP)


def check_company(go_texts: list[str], ts_texts: list[str]) -> list[str]:
    return check_pairs(company_pairs, go_texts, ts_texts, NESTED_MAP)


def main() -> int:
    shared_go_text = GO_MODELS.read_text()
    shared_ts_text = TS_TYPES.read_text()
    company_go_texts = [
        path.read_text()
        for path in sorted((ROOT / "internal/company").glob("*.go"))
        if not path.name.endswith("_test.go")
    ]
    # This administrative projection is nested in MailboxGrantSnapshot. It
    # contains no object keys or credential material; verify it explicitly.
    company_go_texts.append((ROOT / 'internal/models/mailbox_grants.go').read_text())
    company_ts_texts = [
        (ROOT / "web/lib/company.ts").read_text(),
        (ROOT / "web/features/mail/api.ts").read_text(),
        (ROOT / "web/features/company/api.ts").read_text(),
    ]

    problems = check_shared(shared_go_text, shared_ts_text)
    problems += check_company(company_go_texts, company_ts_texts)

    parse_problems: list[str] = []
    shared_go = merge_go_sources([shared_go_text], parse_problems)
    shared_ts = merge_ts_sources([shared_ts_text], parse_problems)
    company_go = merge_go_sources(company_go_texts, parse_problems)
    company_ts = merge_ts_sources(company_ts_texts, parse_problems)
    # check_shared/check_company already report these; do not duplicate them.

    openapi_document, openapi_problems = parse_openapi_text(OPENAPI_SPEC.read_text())
    problems += openapi_problems
    if openapi_document is not None:
        problems += check_openapi_contract(
            openapi_document,
            shared_go,
            shared_ts,
            company_go,
            company_ts,
        )

    if openapi_document is not None:
        try:
            inventory = json.loads((ROOT / 'docs/company-mail/evidence/R5-API-MATRIX.json').read_text())
            problems += check_company_operations(openapi_document, inventory)
            problems += check_input_schemas(openapi_document)
            problems += check_receipt_schemas(openapi_document)
        except (OSError, ValueError) as exc:
            problems.append(f'[openapi-routes] cannot load route inventory: {exc}')

    if problems:
        print("Contract drift detected across Go DTOs, TypeScript projections and OpenAPI:\n")
        print("\n".join(problems))
        return 1

    print(
        f"Contract check passed: {len(SHARED_TYPES)} shared models verified across "
        f"Go and TypeScript; {len(company_pairs)} explicit company DTOs additionally "
        f"verified against OpenAPI component schemas for fields, required/optional "
        "wire semantics, nullability, scalar/array/map projection, nested references "
        "and string enums."
    )
    company_rows = [r for r in inventory if r['path'].startswith('/api/v1/company/')]
    typed = sum(r['handler'] in COMPANY_RESPONSES for r in company_rows)
    streamed = sum(r['handler'] in COMPANY_STREAMS for r in company_rows)
    print(f"OpenAPI routing: {len(company_rows)} company operations; {typed} JSON bindings, "
          f"{streamed} non-JSON bindings, including both submit success statuses. "
          "Runtime response coverage is reported separately by check_http_contract.py.")
    return 0


if __name__ == "__main__":
    sys.exit(main())
