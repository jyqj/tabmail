import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { installSession } from "@/lib/session";
import { CompanyAudit } from "./audit";

// Exercise the production component, session-scoped SWR and HTTP client.
// Only the network boundary is controlled; no hook or list state is mocked.
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
const row = (action: string) => ({
  id: action, action, actor: "operator@fixture.test", resource_type: "mailbox",
  resource_id: "synthetic-mailbox", reason: "Synthetic audit reason",
  created_at: "2026-10-08T08:00:00Z",
});
const reply = (actions: string[] = [], total = actions.length) => json({ data: actions.map(row), meta: { total } });
const failure = (status = 503) => json({ error: { code: "UNAVAILABLE", message: `Synthetic audit failure ${status}` } }, status);
type Call = { page: string | null; token: string | null };
let calls: Call[];
let respond: (call: Call) => Promise<Response>;
let pending: ((response: Response) => void)[];
function gate() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function identity(id: string) {
  installSession(`synthetic-token-${id}`, {
    id, tenant_id: `tenant-${id}`, role: "admin", email: `${id}@fixture.test`, display_name: id,
  });
}
beforeEach(() => {
  calls = []; pending = []; respond = async () => reply();
  identity("first");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    if (url.pathname !== "/api/v1/company/audit") throw new Error(`Unexpected request: ${url.pathname}`);
    expect(init?.method ?? "GET").toBe("GET");
    const call = { page: url.searchParams.get("page"), token: new Headers(init?.headers).get("Authorization") };
    calls.push(call);
    return respond(call);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(reply())); });
  vi.unstubAllGlobals();
});

describe("Company audit authoritative read states", () => {
  it("announces an initial load without claiming an empty result or an authoritative total", async () => {
    const load = gate(); respond = () => load.promise;
    render(<CompanyAudit />);
    await waitFor(() => expect(calls).toHaveLength(1));
    expect(screen.getByRole("status")).toHaveTextContent("Loading audit records…");
    expect(screen.queryByText("No audit records")).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeDisabled();
    await act(async () => load.resolve(reply(["mailbox.created"])));
    await screen.findByRole("heading", { name: "mailbox.created" });
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
  });

  it("shows an explicit empty result only after a successful response", async () => {
    render(<CompanyAudit />);
    await screen.findByText("No audit records");
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Refresh" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("preserves the safe audit projection and successful pagination", async () => {
    respond = async () => reply(["grant.revoked"], 31);
    render(<CompanyAudit />);
    await screen.findByRole("heading", { name: "grant.revoked" });
    expect(screen.getByText("Synthetic audit reason")).toBeInTheDocument();
    expect(screen.getByText(/operator@fixture.test · mailbox · synthetic-mailbox/)).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Next" })).toBeEnabled();
  });

  it.each([403, 503])("keeps an initial %s error distinct from empty and recovers through a real retry", async status => {
    respond = async () => failure(status);
    render(<CompanyAudit />);
    await screen.findByRole("alert");
    expect(screen.queryByText("No audit records")).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    const retry = gate(); respond = () => retry.promise;
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(screen.getByRole("button", { name: "Retry loading" })).toBeDisabled();
    await act(async () => retry.resolve(reply(["employee.invited"])));
    await screen.findByRole("heading", { name: "employee.invited" });
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
    expect(calls.map(call => call.page)).toEqual(["1", "1"]);
  });

  it("retires cached rows and total when a background read fails", async () => {
    respond = async () => reply(["old.action"], 61);
    render(<CompanyAudit />);
    await screen.findByRole("heading", { name: "old.action" });
    const refresh = gate(); respond = () => refresh.promise;
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(screen.queryByRole("heading", { name: "old.action" })).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    await act(async () => refresh.resolve(failure()));
    await screen.findByRole("alert");
    expect(screen.queryByText(/61/)).not.toBeInTheDocument();
    respond = async () => reply(["current.action"], 1);
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByRole("heading", { name: "current.action" });
    expect(screen.queryByRole("heading", { name: "old.action" })).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
  });

  it("holds pagination while the next page loads or fails, then retries that same page", async () => {
    const second = gate();
    respond = async call => call.page === "2" ? second.promise : reply(["first.page"], 31);
    render(<CompanyAudit />);
    await screen.findByRole("heading", { name: "first.page" });
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(screen.queryByRole("heading", { name: "first.page" })).not.toBeInTheDocument();
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    await act(async () => second.resolve(failure()));
    await screen.findByRole("alert");
    expect(screen.queryByRole("navigation", { name: "Pagination" })).not.toBeInTheDocument();
    respond = async () => reply(["second.page"], 31);
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByRole("heading", { name: "second.page" });
    expect(screen.getByRole("button", { name: "Previous" })).toBeEnabled();
    expect(screen.getByRole("button", { name: "Next" })).toBeDisabled();
    expect(calls.map(call => call.page)).toEqual(["1", "2", "2"]);
  });

  it("resets pagination on a session change and never exposes a late former-owner response", async () => {
    const oldPage = gate();
    respond = async call => call.token === "Bearer synthetic-token-second" ? reply(["second.owner"]) :
      call.page === "2" ? oldPage.promise : reply(["first.owner"], 31);
    render(<CompanyAudit />);
    await screen.findByRole("heading", { name: "first.owner" });
    fireEvent.click(screen.getByRole("button", { name: "Next" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    act(() => identity("second"));
    await screen.findByRole("heading", { name: "second.owner" });
    expect(calls.at(-1)).toEqual({ page: "1", token: "Bearer synthetic-token-second" });
    await act(async () => oldPage.resolve(reply(["late.secret.audit"])));
    expect(screen.queryByRole("heading", { name: "late.secret.audit" })).not.toBeInTheDocument();
    expect(screen.getByRole("heading", { name: "second.owner" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Previous" })).toBeDisabled();
  });
});
