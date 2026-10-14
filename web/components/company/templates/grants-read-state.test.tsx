import React, { useLayoutEffect, useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
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
const member = { id: "member-one", email: "member@fixture.test", display_name: "Member", is_active: true } as AdminUser;
const mailbox: WorkMailbox = {
  mailbox: { id: "support", full_address: "support@fixture.test", kind: "shared" } as Mailbox,
  revision: 3, can_read: false, can_organize: false, can_send: false, template_only: false,
};
const grant = { template_id: template.id, user_id: member.id, mailbox_id: mailbox.mailbox.id };
const grantButton = () => screen.getByRole("button", { name: "Grant usage on the selected mailbox" });
const revokeButton = () => screen.getByRole("button", { name: "Revoke usage" });
const empty = "This template has no usage grants";
type Call = { path: string; method: string; body: Record<string, unknown> };
let calls: Call[];
let readGrants: () => Promise<Response>;
let readMembers: () => Promise<Response>;
let writeGrant: () => Promise<Response>;
let pending: Array<(response: Response) => void>;
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const directory = (users = [member]) => new Response(JSON.stringify({ data: users, meta: { total: users.length, page: 1, per_page: 100 } }), { headers: { "Content-Type": "application/json" } });
const failed = (status = 503) => new Response(JSON.stringify({ error: { code: status === 403 ? "FORBIDDEN" : "INTERNAL", message: `Synthetic grant read ${status}` } }), { status, headers: { "Content-Type": "application/json" } });
const writes = () => calls.filter(call => call.method !== "GET");
function delayed() {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  pending.push(resolve);
  return { promise, resolve };
}

beforeEach(() => {
  calls = []; pending = [];
  readGrants = async () => json([grant]);
  readMembers = async () => directory();
  writeGrant = async () => json({ updated: true });
  installSession("synthetic-grants-token", { id: "admin", tenant_id: "company", role: "admin", email: "admin@fixture.test", display_name: "Admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : {} };
    calls.push(call);
    if (call.method === "GET" && call.path.endsWith("/grants")) return readGrants();
    if (call.method === "GET" && call.path === "/api/v1/admin/users") return readMembers();
    if (call.method === "PUT" && call.path.endsWith("/grants")) return writeGrant();
    throw new Error(`Unexpected synthetic request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => { pending.forEach(resolve => resolve(json([]))); });
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});

function RefreshControls() {
  const { mutate } = useSWRConfig();
  const refresh = (prefix: string) => {
    void mutate(key => Array.isArray(key) && (key[2] === prefix || (Array.isArray(key[2]) && key[2][0] === prefix))).catch(() => undefined);
  };
  return <><button onClick={() => refresh("template-grants")}>Refresh grants</button><button onClick={() => refresh("template-users")}>Refresh members</button></>;
}
function mount(selected: MailTemplate | null = template) {
  const cache = new Map();
  let changeBoxes!: (boxes: WorkMailbox[]) => void;
  let changeReady!: (ready: boolean) => void;
  function Harness() {
    const [boxes, setBoxes] = useState([mailbox]);
    const [ready, setReady] = useState(true);
    const [box, setBox] = useState(mailbox.mailbox.id);
    useLayoutEffect(() => { changeBoxes = setBoxes; changeReady = setReady; }, []);
    return <SWRConfig value={{ provider: () => cache, dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}>
      <TemplateGrantsView template={selected} mailboxes={boxes} mailboxesReady={ready} mailbox={box} setMailbox={setBox} />
      <RefreshControls />
    </SWRConfig>;
  }
  const view = render(<Harness />);
  return { ...view, setBoxes: (boxes: WorkMailbox[]) => act(() => changeBoxes(boxes)), setReady: (ready: boolean) => act(() => changeReady(ready)) };
}
async function chooseMember() {
  await screen.findByRole("option", { name: "Member · member@fixture.test" });
  fireEvent.change(screen.getByLabelText("Member allowed to use the template"), { target: { value: member.id } });
}
async function readyForm() {
  const view = mount(); await chooseMember();
  await waitFor(() => expect(grantButton()).toBeEnabled());
  return view;
}

it("waits for a selected template before loading the employee directory or grants", () => {
  mount(null);
  expect(screen.getByText("Pick a template from the library to manage its usage grants.")).toBeInTheDocument();
  expect(calls).toHaveLength(0);
});

it("distinguishes pending grants from an empty successful list and blocks writes", async () => {
  const gate = delayed(); readGrants = () => gate.promise;
  mount(); await chooseMember();
  expect(screen.getByText("Loading usage grants…")).toHaveAttribute("role", "status");
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(grantButton()).toBeDisabled();
  fireEvent.click(grantButton()); expect(writes()).toHaveLength(0);
  await act(async () => gate.resolve(json([])));
  await screen.findByText(empty); expect(grantButton()).toBeEnabled();
});

it("shows the empty state only for an authoritative successful empty list", async () => {
  readGrants = async () => json([]);
  mount(); await screen.findByText(empty);
  expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  expect(writes()).toHaveLength(0);
});

it.each([403, 503])("blocks grants after an initial %s error and retries reads without a write", async status => {
  readGrants = async () => failed(status);
  mount(); await chooseMember(); await screen.findByRole("alert");
  expect(grantButton()).toBeDisabled(); expect(screen.queryByText(empty)).not.toBeInTheDocument();
  readGrants = async () => json([]);
  fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
  await waitFor(() => expect(grantButton()).toBeEnabled());
  await screen.findByText(empty); expect(writes()).toHaveLength(0);
});

it.each([403, 503])("hides cached grant actions after a %s revalidation failure", async status => {
  await readyForm(); expect(revokeButton()).toBeEnabled();
  readGrants = async () => failed(status);
  fireEvent.click(screen.getByRole("button", { name: "Refresh grants" }));
  await screen.findByRole("alert");
  expect(grantButton()).toBeDisabled();
  expect(screen.queryByRole("button", { name: "Revoke usage" })).not.toBeInTheDocument();
  expect(screen.queryByText(empty)).not.toBeInTheDocument();
  expect(writes()).toHaveLength(0);
});

it("blocks cached actions while grants are revalidating and preserves the chosen member", async () => {
  await readyForm();
  const gate = delayed(); readGrants = () => gate.promise;
  fireEvent.click(screen.getByRole("button", { name: "Refresh grants" }));
  await waitFor(() => expect(calls.filter(call => call.path.endsWith("/grants"))).toHaveLength(2));
  expect(screen.getByText("Refreshing usage grants…")).toHaveAttribute("role", "status");
  expect(grantButton()).toBeDisabled(); expect(revokeButton()).toBeDisabled();
  await act(async () => gate.resolve(json([grant])));
  await waitFor(() => expect(grantButton()).toBeEnabled());
  expect(screen.getByLabelText("Member allowed to use the template")).toHaveValue(member.id);
  expect(writes()).toHaveLength(0);
});

it("blocks grants and revocation when a cached employee-directory read fails", async () => {
  await readyForm(); readMembers = async () => failed(403);
  fireEvent.click(screen.getByRole("button", { name: "Refresh members" }));
  await screen.findByRole("alert");
  expect(grantButton()).toBeDisabled();
  const revoke = screen.queryByRole("button", { name: "Revoke usage" });
  if (revoke) expect(revoke).toBeDisabled();
  expect(writes()).toHaveLength(0);
});

it("does not submit a previously selected member after the refreshed directory marks them inactive", async () => {
  await readyForm(); readMembers = async () => directory([{ ...member, is_active: false }]);
  fireEvent.click(screen.getByRole("button", { name: "Refresh members" }));
  await waitFor(() => expect(screen.queryByRole("option", { name: "Member · member@fixture.test" })).not.toBeInTheDocument());
  expect(grantButton()).toBeDisabled();
  fireEvent.click(grantButton()); expect(writes()).toHaveLength(0);
});

it("does not submit a previously selected mailbox after it leaves the current mailbox list", async () => {
  const view = await readyForm(); view.setBoxes([]);
  expect(grantButton()).toBeDisabled();
  fireEvent.click(grantButton()); expect(writes()).toHaveLength(0);
});

it("blocks permission changes when the parent mailbox list is unavailable or revalidating", async () => {
  const view = await readyForm(); view.setReady(false);
  expect(grantButton()).toBeDisabled(); expect(revokeButton()).toBeDisabled();
  fireEvent.click(grantButton()); fireEvent.click(revokeButton()); expect(writes()).toHaveLength(0);
  view.setReady(true); expect(grantButton()).toBeEnabled();
});

it("grants usage without requiring the administrator to hold employee sender rights", async () => {
  readGrants = async () => json([]);
  await readyForm();
  fireEvent.click(grantButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0]).toEqual({ path: "/api/v1/company/templates/welcome/grants", method: "PUT", body: { user_id: member.id, mailbox_id: mailbox.mailbox.id, enabled: true } });
});

it("retains an explicit current-grant revocation and its subsequent authoritative refresh", async () => {
  await readyForm(); readGrants = async () => json([]);
  fireEvent.click(revokeButton());
  await waitFor(() => expect(writes()).toHaveLength(1));
  expect(writes()[0]).toMatchObject({ path: "/api/v1/company/templates/welcome/grants", method: "PUT", body: { user_id: member.id, mailbox_id: mailbox.mailbox.id, enabled: false } });
  await waitFor(() => expect(screen.queryByRole("button", { name: "Revoke usage" })).not.toBeInTheDocument());
});
