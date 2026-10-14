import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession } from "@/lib/session";
import TenantsPage from "./page";

// The actual page, Base UI dialogs/select, SWR and HTTP/session helpers run.
// Controlled HTTP and clipboard-independent browser primitives are the only seams.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const timestamp = "2026-10-09T00:00:00Z";
const actor = { id: "operator", tenant_id: "platform", role: "super_admin" as const,
  email: "operator@example.test", display_name: "Operator" };
const initialTenant = { id: "existing", name: "Existing company", plan_id: "starter", is_super: false, created_at: timestamp };
const plans = [{ id: "starter", name: "Starter", created_at: timestamp }, { id: "team", name: "Team", created_at: timestamp }];
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = () => new Response(JSON.stringify({ error: { code: "INTERNAL", message: "Synthetic request failure" } }), { status: 503, headers: { "Content-Type": "application/json" } });
type Call = { path: string; method: string; body: unknown; authorization: string | null };
let calls: Call[];
let tenants: typeof initialTenant[];
let listReply: () => Promise<Response>;
let createReply: (body: { name: string; plan_id: string }) => Promise<Response>;
let pending: (() => void)[];
function defer(response: Response) {
  let release!: () => void;
  const promise = new Promise<Response>(resolve => { release = () => resolve(response); });
  pending.push(release);
  return { promise, release };
}
beforeEach(() => {
  calls = []; pending = []; tenants = [initialTenant];
  listReply = async () => json(tenants);
  createReply = async body => {
    const created = { ...initialTenant, ...body, id: `created-${tenants.length}` };
    tenants = [...tenants, created];
    return json(created);
  };
  installSession("creation-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: init?.body ? JSON.parse(String(init.body)) : undefined, authorization: new Headers(init?.headers).get("Authorization") };
    calls.push(call);
    if (call.path === "/api/v1/admin/plans" && call.method === "GET") return json(plans);
    if (call.path === "/api/v1/admin/tenants" && call.method === "GET") return listReply();
    if (call.path === "/api/v1/admin/tenants" && call.method === "POST") return createReply(call.body);
    throw new Error(`Unexpected create request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(release => release())); vi.unstubAllGlobals(); });
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, revalidateOnFocus: false, shouldRetryOnError: false }}>
    <SidebarProvider><TenantsPage /></SidebarProvider></SWRConfig>);
  await screen.findByText(initialTenant.name);
  return view;
}
async function open() {
  await userEvent.click(screen.getByRole("button", { name: "Create Tenant" }));
  return screen.findByRole("dialog", { name: "Create Tenant" });
}
const nameInput = () => screen.getByPlaceholderText("Example: Acme Corp");
async function selectPlan(label = "Starter") {
  await userEvent.click(within(screen.getByRole("dialog", { name: "Create Tenant" })).getByRole("combobox"));
  await userEvent.click(await screen.findByRole("option", { name: label }));
}
async function fill(name = "First company", plan = "Starter") { fireEvent.change(nameInput(), { target: { value: name } }); await selectPlan(plan); }
async function start() { await userEvent.click(screen.getByRole("button", { name: "Create" })); await waitFor(() => expect(writes()).toHaveLength(1)); }
async function close() { await userEvent.click(within(screen.getByRole("dialog", { name: "Create Tenant" })).getByRole("button", { name: "关闭" })); }
const writes = () => calls.filter(call => call.method === "POST");
const reads = () => calls.filter(call => call.method === "GET" && call.path === "/api/v1/admin/tenants");
const submitButton = () => screen.getByRole("button", { name: /^Creat(e|ing\.\.\.)$/ });

it("creates the selected name and plan once, closes an unchanged dialog and refreshes the list", async () => {
  await mount(); await open(); await fill(); await start();
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create Tenant" })).not.toBeInTheDocument());
  expect(writes()[0]).toMatchObject({ body: { name: "First company", plan_id: "starter" }, authorization: "Bearer creation-token" });
  await screen.findByText("First company");
  expect(toast.success).toHaveBeenCalledTimes(1);
});
it.each(["name", "plan", "name ABA"])("preserves a newer %s edit when the first creation is acknowledged", async kind => {
  const gate = defer(json({ ...initialTenant, id: "first-created", name: "First company" })); createReply = () => gate.promise;
  await mount(); await open(); await fill(); await start();
  if (kind === "plan") await selectPlan("Team");
  else {
    fireEvent.change(nameInput(), { target: { value: "Second company" } });
    if (kind === "name ABA") fireEvent.change(nameInput(), { target: { value: "First company" } });
  }
  await act(async () => gate.release());
  expect(screen.getByRole("dialog", { name: "Create Tenant" })).toBeInTheDocument();
  expect(nameInput()).toHaveValue(kind === "name" ? "Second company" : "First company");
  expect(screen.getByRole("combobox")).toHaveTextContent(kind === "plan" ? "Team" : "Starter");
  await waitFor(() => expect(submitButton()).toBeEnabled()); expect(writes()).toHaveLength(1);
});
it("does not close or clear a reopened dialog even if its values have not changed", async () => {
  const gate = defer(json({ ...initialTenant, id: "created" })); createReply = () => gate.promise;
  await mount(); await open(); await fill(); await start(); await close(); await open();
  await act(async () => gate.release());
  expect(screen.getByRole("dialog", { name: "Create Tenant" })).toBeInTheDocument();
  expect(nameInput()).toHaveValue("First company");
});
it("an old request cannot release a pending creation in a reopened dialog", async () => {
  const first = defer(json({ ...initialTenant, id: "first-created" })), second = defer(json({ ...initialTenant, id: "second-created" }));
  let creates = 0; createReply = () => ++creates === 1 ? first.promise : second.promise;
  await mount(); await open(); await fill(); await start(); await close(); await open();
  fireEvent.change(nameInput(), { target: { value: "Second company" } });
  await userEvent.click(screen.getByRole("button", { name: "Create" }));
  await waitFor(() => expect(writes()).toHaveLength(2));
  await act(async () => first.release()); expect(submitButton()).toBeDisabled(); expect(nameInput()).toHaveValue("Second company");
  await act(async () => second.release());
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create Tenant" })).not.toBeInTheDocument());
});
it.each(["success", "failure"])("ignores a delayed creation %s after unmount", async outcome => {
  const gate = defer(outcome === "success" ? json({ ...initialTenant, id: "created" }) : failed()); createReply = () => gate.promise;
  const view = await mount(); await open(); await fill(); await start(); const before = reads().length; view.unmount();
  await act(async () => gate.release());
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(reads()).toHaveLength(before);
});
it("preserves the new identity's draft and suppresses the old-session failure", async () => {
  const gate = defer(json({ ...initialTenant, id: "created" })); createReply = () => gate.promise;
  await mount(); await open(); await fill(); await start();
  act(() => installSession("replacement-token", { ...actor, id: "new-operator" }));
  if (!screen.queryByRole("dialog", { name: "Create Tenant" })) await open();
  fireEvent.change(nameInput(), { target: { value: "New identity company" } });
  await act(async () => gate.release());
  expect(nameInput()).toHaveValue("New identity company"); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it("accepts a token-only rotation without retiring the current creation", async () => {
  const gate = defer(json({ ...initialTenant, id: "created" })); createReply = () => gate.promise;
  await mount(); await open(); await fill(); await start();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  await act(async () => gate.release());
  await waitFor(() => expect(screen.queryByRole("dialog", { name: "Create Tenant" })).not.toBeInTheDocument());
  expect(toast.success).toHaveBeenCalledTimes(1); expect(writes()).toHaveLength(1);
});
it("does not repeat a create on same-event double activation", async () => {
  const gate = defer(json({ ...initialTenant, id: "created" })); createReply = () => gate.promise;
  await mount(); await open(); await fill(); const button = submitButton();
  act(() => { button.click(); button.click(); }); await waitFor(() => expect(writes()).toHaveLength(1));
});
it("keeps an acknowledged creation distinct from a failed list refresh and retries only GET", async () => {
  await mount(); await open(); await fill(); listReply = async () => failed(); await start();
  const alert = await screen.findByRole("alert"); expect(alert).toHaveTextContent(/created.*list.*refresh/i);
  expect(toast.success).toHaveBeenCalledTimes(1); expect(writes()).toHaveLength(1);
  listReply = async () => json(tenants);
  await userEvent.click(within(alert).getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  await screen.findByText("First company"); expect(writes()).toHaveLength(1);
});
it("preserves input after a rejected write and does not perform a confirmation read", async () => {
  createReply = async () => failed(); await mount(); await open(); await fill(); await start();
  await waitFor(() => expect(toast.error).toHaveBeenCalledTimes(1));
  expect(nameInput()).toHaveValue("First company"); expect(reads()).toHaveLength(1); expect(toast.success).not.toHaveBeenCalled();
});
