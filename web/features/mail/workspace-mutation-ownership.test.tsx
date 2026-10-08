import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, within } from "@testing-library/react";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { workMessages } from "@/lib/company";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import { draftPage } from "./api";
import { MailWorkspace } from "./workspace";

// Real workspace, folders, message actions, request transport and SWR. Only
// Next's location, an unrelated editor view and synthetic HTTP are substituted.
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
vi.mock("@/components/company/compose", () => ({ Compose: () => <p>Compose editor</p> }));

const tenant = "10000000-0000-4000-8000-000000000001";
const mailbox = "mailbox-one";
const base = `/api/v1/company/mailboxes/${mailbox}`;
const initialMessageQuery = `mailbox=${mailbox}&message=message-one`;
const initialDraftQuery = `mailbox=${mailbox}&folder=drafts`;
type Kind = "message" | "draft";
type Call = { path: string; query: string; method: string; headers: Headers; signal?: AbortSignal | null; body?: unknown };
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
let changed: boolean;
const json = (data: unknown, status = 200, list = false) => new Response(JSON.stringify(status === 200 ? {
  data, ...(list ? { meta: { total: 60, page: 1, per_page: 30 } } : {}),
} : data), { status, headers: { "Content-Type": "application/json" } });
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
const message = (id: string) => ({ id, mailbox_id: mailbox, sender: "sender@fixture.test", recipients: ["reader@fixture.test"],
  subject: `Subject ${id}`, text_body: `Body ${id}`, seen: false, starred: changed,
  received_at: "2026-10-08T00:00:00Z" });
const draft = { id: "draft-one", mailbox_id: mailbox, revision: 7, updated_at: "2026-10-08T00:00:00Z",
  payload: { to: ["recipient@fixture.test"], subject: "Draft subject", text_body: "Draft body" } };
const writes = () => calls.filter(call => call.method === "POST" || call.method === "DELETE");
const listPath = (kind: Kind) => kind === "message" ? `${base}/messages` : "/api/v1/company/drafts";
const lists = (kind: Kind, query?: string) => calls.filter(call => call.method === "GET" && call.path === listPath(kind) && (query === undefined || call.query === query));
function identity(name = "reader", selectedTenant = tenant) {
  installSession(`${name}-token`, { id: name, tenant_id: selectedTenant, email: `${name}@fixture.test`, display_name: name, role: "user" });
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
function OriginalListObserver({ kind }: { kind: Kind }) {
  // Successful writes must invalidate their actual origin, independently of
  // whichever page the workspace's current bound mutate happens to reference.
  useAPI(kind === "message" ? ["work-messages", mailbox, "inbox", "", 1] : null,
    () => workMessages(mailbox, "inbox", "", 1));
  useAPI(kind === "draft" ? ["work-drafts", 1] : null, () => draftPage(1));
  return null;
}
async function mount(kind: Kind, observeOriginal = false) {
  location.query = kind === "message" ? initialMessageQuery : initialDraftQuery;
  const mounted = render(<><MailWorkspace />{observeOriginal && <OriginalListObserver kind={kind} />}</>);
  await screen.findByText(kind === "message" ? "Body message-one" : "Draft subject");
  await settle();
  return mounted;
}
async function begin(kind: Kind) {
  const pending = deferred();
  intercept = call => call.method === "POST" || call.method === "DELETE" ? pending.promise : undefined;
  fireEvent.click(screen.getByRole("button", { name: kind === "message" ? "Star" : "Delete draft" }));
  await settle();
  expect(writes()).toHaveLength(1);
  return pending;
}
async function depart(kind: Kind, navigation: string) {
  if (navigation === "page") fireEvent.click(screen.getByRole("button", { name: "Next" }));
  else if (navigation === "search") {
    fireEvent.change(screen.getByRole("textbox", { name: "Search subject, addresses and indexed body" }), { target: { value: "new search" } });
    fireEvent.click(screen.getByRole("button", { name: "Search" }));
  } else if (navigation === "selection") fireEvent.click(screen.getByRole("button", { name: /Subject message-two/ }));
  else if (navigation === "compose") fireEvent.click(screen.getByRole("button", { name: "Compose" }));
  else if (navigation === "away and back") {
    const before = location.query;
    await act(async () => navigate(`${before}&page=2`));
    await act(async () => navigate(before));
  } else {
    fireEvent.click(within(screen.getByRole("navigation", { name: "Mail folders" })).getByRole("button", { name: kind === "message" ? "Drafts" : "Inbox" }));
  }
  await settle();
}

beforeEach(() => {
  calls = []; changed = false; intercept = undefined;
  identity();
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  location.replace.mockImplementation((url: string) => navigate(url.split("?")[1] ?? ""));
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const url = new URL(String(input), "http://localhost");
    const call: Call = { path: url.pathname, query: url.search, method: init?.method ?? "GET", headers: new Headers(init?.headers), signal: init?.signal,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/events")) return new Response(new ReadableStream<Uint8Array>({ start(controller) {
      init?.signal?.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
    } }), { headers: { "Content-Type": "text/event-stream" } });
    if (call.path === "/api/v1/company/mailboxes") return json([{ mailbox: { id: mailbox, tenant_id: tenant, kind: "personal", full_address: "reader@fixture.test" },
      can_read: true, can_send: true, can_organize: true, template_only: false, revision: 1 }]);
    if (call.path === `${base}/messages`) return json(["message-one", "message-two"].map(message), 200, true);
    if (/\/messages\/[^/]+$/.test(call.path)) return json(message(call.path.split("/").at(-1)!));
    if (call.path.endsWith("/attachments")) return json([]);
    if (call.path.endsWith("/index-status")) return json({ total: 0, indexed: 0, failed: 0 });
    if (call.path === "/api/v1/company/drafts") return json(changed ? [] : [draft], 200, true);
    throw new Error(`Unexpected request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("mail mutation completion ownership", () => {
  it.each(["message", "draft"] as const)("the current %s action preserves its original command and refreshes its list", async kind => {
    await mount(kind);
    const pending = await begin(kind);
    const query = lists(kind)[0].query, before = lists(kind, query).length;
    changed = true;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(lists(kind, query)).toHaveLength(before + 1);
    expect(writes()).toHaveLength(1);
    if (kind === "message") {
      expect(writes()[0].body).toEqual({ action: "starred" });
      expect(await screen.findByRole("button", { name: "Unstar" })).toBeEnabled();
    } else {
      expect(writes()[0]).toMatchObject({ path: "/api/v1/company/drafts/draft-one", query: "?revision=7", method: "DELETE" });
      expect(screen.queryByText("Draft subject")).not.toBeInTheDocument();
    }
  });

  it.each([["message", "page"], ["message", "search"], ["draft", "page"]] as const)(
    "late %s success after %s navigation invalidates the original list only", async (kind, navigation) => {
      await mount(kind, true);
      const original = lists(kind)[0].query;
      const pending = await begin(kind);
      await depart(kind, navigation);
      const current = lists(kind).at(-1)!.query;
      const beforeOriginal = lists(kind, original).length, beforeCurrent = lists(kind, current).length;
      const newerURL = location.query, replacements = location.replace.mock.calls.length;
      changed = true;
      await act(async () => pending.resolve(json({ updated: true })));
      await settle();
      expect.soft(lists(kind, original)).toHaveLength(beforeOriginal + 1);
      expect.soft(lists(kind, current)).toHaveLength(beforeCurrent);
      expect(location.query).toBe(newerURL);
      expect(location.replace).toHaveBeenCalledTimes(replacements);
      expect(toast.error).not.toHaveBeenCalled();
    },
  );

  it.each([
    ["message", "selection"], ["message", "search"], ["message", "page"], ["message", "compose"], ["message", "folder"], ["message", "away and back"],
    ["draft", "page"], ["draft", "compose"], ["draft", "folder"], ["draft", "away and back"],
  ] as const)("late %s failure cannot report into a newer %s view", async (kind, navigation) => {
    await mount(kind);
    const pending = await begin(kind);
    await depart(kind, navigation);
    const before = calls.length;
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Old action denied" } }, 403)));
    await settle();
    expect(toast.error).not.toHaveBeenCalled();
    expect(calls).toHaveLength(before);
  });

  it.each(["message", "draft"] as const)("unmount retires the %s action's failure reporting", async kind => {
    const mounted = await mount(kind);
    const pending = await begin(kind);
    mounted.unmount();
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Old action denied" } }, 403)));
    await settle();
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(["message", "draft"] as const)("unrelated rerenders and token rotation keep a current %s failure visible", async kind => {
    await mount(kind);
    const pending = await begin(kind);
    const scope = sessionScope();
    await act(async () => {
      localStorage.setItem("tabmail_access_token", "rotated-token");
      window.dispatchEvent(new Event(AUTH_EVENT));
      navigate(`${location.query}&retained=new`);
    });
    expect(sessionScope()).toBe(scope);
    await act(async () => pending.resolve(json({ error: { code: "FORBIDDEN", message: "Current action denied" } }, 403)));
    await settle();
    expect(toast.error).toHaveBeenCalledWith("Current action denied");
  });

  it.each(["message", "draft"] as const)("a replacement session cannot receive the %s action's late result", async kind => {
    await mount(kind, true);
    const pending = await begin(kind);
    await act(async () => identity("replacement"));
    await settle();
    expect(writes()[0].signal?.aborted).toBe(true);
    const before = calls.length;
    await act(async () => pending.resolve(json({ updated: true })));
    await settle();
    expect(calls).toHaveLength(before);
    expect(toast.error).not.toHaveBeenCalled();
  });
});
