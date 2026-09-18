import { createContext, createElement, useCallback, useContext, useEffect, useMemo, useSyncExternalStore, type ReactNode } from "react";
import { translate } from "./catalogs.ts";
import type { TKey } from "./en.ts";
import type { Locale } from "./locale-registry.ts";
import type { MessageVars } from "./message.ts";
import { bindActiveLocale, getActiveLocale, persistLocale, subscribeActiveLocale } from "./session.ts";

export type TFn = (key: TKey, vars?: MessageVars) => string;

export type I18nApi = {
  locale: Locale;
  setLocale: (locale: Locale) => void;
  t: TFn;
};

const I18nApiContext = createContext<I18nApi | null>(null);

export function provideI18nApi(api: I18nApi, children: ReactNode) {
  return createElement(I18nApiContext.Provider, { value: api }, children);
}

export function useI18n(): I18nApi {
  const api = useContext(I18nApiContext);
  if (!api) throw new Error("useI18n must be used within LanguageProvider");
  return api;
}

export function useT(): TFn {
  return useI18n().t;
}

export function LanguageProvider({ children }: { children: ReactNode }) {
  const locale = useSyncExternalStore(subscribeActiveLocale, getActiveLocale, getActiveLocale);

  useEffect(() => {
    persistLocale(locale);
  }, [locale]);

  const setLocale = useCallback((next: Locale) => {
    bindActiveLocale(next);
  }, []);

  const t: TFn = useCallback((key, vars) => translate(locale, key, vars), [locale]);
  const api = useMemo(() => ({ locale, setLocale, t }), [locale, setLocale, t]);
  return provideI18nApi(api, children);
}
