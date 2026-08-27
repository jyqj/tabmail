"use client";

import {
  createContext,
  useContext,
  useEffect,
  useCallback,
  useMemo,
  useSyncExternalStore,
  type ReactNode,
} from "react";

import { createTranslate } from "./i18n-format";
import { DEFAULT_LOCALE, type Locale, type Messages, type Translate } from "./i18n-types";
import { readLocaleCookie, writeLocaleCookie } from "./locale-cookie";
import { zh } from "./messages/zh";

export type { Locale };

// Only the default catalog is bundled with the app; the others are fetched the
// first time someone selects them. Until a catalog arrives `t` answers from the
// default one, so switching language paints once with the old strings and then
// again with the new ones instead of blanking the page.
const loaders: Record<Locale, () => Promise<Messages>> = {
  zh: async () => zh,
  en: async () => (await import("./messages/en")).en,
};

const loaded: Partial<Record<Locale, Messages>> = { [DEFAULT_LOCALE]: zh };

interface LocaleState {
  locale: Locale;
  messages: Messages;
}

let state: LocaleState = { locale: DEFAULT_LOCALE, messages: zh };
const serverState: LocaleState = state;

const storeListeners = new Set<() => void>();

function subscribeLocaleStore(callback: () => void) {
  storeListeners.add(callback);
  return () => { storeListeners.delete(callback); };
}

function getLocaleSnapshot(): LocaleState {
  return state;
}

function getLocaleServerSnapshot(): LocaleState {
  return serverState;
}

function publish(next: LocaleState) {
  if (next.locale === state.locale && next.messages === state.messages) return;
  state = next;
  storeListeners.forEach((cb) => cb());
}

// Resolves once `locale` can be rendered without falling back. Callers that
// want a switch to land in a single paint (or a test that renders a non-default
// locale) can await this before selecting the locale.
export async function preloadLocale(locale: Locale): Promise<void> {
  if (loaded[locale]) return;
  const messages = await loaders[locale]();
  loaded[locale] = messages;
  if (state.locale === locale) publish({ locale, messages });
}

function selectLocale(locale: Locale) {
  publish({ locale, messages: loaded[locale] ?? zh });
  void preloadLocale(locale);
}

// Adopt the stored locale as soon as the module loads rather than from an
// effect, so the first client render already reports it and consumers that key
// effects off `t` do not have to tear down and re-run after mount.
if (typeof window !== "undefined") {
  selectLocale(readLocaleCookie());
}

interface I18nContextValue {
  locale: Locale;
  setLocale: (l: Locale) => void;
  t: Translate;
}

const I18nContext = createContext<I18nContextValue | null>(null);

export function I18nProvider({ children }: { children: ReactNode }) {
  const { locale, messages } = useSyncExternalStore(
    subscribeLocaleStore,
    getLocaleSnapshot,
    getLocaleServerSnapshot,
  );

  useEffect(() => {
    document.documentElement.lang = locale;
  }, [locale]);

  useEffect(() => {
    selectLocale(readLocaleCookie());
  }, []);

  const setLocale = useCallback((l: Locale) => {
    writeLocaleCookie(l);
    document.documentElement.lang = l;
    selectLocale(l);
  }, []);

  const t = useMemo(() => createTranslate(messages), [messages]);

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
