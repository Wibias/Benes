/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Render-state classification for data surfaces.
 *
 * Request ownership stays in `client-resource`; this module turns a store snapshot into the
 * three decisions a page actually makes: replace the content with a skeleton, show progress
 * next to existing content, or show a failure. Pages used to answer those with per-page
 * booleans, which is why a slow load could look identical to an empty result.
 *
 * The model is a fold: read the snapshot into four independent facts, fold the facts into
 * one phase, then resolve that phase through a shape table. Each phase carries exactly one
 * presentation, so nothing downstream re-derives flags from the booleans.
 */

import { useCallback, useMemo } from "react";
import {
  type ClientResourceOptions,
  type ResourceSnapshot,
  type ResourceView,
  useKeyedClientResource,
} from "./client-resource.ts";
import { mergeDataSurfaceSeed } from "./data-surface-seed.ts";
import {
  readSessionListCacheEntry,
  writeSessionListCacheEntry,
  type SessionListEntry,
} from "./session-list-cache.ts";

/** Every kind a surface reports; the union below is derived from this one list. */
const SURFACE_KINDS = [
  "disabled",
  "cold",
  "retrying-cold",
  "loading-with-stale-data",
  "ready-empty",
  "ready-populated",
  "failed-cold",
  "failed-with-stale",
] as const;

export type DataSurfaceKind = (typeof SURFACE_KINDS)[number];

/** A page's own test for "this successful payload is empty". */
type SurfaceEmptyTest<T> = (data: T) => boolean;

/** The loader a surface's page hands over. */
type SurfaceLoader<T> = (signal: AbortSignal) => Promise<T>;

/** The payload an attempt settled on, and the error it carries, if any. */
type SurfaceAttempt<T> = {
  data: T | undefined;
  error: unknown;
};

/** What the attempt produced, before the phase decides how to present it. */
type SurfaceOutcome<T> = SurfaceAttempt<T> & { kind: DataSurfaceKind };

/** The presentation flags the phase resolves to. */
type SurfaceProgress = {
  /** True only when the content area should be replaced by a skeleton. */
  showSkeleton: boolean;
  /** True while a request is in flight; drives the inline status line and `aria-busy`. */
  refreshing: boolean;
  /** True when the latest settled attempt failed, so the error banner must stay visible. */
  showError: boolean;
};

export type DataSurfaceState<T> = SurfaceOutcome<T> & SurfaceProgress;

export type DataSurfaceResource<T> = ResourceView<T> & { state: DataSurfaceState<T> };

/**
 * Every resource-layer knob is forwarded as `ClientResourceOptions` declares it, so a
 * surface never restates the resource layer's poll, cache, or deadline semantics.
 */
export type DataSurfaceOptions<T> = ClientResourceOptions<T> & {
  /** The page owns its domain-specific definition of an empty successful payload. */
  isEmpty: SurfaceEmptyTest<T>;
  /**
   * Opt-in session-cache wiring: the surface seeds from this key on mount and writes
   * every successful payload back with its timestamp. Pages that already own their cache
   * keep doing it themselves and leave this unset.
   *
   * Seeding alone (no `staleAfterMs`) keeps today's always-revalidate behavior: the cached
   * payload paints immediately instead of a skeleton, and the live value still arrives. Add
   * `staleAfterMs` only for a surface that OWNS its truth — a view that mirrors state
   * another page can mutate would otherwise contradict the toggle the user just made, with
   * no request in flight to correct it.
   */
  sessionCacheKey?: string;
};

/** The four independent facts a snapshot yields; every decision below reads only these. */
type SurfaceFacts = {
  /** The page wants this surface to render at all. */
  wanted: boolean;
  /** A payload is on screen. */
  hasPayload: boolean;
  /** A request is in flight. */
  inFlight: boolean;
  /** The latest settled attempt failed. */
  failed: boolean;
};

/**
 * The phase a surface is in. There is one phase per presentation: the two loading phases
 * that keep content apart from the two that do not, so no phase needs a conditional to pick
 * its own flags.
 */
type SurfacePhase =
  | "off"
  | "empty-loading"
  | "empty-retry"
  | "content-loading"
  | "content-retry"
  | "content"
  | "empty-settled"
  | "cold-failure"
  | "stale-failure";

type SurfaceShape = {
  /** The kind this phase reports, or null when the settled payload decides it. */
  kind: DataSurfaceKind | null;
  showSkeleton: boolean;
  refreshing: boolean;
  showError: boolean;
  /** Whether the settled payload becomes the state's data. */
  carriesPayload: boolean;
  /** Whether the attempt's error travels with the state. */
  carriesError: boolean;
};

const SURFACE_SHAPES: Record<SurfacePhase, SurfaceShape> = {
  off: { kind: "disabled", showSkeleton: false, refreshing: false, showError: false, carriesPayload: false, carriesError: false },
  "empty-loading": { kind: "cold", showSkeleton: true, refreshing: true, showError: false, carriesPayload: false, carriesError: false },
  "empty-retry": { kind: "retrying-cold", showSkeleton: true, refreshing: true, showError: false, carriesPayload: false, carriesError: true },
  "content-loading": { kind: "loading-with-stale-data", showSkeleton: false, refreshing: true, showError: false, carriesPayload: true, carriesError: true },
  "content-retry": { kind: "loading-with-stale-data", showSkeleton: false, refreshing: true, showError: true, carriesPayload: true, carriesError: true },
  content: { kind: null, showSkeleton: false, refreshing: false, showError: false, carriesPayload: true, carriesError: false },
  "empty-settled": { kind: "cold", showSkeleton: true, refreshing: false, showError: false, carriesPayload: false, carriesError: false },
  "cold-failure": { kind: "failed-cold", showSkeleton: false, refreshing: false, showError: true, carriesPayload: false, carriesError: true },
  "stale-failure": { kind: "failed-with-stale", showSkeleton: false, refreshing: false, showError: true, carriesPayload: true, carriesError: true },
};

function surfaceFacts<T>(view: ResourceSnapshot<T>, wanted: boolean): SurfaceFacts {
  return {
    wanted,
    hasPayload: view.data !== undefined,
    inFlight: view.refreshing,
    failed: !view.lastAttemptOk && view.error !== undefined,
  };
}

/**
 * Fold the facts into the one phase whose shape describes this snapshot. An in-flight
 * request outranks a settled failure, so a slow retry keeps showing progress instead of
 * freezing on the previous error.
 */
function surfacePhase(facts: SurfaceFacts): SurfacePhase {
  if (!facts.wanted) return "off";
  if (facts.inFlight) {
    if (facts.hasPayload) return facts.failed ? "content-retry" : "content-loading";
    return facts.failed ? "empty-retry" : "empty-loading";
  }
  if (facts.failed) return facts.hasPayload ? "stale-failure" : "cold-failure";
  return facts.hasPayload ? "content" : "empty-settled";
}

/** The payload the phase exposes, and the error it carries alongside. */
function surfaceAttempt<T>(shape: SurfaceShape, view: ResourceSnapshot<T>): SurfaceAttempt<T> {
  return {
    data: shape.carriesPayload ? view.data : undefined,
    error: shape.carriesError ? view.error : undefined,
  };
}

export function classifyDataSurface<T>(
  view: ResourceSnapshot<T>,
  isEmpty: SurfaceEmptyTest<T>,
  wanted: boolean,
): DataSurfaceState<T> {
  const shape = SURFACE_SHAPES[surfacePhase(surfaceFacts(view, wanted))];
  const attempt = surfaceAttempt(shape, view);
  const kind = shape.kind ?? (isEmpty(attempt.data as T) ? "ready-empty" : "ready-populated");
  return {
    kind,
    data: attempt.data,
    error: attempt.error,
    showSkeleton: shape.showSkeleton,
    refreshing: shape.refreshing,
    showError: shape.showError,
  };
}

/** Read the cached board for a key; no key means there is no seed to read. */
function readCachedSeed<T>(cacheKey: string | undefined): SessionListEntry<T> | null {
  return cacheKey ? readSessionListCacheEntry<T>(cacheKey) : null;
}

/** Keep the cached board instead of a placeholder seed; see data-surface-seed. */
function cacheSeed<T>(
  cachedSeed: SessionListEntry<T> | null,
  forwarded: ClientResourceOptions<T>,
): { initialData?: T; initialDataCachedAt?: number | null } {
  return {
    initialData: mergeDataSurfaceSeed(cachedSeed?.data, forwarded.initialData),
    initialDataCachedAt: forwarded.initialDataCachedAt ?? cachedSeed?.cachedAt ?? null,
  };
}

/**
 * Thin adapter over `useKeyedClientResource`: no extra cache, fetch, effect, or timer. All
 * inputs come from the external store snapshot, so every subscriber classifies identically.
 */
export function useDataSurface<T>(
  key: string,
  deps: readonly unknown[],
  load: SurfaceLoader<T>,
  options: DataSurfaceOptions<T>,
): DataSurfaceResource<T> {
  const { isEmpty, sessionCacheKey, enabled = true, ...forwarded } = options;
  // Parsing a cached board is not free, so read the seed once per key: a page like the
  // Integrations overview holds eight of these, and each would otherwise re-parse on every
  // render as the other resources settle.
  const cachedSeed = useMemo(() => readCachedSeed<T>(sessionCacheKey), [sessionCacheKey]);
  const loadAndStore = useCallback<SurfaceLoader<T>>(async (signal) => {
    const payload = await load(signal);
    if (sessionCacheKey) writeSessionListCacheEntry(sessionCacheKey, payload);
    return payload;
  }, [load, sessionCacheKey]);
  const resourceOptions: ClientResourceOptions<T> = sessionCacheKey
    ? { ...forwarded, ...cacheSeed(cachedSeed, forwarded) }
    : forwarded;
  const live = useKeyedClientResource(key, deps, loadAndStore, { enabled, ...resourceOptions });
  return { ...live, state: classifyDataSurface(live, isEmpty, enabled) };
}
