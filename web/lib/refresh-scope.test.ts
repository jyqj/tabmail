import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { request, tryRefreshToken } from "./api/base";
import { advanceSession, installSession, sessionScope } from "./session";

// The real request/refresh/session clients run against controlled HTTP and a
// serial Web Lock queue initially held by another tab. No cookies are issued.
const user = (id = "a") => ({ id, email: `${id}@fixture.test`, display_name: id,
  role: "super_admin" as const, tenant_id: "tenant-a" });
const json = (status: number, body: unknown) => new Response(JSON.stringify(body), {
  status, headers: { "Content-Type": "application/json" },
});
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>(done => { resolve = done; });
  return { promise, resolve };
}
function heldLock() {
  const held = deferred<void>();
  let tail: Promise<unknown> = held.promise;
  const acquire = vi.fn((_name: string, work: () => unknown) => {
    const result = tail.then(work);
    tail = result.catch(() => undefined);
    return result;
  });
  Object.defineProperty(navigator, "locks", { configurable: true, value: { request: acquire } });
  return { acquire, release: () => held.resolve(), idle: () => tail };
}
let lock: ReturnType<typeof heldLock>;
beforeEach(() => {
  localStorage.clear();
  installSession("old-access", user());
  lock = heldLock();
});
afterEach(async () => {
  lock.release();
  await lock.idle();
  vi.unstubAllGlobals();
});
const changes = {
  account: () => installSession("b-access", user("b")),
  tenant: () => { localStorage.setItem("tabmail_tenant_id", "tenant-b"); advanceSession(); },
  epoch: () => advanceSession(),
};

describe("refresh coalescing belongs to the requesting session scope", () => {
  it.each(["account", "tenant", "epoch"] as const)("allows the new %s scope to refresh behind an older queued operation", async change => {
    const stale = tryRefreshToken("old-access", sessionScope());
    changes[change]();
    const currentScope = sessionScope();
    const currentUser = JSON.parse(localStorage.getItem("tabmail_user")!);
    const currentTenant = localStorage.getItem("tabmail_tenant_id");
    const fetcher = vi.fn<typeof fetch>(async () => json(200, { data: { access_token: "renewed-current", user: currentUser } }));
    vi.stubGlobal("fetch", fetcher);
    const current = tryRefreshToken(localStorage.getItem("tabmail_access_token")!, currentScope);
    lock.release();
    expect(await stale).toBe(false);
    expect(await current).toBe(true);
    expect(fetcher).toHaveBeenCalledTimes(1);
    expect(fetcher.mock.calls[0][1]).toMatchObject({ method: "POST", credentials: "include" });
    expect(localStorage.getItem("tabmail_access_token")).toBe("renewed-current");
    expect(localStorage.getItem("tabmail_tenant_id")).toBe(currentTenant);
    expect(sessionScope()).toBe(currentScope);
  });

  it("lets an actual current-tenant GET renew after 401 instead of inheriting a stale false result", async () => {
    const stale = tryRefreshToken("old-access");
    changes.tenant();
    const seen: { path: string; authorization: string | null; tenant: string | null }[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL, init?: RequestInit) => {
      const path = new URL(String(input), "http://localhost").pathname;
      const headers = new Headers(init?.headers);
      seen.push({ path, authorization: headers.get("Authorization"), tenant: headers.get("X-Tenant-ID") });
      if (path === "/api/v1/auth/refresh") return json(200, { data: { access_token: "renewed-current", user: user() } });
      if (headers.get("Authorization") === "Bearer old-access") return json(401, { error: { code: "UNAUTHORIZED" } });
      return json(200, { data: ["current-tenant-result"] });
    });
    const current = request("/api/v1/company/mailboxes").then(value => ({ value }), error => ({ error }));
    // Drain the response continuation so the 401 has requested its refresh while
    // the earlier scope still owns the queued promise, before releasing the lock.
    await Promise.resolve();
    await Promise.resolve();
    lock.release();
    expect(await stale).toBe(false);
    expect(await current).toEqual({ value: { data: ["current-tenant-result"] } });
    expect(seen).toEqual([
      { path: "/api/v1/company/mailboxes", authorization: "Bearer old-access", tenant: "tenant-b" },
      { path: "/api/v1/auth/refresh", authorization: null, tenant: null },
      { path: "/api/v1/company/mailboxes", authorization: "Bearer renewed-current", tenant: "tenant-b" },
    ]);
  });

  it("keeps current-scope callers coalesced after the older scope finishes, including a temporary failure", async () => {
    const stale = tryRefreshToken("old-access");
    changes.tenant();
    const pending = deferred<Response>();
    const fetcher = vi.fn(() => pending.promise);
    vi.stubGlobal("fetch", fetcher);
    const current = tryRefreshToken("old-access");
    lock.release();
    try {
      expect(await stale).toBe(false);
      await vi.waitFor(() => expect(fetcher).toHaveBeenCalledTimes(1));
      const concurrent = tryRefreshToken("old-access");
      pending.resolve(json(503, { error: { code: "INTERNAL" } }));
      expect(await Promise.all([current, concurrent])).toEqual([false, false]);
      expect(fetcher).toHaveBeenCalledTimes(1);
      expect(lock.acquire).toHaveBeenCalledTimes(2);
      expect(localStorage.getItem("tabmail_access_token")).toBe("old-access");
    } finally {
      pending.resolve(json(503, { error: { code: "INTERNAL" } }));
      await current;
    }
  });

  it("does not mask a current-scope denied refresh or replay its unsafe write", async () => {
    const stale = tryRefreshToken("old-access");
    changes.tenant();
    const seen: string[] = [];
    vi.stubGlobal("fetch", async (input: RequestInfo | URL) => {
      const path = new URL(String(input), "http://localhost").pathname;
      seen.push(path);
      return path === "/api/v1/auth/refresh"
        ? json(403, { error: { code: "FORBIDDEN" } })
        : json(401, { error: { code: "UNAUTHORIZED" } });
    });
    const current = request("/api/v1/company/mailboxes", { method: "POST", body: { address: "new@fixture.test" } })
      .then(value => ({ value }), error => ({ error }));
    await Promise.resolve();
    await Promise.resolve();
    lock.release();
    expect(await stale).toBe(false);
    const result = await current;
    expect(result).toHaveProperty("error");
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(seen).toEqual(["/api/v1/company/mailboxes", "/api/v1/auth/refresh"]);
  });
});
