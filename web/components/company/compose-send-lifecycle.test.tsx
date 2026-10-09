import React, { StrictMode, Suspense, startTransition, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { advanceSession } from "@/lib/session";
import type { MailDraft, MailDraftEditor, WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// Mounted Compose, DraftWriter, session guards, company requests and SWR are
// real. Every request is handled by synthetic fetch; no live backend or mail.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
type Call = { path: string; method: string; body?: Record<string, unknown>; headers: Headers; signal?: AbortSignal | null };
const draftsPath = "/api/v1/company/drafts";
const mailbox: WorkMailbox = {
  mailbox: { id: "mailbox-one", tenant_id: "tenant-one", kind: "shared", zone_id: "zone-one", local_part: "shared",
    resolved_domain: "fixture.test", full_address: "shared@fixture.test", access_mode: "public", retention_hours_override: null,
    expires_at: null, created_at: "2026-10-05T00:00:00Z" },
  can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
};
function draft(id = "draft-one", revision = 1): MailDraft {
  return { id, mailbox_id: "mailbox-one", revision, updated_at: "2026-10-05T00:00:00Z",
    payload: { to: ["recipient@fixture.test"], subject: id, text_body: "Synthetic body" } };
}
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (code: string, status: number) => new Response(JSON.stringify({ error: { code, message: `Synthetic ${code}` } }), { status, headers: { "Content-Type": "application/json" } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (reason: unknown) => void;
  const promise = new Promise<T>((done, fail) => { resolve = done; reject = fail; });
  return { promise, resolve, reject };
}
let calls: Call[];
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
const writes = () => calls.filter(call => call.method === "PUT" || (call.path === draftsPath && call.method === "POST"));
const submits = () => calls.filter(call => call.path.endsWith("/submit"));
function acknowledgement(call: Call) {
  return json({ ...call.body, revision: Number(call.body!.revision) + 1, updated_at: "2026-10-05T00:00:01Z" });
}
beforeEach(() => {
  calls = []; intercept = undefined;
  localStorage.setItem("tabmail_access_token", "synthetic-original-token");
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/templates") && call.method === "GET") return json([]);
    if (call.path.endsWith("/submit") && call.method === "POST") return json({ id: "synthetic-job" });
    if (call.method === "PUT" || (call.path === draftsPath && call.method === "POST")) return acknowledgement(call);
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
async function mount(initial: MailDraftEditor = draft(), strict = false) {
  const onSent = vi.fn(), onClose = vi.fn();
  const editor = <Compose initial={initial} mailboxes={[mailbox]} onSent={onSent} onClose={onClose} />;
  const result = render(strict ? <StrictMode>{editor}</StrictMode> : editor);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  return { ...result, onSent, onClose };
}
const edit = () => fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Unsaved synthetic subject" } });
const send = () => fireEvent.click(screen.getByRole("button", { name: /^(Send|Retry same submission)$/ }));
async function ready() { await waitFor(() => expect(screen.getByRole("button", { name: /^(Send|Retry same submission)$/ })).toBeEnabled()); }
function expectSilent() {
  expect(toast.success).not.toHaveBeenCalled();
  expect(toast.error).not.toHaveBeenCalled();
}
async function settleSave(pending: ReturnType<typeof deferred<Response>>) {
  await act(async () => { pending.resolve(acknowledgement(writes()[0])); });
}

describe("Compose send lifecycle ownership", () => {
  it.each(["saved", "new"])("does not submit a %s draft when navigation unmounts it during save", async kind => {
    const initial: MailDraftEditor = kind === "saved" ? draft() : { mailbox_id: mailbox.mailbox.id, payload: draft().payload, revision: 0 };
    const { unmount, onSent } = await mount(initial);
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" || call.path === draftsPath ? pending.promise : undefined;
    edit(); send();
    await waitFor(() => expect(writes()).toHaveLength(1));
    expect(writes()[0].method).toBe(kind === "saved" ? "PUT" : "POST");
    unmount(); await settleSave(pending);
    expect(submits()).toHaveLength(0);
    expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("does not submit an already-saved draft when it unmounts before the flush resolves", async () => {
    const { unmount, onSent } = await mount();
    send(); unmount();
    await act(async () => { await Promise.resolve(); });
    expect(writes()).toHaveLength(0); expect(submits()).toHaveLength(0);
    expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it.each(["draft-one", "draft-two"])("cannot revive the old send after a new editor mounts for %s", async id => {
    const onSent = vi.fn();
    function Host() {
      const [generation, setGeneration] = useState(0);
      return <><button onClick={() => setGeneration(1)}>Navigate</button><Compose key={generation}
        initial={generation ? draft(id, 8) : draft()} mailboxes={[mailbox]} onClose={vi.fn()} onSent={onSent} /></>;
    }
    render(<Host />); await ready();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    edit(); send(); await waitFor(() => expect(writes()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Navigate" }));
    await settleSave(pending);
    expect(submits()).toHaveLength(0); expect(onSent).not.toHaveBeenCalled(); expectSilent();
    expect(screen.getByLabelText("Subject")).toHaveValue(id);
    expect(screen.getByText("Saved · v8")).toBeInTheDocument();
    intercept = undefined; send();
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(1);
    expect(submits()[0]).toMatchObject({ path: `${draftsPath}/${id}/submit`, body: { expected_revision: 8 } });
  });

  it("does not close a replacement editor when an already-dispatched submit succeeds", async () => {
    const onSent = vi.fn();
    function Host() {
      const [current, setCurrent] = useState<MailDraft | null>(draft());
      return <><button onClick={() => setCurrent(draft("draft-two", 8))}>Navigate</button>{current && <Compose key={current.id}
        initial={current} mailboxes={[mailbox]} onClose={() => setCurrent(null)} onSent={() => { onSent(); setCurrent(null); }} />}</>;
    }
    render(<Host />); await ready();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/submit") ? pending.promise : undefined;
    send(); await waitFor(() => expect(submits()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Navigate" }));
    await act(async () => { pending.resolve(json({ id: "synthetic-already-dispatched-job" })); });
    // The request already crossed the send boundary. Only its stale UI owner
    // is canceled; this assertion makes no claim that the job was rolled back.
    expect(submits()).toHaveLength(1); expect(onSent).not.toHaveBeenCalled(); expectSilent();
    expect(screen.getByLabelText("Subject")).toHaveValue("draft-two");
  });

  it.each(["conflict", "forbidden", "network"])("suppresses a stale %s submit result after unmount", async kind => {
    const { unmount, onSent } = await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/submit") ? pending.promise : undefined;
    send(); await waitFor(() => expect(submits()).toHaveLength(1)); unmount();
    await act(async () => {
      if (kind === "network") pending.reject(new TypeError("Synthetic offline"));
      else pending.resolve(failure(kind === "conflict" ? "CONFLICT" : "FORBIDDEN", kind === "conflict" ? 409 : 403));
    });
    expect(submits()).toHaveLength(1); expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("keeps direct sends and repeated-click locking working", async () => {
    const { onSent } = await mount(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    const button = screen.getByRole("button", { name: "Send" });
    fireEvent.click(button); fireEvent.click(button); await waitFor(() => expect(writes()).toHaveLength(1));
    await settleSave(pending);
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(1);
    expect(submits()[0].body).toEqual({ expected_revision: 2 });
    expect(submits()[0].headers.get("Idempotency-Key")).toBe("draft-one.2");
    expect(toast.success).toHaveBeenCalledOnce(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("keeps the writer usable after StrictMode effect cleanup/setup replay", async () => {
    const { onSent } = await mount(draft(), true); edit(); send();
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(writes()).toHaveLength(1); expect(submits()).toHaveLength(1);
    expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(["can_send", "template_only"])("does not submit after %s eligibility changes while saving", async kind => {
    const onSent = vi.fn();
    function Host() {
      const [current, setCurrent] = useState(mailbox);
      return <><button onClick={() => setCurrent({ ...mailbox, [kind]: kind === "template_only" })}>Revoke permission</button>
        <Compose initial={draft()} mailboxes={[current]} onClose={vi.fn()} onSent={onSent} /></>;
    }
    render(<Host />); await ready(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Revoke permission" }));
    await settleSave(pending);
    expect(submits()).toHaveLength(0); expect(onSent).not.toHaveBeenCalled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  // Preserve the independent review's committed-versus-speculative controls.
  it("permission restoration requires a fresh click instead of reviving the old send", async () => {
    const onSent = vi.fn();
    function Host() {
      const [allowed, setAllowed] = useState(true);
      return <><button onClick={() => setAllowed(false)}>Revoke</button><button onClick={() => setAllowed(true)}>Restore</button>
        <Compose initial={draft()} mailboxes={[{ ...mailbox, can_send: allowed }]} onClose={vi.fn()} onSent={onSent} /></>;
    }
    render(<Host />); await ready(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Revoke" }));
    fireEvent.click(screen.getByRole("button", { name: "Restore" }));
    await settleSave(pending);
    expect(submits()).toHaveLength(0); expect(onSent).not.toHaveBeenCalled(); expectSilent();
    intercept = undefined; send(); await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(writes()).toHaveLength(1); expect(submits()).toHaveLength(1);
    expect(submits()[0].headers.get("Idempotency-Key")).toBe("draft-one.2");
  });

  it("a speculative denied render cannot revoke the committed authorized send", async () => {
    const onSent = vi.fn(), never = new Promise<void>(() => {});
    function Suspender({ suspended }: { suspended: boolean }) { if (suspended) throw never; return null; }
    function Host() {
      const [allowed, setAllowed] = useState(true), [suspended, setSuspended] = useState(false);
      return <><button onClick={() => startTransition(() => { setAllowed(false); setSuspended(true); })}>Speculative revoke</button>
        <Suspense fallback={<span>Loading replacement</span>}>
          <Compose initial={draft()} mailboxes={[{ ...mailbox, can_send: allowed }]} onClose={vi.fn()} onSent={onSent} />
          <Suspender suspended={suspended} />
        </Suspense></>;
    }
    render(<Host />); await ready(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Speculative revoke" }));
    expect(screen.queryByText("Loading replacement")).not.toBeInTheDocument();
    await settleSave(pending);
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(1); expect(toast.error).not.toHaveBeenCalled();
  });

  it.each(["CONFLICT", "FORBIDDEN"])("does not surface a stale %s save failure after unmount", async code => {
    const { unmount, onSent } = await mount(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1)); unmount();
    await act(async () => { pending.resolve(failure(code, code === "CONFLICT" ? 409 : 403)); });
    expect(submits()).toHaveLength(0); expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("preserves same-identity token rotation while a save is pending", async () => {
    const { onSent } = await mount(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1));
    localStorage.setItem("tabmail_access_token", "synthetic-rotated-token");
    await settleSave(pending);
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(1);
    expect(submits()[0].headers.get("Authorization")).toBe("Bearer synthetic-rotated-token");
  });

  it("does not submit under a changed session after save", async () => {
    const { onSent } = await mount(); edit();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    send(); await waitFor(() => expect(writes()).toHaveLength(1));
    await act(async () => { advanceSession(); }); await settleSave(pending);
    expect(writes()[0].signal?.aborted).toBe(true); expect(submits()).toHaveLength(0);
    expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("does not report a dispatched submit after a session change", async () => {
    const { onSent } = await mount();
    const pending = deferred<Response>();
    intercept = call => call.path.endsWith("/submit") ? pending.promise : undefined;
    send(); await waitFor(() => expect(submits()).toHaveLength(1));
    await act(async () => { advanceSession(); pending.resolve(json({ id: "synthetic-job" })); });
    expect(submits()[0].signal?.aborted).toBe(true); expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("does not retry an old pending send under a new session", async () => {
    const { onSent } = await mount();
    intercept = call => call.path.endsWith("/submit") ? Promise.reject(new TypeError("Synthetic offline")) : undefined;
    send(); await waitFor(() => expect(toast.error).toHaveBeenCalledOnce()); await ready();
    expect(submits()).toHaveLength(1);
    await act(async () => { advanceSession(); });
    vi.mocked(toast.error).mockClear(); intercept = undefined; send(); await ready();
    expect(submits()).toHaveLength(1); expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("keeps uncertain retries pinned to their original revision and key", async () => {
    const { onSent } = await mount();
    intercept = call => call.path.endsWith("/submit") ? Promise.reject(new TypeError("Synthetic offline")) : undefined;
    send(); await waitFor(() => expect(toast.error).toHaveBeenCalledOnce()); await ready();
    intercept = undefined; send(); await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(2); expect(writes()).toHaveLength(0);
    expect(submits()[1].body).toEqual(submits()[0].body);
    expect(submits()[1].headers.get("Idempotency-Key")).toBe(submits()[0].headers.get("Idempotency-Key"));
  });

  it("explicit Close during autosave never turns its acknowledgement into a send", async () => {
    const { onClose, onSent, unmount } = await mount();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    vi.useFakeTimers(); edit();
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(writes()).toHaveLength(1);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).toHaveBeenCalledOnce(); unmount(); await settleSave(pending);
    expect(submits()).toHaveLength(0); expect(onSent).not.toHaveBeenCalled(); expectSilent();
  });

  it("canceling Close leaves the editor eligible for an explicit send", async () => {
    const { onClose, onSent } = await mount(); edit();
    vi.mocked(window.confirm).mockReturnValue(false);
    fireEvent.click(screen.getByRole("button", { name: "Close" }));
    expect(onClose).not.toHaveBeenCalled(); send();
    await waitFor(() => expect(onSent).toHaveBeenCalledOnce());
    expect(submits()).toHaveLength(1);
  });
});
