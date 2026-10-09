import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { MailWorkspace } from "../workspace";
import { installSession } from "@/lib/session";

// Exercise the shipping workspace, SWR, request parser and receipt components.
// Only the network boundary and Next navigation are controlled.
vi.mock("next/navigation", () => ({
  usePathname: () => "/mail", useSearchParams: () => new URLSearchParams("folder=receipts"),
  useRouter: () => ({ replace: vi.fn() }),
}));
const tenant = "10000000-0000-4000-8000-000000000001";
const nextTenant = "10000000-0000-4000-8000-000000000002";
const id = (number: number) => `30000000-0000-4000-8000-${String(number).padStart(12, "0")}`;
const receipt = (number: number, tenantId = tenant) => ({
  id: id(number), tenant_id: tenantId, state: "sent", status: "accepted",
  progress: { completeness: "known", counts: { total: 1, accepted: 1, pending: 0, temporary: 0, permanent: 0, uncertain: 0 } },
  delivery_uncertain: false, capabilities: { view_content: false, retry: false, retry_block_reason: "state_not_retryable" },
});
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), { status, headers: { "Content-Type": "application/json" } });
type Call = { url: URL; headers: Headers; method: string; signal?: AbortSignal | null };
let calls: Call[], total: number, pageSize: number;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const listCalls = () => calls.filter(call => call.url.pathname === "/api/v1/outbound");
function listResponse(page: number, selectedTenant = tenant) {
  const first = (page - 1) * pageSize;
  return json({ data: Array.from({ length: Math.min(pageSize, Math.max(0, total - first)) }, (_, index) => receipt(first + index + 1, selectedTenant)), meta: { total, page, per_page: pageSize } });
}
function identity(selectedTenant = tenant) {
  installSession("pagination-fixture-token", { id: "pagination-reader", tenant_id: selectedTenant, role: "user", display_name: "Reader", email: "reader@fixture.test" });
}
beforeEach(() => {
  identity(); calls = []; total = 41; pageSize = 20; intercept = undefined;
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: new URL(String(input), "http://localhost"), headers: new Headers(init?.headers), method: init?.method ?? "GET", signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.url.pathname === "/api/v1/company/mailboxes") return json({ data: [] });
    if (call.url.pathname === "/api/v1/company/submissions") return json({ data: [], meta: { total: 0, page: 1, per_page: 30 } });
    if (call.url.pathname === "/api/v1/outbound") return listResponse(Number(call.url.searchParams.get("page")), call.headers.get("X-Tenant-ID") ?? tenant);
    const match = /^\/api\/v1\/outbound\/30000000-0000-4000-8000-(\d+)$/.exec(call.url.pathname);
    if (match) return json({ data: receipt(Number(match[1]), call.headers.get("X-Tenant-ID") ?? tenant) });
    throw new Error(`Unexpected pagination request: ${call.url.pathname}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function mount() {
  render(<MailWorkspace />);
  fireEvent.click(await screen.findByRole("button", { name: "Compatibility receipts" }));
  await screen.findByText(`Task: ${id(1)}`);
}
const pager = () => screen.getByRole("navigation", { name: "Pagination" });
const next = () => fireEvent.click(within(pager()).getByRole("button", { name: "Next" }));
const previous = () => fireEvent.click(within(pager()).getByRole("button", { name: "Previous" }));
async function page(number: number) {
  await waitFor(() => expect(pager()).toHaveTextContent(`${number} / ${Math.max(1, Math.ceil(total / pageSize))} · ${total}`));
}

describe("compatibility receipt pagination", () => {
  it("reaches every older receipt using the server page size and bounds previous/next", async () => {
    await mount(); await page(1);
    expect(within(pager()).getByRole("button", { name: "Previous" })).toBeDisabled();
    next(); await page(2);
    expect(screen.getByText(`Task: ${id(21)}`)).toBeInTheDocument();
    expect(screen.queryByText(`Task: ${id(1)}`)).not.toBeInTheDocument();
    next(); await page(3);
    expect(screen.getByText(`Task: ${id(41)}`)).toBeInTheDocument();
    expect(within(pager()).getByRole("button", { name: "Next" })).toBeDisabled();
    previous(); await page(2);
    expect(listCalls().some(call => call.url.searchParams.get("page") === "2")).toBe(true);
    expect(listCalls().every(call => call.url.searchParams.get("per_page") === "20")).toBe(true);
    expect(calls.every(call => call.method === "GET")).toBe(true);
    expect(calls.some(call => /\/(content|attachments|retry)$/.test(call.url.pathname))).toBe(false);
  });

  it("uses returned page size instead of the company folder's fixed 30-row size", async () => {
    total = 11; pageSize = 5;
    await mount(); await page(1);
    next(); await page(2);
    expect(screen.getByText(`Task: ${id(6)}`)).toBeInTheDocument();
    next(); await page(3);
    expect(screen.getByText(`Task: ${id(11)}`)).toBeInTheDocument();
    expect(within(pager()).getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("keeps the explicitly selected aggregate when its row is on another page", async () => {
    await mount();
    fireEvent.click(screen.getAllByRole("button", { name: "View compatibility receipt" })[0]);
    expect(await screen.findByTestId("ordinary-receipt-aggregate")).toHaveTextContent(id(1));
    next(); await page(2);
    expect(screen.getByTestId("ordinary-receipt-aggregate")).toHaveTextContent(id(1));
    expect(screen.getByText(`Task: ${id(21)}`)).toBeInTheDocument();
    expect(calls.some(call => /\/(content|attachments)$/.test(call.url.pathname))).toBe(false);
  });

  it("workspace refresh revalidates the current compatibility page rather than only page one", async () => {
    await mount(); next(); await page(2);
    const before = listCalls().length; total = 45;
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await page(2);
    expect(listCalls().slice(before).some(call => call.url.searchParams.get("page") === "2")).toBe(true);
    expect(screen.getByText(`Task: ${id(21)}`)).toBeInTheDocument();
  });

  it("shows a current-page error and retries that page without pretending the list is empty", async () => {
    await mount();
    intercept = call => call.url.pathname === "/api/v1/outbound" && call.url.searchParams.get("page") === "2"
      ? Promise.resolve(json({ error: { code: "INTERNAL", message: "Compatibility page unavailable" } }, 500)) : undefined;
    next();
    await screen.findByText("Compatibility page unavailable");
    expect(screen.queryByText("No compatibility receipts")).not.toBeInTheDocument();
    expect(screen.queryByText(`Task: ${id(1)}`)).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    intercept = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await page(2);
    expect(screen.getByText(`Task: ${id(21)}`)).toBeInTheDocument();
  });

  it("rejects a response for the wrong page rather than relabeling stale rows", async () => {
    await mount();
    intercept = call => call.url.pathname === "/api/v1/outbound" && call.url.searchParams.get("page") === "2" ? Promise.resolve(listResponse(1)) : undefined;
    next();
    await screen.findByText("Invalid or out-of-scope receipt response");
    expect(screen.queryByText(`Task: ${id(1)}`)).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
  });

  it("returns to the last available page after a refreshed total shrinks", async () => {
    await mount(); next(); await page(2); next(); await page(3);
    total = 1;
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await page(1);
    expect(screen.getByText(`Task: ${id(1)}`)).toBeInTheDocument();
    expect(within(pager()).getByRole("button", { name: "Next" })).toBeDisabled();
    expect(within(pager()).getByRole("button", { name: "Previous" })).toBeDisabled();
  });

  it("a pending old-session page cannot supply replacement-session rows", async () => {
    await mount(); let release!: (response: Response) => void;
    const pending = new Promise<Response>(resolve => { release = resolve; });
    intercept = call => call.url.pathname === "/api/v1/outbound" && call.url.searchParams.get("page") === "2" && call.headers.get("X-Tenant-ID") === tenant ? pending : undefined;
    next();
    await waitFor(() => expect(listCalls().some(call => call.url.searchParams.get("page") === "2")).toBe(true));
    const old = listCalls().find(call => call.url.searchParams.get("page") === "2")!;
    await act(async () => identity(nextTenant));
    await waitFor(() => expect(listCalls().some(call => call.headers.get("X-Tenant-ID") === nextTenant)).toBe(true));
    expect(old.signal?.aborted).toBe(true);
    await act(async () => release(json({ data: [{ ...receipt(99), status: "needs_attention" }], meta: { total: 41, page: 2, per_page: 20 } })));
    await waitFor(() => expect(screen.queryByText(`Task: ${id(99)}`)).not.toBeInTheDocument());
    expect(screen.queryByText("Invalid or out-of-scope receipt response")).not.toBeInTheDocument();
  });
});
