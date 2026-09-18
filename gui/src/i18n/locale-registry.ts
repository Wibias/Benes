/** Supported dashboard locales and their document-language tags. */

export const TRANSLATED_LOCALE_CODES = ["de", "fr", "ko", "zh", "zh-TW", "ru", "ja", "tr"] as const;

export type TranslatedLocale = (typeof TRANSLATED_LOCALE_CODES)[number];
export type Locale = "en" | TranslatedLocale;

export type LocaleProfile = {
  readonly code: Locale;
  readonly htmlLang: string;
};

export const LOCALE_PROFILES: readonly LocaleProfile[] = [
  { code: "en", htmlLang: "en" },
  { code: "de", htmlLang: "de" },
  { code: "fr", htmlLang: "fr" },
  { code: "ko", htmlLang: "ko" },
  { code: "zh", htmlLang: "zh-CN" },
  { code: "zh-TW", htmlLang: "zh-TW" },
  { code: "ru", htmlLang: "ru" },
  { code: "ja", htmlLang: "ja" },
  { code: "tr", htmlLang: "tr" },
];

const SUPPORTED_CODES = new Set<string>(LOCALE_PROFILES.map((profile) => profile.code));
const HTML_LANG_BY_LOCALE = new Map(LOCALE_PROFILES.map((profile) => [profile.code, profile.htmlLang]));

export function isLocale(value: string): value is Locale {
  return SUPPORTED_CODES.has(value);
}

export function localeHtmlLang(locale: Locale): string {
  return HTML_LANG_BY_LOCALE.get(locale) ?? "en";
}
