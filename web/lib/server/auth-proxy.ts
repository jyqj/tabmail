// Route-handler helpers that proxy the session endpoints to the backend.
// Filesystem routes win over the afterFiles rewrites in next.config.ts, so
// the app/api/v1/auth/* handlers intercept these paths before the generic
// /api/v1 rewrite would forward them verbatim.

import { NextRequest, NextResponse } from "next/server";

import {
  REFRESH_COOKIE_NAME,
  backendBase,
  refreshCookieOptions,
  splitRefreshToken,
} from "./auth-session";

const upstreamUnavailable = {
  error: { code: "UPSTREAM_UNAVAILABLE", message: "auth service unavailable" },
};

export function clearRefreshCookie(res: NextResponse) {
  res.cookies.set(REFRESH_COOKIE_NAME, "", { ...refreshCookieOptions(), maxAge: 0 });
}

async function forward(path: string, init: RequestInit): Promise<Response | null> {
  try {
    return await fetch(backendBase() + path, { ...init, cache: "no-store" });
  } catch {
    return null;
  }
}

// proxySessionIssue forwards a credential exchange (login, register,
// accept-invite) and moves the issued refresh token into the httpOnly cookie.
export async function proxySessionIssue(request: NextRequest, path: string): Promise<NextResponse> {
  const upstream = await forward(path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: await request.text(),
  });
  if (!upstream) {
    return NextResponse.json(upstreamUnavailable, { status: 502 });
  }
  const payload = await upstream.json().catch(() => null);
  if (!upstream.ok) {
    const res = NextResponse.json(payload ?? upstreamUnavailable, { status: upstream.status });
    const retryAfter = upstream.headers.get("Retry-After");
    if (retryAfter) res.headers.set("Retry-After", retryAfter);
    return res;
  }
  const { body, refreshToken } = splitRefreshToken(payload);
  const res = NextResponse.json(body, { status: upstream.status });
  if (refreshToken) {
    res.cookies.set(REFRESH_COOKIE_NAME, refreshToken, refreshCookieOptions());
  }
  return res;
}

// proxySessionRefresh exchanges the cookie for a new token pair. The backend
// rotates refresh tokens, so a success always rewrites the cookie; a refused
// refresh clears it so the browser stops retrying a dead session.
export async function proxySessionRefresh(request: NextRequest): Promise<NextResponse> {
  const token = request.cookies.get(REFRESH_COOKIE_NAME)?.value;
  if (!token) {
    return NextResponse.json(
      { error: { code: "UNAUTHORIZED", message: "no refresh session" } },
      { status: 401 },
    );
  }
  const upstream = await forward("/api/v1/auth/refresh", {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ refresh_token: token }),
  });
  if (!upstream) {
    return NextResponse.json(upstreamUnavailable, { status: 502 });
  }
  const payload = await upstream.json().catch(() => null);
  if (!upstream.ok) {
    const res = NextResponse.json(payload ?? upstreamUnavailable, { status: upstream.status });
    if (upstream.status === 401 || upstream.status === 403) {
      clearRefreshCookie(res);
    }
    return res;
  }
  const { body, refreshToken } = splitRefreshToken(payload);
  const res = NextResponse.json(body, { status: upstream.status });
  if (refreshToken) {
    res.cookies.set(REFRESH_COOKIE_NAME, refreshToken, refreshCookieOptions());
  }
  return res;
}

// proxySessionLogout revokes the session upstream on a best-effort basis and
// always clears the cookie: a browser logout must succeed locally even when
// the backend is unreachable or the access token already expired.
export async function proxySessionLogout(request: NextRequest): Promise<NextResponse> {
  const token = request.cookies.get(REFRESH_COOKIE_NAME)?.value ?? "";
  const headers: Record<string, string> = { "Content-Type": "application/json" };
  const authorization = request.headers.get("authorization");
  if (authorization) headers.Authorization = authorization;
  await forward("/api/v1/auth/logout", {
    method: "POST",
    headers,
    body: JSON.stringify(token ? { refresh_token: token } : {}),
  });
  const res = new NextResponse(null, { status: 204 });
  clearRefreshCookie(res);
  return res;
}
