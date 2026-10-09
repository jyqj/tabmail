import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AuthProvider } from "@/contexts/auth-context";
import { installSession } from "@/lib/session";
import RecoveryPage from "@/app/(dashboard)/company/recovery/page";

// Mount the shipping page, auth boundary, SWR and HTTP client. Only fetch and
// toast delivery are controlled; no list state or hook result is mocked.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const receiptA = "30000000-0000-4000-8000-000000000001";
const receiptB = "30000000-0000-4000-8000-000000000002";
const job = "30000000-0000-4000-8000-000000000003";
const reason = "Keep the operator recovery evidence";
const reasonLabel = "Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)";
const loading = "Loading recovery tasks…";
const empty = "No recovery tasks";
const receipt = (id: string) => ({ id, state: "held", updated_at: "2026-10-07T00:00:00Z", raw_size: 64, targets: [] });
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
const list = (ids: string[] = [], total = ids.length) => json({ data: ids.map(receipt), meta: { total } });
const failed = (status: number) => json({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL",
  message: `Synthetic recovery queue ${status}` } }, status);
type Call = { path: string; page: string | null; method: string };
let calls: Call[];
let listReply: (page: string | null) => Promise<Response>;
let pending: ((response: Response) => void)[];
function delayedList() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
const listCalls = () => calls.filter(call => call.path === "/api/v1/company/recovery");
const queue = () => within(screen.getByRole("heading", { name: "Inbound recovery queue" }).closest("section")!);
const mount = () => render(<AuthProvider><RecoveryPage /></AuthProvider>);
beforeEach(() => {
  calls = []; pending = [];
  listReply = async () => list();
  installSession("synthetic-operator-token", { id: "20000000-0000-4000-8000-000000000001",
    tenant_id: "10000000-0000-4000-8000-000000000001", email: "operator@fixture.test", display_name: "Operator", role: "super_admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    calls.push({ path: url.pathname, page: url.searchParams.get("page"), method: init?.method ?? "GET" });
    if (url.pathname === "/api/v1/company/recovery") return listReply(url.searchParams.get("page"));
    if (url.pathname === "/api/v1/auth/me/permissions" || url.pathname === "/api/v1/admin/runtime-config") return json({ data: {} });
    throw new Error(`Unexpected synthetic request: ${url.pathname}`);
  });
});
afterEach(async () => {
  await act(async () => { pending.forEach(resolve => resolve(list())); });
  cleanup();
  vi.unstubAllGlobals();
});

describe("Recovery queue authoritative load states", () => {
  it("announces the initial load and disables list actions until a response arrives", async () => {
    const gate = delayedList(); listReply = () => gate.promise;
    mount();
    await waitFor(() => expect(listCalls()).toHaveLength(1));
    expect(queue().getByText(loading)).toHaveAttribute("role", "status");
    expect(queue().queryByText(empty)).not.toBeInTheDocument();
    for (const name of ["Previous", "Next", "Refresh"]) expect(queue().getByRole("button", { name })).toBeDisabled();
    await act(async () => gate.resolve(list([receiptA])));
    await queue().findByText(`${receiptA} · held`);
    expect(queue().queryByText(loading)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Inspect original and targets" })).toBeEnabled();
  });

  it("shows the empty state only after a successful empty response", async () => {
    mount(); await queue().findByText(empty);
    expect(queue().queryByRole("alert")).not.toBeInTheDocument();
    expect(queue().queryByText(loading)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    expect(queue().getByRole("button", { name: "Refresh" })).toBeEnabled();
  });

  it.each([503, 403])("does not describe an initial %s failure as an empty queue", async status => {
    listReply = async () => failed(status);
    mount(); await queue().findByText(`Synthetic recovery queue ${status}`);
    expect(queue().getByRole("alert")).toBeInTheDocument();
    expect(queue().queryByText(empty)).not.toBeInTheDocument();
    expect(queue().queryByRole("button", { name: "Inspect original and targets" })).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Retry loading" })).toBeEnabled();
    expect(queue().getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    expect(listCalls()).toHaveLength(1);
  });

  it("restores rows and current pagination after explicit retry while retaining operator input", async () => {
    listReply = async () => failed(503);
    mount(); await queue().findByRole("alert");
    fireEvent.change(screen.getByLabelText(reasonLabel), { target: { value: reason } });
    fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: job } });
    listReply = async () => list([receiptB], 31);
    fireEvent.click(queue().getByRole("button", { name: "Retry loading" }));
    await queue().findByText(`${receiptB} · held`);
    expect(queue().queryByRole("alert")).not.toBeInTheDocument();
    expect(queue().queryByText(empty)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Next" })).toBeEnabled();
    expect(screen.getByLabelText(reasonLabel)).toHaveValue(reason);
    expect(screen.getByLabelText("Outbound job ID")).toHaveValue(job);
    expect(listCalls()).toHaveLength(2);
    expect(calls.every(call => call.method === "GET")).toBe(true);
  });

  it.each([503, 403])("retires stale rows and pagination after a %s refresh failure, then accepts a successful retry", async status => {
    listReply = async () => list([receiptA], 31);
    mount(); await queue().findByText(`${receiptA} · held`);
    fireEvent.change(screen.getByLabelText(reasonLabel), { target: { value: reason } });
    expect(queue().getByRole("button", { name: "Next" })).toBeEnabled();
    listReply = async () => failed(status);
    fireEvent.click(queue().getByRole("button", { name: "Refresh" }));
    await queue().findByText(`Synthetic recovery queue ${status}`);
    expect(queue().queryByText(`${receiptA} · held`)).not.toBeInTheDocument();
    expect(queue().queryByRole("button", { name: "Inspect original and targets" })).not.toBeInTheDocument();
    expect(queue().queryByText(empty)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    expect(screen.getByLabelText(reasonLabel)).toHaveValue(reason);
    listReply = async () => list([receiptB]);
    fireEvent.click(queue().getByRole("button", { name: "Retry loading" }));
    await queue().findByText(`${receiptB} · held`);
    expect(queue().queryByText(`${receiptA} · held`)).not.toBeInTheDocument();
    expect(queue().queryByRole("alert")).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    expect(listCalls()).toHaveLength(3);
    expect(calls.every(call => call.method === "GET")).toBe(true);
  });

  it("holds pagination while page two loads or fails and retries that same page explicitly", async () => {
    const second = delayedList();
    listReply = async page => page === "2" ? second.promise : list([receiptA], 31);
    mount(); await queue().findByText(`${receiptA} · held`);
    fireEvent.click(queue().getByRole("button", { name: "Next" }));
    await waitFor(() => expect(listCalls()).toHaveLength(2));
    expect(queue().getByText(loading)).toHaveAttribute("role", "status");
    expect(queue().queryByText(`${receiptA} · held`)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    await act(async () => second.resolve(failed(503)));
    await queue().findByRole("alert");
    expect(queue().queryByText(empty)).not.toBeInTheDocument();
    expect(queue().getByRole("button", { name: "Previous" })).toBeDisabled();
    listReply = async () => list([receiptB], 31);
    fireEvent.click(queue().getByRole("button", { name: "Retry loading" }));
    await queue().findByText(`${receiptB} · held`);
    expect(queue().getByRole("button", { name: "Previous" })).toBeEnabled();
    expect(queue().getByRole("button", { name: "Next" })).toBeDisabled();
    expect(listCalls().map(call => call.page)).toEqual(["1", "2", "2"]);
  });
});
