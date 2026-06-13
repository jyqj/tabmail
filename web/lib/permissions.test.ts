import { describe, expect, it } from "vitest";

import {
  canCreateAPIKeys,
  canCreateDomains,
  canCreateRoutes,
  canManageTenantUsers,
  canSend,
  isAdminLevel,
  isSuperAdminLevel,
  type PermissionLevel,
} from "./permissions";
import type { EffectivePermission } from "@/lib/types";

const LEVELS: PermissionLevel[] = ["public", "mailbox", "user", "admin", "super_admin"];

function perms(overrides: Partial<EffectivePermission> = {}): EffectivePermission {
  return {
    can_send: false,
    daily_send_quota: 0,
    daily_receive_quota: 0,
    max_mailboxes: 0,
    max_domains: 0,
    allowed_zone_ids: null,
    can_create_domains: false,
    can_create_routes: false,
    can_create_api_keys: false,
    ...overrides,
  };
}

const CAPABILITIES = [
  { name: "canCreateDomains", fn: canCreateDomains, flag: "can_create_domains" },
  { name: "canCreateRoutes", fn: canCreateRoutes, flag: "can_create_routes" },
  { name: "canSend", fn: canSend, flag: "can_send" },
  { name: "canCreateAPIKeys", fn: canCreateAPIKeys, flag: "can_create_api_keys" },
] as const;

describe.each(CAPABILITIES)("$name", ({ fn, flag }) => {
  it("admin levels are allowed even when permissions are null (level check first)", () => {
    expect(fn("super_admin", null)).toBe(true);
    expect(fn("admin", null)).toBe(true);
    expect(fn("super_admin", undefined)).toBe(true);
    expect(fn("admin", undefined)).toBe(true);
  });

  it("admin levels are allowed even when the flag is explicitly false (backend synthetic perms)", () => {
    expect(fn("super_admin", perms())).toBe(true);
    expect(fn("admin", perms())).toBe(true);
  });

  it("user is allowed iff the flag is explicitly true", () => {
    expect(fn("user", perms({ [flag]: true }))).toBe(true);
    expect(fn("user", perms({ [flag]: false }))).toBe(false);
  });

  it("user is not allowed by other flags being true", () => {
    const allOthersTrue = perms({
      can_send: true,
      can_create_domains: true,
      can_create_routes: true,
      can_create_api_keys: true,
      [flag]: false,
    });
    expect(fn("user", allOthersTrue)).toBe(false);
  });

  it("user fails closed while permissions are loading or failed (null/undefined)", () => {
    expect(fn("user", null)).toBe(false);
    expect(fn("user", undefined)).toBe(false);
  });

  it("public and mailbox sessions are never allowed", () => {
    for (const level of ["public", "mailbox"] as const) {
      expect(fn(level, null)).toBe(false);
      expect(fn(level, perms({ [flag]: true }))).toBe(false);
    }
  });
});

describe("canManageTenantUsers", () => {
  it("is allowed for super_admin only", () => {
    for (const level of LEVELS) {
      expect(canManageTenantUsers(level)).toBe(level === "super_admin");
    }
  });
});

describe("level helpers", () => {
  it("isAdminLevel covers admin and super_admin", () => {
    for (const level of LEVELS) {
      expect(isAdminLevel(level)).toBe(level === "admin" || level === "super_admin");
    }
  });

  it("isSuperAdminLevel covers super_admin only", () => {
    for (const level of LEVELS) {
      expect(isSuperAdminLevel(level)).toBe(level === "super_admin");
    }
  });
});
