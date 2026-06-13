import type { APIResponse, SendIdentity } from "../types";
import { request } from "./base";

export function listSendIdentities() {
  return request<APIResponse<SendIdentity[]>>("/api/v1/send-identities");
}

export function createSendIdentity(body: { zone_id: string; address: string }) {
  return request<APIResponse<SendIdentity>>("/api/v1/send-identities", {
    method: "POST",
    body,
  });
}

export function deleteSendIdentity(id: string) {
  return request<void>(`/api/v1/send-identities/${id}`, { method: "DELETE" });
}
