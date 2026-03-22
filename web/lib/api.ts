import type {
  Plan,
  Tenant,
  TenantOverride,
  TenantAPIKey,
  APIKeyCreated,
  EffectiveConfig,
  DomainZone,
  DomainRoute,
  Mailbox,
  Message,
  MessageDetail,
  VerificationStatus,
  SystemStats,
  MailboxTokenResponse,
  APIResponse,
  APIListResponse,
  APIError,
  AccessMode,
  RouteType,
  MonitorEvent,
  SMTPPolicy,
} from "./types";

export function getBaseUrl(): string {
  if (typeof window !== "undefined") {
    return process.env.NEXT_PUBLIC_API_URL || "";
  }
  return process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  params?: Record<string, string | number>;
}

interface EventStreamOptions {
  signal?: AbortSignal;
  onEvent: (event: { type: string; data: unknown }) => void;
}

function getStoredKey(key: string): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(key);
}

function shouldUseMailboxToken(path: string, mailboxAddress: string | null): boolean {
  if (!path.startsWith("/api/v1/mailbox/")) return false;
  if (!mailboxAddress) return true;
  const parts = path.split("/");
  const encodedAddress = parts[4];
  if (!encodedAddress) return false;
  return decodeURIComponent(encodedAddress).toLowerCase() === mailboxAddress.toLowerCase();
}

function buildHeaders(path: string, extra?: Record<string, string>) {
  const headers: Record<string, string> = { ...(extra || {}) };
  const adminKey = getStoredKey("tabmail_admin_key");
  const apiKey = getStoredKey("tabmail_api_key");
  const tenantId = getStoredKey("tabmail_tenant_id");
  const mailboxToken = getStoredKey("tabmail_mailbox_token");
  const mailboxAddress = getStoredKey("tabmail_mailbox_address");

  if (adminKey) {
    headers["X-Admin-Key"] = adminKey;
    if (tenantId) headers["X-Tenant-ID"] = tenantId;
  } else if (apiKey) {
    headers["X-API-Key"] = apiKey;
  } else if (mailboxToken && shouldUseMailboxToken(path, mailboxAddress)) {
    headers["Authorization"] = `Bearer ${mailboxToken}`;
  }
  return headers;
}

async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const base = getBaseUrl();
  const url = new URL(`${base}${path}`, typeof window !== "undefined" ? window.location.origin : undefined);

  if (opts.params) {
    for (const [k, v] of Object.entries(opts.params)) {
      if (v !== undefined && v !== null) url.searchParams.set(k, String(v));
    }
  }

  const headers: Record<string, string> = buildHeaders(path, opts.headers);
  if (opts.body) headers["Content-Type"] = "application/json";

  const res = await fetch(url.toString(), {
    method: opts.method || "GET",
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });

  if (!res.ok) {
    const err: APIError = await res.json().catch(() => ({
      error: { code: "UNKNOWN", message: res.statusText },
    }));
    throw err;
  }

  if (res.status === 204) return {} as T;

  const ct = res.headers.get("content-type") || "";
  if (ct.includes("message/rfc822") || ct.includes("text/plain")) {
    return (await res.text()) as unknown as T;
  }

  return res.json();
}

// ── Token ────────────────────────────────────────────────────────────
export function issueToken(address: string, password: string) {
  return request<APIResponse<MailboxTokenResponse>>("/api/v1/token", {
    method: "POST",
    body: { address, password },
  });
}

// ── Domains ──────────────────────────────────────────────────────────
export function listDomains() {
  return request<APIResponse<DomainZone[]>>("/api/v1/domains");
}

export function createDomain(domain: string) {
  return request<APIResponse<DomainZone>>("/api/v1/domains", {
    method: "POST",
    body: { domain },
  });
}

export function deleteDomain(id: string) {
  return request<void>(`/api/v1/domains/${id}`, { method: "DELETE" });
}

export function verifyDomain(id: string) {
  return request<APIResponse<DomainZone>>(`/api/v1/domains/${id}/verify`, {
    method: "POST",
  });
}

export function getVerificationStatus(id: string) {
  return request<APIResponse<VerificationStatus>>(
    `/api/v1/domains/${id}/verification-status`
  );
}

// ── Routes ───────────────────────────────────────────────────────────
export function listRoutes(domainId: string) {
  return request<APIResponse<DomainRoute[]>>(`/api/v1/domains/${domainId}/routes`);
}

export function createRoute(
  domainId: string,
  body: {
    route_type: RouteType;
    match_value: string;
    range_start?: number;
    range_end?: number;
    auto_create_mailbox?: boolean;
    retention_hours_override?: number;
    access_mode_default?: AccessMode;
  }
) {
  return request<APIResponse<DomainRoute>>(
    `/api/v1/domains/${domainId}/routes`,
    { method: "POST", body }
  );
}

export function deleteRoute(domainId: string, routeId: string) {
  return request<void>(`/api/v1/domains/${domainId}/routes/${routeId}`, {
    method: "DELETE",
  });
}

// ── Mailboxes ────────────────────────────────────────────────────────
export function listMailboxes(page = 1, perPage = 30) {
  return request<APIListResponse<Mailbox>>("/api/v1/mailboxes", {
    params: { page, per_page: perPage },
  });
}

export function createMailbox(body: {
  address: string;
  access_mode?: AccessMode;
  password?: string;
}) {
  return request<APIResponse<Mailbox>>("/api/v1/mailboxes", {
    method: "POST",
    body,
  });
}

export function deleteMailbox(id: string) {
  return request<void>(`/api/v1/mailboxes/${id}`, { method: "DELETE" });
}

// ── Messages ─────────────────────────────────────────────────────────
export function listMessages(address: string, page = 1, perPage = 30) {
  return request<APIListResponse<Message>>(
    `/api/v1/mailbox/${encodeURIComponent(address)}`,
    { params: { page, per_page: perPage } }
  );
}

export function getMessage(address: string, id: string) {
  return request<APIResponse<MessageDetail>>(
    `/api/v1/mailbox/${encodeURIComponent(address)}/${id}`
  );
}

export function markMessageSeen(address: string, id: string) {
  return request<APIResponse<Message>>(
    `/api/v1/mailbox/${encodeURIComponent(address)}/${id}`,
    { method: "PATCH", body: { seen: true } }
  );
}

export function deleteMessage(address: string, id: string) {
  return request<void>(
    `/api/v1/mailbox/${encodeURIComponent(address)}/${id}`,
    { method: "DELETE" }
  );
}

export function purgeMailbox(address: string) {
  return request<void>(`/api/v1/mailbox/${encodeURIComponent(address)}`, {
    method: "DELETE",
  });
}

export function getMessageSource(address: string, id: string) {
  return request<string>(
    `/api/v1/mailbox/${encodeURIComponent(address)}/${id}/source`
  );
}

export async function streamMailboxEvents(
  address: string,
  { signal, onEvent }: EventStreamOptions
) {
  const path = `/api/v1/mailbox/${encodeURIComponent(address)}/events`;
  const res = await fetch(`${getBaseUrl()}${path}`, {
    method: "GET",
    headers: buildHeaders(path),
    signal,
  });

  if (!res.ok || !res.body) {
    const err: APIError = await res.json().catch(() => ({
      error: { code: "UNKNOWN", message: res.statusText },
    }));
    throw err;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let currentEvent = "message";

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });

    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);

      let dataLine = "";
      for (const line of chunk.split("\n")) {
        if (line.startsWith("event:")) currentEvent = line.slice(6).trim();
        if (line.startsWith("data:")) dataLine += line.slice(5).trim();
      }
      if (dataLine) {
        try {
          onEvent({ type: currentEvent, data: JSON.parse(dataLine) });
        } catch {
          onEvent({ type: currentEvent, data: dataLine });
        }
      }
      currentEvent = "message";
      boundary = buffer.indexOf("\n\n");
    }
  }
}

export async function streamAdminMonitorEvents({
  signal,
  onEvent,
}: EventStreamOptions) {
  const path = "/api/v1/admin/monitor/events";
  const res = await fetch(`${getBaseUrl()}${path}`, {
    method: "GET",
    headers: buildHeaders(path),
    signal,
  });

  if (!res.ok || !res.body) {
    const err: APIError = await res.json().catch(() => ({
      error: { code: "UNKNOWN", message: res.statusText },
    }));
    throw err;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let currentEvent = "message";

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;
    buffer += decoder.decode(value, { stream: true });
    let boundary = buffer.indexOf("\n\n");
    while (boundary >= 0) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);
      let dataLine = "";
      for (const line of chunk.split("\n")) {
        if (line.startsWith("event:")) currentEvent = line.slice(6).trim();
        if (line.startsWith("data:")) dataLine += line.slice(5).trim();
      }
      if (dataLine) {
        try {
          onEvent({ type: currentEvent, data: JSON.parse(dataLine) as MonitorEvent });
        } catch {
          onEvent({ type: currentEvent, data: dataLine });
        }
      }
      currentEvent = "message";
      boundary = buffer.indexOf("\n\n");
    }
  }
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

// ── Admin: Tenants ───────────────────────────────────────────────────
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
  return request<APIResponse<EffectiveConfig>>(
    `/api/v1/admin/tenants/${id}/config`
  );
}

// ── Admin: API Keys ──────────────────────────────────────────────────
export function createAPIKey(
  tenantId: string,
  body: { label?: string; scopes?: string[] }
) {
  return request<APIResponse<APIKeyCreated>>(
    `/api/v1/admin/tenants/${tenantId}/keys`,
    { method: "POST", body }
  );
}

export function listAPIKeys(tenantId: string) {
  return request<APIResponse<TenantAPIKey[]>>(
    `/api/v1/admin/tenants/${tenantId}/keys`
  );
}

export function revokeAPIKey(tenantId: string, keyId: string) {
  return request<void>(
    `/api/v1/admin/tenants/${tenantId}/keys/${keyId}`,
    { method: "DELETE" }
  );
}

// ── Admin: Plans ─────────────────────────────────────────────────────
export function listPlans() {
  return request<APIResponse<Plan[]>>("/api/v1/admin/plans");
}

export function createPlan(body: Partial<Omit<Plan, "id" | "created_at" | "updated_at">>) {
  return request<APIResponse<Plan>>("/api/v1/admin/plans", {
    method: "POST",
    body,
  });
}

export function updatePlan(
  id: string,
  body: Partial<Omit<Plan, "id" | "created_at" | "updated_at">>
) {
  return request<APIResponse<Plan>>(`/api/v1/admin/plans/${id}`, {
    method: "PATCH",
    body,
  });
}

export function deletePlan(id: string) {
  return request<void>(`/api/v1/admin/plans/${id}`, { method: "DELETE" });
}

// ── Admin: Stats ─────────────────────────────────────────────────────
export function getStats() {
  return request<APIResponse<SystemStats>>("/api/v1/admin/stats");
}

// ── Health ───────────────────────────────────────────────────────────
export function healthCheck() {
  return request<{ status: string }>("/health");
}
