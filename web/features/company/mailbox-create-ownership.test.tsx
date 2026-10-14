import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import MailboxesPage from "@/app/(dashboard)/company/mailboxes/page";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AdminUser, AuthUser, Mailbox } from "@/lib/types";
import type { WorkMailbox } from "@/lib/company";

// Actual company mailbox route, form, selectors, SWR and request transport.
// Synthetic HTTP responses may finish after navigation despite an abort.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({ usePathname: () => "/company/mailboxes" }));
const tenant = "creation-company";
const alice: AdminUser = { id: "create-owner-alice", tenant_id: tenant, role: "user", is_active: true,
  display_name: "Alice Owner", email: "alice@example.test", created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z" };
const bob: AdminUser = { ...alice, id: "create-owner-bob", display_name: "Bob Owner", email: "bob@example.test" };
const mailbox = (id: string): WorkMailbox => ({ mailbox: { id, kind: "shared", tenant_id: tenant,
  full_address: `${id}@company.test`, send_policy: "free", owner_user_id: alice.id } as Mailbox,
  revision: 3, can_read: false, can_organize: false, can_send: false, template_only: false });
const existing = [mailbox("existing-a"), mailbox("existing-b")];
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: {
  code: status === 403 ? "FORBIDDEN" : "UNAVAILABLE", message: `Synthetic mailbox ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
const created = () => json({ id: "new-mailbox", full_address: "new-mailbox@company.test" });
type Call = { path: string; method: string; body: Record<string, unknown>; tenant: string | null };
let calls: Call[];
let members: AdminUser[];
let readMembers: () => Promise<Response>;
let readBoxes: () => Promise<Response>;
let createReply: () => Promise<Response>;
let pending: Array<(value: Response) => void>;
const posts = () => calls.filter(call => call.method === "POST");
const boxReads = () => calls.filter(call => call.method === "GET" && call.path === "/api/v1/company/mailboxes");
const create = () => screen.getByRole("button", { name: "Create mailbox" });
const local = () => screen.getByLabelText("Mailbox local part");
const kind = () => screen.getByLabelText("Resource type");
const owner = () => screen.getByLabelText("Mailbox owner");
const retention = () => screen.getByLabelText("Retention hours (0 = permanent)");
const change = (field: HTMLElement, value: string) => fireEvent.change(field, { target: { value } });
const listMembers = () => new Response(JSON.stringify({ data: members,
  meta: { page: 1, per_page: 100, total: members.length } }), { headers: { "Content-Type": "application/json" } });
function identity(id = "create-admin", company = tenant, role: AuthUser["role"] = "admin") {
  installSession(`${id}-${company}-${role}`, { id, tenant_id: company, role, email: `${id}@example.test`, display_name: id });
}
function delayed() {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve); return { promise, resolve };
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function RefreshDirectory() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(key => Array.isArray(key) && key[1] === sessionScope() &&
    key[2] === "company-employees").catch(() => undefined); }}>Refresh owner directory</button>;
}
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><MailboxesPage /><RefreshDirectory /></SWRConfig>);
  await screen.findByRole("option", { name: "existing-a@company.test · shared" }); await settle();
  change(local(), "requested-local"); change(retention(), "24"); return view;
}
async function start(personal = false) {
  const view = await mount();
  if (personal) { change(kind(), "personal"); change(owner(), alice.id); }
  fireEvent.click(create()); await waitFor(() => expect(posts()).toHaveLength(1)); return view;
}
beforeEach(() => {
  calls = []; members = [{ ...alice }, { ...bob }]; pending = []; identity();
  readMembers = async () => listMembers(); readBoxes = async () => json(existing); createReply = async () => created();
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : {}, tenant: new Headers(init?.headers).get("X-Tenant-ID") };
    calls.push(call);
    if (call.path === "/api/v1/admin/users" && call.method === "GET") return readMembers();
    if (call.path === "/api/v1/company/mailboxes" && call.method === "GET") return readBoxes();
    if (call.path === "/api/v1/company/mailboxes" && call.method === "POST") return createReply();
    if (call.path.endsWith("/grants") && call.method === "GET") return json({ revision: 3, grants: [] });
    throw new Error(`Unexpected mailbox creation request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(json([])))); vi.unstubAllGlobals(); });

describe("mailbox creation request and draft ownership", () => {
  it("clears an unchanged successful local part while retaining resource type and retention", async () => {
    await start(); await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Private mailbox created"));
    expect(posts()[0]).toEqual({ path: "/api/v1/company/mailboxes", method: "POST", tenant,
      body: { local_part: "requested-local", kind: "shared", retention_hours: 24 } });
    expect(local()).toHaveValue(""); expect(kind()).toHaveValue("shared"); expect(retention()).toHaveValue(24);
  });

  it("keeps a newer local part when the previous creation succeeds", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start(); change(local(), "next-local");
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue("next-local"); expect(posts()).toHaveLength(1);
  });

  it.each(["local part", "retention", "resource type"])("does not clear a new A → B → A %s intent", async field => {
    const gate = delayed(); createReply = () => gate.promise; await start();
    if (field === "local part") { change(local(), "new-local"); change(local(), "requested-local"); }
    if (field === "retention") { change(retention(), "72"); change(retention(), "24"); }
    if (field === "resource type") { change(kind(), "personal"); change(kind(), "shared"); }
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue("requested-local"); expect(kind()).toHaveValue("shared"); expect(retention()).toHaveValue(24);
  });

  it("keeps a different personal owner and the new resource intent after a pending shared creation", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start();
    change(kind(), "personal"); change(owner(), bob.id);
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue("requested-local"); expect(kind()).toHaveValue("personal"); expect(owner()).toHaveValue(bob.id);
    expect(posts()[0].body).toEqual({ local_part: "requested-local", kind: "shared", retention_hours: 24 });
  });

  it("keeps an A → B → A owner reselection independent of an older personal creation", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start(true);
    change(owner(), bob.id); change(owner(), alice.id);
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue("requested-local"); expect(owner()).toHaveValue(alice.id);
    expect(posts()[0].body).toEqual({ local_part: "requested-local", kind: "personal", owner_user_id: alice.id, retention_hours: 0 });
  });

  it("does not clear the unfinished local part when a directory refresh retires the submitted owner", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start(true);
    members = [{ ...bob }]; fireEvent.click(screen.getByRole("button", { name: "Refresh owner directory" }));
    await waitFor(() => expect(owner()).toHaveValue(""));
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue("requested-local"); expect(owner()).toHaveValue(""); expect(create()).toBeDisabled();
  });

  it("preserves a newer managed-mailbox selection when creation finishes", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start();
    change(screen.getByLabelText("Manage mailbox"), "existing-b");
    await act(async () => gate.resolve(created()));
    expect(screen.getByLabelText("Manage mailbox")).toHaveValue("existing-b");
  });

  it.each([false, true])("suppresses a retired page's creation side effects (failure=%s)", async error => {
    const gate = delayed(); createReply = () => gate.promise; const view = await start();
    view.unmount(); const count = boxReads().length;
    await act(async () => gate.resolve(error ? failed() : created()));
    expect(boxReads()).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(["account", "tenant", "role"])("lets the new %s create while an older session request remains pending", async boundary => {
    const first = delayed(); const second = delayed(); createReply = () => posts().length === 1 ? first.promise : second.promise;
    await start(); const company = boundary === "tenant" ? "replacement-company" : tenant;
    members = members.map(member => ({ ...member, tenant_id: company }));
    act(() => identity(boundary === "account" ? "replacement-admin" : "create-admin", company, boundary === "role" ? "super_admin" : "admin"));
    await settle(); expect(local()).toHaveValue("requested-local"); expect(retention()).toHaveValue(24);
    change(local(), "replacement-local"); expect(create()).toBeEnabled(); fireEvent.click(create());
    await waitFor(() => expect(posts()).toHaveLength(2));
    await act(async () => first.resolve(created()));
    expect(create()).toBeDisabled(); expect(local()).toHaveValue("replacement-local"); expect(toast.success).not.toHaveBeenCalled();
    await act(async () => second.resolve(created()));
    expect(local()).toHaveValue(""); expect(toast.success).toHaveBeenCalledTimes(1);
    expect(posts()[1].tenant).toBe(company);
  });

  it("keeps an active creation and its busy state across token-only rotation", async () => {
    const gate = delayed(); createReply = () => gate.promise; await start(); const before = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "rotation-only-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(before); expect(create()).toBeDisabled();
    await act(async () => gate.resolve(created()));
    expect(local()).toHaveValue(""); expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("blocks synchronous duplicate clicks without sending a second non-idempotent create", async () => {
    const gate = delayed(); createReply = () => gate.promise; await mount(); const button = create();
    act(() => { fireEvent.click(button); fireEvent.click(button); });
    expect(posts()).toHaveLength(1); await act(async () => gate.resolve(created())); expect(posts()).toHaveLength(1);
  });

  it("keeps current input and owner after a rejected personal creation", async () => {
    createReply = async () => failed(); await start(true);
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic mailbox 503"));
    expect(local()).toHaveValue("requested-local"); expect(owner()).toHaveValue(alice.id); expect(create()).toBeEnabled();
  });

  it("keeps an acknowledged creation separate from failed mailbox-list readback and retries only reads", async () => {
    await mount(); readBoxes = async () => failed(); fireEvent.click(create());
    await screen.findByRole("alert");
    expect(screen.getByText("The mailbox was created, but the mailbox list could not be refreshed. Retry loading to check the current list.")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Private mailbox created"); expect(posts()).toHaveLength(1);
    readBoxes = async () => json(existing); fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument()); expect(posts()).toHaveLength(1);
  });

  it("does not clear edits made while the post-create list refresh is pending", async () => {
    const gate = delayed(); await mount(); readBoxes = () => gate.promise; fireEvent.click(create());
    await waitFor(() => expect(boxReads()).toHaveLength(2)); change(local(), "typed-during-readback");
    await act(async () => gate.resolve(json(existing))); expect(local()).toHaveValue("typed-during-readback");
  });
});
