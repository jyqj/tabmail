import { request, getBaseUrl } from "./base";

export async function healthCheck() {
  const res = await fetch(`${getBaseUrl()}/health`);
  if (!res.ok) throw new Error(res.statusText);
  return res.json() as Promise<{ status: string }>;
}

