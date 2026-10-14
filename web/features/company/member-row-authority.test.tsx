import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { useSWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, clearSessionCredentials, installSession, sessionScope } from "@/lib/session";
import type { AdminUser, AuthUser } from "@/lib/types";
import UsersPage from "./user-management";

// Real AuthProvider, page, Base UI, SWR, session/API and event revocation path.
// Only fetch, confirmation and toast are synthetic. No backend authz claim.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const tenant = "10000000-0000-4000-8000-000000000001";
const foreign = "10000000-0000-4000-8000-000000000002";
const actorID = "20000000-0000-4000-8000-000000000001";
const memberID = "20000000-0000-4000-8000-000000000002";
type Role = "admin" | "super_admin" | "user";
type Operation = "active" | "delete";
const actor = (role: Role = "admin", id = actorID, tenant_id = tenant): AuthUser => ({ id, role, tenant_id, email: `${id}@example.test`, display_name: "Actor" });
const member = (role: Role = "user", tenant_id = tenant, id = memberID): AdminUser => ({ id, role, tenant_id, email: "target@example.test", display_name: "Target", is_active: true, created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z" });
const effective = { can_send: false, daily_send_quota: 0, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: [], can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
const reply = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 403) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: "Controlled member failure" } }), { status, headers: { "Content-Type": "application/json" } });
function deferred() { let resolve!: (response: Response) => void; const promise = new Promise<Response>(done => { resolve = done; }); return { promise, resolve }; }
let target: AdminUser;
let writes: { path: string; method: string; body: unknown; tenant: string | null; authorization: string | null }[];
let eventReads: ReturnType<typeof deferred>[];
let writeReply: () => Promise<Response>;
let userReads: number;
function identity(user = actor()) { installSession(`${user.id}-${user.role}-token`, user); }
function changeIdentity(boundary: "account" | "tenant" | "role") {
  if (boundary === "tenant") { localStorage.setItem("tabmail_tenant_id", foreign); window.dispatchEvent(new Event(AUTH_EVENT)); }
  else identity(actor(boundary === "role" ? "user" : "admin", boundary === "account" ? "changed-actor" : actorID));
}
function Controls() {
  const auth = useAuth(), { mutate } = useSWRConfig();
  return <><output data-testid="actor-context">{auth.level}:{auth.tenantId}:{auth.user?.id}</output>
    <button onClick={() => { void mutate(key => Array.isArray(key) && (Array.isArray(key[2]) ? key[2][0] : key[2]) === "admin-users", { data: [target] }, { revalidate: false }); }}>Restore stale member cache</button></>;
}
beforeEach(() => {
  target = member(); writes = []; eventReads = []; userReads = 0; writeReply = async () => reply(target); identity();
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname, method = init?.method ?? "GET", headers = new Headers(init?.headers);
    if (method !== "GET") { writes.push({ path, method, body: init?.body ? JSON.parse(String(init.body)) : undefined, tenant: headers.get("X-Tenant-ID"), authorization: headers.get("Authorization") }); return writeReply(); }
    if (path === "/api/v1/auth/me/permissions") return reply(effective);
    if (path === "/api/v1/admin/users") { userReads++; return reply([target]); }
    if (path === "/api/v1/admin/permissions" || path === "/api/v1/domains") return reply([]);
    if (path === "/api/v1/company/events") { const pending = deferred(); eventReads.push(pending); return pending.promise; }
    throw new Error(`Unexpected member request: ${method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
async function mount(role: Role | "public" = "admin") {
  render(<AuthProvider><Controls /><SidebarProvider><UsersPage /></SidebarProvider></AuthProvider>);
  await waitFor(() => expect(screen.getByTestId("actor-context")).toHaveTextContent(`${role}:`));
  await screen.findByText(target.email); await settle();
}
const row = () => screen.getByText(target.email).closest("tr")!;
const toggle = () => within(row()).getByRole("switch");
async function deletion() { await userEvent.click(within(row()).getByRole("button")); return screen.findByRole("menuitem", { name: "Delete User" }); }
async function actOn(operation: Operation) { await userEvent.click(operation === "active" ? toggle() : await deletion()); }
async function expectBlocked() {
  expect(toggle()).toHaveAttribute("aria-disabled", "true"); expect(toggle()).toHaveAttribute("tabindex", "-1");
  await userEvent.click(toggle());
  const remove = await deletion(); expect(remove).toHaveAttribute("aria-disabled", "true"); await userEvent.click(remove); await settle();
  expect(window.confirm).not.toHaveBeenCalled(); expect(writes).toHaveLength(0);
  expect(screen.getByRole("menuitem", { name: "Permissions" })).not.toHaveAttribute("aria-disabled", "true");
}
async function expectWrite(operation: Operation) {
  await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toMatchObject({ path: `/api/v1/admin/users/${target.id}`, method: operation === "active" ? "PATCH" : "DELETE", tenant });
  expect(writes[0].body).toEqual(operation === "active" ? { is_active: false } : undefined);
}

describe.each(["active", "delete"] as const)("member %s command", operation => {
  it("allows an admin to manage an employee in the selected tenant", async () => { await mount(); await actOn(operation); await expectWrite(operation); });
  it.each(["user", "admin", "super_admin"] as const)("allows a superadmin to manage a same-tenant %s", async role => {
    identity(actor("super_admin")); target = member(role); await mount("super_admin"); await actOn(operation); await expectWrite(operation);
  });
  it("uses the superadmin's selected tenant, not their home tenant", async () => {
    identity(actor("super_admin", actorID, foreign)); localStorage.setItem("tabmail_tenant_id", tenant); window.dispatchEvent(new Event(AUTH_EVENT));
    await mount("super_admin"); await actOn(operation); await expectWrite(operation);
  });
  it("preserves normal server rejection feedback and keeps the member row", async () => {
    writeReply = async () => failure(500); await mount(); await actOn(operation); await expectWrite(operation);
    await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1)); expect(toast.success).not.toHaveBeenCalled(); expect(row()).toBeInTheDocument();
  });
  it.each(["success", "failure"] as const)("does not show old-session %s feedback after an in-flight command", async outcome => {
    const pending = deferred(); writeReply = () => pending.promise; await mount(); await actOn(operation); await expectWrite(operation);
    act(() => identity(actor("admin", "new-actor"))); await screen.findByText(target.email); await settle();
    const readsBeforeReply = userReads;
    await act(async () => pending.resolve(outcome === "success" ? reply(target) : failure(500))); await settle();
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(userReads).toBe(readsBeforeReply); expect(writes).toHaveLength(1);
  });
});

it.each(["admin", "super_admin"] as const)("does not let tenant admins manage a %s peer", async role => { target = member(role); await mount(); await expectBlocked(); });
it.each(["admin", "super_admin"] as const)("does not let a %s use a cross-tenant row", async role => { identity(actor(role)); target = member("user", foreign); await mount(role); await expectBlocked(); });
it.each(["user", "public"] as const)("keeps member writes disabled for a %s actor", async role => { if (role === "public") clearSessionCredentials(); else identity(actor(role)); await mount(role); await expectBlocked(); });
it("rejects member commands without a selected tenant", async () => { localStorage.removeItem("tabmail_tenant_id"); window.dispatchEvent(new Event(AUTH_EVENT)); await mount(); await expectBlocked(); });
it("blocks superadmin self-deletion but does not invent a blanket self-freeze ban", async () => {
  identity(actor("super_admin")); target = member("super_admin", tenant, actorID); await mount("super_admin");
  expect(toggle()).not.toHaveAttribute("aria-disabled", "true"); const remove = await deletion(); expect(remove).toHaveAttribute("aria-disabled", "true"); await userEvent.click(remove);
  expect(window.confirm).not.toHaveBeenCalled(); expect(writes).toHaveLength(0); await userEvent.keyboard("{Escape}"); await userEvent.click(toggle()); await expectWrite("active");
});
it.each(["account", "tenant", "role"] as const)("rechecks the %s context after delete confirmation returns true", async boundary => {
  await mount(); vi.mocked(window.confirm).mockImplementation(() => { changeIdentity(boundary); return true; });
  await userEvent.click(await deletion()); await settle(); expect(window.confirm).toHaveBeenCalledTimes(1); expect(writes).toHaveLength(0);
});
it.each(["account", "tenant", "role"] as const)("blocks an old status callback when the %s changes before React commits the new view", async boundary => {
  await mount(); const oldToggle = toggle(); act(() => { changeIdentity(boundary); fireEvent.click(oldToggle); }); await settle(); expect(writes).toHaveLength(0);
});
it("keeps valid commands available through token-only rotation", async () => {
  await mount(); const scope = sessionScope();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-member-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  expect(sessionScope()).toBe(scope); await actOn("active"); await expectWrite("active"); expect(writes[0].authorization).toBe("Bearer rotated-member-token");
});
it("does not revive row commands from stale cache after the real event consumer revokes the current scope", async () => {
  await mount(); await waitFor(() => expect(eventReads.length).toBeGreaterThan(0));
  await act(async () => { for (const pending of eventReads) pending.resolve(failure()); });
  await waitFor(() => expect(screen.queryByText(target.email)).not.toBeInTheDocument());
  await userEvent.click(screen.getByRole("button", { name: "Restore stale member cache" })); await screen.findByText(target.email);
  await expectBlocked();
});

it("does not allow an ordinary admin to manage matching rows in a foreign selected tenant", async () => {
  localStorage.setItem("tabmail_tenant_id", foreign); window.dispatchEvent(new Event(AUTH_EVENT)); target = member("user", foreign);
  await mount(); await expectBlocked();
});
