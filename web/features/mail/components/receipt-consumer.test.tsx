import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { useSWRConfig } from "swr";
import { AuthProvider, useAuth } from "@/contexts/auth-context";
import { AUTH_EVENT, installSession, sessionScope } from "@/lib/session";
import type { AuthUser } from "@/lib/types";
import { legacyOutboundAttempts, legacyOutboundRecipients, retryOutboundReceipt } from "@/lib/legacy-outbound";
import { ReceiptFolder } from "./receipt-folder";
import { LegacyReceiptFolder } from "./legacy-receipt-folder";
import { SubmissionPane } from "./submission-pane";
import { SubmissionContentView } from "./submission-content";
import { SentFolder } from "./sent-folder";

// Source-prepared tests: only fetch is substituted. Real AuthProvider + SWR +
// locale providers (vitest.setup), transport, runtime parsers and components.
const tenantA = "10000000-0000-4000-8000-000000000001";
const tenantB = "10000000-0000-4000-8000-000000000002";
const actorId = "20000000-0000-4000-8000-000000000001";
const job = "30000000-0000-4000-8000-000000000001";
const mailboxId = "40000000-0000-4000-8000-000000000001";
const date = "2026-10-02T01:02:03Z";
const privateCanary = "RAW-JOB-PRIVATE-CANARY";
const actor = (id = actorId): AuthUser => ({ id, tenant_id: tenantA, email: "current@fixture.test", display_name: "Reader", role: "user" });
const receipt = (tenant = tenantA) => ({ id: job, tenant_id: tenant, state: "sent", status: "partially_accepted",
  progress: { completeness: "known", counts: { total: 2, accepted: 1, pending: 0, temporary: 0, permanent: 1, uncertain: 0 } },
  created_at: date, delivery_uncertain: false, capabilities: { view_content: true, retry: false, retry_block_reason: "state_not_retryable" } });
const live = () => ({ id: job, subject: "original live subject", from: "original-sender@fixture.test", to: ["original-to@fixture.test"],
  cc: ["original-cc@fixture.test"], bcc: ["private-bcc@fixture.test"], recipient_completeness: "complete", created_at: date,
  text_body: "authorized live body", content_redacted: false });
type Call = { path: string; method: string; authorization: string | null; tenant: string | null; signal?: AbortSignal | null };
let calls: Call[];
let receiptReply: (call: Call) => unknown;
let contentReply: (call: Call) => Promise<Response>;
let attachmentReply: (call: Call) => Promise<Response>;
let refreshReply: () => Promise<Response>;
const json = (data: unknown, status = 200) => new Response(JSON.stringify({ data }), { status, headers: { "Content-Type": "application/json" } });
const denied = (status: number) => new Response(JSON.stringify({ error: { code: status === 404 ? "NOT_FOUND" : "FORBIDDEN", message: "Read unavailable" } }), { status, headers: { "Content-Type": "application/json" } });
const contents = () => calls.filter(call => call.path.endsWith("/content"));
const attachments = () => calls.filter(call => call.path.endsWith("/attachments"));
function controlledJSON(data: unknown) {
  const text = JSON.stringify({ data }), middle = Math.floor(text.length / 2);
  let stream!: ReadableStreamDefaultController<Uint8Array>;
  const body = new ReadableStream<Uint8Array>({ start(controller) { stream = controller; } });
  return { response: new Response(body, { headers: { "Content-Type": "application/json" } }),
    first: () => stream.enqueue(new TextEncoder().encode(text.slice(0, middle))),
    finish: () => { stream.enqueue(new TextEncoder().encode(text.slice(middle))); stream.close(); } };
}
function Controls() {
  const auth = useAuth();
  const { mutate } = useSWRConfig();
  return <>
    <button onClick={() => auth.setTenantId(tenantB)}>Switch company</button>
    <button onClick={() => auth.loginWithTokens("new-session-token", actor("20000000-0000-4000-8000-000000000002"))}>Switch account</button>
    <button onClick={() => auth.logout()}>Logout</button>
    <button onClick={() => void mutate(["session", sessionScope(), ["submission", job]])}>Refresh receipt</button>
    <button onClick={() => { localStorage.setItem("tabmail_access_token", "rotated-token"); window.dispatchEvent(new Event(AUTH_EVENT)); }}>Rotate token only</button>
  </>;
}
const mount = (children: React.ReactNode = <SubmissionPane id={job} />) => render(<AuthProvider><Controls />{children}</AuthProvider>);
beforeEach(() => {
  installSession("receipt-token", actor());
  calls = [];
  receiptReply = call => receipt(call.tenant ?? tenantA);
  contentReply = async () => json(live());
  attachmentReply = async () => json([{ id: mailboxId, filename: "private-attachment.txt", content_type: "text/plain", size: 42, state: "ready" }]);
  refreshReply = async () => denied(401);
  Object.defineProperty(navigator, "locks", { configurable: true, value: { request: async (_name: string, run: () => unknown) => run() } });
  vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
    const path = new URL(String(input), "http://localhost").pathname;
    const headers = new Headers(init?.headers);
    const call: Call = { path, method: init?.method ?? "GET", authorization: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID"), signal: init?.signal };
    calls.push(call);
    if (path === "/api/v1/auth/refresh") return refreshReply();
    if (path === "/api/v1/auth/logout") return json({});
    if (path === "/api/v1/auth/me/permissions") return json({ can_send: false, daily_send_quota: 0, daily_receive_quota: 100, max_mailboxes: 1, max_domains: 1, allowed_zone_ids: null, can_create_domains: false, can_create_routes: false, can_create_api_keys: false });
    if (path.endsWith("/content")) return contentReply(call);
    if (path.endsWith("/attachments")) return attachmentReply(call);
    if (path.endsWith("/sent")) return new Response(JSON.stringify({ data: [{ id: job, mailbox_id: mailboxId, subject: "authorized asset list title", from: "asset-list-sender@fixture.test", to: [], created_at: date, revision: 1, attachment_count: 1, delivery_available: true }], meta: { total: 1, page: 1, per_page: 30 } }), { headers: { "Content-Type": "application/json" } });
    if (path === "/api/v1/company/submissions" || path === "/api/v1/outbound") return new Response(JSON.stringify({ data: [receiptReply(call)], meta: { total: 1, page: 1, per_page: 30 } }), { headers: { "Content-Type": "application/json" } });
    if (path.startsWith(`/api/v1/outbound/${job}`) || path === `/api/v1/company/submissions/${job}`) return json(receiptReply(call));
    throw new Error(`Unexpected ordinary receipt request: ${path}`);
  });
});
afterEach(() => { cleanup(); vi.useRealTimers(); vi.unstubAllGlobals(); Object.defineProperty(navigator, "locks", { configurable: true, value: undefined }); });
const open = async () => fireEvent.click(await screen.findByTestId("receipt-content-disclosure"));

// Counts include the complete ledger, including hidden recipients; no private
// address or kind exists on the receipt side of this boundary.
describe("shipping ordinary receipt consumers", () => {
  it("lists neutral task IDs and full-ledger mixed results without automatic content reads", async () => {
    mount(<ReceiptFolder page={1} selected={job} onSelect={vi.fn()} onPage={vi.fn()} />);
    expect(await screen.findByTestId("receipt-count-total")).toHaveTextContent("2");
    expect(screen.getByTestId("receipt-count-accepted")).toHaveTextContent("1");
    expect(screen.getByTestId("receipt-count-permanent")).toHaveTextContent("1");
    expect(screen.getAllByText(/Accepted by the next hop, not proof of final delivery/).length).toBeGreaterThan(0);
    expect(document.body).not.toHaveTextContent("original live subject");
    expect(document.body).not.toHaveTextContent("original-sender@fixture.test");
    expect(document.body).not.toHaveTextContent("private-bcc@fixture.test");
    expect(contents()).toHaveLength(0);
  });
  it("renders unknown evidence without inventing zero, acceptance or status-based content/retry authority", async () => {
    receiptReply = () => ({ id: job, state: "unknown", status: "needs_attention", progress: { completeness: "unknown" }, delivery_uncertain: false });
    mount();
    expect(await screen.findByText(/Delivery progress unknown/)).toBeInTheDocument();
    expect(screen.queryByTestId("receipt-count-total")).not.toBeInTheDocument();
    expect(screen.queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument();
    expect(screen.queryByTestId("receipt-retry")).not.toBeInTheDocument();
    expect(contents()).toHaveLength(0);
  });
  it("fails closed on old raw-job response extensions rather than displaying/stripping them", async () => {
    receiptReply = call => ({ ...receipt(call.tenant ?? tenantA), subject: privateCanary, mail_from: privateCanary, bcc: [privateCanary] });
    mount();
    await screen.findByText("Invalid or out-of-scope receipt response");
    expect(document.body).not.toHaveTextContent(privateCanary);
    expect(screen.queryByTestId("ordinary-receipt-aggregate")).not.toBeInTheDocument();
    expect(contents()).toHaveLength(0);
  });
  it("uses the same aggregate DTO on compatibility detail, attempts, recipients and retry transports", async () => {
    mount(<LegacyReceiptFolder initialSelected={job} />);
    await screen.findByTestId("ordinary-receipt-aggregate");
    expect(await legacyOutboundAttempts(job)).toMatchObject({ id: job, progress: receipt().progress });
    expect(await legacyOutboundRecipients(job)).toMatchObject({ id: job, progress: receipt().progress });
    expect(await retryOutboundReceipt(job)).toMatchObject({ id: job });
    expect(calls.find(call => call.path.endsWith("/retry"))).toMatchObject({ method: "POST", authorization: "Bearer receipt-token", tenant: tenantA });
    expect(document.body).not.toHaveTextContent("private-bcc@fixture.test");
    expect(contents()).toHaveLength(0);
    await open(); await screen.findByText("authorized live body");
    expect(screen.getByTestId("sent-structured-bcc")).toHaveTextContent("private-bcc@fixture.test");
  });
  it("fetches live body/BCC lazily with the current tenant/token and then attachments", async () => {
    mount(); await open();
    expect(await screen.findByText("authorized live body")).toBeInTheDocument();
    expect(screen.getByText("original live subject")).toBeInTheDocument();
    expect(screen.getByTestId("sent-structured-bcc")).toHaveTextContent("private-bcc@fixture.test");
    expect(document.body).toHaveTextContent("original-sender@fixture.test");
    expect(contents()[0]).toMatchObject({ method: "GET", tenant: tenantA, authorization: "Bearer receipt-token" });
    expect(calls.indexOf(attachments()[0])).toBeGreaterThan(calls.indexOf(contents()[0]));
    expect(attachments()[0].signal).toBeTruthy();
  });
  it.each(["complete", "legacy_unknown"])("shows %s BCC semantics only from the live structured marker", async marker => {
    contentReply = async () => json({ ...live(), bcc: marker === "complete" ? [] : null, recipient_completeness: marker });
    mount(); await open(); await screen.findByText("authorized live body");
    expect(screen.getByTestId("sent-structured-bcc")).toHaveTextContent(marker === "complete" ? "No BCC recipients" : "Historical BCC recipients are unknown, not proof that none were set.");
    expect(document.body).not.toHaveTextContent("private-bcc@fixture.test");
  });
  it("does not display custom headers or accept a custom BCC fallback", async () => {
    contentReply = async () => json({ ...live(), headers: { Bcc: privateCanary }, bcc: null, recipient_completeness: "legacy_unknown" });
    mount(); await open();
    await screen.findByText(/Content and attachments are currently unavailable/);
    expect(document.body).not.toHaveTextContent(privateCanary);
    expect(document.body).not.toHaveTextContent("authorized live body");
    expect(attachments()).toHaveLength(0);
  });
  it("renders sent-folder detail from the current live content, not the queue or list sender", async () => {
    mount(<SentFolder mailbox={{ mailbox: { id: mailboxId, tenant_id: tenantA, zone_id: tenantA, local_part: "original-sender", resolved_domain: "fixture.test", full_address: "original-sender@fixture.test", access_mode: "token", created_at: date, kind: "shared" }, can_read: true, can_send: false, can_organize: false, template_only: false, revision: 1 }} folder="sent" q="" page={1} selected={job} onSelect={vi.fn()} onPage={vi.fn()} />);
    await screen.findByText("authorized live body");
    expect(screen.getByTestId("live-submission-content")).toHaveTextContent("original-sender@fixture.test");
    expect(screen.getByTestId("live-submission-content")).not.toHaveTextContent("asset-list-sender@fixture.test");
    expect(screen.getByTestId("sent-structured-bcc")).toHaveTextContent("private-bcc@fixture.test");
  });
});

describe("live content lifecycle and epoch-bound ReadableStream responses", () => {
  it.each([401, 403, 404])("clears old private body and attachments after final %s during live revalidation", async status => {
    vi.useFakeTimers({ toFake: ["setInterval", "clearInterval"] });
    let reads = 0;
    contentReply = async () => ++reads === 1 ? json(live()) : denied(status);
    mount(<SubmissionContentView id={job} />);
    await screen.findByText("authorized live body");
    expect(screen.getByText(/private-attachment.txt/)).toBeInTheDocument();
    await act(async () => { await vi.advanceTimersByTimeAsync(15000); });
    await screen.findByText(/Content and attachments are currently unavailable/);
    expect(document.body).not.toHaveTextContent("authorized live body");
    expect(document.body).not.toHaveTextContent("private-bcc@fixture.test");
    expect(document.body).not.toHaveTextContent("private-attachment.txt");
  });
  it("capability revocation closes the disclosure, clears private state, and re-grant never auto-opens it", async () => {
    mount(); await open(); await screen.findByText("authorized live body");
    receiptReply = () => ({ ...receipt(), capabilities: { view_content: false, retry: false, retry_block_reason: "sender_authority" } });
    fireEvent.click(screen.getByText("Refresh receipt"));
    await waitFor(() => expect(screen.queryByTestId("receipt-content-disclosure")).not.toBeInTheDocument());
    expect(document.body).not.toHaveTextContent("authorized live body");
    receiptReply = () => receipt();
    fireEvent.click(screen.getByText("Refresh receipt"));
    await screen.findByTestId("receipt-content-disclosure");
    expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "false");
    expect(document.body).not.toHaveTextContent("authorized live body");
  });
  it.each(["Switch company", "Switch account", "Logout"])("aborts partial old JSON and never admits its late body after %s", async control => {
    const delayed = controlledJSON({ ...live(), text_body: "OLD-SCOPE-PRIVATE-BODY" });
    contentReply = async () => delayed.response;
    mount(); await open();
    await waitFor(() => expect(contents()).toHaveLength(1));
    const signal = contents()[0].signal;
    await act(async () => { delayed.first(); });
    fireEvent.click(screen.getByText(control));
    await waitFor(() => expect(signal?.aborted).toBe(true));
    await act(async () => { delayed.finish(); });
    expect(document.body).not.toHaveTextContent("OLD-SCOPE-PRIVATE-BODY");
    expect(attachments()).toHaveLength(0);
    expect(screen.queryByTestId("live-submission-content")).not.toBeInTheDocument();
  });
  it("aborts a collapsed disclosure and discards an otherwise valid late response without caching it", async () => {
    const delayed = controlledJSON(live()); contentReply = async () => delayed.response;
    mount(); await open(); await waitFor(() => expect(contents()).toHaveLength(1));
    await act(async () => { delayed.first(); });
    fireEvent.click(screen.getByTestId("receipt-content-disclosure"));
    expect(contents()[0].signal?.aborted).toBe(true);
    await act(async () => { delayed.finish(); });
    expect(document.body).not.toHaveTextContent("authorized live body");
    expect(attachments()).toHaveLength(0);
  });
  it("token-only rotation preserves disclosure scope and authorized private content", async () => {
    mount(); await open(); await screen.findByText("authorized live body");
    const scope = sessionScope();
    fireEvent.click(screen.getByText("Rotate token only"));
    expect(sessionScope()).toBe(scope);
    expect(screen.getByTestId("receipt-content-disclosure")).toHaveAttribute("aria-expanded", "true");
    expect(screen.getByText("authorized live body")).toBeInTheDocument();
  });
  it("successful existing refresh retries the live read with a rotated token without inventing a new identity scope", async () => {
    const before = sessionScope();
    refreshReply = async () => json({ access_token: "fresh-read-token" });
    contentReply = async call => call.authorization === "Bearer receipt-token" ? denied(401) : json(live());
    mount(); await open(); await screen.findByText("authorized live body");
    expect(sessionScope()).toBe(before);
    expect(contents().map(call => call.authorization)).toEqual(["Bearer receipt-token", "Bearer fresh-read-token"]);
  });
});
