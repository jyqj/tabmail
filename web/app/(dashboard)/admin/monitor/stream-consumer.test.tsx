import { Component, type ReactNode } from "react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import AdminMonitorPage from "./page";

// Shipping page + admin facade + streamEvents + parser + SWR. Only transport
// bytes/toast are synthetic; these are not browser or live HTTP/PG receipts.
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
const actor: AuthUser = { id: "operator-one", tenant_id: "tenant-one", role: "super_admin", email: "operator@example.test", display_name: "Operator" };
const prefix = "/api/v1/admin/monitor";
const reply = (data: unknown, total = 1) => new Response(JSON.stringify({ data, meta: { page: 1, per_page: 30, total } }), { headers: { "Content-Type": "application/json" } });
const failure = () => new Response(JSON.stringify({ error: { code: "FORBIDDEN", message: "Controlled monitor failure" } }), { status: 403 });
function deferred() { let resolve!: (value: Response) => void; const promise = new Promise<Response>(done => { resolve = done; }); return { promise, resolve }; }
function event(type = "message", subject = "Live event") { return { type, mailbox: "mailbox@example.test", sender: "sender@example.test", subject, message_id: `id:${subject}`, at: "2026-10-08T00:00:00Z" }; }
type Connection = ReturnType<typeof deferred> & { signal: AbortSignal; authorization: string | null };
let connections: Connection[];
let historyReads: URL[];
let historyReply: () => Promise<Response>;
let sockets: ReturnType<typeof socket>[];
function socket() {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  let ended = false;
  const stream = new ReadableStream<Uint8Array>({ start(value) { controller = value; }, cancel() { ended = true; } });
  return {
    response: new Response(stream, { headers: { "Content-Type": "text/event-stream" } }),
    send(type: string, data: unknown) { if (!ended) controller.enqueue(new TextEncoder().encode(`event: ${type}\ndata: ${JSON.stringify(data)}\n\n`)); },
    close() { if (!ended) { ended = true; controller.close(); } },
    abort() { if (!ended) { ended = true; controller.error(new DOMException("Aborted", "AbortError")); } },
  };
}
class Boundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <p role="alert">Monitor render crashed</p> : this.props.children; }
}
beforeEach(() => {
  connections = []; historyReads = []; sockets = [];
  historyReply = async () => reply([event("message", "Initial history")]);
  installSession("monitor-token", actor);
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  Object.defineProperty(window, "matchMedia", { configurable: true, value: (query: string) => ({ matches: false, media: query, addListener() {}, removeListener() {}, addEventListener() {}, removeEventListener() {}, dispatchEvent() { return false; } }) });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    if (url.pathname === "/api/v1/auth/me/permissions") return reply({});
    if (url.pathname === `${prefix}/history`) { historyReads.push(url); return historyReply(); }
    if (url.pathname === `${prefix}/events`) {
      const connection = { ...deferred(), signal: init!.signal!, authorization: new Headers(init?.headers).get("Authorization") };
      connections.push(connection); return connection.promise;
    }
    throw new Error(`Unexpected request: ${url.pathname}`);
  });
});
afterEach(() => { cleanup(); for (const stream of sockets) stream.abort(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); }); }
async function mount() {
  const page = render(<AuthProvider><SWRConfig value={{ shouldRetryOnError: false, dedupingInterval: 0, revalidateOnFocus: false }}><SidebarProvider><Boundary><AdminMonitorPage /></Boundary></SidebarProvider></SWRConfig></AuthProvider>);
  await screen.findByText("Initial history"); await waitFor(() => expect(connections).toHaveLength(1)); return page;
}
async function connect(index = 0) {
  const stream = socket(); sockets.push(stream);
  connections[index].signal.addEventListener("abort", () => stream.abort(), { once: true });
  await act(async () => connections[index].resolve(stream.response)); await settle(); return stream;
}
function alive() { expect(screen.queryByText("Monitor render crashed")).not.toBeInTheDocument(); expect(screen.getByText("Persisted history")).toBeInTheDocument(); }
async function send(stream: ReturnType<typeof socket>, type: string, data: unknown) { await act(async () => stream.send(type, data)); await settle(); }

it("consumes the real client's initial null resync without inserting a null monitor row", async () => {
  await mount(); await connect(); alive(); expect(screen.getByText("Waiting for monitor events…")).toBeInTheDocument();
});
it("refreshes authoritative history on the initial connection resync", async () => {
  await mount(); historyReply = async () => reply([event("delete", "Fresh history")]); await connect();
  alive(); expect(await screen.findByText("Fresh history")).toBeInTheDocument(); expect(historyReads).toHaveLength(2);
});
it.each([null, { history: true }])("consumes server resync %j as a history hint, not an event", async data => {
  await mount(); const stream = await connect(); alive(); const before = historyReads.length;
  historyReply = async () => reply([event("purge", "Resynced history")]); await send(stream, "resync", data);
  expect(await screen.findByText("Resynced history")).toBeInTheDocument(); expect(historyReads).toHaveLength(before + 1); alive();
});
it.each(["message", "delete", "purge"])("retains a legitimate %s event after control frames", async type => {
  await mount(); const stream = await connect(); alive(); await send(stream, "ready", { ready: true });
  await send(stream, "ping", {}); await send(stream, type, event(type, `${type} live`));
  expect(screen.getByText("Live")).toBeInTheDocument(); expect(screen.getByText(`${type} live`)).toBeInTheDocument();
  expect(screen.queryByText("Invalid Date")).not.toBeInTheDocument();
});
it("keeps ready/ping/unknown controls out of the buffer and ignores unusable event data", async () => {
  await mount(); const stream = await connect(); alive();
  for (const [type, data] of [["ready", { ready: true }], ["ping", null], ["future-control", { cursor: 7 }], ["message", null], ["delete", { history: true }]] as const) await send(stream, type, data);
  alive(); expect(screen.getByText("Waiting for monitor events…")).toBeInTheDocument(); expect(screen.getByText("Live")).toBeInTheDocument();
});
it("uses the current history filters when a later resync arrives", async () => {
  await mount(); const stream = await connect(); alive();
  fireEvent.change(screen.getByPlaceholderText("Filter mailbox"), { target: { value: "current-box" } });
  fireEvent.change(screen.getByPlaceholderText("Filter sender"), { target: { value: "current-sender" } }); await settle();
  const before = historyReads.length; await send(stream, "resync", { history: true });
  expect(historyReads).toHaveLength(before + 1); expect(historyReads.at(-1)!.searchParams.get("mailbox")).toBe("current-box");
  expect(historyReads.at(-1)!.searchParams.get("sender")).toBe("current-sender"); expect(connections).toHaveLength(1);
});
it("recovers history after a failed resync read on the next server hint", async () => {
  await mount(); const stream = await connect(); alive(); historyReply = async () => failure();
  await send(stream, "resync", { history: true }); expect(toast.error).toHaveBeenCalled(); alive();
  historyReply = async () => reply([event("message", "Recovered history")]); await send(stream, "resync", { history: true });
  expect(screen.getByText("Recovered history")).toBeInTheDocument();
});
it("handles a normal EOF reconnect and obtains fresh history again", async () => {
  await mount(); const stream = await connect(); alive(); await send(stream, "ready", {});
  vi.useFakeTimers(); await act(async () => stream.close()); await act(async () => vi.advanceTimersByTimeAsync(1000)); await settle();
  expect(connections).toHaveLength(2); historyReply = async () => reply([event("message", "Reconnect history")]);
  const next = await connect(1); await send(next, "ready", {}); alive(); expect(screen.getByText("Reconnect history")).toBeInTheDocument();
});
it("manual reconnect retires the previous stream and accepts only the new connection", async () => {
  await mount(); const stream = await connect(); alive(); await send(stream, "ready", {});
  fireEvent.click(screen.getByRole("button", { name: "Reconnect" })); await settle(); expect(connections[0].signal.aborted).toBe(true);
  expect(screen.getByText("Disconnected")).toBeInTheDocument(); expect(connections).toHaveLength(2);
  const next = await connect(1); await send(next, "ready", {}); expect(screen.getByText("Live")).toBeInTheDocument(); expect(toast.error).not.toHaveBeenCalled();
});
it("reports final stream permission rejection without claiming a live connection", async () => {
  await mount(); await act(async () => connections[0].resolve(failure())); await settle(); alive();
  expect(screen.getByText("Disconnected")).toBeInTheDocument(); expect(toast.error).toHaveBeenCalledTimes(1);
});
it("unmount aborts an open reader without a disconnect toast or further history reads", async () => {
  const page = await mount(); await connect(); alive(); const before = historyReads.length;
  page.unmount(); await settle(); expect(connections[0].signal.aborted).toBe(true); expect(historyReads).toHaveLength(before); expect(toast.error).not.toHaveBeenCalled();
});
it("late connection completion after unmount cannot resync the retired history", async () => {
  const page = await mount(); const before = historyReads.length; page.unmount(); await connect();
  expect(historyReads).toHaveLength(before); expect(toast.error).not.toHaveBeenCalled();
});
it("replacement sessions start fresh and reject the old pending connection", async () => {
  await mount(); const old = connections[0];
  act(() => installSession("replacement-token", { ...actor, id: "operator-two" })); await settle(); expect(connections).toHaveLength(2);
  expect(old.signal.aborted).toBe(true); await connect(0); await connect(1); alive(); expect(toast.error).not.toHaveBeenCalled();
  expect(connections[1].authorization).toBe("Bearer replacement-token");
});
it("token-only rotation preserves the connection and current buffered events", async () => {
  await mount(); const stream = await connect(); alive(); await send(stream, "message", event()); const before = sessionScope();
  act(() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); }); await settle();
  expect(sessionScope()).toBe(before); expect(connections).toHaveLength(1); expect(connections[0].signal.aborted).toBe(false); expect(screen.getByText("Live event")).toBeInTheDocument();
});
