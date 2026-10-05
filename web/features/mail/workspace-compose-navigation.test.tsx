import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { MailDraft } from "@/lib/company";
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
const submits = () => calls.filter(call => call.path.endsWith("/submit"));
const writes = () => calls.filter(call => call.method !== "GET" && !call.path.endsWith("/submit"));
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
    if (call.path.endsWith("/templates")) return json([]);
    if (call.path.endsWith("/submit")) return json({ id: "synthetic-job" });
    if (call.path.endsWith("/submissions") || call.path.endsWith("/sent") || call.path.endsWith("/messages")) return json([], true);
    if (call.path.endsWith("/index-status")) return json({ total: 0, indexed: 0, failed: 0 });
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function mountEditor() {
  render(<MailWorkspace />);
  fireEvent.click(await screen.findByRole("button", { name: "Continue editing" }));
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  expect(screen.getByLabelText("Subject")).toHaveValue("Synthetic draft");
  await settle();
}
async function dispatch() {
  const pending = deferred();
  intercept = call => call.path.endsWith("/submit") ? pending.promise : undefined;
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(submits()).toHaveLength(1));
  expect(writes()).toHaveLength(0); // Already saved: no ambiguous deferred-save cancellation assertion.
  return pending;
}

describe("same-mounted Compose query navigation investigation", () => {
  it("retains the same editor and unsaved input across query navigation without sending", async () => {
    await mountEditor();
    const subject = screen.getByLabelText("Subject");
    fireEvent.change(subject, { target: { value: "Unsaved local input" } });
    await act(async () => navigate(newerQuery));
    expect(screen.getByLabelText("Subject")).toBe(subject);
    expect(subject).toHaveValue("Unsaved local input");
    expect(screen.getByLabelText("From mailbox")).toHaveValue("mailbox-one");
    expect(location.query).toBe(newerQuery);
    expect(submits()).toHaveLength(0);
    expect(location.replace).not.toHaveBeenCalled();
  });

  it("ordinary dispatched success closes the consumed editor and navigates to receipts", async () => {
    await mountEditor(); const pending = await dispatch();
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(submits()).toHaveLength(1);
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(new URLSearchParams(location.query).get("folder")).toBe("receipts");
    expect(location.replace).toHaveBeenCalledOnce();
    expect(toast.success).toHaveBeenCalledOnce();
  });

  it("already-dispatched success must not overwrite a newer mailbox/folder/message query", async () => {
    await mountEditor(); const pending = await dispatch();
    const subject = screen.getByLabelText("Subject");
    await act(async () => navigate(newerQuery));
    expect(screen.getByLabelText("Subject")).toBe(subject); // No unmount/key replacement.
    expect(location.query).toBe(newerQuery);
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(submits()).toHaveLength(1); // The dispatched request cannot be claimed canceled.
    expect.soft(location.query).toBe(newerQuery);
    expect.soft(location.replace).not.toHaveBeenCalled();
  });

  it("successful completion preserves unrelated query data added while the same view remains", async () => {
    await mountEditor(); const pending = await dispatch();
    await act(async () => navigate(`${initialQuery}&retained=new-value`));
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(submits()).toHaveLength(1);
    expect(new URLSearchParams(location.query).get("folder")).toBe("receipts");
    expect(new URLSearchParams(location.query).get("retained")).toBe("new-value");
  });

  it.each([
    ["mailbox", "mailbox-two"], ["folder", "sent"], ["q", "new-search"],
    ["page", "3"], ["message", "message-two"],
  ])("a committed %s change retires redirect ownership but reconciles the consumed editor", async (key, value) => {
    await mountEditor(); const pending = await dispatch();
    const query = new URLSearchParams(initialQuery); query.set(key, value);
    await act(async () => navigate(query.toString()));
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(location.query).toBe(query.toString());
    expect(location.replace).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
    expect(submits()).toHaveLength(1);
    expect(toast.success).toHaveBeenCalledOnce();
  });

  it("changing received/sent source in an archive view retires redirect ownership", async () => {
    await mountEditor();
    await act(async () => navigate("mailbox=mailbox-one&folder=archive"));
    const pending = await dispatch();
    const query = "mailbox=mailbox-one&folder=archive&source=sent";
    await act(async () => navigate(query));
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(location.query).toBe(query);
    expect(location.replace).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
  });

  it("navigation away and back cannot revive an old redirect", async () => {
    await mountEditor(); const pending = await dispatch();
    await act(async () => navigate(newerQuery));
    await act(async () => navigate(initialQuery));
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(location.query).toBe(initialQuery);
    expect(location.replace).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
  });

  it("query order alone preserves a legitimate completion", async () => {
    await mountEditor(); const pending = await dispatch();
    const query = new URLSearchParams(initialQuery); query.sort();
    await act(async () => navigate(query.toString()));
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(new URLSearchParams(location.query).get("folder")).toBe("receipts");
    expect(location.replace).toHaveBeenCalledOnce();
  });

  it("refresh while submitting preserves a legitimate completion", async () => {
    await mountEditor(); const pending = await dispatch();
    fireEvent.click(screen.getByRole("button", { name: "Refresh" }));
    await settle();
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    expect(new URLSearchParams(location.query).get("folder")).toBe("receipts");
    expect(location.replace).toHaveBeenCalledOnce();
  });

  it("an explicit Send after earlier query navigation owns the new view", async () => {
    await mountEditor();
    await act(async () => navigate(newerQuery));
    const pending = await dispatch();
    await act(async () => pending.resolve(json({ id: "synthetic-job" })));
    await settle();
    const query = new URLSearchParams(location.query);
    expect(query.get("folder")).toBe("receipts");
    expect(query.get("mailbox")).toBe("mailbox-two");
    expect(query.get("q")).toBe("new-search");
    expect(location.replace).toHaveBeenCalledOnce();
  });

  it("query navigation during save preserves the explicit send and only retires its redirect", async () => {
    await mountEditor();
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Updated synthetic draft" } });
    const pending = deferred();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(submits()).toHaveLength(0);
    await act(async () => navigate(newerQuery));
    expect(screen.getByLabelText("Subject")).toHaveValue("Updated synthetic draft");
    await act(async () => pending.resolve(json({ ...(writes()[0].body as MailDraft), revision: 2,
      updated_at: "2026-10-05T00:00:01Z" })));
    await settle();
    expect(submits()).toHaveLength(1);
    expect(submits()[0].body).toEqual({ expected_revision: 2 });
    expect(location.query).toBe(newerQuery);
    expect(location.replace).not.toHaveBeenCalled();
    expect(screen.queryByLabelText("Subject")).not.toBeInTheDocument();
  });
});
