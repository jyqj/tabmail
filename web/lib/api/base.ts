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

const MAILBOX_API_KEY_ADDRESS_KEY = "tabmail_mailbox_api_key_address";
const MAILBOX_API_KEY_KEY = "tabmail_mailbox_api_key";

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

function setStoredKey(key: string, value: string | null) {
  if (typeof window === "undefined") return;
  if (value && value.trim()) localStorage.setItem(key, value.trim());
  else localStorage.removeItem(key);
}

function notifyAuthChange() {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event("tabmail-auth-change"));
  }
}

function getMailboxAddressFromPath(path: string): string | null {
  if (!path.startsWith("/api/v1/mailbox/")) return null;

  const parts = path.split("/");
  const encodedAddress = parts[4];
  if (!encodedAddress) return null;

  try {
    return decodeURIComponent(encodedAddress).toLowerCase();
  } catch {
    return null;
  }
}

function shouldUseMailboxCredential(path: string, mailboxAddress: string | null): boolean {
  const pathAddress = getMailboxAddressFromPath(path);
  if (!pathAddress) return false;
  if (!mailboxAddress) return true;

  return pathAddress === mailboxAddress.toLowerCase();
}

function hasExplicitAuthHeader(headers: Record<string, string>) {
  return Boolean(
    headers.Authorization ||
      headers.authorization ||
      headers["X-API-Key"] ||
      headers["x-api-key"]
  );
}

export function getMailboxAPIKeySnapshot() {
  return {
    address: getStoredKey(MAILBOX_API_KEY_ADDRESS_KEY),
    key: getStoredKey(MAILBOX_API_KEY_KEY),
  };
}

export function setMailboxAPIKeyAuth(address: string | null, apiKey: string | null) {
  setStoredKey(MAILBOX_API_KEY_ADDRESS_KEY, address?.trim().toLowerCase() || null);
  setStoredKey(MAILBOX_API_KEY_KEY, apiKey?.trim() || null);
  notifyAuthChange();
}

export function clearMailboxAPIKeyAuth() {
  setMailboxAPIKeyAuth(null, null);
}

function requestUsedAccessToken(headers: Record<string, string>): boolean {
  const accessToken = getStoredKey("tabmail_access_token");
  return Boolean(accessToken && headers.Authorization === `Bearer ${accessToken}`);
}

function hasStoredAdminSession(): boolean {
  const rawUser = getStoredKey("tabmail_user");
  if (!rawUser) return false;
  try {
    const user = JSON.parse(rawUser) as { role?: string };
    return (
      user.role === "super_admin" ||
      user.role === "admin"
    );
  } catch {
    return false;
  }
}

export function buildHeaders(path: string, extra?: Record<string, string>) {
  const headers: Record<string, string> = { ...(extra || {}) };
  if (hasExplicitAuthHeader(headers)) return headers;

  const accessToken = getStoredKey("tabmail_access_token");
  const tenantId = getStoredKey("tabmail_tenant_id");
  const mailboxToken = getStoredKey("tabmail_mailbox_token");
  const mailboxAddress = getStoredKey("tabmail_mailbox_address");
  const mailboxAPIKey = getStoredKey(MAILBOX_API_KEY_KEY);
  const mailboxAPIKeyAddress = getStoredKey(MAILBOX_API_KEY_ADDRESS_KEY);

  // Mailbox-scoped inbox tokens must win for their mailbox paths, even when a
  // console JWT or a stored mailbox API key is also present.
  if (mailboxToken && shouldUseMailboxCredential(path, mailboxAddress)) {
    headers.Authorization = `Bearer ${mailboxToken}`;
  } else if (accessToken && hasStoredAdminSession()) {
    // Admin sessions keep JWT semantics for admin-only mailbox operations such
    // as break-glass/delete/purge, even if a mailbox API key is stored.
    headers.Authorization = `Bearer ${accessToken}`;
    if (tenantId) headers["X-Tenant-ID"] = tenantId;
  } else if (mailboxAPIKey && shouldUseMailboxCredential(path, mailboxAPIKeyAddress)) {
    // Explicit mailbox API-key access is scoped to the matching mailbox path.
    // Use X-API-Key rather than Authorization so mailbox-token/JWT bearer
    // semantics remain distinct and unaffected outside this mailbox.
    headers["X-API-Key"] = mailboxAPIKey;
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
  if (res.status === 401 && requestUsedAccessToken(headers) && getStoredKey("tabmail_refresh_token")) {
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

// Singleflight: coalesce concurrent refresh calls so only one network request
// is made per rotation cycle. Subsequent 401 handlers await the same promise.
let refreshPromise: Promise<boolean> | null = null;

async function tryRefreshToken(): Promise<boolean> {
  if (refreshPromise) return refreshPromise;

  refreshPromise = doRefreshToken().finally(() => {
    refreshPromise = null;
  });

  return refreshPromise;
}

async function doRefreshToken(): Promise<boolean> {
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
  async function connect() {
    const res = await fetch(`${getBaseUrl()}${path}`, {
      method: "GET",
      headers: buildHeaders(path),
      signal,
    });

    if (!res.ok || !res.body) {
      // If 401 and we have tokens, try refresh then reconnect once
      if (res.status === 401 && requestUsedAccessToken(buildHeaders(path)) && getStoredKey("tabmail_refresh_token")) {
        const refreshed = await tryRefreshToken();
        if (refreshed) {
          const retryRes = await fetch(`${getBaseUrl()}${path}`, {
            method: "GET",
            headers: buildHeaders(path),
            signal,
          });
          if (retryRes.ok && retryRes.body) return retryRes;
        }
      }

      const err: APIError = await res.json().catch(() => ({
        error: { code: "UNKNOWN", message: res.statusText },
      }));
      throw err;
    }

    return res;
  }

  async function readStream(res: Response) {
    const reader = res.body!.getReader();
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

  const initialRes = await connect();
  try {
    await readStream(initialRes);
  } catch (streamErr) {
    // Stream read failed (connection dropped). If not intentionally aborted,
    // try refresh + reconnect once.
    if (signal?.aborted) throw streamErr;

    const refreshed = await tryRefreshToken();
    if (!refreshed) throw streamErr;

    const reconnectRes = await connect();
    await readStream(reconnectRes);
  }
}
