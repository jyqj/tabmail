import React, { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { advanceSession } from "@/lib/session";
import { addresses, type DraftPayload, type MailDraft, type WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

// Real Compose, recipient inputs, DraftWriter, company transport, session guards
// and SWR run. Only fetch and toast delivery are substituted. No live mail/API.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const fields = ["to", "cc", "bcc"] as const;
type RecipientField = typeof fields[number];
type Call = { path: string; method: string; body?: Record<string, unknown>; headers: Headers; signal?: AbortSignal | null };
const labels = { to: "To (plain email addresses)", cc: "Cc", bcc: "Bcc" };
const draftPath = "/api/v1/company/drafts/draft-one";
const mailbox: WorkMailbox = {
  mailbox: { id: "mailbox-one", tenant_id: "tenant-one", kind: "shared", zone_id: "zone-one", local_part: "shared",
    resolved_domain: "fixture.test", full_address: "shared@fixture.test", access_mode: "public", retention_hours_override: null,
    expires_at: null, created_at: "2026-10-05T00:00:00Z" },
  can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
};
function draft(revision = 1, payload: Partial<DraftPayload> = {}): MailDraft {
  return { id: "draft-one", mailbox_id: "mailbox-one", revision, updated_at: "2026-10-05T00:00:00Z",
    payload: { to: ["original-to@fixture.test"], cc: ["original-cc@fixture.test"], bcc: ["original-bcc@fixture.test"],
      subject: "Original subject", text_body: "Original body", ...payload } };
}
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (code = "CONFLICT", message = "Draft changed elsewhere") => new Response(JSON.stringify({ error: { code, message } }), { status: code === "CONFLICT" ? 409 : 400, headers: { "Content-Type": "application/json" } });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
let calls: Call[], server: MailDraft, submissions: MailDraft[], writeFailure: boolean;
let intercept: ((call: Call) => Promise<Response> | undefined) | undefined;
beforeEach(() => {
  calls = []; server = draft(); submissions = []; writeFailure = false; intercept = undefined;
  vi.spyOn(window, "confirm").mockReturnValue(true);
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, headers: new Headers(init?.headers), signal: init?.signal };
    calls.push(call);
    const pending = intercept?.(call); if (pending) return pending;
    if (call.path.endsWith("/templates") && call.method === "GET") return json([]);
    if (call.path === `${draftPath}/submit` && call.method === "POST") {
      if (call.body?.expected_revision !== server.revision) return failure();
      submissions.push(structuredClone(server));
      return json({ id: "synthetic-submission" });
    }
    if (call.path === draftPath && call.method === "GET") return json(server);
    if ((call.path === draftPath && call.method === "PUT") || (call.path === "/api/v1/company/drafts" && call.method === "POST")) {
      if (writeFailure) return failure();
      const command = call.body as unknown as MailDraft;
      server = { ...command, revision: command.revision + 1, updated_at: "2026-10-05T00:00:01Z" };
      return json(server);
    }
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); });
const input = (field: RecipientField) => screen.getByRole("textbox", { name: labels[field] }) as HTMLTextAreaElement;
const edit = (field: RecipientField, value: string) => fireEvent.change(input(field), { target: { value } });
function assertRecipients(expected: Partial<Record<RecipientField, string>>) {
  for (const field of fields) expect.soft(input(field)).toHaveValue(expected[field] ?? "");
}
async function mount(initial = draft()) {
  const onSent = vi.fn();
  const result = render(<Compose initial={initial} mailboxes={[mailbox]} onClose={vi.fn()} onSent={onSent} />);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  return { ...result, onSent };
}
async function conflict() {
  writeFailure = true;
  fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
  await screen.findByText("Draft changed elsewhere");
  await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
}
async function reload() {
  fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
  await waitFor(() => expect(screen.queryByRole("button", { name: "Reload server draft" })).not.toBeInTheDocument());
  await waitFor(() => expect(screen.getByRole("button", { name: "Save draft" })).toBeEnabled());
}
async function send() {
  fireEvent.click(screen.getByRole("button", { name: "Send" }));
  await waitFor(() => expect(submissions).toHaveLength(1));
}
const draftReads = () => calls.filter(call => call.path === draftPath && call.method === "GET");
const writes = () => calls.filter(call => call.method === "PUT" || (call.method === "POST" && !call.path.endsWith("/submit")));

describe("Compose explicit recipient reload", () => {
  it.each(fields)("replaces visible %s and submits exactly the displayed server revision", async field => {
    const { onSent } = await mount();
    edit(field, `discarded-${field}@fixture.test`);
    await conflict();
    server = draft(7, { to: ["server-to@fixture.test"], cc: ["server-cc@fixture.test"], bcc: ["server-bcc@fixture.test"] });
    await reload();
    assertRecipients({ to: "server-to@fixture.test", cc: "server-cc@fixture.test", bcc: "server-bcc@fixture.test" });
    const displayed = Object.fromEntries(fields.map(name => [name, addresses(input(name).value)]));
    await send();
    expect.soft(submissions[0].payload).toMatchObject(displayed);
    expect(submissions[0]).toEqual(server);
    expect(calls.find(call => call.path.endsWith("/submit"))).toMatchObject({ body: { expected_revision: 7 } });
    expect(calls.find(call => call.path.endsWith("/submit"))!.headers.get("Idempotency-Key")).toBe("draft-one.7");
    expect(writes()).toHaveLength(1);
    expect(onSent).toHaveBeenCalledOnce();
  });

  it.each(["omitted", "empty"])("clears discarded Cc/Bcc when the server fields are %s", async kind => {
    await mount();
    edit("to", "local@fixture.test"); edit("cc", "discarded-cc@fixture.test"); edit("bcc", "discarded-bcc@fixture.test");
    await conflict();
    server = draft(8, { cc: kind === "omitted" ? undefined : [], bcc: kind === "omitted" ? undefined : [] });
    await reload();
    assertRecipients({ to: "original-to@fixture.test", cc: "", bcc: "" });
    await send();
    expect(submissions[0].payload.cc ?? []).toEqual([]);
    expect(submissions[0].payload.bcc ?? []).toEqual([]);
  });

  it("discards raw separators even when the server recipients normalize to the same payload", async () => {
    await mount();
    edit("to", "original-to@fixture.test;  \n");
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "Local subject" } });
    await conflict();
    server = draft(2);
    await reload();
    expect(input("to")).toHaveValue("original-to@fixture.test");
    expect(screen.getByLabelText("Subject")).toHaveValue("Original subject");
  });

  it("supports repeated explicit reloads without retaining earlier recipient text", async () => {
    await mount();
    for (const revision of [2, 3]) {
      edit("to", `local-${revision}@fixture.test`);
      await conflict();
      server = draft(revision, { to: [`server-${revision}@fixture.test`] });
      await reload();
      expect.soft(input("to")).toHaveValue(`server-${revision}@fixture.test`);
    }
    expect(draftReads()).toHaveLength(2);
  });

  it("canceling discard preserves local text and never reads the server draft", async () => {
    await mount();
    const raw = "local@fixture.test; unfinished@";
    edit("to", raw); await conflict();
    vi.mocked(window.confirm).mockReturnValue(false);
    fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
    expect(window.confirm).toHaveBeenCalledWith("Replace this window's edits with the server version?");
    expect(input("to")).toHaveValue(raw);
    expect(draftReads()).toHaveLength(0);
  });

  it.each(["network failure", "wrong identity", "invalid revision"])("a %s during reload preserves local raw input and the conflict", async reason => {
    await mount();
    const raw = "local@fixture.test; unfinished@";
    edit("to", raw); await conflict();
    intercept = call => call.path === draftPath && call.method === "GET"
      ? reason === "network failure" ? Promise.reject(new TypeError("Synthetic offline")) : Promise.resolve(json({ ...draft(4), ...(reason === "wrong identity" ? { id: "different-draft" } : { revision: 0 }) }))
      : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
    await waitFor(() => expect(draftReads()).toHaveLength(1));
    await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
    expect(input("to")).toHaveValue(raw);
    expect(screen.getByText("Draft changed elsewhere")).toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(screen.getByRole("button", { name: "Send" })).toBeEnabled());
    expect(submissions).toHaveLength(0);
    expect(writes()).toHaveLength(1);
  });

  it("keeps partial text through unrelated changes, autosave, and an older save acknowledgement", async () => {
    await mount();
    const pending = deferred<Response>();
    intercept = call => call.method === "PUT" ? pending.promise : undefined;
    vi.useFakeTimers();
    edit("to", "first@fixture.test; partial@");
    const before = input("to");
    await act(async () => { await vi.advanceTimersByTimeAsync(2000); });
    expect(writes()).toHaveLength(1);
    const newer = "first@fixture.test; partial@fixture.test; \n";
    edit("to", newer);
    fireEvent.change(screen.getByLabelText("Subject"), { target: { value: "New subject" } });
    await act(async () => {
      pending.resolve(json({ ...writes()[0].body, revision: 2, updated_at: "2026-10-05T00:00:01Z" }));
    });
    expect(input("to")).toBe(before);
    expect(input("to")).toHaveValue(newer);
    expect(screen.getByLabelText("Subject")).toHaveValue("New subject");
    vi.useRealTimers();
  });

  it("keeps raw formatting in all fields through explicit saves and a new-copy recovery", async () => {
    await mount();
    const values = { to: "to@fixture.test;  \n", cc: "cc@fixture.test; unfinished@", bcc: "bcc@fixture.test, " };
    for (const field of fields) edit(field, values[field]);
    await conflict();
    writeFailure = false;
    fireEvent.click(screen.getByRole("button", { name: "Keep edits as a new draft" }));
    fireEvent.click(screen.getByRole("button", { name: "Save draft" }));
    await screen.findByText("Saved · v1");
    assertRecipients(values);
    expect(writes().at(-1)).toMatchObject({ method: "POST", path: "/api/v1/company/drafts", body: { revision: 0, payload: { to: ["to@fixture.test"], cc: ["cc@fixture.test", "unfinished@"], bcc: ["bcc@fixture.test"] } } });
  });

  it("keeps invalid text on validation failure and clears it only after confirmed authoritative reload", async () => {
    await mount();
    edit("to", "not-an-address");
    intercept = call => call.method === "PUT" ? Promise.resolve(failure("BAD_REQUEST", "Invalid recipient address")) : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await screen.findByText("Invalid recipient address");
    expect(input("to")).toHaveValue("not-an-address");
    expect(submissions).toHaveLength(0);
    await waitFor(() => expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled());
    await reload();
    assertRecipients({ to: "original-to@fixture.test", cc: "original-cc@fixture.test", bcc: "original-bcc@fixture.test" });
    await send();
    expect(submissions[0].payload.to).toEqual(["original-to@fixture.test"]);
  });

  it("locks recipients during reload and ignores repeated clicks while the read is pending", async () => {
    await mount(); edit("to", "local@fixture.test"); await conflict();
    const pending = deferred<Response>();
    intercept = call => call.path === draftPath && call.method === "GET" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
    await waitFor(() => expect(draftReads()).toHaveLength(1));
    for (const field of fields) expect(input(field)).toBeDisabled();
    const button = screen.getByRole("button", { name: "Reload server draft" });
    expect(button).toBeDisabled(); fireEvent.click(button);
    expect(draftReads()).toHaveLength(1);
    server = draft(4, { to: ["server@fixture.test"] });
    await act(async () => { pending.resolve(json(server)); });
    expect(input("to")).toBeEnabled();
    expect(input("to")).toHaveValue("server@fixture.test");
  });

  it("does not adopt a delayed reload after the session changes", async () => {
    await mount(); edit("to", "local@fixture.test;"); await conflict();
    const pending = deferred<Response>();
    intercept = call => call.path === draftPath && call.method === "GET" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
    await waitFor(() => expect(draftReads()).toHaveLength(1));
    await act(async () => { advanceSession(); pending.resolve(json(draft(8, { to: ["stale@fixture.test"] }))); });
    expect(draftReads()[0].signal?.aborted).toBe(true);
    expect(input("to")).toHaveValue("local@fixture.test;");
    expect(screen.getByRole("button", { name: "Reload server draft" })).toBeEnabled();
    expect(submissions).toHaveLength(0);
  });

  it("keeps a replacement keyed draft independent from the old editor's delayed reload", async () => {
    const replacement = { ...draft(3, { to: ["replacement@fixture.test"], cc: [], bcc: [] }), id: "draft-two" };
    function Host() {
      const [current, setCurrent] = useState(draft());
      return <><button onClick={() => setCurrent(replacement)}>Switch draft</button><Compose key={current.id} initial={current} mailboxes={[mailbox]} onClose={vi.fn()} onSent={vi.fn()} /></>;
    }
    render(<Host />);
    await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
    edit("to", "local@fixture.test"); await conflict();
    const pending = deferred<Response>();
    intercept = call => call.path === draftPath && call.method === "GET" ? pending.promise : undefined;
    fireEvent.click(screen.getByRole("button", { name: "Reload server draft" }));
    await waitFor(() => expect(draftReads()).toHaveLength(1));
    fireEvent.click(screen.getByRole("button", { name: "Switch draft" }));
    assertRecipients({ to: "replacement@fixture.test" });
    await act(async () => { pending.resolve(json(draft(10, { to: ["old-response@fixture.test"] }))); });
    assertRecipients({ to: "replacement@fixture.test" });
    expect(screen.getByText("Saved · v3")).toBeInTheDocument();
    expect(submissions).toHaveLength(0);
  });
});
