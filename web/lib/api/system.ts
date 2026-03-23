import { request } from "./base";

export function healthCheck() {
  return request<{ status: string }>("/health");
}
