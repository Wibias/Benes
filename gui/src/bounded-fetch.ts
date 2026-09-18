/**
 * Own a single request deadline.
 *
 * The returned `controller` is the caller abort handle. The returned `signal`
 * is the effective abort source: either that owner or the elapsed timeout.
 * `clear()` drops timer ownership on the fallback path and must not abort a
 * request that already succeeded.
 */

export type BoundedFetch = {
  controller: AbortController;
  signal: AbortSignal;
  clear: () => void;
};

function requireDeadlineMs(ms: number): number {
  if (!Number.isFinite(ms) || ms < 0) {
    throw new RangeError("createBoundedFetch timeout must be a finite number of milliseconds >= 0");
  }
  return ms;
}

function nativeMergedSignal(owner: AbortSignal, timeoutMs: number): AbortSignal | null {
  if (typeof AbortSignal === "undefined") return null;
  if (typeof AbortSignal.any !== "function") return null;
  if (typeof AbortSignal.timeout !== "function") return null;
  return AbortSignal.any([owner, AbortSignal.timeout(timeoutMs)]);
}

function fallbackMergedSignal(owner: AbortController, timeoutMs: number): { signal: AbortSignal; clear: () => void } {
  const timeoutOwner = new AbortController();
  const merged = new AbortController();
  const forward = (source: AbortSignal) => {
    if (source.aborted) {
      merged.abort(source.reason);
      return;
    }
    source.addEventListener("abort", () => merged.abort(source.reason), { once: true });
  };
  forward(owner.signal);
  forward(timeoutOwner.signal);
  const timer = setTimeout(() => {
    timeoutOwner.abort();
  }, timeoutMs);
  return {
    signal: merged.signal,
    clear: () => {
      clearTimeout(timer);
    },
  };
}

export function createBoundedFetch(ms: number): BoundedFetch {
  const timeoutMs = requireDeadlineMs(ms);
  const controller = new AbortController();
  const native = nativeMergedSignal(controller.signal, timeoutMs);
  if (native) {
    return {
      controller,
      signal: native,
      clear: () => undefined,
    };
  }
  const fallback = fallbackMergedSignal(controller, timeoutMs);
  return {
    controller,
    signal: fallback.signal,
    clear: fallback.clear,
  };
}
