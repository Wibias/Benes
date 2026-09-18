/**
 * Locale-override maintenance.
 *
 * New canonical English keys fall back to English at runtime. This tool never
 * copies English strings into translated resources and never deletes overrides.
 */
import { en } from "../src/i18n/en.ts";
import { localeOverrides } from "../src/i18n/locale-overrides.ts";
import { TRANSLATED_LOCALE_CODES } from "../src/i18n/locale-registry.ts";

const canonical = new Set(Object.keys(en));
const extra: string[] = [];
const coverage: Record<string, { translated: number; fallback: number }> = {};

for (const locale of TRANSLATED_LOCALE_CODES) {
  coverage[locale] = { translated: 0, fallback: 0 };
}

for (const key of canonical) {
  const row = localeOverrides[key as keyof typeof localeOverrides];
  for (const locale of TRANSLATED_LOCALE_CODES) {
    if (row && row[locale] !== undefined) coverage[locale].translated += 1;
    else coverage[locale].fallback += 1;
  }
}

for (const key of Object.keys(localeOverrides)) {
  if (!canonical.has(key)) extra.push(key);
}

extra.sort();

for (const locale of TRANSLATED_LOCALE_CODES) {
  const { translated, fallback } = coverage[locale];
  console.log(`${locale}: ${translated} overrides, ${fallback} English fallbacks`);
}

if (extra.length > 0) {
  console.log(`extra override keys: ${extra.length}`);
  for (const key of extra) console.log(`  ${key}`);
} else {
  console.log("no extra override keys");
}
