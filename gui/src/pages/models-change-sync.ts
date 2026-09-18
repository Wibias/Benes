/** Trailing debounce for Models catalog sync-after-change. */

export const MODELS_CHANGE_SYNC_DEBOUNCE_MS = 5_000;

export type TrailingDebounce = {
  schedule: (run: () => void) => void;
  cancel: () => void;
  flush: () => void;
  pending: () => boolean;
};

/** A catalog snapshot is safe only outside a mutation and from the same mutation epoch. */
export function modelsCatalogMutationStable(
  mutationEpochAtStart: number,
  currentMutationEpoch: number,
  pendingMutations: number,
): boolean {
  return mutationEpochAtStart === currentMutationEpoch && pendingMutations === 0;
}

/** A manual load also needs to be the newest load generation. */
export function shouldApplyModelsCatalogLoad(
  loadGeneration: number,
  currentLoadGeneration: number,
  mutationEpochAtStart: number,
  currentMutationEpoch: number,
  pendingMutations: number,
): boolean {
  return loadGeneration === currentLoadGeneration
    && modelsCatalogMutationStable(mutationEpochAtStart, currentMutationEpoch, pendingMutations);
}

/** Schedule `run` after `delayMs` of quiet; each new schedule resets the timer. */
export function createTrailingDebounce(
  delayMs: number,
  timers: {
    setTimeout: typeof setTimeout;
    clearTimeout: typeof clearTimeout;
  } = globalThis,
): TrailingDebounce {
  let timer: ReturnType<typeof setTimeout> | null = null;
  let pending: (() => void) | null = null;

  const clear = () => {
    if (timer != null) timers.clearTimeout(timer);
    timer = null;
  };

  return {
    schedule(run) {
      pending = run;
      clear();
      timer = timers.setTimeout(() => {
        timer = null;
        const next = pending;
        pending = null;
        next?.();
      }, delayMs);
    },
    cancel() {
      clear();
      pending = null;
    },
    flush() {
      clear();
      const next = pending;
      pending = null;
      next?.();
    },
    pending: () => pending != null,
  };
}
