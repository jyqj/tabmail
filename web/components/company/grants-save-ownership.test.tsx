import { useState } from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { SWRConfig } from "swr";
import { toast } from "sonner";
import { GrantEditor } from "./grants";
import { installSession } from "@/lib/session";
import type { WorkMailbox } from "@/lib/company";
import type { AdminUser, Mailbox } from "@/lib/types";

vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
const tenant = "grant-owner-company";
const employees = ["a", "b"].map(id => ({ id, tenant_id: tenant, display_name: id.toUpperCase(), email: `${id}@fixture.test`, role: "user", is_active: true } as AdminUser));
const mailbox = (id = "grant-owner-box"): WorkMailbox => ({
  mailbox: { id, tenant_id: tenant, kind: "shared", full_address: `${id}@fixture.test` } as Mailbox,
  revision: 12, can_read: false, can_send: false, can_organize: false, template_only: false,
});
const grant = (user = "a", canRead = true) => ({ user_id: user, tenant_id: tenant, mailbox_id: "grant-owner-box", can_read: canRead, can_organize: false, can_send: false, template_only: false });
const snapshot = (revision = 12) => ({ revision, grants: [grant(), grant("b", false)] });
const json = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const rejected = (status = 503) => new Response(JSON.stringify({ error: { code: status === 409 ? "CONFLICT" : "UNAVAILABLE", message: "Synthetic grant request unavailable" } }), { status, headers: { "Content-Type": "application/json" } });
type Call = { path: string; method: string; body: Record<string, unknown> | undefined; signal?: AbortSignal | null };
let calls: Call[], revision: number, readReply: (() => Promise<Response> | Response) | undefined;
let writeReply: () => Promise<Response> | Response;
let releases: Array<() => void>;
const puts = () => calls.filter(call => call.method === "PUT");
const gets = () => calls.filter(call => call.method === "GET");
function deferred(response: Response) {
  let release!: () => void;
  const promise = new Promise<Response>(resolve => { release = () => resolve(response); });
  releases.push(release);
  return { promise, release };
}
function identity(id = "grant-admin") {
  installSession(`${id}-token`, { id, tenant_id: tenant, role: "admin", email: `${id}@fixture.test`, display_name: id });
}
beforeEach(() => {
  identity(); calls = []; revision = 12; releases = []; readReply = undefined;
  writeReply = () => { revision = 13; return json({ updated: true, revision }); };
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    if (!/^\/api\/v1\/company\/mailboxes\/(grant-owner-box|other-box)\/grants$/.test(path)) throw new Error(`Unexpected grant ownership request: ${path}`);
    const call = { path, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined, signal: init?.signal };
    calls.push(call);
    return call.method === "PUT" ? writeReply() : readReply?.() ?? json(snapshot(revision));
  });
});
afterEach(async () => { cleanup(); await act(async () => releases.forEach(release => release())); vi.unstubAllGlobals(); });
function Harness({ refresh }: { refresh: () => Promise<unknown> }) {
  const [box, setBox] = useState("grant-owner-box");
  return <><button onClick={() => setBox("other-box")}>Other mailbox</button><GrantEditor mailbox={mailbox(box)} employees={employees} refresh={refresh} /></>;
}
async function mount(refresh = vi.fn().mockResolvedValue(undefined)) {
  const mounted = render(<SWRConfig value={{ dedupingInterval: 0, shouldRetryOnError: false, revalidateOnFocus: false }}><Harness refresh={refresh} /></SWRConfig>);
  await screen.findByText(/a@fixture.test: read/);
  return { ...mounted, refresh };
}
const member = () => screen.getByLabelText("Grant to member");
const save = () => screen.getByRole("button", { name: "Save mailbox grant" });
const reload = () => screen.getByRole("button", { name: "Reload permissions" });
const select = (value: string) => fireEvent.change(member(), { target: { value } });
async function pendingSave() {
  const mounted = await mount(); const pending = deferred(json({ updated: true, revision: 13 }));
  writeReply = () => pending.promise;
  select("a"); fireEvent.click(screen.getByLabelText("Send as")); fireEvent.click(save());
  await waitFor(() => expect(puts()).toHaveLength(1));
  return { ...mounted, ...pending };
}
async function acknowledge(release: () => void) {
  revision = 13;
  await act(async () => release());
  await waitFor(() => expect(toast.success).toHaveBeenCalledWith("Grant updated"));
}

describe("mailbox grant save ownership and acknowledged readback", () => {
  it("submits one complete grant and clears an unchanged submitted form after confirmation", async () => {
    await mount(); select("a"); fireEvent.click(screen.getByLabelText("Read"));
    await act(async () => fireEvent.click(save()));
    expect(puts()).toHaveLength(1);
    expect(puts()[0].body).toEqual({ user_id: "a", can_read: false, can_organize: false, can_send: false, template_only: false, revision: 12 });
    expect(member()).toHaveValue(""); expect(save()).toBeDisabled();
    expect(toast.success).toHaveBeenCalledWith("Grant updated"); expect(toast.error).not.toHaveBeenCalled();
  });

  it("preserves rights edited while their earlier version is saving and does not rebase them", async () => {
    const pending = await pendingSave(); fireEvent.click(screen.getByLabelText("Read"));
    await acknowledge(pending.release);
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Read")).not.toBeChecked();
    expect(screen.getByLabelText("Send as")).toBeChecked(); expect(save()).toBeDisabled();
    expect(puts()).toHaveLength(1);
  });

  it("preserves a different member's new draft after the old member is saved", async () => {
    const pending = await pendingSave(); select("b"); fireEvent.click(screen.getByLabelText("Template-only sending"));
    await acknowledge(pending.release);
    expect(member()).toHaveValue("b"); expect(screen.getByLabelText("Template-only sending")).toBeChecked();
    expect(screen.getByLabelText("Send as")).toBeChecked(); expect(save()).toBeDisabled();
    expect(puts()[0].body?.user_id).toBe("a"); expect(puts()).toHaveLength(1);
  });

  it("does not mistake member A-to-B-to-A reselection for the original submitted intent", async () => {
    const pending = await pendingSave(); select("b"); select("a");
    await acknowledge(pending.release);
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Read")).toBeChecked();
    expect(screen.getByLabelText("Send as")).not.toBeChecked(); expect(save()).toBeDisabled();
  });

  it("does not discard an ABA checkbox edit even if its final values match the submitted draft", async () => {
    const pending = await pendingSave(); fireEvent.click(screen.getByLabelText("Read")); fireEvent.click(screen.getByLabelText("Read"));
    await acknowledge(pending.release);
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Read")).toBeChecked();
    expect(screen.getByLabelText("Send as")).toBeChecked(); expect(save()).toBeDisabled();
  });

  it("keeps an acknowledged save visible when its following permission GET fails", async () => {
    await mount(); select("a"); readReply = () => rejected();
    await act(async () => fireEvent.click(save()));
    expect(toast.success).toHaveBeenCalledWith("Grant updated");
    expect(toast.error).not.toHaveBeenCalled();
    expect(await screen.findByText(/The grant was saved, but current permissions could not be refreshed/)).toBeInTheDocument();
    select("b"); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
    readReply = undefined; await act(async () => fireEvent.click(reload()));
    select("b"); expect(save()).toBeEnabled(); expect(puts()).toHaveLength(1);
  });

  it("does not reclassify a committed PUT as a failure when parent mailbox refresh throws", async () => {
    const refresh = vi.fn().mockRejectedValue(new Error("Synthetic parent read failure"));
    await mount(refresh); select("a"); await act(async () => fireEvent.click(save()));
    expect(refresh).toHaveBeenCalledOnce();
    expect(toast.success).toHaveBeenCalledWith("Grant updated"); expect(toast.error).not.toHaveBeenCalled();
    expect(await screen.findByText(/The grant was saved, but current permissions could not be refreshed/)).toBeInTheDocument();
    select("b"); expect(save()).toBeDisabled();
  });

  it.each(["unchanged revision", "malformed snapshot"])("refuses %s as proof of the completed save's current state", async kind => {
    await mount(); select("a");
    readReply = () => json(kind === "unchanged revision" ? snapshot(12) : { revision: 13, grants: null });
    await act(async () => fireEvent.click(save()));
    expect(toast.success).toHaveBeenCalledWith("Grant updated");
    expect(await screen.findByText(/The grant was saved, but current permissions could not be refreshed/)).toBeInTheDocument();
    select("b"); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("does not release a pending save's review latch from a member reselection before readback", async () => {
    await mount(); select("a"); const pending = deferred(json(snapshot(13))); readReply = () => pending.promise;
    fireEvent.click(save()); await waitFor(() => expect(gets()).toHaveLength(2));
    select("b"); expect(save()).toBeDisabled(); expect(toast.success).toHaveBeenCalledWith("Grant updated");
    await act(async () => pending.release());
    expect(member()).toHaveValue("b"); expect(save()).toBeDisabled(); expect(puts()).toHaveLength(1);
  });

  it("an unmounted editor cannot refresh or report a late save success", async () => {
    const pending = await pendingSave(); pending.unmount();
    await act(async () => pending.release());
    expect(pending.refresh).not.toHaveBeenCalled(); expect(gets()).toHaveLength(1);
    expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("changing mailboxes retires an old save without clearing or blocking the new editor", async () => {
    const pending = await pendingSave(); fireEvent.click(screen.getByRole("button", { name: "Other mailbox" }));
    await waitFor(() => expect(gets().some(call => call.path.includes("/other-box/"))).toBe(true));
    select("b"); expect(save()).toBeEnabled();
    await act(async () => pending.release());
    expect(member()).toHaveValue("b"); expect(save()).toBeEnabled();
    expect(pending.refresh).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
  });

  it("a new session owns its own busy state even while the retired transport remains pending", async () => {
    const pending = await pendingSave();
    await act(async () => identity("replacement-admin"));
    await waitFor(() => expect(gets()).toHaveLength(2));
    select("b"); expect(save()).toBeEnabled(); expect(puts()[0].signal?.aborted).toBe(true);
    await act(async () => pending.release());
    expect(member()).toHaveValue("b"); expect(save()).toBeEnabled();
    expect(pending.refresh).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled();
  });

  it("an old post-save readback cannot refresh the replacement mailbox or report there", async () => {
    const mounted = await mount(); select("a"); const pending = deferred(json(snapshot(13))); readReply = () => pending.promise;
    fireEvent.click(save()); await waitFor(() => expect(gets()).toHaveLength(2));
    vi.mocked(toast.success).mockClear(); readReply = undefined;
    fireEvent.click(screen.getByRole("button", { name: "Other mailbox" }));
    await waitFor(() => expect(gets()).toHaveLength(3)); select("b");
    await act(async () => pending.release());
    expect(member()).toHaveValue("b"); expect(save()).toBeEnabled();
    expect(mounted.refresh).not.toHaveBeenCalled(); expect(toast.success).not.toHaveBeenCalled(); expect(toast.error).not.toHaveBeenCalled();
  });

  it("same-event duplicate clicks still issue only one revision-bound PUT", async () => {
    const pending = await pendingSave(); fireEvent.click(save()); fireEvent.click(save());
    expect(puts()).toHaveLength(1); await acknowledge(pending.release);
  });

  it("a confirmed rejection preserves the unsaved grant and never creates a success receipt", async () => {
    await mount(); select("a"); fireEvent.click(screen.getByLabelText("Send as")); writeReply = () => rejected(409);
    await act(async () => fireEvent.click(save()));
    expect(member()).toHaveValue("a"); expect(screen.getByLabelText("Send as")).toBeChecked();
    expect(save()).toBeDisabled(); expect(toast.success).not.toHaveBeenCalled(); expect(gets()).toHaveLength(1);
  });

  it("a truncated successful response preserves intent and requires review instead of blind replay", async () => {
    await mount(); select("a");
    writeReply = () => new Response('{"data":', { headers: { "Content-Type": "application/json" } });
    await act(async () => fireEvent.click(save()));
    expect(member()).toHaveValue("a"); expect(save()).toBeDisabled();
    expect(await screen.findByText(/Could not confirm the grant save result/)).toBeInTheDocument();
    expect(toast.success).not.toHaveBeenCalled(); fireEvent.click(save()); expect(puts()).toHaveLength(1);
  });
});
