import type {
  AdminUser,
  APIKeyCreated,
  APIListResponse,
  APIResponse,
  AuditEntry,
  EffectiveConfig,
  IngestJob,
  MonitorEvent,
  Plan,
  SMTPPolicy,
  SystemSetting,
  SystemStats,
  Tenant,
  TenantAPIKey,
  TenantOverride,
  UpdateUserRequest,
  WebhookDelivery,
} from "../types";
import { request, streamEvents, type EventStreamOptions } from "./base";

type PlanMutation = Partial<Omit<Plan, "id" | "created_at" | "updated_at">>;

export function streamAdminMonitorEvents(options: EventStreamOptions) {
  return streamEvents("/api/v1/admin/monitor/events", options, (data) => data as MonitorEvent);
}

export function listMonitorHistory(params?: {
  page?: number;
  per_page?: number;
  type?: string;
  mailbox?: string;
  sender?: string;
}) {
  return request<APIListResponse<MonitorEvent>>("/api/v1/admin/monitor/history", {
    params: params as Record<string, string | number>,
  });
}

// --- System settings ---

export function listSettings() {
  return request<APIResponse<SystemSetting[]>>("/api/v1/admin/settings");
}

export function updateSettings(body: Record<string, string>) {
  return request<APIResponse<SystemSetting[]>>("/api/v1/admin/settings", {
    method: "PATCH",
    body,
  });
}

// --- SMTP Policy ---

export function getSMTPPolicy() {
  return request<APIResponse<SMTPPolicy>>("/api/v1/admin/policy");
}

export function updateSMTPPolicy(body: SMTPPolicy) {
  return request<APIResponse<SMTPPolicy>>("/api/v1/admin/policy", {
    method: "PATCH",
    body,
  });
}

export function listTenants() {
  return request<APIResponse<Tenant[]>>("/api/v1/admin/tenants");
}

export function createTenant(body: { name: string; plan_id: string }) {
  return request<APIResponse<Tenant>>("/api/v1/admin/tenants", {
    method: "POST",
    body,
  });
}

export function updateTenantOverrides(id: string, overrides: TenantOverride) {
  return request<APIResponse<TenantOverride>>(`/api/v1/admin/tenants/${id}`, {
    method: "PATCH",
    body: overrides,
  });
}

export function deleteTenant(id: string) {
  return request<void>(`/api/v1/admin/tenants/${id}`, { method: "DELETE" });
}

export function getTenantConfig(id: string) {
  return request<APIResponse<EffectiveConfig>>(`/api/v1/admin/tenants/${id}/config`);
}

export function createAPIKey(
  tenantId: string,
  body: { label?: string; scopes?: string[] }
) {
  return request<APIResponse<APIKeyCreated>>(`/api/v1/admin/tenants/${tenantId}/keys`, {
    method: "POST",
    body,
  });
}

export function listAPIKeys(tenantId: string) {
  return request<APIResponse<TenantAPIKey[]>>(`/api/v1/admin/tenants/${tenantId}/keys`);
}

export function revokeAPIKey(tenantId: string, keyId: string) {
  return request<void>(`/api/v1/admin/tenants/${tenantId}/keys/${keyId}`, {
    method: "DELETE",
  });
}

// --- User-facing API key endpoints (own tenant) ---

export function createUserAPIKey(body: { label?: string; scopes?: string[] }) {
  return request<APIResponse<APIKeyCreated>>("/api/v1/keys", {
    method: "POST",
    body,
  });
}

export function listUserAPIKeys() {
  return request<APIResponse<TenantAPIKey[]>>("/api/v1/keys");
}

export function revokeUserAPIKey(keyId: string) {
  return request<void>(`/api/v1/keys/${keyId}`, { method: "DELETE" });
}

export function listPlans() {
  return request<APIResponse<Plan[]>>("/api/v1/admin/plans");
}

export function createPlan(body: PlanMutation) {
  return request<APIResponse<Plan>>("/api/v1/admin/plans", {
    method: "POST",
    body,
  });
}

export function updatePlan(id: string, body: PlanMutation) {
  return request<APIResponse<Plan>>(`/api/v1/admin/plans/${id}`, {
    method: "PATCH",
    body,
  });
}

export function deletePlan(id: string) {
  return request<void>(`/api/v1/admin/plans/${id}`, { method: "DELETE" });
}

export function getStats() {
  return request<APIResponse<SystemStats>>("/api/v1/admin/stats");
}

export function listAudit(params?: { page?: number; per_page?: number }) {
  return request<APIListResponse<AuditEntry>>("/api/v1/admin/audit", {
    params: params as Record<string, string | number>,
  });
}

export function listIngestJobs(params?: {
  page?: number;
  per_page?: number;
  state?: string;
  source?: string;
  recipient?: string;
}) {
  return request<APIListResponse<IngestJob>>("/api/v1/admin/ingest/jobs", {
    params: params as Record<string, string | number>,
  });
}

export function listWebhookDeliveries(params?: {
  page?: number;
  per_page?: number;
  state?: string;
  event_type?: string;
  url?: string;
}) {
  return request<APIListResponse<WebhookDelivery>>("/api/v1/admin/webhooks/deliveries", {
    params: params as Record<string, string | number>,
  });
}

// --- User management ---

export function inviteAdmin(email: string) {
  return request<APIResponse<{ id: string; email: string; invite_code: string; expires_at: string }>>(
    "/api/v1/admin/invite",
    { method: "POST", body: { email } }
  );
}

export function listUsers(params?: { page?: number; per_page?: number }) {
  return request<APIListResponse<AdminUser>>("/api/v1/admin/users", {
    params: params as Record<string, string | number>,
  });
}

export function updateUser(
  id: string,
  body: UpdateUserRequest
) {
  return request<APIResponse<AdminUser>>(`/api/v1/admin/users/${id}`, {
    method: "PATCH",
    body,
  });
}

export function deleteUser(id: string) {
  return request<void>(`/api/v1/admin/users/${id}`, { method: "DELETE" });
}
