import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import EmployeesPage from "@/app/(dashboard)/company/employees/page";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AdminUser } from "@/lib/types";
import type { OffboardingPlan } from "./api";

// Real employee page, handover panel, session-scoped SWR and HTTP adapter.
// Only the remote peer and confirmation/notification surfaces are controlled.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
vi.mock("next/navigation", () => ({ usePathname: () => "/company/employees" }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => ({
  user: JSON.parse(localStorage.getItem("tabmail_user") ?? "null"),
  tenantId: localStorage.getItem("tabmail_tenant_id"),
}) }));

const tenant = "handover-company";
const employee = (id: string, is_active = true): AdminUser => ({
  id, tenant_id: tenant, role: "user", is_active, display_name: id,
  email: `${id}@example.test`, created_at: "2026-10-09T00:00:00Z", updated_at: "2026-10-09T00:00:00Z",
});
const members = [employee("frozen-target", false), employee("active-successor"), employee("other-member")];
const reason = "Reviewed handover ticket 104";
const plan: OffboardingPlan = {
  id: "read-reviewed-plan", target_id: members[0].id, successor_id: members[1].id,
  reason, state: "preview", options: { drafts: "seal" }, expires_at: "2030-01-01T00:00:00Z",
  impact: { mailboxes: 1, drafts: 2, transferable_drafts: 1, attachments: 3, api_keys: 1,
    grants: 2, queued: 1, in_flight: 1, uncertain: 1 },
};
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const directory = (rows = members) => new Response(JSON.stringify({ data: rows,
  meta: { page: 1, per_page: 100, total: rows.length } }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: {
  code: status === 403 ? "FORBIDDEN" : "UNAVAILABLE", message: `Synthetic member read ${status}`,
} }), { status, headers: { "Content-Type": "application/json" } });
type Call = { path: string; method: string; body: Record<string, unknown>; scope: string };
let calls: Call[];
let readMembers: () => Promise<Response>;
let preview: () => Promise<Response>;
let execute: () => Promise<Response>;
let pending: Array<(response: Response) => void>;
const reads = () => calls.filter(call => call.path === "/api/v1/admin/users");
const previews = () => calls.filter(call => call.path.endsWith("/offboard/preview"));
const executions = () => calls.filter(call => call.path.endsWith("/offboard"));
const previewButton = () => screen.getByRole("button", { name: "Preview handover impact" });
const confirmation = () => screen.queryByRole("button", { name: "Confirm handover" });
const membersTable = () => screen.getByRole("table");
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function identity(id = "handover-admin") {
  installSession(`token-${id}`, { id, tenant_id: tenant, role: "admin", email: `${id}@example.test`, display_name: id });
}
function RefreshMembers() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => void mutate(key => Array.isArray(key) && key[1] === sessionScope() &&
    key[2] === "company-employees").catch(() => undefined)}>Refresh member directory</button>;
}
function mount() {
  return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><EmployeesPage /><RefreshMembers /></SWRConfig>);
}
async function settled() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
async function select() {
  await screen.findByRole("option", { name: "frozen-target · frozen-target@example.test" });
  await settled();
  fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: members[0].id } });
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: members[1].id } });
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: reason } });
}
async function reviewed() {
  await select(); fireEvent.click(previewButton());
  await waitFor(() => expect(confirmation()).toBeEnabled());
}
async function refresh() {
  const count = reads().length;
  fireEvent.click(screen.getByRole("button", { name: "Refresh member directory" }));
  await waitFor(() => expect(reads()).toHaveLength(count + 1));
  await settled();
}
function noExecutablePlan() {
  if (confirmation()) expect(confirmation()).toBeDisabled();
  expect(executions()).toHaveLength(0);
}

beforeEach(() => {
  calls = []; pending = [];
  identity();
  readMembers = async () => directory();
  preview = async () => json(plan);
  execute = async () => json({ ...plan, state: "executed", executed_at: "2026-10-09T00:00:00Z" });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname,
      method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {}, scope: sessionScope() };
    calls.push(call);
    if (call.path === "/api/v1/admin/users" && call.method === "GET") return readMembers();
    if (call.path === "/api/v1/company/settings" && call.method === "GET") return json({ tenant_id: tenant, name: "Handover Company", domain: "company.test", revision: 2 });
    if (call.path === "/api/v1/company/invitations" && call.method === "GET") return json([]);
    if (call.path.endsWith("/offboard/preview") && call.method === "POST") return preview();
    if (call.path.endsWith("/offboard") && call.method === "POST") return execute();
    throw new Error(`Unexpected handover request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(resolve => resolve(directory()))); vi.unstubAllGlobals(); });

describe("handover member read ownership", () => {
  it("executes an explicitly reviewed frozen target once and retains the acknowledged receipt through its member refresh", async () => {
    mount(); await reviewed();
    const next = deferred(); readMembers = () => next.promise;
    fireEvent.click(confirmation()!);
    await waitFor(() => expect(reads()).toHaveLength(2));
    expect(screen.getByText(/Handover completed; the disposition receipt is retained/)).toBeInTheDocument();
    await act(async () => next.resolve(directory()));
    expect(screen.getByText(/Handover completed; the disposition receipt is retained/)).toBeInTheDocument();
    expect(previews()[0].body).toEqual({ successor_user_id: members[1].id, options: { drafts: "seal" }, reason });
    expect(executions()).toHaveLength(1); expect(executions()[0].body).toEqual({ plan_id: plan.id });
    expect(confirmation()).not.toBeInTheDocument();
  });

  it("announces initial member loading and never implies an empty completed directory", async () => {
    const first = deferred(); readMembers = () => first.promise; mount();
    await waitFor(() => expect(reads()).toHaveLength(1));
    expect(screen.getByText("Loading members…")).toHaveAttribute("role", "status");
    expect(screen.queryByText("No members")).not.toBeInTheDocument();
    expect(previewButton()).toBeDisabled();
    await act(async () => first.resolve(directory([])));
    expect(screen.getByText("No members")).toBeInTheDocument();
  });

  it.each([403, 503])("disables cached selections after member read %s and retries without losing the form", async status => {
    mount(); await select(); readMembers = async () => failed(status); await refresh();
    await screen.findByRole("alert");
    expect(previewButton()).toBeDisabled(); fireEvent.click(previewButton()); expect(previews()).toHaveLength(0);
    expect(within(membersTable()).queryByText("frozen-target@example.test")).not.toBeInTheDocument();
    expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
    readMembers = async () => directory(); fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(previewButton()).toBeEnabled());
    expect(screen.getByLabelText("Employee to offboard")).toHaveValue(members[0].id);
    expect(screen.getByLabelText("Mailbox successor")).toHaveValue(members[1].id);
    expect(previews()).toHaveLength(0);
  });

  it("blocks preview and hides stale member rows while a refresh remains in flight", async () => {
    mount(); await select(); const next = deferred(); readMembers = () => next.promise; await refresh();
    expect(previewButton()).toBeDisabled();
    expect(screen.getByText("Refreshing members…")).toHaveAttribute("role", "status");
    expect(within(membersTable()).queryByText("frozen-target@example.test")).not.toBeInTheDocument();
    await act(async () => next.resolve(directory()));
    expect(previewButton()).toBeEnabled();
  });

  it.each(["success", "403", "503"])("retires a reviewed plan across a member refresh ending in %s", async outcome => {
    mount(); await reviewed(); const next = deferred(); readMembers = () => next.promise; await refresh();
    noExecutablePlan();
    await act(async () => next.resolve(outcome === "success" ? directory() : failed(Number(outcome))));
    if (outcome !== "success") {
      readMembers = async () => directory(); fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
      await waitFor(() => expect(previewButton()).toBeEnabled());
    }
    noExecutablePlan();
    expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
    fireEvent.click(previewButton()); await waitFor(() => expect(confirmation()).toBeEnabled());
    expect(previews()).toHaveLength(2);
  });

  it.each([false, true])("ignores a pending preview after the member read is retired (failure=%s)", async error => {
    const action = deferred(); preview = () => action.promise; mount(); await select(); fireEvent.click(previewButton());
    await waitFor(() => expect(previews()).toHaveLength(1));
    const next = deferred(); readMembers = () => next.promise; await refresh();
    await act(async () => next.resolve(directory()));
    await act(async () => action.resolve(error ? failed() : json(plan)));
    noExecutablePlan(); expect(screen.queryByText(new RegExp(plan.id))).not.toBeInTheDocument();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each([false, true])("ignores an execution completion after an independent member refresh (failure=%s)", async error => {
    const action = deferred(); execute = () => action.promise; mount(); await reviewed(); fireEvent.click(confirmation()!);
    await waitFor(() => expect(executions()).toHaveLength(1));
    const next = deferred(); readMembers = () => next.promise; await refresh(); await act(async () => next.resolve(directory()));
    await act(async () => action.resolve(error ? failed() : json({ ...plan, state: "executed" })));
    expect(screen.queryByText(/Handover completed/)).not.toBeInTheDocument();
    expect(reads()).toHaveLength(2); expect(toast.error).not.toHaveBeenCalled();
    expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
  });

  it("keeps a reviewed plan through token-only rotation without a new member read", async () => {
    mount(); await reviewed(); const scope = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "rotated-only"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope); expect(confirmation()).toBeEnabled(); expect(reads()).toHaveLength(1);
  });

  it("retires the form and plan immediately when the account is replaced", async () => {
    mount(); await reviewed(); act(() => identity("replacement-admin"));
    expect(confirmation()).not.toBeInTheDocument();
    expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue("");
    expect(executions()).toHaveLength(0);
  });

  it("does not resurrect a pending preview across read failure and recovery with identical rows", async () => {
    const action = deferred(); preview = () => action.promise; mount(); await select(); fireEvent.click(previewButton());
    await waitFor(() => expect(previews()).toHaveLength(1)); readMembers = async () => failed(); await refresh();
    await screen.findByRole("alert"); readMembers = async () => directory();
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(reads()).toHaveLength(3)); await settled();
    await act(async () => action.resolve(json(plan)));
    noExecutablePlan(); expect(previewButton()).toBeEnabled();
  });
});
