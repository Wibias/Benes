import { useEffect, useState } from "react";
import { AppMobileTopbar, AppSidebar } from "./app-sidebar";
import { AppPageBoundary } from "./app-page";
import { mainInnerClass } from "./app-shell";
import { useI18n, useT } from "./i18n/shared";
import { installApiAuthFetch } from "./api";
import { useAppRouteState } from "./use-app-route-state";
import { useAppChrome } from "./use-app-chrome";
import { useNavBoardPrefetch } from "./use-nav-board-prefetch";
import { readJsonIfOk } from "./fetch-json";

installApiAuthFetch();

const API_BASE = import.meta.env.VITE_API_BASE || "";
const BRAND_WORDMARK_SUFFIX = "Benes".slice(1);

function useFabricNavEnabled(apiBase: string): boolean {
  const [enabled, setEnabled] = useState(false);
  useEffect(() => {
    let cancelled = false;
    const load = () => {
      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/fabric/status`);
          const data = await readJsonIfOk<{ enabled?: unknown }>(res);
          if (!cancelled) setEnabled(data?.enabled === true);
        } catch {
          if (!cancelled) setEnabled(false);
        }
      })();
    };
    load();
    window.addEventListener("hashchange", load);
    return () => {
      cancelled = true;
      window.removeEventListener("hashchange", load);
    };
  }, [apiBase]);
  return enabled;
}

function AppBrand({ label }: { label: string }) {
  return (
    <div className="brand" role="img" aria-label={label}>
      <span className="brand-logo" aria-hidden="true" />
      <span className="name" aria-hidden="true">{BRAND_WORDMARK_SUFFIX}</span>
    </div>
  );
}

export default function App() {
  const { page, navigateToPage } = useAppRouteState();
  const { locale, setLocale } = useI18n();
  const t = useT();
  const chrome = useAppChrome(API_BASE);
  const fabricEnabled = useFabricNavEnabled(API_BASE);
  useNavBoardPrefetch(API_BASE, fabricEnabled);
  const brand = <AppBrand label={t("app.logoAria")} />;

  return (
    <div className="app">
      <AppMobileTopbar
        navOpen={chrome.navOpen}
        brand={brand}
        t={t}
        menuBtnRef={chrome.menuBtnRef}
        stopping={chrome.stopping}
        handleStop={() => { void chrome.handleStop(); }}
        codexRestarting={chrome.codexRestarting}
        handleCodexRestart={chrome.handleCodexRestart}
        onToggleNav={() => chrome.setNavOpen(open => !open)}
      />
      {chrome.navOpen && <div className="drawer-scrim" onClick={() => chrome.setNavOpen(false)} aria-hidden="true" />}
      <AppSidebar
        apiBase={API_BASE}
        navOpen={chrome.navOpen}
        sidebarRef={chrome.sidebarRef}
        brand={brand}
        t={t}
        page={page}
        locale={locale}
        setLocale={setLocale}
        theme={chrome.theme}
        cycleTheme={chrome.cycleTheme}
        stopping={chrome.stopping}
        handleStop={() => { void chrome.handleStop(); }}
        codexRestarting={chrome.codexRestarting}
        handleCodexRestart={chrome.handleCodexRestart}
        onClose={() => chrome.setNavOpen(false)}
        onNavigate={(id) => {
          navigateToPage(id);
          chrome.setNavOpen(false);
        }}
        fabricEnabled={fabricEnabled}
      />
      <main className="main" inert={chrome.navOpen}>
        <div className={mainInnerClass(page)}>
          <AppPageBoundary page={page} apiBase={API_BASE} codexRestartEpoch={chrome.codexRestartEpoch} t={t} />
        </div>
      </main>
    </div>
  );
}
