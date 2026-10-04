import { isDeepStrictEqual } from "node:util";
import { permissionBooleanFields, permissionNumberFields, validatePermissionEditorSnapshot,
  validatePermissionEditorCommand, type PermissionEditorSnapshot, type PermissionEditorCommand,
  type PermissionEditorPatch } from "@/lib/api/permission-editor-types";

// These assertions inspect actual raw HTTP snapshots. They never infer raw
// overrides from effective values, and never publish fixture response bodies.
export const resetPermissionPatch: PermissionEditorPatch = {
  can_send: null, can_create_domains: null, can_create_routes: null, can_create_api_keys: null,
  daily_send_quota: null, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
  domain_access: { mode: "inherit", zone_ids: [] },
};
export function assertPermissionIntent(id: string, variant: string, before: PermissionEditorSnapshot,
  command: PermissionEditorCommand, after: PermissionEditorSnapshot): void {
  validatePermissionEditorSnapshot(before);
  validatePermissionEditorSnapshot(after, before.user_id);
  validatePermissionEditorCommand(command, before.user_id);
  const marker = id === "PE01" ? "R5_PROTOCOL_UI_TARGET_PE01_OMITTED" :
    variant === "null" ? "R5_PROTOCOL_UI_TARGET_PE02_NULL" : "R5_PROTOCOL_UI_TARGET_PE02_OMITTED";
  const check = (condition: boolean, detail: string) => { if (!condition) throw new Error(`${marker}: ${detail}`); };
  const expected: PermissionEditorPatch = id === "PE01" || variant === "omitted" ? { daily_send_quota: 25 } :
    variant === "null" ? { can_send: null } : variant === "false" ? { can_send: false } :
    variant === "0" ? { daily_send_quota: 0 } : variant === "[]" ? { domain_access: { mode: "all", zone_ids: [] } } : {};
  check(Object.keys(expected).length > 0 && isDeepStrictEqual(command.patch, expected), "changed-only explicit wire intent differs");
  check(isDeepStrictEqual(command.expected_revision, before.revision), "compound observed revision differs");
  check(after.tenant_id === before.tenant_id && after.revision.profile_id === before.revision.profile_id &&
    after.revision.profile_revision === before.revision.profile_revision, "target/profile binding changed");
  check(BigInt(after.revision.user_revision) > BigInt(before.revision.user_revision), "successful command did not advance user revision");
  for (const field of [...permissionBooleanFields, ...permissionNumberFields]) {
    const value = Object.hasOwn(expected, field) ? expected[field] : before.overrides?.[field] ?? null;
    check((after.overrides?.[field] ?? null) === value, "persisted raw scalar differs or omitted field lost");
  }
  if (variant === "[]") {
    check(after.overrides?.domain_access.mode === "all" && (after.overrides.domain_access.zone_ids ?? []).length === 0 &&
      (after.overrides.allowed_zone_ids ?? []).length === 0 && after.field_sources.domain_access === "override", "explicit empty all scope differs");
  } else {
    check(isDeepStrictEqual(after.overrides?.domain_access, before.overrides?.domain_access) &&
      isDeepStrictEqual(after.overrides?.allowed_zone_ids, before.overrides?.allowed_zone_ids), "omitted raw domain restriction lost");
  }
}
