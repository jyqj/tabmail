// Dedicated audited-exception DTO. Never extend ordinary queue or receipt
// types to obtain content/BCC visibility. Server authorization and the atomic
// audit transaction remain authoritative for every inspection.
export const outboundInspectionHeaderKeys = [
  "From", "To", "Cc", "Subject", "Date", "Message-Id", "Reply-To",
  "In-Reply-To", "References", "Mime-Version", "Content-Type",
  "Content-Transfer-Encoding", "Content-Disposition", "Content-Id",
] as const;
export type OutboundInspectionHeader = typeof outboundInspectionHeaderKeys[number];
export type OutboundInspectionHeaders = Partial<Record<OutboundInspectionHeader, string>>;
export type OutboundInspectionState = "pending" | "processing" | "sent" | "retry" | "failed" | "dead" | "cancelled" | "unknown";
export type OutboundInspectionStatus = "submitted" | "cancelled" | "sending" | "partially_accepted" | "accepted" | "needs_attention" | "unknown";
export type OutboundInspectionRecipientState = "pending" | "accepted" | "temporary" | "permanent" | "uncertain" | "unknown";
export type OutboundInspectionRecipientKind = "to" | "cc" | "bcc" | "envelope" | "unknown";
export interface OutboundInspectionJob {
  id: string;
  tenant_id: string;
  state: OutboundInspectionState;
  /** Next-hop acceptance is not final delivery. */
  status: OutboundInspectionStatus;
  created_at: string;
  updated_at: string;
  mail_from: string;
  to: string[];
  cc: string[];
  bcc: string[];
  subject: string;
  text_body: string;
  html_body: string;
  headers: OutboundInspectionHeaders;
}
export interface OutboundInspectionRecipient {
  address: string;
  kind: OutboundInspectionRecipientKind;
  state: OutboundInspectionRecipientState;
  attempts: number;
  smtp_code: number;
  enhanced_code?: string;
  diagnostic_class: OutboundInspectionRecipientState;
  updated_at: string;
}
export interface OutboundInspection {
  job: OutboundInspectionJob;
  recipients: OutboundInspectionRecipient[];
}

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
export function isInspectionID(value: unknown): value is string {
  return typeof value === "string" && uuid.test(value) && value !== "00000000-0000-0000-0000-000000000000";
}
export function validInspectionReason(reason: string): boolean {
  const size = new TextEncoder().encode(reason.trim()).length;
  return size >= 8 && size <= 1000;
}
const jobKeys = ["id", "tenant_id", "state", "status", "created_at", "updated_at", "mail_from", "to", "cc", "bcc", "subject", "text_body", "html_body", "headers"];
const recipientKeys = ["address", "kind", "state", "attempts", "smtp_code", "diagnostic_class", "updated_at"];
const recipientStates = ["pending", "accepted", "temporary", "permanent", "uncertain", "unknown"] as const;
function invalid(): never {
  // Never interpolate unknown server values into a toast or error message.
  throw new Error("Invalid or out-of-scope inspection response");
}
function record(value: unknown, required: string[], optional: readonly string[] = []): Record<string, unknown> {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return invalid();
  const result = value as Record<string, unknown>;
  if (required.some(key => !Object.hasOwn(result, key)) ||
      Object.keys(result).some(key => !required.includes(key) && !optional.includes(key))) return invalid();
  return result;
}
function string(value: unknown): string {
  return typeof value === "string" ? value : invalid();
}
function dateTime(value: unknown): string {
  const result = string(value);
  return /^\d{4}-\d{2}-\d{2}T/.test(result) && Number.isFinite(Date.parse(result)) ? result : invalid();
}
function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.some(item => typeof item !== "string")) return invalid();
  return [...value];
}
function category<T extends string>(value: unknown, known: readonly T[]): T | "unknown" {
  const result = string(value);
  return known.includes(result as T) ? result as T : "unknown";
}
function headers(value: unknown): OutboundInspectionHeaders {
  const source = record(value, [], outboundInspectionHeaderKeys);
  const result: OutboundInspectionHeaders = {};
  for (const key of outboundInspectionHeaderKeys) {
    if (!Object.hasOwn(source, key)) continue;
    const value = string(source[key]);
    if (/[\r\n]/.test(value)) return invalid();
    result[key] = value;
  }
  return result;
}

/** Validate scope, mandatory keys and the closed disclosure schema before
 * placing anything in React state. Unknown string categories become a fixed
 * non-authorizing label; unknown object keys or malformed values fail closed.
 */
export function parseOutboundInspection(value: unknown, expected: { jobId: string; tenantId: string }): OutboundInspection {
  if (!isInspectionID(expected.jobId) || !isInspectionID(expected.tenantId)) return invalid();
  const source = record(value, ["job", "recipients"]);
  const job = record(source.job, jobKeys);
  if (!isInspectionID(job.id) || !isInspectionID(job.tenant_id) ||
      job.id !== expected.jobId || job.tenant_id !== expected.tenantId) return invalid();
  if (!Array.isArray(source.recipients) || source.recipients.length > 50) return invalid();
  const recipients = source.recipients.map(value => {
    const r = record(value, recipientKeys, ["enhanced_code"]);
    if (typeof r.attempts !== "number" || !Number.isSafeInteger(r.attempts) || r.attempts < 0 ||
        typeof r.smtp_code !== "number" || !Number.isInteger(r.smtp_code) ||
        (r.smtp_code !== 0 && (r.smtp_code < 200 || r.smtp_code > 599))) return invalid();
    let enhanced_code: string | undefined;
    if (Object.hasOwn(r, "enhanced_code")) {
      enhanced_code = string(r.enhanced_code);
      if (!/^[245]\.[0-9]{1,3}\.[0-9]{1,3}$/.test(enhanced_code) || /[\r\n]/.test(enhanced_code)) return invalid();
    }
    return { address: string(r.address), kind: category(r.kind, ["to", "cc", "bcc", "envelope"] as const),
      state: category(r.state, recipientStates), attempts: r.attempts, smtp_code: r.smtp_code,
      ...(enhanced_code === undefined ? {} : { enhanced_code }),
      diagnostic_class: category(r.diagnostic_class, recipientStates), updated_at: dateTime(r.updated_at) };
  });
  if (new Set(recipients.map(r => r.address)).size !== recipients.length) return invalid();
  return { job: { id: job.id, tenant_id: job.tenant_id,
    state: category(job.state, ["pending", "processing", "sent", "retry", "failed", "dead", "cancelled"] as const),
    status: category(job.status, ["submitted", "cancelled", "sending", "partially_accepted", "accepted", "needs_attention"] as const),
    created_at: dateTime(job.created_at), updated_at: dateTime(job.updated_at), mail_from: string(job.mail_from),
    to: strings(job.to), cc: strings(job.cc), bcc: strings(job.bcc), subject: string(job.subject),
    text_body: string(job.text_body), html_body: string(job.html_body), headers: headers(job.headers) }, recipients };
}
