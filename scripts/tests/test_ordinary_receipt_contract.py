"""Ordinary receipt OpenAPI witnesses; static checks are not runtime acceptance.

The formal source-version runner prepares fresh bytes with the Go fixture test
and supplies a source/build/run receipt. Missing evidence fails this test.
"""
import copy
import importlib.util
import json
import os
from pathlib import Path
import re
import sys
import unittest

ROOT = Path(__file__).resolve().parents[2]
sys.path.insert(0, str(ROOT / 'scripts'))
spec = importlib.util.spec_from_file_location("ordinary_receipt_openapi_gate", ROOT / "scripts/check_contract_drift.py")
gate = importlib.util.module_from_spec(spec)
sys.modules[spec.name] = gate
spec.loader.exec_module(gate)
DOC, ERRORS = gate.parse_openapi_text((ROOT / "internal/api/openapi.yaml").read_text())
SCHEMAS = DOC["components"]["schemas"] if DOC else {}
RECEIPT_SOURCE = (ROOT / "internal/company/outbound_receipt.go").read_text()
CONTENT_SOURCE = (ROOT / "internal/company/submissions.go").read_text()
RECEIPT_KEYS = {"id", "tenant_id", "state", "status", "progress", "created_at", "updated_at", "attempt_count", "next_retry", "delivery_uncertain", "capabilities"}
FORBIDDEN = {"subject", "mail_from", "from", "to", "cc", "bcc", "rcpt_to", "recipients", "mailbox_id", "attachment_count", "text_body", "html_body", "headers", "headers_json", "smtp_code", "smtp_response", "last_error", "diagnostic", "lease_until", "delivery_token", "claim_token", "object_key", "raw_mime", "user_id", "api_key_id", "sender_user_id", "content_redacted"}
ROUTES = {
    ("/api/v1/outbound", "get", 200): True,
    ("/api/v1/outbound/{id}", "get", 200): False,
    ("/api/v1/outbound/{id}/attempts", "get", 200): False,
    ("/api/v1/outbound/{id}/retry", "post", 200): False,
    ("/api/v1/company/submissions", "get", 200): True,
    ("/api/v1/company/submissions/{id}", "get", 200): False,
    ("/api/v1/company/drafts/{id}/submit", "post", 200): False,
    ("/api/v1/company/drafts/{id}/submit", "post", 201): False,
}
CONTENT_ROUTE = ("/api/v1/company/submissions/{id}/content", "get", 200)


def resolve(schema):
    while "$ref" in schema:
        schema = SCHEMAS[schema["$ref"].split("/")[-1]]
    return schema


def response(route):
    path, method, status = route
    return DOC["paths"][path][method]["responses"][str(status)]["content"]["application/json"]["schema"]


def accepts(schema, value):
    """Focused evaluator for these schemas only, not a general OAS validator."""
    schema = resolve(schema)
    if "oneOf" in schema and sum(accepts(s, value) for s in schema["oneOf"]) != 1:
        return False
    if "not" in schema and accepts(schema["not"], value):
        return False
    if "enum" in schema and value not in schema["enum"]:
        return False
    kind = "null" if value is None else "boolean" if isinstance(value, bool) else "integer" if isinstance(value, int) else "string" if isinstance(value, str) else "object" if isinstance(value, dict) else "array" if isinstance(value, list) else "number"
    types = schema.get("type")
    if types and kind not in ([types] if isinstance(types, str) else types):
        return False
    if isinstance(value, dict):
        props = schema.get("properties", {})
        if set(schema.get("required", [])) - set(value):
            return False
        extra = schema.get("additionalProperties", True)
        if extra is False and set(value) - set(props):
            return False
        for key, item in value.items():
            if key in props and not accepts(props[key], item):
                return False
            if key not in props and isinstance(extra, dict) and not accepts(extra, item):
                return False
    if isinstance(value, list) and "items" in schema and not all(accepts(schema["items"], item) for item in value):
        return False
    if isinstance(value, int) and not isinstance(value, bool) and value < schema.get("minimum", -float("inf")):
        return False
    return True


def minimal():
    return {"id": "11111111-1111-4111-8111-111111111111", "state": "pending", "status": "needs_attention", "progress": {"completeness": "unknown"}, "delivery_uncertain": False, "capabilities": {"view_content": False, "retry": False, "retry_block_reason": "unknown"}}


class OrdinaryReceiptContractTests(unittest.TestCase):
    def test_parse_and_typed_closed_field_sets(self):
        self.assertEqual(ERRORS, [])
        for source, name in ((RECEIPT_SOURCE, "OutboundReceipt"), (RECEIPT_SOURCE, "OutboundReceiptProgress"), (RECEIPT_SOURCE, "OutboundReceiptCounts"), (CONTENT_SOURCE, "SubmissionCapabilities"), (CONTENT_SOURCE, "SubmissionContent")):
            go, errors = gate.parse_go_source(source)
            self.assertEqual(errors, [])
            fields = go.structs[name]
            schema = resolve(SCHEMAS[name])
            self.assertEqual(set(fields), set(schema["properties"]), name)
            self.assertEqual({k for k, v in fields.items() if not v.omitempty}, set(schema["required"]), name)
            self.assertIs(schema["additionalProperties"], False, name)
            for key in fields:
                self.assertEqual(gate._schema_nullable(schema["properties"][key]), name == "SubmissionContent" and key == "bcc", (name, key))
        self.assertEqual(set(SCHEMAS["OutboundReceipt"]["properties"]), RECEIPT_KEYS)
        self.assertRegex(CONTENT_SOURCE, r"type Submission = OutboundReceipt")

    def test_all_actual_routes_are_closed_receipt_envelopes(self):
        for route, listing in ROUTES.items():
            schema = resolve(response(route))
            self.assertIs(schema["additionalProperties"], False, route)
            self.assertEqual(set(schema["properties"]), {"data", "meta"} if listing else {"data"}, route)
            self.assertEqual(set(schema["required"]), set(schema["properties"]), route)
            payload = schema["properties"]["data"]
            if listing:
                self.assertEqual(payload["type"], "array")
                payload = payload["items"]
                self.assertIs(resolve(schema["properties"]["meta"])["additionalProperties"], False)
            self.assertEqual(resolve(payload), SCHEMAS["OutboundReceipt"], route)
        for alias in ("Submission", "OutboundJob"):
            self.assertEqual(resolve(SCHEMAS[alias]), SCHEMAS["OutboundReceipt"])
        router = (ROOT / "internal/api/router.go").read_text()
        for path, handler in (("/outbound", "ListJobs"), ("/outbound/{id}", "GetJob"), ("/outbound/{id}/attempts", "ListAttempts"), ("/outbound/{id}/retry", "RetryJob")):
            self.assertIn(f'("{path}", oh.{handler})', router)
        handlers = (ROOT / "internal/api/handlers/outbound.go").read_text()
        for call in ("OutboundReceiptView", "ListOutboundReceiptViews", "OutboundAttemptViews", "CommittedReceiptView"):
            self.assertIn("h.subs." + call + "(", handlers)
        submit = (ROOT / "internal/api/handlers/company_mail.go").read_text()
        self.assertIn("h.subs.ReplayReceiptView(", submit)
        self.assertIn("created(w, h.subs.CommittedReceiptView(", submit)

    def test_sensitive_and_unknown_fields_fail_closed_on_every_route(self):
        for route, listing in ROUTES.items():
            for key in FORBIDDEN | {"unexpected_future_field"}:
                bad = minimal() | {key: "not-allowed"}
                wire = {"data": [bad], "meta": {"total": 1, "page": 1, "per_page": 30}} if listing else {"data": bad}
                self.assertFalse(accepts(response(route), wire), (route, key))
        for key in ("progress", "capabilities"):
            bad = minimal()
            bad[key]["private"] = "not-allowed"
            self.assertFalse(accepts(SCHEMAS["OutboundReceipt"], bad))

    def test_actual_enums_and_fallback_optional_metadata(self):
        model = (ROOT / "internal/models/models.go").read_text()
        states = set(re.findall(r'Outbound\w+\s+OutboundState = "([^"]+)"', model))
        self.assertEqual(set(SCHEMAS["OutboundReceipt"]["properties"]["state"]["enum"]), states | {"unknown"})
        delivery = (ROOT / "internal/delivery/projection.go").read_text()
        constants = dict(re.findall(r'(Submission\w+)\s+= "([^"]+)"', delivery))
        returned = set(re.findall(r"return (Submission\w+)", delivery))
        self.assertEqual(set(SCHEMAS["OutboundReceipt"]["properties"]["status"]["enum"]), {constants[k] for k in returned})
        self.assertEqual(set(SCHEMAS["SubmissionCapabilities"]["properties"]["retry_block_reason"]["enum"]), {"", "delivery_uncertain", "state_not_retryable", "sender_authority", "unknown"})
        self.assertTrue(accepts(SCHEMAS["OutboundReceipt"], minimal()))
        for key in ("tenant_id", "created_at", "updated_at", "attempt_count", "next_retry", "capabilities"):
            self.assertNotIn(key, SCHEMAS["OutboundReceipt"]["required"])
            self.assertFalse(accepts(SCHEMAS["OutboundReceipt"], minimal() | {key: None}))
        self.assertNotIn("content_redacted", SCHEMAS["OutboundReceipt"]["properties"])

    def test_progress_distinguishes_unknown_from_complete_ledger(self):
        counts = {"total": 3, "accepted": 1, "pending": 0, "temporary": 1, "permanent": 1, "uncertain": 0}
        schema = SCHEMAS["OutboundReceiptProgress"]
        self.assertTrue(accepts(schema, {"completeness": "known", "counts": counts}))
        self.assertTrue(accepts(schema, {"completeness": "unknown"}))
        for value in ({"completeness": "known"}, {"completeness": "unknown", "counts": counts}, {"completeness": "unknown", "counts": None}, {"completeness": "known", "counts": counts | {"total": 0}}, {"completeness": "known", "counts": counts | {"diagnostic": "blocked"}}):
            self.assertFalse(accepts(schema, value), value)

    def test_content_bcc_complete_empty_and_legacy_unknown_are_distinct(self):
        schema = SCHEMAS["SubmissionContent"]
        good = {"id": minimal()["id"], "subject": "", "from": "sender@fixture.test", "to": [], "bcc": [], "recipient_completeness": "complete", "created_at": "2026-10-02T01:00:00Z", "content_redacted": False}
        self.assertTrue(accepts(schema, good))
        self.assertTrue(accepts(schema, good | {"bcc": ["bcc@fixture.test"]}))
        self.assertTrue(accepts(schema, good | {"bcc": None, "recipient_completeness": "legacy_unknown"}))
        for bad in (good | {"bcc": None}, good | {"recipient_completeness": "legacy_unknown"}, good | {"content_redacted": True}, good | {"object_key": "blocked"}):
            self.assertFalse(accepts(schema, bad))
        for missing in ("bcc", "recipient_completeness"):
            bad = copy.deepcopy(good)
            del bad[missing]
            self.assertFalse(accepts(schema, bad))
        selector = (ROOT / "internal/store/postgres/sent_content_selector.go").read_text()
        self.assertIn('v.RecipientCompleteness == "complete"', selector)
        self.assertIn('v.BCC = append([]string{}, v.BCC...)', selector)
        self.assertIn('v.BCC = nil', selector)
        self.assertIn('submissionContentScope(current, 2)', selector)
        self.assertIn('sentContentFrom', selector)

    def test_inspection_remains_separate(self):
        inspect = SCHEMAS["CompanyOutboundInspection"]
        self.assertEqual(inspect["properties"]["job"]["$ref"], "#/components/schemas/CompanyOutboundInspectionJob")
        self.assertEqual(inspect["properties"]["recipients"]["items"]["$ref"], "#/components/schemas/CompanyOutboundInspectionRecipient")
        for name in ("OutboundReceipt", "OutboundReceiptProgress", "OutboundReceiptCounts", "SubmissionCapabilities"):
            self.assertNotIn("Inspection", repr(SCHEMAS[name]))

    def test_fresh_typed_shipping_envelope_bytes(self):
        import r5_source_runner_prepare as preparation
        self.assertTrue(all(os.getenv(key) for key in ('ORDINARY_RECEIPT_WIRE_FIXTURE',
            'R5_SOURCE_PREPARATION', 'R5_SOURCE_PREPARATION_SHA256', 'R5_SOURCE_RUN_ID')),
            'fresh same-source typed wire build/run evidence required; use the source-version runner')
        data = preparation.validate_wire(ROOT, os.environ['ORDINARY_RECEIPT_WIRE_FIXTURE'],
            os.environ['R5_SOURCE_PREPARATION'], os.environ['R5_SOURCE_PREPARATION_SHA256'], os.environ['R5_SOURCE_RUN_ID'])
        self.assertEqual(data["evidence"], "typed-projection-and-shipping-envelope-only; not HTTP admission or PG acceptance")
        seen = set()
        for item in data["fixtures"]:
            route = (item["path"], item["method"], item["status"])
            self.assertIn(route, set(ROUTES) | {CONTENT_ROUTE})
            self.assertTrue(accepts(response(route), item["body"]), (route, item["name"], item["body"]))
            seen.add((route, item["name"]))
            if route == CONTENT_ROUTE:
                continue
            receipts = item["body"]["data"] if ROUTES[route] else [item["body"]["data"]]
            for receipt in receipts:
                self.assertFalse(set(receipt) - RECEIPT_KEYS)
                if item["name"] == "known":
                    counts = receipt["progress"]["counts"]
                    self.assertEqual(counts["total"], sum(v for k, v in counts.items() if k != "total"))
                    self.assertTrue(receipt["capabilities"]["view_content"])
                if item["name"] == "committed-fallback":
                    self.assertEqual(set(receipt), set(minimal()))
                    self.assertEqual(receipt["progress"], {"completeness": "unknown"})
                    self.assertEqual(receipt["capabilities"], minimal()["capabilities"])
        expected = {(r, name) for r in ROUTES for name in ("known", "unknown")}
        expected |= {(r, "committed-fallback") for r in ROUTES if r[0] == "/api/v1/outbound/{id}/retry" or r[2] == 201}
        expected |= {(CONTENT_ROUTE, name) for name in ("known-empty-bcc", "known-bcc", "legacy-unknown-bcc")}
        self.assertEqual(seen, expected)
        self.assertEqual(len(data["fixtures"]), len(expected))


if __name__ == "__main__":
    unittest.main()
