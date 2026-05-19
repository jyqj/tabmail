"use client";

import {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useState,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import type { AuthUser } from "@/lib/types";

type AuthLevel = "public" | "mailbox" | "admin" | "user";

interface AuthSnapshot {
  // JWT auth (new)
  accessToken: string | null;
  refreshToken: string | null;
  user: AuthUser | null;
  tenantId: string | null;
  // Mailbox auth
  mailboxToken: string | null;
  mailboxAddress: string | null;
}

interface AuthState extends AuthSnapshot {
  level: AuthLevel;
  hydrated: boolean;
  // JWT auth
  loginWithTokens: (accessToken: string, refreshToken: string, user: AuthUser) => void;
  setTenantId: (id: string | null) => void;
  setMailboxAuth: (address: string | null, token: string | null) => void;
  clearMailboxAuth: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);
const AUTH_EVENT = "tabmail-auth-change";
let cachedSnapshot: AuthSnapshot | null = null;

function parseUser(raw: string | null): AuthUser | null {
  if (!raw) return null;
  try {
    return JSON.parse(raw) as AuthUser;
  } catch {
    return null;
  }
}

function readSnapshot(): AuthSnapshot {
  if (typeof window === "undefined") {
    return {
      accessToken: null,
      refreshToken: null,
      user: null,
      tenantId: null,
      mailboxToken: null,
      mailboxAddress: null,
    };
  }

  const nextSnapshot: AuthSnapshot = {
    accessToken: localStorage.getItem("tabmail_access_token"),
    refreshToken: localStorage.getItem("tabmail_refresh_token"),
    user: parseUser(localStorage.getItem("tabmail_user")),
    tenantId: localStorage.getItem("tabmail_tenant_id"),
    mailboxToken: localStorage.getItem("tabmail_mailbox_token"),
    mailboxAddress: localStorage.getItem("tabmail_mailbox_address"),
  };

  if (
    cachedSnapshot &&
    cachedSnapshot.accessToken === nextSnapshot.accessToken &&
    cachedSnapshot.refreshToken === nextSnapshot.refreshToken &&
    cachedSnapshot.tenantId === nextSnapshot.tenantId &&
    cachedSnapshot.mailboxToken === nextSnapshot.mailboxToken &&
    cachedSnapshot.mailboxAddress === nextSnapshot.mailboxAddress
  ) {
    return cachedSnapshot;
  }

  cachedSnapshot = nextSnapshot;
  return nextSnapshot;
}

function subscribe(onStoreChange: () => void) {
  if (typeof window === "undefined") return () => {};

  const handler = () => onStoreChange();
  window.addEventListener("storage", handler);
  window.addEventListener(AUTH_EVENT, handler);
  return () => {
    window.removeEventListener("storage", handler);
    window.removeEventListener(AUTH_EVENT, handler);
  };
}

function notify() {
  if (typeof window !== "undefined") {
    window.dispatchEvent(new Event(AUTH_EVENT));
  }
}

function setStorageItem(key: string, value: string | null) {
  if (typeof window === "undefined") return;
  if (value && value.trim()) localStorage.setItem(key, value);
  else localStorage.removeItem(key);
}

const serverSnapshot: AuthSnapshot = {
  accessToken: null,
  refreshToken: null,
  user: null,
  tenantId: null,
  mailboxToken: null,
  mailboxAddress: null,
};

export function AuthProvider({ children }: { children: ReactNode }) {
  const [hydrated, setHydrated] = useState(false);
  useEffect(() => {
    const timer = window.setTimeout(() => setHydrated(true), 0);
    return () => window.clearTimeout(timer);
  }, []);

  const snapshot = useSyncExternalStore(
    subscribe,
    hydrated ? readSnapshot : () => serverSnapshot,
    () => serverSnapshot,
  );

  const loginWithTokens = useCallback((accessToken: string, refreshToken: string, user: AuthUser) => {
    setStorageItem("tabmail_access_token", accessToken);
    setStorageItem("tabmail_refresh_token", refreshToken);
    localStorage.setItem("tabmail_user", JSON.stringify(user));
    setStorageItem("tabmail_tenant_id", user.tenant_id);
    notify();
  }, []);

  const setTenantId = useCallback((id: string | null) => {
    setStorageItem("tabmail_tenant_id", id?.trim() || null);
    notify();
  }, []);

  const setMailboxAuth = useCallback((address: string | null, token: string | null) => {
    setStorageItem("tabmail_mailbox_address", address?.trim().toLowerCase() || null);
    setStorageItem("tabmail_mailbox_token", token?.trim() || null);
    notify();
  }, []);

  const clearMailboxAuth = useCallback(() => {
    setStorageItem("tabmail_mailbox_address", null);
    setStorageItem("tabmail_mailbox_token", null);
    notify();
  }, []);

  const logout = useCallback(() => {
    setStorageItem("tabmail_access_token", null);
    setStorageItem("tabmail_refresh_token", null);
    localStorage.removeItem("tabmail_user");
    setStorageItem("tabmail_tenant_id", null);
    setStorageItem("tabmail_mailbox_address", null);
    setStorageItem("tabmail_mailbox_token", null);
    notify();
  }, []);

  const level: AuthLevel = snapshot.accessToken && snapshot.user
    ? (snapshot.user.role === "admin" ? "admin" : "user")
    : snapshot.mailboxToken
    ? "mailbox"
    : "public";

  return (
    <AuthContext.Provider
      value={{
        ...snapshot,
        level,
        hydrated,
        loginWithTokens,
        setTenantId,
        setMailboxAuth,
        clearMailboxAuth,
        logout,
      }}
    >
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
