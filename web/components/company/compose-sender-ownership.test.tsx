import React, { StrictMode, Suspense, startTransition, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { advanceSession } from "@/lib/session";
import type { MailAttachment, MailDraft, WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// Exercise the mounted editor, native select/file controls, DraftWriter, SWR,
// company client and session guards. Only fetch is synthetic; no live mail.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

type Call = {
  path: string;
  method: string;
  body?: Record<string, unknown>;
  form?: FormData;
  headers: Headers;
};
const mailbox: WorkMailbox = {
  mailbox: {
    id: "sender-one", tenant_id: "tenant-one", kind: "shared", zone_id: "zone-one", local_part: "first",
    resolved_domain: "fixture.test", full_address: "first@fixture.test", access_mode: "public",
    retention_hours_override: null, expires_at: null, created_at: "2026-10-07T00:00:00Z",
  },
  can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
};
const alternative: WorkMailbox = {
  ...mailbox,
  mailbox: { ...mailbox.mailbox, id: "sender-two", local_part: "second", full_address: "second@fixture.test" },
};
const draft: MailDraft = {
  id: "draft-one", mailbox_id: mailbox.mailbox.id, revision: 4, updated_at: "2026-10-07T00:00:00Z",
  payload: {
    to: ["recipient@fixture.test"], subject: "Original subject", text_body: "Original body",
    attachment_ids: ["existing-attachment"],
  },
};
const pinnedDraft: MailDraft = {
  ...draft,
  payload: { ...draft.payload, template_version_id: "version-one", template_vars: { customer: "Alice" } },
  template_version: {
    id: "version-one", status: "usable", name: "Welcome", version: 1,
    snapshot: {
      subject: "Hello {{customer}}", text_body: "Dear {{customer}}", html_body: "",
      variables: [{ name: "customer", type: "text", required: true, max_length: 80 }],
    },
  },
};
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = () => new Response(JSON.stringify({ error: { code: "FORBIDDEN", message: "Synthetic upload denied" } }), {
  status: 403, headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}
function attachment(id = "new-attachment", sender = mailbox.mailbox.id): MailAttachment {
  return { id, mailbox_id: sender, filename: `${id}.txt`, size: 4, content_type: "text/plain", state: "ready" };
}
const uploads = () => calls.filter(call => call.method === "POST" && call.path.endsWith("/attachments"));
const previews = () => calls.filter(call => call.path.endsWith("/templates/preview"));
const writes = () => calls.filter(call => call.method === "PUT");
const fileInput = () => screen.getByLabelText("Attachments (10 files, 20 MiB total)");
const senderInput = () => screen.getByLabelText("From mailbox") as HTMLSelectElement;
const quiet = () => { expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled(); };
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
let user: ReturnType<typeof userEvent.setup>;

beforeEach(() => {
  calls = []; intercept = undefined; user = userEvent.setup();
  localStorage.setItem("tabmail_access_token", "synthetic-first-token");
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = {
      path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined,
      form: init?.body instanceof FormData ? init.body : undefined, headers: new Headers(init?.headers),
    };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/templates") && call.method === "GET") return json([]);
    if (call.path.endsWith("/attachments") && call.method === "POST") return json(attachment());
    if (call.path.endsWith("/templates/preview")) return json({ subject: "Fresh preview", text_body: "Dear Alice", html_body: "" });
    if (call.method === "PUT") return json({ ...call.body, revision: Number(call.body!.revision) + 1, updated_at: "2026-10-07T00:00:01Z" });
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });

async function mount(initial = draft, boxes = [mailbox, alternative], strict = false) {
  const onSent = vi.fn(), onClose = vi.fn();
  function Host() {
    const [current, setCurrent] = useState(boxes);
    return <>
      <button onClick={() => setCurrent(boxes.map(box => box.mailbox.id === mailbox.mailbox.id ? { ...box, can_send: false } : box))}>Revoke sender</button>
      <button onClick={() => setCurrent(boxes.filter(box => box.mailbox.id !== mailbox.mailbox.id))}>Remove sender</button>
      <button onClick={() => setCurrent(boxes)}>Restore sender</button>
      <Compose initial={initial} mailboxes={current} onClose={onClose} onSent={onSent} />
    </>;
  }
  const view = render(strict ? <StrictMode><Host /></StrictMode> : <Host />);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  return { ...view, onSent, onClose };
}
async function upload(name = "chosen.txt") {
  await user.upload(fileInput(), new File(["test"], name, { type: "text/plain" }));
}
async function finishUpload(pending: ReturnType<typeof deferred<Response>>, id = "late-attachment") {
  await act(async () => { pending.resolve(json(attachment(id))); });
}

describe("Compose current sender selection", () => {
  it.each(["Revoke sender", "Remove sender"])("does not display another From identity after %s and preserves edits through an explicit switch", async action => {
    await mount();
    const rawTo = "new-recipient@fixture.test, ";
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Unsaved subject" } });
    fireEvent.change(screen.getByLabelText("Message"), { target: { value: "Unsaved body" } });
    fireEvent.change(screen.getByLabelText("To (plain email addresses)"), { target: { value: rawTo } });
    await user.click(screen.getByRole("button", { name: action }));

    // Native <select> must still show the unavailable selected sender. Omitting
    // its option makes the DOM silently display sender-two while state stays one.
    expect(senderInput()).toHaveValue(mailbox.mailbox.id);
    expect(senderInput().selectedOptions[0]).toBeDisabled();
    expect(screen.getByRole("button", { name: "Save draft" })).toBeDisabled();
    expect(screen.getByRole("button", { name: "Send" })).toBeDisabled();
    expect(fileInput()).toBeDisabled();
    expect(writes()).toHaveLength(0);

    await user.selectOptions(senderInput(), alternative.mailbox.id);
    await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
    expect(senderInput()).toHaveValue(alternative.mailbox.id);
    expect(screen.getByLabelText("Subject")).toHaveValue("Unsaved subject");
    expect(screen.getByLabelText("Message")).toHaveValue("Unsaved body");
    expect(screen.getByLabelText("To (plain email addresses)")).toHaveValue(rawTo);
    expect(screen.queryByRole("button", { name: "Remove" })).not.toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toMatchObject({
      mailbox_id: alternative.mailbox.id,
      payload: { to: ["new-recipient@fixture.test"], subject: "Unsaved subject", text_body: "Unsaved body", attachment_ids: [] },
    });
    expect(uploads()).toHaveLength(0);
  });

  it("keeps the unavailable From explicit when no sendable mailbox remains", async () => {
    await mount(draft, [mailbox]);
    await user.click(screen.getByRole("button", { name: "Remove sender" }));
    expect(senderInput()).toHaveValue(mailbox.mailbox.id);
    expect(senderInput().selectedOptions[0]).toBeDisabled();
    expect(fileInput()).toBeDisabled();
    expect(screen.getByLabelText("Subject")).toBeEnabled();
    expect(screen.getByLabelText("Message")).toBeEnabled();
    expect(screen.getByLabelText("Subject")).toHaveValue(draft.payload.subject);
  });

  it("does not impersonate an alternative sender when opening a now-unsendable saved draft", async () => {
    await mount(draft, [{ ...mailbox, can_send: false }, alternative]);
    expect(senderInput()).toHaveValue(mailbox.mailbox.id);
    expect(senderInput().selectedOptions[0]).toBeDisabled();
    expect(fileInput()).toBeDisabled();
    await upload();
    expect(uploads()).toHaveLength(0);
  });

  it("disables server preview after current sender authorization is revoked", async () => {
    await mount(pinnedDraft);
    await user.click(screen.getByRole("button", { name: "Revoke sender" }));
    expect(screen.getByRole("button", { name: "Server preview" })).toBeDisabled();
    expect(fileInput()).toBeDisabled();
    expect(screen.getByLabelText("customer * · text")).toBeEnabled();
    await user.click(screen.getByRole("button", { name: "Server preview" }));
    await upload();
    expect(previews()).toHaveLength(0); expect(uploads()).toHaveLength(0);
  });
});

describe("Compose attachment operation ownership", () => {
  it.each(["Revoke sender", "Remove sender"])("discards a late successful upload after %s", async action => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    expect(uploads()).toHaveLength(1);
    expect(uploads()[0].path).toBe("/api/v1/company/mailboxes/sender-one/attachments");
    expect((uploads()[0].form?.get("file") as File).name).toBe("chosen.txt");
    await user.click(screen.getByRole("button", { name: action }));
    await finishUpload(pending);
    expect(screen.queryByText("late-attachment.txt")).not.toBeInTheDocument();
    expect(screen.getAllByRole("button", { name: "Remove" })).toHaveLength(1);
    expect(fileInput()).toBeDisabled();
    expect(writes()).toHaveLength(0); quiet();
  });

  it("does not revive a pending upload when sender permission is restored and allows a fresh choice", async () => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    await user.click(screen.getByRole("button", { name: "Revoke sender" }));
    await user.click(screen.getByRole("button", { name: "Restore sender" }));
    await finishUpload(pending);
    expect(screen.queryByText("late-attachment.txt")).not.toBeInTheDocument();
    expect(fileInput()).toBeEnabled();
    intercept = undefined;
    await upload("fresh-choice.txt");
    await screen.findByText("new-attachment.txt");
    await user.click(screen.getByRole("button", { name: "Save draft" }));
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].body).toMatchObject({
      mailbox_id: mailbox.mailbox.id, payload: { attachment_ids: ["existing-attachment", "new-attachment"] },
    });
    expect(uploads()).toHaveLength(2);
  });

  it.each(["denied", "network"])("does not report a stale %s upload failure after sender revocation", async result => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    await user.click(screen.getByRole("button", { name: "Revoke sender" }));
    await act(async () => {
      if (result === "denied") pending.resolve(failure());
      else pending.reject(new TypeError("Synthetic offline"));
    });
    quiet();
  });

  it.each(["success", "denied", "network"])("isolates a late %s upload response after the editor unmounts", async result => {
    const { unmount, onSent, onClose } = await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload(); unmount();
    await act(async () => {
      if (result === "success") pending.resolve(json(attachment()));
      else if (result === "denied") pending.resolve(failure());
      else pending.reject(new TypeError("Synthetic offline"));
    });
    expect(onSent).not.toHaveBeenCalled(); expect(onClose).not.toHaveBeenCalled();
    expect(writes()).toHaveLength(0); quiet();
  });

  it("still reports a failed upload while its original sender remains authorized", async () => {
    await mount();
    intercept = call => call.path.endsWith("/attachments") ? Promise.resolve(failure()) : undefined;
    await upload();
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic upload denied"));
    expect(fileInput()).toBeEnabled();
    expect(screen.getAllByRole("button", { name: "Remove" })).toHaveLength(1);
  });

  it("keeps normal uploads working after StrictMode effect cleanup/setup replay", async () => {
    await mount(draft, [mailbox], true);
    await upload();
    await screen.findByText("new-attachment.txt");
    expect(uploads()).toHaveLength(1); quiet();
  });

  it("allows a same-identity token rotation during upload", async () => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    localStorage.setItem("tabmail_access_token", "synthetic-rotated-token");
    await finishUpload(pending);
    expect(screen.getByText("late-attachment.txt")).toBeInTheDocument();
    expect(uploads()).toHaveLength(1); quiet();
  });

  it("discards a successful upload from a previous account session", async () => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    await act(async () => { advanceSession(); });
    await finishUpload(pending);
    expect(screen.queryByText("late-attachment.txt")).not.toBeInTheDocument();
    expect(writes()).toHaveLength(0); quiet();
  });

  it("does not revoke a valid upload because of an uncommitted suspended denied render", async () => {
    const never = new Promise<void>(() => {});
    function Suspender({ suspended }: { suspended: boolean }) { if (suspended) throw never; return null; }
    function Host() {
      const [allowed, setAllowed] = useState(true), [suspended, setSuspended] = useState(false);
      return <><button onClick={() => startTransition(() => { setAllowed(false); setSuspended(true); })}>Speculative revoke</button>
        <Suspense fallback={<span>Loading replacement</span>}>
          <Compose initial={draft} mailboxes={[{ ...mailbox, can_send: allowed }]} onClose={vi.fn()} onSent={vi.fn()} />
          <Suspender suspended={suspended} />
        </Suspense></>;
    }
    render(<Host />);
    await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/attachments") ? pending.promise : undefined;
    await upload();
    await user.click(screen.getByRole("button", { name: "Speculative revoke" }));
    expect(screen.queryByText("Loading replacement")).not.toBeInTheDocument();
    await finishUpload(pending);
    expect(screen.getByText("late-attachment.txt")).toBeInTheDocument(); quiet();
  });
});

describe("Compose preview operation ownership", () => {
  it("does not revive a pending preview after sender revocation/restoration", async () => {
    await mount(pinnedDraft);
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/templates/preview") ? pending.promise : undefined;
    await user.click(screen.getByRole("button", { name: "Server preview" }));
    expect(previews()).toHaveLength(1);
    await user.click(screen.getByRole("button", { name: "Revoke sender" }));
    await user.click(screen.getByRole("button", { name: "Restore sender" }));
    await act(async () => { pending.resolve(json({ subject: "Old preview", text_body: "Old rendered body", html_body: "" })); });
    expect(screen.queryByText("Old preview")).not.toBeInTheDocument(); quiet();
    intercept = undefined;
    await user.click(screen.getByRole("button", { name: "Server preview" }));
    await screen.findByText("Fresh preview");
    expect(previews()).toHaveLength(2);
  });

  it("does not report a late preview error after unmount", async () => {
    const { unmount } = await mount(pinnedDraft);
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/templates/preview") ? pending.promise : undefined;
    await user.click(screen.getByRole("button", { name: "Server preview" }));
    unmount();
    await act(async () => { pending.reject(new TypeError("Synthetic preview offline")); });
    quiet();
  });

  it("continues to show an authorized preview", async () => {
    await mount(pinnedDraft);
    await user.click(screen.getByRole("button", { name: "Server preview" }));
    await screen.findByText("Fresh preview");
    expect(previews()[0].body).toEqual({ mailbox_id: mailbox.mailbox.id, template_version_id: "version-one", vars: { customer: "Alice" } });
    quiet();
  });
});
