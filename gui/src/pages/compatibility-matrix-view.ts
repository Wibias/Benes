/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { VerdictFilters } from "../lab/evidence-matrix.ts";

/**
 * Whether anything in the filter bar is narrowing the board.
 *
 * The three API-scoped filters and the free-text subject box are one question to the reader —
 * "am I looking at everything?" — which is what the empty board's copy is chosen by.
 */
export function filtersAreActive(filters: VerdictFilters): boolean {
  return Boolean(
    filters.layer || filters.verdict || filters.subjectQuery.trim() || filters.suiteId.trim(),
  );
}

export function pageScopedExtra<T extends { baseData: unknown; queryKey: string }>(
  extra: T | null,
  baseData: unknown,
  queryKey: string,
): T | null {
  if (extra === null) return null;
  if (extra.baseData !== baseData || extra.queryKey !== queryKey) return null;
  return extra;
}

export function scopedFailureMessage(
  failure: { baseData: unknown; queryKey: string; message: string } | null,
  baseData: unknown,
  queryKey: string,
): string | null {
  const match = pageScopedExtra(failure, baseData, queryKey);
  return match ? match.message : null;
}

export function pageHasMoreVerdicts(
  extra: { hasMore: boolean } | null,
  dataHasMore: boolean | undefined,
): boolean {
  if (extra) return extra.hasMore;
  return dataHasMore ?? false;
}

/** Page-level notices derived from DataSurface kind — Compatibility owns the UX split. */
export type CompatibilityPageNotices = {
  /** Cold failure: no usable data; blocking error copy. */
  loadError: string | null;
  /** Failed refresh/poll with retained stale data; non-blocking warning. */
  staleRefreshWarning: string | null;
};


/** The browser's own fetch failures, which say nothing a reader of this page can act on. */
const BROWSER_FETCH_FAILURES: ReadonlyArray<(message: string) => boolean> = [
  message => message === "Failed to fetch",
  message => message.includes("NetworkError"),
  message => message.includes("network error"),
];

/**
 * The message a failed Lab read earns.
 *
 * A transport failure is the browser talking, not the listener, so it is replaced by the page's
 * own sentence; anything else is passed through, because the listener knows more about its own
 * refusal than this page does.
 */
export function labFailureMessage(error: unknown, fallback: string): string {
  const message = (error instanceof Error ? error.message : "").trim();
  if (message === "" || BROWSER_FETCH_FAILURES.some(isBrowserFailure => isBrowserFailure(message))) {
    return fallback;
  }
  return message;
}

/**
 * Map DataSurface kind to Compatibility page notices.
 * failed-cold → loadFailed (blocking). failed-with-stale → refresh warning (keep board).
 * Other kinds clear both (including in-flight refresh after a prior stale failure).
 */
export function compatibilityPageNotices(input: {
  surfaceKind: string;
  surfaceError: unknown;
  loadFailedLabel: string;
  refreshFailedStaleLabel: string;
  /** Injected by a caller that localizes differently; the page's own reader by default. */
  localizeFetchError?: (error: unknown, fallback: string) => string;
}): CompatibilityPageNotices {
  const localize = input.localizeFetchError ?? labFailureMessage;
  if (input.surfaceKind === "failed-cold") {
    return {
      loadError: localize(input.surfaceError, input.loadFailedLabel),
      staleRefreshWarning: null,
    };
  }
  if (input.surfaceKind === "failed-with-stale") {
    return {
      loadError: null,
      staleRefreshWarning: input.refreshFailedStaleLabel,
    };
  }
  return { loadError: null, staleRefreshWarning: null };
}
