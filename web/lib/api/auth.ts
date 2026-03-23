import type { APIResponse, MailboxTokenResponse } from "../types";
import { request } from "./base";

export function issueToken(address: string, password: string) {
  return request<APIResponse<MailboxTokenResponse>>("/api/v1/token", {
    method: "POST",
    body: { address, password },
  });
}
