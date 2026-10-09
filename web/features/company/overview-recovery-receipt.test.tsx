import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { AUTH_EVENT, installSession } from "@/lib/session";
import { Overview } from "./overview";

// Mount the real overview, SWR and company/request/session stack. The HTTP
// boundary supplies malformed, delayed and rejected server observations.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const actor = { id: "admin", tenant_id: "company", role: "admin" as const,
  email: "admin@example.test", display_name: "Admin" };
const summary = { mailboxes: 3, active_employees: 3, pending_invitations: 0, queued: 1, uncertain: 0, index_failed: 3 };
const reason = "Storage access restored and verified";
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: "Synthetic recovery failure" } }), { status, headers: { "Content-Type": "application/json" } });
let calls: { path: string; method: string; body: unknown; authorization: string | null }[];
let readReply: () => Promise<Response>;
let writeReply: () => Promise<Response>;
let pending: (() => void)[];
function defer(response: Response) {
  let release!: () => void;
  const promise = new Promise<Response>(resolve => { release = () => resolve(response); });
  pending.push(release); return { promise, release };
}
beforeEach(() => {
  calls = []; pending = []; readReply = async () => json(summary); writeReply = async () => json({ requeued: 2, limit: 100 });
  installSession("index-token", actor);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: init?.body ? JSON.parse(String(init.body)) : undefined, authorization: new Headers(init?.headers).get("Authorization") };
    calls.push(call);
    if (call.path === "/api/v1/company/overview" && call.method === "GET") return readReply();
    if (call.path === "/api/v1/company/index/retry" && call.method === "POST") return writeReply();
    throw new Error(`Unexpected recovery request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(release => release())); vi.unstubAllGlobals(); });
async function mount() {
  const view = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, revalidateOnFocus: false, shouldRetryOnError: false }}><Overview /></SWRConfig>);
  await screen.findByRole("button", { name: "Retry failed indexes" }); return view;
}
const input = () => screen.getByLabelText("Recovery reason");
const retry = () => screen.getByRole("button", { name: "Retry failed indexes" });
const writes = () => calls.filter(call => call.method === "POST");
const reads = () => calls.filter(call => call.method === "GET");
async function start() { fireEvent.change(input(), { target: { value: reason } }); await userEvent.click(retry()); await waitFor(() => expect(writes()).toHaveLength(1)); }

it.each([0, 2, 100])("acknowledges valid bounded receipt count %i, clears the reason and refreshes", async requeued => {
  writeReply = async () => json({ requeued, limit: 100 }); await mount(); await start();
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith(`Requeued ${requeued} index jobs`));
  expect(input()).toHaveValue(""); expect(reads()).toHaveLength(2); expect(writes()).toHaveLength(1);
});
it.each([
  ["null", null], ["array", []], ["missing fields", {}], ["negative count", { requeued: -1, limit: 100 }],
  ["fractional count", { requeued: 1.5, limit: 100 }], ["string count", { requeued: "2", limit: 100 }],
  ["over limit", { requeued: 101, limit: 100 }], ["null count", { requeued: null, limit: 100 }],
  ["missing limit", { requeued: 2 }], ["wrong limit", { requeued: 2, limit: 99 }],
  ["string limit", { requeued: 2, limit: "100" }],
])("does not claim success or clear input for a malformed %s receipt", async (_name, receipt) => {
  writeReply = async () => json(receipt); await mount(); await start();
  const alert = await screen.findByRole("alert"); expect(alert).toHaveTextContent(/could not confirm|cannot confirm/i);
  expect(toast.success).not.toHaveBeenCalled(); expect(input()).toHaveValue(reason); expect(retry()).toBeDisabled();
  expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(1);
});
it("resolves an unknown receipt through an explicit read before allowing another reviewed action", async () => {
  writeReply = async () => json({}); await mount(); await start();
  const alert = await screen.findByRole("alert");
  await userEvent.click(within(alert).getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  expect(input()).toHaveValue(reason); expect(retry()).toBeEnabled(); expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(2);
});
it("keeps a confirmed requeue separate from failed summary refresh and retries only GET", async () => {
  await mount(); readReply = async () => failed(); await start();
  const alert = await screen.findByRole("alert"); expect(alert).toHaveTextContent(/requeued.*summary.*refresh/i);
  expect(toast.success).toHaveBeenCalledWith("Requeued 2 index jobs"); expect(writes()).toHaveLength(1);
  readReply = async () => json(summary);
  await userEvent.click(within(alert).getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  expect(input()).toHaveValue(""); expect(writes()).toHaveLength(1);
});
it("keeps the acknowledged receipt while a second read attempt still fails", async () => {
  await mount(); readReply = async () => failed(); await start();
  const alert = await screen.findByRole("alert");
  await userEvent.click(within(alert).getByRole("button", { name: "Retry loading" }));
  expect(screen.getByRole("alert")).toHaveTextContent(/requeued.*summary.*refresh/i);
  expect(toast.success).toHaveBeenCalledTimes(1); expect(writes()).toHaveLength(1);
});
it.each(["success", "failure"])("does not publish a delayed recovery %s after unmount", async outcome => {
  const gate = defer(outcome === "success" ? json({ requeued: 2, limit: 100 }) : failed()); writeReply = () => gate.promise;
  const view = await mount(); await start(); const before = reads().length; view.unmount(); await act(async () => gate.release());
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); expect(reads()).toHaveLength(before);
});
it("allows new-session input and request while an old session completion remains outstanding", async () => {
  const first = defer(json({ requeued: 2, limit: 100 })), second = defer(json({ requeued: 1, limit: 100 }));
  let requests = 0; writeReply = () => ++requests === 1 ? first.promise : second.promise;
  await mount(); await start(); act(() => installSession("new-index-token", { ...actor, id: "replacement-admin" }));
  await waitFor(() => expect(input()).toBeEnabled());
  fireEvent.change(input(), { target: { value: "New administrator reviewed storage" } }); await userEvent.click(retry());
  await waitFor(() => expect(writes()).toHaveLength(2)); await act(async () => first.release());
  expect(input()).toHaveValue("New administrator reviewed storage"); expect(retry()).toBeDisabled(); expect(toast.success).not.toHaveBeenCalled();
  await act(async () => second.release()); expect(toast.success).toHaveBeenCalledWith("Requeued 1 index jobs");
});
it("retains the current operation through token-only rotation", async () => {
  const gate = defer(json({ requeued: 2, limit: 100 })); writeReply = () => gate.promise;
  await mount(); await start(); act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  await act(async () => gate.release()); expect(toast.success).toHaveBeenCalledWith("Requeued 2 index jobs"); expect(writes()).toHaveLength(1);
});
it("does not send a second POST on same-event double activation", async () => {
  const gate = defer(json({ requeued: 2, limit: 100 })); writeReply = () => gate.promise;
  await mount(); fireEvent.change(input(), { target: { value: reason } }); const button = retry();
  act(() => { button.click(); button.click(); }); await waitFor(() => expect(writes()).toHaveLength(1));
});
it("requires an explicit summary review after a write failure while preserving the reason", async () => {
  writeReply = async () => failed(); await mount(); await start();
  await screen.findByRole("alert"); expect(input()).toHaveValue(reason); expect(retry()).toBeDisabled();
  expect(toast.success).not.toHaveBeenCalled(); expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(1);
});
