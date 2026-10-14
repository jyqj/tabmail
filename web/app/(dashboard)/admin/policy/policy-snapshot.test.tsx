import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser, SMTPPolicyInput } from "@/lib/types";
import AdminPolicyPage from "./page";

// Exercise the shipping page, Base UI, SWR, session and GET/PATCH API facade.
// Fetch replies and toast are synthetic; this is not an HTTP/backend/browser gate.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const path = "/api/v1/admin/policy";
const tenant = "10000000-0000-4000-8000-000000000001";
const actor: AuthUser = { id: "20000000-0000-4000-8000-000000000001", tenant_id: tenant, role: "super_admin", email: "operator@example.test", display_name: "Operator" };
const original: SMTPPolicyInput = {
  default_accept: false, accept_domains: ["allowed.example.test"], reject_domains: ["blocked.example.test"],
  default_store: false, store_domains: ["stored.example.test"], discard_domains: ["discarded.example.test"],
  reject_origin_domains: ["origin.example.test"],
};
const fresh: SMTPPolicyInput = { ...original, default_accept: true, reject_domains: ["new-blocked.example.test"] };
const effective = { can_send: false, daily_send_quota: 0, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: [], can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
const reply = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 500) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: "Controlled policy failure" } }), { status, headers: { "Content-Type": "application/json" } });
function deferred() { let resolve!: (response: Response) => void; const promise = new Promise<Response>(done => { resolve = done; }); return { promise, resolve }; }
let reads: { authorization: string | null; tenant: string | null }[];
let writes: { path: string; method: string; body: SMTPPolicyInput; authorization: string | null; tenant: string | null }[];
let readReply: () => Promise<Response>;
let writeReply: () => Promise<Response>;
function Controls() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(key => Array.isArray(key) && key[0] === "session" && key[1] === sessionScope() && key[2] === "smtp-policy").catch(() => {}); }}>Revalidate policy cache</button>;
}
function changeScope(boundary: "account" | "tenant" | "role") {
  if (boundary === "tenant") {
    localStorage.setItem("tabmail_tenant_id", "10000000-0000-4000-8000-000000000002"); window.dispatchEvent(new Event(AUTH_EVENT));
  } else installSession("new-policy-token", { ...actor, id: boundary === "account" ? "new-operator" : actor.id, role: boundary === "role" ? "admin" : actor.role });
}
beforeEach(() => {
  reads = []; writes = []; readReply = async () => reply(original); writeReply = async () => reply(original);
  installSession("policy-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost"), method = init?.method ?? "GET", headers = new Headers(init?.headers);
    if (url.pathname === "/api/v1/auth/me/permissions") return reply(effective);
    if (url.pathname !== path) throw new Error(`Unexpected policy request: ${method} ${url.pathname}`);
    const identity = { authorization: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID") };
    if (method === "GET") { reads.push(identity); return readReply(); }
    writes.push({ path: url.pathname, method, body: JSON.parse(String(init?.body)), ...identity }); return writeReply();
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function mount() {
  return render(<AuthProvider><SWRConfig value={{ shouldRetryOnError: false, dedupingInterval: 0 }}><Controls /><SidebarProvider><AdminPolicyPage /></SidebarProvider></SWRConfig></AuthProvider>);
}
const save = () => screen.getByRole("button", { name: "Save Policy" });
const input = (index = 0) => screen.getAllByRole("textbox")[index];
async function ready() { mount(); await screen.findByDisplayValue("allowed.example.test"); await waitFor(() => expect(save()).toBeEnabled()); }
async function refresh() { await userEvent.click(screen.getByRole("button", { name: "Revalidate policy cache" })); await settle(); }
async function retry() { await userEvent.click(screen.getByRole("button", { name: "Retry loading" })); await settle(); }
async function edit(value = "typed.example.test") { fireEvent.change(input(), { target: { value } }); }
function assertWrite(body: SMTPPolicyInput = original) {
  expect(writes).toHaveLength(1); expect(writes[0]).toEqual({ path, method: "PATCH", body, authorization: "Bearer policy-token", tenant });
}

it("does not save defaults before the first GET supplies a successful snapshot", async () => {
  const pending = deferred(); readReply = () => pending.promise; mount(); await waitFor(() => expect(reads).toHaveLength(1));
  expect(save()).toBeDisabled(); await userEvent.click(save()); expect(writes).toHaveLength(0);
  await act(async () => pending.resolve(reply(original))); await screen.findByDisplayValue("blocked.example.test"); expect(save()).toBeEnabled();
});
it.each(["500", "403", "network"])("blocks first-load %s failures and offers an explicit read retry", async kind => {
  readReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(Number(kind)); };
  mount(); await settle(); await userEvent.click(save()); await settle(); expect(writes).toHaveLength(0);
  expect(save()).toBeDisabled(); expect(screen.getByRole("alert")).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Retry loading" })).toBeEnabled();
});
it.each([null, {}, { ...original, default_accept: "false" }])("does not admit an incomplete successful response %j as a writable snapshot", async data => {
  readReply = async () => reply(data); mount(); await settle(); await screen.findByRole("alert"); expect(save()).toBeDisabled(); expect(writes).toHaveLength(0);
});
it("accepts explicit false and backend null lists, and sends exact empty arrays", async () => {
  const empty = { default_accept: false, default_store: false, accept_domains: [], reject_domains: [], store_domains: [], discard_domains: [], reject_origin_domains: [] };
  readReply = async () => reply({ ...empty, accept_domains: null, reject_domains: null, store_domains: null, discard_domains: null, reject_origin_domains: null });
  mount(); await waitFor(() => expect(save()).toBeEnabled()); await userEvent.click(save()); await settle(); assertWrite(empty);
});
it("retries the GET without automatically writing, then saves the recovered policy", async () => {
  readReply = async () => failure(); mount(); await settle(); readReply = async () => reply(original); await retry();
  await screen.findByDisplayValue("blocked.example.test"); expect(writes).toHaveLength(0); await userEvent.click(save()); await settle(); assertWrite();
});
it("keeps a repeated read failure blocked and recoverable", async () => {
  readReply = async () => failure(); mount(); await settle(); await retry(); expect(reads).toHaveLength(2);
  expect(screen.getByRole("alert")).toBeInTheDocument(); expect(save()).toBeDisabled(); expect(writes).toHaveLength(0);
});
it("blocks writes and controls during refresh while retaining the current input", async () => {
  await ready(); await edit(); const pending = deferred(); readReply = () => pending.promise; await refresh();
  expect(save()).toBeDisabled(); expect(input()).toHaveValue("typed.example.test");
  for (const element of screen.getAllByRole("textbox")) expect(element).toBeDisabled();
  for (const element of screen.getAllByRole("switch")) expect(element).toHaveAttribute("aria-disabled", "true");
  await userEvent.click(save()); expect(writes).toHaveLength(0);
  await act(async () => pending.resolve(reply(fresh))); await waitFor(() => expect(save()).toBeEnabled());
  expect(input()).toHaveValue("typed.example.test"); expect(input(1)).toHaveValue("new-blocked.example.test");
  await userEvent.click(save()); await settle(); assertWrite({ ...fresh, accept_domains: ["typed.example.test"] });
});
it("refreshes an untouched form from the new successful snapshot", async () => {
  await ready(); readReply = async () => reply(fresh); await refresh();
  expect(input(1)).toHaveValue("new-blocked.example.test"); await userEvent.click(save()); await settle(); assertWrite(fresh);
});
it("preserves explicit false and cleared-list edits across a refresh", async () => {
  readReply = async () => reply({ ...original, default_accept: true }); await ready();
  await userEvent.click(screen.getAllByRole("switch")[0]); await edit(""); readReply = async () => reply(fresh); await refresh();
  expect(input()).toHaveValue(""); expect(screen.getAllByRole("switch")[0]).toHaveAttribute("aria-checked", "false");
  await userEvent.click(save()); await settle(); assertWrite({ ...fresh, default_accept: false, accept_domains: [] });
});
it("preserves edited text on refresh failure and recovers only after a new GET succeeds", async () => {
  await ready(); await edit("one.example.test,  two.example.test"); readReply = async () => failure(); await refresh();
  expect(input()).toHaveValue("one.example.test,  two.example.test"); expect(save()).toBeDisabled(); expect(screen.getByRole("alert")).toBeInTheDocument();
  readReply = async () => reply(fresh); await retry(); expect(input()).toHaveValue("one.example.test,  two.example.test");
  await userEvent.click(save()); await settle(); assertWrite({ ...fresh, accept_domains: ["one.example.test", "two.example.test"] });
});
it("locks all inputs and deduplicates an in-flight save", async () => {
  await ready(); await edit(); const pending = deferred(); writeReply = () => pending.promise;
  const button = save(); fireEvent.click(button); fireEvent.click(button); await settle(); expect(writes).toHaveLength(1);
  for (const element of screen.getAllByRole("textbox")) expect(element).toBeDisabled();
  for (const element of screen.getAllByRole("switch")) expect(element).toHaveAttribute("aria-disabled", "true");
  await act(async () => pending.resolve(reply({ ...original, accept_domains: ["typed.example.test"] })));
  await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));
});
it.each(["500", "network"])("keeps submitted edits after a save %s failure without replaying the PATCH", async kind => {
  await ready(); await edit(); writeReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(); };
  await userEvent.click(save()); await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
  expect(input()).toHaveValue("typed.example.test"); expect(save()).toBeEnabled(); expect(writes).toHaveLength(1);
  writeReply = async () => reply({ ...original, accept_domains: ["typed.example.test"] }); await userEvent.click(save()); await settle(); expect(writes).toHaveLength(2);
});
it("revalidates after an acknowledged save and blocks on a failed follow-up GET", async () => {
  await ready(); await edit(); readReply = async () => failure(); writeReply = async () => reply({ ...original, accept_domains: ["typed.example.test"] });
  await userEvent.click(save()); await waitFor(() => expect(reads).toHaveLength(2)); await settle();
  expect(toast.success).toHaveBeenCalledTimes(1); expect(input()).toHaveValue("typed.example.test"); expect(save()).toBeDisabled();
  expect(screen.getByRole("alert")).toBeInTheDocument(); expect(writes).toHaveLength(1);
});
it.each(["account", "tenant", "role"] as const)("retires the old draft and synchronously blocks old callbacks on %s changes", async boundary => {
  await ready(); await edit(); const oldSave = save(), pending = deferred(); readReply = () => pending.promise;
  act(() => { changeScope(boundary); fireEvent.click(oldSave); }); await settle(); expect(writes).toHaveLength(0);
  expect(save()).toBeDisabled(); expect(screen.queryByDisplayValue("typed.example.test")).not.toBeInTheDocument();
  await act(async () => pending.resolve(reply(fresh))); await screen.findByDisplayValue("new-blocked.example.test"); expect(save()).toBeEnabled();
});
it("keeps the current draft through token-only rotation and uses the latest token", async () => {
  await ready(); await edit(); const before = sessionScope();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-policy-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  expect(sessionScope()).toBe(before); expect(input()).toHaveValue("typed.example.test"); await userEvent.click(save()); await settle();
  expect(writes).toHaveLength(1); expect(writes[0].authorization).toBe("Bearer rotated-policy-token"); expect(writes[0].body.accept_domains).toEqual(["typed.example.test"]);
});
describe.each(["success", "failure"] as const)("late policy %s", outcome => {
  it("does not accept an old session's initial read", async () => {
    const old = deferred(); readReply = () => old.promise; mount(); await waitFor(() => expect(reads).toHaveLength(1));
    readReply = async () => reply(fresh); act(() => changeScope("account")); await screen.findByDisplayValue("new-blocked.example.test");
    await act(async () => old.resolve(outcome === "success" ? reply(original) : failure())); await settle();
    expect(input(1)).toHaveValue("new-blocked.example.test"); expect(toast.error).not.toHaveBeenCalled(); expect(writes).toHaveLength(0);
  });
  it("does not let an old save toast or revalidate the new session", async () => {
    await ready(); await edit(); const pending = deferred(); writeReply = () => pending.promise; await userEvent.click(save());
    readReply = async () => reply(fresh); act(() => changeScope("account")); await screen.findByDisplayValue("new-blocked.example.test"); await settle(); const count = reads.length;
    await act(async () => pending.resolve(outcome === "success" ? reply(original) : failure())); await settle();
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(reads).toHaveLength(count); expect(input(1)).toHaveValue("new-blocked.example.test");
  });
  it("ignores save feedback and cache refresh after the page unmounts", async () => {
    const page = mount(); await screen.findByDisplayValue("allowed.example.test"); const pending = deferred(); writeReply = () => pending.promise;
    await userEvent.click(save()); const count = reads.length; page.unmount();
    await act(async () => pending.resolve(outcome === "success" ? reply(original) : failure())); await settle();
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(reads).toHaveLength(count);
  });
});
