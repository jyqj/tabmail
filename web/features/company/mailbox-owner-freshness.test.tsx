import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AdminUser } from "@/lib/types";
import { MailboxAdmin } from "./mailbox-admin";

// Real MailboxAdmin, EmployeeField, SWR, session and API request layer.
// The extra refresh button exercises SWR revalidation without mocking the hook.
const tenant = "owner-company";
const alice: AdminUser = { id: "owner-alice", tenant_id: tenant, role: "user", is_active: true, display_name: "Alice owner", email: "alice@example.test", created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z" };
const bob: AdminUser = { ...alice, id: "owner-bob", display_name: "Bob owner", email: "bob@example.test" };
type Call = { path: string; method: string; body?: Record<string, unknown>; headers: Headers; signal?: AbortSignal | null };
let calls: Call[];
let members: AdminUser[];
let readMembers: (page: number) => Promise<Response>;
let readBoxes: () => Promise<Response>;
const json = (data: unknown) => new Response(JSON.stringify(data), { headers: { "Content-Type": "application/json" } });
const list = (data = members, total = data.length) => json({ data, meta: { page: 1, per_page: 100, total } });
const failure = (status = 503) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "UNAVAILABLE", message: "Member directory unavailable" } }), { status, headers: { "Content-Type": "application/json" } });
const posts = () => calls.filter(call => call.method === "POST");
const owner = () => screen.getByRole("combobox", { name: "Mailbox owner" });
const create = () => screen.getByRole("button", { name: "Create mailbox" });
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
}
function identity(id = "owner-admin", nextTenant = tenant, role: "admin" | "super_admin" = "admin") {
  installSession(`${id}-${nextTenant}-${role}-token`, { id, tenant_id: nextTenant, role, email: `${id}@example.test`, display_name: id });
}
function RefreshDirectory() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(key => Array.isArray(key) && key[2] === "company-employees").catch(() => {}); }}>Refresh test directory</button>;
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
async function mount(wait = true) {
  render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><RefreshDirectory /><MailboxAdmin /></SWRConfig>);
  fireEvent.change(screen.getByLabelText("Mailbox local part"), { target: { value: "owner-fixture" } });
  await userEvent.selectOptions(screen.getByRole("combobox", { name: "Resource type" }), "personal");
  if (wait) await screen.findByRole("option", { name: "Alice owner · alice@example.test" });
  await settle();
}
async function selectAlice() { await userEvent.selectOptions(owner(), alice.id); expect(owner()).toHaveValue(alice.id); }
async function refresh() { await userEvent.click(screen.getByRole("button", { name: "Refresh test directory" })); await settle(); }
async function expectBlocked() {
  expect(create()).toBeDisabled();
  fireEvent.click(create()); await settle();
  expect(posts()).toHaveLength(0);
  expect(screen.getByLabelText("Mailbox local part")).toHaveValue("owner-fixture");
}
async function expectPersonal(ownerID = alice.id) {
  expect(create()).toBeEnabled(); await userEvent.click(create());
  await waitFor(() => expect(posts()).toHaveLength(1));
  expect(posts()[0].body).toEqual({ local_part: "owner-fixture", kind: "personal", owner_user_id: ownerID, retention_hours: 0 });
}

beforeEach(() => {
  calls = []; members = [{ ...alice }, { ...bob }];
  readMembers = async () => list(); readBoxes = async () => json({ data: [] });
  identity();
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const method = init?.method ?? "GET";
    calls.push({ path: url.pathname, method, body: init?.body ? JSON.parse(String(init.body)) : undefined, headers: new Headers(init?.headers), signal: init?.signal });
    if (url.pathname === "/api/v1/admin/users" && method === "GET") return readMembers(Number(url.searchParams.get("page")));
    if (url.pathname === "/api/v1/company/mailboxes" && method === "GET") return readBoxes();
    if (url.pathname === "/api/v1/company/mailboxes" && method === "POST") return json({ data: { id: "new-owner-mailbox" } });
    throw new Error(`Unexpected owner fixture request: ${method} ${url.pathname}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("personal mailbox owner eligibility", () => {
  it("waits for the initial directory before allowing an explicit owner selection", async () => {
    const pending = deferred(); readMembers = () => pending.promise;
    await mount(false);
    expect(owner()).toBeDisabled(); await expectBlocked();
    await act(async () => pending.resolve(list())); await settle();
    expect(owner()).toBeEnabled(); await selectAlice(); await expectPersonal();
  });

  it("creates a personal mailbox from an explicitly selected current active owner with retention zero", async () => {
    await mount(); await selectAlice(); await expectPersonal();
  });

  it.each(["admin", "super_admin"] as const)("does not invent a ban on active %s owners", async role => {
    members[0].role = role;
    await mount(); await selectAlice(); await expectPersonal();
  });

  it.each(["frozen", "removed", "foreign-tenant"] as const)("retires a %s selected owner and requires explicit reselection if that id becomes eligible again", async change => {
    await mount(); await selectAlice();
    members = change === "removed" ? [{ ...bob }] : [{ ...alice, ...(change === "frozen" ? { is_active: false } : { tenant_id: "another-company" }) }, { ...bob }];
    await refresh();
    expect(owner()).toHaveValue(""); await expectBlocked();
    members = [{ ...alice }, { ...bob }]; await refresh();
    expect(owner()).toHaveValue(""); await expectBlocked();
    await userEvent.selectOptions(owner(), bob.id); await expectPersonal(bob.id);
  });

  it("blocks creation during revalidation and keeps the selection if the successful directory still contains the owner", async () => {
    await mount(); await selectAlice();
    const pending = deferred(); readMembers = () => pending.promise;
    await refresh();
    expect(owner()).toBeDisabled(); await expectBlocked();
    await act(async () => pending.resolve(list())); await settle();
    expect(owner()).toBeEnabled(); expect(owner()).toHaveValue(alice.id); await expectPersonal();
  });

  it.each([503, 403, "network"] as const)("rejects a cached owner after directory %s and requires a successful read plus a new selection", async outcome => {
    await mount(); await selectAlice();
    readMembers = async () => { if (outcome === "network") throw new TypeError("Member directory network loss"); return failure(outcome); };
    await refresh();
    await screen.findByRole("alert");
    expect(owner()).toBeDisabled(); expect(owner()).toHaveValue(""); await expectBlocked();
    readMembers = async () => list();
    await userEvent.click(screen.getByRole("button", { name: "Retry loading" })); await settle();
    expect(owner()).toBeEnabled(); expect(owner()).toHaveValue(""); await expectBlocked();
    await selectAlice(); await expectPersonal();
  });

  it("does not accept a partial directory when a later page fails", async () => {
    await mount(); await selectAlice();
    readMembers = async page => page === 1 ? list([{ ...alice }], 101) : failure();
    await refresh();
    await screen.findByRole("alert");
    expect(owner()).toHaveValue(""); await expectBlocked();
    expect(calls.filter(call => call.path === "/api/v1/admin/users")).toHaveLength(3);
  });

  it.each(["removed", "failed-read"] as const)("does not revive a hidden personal owner after a %s directory refresh", async change => {
    await mount(); await selectAlice();
    await userEvent.selectOptions(screen.getByLabelText("Resource type"), "shared");
    if (change === "removed") members = [{ ...bob }]; else readMembers = async () => failure();
    await refresh();
    members = [{ ...alice }, { ...bob }]; readMembers = async () => list(); await refresh();
    await userEvent.selectOptions(screen.getByLabelText("Resource type"), "personal");
    expect(owner()).toHaveValue(""); await expectBlocked();
    await selectAlice(); await expectPersonal();
  });

  it.each(["loading", "failed"] as const)("keeps shared creation independent of a %s member directory", async state => {
    const pending = deferred(); readMembers = state === "loading" ? () => pending.promise : async () => failure();
    await mount(false);
    await userEvent.selectOptions(screen.getByLabelText("Resource type"), "shared");
    fireEvent.change(screen.getByLabelText("Retention hours (0 = permanent)"), { target: { value: "24" } });
    expect(create()).toBeEnabled(); await userEvent.click(create());
    await waitFor(() => expect(posts()).toHaveLength(1));
    expect(posts()[0].body).toEqual({ local_part: "owner-fixture", kind: "shared", retention_hours: 24 });
  });

  it("does not discard a valid owner because only the separate mailbox list failed", async () => {
    readBoxes = async () => failure();
    await mount(); await screen.findByRole("alert"); await selectAlice(); await expectPersonal();
  });

  it.each(["account", "tenant", "role"] as const)("does not carry an owner selection across a %s boundary even if the same id is listed again", async boundary => {
    await mount(); await selectAlice();
    const nextTenant = boundary === "tenant" ? "new-owner-company" : tenant;
    members = [{ ...alice, tenant_id: nextTenant }, { ...bob, tenant_id: nextTenant }];
    act(() => identity(boundary === "account" ? "new-owner-admin" : "owner-admin", nextTenant, boundary === "role" ? "super_admin" : "admin"));
    await settle();
    await screen.findByRole("option", { name: "Alice owner · alice@example.test" });
    expect(owner()).toHaveValue(""); await expectBlocked();
    await selectAlice(); await expectPersonal();
  });

  it("retains the selected owner through token-only rotation within the same session", async () => {
    await mount(); await selectAlice();
    const scope = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "owner-rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope); expect(owner()).toHaveValue(alice.id); await expectPersonal();
  });
});
