import type {
  APIResponse,
  APIListResponse,
  Mailbox,
  Message,
  MessageDetail,
  AdminUser,
} from "./types";
import { request, type RequestOptions } from "./api/base";

// Outbound send policy vocabulary shared by the company default and the
// per-mailbox override. Kept in sync with authz.MailSendPolicy on the server.
export type MailSendPolicy = "free" | "template_required" | "disabled";

export interface CompanySettings {
  tenant_id?: string;
  name: string;
  primary_zone_id: string;
  domain?: string;
  revision: number;
  /** Company-wide default send policy. Omitted on input = leave unchanged. */
  mail_send_policy?: MailSendPolicy;
}
export interface WorkMailbox {
  mailbox: Mailbox;
  can_read: boolean;
  can_organize: boolean;
  can_send: boolean;
  template_only: boolean;
  revision: number;
}
export interface WorkGrant {
  user_id: string;
  can_read: boolean;
  can_organize: boolean;
  can_send: boolean;
  template_only: boolean;
}
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
  id?: string;
  name: string;
  draft: TemplateDraft;
  revision: number;
  retired?: boolean;
  updated_at?: string;
}
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
  id?: string;
  mailbox_id: string;
  payload: DraftPayload;
  template_version?: DraftTemplateVersion;
  revision: number;
  updated_at?: string;
}
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
export type SubmissionStatus =
  | "submitted"
  | "waiting"
  | "sending"
  | "partially_accepted"
  | "accepted"
  | "needs_attention";
export interface SubmissionRecipient {
  address: string;
  state: string;
}
// Employee-facing projection of an outbound submission. Queue-internal fields
// (attempts, leases, SMTP responses) stay in the recovery/operations surface.
// Interaction hints projected by the server for one submission receipt. They
// express what the interface may offer — they are never authorization
// credentials, and every action re-runs the full authorization chain
// server-side. Omitted on stale cached data.
export interface SubmissionCapabilities {
  view_content: boolean;
  retry: boolean;
  retry_block_reason?: string;
}
export interface Submission {
  id: string;
  mailbox_id: string;
  from: string;
  subject: string;
  recipients: SubmissionRecipient[];
  status: SubmissionStatus;
  template_version_id?: string;
  draft_consumed: boolean;
  attachment_count: number;
  created_at: string;
  content_redacted: boolean;
  delivery_uncertain: boolean;
  capabilities?: SubmissionCapabilities;
}
// The actual sent message for a readable submission. Recipients are the
// structural To/CC columns; BCC and queue internals are never projected, and
// custom headers arrive already filtered to the wire-safe subset.
export interface SubmissionContent {
  id: string;
  subject: string;
  from: string;
  to: string[];
  cc?: string[];
  headers?: Record<string, string>;
  text_body?: string;
  html_body?: string;
  created_at: string;
  content_redacted: boolean;
}
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
export function submissions(page: number) {
  return request<APIListResponse<Submission>>(
    "/api/v1/company/submissions",
    { params: { page, per_page: 30 } },
  );
}
export function submission(id: string) {
  return company<Submission>(`/submissions/${encodeURIComponent(id)}`);
}
export function submissionContent(id: string) {
  return company<SubmissionContent>(
    `/submissions/${encodeURIComponent(id)}/content`,
  );
}
export function submissionAttachments(id: string) {
  return company<SubmissionAttachment[]>(
    `/submissions/${encodeURIComponent(id)}/attachments`,
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
// Plain addresses only: this deliberately matches the compose form contract.
// Display-name parsing belongs on the server; do not guess with an ad-hoc RFC parser.
export const addresses = (value: string) => [
  ...new Set(
    value
      .split(/[,;\n]+/)
      .map((v) => v.trim())
      .filter(Boolean),
  ),
];
