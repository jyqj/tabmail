import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { WorkMailbox } from "@/lib/company";
import type { AdminUser } from "@/lib/types";
import { GrantEditor } from "./grants";

// Shipping GrantEditor, actual lifecycle controls, SWR and request transport.
// Only the HTTP peer and toast renderer are synthetic; no browser/PG claim.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const tenant = "lifecycle-company";
const timestamp = "2026-10-09T00:00:00Z";
const employees: AdminUser[] = ["owner", "alice", "bob"].map(id => ({
  id, tenant_id: tenant, email: `${id}@fixture.test`, display_name: id,
  role: "user", is_active: true, created_at: timestamp, updated_at: timestamp,
}));
function box(kind: "personal" | "legacy" | "shared" = "personal", revision = 9, owner = "owner"): WorkMailbox {
  return { mailbox: { id: "lifecycle-box", tenant_id: tenant, kind,
    owner_user_id: kind === "personal" ? owner : undefined, zone_id: "zone", local_part: "lifecycle",
    resolved_domain: "fixture.test", full_address: "lifecycle@fixture.test", access_mode: "token",
    retention_hours_override: null, expires_at: null, created_at: timestamp },
  revision, can_read: false, can_send: false, can_organize: false, template_only: false };
}
type Call = { path: string; method: string; body?: Record<string, unknown> };
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (status = 409) => new Response(JSON.stringify({ error: {
  code: status === 409 ? "CONFLICT" : "UNAVAILABLE", message: status === 409 ? "Mailbox changed elsewhere" : "Synthetic read unavailable",
} }), { status, headers: { "Content-Type": "application/json" } });
let calls: Call[], server: WorkMailbox, rejectWrite: number, refreshFails: boolean;
let reads: Array<Response | Promise<Response>>, pending: Array<() => void>;
let delayedWrite: Promise<Response> | undefined;
function defer(value: Response) {
  let resolve!: (value: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  const release = () => resolve(value); pending.push(release);
  return { promise, release };
}
const writes = () => calls.filter(call => call.method === "POST");
const mailboxReads = () => calls.filter(call => call.path === "/api/v1/company/mailboxes");

beforeEach(() => {
  calls = []; reads = []; pending = []; server = box(); rejectWrite = 409; refreshFails = false; delayedWrite = undefined;
  installSession("synthetic-lifecycle-token", { id: "admin", tenant_id: tenant, role: "admin",
    email: "admin@fixture.test", display_name: "Synthetic administrator" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { path: new URL(String(input), "http://localhost").pathname,
      method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    if (call.path === "/api/v1/company/mailboxes" && call.method === "GET")
      return await (reads.shift() ?? json([server]));
    if (call.path.endsWith("/grants") && call.method === "GET") return json({ revision: server.revision, grants: [] });
    if (call.method === "POST" && (call.path.endsWith("/handover") || call.path.endsWith("/convert-shared"))) {
      if (delayedWrite) return await delayedWrite;
      if (rejectWrite) return failure(rejectWrite);
      server = call.path.endsWith("/handover") ? box("personal", server.revision + 1, String(call.body?.owner_user_id))
        : box("shared", server.revision + 1);
      return json(call.path.endsWith("/handover") ? { transferred: true } : { converted: true });
    }
    throw new Error(`Unexpected lifecycle request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup(); await act(async () => pending.forEach(release => release())); vi.unstubAllGlobals();
});
function Harness({ initial }: { initial: WorkMailbox }) {
  const [current, setCurrent] = useState(initial);
  return <>
    <button onClick={() => setCurrent(structuredClone(server))}>Observe mailbox</button>
    <GrantEditor mailbox={current} employees={employees} refresh={async () => {
      if (refreshFails) throw new Error("Synthetic parent read unavailable");
      setCurrent(structuredClone(server));
    }} />
  </>;
}
async function mount(kind: "personal" | "legacy" = "personal") {
  server = box(kind);
  const result = render(<SWRConfig value={{ provider: () => new Map(), dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><Harness initial={structuredClone(server)} /></SWRConfig>);
  await waitFor(() => expect(calls.some(call => call.path.endsWith("/grants"))).toBe(true));
  return result;
}
const action = () => screen.getByRole("button", { name: /^(Transfer mailbox|Convert to private)/ });
const reason = () => screen.getByLabelText("Resource-change reason (8+ characters)");
const changeReason = (value = "Approved mailbox transition") => fireEvent.change(reason(), { target: { value } });
const choose = (value: string) => fireEvent.change(screen.getByLabelText("New owner"), { target: { value } });
const review = () => screen.getByRole("button", { name: "Reload mailbox for review" });
const acknowledge = () => fireEvent.click(screen.getByLabelText("I reviewed the current mailbox before applying this change"));
async function submitted(kind: "personal" | "legacy" = "personal") {
  await mount(kind); if (kind === "personal") choose("alice"); changeReason();
  await act(async () => fireEvent.click(action())); expect(writes()).toHaveLength(1);
}
async function loadReview() {
  await act(async () => fireEvent.click(review()));
}

describe("mailbox lifecycle revision review", () => {
  it.each(["personal", "legacy"] as const)("preserves the %s lifecycle endpoint, revision and exact reason", async kind => {
    rejectWrite = 0; await mount(kind); if (kind === "personal") choose("alice");
    changeReason("  Approved transfer 中文  "); await act(async () => fireEvent.click(action()));
    expect(writes()[0]).toEqual({ path: `/api/v1/company/mailboxes/lifecycle-box/${kind === "personal" ? "handover" : "convert-shared"}`,
      method: "POST", body: { ...(kind === "personal" ? { owner_user_id: "alice" } : {}), revision: 9, reason: "  Approved transfer 中文  " } });
    expect(toast.success).toHaveBeenCalledWith("Mailbox lifecycle updated");
  });

  it.each(["personal", "legacy"] as const)("keeps a %s 409 locked until an explicit successful review", async kind => {
    await submitted(kind); expect(action()).toBeDisabled();
    fireEvent.click(action()); expect(writes()).toHaveLength(1); expect(mailboxReads()).toHaveLength(0);
  });

  it("does not unlock a conflict after target edits or away-and-back selection", async () => {
    await submitted(); choose("bob"); choose(""); choose("alice"); changeReason("Revised approval reason");
    expect(action()).toBeDisabled(); fireEvent.click(action()); expect(writes()).toHaveLength(1);
  });

  it("does not rebase a rejected handover onto a background mailbox revision", async () => {
    await submitted(); server = box("personal", 12, "bob"); fireEvent.click(screen.getByRole("button", { name: "Observe mailbox" }));
    expect(action()).toBeDisabled(); fireEvent.click(action()); expect(writes()).toHaveLength(1);
    expect(reason()).toHaveValue("Approved mailbox transition");
  });

  it("pins a dirty lifecycle draft before its first submission", async () => {
    await mount(); choose("alice"); changeReason(); server = box("personal", 12, "bob");
    fireEvent.click(screen.getByRole("button", { name: "Observe mailbox" }));
    expect(action()).toBeDisabled(); fireEvent.click(action()); expect(writes()).toHaveLength(0);
  });

  it.each(["failed", "older", "missing", "duplicate", "other tenant", "invalid revision"] as const)("a %s mailbox read cannot release a conflict", async kind => {
    await submitted(); server = box("personal", 12, "bob"); fireEvent.click(screen.getByRole("button", { name: "Observe mailbox" }));
    const other = structuredClone(server); other.mailbox.tenant_id = "other-company";
    reads.push(kind === "failed" ? failure(503) : json(kind === "older" ? [box("personal", 10)] : kind === "missing" ? []
      : kind === "duplicate" ? [server, server] : kind === "other tenant" ? [other] : [{ ...server, revision: 0 }]));
    await loadReview(); expect(action()).toBeDisabled();
    expect(reason()).toHaveValue("Approved mailbox transition"); expect(writes()).toHaveLength(1);
  });

  it.each([9, 13])("requires acknowledgement and submits the actually reviewed revision %s", async revision => {
    await submitted(); server = box("personal", revision, revision === 9 ? "owner" : "bob"); await loadReview();
    expect(action()).toBeDisabled(); expect(reason()).toHaveValue("Approved mailbox transition");
    acknowledge(); expect(action()).toBeEnabled(); rejectWrite = 0;
    await act(async () => fireEvent.click(action()));
    expect(writes()[1].body).toEqual({ owner_user_id: "alice", revision, reason: "Approved mailbox transition" });
    expect(mailboxReads()).toHaveLength(1);
  });

  it("does not keep a reviewed owner as the next transfer target", async () => {
    await submitted(); server = box("personal", 13, "alice"); await loadReview(); acknowledge();
    expect(action()).toBeDisabled(); choose("bob"); expect(action()).toBeEnabled(); rejectWrite = 0;
    await act(async () => fireEvent.click(action())); expect(writes()[1].body?.owner_user_id).toBe("bob");
  });

  it("requires acknowledgement before a reviewed legacy conversion", async () => {
    await submitted("legacy"); server = box("legacy", 13); await loadReview();
    expect(action()).toBeDisabled(); acknowledge(); expect(action()).toBeEnabled(); rejectWrite = 0;
    await act(async () => fireEvent.click(action())); expect(writes()[1].body?.revision).toBe(13);
  });

  it("preserves a successful receipt when parent refresh fails, without enabling a replay", async () => {
    rejectWrite = 0; refreshFails = true; await submitted();
    expect(toast.success).toHaveBeenCalledWith("Mailbox lifecycle updated"); expect(action()).toBeDisabled();
    fireEvent.click(action()); expect(writes()).toHaveLength(1);
    await loadReview(); expect(writes()).toHaveLength(1); expect(mailboxReads()).toHaveLength(1);
  });

  it("requires review after an unknown or unavailable write result", async () => {
    rejectWrite = 503; await submitted(); expect(action()).toBeDisabled();
    fireEvent.click(action()); expect(writes()).toHaveLength(1);
  });

  it("does not adopt a review that overlaps a newer local draft", async () => {
    await submitted(); const delayed = defer(json([box("personal", 13)])); reads.push(delayed.promise);
    fireEvent.click(review()); await waitFor(() => expect(mailboxReads()).toHaveLength(1));
    changeReason("New approval entered during read"); await act(async () => delayed.release());
    expect(action()).toBeDisabled(); expect(reason()).toHaveValue("New approval entered during read");
    expect(writes()).toHaveLength(1);
  });

  it("does not admit duplicate lifecycle writes before React renders busy", async () => {
    await mount(); choose("alice"); changeReason(); const delayed = defer(failure()); delayedWrite = delayed.promise;
    const button = action(); act(() => { fireEvent.click(button); fireEvent.click(button); });
    await waitFor(() => expect(writes()).toHaveLength(1)); await act(async () => delayed.release());
    expect(writes()).toHaveLength(1);
  });
});
