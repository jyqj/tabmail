import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { MailWorkspace } from "./workspace";

// Exercise all six shipping entry points through the real workspace, folders,
// AuthProvider, SWR, parsers and HTTP client. Only Next's URL adapter, fetch and
// notification delivery are synthetic; this is not a live browser/API gate.
const location = vi.hoisted(() => ({ query: "", listeners: new Set<() => void>(), replace: vi.fn() }));
vi.mock("next/navigation", async () => {
  const { useSyncExternalStore } = await import("react");
  return {
    usePathname: () => "/mail",
    useSearchParams: () => new URLSearchParams(useSyncExternalStore(
      (notify: () => void) => { location.listeners.add(notify); return () => { location.listeners.delete(notify); }; },
      () => location.query,
    )),
    useRouter: () => ({ replace: location.replace }),
  };
});
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

const kinds = ["received", "sent", "draft", "receipt", "legacy", "conversation"] as const;
type Kind = typeof kinds[number];
const paginated = ["received", "sent", "draft", "receipt"] as const;
const tenantA = "10000000-0000-4000-8000-000000000001";
const tenantB = "10000000-0000-4000-8000-000000000002";
const actorA = "20000000-0000-4000-8000-000000000001";
const actorB = "20000000-0000-4000-8000-000000000002";
const mailbox = "40000000-0000-4000-8000-000000000001";
const otherMailbox = "40000000-0000-4000-8000-000000000002";
const sourceMessage = "50000000-0000-4000-8000-000000000001";
const date = "2026-10-08T00:00:00Z";
const labels = {
  received: { loading: "Loading messages…", refreshing: "Refreshing messages…", empty: "No messages" },
  sent: { loading: "Loading sent mail…", refreshing: "Refreshing sent mail…", empty: "No messages" },
  draft: { loading: "Loading drafts…", refreshing: "Refreshing drafts…", empty: "No drafts" },
  receipt: { loading: "Loading submissions…", refreshing: "Refreshing submissions…", empty: "No submissions on this page" },
  legacy: { loading: "Loading compatibility receipts…", refreshing: "Refreshing compatibility receipts…", empty: "No compatibility receipts" },
  conversation: { loading: "Loading conversation…", refreshing: "Refreshing conversation…", empty: "No related messages" },
} as const;
type Call = { path: string; query: string; method: string; token: string | null; tenant: string | null; signal?: AbortSignal | null };
let calls: Call[], pending: ((response: Response) => void)[], kind: Kind;
let answer: (call: Call) => Promise<Response>;
const json = (data: unknown, meta?: unknown) => new Response(JSON.stringify({ data, ...(meta ? { meta } : {}) }), {
  headers: { "Content-Type": "application/json" },
});
const failureText = (reason: number | "network") => `Synthetic ${kind} list failure ${reason}`;
const failure = (reason: number) => new Response(JSON.stringify({ error: {
  code: reason === 403 ? "FORBIDDEN" : "UNAVAILABLE", message: failureText(reason),
} }), { status: reason, headers: { "Content-Type": "application/json" } });
function gate() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
const idFor = (entry: Kind, version = 1) => `3${kinds.indexOf(entry) + 1}000000-0000-4000-8000-${String(version).padStart(12, "0")}`;
const rowText = (entry: Kind, version = 1) => entry === "receipt" || entry === "legacy"
  ? `Task: ${idFor(entry, version)}` : `${entry} row ${version}`;
function receipt(id: string, tenant = tenantA) {
  return { id, tenant_id: tenant, state: "sent", status: "accepted", created_at: date, delivery_uncertain: false,
    progress: { completeness: "known", counts: { total: 1, accepted: 1, pending: 0, temporary: 0, permanent: 0, uncertain: 0 } },
    capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" } };
}
function message(id: string, subject: string, mailboxId = mailbox) {
  return { id, mailbox_id: mailboxId, sender: "sender@fixture.test", recipients: ["reader@fixture.test"], subject,
    received_at: date, seen: true, starred: false };
}
function row(entry: Kind, version: number, tenant = tenantA) {
  const id = idFor(entry, version), subject = rowText(entry, version);
  if (entry === "receipt" || entry === "legacy") return receipt(id, tenant);
  if (entry === "draft") return { id, mailbox_id: mailbox, revision: version, updated_at: date,
    payload: { to: ["reader@fixture.test"], subject, text_body: "Draft body" } };
  if (entry === "sent") return { id, mailbox_id: mailbox, subject, from: "sender@fixture.test", to: ["reader@fixture.test"],
    created_at: date, revision: version, attachment_count: 0, delivery_available: true };
  return message(id, subject);
}
function reply(entry = kind, versions = [1], total = versions.length, call?: Call) {
  const params = new URLSearchParams(call?.query);
  return json(versions.map(version => row(entry, version, call?.tenant ?? tenantA)), {
    total, page: Number(params.get("page") ?? 1), per_page: entry === "legacy" ? 20 : entry === "conversation" ? 100 : 30,
  });
}
function listCall(entry: Kind, call: Call) {
  return call.method === "GET" && (entry === "received" ? /^\/api\/v1\/company\/mailboxes\/[^/]+\/messages$/.test(call.path)
    : entry === "sent" ? /^\/api\/v1\/company\/mailboxes\/[^/]+\/sent$/.test(call.path)
    : entry === "draft" ? call.path === "/api/v1/company/drafts"
    : entry === "receipt" ? call.path === "/api/v1/company/submissions"
    : entry === "legacy" ? call.path === "/api/v1/outbound"
    : /^\/api\/v1\/company\/mailboxes\/[^/]+\/messages\/[^/]+\/conversation$/.test(call.path));
}
const reads = () => calls.filter(call => listCall(kind, call));
const pageOf = (call: Call) => new URLSearchParams(call.query).get("page");
const linkName = (version: number) => `${rowText("conversation", version)} · sender@fixture.test`;
const getRow = (version = 1) => kind === "conversation" ? screen.getByRole("link", { name: linkName(version) }) : screen.getByText(rowText(kind, version));
const queryRow = (version = 1) => kind === "conversation" ? screen.queryByRole("link", { name: linkName(version) }) : screen.queryByText(rowText(kind, version));
const findRow = (entry = kind, version = 1) => entry === "conversation" ? screen.findByRole("link", { name: linkName(version) }) : screen.findByText(rowText(entry, version));
const getStatus = (phase: "loading" | "refreshing") => {
  const status = screen.getByText(labels[kind][phase]);
  expect(status).toHaveAttribute("role", "status");
  return status;
};
function expectNoEmpty() { expect(screen.queryByText(labels[kind].empty)).not.toBeInTheDocument(); }
function expectNoPager() {
  // Conversation is nested in a ReceivedFolder which has its own pagination.
  if (kind !== "conversation") expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
}
function expectReadOnly() {
  expect(calls.every(call => call.method === "GET")).toBe(true);
  expect(calls.some(call => /\/(content|retry|download)(\/|$)/.test(call.path))).toBe(false);
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
function SessionControls() {
  const auth = useAuth();
  return <>
    <button onClick={() => auth.loginWithTokens("second-token", { id: actorB, tenant_id: tenantA, role: "user", email: "second@fixture.test", display_name: "Second" })}>Switch account</button>
    <button onClick={() => auth.setTenantId(tenantB)}>Switch tenant</button>
    <button onClick={() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); }}>Rotate token only</button>
  </>;
}
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
async function openEntry() {
  if (kind === "legacy") {
    const button = await screen.findByRole("button", { name: "Compatibility receipts" });
    if (button.getAttribute("aria-pressed") !== "true") fireEvent.click(button);
  } else if (kind === "conversation") {
    const button = await screen.findByRole("button", { name: "Conversation in this mailbox" });
    if (button.getAttribute("aria-expanded") !== "true") fireEvent.click(button);
  }
  await waitFor(() => expect(reads().length).toBeGreaterThan(0));
}
async function mount(entry: Kind, extra: Record<string, string> = {}) {
  kind = entry;
  const query = new URLSearchParams({ mailbox, ...(entry === "sent" ? { folder: "sent" } : entry === "draft" ? { folder: "drafts" }
    : entry === "receipt" || entry === "legacy" ? { folder: "receipts" } : entry === "conversation" ? { message: sourceMessage } : {}), ...extra });
  location.query = query.toString();
  const view = render(<AuthProvider><SessionControls /><MailWorkspace /></AuthProvider>);
  await openEntry();
  // The real SSE client resynchronizes on its initial connection. Let that
  // legitimate read settle before measuring an explicit refresh or retry.
  await waitFor(() => expect(calls.some(call => call.path.endsWith("/events"))).toBe(true));
  await settle();
  return view;
}
async function ready(entry: Kind, extra: Record<string, string> = {}) {
  await mount(entry, extra); await findRow(entry); await settle();
}
async function refresh() {
  const count = reads().length;
  fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
  await waitFor(() => expect(reads()).toHaveLength(count + 1));
}
beforeEach(() => {
  calls = []; pending = []; kind = "received";
  answer = async call => reply(kind, [1], 61, call);
  installSession("first-token", { id: actorA, tenant_id: tenantA, role: "user", email: "first@fixture.test", display_name: "First" });
  location.replace.mockImplementation((url: string) => navigate(new URL(url, "http://localhost").search.slice(1)));
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost"), headers = new Headers(init?.headers);
    const call: Call = { path: url.pathname, query: url.search, method: init?.method ?? "GET", token: headers.get("Authorization"),
      tenant: headers.get("X-Tenant-ID"), signal: init?.signal };
    calls.push(call);
    // Overlapping SSE resync and user reads each own an HTTP response body.
    if (listCall(kind, call)) return answer(call).then(response => response.clone());
    if (call.path === "/api/v1/auth/me/permissions") return json({ can_send: true, daily_send_quota: 10, daily_receive_quota: 100,
      max_mailboxes: 2, max_domains: 1, allowed_zone_ids: null, can_create_domains: false, can_create_routes: false, can_create_api_keys: false });
    if (call.path.endsWith("/events")) return new Response(new ReadableStream<Uint8Array>({ start(controller) {
      call.signal?.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
    } }), { headers: { "Content-Type": "text/event-stream" } });
    if (call.path === "/api/v1/company/mailboxes") return json([mailbox, otherMailbox].map(id => ({ mailbox: {
      id, tenant_id: call.tenant, kind: "shared", full_address: `${id}@fixture.test`,
    }, can_read: true, can_send: true, can_organize: true, template_only: false, revision: 1 })));
    if (call.path.endsWith("/index-status")) return json({ total: 1, indexed: 1, failed: 0 });
    if (listCall("received", call)) return json([message(sourceMessage, "Opened source message")], { total: 1, page: 1, per_page: 30 });
    for (const entry of kinds) if (listCall(entry, call)) return reply(entry, [9], 1, call);
    if (/\/messages\/[^/]+$/.test(call.path)) {
      const id = call.path.split("/").at(-1)!;
      return json({ ...message(id, "Opened source message"), text_body: `Body for ${id}` });
    }
    if (call.path.endsWith("/attachments")) return json([]);
    if (call.path.endsWith("/content")) {
      const id = call.path.split("/").at(-2)!;
      return json({ id, subject: `Live subject ${id}`, from: "sender@fixture.test", to: ["reader@fixture.test"], bcc: [],
        recipient_completeness: "complete", created_at: date, text_body: `Live body ${id}`, content_redacted: false });
    }
    if (/^\/api\/v1\/(company\/submissions|outbound)\/[^/]+$/.test(call.path)) return json(receipt(call.path.split("/").at(-1)!, call.tenant ?? tenantA));
    throw new Error(`Unexpected list-state request: ${call.method} ${call.path}${call.query}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(reply(kind, [], 0))); });
  vi.useRealTimers(); vi.unstubAllGlobals();
});

describe.each(kinds)("shipping %s list states", entry => {
  it("announces its first real read without an empty result or invented total", async () => {
    const first = gate(); answer = () => first.promise;
    await mount(entry);
    getStatus("loading"); expectNoEmpty(); expectNoPager(); expect(queryRow()).not.toBeInTheDocument();
    expect(reads().length).toBeGreaterThan(0);
    expect(reads().every(call => call.token === "Bearer first-token" && call.tenant === tenantA)).toBe(true);
    await act(async () => first.resolve(reply(entry, [1], 1, reads()[0])));
    await findRow(entry);
    expect(screen.queryByText(labels[entry].loading)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expectNoEmpty(); expectReadOnly();
  });

  it("renders an explicit empty result only after successful completion", async () => {
    answer = async call => reply(kind, [], 0, call);
    await mount(entry); await screen.findByText(labels[entry].empty);
    expect(screen.queryByText(labels[entry].loading)).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(queryRow()).not.toBeInTheDocument();
    if (paginated.some(value => value === entry)) expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    expectReadOnly();
  });

  it("announces refresh while retaining its existing row DOM until the new response", async () => {
    await ready(entry); const before = getRow(), reload = gate(); answer = () => reload.promise;
    await refresh(); getStatus("refreshing"); expect(getRow()).toBe(before); expectNoEmpty(); expectNoPager();
    await act(async () => reload.resolve(reply(entry, [2], 1, reads().at(-1))));
    await findRow(entry, 2);
    expect(queryRow()).not.toBeInTheDocument();
    expect(screen.queryByText(labels[entry].refreshing)).not.toBeInTheDocument(); expectReadOnly();
  });

  it("does not present a cached empty result as settled while refreshing it", async () => {
    answer = async call => reply(kind, [], 0, call);
    await mount(entry); await screen.findByText(labels[entry].empty);
    const reload = gate(); answer = () => reload.promise;
    await refresh(); getStatus("refreshing"); expectNoEmpty(); expectNoPager();
    await act(async () => reload.resolve(reply(entry, [], 0, reads().at(-1))));
    await screen.findByText(labels[entry].empty);
    expect(screen.queryByText(labels[entry].refreshing)).not.toBeInTheDocument();
  });

  it.each(["network", 403, 503] as const)("keeps initial %s failure distinct and retries the same read once", async reason => {
    answer = async () => { if (reason === "network") throw new TypeError(failureText(reason)); return failure(reason); };
    await mount(entry); await screen.findByText(failureText(reason));
    expect(screen.getByRole("alert")).toHaveTextContent(failureText(reason)); expectNoEmpty(); expectNoPager();
    expect(queryRow()).not.toBeInTheDocument();
    const previous = reads().at(-1)!, count = reads().length, retry = gate(); answer = () => retry.promise;
    const button = screen.getByRole("button", { name: "Retry loading" }); fireEvent.click(button);
    await waitFor(() => expect(reads()).toHaveLength(count + 1));
    getStatus("loading"); expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "Retry loading" })).not.toBeInTheDocument();
    fireEvent.click(button); expect(reads()).toHaveLength(count + 1); expectNoEmpty(); expectNoPager();
    expect(reads().at(-1)).toMatchObject({ path: previous.path, query: previous.query, token: previous.token, tenant: previous.tenant });
    await act(async () => retry.resolve(reply(entry, [2], 1, reads().at(-1))));
    await findRow(entry, 2);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expectReadOnly();
  });

  it("removes cached rows and total after a failed refresh, then recovers to confirmed empty", async () => {
    await ready(entry); const reload = gate(); answer = () => reload.promise;
    await refresh(); await act(async () => reload.resolve(failure(403)));
    await screen.findByText(failureText(403));
    expect(queryRow()).not.toBeInTheDocument(); expectNoEmpty(); expectNoPager();
    const retry = gate(), count = reads().length; answer = () => retry.promise;
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(reads()).toHaveLength(count + 1));
    getStatus("refreshing"); expect(queryRow()).not.toBeInTheDocument(); expectNoEmpty(); expectNoPager();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    await act(async () => retry.resolve(reply(entry, [], 0, reads().at(-1))));
    await screen.findByText(labels[entry].empty);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });

  it.each(["account", "tenant"])("a new %s starts its own load and rejects the former list's late response", async boundary => {
    await ready(entry); const oldScope = sessionScope(), old = gate(), current = gate();
    answer = call => call.token === "Bearer first-token" && call.tenant === tenantA ? old.promise : current.promise;
    await refresh(); const former = reads().at(-1)!;
    fireEvent.click(screen.getByRole("button", { name: boundary === "account" ? "Switch account" : "Switch tenant" }));
    await waitFor(() => expect(sessionScope()).not.toBe(oldScope));
    expect(former.signal?.aborted).toBe(true);
    await openEntry();
    await waitFor(() => expect(reads().some(call => call.token !== former.token || call.tenant !== former.tenant)).toBe(true));
    getStatus("loading"); expect(queryRow()).not.toBeInTheDocument(); expectNoEmpty(); expectNoPager();
    await act(async () => old.resolve(reply(entry, [77], 777, former)));
    expect(queryRow(77)).not.toBeInTheDocument(); getStatus("loading");
    const latest = reads().at(-1)!;
    expect(latest).toMatchObject({ token: boundary === "account" ? "Bearer second-token" : "Bearer first-token", tenant: boundary === "tenant" ? tenantB : tenantA });
    await act(async () => current.resolve(reply(entry, [2], 1, latest)));
    await findRow(entry, 2); expect(queryRow(77)).not.toBeInTheDocument(); expectReadOnly();
  });

  it("token-only rotation retains the visible list and refresh uses the new credential", async () => {
    await ready(entry); const scope = sessionScope(), before = getRow(), count = reads().length;
    fireEvent.click(screen.getByRole("button", { name: "Rotate token only" })); await settle();
    expect(sessionScope()).toBe(scope); expect(reads()).toHaveLength(count); expect(getRow()).toBe(before);
    answer = async call => reply(entry, [2], 1, call);
    await refresh(); await findRow(entry, 2);
    expect(reads().at(-1)).toMatchObject({ token: "Bearer rotated-token", tenant: tenantA }); expectReadOnly();
  });
});

it.each(paginated)("%s page navigation hides the former page and retries the failed target with its query", async entry => {
  const second = gate();
  answer = async call => pageOf(call) === "2" ? second.promise : reply(kind, [1], 61, call);
  await ready(entry, entry === "received" || entry === "sent" ? { q: "needle & literal" } : {});
  const count = reads().length;
  expect(reads().every(call => pageOf(call) === "1")).toBe(true);
  fireEvent.click(screen.getByRole("button", { name: "Next" }));
  await waitFor(() => expect(reads()).toHaveLength(count + 1));
  expect(new URLSearchParams(location.query).get("page")).toBe("2");
  getStatus("loading"); expect(queryRow()).not.toBeInTheDocument(); expectNoEmpty(); expectNoPager();
  await act(async () => second.resolve(failure(503))); await screen.findByText(failureText(503)); expectNoPager();
  answer = async call => reply(entry, [2], 61, call);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await findRow(entry, 2);
  expect(reads().slice(count).map(pageOf)).toEqual(["2", "2"]);
  expect(reads()[count + 1].query).toBe(reads()[count].query);
  if (entry === "received" || entry === "sent") expect(new URLSearchParams(reads()[count + 1].query).get("q")).toBe("needle & literal");
  expect(screen.getByRole("button", { name: "Previous" })).toBeEnabled();
  expect(screen.getByRole("button", { name: "Next" })).toBeEnabled(); expectReadOnly();
});

it.each(["received", "sent", "receipt", "legacy"] as const)("%s refresh feedback keeps the selected real detail mounted", async entry => {
  await ready(entry);
  fireEvent.click(entry === "legacy" ? screen.getByRole("button", { name: "View compatibility receipt" }) : getRow().closest("button")!);
  const id = idFor(entry);
  if (entry === "receipt" || entry === "legacy") {
    const disclosure = await screen.findByTestId("receipt-content-disclosure");
    expect(calls.some(call => call.path.endsWith("/content"))).toBe(false);
    fireEvent.click(disclosure);
  }
  const body = await screen.findByText(entry === "received" ? `Body for ${id}` : `Live body ${id}`);
  const pane = entry === "received" ? body.parentElement : screen.getByTestId("live-submission-content");
  const query = location.query, reload = gate(); answer = () => reload.promise;
  await refresh(); getStatus("refreshing");
  expect(screen.getByText(body.textContent!)).toBe(body);
  expect(entry === "received" ? body.parentElement : screen.getByTestId("live-submission-content")).toBe(pane);
  if (entry === "receipt" || entry === "legacy") expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "true");
  await act(async () => reload.resolve(reply(entry, [1], 1, reads().at(-1))));
  await settle(); expect(location.query).toBe(query); expect(screen.getByText(body.textContent!)).toBe(body);
  expect(calls.every(call => call.method === "GET")).toBe(true);
});

it.each([
  ["received", "archive"], ["received", "trash"], ["sent", "archive"], ["sent", "trash"],
] as const)("%s %s uses the correct real folder and keeps loading feedback on its current mailbox", async (entry, folder) => {
  const first = gate(); answer = () => first.promise;
  await mount(entry, { folder, ...(entry === "sent" ? { source: "sent" } : {}), q: "archived needle" });
  getStatus("loading"); expectNoEmpty(); expectNoPager();
  expect(new URLSearchParams(reads()[0].query).get("folder")).toBe(folder);
  expect(new URLSearchParams(reads()[0].query).get("q")).toBe("archived needle");
  const next = gate(), count = reads().length; answer = () => next.promise;
  fireEvent.click(screen.getByRole("button", { name: new RegExp(`${otherMailbox}@fixture.test`) }));
  await waitFor(() => expect(reads().slice(count).some(call => call.path.includes(`/mailboxes/${otherMailbox}/`))).toBe(true));
  await settle();
  getStatus("loading");
  await act(async () => first.resolve(reply(entry, [77], 777, reads()[0])));
  expect(queryRow(77)).not.toBeInTheDocument(); getStatus("loading");
  expect(reads().at(-1)!.path).toContain(`/mailboxes/${otherMailbox}/`);
  expect(new URLSearchParams(reads().at(-1)!.query).get("folder")).toBe(folder);
  await act(async () => next.resolve(reply(entry, [2], 1, reads().at(-1))));
  await findRow(entry, 2); expect(queryRow(77)).not.toBeInTheDocument(); expectReadOnly();
});

it("conversation failure hides its old truncation note and retry uses the same source message", async () => {
  answer = async call => reply(kind, [1], 101, call);
  await ready("conversation");
  const note = "Showing the first 100. Use mailbox search to find the remaining messages.";
  expect(screen.getByText(note)).toBeInTheDocument();
  answer = async () => failure(403); await refresh(); await screen.findByText(failureText(403));
  expect(queryRow()).not.toBeInTheDocument(); expect(screen.queryByText(note)).not.toBeInTheDocument();
  answer = async call => reply(kind, [2], 1, call);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await findRow(kind, 2);
  expect(new Set(reads().map(call => call.path))).toEqual(new Set([`/api/v1/company/mailboxes/${mailbox}/messages/${sourceMessage}/conversation`]));
  expect(reads().every(call => new URLSearchParams(call.query).get("per_page") === "100")).toBe(true);
  expect(screen.getByRole("link", { name: new RegExp(rowText(kind, 2)) })).toHaveAttribute("href", `/mail?mailbox=${mailbox}&message=${idFor(kind, 2)}`);
  expectReadOnly();
});

it("an empty receipt page remains distinct from its independently authorized deep-link detail", async () => {
  answer = async call => reply(kind, [], 0, call);
  const selected = idFor("receipt", 8);
  await mount("receipt", { message: selected });
  await screen.findByTestId("ordinary-receipt-aggregate");
  expect(screen.getByText(labels.receipt.empty)).toBeInTheDocument();
  expect(within(screen.getByTestId("ordinary-receipt-aggregate")).getByText(`Task: ${selected}`)).toBeInTheDocument();
  expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false"); expectReadOnly();
});
