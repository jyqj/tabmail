import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { MailboxAdmin } from "./mailbox-admin";
import { installSession } from "@/lib/session";

const tenant = "access-directory-company";
const member = (id: string) => ({ id, tenant_id: tenant, role: "user", display_name: id, email: `${id}@fixture.test`, is_active: true });
const list = (data: unknown[]) => new Response(JSON.stringify({ data, meta: { page: 1, per_page: 100, total: data.length } }), { headers: { "Content-Type": "application/json" } });
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
let directory: () => Promise<Response> | Response;
let reads: number;
let release: (() => void) | undefined;
function RefreshDirectory() {
  const { mutate } = useSWRConfig();
  return <button onClick={() => void mutate(key => Array.isArray(key) && key[2] === "company-employees")}>Read employee directory again</button>;
}
beforeEach(() => {
  installSession("access-directory-token", { id: "admin", tenant_id: tenant, role: "admin", display_name: "Admin", email: "admin@fixture.test" });
  directory = () => list([member("alice"), member("bob")]); reads = 0; release = undefined;
  vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (path === "/api/v1/admin/users") return directory();
    if (path === "/api/v1/company/mailboxes") return json([{ mailbox: { id: "directory-box", tenant_id: tenant, kind: "shared", full_address: "directory@fixture.test", send_policy: "free" }, revision: 1, can_read: false, can_send: false, can_organize: false, template_only: false }]);
    if (path === "/api/v1/company/mailboxes/directory-box/grants") return json({ revision: 1, grants: [] });
    if (path === "/api/v1/company/mailboxes/directory-box/access/alice") {
      reads++;
      return json({ mailbox_id: "directory-box", user_id: "alice", source: "grant", active: true, can_read: true, can_send: false, can_organize: false, template_only: false, send_policy: "free", reasons: ["grant"] });
    }
    throw new Error(`Unexpected managed mailbox inspection request: ${path}`);
  });
});
afterEach(async () => { cleanup(); await act(async () => release?.()); vi.unstubAllGlobals(); });
async function mount() {
  render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><MailboxAdmin /><RefreshDirectory /></SWRConfig>);
  await screen.findByRole("option", { name: "directory@fixture.test · shared" });
  fireEvent.change(screen.getByLabelText("Manage mailbox"), { target: { value: "directory-box" } });
  fireEvent.change(screen.getByLabelText("Inspect employee access"), { target: { value: "alice" } });
  await screen.findByText(/Read: true/);
}
it("the real mailbox management page passes pending employee-directory authority into the inspector", async () => {
  await mount();
  const pending = new Promise<Response>(resolve => { release = () => resolve(list([member("bob")])); });
  directory = () => pending;
  fireEvent.click(screen.getByRole("button", { name: "Read employee directory again" }));
  await waitFor(() => expect(screen.getByLabelText("Inspect employee access")).toBeDisabled());
  expect(screen.queryByText(/Read: true/)).not.toBeInTheDocument();
  await act(async () => release?.());
  expect(screen.getByLabelText("Inspect employee access")).toHaveValue(""); expect(reads).toBe(1);
});
it("a failed real directory refresh never leaves its old employee's rights labeled as current", async () => {
  await mount();
  directory = () => new Response(JSON.stringify({ error: { code: "UNAVAILABLE", message: "Employee directory unavailable" } }), { status: 503, headers: { "Content-Type": "application/json" } });
  fireEvent.click(screen.getByRole("button", { name: "Read employee directory again" }));
  await screen.findByText("Employee directory unavailable");
  expect(screen.getByLabelText("Inspect employee access")).toBeDisabled();
  expect(screen.queryByText(/Read: true/)).not.toBeInTheDocument(); expect(reads).toBe(1);
});
