import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import AdminIngestPage from "./ingest/page";
import AdminWebhooksPage from "./webhooks/page";
import AuditPage from "./audit/page";
import AdminDomainsPage from "./domains/page";
import { SidebarProvider } from "@/components/ui/sidebar";
import { AUTH_EVENT, installSession } from "@/lib/session";

// Real route pages, dashboard header/sidebar context, tables, SWR, session and
// HTTP client. Only the network, viewport APIs and notifications are controlled.
vi.mock("sonner", () => ({ toast: { error: vi.fn() } }));
const pages = [
  { name: "ingest", Page: AdminIngestPage, path: "/api/v1/admin/ingest/jobs", empty: "No ingest jobs", paged: true, stats: true },
  { name: "webhooks", Page: AdminWebhooksPage, path: "/api/v1/admin/webhooks/deliveries", empty: "No webhook deliveries", paged: true, stats: true },
  { name: "audit", Page: AuditPage, path: "/api/v1/admin/audit", empty: "No audit entries yet.", paged: true, stats: false },
  { name: "domains", Page: AdminDomainsPage, path: "/api/v1/admin/domains", empty: "No domain resources", paged: false, stats: false },
] as const;
type PageCase = typeof pages[number];
type Call = { path: string; params: Record<string, string>; token: string | null; tenant: string | null };
let calls: Call[];
let respond: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
function row(kind: PageCase, marker: string) {
  const common = { id: `record-${marker}`, created_at: "2026-10-09T00:00:00Z" };
  if (kind.name === "ingest") return { ...common, state: "done", source: "smtp", mail_from: marker,
    recipients: ["recipient@example.test"], attempts: 1, next_attempt_at: common.created_at, last_error: `diagnostic-${marker}` };
  if (kind.name === "webhooks") return { ...common, state: "delivered", event_type: "message.received", url: marker,
    attempts: 1, last_tried_at: common.created_at, last_error: `diagnostic-${marker}` };
  if (kind.name === "audit") return { ...common, action: marker, actor: "operator@example.test", resource_type: "mailbox", resource_id: "synthetic-resource" };
  return { ...common, domain: marker, tenant_id: "synthetic-tenant", parent_zone_id: "", is_verified: true, mx_verified: true, visibility: "private" };
}
const reply = (kind: PageCase, markers: string[] = [], total = markers.length) => json({
  data: markers.map(marker => row(kind, marker)), ...(kind.paged ? { meta: { total, per_page: 30 } } : {}),
});
const failure = (status = 503) => json({ error: { code: "UNAVAILABLE", message: `Synthetic list failure ${status}` } }, status);
function gate() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function identity(id: string) {
  installSession(`synthetic-token-${id}`, { id, tenant_id: `tenant-${id}`, role: "super_admin",
    email: `${id}@example.test`, display_name: id });
}
function mount({ Page }: PageCase) {
  return render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0, shouldRetryOnError: false,
    revalidateOnFocus: false }}><SidebarProvider><Page /></SidebarProvider></SWRConfig>);
}
const refresh = () => screen.getByRole("button", { name: "Refresh" });
const retry = () => screen.getByRole("button", { name: "Retry loading" });
const next = () => screen.getByRole("button", { name: "Next" });
const previous = () => screen.getByRole("button", { name: "Previous" });
const totalCard = () => screen.getByText("Total", { exact: true }).closest('[data-slot="card"]');

for (const kind of pages) it(`${kind.name} independent route remount does not reuse the prior completed observation`, async () => {
  const cache = new Map();
  const { Page } = kind;
  const renderPage = () => render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    shouldRetryOnError: false, revalidateOnFocus: false }}><SidebarProvider><Page /></SidebarProvider></SWRConfig>);
  respond = async () => reply(kind, ["previous.visit"], 61);
  const first = renderPage(); await screen.findByRole("table"); first.unmount();
  const current = gate(); respond = () => current.promise;
  renderPage();
  expect(screen.queryAllByText("previous.visit", { exact: true })).toHaveLength(0);
  noRowsOrTotals(kind);
  await waitFor(() => expect(calls).toHaveLength(2));
  await act(async () => current.resolve(reply(kind, ["current.visit"], 1)));
  expect(await screen.findByRole("table")).toHaveTextContent("current.visit");
});

function noRowsOrTotals(kind: PageCase) {
  expect(screen.queryByRole("table")).not.toBeInTheDocument();
  expect(screen.queryByText(kind.empty)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Next" })).not.toBeInTheDocument();
  expect(screen.queryByText("61 total")).not.toBeInTheDocument();
  if (kind.stats) {
    expect(totalCard()).toHaveTextContent("—");
    expect(totalCard()).not.toHaveTextContent(/\d/);
  }
}
beforeEach(() => {
  calls = []; pending = []; identity("first");
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("matchMedia", () => ({ matches: false, addEventListener() {}, removeEventListener() {} }));
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    expect(pages.map(page => page.path)).toContain(url.pathname);
    expect(init?.method ?? "GET").toBe("GET");
    const headers = new Headers(init?.headers);
    const call = { path: url.pathname, params: Object.fromEntries(url.searchParams),
      token: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID") };
    calls.push(call);
    return respond(call);
  });
});
afterEach(async () => {
  cleanup(); await act(async () => pending.forEach(resolve => resolve(json({ data: [] }))));
  vi.unstubAllGlobals();
});

for (const kind of pages) describe(`${kind.name} administrative list read state`, () => {
  it("announces the initial pending read without claiming rows, an empty result or counts", async () => {
    const load = gate(); respond = () => load.promise; mount(kind);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(screen.getByRole("status")).toHaveTextContent("Loading records…");
    noRowsOrTotals(kind); expect(refresh()).toBeDisabled();
    await act(async () => load.resolve(reply(kind, ["confirmed.initial"])));
    expect(await screen.findByRole("table")).toHaveTextContent("confirmed.initial");
    expect(refresh()).toBeEnabled(); expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows a confirmed empty result and zero counts only after a successful read", async () => {
    respond = async () => reply(kind); mount(kind);
    await screen.findByText(kind.empty);
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(refresh()).toBeEnabled();
    expect(screen.queryByRole("table")).not.toBeInTheDocument();
    if (kind.stats) expect(totalCard()).toHaveTextContent("Total0");
  });

  it.each([403, 503])("retains an initial %s failure and recovers only through an explicit same-read retry", async status => {
    respond = async () => failure(status); mount(kind);
    expect(await screen.findByRole("alert")).toHaveTextContent(`Synthetic list failure ${status}`);
    noRowsOrTotals(kind); expect(refresh()).toBeDisabled();
    const load = gate(); respond = () => load.promise; fireEvent.click(retry());
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(retry()).toBeDisabled(); fireEvent.click(retry()); expect(calls).toHaveLength(2);
    expect(screen.getByRole("status")).toHaveTextContent("Loading records…"); noRowsOrTotals(kind);
    expect(calls[1]).toEqual(calls[0]);
    await act(async () => load.resolve(reply(kind, ["retried.current"], 1)));
    expect(await screen.findByRole("table")).toHaveTextContent("retried.current");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument(); expect(refresh()).toBeEnabled();
  });

  it("retires cached rows, diagnostics and totals during refresh and after failure, then displays the retry response", async () => {
    respond = async () => reply(kind, ["cached.old"], 61); mount(kind);
    expect(await screen.findByRole("table")).toHaveTextContent("cached.old");
    if (kind.stats) expect(totalCard()).toHaveTextContent("Total61");
    const load = gate(); respond = () => load.promise; fireEvent.click(refresh());
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(screen.getByRole("status")).toHaveTextContent("Refreshing records…");
    noRowsOrTotals(kind); expect(refresh()).toBeDisabled();
    expect(screen.queryByText("diagnostic-cached.old")).not.toBeInTheDocument();
    await act(async () => load.resolve(failure())); await screen.findByRole("alert");
    noRowsOrTotals(kind); expect(screen.queryByText("diagnostic-cached.old")).not.toBeInTheDocument();
    respond = async () => reply(kind, ["retried.new"], 1); fireEvent.click(retry());
    expect(await screen.findByRole("table")).toHaveTextContent("retried.new");
    expect(screen.queryByText("cached.old", { exact: true })).not.toBeInTheDocument();
    if (kind.stats) expect(totalCard()).toHaveTextContent("Total1");
    expect(calls).toHaveLength(3); expect(calls[2]).toEqual(calls[0]);
  });

  it("does not display a late former-session response or retry it in the new session", async () => {
    const oldRead = gate(); const newRead = gate();
    respond = call => call.token === "Bearer synthetic-token-second" ? newRead.promise : oldRead.promise;
    mount(kind); await waitFor(() => expect(calls).toHaveLength(1));
    act(() => identity("second")); await waitFor(() => expect(calls).toHaveLength(2));
    await act(async () => newRead.resolve(reply(kind, ["second.owner"])));
    expect(await screen.findByRole("table")).toHaveTextContent("second.owner");
    await act(async () => oldRead.resolve(reply(kind, ["former.owner.secret"])));
    expect(screen.getByRole("table")).toHaveTextContent("second.owner");
    expect(screen.queryByText("former.owner.secret")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(calls[1]).toMatchObject({ token: "Bearer synthetic-token-second", tenant: "tenant-second" });
  });

  it("allows a pending read to finish across a same-session bearer rotation and uses the renewed token for refresh", async () => {
    const load = gate(); respond = () => load.promise; mount(kind);
    await waitFor(() => expect(calls).toHaveLength(1));
    act(() => { localStorage.setItem("tabmail_access_token", "synthetic-renewed-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(calls).toHaveLength(1);
    await act(async () => load.resolve(reply(kind, ["rotation.current"])));
    expect(await screen.findByRole("table")).toHaveTextContent("rotation.current");
    respond = async () => reply(kind, ["rotation.refreshed"]); fireEvent.click(refresh());
    await waitFor(() => expect(screen.getByRole("table")).toHaveTextContent("rotation.refreshed"));
    expect(calls[1]).toMatchObject({ token: "Bearer synthetic-renewed-token", tenant: "tenant-first" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});

for (const kind of pages.filter(page => page.paged)) describe(`${kind.name} page ownership and empty page recovery`, () => {
  it("retries the requested second page after failure and can go back from a confirmed empty page", async () => {
    const second = gate(); respond = call => call.params.page === "2" ? second.promise : Promise.resolve(reply(kind, ["first.page"], 31));
    mount(kind); await screen.findByRole("table"); fireEvent.click(next());
    await waitFor(() => expect(calls).toHaveLength(2));
    noRowsOrTotals(kind); expect(screen.getByRole("status")).toHaveTextContent("Loading records…");
    await act(async () => second.resolve(failure())); await screen.findByRole("alert"); noRowsOrTotals(kind);
    respond = async call => call.params.page === "2" ? reply(kind, [], 0) : reply(kind, ["back.to.first"], 1);
    fireEvent.click(retry()); await screen.findByText(kind.empty);
    expect(previous()).toBeEnabled(); expect(next()).toBeDisabled(); fireEvent.click(previous());
    expect(await screen.findByRole("table")).toHaveTextContent("back.to.first");
    expect(calls.map(call => call.params.page)).toEqual(["1", "2", "2", "1"]);
  });

  it("resets pagination when a new session supersedes the in-flight second page", async () => {
    const oldRead = gate(); respond = call => call.token === "Bearer synthetic-token-second" ? Promise.resolve(reply(kind, ["new.owner.first"])) :
      call.params.page === "2" ? oldRead.promise : Promise.resolve(reply(kind, ["old.owner.first"], 31));
    mount(kind); await screen.findByRole("table"); fireEvent.click(next());
    await waitFor(() => expect(calls).toHaveLength(2)); act(() => identity("second"));
    expect(await screen.findByRole("table")).toHaveTextContent("new.owner.first");
    expect(calls.at(-1)?.params.page).toBe("1");
    await act(async () => oldRead.resolve(reply(kind, ["old.owner.second"])));
    expect(screen.getByRole("table")).toHaveTextContent("new.owner.first");
    expect(screen.queryByText("old.owner.second")).not.toBeInTheDocument();
  });
});

for (const scenario of [
  { kind: pages[0], placeholder: "recipient@example.com", param: "recipient", value: "target@example.test" },
  { kind: pages[1], placeholder: "https://example.com/hook", param: "url", value: "https://target.example.test/hook" },
]) it(`${scenario.kind.name} retries the current filter unchanged, then retires it when the session changes`, async () => {
  respond = async () => reply(scenario.kind, ["before.filter"], 1); mount(scenario.kind); await screen.findByRole("table");
  const filtered = gate(); respond = () => filtered.promise;
  fireEvent.change(screen.getByPlaceholderText(scenario.placeholder), { target: { value: scenario.value } });
  await waitFor(() => expect(calls).toHaveLength(2)); noRowsOrTotals(scenario.kind);
  await act(async () => filtered.resolve(failure())); await screen.findByRole("alert");
  respond = async () => reply(scenario.kind, ["filtered.current"]); fireEvent.click(retry());
  expect(await screen.findByRole("table")).toHaveTextContent("filtered.current");
  expect(screen.getByPlaceholderText(scenario.placeholder)).toHaveValue(scenario.value);
  expect(calls[2].params).toEqual(calls[1].params); expect(calls[2].params[scenario.param]).toBe(scenario.value);
  respond = async () => reply(scenario.kind, ["new.session.default"]); act(() => identity("second"));
  await waitFor(() => expect(screen.getByRole("table")).toHaveTextContent("new.session.default"));
  expect(screen.getByPlaceholderText(scenario.placeholder)).toHaveValue("");
  expect(calls.at(-1)?.params[scenario.param]).toBeUndefined(); expect(calls.at(-1)?.params.page).toBe("1");
});
