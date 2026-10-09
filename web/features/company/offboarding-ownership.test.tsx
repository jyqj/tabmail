import React from "react";
import { act, cleanup, fireEvent, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import type { AdminUser, AuthUser } from "@/lib/types";
import { I18nProvider } from "@/lib/i18n";
import { AUTH_EVENT } from "@/lib/session";
import type { OffboardingPlan } from "./api";
import { OffboardingPanel } from "./offboarding-panel";

const { render } = await vi.importActual<typeof import("@testing-library/react")>("@testing-library/react");
const host = vi.hoisted(() => ({ user: null as AuthUser | null, tenantId: null as string | null }));
const api = vi.hoisted(() => ({ preview: vi.fn(), execute: vi.fn(), error: vi.fn() }));
vi.mock("@/contexts/auth-context", () => ({ useAuth: () => host }));
vi.mock("./api", () => ({ previewOffboarding: api.preview, executeOffboarding: api.execute }));
vi.mock("sonner", () => ({ toast: { error: api.error } }));

const employee = (id: string, is_active = true): AdminUser => ({
  id, role: "user", tenant_id: "company", is_active, email: `${id}@fixture.test`,
  display_name: id, created_at: "", updated_at: "",
});
const rows = [employee("target", false), employee("successor"), employee("other")];
const reason = "Handover ticket 123";
const plan: OffboardingPlan = {
  id: "handover-plan-one", target_id: "target", successor_id: "successor", options: { drafts: "seal" },
  reason, state: "preview", expires_at: "2030-01-01T00:00:00Z",
  impact: { mailboxes: 1, drafts: 2, transferable_drafts: 1, attachments: 0, api_keys: 0, grants: 0, queued: 0, in_flight: 1, uncertain: 1 },
};
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function setup() {
  const refresh = vi.fn(async () => {});
  let currentRows = rows;
  const view = render(<OffboardingPanel employees={currentRows} refresh={refresh} />, { wrapper: I18nProvider });
  return {
    ...view, refresh,
    update(nextRows = currentRows) {
      currentRows = nextRows;
      view.rerender(<OffboardingPanel employees={currentRows} refresh={refresh} />);
    },
  };
}
function select() {
  fireEvent.change(screen.getByLabelText("Employee to offboard"), { target: { value: "target" } });
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: "successor" } });
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: reason } });
}
function preview() {
  fireEvent.click(screen.getByRole("button", { name: "Preview handover impact" }));
}
async function readyPlan() {
  select();
  preview();
  await waitFor(() => expect(screen.getByRole("button", { name: "Confirm handover" })).toBeEnabled());
}
function noExecutablePlan() {
  const confirm = screen.queryByRole("button", { name: "Confirm handover" });
  if (confirm) expect(confirm).toBeDisabled();
  expect(api.execute).not.toHaveBeenCalled();
}
beforeEach(() => {
  host.user = { id: "caller", role: "admin", tenant_id: "company", email: "caller@fixture.test", display_name: "Caller" };
  host.tenantId = null;
  api.preview.mockReset().mockResolvedValue(plan);
  api.execute.mockReset().mockResolvedValue({ ...plan, state: "executed", executed_at: "2026-10-07T15:00:00Z" });
  api.error.mockReset();
  vi.spyOn(window, "confirm").mockReturnValue(true);
});
afterEach(() => { cleanup(); vi.restoreAllMocks(); });

it("keeps the current preview and executes its exact plan once", async () => {
  const view = setup();
  await readyPlan();
  expect(api.preview).toHaveBeenCalledWith("target", "successor", { drafts: "seal" }, reason);
  const execute = screen.getByRole("button", { name: "Confirm handover" });
  fireEvent.click(execute);
  fireEvent.click(execute);
  await waitFor(() => expect(screen.getByText(/Handover completed/)).toBeInTheDocument());
  expect(api.execute).toHaveBeenCalledExactlyOnceWith("target", plan.id);
  expect(view.refresh).toHaveBeenCalledTimes(1);
});

it.each([
  ["Employee to offboard", "other"],
  ["Mailbox successor", "other"],
  ["Draft disposition", "discard"],
  ["Reason / ticket (at least 8 characters)", "New intentional reason"],
])("discards a delayed preview after changing %s and preserves the new input", async (label, value) => {
  const pending = deferred<OffboardingPlan>();
  api.preview.mockReturnValueOnce(pending.promise);
  setup(); select(); preview();
  fireEvent.change(screen.getByLabelText(label), { target: { value } });
  await act(async () => pending.resolve(plan));
  noExecutablePlan();
  expect(screen.queryByText(new RegExp(plan.id))).not.toBeInTheDocument();
  expect(screen.getByLabelText(label)).toHaveValue(value);
  expect(api.error).not.toHaveBeenCalled();
});

it("does not revive a pending preview after selection moves away and back", async () => {
  const pending = deferred<OffboardingPlan>();
  api.preview.mockReturnValueOnce(pending.promise);
  setup(); select(); preview();
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: "other" } });
  fireEvent.change(screen.getByLabelText("Mailbox successor"), { target: { value: "successor" } });
  await act(async () => pending.resolve(plan));
  noExecutablePlan();
  expect(screen.getByLabelText("Mailbox successor")).toHaveValue("successor");
});

const changes = ["role", "tenant", "target", "successor"] as const;
function changeAuthority(change: typeof changes[number], view: ReturnType<typeof setup>) {
  if (change === "role") host.user = { ...host.user!, role: "user" };
  if (change === "tenant") host.tenantId = "other-company";
  view.update(change === "target" ? rows.filter(row => row.id !== "target") :
    change === "successor" ? rows.map(row => row.id === "successor" ? { ...row, is_active: false } : row) : rows);
}
it.each(changes)("rejects a pending preview after %s authority changes", async change => {
  const pending = deferred<OffboardingPlan>();
  api.preview.mockReturnValueOnce(pending.promise);
  const view = setup(); select(); preview();
  changeAuthority(change, view);
  await act(async () => pending.resolve(plan));
  noExecutablePlan();
  expect(screen.queryByText(new RegExp(plan.id))).not.toBeInTheDocument();
});

it.each(changes)("invalidates an already reviewed plan after %s authority changes", async change => {
  const view = setup();
  await readyPlan();
  changeAuthority(change, view);
  noExecutablePlan();
  expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
  expect(screen.getByRole("alert")).toHaveTextContent("Preview again");
});

it("does not restore a reviewed plan when authority is later restored", async () => {
  const view = setup();
  await readyPlan();
  const caller = host.user!;
  changeAuthority("role", view);
  host.user = caller; view.update();
  noExecutablePlan();
  preview();
  await waitFor(() => expect(screen.getByRole("button", { name: "Confirm handover" })).toBeEnabled());
  expect(api.preview).toHaveBeenCalledTimes(2);
});

it("keeps a reviewed plan through token-only rotation and unrelated member updates", async () => {
  const view = setup();
  await readyPlan();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  view.update(rows.map(row => row.id === "other" ? { ...row, display_name: "Renamed colleague" } : row));
  expect(screen.getByRole("button", { name: "Confirm handover" })).toBeEnabled();
});

it("invalidates a reviewed plan when the session epoch changes", async () => {
  setup();
  await readyPlan();
  act(() => { localStorage.setItem("tabmail_session_epoch", "replacement-session"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  noExecutablePlan();
});

it.each(["success", "conflict"] as const)("ignores an obsolete execution %s without refreshing or resetting the newer form", async result => {
  const pending = deferred<OffboardingPlan>();
  api.execute.mockReturnValueOnce(pending.promise);
  const view = setup();
  await readyPlan();
  fireEvent.click(screen.getByRole("button", { name: "Confirm handover" }));
  changeAuthority("role", view);
  expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: "New review notes" } });
  await act(async () => result === "success" ? pending.resolve({ ...plan, state: "executed" }) : pending.reject({ error: { code: "CONFLICT", message: "Old conflict" } }));
  expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue("New review notes");
  expect(screen.queryByText(/Handover completed/)).not.toBeInTheDocument();
  expect(view.refresh).not.toHaveBeenCalled();
  expect(api.error).not.toHaveBeenCalled();
});

it("suppresses an obsolete preview failure but still reports a current failure", async () => {
  const pending = deferred<OffboardingPlan>();
  api.preview.mockReturnValueOnce(pending.promise);
  setup(); select(); preview();
  fireEvent.change(screen.getByLabelText("Reason / ticket (at least 8 characters)"), { target: { value: "New review notes" } });
  await act(async () => pending.reject(new Error("old preview failure")));
  expect(api.error).not.toHaveBeenCalled();
  api.preview.mockRejectedValueOnce(new Error("current preview failure"));
  preview();
  await waitFor(() => expect(api.error).toHaveBeenCalledExactlyOnceWith("current preview failure"));
});

it.each(["preview", "execute"] as const)("has no late %s effects after unmount", async operation => {
  const pending = deferred<OffboardingPlan>();
  const view = setup();
  if (operation === "preview") { api.preview.mockReturnValueOnce(pending.promise); select(); preview(); }
  else { await readyPlan(); api.execute.mockReturnValueOnce(pending.promise); fireEvent.click(screen.getByRole("button", { name: "Confirm handover" })); }
  view.unmount();
  await act(async () => operation === "preview" ? pending.reject(new Error("unmounted preview failure")) : pending.resolve({ ...plan, state: "executed" }));
  expect(view.refresh).not.toHaveBeenCalled();
  expect(api.error).not.toHaveBeenCalled();
});

it("requires another explicit preview after a current execution conflict and keeps the reason", async () => {
  api.execute.mockRejectedValueOnce({ error: { code: "CONFLICT", message: "Assets changed" } });
  setup();
  await readyPlan();
  fireEvent.click(screen.getByRole("button", { name: "Confirm handover" }));
  await waitFor(() => expect(screen.getByRole("alert")).toHaveTextContent("Preview again"));
  expect(screen.getByLabelText("Reason / ticket (at least 8 characters)")).toHaveValue(reason);
  expect(api.preview).toHaveBeenCalledTimes(1);
  expect(api.execute).toHaveBeenCalledTimes(1);
});
