import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import { MailboxAdmin } from "./mailbox-admin";

const member = { id: "retention-member", tenant_id: "retention-tenant", role: "user", is_active: true, display_name: "Retention member", email: "retention@example.test", created_at: "2026-10-08T00:00:00Z", updated_at: "2026-10-08T00:00:00Z" };
type Call = { path: string; method: string; body: unknown };
let calls: Call[];
let createReply: () => Promise<Response>;
const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), { status, headers: { "Content-Type": "application/json" } });
const posts = () => calls.filter(call => call.method === "POST");
const create = () => screen.getByRole("button", { name: "Create mailbox" });
const retention = () => screen.getByLabelText("Retention hours (0 = permanent)");
const type = (kind: string) => fireEvent.change(screen.getByLabelText("Resource type"), { target: { value: kind } });
const editRetention = (value: string) => fireEvent.change(retention(), { target: { value } });
async function settle() { await act(async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); }); }
async function mount() {
  render(<MailboxAdmin />);
  await waitFor(() => expect(calls.filter(call => call.method === "GET")).toHaveLength(2));
  await settle();
  fireEvent.change(screen.getByLabelText("Mailbox local part"), { target: { value: "retention-fixture" } });
}
function personal() {
  type("personal");
  fireEvent.change(screen.getByLabelText("Mailbox owner"), { target: { value: member.id } });
}

beforeEach(() => {
  calls = [];
  createReply = async () => json({ data: { id: "created-mailbox" } });
  installSession("synthetic-retention-session", { id: "retention-admin", tenant_id: member.tenant_id, role: "admin", email: "admin@example.test", display_name: "Retention admin" });
  vi.spyOn(toast, "error").mockImplementation(() => "error-toast");
  vi.spyOn(toast, "success").mockImplementation(() => "success-toast");
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const method = init?.method ?? "GET";
    calls.push({ path, method, body: init?.body ? JSON.parse(String(init.body)) : undefined });
    if (path === "/api/v1/admin/users" && method === "GET") return json({ data: [member], meta: { page: 1, per_page: 100, total: 1 } });
    if (path === "/api/v1/company/mailboxes" && method === "GET") return json({ data: [] });
    if (path === "/api/v1/company/mailboxes" && method === "POST") return createReply();
    throw new Error(`Unexpected fixture request: ${method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });

describe("company mailbox retention input", () => {
  it.each([
    ["0", 0], ["1", 1], ["876000", 876000], ["00024", 24], ["1e3", 1000],
  ])("creates a shared mailbox with the explicit whole-hour value %s", async (value, expected) => {
    await mount();
    editRetention(String(value));
    expect(create()).toBeEnabled();
    fireEvent.click(create());
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));
    expect(posts()).toEqual([{ path: "/api/v1/company/mailboxes", method: "POST", body: { local_part: "retention-fixture", kind: "shared", retention_hours: expected } }]);
  });

  it.each(["", "-1", "1.5", "876001", "9007199254740992", "1e309"])("blocks the invalid shared retention input %j without creating a mailbox", async value => {
    await mount();
    editRetention(value);
    expect(create()).toBeDisabled();
    fireEvent.click(create());
    await settle();
    expect(posts()).toHaveLength(0);
    expect(screen.getByLabelText("Mailbox local part")).toHaveValue("retention-fixture");
  });

  it("keeps a cleared retention field empty until the user explicitly chooses permanent storage", async () => {
    await mount();
    editRetention("24");
    editRetention("");
    expect(retention()).toHaveValue(null);
    expect(create()).toBeDisabled();
    editRetention("0");
    expect(create()).toBeEnabled();
    fireEvent.click(create());
    await waitFor(() => expect(posts()).toHaveLength(1));
    expect(posts()[0].body).toEqual({ local_part: "retention-fixture", kind: "shared", retention_hours: 0 });
  });

  it.each(["", "-1", "1.5", "876001"])("ignores hidden shared retention %j for a personal mailbox", async value => {
    await mount();
    editRetention(value);
    personal();
    expect(screen.queryByLabelText("Retention hours (0 = permanent)")).not.toBeInTheDocument();
    expect(create()).toBeEnabled();
    fireEvent.click(create());
    await waitFor(() => expect(toast.success).toHaveBeenCalledTimes(1));
    expect(posts()).toEqual([{ path: "/api/v1/company/mailboxes", method: "POST", body: { local_part: "retention-fixture", kind: "personal", owner_user_id: member.id, retention_hours: 0 } }]);
  });

  it("restores the invalid shared input after visiting the personal form", async () => {
    await mount();
    editRetention("");
    personal();
    expect(create()).toBeEnabled();
    type("shared");
    expect(retention()).toHaveValue(null);
    expect(create()).toBeDisabled();
    expect(posts()).toHaveLength(0);
  });

  it("keeps the selected shared retention intact when the server rejects creation", async () => {
    createReply = async () => json({ error: { code: "UNAVAILABLE", message: "Mailbox creation unavailable" } }, 503);
    await mount();
    editRetention("48");
    fireEvent.click(create());
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Mailbox creation unavailable"));
    expect(retention()).toHaveValue(48);
    expect(screen.getByLabelText("Mailbox local part")).toHaveValue("retention-fixture");
    expect(create()).toBeEnabled();
    expect(posts()).toHaveLength(1);
  });
});

// A floating-point conversion can round a fractional input to a whole hour,
// including underflow to zero (permanent storage). Preserve the input intent.
it.each(["1e-324", "1.0000000000000001", "876000.00000000001"])("rejects fractional intent %s even if Number rounds it to an integer", async value => {
  await mount();
  editRetention(value);
  expect(create()).toBeDisabled();
  fireEvent.click(create());
  await settle();
  expect(posts()).toHaveLength(0);
});

it.each([["1.20e1", 12], ["12.000", 12], ["0e-324", 0]])("preserves an exact whole-hour decimal %s", async (value, expected) => {
  await mount();
  editRetention(String(value));
  expect(create()).toBeEnabled();
  fireEvent.click(create());
  await waitFor(() => expect(posts()).toHaveLength(1));
  expect(posts()[0].body).toEqual({ local_part: "retention-fixture", kind: "shared", retention_hours: expected });
});
