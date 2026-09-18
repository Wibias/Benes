/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useState } from "react";
import {
  readPageFromHash,
  resolveAppHashChange,
  type Page,
} from "./app-routing.ts";
import { navigateHash, normalizeHashPath, replaceHash } from "./hash-routing.ts";

/** The namespace the retired Classic/Workspace layout preference used. */
const STALE_VIEW_PREFIX = "benes-";

/** Layout-preference keys written by the removed Classic/Workspace switch. */
const STALE_VIEW_SURFACES = [
  "global-view",
  "view",
  "providers-view",
  "subagents-view",
  "storage-view",
  "codexauth-view",
  "apikeys-view",
  "claudecode-view",
  "usage-view",
  "logs-view",
  "models-view",
  "dashboard-view",
];

/**
 * One-shot cleanup of the retired layout keys. There is a single layout now, so these
 * would otherwise sit in every user's storage forever.
 * TODO: drop this sweep (and its call) one release after 2.7.x.
 */
export function clearStaleViewKeys(storage: Pick<Storage, "removeItem">): void {
  for (const surface of STALE_VIEW_SURFACES) {
    try {
      storage.removeItem(STALE_VIEW_PREFIX + surface);
    } catch {
      /* private mode / quota — nothing to clean up */
    }
  }
}

function currentRouteHash(): string {
  return normalizeHashPath(window.location.hash);
}

/**
 * Route events both mean "re-read the hash": hashchange covers a location.hash
 * assignment, popstate covers Back/Forward. Returns the detach.
 *
 * Exported so a page's own sub-surface state listens to the same definition of a route
 * event instead of binding the same two events again: which events mean "the URL moved"
 * is one decision, and it is this module's.
 */
export function onRouteEvent(listener: () => void): () => void {
  for (const event of ROUTE_EVENTS) window.addEventListener(event, listener);
  return () => {
    for (const event of ROUTE_EVENTS) window.removeEventListener(event, listener);
  };
}

const ROUTE_EVENTS = ["hashchange", "popstate"] as const;

/**
 * Production App route ownership. A deliberate page change pushes history; a normalized
 * or rewritten hash replaces the current entry, so Back is never trapped on a URL the
 * router immediately corrects.
 */
export function useAppRouteState() {
  const [page, setPageState] = useState<Page>(() => readPageFromHash());

  useEffect(() => {
    if (typeof localStorage !== "undefined") clearStaleViewKeys(localStorage);
  }, []);

  /** Resolve an incoming hash and show its page, correcting the URL when asked. */
  const applyRouteHash = useCallback((incomingHash: string) => {
    const { page: nextPage, replaceTo } = resolveAppHashChange(incomingHash);
    if (replaceTo) replaceHash(replaceTo);
    setPageState(nextPage);
  }, []);

  /**
   * Deliberate navigation to a page root. Pages that own sub-hashes (for example
   * `#dashboard/providers`) address those by hash from inside the page.
   */
  const navigateToPage = useCallback((id: Page) => {
    navigateHash(id);
    setPageState(id);
  }, []);

  useEffect(() => onRouteEvent(() => applyRouteHash(currentRouteHash())), [applyRouteHash]);

  /*
   * Mount and page-driven normalization share the one resolver. This replaces an earlier
   * hand-rolled pair of redirects whose two copies had to be kept in agreement, and it
   * covers the gap `useState` cannot: a hash changed between render and commit, before
   * the event listener above existed, would otherwise be observed by nobody.
   */
  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect, react/react-compiler -- reconciles a hash changed before the listener was bound; an unchanged page re-renders as a no-op
    applyRouteHash(currentRouteHash());
  }, [applyRouteHash]);

  return { page, navigateToPage };
}
