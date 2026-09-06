import { request } from "./base";
import type {
  APIResponse,
  APIListResponse,
  Mailbox,
  OutboundJob,
} from "../types";

export interface Company {
  tenant_id: string;
  primary_zone_id: string;
  domain: string;
  name: string;
  owner_id: string;
}
export interface CompanyMember {
  user_id: string;
  email: string;
  display_name: string;
  company_role: "admin" | "employee" | "restricted" | "viewer";
  is_active: boolean;
  daily_send_quota: number;
}
export interface CompanyState {
  company: Company | null;
  member: CompanyMember | null;
}
export interface MailboxGrant {
  mailbox_id: string;
  user_id: string;
  address: string;
  can_read: boolean;
  can_organize: boolean;
  can_send: boolean;
  template_only: boolean;
}
export interface MailTemplate {
  id: string;
  family_id: string;
  version: number;
  name: string;
  subject: string;
  text_body: string;
  html_body: string;
  variables: Record<string, number>;
  mailbox_ids: string[];
  status: "draft" | "published" | "retired";
}
export type TemplateDraft = Pick<
  MailTemplate,
  "name" | "subject" | "text_body" | "html_body" | "variables" | "mailbox_ids"
> & { family_id?: string };
export const companyState = () =>
  request<APIResponse<CompanyState>>("/api/v1/company");
export const companyMembers = () =>
  request<APIResponse<CompanyMember[]>>("/api/v1/company/employees");
export const companyGrants = (all = false) =>
  request<APIResponse<MailboxGrant[]>>("/api/v1/company/grants", {
    params: { all: String(all) },
  });
export const companyMailboxes = (all = false, page = 1) =>
  request<APIListResponse<Mailbox>>("/api/v1/company/mailboxes", {
    params: { all: String(all), page, per_page: 100 },
  });
export const companyTemplates = (all = false) =>
  request<APIResponse<MailTemplate[]>>("/api/v1/company/templates", {
    params: { all: String(all) },
  });
export const enableCompany = (primary_zone_id: string, name: string) =>
  request<APIResponse<Company>>("/api/v1/company", {
    method: "POST",
    body: { primary_zone_id, name, confirm_lockdown: true },
  });
export const provisionEmployee = (
  local_part: string,
  display_name: string,
  company_role: CompanyMember["company_role"],
  daily_send_quota: number,
) =>
  request<
    APIResponse<{
      activation_token: string;
      member: CompanyMember;
      expires_at: string;
    }>
  >("/api/v1/company/employees", {
    method: "POST",
    body: { local_part, display_name, company_role, daily_send_quota },
  });
export const updateEmployee = (member: CompanyMember) =>
  request("/api/v1/company/employees/" + encodeURIComponent(member.user_id), {
    method: "PATCH",
    body: {
      company_role: member.company_role,
      is_active: member.is_active,
      daily_send_quota: member.daily_send_quota,
    },
  });
export const reinviteEmployee = (id: string) =>
  request<APIResponse<{ activation_token: string }>>(
    "/api/v1/company/employees/" + encodeURIComponent(id) + "/invite",
    { method: "POST", body: {} },
  );
export const createSharedMailbox = (local_part: string) =>
  request("/api/v1/company/mailboxes", {
    method: "POST",
    body: { local_part },
  });
export const saveGrant = (grant: MailboxGrant) =>
  request("/api/v1/company/grants", { method: "PUT", body: grant });
export const saveTemplate = (template: TemplateDraft) =>
  request<APIResponse<MailTemplate>>("/api/v1/company/templates", {
    method: "POST",
    body: template,
  });
export const setTemplateStatus = (
  id: string,
  status: "published" | "retired",
) =>
  request("/api/v1/company/templates/" + encodeURIComponent(id), {
    method: "PATCH",
    body: { status },
  });
export const previewTemplate = (
  template_id: string,
  mailbox_id: string,
  variables: Record<string, string>,
) =>
  request<
    APIResponse<{ subject: string; text_body: string; html_body: string }>
  >("/api/v1/company/templates/preview", {
    method: "POST",
    body: { template_id, mailbox_id, variables },
  });
export const activateEmployee = (token: string, password: string) =>
  request("/api/v1/company/activate-account", {
    method: "POST",
    body: { token, password },
  });
export interface CompanyCompose {
  from: string;
  recipients: string;
  templateId: string;
  variables: Record<string, string>;
  subject: string;
  text: string;
  templateRequired: boolean;
}
export function composePayload(form: CompanyCompose) {
  const to = form.recipients
    .split(/[\n,;]+/)
    .map((x) => x.trim())
    .filter(Boolean);
  if (!form.from || !to.length) throw new Error("请选择发件身份并填写收件人");
  if (form.templateRequired && !form.templateId)
    throw new Error("这个身份必须使用已发布模板");
  if (form.templateId)
    return {
      from: form.from,
      to,
      template_id: form.templateId,
      variables: form.variables,
    };
  if (!form.subject.trim() || !form.text.trim())
    throw new Error("请填写主题和正文");
  return { from: form.from, to, subject: form.subject, text_body: form.text };
}
export const sendCompanyMail = (form: CompanyCompose) =>
  request<APIResponse<OutboundJob>>("/api/v1/send", {
    method: "POST",
    body: composePayload(form),
  });
export function companyError(error: unknown): string {
  return error instanceof Error
    ? error.message
    : (error as { error?: { message?: string } })?.error?.message ||
        "操作失败，请重试";
}
