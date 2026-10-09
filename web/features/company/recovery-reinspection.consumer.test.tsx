import React from "react";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { toast } from "sonner";
import { AuthProvider } from "@/contexts/auth-context";
import { installSession } from "@/lib/session";
import type { Receipt } from "@/lib/company";
import RecoveryPage from "@/app/(dashboard)/company/recovery/page";

// Mount the actual recovery page, auth/session boundary, SWR and request client.
// Only fetch and toast delivery are synthetic. No real recovery is dispatched.
vi.mock("sonner", () => ({ toast: { success: vi.fn(), error: vi.fn() } }));
type Kind = "inbound" | "outbound";
type Call = { path: string; method: string; body?: Record<string, unknown> };
const tenant = "10000000-0000-4000-8000-000000000001";
const receiptId = "30000000-0000-4000-8000-000000000001";
const jobId = "30000000-0000-4000-8000-000000000002";
const otherJob = "30000000-0000-4000-8000-000000000003";
const target = "40000000-0000-4000-8000-000000000001";
const freshTarget = "40000000-0000-4000-8000-000000000002";
const oldVersion = "2026-10-07T00:00:00Z";
const freshVersion = "2026-10-07T00:01:00Z";
const reason = "Verified recovery operator evidence";
const reasonLabel = "Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)";
const required = {
  inbound: "Recovery state changed. Inspect the original and targets again before retrying.",
  outbound: "Delivery state changed. Inspect this job again before saving evidenced outcomes.",
};
const actionLabel = { inbound: "Retry only selected unfinished targets", outbound: "Save evidenced outcomes" };
function inbound(version = oldVersion, mailboxId = target): { receipt: Receipt; original_valid: boolean } {
  return { original_valid: true, receipt: { id: receiptId, state: "held", updated_at: version, raw_size: 64,
    targets: [{ mailbox_id: mailboxId, address: "recover@fixture.test", state: "failed" }] } };
}
function outbound(version = oldVersion) {
  return { job: { id: jobId, tenant_id: tenant, state: "sent", status: "needs_attention", created_at: oldVersion,
    updated_at: version, mail_from: "sender@fixture.test", to: ["uncertain@fixture.test"], cc: [], bcc: [],
    subject: "Inspected message", text_body: "Inspected body", html_body: "", headers: {} },
    recipients: [{ address: "uncertain@fixture.test", kind: "to", state: "uncertain", attempts: 1,
      smtp_code: 0, diagnostic_class: "uncertain", updated_at: version }] };
}
const response = (data: unknown) => new Response(JSON.stringify({ data }), { headers: { "Content-Type": "application/json" } });
const failure = (code: string, status: number) => new Response(JSON.stringify({ error: { code, message: `Synthetic ${code}` } }), {
  status, headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
let calls: Call[];
let inspection: (kind: Kind) => Promise<Response>;
let command: (kind: Kind) => Promise<Response>;
const commands = () => calls.filter(call => /\/(retry|reconcile)$/.test(call.path));
const inspections = () => calls.filter(call => call.path.endsWith("/inspect"));
beforeEach(() => {
  calls = [];
  inspection = async kind => response(kind === "inbound" ? inbound() : outbound());
  command = async () => failure("CONFLICT", 409);
  installSession("synthetic-operator-token", { id: "20000000-0000-4000-8000-000000000001", tenant_id: tenant,
    email: "operator@fixture.test", display_name: "Operator", role: "super_admin" });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    calls.push({ path, method: init?.method ?? "GET", body: typeof init?.body === "string" ? JSON.parse(init.body) : undefined });
    if (path === "/api/v1/auth/me/permissions" || path === "/api/v1/admin/runtime-config") return response({});
    if (path === "/api/v1/company/recovery") return new Response(JSON.stringify({ data: [inbound().receipt], meta: { total: 1 } }), {
      headers: { "Content-Type": "application/json" },
    });
    if (path.endsWith("/inspect")) return inspection(path.includes("/outbound/") ? "outbound" : "inbound");
    if (/\/(retry|reconcile)$/.test(path)) return command(path.includes("/outbound/") ? "outbound" : "inbound");
    throw new Error(`Unexpected synthetic request: ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function mount() {
  render(<AuthProvider><RecoveryPage /></AuthProvider>);
  await screen.findByRole("button", { name: "Inspect original and targets" });
  fireEvent.change(screen.getByLabelText(reasonLabel), { target: { value: reason } });
}
async function inspect(kind: Kind) {
  if (kind === "outbound") fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: jobId } });
  const button = screen.getByRole("button", { name: kind === "inbound" ? "Inspect original and targets" : "Audit and inspect" });
  await waitFor(() => expect(button).toBeEnabled());
  fireEvent.click(button);
  await screen.findByRole("button", { name: actionLabel[kind] });
  await waitFor(() => expect(button).toBeEnabled());
}
function select(kind: Kind) {
  if (kind === "inbound") fireEvent.click(screen.getByRole("checkbox"));
  else fireEvent.change(screen.getByLabelText("Confirm outcome uncertain@fixture.test"), { target: { value: "accepted" } });
}
async function submit(kind: Kind) {
  const before = commands().length;
  fireEvent.click(screen.getByRole("button", { name: actionLabel[kind] }));
  await waitFor(() => expect(commands()).toHaveLength(before + 1));
  await waitFor(() => expect(screen.getByRole("button", { name: kind === "inbound" ? "Inspect original and targets" : "Audit and inspect" })).toBeEnabled());
}

describe("Recovery inspection conflicts require explicit reinspection", () => {
  it.each(["inbound", "outbound"] as const)("retires the %s inspection and choices after a conflict, preserving the reason", async kind => {
    await mount(); await inspect(kind); select(kind); await submit(kind);
    expect(screen.queryByRole("button", { name: actionLabel[kind] })).not.toBeInTheDocument();
    expect(screen.getByText(required[kind])).toHaveAttribute("role", "alert");
    expect(screen.getByLabelText(reasonLabel)).toHaveValue(reason);
    expect(screen.queryByRole("checkbox")).not.toBeInTheDocument();
    expect(screen.queryByLabelText("Audited outbound inspection result")).not.toBeInTheDocument();
    expect(commands()).toHaveLength(1); expect(inspections()).toHaveLength(1);
    expect(commands()[0].body).toMatchObject({ updated_at: oldVersion, reason });
    expect(toast.success).not.toHaveBeenCalled();
  });

  it.each(["inbound", "outbound"] as const)("requires a successful new %s inspection and fresh selection before another write", async kind => {
    await mount(); await inspect(kind); select(kind); await submit(kind);
    expect(screen.queryByRole("button", { name: actionLabel[kind] })).not.toBeInTheDocument();
    const count = inspections().length;
    inspection = async () => failure("FORBIDDEN", 403);
    fireEvent.click(screen.getByRole("button", { name: kind === "inbound" ? "Inspect original and targets" : "Audit and inspect" }));
    await waitFor(() => expect(inspections()).toHaveLength(count + 1));
    await waitFor(() => expect(toast.error).toHaveBeenCalledWith("Synthetic FORBIDDEN"));
    expect(screen.queryByRole("button", { name: actionLabel[kind] })).not.toBeInTheDocument();
    expect(screen.getByText(required[kind])).toBeInTheDocument();
    expect(commands()).toHaveLength(1);

    inspection = async item => response(item === "inbound" ? inbound(freshVersion, freshTarget) : outbound(freshVersion));
    command = async () => response({ queued: true, reconciled: true });
    // Reinspect the same target without changing the reason or reusing choices.
    fireEvent.click(screen.getByRole("button", { name: kind === "inbound" ? "Inspect original and targets" : "Audit and inspect" }));
    await screen.findByRole("button", { name: actionLabel[kind] });
    await waitFor(() => expect(screen.getByRole("button", { name: kind === "inbound" ? "Inspect original and targets" : "Audit and inspect" })).toBeEnabled());
    expect(screen.getByRole("button", { name: actionLabel[kind] })).toBeDisabled();
    expect(screen.queryByText(required[kind])).not.toBeInTheDocument();
    select(kind); await submit(kind);
    expect(commands()[1].body).toMatchObject({ updated_at: freshVersion, reason });
    if (kind === "inbound") expect(commands()[1].body?.targets).toEqual([freshTarget]);
    else expect(commands()[1].body?.results).toEqual([{ address: "uncertain@fixture.test", state: "accepted" }]);
    expect(commands()).toHaveLength(2);
  });

  it.each(["inbound", "outbound"] as const)("does not auto-replay a denied or unknown %s write and preserves the operator reason", async kind => {
    await mount(); await inspect(kind); select(kind);
    command = async () => failure("FORBIDDEN", 403);
    await submit(kind);
    expect(toast.error).toHaveBeenCalledWith("Synthetic FORBIDDEN");
    expect(screen.getByLabelText(reasonLabel)).toHaveValue(reason);
    expect(commands()).toHaveLength(1); expect(inspections()).toHaveLength(1);
    command = async () => { throw new TypeError("Synthetic offline"); };
    await submit(kind);
    expect(toast.error).toHaveBeenCalledWith("Synthetic offline");
    expect(screen.getByLabelText(reasonLabel)).toHaveValue(reason);
    expect(commands()).toHaveLength(2); expect(inspections()).toHaveLength(1);
    expect(toast.success).not.toHaveBeenCalled();
  });

  it("does not label a newly entered outbound job with an older reconciliation conflict", async () => {
    await mount(); await inspect("outbound"); select("outbound");
    const pending = deferred<Response>(); command = () => pending.promise;
    fireEvent.click(screen.getByRole("button", { name: actionLabel.outbound }));
    await waitFor(() => expect(commands()).toHaveLength(1));
    fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: otherJob } });
    await act(async () => { pending.resolve(failure("CONFLICT", 409)); });
    expect(screen.getByLabelText("Outbound job ID")).toHaveValue(otherJob);
    expect(screen.queryByText(required.outbound)).not.toBeInTheDocument();
    expect(screen.queryByRole("button", { name: actionLabel.outbound })).not.toBeInTheDocument();
    expect(commands()).toHaveLength(1);
  });
});
