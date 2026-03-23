import type { APIListResponse, APIResponse, AccessMode, Mailbox } from "../types";
import { request } from "./base";

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
