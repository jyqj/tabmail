import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { installSession } from "@/lib/session";
import type { DraftPayload, MailDraft, MailDraftInput, MailDraftEditor, WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// Actual Compose, RichMessage, DraftWriter and HTTP request layer. No real
// mailbox, browser clipboard, delivery or PostgreSQL fixture is involved.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const mailbox: WorkMailbox = { mailbox: { id: "formatted-box", tenant_id: "formatted-company", kind: "shared",
  zone_id: "zone", local_part: "formatted", resolved_domain: "fixture.test", full_address: "formatted@fixture.test",
  access_mode: "token", retention_hours_override: null, expires_at: null, created_at: "2026-10-09T00:00:00Z" },
  revision: 1, can_read: true, can_send: true, can_organize: false, template_only: false };
function draft(payload: Partial<DraftPayload> = {}): MailDraft {
  return { id: "formatted-draft", mailbox_id: mailbox.mailbox.id, revision: 1, updated_at: "2026-10-09T00:00:00Z",
    payload: { to: ["recipient@fixture.test"], subject: "Saved formatted message", text_body: "Original bold body",
      html_body: "<p>Original <strong>bold</strong> body</p>", ...payload } };
}
type Call = { path: string; method: string; body?: Record<string, unknown> };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
let calls: Call[], server: MailDraft, rejectSave: boolean;
beforeEach(() => {
  calls = []; server = draft(); rejectSave = false;
  installSession("synthetic-formatted-token", { id: "writer", tenant_id: "formatted-company", role: "user",
    email: "writer@fixture.test", display_name: "Synthetic writer" });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    if (call.method === "GET" && call.path.endsWith("/templates")) return json([]);
    if (call.method === "GET" && call.path === "/api/v1/company/drafts/formatted-draft") return json(server);
    if (call.method === "PUT" || (call.method === "POST" && call.path === "/api/v1/company/drafts")) {
      if (rejectSave) return new Response(JSON.stringify({ error: { code: "CONFLICT", message: "Draft changed elsewhere" } }),
        { status: 409, headers: { "Content-Type": "application/json" } });
      const command = call.body as unknown as MailDraftInput;
      server = { ...command, id: command.id!, revision: command.revision + 1, updated_at: "2026-10-09T00:00:01Z" };
      return json(server);
    }
    throw new Error(`Unexpected formatted-draft request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { window.getSelection()?.removeAllRanges(); cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
const writes = () => calls.filter(call => call.method === "PUT" || (call.method === "POST" && call.path === "/api/v1/company/drafts"));
const rich = () => screen.getByRole("textbox", { name: "Formatted message" });
const click = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
async function mount(initial: MailDraftEditor = draft()) {
  if (initial.id) server = initial as MailDraft;
  render(<Compose initial={initial} mailboxes={[mailbox]} onClose={vi.fn()} onSent={vi.fn()} />);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
}
function changeBody(html = "<p>Updated <strong>bold</strong> body</p>") {
  const editor = screen.queryByRole("textbox", { name: "Formatted message" });
  if (!editor) {
    // Exercise the visible old plain editor too, so the baseline reaches the
    // actual save/auto-save and proves that its first edit drops stored HTML.
    const plain = screen.getByRole("textbox", { name: "Message" });
    fireEvent.change(plain, { target: { value: new DOMParser().parseFromString(html, "text/html").body.textContent } });
    return plain;
  }
  editor.focus(); editor.innerHTML = html; fireEvent.input(editor); return editor;
}
async function save() { click("Save draft"); await waitFor(() => expect(writes()).toHaveLength(1)); }

describe("opening formatted drafts without losing HTML", () => {
  it("opens a saved HTML draft in the formatted editor without generating an edit", async () => {
    await mount(); expect(rich().querySelector("strong")).toHaveTextContent("bold");
    expect(screen.getByRole("button", { name: "Use plain text" })).toHaveAttribute("aria-pressed", "true");
    expect(writes()).toHaveLength(0); expect(window.confirm).not.toHaveBeenCalled();
  });

  it("preserves formatting in the first edit and explicit draft save after reopening", async () => {
    await mount(); changeBody(); await save();
    expect(writes()[0].body?.payload).toMatchObject({ text_body: "Updated bold body", html_body: "<p>Updated <strong>bold</strong> body</p>" });
  });

  it("preserves formatting in the first autosave after reopening", async () => {
    await mount(); vi.useFakeTimers(); changeBody();
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(writes()).toHaveLength(1);
    expect(writes()[0].body?.payload).toMatchObject({ html_body: "<p>Updated <strong>bold</strong> body</p>" });
  });

  it("opens new reply/forward payloads with HTML in the formatted editor", async () => {
    const initial = draft(); await mount({ mailbox_id: initial.mailbox_id, revision: 0, payload: initial.payload });
    expect(rich().querySelector("strong")).toHaveTextContent("bold"); changeBody(); await save();
    expect(writes()[0].method).toBe("POST"); expect(writes()[0].body?.payload).toMatchObject({ html_body: "<p>Updated <strong>bold</strong> body</p>" });
  });

  it("requires confirmation before deliberately discarding saved formatting", async () => {
    await mount(); vi.mocked(window.confirm).mockReturnValue(false); click("Use plain text");
    expect(window.confirm).toHaveBeenCalledOnce(); expect(rich().querySelector("strong")).toHaveTextContent("bold");
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Subject edit only" } }); await save();
    expect(writes()[0].body?.payload).toMatchObject({ html_body: "<p>Original <strong>bold</strong> body</p>" });
  });

  it("allows an explicitly confirmed transition to plain text", async () => {
    await mount(); click("Use plain text"); expect(window.confirm).toHaveBeenCalledOnce();
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("Original bold body");
    expect(screen.queryByRole("textbox", { name: "Formatted message" })).not.toBeInTheDocument(); await save();
    expect(writes()[0].body?.payload).not.toHaveProperty("html_body");
  });

  it("sanitizes initial HTML before exposing an editable DOM", async () => {
    await mount(draft({ text_body: "Safe", html_body: '<p onclick="bad()"><strong>Safe</strong><script>bad()</script><img src="https://tracker.fixture.test/pixel"><a href="javascript:bad()">link</a></p>' }));
    expect(rich().querySelector("strong")).toHaveTextContent("Safe");
    expect(rich().querySelector("script,img,[onclick],[href]")).toBeNull();
    expect(calls.every(call => call.path.endsWith("/templates"))).toBe(true);
  });

  it.each([undefined, ""])("keeps a draft with %s HTML in plain text mode", async html => {
    await mount(draft({ text_body: "Plain first", html_body: html }));
    expect(screen.getByRole("textbox", { name: "Message" })).toHaveValue("Plain first");
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "Plain edit" } }); await save();
    expect(writes()[0].body?.payload).toMatchObject({ text_body: "Plain edit" });
    expect(writes()[0].body?.payload).not.toHaveProperty("html_body");
  });

  it("still allows a plain draft to opt into formatting", async () => {
    await mount(draft({ text_body: "Plain first", html_body: undefined })); click("Formatting editor");
    expect(rich()).toHaveTextContent("Plain first"); changeBody(); await save();
    expect(writes()[0].body?.payload).toMatchObject({ html_body: "<p>Updated <strong>bold</strong> body</p>" });
  });

  it("shows newly loaded HTML after a plain draft conflicts and is explicitly reloaded", async () => {
    await mount(draft({ text_body: "Initially plain", html_body: undefined }));
    fireEvent.change(screen.getByRole("textbox", { name: "Message" }), { target: { value: "Local plain edit" } });
    rejectSave = true; click("Save draft"); await screen.findByText("Draft changed elsewhere");
    server = { ...draft(), revision: 4 }; click("Reload server draft");
    await waitFor(() => expect(screen.queryByRole("button", { name: "Reload server draft" })).not.toBeInTheDocument());
    expect(rich().querySelector("strong")).toHaveTextContent("bold");
  });
});
