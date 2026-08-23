import type { NextRequest } from "next/server";

import { proxySessionLogout } from "@/lib/server/auth-proxy";

export async function POST(request: NextRequest) {
  return proxySessionLogout(request);
}
