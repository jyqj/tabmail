import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { buildHeaders, getAccessToken, request, setAccessToken } from "./base";

describe("api/base", () => {
  beforeEach(() => {
    localStorage.clear();
    setAccessToken(null);
    vi.unstubAllGlobals();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("使用内存中的 JWT 并附带 tenant id", () => {
    setAccessToken("access-token");
    localStorage.setItem("tabmail_tenant_id", "tenant-1");
    localStorage.setItem("tabmail_mailbox_token", "mailbox-token");

    expect(buildHeaders("/api/v1/domains")).toEqual({
      Authorization: "Bearer access-token",
      "X-Tenant-ID": "tenant-1",
    });
  });

  it("access token 只存在内存里，不落 localStorage", () => {
    setAccessToken("access-token");
    expect(getAccessToken()).toBe("access-token");
    expect(localStorage.getItem("tabmail_access_token")).toBeNull();
    expect(localStorage.getItem("tabmail_refresh_token")).toBeNull();
  });

  it("目标 mailbox 路径上优先使用 mailbox token，避免被 JWT 抢占", () => {
    setAccessToken("access-token");
    localStorage.setItem("tabmail_tenant_id", "tenant-1");
    localStorage.setItem("tabmail_mailbox_token", "mailbox-token");
    localStorage.setItem("tabmail_mailbox_address", "user@mail.test");

    expect(buildHeaders("/api/v1/mailbox/user%40mail.test")).toEqual({
      Authorization: "Bearer mailbox-token",
    });
    expect(buildHeaders("/api/v1/mailbox/other%40mail.test")).toEqual({
      Authorization: "Bearer access-token",
      "X-Tenant-ID": "tenant-1",
    });
  });

  it("401 时通过 cookie 刷新并用新 token 重试", async () => {
    setAccessToken("stale-token");
    localStorage.setItem(
      "tabmail_user",
      JSON.stringify({ id: "u1", email: "user@mail.test", role: "user" })
    );

    const fetchMock = vi.fn(async (input: RequestInfo | URL, init?: RequestInit) => {
      const url = String(input);
      if (url.endsWith("/api/v1/auth/refresh")) {
        return new Response(JSON.stringify({ data: { access_token: "fresh-token" } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      const auth = (init?.headers as Record<string, string> | undefined)?.Authorization;
      if (auth === "Bearer fresh-token") {
        return new Response(JSON.stringify({ data: { ok: true } }), {
          status: 200,
          headers: { "content-type": "application/json" },
        });
      }
      return new Response(JSON.stringify({ error: { code: "UNAUTHORIZED", message: "expired" } }), {
        status: 401,
        headers: { "content-type": "application/json" },
      });
    });
    vi.stubGlobal("fetch", fetchMock);

    const result = await request<{ data: { ok: boolean } }>("/api/v1/domains");

    expect(result).toEqual({ data: { ok: true } });
    expect(getAccessToken()).toBe("fresh-token");
    // original + refresh + retry
    expect(fetchMock).toHaveBeenCalledTimes(3);
    // The refresh call carries no token in the body: the httpOnly cookie is
    // the credential and the route handler reads it server-side.
    const refreshCall = fetchMock.mock.calls.find(([input]) =>
      String(input).endsWith("/api/v1/auth/refresh")
    );
    expect(refreshCall?.[1]?.body).toBeUndefined();
    // It does carry the double-submit CSRF pair for the route handler.
    const refreshHeaders = refreshCall?.[1]?.headers as Record<string, string>;
    expect(refreshHeaders["X-CSRF-Token"]).toBeTruthy();
    expect(document.cookie).toContain(`tabmail_csrf=${refreshHeaders["X-CSRF-Token"]}`);
  });

  it("会话端点携带与 cookie 匹配的双提交 CSRF 头，其余端点不带", async () => {
    const fetchMock = vi.fn(
      async (_input: RequestInfo | URL, _init?: RequestInit) =>
        new Response(JSON.stringify({ data: {} }), {
          status: 200,
          headers: { "content-type": "application/json" },
        })
    );
    vi.stubGlobal("fetch", fetchMock);

    await request("/api/v1/auth/login", {
      method: "POST",
      body: { email: "user@mail.test", password: "pw" },
    });
    const loginHeaders = fetchMock.mock.calls[0][1]?.headers as Record<string, string>;
    expect(loginHeaders["X-CSRF-Token"]).toBeTruthy();
    expect(document.cookie).toContain(`tabmail_csrf=${loginHeaders["X-CSRF-Token"]}`);

    await request("/api/v1/domains");
    const plainHeaders = fetchMock.mock.calls[1][1]?.headers as Record<string, string>;
    expect(plainHeaders["X-CSRF-Token"]).toBeUndefined();
  });

  it("刷新失败时清除本地会话标记", async () => {
    setAccessToken("stale-token");
    localStorage.setItem(
      "tabmail_user",
      JSON.stringify({ id: "u1", email: "user@mail.test", role: "user" })
    );

    const fetchMock = vi.fn(async () =>
      new Response(JSON.stringify({ error: { code: "UNAUTHORIZED", message: "nope" } }), {
        status: 401,
        headers: { "content-type": "application/json" },
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    await expect(request("/api/v1/domains")).rejects.toMatchObject({
      error: { code: "UNAUTHORIZED" },
    });
    expect(getAccessToken()).toBeNull();
    expect(localStorage.getItem("tabmail_user")).toBeNull();
  });

  it("request 会拼接 query 参数并返回文本响应", async () => {
    const fetchMock = vi.fn().mockResolvedValue(
      new Response("raw message", {
        status: 200,
        headers: { "content-type": "text/plain; charset=utf-8" },
      })
    );
    vi.stubGlobal("fetch", fetchMock);

    const result = await request<string>("/api/v1/mailbox/user%40mail.test/source", {
      params: { page: 2, per_page: 10 },
    });

    expect(result).toBe("raw message");
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const url = fetchMock.mock.calls[0][0] as string;
    expect(url).toContain("/api/v1/mailbox/user%40mail.test/source");
    expect(url).toContain("page=2");
    expect(url).toContain("per_page=10");
  });

  it("request 在非 2xx 时抛出后端错误体", async () => {
    vi.stubGlobal(
      "fetch",
      vi.fn().mockResolvedValue(
        new Response(JSON.stringify({ error: { code: "FORBIDDEN", message: "nope" } }), {
          status: 403,
          headers: { "content-type": "application/json" },
        })
      )
    );

    await expect(request("/api/v1/domains")).rejects.toEqual({
      error: { code: "FORBIDDEN", message: "nope" },
    });
  });
});
