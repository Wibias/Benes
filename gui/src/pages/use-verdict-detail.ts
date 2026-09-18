/** Benes dashboard source. The verdict detail a Compatibility board shows beside itself.
 *
 * One keyed resource per verdict: `data-surface` owns what this used to hand-roll — aborting a
 * superseded verdict's request, refusing to write after the reader moved on, and the retry — so
 * this hook only says which verdict is open and how its request reads to the pane.
 *
 * An operator who never selects a verdict never starts a read: the resource is enabled by that
 * selection.
 */
import { useCallback } from "react";
import { useDataSurface } from "../data-surface.ts";
import type { VerdictDto } from "../lab/evidence-matrix.ts";
import { fetchVerdictDetail, type VerdictDetailData } from "../lab/lab-client.ts";
import { labFailureMessage } from "./compatibility-matrix-view.ts";

/** One verdict's detail, as the pane draws it. */
export interface VerdictDetailState {
  readonly detail: VerdictDetailData | null;
  readonly loading: boolean;
  readonly error: string | null;
}

export function useVerdictDetail(
  apiBase: string,
  verdict: VerdictDto | null,
  active: boolean,
  loadFailedLabel: string,
): VerdictDetailState {
  const selection = verdict === null ? "" : verdict.projectionKey;
  const load = useCallback(
    async (signal: AbortSignal) => (verdict === null ? null : fetchVerdictDetail(apiBase, verdict, signal)),
    [apiBase, verdict],
  );
  const { state } = useDataSurface<VerdictDetailData | null>(
    `lab-verdict:${apiBase}:${selection}`,
    [apiBase, selection],
    load,
    { isEmpty: detail => detail === null, enabled: active && verdict !== null },
  );
  return {
    detail: state.data ?? null,
    loading: verdict !== null && state.data === undefined && !state.showError,
    error: state.showError ? labFailureMessage(state.error, loadFailedLabel) : null,
  };
}
