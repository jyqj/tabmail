import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import type { WorkMailbox } from "@/lib/company";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { MessagePane } from "./message-pane";

// Keep MessagePane, SWR, the action guard and authenticated request layer real.
// There is no SSE connection or timer advancement to conceal a missing read.
const tenant = "10000000-0000-4000-8000-000000000001";
const mailbox: WorkMailbox = {
  mailbox: { id: "mailbox-one", tenant_id: tenant, zone_id: "zone-one", kind: "personal", local_part: "reader", resolved_domain: "fixture.test", full_address: "reader@fixture.test", access_mode: "token", created_at: "2026-10-05T00:00:00Z" },
  can_read: true, can_send: false, can_organize: true, template_only: false, revision: 1,
};
const base = "/api/v1/company/mailboxes/mailbox-one/messages/message-one";
type State = { seen: boolean; starred: boolean; archived_at?: string; deleted_at?: string };
type Call = { path: string; method: string; body: string | undefined; headers: Headers; signal?: AbortSignal | null };
let state: State, calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const onMutation = vi.fn();
const json = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), { status, headers: { "Content-Type": "application/json" } });
const detail = (body = "Message body") => ({ id: "message-one", mailbox_id: "mailbox-one", sender: "sender@fixture.test", recipients: ["reader@fixture.test"], subject: "Message subject", received_at: "2026-10-05T00:00:00Z", text_body: body, ...state });
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
function identity(user = "reader", selectedTenant = tenant) {
  installSession(`${user}-token`, { id: user, tenant_id: selectedTenant, email: `${user}@fixture.test`, display_name: user, role: "user" });
}
const posts = () => calls.filter(call => call.method === "POST");
const reads = () => calls.filter(call => call.method === "GET" && call.path === base);
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
async function mount() {
  render(<MessagePane mailbox={mailbox} id="message-one" onMutation={onMutation} onCompose={vi.fn()}/>);
  await screen.findByText("Message body");
}

beforeEach(() => {
  state = { seen: false, starred: false }; calls = []; intercept = undefined;
  identity();
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: typeof init?.body === "string" ? init.body : undefined, headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.method === "POST" && call.path === `${base}/actions`) {
      const action = JSON.parse(call.body ?? "{}").action;
      if (action === "seen" || action === "unseen") state.seen = action === "seen";
      else if (action === "starred" || action === "unstarred") state.starred = action === "starred";
      else if (action === "archive" || action === "unarchive") state.archived_at = action === "archive" ? "2026-10-05T00:00:00Z" : undefined;
      else if (action === "trash" || action === "restore") state.deleted_at = action === "trash" ? "2026-10-05T00:00:00Z" : undefined;
      else throw new Error(`Unexpected action: ${action}`);
      return json({ updated: true });
    }
    if (call.method === "GET" && call.path === base) return json(detail());
    if (call.method === "GET" && call.path === `${base}/attachments`) return json([]);
    throw new Error(`Unexpected request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("received message action reconciliation", () => {
  it.each([
    { initial: {}, action: "seen", before: "Mark read", after: "Mark unread" },
    { initial: { seen: true }, action: "unseen", before: "Mark unread", after: "Mark read" },
    { initial: {}, action: "starred", before: "Star", after: "Unstar" },
    { initial: { starred: true }, action: "unstarred", before: "Unstar", after: "Star" },
    { initial: {}, action: "archive", before: "Archive", after: "Move to inbox" },
    { initial: { archived_at: "2026-10-04T00:00:00Z" }, action: "unarchive", before: "Move to inbox", after: "Archive" },
    { initial: {}, action: "trash", before: "Move to trash", after: "Restore" },
    { initial: { deleted_at: "2026-10-04T00:00:00Z" }, action: "restore", before: "Restore", after: "Move to trash" },
  ])("$action re-reads authoritative detail before enabling the next action", async ({ initial, action, before, after }) => {
    Object.assign(state, initial);
    await mount();
    fireEvent.click(screen.getByRole("button", { name: before }));
    await settle();
    expect(posts()).toHaveLength(1);
    expect(posts()[0].body).toBe(JSON.stringify({ action }));
    expect(onMutation).toHaveBeenCalledTimes(1);
    expect(reads()).toHaveLength(2);
    expect(calls.indexOf(reads()[1])).toBeGreaterThan(calls.indexOf(posts()[0]));
    expect(screen.getByRole("button", { name: after })).toBeEnabled();
    expect(screen.queryByRole("button", { name: before })).not.toBeInTheDocument();
    expect(calls.every(call => call.headers.get("Authorization") === "Bearer reader-token" && call.headers.get("X-Tenant-ID") === tenant)).toBe(true);
  });

  it("keeps actions disabled until the authoritative read settles and then sends the opposite action", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.path === base ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    await settle();
    expect(reads()).toHaveLength(2);
    const button = screen.getByRole("button", { name: "Mark read" });
    expect(button).toBeDisabled();
    fireEvent.click(button);
    expect(posts()).toHaveLength(1);
    await act(async () => pending.resolve(json(detail())));
    await waitFor(() => expect(screen.getByRole("button", { name: "Mark unread" })).toBeEnabled());
    intercept = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Mark unread" }));
    await settle();
    expect(posts().map(call => JSON.parse(call.body!).action)).toEqual(["seen", "unseen"]);
    expect(screen.getByRole("button", { name: "Mark read" })).toBeEnabled();
  });

  it("does not update or re-read before a pending or rejected POST is confirmed", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.method === "POST" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Star" }));
    fireEvent.click(screen.getByRole("button", { name: "Star" }));
    await settle();
    expect(posts()).toHaveLength(1); expect(reads()).toHaveLength(1);
    expect(screen.queryByRole("button", { name: "Unstar" })).not.toBeInTheDocument();
    expect(onMutation).not.toHaveBeenCalled();
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Action denied" } }, 403)));
    await waitFor(() => expect(screen.getByRole("button", { name: "Star" })).toBeEnabled());
    expect(onMutation).not.toHaveBeenCalled(); expect(reads()).toHaveLength(1);
    expect(toast.error).toHaveBeenCalledWith("Action denied");
  });

  it("a failed reconciliation exposes a read retry without replaying the successful action", async () => {
    await mount();
    intercept = call => call.path === base ? Promise.resolve(json({ error: { code: "UNAVAILABLE", message: "Read unavailable" } }, 503)) : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Star" }));
    await settle();
    expect(reads()).toHaveLength(2); expect(posts()).toHaveLength(1);
    expect(screen.getByRole("alert")).toHaveTextContent("Read unavailable");
    expect(screen.queryByRole("button", { name: "Star" })).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Unstar" })).not.toBeInTheDocument();
    intercept = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByRole("button", { name: "Unstar" });
    expect(posts()).toHaveLength(1); expect(onMutation).toHaveBeenCalledTimes(1);
  });

  it.each(["account", "tenant"])("a late action response cannot revalidate the replacement %s", async kind => {
    await mount();
    const pending = deferred();
    intercept = call => call.method === "POST" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    await settle();
    const oldScope = sessionScope();
    await act(async () => identity(kind === "account" ? "next-reader" : "reader", kind === "tenant" ? "10000000-0000-4000-8000-000000000002" : tenant));
    await settle();
    expect(sessionScope()).not.toBe(oldScope); expect(posts()[0].signal?.aborted).toBe(true);
    const before = calls.length;
    await act(async () => pending.resolve(json({ updated: true })));
    expect(calls).toHaveLength(before); expect(onMutation).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Mark read" })).toBeEnabled();
  });

  it("uses a rotated token for the read after a successful same-identity action", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.method === "POST" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Mark read" }));
    const scope = sessionScope();
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope);
    state.seen = true;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(reads()).toHaveLength(2);
    expect(reads()[1].headers.get("Authorization")).toBe("Bearer rotated-token");
    expect(screen.getByRole("button", { name: "Mark unread" })).toBeEnabled();
    expect(posts()).toHaveLength(1);
  });
});
