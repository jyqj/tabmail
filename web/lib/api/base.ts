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
  const accessToken = getStoredKey("tabmail_access_token");
  const tenantId = getStoredKey("tabmail_tenant_id");
  const mailboxToken = getStoredKey("tabmail_mailbox_token");
  const mailboxAddress = getStoredKey("tabmail_mailbox_address");

  // Mailbox-scoped inbox tokens must win for their mailbox paths, even when a
  // console JWT is also present.
  if (mailboxToken && shouldUseMailboxToken(path, mailboxAddress)) {
    headers.Authorization = `Bearer ${mailboxToken}`;
  } else if (accessToken) {
    headers.Authorization = `Bearer ${accessToken}`;
    if (tenantId) headers["X-Tenant-ID"] = tenantId;
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

  let res = await fetch(url.toString(), {
    method: opts.method || "GET",
    headers,
    body: opts.body ? JSON.stringify(opts.body) : undefined,
  });

  // Auto-refresh on 401 if we have a refresh token
  if (res.status === 401 && getStoredKey("tabmail_access_token") && getStoredKey("tabmail_refresh_token")) {
    const refreshed = await tryRefreshToken();
    if (refreshed) {
      // Retry with new token
      const retryHeaders = buildHeaders(path, opts.headers);
      if (opts.body) retryHeaders["Content-Type"] = "application/json";
      res = await fetch(url.toString(), {
        method: opts.method || "GET",
        headers: retryHeaders,
        body: opts.body ? JSON.stringify(opts.body) : undefined,
      });
    }
  }

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

async function tryRefreshToken(): Promise<boolean> {
  const refreshToken = getStoredKey("tabmail_refresh_token");
  if (!refreshToken) return false;

  try {
    const base = getBaseUrl();
    const res = await fetch(`${base}/api/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ refresh_token: refreshToken }),
    });

    if (!res.ok) {
      // Refresh failed — clear tokens
      if (typeof window !== "undefined") {
        localStorage.removeItem("tabmail_access_token");
        localStorage.removeItem("tabmail_refresh_token");
        localStorage.removeItem("tabmail_user");
        window.dispatchEvent(new Event("tabmail-auth-change"));
      }
      return false;
    }

    const data = await res.json();
    if (typeof window !== "undefined" && data?.data) {
      localStorage.setItem("tabmail_access_token", data.data.access_token);
      localStorage.setItem("tabmail_refresh_token", data.data.refresh_token);
      window.dispatchEvent(new Event("tabmail-auth-change"));
    }
    return true;
  } catch {
    return false;
  }
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
