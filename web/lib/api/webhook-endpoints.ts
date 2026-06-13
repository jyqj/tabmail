import type { APIResponse } from "../types";
import { request } from "./base";

export interface WebhookEndpoint {
  id: string;
  tenant_id: string;
  url: string;
  event_types: string[];
  is_active: boolean;
  created_by?: string | null;
  created_at: string;
  updated_at: string;
}

export function listWebhookEndpoints() {
  return request<APIResponse<WebhookEndpoint[]>>("/api/v1/webhook-endpoints");
}

export function createWebhookEndpoint(body: { url: string; secret?: string; event_types?: string[] }) {
  return request<APIResponse<WebhookEndpoint>>("/api/v1/webhook-endpoints", {
    method: "POST",
    body,
  });
}

export function updateWebhookEndpoint(id: string, body: { url?: string; event_types?: string[]; is_active?: boolean }) {
  return request<APIResponse<WebhookEndpoint>>(`/api/v1/webhook-endpoints/${id}`, {
    method: "PATCH",
    body,
  });
}

export function deleteWebhookEndpoint(id: string) {
  return request<void>(`/api/v1/webhook-endpoints/${id}`, { method: "DELETE" });
}
