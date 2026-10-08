import type {
  APIResponse,
  APIListResponse,
  Mailbox,
  Message,
  MessageDetail,
  AdminUser,
} from "./types";
import { request, type RequestOptions } from "./api/base";
import { parseReceiptListResponse, parseReceiptResponse, parseContentResponse,
  type OrdinaryReceipt, type ReceiptCapabilities, type ReceiptStatus, type LiveSubmissionContent } from "./receipt-types";
import { assertSession, sessionScope } from "./session";

// Outbound send policy vocabulary shared by the company default and the
// per-mailbox override. Kept in sync with authz.MailSendPolicy on the server.
export type MailSendPolicy = "free" | "template_required" | "disabled";

export interface CompanySettings {
  tenant_id: string;
  name: string;
  primary_zone_id: string;
  domain: string;
  revision: number;
  /** Company-wide default send policy. Omitted on input = leave unchanged. */
  mail_send_policy?: MailSendPolicy;
}
export type CompanySettingsInput = Pick<CompanySettings,
  "name" | "primary_zone_id" | "revision" | "mail_send_policy">;
export interface WorkMailbox {
  mailbox: Mailbox;
  can_read: boolean;
  can_organize: boolean;
  can_send: boolean;
  template_only: boolean;
  revision: number;
}
export interface WorkGrant {
  tenant_id: string;
  mailbox_id: string;
  granted_by?: string;
  created_at: string;
  updated_at: string;
  user_id: string;
  can_read: boolean;
  can_organize: boolean;
  can_send: boolean;
  template_only: boolean;
}
export type WorkGrantInput = Pick<WorkGrant,
  "user_id" | "can_read" | "can_organize" | "can_send" | "template_only">;
export interface MailboxGrantSnapshot {
  revision: number;
  grants: WorkGrant[];
}
export interface Invitation {
  id: string;
  email: string;
  mailbox_address: string;
  display_name: string;
  expires_at: string;
  consumed_at?: string;
  revoked_at?: string;
  created_at: string;
}
export interface TemplateVariable {
  name: string;
  type: "text" | "email" | "integer" | "date" | "url";
  required: boolean;
  max_length: number;
  options?: string[];
}
export interface TemplateDraft {
  subject: string;
  text_body: string;
  html_body: string;
  variables: TemplateVariable[];
}
export interface MailTemplate {
  id: string;
  name: string;
  draft: TemplateDraft;
  revision: number;
  retired: boolean;
  updated_at: string;
}
export type MailTemplateInput = Pick<MailTemplate, "name" | "draft" | "revision">;
export type MailTemplateEditor = MailTemplate | (MailTemplateInput & { id?: never; retired?: never });
export interface TemplateVersion {
  id: string;
  template_id: string;
  name: string;
  version: number;
  snapshot: TemplateDraft;
  published_at: string;
  content_hash: string;
  revoked_at?: string;
}
export interface RenderedTemplate {
  subject: string;
  text_body: string;
  html_body: string;
}
export interface DraftPayload {
  to: string[];
  cc?: string[];
  bcc?: string[];
  subject: string;
  text_body: string;
  html_body?: string;
  headers?: Record<string, string>;
  template_version_id?: string;
  template_vars?: Record<string, string>;
  attachment_ids?: string[];
}
// Server-resolved eligibility of a draft's pinned template version. The
// status is the single source of truth handed down by the backend — the
// client never re-derives the rule. It is an interaction label only: every
// send is re-authorized server-side (TemplateForSend). Snapshot is embedded
// only in the "usable" state.
export type DraftTemplateVersionStatus =
  | "missing"
  | "revoked"
  | "corrupt"
  | "retired"
  | "unauthorized"
  | "usable";
export interface DraftTemplateVersion {
  id: string;
  template_id?: string;
  name?: string;
  version?: number;
  status: DraftTemplateVersionStatus;
  snapshot?: TemplateDraft;
}
export interface MailDraft {
  id: string;
  mailbox_id: string;
  payload: DraftPayload;
  template_version?: DraftTemplateVersion;
  revision: number;
  updated_at: string;
}
// Creation may supply a client-owned id for exact replay. Response metadata
// can be echoed, but neither an id nor a server timestamp is invented by a form.
export type MailDraftInput = Omit<MailDraft, "id" | "updated_at"> &
  Partial<Pick<MailDraft, "id" | "updated_at">>;
export type NewMailDraft = Pick<MailDraft, "mailbox_id" | "payload" | "template_version"> & {
  id?: string;
  revision: 0;
};
export type MailDraftEditor = MailDraft | NewMailDraft;
export interface MailAttachment {
  id: string;
  mailbox_id: string;
  filename: string;
  size: number;
  content_type: string;
  state: string;
}
export interface DraftSubmission {
  id: string;
  message_id: string;
  state: string;
  created_at: string;
}
export interface InboundAttachment {
  id: string;
  index: number;
  filename: string;
  size: number;
  content_type: string;
}
export interface RecipientResult {
  address: string;
  state: "pending" | "accepted" | "temporary" | "permanent" | "uncertain";
  smtp_code: number;
  diagnostic?: string;
  attempts: number;
  updated_at: string;
}
export interface RecoveryTarget {
  mailbox_id: string;
  address: string;
  state: string;
  error?: string;
}
// Ordinary receipts never contain addresses, subjects, headers or body data.
export type SubmissionStatus = ReceiptStatus;
export interface SubmissionRecipient {
  address: string;
  state: string;
}
export type SubmissionCapabilities = ReceiptCapabilities;
export type Submission = OrdinaryReceipt;
// Only a current-readable live content GET may expose structural recipients.
export type SubmissionContent = LiveSubmissionContent;
// Metadata of an attachment pinned to a sent submission. Storage keys stay
// server-side; downloads go through the per-submission download endpoint.
export interface SubmissionAttachment {
  id: string;
  filename: string;
  content_type: string;
  size: number;
  state: string;
}
export interface Receipt {
  id: string;
  state: string;
  error?: string;
  updated_at: string;
  raw_size: number;
  targets: RecoveryTarget[];
}
// Company domain onboarding wizard. Only what the administrator needs: the
// verification summary and the DNS records to publish — no platform fields.
export interface DomainDNSCheck {
  status: string;
  details?: string[];
}
export interface DomainVerificationChecks {
  txt: DomainDNSCheck;
  mx: DomainDNSCheck;
  spf: DomainDNSCheck;
  dkim: DomainDNSCheck;
  dmarc: DomainDNSCheck;
}
export interface CompanyDomain {
  id: string;
  domain: string;
  is_verified: boolean;
  mx_verified: boolean;
  dkim_enabled: boolean;
  txt_record: string;
  expected_mx: string;
  dkim_host?: string;
  dkim_record?: string;
  created_at: string;
}
export interface DomainVerification {
  id: string;
  domain: string;
  is_verified: boolean;
  mx_verified: boolean;
  dkim_enabled: boolean;
  txt_record: string;
  expected_mx: string;
  dkim_host?: string;
  dkim_record?: string;
  checks: DomainVerificationChecks;
}
export const workPath = (id: string) => `/mailboxes/${encodeURIComponent(id)}`;
export const companyDomains = () =>
  company<CompanyDomain[]>("/domains").then((v) => v ?? []);
export function addCompanyDomain(domain: string) {
  return company<CompanyDomain>("/domains", {
    method: "POST",
    body: { domain },
  });
}
export function verifyCompanyDomain(id: string) {
  return company<DomainVerification>(
    `/domains/${encodeURIComponent(id)}/verify`,
    { method: "POST" },
  );
}
export function companyDomainVerification(id: string) {
  return company<DomainVerification>(
    `/domains/${encodeURIComponent(id)}/verification`,
  );
}
export function deleteCompanyDomain(id: string) {
  return company<void>(`/domains/${encodeURIComponent(id)}`, {
    method: "DELETE",
  });
}
// Store or clear the mailbox-level send-policy override. An empty policy is
// sent as null so the server clears the override and the mailbox inherits the
// company default again.
export function setMailboxSendPolicy(
  id: string,
  policy: MailSendPolicy | "",
  revision: number,
) {
  return company<{ updated: boolean }>(
    `/mailboxes/${encodeURIComponent(id)}/send-policy`,
    { method: "PUT", body: { send_policy: policy || null, revision } },
  );
}
// Emergency one-way revoke of a single published template version. Distinct
// from template-level retire: stops every not-yet-started delivery of that
// version; delivered outcomes, history and sibling versions are untouched.
export function revokeTemplateVersion(
  templateId: string,
  version: number,
  revision: number,
) {
  return company<{ revoked: boolean }>(
    `/templates/${encodeURIComponent(templateId)}/versions/${version}/revoke`,
    { method: "POST", body: { revision } },
  );
}
export function submitDraft(
  id: string,
  revision: number,
  idempotencyKey: string,
) {
  return company<DraftSubmission>(
    `/drafts/${encodeURIComponent(id)}/submit`,
    {
      method: "POST",
      headers: { "Idempotency-Key": idempotencyKey },
      body: { expected_revision: revision },
    },
  );
}
export async function company<T>(
  path: string,
  opts: RequestOptions = {},
): Promise<T> {
  const res = await request<APIResponse<T>>(`/api/v1/company${path}`, opts);
  return res.data;
}
export const workMailboxes = () =>
  company<WorkMailbox[]>("/mailboxes").then((v) => v ?? []);
export function workMessages(
  id: string,
  folder: string,
  q: string,
  page: number,
) {
  return request<APIListResponse<Message>>(
    `/api/v1/company${workPath(id)}/messages`,
    { params: { folder, q, page, per_page: 30 } },
  );
}
export function workMessage(id: string, message: string) {
  return company<MessageDetail>(
    `${workPath(id)}/messages/${encodeURIComponent(message)}`,
  );
}
export async function submissions(page: number) {
  const scope = sessionScope();
  const tenantId = typeof window === "undefined" ? undefined : localStorage.getItem("tabmail_tenant_id");
  const result = await request<unknown>("/api/v1/company/submissions", { params: { page, per_page: 30 } });
  assertSession(scope);
  return parseReceiptListResponse(result, { tenantId });
}
export async function submission(id: string) {
  const scope = sessionScope();
  const tenantId = typeof window === "undefined" ? undefined : localStorage.getItem("tabmail_tenant_id");
  const result = await request<unknown>(`/api/v1/company/submissions/${encodeURIComponent(id)}`);
  assertSession(scope);
  return parseReceiptResponse(result, { id, tenantId });
}
export async function submissionContent(id: string, signal?: AbortSignal) {
  const scope = sessionScope();
  const result = await request<unknown>(`/api/v1/company/submissions/${encodeURIComponent(id)}/content`, { signal });
  assertSession(scope);
  return parseContentResponse(result, id);
}
export function submissionAttachments(id: string, signal?: AbortSignal) {
  return company<SubmissionAttachment[]>(
    `/submissions/${encodeURIComponent(id)}/attachments`,
    { signal },
  );
}
export async function allEmployees(): Promise<AdminUser[]> {
  const users: AdminUser[] = [];
  for (let page = 1; page <= 100; page++) {
    const res = await request<APIListResponse<AdminUser>>(
      "/api/v1/admin/users",
      { params: { page, per_page: 100 } },
    );
    users.push(...(res.data ?? []));
    if (users.length >= res.meta.total || !res.data?.length) return users;
  }
  throw new Error(
    "Employee directory exceeds 10,000 records; use the paginated administration view",
  );
}
export function errorText(err: unknown): string {
  if (typeof err === "object" && err && "error" in err) {
    const value = (err as { error?: { message?: string } }).error?.message;
    if (value) return value;
  }
  return err instanceof Error ? err.message : "Request failed";
}
export async function downloadCompanyFile(path: string, filename: string) {
  const blob = await request<Blob>(`/api/v1/company${path}`, {
    responseType: "blob",
  });
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  a.click();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}
// Separate entered recipients without rewriting their address syntax. Quoted
// local parts, display names, and comments can contain the same separators.
// This only tokenizes: unfinished/invalid input stays intact for server validation.
export const addresses = (value: string) => {
  const parts: string[] = [];
  let start = 0;
  let quoted = false;
  let escaped = false;
  let commentDepth = 0;
  const append = (end: number) => {
    const part = value.slice(start, end).trim();
    if (part) parts.push(part);
  };
  for (let i = 0; i < value.length; i++) {
    const character = value[i];
    if (escaped) {
      escaped = false;
      continue;
    }
    if (character === "\\" && (quoted || commentDepth > 0)) {
      escaped = true;
      continue;
    }
    if (commentDepth > 0) {
      if (character === "(") commentDepth++;
      else if (character === ")") commentDepth--;
      continue;
    }
    if (character === '"') {
      quoted = !quoted;
      continue;
    }
    if (quoted) continue;
    if (character === "(") {
      commentDepth = 1;
      continue;
    }
    if (character === "," || character === ";" || character === "\n") {
      append(i);
      start = i + 1;
    }
  }
  append(value.length);
  return [...new Set(parts)];
};
