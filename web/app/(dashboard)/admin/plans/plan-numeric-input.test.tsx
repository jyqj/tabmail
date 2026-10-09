import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";
import type { AuthUser, Plan } from "@/lib/types";
import PlansPage from "./page";

// Shipping page/dialogs and POST/PATCH API facade; only network replies/toasts
// are synthetic. Native number inputs sanitize nonnumeric/nonfinite text to blank.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const path = "/api/v1/admin/plans";
const tenant = "10000000-0000-4000-8000-000000000001";
const actor: AuthUser = { id: "20000000-0000-4000-8000-000000000001", tenant_id: tenant, role: "super_admin", email: "operator@example.test", display_name: "Operator" };
const numeric = [
  ["max_domains", "Max Domains"], ["max_mailboxes_per_domain", "Max Mailboxes / Domain"],
  ["max_messages_per_mailbox", "Max Messages / Mailbox"], ["max_message_bytes", "Max Message Bytes"],
  ["retention_hours", "Retention (hours)"], ["rpm_limit", "RPM Limit"], ["daily_quota", "Daily Quota"],
] as const;
const original: Plan = { id: "30000000-0000-4000-8000-000000000001", name: "existing-plan", max_domains: 5, max_mailboxes_per_domain: 100, max_messages_per_mailbox: 200, max_message_bytes: 10485760, retention_hours: 48, rpm_limit: 60, daily_quota: 1000, created_at: "2026-01-01T00:00:00Z", updated_at: "2026-01-01T00:00:00Z" };
const expectedOriginal = { name: "changed-plan", ...Object.fromEntries(numeric.map(([key]) => [key, original[key]])) };
const effective = { can_send: false, daily_send_quota: 0, daily_receive_quota: 0, max_mailboxes: 0, max_domains: 0, allowed_zone_ids: [], can_create_domains: false, can_create_routes: false, can_create_api_keys: false };
const reply = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
let writes: { path: string; method: string; body: Record<string, unknown>; authorization: string | null; tenant: string | null }[];
let writeReply: () => Promise<Response>;
beforeEach(() => {
  writes = []; writeReply = async () => reply(original); installSession("plan-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost"), method = init?.method ?? "GET", headers = new Headers(init?.headers);
    if (url.pathname === "/api/v1/auth/me/permissions") return reply(effective);
    if (url.pathname === path && method === "GET") return reply([original]);
    if ((url.pathname === path && method === "POST") || (url.pathname === `${path}/${original.id}` && method === "PATCH")) {
      writes.push({ path: url.pathname, method, body: JSON.parse(String(init?.body)), authorization: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID") }); return writeReply();
    }
    throw new Error(`Unexpected plan request: ${method} ${url.pathname}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
async function open(mode: "create" | "edit") {
  render(<AuthProvider><SWRConfig value={{ shouldRetryOnError: false, dedupingInterval: 0 }}><SidebarProvider><PlansPage /></SidebarProvider></SWRConfig></AuthProvider>);
  await screen.findByText(original.name);
  if (mode === "create") await userEvent.click(screen.getByRole("button", { name: "Create Plan" }));
  else {
    const row = screen.getByText(original.name).closest("tr")!;
    // Existing icon-only edit action, scoped to the actual plan row.
    await userEvent.click(within(row).getAllByRole("button")[0]);
  }
  const dialog = await screen.findByRole("dialog", { name: mode === "create" ? "Create Plan" : "Edit Plan" });
  fireEvent.change(within(dialog).getByPlaceholderText("Plan Name"), { target: { value: "  changed-plan  " } });
  return dialog;
}
const input = (dialog: HTMLElement, label: string) => within(dialog).getByPlaceholderText(label) as HTMLInputElement;
const change = (dialog: HTMLElement, label: string, value: string) => fireEvent.change(input(dialog, label), { target: { value } });
async function submit(dialog: HTMLElement, mode: "create" | "edit") { await userEvent.click(within(dialog).getByRole("button", { name: mode === "create" ? "Create" : "Save" })); await settle(); }
function assertWrite(mode: "create" | "edit", body: Record<string, unknown>) {
  expect(writes).toEqual([{ path: mode === "create" ? path : `${path}/${original.id}`, method: mode === "create" ? "POST" : "PATCH", body, authorization: "Bearer plan-token", tenant }]);
}

for (const mode of ["create", "edit"] as const) describe(`${mode} plan numeric input`, () => {
  for (const [key, label] of numeric) {
    it.each([
      ["blank", ""], ["fraction", "1.5"], ["nonfinite", "1e309"], ["upper overflow", "2147483648"], ["lower overflow", "-2147483649"],
    ])(`rejects ${key} %s without sending or discarding the form`, async (_kind, raw) => {
      const dialog = await open(mode); change(dialog, label, raw); const entered = input(dialog, label).value;
      await submit(dialog, mode); expect(writes).toHaveLength(0);
      expect(dialog).toBeInTheDocument(); expect(input(dialog, label).value).toBe(entered);
      expect(input(dialog, "Plan Name").value).toBe("  changed-plan  ");
      expect(input(dialog, label)).toHaveAttribute("aria-invalid", "true");
      expect(input(dialog, label)).toHaveAccessibleDescription(/whole number.*-2147483648.*2147483647/i);
      expect(toast.success).not.toHaveBeenCalled();
    });
  }
  it("rejects whitespace and all invalid fields together, then submits a corrected draft", async () => {
    const dialog = await open(mode); for (const [, label] of numeric) change(dialog, label, " ");
    await submit(dialog, mode); expect(writes).toHaveLength(0);
    for (const [, label] of numeric) { expect(input(dialog, label)).toHaveAttribute("aria-invalid", "true"); change(dialog, label, "0"); }
    await submit(dialog, mode); assertWrite(mode, { name: "changed-plan", ...Object.fromEntries(numeric.map(([key]) => [key, 0])) });
  });
  it.each(["1.0000000000000001", "1e-400"])("does not round fractional text %s to a valid integer", async raw => {
    const dialog = await open(mode); change(dialog, "Retention (hours)", raw); await submit(dialog, mode);
    expect(writes).toHaveLength(0); expect(input(dialog, "Retention (hours)").value).toBe(raw);
    expect(input(dialog, "Retention (hours)")).toHaveAttribute("aria-invalid", "true");
  });
  it.each(["0", "-1", "-2147483648", "2147483647", "3000000", "1e3", "1.0"])("preserves legal integer %s exactly on the wire for every field", async raw => {
    const dialog = await open(mode); for (const [, label] of numeric) change(dialog, label, raw);
    await submit(dialog, mode); assertWrite(mode, { name: "changed-plan", ...Object.fromEntries(numeric.map(([key]) => [key, Number(raw)])) });
    await waitFor(() => expect(dialog).not.toBeInTheDocument());
  });
  it("preserves unchanged valid defaults/loaded values", async () => {
    const dialog = await open(mode); await submit(dialog, mode); assertWrite(mode, expectedOriginal);
  });
  it.each(["server", "network"])("retains valid inputs after %s failure and permits an explicit retry", async kind => {
    writeReply = async () => { if (kind === "network") throw new TypeError("offline"); return new Response(JSON.stringify({ error: { code: "INVALID_ARGUMENT", message: "Retention cannot be represented for this date" } }), { status: 400, headers: { "Content-Type": "application/json" } }); };
    const dialog = await open(mode); change(dialog, "Retention (hours)", "3000000"); await submit(dialog, mode);
    expect(writes).toHaveLength(1); expect(writes[0].body).toEqual({ ...expectedOriginal, retention_hours: 3000000 });
    expect(dialog).toBeInTheDocument(); expect(input(dialog, "Retention (hours)").value).toBe("3000000");
    expect(toast.error).toHaveBeenCalled(); if (kind === "server") expect(toast.error).toHaveBeenCalledWith("Retention cannot be represented for this date");
    expect(toast.success).not.toHaveBeenCalled(); writeReply = async () => reply(original); await submit(dialog, mode);
    expect(writes).toHaveLength(2); expect(writes[1]).toEqual(writes[0]); await waitFor(() => expect(dialog).not.toBeInTheDocument());
  });
});
