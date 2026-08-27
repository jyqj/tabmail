import { DEFAULT_LOCALE, type Locale } from "./i18n-types";

// The locale lives in a cookie rather than localStorage so a Server Component
// can read it and render its copy in the right language on the first paint.
export const LOCALE_COOKIE = "tabmail-locale";

// A language choice is a preference, not a session: keep it for a year.
const LOCALE_COOKIE_MAX_AGE = 60 * 60 * 24 * 365;

export function parseLocale(value: string | undefined | null): Locale {
  return value === "zh" || value === "en" ? value : DEFAULT_LOCALE;
}

export function readLocaleCookie(): Locale {
  if (typeof document === "undefined") return DEFAULT_LOCALE;
  const match = document.cookie.match(
    new RegExp(`(?:^|;\\s*)${LOCALE_COOKIE}=([^;]*)`)
  );
  return parseLocale(match ? decodeURIComponent(match[1]) : null);
}

export function writeLocaleCookie(locale: Locale) {
  if (typeof document === "undefined") return;
  document.cookie = `${LOCALE_COOKIE}=${locale}; path=/; max-age=${LOCALE_COOKIE_MAX_AGE}; samesite=lax`;
}
