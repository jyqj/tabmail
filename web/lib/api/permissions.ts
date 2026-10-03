import type {
  APIResponse,
  EffectivePermission,
  PermissionProfile,
  UserPermissionOverride,
} from "../types";
import { validatePermissionEditorCommand, validatePermissionEditorSnapshot, validateAssignmentCommand, validateObservedPermissionProfile, validateProfileUpdateCommand, validateProfileDeletionCommand, validateProfileDeletionPreview } from "./permission-editor-types";
import type { PermissionEditorCommand, PermissionEditorSnapshot, PermissionAssignmentCommand, PermissionProfileUpdateCommand, PermissionProfileDeletionCommand, PermissionProfileDeletionPreview, EditorPermissionProfile, ObservedPermissionProfile } from "./permission-editor-types";
import { request } from "./base";

// Admin: Permission profiles

export function listPermissionProfiles() {
  return request<APIResponse<EditorPermissionProfile[]>>("/api/v1/admin/permissions");
}

export function createPermissionProfile(data: Partial<PermissionProfile>) {
  return request<APIResponse<PermissionProfile>>("/api/v1/admin/permissions", {
    method: "POST",
    body: data,
  });
}

export async function updatePermissionProfile(id: string, command: PermissionProfileUpdateCommand) {
  validateProfileUpdateCommand(command);
  // Go's UpdateInput is flat; a nested `fields` object is not a wire field.
  const response = await request<APIResponse<ObservedPermissionProfile>>(`/api/v1/admin/permissions/${id}`, {
    method: "PATCH", body: { ...command.fields, expected_revision: command.expected_revision },
  });
  validateObservedPermissionProfile(response.data);
  if (response.data.id !== id) throw new Error("Profile update target changed");
  return response;
}
export async function getPermissionProfileDeletionPreview(id: string) {
  const response = await request<APIResponse<PermissionProfileDeletionPreview>>(`/api/v1/admin/permissions/${id}/deletion-preview`);
  return { ...response, data: validateProfileDeletionPreview(response.data, id) };
}
export async function deletePermissionProfile(id: string, command: PermissionProfileDeletionCommand) {
  validateProfileDeletionCommand(command, id);
  return request<void>(`/api/v1/admin/permissions/${id}`, { method: "DELETE", body: command });
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

// Raw, revision-bound editor reads deliberately do not replace the legacy
// effective permission APIs used by authentication and compatibility callers.
export async function getUserPermissionEditor(userId: string, options?: { signal?: AbortSignal }) {
  const response = await request<APIResponse<PermissionEditorSnapshot>>(`/api/v1/admin/users/${userId}/permission-editor`, { signal: options?.signal });
  validatePermissionEditorSnapshot(response.data, userId);
  return response;
}
export async function patchUserPermissionEditor(userId: string, command: PermissionEditorCommand, options?: { signal?: AbortSignal }) {
  validatePermissionEditorCommand(command, userId);
  const response = await request<APIResponse<PermissionEditorSnapshot>>(`/api/v1/admin/users/${userId}/permission-editor`, {
    method: "PATCH", body: command, signal: options?.signal,
  });
  validatePermissionEditorSnapshot(response.data, userId);
  return response;
}

export async function assignUserPermissionEditor(userId: string, command: PermissionAssignmentCommand, options?: { signal?: AbortSignal }) {
  validateAssignmentCommand(command, userId);
  const response = await request<APIResponse<PermissionEditorSnapshot>>(`/api/v1/admin/users/${userId}/permission-editor/assignment`, {
    method: "POST", body: command, signal: options?.signal,
  });
  validatePermissionEditorSnapshot(response.data, userId);
  return response;
}
// Public spelling shared with the dedicated assignment consumer tests.
export const assignUserPermissionProfile = assignUserPermissionEditor;
