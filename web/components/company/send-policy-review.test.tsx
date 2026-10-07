import React, { useState } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { useAPI } from "@/hooks/use-api";
import { workMailboxes, type MailSendPolicy, type WorkMailbox } from "@/lib/company";
import { installSession } from "@/lib/session";
import type { Mailbox } from "@/lib/types";
import { MailboxSendPolicyEditor } from "./send-policy";

// The editor, parent SWR cache, API helpers and session checks are real. Only
// the HTTP peer and toast renderer are substituted. In particular, the parent
// uses the production bare mutate() contract, including cached data on error.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const listPath = "/api/v1/company/mailboxes";
const policyPath = (id: string) => `${listPath}/${id}/send-policy`;
const box = (id = "a", revision = 3, policy: MailSendPolicy = "free"): WorkMailbox => ({
  mailbox: { id, kind: "shared", full_address: `${id}@fixture.test`, send_policy: policy } as Mailbox,
  revision, can_read: false, can_organize: false, can_send: false, template_only: false,
});
const json = (data: unknown) => new Response(JSON.stringify({ data }), {
  headers: { "Content-Type": "application/json" },
});
const errorResponse = (code: string, status: number) => new Response(JSON.stringify({
  error: { code, message: `Synthetic ${code.toLowerCase()} response` },
}), { status, headers: { "Content-Type": "application/json" } });
const readFailure = () => errorResponse("UNAVAILABLE", 503);
type Call = { method: string; path: string; body: unknown };
let calls: Call[];
let rows: WorkMailbox[];
let readQueue: Array<Response | Promise<Response>>;
let writeQueue: Array<Response | Promise<Response>>;
let writeMode: "success" | "conflict" | "forbidden";
let conflictAdvances: boolean;
let releasePending: Array<() => void>;
const gets = () => calls.filter(call => call.method === "GET");
const puts = () => calls.filter(call => call.method === "PUT");
function defer(response: Response) {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  const release = () => resolve(response);
  releasePending.push(release);
  return { promise, release };
}
function update(id: string, revision: number, policy: MailSendPolicy) {
  rows = rows.map(row => row.mailbox.id === id ? box(id, revision, policy) : row);
}

beforeEach(() => {
  calls = []; rows = [box(), box("b", 8, "template_required")];
  readQueue = []; writeQueue = []; releasePending = []; writeMode = "success"; conflictAdvances = true;
  installSession("synthetic-policy-review-token", {
    id: "policy-admin", tenant_id: "policy-company", role: "admin",
    email: "admin@fixture.test", display_name: "Synthetic policy admin",
  });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const call: Call = { method: init?.method ?? "GET",
      path: new URL(String(input), "http://localhost").pathname,
      body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined };
    calls.push(call);
    if (call.method === "GET" && call.path === listPath) return await (readQueue.shift() ?? json(rows));
    const target = call.path.match(/^\/api\/v1\/company\/mailboxes\/([ab])\/send-policy$/);
    if (target && call.method === "PUT") {
      const queued = writeQueue.shift(); if (queued) return await queued;
      const id = target[1];
      const body = call.body as { revision: number; send_policy: MailSendPolicy | null };
      if (writeMode === "forbidden") return errorResponse("FORBIDDEN", 403);
      if (writeMode === "conflict") {
        if (conflictAdvances) update(id, body.revision + 1, "template_required");
        return errorResponse("CONFLICT", 409);
      }
      if (rows.find(row => row.mailbox.id === id)?.revision !== body.revision) return errorResponse("CONFLICT", 409);
      update(id, body.revision + 1, body.send_policy ?? "free");
      return json({ updated: true, revision: body.revision + 1 });
    }
    throw new Error(`Unexpected synthetic policy request: ${call.method} ${call.path}`);
  });
});
afterEach(async () => {
  cleanup();
  await act(async () => releasePending.forEach(release => release()));
  vi.restoreAllMocks(); vi.unstubAllGlobals();
});

function Harness({ strictRefresh = false }: { strictRefresh?: boolean }) {
  const boxes = useAPI("company-managed-mailboxes", workMailboxes);
  const [selected, setSelected] = useState("a");
  const current = boxes.data?.find(row => row.mailbox.id === selected);
  return <>
    <button onClick={() => setSelected("a")}>Choose A</button>
    <button onClick={() => setSelected("b")}>Choose B</button>
    <button onClick={() => void boxes.mutate()}>Background read</button>
    {current && <div>
      <h2>{current.mailbox.full_address}</h2>
      <MailboxSendPolicyEditor key={selected} mailbox={current} refresh={async () => {
        if (!strictRefresh) return boxes.mutate();
        const latest = await workMailboxes();
        await boxes.mutate(latest, { revalidate: false });
        return latest;
      }} />
    </div>}
  </>;
}
async function mount(strictRefresh = false) {
  const cache = new Map();
  const view = render(<SWRConfig value={{ provider: () => cache, dedupingInterval: 0,
    revalidateOnFocus: false, shouldRetryOnError: false }}><Harness strictRefresh={strictRefresh} /></SWRConfig>);
  await screen.findByText("a@fixture.test");
  return view;
}
const choice = () => screen.getByLabelText("Send policy override");
const save = () => screen.getByRole("button", { name: "Save send policy override" });
const review = () => screen.getByRole("button", { name: "Review latest version" });
const choose = (policy: MailSendPolicy | "") => fireEvent.change(choice(), { target: { value: policy } });
async function conflicted(sameRevision = false) {
  writeMode = "conflict"; conflictAdvances = !sameRevision;
  const view = await mount(); readQueue.push(readFailure()); choose("disabled");
  await act(async () => fireEvent.click(save()));
  expect(puts()).toHaveLength(1); expect(gets()).toHaveLength(2);
  expect(save()).toBeDisabled(); expect(choice()).toHaveValue("disabled");
  return view;
}

describe("mailbox policy review requires current server evidence", () => {
  it("preserves the normal selected-policy write and authoritative parent refresh", async () => {
    await mount(); choose("disabled"); await act(async () => fireEvent.click(save()));
    expect(puts()).toEqual([{ method: "PUT", path: policyPath("a"), body: { send_policy: "disabled", revision: 3 } }]);
    expect(gets()).toHaveLength(2); expect(save()).toBeEnabled();
    expect(screen.getByText(/Effective send policy.*Sending disabled/)).toBeInTheDocument();
    expect(toast.success).toHaveBeenCalledWith("Send policy updated");
  });

  it("sends explicit inheritance as null without requiring content send rights", async () => {
    await mount(); await act(async () => fireEvent.click(save()));
    expect(puts()).toEqual([{ method: "PUT", path: policyPath("a"), body: { send_policy: null, revision: 3 } }]);
    expect(save()).toBeEnabled(); expect(toast.success).toHaveBeenCalledWith("Send policy updated");
  });

  it("requires a real GET instead of old props when reviewing a conflicted form", async () => {
    await conflicted(); await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(puts()).toHaveLength(1);
    expect(choice()).toHaveValue(""); expect(save()).toBeEnabled();
    expect(screen.getByText(/Effective send policy.*Published templates only/)).toBeInTheDocument();
    writeMode = "success"; choose("free"); await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", path: policyPath("a"), body: { send_policy: "free", revision: 4 } });
  });

  it("allows an unchanged revision after transient CONFLICT only after a fresh GET and review", async () => {
    await conflicted(true); await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(choice()).toHaveValue(""); expect(save()).toBeEnabled();
    writeMode = "success"; choose("disabled"); await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", path: policyPath("a"), body: { send_policy: "disabled", revision: 3 } });
  });

  it("retains the conflict and chosen policy when the explicit GET fails", async () => {
    await conflicted(); readQueue.push(readFailure());
    await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(choice()).toHaveValue("disabled");
    expect(save()).toBeDisabled(); fireEvent.click(save()); expect(puts()).toHaveLength(1);
    expect(review()).toBeEnabled();
  });

  it("retries only GET after a failed review and uses the actual later revision", async () => {
    await conflicted(); readQueue.push(readFailure());
    await act(async () => fireEvent.click(review()));
    update("a", 7, "free"); await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(4); expect(puts()).toHaveLength(1); expect(choice()).toHaveValue("");
    writeMode = "success"; choose("disabled"); await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", path: policyPath("a"), body: { send_policy: "disabled", revision: 7 } });
  });

  it("does not substitute a later background prop update for explicit conflict review", async () => {
    await conflicted(); await act(async () => fireEvent.click(screen.getByText("Background read")));
    expect(gets()).toHaveLength(3); expect(save()).toBeDisabled(); expect(choice()).toHaveValue("disabled");
    await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(4); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it.each([
    { label: "backwards revision", data: [box("a", 2)] },
    { label: "fractional revision", data: [box("a", 4.5)] },
    { label: "string revision", data: [{ ...box("a", 4), revision: "4" }] },
    { label: "missing mailbox", data: [box("b", 8)] },
    { label: "duplicate mailbox", data: [box("a", 4), box("a", 4)] },
    { label: "missing policy", data: [{ ...box("a", 4), mailbox: { id: "a" } }] },
    { label: "unknown policy", data: [{ ...box("a", 4), mailbox: { id: "a", send_policy: "unrecognized" } }] },
  ])("rejects $label instead of releasing the pending review", async ({ data }) => {
    await conflicted(); readQueue.push(json(data));
    await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(choice()).toHaveValue("disabled"); expect(save()).toBeDisabled();
    expect(puts()).toHaveLength(1); expect(review()).toBeEnabled();
  });

  it("does not roll back below an acknowledged mutation's actual revision", async () => {
    await mount(); readQueue.push(readFailure()); choose("disabled");
    await act(async () => fireEvent.click(save()));
    expect(toast.success).toHaveBeenCalledWith("Send policy updated"); expect(save()).toBeDisabled();
    readQueue.push(json([box("a", 3)])); await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(save()).toBeDisabled(); expect(choice()).toHaveValue("disabled");
    expect(puts()).toHaveLength(1);
  });

  it("keeps an acknowledged write successful when the parent refresh rejects", async () => {
    await mount(true); readQueue.push(readFailure()); choose("disabled");
    await act(async () => fireEvent.click(save()));
    expect(toast.success).toHaveBeenCalledWith("Send policy updated");
    expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it("uses a current read several revisions after the acknowledged write instead of guessing plus one", async () => {
    await mount(); readQueue.push(readFailure()); choose("disabled");
    await act(async () => fireEvent.click(save()));
    update("a", 9, "template_required"); await act(async () => fireEvent.click(review()));
    expect(gets()).toHaveLength(3); expect(choice()).toHaveValue("");
    choose("free"); await act(async () => fireEvent.click(save()));
    expect(puts()[1]).toEqual({ method: "PUT", path: policyPath("a"), body: { send_policy: "free", revision: 9 } });
  });

  it("keeps review pending and admits only one GET while the explicit read is unresolved", async () => {
    await conflicted(); const pending = defer(json(rows)); readQueue.push(pending.promise);
    const button = review(); act(() => { fireEvent.click(button); fireEvent.click(button); });
    expect(gets()).toHaveLength(3); expect(button).toBeDisabled(); expect(save()).toBeDisabled();
    await act(async () => pending.release()); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it("does not let a late A review alter the currently selected B editor", async () => {
    await conflicted(); const pending = defer(json([box("a", 6, "disabled"), box("b", 8)]));
    readQueue.push(pending.promise); fireEvent.click(review()); expect(gets()).toHaveLength(3);
    fireEvent.click(screen.getByText("Choose B")); choose("template_required");
    await act(async () => pending.release());
    expect(screen.getByText("b@fixture.test")).toBeInTheDocument();
    expect(choice()).toHaveValue("template_required"); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it("does not apply an old review after A to B to A navigation", async () => {
    await conflicted(); const pending = defer(json([box("a", 6, "disabled")]));
    readQueue.push(pending.promise); fireEvent.click(review()); expect(gets()).toHaveLength(3);
    fireEvent.click(screen.getByText("Choose B")); fireEvent.click(screen.getByText("Choose A")); choose("free");
    await act(async () => pending.release());
    expect(choice()).toHaveValue("free"); expect(save()).toBeEnabled();
    expect(screen.getByText(/Effective send policy.*Free-form writing/)).toBeInTheDocument();
  });

  it("rejects a review response below a newer parent snapshot observed while awaiting it", async () => {
    await conflicted(); const pending = defer(json([box("a", 4, "disabled")]));
    readQueue.push(pending.promise); fireEvent.click(review()); expect(gets()).toHaveLength(3);
    update("a", 6, "free"); await act(async () => fireEvent.click(screen.getByText("Background read")));
    await act(async () => pending.release());
    expect(save()).toBeDisabled(); expect(choice()).toHaveValue("disabled"); expect(puts()).toHaveLength(1);
    expect(screen.getByText(/Effective send policy.*Free-form writing/)).toBeInTheDocument();
  });

  it("suppresses a failed review after the editor unmounts", async () => {
    const view = await conflicted(); const pending = defer(readFailure()); readQueue.push(pending.promise);
    fireEvent.click(review()); expect(gets()).toHaveLength(3); const previousErrors = vi.mocked(toast.error).mock.calls.length;
    view.unmount(); await act(async () => pending.release());
    expect(toast.error).toHaveBeenCalledTimes(previousErrors); expect(puts()).toHaveLength(1);
  });

  it("suppresses an old review result after the active session changes", async () => {
    await conflicted(); const pending = defer(json([box("a", 6, "disabled")])); readQueue.push(pending.promise);
    fireEvent.click(review()); expect(gets()).toHaveLength(3); const previousErrors = vi.mocked(toast.error).mock.calls.length;
    await act(async () => installSession("synthetic-other-policy-session", {
      id: "other-admin", tenant_id: "other-company", role: "admin",
      email: "other@fixture.test", display_name: "Other synthetic admin",
    }));
    await screen.findByText("a@fixture.test"); choose("free"); await act(async () => pending.release());
    expect(choice()).toHaveValue("free"); expect(save()).toBeEnabled();
    expect(toast.error).toHaveBeenCalledTimes(previousErrors); expect(puts()).toHaveLength(1);
  });

  it("does not refresh or toast for a mutation completed after selecting another mailbox", async () => {
    await mount(); const pending = defer(json({ updated: true, revision: 4 })); writeQueue.push(pending.promise);
    choose("disabled"); fireEvent.click(save()); fireEvent.click(screen.getByText("Choose B")); choose("free");
    await act(async () => pending.release());
    expect(gets()).toHaveLength(1); expect(choice()).toHaveValue("free"); expect(save()).toBeEnabled();
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("preserves a definitively rejected non-conflict operation without issuing a background retry", async () => {
    await mount(); writeMode = "forbidden"; choose("disabled"); await act(async () => fireEvent.click(save()));
    expect(gets()).toHaveLength(1); expect(puts()).toHaveLength(1);
    expect(choice()).toHaveValue("disabled"); expect(save()).toBeEnabled();
    expect(toast.success).not.toHaveBeenCalled();
  });
});
