import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser, SystemSetting } from "@/lib/types";
import AdminSettingsPage from "./page";

// Actual page, controls, SWR, identity and GET/PATCH facade; synthetic fetch.
// This verifies partial string-valued commands, not bulk DB atomicity or CAS.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() } }));
const path = "/api/v1/admin/settings";
const actor: AuthUser = { id: "settings-operator", tenant_id: "settings-tenant", role: "super_admin", email: "operator@example.test", display_name: "Operator" };
const initial = { monitor_history: "200", public_ip_rpm: "60", strip_plus_tag: "true", mailbox_naming: "full", fallback_retention_hours: "24" };
const entries = (values: Record<string, string>): SystemSetting[] => Object.entries(values).map(([key, value]) => ({ key, value, description: key, updated_at: "2026-10-08T00:00:00Z" }));
const reply = (values: Record<string, string>) => new Response(JSON.stringify({ data: entries(values) }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 500) => new Response(JSON.stringify({ error: { code: "CONTROLLED", message: "Controlled settings failure" } }), { status, headers: { "Content-Type": "application/json" } });
function deferred() { let resolve!: (value: Response) => void; const promise = new Promise<Response>(done => { resolve = done; }); return { promise, resolve }; }
let server: Record<string, string>;
let reads: string[];
let writes: { method: string; body: Record<string, string>; authorization: string | null; tenant: string | null }[];
let readReply: () => Promise<Response>;
let writeReply: (body: Record<string, string>) => Promise<Response>;
function Controls() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(key => Array.isArray(key) && key[0] === "session" && key[1] === sessionScope() && key[2] === "system-settings").catch(() => {}); }}>Refresh settings cache</button>;
}
beforeEach(() => {
  server = { ...initial }; reads = []; writes = []; readReply = async () => reply(server);
  writeReply = async body => { server = { ...server, ...body }; return reply(server); };
  installSession("settings-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost"), headers = new Headers(init?.headers), method = init?.method ?? "GET";
    if (url.pathname === "/api/v1/auth/me/permissions") return new Response(JSON.stringify({ data: {} }));
    if (url.pathname !== path) throw new Error(`Unexpected settings request: ${method} ${url.pathname}`);
    if (method === "GET") { reads.push(headers.get("Authorization") ?? ""); return readReply(); }
    const body = JSON.parse(String(init?.body)); writes.push({ method, body, authorization: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID") }); return writeReply(body);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 35; i++) await Promise.resolve(); }); }
function mount() { return render(<AuthProvider><SWRConfig value={{ shouldRetryOnError: false, dedupingInterval: 0, revalidateOnFocus: false }}><Controls /><SidebarProvider><AdminSettingsPage /></SidebarProvider></SWRConfig></AuthProvider>); }
const save = () => screen.getByRole("button", { name: /Save Changes|Saving\.\.\./ });
const field = (key = "public_ip_rpm") => document.getElementById(`setting-${key}`)!;
const edit = (value: string, key = "public_ip_rpm") => fireEvent.change(field(key), { target: { value } });
async function ready() { const page = mount(); await screen.findByDisplayValue("200"); return page; }
async function refresh() { fireEvent.click(screen.getByRole("button", { name: "Refresh settings cache" })); await settle(); }
async function submit() { fireEvent.click(save()); await settle(); }
function changeScope(kind: "account" | "tenant" | "role") {
  if (kind === "tenant") { localStorage.setItem("tabmail_tenant_id", "replacement-tenant"); window.dispatchEvent(new Event(AUTH_EVENT)); }
  else installSession("replacement-settings-token", { ...actor, id: kind === "account" ? "replacement-operator" : actor.id, role: kind === "role" ? "admin" : actor.role });
}

it("requires the first successful snapshot before saving", async () => {
  const pending = deferred(); readReply = () => pending.promise; mount(); await waitFor(() => expect(reads).toHaveLength(1));
  expect(save()).toBeDisabled(); await submit(); expect(writes).toHaveLength(0);
  await act(async () => pending.resolve(reply(initial))); await screen.findByDisplayValue("200");
});
it.each(["500", "403", "network"])("keeps first-read %s failure visible and offers a read retry", async kind => {
  readReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(Number(kind)); };
  mount(); await settle(); expect(save()).toBeDisabled(); expect(screen.getByRole("alert")).toBeInTheDocument();
  readReply = async () => reply(initial); fireEvent.click(screen.getByRole("button", { name: "Retry loading" })); await settle();
  expect(field("monitor_history")).toHaveValue(200); expect(writes).toHaveLength(0);
});
it("updates pristine values on background refresh without generating reverse writes", async () => {
  await ready(); server.monitor_history = "300"; await refresh(); expect(field("monitor_history")).toHaveValue(300);
  await submit(); expect(writes).toHaveLength(0);
});
it("sends only the intentionally edited field after an unrelated background change", async () => {
  await ready(); edit("70"); server.monitor_history = "300"; await refresh(); await submit();
  expect(writes).toEqual([{ method: "PATCH", body: { public_ip_rpm: "70" }, authorization: "Bearer settings-token", tenant: actor.tenant_id }]);
  expect(field("monitor_history")).toHaveValue(300);
});
it("preserves the explicit same-field draft across a changed server snapshot", async () => {
  await ready(); edit("70"); server.public_ip_rpm = "90"; await refresh(); expect(field()).toHaveValue(70);
  await submit(); expect(writes[0].body).toEqual({ public_ip_rpm: "70" });
});
it("a reverted edit becomes pristine and follows subsequent background values", async () => {
  await ready(); edit("70"); edit("60"); server.public_ip_rpm = "90"; await refresh();
  expect(field()).toHaveValue(90); await submit(); expect(writes).toHaveLength(0);
});
it("preserves explicit false and zero string commands", async () => {
  await ready(); fireEvent.click(screen.getByRole("switch")); edit("0"); await submit();
  expect(writes[0].body).toEqual({ strip_plus_tag: "false", public_ip_rpm: "0" });
  expect(screen.getByRole("switch")).toHaveAttribute("aria-checked", "false"); expect(field()).toHaveValue(0);
});
it("preserves an explicit naming choice while untouched settings refresh", async () => {
  await ready(); fireEvent.click(screen.getByRole("button", { name: "local" })); server.monitor_history = "300"; await refresh(); await submit();
  expect(writes[0].body).toEqual({ mailbox_naming: "local" });
});
it("keeps a cleared string as the user's input when the server rejects it, without defaulting to zero", async () => {
  await ready(); edit(""); writeReply = async () => failure(400); await submit();
  expect(writes[0].body).toEqual({ public_ip_rpm: "" }); expect(field()).toHaveValue(null);
  expect(toast.error).toHaveBeenCalledWith("Controlled settings failure");
});
it("keeps newer same-field input after an in-flight save acknowledges the submitted value", async () => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit();
  edit("80"); server.public_ip_rpm = "70"; await act(async () => pending.resolve(reply(server))); await settle();
  expect(field()).toHaveValue(80); writeReply = async body => { server = { ...server, ...body }; return reply(server); }; await submit();
  expect(writes.map(w => w.body)).toEqual([{ public_ip_rpm: "70" }, { public_ip_rpm: "80" }]);
});
it("keeps a different field edited during a pending save", async () => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit();
  edit("400", "monitor_history"); server.public_ip_rpm = "70"; await act(async () => pending.resolve(reply(server))); await settle();
  expect(field("monitor_history")).toHaveValue(400); writeReply = async body => reply({ ...server, ...body }); await submit();
  expect(writes[1].body).toEqual({ monitor_history: "400" });
});
it("preserves an intentional return to the old value while its replacement is being saved", async () => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit(); edit("60");
  server.public_ip_rpm = "70"; await act(async () => pending.resolve(reply(server))); await settle(); expect(field()).toHaveValue(60);
  writeReply = async body => reply({ ...server, ...body }); await submit(); expect(writes[1].body).toEqual({ public_ip_rpm: "60" });
});
it("deduplicates a pending save without disabling continued editing", async () => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise;
  fireEvent.click(save()); fireEvent.click(save()); await settle(); expect(writes).toHaveLength(1); expect(save()).toBeDisabled(); expect(field()).toBeEnabled();
  await act(async () => pending.resolve(reply({ ...initial, public_ip_rpm: "70" }))); await settle();
});
it.each(["500", "network"])("keeps newer edits on a write %s failure and permits an explicit retry", async kind => {
  await ready(); edit("70"); writeReply = async () => { if (kind === "network") throw new TypeError("offline"); return failure(); }; await submit();
  expect(field()).toHaveValue(70); expect(save()).toBeEnabled(); expect(writes).toHaveLength(1);
  writeReply = async body => { server = { ...server, ...body }; return reply(server); }; await submit(); expect(writes).toHaveLength(2);
  expect(writes[1].body).toEqual({ public_ip_rpm: "70" });
});
it("blocks writes during a background read without discarding edits", async () => {
  await ready(); edit("70"); const pending = deferred(); readReply = () => pending.promise; await refresh();
  expect(save()).toBeDisabled(); expect(field()).toHaveValue(70); await submit(); expect(writes).toHaveLength(0);
  await act(async () => pending.resolve(reply({ ...initial, monitor_history: "300" }))); await settle(); await submit(); expect(writes[0].body).toEqual({ public_ip_rpm: "70" });
});
it("a failed background read blocks stale writes, preserves input, and recovers through read retry", async () => {
  await ready(); edit("70"); readReply = async () => failure(); await refresh(); expect(save()).toBeDisabled(); expect(field()).toHaveValue(70);
  expect(screen.getByRole("alert")).toBeInTheDocument(); readReply = async () => reply({ ...initial, monitor_history: "300" });
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" })); await settle(); await submit(); expect(writes[0].body).toEqual({ public_ip_rpm: "70" });
});
it("retains acknowledged values and newer edits when the follow-up read fails", async () => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit(); edit("80"); readReply = async () => failure();
  await act(async () => pending.resolve(reply({ ...initial, public_ip_rpm: "70" }))); await settle(); expect(field()).toHaveValue(80);
  expect(save()).toBeDisabled(); expect(screen.getByRole("alert")).toBeInTheDocument(); expect(toast.success).toHaveBeenCalledTimes(1); expect(writes).toHaveLength(1);
});
it.each(["account", "tenant", "role"] as const)("retires the draft and synchronous old save callback on %s replacement", async kind => {
  await ready(); edit("70"); const oldSave = save(), pending = deferred(); readReply = () => pending.promise;
  act(() => { changeScope(kind); fireEvent.click(oldSave); }); await settle(); expect(writes).toHaveLength(0); expect(save()).toBeDisabled();
  await act(async () => pending.resolve(reply({ ...initial, public_ip_rpm: "90" }))); await settle(); expect(field()).toHaveValue(90);
});
it.each(["success", "failure"] as const)("ignores an old session's late save %s and does not refresh its successor", async outcome => {
  await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit();
  server.public_ip_rpm = "90"; act(() => changeScope("account")); await settle(); const count = reads.length;
  await act(async () => pending.resolve(outcome === "success" ? reply(initial) : failure())); await settle();
  expect(field()).toHaveValue(90); expect(reads).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it.each(["success", "failure"] as const)("ignores a save %s after the page unmounts", async outcome => {
  const page = await ready(); edit("70"); const pending = deferred(); writeReply = () => pending.promise; await submit(); const count = reads.length; page.unmount();
  await act(async () => pending.resolve(outcome === "success" ? reply(initial) : failure())); await settle();
  expect(reads).toHaveLength(count); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it("token-only rotation retains edits and uses the replacement token on save", async () => {
  await ready(); edit("70"); const scope = sessionScope();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-settings-token"); window.dispatchEvent(new Event(AUTH_EVENT)); }); await settle();
  expect(sessionScope()).toBe(scope); expect(field()).toHaveValue(70); await submit(); expect(writes[0].authorization).toBe("Bearer rotated-settings-token");
});
