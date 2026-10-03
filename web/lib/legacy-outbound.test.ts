import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { legacyOutboundReceipt, legacyOutboundRecipients } from "./legacy-outbound";
import { advanceSession, AUTH_EVENT } from "./session";

const id = "30000000-0000-4000-8000-000000000001";
const otherId = "30000000-0000-4000-8000-000000000002";
const tenant = "10000000-0000-4000-8000-000000000001";
const otherTenant = "10000000-0000-4000-8000-000000000002";
const detailPath = `/api/v1/outbound/${id}`;
const receipt = () => ({
  id, tenant_id: tenant, state: "sent", status: "partially_accepted",
  progress: { completeness: "known", counts: { total: 2, accepted: 1, pending: 0, temporary: 0, permanent: 1, uncertain: 0 } },
  delivery_uncertain: false,
  capabilities: { view_content: false, retry: false, retry_block_reason: "state_not_retryable" },
});
const json = (body: unknown, status = 200) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
const unavailable = (status = 404) => json({ error: { code: "READ_UNAVAILABLE", message: "Read unavailable" } }, status);
let reply: (init: RequestInit) => Response | Promise<Response>;
let fetchMock: ReturnType<typeof vi.fn<typeof fetch>>;

beforeEach(() => {
  localStorage.setItem("tabmail_access_token", "synthetic-reader-token");
  localStorage.setItem("tabmail_tenant_id", tenant);
  localStorage.setItem("tabmail_user", JSON.stringify({ id: otherId, role: "user" }));
  reply = () => json({ data: receipt() });
  // Only the registered detail route succeeds. A /recipients request must fail,
  // unlike the old consumer fixture's broad outbound prefix match.
  fetchMock = vi.fn<typeof fetch>(async (input, init = {}) => {
    const url = new URL(String(input));
    if (url.pathname !== detailPath || url.search || init.method !== "GET") return unavailable();
    return reply(init);
  });
  vi.stubGlobal("fetch", fetchMock);
});
afterEach(() => vi.unstubAllGlobals());

describe("legacy recipients compatibility alias over the real transport/parser", () => {
  it("requests exactly authorized GET detail and returns the same aggregate as detail", async () => {
    expect(await legacyOutboundRecipients(id)).toEqual(await legacyOutboundReceipt(id));
    expect(fetchMock).toHaveBeenCalledTimes(2);
    for (const [input, init] of fetchMock.mock.calls) {
      expect(new URL(String(input)).pathname).toBe(detailPath);
      expect(new URL(String(input)).search).toBe("");
      expect(init).toMatchObject({ method: "GET", credentials: "include", headers: {
        Authorization: "Bearer synthetic-reader-token", "X-Tenant-ID": tenant,
      } });
      expect(init?.body).toBeUndefined();
      expect(init?.signal).toBeInstanceOf(AbortSignal);
    }
  });

  it("encodes the supplied ID as one path segment without selecting another endpoint", async () => {
    const suppliedId = `${id}/recipients?tenant=${otherTenant}`;
    await expect(legacyOutboundRecipients(suppliedId)).rejects.toEqual({ error: { code: "READ_UNAVAILABLE", message: "Read unavailable" } });
    const url = new URL(String(fetchMock.mock.calls[0][0]));
    expect(url.pathname).toBe(`/api/v1/outbound/${encodeURIComponent(suppliedId)}`);
    expect(url.search).toBe("");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("preserves unknown progress without counts or inferred authority", async () => {
    const unknown = { id, state: "unknown", status: "needs_attention", progress: { completeness: "unknown" }, delivery_uncertain: false };
    reply = () => json({ data: unknown });
    expect(await legacyOutboundRecipients(id)).toEqual(unknown);
  });

  it.each([
    ["different ID", () => ({ data: { ...receipt(), id: otherId } })],
    ["different tenant", () => ({ data: { ...receipt(), tenant_id: otherTenant } })],
    ["missing known-progress tenant", () => { const value = receipt(); Reflect.deleteProperty(value, "tenant_id"); return { data: value }; }],
    ["recipient array", () => ({ data: [{ address: "private@fixture.test" }] })],
    ["raw recipient extension", () => ({ data: { ...receipt(), recipients: ["private@fixture.test"] } })],
    ["BCC extension", () => ({ data: { ...receipt(), bcc: ["private@fixture.test"] } })],
    ["nested recipient extension", () => ({ data: { ...receipt(), progress: { ...receipt().progress, recipients: [] } } })],
    ["unknown with invented counts", () => ({ data: { ...receipt(), status: "needs_attention", progress: { completeness: "unknown", counts: receipt().progress.counts } } })],
    ["extended envelope", () => ({ data: receipt(), recipients: [] })],
  ])("rejects %s instead of returning recipient details", async (_label, body) => {
    reply = () => json(body());
    await expect(legacyOutboundRecipients(id)).rejects.toThrow("Invalid or out-of-scope receipt response");
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each([401, 403, 404, 500])("propagates API %s without fallback routes or data", async status => {
    reply = () => unavailable(status);
    await expect(legacyOutboundRecipients(id)).rejects.toEqual({ error: { code: "READ_UNAVAILABLE", message: "Read unavailable" } });
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it.each(["tenant", "actor", "role", "epoch"])("discards a delayed success after %s scope rotation", async change => {
    let release!: (response: Response) => void;
    reply = () => new Promise<Response>(resolve => { release = resolve; });
    const pending = legacyOutboundRecipients(id);
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    if (change === "tenant") localStorage.setItem("tabmail_tenant_id", otherTenant);
    if (change === "actor") localStorage.setItem("tabmail_user", JSON.stringify({ id, role: "user" }));
    if (change === "role") localStorage.setItem("tabmail_user", JSON.stringify({ id: otherId, role: "super_admin" }));
    if (change === "epoch") advanceSession();
    else window.dispatchEvent(new Event(AUTH_EVENT));
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(true);
    // Simulate a transport which returns despite cancellation.
    release(json({ data: receipt() }));
    await rejected;
    expect(fetchMock).toHaveBeenCalledTimes(1);
  });

  it("keeps same-scope token rotation valid and uses the new token on the next read", async () => {
    let release!: (response: Response) => void;
    reply = () => new Promise<Response>(resolve => { release = resolve; });
    const pending = legacyOutboundRecipients(id);
    localStorage.setItem("tabmail_access_token", "synthetic-renewed-token");
    window.dispatchEvent(new Event(AUTH_EVENT));
    expect(fetchMock.mock.calls[0][1]?.signal?.aborted).toBe(false);
    release(json({ data: receipt() }));
    expect(await pending).toEqual(receipt());
    reply = () => json({ data: receipt() });
    expect(await legacyOutboundRecipients(id)).toEqual(receipt());
    expect(fetchMock.mock.calls[1][1]?.headers).toMatchObject({ Authorization: "Bearer synthetic-renewed-token", "X-Tenant-ID": tenant });
  });

  it("also discards scope changes during response body parsing", async () => {
    let release!: (body: unknown) => void;
    const response = json({ data: receipt() });
    const parsed = new Promise<unknown>(resolve => { release = resolve; });
    const reading = vi.spyOn(response, "json").mockReturnValue(parsed);
    reply = () => response;
    const pending = legacyOutboundRecipients(id);
    const rejected = expect(pending).rejects.toMatchObject({ name: "AbortError" });
    await vi.waitFor(() => expect(reading).toHaveBeenCalledOnce());
    localStorage.setItem("tabmail_tenant_id", otherTenant);
    release({ data: receipt() });
    await rejected;
  });
});
