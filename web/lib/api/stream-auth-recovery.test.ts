import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { AUTH_EVENT, installSession, sessionScope } from "../session";
import { streamEvents } from "./base";

// Exercise the actual authenticated stream and refresh coordinator. Only HTTP
// and the browser's cross-tab lock are substituted; parser/response bodies and
// timers retain their production paths.
const tenant = "10000000-0000-4000-8000-000000000001";
const user = { id: "reader", tenant_id: tenant, role: "user" as const,
  email: "reader@fixture.test", display_name: "Reader" };
const path = "/api/v1/company/events";
const encoder = new TextEncoder();
const json = (data: unknown, status = 200) => new Response(JSON.stringify(data), {
  status, headers: { "Content-Type": "application/json" },
});
const feed = () => new Response(new ReadableStream<Uint8Array>({
  start(controller) {
    controller.enqueue(encoder.encode("id: current\nevent: company.admin.changed\ndata: {\"state\":\"updated\"}\n\n"));
    controller.close();
  },
}), { headers: { "Content-Type": "text/event-stream" } });
const deferred = () => {
  let resolve!: (response: Response) => void;
  const promise = new Promise<Response>(done => { resolve = done; });
  return { promise, resolve };
};
const controllers: AbortController[] = [];
const flush = () => vi.advanceTimersByTimeAsync(0);
function start() {
  const abort = new AbortController();
  controllers.push(abort);
  const onEvent = vi.fn();
  let settled = false;
  const result = streamEvents(path, { signal: abort.signal, onEvent }).catch(error => error)
    .finally(() => { settled = true; });
  return { abort, onEvent, result, settled: () => settled };
}

beforeEach(() => {
  vi.useFakeTimers();
  installSession("old-token", user);
  Object.defineProperty(navigator, "locks", { configurable: true,
    value: { request: async (_name: string, operation: () => unknown) => operation() } });
});
afterEach(() => {
  for (const abort of controllers.splice(0)) abort.abort();
  vi.useRealTimers();
  vi.unstubAllGlobals();
  Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
});

describe("stream authentication recovery", () => {
  it.each(["503", "network", "invalid JSON", "incomplete response"])(
    "retries after refresh %s without declaring the current authority revoked", async failure => {
      let refreshes = 0;
      const tokens: (string | null)[] = [];
      const fetcher = vi.fn<typeof fetch>(async (input, init) => {
        if (String(input).endsWith("/auth/refresh")) {
          if (++refreshes > 1) return json({ data: { access_token: "renewed-token", user } });
          if (failure === "network") throw new TypeError("Temporarily offline");
          if (failure === "invalid JSON") return new Response("{", { status: 200 });
          if (failure === "incomplete response") return json({ data: {} });
          return json({ error: { code: "UNAVAILABLE" } }, 503);
        }
        const token = new Headers(init?.headers).get("Authorization");
        tokens.push(token);
        return token === "Bearer renewed-token" ? feed() : json({}, 401);
      });
      vi.stubGlobal("fetch", fetcher);
      const scope = sessionScope();
      const stream = start();
      await flush();
      expect(refreshes).toBe(1);
      expect(stream.settled()).toBe(false);
      expect(stream.onEvent).not.toHaveBeenCalled();
      expect(sessionScope()).toBe(scope);
      expect(localStorage.getItem("tabmail_access_token")).toBe("old-token");
      await vi.advanceTimersByTimeAsync(1000);
      expect(refreshes).toBe(2);
      expect(tokens).toEqual(["Bearer old-token", "Bearer old-token", "Bearer renewed-token"]);
      expect(stream.onEvent.mock.calls.map(call => call[0])).toEqual([
        { type: "resync", data: null }, { type: "company.admin.changed", data: { state: "updated" } },
      ]);
      stream.abort.abort();
      expect(await stream.result).toMatchObject({ name: "AbortError" });
    },
  );

  it("reuses a token rotated by another tab while the original stream request was pending", async () => {
    const pending = deferred();
    const fetcher = vi.fn<typeof fetch>().mockImplementationOnce(() => pending.promise)
      .mockImplementation(async () => feed());
    vi.stubGlobal("fetch", fetcher);
    const scope = sessionScope();
    const stream = start();
    localStorage.setItem("tabmail_access_token", "other-tab-token");
    window.dispatchEvent(new Event(AUTH_EVENT));
    pending.resolve(json({}, 401));
    await flush();
    expect(sessionScope()).toBe(scope);
    expect(fetcher).toHaveBeenCalledTimes(2);
    expect(fetcher.mock.calls.every(call => String(call[0]).endsWith(path))).toBe(true);
    expect(new Headers(fetcher.mock.calls[1][1]?.headers).get("Authorization")).toBe("Bearer other-tab-token");
    expect(stream.onEvent).toHaveBeenCalledWith({ type: "company.admin.changed", data: { state: "updated" } });
    stream.abort.abort();
    await stream.result;
  });

  it.each([401, 403])("a rejected refresh %i still clears the session and ends the old stream", async status => {
    const fetcher = vi.fn<typeof fetch>(async input => json({}, String(input).endsWith("/auth/refresh") ? status : 401));
    vi.stubGlobal("fetch", fetcher);
    const scope = sessionScope();
    const stream = start();
    await flush();
    expect(await stream.result).toMatchObject({ name: "AbortError" });
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(sessionScope()).not.toBe(scope);
    expect(stream.onEvent).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(15000);
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each([401, 403])("a refused retried stream %i remains terminal", async status => {
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(json({}, 401))
      .mockResolvedValueOnce(json({ data: { access_token: "renewed-token", user } }))
      .mockResolvedValueOnce(json({}, status));
    vi.stubGlobal("fetch", fetcher);
    const stream = start();
    await flush();
    expect(await stream.result).toMatchObject({ name: "NotAllowedError" });
    expect(stream.onEvent).not.toHaveBeenCalled();
    await vi.advanceTimersByTimeAsync(15000);
    expect(fetcher).toHaveBeenCalledTimes(3);
  });

  it("keeps the existing fail-closed boundary when shared browser locks are unavailable", async () => {
    Object.defineProperty(navigator, "locks", { configurable: true, value: undefined });
    const fetcher = vi.fn<typeof fetch>(async () => json({}, 401));
    vi.stubGlobal("fetch", fetcher);
    const stream = start();
    await flush();
    expect(await stream.result).toMatchObject({ name: "NotAllowedError" });
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(stream.onEvent).not.toHaveBeenCalled();
  });

  it.each(["abort", "session change"])("does not start another stream request after %s during refresh", async boundary => {
    const pending = deferred();
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(json({}, 401))
      .mockImplementationOnce(() => pending.promise).mockImplementation(async () => feed());
    vi.stubGlobal("fetch", fetcher);
    const stream = start();
    await flush();
    expect(fetcher).toHaveBeenCalledTimes(2);
    if (boundary === "abort") stream.abort.abort();
    else installSession("replacement-token", { ...user, id: "replacement" });
    pending.resolve(json({ data: { access_token: "renewed-token", user } }));
    await flush();
    expect(stream.onEvent).not.toHaveBeenCalled();
    expect(await stream.result).toMatchObject({ name: "AbortError" });
    expect(fetcher).toHaveBeenCalledTimes(2);
  });

  it.each(["abort", "session change"])("does not emit a reconnect resync after %s while the refreshed request is pending", async boundary => {
    const pending = deferred();
    const fetcher = vi.fn<typeof fetch>().mockResolvedValueOnce(json({}, 401))
      .mockResolvedValueOnce(json({ data: { access_token: "renewed-token", user } }))
      .mockImplementationOnce(() => pending.promise);
    vi.stubGlobal("fetch", fetcher);
    const stream = start();
    await flush();
    expect(fetcher).toHaveBeenCalledTimes(3);
    if (boundary === "abort") stream.abort.abort();
    else installSession("replacement-token", { ...user, id: "replacement" });
    pending.resolve(feed());
    await flush();
    expect(stream.onEvent).not.toHaveBeenCalled();
    expect(await stream.result).toMatchObject({ name: "AbortError" });
  });
});
