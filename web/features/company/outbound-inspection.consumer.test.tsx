import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import "@testing-library/jest-dom/vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { AUTH_EVENT, installSession } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import RecoveryPage from "@/app/(dashboard)/company/recovery/page";

// Contract fixture follows ProjectOutboundInspection's JSON tags and the
// shipping HTTP response envelope. Only fetch is replaced; AuthProvider,
// company(), request() and the session lease are the production implementations.
const tenantA = "10000000-0000-4000-8000-000000000001";
const tenantB = "10000000-0000-4000-8000-000000000002";
const jobId = "30000000-0000-4000-8000-000000000001";
const otherJob = "30000000-0000-4000-8000-000000000002";
const date = "2026-10-01T10:00:00Z";
const canary = "INTERNAL-SECRET-CANARY-NOT-CONTENT";
const reason = "Controlled outbound inspection verification";
const actor = (role: AuthUser["role"] = "super_admin", id = "20000000-0000-4000-8000-000000000001"): AuthUser => ({
  id, tenant_id: tenantA, email: "ops@fixture.test", display_name: "Ops", role,
});
function payload() {
  return { job: { id: jobId, tenant_id: tenantA, state: "sent", status: "partially_accepted",
    created_at: date, updated_at: date, mail_from: "sender@fixture.test",
    to: ["visible@fixture.test"], cc: [] as string[], bcc: ["private@fixture.test"],
    subject: "inspection-subject", text_body: "inspection-body", html_body: "<p>inspection-body</p>",
    headers: { From: "sender@fixture.test", Subject: "inspection-subject", Date: "Thu, 01 Oct 2026 10:00:00 +0000", "Content-Type": "text/plain; charset=utf-8" } },
    recipients: [
      { address: "visible@fixture.test", kind: "to", state: "accepted", attempts: 1,
        smtp_code: 250, enhanced_code: "5.1.1", diagnostic_class: "accepted", updated_at: date },
      { address: "private@fixture.test", kind: "bcc", state: "permanent", attempts: 1,
        smtp_code: 550, enhanced_code: "5.1.1", diagnostic_class: "permanent", updated_at: date },
    ] };
}
type Call = { path: string; method: string; body: unknown; tenant: string | null;
  authorization: string | null; signal: AbortSignal | null | undefined };
let calls: Call[];
let inspectionReply: (call: Call) => Promise<Response>;
let refreshReply: () => Promise<Response>;
const reply = (data: unknown, status = 200) => new Response(JSON.stringify({ data }), {
  status, headers: { "Content-Type": "application/json" },
});
const inspectionCalls = () => calls.filter(c => c.path.endsWith("/inspect"));
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(r => { resolve = r; });
  return { promise, resolve };
}
function SessionControls() {
  const auth = useAuth();
  return <>
    <output data-testid="auth-scope">{auth.level}:{auth.tenantId}</output>
    <button onClick={() => auth.setTenantId(tenantB)}>Switch company</button>
    <button onClick={() => auth.loginWithTokens("fresh-token", actor("super_admin", "20000000-0000-4000-8000-000000000002"))}>Switch session</button>
    <button onClick={() => auth.logout()}>Logout</button>
  </>;
}
const mount = () => render(<AuthProvider><SessionControls /><RecoveryPage /></AuthProvider>);
beforeEach(() => {
  calls = [];
  inspectionReply = async () => reply(payload());
  refreshReply = async () => reply({ access_token: "rotated-token" });
  installSession("old-token", actor());
  Object.defineProperty(navigator, "locks", { configurable: true,
    value: { request: async (_name: string, run: () => unknown) => run() } });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const headers = new Headers(init?.headers);
    const call: Call = { path, method: init?.method ?? "GET", body: init?.body ? JSON.parse(String(init.body)) : undefined,
      tenant: headers.get("X-Tenant-ID"), authorization: headers.get("Authorization"), signal: init?.signal };
    calls.push(call);
    if (path.endsWith("/inspect")) return inspectionReply(call);
    if (path === "/api/v1/auth/refresh") return refreshReply();
    if (path === "/api/v1/company/recovery") return new Response(JSON.stringify({ data: [], meta: { total: 0 } }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/admin/runtime-config") return reply({});
    if (path === "/api/v1/auth/me/permissions" || path === "/api/v1/auth/logout") return reply({});
    if (path.endsWith("/reconcile")) return reply({ reconciled: true });
    throw new Error(`Unexpected inspection request: ${call.method} ${path}`);
  });
});
afterEach(() => { cleanup(); vi.unstubAllGlobals(); });
async function inspect(id = jobId) {
  const count = inspectionCalls().length;
  await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent("super_admin:"));
  fireEvent.change(screen.getByLabelText("Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)"), { target: { value: reason } });
  fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: id } });
  fireEvent.click(screen.getByRole("button", { name: "Audit and inspect" }));
  await waitFor(() => expect(inspectionCalls()).toHaveLength(count + 1));
}
const result = () => screen.queryByLabelText("Audited outbound inspection result");
const settled = () => waitFor(() => expect(screen.getByRole("button", { name: "Audit and inspect" })).toBeEnabled());

describe("audited outbound inspection shipping consumer", () => {
  it("renders the safe recipient class/code and next-hop acceptance, not a raw diagnostic", async () => {
    mount(); await inspect();
    await screen.findByRole("heading", { name: "inspection-subject" });
    expect(screen.getAllByText(/5\.1\.1/).length).toBeGreaterThan(0);
    expect(screen.getAllByText(/Next-hop accepted/).length).toBeGreaterThan(0);
    expect(document.body).not.toHaveTextContent(canary);
    expect(inspectionCalls()[0]).toMatchObject({ method: "POST", tenant: tenantA,
      authorization: "Bearer old-token", body: { reason } });
  });

  it("renders subject, structured recipients, bodies and safe headers without executing untrusted HTML", async () => {
    const wire = payload();
    wire.job.cc = ["copy@fixture.test"];
    wire.job.html_body = '<img src="https://remote.fixture.test/track" onerror="window.inspectionExecuted=true"><script>window.inspectionExecuted=true</script>';
    inspectionReply = async () => reply(wire);
    mount(); await inspect(); await screen.findByRole("heading", { name: wire.job.subject });
    expect(result()).toHaveTextContent(wire.job.text_body);
    expect(result()).toHaveTextContent(wire.job.html_body);
    expect(result()).toHaveTextContent("copy@fixture.test");
    expect(result()).toHaveTextContent("private@fixture.test");
    expect(result()).toHaveTextContent("Bcc (this audited exception only)");
    expect(result()).toHaveTextContent("Content-Type: text/plain; charset=utf-8");
    expect(result()).toHaveTextContent("Thu, 01 Oct 2026 10:00:00 +0000");
    expect(result()!.querySelector("iframe, img, script")).toBeNull();
    expect(Reflect.get(window, "inspectionExecuted")).toBeUndefined();
    expect(calls.some(call => call.path === "/track")).toBe(false);
  });

  for (const location of ["job", "recipient", "header", "top-level"] as const) {
    it(`rejects ${location} raw/private extensions without rendering body, BCC or secret canary`, async () => {
      const wire = payload();
      if (location === "job") Object.assign(wire.job, { last_error: canary, delivery_token: canary, smtp_response: canary });
      else if (location === "recipient") Object.assign(wire.recipients[0], { diagnostic: canary });
      else if (location === "header") Object.assign(wire.job.headers, { "Content-Secret": canary, "DKIM-Signature": canary });
      else Object.assign(wire, { debug: canary });
      inspectionReply = async () => reply(wire);
      mount(); await inspect(); await settled();
      expect(result()).not.toBeInTheDocument();
      expect(document.body).not.toHaveTextContent(wire.job.text_body);
      expect(document.body).not.toHaveTextContent("private@fixture.test");
      expect(document.body).not.toHaveTextContent(canary);
    });
  }

  it("renders fixed unknown-category fallbacks without importing raw enum strings or enabling reconciliation", async () => {
    const wire = payload();
    wire.job.state = wire.job.status = canary;
    wire.recipients.forEach(r => { r.kind = r.state = r.diagnostic_class = canary; });
    inspectionReply = async () => reply(wire);
    mount(); await inspect(); await screen.findByRole("heading", { name: wire.job.subject });
    expect(result()).toHaveTextContent("Unknown category; review required");
    expect(result()).not.toHaveTextContent(canary);
    expect(screen.queryByRole("combobox")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: "Save evidenced outcomes" })).toBeDisabled();
  });

  for (const field of ["status", "bcc", "headers", "html_body", "kind", "diagnostic_class"] as const) {
    it(`fails closed instead of reusing an old DTO when mandatory ${field} is absent`, async () => {
      const wire = payload();
      if (field === "kind" || field === "diagnostic_class") Reflect.deleteProperty(wire.recipients[0], field);
      else Reflect.deleteProperty(wire.job, field);
      inspectionReply = async () => reply(wire);
      mount(); await inspect(); await settled();
      expect(result()).not.toBeInTheDocument();
      expect(document.body).not.toHaveTextContent("inspection-body");
    });
  }

  for (const field of ["id", "tenant_id"] as const) {
    it(`rejects a successful response with the wrong ${field} before disclosing any content`, async () => {
      const wire = payload(); wire.job[field] = field === "id" ? otherJob : tenantB;
      inspectionReply = async () => reply(wire);
      mount(); await inspect(); await settled();
      expect(result()).not.toBeInTheDocument();
      expect(document.body).not.toHaveTextContent("inspection-body");
      expect(document.body).not.toHaveTextContent("private@fixture.test");
    });
  }

  it("uses the actively selected company, not the super administrator's home company", async () => {
    const wire = payload(); wire.job.tenant_id = tenantB;
    inspectionReply = async () => reply(wire);
    mount(); await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent("super_admin:"));
    fireEvent.click(screen.getByRole("button", { name: "Switch company" }));
    await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent(`super_admin:${tenantB}`));
    await inspect(); await screen.findByRole("heading", { name: wire.job.subject });
    expect(inspectionCalls()[0]).toMatchObject({ tenant: tenantB, authorization: "Bearer old-token" });
    expect(result()).toHaveTextContent(`${jobId} / ${tenantB}`);
  });

  it("discards an old target's late response even if the input is changed away and back", async () => {
    const late = deferred<Response>();
    inspectionReply = async () => late.promise;
    mount(); await inspect();
    const input = screen.getByLabelText("Outbound job ID");
    fireEvent.change(input, { target: { value: otherJob } });
    fireEvent.change(input, { target: { value: jobId } });
    await act(async () => { late.resolve(reply(payload())); await late.promise; });
    await settled();
    expect(result()).not.toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("inspection-body");
  });

  for (const event of ["company", "session", "logout"] as const) {
    const control = event === "company" ? "Switch company" : event === "session" ? "Switch session" : "Logout";
    it(`${event} changes clear already loaded inspection body/BCC and do not replay its POST`, async () => {
      mount(); await inspect(); await screen.findByRole("heading", { name: "inspection-subject" });
      fireEvent.click(screen.getByRole("button", { name: control }));
      await waitFor(() => expect(result()).not.toBeInTheDocument());
      expect(document.body).not.toHaveTextContent("inspection-body");
      expect(document.body).not.toHaveTextContent("private@fixture.test");
      expect(screen.getByLabelText("Outbound job ID")).toHaveValue("");
      expect(inspectionCalls()).toHaveLength(1);
    });

    for (const phase of ["inspect", "reconcile"] as const) {
      it(`${event} changes fence a late ${phase} POST with the production session lease`, async () => {
        const late = deferred<Response>();
        const wire = payload();
        wire.job.state = "failed"; wire.job.status = "needs_attention";
        wire.recipients[0].state = wire.recipients[0].diagnostic_class = "uncertain";
        inspectionReply = async () => phase === "inspect" && inspectionCalls().length === 1 ? late.promise : reply(wire);
        const originalFetch = globalThis.fetch;
        if (phase === "reconcile") vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
          if (new URL(String(input), "http://localhost").pathname.endsWith("/reconcile")) {
            const headers = new Headers(init?.headers);
            calls.push({ path: `/api/v1/company/outbound/${jobId}/reconcile`, method: init?.method ?? "GET",
              body: JSON.parse(String(init?.body)), tenant: headers.get("X-Tenant-ID"), authorization: headers.get("Authorization"), signal: init?.signal });
            return late.promise;
          }
          return originalFetch(input, init);
        });
        mount(); await inspect();
        if (phase === "reconcile") {
          await screen.findByRole("heading", { name: wire.job.subject });
          fireEvent.change(screen.getByRole("combobox", { name: "Confirm outcome visible@fixture.test" }), { target: { value: "accepted" } });
          fireEvent.click(screen.getByRole("button", { name: "Save evidenced outcomes" }));
          await waitFor(() => expect(calls.some(c => c.path.endsWith("/reconcile"))).toBe(true));
        }
        const pending = calls.find(c => c.path.endsWith(`/${phase}`))!;
        expect(pending).toMatchObject({ tenant: tenantA, authorization: "Bearer old-token", method: "POST" });
        fireEvent.click(screen.getByRole("button", { name: control }));
        await waitFor(() => expect(screen.getByLabelText("Outbound job ID")).toHaveValue(""));
        await waitFor(() => expect(pending.signal?.aborted).toBe(true));
        const response = reply(phase === "inspect" ? wire : { reconciled: true });
        const consumeBody = vi.spyOn(response, "json");
        await act(async () => { late.resolve(response); await late.promise; });
        expect(consumeBody).not.toHaveBeenCalled();
        expect(result()).not.toBeInTheDocument();
        expect(document.body).not.toHaveTextContent("inspection-body");
        expect(document.body).not.toHaveTextContent("private@fixture.test");
        expect(inspectionCalls()).toHaveLength(1);
        expect(calls.filter(c => c.path.endsWith("/reconcile"))).toHaveLength(phase === "reconcile" ? 1 : 0);
      });
    }
  }

  it("retains same-scope loaded results through token rotation without replaying the audited POST", async () => {
    mount(); await inspect(); await screen.findByRole("heading", { name: "inspection-subject" });
    await act(async () => {
      localStorage.setItem("tabmail_access_token", "rotated-token");
      window.dispatchEvent(new Event(AUTH_EVENT));
    });
    expect(result()).toHaveTextContent("inspection-body");
    expect(inspectionCalls()).toHaveLength(1);
  });

  it("accepts a same-scope late inspection through token rotation without aborting or replaying it", async () => {
    const late = deferred<Response>(); inspectionReply = async () => late.promise;
    mount(); await inspect();
    await act(async () => {
      localStorage.setItem("tabmail_access_token", "rotated-token");
      window.dispatchEvent(new Event(AUTH_EVENT));
    });
    expect(inspectionCalls()[0].signal?.aborted).toBe(false);
    await act(async () => { late.resolve(reply(payload())); await late.promise; });
    await screen.findByRole("heading", { name: "inspection-subject" });
    expect(inspectionCalls()).toHaveLength(1);
  });

  it("requires an explicit review/resubmit after 401 refresh; does not automatically replay inspection", async () => {
    inspectionReply = async () => inspectionCalls().length === 1
      ? new Response(JSON.stringify({ error: { code: "UNAUTHORIZED", message: "Expired" } }), { status: 401, headers: { "Content-Type": "application/json" } })
      : reply(payload());
    mount(); await inspect(); await settled();
    expect(calls.filter(c => c.path === "/api/v1/auth/refresh")).toHaveLength(1);
    expect(inspectionCalls()).toHaveLength(1);
    expect(result()).not.toBeInTheDocument();
    fireEvent.click(screen.getByRole("button", { name: "Audit and inspect" }));
    await screen.findByRole("heading", { name: "inspection-subject" });
    expect(inspectionCalls()).toHaveLength(2);
    expect(inspectionCalls()[1]).toMatchObject({ tenant: tenantA, authorization: "Bearer rotated-token" });
  });

  for (const role of ["admin", "user"] as const) {
    it(`does not enable the audited body exception for ordinary ${role} rows`, async () => {
      installSession("ordinary-token", actor(role));
      mount(); await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent(`${role}:`));
      fireEvent.change(screen.getByLabelText("Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)"), { target: { value: reason } });
      fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: jobId } });
      const button = screen.getByRole("button", { name: "Audit and inspect" });
      expect(button).toBeDisabled(); fireEvent.click(button);
      expect(inspectionCalls()).toHaveLength(0);
      expect(result()).not.toBeInTheDocument();
    });
  }

  it("enforces the trimmed reason byte boundary and valid job ID before issuing an inspection", async () => {
    mount(); await waitFor(() => expect(screen.getByTestId("auth-scope")).toHaveTextContent("super_admin:"));
    const button = screen.getByRole("button", { name: "Audit and inspect" });
    const input = screen.getByLabelText("Inspection / recovery reason (8–1000 UTF-8 bytes after trimming)");
    fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: jobId } });
    for (const bad of ["short", "        ", "x".repeat(1001), "核查原因".repeat(84)]) {
      fireEvent.change(input, { target: { value: bad } }); expect(button).toBeDisabled();
    }
    fireEvent.change(input, { target: { value: "x".repeat(1000) } }); expect(button).toBeEnabled();
    fireEvent.change(screen.getByLabelText("Outbound job ID"), { target: { value: "not-a-job-id" } }); expect(button).toBeDisabled();
    expect(inspectionCalls()).toHaveLength(0);
  });

  it("renders the accepted job status as next-hop acceptance, not final delivery", async () => {
    const wire = payload(); wire.job.status = "accepted";
    wire.recipients = [wire.recipients[0]];
    wire.job.bcc = [];
    inspectionReply = async () => reply(wire);
    mount(); await inspect(); await screen.findByRole("heading", { name: wire.job.subject });
    expect(result()).toHaveTextContent("sender@fixture.test · Next-hop accepted (not final delivery)");
    expect(result()).not.toHaveTextContent("Delivered");
  });

  it("a denied response cannot import success-shaped data or an inspection body", async () => {
    inspectionReply = async () => new Response(JSON.stringify({ data: payload(), error: { code: "FORBIDDEN", message: "Denied" } }),
      { status: 403, headers: { "Content-Type": "application/json" } });
    mount(); await inspect(); await settled();
    expect(result()).not.toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("inspection-body");
    expect(document.body).not.toHaveTextContent("private@fixture.test");
  });

  it("reinspection clears a previous safe result before rejecting malformed replacement data", async () => {
    mount(); await inspect(); await screen.findByRole("heading", { name: "inspection-subject" });
    inspectionReply = async () => reply({ ...payload(), debug: canary });
    fireEvent.click(screen.getByRole("button", { name: "Audit and inspect" }));
    await waitFor(() => expect(inspectionCalls()).toHaveLength(2)); await settled();
    expect(result()).not.toBeInTheDocument();
    expect(document.body).not.toHaveTextContent("inspection-body");
    expect(document.body).not.toHaveTextContent(canary);
  });

  it("renders missing optional enhanced code as absent, never falls back to raw diagnostic", async () => {
    const wire = payload(); wire.recipients.forEach(r => Reflect.deleteProperty(r, "enhanced_code"));
    inspectionReply = async () => reply(wire);
    mount(); await inspect(); await screen.findByRole("heading", { name: wire.job.subject });
    expect(result()).toHaveTextContent("Safe diagnostic class: Next-hop accepted (not final delivery) · SMTP 250");
    expect(result()).not.toHaveTextContent("5.1.1");
    expect(result()).not.toHaveTextContent("undefined");
  });
});
