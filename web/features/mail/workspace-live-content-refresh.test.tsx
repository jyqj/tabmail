import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { SWRConfig } from "swr";
import { AUTH_EVENT, clearSessionCredentials, installSession, sessionScope } from "@/lib/session";
import { MailWorkspace } from "./workspace";
import { SubmissionContentView } from "./components/submission-content";

// Only fetch and Next's URL store are substituted. These are mounted shipping
// consumers with the real session, SWR, content parsers and SSE transport.
const location = vi.hoisted(() => ({ query: "", listeners: new Set<() => void>(), replace: vi.fn() }));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    usePathname: () => "/mail",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(
      callback => { location.listeners.add(callback); return () => location.listeners.delete(callback); },
      () => location.query,
    )),
    useRouter: () => ({ replace: location.replace }),
  };
});
const tenant = "10000000-0000-4000-8000-000000000001";
const otherTenant = "10000000-0000-4000-8000-000000000002";
const job = "20000000-0000-4000-8000-000000000001";
const otherJob = "20000000-0000-4000-8000-000000000002";
const outsideJob = "20000000-0000-4000-8000-000000000003";
const mailbox = "mailbox-one";
const base = `/api/v1/company/mailboxes/${mailbox}`;
const contentPath = (id = job) => `/api/v1/company/submissions/${id}/content`;
const attachmentPath = (id = job) => `/api/v1/company/submissions/${id}/attachments`;
const date = "2026-10-05T01:02:03Z";
type Entry = "company" | "compatibility" | "sent";
type Trigger = "manual refresh" | "SSE invalidation" | "EOF reconnect";
type Call = { path: string; method: string; headers: Headers; signal?: AbortSignal | null };
type Stream = { controller: ReadableStreamDefaultController<Uint8Array>; signal?: AbortSignal | null; closed: boolean };
const json = (data: unknown, list = false) => new Response(JSON.stringify({ data, ...(list ? { meta: { total: (data as unknown[]).length, page: 1, per_page: 30 } } : {}) }), { headers: { "Content-Type": "application/json" } });
const denied = (status = 403) => new Response(JSON.stringify({ error: { code: status === 404 ? "NOT_FOUND" : "FORBIDDEN", message: "Fixture unavailable" } }), { status, headers: { "Content-Type": "application/json" } });
let calls: Call[], streams: Stream[], version: number, canView: boolean, readable: boolean;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const receipt = (selectedTenant = tenant, id = job) => ({
  id, tenant_id: selectedTenant, state: "sent", status: "accepted",
  progress: { completeness: "known", counts: { total: 1, accepted: 1, pending: 0, temporary: 0, permanent: 0, uncertain: 0 } },
  delivery_uncertain: false, capabilities: { view_content: canView, retry: true, retry_block_reason: "" },
});
const live = (id = job, revision = version) => ({ id, subject: `private subject ${revision}`, from: "sender@fixture.test", to: ["to@fixture.test"],
  cc: [], bcc: [`private-bcc-${revision}@fixture.test`], recipient_completeness: "complete", created_at: date,
  text_body: `PRIVATE-LIVE-BODY-${id}-${revision}`, content_redacted: false });
const files = (revision = version) => [{ id: "attachment-one", filename: `private-attachment-${revision}.txt`, content_type: "text/plain", size: 42, state: "ready" }];
function identity(name = "reader", selectedTenant = tenant) {
  installSession(`${name}-token`, { id: name, tenant_id: selectedTenant, email: `${name}@fixture.test`, display_name: name, role: "user" });
}
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
}
function partialJSON(data: unknown) {
  const text = JSON.stringify({ data }), middle = Math.floor(text.length / 2);
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({ start(stream) { controller = stream; } });
  return { response: new Response(body, { headers: { "Content-Type": "application/json" } }),
    first: () => controller.enqueue(new TextEncoder().encode(text.slice(0, middle))),
    finish: () => { controller.enqueue(new TextEncoder().encode(text.slice(middle))); controller.close(); } };
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
    if (path.endsWith("/content")) return json(live(path.split("/").at(-2)));
    if (path.endsWith("/attachments")) return json(files());
    if (path === `${base}/sent`) return json([job, otherJob].map(id => ({ id, mailbox_id: mailbox, subject: `Asset ${id}`, from: "sender@fixture.test", to: [], created_at: date, revision: 1, attachment_count: 1, delivery_available: true })), true);
    const selectedTenant = call.headers.get("X-Tenant-ID") ?? tenant;
    if (path === "/api/v1/company/submissions" || path === "/api/v1/outbound") return json([receipt(selectedTenant), receipt(selectedTenant, otherJob)], true);
    if (/^\/api\/v1\/(company\/submissions|outbound)\/[^/]+$/.test(path)) return json(receipt(selectedTenant, path.split("/").at(-1)));
    throw new Error(`Unexpected live content request: ${call.method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function settle() { await act(async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); }); }
async function navigate(query: string) {
  await act(async () => { location.query = query; for (const notify of location.listeners) notify(); });
  await settle();
}
async function mount(entry: Entry, open = true, outside = false) {
  if (entry === "sent") location.query = `mailbox=${mailbox}&folder=sent&message=${job}`;
  const cache = new Map();
  const mounted = render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false }}>
    <MailWorkspace />{outside && <aside data-testid="outside-view"><SubmissionContentView id={outsideJob} /></aside>}
  </SWRConfig>);
  if (entry === "compatibility") {
    fireEvent.click(await screen.findByRole("button", { name: /^Compatibility receipts$/ }));
    fireEvent.click((await screen.findAllByRole("button", { name: "View compatibility receipt" }))[0]);
  }
  if (entry !== "sent") {
    await screen.findByTestId("receipt-content-disclosure");
    if (open) fireEvent.click(screen.getByTestId("receipt-content-disclosure"));
  }
  if (open) await screen.findByText(live().text_body);
  await waitFor(() => expect(streams).toHaveLength(1)); await settle();
  return { ...mounted, cache };
}
async function trigger(kind: Trigger) {
  if (kind === "manual refresh") { fireEvent.click(screen.getByRole("button", { name: /^Refresh$/ })); await settle(); return; }
  if (kind === "SSE invalidation") {
    await act(async () => streams.at(-1)!.controller.enqueue(new TextEncoder().encode('event: submission.updated\ndata: {}\n\n'))); await settle(); return;
  }
  const before = streams.length;
  vi.useFakeTimers();
  try {
    await act(async () => {
      const last = streams.at(-1)!; last.closed = true; last.controller.close();
      for (let i = 0; i < 40; i++) await Promise.resolve();
      await vi.advanceTimersByTimeAsync(1000);
      for (let i = 0; i < 40; i++) await Promise.resolve();
    });
  } finally { vi.useRealTimers(); }
  expect(streams).toHaveLength(before + 1); await settle();
}
const reads = (path: string) => calls.filter(call => call.path === path);
function expectNoPrivateData(cache: Map<unknown, unknown>) {
  const serialized = JSON.stringify([...cache.values()]);
  for (const canary of ["PRIVATE-LIVE-BODY", "private-bcc", "private subject", "private-attachment"]) expect(serialized).not.toContain(canary);
}
function expectReadOnly() { expect(calls.every(call => call.method === "GET" && !/\/(retry|send|download)(\/|$)/.test(call.path))).toBe(true); }
function expectCleared() {
  expect(document.body).not.toHaveTextContent("PRIVATE-LIVE-BODY");
  expect(document.body).not.toHaveTextContent("private-bcc");
  expect(document.body).not.toHaveTextContent("private-attachment");
}

describe.each<Entry>(["company", "compatibility", "sent"])("%s visible private content refresh", entry => {
  it.each<Trigger>(["manual refresh", "SSE invalidation", "EOF reconnect"])("%s reauthorizes body and then attachments without remount or cache retention", async kind => {
    const { cache } = await mount(entry); const node = screen.getByTestId("live-submission-content"); const before = calls.length; version = 2;
    await trigger(kind);
    expect.soft(calls.slice(before).filter(call => call.path === contentPath())).toHaveLength(1);
    expect.soft(calls.slice(before).filter(call => call.path === attachmentPath())).toHaveLength(1);
    expect.soft(document.body).toHaveTextContent(live().text_body);
    expect.soft(document.body).toHaveTextContent("private-bcc-2@fixture.test");
    expect.soft(document.body).toHaveTextContent("private-attachment-2.txt");
    expect(screen.getByTestId("live-submission-content")).toBe(node);
    if (entry !== "sent") expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "true");
    expectNoPrivateData(cache); expectReadOnly();
  });

  it.each([403, 404])("a live content %s clears already displayed body and attachments immediately", async status => {
    const { cache } = await mount(entry); const beforeAttachments = reads(attachmentPath()).length;
    intercept = call => call.path === contentPath() ? Promise.resolve(denied(status)) : undefined;
    await trigger("manual refresh");
    expect.soft(screen.queryByText(/Content and attachments are currently unavailable/)).toBeInTheDocument();
    expect.soft(document.body).not.toHaveTextContent("PRIVATE-LIVE-BODY");
    expect.soft(document.body).not.toHaveTextContent("private-bcc");
    expect.soft(document.body).not.toHaveTextContent("private-attachment");
    expect(reads(attachmentPath())).toHaveLength(beforeAttachments);
    expectNoPrivateData(cache); expectReadOnly();
  });

  it.each([403, 404])("an attachment %s cannot leave either private surface visible", async status => {
    await mount(entry); version = 2;
    intercept = call => call.path === attachmentPath() ? Promise.resolve(denied(status)) : undefined;
    await trigger("SSE invalidation");
    expect.soft(screen.queryByText(/Content and attachments are currently unavailable/)).toBeInTheDocument();
    expect.soft(document.body).not.toHaveTextContent("PRIVATE-LIVE-BODY");
    expect.soft(document.body).not.toHaveTextContent("private-attachment");
    expectReadOnly();
  });

  it("ping, token-only rotation and unrelated URL data neither refetch nor wipe open content", async () => {
    await mount(entry); const node = screen.getByTestId("live-submission-content"); const before = reads(contentPath()).length; const beforeScope = sessionScope();
    await act(async () => streams[0].controller.enqueue(new TextEncoder().encode('event: ping\ndata: {}\n\n')));
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    await navigate(`${location.query}&panel=unchanged`);
    expect(sessionScope()).toBe(beforeScope); expect(reads(contentPath())).toHaveLength(before);
    expect(screen.getByTestId("live-submission-content")).toBe(node); expect(document.body).toHaveTextContent(live().text_body);
    version = 2; await trigger("manual refresh");
    expect.soft(reads(contentPath())).toHaveLength(before + 1);
    expect.soft(reads(contentPath()).at(-1)?.headers.get("Authorization")).toBe("Bearer rotated-token");
    expectReadOnly();
  });
});

describe.each<Entry>(["company", "compatibility"])("%s disclosure boundaries", entry => {
  it("refresh stays lazy before opening and after collapse", async () => {
    await mount(entry, false); await trigger("manual refresh"); await trigger("EOF reconnect");
    expect(reads(contentPath())).toHaveLength(0); expect(reads(attachmentPath())).toHaveLength(0);
    fireEvent.click(screen.getByTestId("receipt-content-disclosure")); await screen.findByText(live().text_body);
    fireEvent.click(screen.getByTestId("receipt-content-disclosure")); const before = reads(contentPath()).length;
    await trigger("manual refresh"); expect(reads(contentPath())).toHaveLength(before);
    expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false"); expectCleared(); expectReadOnly();
  });
  it.each(["revoked", "denied", "purged"])("an observed %s receipt removes pending private content; regrant never opens it", async mode => {
    await mount(entry); const pending = deferred();
    intercept = call => call.path === contentPath() ? pending.promise : undefined;
    await trigger("manual refresh"); const oldRead = reads(contentPath()).at(-1)!;
    canView = false;
    const prefix = entry === "company" ? "/api/v1/company/submissions/" : "/api/v1/outbound/";
    intercept = call => call.path === `${prefix}${job}` && mode !== "revoked" ? Promise.resolve(denied(mode === "purged" ? 404 : 403)) : call.path === contentPath() ? pending.promise : undefined;
    await trigger("manual refresh");
    expect(screen.queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument(); expectCleared();
    expect(oldRead.signal?.aborted).toBe(true);
    await act(async () => pending.resolve(json(live()))); await settle(); expectCleared();
    intercept = undefined; canView = true; await trigger("manual refresh");
    expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false"); expectCleared(); expectReadOnly();
  });
});

it("refresh reaches only content mounted inside that workspace", async () => {
  const { cache } = await mount("company", true, true); const before = reads(contentPath(outsideJob)).length;
  version = 2; await trigger("manual refresh");
  expect.soft(within(screen.getByRole("main")).queryByText(live().text_body)).toBeInTheDocument();
  expect(reads(contentPath(outsideJob))).toHaveLength(before);
  expect(screen.getByTestId("outside-view")).toHaveTextContent(live(outsideJob, 1).text_body);
  expectNoPrivateData(cache); expectReadOnly();
});

it.each(["body", "attachments"])("a repeated refresh aborts a pending %s and rejects its late completion", async phase => {
  await mount("company"); const pending = deferred(); const path = phase === "body" ? contentPath() : attachmentPath();
  intercept = call => call.path === path ? pending.promise : undefined;
  await trigger("manual refresh"); const oldRead = reads(path).at(-1)!;
  expect.soft(oldRead.signal?.aborted).toBe(false);
  intercept = undefined; version = 2; await trigger("manual refresh");
  expect(oldRead.signal?.aborted).toBe(true);
  await act(async () => pending.resolve(json(phase === "body" ? live(job, 1) : files(1)))); await settle();
  expect.soft(document.body).toHaveTextContent(live().text_body); expect.soft(document.body).not.toHaveTextContent(live(job, 1).text_body);
  expectReadOnly();
});

it.each(["body", "attachments"])("selection change retires the refreshed partial %s stream", async phase => {
  await mount("sent"); const path = phase === "body" ? contentPath() : attachmentPath();
  const partial = partialJSON(phase === "body" ? live(job, 1) : files(1));
  intercept = call => call.path === path ? Promise.resolve(partial.response) : undefined;
  const before = reads(path).length; await trigger("manual refresh");
  expect.soft(reads(path)).toHaveLength(before + 1);
  if (reads(path).length > before) await act(async () => partial.first());
  const oldRead = reads(path).at(-1)!; version = 2;
  await navigate(`mailbox=${mailbox}&folder=sent&message=${otherJob}`);
  await screen.findByText(live(otherJob).text_body); expect(oldRead.signal?.aborted).toBe(true);
  if (reads(path).length > before) await act(async () => partial.finish()); await settle();
  expect(document.body).not.toHaveTextContent(live(job, 1).text_body); expect(document.body).toHaveTextContent(live(otherJob).text_body); expectReadOnly();
});

it.each(["account", "tenant", "logout", "unmount"])("%s retires a pending refreshed body without late admission", async change => {
  const mounted = await mount("company"); const pending = deferred();
  intercept = call => call.path === contentPath() ? pending.promise : undefined;
  const before = reads(contentPath()).length; await trigger("manual refresh");
  expect.soft(reads(contentPath())).toHaveLength(before + 1); const oldRead = reads(contentPath()).at(-1)!;
  if (change === "unmount") mounted.unmount();
  else await act(async () => { if (change === "logout") clearSessionCredentials(); else identity(change === "account" ? "next-reader" : "reader", change === "tenant" ? otherTenant : tenant); });
  expect(oldRead.signal?.aborted).toBe(true);
  await act(async () => pending.resolve(json(live()))); await settle();
  expectCleared(); expectNoPrivateData(mounted.cache); expectReadOnly();
});

it("failed live reauthorization can be recovered by explicit workspace refresh", async () => {
  await mount("company"); intercept = call => call.path === contentPath() ? Promise.resolve(denied()) : undefined;
  await trigger("manual refresh"); expect.soft(screen.queryByText(/Content and attachments are currently unavailable/)).toBeInTheDocument();
  intercept = undefined; version = 2; await trigger("manual refresh");
  expect.soft(document.body).toHaveTextContent(live().text_body); expect(screen.queryByText(/Content and attachments are currently unavailable/)).not.toBeInTheDocument(); expectReadOnly();
});
