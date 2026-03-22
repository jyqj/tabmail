"use client";

import {
  createContext,
  useContext,
  useCallback,
  useSyncExternalStore,
  type ReactNode,
} from "react";

type AuthLevel = "public" | "mailbox" | "tenant" | "admin";

interface AuthSnapshot {
  adminKey: string | null;
  apiKey: string | null;
  tenantId: string | null;
  mailboxToken: string | null;
  mailboxAddress: string | null;
}

interface AuthState extends AuthSnapshot {
  level: AuthLevel;
  setAdminKey: (key: string | null) => void;
  setApiKey: (key: string | null) => void;
  setTenantId: (id: string | null) => void;
  setMailboxAuth: (address: string | null, token: string | null) => void;
  clearMailboxAuth: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);
const AUTH_EVENT = "tabmail-auth-change";

function readSnapshot(): AuthSnapshot {
  if (typeof window === "undefined") {
    return {
      adminKey: null,
      apiKey: null,
      tenantId: null,
      mailboxToken: null,
      mailboxAddress: null,
    };
  }

  return {
    adminKey: localStorage.getItem("tabmail_admin_key"),
    apiKey: localStorage.getItem("tabmail_api_key"),
    tenantId: localStorage.getItem("tabmail_tenant_id"),
    mailboxToken: localStorage.getItem("tabmail_mailbox_token"),
    mailboxAddress: localStorage.getItem("tabmail_mailbox_address"),
  };
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

export function AuthProvider({ children }: { children: ReactNode }) {
  const snapshot = useSyncExternalStore(subscribe, readSnapshot, readSnapshot);

  const setAdminKey = useCallback((key: string | null) => {
    setStorageItem("tabmail_admin_key", key?.trim() || null);
    notify();
  }, []);

  const setApiKey = useCallback((key: string | null) => {
    setStorageItem("tabmail_api_key", key?.trim() || null);
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
    setStorageItem("tabmail_admin_key", null);
    setStorageItem("tabmail_api_key", null);
    setStorageItem("tabmail_tenant_id", null);
    setStorageItem("tabmail_mailbox_address", null);
    setStorageItem("tabmail_mailbox_token", null);
    notify();
  }, []);

  const level: AuthLevel = snapshot.adminKey
    ? "admin"
    : snapshot.apiKey
    ? "tenant"
    : snapshot.mailboxToken
    ? "mailbox"
    : "public";

  return (
    <AuthContext.Provider
      value={{
        ...snapshot,
        level,
        setAdminKey,
        setApiKey,
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
