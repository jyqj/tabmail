import { request } from "./api/base";
import { assertSession, sessionScope } from "./session";
import { parseReceiptListResponse, parseReceiptResponse, type OrdinaryReceipt } from "./receipt-types";

// Compatibility paths have the SAME closed aggregate DTO, never raw queue jobs.
export type LegacyOutboundReceipt = OrdinaryReceipt;
function currentTenant() {
  return typeof window === "undefined" ? undefined : localStorage.getItem("tabmail_tenant_id");
}
export async function legacyOutboundReceipts(page = 1): Promise<LegacyOutboundReceipt[]> {
  const scope = sessionScope(), tenantId = currentTenant();
  const result = await request<unknown>("/api/v1/outbound", { params: { page, per_page: 20 } });
  assertSession(scope);
  return parseReceiptListResponse(result, { tenantId }).data;
}
async function aggregate(id: string, suffix = "", signal?: AbortSignal): Promise<LegacyOutboundReceipt> {
  const scope = sessionScope(), tenantId = currentTenant();
  const result = await request<unknown>(`/api/v1/outbound/${encodeURIComponent(id)}${suffix}`, { signal });
  assertSession(scope);
  return parseReceiptResponse(result, { id, tenantId });
}
export function legacyOutboundReceipt(id: string) { return aggregate(id); }
// Historical function names retain the closed aggregate contract. There is no
// legacy /recipients route; use the authorized detail route for this alias.
export function legacyOutboundRecipients(id: string) { return aggregate(id); }
export function legacyOutboundAttempts(id: string) { return aggregate(id, "/attempts"); }
export async function retryOutboundReceipt(id: string, signal?: AbortSignal): Promise<LegacyOutboundReceipt> {
  const scope = sessionScope(), tenantId = currentTenant();
  const result = await request<unknown>(`/api/v1/outbound/${encodeURIComponent(id)}/retry`, { method: "POST", body: {}, signal });
  assertSession(scope);
  return parseReceiptResponse(result, { id, tenantId });
}
