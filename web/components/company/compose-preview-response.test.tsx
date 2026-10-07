import React, { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { advanceSession } from "@/lib/session";
import type { MailDraft, WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// The actual editor, request parser, company client and ownership guards run.
// An error boundary records render crashes; only fetch and toast are synthetic.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const mailbox: WorkMailbox = {
  mailbox: {
    id: "sender-one", tenant_id: "tenant-one", kind: "shared", zone_id: "zone-one", local_part: "first",
    resolved_domain: "fixture.test", full_address: "first@fixture.test", access_mode: "public",
    retention_hours_override: null, expires_at: null, created_at: "2026-10-07T00:00:00Z",
  },
  can_read: true, can_send: true, can_organize: false, template_only: true, revision: 1,
};
const draft: MailDraft = {
  id: "draft-one", mailbox_id: mailbox.mailbox.id, revision: 4, updated_at: "2026-10-07T00:00:00Z",
  payload: {
    to: ["recipient@fixture.test"], subject: "", text_body: "", template_version_id: "version-one",
    template_vars: { customer: "Alice" },
  },
  template_version: {
    id: "version-one", status: "usable", name: "Welcome", version: 1,
    snapshot: {
      subject: "Hello {{customer}}", text_body: "Dear {{customer}}", html_body: "",
      variables: [{ name: "customer", type: "text", required: true, max_length: 80 }],
    },
  },
};
const valid = { subject: "Verified preview", text_body: "Verified text", html_body: "<p>Verified HTML</p>" };
const invalidMessage = "Invalid template preview response; try again.";
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
class EditorBoundary extends React.Component<{ children: React.ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <p role="alert">Editor crashed</p> : this.props.children; }
}
let pending: ReturnType<typeof deferred<Response>>;
let requests: string[];
beforeEach(() => {
  pending = deferred<Response>(); requests = [];
  localStorage.setItem("tabmail_access_token", "synthetic-preview-token");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    requests.push(`${init?.method ?? "GET"} ${path}`);
    if (path.endsWith("/templates/preview")) return pending.promise;
    if (path.endsWith("/templates")) return json([]);
    throw new Error(`Unexpected synthetic request: ${requests.at(-1)}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function mount() {
  const renderError = vi.fn(), onClose = vi.fn(), onSent = vi.fn();
  function Host() {
    const [allowed, setAllowed] = useState(true);
    return <>
      <button onClick={() => setAllowed(false)}>Revoke sender</button>
      <button onClick={() => setAllowed(true)}>Restore sender</button>
      <Compose initial={draft} mailboxes={[{ ...mailbox, can_send: allowed }]} onClose={onClose} onSent={onSent} />
    </>;
  }
  const view = render(<EditorBoundary><Host /></EditorBoundary>, { onCaughtError: renderError });
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  return { ...view, renderError, onClose, onSent };
}
function startPreview() { fireEvent.click(screen.getByRole("button", { name: "Server preview" })); }
async function finishPreview(data: unknown) { await act(async () => { pending.resolve(json(data)); }); }
const malformedFields: [string, unknown][] = ["subject", "text_body", "html_body"].flatMap(field => {
  const missing: Record<string, unknown> = { ...valid }; delete missing[field];
  return [
    [`object ${field}`, { ...valid, [field]: { unexpected: "object" } }],
    [`null ${field}`, { ...valid, [field]: null }],
    [`missing ${field}`, missing],
  ] as [string, unknown][];
});

describe("Compose preview response integrity", () => {
  it.each([
    ...malformedFields,
    ["numeric subject", { ...valid, subject: 42 }],
    ["null data", null],
    ["array data", []],
    ["missing data", undefined],
  ] as [string, unknown][])("rejects HTTP 200 with %s without crashing or discarding edits", async (_name, data) => {
    const { renderError, onClose, onSent } = await mount();
    fireEvent.change(screen.getByLabelText("customer * · text"), { target: { value: "Unsaved customer" } });
    startPreview();
    await finishPreview(data);
    expect(renderError).not.toHaveBeenCalled();
    expect(screen.queryByText("Editor crashed")).not.toBeInTheDocument();
    expect(screen.getByLabelText("customer * · text")).toHaveValue("Unsaved customer");
    expect(screen.getByRole("button", { name: "Server preview" })).toBeEnabled();
    expect(toast.error).toHaveBeenCalledExactlyOnceWith(invalidMessage);
    expect(toast.success).not.toHaveBeenCalled();
    expect(onClose).not.toHaveBeenCalled(); expect(onSent).not.toHaveBeenCalled();
    expect(requests.filter(request => !request.endsWith("/templates"))).toEqual(["POST /api/v1/company/templates/preview"]);
  });

  it("retains a valid preview after malformed refresh and permits a valid retry", async () => {
    const { renderError } = await mount();
    startPreview(); await finishPreview(valid);
    expect(screen.getByText(valid.subject)).toBeInTheDocument();
    pending = deferred<Response>();
    startPreview(); await finishPreview({ ...valid, subject: null });
    expect(renderError).not.toHaveBeenCalled();
    expect(screen.getByText(valid.subject)).toBeInTheDocument();
    expect(toast.error).toHaveBeenCalledExactlyOnceWith(invalidMessage);
    pending = deferred<Response>();
    startPreview(); await finishPreview({ ...valid, subject: "Retried preview" });
    expect(screen.getByText("Retried preview")).toBeInTheDocument();
    expect(screen.queryByText(valid.subject)).not.toBeInTheDocument();
  });

  it.each([valid, { subject: "", text_body: "", html_body: "" }])("accepts all-string previews, including empty fields", async data => {
    const { renderError } = await mount();
    startPreview(); await finishPreview(data);
    expect(renderError).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
    expect(screen.getByRole("button", { name: "Server preview" })).toBeEnabled();
    if (data.subject) expect(screen.getByText(data.subject)).toBeInTheDocument();
    if (data.html_body) expect(screen.getByTitle("HTML email preview")).toHaveAttribute("srcdoc", expect.stringContaining(data.html_body));
  });

  it.each(["revocation", "unmount", "session"])("discards malformed stale responses silently after %s", async action => {
    const { renderError, unmount } = await mount();
    startPreview();
    if (action === "revocation") {
      fireEvent.click(screen.getByRole("button", { name: "Revoke sender" }));
      fireEvent.click(screen.getByRole("button", { name: "Restore sender" }));
    } else if (action === "unmount") unmount();
    else await act(async () => { advanceSession(); });
    await finishPreview({ subject: { unexpected: "object" }, text_body: null, html_body: [] });
    expect(renderError).not.toHaveBeenCalled();
    expect(toast.error).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
    if (action !== "unmount") expect(screen.getByLabelText("customer * · text")).toHaveValue("Alice");
  });
});
