/**
 * Combos route.
 *
 * The route owns three jobs: the data surface it reads through, the cache it
 * seeds from, and the chrome around the workspace. Everything else already has
 * a Benes owner — the panes are `ComboWorkspace`, the write flow is
 * `combo-workspace-mutations`, and the notice/rail rules are in
 * `combo-workspace-utils`.
 *
 * The panel can be mounted while its Routing tab is hidden. That gates the
 * NETWORK only: a hidden panel must not fetch, but its subtree has to stay
 * mounted, because unmounting would take every unsaved editor draft with it.
 */

import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import ComboWorkspace from "../components/ComboWorkspace";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { combosPageNotices } from "../components/combo-workspace-utils";
import type { ComboItem } from "../combo-workspace-data";
import {
  comboDelete,
  comboPut,
  comboRemoveNotice,
  comboWriteNotice,
  runComboWrite,
  type ComboWrite,
  type ComboWriteFailureKey,
  type ComboWriteResult,
} from "../combo-workspace-mutations";
import { useDataSurface } from "../data-surface";
import { useT, type TFn } from "../i18n/shared";
import { loadCombosWorkspace, type CachedCombosPage } from "../nav-board-loads";
import { combosWorkspaceCacheKey } from "../nav-board-resources";
import { readSessionListCacheEntry } from "../session-list-cache";
import { Notice, ToastNotice } from "../ui";

/** Success banners are transient; a failure stays until the next write. */
const STATUS_LIFETIME_MS = 5000;

/** A cached payload older than this is refreshed in the background. */
const STALE_AFTER_MS = 60_000;

/** The route's transient status line, when there is nothing to announce. */
const NO_NOTICE = { text: "", ok: false };

type CombosNotice = { text: string; ok: boolean };

/** Cached payload the route can render before its first fetch answers. */
function cachedEntry(cacheKey: string): {
  data: CachedCombosPage | undefined;
  cachedAt: number | null;
} {
  const entry = readSessionListCacheEntry<CachedCombosPage>(cacheKey);
  return { data: entry?.data, cachedAt: entry?.cachedAt ?? null };
}

interface CombosProps {
  apiBase: string;
  /**
   * False while this panel is mounted but hidden behind another Routing tab. It gates
   * the NETWORK only — the rendered tree stays put so unsaved editor drafts survive a
   * tab hop. Defaults true so the standalone page keeps its existing behaviour.
   */
  active?: boolean;
  /** Reports the combo count up to the tab strip. */
  onCountChange?: (count: number) => void;
  refreshNonce?: number;
}

type CombosSurfaceOptions = Required<Pick<CombosProps, "apiBase" | "active" | "refreshNonce">>
  & Pick<CombosProps, "onCountChange">;

/**
 * The route's read side: the data surface, the retained payload a hidden panel
 * keeps rendering, and the transient status line.
 */
function useCombosSurface({ apiBase, active, refreshNonce, onCountChange }: CombosSurfaceOptions) {
  const cacheKey = useMemo(() => combosWorkspaceCacheKey(apiBase), [apiBase]);
  const seed = useMemo(() => cachedEntry(cacheKey), [cacheKey]);

  /*
   * The last coherent payload. While `active` is false the resource is disabled and
   * reports `data: undefined` with no skeleton and no error, so falling back to empty
   * arrays there would swap the workspace for a first-run empty state and take every
   * unsaved draft with it. State rather than a ref: this repo avoids render-time ref
   * reads under React Compiler, and a ref would not re-render on a new payload.
   */
  const [retained, setRetained] = useState<CachedCombosPage | null>(seed.data ?? null);
  // One notification at a time: the text, and whether it reports a success.
  const [notice, setNotice] = useState<CombosNotice>(NO_NOTICE);

  const notify = useCallback((text: string, ok: boolean) => setNotice({ text, ok }), []);
  const clearStatus = useCallback(() => setNotice(NO_NOTICE), []);

  useEffect(() => {
    if (!notice.text || !notice.ok) return;
    const timer = window.setTimeout(clearStatus, STATUS_LIFETIME_MS);
    return () => window.clearTimeout(timer);
  }, [clearStatus, notice.ok, notice.text]);

  const load = useCallback(async (signal?: AbortSignal) => {
    const next = await loadCombosWorkspace(apiBase, signal);
    setRetained(next);
    return next;
  }, [apiBase]);

  const resource = useDataSurface<CachedCombosPage>(cacheKey, [apiBase], load, {
    isEmpty: () => false,
    initialData: seed.data,
    initialDataCachedAt: seed.cachedAt,
    staleAfterMs: STALE_AFTER_MS,
    enabled: active,
  });
  const { state, refresh } = resource;
  const seenNonce = useRef(0);

  useEffect(() => {
    if (!active || refreshNonce <= 0 || refreshNonce === seenNonce.current) return;
    seenNonce.current = refreshNonce;
    refresh();
  }, [active, refresh, refreshNonce]);

  const page = state.data ?? retained ?? undefined;
  const comboCount = page?.combos.length ?? 0;

  // Report the count from an effect keyed on the length, not during render, so a
  // parent re-render cannot refire it.
  useEffect(() => {
    if (!page) return;
    onCountChange?.(comboCount);
  }, [comboCount, onCountChange, page]);

  return { state, page, refresh, notice, notify, clearStatus };
}

/**
 * The route's write side. Both mutations follow one shape: issue the request,
 * then either refresh and announce the result, or surface the refusal.
 */
function useComboWrites(
  apiBase: string,
  t: TFn,
  notify: (text: string, ok: boolean) => void,
  refresh: () => void,
) {
  const issue = useCallback(async (
    write: ComboWrite,
    failureKey: ComboWriteFailureKey,
    announcement: () => string,
  ): Promise<ComboWriteResult> => {
    const outcome = await runComboWrite(write, t, failureKey);
    if (!outcome.ok) {
      notify(outcome.error, false);
      return outcome;
    }
    refresh();
    notify(announcement(), true);
    return outcome;
  }, [notify, refresh, t]);

  const save = (item: ComboItem, isCreate: boolean, renameFrom?: string): Promise<ComboWriteResult> =>
    issue(comboPut(apiBase, item, renameFrom), "cws.saveFailed", () =>
      comboWriteNotice(t, item, isCreate, renameFrom));

  const remove = (id: string): Promise<ComboWriteResult> =>
    issue(comboDelete(apiBase, id), "cws.removeFailed", () => comboRemoveNotice(t, id));

  return { save, remove };
}

/** Blocking failure state: no usable payload, so nothing can be rendered. */
function CombosColdFailure({ reason, onRetry }: { reason: string; onRetry: () => void }) {
  const t = useT();
  return (
    <>
      <Notice tone="err" role="alert">{reason}</Notice>
      <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
    </>
  );
}

type CombosSurface = ReturnType<typeof useCombosSurface>;
type ComboWrites = ReturnType<typeof useComboWrites>;

export default function Combos({
  apiBase,
  active = true,
  onCountChange,
  refreshNonce = 0,
}: CombosProps) {
  const t = useT();
  const surface = useCombosSurface({ apiBase, active, refreshNonce, onCountChange });
  const writes = useComboWrites(apiBase, t, surface.notify, surface.refresh);
  return <CombosStage t={t} surface={surface} writes={writes} />;
}

/** The route's three states: skeleton, blocking failure, or the workspace shell. */
function CombosStage({
  t,
  surface,
  writes,
}: {
  t: TFn;
  surface: CombosSurface;
  writes: ComboWrites;
}) {
  const { state, page } = surface;

  if (state.showSkeleton && !page) {
    return <DataSurfaceSkeleton label={t("cws.loading")} rows={5} />;
  }
  if (state.kind === "failed-cold" && !page) {
    return (
      <CombosColdFailure
        reason={state.error instanceof Error ? state.error.message : t("cws.loadFailed")}
        onRetry={surface.refresh}
      />
    );
  }
  return <CombosWorkspaceShell t={t} surface={surface} writes={writes} />;
}

/**
 * The shell a resolved workspace sits in: the write announcement, the
 * stale-refresh warning, and the workspace inside the tab's scroll container.
 */
function CombosWorkspaceShell({
  t,
  surface,
  writes,
}: {
  t: TFn;
  surface: CombosSurface;
  writes: ComboWrites;
}) {
  const { state, page } = surface;
  // Only the stale-refresh warning reaches the shell: the blocking branch is the
  // cold-failure pane above, and every other surface kind clears both notices.
  const { staleRefreshWarning } = combosPageNotices({
    surfaceKind: state.kind,
    surfaceError: state.error,
    loadFailedLabel: t("cws.loadFailed"),
    refreshFailedStaleLabel: t("cws.refreshFailedStale"),
  });

  // Every workspace prop comes from the one payload the route resolved.
  const workspaceData = {
    combos: page?.combos ?? [],
    providers: page?.providers ?? [],
    models: page?.models ?? [],
    cataloguedComboIds: new Set(page?.cataloguedComboIds ?? []),
  };

  return (
    <div className="combos-workspace-shell">
      {surface.notice.text ? (
        <ToastNotice
          tone={surface.notice.ok ? "ok" : "err"}
          dismissLabel={t("common.close")}
          onDismiss={surface.clearStatus}
        >
          {surface.notice.text}
        </ToastNotice>
      ) : null}
      {staleRefreshWarning ? (
        <div className="combos-workspace-shell-banner">
          <Notice tone="warn" role="status">{staleRefreshWarning}</Notice>
        </div>
      ) : null}
      <div className="combos-workspace-shell-body" aria-busy={state.refreshing}>
        <span className="sr-only" role="status" aria-live="polite" aria-atomic="true">
          {state.refreshing ? t("common.loading") : ""}
        </span>
        <ComboWorkspace
          {...workspaceData}
          loading={false}
          onRefresh={surface.refresh}
          onSave={writes.save}
          onRemove={writes.remove}
        />
      </div>
    </div>
  );
}