import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import UsersPage from "./user-management";

// Real AuthProvider, keyed SWR cache, page, API adapter and session transport.
// Only fetch is a network fixture. An abort-ignoring fixture deliberately lets
// late responses arrive so assertSession, not mock cancellation, discards them.
const tenantA = "10000000-0000-4000-8000-000000000001";
const tenantB = "10000000-0000-4000-8000-000000000002";
const alice = "20000000-0000-4000-8000-000000000001";
const bob = "20000000-0000-4000-8000-000000000002";
const boundProfile = "40000000-0000-4000-8000-000000000001";
const destinationProfile = "40000000-0000-4000-8000-000000000002";
const effective = { can_send: false, daily_send_quota: 99, daily_receive_quota: 100,
  max_mailboxes: 2, max_domains: 3, allowed_zone_ids: [], can_create_domains: false,
  can_create_routes: false, can_create_api_keys: false };
const admin = (id: string, tenant_id: string): AuthUser => ({ id, tenant_id,
  email: `${id}@company.test`, display_name: id, role: "admin" });
function snapshot(user_id = alice, tenant_id = tenantA, fresh = false) {
  return { user_id, tenant_id,
    profile: { ...effective, id: boundProfile, tenant_id, name: fresh ? "Fresh bound" : "Old bound",
      description: "", is_system: false, revision: fresh ? "21" : "7",
      created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" },
    overrides: { can_send: false, daily_send_quota: fresh ? 7 : 0, daily_receive_quota: null,
      max_mailboxes: null, max_domains: null, allowed_zone_ids: [], can_create_domains: null,
      can_create_routes: null, can_create_api_keys: null, domain_access: { mode: "none", zone_ids: [] } },
    effective: { ...effective, daily_send_quota: fresh ? 7 : 0 },
    field_sources: { can_send: "override", daily_send_quota: "override", daily_receive_quota: "profile",
      max_mailboxes: "profile", max_domains: "profile", can_create_domains: "profile",
      can_create_routes: "profile", can_create_api_keys: "profile", domain_access: "override" },
    revision: { user_id, tenant_id, user_revision: fresh ? "31" : "11", profile_id: boundProfile,
      profile_revision: fresh ? "21" : "7" }, capabilities: { patch: true, assign_profile: true } };
}
type Snapshot = ReturnType<typeof snapshot>;
type Call = { path: string; method: string; body: unknown; tenant: string | null;
  authorization: string | null; signal: AbortSignal | null | undefined };
const reply = (data: unknown) => new Response(JSON.stringify({ data }), {
  status: 200, headers: { "Content-Type": "application/json" } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(r => { resolve = r; });
  return { promise, resolve };
}
let current: Snapshot;
let calls: Call[];
let editorReply: (call: Call) => Promise<Response>;
let listedTenant: string | undefined;
let freshScope: boolean;
const editorCalls = () => calls.filter(c => c.path.includes("/permission-editor"));
const editorWrites = () => editorCalls().filter(c => c.method !== "GET");

function SessionControls() {
  const auth = useAuth();
  return <>
    <output data-testid="auth-scope">{auth.level}:{auth.tenantId}</output>
    <button data-testid="tenant-change" onClick={() => auth.setTenantId(tenantB)}>Switch tenant</button>
    <button data-testid="session-change" onClick={() => auth.loginWithTokens("fresh-fixture-token", admin("second-admin", tenantA))}>Switch session</button>
    <button data-testid="logout" onClick={() => auth.logout()}>Logout</button>
    <button data-testid="login-after-logout" onClick={() => auth.loginWithTokens("fresh-fixture-token", admin("second-admin", tenantB))}>Login</button>
  </>;
}
function mount() {
  return render(<AuthProvider><SessionControls /><SidebarProvider><UsersPage /></SidebarProvider></AuthProvider>);
}
beforeEach(() => {
  current = snapshot(); calls = []; listedTenant = undefined; freshScope = false;
  editorReply = async () => reply(current);
  installSession("old-fixture-token", admin("first-admin", tenantA));
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({
    matches: false, media: query, onchange: null, addListener() {}, removeListener() {},
    addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  // The real logout transport requires the browser's shared cookie lock.
  Object.defineProperty(navigator, "locks", { configurable: true,
    value: { request: async (_name: string, run: () => unknown) => run() } });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const headers = new Headers(init?.headers);
    const call: Call = { path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined,
      tenant: headers.get("X-Tenant-ID"), authorization: headers.get("Authorization"), signal: init?.signal };
    calls.push(call);
    if (path.includes("/permission-editor")) return editorReply(call);
    if (path === "/api/v1/auth/logout") return reply({});
    if (path === "/api/v1/auth/me/permissions") return reply(effective);
    if (path === "/api/v1/admin/users") return reply([{ id: current.user_id,
      tenant_id: listedTenant ?? current.tenant_id, email: `${current.user_id === bob ? "bob" : "alice"}@company.test`,
      display_name: current.user_id === bob ? "Bob" : "Alice", role: "user", is_active: true,
      permission_profile_id: boundProfile, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" }]);
    if (path === "/api/v1/admin/permissions") return reply([current.profile,
      { ...current.profile, id: destinationProfile, name: freshScope ? "Fresh destination" : "Old destination",
        revision: freshScope ? "22" : "9" }]);
    if (path === "/api/v1/domains") return reply([]);
    throw new Error(`Unexpected session consumer request: ${call.method} ${path}`);
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
const save = (dialog: HTMLElement) => within(dialog).getByRole("button", { name: /^Save Overrides$/ });
const reset = (dialog: HTMLElement) => within(dialog).getByRole("button", { name: /^Reset Overrides$/ });
const quota = (dialog: HTMLElement) => within(dialog).getAllByRole("spinbutton")[0];
async function loaded(email = "alice@company.test", value = 0) {
  const dialog = await openEditor(email);
  await waitFor(() => expect(quota(dialog)).toHaveValue(value));
  await waitFor(() => expect(reset(dialog)).toBeEnabled());
  return dialog;
}
async function selectDestination(dialog: HTMLElement, fresh = false) {
  await userEvent.click(within(dialog).getByRole("combobox"));
  await userEvent.click(await screen.findByRole("option", { name: fresh ? "Fresh destination" : "Old destination" }));
}

describe("permission editor real session ownership consumer", () => {
  for (const event of ["tenant", "session", "logout"] as const) {
    for (const phase of ["loaded", "get", "patch", "assignment"] as const) {
      it(`${event} change clears ${phase} ownership without a manual dialog close and cannot rebase old intent`, async () => {
        const oldSnapshot = snapshot();
        const late = deferred<Response>();
        let held = false;
        const pendingMethod = phase === "get" ? "GET" : phase === "assignment" ? "POST" : "PATCH";
        if (phase !== "loaded") editorReply = async call => {
          if (!held && call.method === pendingMethod) { held = true; return late.promise; }
          return reply(current);
        };
        mount();
        const dialog = phase === "get" ? await openEditor() : await loaded();
        if (phase === "get") {
          await waitFor(() => expect(held).toBe(true));
          expect(save(dialog)).toBeDisabled();
          expect(reset(dialog)).toBeDisabled();
          expect(within(dialog).getByRole("combobox")).toBeDisabled();
        } else {
          fireEvent.change(quota(dialog), { target: { value: "25" } });
          if (phase === "assignment") await selectDestination(dialog);
          expect(save(dialog)).toBeEnabled();
          if (phase === "loaded") expect(reset(dialog)).toBeEnabled();
          else {
            await userEvent.click(save(dialog));
            await waitFor(() => expect(editorWrites()).toHaveLength(1));
            expect(editorWrites()[0]).toMatchObject({ tenant: tenantA, authorization: "Bearer old-fixture-token",
              body: { expected_revision: oldSnapshot.revision, patch: { daily_send_quota: 25 } } });
          }
        }
        const oldPending = phase === "get" ? editorCalls().at(-1) : editorWrites().at(-1);
        const nextTenant = event === "session" ? tenantA : tenantB;
        const nextTarget = event === "session" ? alice : bob;
        current = snapshot(nextTarget, nextTenant, true); freshScope = true;
        const boundary = calls.length;
        fireEvent.click(screen.getByTestId(event === "tenant" ? "tenant-change" : event === "session" ? "session-change" : "logout"));
        // Production AuthProvider clears the page by remounting its session-keyed
        // subtree. Keeping the same dialog is not required; clearing its authority is.
        await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
        if (phase !== "loaded") expect(oldPending?.signal?.aborted).toBe(true);
        const originalWrites = phase === "patch" || phase === "assignment" ? 1 : 0;
        expect(editorWrites()).toHaveLength(originalWrites);
        if (event === "logout") {
          expect(screen.getByTestId("auth-scope")).toHaveTextContent("public:");
          expect(calls.some(c => c.path === "/api/v1/auth/logout" && c.method === "POST" && c.authorization === "Bearer old-fixture-token")).toBe(true);
          fireEvent.click(screen.getByTestId("login-after-logout"));
        }
        await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent(`admin:${nextTenant}`));
        const next = await loaded(`${nextTarget === bob ? "bob" : "alice"}@company.test`, 7);
        expect(save(next)).toBeDisabled();
        expect(within(next).getByRole("combobox")).toHaveTextContent("Fresh bound");
        const nextAuthorization = event === "tenant" ? "Bearer old-fixture-token" : "Bearer fresh-fixture-token";
        expect(editorCalls().at(-1)).toMatchObject({ method: "GET", path: `/api/v1/admin/users/${nextTarget}/permission-editor`,
          tenant: nextTenant, authorization: nextAuthorization });
        expect(calls.slice(boundary).some(c => c.path === "/api/v1/admin/permissions" && c.tenant === nextTenant && c.authorization === nextAuthorization)).toBe(true);
        if (phase !== "loaded") {
          const oldReply = reply({ ...oldSnapshot, overrides: { ...oldSnapshot.overrides, daily_send_quota: 25 } });
          const oldBody = vi.spyOn(oldReply, "json");
          await act(async () => { late.resolve(oldReply); await late.promise; });
          // The actual base.request lease rejects even an abort-ignoring server.
          expect(oldBody).not.toHaveBeenCalled();
          expect(quota(next)).toHaveValue(7);
          expect(within(next).getByRole("combobox")).toHaveTextContent("Fresh bound");
          expect(save(next)).toBeDisabled();
          expect(editorWrites()).toHaveLength(originalWrites);
        }
        await selectDestination(next, true);
        await userEvent.click(save(next));
        await waitFor(() => expect(editorWrites()).toHaveLength(originalWrites + 1));
        expect(editorWrites().at(-1)).toMatchObject({ path: `/api/v1/admin/users/${nextTarget}/permission-editor/assignment`,
          method: "POST", tenant: nextTenant, authorization: nextAuthorization,
          body: { expected_revision: current.revision, profile_id: destinationProfile, profile_revision: "22", patch: {} } });
      });
    }
  }

  it("rejects a self-consistent old-tenant user row and snapshot delivered under a new tenant transport", async () => {
    mount(); await loaded();
    // A freshly delivered response is not proof of scope: both the list row
    // and editor payload claim A while the authenticated request now scopes B.
    fireEvent.click(screen.getByTestId("tenant-change"));
    await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent(`admin:${tenantB}`));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const dialog = await openEditor();
    expect(calls.some(c => c.path === "/api/v1/admin/users" && c.tenant === tenantB)).toBe(true);
    await act(async () => { await Promise.resolve(); });
    expect(reset(dialog)).toBeDisabled();
    expect(within(dialog).getByRole("combobox")).toBeDisabled();
    expect(save(dialog)).toBeDisabled();
    fireEvent.change(quota(dialog), { target: { value: "25" } });
    await userEvent.click(save(dialog));
    expect(editorWrites()).toHaveLength(0);
  });

  for (const method of ["PATCH", "POST"] as const) {
    it(`rejects a same-target wrong-tenant ${method} response rather than importing its capabilities or version`, async () => {
      current = snapshot(bob, tenantB, true); freshScope = true;
      installSession("fresh-fixture-token", admin("second-admin", tenantB));
      const wrongTenant = snapshot(bob, tenantA);
      wrongTenant.overrides.daily_send_quota = wrongTenant.effective.daily_send_quota = 19;
      editorReply = async call => reply(call.method === "GET" ? current : wrongTenant);
      mount(); const dialog = await loaded("bob@company.test", 7);
      fireEvent.change(quota(dialog), { target: { value: "8" } });
      if (method === "POST") await selectDestination(dialog, true);
      await userEvent.click(save(dialog));
      await waitFor(() => expect(editorWrites()).toHaveLength(1));
      await act(async () => { await Promise.resolve(); });
      expect(editorWrites()[0]).toMatchObject({ method, tenant: tenantB,
        body: { expected_revision: current.revision, patch: { daily_send_quota: 8 } } });
      expect(quota(dialog)).toHaveValue(8);
      expect(within(dialog).getByRole("combobox")).toHaveTextContent(method === "POST" ? "Fresh destination" : "Fresh bound");
      expect(within(dialog).getByRole("combobox")).toBeDisabled();
      expect(reset(dialog)).toBeDisabled();
      expect(save(dialog)).toBeDisabled();
      expect(editorWrites()).toHaveLength(1);
    });
  }

  it("new-session read does not reuse the old session's write capabilities", async () => {
    mount(); const old = await loaded();
    fireEvent.change(quota(old), { target: { value: "25" } });
    expect(save(old)).toBeEnabled();
    current = snapshot(alice, tenantA, true); freshScope = true;
    current.capabilities = { patch: false, assign_profile: false };
    fireEvent.click(screen.getByTestId("session-change"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    const next = await openEditor();
    await waitFor(() => expect(quota(next)).toHaveValue(7));
    expect(quota(next)).toBeDisabled();
    expect(save(next)).toBeDisabled();
    expect(reset(next)).toBeDisabled();
    expect(within(next).getByRole("combobox")).toBeDisabled();
    expect(editorWrites()).toHaveLength(0);
  });

  it("a new-tenant read rejects another target's self-consistent snapshot", async () => {
    current = snapshot(bob, tenantB, true); freshScope = true;
    installSession("fresh-fixture-token", admin("second-admin", tenantB));
    editorReply = async () => reply(snapshot(alice, tenantB, true));
    mount(); const dialog = await openEditor("bob@company.test");
    await waitFor(() => expect(editorCalls()).toHaveLength(1));
    await act(async () => { await Promise.resolve(); });
    expect(quota(dialog)).not.toHaveValue(7);
    expect(reset(dialog)).toBeDisabled();
    expect(within(dialog).getByRole("combobox")).toBeDisabled();
    expect(save(dialog)).toBeDisabled();
    expect(editorWrites()).toHaveLength(0);
  });
});
