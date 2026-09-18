/**
 * The dashboard chrome's decisions, with the DOM taken out.
 *
 * `use-app-chrome.ts` owns the effects; this module owns the answers they need — which
 * theme a stored value decodes to, what applying a theme has to touch, where focus belongs
 * after the drawer moves, and what a stop request means for the button that sent it. None of
 * it touches `window`, so all of it is exercised directly.
 */
import type { ProxyStopOutcome } from "./listener-commands.ts";

/** The theme choices the sidebar cycles through. `system` follows the OS. */
export type AppTheme = "light" | "dark" | "system";

/** Where an explicit (non-system) theme choice is remembered. */
export const THEME_STORAGE_KEY = "benes-theme";

/** Document attribute the stylesheet keys off. Its absence means "follow the OS". */
export const THEME_ATTRIBUTE = "data-theme";

/** The layout width at which the drawer is unnecessary and the sidebar is docked. */
export const DESKTOP_LAYOUT_QUERY = "(min-width: 761px)";

/** How long the drawer's opening transition takes before focus may move into it. */
export const DRAWER_FOCUS_DELAY_MS = 200;

/**
 * Everything that closes an open drawer besides its own buttons.
 *
 * A route change or a Back/Forward move means the user is now somewhere else, and a drawer
 * left standing over the new board would be stale. Escape is handled separately, because it
 * is a key rather than a route event.
 */
export const DRAWER_DISMISSAL_ROUTE_EVENT = "hashchange";
export const DRAWER_DISMISSAL_HISTORY_EVENT = "popstate";

/**
 * Decode a stored theme choice.
 *
 * Anything the app did not write — a value from an older release, a hand-edited key, a
 * truncated string — is not a preference, so it reads as the OS default rather than being
 * coerced into one of the explicit choices.
 */
export function decodeStoredTheme(stored: string | null | undefined): AppTheme {
  return stored === "light" || stored === "dark" ? stored : "system";
}

/**
 * The sidebar's class list for one drawer state.
 *
 * Lives here for the same reason `mainInnerClass` lives in `app-shell.ts`: the modifier a
 * state adds to a layout element is presentation policy, not markup.
 */
export function sidebarClassName(navOpen: boolean): string {
  return navOpen ? "sidebar open" : "sidebar";
}

/** What applying one theme has to do to the document and to storage. */
export interface ThemeApplication {
  /** Value for the theme attribute, or `null` to remove it. */
  readonly attribute: string | null;
  /** Whether the explicit override stays in storage. */
  readonly persist: boolean;
}

/**
 * `system` is the absence of a choice, not a third stored value: the attribute comes off and
 * the override is deleted so a later release cannot read a stale explicit theme.
 */
export function themeApplication(theme: AppTheme): ThemeApplication {
  if (theme === "system") return { attribute: null, persist: false };
  return { attribute: theme, persist: true };
}

/** Where focus belongs after a drawer state change. */
export type DrawerFocusTarget = "sidebar" | "menu" | "none";

/**
 * Focus-return policy.
 *
 * Opening moves focus into the drawer so its own controls are reachable without a tab walk
 * through the page behind it. Closing hands it back to the menu button — but only if the
 * drawer had actually been open, otherwise a render that never opened it would steal focus
 * from wherever the user really was.
 */
export function drawerFocusTarget(wasOpen: boolean, isOpen: boolean): DrawerFocusTarget {
  if (isOpen) return "sidebar";
  return wasOpen ? "menu" : "none";
}

/** What the stop command does with one request outcome. */
export interface StopCommandPlan {
  /** Whether the button must become usable again. */
  readonly releaseButton: boolean;
  /** Non-null when the user has to be told why the request failed. */
  readonly alert: string | null;
}

/**
 * A received rejection releases the button and explains itself. An accepted stop does
 * neither: the listener is on its way down, so the button stays in its stopping state until
 * the page goes with it, and a still-clickable button would invite a second shutdown.
 */
export function stopCommandPlan(outcome: ProxyStopOutcome): StopCommandPlan {
  if (outcome.accepted) return { releaseButton: false, alert: null };
  return { releaseButton: true, alert: outcome.message };
}
