import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { useLayoutEffect } from "react";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import TenantsPage from "./page";

// Actual page, Base UI menu/dialog, inputs, SWR, session and API facade.
// Only fetch replies and toast display are synthetic; this is not backend/PG evidence.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const fields = ["max_domains", "max_mailboxes_per_domain", "max_messages_per_mailbox", "max_message_bytes", "retention_hours", "rpm_limit", "daily_quota"] as const;
type Field = typeof fields[number];
type Values = Record<Field, number | null>;
const a = "10000000-0000-4000-8000-000000000001", b = "10000000-0000-4000-8000-000000000002";
const actor: AuthUser = { id: "operator", tenant_id: "platform", role: "super_admin", email: "operator@example.test", display_name: "Operator" };
const original: Values = { max_domains: 7, max_mailboxes_per_domain: null, max_messages_per_mailbox: 203, max_message_bytes: 1048576, retention_hours: 0, rpm_limit: -1, daily_quota: 901 };
const inherited = Object.fromEntries(fields.map(key => [key, null])) as Values;
const effective = { max_domains: 7, max_mailboxes_per_domain: 100, max_messages_per_mailbox: 203, max_message_bytes: 1048576, retention_hours: 0, rpm_limit: -1, daily_quota: 901 };
const rawPath = (id = a) => `/api/v1/admin/tenants/${id}`;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 500) => new Response(JSON.stringify({ error: { code: "CONTROLLED", message: "Controlled override failure" } }), { status, headers: { "Content-Type": "application/json" } });
const raw = (id = a, values: Values = original) => json({ tenant_id: id, ...values });
function deferred() { let resolve!: (value: Response) => void; const promise = new Promise<Response>(done => { resolve = done; }); return { promise, resolve }; }
let server: Record<string, Values>;
let reads: string[];
let writes: { path: string; method: string; body: Values; authorization: string | null }[];
let rawReply: (id: string) => Promise<Response>;
let effectiveReply: (id: string) => Promise<Response>;
let writeReply: (id: string, body: Values) => Promise<Response>;
let refreshRaw: () => void;
function CacheControl() {
  const { mutate } = useSWRConfig();
  useLayoutEffect(() => {
    refreshRaw = () => { void mutate(key => Array.isArray(key) && key[0] === "session" && key[1] === sessionScope() && Array.isArray(key[2]) && key[2][0] === "tenant-overrides").catch(() => {}); };
  }, [mutate]);
  return null;
}
beforeEach(() => {
  server = { [a]: { ...original }, [b]: { ...original, max_domains: 81 } }; reads = []; writes = [];
  rawReply = async id => raw(id, server[id]); effectiveReply = async () => json(effective);
  writeReply = async (id, body) => { server[id] = { ...body }; return json({ tenant_id: id, ...body }); };
  installSession("override-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname, method = init?.method ?? "GET";
    if (path === "/api/v1/auth/me/permissions") return json({});
    if (path === "/api/v1/admin/plans") return json([]);
    if (path === "/api/v1/admin/tenants") return json([a, b].map((id, i) => ({ id, name: `Tenant ${i ? "B" : "A"}`, plan_id: "plan", is_super: false, created_at: "2026-10-08T00:00:00Z" })));
    const match = path.match(/^\/api\/v1\/admin\/tenants\/([^/]+)(\/config)?$/);
    if (!match) throw new Error(`Unexpected override request: ${method} ${path}`);
    const [, id, config] = match;
    if (method === "GET") { reads.push(path); return config ? effectiveReply(id) : rawReply(id); }
    if (method !== "PATCH" || config) throw new Error(`Unexpected override method: ${method} ${path}`);
    const body = JSON.parse(String(init?.body)); writes.push({ path, method, body, authorization: new Headers(init?.headers).get("Authorization") }); return writeReply(id, body);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 35; i++) await Promise.resolve(); }); }
async function mount() {
  const view = render(<AuthProvider><SWRConfig value={{ shouldRetryOnError: false, dedupingInterval: 0, revalidateOnFocus: false }}><CacheControl /><SidebarProvider><TenantsPage /></SidebarProvider></SWRConfig></AuthProvider>);
  await screen.findByText("Tenant A"); return view;
}
async function open(id = a) {
  const row = screen.getByText(id === a ? "Tenant A" : "Tenant B").closest("tr")!;
  await userEvent.click(within(row).getByRole("button"));
  await userEvent.click(await screen.findByRole("menuitem", { name: "Overrides" }));
  await screen.findByRole("dialog"); await settle();
}
const dialog = () => screen.getByRole("dialog");
const field = (key: Field = "max_domains") => within(dialog()).getAllByPlaceholderText("inherit")[fields.indexOf(key)] as HTMLInputElement;
const edit = (value: string, key: Field = "max_domains") => fireEvent.change(field(key), { target: { value } });
const save = () => within(dialog()).getByRole("button", { name: /Save Overrides|Saving\.\.\./ });
async function submit() { fireEvent.click(save()); await settle(); }
async function close() { await userEvent.click(within(dialog()).getByRole("button", { name: "Close" })); await settle(); }
async function ready() { const view = await mount(); await open(); return view; }
async function refresh() { act(() => refreshRaw()); await settle(); }
async function retry() { fireEvent.click(within(dialog()).getByRole("button", { name: "Retry" })); await settle(); }
function changeScope(kind: "account" | "tenant" | "role") {
  if (kind === "tenant") { localStorage.setItem("tabmail_tenant_id", "replacement-platform"); window.dispatchEvent(new Event(AUTH_EVENT)); }
  else installSession("new-override-token", { ...actor, id: kind === "account" ? "replacement-operator" : actor.id, role: kind === "role" ? "admin" : actor.role });
}

it("loads the raw nullable snapshot independently of effective plan values", async () => {
  await ready(); expect(reads).toContain(rawPath());
  for (const key of fields) expect(field(key)).toHaveValue(original[key]);
  expect(save()).toBeEnabled(); expect(writes).toEqual([]);
});
it("an absent persisted override is seven explicit inheritance values, never effective defaults", async () => {
  server[a] = { ...inherited }; await ready();
  for (const key of fields) expect(field(key)).toHaveValue(null);
  await submit(); expect(writes[0].body).toEqual(inherited);
});
it.each(fields)("editing only %s preserves every other nullable raw value in the replacement PATCH", async key => {
  await ready(); edit("17", key); await submit();
  expect(writes).toEqual([{ path: rawPath(), method: "PATCH", body: { ...original, [key]: 17 }, authorization: "Bearer override-token" }]);
});
it("preserves explicit clear, zero, signed integer and large finite retention commands", async () => {
  await ready(); edit(""); edit("0", "max_mailboxes_per_domain"); edit("-2147483648", "rpm_limit"); edit("2147483647", "daily_quota"); edit("3000000", "retention_hours"); await submit();
  expect(writes[0].body).toEqual({ ...original, max_domains: null, max_mailboxes_per_domain: 0, rpm_limit: -2147483648, daily_quota: 2147483647, retention_hours: 3000000 });
});
it("blocks submission until the first raw read completes even when effective values are available", async () => {
  const pending = deferred(); rawReply = () => pending.promise; await ready();
  expect(save()).toBeDisabled(); await submit(); expect(writes).toEqual([]);
  await act(async () => pending.resolve(raw())); await settle(); expect(field()).toHaveValue(7); expect(save()).toBeEnabled();
});
it.each(["500", "403", "network"])("shows initial raw %s failure and retries only the read", async kind => {
  rawReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(Number(kind)); };
  await ready(); expect(save()).toBeDisabled(); expect(within(dialog()).getByRole("alert")).toBeInTheDocument();
  rawReply = async () => raw(); await retry(); expect(field()).toHaveValue(7); expect(save()).toBeEnabled(); expect(writes).toEqual([]);
});
it.each(["missing", "tenant", "fraction", "overflow", "null"])("does not treat a malformed %s snapshot as authoritative inheritance", async kind => {
  const data: Record<string, unknown> = { tenant_id: a, ...original };
  if (kind === "missing") delete data.daily_quota;
  if (kind === "tenant") data.tenant_id = b;
  if (kind === "fraction") data.rpm_limit = 0.5;
  if (kind === "overflow") data.max_domains = 2147483648;
  rawReply = async () => json(kind === "null" ? null : data); await ready();
  expect(save()).toBeDisabled(); expect(within(dialog()).getByRole("alert")).toBeInTheDocument(); await submit(); expect(writes).toEqual([]);
});
it("pristine fields follow raw background changes and preserve the refreshed whole snapshot", async () => {
  await ready(); server[a].max_domains = 29; await refresh(); expect(field()).toHaveValue(29); await submit(); expect(writes[0].body).toEqual(server[a]);
});
it("merges an explicit dirty field over a fresh snapshot without reversing untouched server changes", async () => {
  await ready(); edit("19"); server[a] = { ...original, max_domains: 29, daily_quota: 990 }; await refresh();
  expect(field()).toHaveValue(19); expect(field("daily_quota")).toHaveValue(990); await submit(); expect(writes[0].body).toEqual({ ...server[a], max_domains: 19 });
});
it("a reverted edit becomes pristine and follows a subsequent raw snapshot", async () => {
  await ready(); edit("19"); edit("7"); server[a].max_domains = 29; await refresh(); expect(field()).toHaveValue(29);
});
it("a pending raw refresh blocks writes while retaining the current explicit draft", async () => {
  await ready(); edit("19"); const pending = deferred(); rawReply = () => pending.promise; await refresh();
  expect(save()).toBeDisabled(); expect(field()).toHaveValue(19); await submit(); expect(writes).toEqual([]);
  await act(async () => pending.resolve(raw(a, { ...original, daily_quota: 990 }))); await settle(); expect(save()).toBeEnabled();
});
it("a failed background read blocks stale replacement, retains edits and recovers through retry", async () => {
  await ready(); edit("19"); rawReply = async () => failure(); await refresh(); expect(save()).toBeDisabled(); expect(field()).toHaveValue(19);
  rawReply = async () => raw(a, { ...original, daily_quota: 990 }); await retry(); await submit(); expect(writes[0].body).toEqual({ ...original, max_domains: 19, daily_quota: 990 });
});
it.each(["400", "500", "network"])("a PATCH %s failure preserves all draft values and allows explicit retry", async kind => {
  await ready(); edit("19"); writeReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(Number(kind)); }; await submit();
  expect(field()).toHaveValue(19); expect(save()).toBeEnabled(); expect(toast.error).toHaveBeenCalled();
  writeReply = async (id, body) => { server[id] = body; return json({ ...body, tenant_id: id }); }; await submit(); expect(writes).toHaveLength(2); expect(writes[1].body).toEqual(writes[0].body);
});
it("disables inputs and deduplicates saves while the replacement PATCH is pending", async () => {
  await ready(); edit("19"); const pending = deferred(); writeReply = () => pending.promise; fireEvent.click(save()); fireEvent.click(save()); await settle();
  expect(writes).toHaveLength(1); expect(save()).toBeDisabled(); for (const key of fields) expect(field(key)).toBeDisabled();
  server[a].max_domains = 19; await act(async () => pending.resolve(raw(a, server[a]))); await settle(); expect(field()).toBeEnabled();
});
it.each(["raw", "effective"])("does not reclassify an acknowledged PATCH when its subsequent %s read fails", async kind => {
  await ready(); edit("19"); if (kind === "raw") rawReply = async () => failure(); else effectiveReply = async () => failure(); await submit();
  expect(writes).toHaveLength(1); expect(toast.success).toHaveBeenCalledTimes(1); expect(field()).toHaveValue(19);
  expect(within(dialog()).getByRole("alert")).toBeInTheDocument(); expect(toast.error).not.toHaveBeenCalled();
  if (kind === "raw") expect(save()).toBeDisabled();
});
it.each(["success", "failure"])("ignores a closed tenant A read %s after tenant B is opened", async outcome => {
  const pending = deferred(); rawReply = id => id === a ? pending.promise : Promise.resolve(raw(id, server[id])); await ready(); await close(); await open(b);
  await act(async () => pending.resolve(outcome === "success" ? raw(a, { ...original, max_domains: 999 }) : failure())); await settle();
  expect(field()).toHaveValue(81); expect(toast.error).not.toHaveBeenCalled(); await submit(); expect(writes[0].path).toBe(rawPath(b));
});
it("treats tenant A to B to A as three separate read lifetimes", async () => {
  const pending = deferred(); let count = 0; rawReply = id => id === a && ++count === 1 ? pending.promise : Promise.resolve(raw(id, server[id]));
  await ready(); await close(); await open(b); await close(); server[a].max_domains = 39; await open(a);
  await act(async () => pending.resolve(raw(a, { ...original, max_domains: 999 }))); await settle(); expect(field()).toHaveValue(39);
  expect(reads.filter(path => path === rawPath(a))).toHaveLength(2);
});
it.each(["success", "failure"])("ignores a previous dialog's late PATCH %s after the same tenant reopens", async outcome => {
  await ready(); edit("19"); const pending = deferred(); writeReply = () => pending.promise; await submit(); await close(); server[a].max_domains = 39; await open(); const count = reads.length;
  await act(async () => pending.resolve(outcome === "success" ? raw(a, { ...original, max_domains: 19 }) : failure())); await settle();
  expect(field()).toHaveValue(39); expect(reads).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it.each(["account", "tenant", "role"] as const)("retires the dialog and synchronous old save callback on %s replacement", async kind => {
  await ready(); edit("19"); const oldSave = save(); act(() => { changeScope(kind); fireEvent.click(oldSave); }); await settle();
  expect(writes).toEqual([]); expect(screen.queryByRole("dialog")).not.toBeInTheDocument();
});
it.each(["read", "write"])("ignores a late %s after the session returns to the original account", async kind => {
  const pending = deferred(); if (kind === "read") rawReply = () => pending.promise;
  await ready(); if (kind === "write") { edit("19"); writeReply = () => pending.promise; await submit(); }
  act(() => { installSession("other-token", { ...actor, id: "other" }); installSession("returned-token", actor); }); await settle();
  await act(async () => pending.resolve(failure())); await settle(); expect(screen.queryByRole("dialog")).not.toBeInTheDocument(); expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
});
it("token-only rotation keeps the draft and submits with the current token", async () => {
  await ready(); edit("19"); const scope = sessionScope(); act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); }); await settle();
  expect(sessionScope()).toBe(scope); expect(field()).toHaveValue(19); await submit(); expect(writes[0].authorization).toBe("Bearer rotated-token");
});
it.each(["success", "failure"])("ignores a late PATCH %s after page unmount", async outcome => {
  const view = await ready(); edit("19"); const pending = deferred(); writeReply = () => pending.promise; await submit(); const count = reads.length; view.unmount();
  await act(async () => pending.resolve(outcome === "success" ? raw() : failure())); await settle(); expect(reads).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
