"use client";

import {
  createContext,
  useContext,
  useEffect,
  useCallback,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import zhJSON from "@/locales/zh.json";

export type Locale = "zh" | "en";

const STORAGE_KEY = "tabmail-locale";

type Messages = Record<string, string>;

// The default locale (zh) ships in the main bundle so SSR and first paint
// always have messages; other locales are code-split and load on demand.
const zhMessages: Messages = zhJSON;

const loadedMessages: Partial<Record<Locale, Messages>> = { zh: zhMessages };

const localeLoaders: Record<Locale, () => Promise<{ default: Messages }>> = {
  zh: () => Promise.resolve({ default: zhMessages }),
  en: () => import("@/locales/en.json"),
};

const localeLoadPromises: Partial<Record<Locale, Promise<void>>> = {};

// Bumped whenever a locale's messages finish loading, so subscribers
// re-render with the freshly loaded table.
let messagesVersion = 0;

const storeListeners = new Set<() => void>();

function notifyListeners() {
  storeListeners.forEach((cb) => cb());
}

function subscribeLocaleStore(callback: () => void) {
  storeListeners.add(callback);
  return () => {
    storeListeners.delete(callback);
  };
}

function getLocaleSnapshot(): Locale {
  if (typeof window === "undefined") return "zh";
  const stored = localStorage.getItem(STORAGE_KEY);
  return stored === "zh" || stored === "en" ? stored : "zh";
}

function getLocaleServerSnapshot(): Locale {
  return "zh";
}

function getMessagesVersion() {
  return messagesVersion;
}

/**
 * Loads a locale's message table (no-op when already loaded). Exposed so
 * tests and eager code paths can await the dynamic import deterministically.
 */
export function preloadLocale(locale: Locale): Promise<void> {
  if (loadedMessages[locale]) return Promise.resolve();
  const existing = localeLoadPromises[locale];
  if (existing) return existing;
  const load = localeLoaders[locale]()
    .then((mod) => {
      loadedMessages[locale] = mod.default;
      messagesVersion++;
      notifyListeners();
    })
    .catch(() => {
      // Leave the locale unloaded; t() falls back to the default locale.
      delete localeLoadPromises[locale];
    });
  localeLoadPromises[locale] = load;
  return load;
}

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: (key: string, params?: Record<string, string | number>) => string;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const locale = useSyncExternalStore(
    subscribeLocaleStore,
    getLocaleSnapshot,
    getLocaleServerSnapshot,
  );
  useSyncExternalStore(subscribeLocaleStore, getMessagesVersion, getMessagesVersion);

  const messages = loadedMessages[locale] ?? zhMessages;

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  useEffect(() => {
    void preloadLocale(locale);
  }, [locale]);

  useEffect(() => {
    storeListeners.forEach((cb) => cb());
  }, []);

  const setLocale = useCallback((l: Locale) => {
    localStorage.setItem(STORAGE_KEY, l);
    document.documentElement.lang = l;
    void preloadLocale(l);
    notifyListeners();
  }, []);

  const t = useCallback(
    (key: string, params?: Record<string, string | number>): string => {
      let msg = messages[key] ?? zhMessages[key] ?? key;
      if (!params) return msg;
      msg = msg.replace(/\{(\w+)\|([^|]*)\|([^}]*)}/g, (_match, k, singular, plural) => {
        const v = params[k];
        return v !== undefined ? (Number(v) === 1 ? singular : plural) : _match;
      });
      return Object.entries(params).reduce(
        (s, [k, v]) => s.replaceAll(`{${k}}`, String(v)),
        msg,
      );
    },
    [messages],
  );

  return (
    <I18nContext value={{ locale, setLocale, t }}>
      {children}
    </I18nContext>
  );
}

export function useI18n() {
  const ctx = useContext(I18nContext);
  if (!ctx) throw new Error("useI18n must be used within I18nProvider");
  return ctx;
}
