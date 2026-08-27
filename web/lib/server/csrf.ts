// Server half of the double-submit CSRF check for the session route
// handlers. The deliberate omission of an Origin/Host comparison keeps the
// check deployment-agnostic: reverse proxies may rewrite Host, but the
// cookie/header pair works the same everywhere.

import type { NextRequest } from "next/server";
import { NextResponse } from "next/server";

import { CSRF_COOKIE_NAME, CSRF_HEADER_NAME } from "@/lib/csrf";

// requireCsrf returns the 403 to send when the request does not carry a
// matching CSRF cookie/header pair, or null when the request may proceed.
export function requireCsrf(request: NextRequest): NextResponse | null {
  const header = request.headers.get(CSRF_HEADER_NAME)?.trim() ?? "";
  const cookie = request.cookies.get(CSRF_COOKIE_NAME)?.value.trim() ?? "";
  if (!header || header !== cookie) {
    return NextResponse.json(
      { error: { code: "FORBIDDEN", message: "missing or mismatched CSRF token" } },
      { status: 403 },
    );
  }
  return null;
}
