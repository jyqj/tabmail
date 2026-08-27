"use client";

import {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useState,
  useRef,
  useSyncExternalStore,
  type ReactNode,
} from "react";
import type { AuthUser, EffectivePermission } from "@/lib/types";
import type { PermissionLevel } from "@/lib/permissions";
import { bootstrapSession, getAccessToken, setAccessToken } from "@/lib/api/base";

type AuthLevel = PermissionLevel;

interface AuthSnapshot {
  // JWT auth: the access token lives only in memory (see lib/api/base.ts);
  // the refresh token lives in an httpOnly cookie the client cannot read.
  accessToken: string | null;
  user: AuthUser | null;
  tenantId: string | null;
  // Mailbox auth
  mailboxToken: string | null;
  mailboxAddress: string | null;
}

interface AuthState extends AuthSnapshot {
  level: AuthLevel;
  hydrated: boolean;
  permissions: EffectivePermission | null;
  permissionsLoading: boolean;
  permissionsError: boolean;
  // JWT auth
  loginWithTokens: (accessToken: string, user: AuthUser) => void;
  setTenantId: (id: string | null) => void;
  setMailboxAuth: (address: string | null, token: string | null) => void;
  clearMailboxAuth: () => void;
  logout: () => void;
}

const AuthContext = createContext<AuthState | null>(null);
const AUTH_EVENT = "tabmail-auth-change";
let cachedSnapshot: AuthSnapshot | null = null;
let cachedRawUser: string | null = null;

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
    return serverSnapshot;
  }

  const rawUser = localStorage.getItem("tabmail_user");
  const nextSnapshot: AuthSnapshot = {
    accessToken: getAccessToken(),
    user: parseUser(rawUser),
    tenantId: localStorage.getItem("tabmail_tenant_id"),
    mailboxToken: localStorage.getItem("tabmail_mailbox_token"),
    mailboxAddress: localStorage.getItem("tabmail_mailbox_address"),
  };

  if (
    cachedSnapshot &&
    cachedSnapshot.accessToken === nextSnapshot.accessToken &&
    cachedRawUser === rawUser &&
    cachedSnapshot.tenantId === nextSnapshot.tenantId &&
    cachedSnapshot.mailboxToken === nextSnapshot.mailboxToken &&
    cachedSnapshot.mailboxAddress === nextSnapshot.mailboxAddress
  ) {
    return cachedSnapshot;
  }

  cachedSnapshot = nextSnapshot;
  cachedRawUser = rawUser;
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
  user: null,
  tenantId: null,
  mailboxToken: null,
  mailboxAddress: null,
};

export function AuthProvider({ children }: { children: ReactNode }) {
  const [hydrated, setHydrated] = useState(false);
  const [permissions, setPermissions] = useState<EffectivePermission | null>(null);
  const [permissionsLoading, setPermissionsLoading] = useState(false);
  const [permissionsError, setPermissionsError] = useState(false);
  const permsFetchedForToken = useRef<string | null>(null);

  useEffect(() => {
    const timer = window.setTimeout(() => setHydrated(true), 0);
    return () => window.clearTimeout(timer);
  }, []);

  const snapshot = useSyncExternalStore(
    subscribe,
    hydrated ? readSnapshot : () => serverSnapshot,
    () => serverSnapshot,
  );

  // A reloaded tab has a stored user profile but no in-memory access token:
  // silently exchange the httpOnly refresh cookie for a fresh one. On failure
  // bootstrapSession clears the stale profile and the UI settles on public.
  useEffect(() => {
    if (!hydrated) return;
    if (snapshot.user && !snapshot.accessToken) {
      void bootstrapSession();
    }
    // Run once when hydration completes; token arrival re-renders via the
    // auth-change event, not this effect.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [hydrated]);

  // Load permissions when a JWT user session is present
  useEffect(() => {
    const token = snapshot.accessToken;
    if (!token || !snapshot.user) {
      // No session -- clear cached permissions
      if (permissions !== null) setPermissions(null);
      setPermissionsError(false);
      permsFetchedForToken.current = null;
      return;
    }
    // Avoid re-fetching for the same token
    if (permsFetchedForToken.current === token) return;
    permsFetchedForToken.current = token;

    let cancelled = false;
    setPermissionsLoading(true);

    (async () => {
      try {
        // Dynamic import to avoid circular dependency with api modules
        const { getMyPermissions } = await import("@/lib/api/permissions");
        const res = await getMyPermissions();
        if (!cancelled) {
          setPermissions(res.data);
          setPermissionsError(false);
        }
      } catch {
        // Permission load failed — default to restrictive permissions to avoid
        // showing features the user may not have access to.
        if (!cancelled) {
          setPermissions({
            can_send: false,
            daily_send_quota: 0,
            daily_receive_quota: 0,
            max_mailboxes: 0,
            max_domains: 0,
            allowed_zone_ids: null,
            can_create_domains: false,
            can_create_routes: false,
            can_create_api_keys: false,
          });
          setPermissionsError(true);
        }
      } finally {
        if (!cancelled) setPermissionsLoading(false);
      }
    })();

    return () => { cancelled = true; };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [snapshot.accessToken, snapshot.user]);

  const loginWithTokens = useCallback((accessToken: string, user: AuthUser) => {
    setAccessToken(accessToken);
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
    setAccessToken(null);
    localStorage.removeItem("tabmail_user");
    setStorageItem("tabmail_tenant_id", null);
    setStorageItem("tabmail_mailbox_address", null);
    setStorageItem("tabmail_mailbox_token", null);
    setPermissions(null);
    setPermissionsError(false);
    permsFetchedForToken.current = null;
    notify();
  }, []);

  // A stored user profile counts as a session even while the bootstrap
  // refresh is still in flight; requests self-heal through the 401-refresh
  // path, and a failed bootstrap clears the profile.
  const level: AuthLevel = snapshot.user
    ? (snapshot.user.role === "super_admin"
        ? "super_admin"
        : snapshot.user.role === "admin"
        ? "admin"
        : "user")
    : snapshot.mailboxToken
    ? "mailbox"
    : "public";

  return (
    <AuthContext.Provider
      value={{
        ...snapshot,
        level,
        hydrated,
        permissions,
        permissionsLoading,
        permissionsError,
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

export function usePermissions(): EffectivePermission | null {
  const { permissions } = useAuth();
  return permissions;
}
