"use client";

import { useSyncExternalStore } from "react";
import type { AuthUser } from "./types";

export const AUTH_EVENT = "tabmail-auth-change";
const EPOCH = "tabmail_session_epoch";
const keys = [
  EPOCH,
  "tabmail_user",
  "tabmail_tenant_id",
];
const pending = new Set<AbortController>();

// Token rotation does not change the identity scope; account/tenant changes
// do. Do not include bearer credentials in SWR keys or diagnostics.
export function sessionScope(): string {
  if (typeof window === "undefined") return "server";
  const raw = localStorage.getItem("tabmail_user");
  let user: { id?: string; role?: string } | null = null;
  try {
    user = raw ? JSON.parse(raw) : null;
  } catch {
    /* invalid state is anonymous */
  }
  return JSON.stringify([
    localStorage.getItem(EPOCH),
    user?.id,
    user?.role,
    localStorage.getItem("tabmail_tenant_id"),
  ]);
}

export function assertSession(scope: string) {
  if (scope !== sessionScope())
    throw new DOMException(
      "Session changed; stale result discarded",
      "AbortError",
    );
}

export function advanceSession() {
  if (typeof window === "undefined") return;
  for (const c of pending) c.abort();
  pending.clear();
  localStorage.setItem(EPOCH, crypto.randomUUID());
  window.dispatchEvent(new Event(AUTH_EVENT));
}

export function sessionRequest(signal?: AbortSignal) {
  const scope = sessionScope();
  const controller = new AbortController();
  const abort = () => controller.abort();
  if (signal?.aborted) abort();
  signal?.addEventListener("abort", abort, { once: true });
  const changed = () => {
    if (scope !== sessionScope()) abort();
  };
  if (typeof window !== "undefined") {
    window.addEventListener("storage", changed);
    window.addEventListener(AUTH_EVENT, changed);
  }
  pending.add(controller);
  return {
    scope,
    signal: controller.signal,
    dispose() {
      pending.delete(controller);
      signal?.removeEventListener("abort", abort);
      if (typeof window !== "undefined") {
        window.removeEventListener("storage", changed);
        window.removeEventListener(AUTH_EVENT, changed);
      }
    },
  };
}

function subscribe(notify: () => void) {
  const onStorage = (e: StorageEvent) => {
    if (e.key === null || keys.includes(e.key)) notify();
  };
  window.addEventListener("storage", onStorage);
  window.addEventListener(AUTH_EVENT, notify);
  return () => {
    window.removeEventListener("storage", onStorage);
    window.removeEventListener(AUTH_EVENT, notify);
  };
}
export function useSessionScope() {
  return useSyncExternalStore(subscribe, sessionScope, () => "server");
}

export function clearSessionCredentials() {
  if (typeof window === "undefined") return;
  for (const key of [
    "tabmail_access_token",
    ...keys.filter((k) => k !== EPOCH),
  ])
    localStorage.removeItem(key);
  advanceSession();
}

// Called while holding the shared cookie lock. Installing the local identity
// is part of the credential mutation, not a later React effect.
export function installSession(accessToken: string, user: AuthUser) {
  if (
    localStorage.getItem("tabmail_access_token") === accessToken &&
    localStorage.getItem("tabmail_user") === JSON.stringify(user)
  )
    return;
  localStorage.setItem("tabmail_access_token", accessToken);
  localStorage.setItem("tabmail_user", JSON.stringify(user));
  localStorage.setItem("tabmail_tenant_id", user.tenant_id);
  for (const key of [
    "tabmail_mailbox_token",
    "tabmail_mailbox_address",
    "tabmail_mailbox_api_key",
    "tabmail_mailbox_api_key_address",
  ])
    localStorage.removeItem(key);
  advanceSession();
}
