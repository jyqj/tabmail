import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import * as permissions from "./permissions";
import { installSession } from "../session";
import { buildPermissionEditorCommand, permissionEditorFormFromSnapshot, validatePermissionEditorSnapshot,
  type PermissionEditorSnapshot } from "./permission-editor-types";

// Real request/session serializer at a fetch boundary. No database acceptance.
const id = "20000000-0000-4000-8000-000000000001";
const tenant = "10000000-0000-4000-8000-000000000001";
const selectedProfile = "40000000-0000-4000-8000-000000000002";
const revision = { user_id: id, tenant_id: tenant, user_revision: "9007199254740993", profile_id: null, profile_revision: null };
const effective = { can_send: false, daily_send_quota: 0, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: [], can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
const snapshot: PermissionEditorSnapshot = { user_id: id, tenant_id: tenant, profile: null, overrides: null, effective,
  field_sources: { can_send: "default", daily_send_quota: "default", daily_receive_quota: "default", max_mailboxes: "default", max_domains: "default", can_create_domains: "default", can_create_routes: "default", can_create_api_keys: "default", domain_access: "default" }, revision,
  capabilities: { patch: true, assign_profile: false } };
let requests: { path: string; method: string; body: unknown }[];
const scalarFields = ["can_send", "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains", "can_create_domains", "can_create_routes", "can_create_api_keys"];
function goWireSnapshot(mode: "inherit" | "all" | "none" | "list", zones: "null" | "omitted") {
  // Preserve the actual JSON shape at the network boundary. In particular Go
  // nil slices marshal as null; do not replace these fixtures with [] first.
  return { ...structuredClone(snapshot), overrides: {
    ...Object.fromEntries(scalarFields.map(field => [field, null])), allowed_zone_ids: null,
    domain_access: { mode, ...(zones === "null" ? { zone_ids: null } : {}) },
  }, field_sources: { ...snapshot.field_sources, domain_access: mode === "inherit" ? "default" : "override" } };
}
function installWireReply(wire: unknown) {
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    requests.push({ path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return new Response(JSON.stringify({ data: wire }), { status: 200, headers: { "Content-Type": "application/json" } });
  });
}
beforeEach(() => {
  requests = [];
  installSession("serializer-token", { id: "admin", tenant_id: tenant, email: "admin@company.test", display_name: "Admin", role: "admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    requests.push({ path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
    return new Response(JSON.stringify({ data: path.endsWith("permission-editor") ? snapshot : effective }), { status: 200, headers: { "Content-Type": "application/json" } });
  });
});
afterEach(() => vi.unstubAllGlobals());

describe("permission editor API wire serializer", () => {
  it("serializes assignment and dirty overrides as one POST with both observed revisions", async () => {
    const assigned = { ...snapshot, profile: { ...effective, id: selectedProfile, revision: "9007199254740997", tenant_id: tenant,
      name: "Destination", description: "", is_system: false, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" },
      overrides: { can_send: false, daily_send_quota: 0, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
        can_create_domains: null, can_create_routes: null, can_create_api_keys: null, allowed_zone_ids: null,
        domain_access: { mode: "inherit", zone_ids: null } },
      field_sources: { ...Object.fromEntries([...scalarFields, "domain_access"].map(field => [field, "profile"])), can_send: "override", daily_send_quota: "override" },
      revision: { ...revision, user_revision: "9007199254740994", profile_id: selectedProfile, profile_revision: "9007199254740997" },
      capabilities: { patch: true, assign_profile: true } };
    installWireReply(assigned);
    const command = { expected_revision: revision, profile_id: selectedProfile, profile_revision: "9007199254740997", patch: { can_send: false, daily_send_quota: 0, max_domains: null } };
    const { data } = await permissions.assignUserPermissionEditor(id, command);
    expect(data.profile?.id).toBe(selectedProfile);
    expect(data.revision.profile_revision).toBe("9007199254740997");
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor/assignment`, method: "POST", body: command }]);
    expect((requests[0].body as typeof command).expected_revision.user_revision).toBe("9007199254740993");
    expect((requests[0].body as typeof command).profile_revision).toBe("9007199254740997");
  });
  it("an explicit null assignment pair remains null, not omitted or legacy updateUser", async () => {
    installWireReply({ ...snapshot, capabilities: { patch: true, assign_profile: true } });
    const command = { expected_revision: revision, profile_id: null, profile_revision: null, patch: {} };
    await permissions.assignUserPermissionEditor(id, command);
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor/assignment`, method: "POST", body: command }]);
  });
  it.each(["expected_revision", "profile_id", "profile_revision", "patch"])("assignment omission of %s is rejected before contacting the server", async field => {
    const command: Record<string, unknown> = { expected_revision: revision, profile_id: null, profile_revision: null, patch: {} };
    delete command[field];
    await expect(permissions.assignUserPermissionEditor(id, command as never)).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it.each([
    { profile_id: selectedProfile, profile_revision: null }, { profile_id: null, profile_revision: "5" },
    { profile_id: selectedProfile, profile_revision: "01" }, { profile_id: selectedProfile, profile_revision: 5 },
    { profile_id: selectedProfile, profile_revision: "9223372036854775808" },
  ])("assignment rejects an invalid selected profile observation %j", async observation => {
    await expect(permissions.assignUserPermissionEditor(id, { expected_revision: revision, ...observation, patch: {} } as never)).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it("assignment conflicts never retry as legacy user update, override PUT or second POST", async () => {
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({ path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
      return new Response(JSON.stringify({ error: { code: "CONFLICT", message: "Selected profile changed" } }), { status: 409 });
    });
    await expect(permissions.assignUserPermissionEditor(id, { expected_revision: revision, profile_id: selectedProfile, profile_revision: "9007199254740997", patch: { can_send: false } })).rejects.toMatchObject({ error: { code: "CONFLICT" } });
    expect(requests).toHaveLength(1);
    expect(requests[0].path).toBe(`/api/v1/admin/users/${id}/permission-editor/assignment`);
    expect(requests[0].method).toBe("POST");
  });
  const legalGoWire = (["inherit", "all", "none"] as const).flatMap(mode => (["null", "omitted"] as const).map(zones => ({ mode, zones })));
  it.each(legalGoWire)("GET accepts Go $mode raw zone_ids $zones without changing raw NULL or field sources", async ({ mode, zones }) => {
    const wire = goWireSnapshot(mode, zones);
    installWireReply(wire);
    const { data } = await permissions.getUserPermissionEditor(id);
    expect(data.overrides?.allowed_zone_ids).toBeNull();
    expect(data.field_sources).toEqual(wire.field_sources);
    expect(data.revision).toEqual(revision);
    expect(data.overrides?.domain_access.mode).toBe(mode);
    const form = permissionEditorFormFromSnapshot(data);
    expect(form.domain_access).toEqual({ mode, zone_ids: [] });
    expect(buildPermissionEditorCommand(data, form).patch).toEqual({});
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor`, method: "GET", body: undefined }]);
  });
  it.each(legalGoWire)("PATCH response accepts Go $mode raw zone_ids $zones while preserving NULL and sources", async ({ mode, zones }) => {
    const wire = goWireSnapshot(mode, zones);
    installWireReply(wire);
    const command = { expected_revision: revision, patch: { max_domains: null, domain_access: mode === "inherit" ? null : { mode, zone_ids: [] } } };
    const { data } = await permissions.patchUserPermissionEditor(id, command);
    expect(data.overrides?.allowed_zone_ids).toBeNull();
    expect(data.field_sources).toEqual(wire.field_sources);
    expect(data.revision).toEqual(revision);
    const form = permissionEditorFormFromSnapshot(data);
    expect(form.domain_access).toEqual({ mode, zone_ids: [] });
    expect(buildPermissionEditorCommand(data, form).patch).toEqual({});
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor`, method: "PATCH", body: command }]);
  });
  it.each(["null", "omitted"] as const)("list zone_ids %s remains invalid on GET and PATCH responses", async zones => {
    installWireReply(goWireSnapshot("list", zones));
    await expect(permissions.getUserPermissionEditor(id)).rejects.toThrow();
    await expect(permissions.patchUserPermissionEditor(id, { expected_revision: revision, patch: { max_domains: null } })).rejects.toThrow();
    expect(requests.map(call => call.method)).toEqual(["GET", "PATCH"]);
  });
  it.each([...scalarFields, "domain_access"])("rejects absent source %s before the form can write", async field => {
    const wire = goWireSnapshot("inherit", "null");
    delete (wire.field_sources as Record<string, unknown>)[field];
    installWireReply(wire);
    await expect(permissions.getUserPermissionEditor(id)).rejects.toThrow();
    expect(requests.every(call => call.method === "GET")).toBe(true);
  });
  it.each([{}, { can_send: false }])("rejects incomplete effective %j instead of producing undefined quotas or allow-all", async malformedEffective => {
    installWireReply({ ...snapshot, effective: malformedEffective });
    await expect(permissions.getUserPermissionEditor(id)).rejects.toThrow();
    expect(requests.map(call => call.method)).toEqual(["GET"]);
  });
  it("rejects a profile object missing identity even when sources pretend it is a valid profile", async () => {
    installWireReply({ ...snapshot, profile: {}, field_sources: Object.fromEntries([...scalarFields, "domain_access"].map(field => [field, "profile"])) });
    await expect(permissions.getUserPermissionEditor(id)).rejects.toThrow();
    expect(requests.map(call => call.method)).toEqual(["GET"]);
  });
  it.each([
    { label: "target ID mismatch", wire: { ...snapshot, user_id: tenant, revision: { ...revision, user_id: tenant } } },
    { label: "tenant revision mismatch", wire: { ...snapshot, revision: { ...revision, tenant_id: id } } },
    { label: "numeric revision", wire: { ...snapshot, revision: { ...revision, user_revision: 12 } } },
    { label: "half-present profile version", wire: { ...snapshot, revision: { ...revision, profile_revision: "4" } } },
    { label: "override source without raw value", wire: { ...snapshot, field_sources: { ...snapshot.field_sources, can_send: "override" } } },
    { label: "missing raw presence", wire: { ...snapshot, overrides: undefined } },
  ])("rejects $label at GET without reaching any PATCH", async ({ wire }) => {
    installWireReply(wire);
    await expect(permissions.getUserPermissionEditor(id)).rejects.toThrow();
    expect(requests.map(call => call.method)).toEqual(["GET"]);
  });
  it("creates a same-effective false/zero override instead of mistaking it for inherited intent", () => {
    const form = permissionEditorFormFromSnapshot(snapshot);
    expect(form.can_send).toBeNull();
    expect(form.daily_send_quota).toBe("");
    expect(buildPermissionEditorCommand(snapshot, { ...form, can_send: false, daily_send_quota: "0" })).toEqual({ expected_revision: revision, patch: { can_send: false, daily_send_quota: 0 } });
  });
  it("unchanged raw absence produces no dirty fields; never synthesizes defaults from effective", () => {
    expect(buildPermissionEditorCommand(snapshot, permissionEditorFormFromSnapshot(snapshot))).toEqual({ expected_revision: revision, patch: {} });
  });
  it.each(["all", "none", "inherit"] as const)("keeps %s separate from the other empty-zone intents", mode => {
    const base = structuredClone(snapshot);
    base.overrides = { can_send: false, daily_send_quota: 0, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
      can_create_domains: null, can_create_routes: null, can_create_api_keys: null,
      allowed_zone_ids: [tenant], domain_access: { mode: "list", zone_ids: [tenant] } };
    base.field_sources.can_send = base.field_sources.daily_send_quota = base.field_sources.domain_access = "override";
    const form = permissionEditorFormFromSnapshot(base);
    expect(buildPermissionEditorCommand(base, { ...form, domain_access: { mode, zone_ids: [] } }).patch).toEqual({ domain_access: { mode, zone_ids: [] } });
    expect(base.overrides.domain_access).toEqual({ mode: "list", zone_ids: [tenant] });
  });
  it.each(["-1", "1.5", "NaN", "Infinity", "9007199254740992"])("rejects unsafe quota %s before any write", value => {
    expect(() => buildPermissionEditorCommand(snapshot, { ...permissionEditorFormFromSnapshot(snapshot), daily_send_quota: value })).toThrow();
    expect(requests).toHaveLength(0);
  });
  it("rejects missing raw field source rather than filling it with permissive defaults", () => {
    const malformed = structuredClone(snapshot);
    delete (malformed.field_sources as Partial<typeof malformed.field_sources>).can_send;
    expect(() => validatePermissionEditorSnapshot(malformed)).toThrow();
  });
  it("keeps auth/current and legacy effective consumers on their effective response contract", async () => {
    expect((await permissions.getMyPermissions()).data).toEqual(effective);
    expect((await permissions.getUserPermission(id)).data).toEqual(effective);
    expect(requests.map(r => r.path)).toEqual(["/api/v1/auth/me/permissions", `/api/v1/admin/users/${id}/permissions`]);
  });
  it("reads a separate raw editor snapshot endpoint", async () => {
    expect((await permissions.getUserPermissionEditor(id)).data).toEqual(snapshot);
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor`, method: "GET", body: undefined }]);
  });
  it("preserves omitted/null/false/zero and large decimal revisions in PATCH JSON", async () => {
    const command = { expected_revision: revision, patch: { can_send: false, daily_send_quota: 0, max_domains: null, domain_access: { mode: "none" as const, zone_ids: [] } } };
    await permissions.patchUserPermissionEditor(id, command);
    expect(requests).toEqual([{ path: `/api/v1/admin/users/${id}/permission-editor`, method: "PATCH", body: command }]);
    const wire = requests[0].body as typeof command;
    expect(wire.patch).not.toHaveProperty("daily_receive_quota");
    expect(wire.expected_revision.user_revision).toBe("9007199254740993");
  });
  it("does not retry a rejected editor PATCH using legacy PUT/DELETE or updateUser", async () => {
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      requests.push({ path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined });
      return new Response(JSON.stringify({ error: { code: "CONFLICT", message: "stale" } }), { status: 409 });
    });
    await expect(permissions.patchUserPermissionEditor(id, { expected_revision: revision, patch: { can_send: false } })).rejects.toMatchObject({ error: { code: "CONFLICT" } });
    expect(requests).toHaveLength(1);
    expect(requests[0].method).toBe("PATCH");
    expect(requests[0].path).toBe(`/api/v1/admin/users/${id}/permission-editor`);
  });
  it.each(["0", "01", "-1", "1.5", "9223372036854775808"])("rejects malformed revision %s before contacting the server", async user_revision => {
    await expect(permissions.patchUserPermissionEditor(id, { expected_revision: { ...revision, user_revision }, patch: { can_send: false } })).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it("rejects a target mismatch and a half-present profile observation before writing", async () => {
    await expect(permissions.patchUserPermissionEditor(tenant, { expected_revision: revision, patch: { can_send: false } })).rejects.toThrow();
    await expect(permissions.patchUserPermissionEditor(id, { expected_revision: { ...revision, profile_revision: "5" }, patch: { can_send: false } })).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it.each([{ mode: "list", zone_ids: [] }, { mode: "none", zone_ids: [tenant] }, { mode: "list", zone_ids: [tenant, tenant] }])("rejects invalid domain intent %j before writing", async domain_access => {
    await expect(permissions.patchUserPermissionEditor(id, { expected_revision: revision, patch: { domain_access: domain_access as import("./permission-editor-types").DomainAccess } })).rejects.toThrow();
    expect(requests).toHaveLength(0);
  });
  it("ignores a late editor snapshot from an earlier authenticated identity", async () => {
    let resolve!: (response: Response) => void;
    const pending = new Promise<Response>(done => { resolve = done; });
    vi.stubGlobal("fetch", () => pending);
    const read = permissions.getUserPermissionEditor(id);
    // Attach rejection handling before switching sessions to avoid unhandled
    // promise reporting while the stale request is being cancelled.
    const checked = expect(read).rejects.toMatchObject({ name: "AbortError" });
    installSession("different-token", { id: "other-admin", tenant_id: tenant, email: "other@company.test", display_name: "Other", role: "admin" });
    resolve(new Response(JSON.stringify({ data: snapshot }), { status: 200 }));
    await checked;
  });
});
