import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { DraftPayload, MailDraft } from "@/lib/company";
import { MailWorkspace } from "@/features/mail/workspace";

// This investigation runs real Workspace, Compose, DraftWriter, SWR, session,
// request transport and readers. Only Next's location, fetch and toast delivery
// are substituted. No real mailbox, server, database or mail transport is used.
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
const initialQuery = "mailbox=mailbox-one&folder=drafts&q=old-search&page=2";
const messageQuery = "mailbox=mailbox-one&message=message-one";
const replyPayload: DraftPayload = { to: ["original@fixture.test"], subject: "Re: Original synthetic message", text_body: "Quoted original body" };
const newerQuery = "mailbox=mailbox-two&folder=sent&message=message-two&q=new-search&page=3&source=sent";
const draft: MailDraft = { id: "draft-one", mailbox_id: "mailbox-one", revision: 1,
  updated_at: "2026-10-05T00:00:00Z", payload: { to: ["recipient@fixture.test"], subject: "Synthetic draft", text_body: "Synthetic body" } };
type Call = { path: string; method: string; body?: unknown };
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const json = (data: unknown, list = false) => new Response(JSON.stringify({ data,
  ...(list ? { meta: { total: 60, page: 2, per_page: 30 } } : {}) }), { headers: { "Content-Type": "application/json" } });
function deferred() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
}
function navigate(query: string) { location.query = query; for (const notify of location.listeners) notify(); }
const writes = () => calls.filter(call => call.method !== "GET" && !call.path.endsWith("/compose") && !call.path.endsWith("/events"));
async function settle() { await act(async () => { for (let i = 0; i < 30; i++) await Promise.resolve(); }); }

beforeEach(() => {
  calls = []; intercept = undefined; location.query = initialQuery;
  installSession("synthetic-session", { id: "synthetic-user", tenant_id: "synthetic-tenant",
    email: "author@fixture.test", display_name: "Synthetic author", role: "user" });
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  location.replace.mockImplementation((url: string) => navigate(url.split("?")[1] ?? ""));
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname,
      method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/events")) {
      const body = new ReadableStream<Uint8Array>({ start(controller) {
        init?.signal?.addEventListener("abort", () => controller.error(new DOMException("Aborted", "AbortError")), { once: true });
      } });
      return new Response(body, { headers: { "Content-Type": "text/event-stream" } });
    }
    if (call.path === "/api/v1/company/mailboxes") return json(["mailbox-one", "mailbox-two"].map(id => ({
      mailbox: { id, tenant_id: "synthetic-tenant", kind: "personal", full_address: `${id}@fixture.test` },
      can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
    })));
    if (call.path === "/api/v1/company/drafts") return json([draft], true);
    if (call.path === "/api/v1/company/drafts/draft-one") return json(draft);
    if (call.path.endsWith("/compose")) return json(replyPayload);
    if (/\/messages\/[^/]+$/.test(call.path)) return json({ id: "message-one", subject: "Original synthetic message", sender: "original@fixture.test", recipients: ["mailbox-one@fixture.test"], text_body: "Original body", seen: true, starred: false });
    if (call.path.endsWith("/attachments")) return json([]);
    if (call.path.endsWith("/templates")) return json([]);
    if (call.path.endsWith("/submit")) return json({ id: "synthetic-job" });
    if (call.path.endsWith("/submissions") || call.path.endsWith("/sent") || call.path.endsWith("/messages")) return json([], true);
    if (call.path.endsWith("/index-status")) return json({ total: 0, indexed: 0, failed: 0 });
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
type Opening = "Continue editing" | "Reply" | "Reply all" | "Forward";
const openings: Opening[] = ["Continue editing", "Reply", "Reply all", "Forward"];
async function beginOpening(label: Opening) {
  location.query = label === "Continue editing" ? initialQuery : messageQuery;
  const pending = deferred();
  intercept = call => (label === "Continue editing" ? call.path === "/api/v1/company/drafts/draft-one" : call.path.endsWith("/compose")) ? pending.promise : undefined;
  const rendered = render(<MailWorkspace />);
  fireEvent.click(await screen.findByRole("button", { name: label }));
  await settle();
  expect(calls.filter(call => label === "Continue editing" ? call.path === "/api/v1/company/drafts/draft-one" : call.path.endsWith("/compose"))).toHaveLength(1);
  return { ...pending, ...rendered, reply: () => pending.resolve(json(label === "Continue editing" ? draft : replyPayload)) };
}

// Freeze this real-consumer proof before changing the editor-opening contract.
// Request/session/SWR and every mail component remain production code.
describe("editor opening completion ownership", () => {
  it.each(openings)("%s ordinarily opens its prepared editor", async label => {
    const pending = await beginOpening(label);
    await act(async () => pending.reply());
    expect(screen.getByLabelText("Subject")).toHaveValue(label === "Continue editing" ? draft.payload.subject : replyPayload.subject);
    expect(writes()).toHaveLength(0);
  });

  it.each(openings)("late %s cannot replace a newer dirty Compose", async label => {
    const pending = await beginOpening(label);
    vi.useFakeTimers(); // The newer input is deliberately inside the 2s autosave delay.
    fireEvent.click(screen.getByRole("button", { name: "Compose" }));
    const subject = screen.getByLabelText("Subject");
    fireEvent.change(subject, { target: { value: "Never discard this newer unsaved input" } });
    await act(async () => pending.reply());
    await settle();
    expect.soft(screen.getByLabelText("Subject")).toBe(subject);
    expect.soft(screen.getByLabelText("Subject")).toHaveValue("Never discard this newer unsaved input");
    expect(writes()).toHaveLength(0);
  });

  it.each(openings)("late %s cannot take over a newer committed view", async label => {
    const pending = await beginOpening(label);
    await act(async () => navigate(newerQuery));
    await act(async () => pending.reply());
    await settle();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(location.query).toBe(newerQuery);
    expect(writes()).toHaveLength(0);
  });

  it.each(openings)("%s does not regain ownership after navigating away and back", async label => {
    const pending = await beginOpening(label);
    const original = location.query;
    await act(async () => navigate(newerQuery));
    await act(async () => navigate(original));
    await act(async () => pending.reply());
    await settle();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(location.query).toBe(original);
  });

  it.each(openings)("unrelated query fields and refresh preserve ordinary %s", async label => {
    const pending = await beginOpening(label);
    const original = location.query;
    await act(async () => navigate(`${original}&keep=latest`));
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await settle();
    await act(async () => pending.reply());
    expect(screen.getByLabelText("Subject")).toHaveValue(label === "Continue editing" ? draft.payload.subject : replyPayload.subject);
    expect(location.query).toBe(`${original}&keep=latest`);
  });

  it.each(openings)("superseded %s failures do not disturb the newer editor", async label => {
    const pending = await beginOpening(label);
    fireEvent.click(screen.getByRole("button", { name: "Compose" }));
    const subject = screen.getByLabelText("Subject");
    await act(async () => pending.resolve(new Response(JSON.stringify({ error: { code: "NOT_FOUND", message: "Old synthetic source removed" } }), { status: 404, headers: { "Content-Type": "application/json" } })));
    await settle();
    expect(screen.getByLabelText("Subject")).toBe(subject);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(openings)("current %s failures remain visible", async label => {
    const pending = await beginOpening(label);
    await act(async () => pending.resolve(new Response(JSON.stringify({ error: { code: "NOT_FOUND", message: "Synthetic source removed" } }), { status: 404, headers: { "Content-Type": "application/json" } })));
    await settle();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(toast.error).toHaveBeenCalledOnce();
  });
});
