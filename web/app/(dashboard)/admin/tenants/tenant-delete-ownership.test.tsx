import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession } from "@/lib/session";
import TenantsPage from "./page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const timestamp = "2026-10-09T00:00:00Z";
const actor = { id: "operator", tenant_id: "platform", role: "super_admin" as const,
  email: "operator@example.test", display_name: "Operator" };
const initial = [
  { id: "first", name: "First company", plan_id: "starter", is_super: false, created_at: timestamp },
  { id: "second", name: "Second company", plan_id: "starter", is_super: false, created_at: timestamp },
  { id: "platform", name: "Platform company", plan_id: "starter", is_super: true, created_at: timestamp },
];
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = () => new Response(JSON.stringify({ error: { code: "INTERNAL", message: "Synthetic delete failure" } }), { status: 503, headers: { "Content-Type": "application/json" } });
let calls: { path: string; method: string; authorization: string | null }[];
let tenants: typeof initial;
let listReply: () => Promise<Response>;
let deleteReply: (id: string) => Promise<Response>;
let pending: (() => void)[];
function defer(response: Response) {
  let release!: () => void;
  const promise = new Promise<Response>(resolve => { release = () => resolve(response); });
  pending.push(release); return { promise, release };
}
beforeEach(() => {
  calls = []; pending = []; tenants = [...initial];
  listReply = async () => json(tenants);
  deleteReply = async id => { tenants = tenants.filter(tenant => tenant.id !== id); return new Response(null, { status: 204 }); };
  installSession("delete-token", actor);
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      authorization: new Headers(init?.headers).get("Authorization") };
    calls.push(call);
    if (call.path === "/api/v1/admin/plans" && call.method === "GET") return json([]);
    if (call.path === "/api/v1/admin/tenants" && call.method === "GET") return listReply();
    if (call.path.startsWith("/api/v1/admin/tenants/") && call.method === "DELETE") return deleteReply(call.path.split("/").at(-1)!);
    throw new Error(`Unexpected delete request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(release => release())); vi.unstubAllGlobals(); });
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, revalidateOnFocus: false, shouldRetryOnError: false }}>
    <SidebarProvider><TenantsPage /></SidebarProvider></SWRConfig>);
  await screen.findByText("First company"); return view;
}
async function removal(name = "First company") {
  const row = screen.getByText(name).closest("tr")!;
  await userEvent.click(within(row).getByRole("button"));
  return screen.findByRole("menuitem", { name: "Delete" });
}
const writes = () => calls.filter(call => call.method === "DELETE");
const reads = () => calls.filter(call => call.method === "GET" && call.path === "/api/v1/admin/tenants");

it("deletes exactly the selected tenant after confirmation and refreshes its removal", async () => {
  await mount(); await userEvent.click(await removal());
  await waitFor(() => expect(screen.queryByText("First company")).not.toBeInTheDocument());
  expect(writes()).toEqual([{ path: "/api/v1/admin/tenants/first", method: "DELETE", authorization: "Bearer delete-token" }]);
  expect(screen.getByText("Second company")).toBeInTheDocument(); expect(toast.success).toHaveBeenCalledWith("Tenant deleted");
});
it.each(["dismissed", "undefined", "throws"])("does not delete with a %s confirmation", async outcome => {
  await mount();
  if (outcome === "throws") vi.mocked(window.confirm).mockImplementation(() => { throw new Error("blocked dialog"); });
  else vi.mocked(window.confirm).mockReturnValue(outcome === "dismissed" ? false : undefined as unknown as boolean);
  await userEvent.click(await removal()); expect(writes()).toHaveLength(0);
});
it.each(["account", "tenant", "role"])("does not send an old row DELETE after %s changes during confirmation", async boundary => {
  await mount(); vi.mocked(window.confirm).mockImplementation(() => {
    if (boundary === "tenant") { localStorage.setItem("tabmail_tenant_id", "another-tenant"); window.dispatchEvent(new Event(AUTH_EVENT)); }
    else installSession("replacement-token", { ...actor, id: boundary === "account" ? "replacement-operator" : actor.id,
      role: boundary === "role" ? "admin" : actor.role });
    return true;
  });
  await userEvent.click(await removal()); await act(async () => undefined);
  expect(writes()).toHaveLength(0); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it("does not send a DELETE after its page unmounts during confirmation", async () => {
  const view = await mount(); vi.mocked(window.confirm).mockImplementation(() => { view.unmount(); return true; });
  await userEvent.click(await removal()); expect(writes()).toHaveLength(0);
});
it("allows a token-only rotation during confirmation and uses the current token", async () => {
  await mount(); vi.mocked(window.confirm).mockImplementation(() => {
    localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); return true;
  });
  await userEvent.click(await removal()); await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0].authorization).toBe("Bearer rotated-token");
});
it("claims a row before confirmation so reentrant activation cannot create a second DELETE", async () => {
  const gate = defer(new Response(null, { status: 204 })); deleteReply = () => gate.promise;
  await mount(); const button = await removal();
  let entered = false;
  vi.mocked(window.confirm).mockImplementation(() => { if (!entered) { entered = true; button.click(); } return true; });
  await userEvent.click(button); await waitFor(() => expect(writes()).toHaveLength(1));
  expect(window.confirm).toHaveBeenCalledTimes(1);
});
it("disables a pending row while allowing a separately confirmed tenant deletion", async () => {
  const first = defer(new Response(null, { status: 204 })), second = defer(new Response(null, { status: 204 }));
  deleteReply = id => id === "first" ? first.promise : second.promise;
  await mount(); await userEvent.click(await removal());
  const firstButton = await removal(); expect(firstButton).toHaveAttribute("aria-disabled", "true");
  await userEvent.keyboard("{Escape}"); await userEvent.click(await removal("Second company"));
  expect(writes().map(call => call.path)).toEqual(["/api/v1/admin/tenants/first", "/api/v1/admin/tenants/second"]);
});
it.each(["success", "failure"])("does not publish a delayed deletion %s after unmount", async outcome => {
  const gate = defer(outcome === "success" ? new Response(null, { status: 204 }) : failed()); deleteReply = () => gate.promise;
  const view = await mount(); await userEvent.click(await removal()); const before = reads().length; view.unmount();
  await act(async () => gate.release());
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(reads()).toHaveLength(before);
});
it("does not publish a previous identity's deletion failure", async () => {
  const gate = defer(failed()); deleteReply = () => gate.promise;
  await mount(); await userEvent.click(await removal()); act(() => installSession("new-token", { ...actor, id: "new-operator" }));
  await act(async () => gate.release()); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});
it("keeps acknowledged deletion distinct from a failed refresh and retries only GET", async () => {
  await mount(); listReply = async () => failed(); await userEvent.click(await removal());
  const alert = await screen.findByRole("alert"); expect(alert).toHaveTextContent(/deleted.*list.*refresh/i);
  expect(writes()).toHaveLength(1); expect(toast.success).toHaveBeenCalledTimes(1);
  listReply = async () => json(tenants);
  await userEvent.click(within(alert).getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(screen.queryByText("First company")).not.toBeInTheDocument()); expect(writes()).toHaveLength(1);
});
it("keeps the protected platform tenant deletion disabled", async () => {
  await mount(); const button = await removal("Platform company");
  expect(button).toHaveAttribute("aria-disabled", "true"); await userEvent.click(button);
  expect(window.confirm).not.toHaveBeenCalled(); expect(writes()).toHaveLength(0);
});
