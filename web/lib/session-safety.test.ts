import { beforeEach, afterEach, describe, it, expect, vi } from "vitest";
import { request, tryRefreshToken } from "./api/base";
import {
  installSession,
  sessionScope,
  sessionRequest,
  advanceSession,
} from "./session";
const user = (id = "a") => ({
  id,
  email: `${id}@company.test`,
  display_name: id,
  role: "user" as const,
  tenant_id: "t",
});
const json = (status: number, data: unknown) =>
  new Response(JSON.stringify(data), {
    status,
    headers: { "Content-Type": "application/json" },
  });
function deferred<T>() {
  let resolve!: (value: T) => void;
  const promise = new Promise<T>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}
function locks() {
  let tail: Promise<unknown> = Promise.resolve();
  Object.defineProperty(navigator, "locks", {
    configurable: true,
    value: {
      request: vi.fn((_key: string, f: () => unknown) => {
        const task = tail.then(f);
        tail = task.catch(() => {});
        return task;
      }),
    },
  });
}
beforeEach(() => {
  localStorage.clear();
  locks();
  installSession("old", user());
});
afterEach(() => {
  vi.unstubAllGlobals();
});
describe("credential and identity boundaries", () => {
  it("does not log out after a temporary refresh failure", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(json(503, { error: { code: "INTERNAL" } })),
    );
    expect(await tryRefreshToken("old")).toBe(false);
    expect(localStorage.getItem("tabmail_access_token")).toBe("old");
  });
  it("coalesces concurrent refresh calls and uses the replacement token", async () => {
    const f = vi
      .fn()
      .mockResolvedValue(
        json(200, { data: { access_token: "new", user: user() } }),
      );
    vi.stubGlobal("fetch", f);
    expect(
      await Promise.all([tryRefreshToken("old"), tryRefreshToken("old")]),
    ).toEqual([true, true]);
    expect(f).toHaveBeenCalledTimes(1);
    expect(await tryRefreshToken("old")).toBe(true);
    expect(f).toHaveBeenCalledTimes(1);
  });
  it("does not overwrite another account with a stale refresh response", async () => {
    const d = deferred<Response>();
    vi.stubGlobal(
      "fetch",
      vi.fn(() => d.promise),
    );
    const p = tryRefreshToken("old");
    await vi.waitFor(() => expect(fetch).toHaveBeenCalled());
    installSession("b-token", user("b"));
    d.resolve(json(200, { data: { access_token: "a-late", user: user() } }));
    expect(await p).toBe(false);
    expect(localStorage.getItem("tabmail_access_token")).toBe("b-token");
  });
  it("aborts requests and discards response bodies after account switching", async () => {
    const d = deferred<Response>();
    vi.stubGlobal(
      "fetch",
      vi.fn(() => d.promise),
    );
    const p = request("/api/v1/company/mailboxes").catch((e) => e);
    installSession("b", user("b"));
    d.resolve(json(200, { data: ["A-private-mail"] }));
    expect(((await p) as DOMException).name).toBe("AbortError");
  });
  it("does not automatically replay unsafe POST after renewal", async () => {
    const f = vi
      .fn()
      .mockResolvedValueOnce(json(401, { error: { code: "UNAUTHORIZED" } }))
      .mockResolvedValueOnce(
        json(200, { data: { access_token: "new", user: user() } }),
      );
    vi.stubGlobal("fetch", f);
    await expect(
      request("/api/v1/company/mailboxes", { method: "POST", body: {} }),
    ).rejects.toMatchObject({ error: { code: "RETRY_REQUIRED" } });
    expect(f).toHaveBeenCalledTimes(2);
  });
  it("retries an idempotent send with the same key and request body", async () => {
    const f = vi
      .fn()
      .mockResolvedValueOnce(json(401, { error: { code: "UNAUTHORIZED" } }))
      .mockResolvedValueOnce(
        json(200, { data: { access_token: "new", user: user() } }),
      )
      .mockResolvedValueOnce(json(202, { data: { id: "one-job" } }));
    vi.stubGlobal("fetch", f);
    await request("/api/v1/company/drafts/x/submit", {
      method: "POST",
      headers: { "Idempotency-Key": "same" },
      body: { subject: "unchanged" },
    });
    expect(f.mock.calls[0][1].body).toBe(f.mock.calls[2][1].body);
    expect(f.mock.calls[2][1].headers["Idempotency-Key"]).toBe("same");
  });
  it("keeps the current session when server logout fails", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(json(500, { error: { code: "INTERNAL" } })),
    );
    await expect(
      request("/api/v1/auth/logout", { method: "POST" }),
    ).rejects.toMatchObject({ error: { code: "INTERNAL" } });
    expect(localStorage.getItem("tabmail_access_token")).toBe("old");
  });
  it("holds the shared cookie lock until login body and local identity are installed", async () => {
    const d = deferred<unknown>();
    const response = { status: 200, ok: true, json: () => d.promise };
    const f = vi.fn().mockResolvedValueOnce(response);
    vi.stubGlobal("fetch", f);
    const p = request("/api/v1/auth/login", {
      method: "POST",
      body: { email: "b" },
    });
    await vi.waitFor(() => expect(f).toHaveBeenCalledTimes(1));
    const q = request("/api/v1/auth/logout", { method: "POST" }).catch(
      (e) => e,
    );
    await Promise.resolve();
    expect(f).toHaveBeenCalledTimes(1);
    d.resolve({ data: { access_token: "b-token", user: user("b") } });
    await p;
    expect(((await q) as DOMException).name).toBe("AbortError");
    expect(f).toHaveBeenCalledTimes(1);
    expect(localStorage.getItem("tabmail_access_token")).toBe("b-token");
  });
  it("changes scope on tenant changes but not token rotation", () => {
    const scope = sessionScope();
    localStorage.setItem("tabmail_access_token", "rotated");
    expect(sessionScope()).toBe(scope);
    const lease = sessionRequest();
    localStorage.setItem("tabmail_tenant_id", "other");
    advanceSession();
    expect(lease.signal.aborted).toBe(true);
    expect(sessionScope()).not.toBe(scope);
    lease.dispose();
  });
  it("fails closed without Web Locks rather than racing refresh cookies", async () => {
    Object.defineProperty(navigator, "locks", {
      configurable: true,
      value: undefined,
    });
    vi.stubGlobal("fetch", vi.fn());
    expect(await tryRefreshToken("old")).toBe(false);
    await expect(
      request("/api/v1/auth/login", { method: "POST" }),
    ).rejects.toMatchObject({ error: { code: "SECURE_CONTEXT_REQUIRED" } });
    expect(fetch).not.toHaveBeenCalled();
  });
});
