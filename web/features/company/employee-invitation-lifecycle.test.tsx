import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import EmployeesPage from "@/app/(dashboard)/company/employees/page";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import type { Invitation } from "@/lib/company";

// Actual page, employee form, SWR, session and HTTP adapter. Only the remote
// peer, clipboard and notifications are controlled; pending peers ignore abort.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({ usePathname: () => "/company/employees" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({
  user: JSON.parse(localStorage.getItem("tabmail_user") ?? "null"),
  tenantId: localStorage.getItem("tabmail_tenant_id"),
}) }));

const tenant = "invitation-company";
const token = "a".repeat(64);
const anotherToken = "b".repeat(64);
const recipient = { email: "alice@example.test", display_name: "Alice Example", local_part: "alice" };
const replacement = { email: "bob@example.test", display_name: "Bob Example", local_part: "bob" };
const invitation: Invitation = { id: "invitation-alice", email: recipient.email,
  display_name: recipient.display_name, mailbox_address: "alice@company.test",
  created_at: "2026-10-08T00:00:00Z", expires_at: "2026-10-11T00:00:00Z" };
const otherInvitation: Invitation = { ...invitation, id: "invitation-bob", email: replacement.email,
  display_name: replacement.display_name, mailbox_address: "bob@company.test" };
const settings = { tenant_id: tenant, name: "Invitation Company", domain: "company.test",
  primary_zone_id: "zone-company", revision: 3, mail_send_policy: "free" };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: {
  code: status === 403 ? "FORBIDDEN" : "UNAVAILABLE", message: `Synthetic invitation ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
const accepted = (value = invitation, secret = token) => json({ invitation: value, activation_token: secret });
type Call = { path: string; method: string; body: Record<string, unknown>; scope: string; tenant: string | null };
let calls: Call[];
let catalog: Invitation[];
let readInvitations: () => Promise<Response>;
let readSettings: () => Promise<Response>;
let invite: (call: Call) => Promise<Response>;
let revoke: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
let copy: ReturnType<typeof vi.fn>;
const creates = () => calls.filter(call => call.method === "POST");
const deletes = () => calls.filter(call => call.method === "DELETE");
const reads = () => calls.filter(call => call.method === "GET");
const createButton = () => screen.getByRole("button", { name: "Create employee invitation" });
const link = () => screen.queryByLabelText("One-time activation link (not retained after leaving)") as HTMLInputElement | null;
const input = (label: string) => screen.getByLabelText(label);
const fill = (draft = recipient) => {
  fireEvent.change(input("Login email"), { target: { value: draft.email } });
  fireEvent.change(input("Display name"), { target: { value: draft.display_name } });
  fireEvent.change(input("Company mailbox local part"), { target: { value: draft.local_part } });
};
const expectDraft = (draft = recipient) => {
  expect(input("Login email")).toHaveValue(draft.email);
  expect(input("Display name")).toHaveValue(draft.display_name);
  expect(input("Company mailbox local part")).toHaveValue(draft.local_part);
};
function identity(id = "invitation-admin", company = tenant, role: AuthUser["role"] = "admin") {
  installSession(`${id}-${company}-${role}`, { id, tenant_id: company, role, email: `${id}@example.test`, display_name: id });
}
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function RefreshReads() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => { void mutate(key => Array.isArray(key) && key[1] === sessionScope() &&
    ["company-invitations", "company-settings"].includes(key[2])).catch(() => undefined); }}>Refresh invitation reads</button>;
}
function mount() {
  return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><EmployeesPage /><RefreshReads /></SWRConfig>);
}
async function loaded() { await waitFor(() => expect(input("Company mailbox local part")).toHaveAttribute("placeholder", "alice @ company.test")); await settle(); }
async function start() { await loaded(); fill(); fireEvent.click(createButton()); await waitFor(() => expect(creates()).toHaveLength(1)); }
async function showCreated() { mount(); await start(); await waitFor(() => expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`)); await settle(); }
function revokeButton(value = invitation) {
  const row = screen.getByText(`${value.display_name} · ${value.email}`).closest("div.flex") as HTMLElement;
  return within(row).getByRole("button", { name: "Revoke invitation" });
}

beforeEach(() => {
  calls = []; catalog = []; pending = [];
  identity();
  readInvitations = async () => json(catalog);
  readSettings = async () => json({ ...settings, tenant_id: localStorage.getItem("tabmail_tenant_id") });
  invite = async () => { catalog = [invitation]; return accepted(); };
  revoke = async call => { catalog = catalog.map(value => call.path.endsWith(`/${value.id}`) ? { ...value, revoked_at: "2026-10-08T01:00:00Z" } : value); return json({ revoked: true }); };
  copy = vi.fn(async () => undefined);
  vi.stubGlobal("navigator", { clipboard: { writeText: copy } });
  vi.stubGlobal("fetch", async (url: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(url), "http://localhost").pathname,
      method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {},
      scope: sessionScope(), tenant: new Headers(init?.headers).get("X-Tenant-ID") };
    calls.push(call);
    if (call.path === "/api/v1/admin/users" && call.method === "GET") return new Response(JSON.stringify({ data: [], meta: { page: 1, per_page: 100, total: 0 } }), { headers: { "Content-Type": "application/json" } });
    if (call.path === "/api/v1/company/settings" && call.method === "GET") return readSettings();
    if (call.path === "/api/v1/company/invitations" && call.method === "GET") return readInvitations();
    if (call.path === "/api/v1/company/invitations" && call.method === "POST") return invite(call);
    if (call.path.startsWith("/api/v1/company/invitations/") && call.method === "DELETE") return revoke(call);
    throw new Error(`Unexpected employee invitation request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup(); await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.unstubAllGlobals();
});

describe("company employee invitation lifecycle", () => {
  it("shows the one-time result with its exact recipient and clears only the submitted draft", async () => {
    await showCreated();
    expect(creates()[0]).toMatchObject({ body: recipient, tenant });
    expectDraft({ email: "", display_name: "", local_part: "" });
    expect(screen.getByText(`Invitation for: ${invitation.display_name} · ${invitation.email} · ${invitation.mailbox_address}`)).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Invitation created");
    expect(Object.values(localStorage).join(" ")).not.toContain(token);
  });

  it("preserves the next employee's edits and keeps the acknowledged link assigned to the original employee", async () => {
    const gate = delayed(); invite = () => gate.promise; mount(); await start(); fill(replacement);
    await act(async () => gate.resolve(accepted()));
    expectDraft(replacement);
    expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
    expect(screen.getByText(`Invitation for: ${invitation.display_name} · ${invitation.email} · ${invitation.mailbox_address}`)).toBeInTheDocument();
    expect(creates()).toHaveLength(1);
  });

  it.each(["Login email", "Display name", "Company mailbox local part"])("does not discard an A → B → A edit of %s", async label => {
    const gate = delayed(); invite = () => gate.promise; mount(); await start();
    const original = (input(label) as HTMLInputElement).value;
    fireEvent.change(input(label), { target: { value: "changed-value" } });
    fireEvent.change(input(label), { target: { value: original } });
    await act(async () => gate.resolve(accepted()));
    expectDraft(); expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
  });

  it.each(["account", "tenant", "role"])("retires the displayed activation credential when the %s changes", async change => {
    await showCreated(); fill(replacement);
    act(() => identity(change === "account" ? "replacement-admin" : "invitation-admin", change === "tenant" ? "other-company" : tenant, change === "role" ? "super_admin" : "admin"));
    expect(link()).not.toBeInTheDocument(); expectDraft({ email: "", display_name: "", local_part: "" });
    expect(screen.queryByRole("button", { name: "Copy activation link" })).not.toBeInTheDocument();
  });

  it("keeps the current result across an access-token-only rotation", async () => {
    await showCreated(); const scope = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "rotated-invitation-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope); expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
  });

  it.each([false, true])("ignores a retired page's pending create completion (failure=%s)", async error => {
    const gate = delayed(); invite = () => gate.promise; const view = mount(); await start();
    view.unmount(); const count = reads().length;
    await act(async () => gate.resolve(error ? failed() : accepted()));
    expect(reads()).toHaveLength(count);
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("allows a replacement session to invite independently without an old finally releasing its busy state", async () => {
    const first = delayed(); const second = delayed(); invite = () => creates().length === 1 ? first.promise : second.promise;
    mount(); await start(); act(() => identity("replacement-admin")); await loaded(); fill(replacement);
    expect(createButton()).toBeEnabled(); fireEvent.click(createButton());
    await waitFor(() => expect(creates()).toHaveLength(2));
    await act(async () => first.resolve(accepted()));
    expect(createButton()).toBeDisabled(); expectDraft(replacement); expect(link()).not.toBeInTheDocument();
    await act(async () => second.resolve(accepted(otherInvitation, anotherToken)));
    expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${anotherToken}`);
    expect(toast.success).toHaveBeenCalledTimes(1);
  });

  it("prevents synchronous duplicate submits while an invitation is in flight", async () => {
    const gate = delayed(); invite = () => gate.promise; mount(); await loaded(); fill();
    const button = createButton(); act(() => { fireEvent.click(button); fireEvent.click(button); });
    expect(creates()).toHaveLength(1); expect(button).toBeDisabled();
    await act(async () => gate.resolve(accepted())); expect(creates()).toHaveLength(1);
  });

  it("preserves a successful one-time credential and new draft when a later explicit create fails", async () => {
    await showCreated(); fill(replacement); invite = async () => failed();
    fireEvent.click(createButton()); await waitFor(() => expect(toast.error).toHaveBeenCalled());
    expectDraft(replacement); expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
    expect(creates()).toHaveLength(2);
  });

  it("identifies an acknowledged invitation separately from a failed list readback and retries only GET", async () => {
    mount(); await loaded(); readInvitations = async () => failed(); fill(); fireEvent.click(createButton());
    await screen.findByRole("alert");
    expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
    expect(screen.getByText("The invitation was created, but the invitation list could not be refreshed. Keep this activation link and retry loading.")).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Invitation created");
    readInvitations = async () => json(catalog);
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByText(`${invitation.display_name} · ${invitation.email}`);
    expect(creates()).toHaveLength(1); expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
  });

  it("does not create from cached settings after their latest read failed", async () => {
    mount(); await loaded(); fill(); readSettings = async () => failed();
    fireEvent.click(screen.getByRole("button", { name: "Refresh invitation reads" })); await screen.findByRole("alert");
    expect(createButton()).toBeDisabled(); fireEvent.click(createButton()); expect(creates()).toHaveLength(0); expectDraft();
    readSettings = async () => json(settings); fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(createButton()).toBeEnabled()); expect(creates()).toHaveLength(0);
  });

  it("clears the displayed token only when its own invitation was revoked successfully", async () => {
    await showCreated(); fireEvent.click(revokeButton());
    await waitFor(() => expect(deletes()).toHaveLength(1)); await settle();
    expect(link()).not.toBeInTheDocument(); expect(screen.getByText("Revoked")).toBeInTheDocument();
  });

  it("keeps the displayed link when a different invitation is revoked", async () => {
    invite = async () => { catalog = [invitation, otherInvitation]; return accepted(); };
    await showCreated(); fireEvent.click(revokeButton(otherInvitation));
    await waitFor(() => expect(deletes()).toHaveLength(1)); await settle();
    expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
  });

  it.each([false, true])("does not refresh or notify after a revoke's page is retired (failure=%s)", async error => {
    catalog = [invitation]; const gate = delayed(); revoke = () => gate.promise;
    const view = mount(); await screen.findByText(`${invitation.display_name} · ${invitation.email}`);
    fireEvent.click(revokeButton()); await waitFor(() => expect(deletes()).toHaveLength(1));
    view.unmount(); const count = reads().length;
    await act(async () => gate.resolve(error ? failed() : json({ revoked: true })));
    expect(reads()).toHaveLength(count); expect(toast.error).not.toHaveBeenCalled();
  });

  it("copies only the visible current result and reports copy failure without losing the link", async () => {
    await showCreated(); copy.mockRejectedValue(new Error("Clipboard unavailable"));
    fireEvent.click(screen.getByRole("button", { name: "Copy activation link" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Clipboard unavailable"));
    expect(copy).toHaveBeenCalledWith(`${window.location.origin}/auth/activate#${token}`);
    expect(link()).toHaveValue(`${window.location.origin}/auth/activate#${token}`);
  });

  it("does not notify a replacement session when a pending clipboard write completes", async () => {
    let finish!: () => void; copy.mockImplementation(() => new Promise<void>(resolve => { finish = resolve; }));
    await showCreated(); vi.mocked(toast.success).mockClear();
    fireEvent.click(screen.getByRole("button", { name: "Copy activation link" }));
    act(() => identity("replacement-admin")); await act(async () => finish());
    expect(toast.success).not.toHaveBeenCalled(); expect(link()).not.toBeInTheDocument();
  });
});
