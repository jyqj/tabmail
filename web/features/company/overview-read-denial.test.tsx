import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import { Overview } from "./overview";

// Keep the real component, scoped SWR cache and request stack. In particular,
// an ordinary company 403 does not install a different local session identity.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const summary = { mailboxes: 3, active_employees: 3, pending_invitations: 0, queued: 1, uncertain: 0, index_failed: 3 };
const refreshed = { ...summary, mailboxes: 17, index_failed: 1 };
const reason = "Storage access restored and verified";
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const denied = () => new Response(JSON.stringify({ error: { code: "FORBIDDEN", message: "Company overview access denied" } }), { status: 403, headers: { "Content-Type": "application/json" } });
const unavailable = () => new Response(JSON.stringify({ error: { code: "INTERNAL", message: "Overview temporarily unavailable" } }), { status: 503, headers: { "Content-Type": "application/json" } });
let cache = new Map();
let calls: { path: string; method: string }[];
let readReply: () => Promise<Response>;
let writeReply: () => Promise<Response>;
let pending: (() => void)[];
beforeEach(() => {
  cache = new Map(); calls = []; pending = [];
  readReply = async () => json(summary); writeReply = async () => json({ requeued: 2, limit: 100 });
  installSession("overview-denial-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@example.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET" };
    calls.push(call);
    if (call.path === "/api/v1/company/overview" && call.method === "GET") return readReply();
    if (call.path === "/api/v1/company/index/retry" && call.method === "POST") return writeReply();
    throw new Error(`Unexpected overview request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => pending.forEach(release => release())); vi.unstubAllGlobals(); });
function mount() {
  return render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0, revalidateOnFocus: false, shouldRetryOnError: false }}><Overview /></SWRConfig>);
}
const writes = () => calls.filter(call => call.method === "POST");
const reads = () => calls.filter(call => call.method === "GET");
function expectHiddenSummary() {
  for (const label of ["Mailboxes", "Active employees", "Pending invitations", "Queued/in-flight deliveries", "Uncertain results", "Failed body indexes"]) {
    expect(screen.queryByText(label, { exact: true })).not.toBeInTheDocument();
  }
  expect(screen.queryByRole("button", { name: "Retry failed indexes" })).not.toBeInTheDocument();
}
async function start() {
  const input = await screen.findByLabelText("Recovery reason");
  fireEvent.change(input, { target: { value: reason } });
  await userEvent.click(screen.getByRole("button", { name: "Retry failed indexes" }));
  await waitFor(() => expect(writes()).toHaveLength(1));
}
async function retryRead() {
  await userEvent.click(within(screen.getByRole("alert")).getByRole("button", { name: "Retry loading" }));
}

it.each(["confirmed", "unknown"])("hides revoked summary after a %s recovery receipt and preserves GET-only review", async receipt => {
  if (receipt === "unknown") writeReply = async () => json({});
  mount(); await screen.findByText("Mailboxes", { exact: true });
  readReply = async () => denied(); await start();
  if (receipt === "unknown") {
    await screen.findByRole("alert");
    expect(reads()).toHaveLength(1);
    await retryRead();
  }
  const alert = await screen.findByRole("alert");
  expect(alert).toHaveTextContent(/summary.*refresh/i);
  expectHiddenSummary(); expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(2);
  if (receipt === "confirmed") {
    expect(alert).toHaveTextContent(/2.*requeued/i);
    expect(toast.success).toHaveBeenCalledExactlyOnceWith("Requeued 2 index jobs");
  } else expect(toast.success).not.toHaveBeenCalled();

  // A later transient read failure must not restore the previously denied cache.
  readReply = async () => unavailable(); await retryRead();
  expectHiddenSummary(); expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(3);
  if (receipt === "confirmed") expect(screen.getByRole("alert")).toHaveTextContent(/2.*requeued/i);

  readReply = async () => json(refreshed); await retryRead();
  await waitFor(() => expect(screen.queryByRole("alert")).not.toBeInTheDocument());
  expect(screen.getByText("Mailboxes", { exact: true }).parentElement).toHaveTextContent("17");
  expect(screen.getByLabelText("Recovery reason")).toHaveValue(receipt === "unknown" ? reason : "");
  const retry = screen.getByRole("button", { name: "Retry failed indexes" });
  if (receipt === "unknown") expect(retry).toBeEnabled();
  else expect(retry).toBeDisabled();
  expect(writes()).toHaveLength(1); expect(reads()).toHaveLength(4);
});

it("does not redisplay the denied cached overview when the same session remounts before a fresh GET finishes", async () => {
  const view = mount(); await screen.findByText("Mailboxes", { exact: true });
  readReply = async () => denied(); await start(); await screen.findByRole("alert");
  expectHiddenSummary();
  let release!: () => void;
  const nextRead = new Promise<Response>(resolve => { release = () => resolve(json(refreshed)); });
  pending.push(release); readReply = () => nextRead;
  view.unmount(); mount();
  await waitFor(() => expect(reads()).toHaveLength(3));
  expectHiddenSummary(); expect(writes()).toHaveLength(1);
  await act(async () => release());
  expect(screen.getByText("Mailboxes", { exact: true }).parentElement).toHaveTextContent("17");
  expect(writes()).toHaveLength(1);
});
