import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { SidebarProvider } from "@/components/ui/sidebar";
import { installSession } from "@/lib/session";
import PermissionsPage from "./profile-management";

// Actual profile panel, Base UI controls, translations and request/session layer.
// Only the authenticated role, transport responses and toast are controlled.
const auth = vi.hoisted(() => ({ level: "admin" as "admin" | "super_admin", tenantId: "10000000-0000-4000-8000-000000000001" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => auth }));
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const profileId = "40000000-0000-4000-8000-000000000001";
const firstZone = "50000000-0000-4000-8000-000000000001";
const secondZone = "50000000-0000-4000-8000-000000000002";
const path = "/api/v1/admin/permissions";
const profile = {
  id: profileId, tenant_id: auth.tenantId, name: "Pending profile", description: "Original description",
  revision: "9007199254740995", is_system: false, can_send: true,
  daily_send_quota: 10, daily_receive_quota: 20, max_mailboxes: 30, max_domains: 40,
  allowed_zone_ids: [firstZone], can_create_domains: false, can_create_routes: false, can_create_api_keys: false,
  created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z",
};
type Mode = "create" | "edit";
type Write = { method: string; path: string; body: Record<string, unknown>; signal?: AbortSignal | null };
let writes: Write[];
let pendingWrites: ReturnType<typeof deferred>[];
function deferred() {
  let resolve!: (response: Response) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
const reply = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status: number) => new Response(JSON.stringify({ error: {
  code: status === 409 ? "CONFLICT" : "INTERNAL", message: "Controlled write rejection",
} }), { status, headers: { "Content-Type": "application/json" } });
function identity(id = "pending-admin") {
  installSession(`${id}-token`, { id, tenant_id: auth.tenantId, role: auth.level, email: `${id}@example.test`, display_name: id });
}
beforeEach(() => {
  auth.level = "admin"; writes = []; pendingWrites = []; identity();
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({
    matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; },
  }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const method = init?.method ?? "GET";
    if (method !== "GET") {
      if (!((method === "POST" && url.pathname === path) || (method === "PATCH" && url.pathname === `${path}/${profileId}`))) throw new Error(`Unexpected write ${method} ${url.pathname}`);
      const pending = deferred(); pendingWrites.push(pending);
      writes.push({ method, path: url.pathname, body: JSON.parse(String(init?.body)), signal: init?.signal });
      return pending.promise;
    }
    if (url.pathname === "/api/v1/company/events") {
      let close = () => {};
      const abort = () => close();
      const stream = new ReadableStream<Uint8Array>({
        start(controller) { close = () => { controller.close(); close = () => {}; }; },
        cancel() { close = () => {}; init?.signal?.removeEventListener("abort", abort); },
      });
      init?.signal?.addEventListener("abort", abort, { once: true });
      if (init?.signal?.aborted) abort();
      return new Response(stream, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (url.pathname === path) return reply([profile]);
    if (["/api/v1/domains", "/api/v1/admin/domains"].includes(url.pathname)) return reply([
      { id: firstZone, tenant_id: auth.tenantId, domain: "first.example.test" },
      { id: secondZone, tenant_id: auth.tenantId, domain: "second.example.test" },
    ]);
    if (url.pathname === "/api/v1/admin/tenants") return reply([{ id: auth.tenantId, name: "Current company" }]);
    throw new Error(`Unexpected read ${url.pathname}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function open(mode: Mode, mount = true) {
  if (mount) render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><SidebarProvider><PermissionsPage /></SidebarProvider></SWRConfig>);
  await screen.findByText(profile.name);
  if (mode === "create") await userEvent.click(screen.getByRole("button", { name: "Create Profile" }));
  else {
    const row = screen.getByText(profile.name).closest("tr")!;
    await userEvent.click(within(row).getByRole("button"));
    await userEvent.click(await screen.findByRole("menuitem", { name: "Edit" }));
  }
  const dialog = screen.getByRole("dialog");
  await userEvent.clear(within(dialog).getByPlaceholderText("Profile name"));
  await userEvent.type(within(dialog).getByPlaceholderText("Profile name"), "Submitted name");
  await userEvent.clear(within(dialog).getByPlaceholderText("Profile description (optional)"));
  await userEvent.type(within(dialog).getByPlaceholderText("Profile description (optional)"), "Submitted description");
  return dialog;
}
async function submit(dialog: HTMLElement, mode: Mode) {
  const count = writes.length;
  await userEvent.click(within(dialog).getByRole("button", { name: mode === "create" ? "Create Profile" : "Save" }));
  await waitFor(() => expect(writes).toHaveLength(count + 1));
  return pendingWrites.at(-1)!;
}
async function resolve(pending: ReturnType<typeof deferred>, index = writes.length - 1) {
  await act(async () => pending.resolve(reply({ ...profile, ...writes[index].body, revision: "9007199254740997" })));
}
const texts = (dialog: HTMLElement) => [within(dialog).getByPlaceholderText("Profile name"), within(dialog).getByPlaceholderText("Profile description (optional)")];
// The existing unnamed controls are located by their rendered role/order here;
// accessible naming is a separate task, not silently changed by this regression.
const quotas = (dialog: HTMLElement) => within(dialog).getAllByRole("spinbutton");
const toggles = (dialog: HTMLElement) => within(dialog).getAllByRole("switch").slice(0, 4);
function expectSwitchDisabled(control: HTMLElement) {
  // Base UI renders a span with ARIA semantics, not a native disabled button.
  expect(control).toHaveAttribute("aria-disabled", "true");
  expect(control).toHaveAttribute("tabindex", "-1");
}

describe.each(["create", "edit"] as const)("pending profile %s inputs", mode => {
  it("does not accept later text that would be discarded by the submitted result", async () => {
    const dialog = await open(mode);
    await submit(dialog, mode);
    const [name, description] = texts(dialog);
    await userEvent.type(name, " late"); await userEvent.type(description, " late");
    expect(name).toHaveValue("Submitted name"); expect(description).toHaveValue("Submitted description");
    for (const control of texts(dialog)) expect(control).toBeDisabled();
    expect(writes).toHaveLength(1);
  });

  it("keeps every quota fixed while the command is pending", async () => {
    const dialog = await open(mode);
    const before = quotas(dialog).map(input => (input as HTMLInputElement).value);
    await submit(dialog, mode);
    for (const input of quotas(dialog)) await userEvent.type(input, "9");
    expect(quotas(dialog).map(input => (input as HTMLInputElement).value)).toEqual(before);
    for (const input of quotas(dialog)) expect(input).toBeDisabled();
  });

  it("does not change any boolean permission while the command is pending", async () => {
    const dialog = await open(mode);
    const before = toggles(dialog).map(control => control.getAttribute("aria-checked"));
    await submit(dialog, mode);
    for (const control of toggles(dialog)) await userEvent.click(control);
    expect(toggles(dialog).map(control => control.getAttribute("aria-checked"))).toEqual(before);
    for (const control of toggles(dialog)) expectSwitchDisabled(control);
  });

  it("locks both individual domains and the allow-all shortcut", async () => {
    const dialog = await open(mode);
    const domain = () => within(within(dialog).getByText("first.example.test").closest("label")!).getByRole("switch");
    if (mode === "create") await userEvent.click(domain());
    expect(domain()).toBeChecked();
    await submit(dialog, mode);
    await userEvent.click(within(dialog).getByRole("button", { name: "Allow all domains" }));
    expect(domain()).toBeChecked();
    expect(within(dialog).getByRole("button", { name: "Allow all domains" })).toBeDisabled();
    await userEvent.click(domain());
    expect(domain()).toBeChecked(); expectSwitchDisabled(domain());
  });

  it("still completes one successful write and closes its unchanged submitted form", async () => {
    const dialog = await open(mode);
    const pending = await submit(dialog, mode);
    await userEvent.click(within(dialog).getByRole("button", { name: mode === "create" ? "Creating..." : "Saving..." }));
    expect(writes).toHaveLength(1);
    expect(writes[0]).toMatchObject({ method: mode === "create" ? "POST" : "PATCH", body: { name: "Submitted name", description: "Submitted description" } });
    if (mode === "edit") expect(writes[0].body.expected_revision).toBe(profile.revision);
    await resolve(pending);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(toast.success).toHaveBeenCalledTimes(1); expect(toast.error).not.toHaveBeenCalled();
    expect(writes).toHaveLength(1);
  });

  it.each([409, 500, "network"] as const)("preserves and unlocks the submitted values after %s without replaying", async outcome => {
    const dialog = await open(mode);
    const pending = await submit(dialog, mode);
    await act(async () => { if (outcome === "network") pending.reject(new TypeError("Controlled network loss")); else pending.resolve(failure(outcome)); });
    await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expect(texts(dialog)[0]).toHaveValue("Submitted name"); expect(texts(dialog)[1]).toHaveValue("Submitted description");
    for (const control of [...texts(dialog), ...quotas(dialog)]) expect(control).toBeEnabled();
    for (const control of toggles(dialog)) expect(control).not.toHaveAttribute("aria-disabled", "true");
    await userEvent.type(texts(dialog)[1], " revised");
    expect(texts(dialog)[1]).toHaveValue("Submitted description revised");
    if (mode === "edit") {
      expect(within(dialog).getByRole("button", { name: "Save" })).toBeDisabled();
      expect(within(dialog).getByRole("button", { name: "Refresh version" })).toBeEnabled();
    }
    expect(toast.success).not.toHaveBeenCalled(); expect(writes).toHaveLength(1);
  });

  it("an old session completion cannot unlock fields belonging to a newer pending write", async () => {
    let dialog = await open(mode);
    const old = await submit(dialog, mode);
    await act(async () => identity("new-pending-admin"));
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(writes[0].signal?.aborted).toBe(true);
    dialog = await open(mode, false);
    const current = await submit(dialog, mode);
    await resolve(old, 0);
    for (const control of [...texts(dialog), ...quotas(dialog)]) expect(control).toBeDisabled();
    for (const control of toggles(dialog)) expectSwitchDisabled(control);
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
    expect(writes).toHaveLength(2);
    await resolve(current, 1);
    await waitFor(() => expect(screen.queryByRole("dialog")).not.toBeInTheDocument());
    expect(toast.success).toHaveBeenCalledTimes(1);
  });
});

it("locks a platform administrator's profile scope while creation is pending", async () => {
  auth.level = "super_admin"; identity();
  const dialog = await open("create");
  const scope = within(dialog).getByRole("combobox");
  await submit(dialog, "create");
  await userEvent.click(scope);
  expect(screen.queryByRole("option", { name: /Current company/ })).not.toBeInTheDocument();
  expect(scope).toBeDisabled();
  expect(writes[0].body.tenant_id).toBeNull(); expect(writes).toHaveLength(1);
});
