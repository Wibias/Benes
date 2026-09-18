/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * The Codex app-server reading a page head's stale banner is drawn from.
 *
 * The reading is one keyed resource rather than page-local state, because `data-surface`
 * already owns everything this used to hand-roll: a superseded read is aborted, a settled
 * read cannot write after unmount, and a refresh is available to whoever needs one. The
 * resource key carries the API base and the restart epoch is a dependency, so a restart
 * re-reads the same reading instead of starting a second one.
 */
import { useCallback } from "react";
import { useDataSurface } from "./data-surface.ts";
import { fetchCodexAppServerState, type AppServerStateOutcome } from "./listener-commands.ts";

/** What the proxy last reported about the Codex app-server it started. */
export type CodexAppServerReading = AppServerStateOutcome["state"];

/** The reading that renders nothing: the banner draws only on a reported `stale`. */
const QUIET_READING: CodexAppServerReading = null;

export function useCodexAppServerState(apiBase: string, restartEpoch: number): {
  readonly reading: CodexAppServerReading;
  readonly reload: () => void;
} {
  const load = useCallback(
    async (signal: AbortSignal) => (await fetchCodexAppServerState(apiBase, { signal })).state,
    [apiBase],
  );
  const surface = useDataSurface<CodexAppServerReading>(
    `codex-app-server:${apiBase}`,
    [apiBase, restartEpoch],
    load,
    { isEmpty: reading => reading === null },
  );
  return {
    reading: surface.data ?? QUIET_READING,
    // The resource's own refresh, which is stable for a key.
    reload: surface.refresh,
  };
}
