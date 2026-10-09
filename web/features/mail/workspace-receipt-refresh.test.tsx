import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import useSWR from "swr";
import type { ReactNode } from "react";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { MailWorkspace } from "./workspace";

// Only router location and fetch are substituted. Workspace, receipt consumers,
// SWR, session ownership, parsers and the SSE transport all run unchanged.
const location = vi.hoisted(() => ({ query: "", replace: vi.fn() }));
vi.mock("next/navigation", () => ({
  usePathname: () => "/mail", useSearchParams: () => new URLSearchParams(location.query),
  useRouter: () => ({ replace: location.replace }),
}));
const tenant = "10000000-0000-4000-8000-000000000001";
const otherTenant = "10000000-0000-4000-8000-000000000002";
const job = "20000000-0000-4000-8000-000000000001";
const addedJob = "20000000-0000-4000-8000-000000000002";
const mailbox = "mailbox-one";
const base = `/api/v1/company/mailboxes/${mailbox}`;
type Entry = "company" | "compatibility";
type Trigger = "manual refresh" | "SSE invalidation" | "EOF reconnect";
type Call = { path: string; method: string; headers: Headers; signal?: AbortSignal | null };
type Stream = { controller: ReadableStreamDefaultController<Uint8Array>; signal?: AbortSignal | null; closed: boolean };
const listPath = (entry: Entry) => entry === "company" ? "/api/v1/company/submissions" : "/api/v1/outbound";
const detailPath = (entry: Entry) => `${listPath(entry)}/${job}`;
const json = (data: unknown, list = false) => new Response(JSON.stringify({ data, ...(list ? { meta: { total: (data as unknown[]).length, page: 1, per_page: 30 } } : {}) }), { headers: { "Content-Type": "application/json" } });
const denied = () => new Response(JSON.stringify({ error: { code: "FORBIDDEN", message: "Receipt unavailable" } }), { status: 403, headers: { "Content-Type": "application/json" } });
let calls: Call[], streams: Stream[], version: number, canView: boolean, readable: boolean;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const receipt = (selectedTenant = tenant, id = job) => ({
  id, tenant_id: selectedTenant, state: "sent", status: version === 1 ? "partially_accepted" : "accepted",
  progress: { completeness: "known", counts: { total: 2, accepted: version, pending: 0, temporary: 0, permanent: 2 - version, uncertain: 0 } },
  delivery_uncertain: false, capabilities: { view_content: canView, retry: false, retry_block_reason: "state_not_retryable" },
});
function identity(name = "reader", selectedTenant = tenant) {
  installSession(`${name}-token`, { id: name, tenant_id: selectedTenant, email: `${name}@fixture.test`, display_name: name, role: "user" });
}
beforeEach(() => {
  calls = []; streams = []; version = 1; canView = true; readable = true; intercept = undefined;
  identity(); location.query = `mailbox=${mailbox}&folder=receipts&message=${job}`;
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const call: Call = { path, method: init?.method ?? "GET", headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (path === `${base}/events`) {
      const stream: Stream = { controller: undefined as unknown as Stream["controller"], signal: init?.signal, closed: false };
      const body = new ReadableStream<Uint8Array>({ start(controller) { stream.controller = controller; }, cancel() { stream.closed = true; } });
      init?.signal?.addEventListener("abort", () => { if (!stream.closed) { stream.closed = true; stream.controller.error(new DOMException("Aborted", "AbortError")); } }, { once: true });
      streams.push(stream);
      return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (path === "/api/v1/company/mailboxes") return json([{ mailbox: { id: mailbox, tenant_id: call.headers.get("X-Tenant-ID"), kind: "personal", full_address: "reader@fixture.test" }, can_read: readable, can_send: false, can_organize: false, template_only: false, revision: 1 }]);
    const selectedTenant = call.headers.get("X-Tenant-ID") ?? tenant;
    if (path === listPath("company") || path === listPath("compatibility")) return json([receipt(selectedTenant), ...(version === 2 ? [receipt(selectedTenant, addedJob)] : [])], true);
    if (path === detailPath("company") || path === detailPath("compatibility")) return json(receipt(selectedTenant));
    throw new Error(`Unexpected receipt workspace request: ${path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
async function mount(entry: Entry, probes?: ReactNode, failed = false) {
  render(<><MailWorkspace />{probes}</>);
  if (entry === "compatibility") {
    fireEvent.click(await screen.findByRole("button", { name: /^Compatibility receipts$/ }));
    fireEvent.click(await screen.findByRole("button", { name: "View compatibility receipt" }));
  }
  if (failed) await screen.findByText("Receipt unavailable");
  else expect(await screen.findByTestId("receipt-count-accepted")).toHaveTextContent("1");
  await waitFor(() => expect(streams).toHaveLength(1));
  await settle();
}
async function trigger(kind: Trigger) {
  if (kind === "manual refresh") { fireEvent.click(screen.getByRole("button", { name: /^Refresh$/ })); return; }
  if (kind === "SSE invalidation") {
    await act(async () => streams[0].controller.enqueue(new TextEncoder().encode('event: submission.updated\ndata: {}\n\n')));
    return;
  }
  vi.useFakeTimers();
  try {
    await act(async () => {
      streams[0].closed = true; streams[0].controller.close();
      for (let i = 0; i < 30; i++) await Promise.resolve();
      await vi.advanceTimersByTimeAsync(1000);
      for (let i = 0; i < 30; i++) await Promise.resolve();
    });
  } finally { vi.useRealTimers(); }
  expect(streams).toHaveLength(2);
}
function expectMetadataOnly(reads: Call[], expectedToken = "reader-token", expectedTenant = tenant) {
  expect(reads.every(call => call.method === "GET")).toBe(true);
  expect(reads.some(call => /\/(content|attachments|retry)(\/|$)/.test(call.path))).toBe(false);
  expect(reads.every(call => call.headers.get("Authorization") === `Bearer ${expectedToken}` && call.headers.get("X-Tenant-ID") === expectedTenant)).toBe(true);
}

describe.each<Entry>(["company", "compatibility"])("%s receipt workspace refresh", entry => {
  it.each<Trigger>(["manual refresh", "SSE invalidation", "EOF reconnect"])("%s updates the visible aggregate and list without opening content", async kind => {
    await mount(entry); const before = calls.length; version = 2;
    await trigger(kind);
    await waitFor(() => expect(calls.slice(before).some(call => call.path === "/api/v1/company/mailboxes")).toBe(true));
    await settle();
    expect.soft(calls.slice(before).some(call => call.path === detailPath(entry)), "selected aggregate must be re-read").toBe(true);
    expect.soft(calls.slice(before).some(call => call.path === listPath(entry)), "visible list must be re-read").toBe(true);
    expect.soft(screen.getByTestId("receipt-count-accepted")).toHaveTextContent("2");
    expect.soft(screen.queryByText(`Task: ${addedJob}`)).toBeInTheDocument();
    expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false");
    expectMetadataOnly(calls.slice(before));
  });

  it("manual refresh retries an errored selected aggregate without waiting for polling", async () => {
    intercept = call => call.path === detailPath(entry) ? Promise.resolve(denied()) : undefined;
    await mount(entry, undefined, true); const before = calls.length;
    intercept = undefined; version = 2;
    await trigger("manual refresh"); await settle();
    expect(calls.slice(before).some(call => call.path === detailPath(entry))).toBe(true);
    expect(await screen.findByTestId("receipt-count-accepted")).toHaveTextContent("2");
    expect(screen.queryByText("Receipt unavailable")).not.toBeInTheDocument();
    expectMetadataOnly(calls.slice(before));
  });

  it("refresh applies capability revocation and re-grant does not open content", async () => {
    await mount(entry); const before = calls.length; canView = false;
    await trigger("manual refresh"); await settle();
    expect(screen.queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument();
    canView = true;
    await trigger("manual refresh");
    expect(await screen.findByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false");
    expectMetadataOnly(calls.slice(before));
  });

  it("a denied aggregate refresh removes stale metadata and disclosure", async () => {
    await mount(entry); const before = calls.length;
    intercept = call => call.path === detailPath(entry) ? Promise.resolve(denied()) : undefined;
    await trigger("SSE invalidation"); await settle();
    expect(screen.queryByTestId("ordinary-receipt-aggregate")).not.toBeInTheDocument();
    expect(screen.queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument();
    expect(screen.getByText("Receipt unavailable")).toBeInTheDocument();
    expectMetadataOnly(calls.slice(before));
  });

  it.each(["account", "tenant"])("an old in-flight aggregate cannot overwrite the new %s", async kind => {
    await mount(entry); let release!: (response: Response) => void; let oldRead: Call | undefined;
    const delayed = new Promise<Response>(resolve => { release = resolve; });
    const oldReceipt = receipt();
    intercept = call => {
      if (call.path === detailPath(entry) && call.headers.get("Authorization") === "Bearer reader-token" && call.headers.get("X-Tenant-ID") === tenant) { oldRead = call; return delayed; }
    };
    await trigger("manual refresh");
    await waitFor(() => expect(oldRead).toBeDefined());
    version = 2;
    const beforeSwitch = calls.length;
    await act(async () => identity(kind === "account" ? "next-reader" : "reader", kind === "tenant" ? otherTenant : tenant));
    await waitFor(() => expect(screen.getByTestId("receipt-count-accepted")).toHaveTextContent("2"));
    expect(oldRead?.signal?.aborted).toBe(true);
    expect(streams[0].signal?.aborted).toBe(true);
    await act(async () => release(json(oldReceipt))); await settle();
    expect(screen.getByTestId("receipt-count-accepted")).toHaveTextContent("2");
    expectMetadataOnly(calls.slice(beforeSwitch), kind === "account" ? "next-reader-token" : "reader-token", kind === "tenant" ? otherTenant : tenant);
  });

  it("token-only rotation keeps scope and refresh uses the replacement credential", async () => {
    await mount(entry); const scope = sessionScope();
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(scope); const before = calls.length; version = 2;
    await trigger("manual refresh"); await settle();
    expect(calls.slice(before).some(call => call.path === detailPath(entry))).toBe(true);
    expect(screen.getByTestId("receipt-count-accepted")).toHaveTextContent("2");
    expect(streams).toHaveLength(1);
    expectMetadataOnly(calls.slice(before), "rotated-token");
  });
});

it("refresh preserves session and key-family boundaries, and ping never invalidates", async () => {
  const probes = Array.from({ length: 7 }, () => vi.fn(async () => "unchanged"));
  function Probes() {
    useSWR(["session", "other-scope", ["submission", job]], probes[0]);
    useSWR(["session", "other-scope", ["legacy-outbound-receipt", job]], probes[1]);
    useSWR(["session", "other-scope", "legacy-outbound-receipts"], probes[2]);
    useSWR(["session", sessionScope(), ["unrelated-resource", job]], probes[3]);
    useSWR(["other-namespace", sessionScope(), ["submission", job]], probes[4]);
    useSWR(["submission", job], probes[5]);
    useSWR("legacy-outbound-receipts", probes[6]);
    return null;
  }
  await mount("company", <Probes />); const before = calls.length;
  await act(async () => streams[0].controller.enqueue(new TextEncoder().encode('event: ping\ndata: {}\n\n'))); await settle();
  expect(calls).toHaveLength(before);
  await trigger("manual refresh");
  await waitFor(() => expect(calls.length).toBeGreaterThan(before)); await settle();
  for (const probe of probes) expect(probe).toHaveBeenCalledTimes(1);
});

it("mailbox read revocation still closes the existing event stream during receipt refresh", async () => {
  await mount("company"); const before = calls.length; readable = false;
  await trigger("manual refresh");
  await waitFor(() => expect(streams[0].signal?.aborted).toBe(true));
  expect(streams).toHaveLength(1);
  expectMetadataOnly(calls.slice(before));
});
