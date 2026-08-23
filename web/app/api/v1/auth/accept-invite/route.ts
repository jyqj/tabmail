import type { NextRequest } from "next/server";

import { proxySessionIssue } from "@/lib/server/auth-proxy";

export async function POST(request: NextRequest) {
  return proxySessionIssue(request, "/api/v1/auth/accept-invite");
}
