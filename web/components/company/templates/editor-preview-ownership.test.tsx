import React, { useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { advanceSession, AUTH_EVENT, installSession } from "@/lib/session";
import type { MailTemplate, MailTemplateEditor, WorkMailbox } from "@/lib/company";
import { TemplateEditorView } from "./editor";

// The actual mounted editor, company client, response parser and session guards
// run. Only the HTTP peer and notifications are synthetic; no mail is sent.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const template: MailTemplate = {
  id: "template-one", name: "Preview template", revision: 7, retired: false, updated_at: "2026-10-07T00:00:00Z",
  draft: { subject: "Hello {{.customer}}", text_body: "Dear {{.customer}}", html_body: "<p>{{.customer}}</p>",
    variables: [{ name: "customer", type: "text", required: true, max_length: 80 }] },
};
const mailbox: WorkMailbox = {
  mailbox: { id: "mailbox-one", tenant_id: "company", kind: "shared", zone_id: "zone-one", local_part: "first",
    full_address: "first@fixture.test", resolved_domain: "fixture.test", access_mode: "token",
    retention_hours_override: null, expires_at: null, created_at: "2026-10-07T00:00:00Z" },
  // Management preview is not send authorization; CanManage is server-only.
  can_read: false, can_send: false, can_organize: false, template_only: false, revision: 1,
};
const second: WorkMailbox = { ...mailbox, mailbox: { ...mailbox.mailbox, id: "mailbox-two", full_address: "second@fixture.test" } };
const valid = { subject: "Verified preview", text_body: "Verified body", html_body: "<p>Verified HTML</p>" };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const invalidMessage = "Invalid template preview response; try again.";
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: unknown) => void;
  const promise = new Promise<T>((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
class EditorBoundary extends React.Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <p role="alert">Editor crashed</p> : this.props.children; }
}
let pending: ReturnType<typeof deferred<Response>>;
let calls: { path: string; method: string; body: unknown }[];
beforeEach(() => {
  pending = deferred<Response>(); calls = [];
  installSession("synthetic-preview-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    calls.push({ path, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined });
    if (path === "/api/v1/company/templates/preview") return pending.promise;
    throw new Error(`Unexpected preview request: ${init?.method ?? "GET"} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

function mount() {
  const onSaved = vi.fn(async () => {}), onPublished = vi.fn(async () => template), renderError = vi.fn();
  function Host() {
    const [editor, setEditor] = useState<{ key: number; value: MailTemplateEditor | null }>({ key: 0, value: structuredClone(template) });
    const [boxes, setBoxes] = useState([mailbox, second]);
    const [selected, setSelected] = useState(mailbox.mailbox.id);
    return <>
      <button onClick={() => setBoxes([second])}>Remove selected mailbox</button>
      <button onClick={() => setBoxes([mailbox, second])}>Restore mailboxes</button>
      <button onClick={() => setBoxes([{ ...mailbox, mailbox: { ...mailbox.mailbox, full_address: "changed@fixture.test" } }, second])}>Change selected address</button>
      <button onClick={() => setBoxes([{ ...mailbox, revision: 9, can_read: true, can_send: true, can_organize: true }, second])}>Change nonmanagement capabilities</button>
      <button onClick={() => setBoxes([mailbox, { ...second, mailbox: { ...second.mailbox, full_address: "unrelated@fixture.test" } }])}>Change unrelated mailbox</button>
      <button onClick={() => { setBoxes(structuredClone(boxes)); setEditor(value => ({ ...value, value: structuredClone(value.value) })); }}>Repeat equal props</button>
      <button onClick={() => setEditor(value => ({ ...value, value: value.value ? { ...value.value, draft: { ...value.value.draft, subject: "Refreshed draft subject" } } : null }))}>Refresh draft from server</button>
      <button onClick={() => setEditor(value => ({ key: value.key + 1, value: structuredClone(template) }))}>Reselect template</button>
      <button onClick={() => setEditor(value => ({ key: value.key + 1, value: { ...structuredClone(template), id: "template-two", name: "New template selection" } }))}>Select another template</button>
      <TemplateEditorView key={editor.key} edit={editor.value}
        setEdit={update => setEditor(current => current.key === editor.key ? { ...current, value: update(current.value) } : current)}
        mailboxes={boxes} mailbox={selected} setMailbox={setSelected} onSaved={onSaved} onPublished={onPublished} />
    </>;
  }
  const view = render(<EditorBoundary><Host /></EditorBoundary>, { onCaughtError: renderError });
  change("Preview value: customer", "Alice");
  return { ...view, onSaved, onPublished, renderError };
}
const change = (label: string, value: string) => fireEvent.change(screen.getByLabelText(label), { target: { value } });
const start = () => fireEvent.click(screen.getByRole("button", { name: "Validate and preview on server" }));
const control = (name: string) => fireEvent.click(screen.getByRole("button", { name }));
async function finish(...values: [] | [unknown]) {
  await act(async () => pending.resolve(json(values.length === 0 ? valid : values[0])));
}
const quiet = () => { expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); };

it("previews the exact draft, mailbox and values without requiring send capability or invoking save/publish", async () => {
  const view = mount(); start(); await finish();
  expect(calls).toEqual([{ method: "POST", path: "/api/v1/company/templates/preview", body: {
    mailbox_id: mailbox.mailbox.id, draft: template.draft, vars: { customer: "Alice" },
  } }]);
  expect(screen.getByText(valid.subject)).toBeInTheDocument();
  expect(screen.getByTitle("HTML email preview")).toHaveAttribute("sandbox", "");
  expect(view.onSaved).not.toHaveBeenCalled(); expect(view.onPublished).not.toHaveBeenCalled(); quiet();
});

it.each([
  ["Subject template", "New subject"], ["Text body template", "New body"], ["HTML template", "<p>New HTML</p>"],
  ["Variable name", "contact"], ["Allowed values (optional, one per line)", "New choice"],
  ["Preview value: customer", "Bob"], ["Preview / grant mailbox", second.mailbox.id],
])("discards a pending preview after %s changes and keeps the edited value", async (label, value) => {
  mount(); start(); change(label, value); await finish();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(screen.getByLabelText(label)).toHaveValue(value); quiet();
});

it.each([
  ["Subject template", "Other subject", template.draft.subject],
  ["Preview value: customer", "Bob", "Alice"],
  ["Preview / grant mailbox", second.mailbox.id, mailbox.mailbox.id],
])("does not revive a pending preview when %s changes away and back", async (label, away, back) => {
  mount(); start(); change(label, away); change(label, back); await finish();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(screen.getByLabelText(label)).toHaveValue(back); quiet();
});

it("suppresses a superseded request failure while keeping current failures visible", async () => {
  mount(); start(); change("Text body template", "Newer input");
  await act(async () => pending.reject(new Error("Obsolete preview failure"))); quiet();
  pending = deferred<Response>(); start();
  await act(async () => pending.reject(new Error("Current preview failure")));
  expect(toast.error).toHaveBeenCalledExactlyOnceWith("Current preview failure");
  expect(screen.getByLabelText("Text body template")).toHaveValue("Newer input");
});

it.each(["pending", "complete"] as const)("invalidates a %s preview when the selected draft is refreshed externally", async state => {
  mount(); start(); if (state === "complete") await finish();
  control("Refresh draft from server");
  if (state === "pending") await finish();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(screen.getByLabelText("Subject template")).toHaveValue("Refreshed draft subject"); quiet();
});

it.each(["pending", "complete"] as const)("retires a %s preview when its selected mailbox is removed, including after restoration", async state => {
  mount(); start(); if (state === "complete") await finish();
  control("Remove selected mailbox");
  const select = screen.getByLabelText("Preview / grant mailbox") as HTMLSelectElement;
  expect(select).toHaveValue(mailbox.mailbox.id);
  expect(select.selectedOptions[0]).toBeDisabled();
  expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeDisabled();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  control("Restore mailboxes");
  if (state === "pending") await finish();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(select).toHaveValue(mailbox.mailbox.id);
  expect(calls).toHaveLength(1);
  pending = deferred<Response>(); start(); await finish({ ...valid, subject: "Fresh rechecked preview" });
  expect(screen.getByText("Fresh rechecked preview")).toBeInTheDocument(); quiet();
});

it("invalidates a preview when the selected mailbox's sender address changes", async () => {
  mount(); start(); control("Change selected address"); await finish();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect((screen.getByLabelText("Preview / grant mailbox") as HTMLSelectElement).selectedOptions[0]).toHaveTextContent("changed@fixture.test"); quiet();
});

it.each(["Reselect template", "Select another template"])("cannot report an old request failure after %s", async action => {
  mount(); start(); control(action);
  change("Preview value: customer", "New selection values");
  await act(async () => pending.reject(new Error("Old template failure")));
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("New selection values"); quiet();
});

it("has no response validation or toast after unmount", async () => {
  const view = mount(); start(); view.unmount();
  await act(async () => pending.reject(new Error("Unmounted failure")));
  expect(view.renderError).not.toHaveBeenCalled(); quiet();
});

it("hides a completed preview when the session changes and disables its old editor", async () => {
  mount(); start(); await finish();
  act(() => advanceSession());
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeDisabled();
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("Alice"); quiet();
});

it("ignores an old-session malformed response", async () => {
  const view = mount(); start(); act(() => advanceSession());
  await finish({ subject: { invalid: true }, text_body: null, html_body: [] });
  expect(view.renderError).not.toHaveBeenCalled(); quiet();
});

it.each(["name", "token", "Change nonmanagement capabilities", "Change unrelated mailbox", "Repeat equal props"])("keeps a valid preview through the unrelated change: %s", async changeKind => {
  mount(); start();
  if (changeKind === "name") change("Template name", "Renamed only");
  else if (changeKind === "token") act(() => { localStorage.setItem("tabmail_access_token", "rotated-preview-token"); window.dispatchEvent(new Event(AUTH_EVENT)); });
  else control(changeKind);
  await finish();
  expect(screen.getByText(valid.subject)).toBeInTheDocument();
  expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeEnabled(); quiet();
});

const malformed: [string, unknown][] = ["subject", "text_body", "html_body"].flatMap(field => {
  const missing: Record<string, unknown> = { ...valid }; delete missing[field];
  return [[`missing ${field}`, missing], [`null ${field}`, { ...valid, [field]: null }],
    [`object ${field}`, { ...valid, [field]: { invalid: true } }]] as [string, unknown][];
});
it.each([
  ...malformed, ["numeric subject", { ...valid, subject: 42 }], ["array body", { ...valid, text_body: [] }],
  ["boolean HTML", { ...valid, html_body: false }], ["null data", null], ["array data", []], ["missing data", undefined],
] as [string, unknown][])("rejects %s before rendering, preserves edits and allows retry", async (_name, value) => {
  const view = mount(); change("Text body template", "Unsaved draft text"); start(); await finish(value);
  expect(view.renderError).not.toHaveBeenCalled(); expect(screen.queryByText("Editor crashed")).not.toBeInTheDocument();
  expect(screen.getByLabelText("Text body template")).toHaveValue("Unsaved draft text");
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("Alice");
  expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeEnabled();
  expect(toast.error).toHaveBeenCalledExactlyOnceWith(invalidMessage);
  expect(view.onSaved).not.toHaveBeenCalled(); expect(view.onPublished).not.toHaveBeenCalled();
  expect(calls).toHaveLength(1);
});

it("keeps the prior valid preview when a same-input refresh is malformed, then accepts a good retry", async () => {
  const view = mount(); start(); await finish();
  pending = deferred<Response>(); start(); await finish({ ...valid, subject: null });
  expect(screen.getByText(valid.subject)).toBeInTheDocument();
  expect(toast.error).toHaveBeenCalledExactlyOnceWith(invalidMessage);
  pending = deferred<Response>(); start(); await finish({ ...valid, subject: "Good retry" });
  expect(screen.getByText("Good retry")).toBeInTheDocument();
  expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  expect(view.renderError).not.toHaveBeenCalled();
});

it("silently discards a malformed response once its preview input is obsolete", async () => {
  const view = mount(); start(); change("Preview value: customer", "New value");
  await finish({ subject: { invalid: true }, text_body: null, html_body: [] });
  expect(view.renderError).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Preview value: customer")).toHaveValue("New value"); quiet();
});

it.each([valid, { subject: "", text_body: "", html_body: "" }])("accepts a complete string response including empty fields", async value => {
  const view = mount(); start(); await finish(value);
  expect(view.renderError).not.toHaveBeenCalled(); quiet();
  expect(screen.getByRole("button", { name: "Validate and preview on server" })).toBeEnabled();
  if (value.subject) expect(screen.getByText(value.subject)).toBeInTheDocument();
  if (value.html_body) expect(screen.getByTitle("HTML email preview")).toHaveAttribute("srcdoc", expect.stringContaining(value.html_body));
});
