import type { APIResponse, DomainZone, ResourceVisibility } from "../types";
import { request } from "./base";

export function listDomains() {
  return request<APIResponse<DomainZone[]>>("/api/v1/domains");
}

export function listAdminDomains() {
  return request<APIResponse<DomainZone[]>>("/api/v1/admin/domains");
}

export function updateAdminDomainAccess(
  id: string,
  body: { visibility?: ResourceVisibility; allow_random_subdomains?: boolean }
) {
  return request<APIResponse<DomainZone>>(`/api/v1/admin/domains/${id}`, {
    method: "PATCH",
    body,
  });
}
