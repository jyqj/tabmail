import type { APIResponse, MailboxTokenResponse, LoginResponse } from "../types";
import { request } from "./base";

export function acceptInvite(code: string, password: string, displayName?: string) {
  return request<APIResponse<LoginResponse>>("/api/v1/auth/accept-invite", {
    method: "POST",
    body: { invite_code: code, password, display_name: displayName },
  });
}

export function changePassword(currentPassword: string, newPassword: string) {
  return request<APIResponse<{ status: string }>>("/api/v1/auth/change-password", {
    method: "POST",
    body: { old_password: currentPassword, new_password: newPassword },
  });
}

export function issueToken(address: string, password: string) {
  return request<APIResponse<MailboxTokenResponse>>("/api/v1/token", {
    method: "POST",
    body: { address, password },
  });
}

export function login(email: string, password: string) {
  return request<APIResponse<LoginResponse>>("/api/v1/auth/login", {
    method: "POST",
    body: { email, password },
  });
}

export function register(email: string, password: string, displayName?: string) {
  return request<APIResponse<LoginResponse>>("/api/v1/auth/register", {
    method: "POST",
    body: { email, password, display_name: displayName },
  });
}

export function logoutSession(refreshToken?: string) {
  return request<unknown>("/api/v1/auth/logout", {
    method: "POST",
    body: refreshToken ? { refresh_token: refreshToken } : {},
  });
}
