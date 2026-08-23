import { NextRequest } from "next/server";
import { describe, expect, it } from "vitest";

import { requireCsrf } from "./csrf";

function sessionRequest(headers: Record<string, string> = {}) {
  return new NextRequest("http://localhost/api/v1/auth/login", {
    method: "POST",
    headers,
  });
}

describe("requireCsrf", () => {
  it("cookie 与请求头一致时放行", () => {
    const req = sessionRequest({
      cookie: "tabmail_csrf=token-1",
      "x-csrf-token": "token-1",
    });
    expect(requireCsrf(req)).toBeNull();
  });

  it("缺少请求头时拒绝", async () => {
    const res = requireCsrf(sessionRequest({ cookie: "tabmail_csrf=token-1" }));
    expect(res?.status).toBe(403);
    const payload = await res?.json();
    expect(payload).toMatchObject({ error: { code: "FORBIDDEN" } });
  });

  it("缺少 cookie 时拒绝", () => {
    const res = requireCsrf(sessionRequest({ "x-csrf-token": "token-1" }));
    expect(res?.status).toBe(403);
  });

  it("cookie 与请求头不一致时拒绝", () => {
    const res = requireCsrf(
      sessionRequest({ cookie: "tabmail_csrf=token-1", "x-csrf-token": "token-2" })
    );
    expect(res?.status).toBe(403);
  });

  it("双方皆为空串时拒绝", () => {
    const res = requireCsrf(sessionRequest({ cookie: "tabmail_csrf=", "x-csrf-token": "" }));
    expect(res?.status).toBe(403);
  });
});
