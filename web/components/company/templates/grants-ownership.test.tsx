import React, { useLayoutEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { MailTemplate, WorkMailbox } from "@/lib/company";
import type { AdminUser, Mailbox } from "@/lib/types";
import { TemplateGrantsView } from "./grants";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const template: MailTemplate = {
  id: "welcome", name: "Welcome", revision: 4, retired: false,
  updated_at: "2026-10-08T00:00:00Z",
  draft: { subject: "Welcome", text_body: "Hello", html_body: "", variables: [] },
};
const otherTemplate = { ...template, id: "reminder", name: "Reminder" };
const member = { id: "member-one", email: "member@fixture.test", display_name: "Member", is_active: true } as AdminUser;
const otherMember = { ...member, id: "member-two", email: "second@fixture.test", display_name: "Second" };
const mailbox: WorkMailbox = {
  mailbox: { id: "support", full_address: "support@fixture.test", kind: "shared" } as Mailbox,
  revision: 3, can_read: false, can_organize: false, can_send: false, template_only: false,
};
const grant = { user_id: member.id, mailbox_id: mailbox.mailbox.id };
const grantButton = () => screen.getByRole("button", { name: "Grant usage on the selected mailbox" });
const revokeButton = () => screen.getByRole("button", { name: "Revoke usage" });
type Call = { path: string; method: string; body: Record<string, unknown> };
let calls: Call[];
let readGrants: (call: Call) => Promise<Response>;
let writeGrant: (call: Call) => Promise<Response>;
let pending: Array<(response: Response) => void>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failed = () => new Response(JSON.stringify({ error: { code: "INTERNAL", message: "Synthetic grant failure" } }), { status: 503, headers: { "Content-Type": "application/json" } });
const writes = () => calls.filter(call => call.method !== "GET");
const reads = () => calls.filter(call => call.method === "GET");
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}
function changeAccount() {
  installSession("synthetic-other-grants", { id: "other-admin", tenant_id: "other-company", role: "admin", email: "other@fixture.test", display_name: "Other" });
}
beforeEach(() => {
  calls = []; pending = [];
  readGrants = async () => json([grant]);
  writeGrant = async () => json({ updated: true });
  installSession("synthetic-grants-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {} };
    calls.push(call);
    if (call.method === "GET" && call.path.endsWith("/grants")) return readGrants(call);
    if (call.method === "GET" && call.path === "/api/v1/admin/users") return new Response(JSON.stringify({ data: [member, otherMember], meta: { total: 2, page: 1, per_page: 100 } }), { headers: { "Content-Type": "application/json" } });
    if (call.method === "PUT" && call.path.endsWith("/grants")) return writeGrant(call);
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});
function mount() {
  const cache = new Map();
  let select!: (value: MailTemplate | null) => void;
  function Harness() {
    const [selected, setSelected] = useState<MailTemplate | null>(template);
    const [box, setBox] = useState(mailbox.mailbox.id);
    useLayoutEffect(() => { select = setSelected; }, []);
    return <SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}>
      <TemplateGrantsView template={selected} mailboxes={[mailbox]} mailboxesReady mailbox={box} setMailbox={setBox} />
    </SWRConfig>;
  }
  const view = render(<Harness />);
  return { ...view, select: (value: MailTemplate | null) => act(() => select(value)) };
}
async function choose(id = member.id) {
  await screen.findByRole("option", { name: "Member · member@fixture.test" });
  fireEvent.change(screen.getByLabelText("Member allowed to use the template"), { target: { value: id } });
}
async function readyForm() {
  const view = mount(); await choose();
  await waitFor(() => expect(grantButton()).toBeEnabled());
  return view;
}

it("requires a new employee selection after selecting a different template", async () => {
  const view = await readyForm(); view.select(otherTemplate);
  await waitFor(() => expect(calls.some(call => call.path.includes("/reminder/grants"))).toBe(true));
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue("");
  expect(grantButton()).toBeDisabled(); expect(writes()).toHaveLength(0);
  await choose(); await waitFor(() => expect(grantButton()).toBeEnabled());
});

it("requires a new employee selection after the current session changes", async () => {
  await readyForm(); act(changeAccount);
  await screen.findByRole("option", { name: "Member · member@fixture.test" });
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue("");
  expect(grantButton()).toBeDisabled(); expect(writes()).toHaveLength(0);
});

it.each([false, true])("ignores a delayed grant result after switching templates (failure=%s)", async failure => {
  const gate = delayed(); writeGrant = () => gate.promise;
  const view = await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.select(otherTemplate); await choose(otherMember.id);
  await waitFor(() => expect(reads().some(call => call.path.includes("/reminder/grants"))).toBe(true));
  const readCount = reads().length;
  await act(async () => gate.resolve(failure ? failed() : json({ updated: true })));
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  expect(reads()).toHaveLength(readCount);
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(otherMember.id);
});

it.each(["grant", "revoke"])("has no follow-up read or feedback after a pending %s is unmounted", async action => {
  const gate = delayed(); writeGrant = () => gate.promise;
  const view = await readyForm(); fireEvent.click(action === "grant" ? grantButton() : revokeButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.unmount(); const count = reads().length;
  await act(async () => gate.resolve(json({ updated: true })));
  expect(reads()).toHaveLength(count);
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});

it("does not surface a rejected grant from an unmounted view", async () => {
  const gate = delayed(); writeGrant = () => gate.promise;
  const view = await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.unmount(); await act(async () => gate.resolve(failed()));
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});

it("does not revive an old operation after visiting another template and returning", async () => {
  const gate = delayed(); writeGrant = () => gate.promise;
  const view = await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.select(otherTemplate); await choose(); view.select(template);
  await choose(otherMember.id);
  await act(async () => gate.resolve(json({ updated: true })));
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(otherMember.id);
  expect(writes()).toHaveLength(1);
});

it("keeps the new template operation independent from an older pending write", async () => {
  const old = delayed(); const current = delayed();
  writeGrant = call => call.path.includes("/welcome/") ? old.promise : current.promise;
  const view = await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.select(otherTemplate); await choose(otherMember.id);
  await waitFor(() => expect(grantButton()).toBeEnabled());
  fireEvent.click(grantButton()); await waitFor(() => expect(writes()).toHaveLength(2));
  await act(async () => old.resolve(json({ updated: true })));
  expect(grantButton()).toBeDisabled(); expect(toast.success).not.toHaveBeenCalled();
  await act(async () => current.resolve(json({ updated: true })));
  await waitFor(() => expect(grantButton()).toBeEnabled());
  expect(toast.success).toHaveBeenCalledOnce();
  expect(writes()[1]).toMatchObject({ path: "/api/v1/company/templates/reminder/grants", body: { user_id: otherMember.id, enabled: true } });
});

it("suppresses old feedback when the view changes during the post-write refresh", async () => {
  const gate = delayed();
  const view = await readyForm();
  readGrants = call => call.path.includes("/welcome/") ? gate.promise : Promise.resolve(json([grant]));
  fireEvent.click(grantButton());
  await waitFor(() => expect(reads().filter(call => call.path.includes("/welcome/grants"))).toHaveLength(2));
  view.select(otherTemplate); await choose();
  await act(async () => gate.resolve(json([grant])));
  expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
});

it("preserves a current template operation across a revision refresh", async () => {
  const gate = delayed(); writeGrant = () => gate.promise;
  const view = await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  view.select({ ...template, revision: 5, name: "Welcome revised" });
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(member.id);
  await act(async () => gate.resolve(json({ updated: true })));
  await waitFor(() => expect(toast.success).toHaveBeenCalledOnce());
  expect(writes()).toHaveLength(1);
});

it("keeps the explicitly submitted member immutable while preserving later form edits", async () => {
  const gate = delayed(); writeGrant = () => gate.promise;
  await readyForm(); fireEvent.click(grantButton()); fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  await choose(otherMember.id);
  await act(async () => gate.resolve(json({ updated: true })));
  await waitFor(() => expect(toast.success).toHaveBeenCalledOnce());
  expect(writes()[0].body.user_id).toBe(member.id);
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(otherMember.id);
});

it("reports a current failure without automatically replaying the permission write", async () => {
  writeGrant = async () => failed(); await readyForm(); fireEvent.click(grantButton());
  await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic grant failure"));
  expect(writes()).toHaveLength(1); expect(toast.success).not.toHaveBeenCalled();
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(member.id);
});
