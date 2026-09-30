import { request } from "./api/base";
import type { APIResponse } from "./types";

// A compatibility transport, not another content-permission policy. Deliberately
// no queue credentials, raw MIME/body, BCC or object-key fields in the UI type.
export interface LegacyOutboundReceipt {
  id: string;
  subject: string;
  mail_from: string;
  state: string;
  to?: string[];
  cc?: string[];
  created_at: string;
  content_redacted?: boolean;
}
export async function legacyOutboundReceipts(page = 1): Promise<LegacyOutboundReceipt[]> {
  const result = await request<APIResponse<LegacyOutboundReceipt[]>>("/api/v1/outbound", { params: { page, per_page: 20 } });
  return result.data;
}
export async function legacyOutboundReceipt(id: string): Promise<LegacyOutboundReceipt> {
  const result = await request<APIResponse<LegacyOutboundReceipt>>(`/api/v1/outbound/${encodeURIComponent(id)}`);
  return result.data;
}
