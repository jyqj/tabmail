import { cookies } from "next/headers";

import { createTranslate } from "./i18n-format";
import { DEFAULT_LOCALE, type Locale, type Messages, type Translate } from "./i18n-types";
import { LOCALE_COOKIE, parseLocale } from "./locale-cookie";
import { zh } from "./messages/zh";

// Reading the cookie opts the route into dynamic rendering, which is the price
// of serving a page in the reader's language instead of translating it after
// hydration.
const catalogs: Record<Locale, () => Promise<Messages>> = {
  zh: async () => zh,
  en: async () => (await import("./messages/en")).en,
};

export async function getServerLocale(): Promise<Locale> {
  return parseLocale((await cookies()).get(LOCALE_COOKIE)?.value);
}

export interface ServerI18n {
  locale: Locale;
  t: Translate;
}

export async function getServerI18n(): Promise<ServerI18n> {
  const locale = await getServerLocale();
  const messages = await (catalogs[locale] ?? catalogs[DEFAULT_LOCALE])();
  return { locale, t: createTranslate(messages) };
}
