import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { MailWorkspace } from "./workspace";
import useSWR from "swr";
import type { ReactNode } from "react";

// Only router location and fetch are substituted. The mounted workspace,
// message/attachment/conversation readers, SWR, session owner and SSE parser run.
const location = vi.hoisted(() => ({ query: "mailbox=mailbox-one&message=message-one", replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/mail", useSearchParams: () => new URLSearchParams(location.query),
  useRouter: () => ({ replace: location.replace }),
}));
const tenant = "10000000-0000-4000-8000-000000000001";
const mailbox = "mailbox-one", message = "message-one";
const base = `/api/v1/company/mailboxes/${mailbox}`;
const detailPath = `${base}/messages/${message}`;
const observedPaths = [detailPath, `${detailPath}/attachments`, `${detailPath}/conversation`];
type Call = { path: string; method: string; headers: Headers; signal?: AbortSignal | null };
type Stream = { controller: ReadableStreamDefaultController<Uint8Array>; signal?: AbortSignal | null; closed: boolean };
let calls: Call[], streams: Stream[], version: number;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const json = (data: unknown, list = false) => new Response(JSON.stringify({ data, ...(list ? { meta: { total: 1, page: 1, per_page: 30 } } : {}) }), { headers: { "Content-Type": "application/json" } });
const row = () => ({ id: message, mailbox_id: mailbox, sender: "sender@fixture.test", recipients: ["reader@fixture.test"], subject: `Message ${version}`, received_at: "2026-10-04T00:00:00Z", seen: version > 1, starred: version > 1 });
function identity(name = "reader", selectedTenant = tenant) {
  installSession(`${name}-token`, { id: name, tenant_id: selectedTenant, email: `${name}@fixture.test`, display_name: name, role: "user" });
}
beforeEach(() => {
  version = 1; calls = []; streams = []; intercept = undefined; identity();
  location.query = `mailbox=${mailbox}&message=${message}`;
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const call = { path, method: init?.method ?? "GET", headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (path === `${base}/events`) {
      const stream: Stream = { controller: undefined as unknown as Stream["controller"], signal: init?.signal, closed: false };
      const body = new ReadableStream<Uint8Array>({ start(controller) { stream.controller = controller; }, cancel() { stream.closed = true; } });
      init?.signal?.addEventListener("abort", () => { if (!stream.closed) { stream.closed = true; stream.controller.error(new DOMException("Aborted", "AbortError")); } }, { once: true });
      streams.push(stream);
      return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (path === "/api/v1/company/mailboxes") return json([{ mailbox: { id: mailbox, tenant_id: tenant, kind: "personal", full_address: "reader@fixture.test" }, can_read: true, can_send: false, can_organize: false, template_only: false, revision: 1 }]);
    if (path === `${base}/messages`) return json([row()], true);
    if (path === detailPath) return json({ ...row(), text_body: `Body ${version}` });
    if (path === `${detailPath}/attachments`) return json([{ id: "attachment-one", filename: `file-${version}.txt`, size: 42 }]);
    if (path === `${detailPath}/conversation`) return json([{ ...row(), id: "conversation-one", subject: `Conversation ${version}` }], true);
    if (path === `${base}/index-status`) return json({ total: 1, indexed: 1, failed: 0 });
    throw new Error(`Unexpected workspace request: ${path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function mount(probes?: ReactNode) {
  render(<><MailWorkspace />{probes}</>);
  await screen.findByText("Body 1");
  fireEvent.click(screen.getByRole("button", { name: "Conversation in this mailbox" }));
  await screen.findByRole("link", { name: /Conversation 1/ });
  await screen.findByRole("button", { name: /file-1.txt/ });
  await waitFor(() => expect(streams).toHaveLength(1));
  await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); });
}
async function trigger(kind: string) {
  if (kind === "manual refresh") { fireEvent.click(screen.getByRole("button", { name: "Refresh" })); return; }
  if (kind === "SSE invalidation") {
    await act(async () => streams[0].controller.enqueue(new TextEncoder().encode('event: message.updated\ndata: {"id":"message-one"}\n\n')));
    return;
  }
  vi.useFakeTimers();
  try {
    await act(async () => {
      streams[0].closed = true; streams[0].controller.close();
      for (let i = 0; i < 20; i++) await Promise.resolve();
      await vi.advanceTimersByTimeAsync(1000);
      for (let i = 0; i < 20; i++) await Promise.resolve();
    });
  } finally { vi.useRealTimers(); }
  expect(streams).toHaveLength(2);
}
describe("mail workspace visible-read invalidation", () => {
  it.each(["manual refresh", "SSE invalidation", "EOF reconnect"])("%s revalidates the open message, attachments and conversation", async kind => {
    await mount();
    const before = calls.length;
    version = 2;
    await trigger(kind);
    await waitFor(() => expect(calls.slice(before).some(call => call.path === `${base}/messages`)).toBe(true));
    await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); });
    const after = calls.slice(before);
    for (const path of observedPaths) expect.soft(after.some(call => call.path === path), `must re-read ${path}`).toBe(true);
    expect.soft(screen.queryByText("Body 2")).toBeInTheDocument();
    expect.soft(screen.queryByRole("button", { name: /file-2.txt/ })).toBeInTheDocument();
    expect.soft(screen.queryByRole("link", { name: /Conversation 2/ })).toBeInTheDocument();
    expect(after.every(call => call.method === "GET")).toBe(true);
    expect(after.every(call => call.headers.get("Authorization") === "Bearer reader-token" && call.headers.get("X-Tenant-ID") === tenant)).toBe(true);
  });

  it("invalidates only current-session owned reads and ignores ping frames", async () => {
    const otherScope = vi.fn(async () => "other identity"), unrelated = vi.fn(async () => "unrelated");
    function Probes() {
      useSWR(["session", "other-session", ["work-message", mailbox, message]], otherScope);
      useSWR(["session", sessionScope(), ["unrelated-resource", mailbox]], unrelated);
      return null;
    }
    await mount(<Probes />);
    expect(otherScope).toHaveBeenCalledTimes(1); expect(unrelated).toHaveBeenCalledTimes(1);
    const beforePing = calls.length;
    await act(async () => streams[0].controller.enqueue(new TextEncoder().encode('event: ping\ndata: {}\n\n')));
    expect(calls).toHaveLength(beforePing);
    const before = calls.length;
    await trigger("SSE invalidation");
    await waitFor(() => expect(calls.slice(before).some(call => call.path === detailPath)).toBe(true));
    expect(otherScope).toHaveBeenCalledTimes(1); expect(unrelated).toHaveBeenCalledTimes(1);
  });
  it.each(["account", "tenant"])("a late detail read from the old %s cannot replace the current message", async kind => {
    await mount();
    let release!: (response: Response) => void;
    const delayed = new Promise<Response>(resolve => { release = resolve; });
    const oldScope = sessionScope(), before = calls.length;
    let oldRead: Call | undefined;
    intercept = call => {
      if (call.path === detailPath && call.headers.get("Authorization") === "Bearer reader-token" && call.headers.get("X-Tenant-ID") === tenant) {
        oldRead = call;
        return delayed;
      }
    };
    await trigger("SSE invalidation");
    await waitFor(() => expect(oldRead).toBeDefined());
    const oldData = { ...row(), text_body: "Old identity late body" };
    version = 3;
    await act(async () => identity(kind === "tenant" ? "reader" : "next-reader", kind === "tenant" ? "10000000-0000-4000-8000-000000000002" : tenant));
    await screen.findByText("Body 3");
    expect(sessionScope()).not.toBe(oldScope); expect(oldRead?.signal?.aborted).toBe(true);
    expect(streams[0].signal?.aborted).toBe(true);
    await act(async () => release(json(oldData)));
    expect(screen.queryByText("Old identity late body")).not.toBeInTheDocument();
    expect(screen.getByText("Body 3")).toBeInTheDocument();
    expect(calls.slice(before).every(call => call.method === "GET")).toBe(true);
  });
  it("token-only rotation preserves the open conversation and uses the replacement credential", async () => {
    await mount();
    const scope = sessionScope();
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope);
    const before = calls.length;
    await trigger("manual refresh");
    await waitFor(() => expect(calls.length).toBeGreaterThan(before));
    expect(screen.getByRole("link", { name: /Conversation 1/ })).toBeInTheDocument();
    expect(streams).toHaveLength(1);
    expect(calls.slice(before).every(call => call.headers.get("Authorization") === "Bearer rotated-token")).toBe(true);
  });
});
