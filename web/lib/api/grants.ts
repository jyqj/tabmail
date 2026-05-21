import type {
  APIResponse,
  MailboxGrant,
  MailboxGrantRole,
  SendAsGrant,
  SendIdentity,
  ZoneGrant,
  ZoneGrantRole,
} from "../types";
import { request } from "./base";

// Mailbox grants

export function listMailboxGrants(mailboxId: string) {
  return request<APIResponse<MailboxGrant[]>>(`/api/v1/mailboxes/${mailboxId}/grants`);
}

export function createMailboxGrant(
  mailboxId: string,
  body: { principal_type: string; principal_id: string; role: MailboxGrantRole }
) {
  return request<APIResponse<MailboxGrant>>(`/api/v1/mailboxes/${mailboxId}/grants`, {
    method: "POST",
    body,
  });
}

export function deleteMailboxGrant(mailboxId: string, grantId: string) {
  return request<void>(`/api/v1/mailboxes/${mailboxId}/grants/${grantId}`, {
    method: "DELETE",
  });
}

// Zone grants

export function listZoneGrants(zoneId: string) {
  return request<APIResponse<ZoneGrant[]>>(`/api/v1/domains/${zoneId}/grants`);
}

export function createZoneGrant(
  zoneId: string,
  body: { principal_type: string; principal_id: string; role: ZoneGrantRole }
) {
  return request<APIResponse<ZoneGrant>>(`/api/v1/domains/${zoneId}/grants`, {
    method: "POST",
    body,
  });
}

export function deleteZoneGrant(zoneId: string, grantId: string) {
  return request<void>(`/api/v1/domains/${zoneId}/grants/${grantId}`, {
    method: "DELETE",
  });
}

// Send identities

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

// Send-as grants

export function listSendAsGrants(identityId: string) {
  return request<APIResponse<SendAsGrant[]>>(`/api/v1/send-identities/${identityId}/grants`);
}

export function createSendAsGrant(
  identityId: string,
  body: { principal_type: string; principal_id: string; daily_quota: number }
) {
  return request<APIResponse<SendAsGrant>>(`/api/v1/send-identities/${identityId}/grants`, {
    method: "POST",
    body,
  });
}

export function deleteSendAsGrant(identityId: string, grantId: string) {
  return request<void>(`/api/v1/send-identities/${identityId}/grants/${grantId}`, {
    method: "DELETE",
  });
}
