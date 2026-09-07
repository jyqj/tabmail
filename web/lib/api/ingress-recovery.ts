import type { APIListResponse, APIResponse, IngestJob } from "../types";
import { getBaseUrl } from "./base";

export interface IngressSession {
  userId: string;
  accessToken: string;
  tenantId: string | null;
}
export interface IngressTarget {
  job_id: string;
  mailbox_id: string;
  tenant_id: string;
  zone_id: string;
  address: string;
  state: "pending" | "delivered" | "held";
  message_id?: string;
  attempts: number;
  last_error: string;
}
export interface IngressInspection {
  id: string;
  state: string;
  recovery_managed: boolean;
  attempts: number;
  last_error: string;
  created_at: string;
  updated_at: string;
  next_attempt_at: string;
  expected_bytes: number | null;
  can_retry: boolean;
  retry_block_reason: string;
  targets: IngressTarget[];
}
export interface IngressFilters {
  page: number;
  per_page: number;
  state?: string;
  source?: string;
  recipient?: string;
}
export class IngressRequestError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

export function isIngressSessionCurrent(session: IngressSession): boolean {
  try {
    const user = JSON.parse(localStorage.getItem("tabmail_user") ?? "null");
    return (
      user?.id === session.userId &&
      user?.role === "super_admin" &&
      localStorage.getItem("tabmail_access_token") === session.accessToken &&
      localStorage.getItem("tabmail_tenant_id") === session.tenantId
    );
  } catch {
    return false;
  }
}

// Operator mutations never participate in the legacy client's automatic refresh /
// replay. Capture credentials once; no late response may mutate another session.
async function operatorRequest<T>(
  session: IngressSession,
  path: string,
  signal: AbortSignal,
  body?: unknown,
): Promise<T> {
  if (!isIngressSessionCurrent(session))
    throw new IngressRequestError(401, "Session changed");
  const headers: Record<string, string> = {
    Authorization: `Bearer ${session.accessToken}`,
  };
  if (session.tenantId) headers["X-Tenant-ID"] = session.tenantId;
  if (body !== undefined) headers["Content-Type"] = "application/json";
  const response = await fetch(`${getBaseUrl()}${path}`, {
    method: body === undefined ? "GET" : "POST",
    headers,
    cache: "no-store",
    credentials: "omit",
    signal,
    body: body === undefined ? undefined : JSON.stringify(body),
  });
  const data = await response.json().catch(() => null);
  if (!isIngressSessionCurrent(session))
    throw new IngressRequestError(401, "Session changed");
  if (!response.ok)
    throw new IngressRequestError(
      response.status,
      data?.error?.message ?? `HTTP ${response.status}`,
    );
  if (!data || typeof data !== "object")
    throw new IngressRequestError(502, "Invalid response");
  return data as T;
}
export async function listIngress(
  session: IngressSession,
  filters: IngressFilters,
  signal: AbortSignal,
) {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(filters))
    if (value !== undefined && value !== "") params.set(key, String(value));
  const result = await operatorRequest<APIListResponse<IngestJob>>(
    session,
    `/api/v1/admin/ingest/jobs?${params}`,
    signal,
  );
  if (
    !Array.isArray(result.data) ||
    !Number.isSafeInteger(result.meta?.total) ||
    result.meta.total < 0
  )
    throw new IngressRequestError(502, "Invalid receipt list");
  return result;
}
export async function inspectIngress(
  session: IngressSession,
  id: string,
  signal: AbortSignal,
) {
  const result = await operatorRequest<APIResponse<IngressInspection>>(
    session,
    `/api/v1/admin/ingest/jobs/${encodeURIComponent(id)}`,
    signal,
  );
  if (result.data?.id !== id || !Array.isArray(result.data?.targets))
    throw new IngressRequestError(502, "Invalid receipt snapshot");
  return result;
}
export function canRetryInspection(receipt: IngressInspection): boolean {
  return (
    receipt.can_retry === true &&
    receipt.recovery_managed === true &&
    receipt.state === "dead" &&
    receipt.retry_block_reason === "" &&
    typeof receipt.updated_at === "string" &&
    Number.isFinite(Date.parse(receipt.updated_at)) &&
    receipt.targets.length > 0 &&
    receipt.targets.every(
      (t) =>
        t.job_id === receipt.id &&
        ["pending", "delivered", "held"].includes(t.state),
    ) &&
    receipt.targets.some((t) => t.state === "held")
  );
}
export function retryIngress(
  session: IngressSession,
  receipt: IngressInspection,
  reason: string,
  signal: AbortSignal,
) {
  if (
    !canRetryInspection(receipt) ||
    !reason.trim() ||
    [...reason].length > 2000
  )
    throw new IngressRequestError(400, "Invalid recovery review");
  return operatorRequest<APIResponse<{ requeued: boolean }>>(
    session,
    `/api/v1/admin/ingest/jobs/${encodeURIComponent(receipt.id)}/retry`,
    signal,
    {
      reason: reason.trim(),
      observed_updated_at: receipt.updated_at,
    },
  );
}
