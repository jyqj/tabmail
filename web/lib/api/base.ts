import {
  installSession,
  assertSession,
  clearSessionCredentials,
  sessionRequest,
  sessionScope,
} from "../session";
import type { APIError, LoginResponse, APIResponse } from "../types";

export interface RequestOptions {
  signal?: AbortSignal;
  responseType?: "json" | "blob" | "text";
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

function notifyAuthChange() {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event("tabmail-auth-change"));
  }
}

function hasExplicitAuthHeader(headers: Record<string, string>) {
  return Boolean(
    headers.Authorization ||
      headers.authorization ||
      headers["X-API-Key"] ||
      headers["x-api-key"],
  );
}

function requestUsedAccessToken(headers: Record<string, string>): boolean {
  const accessToken = getStoredKey("tabmail_access_token");
  return Boolean(
    accessToken && headers.Authorization === `Bearer ${accessToken}`,
  );
}

export function buildHeaders(path: string, extra?: Record<string, string>) {
  const headers: Record<string, string> = { ...(extra || {}) };
  if (hasExplicitAuthHeader(headers)) return headers;
  if (
    [
      "/api/v1/auth/login",
      "/api/v1/auth/register",
      "/api/v1/company/activate",
    ].includes(path)
  )
    return headers;

  const accessToken = getStoredKey("tabmail_access_token");
  const tenantId = getStoredKey("tabmail_tenant_id");
  if (accessToken) {
    headers.Authorization = `Bearer ${accessToken}`;
    if (tenantId) headers["X-Tenant-ID"] = tenantId;
  }

  return headers;
}

const cookiePaths = new Set([
  "/api/v1/auth/login",
  "/api/v1/auth/register",
  "/api/v1/auth/logout",
  "/api/v1/auth/change-password",
]);

async function credentialRequest<T>(
  path: string,
  opts: RequestOptions,
): Promise<T> {
  const scope = sessionScope();
  const run = async () => {
    assertSession(scope);
    if (opts.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const headers = buildHeaders(path, opts.headers);
    headers["Content-Type"] = "application/json";
    const isLogout = path.endsWith("/logout");
    let res = await fetch(`${getBaseUrl()}${path}`, {
      method: opts.method || "POST",
      credentials: "include",
      headers,
      body: JSON.stringify(opts.body ?? {}),
    });
    assertSession(scope);
    if (res.status === 401 && requestUsedAccessToken(headers)) {
      // Already inside the shared lock: do not recursively acquire it.
      const renewed = await doRefreshToken(
        scope,
        headers.Authorization.slice(7),
      );
      if (scope !== sessionScope()) {
        if (isLogout && !getStoredKey("tabmail_access_token")) return {} as T;
        assertSession(scope);
      }
      if (renewed) {
        if (!isLogout)
          throw {
            error: {
              code: "RETRY_REQUIRED",
              message: "Session renewed. Review and submit again.",
            },
          };
        res = await fetch(`${getBaseUrl()}${path}`, {
          method: "POST",
          credentials: "include",
          headers: {
            ...buildHeaders(path),
            "Content-Type": "application/json",
          },
          body: JSON.stringify(opts.body ?? {}),
        });
      }
    }
    const data =
      res.status === 204
        ? {}
        : await res
            .json()
            .catch(() => ({
              error: { code: "INVALID_RESPONSE", message: res.statusText },
            }));
    assertSession(scope);
    if (!res.ok) throw data;
    if (isLogout || path.endsWith("/change-password"))
      clearSessionCredentials();
    else {
      const login = data as APIResponse<LoginResponse>;
      if (!login.data?.access_token || !login.data?.user?.id)
        throw {
          error: {
            code: "INVALID_RESPONSE",
            message: "Incomplete login response",
          },
        };
      installSession(login.data.access_token, login.data.user);
    }
    return data as T;
  };
  // Cookie-mutating requests require a shared lock. Company deployment uses
  // HTTPS; lack of Web Locks is explicit, rather than an unsafe per-tab lock.
  if (typeof navigator === "undefined" || !navigator.locks)
    throw {
      error: {
        code: "SECURE_CONTEXT_REQUIRED",
        message: "Use HTTPS and a browser supporting Web Locks.",
      },
    };
  return await navigator.locks.request("tabmail-refresh", run);
}

export async function request<T>(
  path: string,
  opts: RequestOptions = {},
): Promise<T> {
  if (cookiePaths.has(path)) return credentialRequest<T>(path, opts);
  const lease = sessionRequest(opts.signal);
  try {
    const base = getBaseUrl();
    const url = new URL(
      `${base}${path}`,
      typeof window !== "undefined" ? window.location.origin : undefined,
    );
    for (const [key, value] of Object.entries(opts.params || {}))
      if (value !== undefined && value !== null)
        url.searchParams.set(key, String(value));
    const form =
      typeof FormData !== "undefined" && opts.body instanceof FormData;
    const headers = buildHeaders(path, opts.headers);
    if (opts.body !== undefined && !form)
      headers["Content-Type"] = "application/json";
    const init: RequestInit = {
      method: opts.method || "GET",
      headers,
      credentials: "include",
      signal: lease.signal,
      body: form
        ? (opts.body as FormData)
        : opts.body === undefined
          ? undefined
          : JSON.stringify(opts.body),
    };
    const usedSessionToken = requestUsedAccessToken(headers);
    const execute = (options: RequestInit) => {
      assertSession(lease.scope);
      return fetch(url.toString(), options);
    };
    let res = await execute(init);
    assertSession(lease.scope);
    if (res.status === 401 && usedSessionToken) {
      const renewed = await tryRefreshToken(
        headers.Authorization?.slice(7) || "",
        lease.scope,
      );
      assertSession(lease.scope);
      if (renewed) {
        const safeRead = ["GET", "HEAD", "OPTIONS"].includes(
          (opts.method || "GET").toUpperCase(),
        );
        const idempotent =
          Boolean(opts.headers?.["Idempotency-Key"]) ||
          path === "/api/v1/auth/logout";
        if (!safeRead && !idempotent)
          throw {
            error: {
              code: "RETRY_REQUIRED",
              message: "Session renewed. Review and submit again.",
            },
          };
        const fresh = buildHeaders(path, opts.headers);
        if (opts.body !== undefined && !form)
          fresh["Content-Type"] = "application/json";
        res = await execute({ ...init, headers: fresh });
        assertSession(lease.scope);
      }
    }
    if (!res.ok) {
      const err: APIError = await res
        .json()
        .catch(() => ({ error: { code: "UNKNOWN", message: res.statusText } }));
      assertSession(lease.scope);
      throw err;
    }
    if (res.status === 204) return {} as T;
    const ct = res.headers.get("content-type") || "";
    let result: unknown;
    if (opts.responseType === "blob") result = await res.blob();
    else if (
      opts.responseType === "text" ||
      ct.includes("message/rfc822") ||
      ct.includes("text/plain")
    )
      result = await res.text();
    else result = await res.json();
    assertSession(lease.scope);
    return result as T;
  } finally {
    lease.dispose();
  }
}

let refreshPromise: Promise<boolean> | null = null;

// Web Locks serializes the shared HttpOnly refresh cookie across tabs. The
// second tab observes the first tab's replacement access token and does not
// replay the consumed refresh token. Unsupported/insecure contexts fail closed
// and require login rather than attempting an unsafe storage-based pseudo-CAS.
export async function tryRefreshToken(
  failedToken: string,
  scope = sessionScope(),
): Promise<boolean> {
  if (refreshPromise) return refreshPromise;
  if (typeof navigator === "undefined" || !navigator.locks) return false;
  const promise = (async () =>
    await navigator.locks.request("tabmail-refresh", async () => {
      if (scope !== sessionScope()) return false;
      const current = getStoredKey("tabmail_access_token");
      if (!current) return false;
      if (current !== failedToken) return true;
      return doRefreshToken(scope, failedToken);
    }))().finally(() => {
    refreshPromise = null;
  });
  refreshPromise = promise;
  return promise;
}

async function doRefreshToken(scope: string, token: string): Promise<boolean> {
  const lease = sessionRequest();
  try {
    const res = await fetch(`${getBaseUrl()}/api/v1/auth/refresh`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "include",
      signal: lease.signal,
    });
    if (
      scope !== sessionScope() ||
      token !== getStoredKey("tabmail_access_token")
    )
      return false;
    if (!res.ok) {
      if (res.status === 401 || res.status === 403) clearSessionCredentials();
      // Preserve state on 5xx, network failures and unexpected responses.
      return false;
    }
    const data = await res.json();
    if (
      scope !== sessionScope() ||
      token !== getStoredKey("tabmail_access_token")
    )
      return false;
    if (typeof data?.data?.access_token !== "string" || !data.data.access_token)
      return false;
    const raw = getStoredKey("tabmail_user");
    let currentUser: { id?: string } | null = null;
    try {
      currentUser = raw ? JSON.parse(raw) : null;
    } catch {
      return false;
    }
    if (data.data.user && data.data.user.id !== currentUser?.id) return false;
    localStorage.setItem("tabmail_access_token", data.data.access_token);
    if (data.data.user)
      localStorage.setItem("tabmail_user", JSON.stringify(data.data.user));
    notifyAuthChange();
    return true;
  } catch {
    return false;
  } finally {
    lease.dispose();
  }
}

function delay(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise((resolve, reject) => {
    const aborted = () => {
      clearTimeout(timer);
      reject(new DOMException("Aborted", "AbortError"));
    };
    const timer = setTimeout(() => {
      signal.removeEventListener("abort", aborted);
      resolve();
    }, ms);
    if (signal.aborted) aborted();
    else signal.addEventListener("abort", aborted, { once: true });
  });
}

export async function streamEvents(
  path: string,
  { signal, onEvent }: EventStreamOptions,
  transform?: (data: unknown) => unknown,
) {
  const lease = sessionRequest(signal);
  let cursor = "";
  let failures = 0;
  try {
    while (!lease.signal.aborted) {
      assertSession(lease.scope);
      try {
        const headers = buildHeaders(path);
        if (cursor) headers["Last-Event-ID"] = cursor;
        let res = await fetch(`${getBaseUrl()}${path}`, {
          headers,
          credentials: "include",
          signal: lease.signal,
        });
        assertSession(lease.scope);
        if (res.status === 401 && requestUsedAccessToken(headers)) {
          if (
            !(await tryRefreshToken(
              headers.Authorization?.slice(7) || "",
              lease.scope,
            ))
          )
            throw new Error("Authentication required");
          assertSession(lease.scope);
          const retry = buildHeaders(path);
          if (cursor) retry["Last-Event-ID"] = cursor;
          res = await fetch(`${getBaseUrl()}${path}`, {
            headers: retry,
            credentials: "include",
            signal: lease.signal,
          });
        }
        if (res.status === 401 || res.status === 403)
          throw new DOMException(
            "Stream permission revoked",
            "NotAllowedError",
          );
        if (!res.ok || !res.body)
          throw new Error("Stream temporarily unavailable");
        // Re-sync on every connection, including a normal EOF/reconnect.
        onEvent({ type: "resync", data: null });
        const reader = res.body.getReader();
        const decoder = new TextDecoder();
        let buffer = "";
        try {
          while (!lease.signal.aborted) {
            const { value, done } = await reader.read();
            assertSession(lease.scope);
            if (done) break;
            buffer += decoder
              .decode(value, { stream: true })
              .replace(/\r\n/g, "\n");
            if (buffer.length > 1024 * 1024)
              throw new Error("Oversized event stream frame");
            let boundary: number;
            while ((boundary = buffer.indexOf("\n\n")) >= 0) {
              const chunk = buffer.slice(0, boundary);
              buffer = buffer.slice(boundary + 2);
              let event = "message";
              const lines: string[] = [];
              for (const line of chunk.split("\n")) {
                if (line.startsWith("id:")) cursor = line.slice(3).trim();
                if (line.startsWith("event:")) event = line.slice(6).trim();
                if (line.startsWith("data:"))
                  lines.push(line.slice(5).trimStart());
              }
              if (!lines.length) continue;
              let data: unknown = lines.join("\n");
              try {
                data = JSON.parse(data as string);
              } catch {
                /* raw event data */
              }
              assertSession(lease.scope);
              onEvent({
                type: event,
                data: transform ? transform(data) : data,
              });
              failures = 0;
            }
          }
        } finally {
          await reader.cancel().catch(() => undefined);
          reader.releaseLock();
        }
      } catch (error) {
        assertSession(lease.scope);
        if (
          lease.signal.aborted ||
          (error instanceof DOMException && error.name === "NotAllowedError")
        )
          throw error;
      }
      await delay(
        Math.min(15000, 1000 * 2 ** Math.min(failures++, 4)),
        lease.signal,
      );
    }
  } finally {
    lease.dispose();
  }
}
