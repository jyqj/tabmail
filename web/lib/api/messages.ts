import type {
  APIListResponse,
  APIResponse,
  Message,
  MessageDetail,
} from "../types";
import { request, streamEvents, type EventStreamOptions } from "./base";

export function listMessages(address: string, page = 1, perPage = 30) {
  return request<APIListResponse<Message>>(`/api/v1/mailbox/${encodeURIComponent(address)}`, {
    params: { page, per_page: perPage },
  });
}

export function getMessage(address: string, id: string) {
  return request<APIResponse<MessageDetail>>(`/api/v1/mailbox/${encodeURIComponent(address)}/${id}`);
}

export function markMessageSeen(address: string, id: string) {
  return request<APIResponse<Message>>(`/api/v1/mailbox/${encodeURIComponent(address)}/${id}`, {
    method: "PATCH",
    body: { seen: true },
  });
}

export function deleteMessage(address: string, id: string) {
  return request<void>(`/api/v1/mailbox/${encodeURIComponent(address)}/${id}`, {
    method: "DELETE",
  });
}

export function purgeMailbox(address: string) {
  return request<void>(`/api/v1/mailbox/${encodeURIComponent(address)}`, {
    method: "DELETE",
  });
}

export function getMessageSource(address: string, id: string) {
  return request<string>(`/api/v1/mailbox/${encodeURIComponent(address)}/${id}/source`);
}

export function streamMailboxEvents(address: string, options: EventStreamOptions) {
  return streamEvents(`/api/v1/mailbox/${encodeURIComponent(address)}/events`, options);
}
