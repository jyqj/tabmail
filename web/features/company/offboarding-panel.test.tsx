import React from "react";
import { cleanup, fireEvent, screen } from "@testing-library/react";
import { afterEach, expect, it, vi } from "vitest";
import type { AdminUser, AuthUser } from "@/lib/types";
import { I18nProvider } from "@/lib/i18n";
import { OffboardingPanel } from "./offboarding-panel";
const { render } = await vi.importActual<typeof import("@testing-library/react")>("@testing-library/react");
const host = vi.hoisted(() => ({ user: null as AuthUser | null, tenantId: null as string | null }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({ user: host.user, tenantId: host.tenantId }) }));
const employee = (id: string, role: AdminUser["role"] = "user", is_active = true, tenant_id = "company"): AdminUser => ({ id, role, is_active, tenant_id, email: `${id}@fixture.test`, display_name: id, created_at: "", updated_at: "" });
const rows = [employee("caller", "admin"), employee("active"), employee("frozen", "user", false), employee("peer", "admin"), employee("higher", "super_admin"), employee("foreign", "user", true, "foreign")];
const values = (label: string) => Array.from((screen.getByLabelText(label) as HTMLSelectElement).options).map(o => o.value);
const setup = (role: AuthUser["role"] = "admin") => {
  host.tenantId = null;
  host.user = { id: "caller", role, tenant_id: "company", email: "caller@fixture.test", display_name: "Caller" };
  return render(<OffboardingPanel employees={rows} refresh={async () => {}} />, { wrapper: I18nProvider });
};
afterEach(cleanup);
it("separates active/frozen targets from active successors and excludes self/foreign/higher roles", () => {
  setup();
  expect(values("Employee to offboard")).toEqual(["", "active", "frozen"]);
  expect(values("Mailbox successor")).toEqual(["", "active"]);
  fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: "active" } });
  expect(values("Mailbox successor")).toEqual([""]);
});
it("preserves super-admin hierarchy and fails closed for ordinary/absent callers", () => {
  const view = setup("super_admin");
  expect(values("Employee to offboard")).toEqual(["", "active", "frozen", "peer", "higher"]);
  expect(values("Mailbox successor")).toEqual(["", "active", "peer", "higher"]);
  host.user = { ...host.user!, role: "user" };
  view.rerender(<OffboardingPanel employees={rows} refresh={async () => {}} />);
  expect(values("Employee to offboard")).toEqual([""]);
  host.user = null;
  view.rerender(<OffboardingPanel employees={rows} refresh={async () => {}} />);
  expect(values("Mailbox successor")).toEqual([""]);
});
it("revalidates retained selections on refresh and authority changes", () => {
  const view = setup();
  fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: "frozen" } });
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: "active" } });
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: "Review ticket LF01" } });
  const preview = screen.getByRole("button", { name: "Preview handover impact" });
  expect(preview).toBeEnabled();
  const rerender = (employees: AdminUser[]) => view.rerender(<OffboardingPanel employees={employees} refresh={async () => {}} />);
  for (const change of [{ is_active: false }, { tenant_id: "foreign" }, { role: "admin" as const }]) {
    rerender(rows.map(u => u.id === "active" ? { ...u, ...change } : u));
    expect(preview).toBeDisabled();
  }
  rerender(rows.filter(u => u.id !== "frozen"));
  expect(preview).toBeDisabled();
  rerender(rows);
  expect(preview).toBeEnabled();
  host.user = { ...host.user!, role: "user" };
  rerender(rows);
  expect(preview).toBeDisabled();
});

it("uses the selected company for super-admin access without widening ordinary admin authority", () => {
  const view = setup("super_admin");
  host.tenantId = "foreign";
  view.rerender(<OffboardingPanel employees={rows} refresh={async () => {}} />);
  expect(values("Employee to offboard")).toEqual(["", "foreign"]);
  host.user = { ...host.user!, role: "admin" };
  view.rerender(<OffboardingPanel employees={rows} refresh={async () => {}} />);
  expect(values("Employee to offboard")).toEqual([""]);
});
