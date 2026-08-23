import type { NextRequest } from "next/server";

import { proxySessionRefresh } from "@/lib/server/auth-proxy";

export async function POST(request: NextRequest) {
  return proxySessionRefresh(request);
}
