// Session-cookie plumbing shared by the /api/v1/auth route handlers. The
// refresh token never reaches browser JavaScript: these helpers move it
// between the backend's JSON session payload and an httpOnly cookie scoped to
// the auth endpoints.

export const REFRESH_COOKIE_NAME = "tabmail_refresh_token";

// Matches authn.RefreshTokenTTL on the backend (7 days). The cookie may
// outlive a revoked token; the backend remains the source of truth.
export const REFRESH_COOKIE_MAX_AGE_SECONDS = 7 * 24 * 60 * 60;

// The cookie is only needed by the auth endpoints themselves (refresh,
// logout), so its path keeps it off every other request.
export const REFRESH_COOKIE_PATH = "/api/v1/auth";

export interface RefreshCookieOptions {
  httpOnly: boolean;
  sameSite: "strict";
  secure: boolean;
  path: string;
  maxAge: number;
}

export function refreshCookieOptions(): RefreshCookieOptions {
  return {
    httpOnly: true,
    sameSite: "strict",
    secure: process.env.NODE_ENV === "production",
    path: REFRESH_COOKIE_PATH,
    maxAge: REFRESH_COOKIE_MAX_AGE_SECONDS,
  };
}

// backendBase mirrors next.config.ts: route handlers run on the server, so
// they talk to the backend directly instead of re-entering the rewrites.
export function backendBase(): string {
  return (
    process.env.INTERNAL_API_URL ||
    process.env.NEXT_PUBLIC_API_URL ||
    "http://localhost:8080"
  );
}

export interface SessionSplit {
  /** The payload to return to the browser, with the refresh token removed. */
  body: unknown;
  /** The refresh token destined for the httpOnly cookie, if one was issued. */
  refreshToken: string | null;
}

// splitRefreshToken lifts data.refresh_token out of a backend session
// envelope ({ data: { access_token, refresh_token, ... } }). Payloads in any
// other shape pass through untouched.
export function splitRefreshToken(payload: unknown): SessionSplit {
  if (typeof payload !== "object" || payload === null) {
    return { body: payload, refreshToken: null };
  }
  const envelope = payload as { data?: unknown };
  if (typeof envelope.data !== "object" || envelope.data === null) {
    return { body: payload, refreshToken: null };
  }
  const data = envelope.data as Record<string, unknown>;
  const token = data.refresh_token;
  if (typeof token !== "string" || token === "") {
    return { body: payload, refreshToken: null };
  }
  const rest = { ...data };
  delete rest.refresh_token;
  return { body: { ...envelope, data: rest }, refreshToken: token };
}
