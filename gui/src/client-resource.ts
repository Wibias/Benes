/**
 * Dashboard keyed resource runtime.
 *
 * | Concern | Owner | Why the boundary exists |
 * | snapshot / cache | one Resource per key | boards share one in-memory value for an identity |
 * | attempt lifecycle | Resource.attempt + policy commit | one in-flight request; stale generations never paint |
 * | polling | per-key pulse timer | each identity has one cadence (lowest positive subscriber interval) |
 * | visibility | one document listener | hidden tabs should not wake timers unless a subscriber opted out |
 * | subscriptions | Resource.watchers | one membership record per subscriber, not parallel maps |
 * | React | useClientResource / useKeyedClientResource | useSyncExternalStore adapter over the same Resource |
 *
 * This is one GUI facility. Pure settle/skip/timeout rules live in
 * client-resource-policy.ts. There is no second cache engine.
 */
import { useCallback, useLayoutEffect, useRef, useSyncExternalStore } from "react";
import {
  commitResourceFetch,
  DEFAULT_REQUEST_DEADLINE_MS,
  RESOURCE_TIMEOUT,
  emptyResourceSnapshot,
  resourceInFlightSnapshot,
  resourceReplaceInflight,
  shouldSkipQuietResourceFetch,
  waitForResourceAbort,
  type ResourceSnapshotFields,
} from "./client-resource-policy.ts";

export type ResourceSnapshot<T> = ResourceSnapshotFields<T>;
export type RefreshRequest = { forceLoading?: boolean };
export type Fetcher<T> = (signal: AbortSignal) => Promise<T>;
export type ResourceView<T> = ResourceSnapshot<T> & { refresh: (opts?: RefreshRequest) => void };

export interface ClientResourceOptions<T = unknown> {
  pollMs?: number;
  enabled?: boolean;
  pauseWhenHidden?: boolean;
  initialData?: T;
  deadlineMs?: number;
  staleAfterMs?: number;
  initialDataCachedAt?: number | null;
}

export type ResourceInspect = {
  key: string;
  subscribers: number;
  pollMs: number | undefined;
  generation: number;
  refreshing: boolean;
  hasAttempt: boolean;
  lastSettledAt: number | null;
  seedNeedsRevalidate: boolean;
  loaderCount: number;
  hiddenPause: { paused: number; running: number };
};

type Watcher<T> = {
  notify: () => void;
  load: Fetcher<T>;
  pollMs: number | undefined;
  pauseWhenHidden: boolean;
  deadlineMs: number | undefined;
};

type Attempt = {
  epoch: number;
  abort: AbortController;
  owner: (() => void) | null;
  deadlineMs: number;
  timer: ReturnType<typeof setTimeout>;
  timedOut: () => boolean;
};

const EMPTY_SNAPSHOT: ResourceSnapshot<never> = emptyResourceSnapshot();
const table = new Map<string, Resource<unknown>>();
let pageListener: (() => void) | null = null;

function pageIsHidden(): boolean {
  return typeof document !== "undefined" && document.visibilityState === "hidden";
}

function positiveMs(value: number | undefined): value is number {
  return typeof value === "number" && value > 0;
}

function bindPage(): void {
  if (typeof document === "undefined" || pageListener) return;
  const onChange = () => {
    const hidden = pageIsHidden();
    for (const item of table.values()) {
      item.retunePulse();
      if (!hidden) item.catchUp();
    }
  };
  document.addEventListener("visibilitychange", onChange);
  pageListener = onChange;
}

function unbindPageIfIdle(): void {
  if (!pageListener) return;
  for (const item of table.values()) {
    if (item.wantsCadence()) return;
  }
  if (typeof document !== "undefined") {
    document.removeEventListener("visibilitychange", pageListener);
  }
  pageListener = null;
}

class Resource<T> {
  readonly key: string;
  snapshot: ResourceSnapshot<T> = emptyResourceSnapshot();
  epoch = 0;
  seedDirty = false;
  settledAt: number | undefined;
  private readonly watchers = new Map<() => void, Watcher<T>>();
  private attempt: Attempt | null = null;
  private pulse: ReturnType<typeof setInterval> | null = null;
  private pulseMs: number | undefined;

  constructor(key: string) {
    this.key = key;
  }

  watcherCount(): number {
    return this.watchers.size;
  }

  hasPulse(): boolean {
    return this.pulse !== null;
  }

  wantsCadence(): boolean {
    return this.fastestPoll() != null;
  }

  inspect(): ResourceInspect {
    let pollMs: number | undefined;
    let paused = 0;
    let running = 0;
    for (const watcher of this.watchers.values()) {
      if (!positiveMs(watcher.pollMs)) continue;
      pollMs = pollMs == null ? watcher.pollMs : Math.min(pollMs, watcher.pollMs);
      if (watcher.pauseWhenHidden === false) running += 1;
      else paused += 1;
    }
    return {
      key: this.key,
      subscribers: this.watchers.size,
      pollMs,
      generation: this.epoch,
      refreshing: this.snapshot.refreshing,
      hasAttempt: this.attempt !== null,
      lastSettledAt: this.settledAt ?? null,
      seedNeedsRevalidate: this.seedDirty,
      loaderCount: this.watchers.size,
      hiddenPause: { paused, running },
    };
  }

  join(notify: () => void, watcher: Omit<Watcher<T>, "notify">, staleAfterMs?: number): () => void {
    this.watchers.set(notify, { ...watcher, notify });
    if (this.watchers.size === 1 && this.needsOpeningLoad(staleAfterMs)) {
      void this.ask({
        replace: true,
        owner: notify,
        load: watcher.load,
        deadlineMs: watcher.deadlineMs,
      });
    }
    this.retunePulse();
    return () => this.leave(notify);
  }

  publish(data: T): void {
    this.attempt?.abort.abort();
    this.attempt = null;
    this.epoch += 1;
    this.snapshot = {
      ...emptyResourceSnapshot<T>(),
      data,
      hasSucceeded: true,
      lastAttemptOk: true,
    };
    this.seedDirty = this.watchers.size === 0;
    this.settledAt = Date.now();
    this.emit();
  }

  markFreshSeed(cachedAt: number): void {
    this.seedDirty = false;
    this.settledAt = cachedAt;
  }

  reload(load: Fetcher<T>, owner: (() => void) | null, opts?: RefreshRequest, deadlineMs?: number): void {
    const watching = owner ? this.watchers.get(owner) : undefined;
    void this.ask({
      replace: true,
      owner,
      load,
      forceLoading: opts?.forceLoading,
      deadlineMs: watching?.deadlineMs ?? deadlineMs,
    });
  }

  abortOwned(): void {
    this.attempt?.abort.abort();
    this.attempt = null;
    this.stopPulse();
  }

  retunePulse(): void {
    const ms = this.fastestPoll();
    if (ms == null) {
      this.stopPulse();
      unbindPageIfIdle();
      return;
    }
    const allowed = !pageIsHidden() || this.hiddenOptOut();
    if (!allowed) {
      this.stopPulse();
      bindPage();
      return;
    }
    if (this.pulse !== null && this.pulseMs === ms) return;
    this.stopPulse();
    this.pulseMs = ms;
    this.pulse = setInterval(() => this.quietTick(), ms);
    bindPage();
  }

  catchUp(): void {
    if (this.fastestPoll() == null) return;
    const watcher = this.pickWatcher(false);
    if (!watcher) return;
    void this.ask({
      replace: false,
      owner: watcher.notify,
      load: watcher.load,
      deadlineMs: watcher.deadlineMs,
    });
  }

  private needsOpeningLoad(staleAfterMs: number | undefined): boolean {
    if (this.snapshot.data === undefined) return true;
    if (this.seedDirty) return true;
    if (typeof staleAfterMs !== "number") return false;
    if (this.settledAt == null) return false;
    return Date.now() - this.settledAt > staleAfterMs;
  }

  private leave(notify: () => void): void {
    this.watchers.delete(notify);
    const killed = this.cancelOwnedBy(notify);
    if (this.watchers.size === 0) {
      this.stopPulse();
      unbindPageIfIdle();
      scheduleEviction(this.key, this as Resource<unknown>);
      return;
    }
    if (killed) {
      const next = this.pickWatcher(pageIsHidden());
      if (next) {
        void this.ask({
          replace: true,
          owner: next.notify,
          load: next.load,
          deadlineMs: next.deadlineMs,
        });
      }
    }
    this.retunePulse();
  }

  private fastestPoll(): number | undefined {
    let lowest: number | undefined;
    for (const watcher of this.watchers.values()) {
      if (!positiveMs(watcher.pollMs)) continue;
      lowest = lowest == null ? watcher.pollMs : Math.min(lowest, watcher.pollMs);
    }
    return lowest;
  }

  private hiddenOptOut(): boolean {
    for (const watcher of this.watchers.values()) {
      if (positiveMs(watcher.pollMs) && watcher.pauseWhenHidden === false) return true;
    }
    return false;
  }

  private pickWatcher(hidden: boolean): Watcher<T> | null {
    if (hidden) {
      for (const watcher of this.watchers.values()) {
        if (positiveMs(watcher.pollMs) && watcher.pauseWhenHidden === false) return watcher;
      }
      return null;
    }
    for (const watcher of this.watchers.values()) {
      if (positiveMs(watcher.pollMs)) return watcher;
    }
    for (const watcher of this.watchers.values()) return watcher;
    return null;
  }

  private quietTick(): void {
    const watcher = this.pickWatcher(pageIsHidden());
    if (!watcher) return;
    void this.ask({
      replace: false,
      owner: watcher.notify,
      load: watcher.load,
      deadlineMs: watcher.deadlineMs,
    });
  }

  private stopPulse(): void {
    if (this.pulse !== null) clearInterval(this.pulse);
    this.pulse = null;
    this.pulseMs = undefined;
  }

  private cancelOwnedBy(owner: () => void): boolean {
    if (this.attempt?.owner !== owner) return false;
    this.attempt.abort.abort();
    this.attempt = null;
    this.epoch += 1;
    if (this.snapshot.refreshing) {
      this.snapshot = { ...this.snapshot, refreshing: false };
      this.emit();
    }
    return true;
  }

  private async ask(options: {
    replace?: boolean;
    owner: (() => void) | null;
    load: Fetcher<T>;
    forceLoading?: boolean;
    deadlineMs?: number;
  }): Promise<void> {
    const replace = resourceReplaceInflight(options.replace);
    if (shouldSkipQuietResourceFetch(this.attempt !== null, replace)) return;
    if (replace) this.attempt?.abort.abort();
    const handle = this.openAttempt(options.owner, options.deadlineMs ?? DEFAULT_REQUEST_DEADLINE_MS, options.forceLoading);
    try {
      const data = await Promise.race([
        options.load(handle.abort.signal),
        waitForResourceAbort(handle.abort, handle.timedOut, handle.deadlineMs),
      ]);
      this.settle(handle, { status: "success", data });
    } catch (error) {
      this.settle(handle, { status: "error", error });
    } finally {
      this.closeAttempt(handle);
    }
  }

  private openAttempt(owner: (() => void) | null, deadlineMs: number, forceLoading?: boolean): Attempt {
    const abort = new AbortController();
    const epoch = ++this.epoch;
    let timedOut = false;
    const timer = setTimeout(() => {
      timedOut = true;
      abort.abort(RESOURCE_TIMEOUT);
    }, deadlineMs);
    const handle: Attempt = { epoch, abort, owner, deadlineMs, timer, timedOut: () => timedOut };
    this.attempt = handle;
    this.snapshot = resourceInFlightSnapshot(this.snapshot, forceLoading);
    this.emit();
    return handle;
  }

  private settle(handle: Attempt, outcome: { status: "success"; data: T } | { status: "error"; error: unknown }): void {
    const commit = commitResourceFetch({
      attemptGeneration: handle.epoch,
      storeGeneration: this.epoch,
      aborted: handle.abort.signal.aborted,
      timedOut: handle.timedOut(),
    }, outcome, this.snapshot);
    if (commit.kind === "ignore") return;
    this.seedDirty = commit.seedNeedsRevalidate;
    if (commit.kind === "success") this.settledAt = Date.now();
    this.snapshot = commit.snapshot;
  }

  private closeAttempt(handle: Attempt): void {
    clearTimeout(handle.timer);
    if (this.attempt === handle) this.attempt = null;
    this.emit();
  }

  private emit(): void {
    for (const notify of this.watchers.keys()) notify();
  }
}

function resourceOf<T>(key: string): Resource<T> {
  let item = table.get(key) as Resource<T> | undefined;
  if (!item) {
    item = new Resource<T>(key);
    table.set(key, item as Resource<unknown>);
  }
  return item;
}

function scheduleEviction(key: string, item: Resource<unknown>): void {
  setTimeout(() => {
    if (item.watcherCount() !== 0) return;
    if (table.get(key) !== item) return;
    item.abortOwned();
    table.delete(key);
    unbindPageIfIdle();
  }, 0);
}

function optionalPositive(value: number | undefined): number | undefined {
  if (typeof value !== "number") return undefined;
  if (!Number.isFinite(value) || value <= 0) return undefined;
  return value;
}

function cachedAtFrom(value: number | null | undefined): number | null | undefined {
  if (value == null) return value;
  if (!Number.isFinite(value) || value <= 0) return null;
  return value;
}

export function setClientResourceData<T>(key: string, data: T): void {
  resourceOf<T>(key).publish(data);
}

export function seedClientResourceForTests<T>(key: string, data: T, cachedAt?: number | null, staleAfterMs?: number): void {
  seedVacant(key, data, cachedAt, staleAfterMs);
}

function seedVacant<T>(key: string, data: T, cachedAt?: number | null, staleAfterMs?: number): void {
  const item = resourceOf<T>(key);
  if (item.watcherCount() !== 0 || item.snapshot.data !== undefined) return;
  item.publish(data);
  if (typeof staleAfterMs === "number" && typeof cachedAt === "number" && Date.now() - cachedAt < staleAfterMs) {
    item.markFreshSeed(cachedAt);
  }
}

export type SubscriberSetup<T> = Pick<ClientResourceOptions<T>, "pollMs" | "pauseWhenHidden" | "deadlineMs" | "staleAfterMs"> & {
  fetcher: Fetcher<T>;
};

export function subscribeClientResourceForTests<T>(
  key: string,
  onStoreChange: () => void,
  setup: SubscriberSetup<T>,
): () => void {
  return resourceOf<T>(key).join(onStoreChange, {
    load: setup.fetcher,
    pollMs: setup.pollMs,
    pauseWhenHidden: setup.pauseWhenHidden !== false,
    deadlineMs: setup.deadlineMs,
  }, setup.staleAfterMs);
}

export function readClientResourceSnapshotForTests<T>(key: string): ResourceSnapshot<T> {
  return resourceOf<T>(key).snapshot;
}

export function inspectClientResource(key: string): ResourceInspect | null {
  const item = table.get(key);
  return item ? item.inspect() : null;
}

export function listClientResourceKeys(): string[] {
  return [...table.keys()].sort((left, right) => left.localeCompare(right));
}

export function describeClientResources(): ResourceInspect[] {
  return [...table.values()].map((item) => item.inspect()).sort((left, right) => left.key.localeCompare(right.key));
}

export function hasPollTimerForTests(key: string): boolean {
  return table.get(key)?.hasPulse() === true;
}

export function pollBucketCountForTests(): number {
  let count = 0;
  for (const item of table.values()) {
    if (item.hasPulse()) count += 1;
  }
  return count;
}

export function visibilityListenerBoundForTests(): boolean {
  return pageListener !== null;
}

export function clearClientResourceStoresForTests(): void {
  for (const item of table.values()) item.abortOwned();
  table.clear();
  unbindPageIfIdle();
}

export function useClientResource<T>(
  key: string,
  fetcher: Fetcher<T>,
  options?: ClientResourceOptions<T>,
): ResourceView<T> {
  const enabled = options?.enabled !== false;
  const pollMs = optionalPositive(options?.pollMs);
  const pauseWhenHidden = options?.pauseWhenHidden !== false;
  const deadlineMs = options?.deadlineMs;
  const staleAfterMs = optionalPositive(options?.staleAfterMs);
  const cachedAt = cachedAtFrom(options?.initialDataCachedAt);
  if (enabled && options?.initialData !== undefined) {
    seedVacant(key, options.initialData, cachedAt, staleAfterMs);
  }

  const fetcherRef = useRef(fetcher);
  useLayoutEffect(function keepFetcher() {
    fetcherRef.current = fetcher;
  });
  const load = useCallback((signal: AbortSignal) => fetcherRef.current(signal), []);
  const ownerRef = useRef<(() => void) | null>(null);

  const subscribe = useCallback((onStoreChange: () => void) => {
    ownerRef.current = onStoreChange;
    if (!enabled) return () => {};
    return resourceOf<T>(key).join(onStoreChange, {
      load,
      pollMs,
      pauseWhenHidden,
      deadlineMs,
    }, staleAfterMs);
  }, [key, load, pollMs, enabled, pauseWhenHidden, deadlineMs, staleAfterMs]);

  const getSnapshot = useCallback(
    () => (enabled ? resourceOf<T>(key).snapshot : EMPTY_SNAPSHOT),
    [key, enabled],
  );
  const snapshot = useSyncExternalStore(subscribe, getSnapshot, getSnapshot);
  const refresh = useCallback((opts?: RefreshRequest) => {
    if (!enabled) return;
    resourceOf<T>(key).reload(load, ownerRef.current, opts, deadlineMs);
  }, [key, load, enabled, deadlineMs]);
  return { ...snapshot, refresh };
}

export function keyedResourceShouldForceLoad(
  previousDeps: readonly unknown[] | null,
  previousKey: string | null,
  nextDeps: readonly unknown[],
  nextKey: string,
): boolean {
  if (previousDeps === null) return false;
  if (previousKey !== null && previousKey !== nextKey) return false;
  if (previousDeps.length !== nextDeps.length) return true;
  return previousDeps.some((value, index) => !Object.is(value, nextDeps[index]));
}

export function useKeyedClientResource<T>(
  key: string,
  deps: readonly unknown[],
  load: Fetcher<T>,
  options?: ClientResourceOptions<T>,
): ResourceView<T> {
  const resource = useClientResource(key, load, options);
  const prevDepsRef = useRef<readonly unknown[] | null>(null);
  const prevKeyRef = useRef<string | null>(null);

  useLayoutEffect(function revalidateOnDeps() {
    const previousDeps = prevDepsRef.current;
    const previousKey = prevKeyRef.current;
    prevDepsRef.current = deps;
    prevKeyRef.current = key;
    if (!keyedResourceShouldForceLoad(previousDeps, previousKey, deps, key)) return;
    resource.refresh({ forceLoading: true });
  });

  return resource;
}
