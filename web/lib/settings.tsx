"use client";

import {
  createContext,
  useContext,
  useCallback,
  useEffect,
  useSyncExternalStore,
  type ReactNode,
} from "react";

export interface Settings {
  autoRefresh: boolean;
  refreshInterval: number;
  preferSSE: boolean;
  defaultTab: "html" | "text" | "source";
  timeFormat: "relative" | "absolute";
}

const STORAGE_KEY = "tabmail-settings";

const defaults: Settings = {
  autoRefresh: true,
  refreshInterval: 10,
  preferSSE: true,
  defaultTab: "html",
  timeFormat: "relative",
};

const listeners = new Set<() => void>();
let cachedRaw: string | null | undefined;
let cachedSettings: Settings = defaults;

function subscribe(cb: () => void) {
  listeners.add(cb);
  return () => { listeners.delete(cb); };
}

function getSnapshot(): Settings {
  if (typeof window === "undefined") return defaults;
  try {
    const raw = localStorage.getItem(STORAGE_KEY);
    if (raw === cachedRaw) {
      return cachedSettings;
    }
    cachedRaw = raw;
    cachedSettings = raw ? { ...defaults, ...JSON.parse(raw) } : defaults;
    return cachedSettings;
  } catch {
    cachedRaw = null;
    cachedSettings = defaults;
    return defaults;
  }
}

function getServerSnapshot(): Settings {
  return defaults;
}

interface SettingsContextValue {
  settings: Settings;
  update: (patch: Partial<Settings>) => void;
}

const SettingsContext = createContext<SettingsContextValue | null>(null);

export function SettingsProvider({ children }: { children: ReactNode }) {
  const settings = useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);

  useEffect(() => { listeners.forEach((cb) => cb()); }, []);

  const update = useCallback((patch: Partial<Settings>) => {
    const current = getSnapshot();
    localStorage.setItem(STORAGE_KEY, JSON.stringify({ ...current, ...patch }));
    listeners.forEach((cb) => cb());
  }, []);

  return (
    <SettingsContext value={{ settings, update }}>
      {children}
    </SettingsContext>
  );
}

export function useSettings() {
  const ctx = useContext(SettingsContext);
  if (!ctx) throw new Error("useSettings must be used within SettingsProvider");
  return ctx;
}
