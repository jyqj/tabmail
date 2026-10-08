import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";
import PermissionsPage from "./profile-management";
import UsersPage from "./user-management";

// Real pages, Base UI, translations, serializer, session and SWR. Synthetic
// auth/HTTP only; the accessible-name, label and keyboard checks run in jsdom.
const auth = vi.hoisted(() => ({ level: "admin" as "admin" | "super_admin", tenantId: "10000000-0000-4000-8000-000000000001" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => auth }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const userID = "20000000-0000-4000-8000-000000000001";
const zone = "30000000-0000-4000-8000-000000000001";
const profileID = "40000000-0000-4000-8000-000000000001";
const profile = { id: profileID, tenant_id: auth.tenantId, name: "Accessible profile", description: "Original description", revision: "9007199254740995", is_system: false,
  can_send: true, can_create_domains: false, can_create_routes: false, can_create_api_keys: false,
  daily_send_quota: 10, daily_receive_quota: 20, max_mailboxes: 30, max_domains: 40, allowed_zone_ids: [zone],
  created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z" };
const booleans = [["can_send", "Can Send"], ["can_create_domains", "Can Create Domains"], ["can_create_routes", "Can Create Routes"], ["can_create_api_keys", "Can Create API Keys"]] as const;
const userQuotas = ["Daily Send Quota", "Daily Receive Quota", "Max Mailboxes", "Max Domains"];
const profileQuotas = ["Daily send quota", "Daily receive quota", "Max mailboxes", "Max domains"];
function snapshot() {
  const effective = { can_send: true, can_create_domains: false, can_create_routes: false, can_create_api_keys: false,
    daily_send_quota: 10, daily_receive_quota: 20, max_mailboxes: 30, max_domains: 40, allowed_zone_ids: [zone] };
  return { user_id: userID, tenant_id: auth.tenantId, profile, effective,
    overrides: { ...effective, domain_access: { mode: "list", zone_ids: [zone] } },
    field_sources: Object.fromEntries([...booleans.map(([key]) => key), "daily_send_quota", "daily_receive_quota", "max_mailboxes", "max_domains", "domain_access"].map(key => [key, "override"])),
    revision: { user_id: userID, tenant_id: auth.tenantId, user_revision: "9007199254740993", profile_id: profileID, profile_revision: profile.revision },
    capabilities: { patch: true, assign_profile: true } };
}
let current: ReturnType<typeof snapshot>;
let writes: { path: string; method: string; body: Record<string, unknown> }[];
let writeReply: (path: string) => Promise<Response>;
const reply = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
function identity() { installSession("accessibility-token", { id: "accessibility-admin", tenant_id: auth.tenantId, role: auth.level, email: "admin@example.test", display_name: "Admin" }); }
beforeEach(() => {
  auth.level = "admin"; current = snapshot(); writes = []; identity();
  writeReply = async path => reply(path.includes("permission-editor") ? current : profile);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname, method = init?.method ?? "GET";
    if (method !== "GET") { writes.push({ path, method, body: JSON.parse(String(init?.body)) }); return writeReply(path); }
    if (path === "/api/v1/company/events") {
      let close = () => {};
      const abort = () => close();
      const stream = new ReadableStream<Uint8Array>({ start(controller) { close = () => { controller.close(); close = () => {}; }; }, cancel() { close = () => {}; init?.signal?.removeEventListener("abort", abort); } });
      init?.signal?.addEventListener("abort", abort, { once: true }); if (init?.signal?.aborted) abort();
      return new Response(stream, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (path === "/api/v1/admin/permissions") return reply([profile]);
    if (path === "/api/v1/admin/users") return reply([{ id: userID, tenant_id: auth.tenantId, email: "alice@example.test", display_name: "Alice", role: "user", is_active: true, created_at: profile.created_at, updated_at: profile.updated_at }]);
    if (path === `/api/v1/admin/users/${userID}/permission-editor`) return reply(current);
    if (["/api/v1/domains", "/api/v1/admin/domains"].includes(path)) return reply([{ id: zone, tenant_id: auth.tenantId, domain: "accessible.example.test" }]);
    if (path === "/api/v1/admin/tenants") return reply([{ id: auth.tenantId, name: "Current company" }]);
    throw new Error(`Unexpected accessibility request: ${method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
function mount(children: React.ReactNode) { return render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><SidebarProvider>{children}</SidebarProvider></SWRConfig>); }
async function openProfile(mode: "create" | "edit") {
  mount(<PermissionsPage />); await screen.findByText(profile.name);
  if (mode === "create") await userEvent.click(screen.getByRole("button", { name: "Create Profile" }));
  else { await userEvent.click(within(screen.getByText(profile.name).closest("tr")!).getByRole("button")); await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" })); }
  return screen.getByRole("dialog");
}
async function openUser() {
  mount(<UsersPage />); const row = (await screen.findByText("alice@example.test")).closest("tr")!;
  await userEvent.click(within(row).getByRole("button")); await userEvent.click(await screen.findByRole("menuitem", { name: "Permissions" }));
  const dialog = screen.getByRole("dialog"); await waitFor(() => expect(within(dialog).getAllByRole("spinbutton")[0]).toHaveValue(10)); return dialog;
}
function label(dialog: HTMLElement, text: string) {
  const labels = Array.from(dialog.querySelectorAll("label")).filter(item => item.textContent === text);
  expect(labels).toHaveLength(1); return labels[0];
}
function uniqueTargets(dialogs: HTMLElement[]) {
  const labels = dialogs.flatMap(dialog => Array.from(dialog.querySelectorAll<HTMLLabelElement>("label[for]")));
  expect(labels.length).toBeGreaterThanOrEqual(dialogs.length * 10);
  const targets = labels.map(item => item.htmlFor); expect(new Set(targets).size).toBe(targets.length);
  for (const item of labels) { const target = document.getElementById(item.htmlFor); expect(target).not.toBeNull(); expect(item.closest('[role="dialog"]')).toContainElement(target); }
}

describe.each(["create", "edit"] as const)("profile %s accessible controls", mode => {
  it("names every text, quota and boolean control from its visible label", async () => {
    const dialog = await openProfile(mode);
    for (const name of ["Name", "Description"]) expect(within(dialog).getByRole("textbox", { name })).toBe(within(dialog).getByLabelText(name));
    for (const name of profileQuotas) expect(within(dialog).getByRole("spinbutton", { name })).toBe(within(dialog).getByLabelText(name));
    for (const name of ["Can send", "Can create domains", "Can create routes", "Can create API keys"]) {
      const control = within(dialog).getByRole("switch", { name }), before = control.getAttribute("aria-checked");
      await userEvent.click(label(dialog, name)); expect(control.getAttribute("aria-checked")).not.toBe(before);
    }
    uniqueTargets([dialog]);
  });
  it("supports named controls and keyboard edits without changing the serialized command", async () => {
    const dialog = await openProfile(mode), name = within(dialog).getByRole("textbox", { name: "Name" });
    await userEvent.clear(name); await userEvent.type(name, "Keyboard profile");
    name.focus(); await userEvent.tab(); expect(within(dialog).getByRole("textbox", { name: "Description" })).toHaveFocus();
    const toggle = within(dialog).getByRole("switch", { name: "Can send" }); toggle.focus(); await userEvent.keyboard(" "); expect(toggle).not.toBeChecked();
    fireEvent.change(within(dialog).getByRole("spinbutton", { name: "Daily send quota" }), { target: { value: "0" } });
    await userEvent.click(within(dialog).getByRole("button", { name: mode === "create" ? "Create Profile" : "Save" }));
    await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0]).toMatchObject({ method: mode === "create" ? "POST" : "PATCH", body: { name: "Keyboard profile", can_send: false, daily_send_quota: 0 } });
    if (mode === "edit") expect(writes[0].body.expected_revision).toBe(profile.revision);
  });
});
it("names the platform profile scope selector and connects its visible label", async () => {
  auth.level = "super_admin"; identity(); const dialog = await openProfile("create");
  const select = within(dialog).getByRole("combobox", { name: "Profile scope" }); expect(within(dialog).getByLabelText("Profile scope")).toBe(select);
  await userEvent.click(select); await userEvent.click(await screen.findByRole("option", { name: /Current company/ })); expect(within(dialog).getByRole("switch", { name: "accessible.example.test" })).toBeInTheDocument(); expect(writes).toHaveLength(0);
});
it("keeps unique label targets when two profile forms are open at the same time", async () => {
  mount(<><PermissionsPage /><PermissionsPage /></>); await screen.findAllByText(profile.name);
  const triggers = screen.getAllByRole("button", { name: "Create Profile" }); fireEvent.click(triggers[0]); fireEvent.click(triggers[1]); await settle();
  const dialogs = Array.from(document.querySelectorAll<HTMLElement>('[role="dialog"]')); expect(dialogs).toHaveLength(2); uniqueTargets(dialogs);
});
it("names a profile domain switch and preserves label and keyboard activation", async () => {
  const dialog = await openProfile("edit"), toggle = within(dialog).getByRole("switch", { name: "accessible.example.test" });
  expect(toggle).toBeChecked(); await userEvent.click(label(dialog, "accessible.example.test")); expect(toggle).not.toBeChecked(); toggle.focus(); await userEvent.keyboard(" "); expect(toggle).toBeChecked(); expect(writes).toHaveLength(0);
});
it("names all member override inputs, switches and the profile selector", async () => {
  const dialog = await openUser(); expect(within(dialog).getByLabelText("Permission Profile")).toBe(within(dialog).getByRole("combobox", { name: "Permission Profile" }));
  for (const name of userQuotas) expect(within(dialog).getByLabelText(name)).toBe(within(dialog).getByRole("spinbutton", { name }));
  for (const [, name] of booleans) expect(within(dialog).getByRole("switch", { name })).toBeInTheDocument(); uniqueTargets([dialog]);
});
it.each(booleans)("the named inherit action clears only %s to null through the real serializer", async (key, name) => {
  const dialog = await openUser(), inherit = within(dialog).getByRole("button", { name: `Inherit: ${name}` }); expect(inherit).toHaveAttribute("title", `Inherit: ${name}`);
  inherit.focus(); await userEvent.keyboard("{Enter}"); expect(within(dialog).queryByRole("button", { name: `Inherit: ${name}` })).not.toBeInTheDocument(); expect(writes).toHaveLength(0);
  await userEvent.click(within(dialog).getByRole("button", { name: "Save Overrides" })); await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0]).toEqual({ path: `/api/v1/admin/users/${userID}/permission-editor`, method: "PATCH", body: { expected_revision: current.revision, patch: { [key]: null } } });
});
it("a named member switch supports visible label and keyboard activation with explicit false", async () => {
  const dialog = await openUser(), toggle = within(dialog).getByRole("switch", { name: "Can Send" }); await userEvent.click(label(dialog, "Can Send")); expect(toggle).not.toBeChecked();
  toggle.focus(); await userEvent.keyboard(" "); expect(toggle).toBeChecked(); await userEvent.keyboard(" "); expect(toggle).not.toBeChecked();
  await userEvent.click(within(dialog).getByRole("button", { name: "Save Overrides" })); await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toEqual({ expected_revision: current.revision, patch: { can_send: false } });
});
it.each([["", null], ["0", 0]] as const)("a named quota keeps %j distinct in the wire command", async (value, expected) => {
  const dialog = await openUser(); fireEvent.change(within(dialog).getByRole("spinbutton", { name: "Daily Send Quota" }), { target: { value } });
  await userEvent.click(within(dialog).getByRole("button", { name: "Save Overrides" })); await waitFor(() => expect(writes).toHaveLength(1));
  expect(writes[0].body).toEqual({ expected_revision: current.revision, patch: { daily_send_quota: expected } });
});
it("names the member domain switch and the domain-specific inherit action", async () => {
  const dialog = await openUser(); expect(within(dialog).getByRole("switch", { name: "accessible.example.test" })).toBeChecked();
  await userEvent.click(within(dialog).getByRole("button", { name: "Inherit: Allowed Domain Scope" })); await userEvent.click(within(dialog).getByRole("button", { name: "Save Overrides" }));
  await waitFor(() => expect(writes).toHaveLength(1)); expect(writes[0].body).toEqual({ expected_revision: current.revision, patch: { domain_access: { mode: "inherit", zone_ids: [] } } });
});
it("retains accessible names and disables named member controls while a write is pending", async () => {
  writeReply = () => new Promise<Response>(() => {}); const dialog = await openUser();
  fireEvent.change(within(dialog).getByRole("spinbutton", { name: "Daily Send Quota" }), { target: { value: "25" } }); await userEvent.click(within(dialog).getByRole("button", { name: "Save Overrides" })); await waitFor(() => expect(writes).toHaveLength(1));
  for (const name of userQuotas) expect(within(dialog).getByRole("spinbutton", { name })).toBeDisabled();
  for (const [, name] of booleans) { expect(within(dialog).getByRole("switch", { name })).toHaveAttribute("aria-disabled", "true"); expect(within(dialog).getByRole("button", { name: `Inherit: ${name}` })).toBeDisabled(); }
});
it("retains accessible names for a read-only snapshot without enabling mutations", async () => {
  current.capabilities.patch = false; current.capabilities.assign_profile = false; const dialog = await openUser();
  for (const name of userQuotas) expect(within(dialog).getByRole("spinbutton", { name })).toBeDisabled();
  for (const [, name] of booleans) expect(within(dialog).getByRole("switch", { name })).toHaveAttribute("aria-disabled", "true");
  expect(within(dialog).getByRole("combobox", { name: "Permission Profile" })).toBeDisabled(); expect(writes).toHaveLength(0);
});
