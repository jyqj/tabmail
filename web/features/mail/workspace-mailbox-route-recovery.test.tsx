import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { installSession } from "@/lib/session";
import type { MailDraft } from "@/lib/company";
import { MailWorkspace } from "./workspace";

// Recovery regression with NEW bytes and a NEW path. The earlier 181-line
// regression and its 02d92c... identity are historical evidence, not this file.
// Qualification requires this exact file on both the public R2 source and the
// candidate. Only Next navigation, HTTP responses and toast delivery are fake;
// Workspace, SWR, session, request/SSE parsing, readers and Compose are real.
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
const tenant = "synthetic-tenant";
const listPath = "/api/v1/company/mailboxes";
const draft: MailDraft = {
  id: "draft-one", mailbox_id: "mailbox-two", revision: 1, updated_at: "2026-10-09T00:00:00Z",
  payload: { to: ["recipient@fixture.test"], subject: "Saved cross-mailbox draft", text_body: "Saved synthetic body" },
};
type Call = { path: string; method: string; signal?: AbortSignal | null };
type Stream = { path: string; signal?: AbortSignal | null; closed: boolean };
function box(id: string, canRead = true, canSend = true) {
  return { mailbox: { id, tenant_id: tenant, kind: "personal", full_address: id + "@fixture.test" },
    can_read: canRead, can_send: canSend, can_organize: false, template_only: false, revision: 1 };
}
let boxes: ReturnType<typeof box>[], calls: Call[], streams: Stream[], unexpected: string[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const json = (data: unknown, list = false) => new Response(JSON.stringify({
  data, ...(list ? { meta: { total: Array.isArray(data) ? data.length : 0, page: 1, per_page: 30 } } : {}),
}), { headers: { "Content-Type": "application/json" } });
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }
const writes = () => calls.filter(call => call.method !== "GET");
const contentReads = () => calls.filter(call => /\/mailboxes\/[^/]+\/(?:messages|sent|index-status|events)(?:\/|$)/.test(call.path));
const readsFor = (id: string) => contentReads().filter(call => call.path.startsWith(listPath + "/" + id + "/"));
const composeButton = () => screen.getByRole("button", { name: "Compose" });
const mailboxButton = (id: string) => screen.getByRole("button", { name: new RegExp(id + "@fixture\\.test") });
async function mount(query: string) {
  location.query = query;
  render(<MailWorkspace />);
  await screen.findByRole("button", { name: /mailbox-one@fixture\.test/ });
  await settle();
}
async function assertUnavailable() {
  await settle();
  expect(contentReads()).toHaveLength(0);
  expect(composeButton()).toBeDisabled();
  expect(screen.getByRole("status")).toHaveTextContent(/requested mailbox.*unavailable/i);
  expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
  expect(writes()).toHaveLength(0);
}
async function assertReading(id: string) {
  await waitFor(() => expect(readsFor(id).some(call => call.path.endsWith("/messages"))).toBe(true));
  await settle();
  expect(mailboxButton(id)).toHaveAttribute("aria-pressed", "true");
  expect(streams.some(stream => stream.path === listPath + "/" + id + "/events" && !stream.signal?.aborted)).toBe(true);
}
beforeEach(() => {
  boxes = [box("mailbox-one"), box("mailbox-two")];
  calls = []; streams = []; unexpected = []; intercept = undefined;
  location.query = "";
  installSession("synthetic-session", { id: "synthetic-user", tenant_id: tenant,
    email: "author@fixture.test", display_name: "Synthetic author", role: "user" });
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  location.replace.mockImplementation((url: string) => navigate(url.split("?")[1] ?? ""));
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname,
      method: init?.method ?? "GET", signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/events")) {
      const stream: Stream = { path: call.path, signal: call.signal, closed: false };
      const body = new ReadableStream<Uint8Array>({
        start(controller) {
          call.signal?.addEventListener("abort", () => {
            if (!stream.closed) { stream.closed = true; controller.error(new DOMException("Aborted", "AbortError")); }
          }, { once: true });
        },
        cancel() { stream.closed = true; },
      });
      streams.push(stream);
      return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (call.path === listPath) return json(boxes);
    if (call.path === "/api/v1/company/drafts") return json([draft], true);
    if (call.path === "/api/v1/company/drafts/draft-one") return json(draft);
    if (/\/messages\/[^/]+$/.test(call.path)) return json({
      id: call.path.split("/").at(-1), subject: "Synthetic detail", sender: "sender@fixture.test",
      recipients: ["mailbox-one@fixture.test"], text_body: "Synthetic detail body", seen: true, starred: false,
    });
    if (call.path.endsWith("/attachments") || call.path.endsWith("/templates")) return json([]);
    if (call.path.endsWith("/messages") || call.path.endsWith("/sent") || call.path.endsWith("/submissions")) return json([], true);
    if (call.path.endsWith("/index-status")) return json({ total: 0, indexed: 0, failed: 0 });
    unexpected.push(call.method + " " + call.path);
    throw new Error("Unexpected synthetic request: " + unexpected.at(-1));
  });
});
afterEach(() => {
  cleanup(); vi.useRealTimers(); vi.unstubAllGlobals();
  expect(unexpected).toEqual([]);
});

describe("explicit mailbox route recovery", () => {
  for (const [id, folder] of [["R01", "inbox"], ["R02", "sent"], ["R03", "archive"], ["R04", "trash"]] as const) {
    it(id + " keeps an unavailable " + folder + " route unresolved", async () => {
      await mount("mailbox=unavailable&folder=" + folder);
      await assertUnavailable();
      expect(mailboxButton("mailbox-one")).toHaveAttribute("aria-pressed", "false");
      expect(mailboxButton("mailbox-two")).toHaveAttribute("aria-pressed", "false");
    });
  }
  it("R05 rejects an explicit mailbox with no read or send rights", async () => {
    boxes = [box("mailbox-one"), box("mailbox-two", false, false)];
    await mount("mailbox=mailbox-two");
    await assertUnavailable();
  });
  it("R06 distinguishes an empty explicit mailbox from an absent parameter", async () => {
    await mount("mailbox=");
    await assertUnavailable();
  });
  it("R07 prevents Compose from silently choosing another mailbox", async () => {
    await mount("mailbox=unavailable");
    expect(composeButton()).toBeDisabled();
    fireEvent.click(composeButton());
    await settle();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(writes()).toHaveLength(0);
  });
  it("R08 retains the ordinary default when no mailbox parameter exists", async () => {
    await mount("");
    await assertReading("mailbox-one");
    expect(readsFor("mailbox-two")).toHaveLength(0);
    expect(composeButton()).toBeEnabled();
  });
  it("R09 resolves a valid explicit mailbox instead of the first one", async () => {
    await mount("mailbox=mailbox-two");
    await assertReading("mailbox-two");
    expect(readsFor("mailbox-one")).toHaveLength(0);
  });
  it("R10 keeps a selected send-only identity usable for Compose", async () => {
    boxes = [box("mailbox-one"), box("mailbox-two", false, true)];
    await mount("mailbox=mailbox-two");
    expect(contentReads()).toHaveLength(0);
    expect(mailboxButton("mailbox-two")).toHaveAttribute("aria-pressed", "true");
    expect(composeButton()).toBeEnabled();
    fireEvent.click(composeButton());
    expect(await screen.findByLabelText("From mailbox")).toHaveValue("mailbox-two");
    expect(writes()).toHaveLength(0);
  });
  it("R11 retains an authorized sender for a selected read-only mailbox", async () => {
    boxes = [box("mailbox-one"), box("mailbox-two", true, false)];
    await mount("mailbox=mailbox-two");
    await assertReading("mailbox-two");
    fireEvent.click(composeButton());
    expect(await screen.findByLabelText("From mailbox")).toHaveValue("mailbox-one");
    expect(writes()).toHaveLength(0);
  });
  it("R12 does not report permanent unavailability during the initial read", async () => {
    location.query = "mailbox=mailbox-two";
    const pending = deferred();
    intercept = call => call.path === listPath ? pending.promise.then(response => response.clone()) : undefined;
    render(<MailWorkspace />);
    await waitFor(() => expect(calls.filter(call => call.path === listPath)).toHaveLength(1));
    expect(screen.queryByText(/requested mailbox.*unavailable/i)).not.toBeInTheDocument();
    expect(contentReads()).toHaveLength(0);
    expect(composeButton()).toBeDisabled();
    await act(async () => pending.resolve(json(boxes)));
    await assertReading("mailbox-two");
  });
  it("R13 closes a revoked mailbox and its stream without a fallback read", async () => {
    await mount("mailbox=mailbox-two");
    await assertReading("mailbox-two");
    const stream = streams.find(value => value.path.endsWith("/mailbox-two/events"))!;
    boxes = [box("mailbox-one")];
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await waitFor(() => expect(screen.getByRole("status")).toHaveTextContent(/requested mailbox.*unavailable/i));
    await settle();
    expect(stream.signal?.aborted).toBe(true);
    expect(readsFor("mailbox-one")).toHaveLength(0);
    expect(composeButton()).toBeDisabled();
  });
  it("R14 resolves the requested mailbox after an explicit refresh restores access", async () => {
    boxes = [box("mailbox-one")];
    await mount("mailbox=mailbox-two");
    await assertUnavailable();
    boxes = [box("mailbox-one"), box("mailbox-two")];
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await assertReading("mailbox-two");
    expect(readsFor("mailbox-one")).toHaveLength(0);
    expect(composeButton()).toBeEnabled();
  });
  it("R15 reads another mailbox only after explicit selection and clears stale detail and page", async () => {
    await mount("mailbox=unavailable&message=stale-message&page=4&q=retained");
    await assertUnavailable();
    fireEvent.click(mailboxButton("mailbox-one"));
    await assertReading("mailbox-one");
    const query = new URLSearchParams(location.query);
    expect(query.get("mailbox")).toBe("mailbox-one");
    expect(query.get("message")).toBeNull();
    expect(query.get("page")).toBeNull();
    expect(query.get("q")).toBe("retained");
  });
  it("R16 preserves cross-mailbox drafts and explicitly opening one", async () => {
    await mount("mailbox=unavailable&folder=drafts");
    const open = await screen.findByRole("button", { name: "Continue editing" });
    expect(contentReads()).toHaveLength(0);
    expect(calls.some(call => call.path === "/api/v1/company/drafts")).toBe(true);
    fireEvent.click(open);
    expect(await screen.findByLabelText("Subject")).toHaveValue(draft.payload.subject);
    expect(screen.getByLabelText("From mailbox")).toHaveValue("mailbox-two");
    expect(writes()).toHaveLength(0);
  });
  it("R17 preserves cross-mailbox delivery status without a fallback stream", async () => {
    await mount("mailbox=unavailable&folder=receipts");
    await screen.findByText("No submissions on this page");
    expect(screen.getByRole("heading", { name: "Delivery status" })).toBeInTheDocument();
    expect(calls.some(call => call.path === "/api/v1/company/submissions")).toBe(true);
    expect(contentReads()).toHaveLength(0);
    expect(writes()).toHaveLength(0);
  });
  it("R18 retires pending editor opening between two unavailable mailbox routes", async () => {
    const pending = deferred();
    intercept = call => call.path === "/api/v1/company/drafts/draft-one" ? pending.promise : undefined;
    await mount("mailbox=unavailable-one&folder=drafts");
    fireEvent.click(await screen.findByRole("button", { name: "Continue editing" }));
    await settle();
    expect(calls.filter(call => call.path === "/api/v1/company/drafts/draft-one")).toHaveLength(1);
    await act(async () => navigate("mailbox=unavailable-two&folder=drafts"));
    await act(async () => pending.resolve(json(draft)));
    await settle();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(location.query).toBe("mailbox=unavailable-two&folder=drafts");
    expect(writes()).toHaveLength(0);
  });
});
