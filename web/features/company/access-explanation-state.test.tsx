import { Component, useState, type ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { AccessExplanationPanel } from "./access-explanation";
import { installSession } from "@/lib/session";
import type { AdminUser } from "@/lib/types";
import type { AccessExplanation } from "./api";

const tenant = "access-company";
const box = "access-mailbox";
const employee = (id = "alice", active = true): AdminUser => ({ id, tenant_id: tenant, role: "user", display_name: id, email: `${id}@fixture.test`, is_active: active, created_at: "2026-10-09T00:00:00Z", updated_at: "2026-10-09T00:00:00Z" });
const explanation = (user = "alice", mailbox = box): AccessExplanation => ({ mailbox_id: mailbox, user_id: user, source: "grant", active: true, can_read: true, can_organize: false, can_send: true, template_only: true, send_policy: "template_required", reasons: ["grant", "template_required"] });
const json = (data: unknown, status = 200) => new Response(JSON.stringify(status === 200 ? { data } : data), { status, headers: { "Content-Type": "application/json" } });
type Call = { url: URL; headers: Headers; method: string; signal?: AbortSignal | null };
let calls: Call[], result: (call: Call) => Promise<Response> | Response;
let releases: Array<() => void>;
function identity(id = "access-admin") {
  installSession(`${id}-token`, { id, tenant_id: tenant, role: "admin", display_name: id, email: `${id}@fixture.test` });
}
function deferred(response: Response) {
  let release!: () => void;
  const promise = new Promise<Response>(resolve => { release = () => resolve(response); });
  releases.push(release); return { promise, release };
}
class FailureBoundary extends Component<{ children: ReactNode }, { failed: boolean }> {
  state = { failed: false };
  static getDerivedStateFromError() { return { failed: true }; }
  render() { return this.state.failed ? <p>Unexpected component failure</p> : this.props.children; }
}
function Harness() {
  const [members, setMembers] = useState([employee(), employee("bob")]);
  const [ready, setReady] = useState(true);
  const [mailbox, setMailbox] = useState(box);
  const { mutate } = useSWRConfig();
  return <>
    <button onClick={() => setMembers([employee("bob")])}>Remove inspected member</button>
    <button onClick={() => setMembers([employee("alice", false), employee("bob")])}>Disable inspected member</button>
    <button onClick={() => setReady(false)}>Directory pending</button>
    <button onClick={() => setReady(true)}>Directory ready</button>
    <button onClick={() => setMailbox("replacement-mailbox")}>Other mailbox</button>
    <button onClick={() => void mutate(key => Array.isArray(key) && Array.isArray(key[2]) && key[2][0] === "mailbox-access-explanation")}>Refresh inspector cache</button>
    <FailureBoundary><AccessExplanationPanel mailbox={mailbox} employees={members} employeesReady={ready} /></FailureBoundary>
  </>;
}
beforeEach(() => {
  identity(); calls = []; releases = [];
  result = call => {
    const match = /^\/api\/v1\/company\/mailboxes\/([^/]+)\/access\/([^/]+)$/.exec(call.url.pathname);
    if (!match) throw new Error(`Unexpected access explanation URL: ${call.url.pathname}`);
    return json(explanation(decodeURIComponent(match[2]), decodeURIComponent(match[1])));
  };
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call = { url: new URL(String(input), "http://localhost"), headers: new Headers(init?.headers), method: init?.method ?? "GET", signal: init?.signal };
    calls.push(call); return result(call);
  });
});
afterEach(async () => { cleanup(); await act(async () => releases.forEach(release => release())); vi.unstubAllGlobals(); });
const selector = () => screen.getByLabelText("Inspect employee access");
const choose = (user = "alice") => fireEvent.change(selector(), { target: { value: user } });
const rights = () => screen.queryByText(/Read: true/);
async function mount(selected = true) {
  render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><Harness /></SWRConfig>);
  if (selected) { choose(); await screen.findByText(/Read: true/); }
}
const invalidMessage = "Could not confirm current access for this mailbox and member. Reload the explanation.";

describe("current mailbox access explanation", () => {
  it("waits for explicit selection and communicates that no member has been inspected", async () => {
    await mount(false);
    expect(screen.getByText("Select a member to inspect their current access.")).toBeInTheDocument();
    expect(calls).toHaveLength(0); expect(rights()).not.toBeInTheDocument();
  });

  it("renders legitimate server capabilities and known reasons through the existing read-only endpoint", async () => {
    await mount();
    expect(rights()).toBeInTheDocument(); expect(screen.getByText("Sending requires an authorized template")).toBeInTheDocument();
    expect(calls).toHaveLength(1);
    expect(calls[0]).toMatchObject({ method: "GET" });
    expect(calls[0].url.pathname).toBe(`/api/v1/company/mailboxes/${box}/access/alice`);
    expect(calls[0].headers.get("X-Tenant-ID")).toBe(tenant);
    expect(calls.some(call => /\/(content|attachments)$/.test(call.url.pathname))).toBe(false);
  });

  it("shows loading feedback for a selected member until the first explanation settles", async () => {
    const pending = deferred(json(explanation())); result = () => pending.promise;
    await mount(false); choose();
    expect(screen.getByRole("status")).toHaveTextContent("Loading current employee access"); expect(rights()).not.toBeInTheDocument();
    await act(async () => pending.release()); expect(rights()).toBeInTheDocument();
  });

  it.each([
    ["wrong mailbox", { mailbox_id: "other-mailbox" }],
    ["wrong member", { user_id: "bob" }],
    ["nonboolean active", { active: "true" }],
    ["nonboolean read", { can_read: "true" }],
    ["missing organize", { can_organize: undefined }],
    ["nonboolean send", { can_send: 1 }],
    ["nonboolean template flag", { template_only: "false" }],
    ["unknown source", { source: "administrator" }],
    ["unknown send policy", { send_policy: "allow_all" }],
    ["malformed reason entry", { reasons: [42] }],
    ["missing reasons", { reasons: null }],
  ])("rejects %s without displaying an unverified right or crashing the page", async (_label, patch) => {
    result = () => json({ ...explanation(), ...patch });
    await mount(false); choose();
    expect(await screen.findByText(invalidMessage)).toBeInTheDocument();
    expect(rights()).not.toBeInTheDocument(); expect(screen.queryByText("Unexpected component failure")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Retry loading" })).toBeEnabled();
  });

  it("retires a removed member's cached explanation and requires another deliberate selection", async () => {
    await mount(); fireEvent.click(screen.getByRole("button", { name: "Remove inspected member" }));
    expect(selector()).toHaveValue(""); expect(rights()).not.toBeInTheDocument();
    expect(screen.getByText("Select a member to inspect their current access.")).toBeInTheDocument();
    expect(calls).toHaveLength(1);
  });

  it("hides stale rights and prevents selection while the employee directory is refreshing", async () => {
    await mount(); fireEvent.click(screen.getByRole("button", { name: "Directory pending" }));
    expect(selector()).toBeDisabled(); expect(rights()).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toHaveTextContent("Loading current employee directory");
    expect(calls).toHaveLength(1);
    result = () => json({ ...explanation(), can_read: false, can_send: false, template_only: false, reasons: ["zone_restricted"] });
    fireEvent.click(screen.getByRole("button", { name: "Directory ready" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    await screen.findByText("Permission profile excludes this domain"); expect(rights()).not.toBeInTheDocument();
  });

  it("a settled directory change re-reads the selected member and accepts legitimate inactive results", async () => {
    await mount();
    result = () => json({ ...explanation(), active: false, can_read: false, can_organize: false, can_send: false, template_only: false, reasons: ["grant", "inactive"] });
    fireEvent.click(screen.getByRole("button", { name: "Disable inspected member" }));
    await screen.findByText("Account disabled");
    expect(calls).toHaveLength(2); expect(selector()).toHaveValue("alice"); expect(rights()).not.toBeInTheDocument();
  });

  it("does not label a retained cached explanation as current while it is being revalidated", async () => {
    await mount(); const pending = deferred(json({ ...explanation(), can_read: false })); result = () => pending.promise;
    fireEvent.click(screen.getByRole("button", { name: "Refresh inspector cache" }));
    await waitFor(() => expect(calls).toHaveLength(2));
    expect(rights()).not.toBeInTheDocument(); expect(screen.getByRole("status")).toHaveTextContent("Refreshing current employee access");
    await act(async () => pending.release()); expect(rights()).not.toBeInTheDocument();
  });

  it("failed refresh clears old rights and an explicit retry restores only a fresh successful response", async () => {
    await mount(); result = () => json({ error: { code: "FORBIDDEN", message: "Explanation unavailable" } }, 403);
    fireEvent.click(screen.getByRole("button", { name: "Refresh inspector cache" }));
    await screen.findByText("Explanation unavailable"); expect(rights()).not.toBeInTheDocument();
    result = () => json(explanation());
    fireEvent.click(screen.getByRole("button", { name: "Retry loading" }));
    await screen.findByText(/Read: true/); expect(calls).toHaveLength(3);
    expect(calls.every(call => call.method === "GET")).toBe(true);
  });

  it("a response for the old selected member cannot replace the newer member's explanation", async () => {
    const pending = deferred(json({ ...explanation(), reasons: ["OWNER-OLD-RESPONSE"] }));
    result = call => call.url.pathname.endsWith("/alice") ? pending.promise : json({ ...explanation("bob"), can_read: false, reasons: ["none"] });
    await mount(false); choose(); choose("bob");
    await screen.findByText("No ownership or explicit grant; management is not content access");
    await act(async () => pending.release());
    expect(selector()).toHaveValue("bob"); expect(rights()).not.toBeInTheDocument(); expect(screen.queryByText("OWNER-OLD-RESPONSE")).not.toBeInTheDocument();
  });

  it("session and mailbox changes retire the old inspected selection", async () => {
    await mount();
    await act(async () => identity("replacement-admin"));
    expect(selector()).toHaveValue(""); expect(rights()).not.toBeInTheDocument();
    choose(); await screen.findByText(/Read: true/);
    fireEvent.click(screen.getByRole("button", { name: "Other mailbox" }));
    expect(selector()).toHaveValue(""); expect(rights()).not.toBeInTheDocument();
  });
});
