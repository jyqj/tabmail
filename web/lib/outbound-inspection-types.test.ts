import { describe, expect, it } from "vitest";
import {
  isInspectionID, outboundInspectionHeaderKeys, parseOutboundInspection,
  validInspectionReason, type OutboundInspectionJob, type OutboundInspectionRecipient,
} from "./outbound-inspection-types";

const jobId = "30000000-0000-4000-8000-000000000001";
const tenantId = "10000000-0000-4000-8000-000000000001";
const expected = { jobId, tenantId };
const date = "2026-10-01T10:00:00Z";
const canary = "INTERNAL-SECRET-CANARY-NOT-CONTENT";
const fixture = () => ({ job: { id: jobId, tenant_id: tenantId,
  state: "sent", status: "accepted", created_at: date, updated_at: date,
  mail_from: "sender@fixture.test", to: ["visible@fixture.test"], cc: [], bcc: [],
  subject: "inspection-subject", text_body: "inspection-body", html_body: "<p>inspection-body</p>",
  headers: {} as Record<string, string> }, recipients: [{ address: "visible@fixture.test",
    kind: "to", state: "accepted", attempts: 1, smtp_code: 250,
    diagnostic_class: "accepted", updated_at: date, enhanced_code: "2.0.0" }] });

// Compile-time contracts: these exception types deliberately have no old raw
// diagnostic/queue-private compatibility properties or arbitrary header keys.
type AssertNever<T extends never> = T;
type NoPrivateJob = AssertNever<Extract<keyof OutboundInspectionJob,
  "last_error" | "smtp_response" | "delivery_token" | "lease_until" | "raw_storage_key" | "source_object_key">>;
type NoRawDiagnostic = AssertNever<Extract<keyof OutboundInspectionRecipient, "diagnostic">>;
const typedBoundary: [NoPrivateJob?, NoRawDiagnostic?] = [];
void typedBoundary;

describe("closed audited inspection disclosure parser", () => {
  it("consumes the explicit wire shape without retaining the source object", () => {
    const source = fixture();
    const parsed = parseOutboundInspection(source, expected);
    expect(parsed).toEqual(source);
    expect(parsed).not.toBe(source);
    expect(parsed.job).not.toBe(source.job);
    source.job.subject = canary;
    source.job.to.push(canary);
    expect(JSON.stringify(parsed)).not.toContain(canary);
  });

  it.each(["state", "status", "kind", "diagnostic_class", "recipient_state"])("maps unknown %s to a fixed non-secret category", field => {
    const source = fixture();
    if (field === "state" || field === "status") source.job[field] = canary;
    else if (field === "recipient_state") source.recipients[0].state = canary;
    else source.recipients[0][field as "kind" | "diagnostic_class"] = canary;
    const parsed = parseOutboundInspection(source, expected);
    expect(JSON.stringify(parsed)).not.toContain(canary);
    expect(JSON.stringify(parsed)).toContain("unknown");
  });

  it.each(Object.keys(fixture().job))("fails closed when mandatory job.%s is absent", key => {
    const source = fixture();
    Reflect.deleteProperty(source.job, key);
    expect(() => parseOutboundInspection(source, expected)).toThrow("Invalid or out-of-scope inspection response");
  });
  it.each(["address", "kind", "state", "attempts", "smtp_code", "diagnostic_class", "updated_at"])("fails closed when mandatory recipient.%s is absent", key => {
    const source = fixture();
    Reflect.deleteProperty(source.recipients[0], key);
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it("accepts omission of the optional enhanced status code", () => {
    const source = fixture();
    Reflect.deleteProperty(source.recipients[0], "enhanced_code");
    expect(parseOutboundInspection(source, expected).recipients[0]).not.toHaveProperty("enhanced_code");
  });

  it.each(["last_error", "smtp_response", "delivery_token", "claimed_at", "lease_until", "raw_mime", "raw_storage_key", "source_object_key", "smtp_password", "dkim_key", "api_key", "jwt", "future_field"])("rejects unknown/private job.%s instead of a raw fallback", key => {
    const source = fixture();
    Object.assign(source.job, { [key]: canary });
    expect(() => parseOutboundInspection(source, expected)).toThrow("Invalid or out-of-scope inspection response");
  });
  it.each(["diagnostic", "smtp_response", "future_field"])("rejects recipient.%s without exposing the unknown value", key => {
    const source = fixture();
    Object.assign(source.recipients[0], { [key]: canary });
    try { parseOutboundInspection(source, expected); throw new Error("Expected rejection"); }
    catch (error) { expect(String(error)).not.toContain(canary); expect(String(error)).toContain("Invalid or out-of-scope"); }
  });
  it("rejects top-level extension fields", () => {
    expect(() => parseOutboundInspection({ ...fixture(), debug: canary }, expected)).toThrow();
  });

  it("supports exactly the 14 canonical structural header keys", () => {
    expect(outboundInspectionHeaderKeys).toHaveLength(14);
    const source = fixture();
    source.job.headers = Object.fromEntries(outboundInspectionHeaderKeys.map(key => [key, "safe-header-value"]));
    expect(parseOutboundInspection(source, expected).job.headers).toEqual(source.job.headers);
  });
  it.each(["Bcc", "Received", "Authentication-Results", "Authorization", "DKIM-Signature", "X-Api-Key", "X-Secret", "Content-Secret", "Content-Length", "Content-Location", "subject", "__proto__"])("rejects non-allowlisted header %s; no Content-* wildcard", key => {
    const source = fixture();
    Object.defineProperty(source.job.headers, key, { value: canary, enumerable: true });
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it.each(["ok\rsecret", "ok\nsecret", 9, null])("rejects malformed or injected allowed-header value %s", value => {
    const source = fixture();
    Object.assign(source.job.headers, { Subject: value });
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it.each(["5.1.1 raw diagnostic", "5.1.1\n", "3.1.1", "5.1234.1", "", null])("rejects invalid enhanced code %s", value => {
    const source = fixture();
    Object.assign(source.recipients[0], { enhanced_code: value });
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it.each([0, 200, 599])("accepts sanitized numeric SMTP code %s", code => {
    const source = fixture(); source.recipients[0].smtp_code = code;
    expect(parseOutboundInspection(source, expected).recipients[0].smtp_code).toBe(code);
  });
  it.each([199, 600, -1, 250.5, "250", null])("rejects invalid SMTP code %s", code => {
    const source = fixture(); Object.assign(source.recipients[0], { smtp_code: code });
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it.each(["job", "tenant"])("binds the %s to the requested target and selected company", field => {
    const source = fixture();
    source.job[field === "job" ? "id" : "tenant_id"] = "90000000-0000-4000-8000-000000000009";
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it("rejects oversized or duplicate recipient ledgers rather than truncating", () => {
    const source = fixture();
    source.recipients.push({ ...source.recipients[0] });
    expect(() => parseOutboundInspection(source, expected)).toThrow();
    source.recipients = Array.from({ length: 51 }, (_, i) => ({ ...source.recipients[0], address: `${i}@fixture.test` }));
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it.each([null, {}, { job: null, recipients: [] }, { job: {}, recipients: null }])("rejects malformed payload %s", source => {
    expect(() => parseOutboundInspection(source, expected)).toThrow();
  });
  it("validates IDs and the trimmed 8–1000 UTF-8-byte reason boundary", () => {
    expect(isInspectionID(jobId)).toBe(true);
    expect(isInspectionID("00000000-0000-0000-0000-000000000000")).toBe(false);
    expect(isInspectionID("not-an-id")).toBe(false);
    expect(validInspectionReason("        ")).toBe(false);
    expect(validInspectionReason("short")).toBe(false);
    expect(validInspectionReason(" xxxxxxxx ")).toBe(true);
    expect(validInspectionReason("x".repeat(1000))).toBe(true);
    expect(validInspectionReason("x".repeat(1001))).toBe(false);
    expect(validInspectionReason("核查原因".repeat(84))).toBe(false);
  });
});
