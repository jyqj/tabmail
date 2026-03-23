import type { APIError } from "../types";

export interface RequestOptions {
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
  params?: Record<string, string | number>;
}

export interface EventStreamOptions {
  signal?: AbortSignal;
  onEvent: (event: { type: string; data: unknown }) => void;
}

export function getBaseUrl(): string {
  if (typeof window !== "undefined") {
    return process.env.NEXT_PUBLIC_API_URL || "";
  }
  return process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";
}

function getStoredKey(key: string): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(key);
}

function shouldUseMailboxToken(path: string, mailboxAddress: string | null): boolean {
  if (!path.startsWith("/api/v1/mailbox/")) return false;
  if (!mailboxAddress) return true;

  const parts = path.split("/");
  const encodedAddress = parts[4];
  if (!encodedAddress) return false;

  return decodeURIComponent(encodedAddress).toLowerCase() === mailboxAddress.toLowerCase();
}

export function buildHeaders(path: string, extra?: Record<string, string>) {
  const headers: Record<string, string> = { ...(extra || {}) };
  const adminKey = getStoredKey("tabmail_admin_key");
  const apiKey = getStoredKey("tabmail_api_key");
  const tenantId = getStoredKey("tabmail_tenant_id");
  const mailboxToken = getStoredKey("tabmail_mailbox_token");
  const mailboxAddress = getStoredKey("tabmail_mailbox_address");

  if (adminKey) {
    headers["X-Admin-Key"] = adminKey;
    if (tenantId) headers["X-Tenant-ID"] = tenantId;
  } else if (apiKey) {
    headers["X-API-Key"] = apiKey;
  } else if (mailboxToken && shouldUseMailboxToken(path, mailboxAddress)) {
    headers.Authorization = `Bearer ${mailboxToken}`;
  }

  return headers;
}

export async function request<T>(path: string, opts: RequestOptions = {}): Promise<T> {
  const base = getBaseUrl();
  const url = new URL(
    `${base}${path}`,
    typeof window !== "undefined" ? window.location.origin : undefined
  );

  if (opts.params) {
    for (const [k, v] of Object.entries(opts.params)) {
      if (v !== undefined && v !== null) {
        url.searchParams.set(k, String(v));
      }
    }
  }

  const headers = buildHeaders(path, opts.headers);
  if (opts.body) headers["Content-Type"] = "application/json";

  const res = await fetch(url.toString(), {
    method: opts.method || "GET",
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });

  if (!res.ok) {
    const err: APIError = await res.json().catch(() => ({
      error: { code: "UNKNOWN", message: res.statusText },
    }));
    throw err;
  }

  if (res.status === 204) return {} as T;

  const ct = res.headers.get("content-type") || "";
  if (ct.includes("message/rfc822") || ct.includes("text/plain")) {
    return (await res.text()) as unknown as T;
  }

  return res.json();
}

export async function streamEvents(
  path: string,
  { signal, onEvent }: EventStreamOptions,
  transform?: (data: unknown) => unknown
) {
  const res = await fetch(`${getBaseUrl()}${path}`, {
    method: "GET",
    headers: buildHeaders(path),
    signal,
  });

  if (!res.ok || !res.body) {
    const err: APIError = await res.json().catch(() => ({
      error: { code: "UNKNOWN", message: res.statusText },
    }));
    throw err;
  }

  const reader = res.body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  let currentEvent = "message";

  while (true) {
    const { value, done } = await reader.read();
    if (done) break;

    buffer += decoder.decode(value, { stream: true });
    let boundary = buffer.indexOf("\n\n");

    while (boundary >= 0) {
      const chunk = buffer.slice(0, boundary);
      buffer = buffer.slice(boundary + 2);

      let dataLine = "";
      for (const line of chunk.split("\n")) {
        if (line.startsWith("event:")) currentEvent = line.slice(6).trim();
        if (line.startsWith("data:")) dataLine += line.slice(5).trim();
      }

      if (dataLine) {
        try {
          const parsed = JSON.parse(dataLine) as unknown;
          onEvent({ type: currentEvent, data: transform ? transform(parsed) : parsed });
        } catch {
          onEvent({ type: currentEvent, data: dataLine });
        }
      }

      currentEvent = "message";
      boundary = buffer.indexOf("\n\n");
    }
  }
}
