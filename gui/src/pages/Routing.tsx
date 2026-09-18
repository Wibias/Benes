/** Benes dashboard source. The Routing page.
 *
 * A composition, not a workspace: it owns which surface the URL addresses, whether the
 * listener's Codex app-server is stale, and the restart the page head offers, and hands all
 * of that to `RoutingPageShell`. The workspaces themselves — Profiles, Compatibility,
 * Combos — and what each of them shows stay the shell's and the registry's business.
 */
import { useCallback, useEffect, useState } from "react";
import { useT } from "../i18n/shared.ts";
import { onRouteEvent } from "../use-app-route-state.ts";
import { useCodexAppServerState } from "../use-codex-app-server-state.ts";
import { useCodexRestart } from "../use-codex-restart.ts";
import { RoutingPageShell } from "./routing-page-shell.tsx";
import {
  mountedRoutingSurfaces,
  readRoutingSurface,
  selectRoutingSurface,
  type RoutingSurface,
} from "./routing-tab.ts";

/** One visit: the surface the URL addresses, and every surface shown since the page opened. */
interface RoutingVisit {
  readonly surface: RoutingSurface;
  readonly mounted: ReadonlySet<RoutingSurface>;
}

/**
 * The state after showing one surface.
 *
 * Recording the visit keeps a workspace the reader already opened mounted behind the one
 * on screen, so returning to it shows what it had rather than re-reading it.
 */
function visitOf(surface: RoutingSurface, mounted: ReadonlySet<RoutingSurface>): RoutingVisit {
  return { surface, mounted: mountedRoutingSurfaces(mounted, surface) };
}

/**
 * The visited surfaces, kept in step with the URL.
 *
 * A route event is `use-app-route-state`'s definition of "the hash may have changed", so
 * this page shares it rather than binding the same two events a second time.
 */
function useRoutingVisit(): [RoutingVisit, (next: RoutingSurface) => void] {
  const [visit, setVisit] = useState<RoutingVisit>(
    () => visitOf(readRoutingSurface(), new Set<RoutingSurface>()),
  );
  const show = useCallback((next: RoutingSurface) => {
    setVisit(current => visitOf(next, current.mounted));
  }, []);
  useEffect(() => onRouteEvent(() => show(readRoutingSurface())), [show]);
  // Deliberate navigation pushes the surface's hash and shows it now, rather than waiting
  // for the event the push is about to emit.
  const select = useCallback((next: RoutingSurface) => {
    selectRoutingSurface(next);
    show(next);
  }, [show]);
  return [visit, select];
}

export default function Routing({ apiBase, restartEpoch = 0 }: { apiBase: string; restartEpoch?: number }) {
  const t = useT();
  const [visit, selectSurface] = useRoutingVisit();
  const appServer = useCodexAppServerState(apiBase, restartEpoch);
  /** A restart of the same reading: the epoch it advances is the app's, this is the page's. */
  const codexController = useCodexRestart(apiBase, { onSettled: () => { appServer.reload(); } });
  const [refreshNonce, setRefreshNonce] = useState(0);

  return (
    <RoutingPageShell
      t={t}
      surface={visit.surface}
      appServerState={appServer.reading}
      codexController={codexController}
      onRefresh={() => setRefreshNonce(value => value + 1)}
      refreshNonce={refreshNonce}
      mounted={visit.mounted}
      apiBase={apiBase}
      onSelectSurface={selectSurface}
    />
  );
}
