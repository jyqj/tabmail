import type {
  APIResponse,
  APIListResponse,
  Mailbox,
  Message,
  MessageDetail,
  AdminUser,
} from "./types";
import { request, type RequestOptions } from "./api/base";

export interface CompanySettings {
  tenant_id?: string;
  name: string;
  primary_zone_id: string;
  domain?: string;
  revision: number;
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
export interface MailDraft {
  id?: string;
  mailbox_id: string;
  payload: DraftPayload;
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
export interface Receipt {
  id: string;
  state: string;
  error?: string;
  updated_at: string;
  raw_size: number;
  targets: RecoveryTarget[];
}
export const workPath = (id: string) => `/mailboxes/${encodeURIComponent(id)}`;
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
