/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { KeyboardEvent } from "react";
import { parseHashRoute } from "../hash-routing";

/** The two Diagnostics workspace tabs, in the order the page chrome renders them. */
export type LogsTab = "logs" | "debug";

/** The hash path each tab owns. `logs` is also the fallback for every other Diagnostics hash. */
const HASH_PATH: Record<LogsTab, string> = {
  logs: "logs",
  debug: "logs/debug",
};

/** The rendered tab element each tab owns. */
const TAB_ELEMENT_ID: Record<LogsTab, string> = {
  logs: "logs-tab-logs",
  debug: "logs-tab-debug",
};

/**
 * The element id the tablist gives `tab`.
 *
 * The page chrome stamps these ids and the roving-focus keys move focus to them, so both sides
 * have to read the same table rather than repeat its literals.
 */
export function logsTabElementId(tab: LogsTab): string {
  return TAB_ELEMENT_ID[tab];
}

/**
 * Roving-focus keys this tablist claims, mapped to the tab they move to.
 *
 * A two-tab tablist wraps: the first tab sits immediately left of the last one, so `Home`
 * and `ArrowLeft` share a target and `End` and `ArrowRight` share the other. Every key
 * missing from this table keeps its browser meaning.
 */
const FOCUS_TARGET_BY_KEY: Record<string, LogsTab> = {
  ArrowLeft: "logs",
  Home: "logs",
  ArrowRight: "debug",
  End: "debug",
};

/** The tab the address bar currently addresses. */
export function readTabFromHash(hash?: string): LogsTab {
  const source = hash ?? (typeof window === "undefined" ? "" : window.location.hash);
  return parseHashRoute(source).path === HASH_PATH.debug ? "debug" : "logs";
}

/** Address a tab. Assigning the hash lets `hashchange` reach every listener on the page. */
export function selectLogsTab(tab: LogsTab): void {
  window.location.hash = HASH_PATH[tab];
}

/** Move the tablist to the tab a claimed key asks for, then focus that tab. */
export function logsTabKeyDown(event: KeyboardEvent): void {
  const target = FOCUS_TARGET_BY_KEY[event.key];
  if (target === undefined) return;
  event.preventDefault();
  selectLogsTab(target);
  document.getElementById(logsTabElementId(target))?.focus();
}