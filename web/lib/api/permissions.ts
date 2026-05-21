import type {
  APIResponse,
  EffectivePermission,
  PermissionProfile,
  UserPermissionOverride,
} from "../types";
import { request } from "./base";

// Admin: Permission profiles

export function listPermissionProfiles() {
  return request<APIResponse<PermissionProfile[]>>("/api/v1/admin/permissions");
}

export function createPermissionProfile(data: Partial<PermissionProfile>) {
  return request<APIResponse<PermissionProfile>>("/api/v1/admin/permissions", {
    method: "POST",
    body: data,
  });
}

export function updatePermissionProfile(id: string, data: Partial<PermissionProfile>) {
  return request<APIResponse<PermissionProfile>>(`/api/v1/admin/permissions/${id}`, {
    method: "PATCH",
    body: data,
  });
}

export function deletePermissionProfile(id: string) {
  return request<void>(`/api/v1/admin/permissions/${id}`, { method: "DELETE" });
}

// Admin: User permissions

export function getUserPermission(userId: string) {
  return request<APIResponse<EffectivePermission>>(`/api/v1/admin/users/${userId}/permissions`);
}

export function setUserPermissionOverride(userId: string, data: Partial<UserPermissionOverride>) {
  return request<APIResponse<UserPermissionOverride>>(`/api/v1/admin/users/${userId}/permissions`, {
    method: "PUT",
    body: data,
  });
}

export function deleteUserPermissionOverride(userId: string) {
  return request<void>(`/api/v1/admin/users/${userId}/permissions`, { method: "DELETE" });
}

// Current user

export function getMyPermissions() {
  return request<APIResponse<EffectivePermission>>("/api/v1/auth/me/permissions");
}
