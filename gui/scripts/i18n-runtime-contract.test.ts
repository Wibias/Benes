import assert from "node:assert/strict";
import { createElement } from "react";
import { renderToString } from "react-dom/server";
import test from "node:test";

import { catalogValue } from "../src/i18n/catalogs.ts";
import { LOCALE_PROFILES, localeHtmlLang } from "../src/i18n/locale-registry.ts";
import { interpolate } from "../src/i18n/message.ts";
import {
  bindActiveLocale,
  detectInitialLocale,
  getActiveLocale,
  localeFromBrowserLanguage,
  persistLocale,
} from "../src/i18n/session.ts";
import { Trans } from "../src/i18n/command-chip.ts";
import { LanguageProvider, useI18n, useT } from "../src/i18n/hooks.ts";

test("locale profiles pin HTML language tags", () => {
  const byCode = Object.fromEntries(LOCALE_PROFILES.map((profile) => [profile.code, profile.htmlLang]));
  assert.deepEqual(byCode, {
    en: "en",
    de: "de",
    fr: "fr",
    ko: "ko",
    zh: "zh-CN",
    "zh-TW": "zh-TW",
    ru: "ru",
    ja: "ja",
    tr: "tr",
  });
  assert.equal(localeHtmlLang("zh"), "zh-CN");
});

test("initial locale prefers a supported stored value", () => {
  assert.equal(detectInitialLocale(() => "ja", "de-DE"), "ja");
  assert.equal(detectInitialLocale(() => "nope", "fr-FR"), "fr");
});

test("storage failure is non-fatal during detection", () => {
  assert.equal(detectInitialLocale(() => { throw new Error("blocked"); }, "ko-KR"), "ko");
});

test("browser language mapping", () => {
  assert.equal(localeFromBrowserLanguage("de-AT"), "de");
  assert.equal(localeFromBrowserLanguage("fr-CA"), "fr");
  assert.equal(localeFromBrowserLanguage("ko-KR"), "ko");
  assert.equal(localeFromBrowserLanguage("ru-RU"), "ru");
  assert.equal(localeFromBrowserLanguage("ja-JP"), "ja");
  assert.equal(localeFromBrowserLanguage("tr-TR"), "tr");
  assert.equal(localeFromBrowserLanguage("zh-TW"), "zh-TW");
  assert.equal(localeFromBrowserLanguage("zh-HK"), "zh-TW");
  assert.equal(localeFromBrowserLanguage("zh-MO"), "zh-TW");
  assert.equal(localeFromBrowserLanguage("zh-Hant"), "zh-TW");
  assert.equal(localeFromBrowserLanguage("zh-CN"), "zh");
  assert.equal(localeFromBrowserLanguage("zh"), "zh");
  assert.equal(localeFromBrowserLanguage("en-US"), "en");
  assert.equal(localeFromBrowserLanguage(undefined), "en");
  assert.equal(localeFromBrowserLanguage("pt-BR"), "en");
});

test("active locale starts from detection and follows bind", () => {
  bindActiveLocale(detectInitialLocale(() => null, "en-US"));
  assert.equal(getActiveLocale(), "en");
  bindActiveLocale("tr");
  assert.equal(getActiveLocale(), "tr");
});

test("persistLocale writes document language and storage, ignoring storage failure", () => {
  const documentElement = { lang: "" };
  const stored: string[] = [];
  persistLocale("zh", documentElement, (value) => { stored.push(value); });
  assert.equal(documentElement.lang, "zh-CN");
  assert.deepEqual(stored, ["zh"]);
  persistLocale("de", documentElement, () => { throw new Error("blocked"); });
  assert.equal(documentElement.lang, "de");
});

test("interpolation contract", () => {
  assert.equal(interpolate("Hello {name}", { name: "Ada" }), "Hello Ada");
  assert.equal(interpolate("{name} and {name}", { name: "Ada" }), "Ada and Ada");
  assert.equal(interpolate("{a} {b}", { a: "x", b: 2 }), "x 2");
  assert.equal(interpolate("Hello {name}"), "Hello {name}");
  assert.equal(interpolate("Hello {name}", { name: "Ada", extra: "z" }), "Hello Ada");
  assert.equal(interpolate("Hello {name}", { other: "Ada" }), "Hello {name}");
});

test("lookup falls back from locale to English to key", () => {
  assert.equal(catalogValue("de", "nav.dashboard"), "Übersicht");
  assert.equal(catalogValue("en", "nav.dashboard"), "Dashboard");
  const englishOnly = catalogValue("fr", "nav.group.configuration");
  assert.equal(englishOnly, catalogValue("en", "nav.group.configuration"));
});

function renderWithLocale(locale: "en" | "de" | "ja", node: ReturnType<typeof createElement>): string {
  bindActiveLocale(locale);
  return renderToString(createElement(LanguageProvider, null, node));
}

test("Trans renders prefix, chip, command, and suffix when {cmd} is present", () => {
  const english = renderWithLocale("en", createElement(Trans, { k: "dash.runStart", cmd: "benes start" }));
  assert.match(english, /^Run /);
  assert.match(english, /<code class="chip">benes start<\/code>/);
  assert.match(english, / to start the proxy\./);

  const japanese = renderWithLocale("ja", createElement(Trans, { k: "dash.runStart", cmd: "benes start" }));
  assert.match(japanese, /^<code class="chip">benes start<\/code>/);
  assert.match(japanese, /を実行してプロキシを起動してください。/);
});

test("Trans still emits the command chip when {cmd} is absent", () => {
  const html = renderWithLocale("de", createElement(Trans, { k: "nav.dashboard", cmd: "benes start" }));
  assert.match(html, /Übersicht/);
  assert.match(html, /<code class="chip">benes start<\/code>/);
});

test("Trans interpolates vars through the component path", () => {
  const html = renderWithLocale(
    "en",
    createElement(Trans, { k: "lab.summary.counts", cmd: "benes lab", vars: { subjects: 3, verdicts: 7 } }),
  );
  assert.match(html, /3 subjects · 7 verdicts/);
  assert.match(html, /<code class="chip">benes lab<\/code>/);
});

test("LanguageProvider supplies useT translations and setLocale updates the next render", () => {
  function Probe() {
    const { t } = useI18n();
    return createElement("span", null, t("nav.dashboard"));
  }
  bindActiveLocale("en");
  const english = renderToString(createElement(LanguageProvider, null, createElement(Probe)));
  assert.match(english, />Dashboard</);

  let changeLocale: ((next: "de") => void) | undefined;
  function Capture() {
    const { t, setLocale } = useI18n();
    changeLocale = setLocale;
    return createElement("span", null, t("nav.dashboard"));
  }
  const first = renderToString(createElement(LanguageProvider, null, createElement(Capture)));
  assert.match(first, />Dashboard</);
  changeLocale!("de");
  const second = renderToString(createElement(LanguageProvider, null, createElement(Capture)));
  assert.match(second, />Übersicht</);
});

test("useT throws outside LanguageProvider", () => {
  function Bare() {
    useT();
    return null;
  }
  assert.throws(
    () => renderToString(createElement(Bare)),
    /useI18n must be used within LanguageProvider/,
  );
});
