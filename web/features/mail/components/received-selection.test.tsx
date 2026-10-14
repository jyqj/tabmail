import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { MailWorkspace } from "@/features/mail/workspace";

// Real workspace, folder, detail, SWR and request layer. Only location,
// transport responses and toast delivery are controlled; no timer advancement.
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

type Call = { path: string; query: URLSearchParams; method: string; headers: Headers; body?: { action?: string } };
let calls: Call[];
let listed: string[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const mailboxId = "selection-mailbox";
const first = "selection-message-one";
const second = "selection-message-two";
const messageBase = `/api/v1/company/mailboxes/${mailboxId}/messages`;
const firstBody = `Private body ${first}`;
const detail = (id: string) => ({ id, mailbox_id: mailboxId, subject: `Subject ${id}`, sender: "sender@example.test", recipients: ["reader@example.test"], received_at: "2026-10-08T00:00:00Z", text_body: `Private body ${id}`, seen: false, starred: false });
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const listResponse = (ids: string[]) => new Response(JSON.stringify({ data: ids.map(detail), meta: { page: 1, per_page: 30, total: ids.length } }), { headers: { "Content-Type": "application/json" } });
const failure = () => new Response(JSON.stringify({ error: { code: "UNAVAILABLE", message: "Current list unavailable" } }), { status: 503, headers: { "Content-Type": "application/json" } });
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
function identity(user = "selection-reader", tenant = "selection-company") {
  installSession(`${user}-token`, { id: user, tenant_id: tenant, role: "user", email: `${user}@example.test`, display_name: user });
}
async function settle() { await act(async () => { for (let i = 0; i < 40; i++) await Promise.resolve(); }); }
async function mount(query = `mailbox=${mailboxId}&message=${first}`) {
  location.query = query;
  const view = render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><MailWorkspace /></SWRConfig>);
  await screen.findByText(firstBody);
  await settle();
  return view;
}
async function refresh() { fireEvent.click(screen.getByRole("button", { name: "Refresh" })); await settle(); }
function expectSelected(id = first) {
  expect(new URLSearchParams(location.query).get("message")).toBe(id);
  expect(screen.getByText(`Private body ${id}`)).toBeInTheDocument();
}
function expectClosed() {
  expect(screen.queryByText(firstBody)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download original" })).not.toBeInTheDocument();
  expect(new URLSearchParams(location.query).get("message")).toBeNull();
  expect(screen.getByText("Select a message to read")).toBeInTheDocument();
}

beforeEach(() => {
  calls = []; listed = [first, second]; intercept = undefined;
  identity();
  location.replace.mockImplementation((url: string) => navigate(url.split("?")[1] ?? ""));
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const call: Call = { path: url.pathname, query: url.searchParams, method: init?.method ?? "GET", headers: new Headers(init?.headers), body: init?.body ? JSON.parse(String(init.body)) : undefined };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/events")) return new Response(new ReadableStream<Uint8Array>({ start(controller) {
      init?.signal?.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
    } }), { headers: { "Content-Type": "text/event-stream" } });
    if (call.path === "/api/v1/company/mailboxes") return json([mailboxId, "second-mailbox"].map(id => ({ mailbox: { id, tenant_id: "selection-company", kind: "shared", full_address: `${id}@example.test` }, can_read: true, can_send: false, can_organize: true, template_only: false, revision: 1 })));
    if (call.path.endsWith("/index-status")) return json({ indexed: 2, total: 2, failed: 0 });
    if (call.path.endsWith("/messages")) return listResponse(listed);
    if (call.path.endsWith("/attachments")) return json([]);
    if (call.path.endsWith("/actions")) return json({ updated: true });
    if (/\/messages\/[^/]+$/.test(call.path)) return json(detail(call.path.split("/").at(-1)!));
    throw new Error(`Unexpected selection fixture request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("received selection after a confirmed list change", () => {
  it.each(["inbox", "archive", "trash"])("retires a previously listed %s selection after a successful refresh removes it", async folder => {
    await mount(`mailbox=${mailboxId}&folder=${folder}&message=${first}&unrelated=keep`);
    listed = [second];
    await refresh();
    expectClosed();
    expect(new URLSearchParams(location.query).get("unrelated")).toBe("keep");
    expect(calls.filter(call => call.method === "POST")).toHaveLength(0);
  });

  it("keeps the selection while a refresh is pending and retires it only after success", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.path === messageBase ? pending.promise : undefined;
    await refresh();
    expectSelected();
    await act(async () => pending.resolve(listResponse([second])));
    await settle();
    expectClosed();
  });

  it("does not turn a failed list refresh into deletion, but honors a successful read retry", async () => {
    await mount();
    intercept = call => call.path === messageBase ? Promise.resolve(failure()) : undefined;
    await refresh();
    expectSelected();
    expect(screen.getByRole("alert")).toHaveTextContent("Current list unavailable");
    intercept = undefined; listed = [second];
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await settle();
    expectClosed();
    expect(calls.filter(call => call.method === "POST")).toHaveLength(0);
  });

  it("preserves a current selected row that remains in the successful list", async () => {
    await mount();
    await refresh();
    expectSelected();
    expect(location.replace).not.toHaveBeenCalled();
  });

  it("preserves a valid deep link never observed in the current page", async () => {
    listed = [second];
    await mount(`mailbox=${mailboxId}&message=${first}&page=2&q=other`);
    listed = [];
    await refresh();
    expectSelected();
    expect(location.replace).not.toHaveBeenCalled();
  });

  it("does not misclassify the first delayed absent list as a removal", async () => {
    const pending = deferred();
    intercept = call => call.path === messageBase ? pending.promise : undefined;
    await mount();
    expectSelected();
    await act(async () => pending.resolve(listResponse([second])));
    await settle();
    expectSelected();
    expect(location.replace).not.toHaveBeenCalled();
  });

  it("keeps a newer selection when the older selection disappears during a pending refresh", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.path === messageBase ? pending.promise : undefined;
    await refresh();
    fireEvent.click(screen.getByRole("button", { name: new RegExp(`Subject ${second}`) }));
    await screen.findByText(`Private body ${second}`);
    await act(async () => pending.resolve(listResponse([second])));
    await settle();
    expectSelected(second);
  });

  it.each([
    ["page", "page=2"], ["search", "q=new-query"], ["folder", "folder=archive"], ["mailbox", "mailbox=second-mailbox"],
  ])("does not let the previous %s view retire a valid new deep link", async (_name, patch) => {
    await mount();
    const pending = deferred();
    intercept = call => call.path === messageBase && call.query.get("page") === "1" && call.query.get("q") === "" && call.query.get("folder") === "inbox" ? pending.promise : undefined;
    await refresh();
    listed = [second];
    const next = new URLSearchParams(`mailbox=${mailboxId}&message=${first}`);
    for (const [key, value] of new URLSearchParams(patch)) next.set(key, value);
    act(() => navigate(next.toString()));
    await settle();
    await act(async () => pending.resolve(listResponse([])));
    await settle();
    expectSelected();
    expect(new URLSearchParams(location.query).get(new URLSearchParams(patch).keys().next().value!)).toBe(new URLSearchParams(patch).values().next().value!);
    expect(location.replace).not.toHaveBeenCalled();
  });

  it.each(["account", "tenant"])("resets old observation when the %s changes", async boundary => {
    await mount();
    listed = [second];
    act(() => identity(boundary === "account" ? "other-reader" : "selection-reader", boundary === "tenant" ? "other-company" : "selection-company"));
    await settle();
    expectSelected();
    expect(location.replace).not.toHaveBeenCalled();
  });

  it("keeps observation through token rotation within the same identity", async () => {
    await mount();
    const before = sessionScope();
    act(() => { localStorage.setItem("tabmail_access_token", "rotated-selection-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    expect(sessionScope()).toBe(before);
    listed = [second];
    await refresh();
    expectClosed();
  });

  it("cannot resurrect retired content when a delayed detail response finally completes", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.path === `${messageBase}/${first}` ? pending.promise : undefined;
    listed = [second];
    await refresh();
    expectClosed();
    await act(async () => pending.resolve(json(detail(first))));
    await settle();
    expectClosed();
  });

  it("closes the pane after a confirmed archive is reflected by the list", async () => {
    await mount();
    intercept = call => {
      if (call.path.endsWith("/actions")) { listed = [second]; return Promise.resolve(json({ updated: true })); }
      return undefined;
    };
    fireEvent.click(within(screen.getByText(firstBody).closest("section")!).getByRole("button", { name: "Archive" }));
    await settle();
    expectClosed();
    expect(calls.filter(call => call.method === "POST").map(call => call.body)).toEqual([{ action: "archive" }]);
  });

  it("keeps selected content through a pending and rejected archive", async () => {
    await mount();
    const pending = deferred();
    intercept = call => call.path.endsWith("/actions") ? pending.promise : undefined;
    fireEvent.click(within(screen.getByText(firstBody).closest("section")!).getByRole("button", { name: "Archive" }));
    await settle();
    expectSelected();
    await act(async () => pending.resolve(failure()));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Current list unavailable"));
    expectSelected();
    expect(location.replace).not.toHaveBeenCalled();
  });
});

it("retired content stays closed while navigation has not acknowledged the clear", async () => {
  await mount();
  location.replace.mockImplementation(() => {});
  listed = [second];
  await refresh();
  expect(screen.queryByText(firstBody)).not.toBeInTheDocument();
  expect(screen.queryByRole("button", { name: "Download original" })).not.toBeInTheDocument();
  expect(location.replace).toHaveBeenCalledTimes(1);
  expect(new URLSearchParams(location.query).get("message")).toBe(first);
  listed = [first, second];
  await refresh();
  expect(screen.queryByText(firstBody)).not.toBeInTheDocument();
  expect(location.replace).toHaveBeenCalledTimes(1);
});

it("explicit selection can reopen a message after its earlier selection was retired", async () => {
  await mount();
  listed = [second];
  await refresh();
  expectClosed();
  listed = [first, second];
  await refresh();
  fireEvent.click(screen.getByRole("button", { name: new RegExp(`Subject ${first}`) }));
  await screen.findByText(firstBody);
  expectSelected();
  expect(screen.getByRole("button", { name: "Download original" })).toBeEnabled();
});
