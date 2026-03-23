import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { buildHeaders, request } from "./base";

describe("api/base", () => {
  beforeEach(() => {
    localStorage.clear();
    vi.unstubAllGlobals();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("优先使用 admin key 并附带 tenant id", () => {
    localStorage.setItem("tabmail_admin_key", "admin-secret");
    localStorage.setItem("tabmail_tenant_id", "tenant-1");
    localStorage.setItem("tabmail_api_key", "tenant-key");
    localStorage.setItem("tabmail_mailbox_token", "mailbox-token");

    expect(buildHeaders("/api/v1/domains")).toEqual({
      "X-Admin-Key": "admin-secret",
      "X-Tenant-ID": "tenant-1",
    });
  });

  it("仅在目标 mailbox 路径上附带 mailbox token", () => {
    localStorage.setItem("tabmail_mailbox_token", "mailbox-token");
    localStorage.setItem("tabmail_mailbox_address", "user@mail.test");

    expect(buildHeaders("/api/v1/mailbox/user%40mail.test")).toEqual({
      Authorization: "Bearer mailbox-token",
    });
    expect(buildHeaders("/api/v1/mailbox/other%40mail.test")).toEqual({});
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
