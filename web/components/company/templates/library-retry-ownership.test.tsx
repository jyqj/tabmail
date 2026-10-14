import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import TemplatesPage from "@/app/(dashboard)/company/templates/page";
import { installSession } from "@/lib/session";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const prefix = "/api/v1/company/templates";
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = (message: string) => new Response(JSON.stringify({ error: { code: "INTERNAL", message } }), { status: 503, headers: { "Content-Type": "application/json" } });
let calls: string[];
let readLibrary: () => Promise<Response>;
let pending: Array<(response: Response) => void>;
function delayed() {
  let resolve!: (response: Response) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<Response>((yes, no) => { resolve = yes; reject = no; });
  pending.push(resolve);
  return { promise, resolve, reject };
}
function changeAccount() {
  installSession("synthetic-library-retry-other", { id: "other-admin", tenant_id: "other-company", role: "admin", email: "other@fixture.test", display_name: "Other" });
}
beforeEach(() => {
  calls = []; pending = [];
  readLibrary = async () => failed("Initial library unavailable");
  installSession("synthetic-library-retry", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("ResizeObserver", class { observe() {} unobserve() {} disconnect() {} });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const method = init?.method ?? "GET";
    calls.push(`${method} ${path}`);
    if (method === "GET" && path === prefix) return readLibrary();
    if (method === "GET" && path === "/api/v1/company/mailboxes") return json([]);
    throw new Error(`Unexpected synthetic request: ${method} ${path}`);
  });
});
afterEach(async () => {
  cleanup(); await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});
async function mountFailedLibrary() {
  const cache = new Map();
  const view = render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}>
    <TemplatesPage />
  </SWRConfig>);
  await screen.findByRole("alert");
  return view;
}

it.each(["unmount", "session"] as const)("suppresses a rejected manual library retry after %s", async destination => {
  const view = await mountFailedLibrary();
  const gate = delayed(); readLibrary = () => gate.promise;
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(calls.filter(call => call === `GET ${prefix}`)).toHaveLength(2));
  if (destination === "unmount") view.unmount();
  else {
    readLibrary = async () => json([]);
    act(changeAccount); await screen.findByText("No templates yet");
  }
  const count = calls.length;
  await act(async () => gate.reject(new TypeError("Obsolete retry transport failure")));
  expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
  expect(calls).toHaveLength(count);
});

it("suppresses a failed HTTP response from a manual retry after unmount", async () => {
  const view = await mountFailedLibrary();
  const gate = delayed(); readLibrary = () => gate.promise;
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(calls.filter(call => call === `GET ${prefix}`)).toHaveLength(2));
  view.unmount(); const count = calls.length;
  await act(async () => gate.resolve(failed("Obsolete retry response failure")));
  expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
  expect(calls).toHaveLength(count);
});

it("keeps a current owner's real retry failure visible and does not start subsequent reads", async () => {
  await mountFailedLibrary();
  const mailboxReads = calls.filter(call => call.endsWith("/company/mailboxes")).length;
  readLibrary = async () => failed("Current retry response failure");
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Current retry response failure"));
  expect(calls.filter(call => call === `GET ${prefix}`)).toHaveLength(2);
  expect(calls.filter(call => call.endsWith("/company/mailboxes"))).toHaveLength(mailboxReads);
  expect(calls.every(call => call.startsWith("GET "))).toBe(true);
});
