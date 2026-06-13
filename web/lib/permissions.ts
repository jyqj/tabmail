import type { EffectivePermission } from "@/lib/types";

// ============================================================
// Frontend permission predicates — UX ONLY.
//
// These helpers only control UI affordances (button visibility,
// degradation hints, nav items). They are NOT a security boundary:
// authoritative enforcement lives in the backend authz seam, which
// re-checks every request server-side.
//
// Decision table mirrored from the backend:
// - super_admin       -> allowed everything
// - admin             -> allowed everything tenant-scoped (the backend
//                        injects an unlimited synthetic permission for
//                        admins, so the level check comes FIRST and a
//                        missing/failed permissions fetch never blocks
//                        admins)
// - user              -> allowed iff permissions?.can_<x> === true
// - permissions null  -> (still loading or fetch failed) treated as NOT
//                        allowed for users — fail closed
// - public / mailbox  -> never allowed for these account capabilities
// ============================================================

export type PermissionLevel = "public" | "mailbox" | "user" | "admin" | "super_admin";

/** True for tenant admins and platform super admins. */
export function isAdminLevel(level: PermissionLevel): boolean {
  return level === "super_admin" || level === "admin";
}

/** True only for the platform-wide super admin. */
export function isSuperAdminLevel(level: PermissionLevel): boolean {
  return level === "super_admin";
}

/**
 * Shared capability rule: admins always pass (level check FIRST —
 * backend injects unlimited synthetic permissions for them); plain
 * users pass only when the effective permission flag is explicitly
 * true. Null/undefined permissions fail closed for users.
 */
function hasCapability(
  level: PermissionLevel,
  permissions: EffectivePermission | null | undefined,
  flag: "can_create_domains" | "can_create_routes" | "can_send" | "can_create_api_keys",
): boolean {
  if (isAdminLevel(level)) return true;
  return level === "user" && permissions?.[flag] === true;
}

export function canCreateDomains(
  level: PermissionLevel,
  permissions: EffectivePermission | null | undefined,
): boolean {
  return hasCapability(level, permissions, "can_create_domains");
}

export function canCreateRoutes(
  level: PermissionLevel,
  permissions: EffectivePermission | null | undefined,
): boolean {
  return hasCapability(level, permissions, "can_create_routes");
}

export function canSend(
  level: PermissionLevel,
  permissions: EffectivePermission | null | undefined,
): boolean {
  return hasCapability(level, permissions, "can_send");
}

export function canCreateAPIKeys(
  level: PermissionLevel,
  permissions: EffectivePermission | null | undefined,
): boolean {
  return hasCapability(level, permissions, "can_create_api_keys");
}

/** Tenant-user management (invites, cross-tenant user admin) is super_admin only. */
export function canManageTenantUsers(level: PermissionLevel): boolean {
  return isSuperAdminLevel(level);
}
