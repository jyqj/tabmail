import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { installSession, sessionScope } from "@/lib/session";
import { COMPANY_EVENTS_PATH, parseCompanyEvent, streamCompanyEvents } from "./company-events";

const tenant = "10000000-0000-4000-8000-000000000001";
const other = "10000000-0000-4000-8000-000000000002";
const resource = "40000000-0000-4000-8000-000000000001";
const envelope = () => ({ type: "company.admin.changed", tenant_id: tenant, occurred_at: "2026-10-02T01:02:03.123456789Z", metadata: { action: "permission.profile.update", resource_type: "permission_profile", resource_id: resource } });
beforeEach(() => installSession("events-token", { id: resource, tenant_id: tenant, email: "admin@test.invalid", display_name: "Admin", role: "admin" }));
afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); Object.defineProperty(navigator, "locks", { configurable: true, value: undefined }); });

describe("strict selected-tenant company invalidations", () => {
  it("accepts only known minimal metadata and tenant-bound control frames", () => {
    expect(parseCompanyEvent("company.admin.changed", envelope(), tenant)).toEqual({ kind: "changed", metadata: envelope().metadata });
    expect(parseCompanyEvent("resync", { tenant_id: tenant }, tenant)).toEqual({ kind: "resync" });
    expect(parseCompanyEvent("ready", { tenant_id: tenant }, tenant)).toEqual({ kind: "resync" });
  });
  it.each([
    ["cross-tenant", () => ({ ...envelope(), tenant_id: other })],
    ["missing-tenant", () => ({ ...envelope(), tenant_id: undefined })],
    ["unknown-envelope-key", () => ({ ...envelope(), effective: { can_send: true } })],
    ["unknown-private-metadata", () => ({ ...envelope(), metadata: { ...envelope().metadata, body: "private" } })],
    ["unknown-action", () => ({ ...envelope(), metadata: { ...envelope().metadata, action: "future.grant" } })],
    ["wrong-resource-type", () => ({ ...envelope(), metadata: { ...envelope().metadata, resource_type: "user" } })],
    ["unknown-resource-id", () => ({ ...envelope(), metadata: { ...envelope().metadata, resource_id: "not-uuid" } })],
    ["wrong-type", () => ({ ...envelope(), type: "message.updated" })],
    ["invalid-date", () => ({ ...envelope(), occurred_at: "not-time" })],
  ] as const)("rejects %s without deriving authority", (_name, value) => {
    expect(parseCompanyEvent("company.admin.changed", value(), tenant)).toBeNull();
  });
  it("rejects server control frames without exact tenant binding", () => {
    for (const data of [null, {}, { tenant_id: other }, { tenant_id: tenant, permission: true }]) {
      expect(parseCompanyEvent("resync", data, tenant)).toBeNull();
    }
    expect(parseCompanyEvent("future.event", envelope(), tenant)).toBeNull();
  });
});

function liveBody(signal?: AbortSignal | null) {
  let controller!: ReadableStreamDefaultController<Uint8Array>;
  let closed = false;
  const body = new ReadableStream<Uint8Array>({ start(c) { controller = c; }, cancel() { closed = true; } });
  signal?.addEventListener("abort", () => { if (!closed) { closed = true; controller.error(new DOMException("Aborted", "AbortError")); } }, { once: true });
  return { body, push: (text: string) => controller.enqueue(new TextEncoder().encode(text)), close: () => { closed = true; controller.close(); } };
}
const flush = async () => { for (let i = 0; i < 20; i++) await Promise.resolve(); };

describe("actual authenticated ReadableStream transport", () => {
  it("strictly tags wire data, tolerates duplicate UUIDs and re-syncs after normal EOF with advisory cursor", async () => {
    vi.useFakeTimers();
    const bodies: ReturnType<typeof liveBody>[] = [];
    const fetcher = vi.fn(async (_url: string, init?: RequestInit) => {
      const live = liveBody(init?.signal); bodies.push(live);
      return new Response(live.body, { headers: { "Content-Type": "text/event-stream" } });
    });
    vi.stubGlobal("fetch", fetcher);
    const abort = new AbortController(), onInvalidate = vi.fn();
    const running = streamCompanyEvents(tenant, { signal: abort.signal, onInvalidate }).catch(error => error);
    await flush();
    expect(fetcher.mock.calls[0][0]).toContain(COMPANY_EVENTS_PATH);
    expect(fetcher.mock.calls[0][1]?.headers).toMatchObject({ Authorization: "Bearer events-token", "X-Tenant-ID": tenant });
    bodies[0].push("event: resync\ndata: null\n\n");
    bodies[0].push(`id: ${resource}\nevent: company.admin.changed\ndata: ${JSON.stringify(envelope())}\n\n`);
    bodies[0].push(`id: ${resource}\nevent: company.admin.changed\ndata: ${JSON.stringify(envelope())}\n\n`);
    bodies[0].push(`event: company.admin.changed\ndata: ${JSON.stringify({ ...envelope(), tenant_id: other })}\n\n`);
    await flush();
    expect(onInvalidate.mock.calls.map(call => call[0].kind)).toEqual(["resync", "changed", "changed"]);
    bodies[0].close(); await flush(); await vi.advanceTimersByTimeAsync(1000); await flush();
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls[1][1]?.headers).toMatchObject({ "Last-Event-ID": resource });
    expect(onInvalidate.mock.calls.at(-1)?.[0]).toEqual({ kind: "resync" });
    abort.abort(); await running;
  });
  it.each([401, 403])("terminal %s stops rather than looping indefinitely", async status => {
    const fetcher = vi.fn(async () => new Response("{}", { status })); vi.stubGlobal("fetch", fetcher);
    await expect(streamCompanyEvents(tenant, { signal: new AbortController().signal, onInvalidate: vi.fn() })).rejects.toMatchObject({ name: "NotAllowedError" });
    expect(fetcher).toHaveBeenCalledTimes(1);
  });
  it("successful existing refresh keeps the identity scope and retries with the replacement token", async () => {
    const scope = sessionScope(), abort = new AbortController();
    let live: ReturnType<typeof liveBody> | undefined;
    Object.defineProperty(navigator, "locks", { configurable: true, value: { request: async (_name: string, callback: () => Promise<unknown>) => callback() } });
    const fetcher = vi.fn(async (url: string, init?: RequestInit) => {
      if (String(url).endsWith("/auth/refresh")) return new Response(JSON.stringify({ data: { access_token: "renewed-token" } }), { headers: { "Content-Type": "application/json" } });
      if (new Headers(init?.headers).get("Authorization") === "Bearer events-token") return new Response("{}", { status: 401 });
      live = liveBody(init?.signal); return new Response(live.body);
    });
    vi.stubGlobal("fetch", fetcher);
    const onInvalidate = vi.fn(); const running = streamCompanyEvents(tenant, { signal: abort.signal, onInvalidate }).catch(error => error);
    await flush(); expect(onInvalidate).toHaveBeenCalledWith({ kind: "resync" });
    expect(sessionScope()).toBe(scope); expect(localStorage.getItem("tabmail_access_token")).toBe("renewed-token");
    expect(fetcher).toHaveBeenCalledTimes(3); expect(live).toBeTruthy();
    abort.abort(); await running;
    Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
  });
});
