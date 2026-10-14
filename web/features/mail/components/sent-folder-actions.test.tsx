import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { useAPI } from "@/hooks/use-api";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { toast } from "sonner";
import { archivedPage } from "../api";
import { MailWorkspace } from "../workspace";

// Keep the workspace, SentFolder, SWR, action guard and authenticated API real.
// Only location, unrelated private-content/editor readers and fetch are fixtures.
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
vi.mock("./submission-content", () => ({ SubmissionContentView: ({ id }: { id: string }) => <p>Content {id}</p> }));
vi.mock("@/components/company/compose", () => ({ Compose: () => <p>Compose editor</p> }));

const tenant = "10000000-0000-4000-8000-000000000001";
const base = "/api/v1/company/mailboxes/mailbox-one";
const initialQuery = "mailbox=mailbox-one&folder=sent&message=message-one";
type Call = { path: string; query: string; method: string; body?: string; headers: Headers; signal?: AbortSignal | null };
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
let removed: boolean;
const json = (data: unknown, status = 200, list = false) => new Response(JSON.stringify(status === 200 ? { data, ...(list ? { meta: { total: 60, page: 1, per_page: 30 } } : {}) } : data), { status, headers: { "Content-Type": "application/json" } });
const row = (id: string) => ({ id, mailbox_id: "mailbox-one", from: "sender@fixture.test", to: ["reader@fixture.test"], subject: `Subject ${id}`, created_at: "2026-10-05T00:00:00Z", revision: 7, attachment_count: 0, delivery_available: false });
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
function identity(name = "reader", selectedTenant = tenant) {
  installSession(`${name}-token`, { id: name, tenant_id: selectedTenant, email: `${name}@fixture.test`, display_name: name, role: "user" });
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
const posts = () => calls.filter(call => call.method === "POST");
const listReads = (query?: string) => calls.filter(call => call.method === "GET" && call.path === `${base}/sent` && (!query || call.query === query));
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function OriginalListObserver({ folder }: { folder: string }) {
  // Another mounted consumer proves successful completion still invalidates the
  // original list, even after the workspace has changed its list key/unmounted.
  useAPI(["sent-assets", "mailbox-one", folder, "", 1], () => archivedPage("mailbox-one", folder, "", 1));
  return null;
}
async function mount(folder = "sent", observeOriginal = false) {
  location.query = `mailbox=mailbox-one&folder=${folder}&message=message-one${folder === "sent" ? "" : "&source=sent"}`;
  const result = render(<><MailWorkspace />{observeOriginal && <OriginalListObserver folder={folder}/>}</>);
  await screen.findByText("Content message-one");
  await settle();
  return result;
}
async function pendingAction(label = "Archive") {
  const pending = deferred();
  intercept = call => call.method === "POST" ? pending.promise : undefined;
  fireEvent.click(within(screen.getByRole("main")).getAllByRole("button", { name: label }).at(-1)!);
  await settle();
  expect(posts()).toHaveLength(1);
  return pending;
}

beforeEach(() => {
  calls = []; intercept = undefined; removed = false;
  identity();
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  location.replace.mockImplementation((url: string) => navigate(url.split("?")[1] ?? ""));
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const call: Call = { path: url.pathname, query: url.search, method: init?.method ?? "GET", body: typeof init?.body === "string" ? init.body : undefined, headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/events")) {
      const body = new ReadableStream<Uint8Array>({ start(controller) {
        init?.signal?.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
      } });
      return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (call.path === "/api/v1/company/mailboxes") return json(["mailbox-one", "mailbox-two"].map(id => ({ mailbox: { id, tenant_id: tenant, kind: "personal", full_address: `${id}@fixture.test` }, can_read: true, can_send: true, can_organize: true, template_only: false, revision: 1 })));
    if (call.path.endsWith("/sent") && call.method === "GET") return json((removed ? ["message-two"] : ["message-one", "message-two"]).map(row), 200, true);
    if (call.path.endsWith("/sent/message-one/actions") && call.method === "POST") { removed = true; return json({ updated: true }); }
    if (call.path.endsWith("/messages")) return json([], 200, true);
    if (call.path.endsWith("/index-status")) return json({ total: 0, indexed: 0, failed: 0 });
    throw new Error(`Unexpected request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("sent-folder action completion ownership", () => {
  it.each([
    ["sent", "Archive", "archive"], ["sent", "Move to trash", "trash"],
    ["archive", "Move to sent", "unarchive"], ["archive", "Move to trash", "trash"],
    ["trash", "Restore", "restore"],
  ])("%s / %s clears its current selection and reloads the original list", async (folder, label, action) => {
    await mount(folder);
    const originalList = listReads()[0].query;
    const pending = await pendingAction(label);
    expect(screen.getByText("Content message-one")).toBeInTheDocument();
    expect(location.replace).not.toHaveBeenCalled();
    fireEvent.click(screen.getAllByRole("button", { name: label }).at(-1)!);
    expect(posts()).toHaveLength(1);
    const originalCount = listReads(originalList).length;
    removed = true;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(posts()[0].path).toBe(`${base}/sent/message-one/actions`);
    expect(JSON.parse(posts()[0].body!)).toEqual({ revision: 7, action });
    expect(new URLSearchParams(location.query).get("message")).toBeNull();
    expect(location.replace).toHaveBeenCalledTimes(1);
    expect(listReads(originalList)).toHaveLength(originalCount + 1);
    expect(screen.queryByText("Content message-one")).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Subject message-one/ })).not.toBeInTheDocument();
  });

  it.each(["selection", "search", "page", "folder", "mailbox", "compose", "away and back"])("late success preserves newer %s and invalidates the original list", async navigation => {
    await mount("sent", true);
    const originalList = listReads()[0].query;
    const pending = await pendingAction();
    if (navigation === "selection" || navigation === "away and back") {
      fireEvent.click(screen.getByRole("button", { name: /Subject message-two/ }));
      if (navigation === "away and back") fireEvent.click(screen.getByRole("button", { name: /Subject message-one/ }));
    } else if (navigation === "search") {
      fireEvent.change(screen.getByRole("textbox", { name: "Search subject, addresses and indexed body" }), { target: { value: "new search" } });
      fireEvent.click(screen.getByRole("button", { name: "Search" }));
    } else if (navigation === "page") fireEvent.click(screen.getByRole("button", { name: "Next" }));
    else if (navigation === "folder") fireEvent.click(within(screen.getByRole("navigation", { name: "Mail folders" })).getByRole("button", { name: "Inbox" }));
    else if (navigation === "mailbox") fireEvent.click(screen.getByRole("button", { name: /mailbox-two@fixture.test/ }));
    else fireEvent.click(screen.getByRole("button", { name: "Compose" }));
    await settle();
    const newerQuery = location.query, replacements = location.replace.mock.calls.length;
    const newerList = listReads().at(-1)?.query;
    const newerListCount = listReads(newerList).length;
    const originalCount = listReads(originalList).length;
    removed = true;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect.soft(location.query).toBe(newerQuery);
    expect.soft(location.replace).toHaveBeenCalledTimes(replacements);
    expect.soft(listReads(originalList)).toHaveLength(originalCount + 1);
    if (navigation === "search" || navigation === "page") expect.soft(listReads(newerList)).toHaveLength(newerListCount);
    if (navigation === "selection") expect(screen.getByText("Content message-two")).toBeInTheDocument();
    if (navigation === "compose") expect(screen.getByText("Compose editor")).toBeInTheDocument();
  });

  it("unmounting the workspace prevents a pending completion from changing the URL", async () => {
    const mounted = await mount();
    const pending = await pendingAction();
    mounted.unmount();
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(location.query).toBe(initialQuery);
    expect(location.replace).not.toHaveBeenCalled();
  });

  it.each([false, true])("a rejected action preserves selection and does not invalidate (newer view: %s)", async newerView => {
    await mount();
    const pending = await pendingAction();
    if (newerView) fireEvent.click(screen.getByRole("button", { name: /Subject message-two/ }));
    const query = location.query, replacements = location.replace.mock.calls.length, reads = listReads().length;
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Action denied" } }, 403)));
    await settle();
    expect(location.query).toBe(query);
    expect(location.replace).toHaveBeenCalledTimes(replacements);
    expect(listReads()).toHaveLength(reads);
    expect(screen.getByText(`Content ${newerView ? "message-two" : "message-one"}`)).toBeInTheDocument();
    if (newerView) expect(toast.error).not.toHaveBeenCalled();
    else expect(toast.error).toHaveBeenCalledWith("Action denied");
  });

  it("unmounting suppresses a late action failure", async () => {
    const mounted = await mount();
    const pending = await pendingAction();
    mounted.unmount();
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Action denied" } }, 403)));
    await settle();
    expect(location.replace).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("uses the latest navigation callback without retiring an unchanged view", async () => {
    await mount();
    const pending = await pendingAction();
    await act(async () => navigate(`${initialQuery}&retained=new`));
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(location.replace).toHaveBeenCalledTimes(1);
    expect(new URLSearchParams(location.query).get("message")).toBeNull();
    expect(new URLSearchParams(location.query).get("retained")).toBe("new");
  });

  it.each(["account", "tenant"])("late success after replacing the %s cannot navigate or revalidate", async kind => {
    await mount();
    const pending = await pendingAction();
    const oldScope = sessionScope();
    await act(async () => identity(kind === "account" ? "next-reader" : "reader", kind === "tenant" ? "10000000-0000-4000-8000-000000000002" : tenant));
    await settle();
    expect(sessionScope()).not.toBe(oldScope);
    expect(posts()[0].signal?.aborted).toBe(true);
    const before = calls.length;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(calls).toHaveLength(before);
    expect(location.replace).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it("same-identity token rotation and unrelated rerenders preserve legitimate success", async () => {
    await mount();
    const pending = await pendingAction();
    const scope = sessionScope();
    await act(async () => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await settle();
    expect(sessionScope()).toBe(scope);
    const before = listReads().length;
    removed = true;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(location.replace).toHaveBeenCalledTimes(1);
    expect(new URLSearchParams(location.query).get("message")).toBeNull();
    expect(listReads()).toHaveLength(before + 1);
    expect(listReads().at(-1)!.headers.get("Authorization")).toBe("Bearer rotated-token");
  });
});
