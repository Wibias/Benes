/**
 * Production dashboard chrome: theme, mobile drawer, and the two listener commands.
 *
 * Three concerns live here, each with its own small owner: the theme, the mobile drawer, and
 * the listener commands. The decisions themselves are in `app-chrome-policy.ts`; what is left
 * is orchestration — subscribing to the events that matter, moving focus and page scroll with
 * the drawer, and handing each command its translated copy. Codex restart stays owned by
 * `use-codex-restart.ts`; this hook only tracks the epoch the pages re-read once it settles.
 */
import { useCallback, useEffect, useRef, useState } from "react";
import type { Dispatch, SetStateAction } from "react";
import { useT } from "./i18n/shared";
import type { TFn } from "./i18n/shared";
import { requestProxyStop } from "./listener-commands";
import { useCodexRestart } from "./use-codex-restart";
import { nextTheme } from "./app-shell";
import {
  DESKTOP_LAYOUT_QUERY,
  DRAWER_DISMISSAL_HISTORY_EVENT,
  DRAWER_DISMISSAL_ROUTE_EVENT,
  DRAWER_FOCUS_DELAY_MS,
  THEME_ATTRIBUTE,
  THEME_STORAGE_KEY,
  decodeStoredTheme,
  drawerFocusTarget,
  stopCommandPlan,
  themeApplication,
  type AppTheme,
} from "./app-chrome-policy";

/** A drawer is never left standing over a board the user has already navigated to. */
function useDrawerDismissedByRoute(setNavOpen: Dispatch<SetStateAction<boolean>>) {
  useEffect(() => {
    const dismiss = () => setNavOpen(false);
    window.addEventListener(DRAWER_DISMISSAL_ROUTE_EVENT, dismiss);
    window.addEventListener(DRAWER_DISMISSAL_HISTORY_EVENT, dismiss);
    return () => {
      window.removeEventListener(DRAWER_DISMISSAL_ROUTE_EVENT, dismiss);
      window.removeEventListener(DRAWER_DISMISSAL_HISTORY_EVENT, dismiss);
    };
  }, [setNavOpen]);
}

/**
 * Freeze the page behind the drawer and hand back the release.
 *
 * The element is a parameter rather than a hard-coded `document.body`, and the exact previous
 * inline value is captured before it is overwritten, so a page that set one of its own gets it
 * back rather than inheriting `hidden`.
 */
function lockPageScrollUnder(element: HTMLElement): () => void {
  const previous = element.style.overflow;
  element.style.overflow = "hidden";
  return () => {
    element.style.overflow = previous;
  };
}

/** While the drawer is open it owns the keyboard and the page behind it stops scrolling. */
function useOpenDrawerLifetime(navOpen: boolean, setNavOpen: Dispatch<SetStateAction<boolean>>) {
  useEffect(() => {
    if (!navOpen) return;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setNavOpen(false);
    };
    window.addEventListener("keydown", onKeyDown);
    const releaseScroll = lockPageScrollUnder(document.body);
    return () => {
      window.removeEventListener("keydown", onKeyDown);
      releaseScroll();
    };
  }, [navOpen, setNavOpen]);
}

/**
 * Focus enters the drawer after its transition, and returns to the menu button on close.
 *
 * The "was it open last time" memory belongs to this effect, so it is owned here rather than
 * threaded in as a shared ref.
 */
function useDrawerFocusReturn(
  navOpen: boolean,
  sidebar: { current: HTMLElement | null },
  menuButton: { current: HTMLButtonElement | null },
) {
  const wasOpen = useRef(false);
  useEffect(() => {
    const target = drawerFocusTarget(wasOpen.current, navOpen);
    wasOpen.current = navOpen;
    if (target === "sidebar") {
      const timer = setTimeout(() => sidebar.current?.focus(), DRAWER_FOCUS_DELAY_MS);
      return () => clearTimeout(timer);
    }
    if (target === "menu") menuButton.current?.focus();
  }, [navOpen, sidebar, menuButton]);
}

/** The docked layout makes the drawer unnecessary, so starting to match it closes one. */
function useDockedLayoutCollapsesDrawer(setNavOpen: Dispatch<SetStateAction<boolean>>) {
  useEffect(() => {
    const query = window.matchMedia(DESKTOP_LAYOUT_QUERY);
    const onChange = () => {
      if (query.matches) setNavOpen(false);
    };
    query.addEventListener("change", onChange);
    return () => query.removeEventListener("change", onChange);
  }, [setNavOpen]);
}

/**
 * The mobile drawer, as one owner.
 *
 * Open state, the element refs the focus handoff needs, and the four things that can end it
 * belong together: split across a larger hook they would be easy to keep half-updated.
 */
function useMobileDrawer() {
  const [navOpen, setNavOpen] = useState(false);
  const menuBtnRef = useRef<HTMLButtonElement>(null);
  const sidebarRef = useRef<HTMLElement>(null);
  useDrawerDismissedByRoute(setNavOpen);
  useOpenDrawerLifetime(navOpen, setNavOpen);
  useDrawerFocusReturn(navOpen, sidebarRef, menuBtnRef);
  useDockedLayoutCollapsesDrawer(setNavOpen);
  return { navOpen, setNavOpen, menuBtnRef, sidebarRef };
}

/** The document attribute and the stored override follow the chosen theme exactly. */
function useThemeAppliedToDocument(theme: AppTheme) {
  useEffect(() => {
    const { attribute, persist } = themeApplication(theme);
    if (attribute === null) document.documentElement.removeAttribute(THEME_ATTRIBUTE);
    else document.documentElement.setAttribute(THEME_ATTRIBUTE, attribute);
    if (persist) localStorage.setItem(THEME_STORAGE_KEY, theme);
    else localStorage.removeItem(THEME_STORAGE_KEY);
  }, [theme]);
}

/** The one localized failure formatter a stop request needs. */
function stopRequestMessages(translate: TFn) {
  return {
    formatFailure: (status: number) => translate("dash.stopFailed", { status: String(status) }),
  };
}

/** Ask before taking the listener down. Declining — or dismissing the prompt — stops nothing. */
function confirmStopRequest(translate: TFn): boolean {
  return confirm(translate("dash.stopConfirm"));
}

/**
 * The stop command.
 *
 * Same shape as `useCodexRestart`: an accepted stop means the listener is on its way down, so
 * the button stays in its stopping state until the page goes with it. A rejection releases it
 * and explains itself through the message the request formatter produced.
 */
function useListenerStop(apiBase: string) {
  const t = useT();
  const [stopping, setStopping] = useState(false);
  const stop = useCallback(async () => {
    if (!confirmStopRequest(t)) return;
    setStopping(true);
    const plan = stopCommandPlan(await requestProxyStop(apiBase, stopRequestMessages(t)));
    if (plan.releaseButton) setStopping(false);
    if (plan.alert !== null) alert(plan.alert);
  }, [apiBase, t]);
  return { stopping, stop };
}

/**
 * The restart command plus the epoch that means "the server state changed".
 *
 * The epoch advances only once the operation settles: a page refreshing on the click would
 * read the stale app-server state the restart exists to replace, so the two are one concept
 * and are kept in one place.
 */
function useCodexRestartEpoch(apiBase: string) {
  const [epoch, setEpoch] = useState(0);
  const controller = useCodexRestart(apiBase, {
    onSettled: () => setEpoch(current => current + 1),
  });
  return { epoch, ...controller };
}

export function useAppChrome(apiBase: string) {
  const [theme, setTheme] = useState<AppTheme>(() => decodeStoredTheme(localStorage.getItem(THEME_STORAGE_KEY)));
  const drawer = useMobileDrawer();
  const { stopping, stop } = useListenerStop(apiBase);
  const { epoch, restarting: codexRestarting, restart: handleCodexRestart } = useCodexRestartEpoch(apiBase);

  useThemeAppliedToDocument(theme);

  return {
    theme,
    cycleTheme: () => setTheme(nextTheme),
    navOpen: drawer.navOpen,
    setNavOpen: drawer.setNavOpen,
    stopping,
    handleStop: stop,
    menuBtnRef: drawer.menuBtnRef,
    sidebarRef: drawer.sidebarRef,
    codexRestartEpoch: epoch,
    codexRestarting,
    handleCodexRestart,
  };
}
