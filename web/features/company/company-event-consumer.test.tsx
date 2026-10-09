import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import PermissionsPage from "./profile-management";
import UsersPage from "./user-management";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";

// Real source owners, React/Base UI, SWR, API/session and ReadableStream run
// here. Deterministic fetch fixtures are NOT Go/PG/shipping browser evidence.
const host = vi.hoisted(() => ({ level: "admin", tenantId: "10000000-0000-4000-8000-000000000001" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => host }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const tenant = host.tenantId;
const otherTenant = "10000000-0000-4000-8000-000000000002";
const id = "40000000-0000-4000-8000-000000000001";
const employee = "20000000-0000-4000-8000-000000000001";
const effective = { can_send: false, daily_send_quota: 10, daily_receive_quota: 10, max_mailboxes: 2, max_domains: 2, allowed_zone_ids: null, can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
let profile: ReturnType<typeof makeProfile>;
let calls: { path: string; method: string; headers: Headers; signal?: AbortSignal | null }[];
let streams: { controller: ReadableStreamDefaultController<Uint8Array>; signal?: AbortSignal | null; closed: boolean }[];
let streamStatus: number;
let authorityStatus: number;
let intercept: ((path: string, init?: RequestInit) => Promise<Response> | undefined) | undefined;
function makeProfile() { return { ...effective, id, tenant_id: host.tenantId, name: "Controlled", description: "Initial", revision: "9007199254740995", is_system: false, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" }; }
function editorSnapshot() {
  return { user_id: employee, tenant_id: host.tenantId, profile,
    overrides: { can_send: null, daily_send_quota: null, daily_receive_quota: null, max_mailboxes: null, max_domains: null, allowed_zone_ids: null, can_create_domains: null, can_create_routes: null, can_create_api_keys: null, domain_access: { mode: "inherit", zone_ids: null } },
    effective, field_sources: Object.fromEntries(["can_send", "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains", "can_create_domains", "can_create_routes", "can_create_api_keys", "domain_access"].map(field => [field, "profile"])),
    revision: { user_id: employee, tenant_id: host.tenantId, user_revision: "9007199254740993", profile_id: id, profile_revision: profile.revision }, capabilities: { patch: true, assign_profile: true } };
}
const response = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : { error: { code: status === 401 ? "UNAUTHORIZED" : "FORBIDDEN", message: "Denied" } }), { status, headers: { "Content-Type": "application/json" } });
function identity(name = "admin", targetTenant = host.tenantId) {
  installSession(`${name}-token`, { id: name, tenant_id: targetTenant, email: `${name}@test.invalid`, display_name: name, role: host.level as "admin" });
}
beforeEach(() => {
  host.tenantId = tenant; host.level = "admin"; profile = makeProfile(); calls = []; streams = [];
  streamStatus = authorityStatus = 200; intercept = undefined; identity();
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    calls.push({ path, method: init?.method ?? "GET", headers: new Headers(init?.headers), signal: init?.signal });
    const pending = intercept?.(path, init); if (pending) return pending;
    if (path === "/api/v1/company/events") {
      if (streamStatus !== 200) return response(null, streamStatus);
      const stream = { controller: undefined as unknown as ReadableStreamDefaultController<Uint8Array>, signal: init?.signal, closed: false };
      const body = new ReadableStream<Uint8Array>({ start(controller) { stream.controller = controller; }, cancel() { stream.closed = true; } });
      init?.signal?.addEventListener("abort", () => { if (!stream.closed) { stream.closed = true; stream.controller.error(new DOMException("Aborted", "AbortError")); } }, { once: true });
      streams.push(stream); return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (path === "/api/v1/company/overview") return response({}, authorityStatus);
    if (path === "/api/v1/admin/permissions") return response([profile]);
    if (path.endsWith("/permission-editor")) return response(editorSnapshot());
    if (path === "/api/v1/admin/users") return response([{ id: employee, tenant_id: host.tenantId, email: "employee@test.invalid", display_name: "Employee", role: "user", is_active: true, permission_profile_id: id, created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z" }]);
    if (path.endsWith("/deletion-preview")) return response({ profile_id: id, profile_revision: profile.revision, members: [], changes: [] });
    return response([]);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); Object.defineProperty(navigator, "locks", { configurable: true, value: undefined }); });
function changed(extra: Record<string, unknown> = {}) {
  return { type: "company.admin.changed", tenant_id: host.tenantId, occurred_at: "2026-10-02T01:02:03Z", metadata: { action: "permission.profile.update", resource_type: "permission_profile", resource_id: id }, ...extra };
}
async function emit(data: unknown, type = "company.admin.changed", index = streams.length - 1) {
  await act(async () => streams[index].controller.enqueue(new TextEncoder().encode(`id: ${id}\nevent: ${type}\ndata: ${JSON.stringify(data)}\n\n`)));
}
async function settled() {
  await waitFor(() => expect(streams).toHaveLength(1));
  await waitFor(() => expect(calls.filter(call => call.path === "/api/v1/admin/permissions").length).toBeGreaterThan(1));
  await screen.findByText("Controlled");
}
async function openProfile(action = "Edit") {
  await settled(); const row = screen.getByText("Controlled").closest("tr")!;
  await userEvent.click(within(row).getByRole("button"));
  await userEvent.click(await screen.findByRole("menuitem", { name: action }));
  return screen.findByRole("dialog");
}
const writes = () => calls.filter(call => call.method !== "GET");
async function closeAndReconnect(index = 0, manageClock = true) {
  if (manageClock) vi.useFakeTimers();
  try {
    await act(async () => {
      streams[index].closed = true; streams[index].controller.close();
      for (let i = 0; i < 20; i++) await Promise.resolve();
      await vi.advanceTimersByTimeAsync(1000);
      for (let i = 0; i < 20; i++) await Promise.resolve();
    });
  } finally { if (manageClock) vi.useRealTimers(); }
}
function enableActualRefreshLock() {
  Object.defineProperty(navigator, "locks", { configurable: true, value: {
    request: async (_name: string, callback: () => Promise<unknown>) => callback(),
  } });
}

describe("mounted company event consumers", () => {
  it("mounted EOF reconnect re-reads overview then authoritative resource without rebasing draft", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "EOF-safe draft" } });
    const before = calls.length;
    profile = { ...profile, name: "EOF authoritative changed", revision: "9007199254740999" };
    await closeAndReconnect();
    await screen.findByText("EOF authoritative changed");
    expect(streams).toHaveLength(2);
    // No manual resync injection: the mounted source observes an actual EOF,
    // base streamEvents reconnects, and its connection resync drives both GETs.
    expect(calls.slice(before).map(call => [call.method, call.path])).toEqual([
      ["GET", "/api/v1/company/events"],
      ["GET", "/api/v1/company/overview"],
      ["GET", "/api/v1/admin/permissions"],
    ]);
    expect(calls.slice(before).every(call => call.headers.get("X-Tenant-ID") === tenant)).toBe(true);
    expect(within(dialog).getByDisplayValue("EOF-safe draft")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^Save$/ })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it("refresh endpoint final failure stops the original scope and clears its private draft", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Expired private draft" } });
    const oldScope = sessionScope(), oldSignal = streams[0].signal;
    enableActualRefreshLock();
    intercept = (path, init) => {
      if (path === "/api/v1/company/events") return Promise.resolve(response(null, 401));
      if (path === "/api/v1/auth/refresh") return Promise.resolve(response(null, 403));
      // Fail closed after the existing credential owner clears the identity;
      // no successful anonymous fake GET can repopulate a private UI cache.
      if (!new Headers(init?.headers).get("Authorization")) return Promise.resolve(response(null, 401));
    };
    vi.useFakeTimers();
    try {
      // Keep ONE clock from the EOF/backoff through the no-retry window. A
      // clock reset here would silently discard an incorrectly live timer.
      await closeAndReconnect(0, false);
      expect(screen.queryByRole("dialog")).toBeNull();
      expect(sessionScope()).not.toBe(oldScope); expect(oldSignal?.aborted).toBe(true);
      expect(localStorage.getItem("tabmail_access_token")).toBeNull();
      expect(localStorage.getItem("tabmail_user")).toBeNull();
      expect(localStorage.getItem("tabmail_tenant_id")).toBeNull();
      expect(screen.queryByText("Controlled")).toBeNull();
      expect(screen.queryByDisplayValue("Expired private draft")).toBeNull();
      expect(calls.filter(call => call.path === "/api/v1/auth/refresh").map(call => call.method)).toEqual(["POST"]);
      expect(calls.filter(call => call.path === "/api/v1/company/events")).toHaveLength(2);
      await act(async () => vi.advanceTimersByTimeAsync(30000));
      expect(calls.filter(call => call.path === "/api/v1/company/events")).toHaveLength(2);
      expect(calls.filter(call => call.path === "/api/v1/auth/refresh")).toHaveLength(1);
      expect(writes().map(call => call.path)).toEqual(["/api/v1/auth/refresh"]);
    } finally { vi.useRealTimers(); }
  });
  it("refresh endpoint success preserves stable identity and dirty draft through actual stream retry", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Refresh-safe draft" } });
    const oldScope = sessionScope(), oldSignal = streams[0].signal, before = calls.length;
    enableActualRefreshLock();
    intercept = (path, init) => {
      if (path === "/api/v1/auth/refresh") return Promise.resolve(response({ access_token: "refresh-replacement-token" }));
      if (path === "/api/v1/company/events" && new Headers(init?.headers).get("Authorization") === "Bearer admin-token") return Promise.resolve(response(null, 401));
    };
    await closeAndReconnect();
    await waitFor(() => expect(streams).toHaveLength(2));
    expect(sessionScope()).toBe(oldScope); expect(oldSignal?.aborted).toBe(false);
    expect(localStorage.getItem("tabmail_access_token")).toBe("refresh-replacement-token");
    expect(calls.slice(before).map(call => [call.method, call.path])).toEqual([
      ["GET", "/api/v1/company/events"],
      ["POST", "/api/v1/auth/refresh"],
      ["GET", "/api/v1/company/events"],
      ["GET", "/api/v1/company/overview"],
      ["GET", "/api/v1/admin/permissions"],
    ]);
    const renewed = calls.slice(before).filter(call => call.path !== "/api/v1/auth/refresh").slice(1);
    expect(renewed.every(call => call.headers.get("Authorization") === "Bearer refresh-replacement-token" && call.headers.get("X-Tenant-ID") === tenant)).toBe(true);
    expect(screen.getByRole("dialog")).toBe(dialog);
    expect(within(dialog).getByDisplayValue("Refresh-safe draft")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^Save$/ })).toBeDisabled();
    expect(writes().map(call => call.path)).toEqual(["/api/v1/auth/refresh"]);
  });
  it("re-reads authoritative profiles on duplicate events without rebasing a dirty CAS form", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    const name = within(dialog).getByPlaceholderText("Profile name");
    fireEvent.change(name, { target: { value: "Preserved draft" } });
    const before = calls.filter(call => call.path === "/api/v1/admin/permissions").length;
    profile = { ...profile, name: "Authoritative changed", can_send: true, revision: "9007199254740999" };
    await emit(changed()); await emit(changed());
    await waitFor(() => expect(calls.filter(call => call.path === "/api/v1/admin/permissions").length).toBeGreaterThan(before));
    await screen.findByText("Authoritative changed");
    expect(within(dialog).getByDisplayValue("Preserved draft")).toBeInTheDocument();
    expect(within(dialog).getByRole("button", { name: /^Save$/ })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it("strictly ignores wrong-tenant, unknown and permission-bearing payloads", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Draft" } });
    const before = calls.length;
    await emit(changed({ tenant_id: otherTenant }));
    await emit(changed({ effective: { can_send: true } }));
    await emit(changed(), "unknown.changed");
    await emit(null, "resync");
    expect(calls).toHaveLength(before);
    expect(within(dialog).getByRole("button", { name: /^Save$/ })).toBeEnabled();
    expect(writes()).toHaveLength(0);
  });
  it("periodic tenant-bound resync invalidates deletion confirmation without automatic DELETE", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile("Delete");
    const checkbox = await within(dialog).findByRole("checkbox");
    await userEvent.click(checkbox);
    expect(within(dialog).getByRole("button", { name: /^Delete$/ })).toBeEnabled();
    await emit({ tenant_id: tenant }, "resync");
    await waitFor(() => expect(within(dialog).queryByRole("checkbox")).toBeNull());
    expect(within(dialog).getByRole("button", { name: /^Delete$/ })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it("same-principal token rotation keeps the stream and dirty draft", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Rotation-safe draft" } });
    const scope = sessionScope();
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope); expect(streams).toHaveLength(1); expect(streams[0].signal?.aborted).toBe(false);
    expect(within(dialog).getByDisplayValue("Rotation-safe draft")).toBeInTheDocument();
    await emit(changed());
    await waitFor(() => expect(calls.some(call => call.path === "/api/v1/company/overview" && call.headers.get("Authorization") === "Bearer rotated-token")).toBe(true));
    expect(within(dialog).getByDisplayValue("Rotation-safe draft")).toBeInTheDocument();
  });
  it("a late same-scope preview GET cannot restore confirmation after resync", async () => {
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>(resolve => { release = resolve; });
    intercept = path => path.endsWith("/deletion-preview") ? delayed : undefined;
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile("Delete");
    await waitFor(() => expect(calls.some(call => call.path.endsWith("/deletion-preview"))).toBe(true));
    await emit({ tenant_id: tenant }, "resync");
    await act(async () => release(response({ profile_id: id, profile_revision: profile.revision, members: [], changes: [] })));
    expect(within(dialog).queryByRole("checkbox")).toBeNull();
    expect(within(dialog).getByRole("button", { name: /^Delete$/ })).toBeDisabled();
    expect(writes()).toHaveLength(0);
  });
  it("a current role downgrade aborts the prior stream and clears the old private draft", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Admin draft" } });
    await act(async () => { host.level = "user"; identity(); });
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(streams[0].signal?.aborted).toBe(true);
    expect(calls.filter(call => call.path === "/api/v1/company/events")).toHaveLength(1);
    expect(writes()).toHaveLength(0);
  });
  it.each([401, 403])("terminal %s evicts only the current management scope and stops retrying", async status => {
    streamStatus = status; render(<SidebarProvider><PermissionsPage /></SidebarProvider>);
    await waitFor(() => expect(calls.filter(call => call.path === "/api/v1/company/events")).toHaveLength(1));
    await waitFor(() => expect(screen.queryByText("Controlled")).toBeNull());
    expect(localStorage.getItem("tabmail_access_token")).toBe("admin-token");
    expect(writes()).toHaveLength(0);
  });
  it("authoritative read revocation aborts the live stream and closes private draft UI", async () => {
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); const dialog = await openProfile();
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "Private draft" } });
    authorityStatus = 403; await emit({ tenant_id: tenant }, "resync");
    await waitFor(() => expect(screen.queryByRole("dialog")).toBeNull());
    expect(streams[0].signal?.aborted).toBe(true); expect(screen.queryByText("Controlled")).toBeNull();
    expect(writes()).toHaveLength(0);
  });
  it("old-session late 403 cannot evict a new tenant or its draft", async () => {
    let release!: (response: Response) => void;
    let oldRequest: AbortSignal | null | undefined;
    const oldResponse = new Promise<Response>(resolve => { release = resolve; });
    render(<SidebarProvider><PermissionsPage /></SidebarProvider>); await settled();
    intercept = (path, init) => { if (path === "/api/v1/company/overview" && new Headers(init?.headers).get("Authorization") === "Bearer admin-token") { oldRequest = init?.signal; return oldResponse; } };
    await emit(changed());
    await waitFor(() => expect(oldRequest).toBeTruthy());
    await act(async () => { host.tenantId = otherTenant; profile = { ...makeProfile(), name: "New tenant profile" }; identity("new-admin"); });
    await screen.findByText("New tenant profile");
    const row = screen.getByText("New tenant profile").closest("tr")!;
    await userEvent.click(within(row).getByRole("button")); await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" }));
    const dialog = screen.getByRole("dialog");
    fireEvent.change(within(dialog).getByPlaceholderText("Profile name"), { target: { value: "New identity draft" } });
    await act(async () => release(response(null, 403)));
    expect(oldRequest?.aborted).toBe(true); expect(within(dialog).getByDisplayValue("New identity draft")).toBeInTheDocument();
    expect(localStorage.getItem("tabmail_access_token")).toBe("new-admin-token");
  });
  it("real permission editor preserves dirty intent but requires explicit reload after an event", async () => {
    render(<SidebarProvider><UsersPage /></SidebarProvider>);
    await settled(); const row = screen.getByText("employee@test.invalid").closest("tr")!;
    await userEvent.click(within(row).getByRole("button")); await userEvent.click(await screen.findByRole("menuitem", { name: "Permissions" }));
    const dialog = screen.getByRole("dialog"); const quota = (await within(dialog).findAllByRole("spinbutton"))[0];
    fireEvent.change(quota, { target: { value: "25" } });
    expect(within(dialog).getByRole("button", { name: /^Save Overrides$/ })).toBeEnabled();
    await emit(changed());
    await waitFor(() => expect(within(dialog).getByRole("button", { name: /^Save Overrides$/ })).toBeDisabled());
    expect(quota).toHaveValue(25); expect(writes()).toHaveLength(0);
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-user-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(quota).toHaveValue(25); expect(screen.getByRole("dialog")).toBe(dialog);
  });
});
