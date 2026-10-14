import React from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig, useSWRConfig } from "swr";
import { toast } from "sonner";
import { installSession } from "@/lib/session";
import type { MailboxGrantSnapshot, WorkGrant, WorkMailbox } from "@/lib/company";
import type { AdminUser, Mailbox } from "@/lib/types";
import { GrantEditor } from "./grants";

// Real inputs, SWR and company/request APIs. Only the HTTP peer and toast
// renderer are substituted; reads and writes are recorded independently.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const timestamp = "2026-10-08T00:00:00Z";
const mailbox: WorkMailbox = {
  mailbox: { id: "review-box", kind: "shared", full_address: "review@fixture.test" } as Mailbox,
  revision: 9, can_read: false, can_send: false, can_organize: false, template_only: false,
};
const employees: AdminUser[] = ["a", "b"].map(id => ({
  id, tenant_id: "review-company", email: `${id}@fixture.test`, display_name: id.toUpperCase(),
  role: "user", is_active: true, created_at: timestamp, updated_at: timestamp,
}));
const savedGrant = (revision: number): WorkGrant => ({
  user_id: "a", tenant_id: "review-company", mailbox_id: "review-box",
  can_read: revision === 12, can_organize: false, can_send: false, template_only: false,
  created_at: timestamp, updated_at: timestamp,
});
const snapshot = (revision: number): MailboxGrantSnapshot => ({ revision, grants: [savedGrant(revision)] });
const path = "/api/v1/company/mailboxes/review-box/grants";
const json = (data: unknown) => new Response(JSON.stringify({ data }), {
  headers: { "Content-Type": "application/json" },
});
const readFailure = () => new Response(JSON.stringify({ error: {
  code: "UNAVAILABLE", message: "Synthetic permission read unavailable",
} }), { status: 503, headers: { "Content-Type": "application/json" } });
type Call = { method: string; body: unknown };
let calls: Call[];
let readsQueue: Array<Response | Promise<Response>>;
let wireRevision: number;
let rejectWrites: boolean;
let conflictAdvancesRevision: boolean;
let releasePending: Array<() => void>;
const puts = () => calls.filter(call => call.method === "PUT");
const gets = () => calls.filter(call => call.method === "GET");
function defer(reply: Response) {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  const release = () => resolve(reply);
  releasePending.push(release);
  return { promise, release };
}

beforeEach(() => {
  calls = []; readsQueue = []; releasePending = []; wireRevision = 12; rejectWrites = true; conflictAdvancesRevision = true;
  installSession("synthetic-grant-review-token", {
    id: "review-admin", tenant_id: "review-company", role: "admin",
    email: "admin@fixture.test", display_name: "Synthetic review admin",
  });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const requestPath = new URL(String(input), "http://localhost").pathname;
    if (requestPath !== path) throw new Error(`Unexpected synthetic request: ${requestPath}`);
    const call = { method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    if (call.method === "GET") return await (readsQueue.shift() ?? json(snapshot(wireRevision)));
    if (call.method === "PUT") {
      if (!rejectWrites || conflictAdvancesRevision) wireRevision = 13;
      return rejectWrites ? new Response(JSON.stringify({ error: {
        code: "CONFLICT", message: conflictAdvancesRevision ? "Synthetic stale grant revision" : "Synthetic resource busy; reload before retrying",
      } }), { status: 409, headers: { "Content-Type": "application/json" } }) : json({ updated: true });
    }
    throw new Error(`Unexpected synthetic method: ${call.method}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => releasePending.forEach(release => release()));
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});

function Harness({ refresh }: { refresh: () => Promise<unknown> }) {
  const { mutate } = useSWRConfig();
  return <>
    <button onClick={() => void mutate(key => Array.isArray(key) &&
      Array.isArray(key[2]) && key[2][0] === "mailbox-grants")}>Background refresh</button>
    <GrantEditor mailbox={mailbox} employees={employees} refresh={refresh} />
  </>;
}
async function mount(ready = true, refresh = vi.fn().mockResolvedValue(undefined)) {
  const cache = new Map();
  render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><Harness refresh={refresh} /></SWRConfig>);
  if (ready) await screen.findByText(/a@fixture.test: read/);
  return refresh;
}
const member = () => screen.getByLabelText("Grant to member");
const save = () => screen.getByRole("button", { name: "Save mailbox grant" });
const reload = () => screen.getByRole("button", { name: "Reload permissions" });
const select = (id: string) => fireEvent.change(member(), { target: { value: id } });
async function conflicted() {
  await mount(); select("a");
  fireEvent.click(screen.getByLabelText("Send as"));
  await act(async () => fireEvent.click(save()));
  expect(puts()).toHaveLength(1);
  expect(save()).toBeDisabled();
  expect(gets()).toHaveLength(1);
}

describe("grant conflict review requires an authoritative snapshot", () => {
  it("submits the atomic grant revision and preserves explicit false values", async () => {
    rejectWrites = false; await mount(); select("a");
    fireEvent.click(screen.getByLabelText("Read"));
    await act(async () => fireEvent.click(save()));
    expect(puts()).toEqual([{ method: "PUT", body: {
      user_id: "a", can_read: false, can_organize: false, can_send: false, template_only: false, revision: 12,
    } }]);
    expect(toast.success).toHaveBeenCalledWith("Grant updated");
  });

  it("does not clear a 409 by choosing another member from the rejected cache", async () => {
    await conflicted(); select("b");
    expect(save()).toBeDisabled(); fireEvent.click(save());
    expect(puts()).toHaveLength(1); expect(gets()).toHaveLength(1);
  });

  it("does not clear a 409 by clearing and reselecting the original member", async () => {
    await conflicted(); select(""); select("a");
    expect(save()).toBeDisabled(); fireEvent.click(save());
    expect(puts()).toHaveLength(1); expect(gets()).toHaveLength(1);
  });

  it("keeps a 409 latched while the administrator edits individual rights", async () => {
    await conflicted(); fireEvent.click(screen.getByLabelText("Read"));
    expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    expect(screen.getByLabelText("Read")).not.toBeChecked();
    expect(screen.getByLabelText("Send as")).toBeChecked();
  });

  it("does not use a background read and reselection to substitute for explicit conflict review", async () => {
    await conflicted();
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Background refresh" })));
    expect(gets()).toHaveLength(2); select("b");
    expect(save()).toBeDisabled(); fireEvent.click(save());
    expect(puts()).toHaveLength(1);
  });

  it("preserves the conflicted form when the explicit grant reload fails", async () => {
    await conflicted(); readsQueue.push(readFailure());
    await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue("a");
    expect(screen.getByLabelText("Read")).toBeChecked();
    expect(screen.getByLabelText("Send as")).toBeChecked();
    expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("does not release a failed explicit review after a later background recovery", async () => {
    await conflicted(); readsQueue.push(readFailure());
    await act(async () => fireEvent.click(reload()));
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Background refresh" })));
    select("b");
    expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("allows a same-revision explicit read after a transient conflict, followed by deliberate review", async () => {
    // companyTxScope also maps lock/serialization errors to CONFLICT; those
    // failures do not necessarily increment the mailbox revision.
    conflictAdvancesRevision = false; await conflicted();
    await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue(""); expect(save()).toBeDisabled();
    expect(gets()).toHaveLength(2); expect(puts()).toHaveLength(1);
    select("a"); expect(screen.getByLabelText("Read")).toBeChecked();
    expect(screen.getByLabelText("Send as")).not.toBeChecked();
    rejectWrites = false; await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", body: {
      user_id: "a", can_read: true, can_organize: false, can_send: false, template_only: false, revision: 12,
    } });
  });

  it("does not accept a grant revision that moves backwards after a conflict", async () => {
    await conflicted(); readsQueue.push(json(snapshot(11)));
    await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue("a");
    expect(screen.getByLabelText("Send as")).toBeChecked();
    select("b"); expect(save()).toBeDisabled();
    expect(puts()).toHaveLength(1); expect(gets()).toHaveLength(2);
  });

  it("requires deliberate member review after an explicit read of the new revision", async () => {
    await conflicted();
    await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue(""); expect(save()).toBeDisabled();
    expect(puts()).toHaveLength(1); expect(gets()).toHaveLength(2);
    select("a"); expect(screen.getByLabelText("Read")).not.toBeChecked();
    expect(screen.getByLabelText("Send as")).not.toBeChecked();
    expect(save()).toBeEnabled(); rejectWrites = false;
    await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", body: {
      user_id: "a", can_read: false, can_organize: false, can_send: false, template_only: false, revision: 13,
    } });
  });

  it("holds the write gate until an explicit reload has actually completed", async () => {
    await conflicted(); const pending = defer(json(snapshot(13))); readsQueue.push(pending.promise);
    fireEvent.click(reload()); select("b");
    expect(save()).toBeDisabled(); expect(reload()).toBeDisabled();
    await act(async () => pending.release());
    expect(member()).toHaveValue(""); expect(save()).toBeDisabled();
    select("b"); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it("does not admit writes from an older snapshot while background revalidation is pending", async () => {
    await mount(); select("a"); const pending = defer(json(snapshot(13))); readsQueue.push(pending.promise);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Background refresh" })));
    expect(save()).toBeDisabled(); fireEvent.click(save()); expect(puts()).toEqual([]);
    await act(async () => pending.release());
    expect(save()).toBeDisabled(); expect(member()).toHaveValue("a");
    expect(screen.getByLabelText("Read")).toBeChecked();
  });

  it("does not bind a cached revision when a member is selected during revalidation", async () => {
    await mount(); const pending = defer(json(snapshot(12))); readsQueue.push(pending.promise);
    await act(async () => fireEvent.click(screen.getByRole("button", { name: "Background refresh" })));
    select("a"); await act(async () => pending.release());
    expect(save()).toBeDisabled(); expect(puts()).toEqual([]);
    select(""); select("a"); expect(save()).toBeEnabled();
  });

  it("preserves unsaved rights and blocks saving when a normal explicit reload fails", async () => {
    await mount(); select("a"); fireEvent.click(screen.getByLabelText("Send as"));
    readsQueue.push(readFailure()); await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Send as")).toBeChecked();
    expect(save()).toBeDisabled(); expect(puts()).toEqual([]);
  });

  it("preserves the conflict if refreshing the surrounding mailbox list fails", async () => {
    await mount(true, vi.fn().mockRejectedValue(new Error("Synthetic parent refresh unavailable")));
    select("a"); fireEvent.click(screen.getByLabelText("Send as"));
    await act(async () => fireEvent.click(save()));
    await act(async () => fireEvent.click(reload()));
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Send as")).toBeChecked();
    expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("requires a completed first read and deliberate selection before any write", async () => {
    const pending = defer(json(snapshot(12))); readsQueue.push(pending.promise);
    await mount(false); select("a"); expect(save()).toBeDisabled();
    await act(async () => pending.release()); expect(save()).toBeDisabled();
    select(""); select("a"); expect(save()).toBeEnabled(); expect(puts()).toEqual([]);
  });
});
