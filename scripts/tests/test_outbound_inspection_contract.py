"""P2-060 static inspection DTO/OpenAPI witnesses, not HTTP or PG acceptance.

This suite reads the checked-in source and specification only. Its small schema
evaluator covers the inspection response keywords, not arbitrary JSON Schema.
"""
import copy
import importlib.util
from pathlib import Path
import re
import sys
import unittest


ROOT = Path(__file__).resolve().parents[2]
spec = importlib.util.spec_from_file_location(
    "inspection_openapi_gate", ROOT / "scripts/check_contract_drift.py"
)
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)
DOC, ERRORS = gate.parse_openapi_text((ROOT / "internal/api/openapi.yaml").read_text())
SCHEMAS = DOC["components"]["schemas"] if DOC else {}
SOURCE = (ROOT / "internal/company/outbound_inspection.go").read_text()
ROUTE = "/api/v1/company/outbound/{id}/inspect"
JOB = "CompanyOutboundInspectionJob"
RECIPIENT = "CompanyOutboundInspectionRecipient"
HEADERS = "CompanyOutboundInspectionHeaders"
STATES = {"pending", "accepted", "temporary", "permanent", "uncertain", "unknown"}
SAFE_HEADERS = {
    "From", "To", "Cc", "Subject", "Date", "Message-Id", "Reply-To",
    "In-Reply-To", "References", "Mime-Version", "Content-Type",
    "Content-Transfer-Encoding", "Content-Disposition", "Content-Id",
}
FORBIDDEN = {
    "lease_until", "claimed_at", "delivery_token", "claim_token", "object_key",
    "raw_object_key", "raw_mime", "headers_json", "in_flight_domain",
    "last_error", "smtp_response", "diagnostic", "dkim_private_key",
    "smtp_password", "api_key", "api_key_secret", "jwt_secret", "password",
    "content_digest", "submit_actor", "idempotency_key", "request_hash",
}
ORDINARY_FORBIDDEN = FORBIDDEN | {
    "subject", "mail_from", "from", "to", "cc", "bcc", "rcpt_to", "recipients",
    "mailbox_id", "attachment_count", "text_body", "html_body", "headers",
    "smtp_code", "user_id", "api_key_id", "sender_user_id", "content_redacted",
    "delivered_domains", "message_id_header", "max_attempts", "address", "kind",
    "enhanced_code",
}
RECEIPT_KEYS = {
    "id", "tenant_id", "state", "status", "progress", "created_at", "updated_at",
    "attempt_count", "next_retry", "delivery_uncertain", "capabilities",
}
ORDINARY_COMPONENTS = {
    "OutboundReceipt", "OutboundReceiptProgress", "OutboundReceiptCounts",
    "SubmissionCapabilities",
}


def ordinary_closure(schema, schemas, trail=()):
    """Expand only this receipt's local refs; reject unsupported schema shapes.

    This is a boundary assertion, not a permissive JSON Schema interpreter.
    Walk schema-bearing keywords only (never descriptions or enum values).
    """
    if not isinstance(schema, dict):
        raise AssertionError("ordinary schema must be an object")
    if "$ref" in schema:
        ref = schema["$ref"]
        if set(schema) != {"$ref"}:
            raise AssertionError("ordinary $ref siblings unsupported")
        if not isinstance(ref, str) or not re.fullmatch(r"#/components/schemas/[A-Za-z0-9_]+", ref):
            raise AssertionError(f"unsupported ordinary local ref: {ref!r}")
        name = ref.rsplit("/", 1)[1]
        if name not in schemas:
            raise AssertionError(f"unknown ordinary ref: {ref}")
        if name in trail:
            raise AssertionError(f"cyclic ordinary ref: {' -> '.join((*trail, name))}")
        if name not in ORDINARY_COMPONENTS | {"OutboundJob", "Submission"}:
            raise AssertionError(f"non-ordinary component in receipt closure: {name}")
        return ordinary_closure(schemas[name], schemas, (*trail, name))
    supported = {"type", "format", "description", "additionalProperties", "required",
                 "properties", "enum", "minimum", "oneOf", "not"}
    unknown = set(schema) - supported
    if unknown:
        raise AssertionError(f"unsupported ordinary keywords: {sorted(unknown)}")
    if schema.get("type") == "object" and schema.get("additionalProperties") is not False:
        raise AssertionError("ordinary object must be closed")
    props = schema.get("properties", {})
    forbidden = set(props) & ORDINARY_FORBIDDEN
    if forbidden:
        raise AssertionError(f"forbidden ordinary fields: {sorted(forbidden)}")
    out = copy.deepcopy(schema)
    if "properties" in schema:
        out["properties"] = {k: ordinary_closure(v, schemas, trail) for k, v in props.items()}
    if "oneOf" in schema:
        out["oneOf"] = [ordinary_closure(v, schemas, trail) for v in schema["oneOf"]]
    if "not" in schema:
        out["not"] = ordinary_closure(schema["not"], schemas, trail)
    return out


def assert_ordinary_boundary(schemas):
    canonical = ordinary_closure({"$ref": "#/components/schemas/OutboundReceipt"}, schemas)
    if set(canonical["properties"]) != RECEIPT_KEYS:
        raise AssertionError("ordinary receipt field set changed")
    for alias in ("OutboundJob", "Submission"):
        expanded = ordinary_closure({"$ref": "#/components/schemas/" + alias}, schemas)
        if expanded != canonical:
            raise AssertionError(f"ordinary alias differs from receipt: {alias}")
    return canonical


def body(source, name):
    match = re.search(r"func (?:\([^)]*\) )?" + name + r"\([^\n]*\) [^\n]*\{(.*?)\n\}", source, re.S)
    if match is None:
        raise AssertionError(f"missing Go function: {name}")
    return match.group(1)


def accepts(schema, value):
    """Evaluate only the keywords present in these focused response schemas."""
    if "$ref" in schema:
        return accepts(SCHEMAS[schema["$ref"].split("/")[-1]], value)
    if "anyOf" in schema and not any(accepts(s, value) for s in schema["anyOf"]):
        return False
    if "enum" in schema and value not in schema["enum"]:
        return False
    kind = (
        "null" if value is None else "boolean" if isinstance(value, bool)
        else "integer" if isinstance(value, int) else "string" if isinstance(value, str)
        else "object" if isinstance(value, dict) else "array" if isinstance(value, list)
        else "number"
    )
    types = schema.get("type")
    types = [types] if isinstance(types, str) else types
    if types and kind not in types:
        return False
    if isinstance(value, dict):
        props = schema.get("properties", {})
        if any(k not in value for k in schema.get("required", [])):
            return False
        if schema.get("additionalProperties") is False and set(value) - set(props):
            return False
        if any(not accepts(props[k], v) for k, v in value.items() if k in props):
            return False
    if isinstance(value, list):
        if len(value) > schema.get("maxItems", float("inf")):
            return False
        if "items" in schema and not all(accepts(schema["items"], v) for v in value):
            return False
    if isinstance(value, int) and not isinstance(value, bool):
        if value < schema.get("minimum", -float("inf")) or value > schema.get("maximum", float("inf")):
            return False
    if isinstance(value, str) and "pattern" in schema:
        if re.search(schema["pattern"], value) is None:
            return False
    return True


def witness():
    now = "2026-10-02T01:00:00Z"
    return {
        "job": {
            "id": "11111111-1111-4111-8111-111111111111",
            "tenant_id": "22222222-2222-4222-8222-222222222222",
            "state": "sent", "status": "partially_accepted",
            "created_at": now, "updated_at": now,
            "mail_from": "sender@fixture.test", "to": ["public@fixture.test"],
            "cc": [], "bcc": ["hidden@fixture.test"], "subject": "inspection",
            "text_body": "controlled body", "html_body": "<p>controlled body</p>",
            "headers": {"Content-Type": "text/plain", "Message-Id": "<safe@fixture.test>"},
        },
        "recipients": [{
            "address": "hidden@fixture.test", "kind": "bcc", "state": "permanent",
            "attempts": 2, "smtp_code": 550, "enhanced_code": "5.1.1",
            "diagnostic_class": "permanent", "updated_at": now,
        }],
    }


class OutboundInspectionContractTests(unittest.TestCase):
    def test_document_and_exclusive_response_refs(self):
        self.assertEqual(ERRORS, [])
        response = SCHEMAS["CompanyOutboundInspection"]
        self.assertEqual(response["properties"]["job"]["$ref"], "#/components/schemas/" + JOB)
        self.assertEqual(response["properties"]["recipients"]["items"]["$ref"], "#/components/schemas/" + RECIPIENT)
        self.assertEqual(SCHEMAS[JOB]["properties"]["headers"]["$ref"], "#/components/schemas/" + HEADERS)
        for path, methods in DOC["paths"].items():
            for method, operation in methods.items():
                if isinstance(operation, dict) and "#/components/schemas/CompanyOutboundInspection" in repr(operation):
                    self.assertEqual((path, method), (ROUTE, "post"))

    def test_dto_tags_required_optional_and_closed_objects(self):
        for go_name, component in (
            ("OutboundInspection", "CompanyOutboundInspection"),
            ("OutboundInspectionJob", JOB),
            ("OutboundInspectionRecipient", RECIPIENT),
        ):
            match = re.search(r"type " + go_name + r" struct \{(.*?)\n\}", SOURCE, re.S)
            self.assertIsNotNone(match, go_name)
            tags = re.findall(r'json:"([^",]+)(,omitempty)?"', match.group(1))
            schema = SCHEMAS[component]
            self.assertEqual({tag for tag, _ in tags}, set(schema["properties"]), go_name)
            self.assertEqual({tag for tag, optional in tags if not optional}, set(schema["required"]), go_name)
            self.assertIs(schema["additionalProperties"], False)
            self.assertFalse(any(gate._schema_nullable(p) for p in schema["properties"].values()))
            self.assertNotIn("models.OutboundJob", match.group(1))
            self.assertNotIn("json:\"diagnostic\"", match.group(1))
        # Projection initializes empty slices/maps, so absent values are []/{},
        # never null, regardless of nil storage fields.
        projection = body(SOURCE, "ProjectOutboundInspection")
        for key in ("To", "CC", "BCC"):
            self.assertIn(f"{key}: append([]string{{}}, j.{key}...)", projection)
        self.assertIn("Recipients: make([]OutboundInspectionRecipient, 0, len(ledger))", projection)
        self.assertIn("out := map[string]string{}", body(SOURCE, "inspectionHeaders"))

    def test_enums_match_projection_including_unknown(self):
        model = (ROOT / "internal/models/models.go").read_text()
        states = set(re.findall(r'Outbound\w+\s+OutboundState = "([^"]+)"', model))
        self.assertEqual(set(SCHEMAS[JOB]["properties"]["state"]["enum"]), states)
        delivery = (ROOT / "internal/delivery/projection.go").read_text()
        constants = dict(re.findall(r'(Submission\w+)\s+= "([^"]+)"', delivery))
        returned = set(re.findall(r"return (Submission\w+)", body(delivery, "DeriveSubmissionStatus")))
        self.assertEqual(set(SCHEMAS[JOB]["properties"]["status"]["enum"]), {constants[k] for k in returned})
        for key in ("state", "diagnostic_class"):
            self.assertEqual(set(SCHEMAS[RECIPIENT]["properties"][key]["enum"]), STATES)
        self.assertEqual(set(SCHEMAS[RECIPIENT]["properties"]["kind"]["enum"]), {"to", "cc", "bcc", "envelope"})
        projection = body(SOURCE, "ProjectOutboundInspection")
        self.assertIn('state = "unknown"', projection)
        self.assertIn("DiagnosticClass: state", projection)
        self.assertIn('kind = "envelope"', projection)
        value = witness()
        value["recipients"][0].update(state="unknown", diagnostic_class="unknown", kind="envelope")
        self.assertTrue(accepts(SCHEMAS["CompanyOutboundInspection"], value))
        for key in ("state", "diagnostic_class"):
            value["recipients"][0][key] = "raw-SMTP-secret"
            self.assertFalse(accepts(SCHEMAS["CompanyOutboundInspection"], value))
            value["recipients"][0][key] = "unknown"

    def test_safe_header_exact_allowlist_no_wildcards_or_secrets(self):
        header_schema = SCHEMAS[HEADERS]
        switch = body(SOURCE, "inspectionHeaders").split("switch canonical {", 1)[1].split("default:", 1)[0]
        source_names = set(re.findall(r'"([^"\n]+)"', switch))
        self.assertEqual(source_names, SAFE_HEADERS)
        self.assertEqual(set(header_schema["properties"]), source_names)
        self.assertIs(header_schema["additionalProperties"], False)
        self.assertNotIn("patternProperties", header_schema)
        self.assertTrue(accepts(header_schema, {}))
        for name in SAFE_HEADERS:
            self.assertTrue(accepts(header_schema, {name: "safe-value"}))
            for value in (None, 42, ["safe"], "safe\r\nX-Secret: secret", "safe\n"):
                self.assertFalse(accepts(header_schema, {name: value}), (name, value))
        for name in ("Bcc", "Received", "Authorization", "Dkim-Signature", "X-Api-Key", "X-Jwt", "Content-Secret", "X-Custom"):
            self.assertFalse(accepts(header_schema, {name: "system-secret"}), name)
        self.assertIn('strings.ContainsAny(value, "\\r\\n")', body(SOURCE, "inspectionHeaders"))

    def test_forbidden_model_fields_rejected_at_every_inspection_layer(self):
        value = witness()
        schema = SCHEMAS["CompanyOutboundInspection"]
        self.assertTrue(accepts(schema, value))
        for forbidden in FORBIDDEN:
            for layer in ("root", "job", "recipient"):
                changed = copy.deepcopy(value)
                target = changed if layer == "root" else changed["job"] if layer == "job" else changed["recipients"][0]
                target[forbidden] = "private-system-secret"
                self.assertFalse(accepts(schema, changed), (forbidden, layer))

    def test_nullable_optional_empty_and_mutation_witnesses(self):
        schema = SCHEMAS["CompanyOutboundInspection"]
        value = witness()
        value["job"].update(to=[], cc=[], bcc=[], headers={}, text_body="", html_body="")
        value["recipients"][0].pop("enhanced_code")
        self.assertTrue(accepts(schema, value))
        for key in value["job"]:
            bad = copy.deepcopy(value)
            bad["job"][key] = None
            self.assertFalse(accepts(schema, bad), key)
            bad["job"].pop(key)
            self.assertFalse(accepts(schema, bad), key)
        for key in ("job", "recipients"):
            bad = copy.deepcopy(value)
            bad[key] = None
            self.assertFalse(accepts(schema, bad), key)
        bad = copy.deepcopy(value)
        bad["recipients"][0]["enhanced_code"] = None
        self.assertFalse(accepts(schema, bad))
        # An accidental re-opened schema demonstrably admits the forbidden key.
        mutant = copy.deepcopy(SCHEMAS[JOB])
        mutant["additionalProperties"] = True
        self.assertTrue(accepts(mutant, {**value["job"], "lease_until": "secret"}))
        self.assertFalse(accepts(SCHEMAS[JOB], {**value["job"], "lease_until": "secret"}))

    def test_numeric_and_enhanced_smtp_codes_are_sanitized_not_raw_diagnostics(self):
        props = SCHEMAS[RECIPIENT]["properties"]
        for code in (0, 200, 250, 599):
            self.assertTrue(accepts(props["smtp_code"], code))
        for code in (-1, 1, 199, 600, None, "550", True):
            self.assertFalse(accepts(props["smtp_code"], code), code)
        for code in ("2.0.0", "4.100.999", "5.1.1"):
            self.assertTrue(accepts(props["enhanced_code"], code))
        for code in ("", "550 5.1.1 raw-secret", "5.1234.1", "6.1.1", "5.1.1\n", None):
            self.assertFalse(accepts(props["enhanced_code"], code), code)
        projection = body(SOURCE, "ProjectOutboundInspection")
        self.assertIn("if code < 200 || code > 599", projection)
        self.assertIn("code = 0", projection)
        self.assertIn("enhanced = match[1]", projection)

    def test_formal_route_authority_reason_and_strong_errors_unchanged(self):
        operation = DOC["paths"][ROUTE]["post"]
        self.assertEqual(operation["security"], [{"BearerAuth": []}])
        self.assertTrue(operation["requestBody"]["required"])
        request = operation["requestBody"]["content"]["application/json"]["schema"]
        self.assertEqual(request["required"], ["reason"])
        self.assertEqual(set(request["properties"]), {"reason"})
        self.assertIs(request["additionalProperties"], False)
        reason = request["properties"]["reason"]
        self.assertEqual((reason["minLength"], reason["maxLength"]), (8, 1000))
        self.assertIn("UTF-8 bytes", reason["description"])
        for status in ("400", "401", "403", "404", "409", "429", "500"):
            self.assertEqual(operation["responses"][status]["$ref"], "#/components/responses/CompanyFailure")
        router = (ROOT / "internal/api/handlers/company_routes.go").read_text()
        self.assertIn('r.With(middleware.RequireSuperAdmin).Post("/outbound/{id}/inspect", c.Recovery.InspectOutbound)', router)
        handler = body((ROOT / "internal/api/handlers/company_recovery.go").read_text(), "InspectOutbound")
        self.assertIn("h.inspector.InspectOutboundRecovery", handler)
        self.assertNotIn("AccessibleOutboundJob", handler)
        store = body((ROOT / "internal/store/postgres/outbound_inspection.go").read_text(), "InspectOutboundRecovery")
        for token in ("recoveryReferencedActor(ctx, tx, a)", "company.ProjectOutboundInspection(j, ledger)", "companyAudit(ctx, tx,", "tx.Commit(ctx)"):
            self.assertIn(token, store)
        self.assertLess(store.index("companyAudit(ctx, tx,"), store.index("tx.Commit(ctx)"))
        self.assertLess(store.index("tx.Commit(ctx)"), store.rindex("return v, nil"))

    def test_exception_and_acceptance_semantics_are_explicit(self):
        description = SCHEMAS[JOB]["description"] + " " + DOC["paths"][ROUTE]["post"]["description"]
        self.assertIn("ordinary", description)
        self.assertIn("BCC", description)
        self.assertIn("next-hop", SCHEMAS[JOB]["properties"]["status"]["description"])
        self.assertIn("not final delivery", SCHEMAS[RECIPIENT]["properties"]["state"]["description"])
        self.assertNotIn("delivered", SCHEMAS[JOB]["properties"]["status"]["enum"])
        self.assertEqual(SCHEMAS["CompanyOutboundInspection"]["properties"]["recipients"]["maxItems"], 50)

    def test_ordinary_receipt_schemas_not_expanded(self):
        # The former direct ["properties"] access raised KeyError on the R5
        # alias. Preserve its boundary intent, not its pre-R5 raw-model fields.
        ordinary = assert_ordinary_boundary(SCHEMAS)
        self.assertEqual(set(ordinary["properties"]), RECEIPT_KEYS)
        for filename, names in (
            ("outbound_receipt.go", ("OutboundReceipt", "OutboundReceiptProgress", "OutboundReceiptCounts")),
            ("submissions.go", ("SubmissionCapabilities",)),
        ):
            source = (ROOT / "internal/company" / filename).read_text()
            go, errors = gate.parse_go_source(source)
            self.assertEqual(errors, [])
            for name in names:
                schema = ordinary_closure(SCHEMAS[name], SCHEMAS)
                self.assertEqual(set(schema["properties"]), set(go.structs[name]), name)
                self.assertEqual(set(schema["required"]), {
                    key for key, field in go.structs[name].items() if not field.omitempty
                }, name)
        self.assertNotIn("Inspection", repr(ordinary))
        # CompanyRecipient is a separate legacy/recovery shape, not an ordinary
        # receipt dependency. Keep its old baseline without blessing exposure.
        self.assertEqual(set(SCHEMAS["CompanyRecipient"]["properties"]), {
            "address", "state", "smtp_code", "diagnostic", "attempts", "updated_at",
        })
        self.assertEqual(set(SCHEMAS["CompanyRecipient"]["properties"]["state"]["enum"]), STATES - {"unknown"})

    def test_ordinary_schema_pollution_fails_at_every_closure_object(self):
        assert_ordinary_boundary(SCHEMAS)  # unmodified positive control
        for name in ORDINARY_COMPONENTS:
            for key in ORDINARY_FORBIDDEN:
                with self.subTest(component=name, forbidden=key):
                    mutant = copy.deepcopy(SCHEMAS)
                    mutant[name]["properties"][key] = {"type": "string"}
                    with self.assertRaisesRegex(AssertionError, "forbidden ordinary fields"):
                        assert_ordinary_boundary(mutant)
            mutant = copy.deepcopy(SCHEMAS)
            mutant[name]["additionalProperties"] = True
            with self.subTest(component=name, reopened=True):
                with self.assertRaisesRegex(AssertionError, "ordinary object must be closed"):
                    assert_ordinary_boundary(mutant)

    def test_ordinary_alias_and_nested_inspection_refs_fail(self):
        for alias in ("OutboundJob", "Submission"):
            for target in ("CompanyOutboundInspection", JOB, RECIPIENT, HEADERS, "CompanyRecipient"):
                mutant = copy.deepcopy(SCHEMAS)
                mutant[alias] = {"$ref": "#/components/schemas/" + target}
                with self.subTest(alias=alias, target=target):
                    with self.assertRaisesRegex(AssertionError, "non-ordinary component"):
                        assert_ordinary_boundary(mutant)
        mutant = copy.deepcopy(SCHEMAS)
        mutant["OutboundReceipt"]["properties"]["progress"] = {"$ref": "#/components/schemas/" + JOB}
        with self.assertRaisesRegex(AssertionError, "non-ordinary component"):
            assert_ordinary_boundary(mutant)
        mutant = copy.deepcopy(SCHEMAS)
        mutant["OutboundJob"] = copy.deepcopy(SCHEMAS["OutboundReceipt"])
        mutant["OutboundJob"]["properties"]["private_future_field"] = {"type": "string"}
        with self.assertRaisesRegex(AssertionError, "ordinary alias differs"):
            assert_ordinary_boundary(mutant)

    def test_ordinary_local_refs_fail_explicitly_on_unknown_cycles_and_unsupported(self):
        for ref, message in (
            ("#/components/schemas/MissingReceipt", "unknown ordinary ref"),
            ("https://fixture.test/receipt.json", "unsupported ordinary local ref"),
            ("#/components/responses/OutboundReceipt", "unsupported ordinary local ref"),
            ("#/components/schemas/OutboundReceipt/properties/id", "unsupported ordinary local ref"),
        ):
            mutant = copy.deepcopy(SCHEMAS)
            mutant["OutboundJob"] = {"$ref": ref}
            with self.subTest(ref=ref):
                with self.assertRaisesRegex(AssertionError, message):
                    assert_ordinary_boundary(mutant)
        for target in ("OutboundJob", "Submission"):
            mutant = copy.deepcopy(SCHEMAS)
            mutant["OutboundJob"] = {"$ref": "#/components/schemas/" + target}
            mutant[target] = {"$ref": "#/components/schemas/OutboundJob"}
            with self.subTest(cycle=target):
                with self.assertRaisesRegex(AssertionError, "cyclic ordinary ref"):
                    assert_ordinary_boundary(mutant)
        mutant = copy.deepcopy(SCHEMAS)
        mutant["OutboundReceiptCounts"]["properties"]["total"] = {"$ref": "#/components/schemas/OutboundReceipt"}
        with self.assertRaisesRegex(AssertionError, "cyclic ordinary ref"):
            assert_ordinary_boundary(mutant)
        for extra, message in (({"allOf": []}, "unsupported ordinary keywords"),
                               ({"patternProperties": {}}, "unsupported ordinary keywords")):
            mutant = copy.deepcopy(SCHEMAS)
            mutant["OutboundReceipt"].update(extra)
            with self.assertRaisesRegex(AssertionError, message):
                assert_ordinary_boundary(mutant)
        mutant = copy.deepcopy(SCHEMAS)
        mutant["OutboundJob"]["properties"] = {"diagnostic": {"type": "string"}}
        with self.assertRaisesRegex(AssertionError, r"ordinary \$ref siblings unsupported"):
            assert_ordinary_boundary(mutant)


if __name__ == "__main__":
    unittest.main()
