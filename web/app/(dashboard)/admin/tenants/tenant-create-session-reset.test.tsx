import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession } from "@/lib/session";
import TenantsPage from "./page";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const actor = { id: "operator", tenant_id: "platform", role: "super_admin" as const,
  email: "operator@example.test", display_name: "Operator" };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
let writes: string[];
beforeEach(() => {
  writes = []; installSession("reset-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query,
    addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (init?.method && init.method !== "GET") { writes.push(path); return json({}); }
    if (path === "/api/v1/admin/plans") return json([{ id: "starter", name: "Starter" }]);
    if (path === "/api/v1/admin/tenants") return json([{ id: "existing", name: "Existing company", plan_id: "starter", is_super: false, created_at: "2026-10-09T00:00:00Z" }]);
    throw new Error(`Unexpected reset request: ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function open() {
  await userEvent.click(screen.getByRole("button", { name: "Create Tenant" }));
  return screen.findByRole("dialog", { name: "Create Tenant" });
}
async function draft() {
  render(<SWRConfig value={{ provider: () => new Map(), revalidateOnFocus: false, shouldRetryOnError: false }}>
    <SidebarProvider><TenantsPage /></SidebarProvider></SWRConfig>);
  await screen.findByText("Existing company"); const dialog = await open();
  fireEvent.change(screen.getByPlaceholderText("Example: Acme Corp"), { target: { value: "Private draft from prior identity" } });
  await userEvent.click(within(dialog).getByRole("combobox"));
  await userEvent.click(await screen.findByRole("option", { name: "Starter" }));
}

it.each(["account", "tenant", "role"])("retires the previous creation form when %s changes", async boundary => {
  await draft(); act(() => {
    if (boundary === "tenant") { localStorage.setItem("tabmail_tenant_id", "different-company"); window.dispatchEvent(new Event(AUTH_EVENT)); }
    else installSession("replacement-token", { ...actor, id: boundary === "account" ? "new-operator" : actor.id,
      role: boundary === "role" ? "admin" : actor.role });
  });
  expect(screen.queryByRole("dialog", { name: "Create Tenant" })).not.toBeInTheDocument();
  await open(); expect(screen.getByPlaceholderText("Example: Acme Corp")).toHaveValue("");
  expect(screen.getByRole("combobox")).toHaveTextContent("Select a plan");
  expect(screen.getByRole("button", { name: "Create" })).toBeDisabled(); expect(writes).toHaveLength(0);
});
it("retains unfinished creation input on a token-only rotation", async () => {
  await draft(); act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  expect(screen.getByRole("dialog", { name: "Create Tenant" })).toBeInTheDocument();
  expect(screen.getByPlaceholderText("Example: Acme Corp")).toHaveValue("Private draft from prior identity");
  expect(screen.getByRole("combobox")).toHaveTextContent("Starter"); expect(writes).toHaveLength(0);
});
