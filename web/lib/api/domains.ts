import type {
  AccessMode,
  APIResponse,
  DomainVerificationResult,
  DomainRoute,
  DomainZone,
  RouteType,
  VerificationStatus,
} from "../types";
import { request } from "./base";

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
  return request<APIResponse<DomainVerificationResult>>(`/api/v1/domains/${id}/verify`, {
    method: "POST",
  });
}

export function getVerificationStatus(id: string) {
  return request<APIResponse<VerificationStatus>>(`/api/v1/domains/${id}/verification-status`);
}

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
  return request<APIResponse<DomainRoute>>(`/api/v1/domains/${domainId}/routes`, {
    method: "POST",
    body,
  });
}

export function deleteRoute(domainId: string, routeId: string) {
  return request<void>(`/api/v1/domains/${domainId}/routes/${routeId}`, {
    method: "DELETE",
  });
}
