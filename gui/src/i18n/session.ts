/** Locale detection, document language, storage, and non-React active locale. */

import { isLocale, localeHtmlLang, type Locale } from "./locale-registry.ts";

export const LOCALE_STORAGE_KEY = "benes-lang";

const BROWSER_PREFIXES: ReadonlyArray<readonly [string, Locale]> = [
  ["de", "de"],
  ["fr", "fr"],
  ["ko", "ko"],
  ["ru", "ru"],
  ["ja", "ja"],
  ["tr", "tr"],
];

const TRADITIONAL_CHINESE_MARKERS = ["tw", "hk", "mo", "hant"] as const;

export function localeFromBrowserLanguage(tag: string | undefined): Locale {
  const normalized = (tag ?? "en").toLowerCase();
  for (const [prefix, locale] of BROWSER_PREFIXES) {
    if (normalized.startsWith(prefix)) return locale;
  }
  if (normalized.startsWith("zh")) {
    if (TRADITIONAL_CHINESE_MARKERS.some((marker) => normalized.includes(marker))) return "zh-TW";
    return "zh";
  }
  return "en";
}

export function detectInitialLocale(
  readStored: () => string | null = readBrowserStoredLocale,
  browserLanguage: string | undefined = currentBrowserLanguage(),
): Locale {
  const stored = readStoredLocale(readStored);
  if (stored) return stored;
  return localeFromBrowserLanguage(browserLanguage);
}

export function readStoredLocale(readStored: () => string | null): Locale | null {
  try {
    const stored = readStored();
    if (stored && isLocale(stored)) return stored;
  } catch {
    /* storage is optional */
  }
  return null;
}

let boundLocale: Locale | null = null;
const localeListeners = new Set<() => void>();

export function getActiveLocale(): Locale {
  if (boundLocale === null) boundLocale = detectInitialLocale();
  return boundLocale;
}

export function bindActiveLocale(locale: Locale): void {
  if (boundLocale === locale) return;
  boundLocale = locale;
  for (const listener of localeListeners) listener();
}

export function subscribeActiveLocale(onChange: () => void): () => void {
  localeListeners.add(onChange);
  return () => {
    localeListeners.delete(onChange);
  };
}

export function persistLocale(
  locale: Locale,
  documentElement: { lang: string } | null | undefined = currentDocumentElement(),
  writeStored: (value: string) => void = writeBrowserStoredLocale,
): void {
  if (documentElement) documentElement.lang = localeHtmlLang(locale);
  try {
    writeStored(locale);
  } catch {
    /* persistence is optional */
  }
}

function currentBrowserLanguage(): string | undefined {
  return globalThis.navigator?.language;
}

function currentDocumentElement(): { lang: string } | null | undefined {
  return globalThis.document?.documentElement;
}

function readBrowserStoredLocale(): string | null {
  return globalThis.localStorage.getItem(LOCALE_STORAGE_KEY);
}

function writeBrowserStoredLocale(value: string): void {
  globalThis.localStorage.setItem(LOCALE_STORAGE_KEY, value);
}
