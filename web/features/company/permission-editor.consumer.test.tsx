import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import UsersPage from "./user-management";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";

// Consumer evidence only: real page, Base UI, API serializer, session and SWR;
// HTTP replies are fixtures, not PostgreSQL or shipping-browser acceptance.
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ level: "admin", tenantId: tenant }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const tenant = "10000000-0000-4000-8000-000000000001";
const alice = "20000000-0000-4000-8000-000000000001";
const bob = "20000000-0000-4000-8000-000000000002";
const zone = "30000000-0000-4000-8000-000000000001";
const profile = "40000000-0000-4000-8000-000000000001";
const assignmentProfile = "40000000-0000-4000-8000-000000000002";
const effective = { can_send: false, daily_send_quota: 99, daily_receive_quota: 100,
  max_mailboxes: 2, max_domains: 3, allowed_zone_ids: [zone],
  can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
function snapshot(id = alice, revision = "9007199254740993") {
  return { user_id: id, tenant_id: tenant, profile: { ...effective, id: profile, revision: "9007199254740995", name: "Restricted", description: "",
    tenant_id: tenant, is_system: false, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" },
    overrides: { can_send: false, daily_send_quota: 0 as number | null, daily_receive_quota: null,
      max_mailboxes: null, max_domains: null, allowed_zone_ids: [] as string[], can_create_domains: null,
      can_create_routes: null, can_create_api_keys: null, domain_access: { mode: "none", zone_ids: [] as string[] } },
    effective: { ...effective, daily_send_quota: 0, allowed_zone_ids: [] }, field_sources: { can_send: "override", daily_send_quota: "override", daily_receive_quota: "profile", max_mailboxes: "profile", max_domains: "profile", can_create_domains: "profile", can_create_routes: "profile", can_create_api_keys: "profile", domain_access: "override" },
    revision: { user_id: id, tenant_id: tenant, user_revision: revision, profile_id: profile, profile_revision: "9007199254740995" },
    capabilities: { patch: true, assign_profile: false } };
}
type Snapshot = ReturnType<typeof snapshot>;
function inheritedGoWireSnapshot() {
  const base = snapshot();
  return { ...base, effective: { ...effective }, overrides: {
    can_send: null, daily_send_quota: null, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
    can_create_domains: null, can_create_routes: null, can_create_api_keys: null,
    allowed_zone_ids: null, domain_access: { mode: "inherit", zone_ids: null },
  }, field_sources: Object.fromEntries(Object.keys(base.field_sources).map(field => [field, "profile"])) };
}
type Call = { path: string; method: string; body: unknown };
let current: Snapshot;
let availableProfiles: Snapshot["profile"][];
let calls: Call[];
let editorReply: (id: string, call: Call) => Promise<Response>;
const reply = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), { status, headers: { "Content-Type": "application/json" } });
const writes = () => calls.filter(c => c.method !== "GET");

beforeEach(() => {
  current = snapshot(); availableProfiles = [current.profile]; calls = [];
  editorReply = async (_id, call) => {
    if (call.method === "PATCH") current = { ...current, revision: { ...current.revision, user_revision: "9007199254740994" } };
    return reply(current);
  };
  installSession("consumer-token", { id: "admin", tenant_id: tenant, email: "admin@company.test", display_name: "Admin", role: "admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const call = { path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined };
    calls.push(call);
    const match = path.match(/^\/api\/v1\/admin\/users\/([^/]+)\/permission-editor(?:\/assignment)?$/);
    if (match) return editorReply(match[1], call);
    if (path === "/api/v1/admin/users") return reply([alice, bob].map((id, i) => ({ id, tenant_id: tenant, email: `${i ? "bob" : "alice"}@company.test`, display_name: i ? "Bob" : "Alice", role: "user", is_active: true, permission_profile_id: profile, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" })));
    if (path === "/api/v1/admin/permissions") return reply(availableProfiles);
    if (path === "/api/v1/domains") return reply([{ id: zone, tenant_id: tenant, domain: "company.test" }]);
    // Legacy effective GET stays available for the compatibility assertion.
    if (/\/permissions$/.test(path) && call.method === "GET") return reply(effective);
    throw new Error(`Unexpected consumer request: ${call.method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

async function openEditor(email = "alice@company.test") {
  const cell = await screen.findByText(email);
  const trigger = cell.closest("tr")?.querySelector<HTMLButtonElement>('[data-slot="dropdown-menu-trigger"]');
  expect(trigger).toBeTruthy();
  await userEvent.click(trigger!);
  await userEvent.click(await screen.findByRole("menuitem", { name: "Permissions" }));
  return screen.findByRole("dialog");
}
function mount() { return render(<SidebarProvider><UsersPage /></SidebarProvider>); }
async function loaded() {
  const dialog = await openEditor();
  await waitFor(() => expect(calls.some(c => c.path.endsWith("/permission-editor"))).toBe(true));
  await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(0));
  return dialog;
}
const save = (dialog: HTMLElement) => within(dialog).getByRole("button", { name: /^Save Overrides$/ });
function enableAssignment() {
  current.capabilities.assign_profile = true;
  availableProfiles.push({ ...current.profile, id: assignmentProfile, name: "Destination", revision: "9007199254740997" });
}
async function chooseProfile(dialog: HTMLElement, name: string) {
  const selector = within(dialog).getByRole("combobox");
  expect(selector).toBeEnabled();
  await userEvent.click(selector);
  await userEvent.click(await screen.findByRole("option", { name }));
}

describe("permission editor real consumer", () => {
  it("formal assignment capability enables a versioned profile selector without immediately mutating permissions", async () => {
    enableAssignment();
    mount(); const dialog = await loaded();
    await chooseProfile(dialog, "Destination");
    expect(writes()).toHaveLength(0);
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(0);
  });

  it("assignment submits the observed target profile revision and dirty overrides in one atomic POST", async () => {
    enableAssignment();
    const assigned = snapshot(alice, "9007199254740994");
    assigned.profile = { ...assigned.profile, id: assignmentProfile, name: "Destination", revision: "9007199254740997" };
    assigned.revision = { ...assigned.revision, profile_id: assignmentProfile, profile_revision: "9007199254740997" };
    assigned.capabilities.assign_profile = true;
    assigned.overrides.daily_send_quota = assigned.effective.daily_send_quota = 25;
    editorReply = async (_id, call) => reply(call.method === "POST" ? assigned : current);
    mount(); const dialog = await loaded();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    await chooseProfile(dialog, "Destination");
    expect(writes()).toHaveLength(0);
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toEqual({ path: `/api/v1/admin/users/${alice}/permission-editor/assignment`, method: "POST",
      body: { expected_revision: snapshot().revision, profile_id: assignmentProfile, profile_revision: "9007199254740997", patch: { daily_send_quota: 25 } } });
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(within(dialog).getByRole("combobox")).toHaveTextContent("Destination");
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(25);
  });

  it("unassignment sends an explicit NULL profile identity/version pair with an empty override patch", async () => {
    enableAssignment();
    const unassigned = { ...snapshot(alice, "9007199254740994"), profile: null,
      revision: { ...snapshot(alice, "9007199254740994").revision, profile_id: null, profile_revision: null },
      field_sources: { ...snapshot().field_sources, daily_receive_quota: "default", max_mailboxes: "default", max_domains: "default",
        can_create_domains: "default", can_create_routes: "default", can_create_api_keys: "default" },
      capabilities: { patch: true, assign_profile: true } };
    editorReply = async (_id, call) => reply(call.method === "POST" ? unassigned : current);
    mount(); const dialog = await loaded();
    await chooseProfile(dialog, "Default");
    expect(writes()).toHaveLength(0);
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toEqual({ path: `/api/v1/admin/users/${alice}/permission-editor/assignment`, method: "POST",
      body: { expected_revision: snapshot().revision, profile_id: null, profile_revision: null, patch: {} } });
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(within(dialog).getByRole("combobox")).toHaveTextContent("Default");
  });

  it("assignment conflict preserves the staged profile and dirty quota but prohibits automatic replays", async () => {
    enableAssignment();
    let remoteProfileRevision = "9007199254740997";
    editorReply = async (_id, call) => {
      const body = call.body as { profile_revision?: string } | undefined;
      return call.method === "POST" && body?.profile_revision !== remoteProfileRevision
        ? reply({ error: { code: "CONFLICT", message: "Observed profile revision changed (ABA)" } }, 409)
        : reply(current);
    };
    mount(); const dialog = await loaded();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    await chooseProfile(dialog, "Destination");
    // Another administrator changes the selected profile after observation.
    // Never swap the selected observation for a newer version automatically.
    remoteProfileRevision = "9007199254740999";
    availableProfiles = availableProfiles.map(item => item.id === assignmentProfile ? { ...item, revision: remoteProfileRevision } : item);
    await userEvent.click(save(dialog));
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(25);
    expect(within(dialog).getByRole("combobox")).toHaveTextContent("Destination");
    expect(writes()).toHaveLength(1);
    expect(writes()[0].method).toBe("POST");
    expect(writes()[0].body).toEqual({ expected_revision: snapshot().revision, profile_id: assignmentProfile, profile_revision: "9007199254740997", patch: { daily_send_quota: 25 } });
    await userEvent.click(within(dialog).getByRole("button", { name: /^Reload$/ }));
    await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(0));
    expect(within(dialog).getByRole("combobox")).toHaveTextContent("Restricted");
    expect(save(dialog)).toBeDisabled();
    expect(writes()).toHaveLength(1);
    // An explicit reload/reselect is a fresh observation, not a stale replay.
    await chooseProfile(dialog, "Destination");
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(2));
    expect(writes()[1].body).toEqual({ expected_revision: snapshot().revision, profile_id: assignmentProfile, profile_revision: remoteProfileRevision, patch: {} });
  });

  it("an old assignment response cannot overwrite a newly opened user's profile or overrides", async () => {
    enableAssignment();
    let finishAlice!: (response: Response) => void;
    const pendingAlice = new Promise<Response>(resolve => { finishAlice = resolve; });
    const bobSnapshot = snapshot(bob, "22");
    bobSnapshot.capabilities.assign_profile = true;
    bobSnapshot.overrides.daily_send_quota = 7;
    editorReply = async (id, call) => call.method === "POST" ? pendingAlice : reply(id === bob ? bobSnapshot : current);
    mount(); const aliceDialog = await loaded();
    await chooseProfile(aliceDialog, "Destination");
    fireEvent.change(within(aliceDialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    await userEvent.click(save(aliceDialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const bobDialog = await openEditor("bob@company.test");
    await waitFor(() => expect(within(bobDialog).getAllByRole("spinbutton")[0]).toHaveValue(7));
    const lateAlice = snapshot();
    lateAlice.profile = { ...lateAlice.profile, id: assignmentProfile, name: "Destination", revision: "9007199254740997" };
    lateAlice.revision = { ...lateAlice.revision, profile_id: assignmentProfile, profile_revision: "9007199254740997", user_revision: "9007199254740994" };
    lateAlice.overrides.daily_send_quota = 25;
    await act(async () => { finishAlice(reply(lateAlice)); await pendingAlice; });
    expect(within(bobDialog).getAllByRole("spinbutton")[0]).toHaveValue(7);
    expect(within(bobDialog).getByRole("combobox")).toHaveTextContent("Restricted");
    expect(writes()).toHaveLength(1);
    fireEvent.change(within(bobDialog).getAllByRole("spinbutton")[0], { target: { value: "8" } });
    await userEvent.click(save(bobDialog));
    await waitFor(() => expect(writes()).toHaveLength(2));
    expect(writes()[1]).toEqual({ path: `/api/v1/admin/users/${bob}/permission-editor`, method: "PATCH", body: { expected_revision: bobSnapshot.revision, patch: { daily_send_quota: 8 } } });
  });

  it("loads the real Go inherited raw NULL slice instead of staying disabled behind a validation error", async () => {
    const wire = inheritedGoWireSnapshot();
    editorReply = async () => reply(wire);
    mount(); const dialog = await openEditor();
    await waitFor(() => expect(within(dialog).getByRole("button", { name: /^Reset Overrides$/ })).toBeEnabled());
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(null);
    expect(within(dialog).getAllByRole("spinbutton")[1]).toHaveValue(null);
    expect(within(dialog).getAllByRole("switch")[0]).not.toBeChecked();
    expect(save(dialog)).toBeDisabled();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toEqual({ expected_revision: wire.revision, patch: { daily_send_quota: 25 } });
  });

  it("accepts a clear/reset response containing Go inherited zone_ids NULL and can continue editing", async () => {
    const wire = inheritedGoWireSnapshot();
    editorReply = async (_id, call) => reply(call.method === "PATCH" ? wire : current);
    mount(); const dialog = await loaded();
    await userEvent.click(within(dialog).getByRole("button", { name: /^Reset Overrides$/ }));
    await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(null));
    expect(save(dialog)).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: /^Reset Overrides$/ })).toBeEnabled();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    expect(save(dialog)).toBeEnabled();
    expect(writes()).toHaveLength(1);
    expect(writes()[0].method).toBe("PATCH");
  });

  it.each([{}, { can_send: false }])("incomplete effective %j never enables a consumer write", async malformedEffective => {
    editorReply = async () => reply({ ...current, effective: malformedEffective });
    mount(); const dialog = await openEditor();
    await waitFor(() => expect(calls.some(call => call.path.endsWith("/permission-editor"))).toBe(true));
    // Poll until the read is settled, not just until fetch was called: Reload
    // remains disabled while loading and becomes enabled after completion.
    await waitFor(() => expect(within(dialog).getByRole("button", { name: /^Reload$/ })).toBeEnabled());
    expect(save(dialog)).toBeDisabled();
    expect(within(dialog).getByRole("button", { name: /^Reset Overrides$/ })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });

  it("loads raw false/zero/null separately from effective and patches only the dirty quota", async () => {
    mount(); const dialog = await loaded();
    const fields = within(dialog).getAllByRole("spinbutton");
    expect(fields[1]).toHaveValue(null);
    expect(within(dialog).getAllByRole("switch")[0]).not.toBeChecked();
    expect(within(dialog).getByRole("combobox")).toBeDisabled();
    fireEvent.change(fields[0], { target: { value: "25" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toEqual({ path: `/api/v1/admin/users/${alice}/permission-editor`, method: "PATCH", body: { expected_revision: snapshot().revision, patch: { daily_send_quota: 25 } } });
  });

  it("clearing an existing numeric override sends explicit null rather than omission", async () => {
    mount(); const dialog = await loaded();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toEqual({ expected_revision: snapshot().revision, patch: { daily_send_quota: null } });
  });

  it("setting an inherited quota to zero sends explicit zero, even when unlimited is the effective value", async () => {
    current.overrides.daily_send_quota = null;
    current.effective.daily_send_quota = 0;
    current.field_sources.daily_send_quota = "profile";
    mount(); const dialog = await openEditor();
    await waitFor(() => expect(calls.some(c => c.path.endsWith("/permission-editor"))).toBe(true));
    const quota = within(dialog).getAllByRole("spinbutton")[0];
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(quota).toHaveValue(null);
    fireEvent.change(quota, { target: { value: "0" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toEqual({ expected_revision: snapshot().revision, patch: { daily_send_quota: 0 } });
  });

  it("preserves dirty edits on conflict and never automatically replays or falls back to PUT", async () => {
    editorReply = async (_id, call) => call.method === "PATCH" ? reply({ error: { code: "CONFLICT", message: "Revision changed; reload before saving" } }, 409) : reply(current);
    mount(); const dialog = await loaded();
    const quota = within(dialog).getAllByRole("spinbutton")[0];
    fireEvent.change(quota, { target: { value: "25" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(quota).toHaveValue(25);
    expect(writes()).toHaveLength(1);
    expect(writes()[0].method).toBe("PATCH");
    expect(within(dialog).getByRole("button", { name: /reload|重新/i })).toBeEnabled();
  });

  it("reload after conflict explicitly discards draft and uses the fresh observed revision", async () => {
    let conflicted = false;
    editorReply = async (_id, call) => {
      if (call.method === "PATCH" && !conflicted) {
        conflicted = true;
        current = snapshot(alice, "9007199254740997");
        current.overrides.daily_send_quota = 10;
        return reply({ error: { code: "CONFLICT", message: "Revision changed" } }, 409);
      }
      return reply(current);
    };
    mount(); const dialog = await loaded();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "25" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(save(dialog)).toBeDisabled());
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(25);
    await userEvent.click(within(dialog).getByRole("button", { name: /reload/i }));
    await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(10));
    expect(writes()).toHaveLength(1);
    expect(save(dialog)).toBeDisabled();
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "30" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(2));
    expect(writes()[1].body).toEqual({ expected_revision: current.revision, patch: { daily_send_quota: 30 } });
  });

  it.each([["All", "all"], ["No domains allowed", "none"], ["Inherit: Allowed Domain Scope", "inherit"]])("domain %s preserves empty-zone mode instead of treating all empty arrays alike", async (label, mode) => {
    // Start from list so every tested choice is a true dirty source transition.
    current.overrides.domain_access = { mode: "list", zone_ids: [zone] };
    current.overrides.allowed_zone_ids = [zone];
    mount(); const dialog = await loaded();
    await userEvent.click(within(dialog).getByRole("button", { name: new RegExp(`^${label}$`, "i") }));
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    const body = writes()[0].body as { expected_revision: unknown; patch: { domain_access: { mode: string; zone_ids?: string[] | null } | null } };
    expect(body.expected_revision).toEqual(snapshot().revision);
    expect(Object.keys(body.patch)).toEqual(["domain_access"]);
    // The frozen wire contract permits NULL for inherit and absent/null/[]
    // zone_ids for non-list modes; compare intent, not one JSON spelling.
    expect(body.patch.domain_access?.mode ?? "inherit").toBe(mode);
    expect(body.patch.domain_access?.zone_ids ?? []).toEqual([]);
  });

  it("unchecking the last allowed zone sends none, never the legacy empty-array allow-all", async () => {
    current.overrides.domain_access = { mode: "list", zone_ids: [zone] };
    current.overrides.allowed_zone_ids = [zone];
    mount(); const dialog = await loaded();
    const zoneSwitch = within(dialog).getByRole("switch", { name: "company.test" });
    expect(zoneSwitch).toBeChecked();
    await userEvent.click(zoneSwitch);
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toEqual({ expected_revision: snapshot().revision, patch: { domain_access: { mode: "none", zone_ids: [] } } });
  });

  it("disables mutations for capability false without pretending the edit succeeded", async () => {
    current.capabilities.patch = false;
    mount(); const dialog = await loaded();
    expect(save(dialog)).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });

  it("reset is one revision-bound explicit inherit patch, not legacy DELETE", async () => {
    mount(); const dialog = await loaded();
    await userEvent.click(within(dialog).getByRole("button", { name: /^Reset Overrides$/ }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toEqual({ path: `/api/v1/admin/users/${alice}/permission-editor`, method: "PATCH", body: { expected_revision: snapshot().revision,
      patch: { can_send: null, daily_send_quota: null, daily_receive_quota: null, max_mailboxes: null, max_domains: null,
        can_create_domains: null, can_create_routes: null, can_create_api_keys: null, domain_access: { mode: "inherit", zone_ids: [] } } } });
  });

  it("a failed snapshot never uses effective-only legacy data to enable an unsafe save", async () => {
    editorReply = async () => reply({ error: { code: "FORBIDDEN", message: "Permission snapshot denied" } }, 403);
    mount(); const dialog = await openEditor();
    await waitFor(() => expect(calls.some(c => c.path.endsWith("/permission-editor"))).toBe(true));
    expect(save(dialog)).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });

  it("discards an older target snapshot that resolves after switching users", async () => {
    let resolveAlice!: (value: Response) => void;
    const pendingAlice = new Promise<Response>(resolve => { resolveAlice = resolve; });
    const bobSnapshot = snapshot(bob, "22");
    bobSnapshot.overrides.daily_send_quota = 7;
    editorReply = async (id) => id === alice ? pendingAlice : reply(bobSnapshot);
    mount(); await openEditor();
    await waitFor(() => expect(calls.some(c => c.path === `/api/v1/admin/users/${alice}/permission-editor`)).toBe(true));
    await userEvent.keyboard("{Escape}");
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const dialog = await openEditor("bob@company.test");
    await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(7));
    await act(async () => { resolveAlice(reply(snapshot())); await pendingAlice; });
    expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(7);
    fireEvent.change(within(dialog).getAllByRole("spinbutton")[0], { target: { value: "8" } });
    await userEvent.click(save(dialog));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0]).toEqual({ path: `/api/v1/admin/users/${bob}/permission-editor`, method: "PATCH", body: { expected_revision: bobSnapshot.revision, patch: { daily_send_quota: 8 } } });
  });
});
