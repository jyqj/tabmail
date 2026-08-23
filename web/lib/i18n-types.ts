export type Locale = "zh" | "en";

export type Messages = Record<string, string>;

export type Translate = (key: string, params?: Record<string, string | number>) => string;

export const DEFAULT_LOCALE: Locale = "zh";
