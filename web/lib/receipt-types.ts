// Closed ordinary-operation schema. Never cast a raw queue job to this DTO.
import type { APIListResponse } from "./types";

export type ReceiptState = "pending" | "processing" | "sent" | "retry" | "failed" | "dead" | "cancelled" | "unknown";
export type ReceiptStatus = "cancelled" | "submitted" | "sending" | "partially_accepted" | "accepted" | "needs_attention";
export type RetryBlockReason = "" | "delivery_uncertain" | "state_not_retryable" | "sender_authority" | "unknown";
export interface ReceiptCapabilities {
  view_content: boolean;
  retry: boolean;
  retry_block_reason: RetryBlockReason;
}
export interface ReceiptCounts {
  total: number;
  accepted: number;
  pending: number;
  temporary: number;
  permanent: number;
  uncertain: number;
}
export type ReceiptProgress = { completeness: "unknown"; counts?: never } | { completeness: "known"; counts: ReceiptCounts };
export interface OrdinaryReceipt {
  id: string;
  tenant_id?: string;
  state: ReceiptState;
  /** Complete-ledger result; acceptance by the next hop is not final delivery. */
  status: ReceiptStatus;
  progress: ReceiptProgress;
  created_at?: string;
  updated_at?: string;
  attempt_count?: number;
  next_retry?: string;
  delivery_uncertain: boolean;
  capabilities?: ReceiptCapabilities;
}
export interface LiveSubmissionContent {
  id: string;
  subject: string;
  from: string;
  to: string[];
  cc?: string[];
  bcc: string[] | null;
  recipient_completeness: "complete" | "legacy_unknown";
  headers?: Record<string, string>;
  text_body?: string;
  html_body?: string;
  created_at: string;
  /** Successful live content is readable; unreadable content returns an error. */
  content_redacted: false;
}
export interface ReceiptScope { id?: string; tenantId?: string | null }
const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function invalid(): never {
  // Server values (including private fields and diagnostics) must not reach UI errors.
  throw new Error("Invalid or out-of-scope receipt response");
}
function record(value: unknown, required: readonly string[], optional: readonly string[] = []): Record<string, unknown> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return invalid();
  const source = value as Record<string, unknown>;
  if (required.some(key => !Object.hasOwn(source, key)) ||
    Object.keys(source).some(key => !required.includes(key) && !optional.includes(key))) return invalid();
  return source;
}
function string(value: unknown): string { return typeof value === "string" ? value : invalid(); }
function id(value: unknown): string {
  const result = string(value);
  return uuid.test(result) && result !== "00000000-0000-0000-0000-000000000000" ? result : invalid();
}
function boolean(value: unknown): boolean { return typeof value === "boolean" ? value : invalid(); }
function integer(value: unknown): number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0 ? value : invalid();
}
function date(value: unknown): string {
  const result = string(value);
  return /^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})$/.test(result) && Number.isFinite(Date.parse(result)) ? result : invalid();
}
function category<T extends string>(value: unknown, allowed: readonly T[]): T {
  return typeof value === "string" && allowed.includes(value as T) ? value as T : invalid();
}
function strings(value: unknown): string[] {
  if (!Array.isArray(value) || value.some(v => typeof v !== "string")) return invalid();
  return [...value];
}
export function parseOrdinaryReceipt(value: unknown, scope: ReceiptScope = {}): OrdinaryReceipt {
  const source = record(value, ["id", "state", "status", "progress", "delivery_uncertain"],
    ["tenant_id", "created_at", "updated_at", "attempt_count", "next_retry", "capabilities"]);
  const receiptId = id(source.id);
  if (scope.id !== undefined && receiptId !== scope.id) return invalid();
  const tenant = Object.hasOwn(source, "tenant_id") ? id(source.tenant_id) : undefined;
  if (tenant !== undefined && scope.tenantId !== undefined && tenant !== scope.tenantId) return invalid();
  const state = category(source.state, ["pending", "processing", "sent", "retry", "failed", "dead", "cancelled", "unknown"] as const);
  const status = category(source.status, ["cancelled", "submitted", "sending", "partially_accepted", "accepted", "needs_attention"] as const);
  const progressSource = record(source.progress, ["completeness"], ["counts"]);
  let progress: ReceiptProgress;
  if (progressSource.completeness === "unknown") {
    if (Object.hasOwn(progressSource, "counts") || status !== "needs_attention") return invalid();
    progress = { completeness: "unknown" };
  } else if (progressSource.completeness === "known") {
    const raw = record(progressSource.counts, ["total", "accepted", "pending", "temporary", "permanent", "uncertain"]);
    const counts: ReceiptCounts = { total: integer(raw.total), accepted: integer(raw.accepted), pending: integer(raw.pending),
      temporary: integer(raw.temporary), permanent: integer(raw.permanent), uncertain: integer(raw.uncertain) };
    const sum = counts.accepted + counts.pending + counts.temporary + counts.permanent + counts.uncertain;
    if (!Number.isSafeInteger(sum) || sum !== counts.total || counts.total === 0 || state === "unknown") return invalid();
    progress = { completeness: "known", counts };
  } else return invalid();
  const delivery_uncertain = boolean(source.delivery_uncertain);
  if (progress.completeness === "known" && progress.counts.uncertain > 0 && !delivery_uncertain) return invalid();
  let capabilities: ReceiptCapabilities | undefined;
  if (Object.hasOwn(source, "capabilities")) {
    const raw = record(source.capabilities, ["view_content", "retry", "retry_block_reason"]);
    capabilities = { view_content: boolean(raw.view_content), retry: boolean(raw.retry),
      retry_block_reason: category(raw.retry_block_reason, ["", "delivery_uncertain", "state_not_retryable", "sender_authority", "unknown"] as const) };
    if (capabilities.retry && (capabilities.retry_block_reason !== "" || delivery_uncertain || progress.completeness !== "known")) return invalid();
  }
  const result: OrdinaryReceipt = { id: receiptId, state, status, progress, delivery_uncertain };
  if (tenant !== undefined) result.tenant_id = tenant;
  for (const key of ["created_at", "updated_at", "next_retry"] as const) if (Object.hasOwn(source, key)) result[key] = date(source[key]);
  if (Object.hasOwn(source, "attempt_count")) result.attempt_count = integer(source.attempt_count);
  if (capabilities !== undefined) result.capabilities = capabilities;
  // A committed display fallback carries no current scope or usable authority.
  if (tenant === undefined && (progress.completeness !== "unknown" || capabilities?.view_content || capabilities?.retry)) return invalid();
  return result;
}
export function parseReceiptResponse(value: unknown, scope: ReceiptScope = {}): OrdinaryReceipt {
  return parseOrdinaryReceipt(record(value, ["data"]).data, scope);
}
export function parseReceiptListResponse(value: unknown, scope: ReceiptScope = {}): APIListResponse<OrdinaryReceipt> {
  const source = record(value, ["data", "meta"]);
  const meta = record(source.meta, ["total", "page", "per_page"]);
  if (!Array.isArray(source.data)) return invalid();
  const result = { data: source.data.map(row => parseOrdinaryReceipt(row, scope)),
    meta: { total: integer(meta.total), page: integer(meta.page), per_page: integer(meta.per_page) } };
  if (result.meta.page < 1 || result.meta.per_page < 1 || result.data.length > result.meta.per_page || result.data.length > result.meta.total) return invalid();
  return result;
}
// Same display-header rules as the backend. Structured BCC is NEVER read from
// this map; consumers deliberately do not render custom headers.
const blockedHeaders = new Set(["from", "to", "cc", "bcc", "subject", "date", "message-id", "mime-version", "content-type",
  "content-transfer-encoding", "return-path", "sender", "received", "dkim-signature", "domainkey-signature", "arc-seal",
  "arc-message-signature", "arc-authentication-results", "x-mailer", "x-originating-ip", "x-originating-email",
  "x-google-dkim-signature", "authentication-results", "received-spf"]);
function headers(value: unknown): Record<string, string> {
  if (!value || typeof value !== "object" || Array.isArray(value)) return invalid();
  const result: Record<string, string> = {};
  for (const [key, headerValue] of Object.entries(value)) {
    if (key.length > 126 || !/^[A-Za-z0-9][A-Za-z0-9-]*$/.test(key) || blockedHeaders.has(key.toLowerCase())) return invalid();
    Object.defineProperty(result, key, { value: string(headerValue), enumerable: true });
  }
  return result;
}
export function parseSubmissionContent(value: unknown, expectedId: string): LiveSubmissionContent {
  const raw = record(value, ["id", "subject", "from", "to", "bcc", "recipient_completeness", "created_at", "content_redacted"],
    ["cc", "headers", "text_body", "html_body"]);
  if (id(raw.id) !== expectedId) return invalid();
  if (raw.content_redacted !== false) return invalid();
  const completeness = category(raw.recipient_completeness, ["complete", "legacy_unknown"] as const);
  if (completeness === "legacy_unknown" && raw.bcc !== null || completeness === "complete" && !Array.isArray(raw.bcc)) return invalid();
  const result: LiveSubmissionContent = { id: expectedId, subject: string(raw.subject), from: string(raw.from), to: strings(raw.to),
    bcc: completeness === "complete" ? strings(raw.bcc) : null, recipient_completeness: completeness,
    created_at: date(raw.created_at), content_redacted: false };
  if (Object.hasOwn(raw, "cc")) result.cc = strings(raw.cc);
  if (Object.hasOwn(raw, "headers")) result.headers = headers(raw.headers);
  for (const key of ["text_body", "html_body"] as const) if (Object.hasOwn(raw, key)) result[key] = string(raw[key]);
  return result;
}
export function parseContentResponse(value: unknown, expectedId: string): LiveSubmissionContent {
  return parseSubmissionContent(record(value, ["data"]).data, expectedId);
}
