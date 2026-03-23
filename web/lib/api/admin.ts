import type {
  APIKeyCreated,
  APIListResponse,
  APIResponse,
  AuditEntry,
  EffectiveConfig,
  MonitorEvent,
  Plan,
  SMTPPolicy,
  SystemStats,
  Tenant,
  TenantAPIKey,
  TenantOverride,
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
