/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Shared visibility-aware interval for the dashboard's raw pollers.
 *
 * The client-resource layer already suspends its own store timers while the tab is
 * hidden; the raw setInterval pollers around it did not, so a background tab kept
 * paying full poll cost. This module is the single owner of the rule: hidden means no
 * timer and no callback, and coming back fires one catch-up tick before the cadence
 * resumes.
 */

export type VisibilityPollOptions = {
  /** Fire the callback once on start. Default false. */
  immediate?: boolean;
  /**
   * Default true: hidden tabs neither tick nor hold a timer. Set false only for a poll
   * whose whole purpose is noticing something happening off-screen (a restarted server
   * answering, for instance).
   */
  pauseWhenHidden?: boolean;
};

type PollSettings = {
  /** Drop the timer while the tab is hidden. */
  suspendWhileHidden: boolean;
  /** Run the callback once, before the first interval elapses. */
  tickOnStart: boolean;
};

function pollSettings(options?: VisibilityPollOptions): PollSettings {
  return {
    suspendWhileHidden: options?.pauseWhenHidden !== false,
    tickOnStart: options?.immediate === true,
  };
}

/** A repeating timer that the poller creates and drops. */
interface RepeatingTimer {
  start(run: () => void, intervalMs: number): void;
  cancel(): void;
}

function tabIsHidden(): boolean {
  return typeof document !== "undefined" && document.visibilityState === "hidden";
}

/**
 * Timers run in the scope the migrated pollers already used: `window` when it exists,
 * because their instrumentation intercepts it there, and the bare global otherwise.
 */
function timerScope(): Pick<Window, "setInterval" | "clearInterval"> {
  return typeof window === "undefined" ? globalThis : window;
}

function repeatingTimer(): RepeatingTimer {
  let id: ReturnType<typeof setInterval> | null = null;
  return {
    start(run, intervalMs) {
      if (id === null) id = timerScope().setInterval(run, intervalMs) as unknown as ReturnType<typeof setInterval>;
    },
    cancel() {
      if (id === null) return;
      timerScope().clearInterval(id as unknown as number);
      id = null;
    },
  };
}

/** Subscribe to tab visibility changes; a host without a document never notifies. */
function onTabVisibility(listener: () => void): () => void {
  if (typeof document === "undefined") return () => {};
  document.addEventListener("visibilitychange", listener);
  return () => {
    document.removeEventListener("visibilitychange", listener);
  };
}

/**
 * Start an interval that exists only while the tab is visible (unless opted out).
 * Returns stop(), which drops the timer and the visibility listener in any state.
 */
export function startVisibilityPoll(
  callback: () => void,
  intervalMs: number,
  options?: VisibilityPollOptions,
): () => void {
  const settings = pollSettings(options);
  const timer = repeatingTimer();
  let disposed = false;

  // A callback that throws must not kill the poll: without this the cadence would stop
  // silently instead of retrying on the next tick.
  const run = () => {
    try {
      callback();
    } catch (error) {
      console.error("[visibility-poll]", error);
    }
  };

  const sync = (catchUp: boolean) => {
    if (disposed) return;
    if (settings.suspendWhileHidden && tabIsHidden()) {
      timer.cancel();
      return;
    }
    if (catchUp) run();
    timer.start(run, intervalMs);
  };

  const detach = settings.suspendWhileHidden ? onTabVisibility(() => sync(true)) : () => {};
  sync(false);
  if (settings.tickOnStart) run();

  return () => {
    disposed = true;
    timer.cancel();
    detach();
  };
}
