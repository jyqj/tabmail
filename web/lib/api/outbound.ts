import type {
  APIListResponse,
  APIResponse,
  OutboundJob,
  SendEmailRequest,
  SendEmailResponse,
} from "../types";
import { request } from "./base";

export function sendEmail(body: SendEmailRequest) {
  return request<APIResponse<SendEmailResponse>>("/api/v1/send", {
    method: "POST",
    body,
  });
}

export function getOutboundJob(id: string) {
  return request<APIResponse<OutboundJob>>(`/api/v1/outbound/${id}`);
}

export function listOutboundJobs(params?: { page?: number; per_page?: number }) {
  return request<APIListResponse<OutboundJob>>("/api/v1/outbound", {
    params: params as Record<string, string | number>,
  });
}
