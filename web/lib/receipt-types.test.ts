import { describe, expect, it } from "vitest";
import { parseOrdinaryReceipt, parseReceiptListResponse, parseSubmissionContent } from "./receipt-types";

const id = "30000000-0000-4000-8000-000000000001";
const tenant = "10000000-0000-4000-8000-000000000001";
const date = "2026-10-02T01:02:03.123456789Z";
const known = () => ({ id, tenant_id: tenant, state: "sent", status: "partially_accepted",
  progress: { completeness: "known", counts: { total: 2, accepted: 1, pending: 0, temporary: 0, permanent: 1, uncertain: 0 } },
  created_at: date, updated_at: date, attempt_count: 1, delivery_uncertain: false,
  capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" } });
const content = () => ({ id, subject: "live subject", from: "original@fixture.test", to: ["to@fixture.test"],
  cc: [], bcc: ["bcc@fixture.test"], recipient_completeness: "complete", created_at: date, text_body: "live body", content_redacted: false });

describe("closed ordinary receipt runtime DTO", () => {
  it("preserves full-ledger aggregate outcomes and scopes exact IDs/tenant", () => {
    expect(parseOrdinaryReceipt(known(), { id, tenantId: tenant })).toEqual(known());
    expect(() => parseOrdinaryReceipt(known(), { id: tenant })).toThrow();
    expect(() => parseOrdinaryReceipt(known(), { tenantId: id })).toThrow();
  });
  it("rejects stale waiting status and accepts only actual producer status categories", () => {
    expect(() => parseOrdinaryReceipt({ ...known(), status: "waiting" }))
      .toThrow("Invalid or out-of-scope receipt response");
    const pending = { total: 2, accepted: 0, pending: 2, temporary: 0, permanent: 0, uncertain: 0 };
    const cases = [
      { state: "pending", status: "submitted", counts: pending },
      { state: "processing", status: "sending", counts: pending },
      { state: "sent", status: "partially_accepted", counts: known().progress.counts },
      { state: "sent", status: "accepted", counts: { ...pending, accepted: 2, pending: 0 } },
      { state: "failed", status: "needs_attention", counts: { ...pending, permanent: 2, pending: 0 } },
      { state: "cancelled", status: "cancelled", counts: pending },
    ];
    for (const entry of cases) {
      expect(parseOrdinaryReceipt({ ...known(), state: entry.state, status: entry.status,
        progress: { completeness: "known", counts: entry.counts } }).status).toBe(entry.status);
    }
  });
  it("accepts the committed unknown fallback without counts or current authority", () => {
    expect(parseOrdinaryReceipt({ id, state: "pending", status: "needs_attention", progress: { completeness: "unknown" }, delivery_uncertain: false,
      capabilities: { view_content: false, retry: false, retry_block_reason: "unknown" } }).progress).toEqual({ completeness: "unknown" });
  });
  it.each(["subject", "mail_from", "attachment_count", "recipients", "bcc", "text_body", "raw_headers", "smtp_code", "smtp_response", "last_error", "diagnostic", "lease", "delivery_token", "object_key", "user_id", "api_key_id"])("rejects private/raw %s instead of stripping it", field => {
    expect(() => parseOrdinaryReceipt({ ...known(), [field]: "SECRET-CANARY" })).toThrow("Invalid or out-of-scope receipt response");
  });
  it("rejects nested private extensions and arbitrary categories/reasons", () => {
    const source = known();
    for (const bad of [{ ...source, capabilities: { ...source.capabilities, sender_id: id } },
      { ...source, capabilities: { ...source.capabilities, retry_block_reason: "RAW-SMTP-DIAGNOSTIC" } },
      { ...source, state: "RAW-INTERNAL-STATE" }, { ...source, status: "delivered" },
      { ...source, progress: { ...source.progress, recipients: [] } },
      { ...source, progress: { ...source.progress, counts: { ...source.progress.counts, bcc: 1 } } }]) {
      expect(() => parseOrdinaryReceipt(bad)).toThrow();
    }
  });
  it.each([-1, 0.5, Number.MAX_SAFE_INTEGER + 1, Infinity, NaN, "2"])("rejects unsafe/invalid count %s", total => {
    const source = known();
    expect(() => parseOrdinaryReceipt({ ...source, progress: { completeness: "known", counts: { ...source.progress.counts, total } } })).toThrow();
  });
  it("requires a finite complete-ledger sum and never turns unknown into accepted/zero", () => {
    const source = known();
    expect(() => parseOrdinaryReceipt({ ...source, progress: { completeness: "known", counts: { ...source.progress.counts, total: 3 } } })).toThrow();
    expect(() => parseOrdinaryReceipt({ ...source, progress: { completeness: "unknown", counts: source.progress.counts }, status: "needs_attention" })).toThrow();
    expect(() => parseOrdinaryReceipt({ ...source, progress: { completeness: "unknown" }, status: "accepted" })).toThrow();
    expect(() => parseOrdinaryReceipt({ ...source, capabilities: { view_content: true, retry: true, retry_block_reason: "unknown" } })).toThrow();
    const fallback = { ...source, tenant_id: undefined };
    Reflect.deleteProperty(fallback, "tenant_id");
    expect(() => parseOrdinaryReceipt(fallback)).toThrow();
  });
  it("checks list envelope, metadata and every row without accepting old array detail DTOs", () => {
    expect(parseReceiptListResponse({ data: [known()], meta: { page: 1, per_page: 20, total: 1 } }).data).toHaveLength(1);
    for (const value of [{ data: [known()] }, { data: [known()], meta: { page: 1, per_page: 20, total: 0 } },
      { data: [known()], meta: { page: 1, per_page: 20, total: 1 }, raw_job: {} }]) expect(() => parseReceiptListResponse(value)).toThrow();
  });
});

describe("current-readable sent-content structured BCC", () => {
  it("requires literal false for successful live sent-content responses", () => {
    expect(parseSubmissionContent(content(), id).content_redacted).toBe(false);
    expect(parseSubmissionContent({ ...content(), bcc: null, recipient_completeness: "legacy_unknown" }, id).content_redacted).toBe(false);
    for (const value of [true, null, undefined, "false", 0, {}]) {
      expect(() => parseSubmissionContent({ ...content(), content_redacted: value }, id))
        .toThrow("Invalid or out-of-scope receipt response");
    }
    const missing = content(); Reflect.deleteProperty(missing, "content_redacted");
    expect(() => parseSubmissionContent(missing, id)).toThrow("Invalid or out-of-scope receipt response");
  });
  it("distinguishes known populated, known empty and historical unknown", () => {
    expect(parseSubmissionContent(content(), id).bcc).toEqual(["bcc@fixture.test"]);
    expect(parseSubmissionContent({ ...content(), bcc: [] }, id).bcc).toEqual([]);
    expect(parseSubmissionContent({ ...content(), bcc: null, recipient_completeness: "legacy_unknown" }, id).bcc).toBeNull();
  });
  it.each([
    { bcc: null, recipient_completeness: "complete" },
    { bcc: [], recipient_completeness: "legacy_unknown" },
    { bcc: ["bcc@fixture.test"], recipient_completeness: "legacy_unknown" },
    { bcc: "bcc@fixture.test", recipient_completeness: "complete" },
    { bcc: [null], recipient_completeness: "complete" },
    { bcc: [], recipient_completeness: "unknown" },
  ])("fails closed on incompatible BCC marker %j", override => {
    expect(() => parseSubmissionContent({ ...content(), ...override }, id)).toThrow();
  });
  it("accepts safe custom header entries without shadowing the source object", () => {
    const safeHeaders = { "X-Custom": "not rendered", "Reply-To": "reply@fixture.test" };
    const parsed = parseSubmissionContent({ ...content(), headers: safeHeaders }, id);
    expect(parsed.headers).toEqual(safeHeaders);
    expect(parsed.bcc).toEqual(["bcc@fixture.test"]);
    expect(parseSubmissionContent({ ...content(), headers: {} }, id).headers).toEqual({});
    expect(() => parseSubmissionContent({ ...content(), headers: { ...safeHeaders, Bcc: "header-bcc@fixture.test" } }, id))
      .toThrow("Invalid or out-of-scope receipt response");
    expect(() => parseSubmissionContent({ ...content(), headers: { "X-Custom": 1 } }, id))
      .toThrow("Invalid or out-of-scope receipt response");
  });
  it("rejects absent markers, cross-ID content, queue fallbacks and custom BCC headers", () => {
    const missing = content(); Reflect.deleteProperty(missing, "bcc");
    for (const value of [missing, { ...content(), id: tenant }, { ...content(), rcpt_to: ["private@fixture.test"] },
      { ...content(), headers: { Bcc: "header-bcc@fixture.test" } }, { ...content(), headers: { bCc: "header-bcc@fixture.test" } }]) {
      expect(() => parseSubmissionContent(value, id)).toThrow();
    }
    expect(parseSubmissionContent({ ...content(), headers: { "X-Custom": "not rendered" } }, id).headers).toEqual({ "X-Custom": "not rendered" });
  });
});
