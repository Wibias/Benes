import { en, type TKey } from "./en.ts";
import { isLabCatalogKey, labCatalogText } from "./lab.ts";
import { localeOverrides } from "./locale-overrides.ts";
import { LOCALE_PROFILES, type Locale } from "./locale-registry.ts";
import { interpolate, type MessageVars } from "./message.ts";

export type { Locale, TKey };
export { LOCALE_PROFILES };

export function readCatalog(locale: Locale, key: TKey): string {
  if (isLabCatalogKey(key)) return labCatalogText(locale, key);
  if (locale !== "en") {
    const translated = localeOverrides[key]?.[locale];
    if (translated !== undefined) return translated;
  }
  return en[key] ?? key;
}

export function catalogValue(locale: Locale, key: TKey): string {
  return readCatalog(locale, key);
}

export function localeDisplayName(locale: Locale): string {
  return readCatalog(locale, "lang.nativeName");
}

export function translate(locale: Locale, key: TKey, vars?: MessageVars): string {
  return interpolate(readCatalog(locale, key), vars);
}
