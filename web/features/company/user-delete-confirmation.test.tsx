import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import UsersPage from "./user-management";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";

vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ level: "admin", tenantId: tenant }) }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const tenant = "10000000-0000-4000-8000-000000000001";
const employee = "20000000-0000-4000-8000-000000000001";
const writes: string[] = [];
const reply = (data: unknown) => new Response(JSON.stringify({ data }), {
  headers: { "Content-Type": "application/json" },
});

beforeEach(() => {
  writes.length = 0;
  installSession("confirmation-token", { id: "admin", tenant_id: tenant, email: "admin@company.test", display_name: "Admin", role: "admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, onchange: null, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const method = init?.method ?? "GET";
    if (method !== "GET") {
      writes.push(`${method} ${path}`);
      return reply({ deleted: true });
    }
    if (path === "/api/v1/admin/users") return reply([{
      id: employee, tenant_id: tenant, email: "employee@company.test", display_name: "Employee", role: "user", is_active: true,
      created_at: "2026-10-01T00:00:00Z", updated_at: "2026-10-01T00:00:00Z",
    }]);
    if (path === "/api/v1/admin/permissions" || path === "/api/v1/domains") return reply([]);
    throw new Error(`Unexpected confirmation request: ${method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

async function deleteEmployee() {
  render(<SidebarProvider><UsersPage /></SidebarProvider>);
  const email = await screen.findByText("employee@company.test");
  const trigger = email.closest("tr")!.querySelector<HTMLButtonElement>('[data-slot="dropdown-menu-trigger"]')!;
  await userEvent.click(trigger);
  await userEvent.click(await screen.findByRole("menuitem", { name: "Delete User" }));
}

describe("employee deletion requires explicit confirmation", () => {
  it.each(["missing", "throws", "undefined", "dismissed"])("refuses %s confirmation", async (failure) => {
    const confirm = vi.spyOn(window, "confirm");
    if (failure === "missing") vi.stubGlobal("confirm", undefined);
    else if (failure === "throws") confirm.mockImplementation(() => { throw new Error("dialog blocked"); });
    else confirm.mockReturnValue(failure === "dismissed" ? false : undefined as unknown as boolean);
    await deleteEmployee();
    expect(writes).toEqual([]);
  });

  it("deletes only the selected employee after an explicit true result", async () => {
    vi.spyOn(window, "confirm").mockReturnValue(true);
    await deleteEmployee();
    await waitFor(() => expect(writes).toEqual([`DELETE /api/v1/admin/users/${employee}`]));
  });
});
