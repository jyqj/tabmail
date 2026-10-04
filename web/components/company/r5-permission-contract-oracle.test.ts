import { expect, test } from "vitest";
import { assertPermissionIntent } from "./r5-permission-contract-oracle";
import type { PermissionEditorSnapshot, PermissionEditorCommand, PermissionEditorPatch } from "@/lib/api/permission-editor-types";
// Pure oracle regressions. Synthetic values are never HTTP/PG qualification.
const user = "20000000-0000-4000-8000-000000000001", tenant = "10000000-0000-4000-8000-000000000001", zone = "30000000-0000-4000-8000-000000000001";
function snapshot(): PermissionEditorSnapshot {
  return { user_id: user, tenant_id: tenant, profile: null,
    revision: { user_id: user, tenant_id: tenant, user_revision: "9007199254740993", profile_id: null, profile_revision: null },
    overrides: { can_send: false, daily_send_quota: 19, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
      can_create_domains: null, can_create_routes: null, can_create_api_keys: null,
      allowed_zone_ids: [zone], domain_access: { mode: "list", zone_ids: [zone] } },
    effective: { can_send: false, daily_send_quota: 19, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0,
      can_create_domains: false, can_create_routes: false, can_create_api_keys: false, allowed_zone_ids: [zone] },
    field_sources: { can_send: "override", daily_send_quota: "override", daily_receive_quota: "default", max_mailboxes: "default", max_domains: "default",
      can_create_domains: "default", can_create_routes: "default", can_create_api_keys: "default", domain_access: "override" },
    capabilities: { patch: true, assign_profile: false } };
}
function observations(variant: string) {
  const before = snapshot(), after = snapshot();
  after.revision.user_revision = "9007199254740994";
  let patch: PermissionEditorPatch;
  if (variant === "false") {
    before.overrides!.can_send = true; before.effective.can_send = true; patch = { can_send: false };
  } else if (variant === "null") {
    patch = { can_send: null }; after.overrides!.can_send = null; after.field_sources.can_send = "default";
  } else if (variant === "[]") {
    patch = { domain_access: { mode: "all", zone_ids: [] } };
    after.overrides!.domain_access = { mode: "all", zone_ids: null }; after.overrides!.allowed_zone_ids = null; after.effective.allowed_zone_ids = null;
  } else {
    const value = variant === "0" ? 0 : 25; patch = { daily_send_quota: value };
    after.overrides!.daily_send_quota = value; after.effective.daily_send_quota = value;
  }
  const command: PermissionEditorCommand = { expected_revision: { ...before.revision }, patch };
  return { before, after, command };
}
test.each(["omitted", "null", "false", "0", "[]"])("accepts distinct raw %s intent and exact changed-only patch", variant => {
  const { before, after, command } = observations(variant);
  expect(() => assertPermissionIntent("PE02", variant, before, command, after)).not.toThrow();
});
test("rejects raw loss even when effective permission is unchanged", () => {
  const { before, after, command } = observations("omitted");
  after.overrides!.can_send = null; after.field_sources.can_send = "default";
  expect(() => assertPermissionIntent("PE01", "default", before, command, after)).toThrow("R5_PROTOCOL_UI_TARGET_PE01_OMITTED");
});
test("rejects null omission, redundant security fields and legacy zone spelling", () => {
  for (const variant of ["null", "omitted", "[]"]) {
    const { before, after, command } = observations(variant);
    command.patch = variant === "null" ? { daily_send_quota: 25 } : variant === "omitted" ? { daily_send_quota: 25, can_send: false } : { allowed_zone_ids: [] } as never;
    expect(() => assertPermissionIntent("PE02", variant, before, command, after)).toThrow();
  }
});
test("rejects revision reuse and wrong compound observation", () => {
  const { before, after, command } = observations("0");
  after.revision.user_revision = before.revision.user_revision;
  expect(() => assertPermissionIntent("PE02", "0", before, command, after)).toThrow("revision");
  after.revision.user_revision = "9007199254740994"; command.expected_revision.user_revision = "9007199254740992";
  expect(() => assertPermissionIntent("PE02", "0", before, command, after)).toThrow("compound");
});
