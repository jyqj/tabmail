import type { EffectivePermission, PermissionProfile } from "../types";

export const permissionBooleanFields = [
  "can_send", "can_create_domains", "can_create_routes", "can_create_api_keys",
] as const;
export const permissionNumberFields = [
  "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains",
] as const;
export type PermissionBooleanField = typeof permissionBooleanFields[number];
export type PermissionNumberField = typeof permissionNumberFields[number];
export type PermissionField = PermissionBooleanField | PermissionNumberField | "domain_access";
export type PermissionSource = "override" | "profile" | "default";
export interface DomainAccess {
  mode: "inherit" | "all" | "list" | "none";
  zone_ids: string[];
}
export type DomainAccessWire =
  | { mode: "list"; zone_ids: string[] }
  | { mode: "inherit" | "all" | "none"; zone_ids?: string[] | null };
export type EditorPermissionProfile = PermissionProfile & { revision?: string };
export type EditorEffectivePermission = EffectivePermission & { domain_access_mode?: "" | "all" | "list" | "none" };
export interface PermissionEditorRevision {
  user_id: string;
  tenant_id: string;
  user_revision: string;
  profile_id: string | null;
  profile_revision: string | null;
}
export type RawPermissionOverrides = {
  [K in PermissionBooleanField]: boolean | null;
} & {
  [K in PermissionNumberField]: number | null;
} & {
  allowed_zone_ids: string[] | null;
  domain_access: DomainAccessWire;
};
export interface PermissionEditorSnapshot {
  user_id: string;
  tenant_id: string;
  profile: EditorPermissionProfile | null;
  overrides: RawPermissionOverrides | null;
  effective: EditorEffectivePermission;
  field_sources: Record<PermissionField, PermissionSource>;
  revision: PermissionEditorRevision;
  capabilities: { patch: boolean; assign_profile: boolean };
}
export type PermissionEditorPatch = Partial<{
  [K in PermissionBooleanField]: boolean | null;
} & {
  [K in PermissionNumberField]: number | null;
} & { domain_access: DomainAccessWire | null }>;
export interface PermissionEditorCommand {
  expected_revision: PermissionEditorRevision;
  patch: PermissionEditorPatch;
}
export type PermissionEditorForm = {
  [K in PermissionBooleanField]: boolean | null;
} & {
  [K in PermissionNumberField]: string;
} & { domain_access: DomainAccess };

const uuid = /^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$/i;
function isUUID(value: unknown): value is string {
  return typeof value === "string" && uuid.test(value) && value !== "00000000-0000-0000-0000-000000000000";
}
function decimalRevision(value: unknown): value is string {
  return typeof value === "string" && value.length <= 19 && /^[1-9][0-9]*$/.test(value) && BigInt(value) <= BigInt("9223372036854775807");
}
function quota(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}
export function validatePermissionRevision(revision: PermissionEditorRevision, target?: string): void {
  if (!revision || !isUUID(revision.user_id) || !isUUID(revision.tenant_id) ||
      (target !== undefined && revision.user_id !== target) || !decimalRevision(revision.user_revision) ||
      (revision.profile_id === null ? revision.profile_revision !== null :
        !isUUID(revision.profile_id) || !decimalRevision(revision.profile_revision))) {
    throw new Error("Invalid permission revision or target identity");
  }
}
function normalizedDomainAccess(domain: DomainAccessWire): DomainAccess {
  if (!domain || typeof domain !== "object" || !["inherit", "all", "list", "none"].includes(domain.mode) ||
      Object.keys(domain).some(key => !["mode", "zone_ids"].includes(key))) {
    throw new Error("Invalid explicit domain access");
  }
  // Go's nil UUID slice legitimately marshals to NULL. Omitted non-list IDs
  // are also legal in the value object; neither spelling can imply list/all.
  const ids = domain.zone_ids;
  if (domain.mode === "list" ? !Array.isArray(ids) || ids.length === 0 : ids != null && (!Array.isArray(ids) || ids.length !== 0)) {
    throw new Error("Invalid explicit domain access IDs");
  }
  const zone_ids = ids ?? [];
  if (zone_ids.some(id => !isUUID(id)) || new Set(zone_ids).size !== zone_ids.length) throw new Error("Invalid explicit domain access IDs");
  return { mode: domain.mode, zone_ids: [...zone_ids] };
}
export function validateDomainAccess(domain: DomainAccessWire): void {
  normalizedDomainAccess(domain);
}
function validatePermissionValues(permission: EffectivePermission): void {
  if (!permission || typeof permission !== "object") throw new Error("Missing effective permission fields");
  for (const field of permissionBooleanFields) {
    if (!Object.hasOwn(permission, field) || typeof permission[field] !== "boolean") throw new Error("Invalid mandatory effective boolean");
  }
  for (const field of permissionNumberFields) {
    if (!Object.hasOwn(permission, field) || !quota(permission[field])) throw new Error("Invalid mandatory effective quota");
  }
  if (permission.allowed_zone_ids != null && (!Array.isArray(permission.allowed_zone_ids) || permission.allowed_zone_ids.some(id => !isUUID(id)))) throw new Error("Invalid effective zone IDs");
}
export function validatePermissionEditorSnapshot(snapshot: PermissionEditorSnapshot, target?: string): void {
  if (!snapshot || !Object.hasOwn(snapshot, "profile") || !isUUID(snapshot.user_id) || !isUUID(snapshot.tenant_id) || !snapshot.revision ||
      snapshot.revision.user_id !== snapshot.user_id || snapshot.revision.tenant_id !== snapshot.tenant_id ||
      (snapshot.profile?.id ?? null) !== snapshot.revision.profile_id || !snapshot.effective ||
      typeof snapshot.capabilities?.patch !== "boolean" || typeof snapshot.capabilities?.assign_profile !== "boolean") {
    throw new Error("Invalid permission editor snapshot");
  }
  validatePermissionRevision(snapshot.revision, target);
  validatePermissionValues(snapshot.effective);
  const effectiveMode = snapshot.effective.domain_access_mode;
  if (effectiveMode !== undefined && !["", "all", "list", "none"].includes(effectiveMode)) throw new Error("Unknown canonical effective domain access");
  if (effectiveMode === "list" && !snapshot.effective.allowed_zone_ids?.length) throw new Error("Empty effective list cannot imply all");
  if (snapshot.profile !== null) {
    const profile = snapshot.profile;
    if (!profile || !isUUID(profile.id) || (profile.tenant_id != null && profile.tenant_id !== snapshot.tenant_id) ||
        typeof profile.name !== "string" || typeof profile.description !== "string" || typeof profile.is_system !== "boolean" ||
        typeof profile.created_at !== "string" || typeof profile.updated_at !== "string" ||
        (profile.revision !== undefined && profile.revision !== snapshot.revision.profile_revision)) throw new Error("Invalid bound permission profile");
    validatePermissionValues(profile);
  }
  const raw = snapshot.overrides;
  if (raw !== null) {
    if (!raw || !Object.hasOwn(raw, "allowed_zone_ids") ||
        (raw.allowed_zone_ids !== null && (!Array.isArray(raw.allowed_zone_ids) || raw.allowed_zone_ids.some(id => !isUUID(id))))) {
      throw new Error("Missing raw permission override presence");
    }
    validateDomainAccess(raw.domain_access);
  }
  for (const field of [...permissionBooleanFields, ...permissionNumberFields]) {
    const value = raw === null ? null : raw[field];
    if (raw !== null && (!Object.hasOwn(raw, field) ||
        (value !== null && (permissionBooleanFields.includes(field as PermissionBooleanField) ? typeof value !== "boolean" : !quota(value))))) {
      throw new Error("Invalid raw permission scalar");
    }
    const source: PermissionSource = value !== null ? "override" : snapshot.profile ? "profile" : "default";
    if (snapshot.field_sources?.[field] !== source) throw new Error("Inconsistent raw permission source");
  }
  const domainSource: PermissionSource = raw && raw.domain_access.mode !== "inherit" ? "override" : snapshot.profile ? "profile" : "default";
  if (snapshot.field_sources?.domain_access !== domainSource) throw new Error("Inconsistent domain access source");
}
export function permissionEditorFormFromSnapshot(snapshot: PermissionEditorSnapshot): PermissionEditorForm {
  validatePermissionEditorSnapshot(snapshot);
  const raw = snapshot.overrides;
  return {
    can_send: raw?.can_send ?? null,
    can_create_domains: raw?.can_create_domains ?? null,
    can_create_routes: raw?.can_create_routes ?? null,
    can_create_api_keys: raw?.can_create_api_keys ?? null,
    daily_send_quota: raw?.daily_send_quota == null ? "" : String(raw.daily_send_quota),
    daily_receive_quota: raw?.daily_receive_quota == null ? "" : String(raw.daily_receive_quota),
    max_mailboxes: raw?.max_mailboxes == null ? "" : String(raw.max_mailboxes),
    max_domains: raw?.max_domains == null ? "" : String(raw.max_domains),
    domain_access: raw ? normalizedDomainAccess(raw.domain_access) : { mode: "inherit", zone_ids: [] },
  };
}
function numericIntent(value: string): number | null {
  if (value.trim() === "") return null;
  const number = Number(value);
  if (!quota(number)) throw new Error("Quota must be a nonnegative safe integer");
  return number;
}
export function buildPermissionEditorCommand(snapshot: PermissionEditorSnapshot, form: PermissionEditorForm): PermissionEditorCommand {
  validatePermissionEditorSnapshot(snapshot);
  const original = permissionEditorFormFromSnapshot(snapshot);
  const patch: PermissionEditorPatch = {};
  for (const field of permissionBooleanFields) {
    if (form[field] !== null && typeof form[field] !== "boolean") throw new Error("Invalid boolean intent");
    if (form[field] !== original[field]) patch[field] = form[field];
  }
  for (const field of permissionNumberFields) {
    const value = numericIntent(form[field]);
    if (value !== numericIntent(original[field])) patch[field] = value;
  }
  validateDomainAccess(form.domain_access);
  // UUID sets, not selection order, define the list intent. None remains its
  // own mode and is never serialized as the legacy permissive empty array.
  if (form.domain_access.mode !== original.domain_access.mode ||
      [...form.domain_access.zone_ids].sort().join(",") !== [...original.domain_access.zone_ids].sort().join(",")) {
    patch.domain_access = { ...form.domain_access, zone_ids: [...form.domain_access.zone_ids] };
  }
  return { expected_revision: { ...snapshot.revision }, patch };
}
export function validatePermissionEditorCommand(command: PermissionEditorCommand, target: string): void {
  validatePermissionRevision(command.expected_revision, target);
  if (!command.patch || Object.keys(command.patch).length === 0) throw new Error("An explicit changed permission field is required");
  for (const [field, value] of Object.entries(command.patch)) {
    if (permissionBooleanFields.includes(field as PermissionBooleanField)) {
      if (value !== null && typeof value !== "boolean") throw new Error("Invalid boolean permission intent");
    } else if (permissionNumberFields.includes(field as PermissionNumberField)) {
      if (value !== null && !quota(value)) throw new Error("Invalid quota permission intent");
    } else if (field === "domain_access") {
      if (value !== null) validateDomainAccess(value as DomainAccessWire);
    } else throw new Error("Unknown permission patch field");
  }
}

export interface ObservedPermissionProfile extends PermissionProfile { revision: string }
export type PermissionProfileUpdateFields = Partial<{
  name: string | null; description: string | null;
  can_send: boolean | null; daily_send_quota: number | null;
  daily_receive_quota: number | null; max_mailboxes: number | null;
  max_domains: number | null; allowed_zone_ids: string[] | null;
  can_create_domains: boolean | null; can_create_routes: boolean | null;
  can_create_api_keys: boolean | null;
}>;
export interface PermissionProfileUpdateCommand { expected_revision: string; fields: PermissionProfileUpdateFields }
export interface PermissionAssignmentCommand extends PermissionEditorCommand { profile_id: string | null; profile_revision: string | null }
export interface PermissionProfileDeletionPreview {
  profile_id: string; profile_revision: string;
  members: PermissionEditorRevision[];
  changes: { revision: PermissionEditorRevision; before: EditorEffectivePermission; after: EditorEffectivePermission }[];
}
export interface PermissionProfileDeletionCommand { expected_revision: string; confirmed_members: PermissionEditorRevision[] }
export function validateObservedPermissionProfile(profile: EditorPermissionProfile): asserts profile is ObservedPermissionProfile {
  if (!profile || !isUUID(profile.id) || !decimalRevision(profile.revision) ||
      (profile.tenant_id != null && !isUUID(profile.tenant_id)) || typeof profile.name !== "string" ||
      typeof profile.description !== "string" || typeof profile.is_system !== "boolean" ||
      typeof profile.created_at !== "string" || typeof profile.updated_at !== "string") throw new Error("A complete observed profile version is required");
  validatePermissionValues(profile);
}
export function validateProfileRevision(value: string): void {
  if (!decimalRevision(value)) throw new Error("An observed decimal profile revision is required");
}
export function validateAssignmentCommand(command: PermissionAssignmentCommand, target: string): void {
  validatePermissionRevision(command.expected_revision, target);
  if (!Object.hasOwn(command, "profile_id") || !Object.hasOwn(command, "profile_revision") || !Object.hasOwn(command, "patch") ||
      (command.profile_id === null ? command.profile_revision !== null : !isUUID(command.profile_id) || !decimalRevision(command.profile_revision))) throw new Error("Explicit observed destination profile required");
  // Assignment-only still explicitly carries an empty patch, preserving all
  // existing overrides. Reuse the strict field validator when intent exists.
  if (!command.patch || typeof command.patch !== "object" || Array.isArray(command.patch)) throw new Error("Explicit permission patch required");
  if (Object.keys(command.patch).length) validatePermissionEditorCommand(command, target);
}
export function validateProfileUpdateCommand(command: PermissionProfileUpdateCommand): void {
  validateProfileRevision(command.expected_revision);
  if (!command.fields || !Object.keys(command.fields).length) throw new Error("Profile field intent required");
  for (const [field, value] of Object.entries(command.fields)) {
    if (field === "name" || field === "description") { if (value !== null && typeof value !== "string") throw new Error("Invalid profile text intent"); }
    else if (field === "allowed_zone_ids") { if (value !== null && (!Array.isArray(value) || value.some(id => !isUUID(id)))) throw new Error("Invalid profile zone intent"); }
    else if (permissionBooleanFields.includes(field as PermissionBooleanField)) { if (value !== null && typeof value !== "boolean") throw new Error("Invalid profile boolean intent"); }
    else if (permissionNumberFields.includes(field as PermissionNumberField)) { if (value !== null && !quota(value)) throw new Error("Invalid profile quota intent"); }
    else throw new Error("Unknown profile field intent");
  }
}
export function validateProfileDeletionPreview(preview: PermissionProfileDeletionPreview, target: string): PermissionProfileDeletionPreview {
  if (!preview || preview.profile_id !== target || !isUUID(preview.profile_id) || !decimalRevision(preview.profile_revision) ||
      !Object.hasOwn(preview, "members") || !Object.hasOwn(preview, "changes")) throw new Error("Invalid profile deletion observation");
  const members = preview.members ?? [], changes = preview.changes ?? [];
  if (!Array.isArray(members) || !Array.isArray(changes) || members.length !== changes.length) throw new Error("Incomplete profile deletion impact");
  const seen = new Set<string>();
  for (const member of members) {
    validatePermissionRevision(member);
    if (member.profile_id !== target || member.profile_revision !== preview.profile_revision || seen.has(member.user_id)) throw new Error("Invalid confirmed member profile binding");
    seen.add(member.user_id);
  }
  const changed = new Set<string>();
  for (const change of changes) {
    validatePermissionRevision(change.revision);
    const member = members.find(item => item.user_id === change.revision.user_id);
    if (!member || ["user_id", "tenant_id", "user_revision", "profile_id", "profile_revision"].some(key => member[key as keyof PermissionEditorRevision] !== change.revision[key as keyof PermissionEditorRevision]) || changed.has(member.user_id)) throw new Error("Missing or mismatched affected member");
    changed.add(member.user_id); validatePermissionValues(change.before); validatePermissionValues(change.after);
    for (const value of [change.before, change.after]) if (value.domain_access_mode !== undefined && !["", "all", "list", "none"].includes(value.domain_access_mode)) throw new Error("Unknown deletion domain policy");
  }
  return { ...preview, members, changes };
}
export function validateProfileDeletionCommand(command: PermissionProfileDeletionCommand, target: string): void {
  validateProfileRevision(command.expected_revision);
  if (!Array.isArray(command.confirmed_members)) throw new Error("Explicit confirmed member observations required");
  const seen = new Set<string>();
  for (const member of command.confirmed_members) {
    validatePermissionRevision(member);
    if (member.profile_id !== target || member.profile_revision !== command.expected_revision || seen.has(member.user_id)) throw new Error("Invalid profile deletion member binding");
    seen.add(member.user_id);
  }
}
