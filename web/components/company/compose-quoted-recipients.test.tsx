import React from "react";
import { cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { addresses, type DraftPayload, type MailDraft, type WorkMailbox } from "@/lib/company";
import { Compose } from "./compose";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));

describe("recipient separators preserve quoted addresses and comments", () => {
  it.each([
    { name: "ordinary separators and deduplication", raw: " a@example.test, b@example.test;\nc@example.test,a@example.test ", want: ["a@example.test", "b@example.test", "c@example.test"] },
    { name: "quoted comma", raw: '"ops,west"@example.test, other@example.test', want: ['"ops,west"@example.test', "other@example.test"] },
    { name: "quoted semicolon", raw: '"ops;west"@example.test;other@example.test', want: ['"ops;west"@example.test', "other@example.test"] },
    { name: "escaped quote", raw: String.raw`"ops\";west"@example.test,other@example.test`, want: [String.raw`"ops\";west"@example.test`, "other@example.test"] },
    { name: "escaped backslash", raw: String.raw`"ops\\;west"@example.test;other@example.test`, want: [String.raw`"ops\\;west"@example.test`, "other@example.test"] },
    { name: "quoted display name", raw: '"Doe, Jane" <jane@example.test>,other@example.test', want: ['"Doe, Jane" <jane@example.test>', "other@example.test"] },
    { name: "comment separators", raw: "ops@example.test (West, support; desk),other@example.test", want: ["ops@example.test (West, support; desk)", "other@example.test"] },
    { name: "nested comments", raw: "ops@example.test (West (shift; one), support),other@example.test", want: ["ops@example.test (West (shift; one), support)", "other@example.test"] },
    { name: "escaped comment close", raw: String.raw`ops@example.test (West\), support),other@example.test`, want: [String.raw`ops@example.test (West\), support)`, "other@example.test"] },
    { name: "quotes inside comment", raw: 'ops@example.test (West "desk, one"),other@example.test', want: ['ops@example.test (West "desk, one")', "other@example.test"] },
    { name: "parentheses inside quotes", raw: '"ops(west);desk"@example.test,other@example.test', want: ['"ops(west);desk"@example.test', "other@example.test"] },
    { name: "unclosed quote remains invalid", raw: '"unfinished@example.test,other@example.test', want: ['"unfinished@example.test,other@example.test'] },
    { name: "unclosed comment remains invalid", raw: "ops@example.test (unfinished,other@example.test", want: ["ops@example.test (unfinished,other@example.test"] },
    { name: "newline inside quote remains invalid", raw: '"ops\nwest"@example.test,other@example.test', want: ['"ops\nwest"@example.test', "other@example.test"] },
    { name: "nonaddress is not silently removed", raw: "not-an-address,other@example.test", want: ["not-an-address", "other@example.test"] },
    { name: "empty separators", raw: " ,;\n\t; ", want: [] },
    { name: "complete quoted values deduplicate", raw: '"ops,west"@example.test;"ops,west"@example.test', want: ['"ops,west"@example.test'] },
  ])("$name", ({ raw, want }) => {
    expect(addresses(raw)).toEqual(want);
  });
});

type RecipientField = "to" | "cc" | "bcc";
type Call = { path: string; method: string; body?: Record<string, unknown> };
const labels: Record<RecipientField, string> = { to: "To (plain email addresses)", cc: "Cc", bcc: "Bcc" };
const path = "/api/v1/company/drafts/quoted-reply";
const mailbox: WorkMailbox = {
  mailbox: { id: "reply-mailbox", tenant_id: "reply-tenant", kind: "shared", zone_id: "reply-zone", local_part: "reply",
    resolved_domain: "fixture.test", full_address: "reply@fixture.test", access_mode: "token", retention_hours_override: null,
    expires_at: null, created_at: "2026-10-08T00:00:00Z" },
  can_read: true, can_send: true, can_organize: false, template_only: false, revision: 1,
};
function initialDraft(payload: Partial<DraftPayload> = {}): MailDraft {
  return { id: "quoted-reply", mailbox_id: mailbox.mailbox.id, revision: 1, updated_at: "2026-10-08T00:00:00Z",
    payload: { to: ['"ops,west"@fixture.test'], cc: [], bcc: [], subject: "Re: Synthetic reply", text_body: "Synthetic quoted reply", ...payload } };
}
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
let calls: Call[], saved: MailDraft, submitted: MailDraft[], rejectSubmission: boolean;
beforeEach(() => {
  calls = []; saved = initialDraft(); submitted = []; rejectSubmission = false;
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET",
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    if (call.path.endsWith("/templates") && call.method === "GET") return json([]);
    if (call.path === path && call.method === "PUT") {
      const command = call.body as unknown as MailDraft;
      saved = { ...command, revision: command.revision + 1, updated_at: "2026-10-08T00:00:01Z" };
      return json(saved);
    }
    if (call.path === `${path}/submit` && call.method === "POST") {
      if (rejectSubmission) return new Response(JSON.stringify({ error: { code: "BAD_REQUEST", message: "Invalid recipient address" } }), { status: 400, headers: { "Content-Type": "application/json" } });
      expect(call.body?.expected_revision).toBe(saved.revision);
      submitted.push(structuredClone(saved));
      return json({ id: "synthetic-quoted-submission" });
    }
    throw new Error(`Unexpected quoted-reply request: ${call.method} ${call.path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

async function mount(initial: MailDraft) {
  saved = initial;
  const onSent = vi.fn();
  render(<Compose initial={initial} mailboxes={[mailbox]} onClose={vi.fn()} onSent={onSent} />);
  await waitFor(() => expect(screen.getByLabelText("Published template")).toBeEnabled());
  return onSent;
}

describe("editing a quoted reply through Compose", () => {
  it.each(["to", "cc", "bcc"] as const)("preserves complete quoted %s addresses through save and submission", async field => {
    const initial = initialDraft({ [field]: ['"ops,west"@fixture.test'] });
    const onSent = await mount(initial);
    const input = screen.getByRole("textbox", { name: labels[field] });
    expect(input).toHaveValue('"ops,west"@fixture.test');
    const raw = '"ops,west"@fixture.test; "ops;night"@fixture.test, other@fixture.test';
    fireEvent.change(input, { target: { value: raw } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(submitted).toHaveLength(1));
    const expected = ['"ops,west"@fixture.test', '"ops;night"@fixture.test', "other@fixture.test"];
    expect(calls.find(call => call.method === "PUT")?.body).toMatchObject({ payload: { [field]: expected } });
    expect(submitted[0].payload[field]).toEqual(expected);
    expect(onSent).toHaveBeenCalledOnce();
    expect(input).toHaveValue(raw);
  });

  it.each(['"unfinished@fixture.test,other@fixture.test', "ops@fixture.test (unfinished,other@fixture.test"])("retains malformed input and the server rejection for %s", async raw => {
    const onSent = await mount(initialDraft());
    rejectSubmission = true;
    const input = screen.getByRole("textbox", { name: labels.to });
    fireEvent.change(input, { target: { value: raw } });
    fireEvent.click(screen.getByRole("button", { name: "Send" }));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Invalid recipient address"));
    expect(calls.find(call => call.method === "PUT")?.body).toMatchObject({ payload: { to: [raw] } });
    expect(submitted).toHaveLength(0);
    expect(onSent).not.toHaveBeenCalled();
    expect(input).toHaveValue(raw);
  });
});
