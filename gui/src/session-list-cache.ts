/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * SessionStorage helpers for non-secret GUI list/summary shapes (SWR seeds).
 * Never store API keys, tokens, or credentials here — XSS can read sessionStorage.
 */

/** Envelope marker: distinguishes a timestamped entry from a legacy raw value. */
const CACHED_AT_KEY = "__benesCachedAt";

export type SessionListEntry<T> = {
  data: T;
  /** null when the cache predates timestamping — treated as unknown age (stale). */
  cachedAt: number | null;
};

/**
 * The browser store, or undefined in a worker/SSR/test host. A warm seed is an
 * optimization, so a missing store reads as "no cache" rather than an error.
 */
function cacheStore(): Storage | undefined {
  return typeof sessionStorage === "undefined" ? undefined : sessionStorage;
}

function isSeededEnvelope(value: unknown): value is { [CACHED_AT_KEY]: number; data: unknown } {
  if (typeof value !== "object" || value === null) return false;
  const record = value as Record<string, unknown>;
  return typeof record[CACHED_AT_KEY] === "number" && "data" in record;
}

/** A legacy (untimestamped) value reads as unknown age, so it self-heals on first use. */
function decodeEntry<T>(stored: string): SessionListEntry<T> {
  const decoded = JSON.parse(stored) as unknown;
  if (isSeededEnvelope(decoded)) {
    return { data: decoded.data as T, cachedAt: decoded[CACHED_AT_KEY] };
  }
  return { data: decoded as T, cachedAt: null };
}

/**
 * Run a read against the browser store, falling back when the store is unavailable or
 * rejects the operation — a corrupt value and a blocked store mean the same thing here.
 */
function readThrough<T>(missing: T, read: (store: Storage) => T): T {
  const store = cacheStore();
  if (!store) return missing;
  try {
    return read(store);
  } catch {
    return missing;
  }
}

/** Run a write against the browser store; a rejected write leaves the cache untouched. */
function writeThrough(write: (store: Storage) => void): void {
  const store = cacheStore();
  if (!store) return;
  try {
    write(store);
  } catch {
    /* private mode / quota — a seed is never worth a failure */
  }
}

function readEntry<T>(store: Storage, storageKey: string): SessionListEntry<T> | null {
  const stored = store.getItem(storageKey);
  return stored === null ? null : decodeEntry<T>(stored);
}

/** Read a seed with its age. */
export function readSessionListCacheEntry<T>(storageKey: string): SessionListEntry<T> | null {
  return readThrough<SessionListEntry<T> | null>(null, (store) => readEntry<T>(store, storageKey));
}

/** Read a seed without its age, transparent to callers that never ask for one. */
export function readSessionListCache<T>(storageKey: string): T | null {
  return readSessionListCacheEntry<T>(storageKey)?.data ?? null;
}

/** Write a seed with its write time so a revisit can decide whether to revalidate. */
export function writeSessionListCacheEntry<T>(storageKey: string, payload: T): void {
  writeThrough((store) => {
    store.setItem(storageKey, JSON.stringify({ [CACHED_AT_KEY]: Date.now(), data: payload }));
  });
}

/** Write a raw value with no timestamp envelope. */
export function writeSessionListCache(storageKey: string, payload: unknown): void {
  writeThrough((store) => {
    store.setItem(storageKey, JSON.stringify(payload));
  });
}

export function clearSessionListCache(storageKey: string): void {
  writeThrough((store) => {
    store.removeItem(storageKey);
  });
}
