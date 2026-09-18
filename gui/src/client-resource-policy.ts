/**
 * Settle / cache / abort policy for dashboard keyed resources.
 * The runtime owns HTTP and timers; these helpers are the named rules for when a
 * generation may commit, when cached data stays on screen, and how failures look.
 */

export const DEFAULT_REQUEST_DEADLINE_MS = 30_000;
/** Abort-reason sentinel distinguishing the deadline from owner aborts (replace/unmount). */
export const RESOURCE_TIMEOUT = "benes-resource-deadline";

export type ResourceSnapshotFields<T> = {
  data: T | undefined;
  error: unknown;
  loading: boolean;
  refreshing: boolean;
  hasSucceeded: boolean;
  lastAttemptOk: boolean;
};

export type ResourceAttemptIdentity = {
  attemptGeneration: number;
  storeGeneration: number;
  aborted: boolean;
  timedOut: boolean;
};

export type ResourceFetchOutcome<T> =
  | { status: "success"; data: T }
  | { status: "error"; error: unknown };

export type ResourceFetchCommit<T> =
  | { kind: "ignore" }
  | { kind: "success"; snapshot: ResourceSnapshotFields<T>; seedNeedsRevalidate: false }
  | { kind: "failure"; snapshot: ResourceSnapshotFields<T>; seedNeedsRevalidate: false };

/** Default true: a new owner request replaces work in flight. Quiet polls pass false. */
export function resourceReplaceInflight(replaceInflight: boolean | undefined): boolean {
  return replaceInflight !== false;
}

export function shouldSkipQuietResourceFetch(hasInflight: boolean, replaceInflight: boolean): boolean {
  return hasInflight && !replaceInflight;
}

export function resourceShowsBlockingLoad(data: unknown, forceLoading: boolean | undefined): boolean {
  return data === undefined || forceLoading === true;
}

export function emptyResourceSnapshot<T>(): ResourceSnapshotFields<T> {
  return {
    data: undefined,
    error: undefined,
    loading: false,
    refreshing: false,
    hasSucceeded: false,
    lastAttemptOk: false,
  };
}

export function resourceInFlightSnapshot<T>(
  previous: ResourceSnapshotFields<T>,
  forceLoading?: boolean,
): ResourceSnapshotFields<T> {
  return {
    ...previous,
    loading: resourceShowsBlockingLoad(previous.data, forceLoading) ? true : previous.loading,
    refreshing: true,
  };
}

export function resourceTimeoutError(deadlineMs: number): Error {
  return new Error(`resource request timed out after ${deadlineMs}ms`);
}

export function resourceNormalizeLoadError(error: unknown): unknown {
  return error === undefined ? new Error("resource load failed") : error;
}

export function waitForResourceAbort(
  controller: AbortController,
  timedOut: () => boolean,
  deadlineMs: number,
): Promise<never> {
  return new Promise((resolve, reject) => {
    controller.signal.addEventListener("abort", () => {
      if (timedOut()) reject(resourceTimeoutError(deadlineMs));
      else resolve(null as never);
    }, { once: true });
  });
}

function resourceSuccessSnapshot<T>(data: T): ResourceSnapshotFields<T> {
  return {
    data,
    error: undefined,
    loading: false,
    refreshing: false,
    hasSucceeded: true,
    lastAttemptOk: true,
  };
}

function resourceFailureSnapshot<T>(
  previous: ResourceSnapshotFields<T>,
  error: unknown,
): ResourceSnapshotFields<T> {
  return {
    ...previous,
    error: resourceNormalizeLoadError(error),
    loading: false,
    refreshing: false,
    lastAttemptOk: false,
  };
}

export function commitResourceFetch<T>(
  identity: ResourceAttemptIdentity,
  outcome: ResourceFetchOutcome<T>,
  previous: ResourceSnapshotFields<T>,
): ResourceFetchCommit<T> {
  if (outcome.status === "success") {
    if (identity.attemptGeneration !== identity.storeGeneration || identity.aborted) {
      return { kind: "ignore" };
    }
    return { kind: "success", snapshot: resourceSuccessSnapshot(outcome.data), seedNeedsRevalidate: false };
  }
  if (identity.attemptGeneration !== identity.storeGeneration) return { kind: "ignore" };
  if (identity.aborted && !identity.timedOut) return { kind: "ignore" };
  return {
    kind: "failure",
    snapshot: resourceFailureSnapshot(previous, outcome.error),
    seedNeedsRevalidate: false,
  };
}
