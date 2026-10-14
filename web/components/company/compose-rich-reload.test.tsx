import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { installSession } from "@/lib/session";
import type { DraftPayload, MailDraft, MailDraftInput, WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// Real Compose, DraftWriter, RichMessage, DOM Selection and the request layer.
// The HTTP peer and notifications are synthetic. Focus intentionally remains
// on the editable node across action dispatch; no browser/PG gate is claimed.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const draftPath = "/api/v1/company/drafts/rich-draft";
const mailbox: WorkMailbox = {
  mailbox: {
    id: "rich-mailbox", tenant_id: "company", kind: "shared", zone_id: "zone", local_part: "rich",
    resolved_domain: "fixture.test", full_address: "rich@fixture.test", access_mode: "token",
    retention_hours_override: null, expires_at: null, created_at: "2026-10-08T00:00:00Z",
  },
  can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
};
function draft(revision = 1, payload: Partial<DraftPayload> = {}): MailDraft {
  return { id: "rich-draft", mailbox_id: mailbox.mailbox.id, revision, updated_at: "2026-10-08T00:00:00Z",
    payload: { to: ["reader@fixture.test"], subject: "Original subject", text_body: "Original body",
      html_body: "<p><strong>Original body</strong></p>", ...payload } };
}
type Call = { path: string; method: string; body?: Record<string, unknown> };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (code = "CONFLICT", message = "Draft changed elsewhere", status = 409) => new Response(JSON.stringify({ error: { code, message } }), {
  status, headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
let calls: Call[], server: MailDraft, submissions: MailDraft[], writeFailure: boolean;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
beforeEach(() => {
  calls = []; server = draft(); submissions = []; writeFailure = false; intercept = undefined;
  installSession("synthetic-rich-token", { id: "writer", tenant_id: "company", role: "user", email: "writer@fixture.test", display_name: "Writer" });
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/templates") && call.method === "GET") return json([]);
    if (call.path === draftPath && call.method === "GET") return json(server);
    if ((call.path === draftPath && call.method === "PUT") || (call.path === "/api/v1/company/drafts" && call.method === "POST")) {
      if (writeFailure) return failure();
      const command = call.body as unknown as MailDraftInput;
      server = { ...command, id: command.id!, revision: command.revision + 1, updated_at: "2026-10-08T00:00:01Z" };
      return json(server);
    }
    if (call.path === `${draftPath}/submit` && call.method === "POST") {
      if (call.body?.expected_revision !== server.revision) return failure();
      submissions.push(structuredClone(server));
      return json({ id: "synthetic-rich-submission" });
    }
    throw new Error(`Unexpected rich reload request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { window.getSelection()?.removeAllRanges(); cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
const click = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
const reads = () => calls.filter(call => call.path === draftPath && call.method === "GET");
const writes = () => calls.filter(call => call.method === "PUT" || (call.path === "/api/v1/company/drafts" && call.method === "POST"));
async function mount() {
  const onSent = vi.fn();
  const view = render(<Compose initial={draft()} mailboxes={[mailbox]} onClose={vi.fn()} onSent={onSent} />);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  if (screen.queryByRole("button", { name: "Formatting editor" })) click("Formatting editor");
  const editor = screen.getByRole("textbox", { name: "Formatted message" });
  editor.focus();
  expect(document.activeElement).toBe(editor);
  expect(editor.querySelector("strong")).toHaveTextContent("Original body");
  return { ...view, editor, onSent };
}
function edit(editor: HTMLElement, html = "<p><em>Local body</em></p>") {
  editor.focus(); editor.innerHTML = html; fireEvent.input(editor);
  expect(document.activeElement).toBe(editor);
}
function caret(editor: HTMLElement, offset = 3) {
  const node = document.createTreeWalker(editor, NodeFilter.SHOW_TEXT).nextNode()!;
  window.getSelection()!.setBaseAndExtent(node, offset, node, offset);
  return { node, offset };
}
function expectCaret(before: ReturnType<typeof caret>) {
  const selected = window.getSelection()!;
  expect(selected.anchorNode).toBe(before.node); expect(selected.focusNode).toBe(before.node);
  expect(selected.anchorOffset).toBe(before.offset); expect(selected.focusOffset).toBe(before.offset);
}
async function conflict() {
  // A subject edit also forces the same-body reset control through a real PUT.
  fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Local subject" } });
  writeFailure = true; click("Save draft");
  await screen.findByText("Draft changed elsewhere");
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
}
async function reload() {
  click("Reload server draft");
  await waitFor(() => expect(screen.queryByRole("button", { name: "Reload server draft" })).not.toBeInTheDocument());
  await waitFor(() => expect(screen.getByRole("button", { name: "Save draft" })).toBeEnabled());
}
function expectRich(editor: HTMLElement, text: string, html?: string) {
  expect(screen.getByRole("textbox", { name: "Formatted message" })).toBe(editor);
  expect(screen.getByRole("button", { name: "Use plain text" })).toHaveAttribute("aria-pressed", "true");
  expect(editor.textContent).toBe(text);
  if (html !== undefined) expect(editor.innerHTML).toBe(html);
}

it("replaces focused rich DOM on confirmed reload and submits subsequent input against the loaded revision", async () => {
  const { editor, onSent } = await mount(); edit(editor); await conflict();
  expect(document.activeElement).toBe(editor);
  server = draft(7, { subject: "Server subject", text_body: "Server body", html_body: "<p><strong>Server body</strong></p>" });
  await reload();
  expectRich(editor, "Server body", "<p><strong>Server body</strong></p>");
  expect(screen.getByLabelText("Subject")).toHaveValue("Server subject");
  editor.querySelector("strong")!.appendChild(document.createTextNode(" plus edit")); fireEvent.input(editor);
  writeFailure = false; click("Send");
  await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
  expect(submissions).toHaveLength(1);
  expect(submissions[0].revision).toBe(8);
  expect(submissions[0].payload).toMatchObject({ text_body: "Server body plus edit", html_body: "<p><strong>Server body plus edit</strong></p>" });
  expect(reads()).toHaveLength(1);
});

it("clears focused rich content when the explicitly loaded server draft has an empty body", async () => {
  const { editor } = await mount(); edit(editor); await conflict();
  server = draft(3, { text_body: "", html_body: undefined }); await reload();
  expectRich(editor, "", "");
});

it("loads server plain text as literal text into the existing rich mode", async () => {
  const { editor } = await mount(); edit(editor); await conflict();
  server = draft(3, { text_body: "Server <script>literal & safe</script>", html_body: undefined }); await reload();
  expectRich(editor, "Server <script>literal & safe</script>");
  expect(editor.querySelector("script")).toBeNull();
});

it("sanitizes server HTML before replacing focused editable DOM", async () => {
  const { editor } = await mount(); edit(editor); await conflict();
  server = draft(3, { text_body: "Safe body", html_body: '<p onclick="bad()"><strong>Safe body</strong><img src="https://tracker.test/x"><script>bad()</script></p>' });
  await reload(); expectRich(editor, "Safe body", "<p><strong>Safe body</strong></p>");
  expect(editor.querySelector("script,img,[onclick]")).toBeNull();
});

it("discards native DOM edits even when the loaded body values equal the last controlled values", async () => {
  const { editor } = await mount(); await conflict();
  // Explicit discard owns even a native edit not yet delivered as an input
  // event (for example an in-progress composition). Both props stay equal.
  editor.innerHTML = "<p>Native edit awaiting input</p>";
  server = draft(4); await reload();
  expectRich(editor, "Original body", "<p><strong>Original body</strong></p>");
});

it("handles successive explicit reloads without remounting or leaving formatted mode", async () => {
  const { editor } = await mount();
  for (const revision of [3, 5]) {
    edit(editor, `<p><em>Local body ${revision}</em></p>`); await conflict();
    server = draft(revision, { text_body: `Server body ${revision}`, html_body: `<p>Server body ${revision}</p>` });
    await reload(); expectRich(editor, `Server body ${revision}`, `<p>Server body ${revision}</p>`);
  }
  expect(reads()).toHaveLength(2);
});

it("retains current DOM while a reload is pending, then applies its confirmed server body", async () => {
  const { editor } = await mount(); edit(editor); await conflict();
  const before = editor.firstChild, pending = deferred<Response>();
  intercept = call => call.path === draftPath && call.method === "GET" ? pending.promise : undefined;
  click("Reload server draft"); await waitFor(() => expect(reads()).toHaveLength(1));
  expect(editor.firstChild).toBe(before); expect(editor).toHaveAttribute("contenteditable", "false");
  expect(document.activeElement).toBe(editor);
  await act(async () => pending.resolve(json(draft(8, { text_body: "Confirmed later", html_body: "<p>Confirmed later</p>" }))));
  await waitFor(() => expect(screen.getByRole("button", { name: "Save draft" })).toBeEnabled());
  expectRich(editor, "Confirmed later", "<p>Confirmed later</p>");
});

it.each(["network", "503", "malformed JSON", "wrong draft", "invalid revision"])("a %s reload failure preserves current DOM, selection and unsaved input", async kind => {
  const { editor } = await mount(); edit(editor); const before = editor.firstChild, selection = caret(editor); await conflict();
  intercept = call => call.path !== draftPath || call.method !== "GET" ? undefined
    : kind === "network" ? Promise.reject(new TypeError("Synthetic offline"))
    : kind === "503" ? Promise.resolve(failure("UNAVAILABLE", "Reload unavailable", 503))
    : kind === "malformed JSON" ? Promise.resolve(new Response('{"data":', { headers: { "Content-Type": "application/json" } }))
    : Promise.resolve(json({ ...draft(4), ...(kind === "wrong draft" ? { id: "different-draft" } : { revision: 0 }) }));
  click("Reload server draft"); await waitFor(() => expect(reads()).toHaveLength(1));
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
  expectRich(editor, "Local body", "<p><em>Local body</em></p>");
  expect(editor.firstChild).toBe(before); expectCaret(selection);
  expect(screen.getByLabelText("Subject")).toHaveValue("Local subject");
  expect(screen.getByText("Draft changed elsewhere")).toBeInTheDocument();
  expect(submissions).toHaveLength(0); expect(writes()).toHaveLength(1);
});

it("cancelled reload confirmation keeps the existing rich node and never reads", async () => {
  const { editor } = await mount(); edit(editor); const before = editor.firstChild, selection = caret(editor); await conflict();
  vi.mocked(window.confirm).mockReturnValue(false); click("Reload server draft");
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
  expectRich(editor, "Local body", "<p><em>Local body</em></p>");
  expect(editor.firstChild).toBe(before); expectCaret(selection); expect(reads()).toHaveLength(0);
});

it("ordinary input and explicit save preserve the existing rich nodes and caret", async () => {
  const { editor } = await mount(); edit(editor); const before = editor.firstChild, selection = caret(editor);
  fireEvent.input(editor); expect(editor.firstChild).toBe(before); expectCaret(selection);
  click("Save draft"); await screen.findByText("Saved · v2");
  expectRich(editor, "Local body", "<p><em>Local body</em></p>");
  expect(editor.firstChild).toBe(before); expectCaret(selection); expect(reads()).toHaveLength(0);
  expect(writes()).toHaveLength(1);
});

it("an older autosave acknowledgement does not replace newer focused input or its caret", async () => {
  const { editor } = await mount(); const pending = deferred<Response>();
  intercept = call => call.method === "PUT" ? pending.promise : undefined;
  vi.useFakeTimers(); edit(editor);
  await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
  expect(writes()).toHaveLength(1);
  const node = editor.querySelector("em")!.firstChild!; node.textContent = "Newer typing"; fireEvent.input(editor);
  const selection = caret(editor), before = editor.firstChild;
  await act(async () => pending.resolve(json({ ...writes()[0].body, revision: 2, updated_at: "2026-10-08T00:00:01Z" })));
  expectRich(editor, "Newer typing", "<p><em>Newer typing</em></p>");
  expect(editor.firstChild).toBe(before); expectCaret(selection); expect(writes()).toHaveLength(1);
  vi.useRealTimers();
});

it("keeping edits as a new draft preserves rich DOM and does not act as server reload", async () => {
  const { editor } = await mount(); edit(editor); const before = editor.firstChild, selection = caret(editor); await conflict();
  click("Keep edits as a new draft");
  expectRich(editor, "Local body", "<p><em>Local body</em></p>");
  expect(editor.firstChild).toBe(before); expectCaret(selection); expect(reads()).toHaveLength(0);
  writeFailure = false; click("Save draft"); await screen.findByText("Saved · v1");
  expect(writes().at(-1)).toMatchObject({ method: "POST", body: { revision: 0, payload: { text_body: "Local body", html_body: "<p><em>Local body</em></p>" } } });
});
