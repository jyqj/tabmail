import type { APIResponse, DomainZone } from "../types";
import { request } from "./base";

export function listDomains() {
  return request<APIResponse<DomainZone[]>>("/api/v1/domains");
}

export function listAdminDomains() {
  return request<APIResponse<DomainZone[]>>("/api/v1/admin/domains");
}
