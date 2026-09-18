/** Benes dashboard source. The Compatibility matrix page.
 *
 * The page reads one page of Lab verdicts and keeps the reader's place in them: which filters
 * narrow the board, which subview is showing, which verdict is open beside it, and the rows read
 * past the first page. The Lab's evidence semantics stay with the Lab — the matrix, the rows,
 * and the suite list are built by `lab/evidence-matrix`, and what a read's failure means is
 * `./compatibility-matrix-view`'s.
 *
 * The board itself is `./compatibility-matrix-sections`'s, and it draws what this file resolved.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { useDataSurface } from "../data-surface";
import { useI18n } from "../i18n/shared";
import type { Locale } from "../i18n/shared";
import { fetchLabPageData, fetchMoreVerdicts, type LabPageData } from "../lab/lab-client";
import type { LabPage } from "../lab/lab-pages";
import {
  COMPATIBILITY_VERDICTS,
  PRODUCT_EVIDENCE_LAYERS,
  UNFILTERED_VERDICT_FILTERS,
  buildMatrixRows,
  filterVerdicts,
  suiteIdsFromVerdicts,
  verdictQueryFromFilters,
  type VerdictDto,
  type VerdictFilters,
} from "../lab/evidence-matrix";
import { LAYER_TEXT, verdictText } from "./compatibility-matrix-labels";
import {
  CompatibilityMatrixBoard,
  type CompatibilityBoardActions,
  type CompatibilityBoardInput,
  type CompatibilitySubview,
  type FilterOption,
} from "./compatibility-matrix-sections";
import {
  compatibilityPageNotices,
  filtersAreActive,
  labFailureMessage,
  pageHasMoreVerdicts,
  pageScopedExtra,
  scopedFailureMessage,
} from "./compatibility-matrix-view";
import { useVerdictDetail, type VerdictDetailState } from "./use-verdict-detail";

/** How often a visible board re-reads itself. */
const MATRIX_POLL_MS = 60_000;

/** A further-page read, tagged with the page it continues. */
type VerdictPageRead = {
  baseData: LabPageData;
  queryKey: string;
};

/** Rows read past the first page: the page they continue, and the Lab page they hold. */
type VerdictPageRows = VerdictPageRead & LabPage<VerdictDto>;

/** Why a further page did not arrive, and which page it belonged to. */
type VerdictPageFailure = VerdictPageRead & { message: string };

/** The rows read past the first page, and what the last attempt at them did. */
interface VerdictPagination {
  readonly rows: VerdictPageRows | null;
  readonly failure: VerdictPageFailure | null;
  readonly fetching: boolean;
}

const NO_PAGINATION: VerdictPagination = { rows: null, failure: null, fetching: false };

/** The cursor a further page starts from, when a page already points at the next one. */
function nextCursorOf(
  page: VerdictPageRows | null,
  listing: LabPageData | undefined,
): string | undefined {
  if (page !== null && page.hasMore) return page.nextCursor;
  return listing?.nextCursor;
}

export default function CompatibilityMatrix({
  apiBase,
  active = true,
  onCountChange,
  refreshNonce = 0,
}: {
  apiBase: string;
  active?: boolean;
  onCountChange?: (count: number | null) => void;
  refreshNonce?: number;
}) {
  const { t, locale } = useI18n();
  const [filters, setFilters] = useState<VerdictFilters>(UNFILTERED_VERDICT_FILTERS);
  const [subview, setSubview] = useState<CompatibilitySubview>("matrix");
  const [selected, setSelected] = useState<VerdictDto | null>(null);
  const [pagination, setPagination] = useState<VerdictPagination>(NO_PAGINATION);
  const moreRef = useRef<AbortController | null>(null);

  /*
   * The read is keyed by its query, so changing a filter starts a new page rather than reusing
   * the rows of the old one — and the further-page reads are scoped to that same key.
   */
  const queryKey = JSON.stringify(verdictQueryFromFilters(filters));
  const fetchPage = useCallback(
    (signal: AbortSignal) => fetchLabPageData(apiBase, verdictQueryFromFilters(filters), signal),
    [apiBase, filters],
  );
  const surface = useDataSurface<LabPageData>(
    `lab-matrix:${apiBase}:${queryKey}`,
    [apiBase, queryKey],
    fetchPage,
    {
      isEmpty: (page: LabPageData) => page.verdicts.length === 0,
      pollMs: MATRIX_POLL_MS,
      enabled: active,
      pauseWhenHidden: true,
    },
  );
  const detail = useVerdictDetail(apiBase, selected, active, t("lab.detailLoadFailed"));

  /* A new base page retires the rows that were read for the old one, in flight included. */
  useEffect(() => { moreRef.current?.abort(); }, [surface.data]);
  useEffect(() => () => { moreRef.current?.abort(); }, []);

  const clearDetail = useCallback(() => setSelected(null), []);

  const resetPagination = useCallback(() => {
    moreRef.current?.abort();
    moreRef.current = null;
    setPagination(NO_PAGINATION);
  }, []);

  const updateFilters = useCallback((updater: (current: VerdictFilters) => VerdictFilters) => {
    clearDetail();
    resetPagination();
    setFilters(updater);
  }, [clearDetail, resetPagination]);

  const changeSubview = useCallback((next: CompatibilitySubview) => {
    clearDetail();
    setSubview(next);
  }, [clearDetail]);

  const extra = pageScopedExtra(pagination.rows, surface.data, queryKey);
  const moreFailure = scopedFailureMessage(pagination.failure, surface.data, queryKey);

  const shown = useMemo(
    () => (surface.data === undefined ? [] : [...surface.data.verdicts, ...(extra?.rows ?? [])]),
    [extra, surface.data],
  );
  /*
   * The subject box narrows what is on screen; layer, verdict, and suite narrow the read itself,
   * which is why they are not applied a second time here.
   */
  const verdicts = useMemo(
    () => filterVerdicts(shown, { layer: "", verdict: "", subjectQuery: filters.subjectQuery, suiteId: "" }),
    [filters.subjectQuery, shown],
  );
  const matrixRows = useMemo(
    () => (surface.data === undefined ? [] : buildMatrixRows(verdicts, surface.data.subjects)),
    [surface.data, verdicts],
  );

  /* What the tab strip shows: the projection's own total, or what is on screen without one. */
  const reported = useMemo(() => {
    if (!active || surface.data?.status.projectionAvailable !== true) return null;
    const total = surface.data.status.verdictCount;
    return typeof total === "number"
      ? total
      : surface.data.verdicts.length + (extra?.rows.length ?? 0);
  }, [active, extra, surface.data]);
  useEffect(() => { onCountChange?.(reported); }, [onCountChange, reported]);

  const loadMore = useCallback(async () => {
    const baseData = surface.data;
    const cursor = nextCursorOf(extra, baseData);
    if (baseData === undefined || cursor === undefined || pagination.fetching) return;
    moreRef.current?.abort();
    const controller = new AbortController();
    moreRef.current = controller;
    const startedKey = queryKey;
    setPagination(current => ({ ...current, failure: null, fetching: true }));
    try {
      const page = await fetchMoreVerdicts(
        apiBase,
        verdictQueryFromFilters(filters),
        cursor,
        controller.signal,
      );
      if (controller.signal.aborted) return;
      setPagination(current => {
        const held = current.rows;
        const continuing = held !== null
          && held.baseData === baseData
          && held.queryKey === startedKey;
        const rows = [...(continuing && held.hasMore ? held.rows : []), ...page.rows];
        return {
          ...current,
          failure: null,
          rows: page.hasMore
            ? { baseData, queryKey: startedKey, rows, hasMore: true, nextCursor: page.nextCursor }
            : { baseData, queryKey: startedKey, rows, hasMore: false },
        };
      });
    } catch (error) {
      if (!controller.signal.aborted) {
        setPagination(current => ({
          ...current,
          failure: {
            baseData,
            queryKey: startedKey,
            message: labFailureMessage(error, t("lab.loadFailed")),
          },
        }));
      }
    } finally {
      if (moreRef.current === controller) {
        moreRef.current = null;
        setPagination(current => ({ ...current, fetching: false }));
      }
    }
  }, [apiBase, extra, filters, pagination.fetching, queryKey, surface.data, t]);

  const selectVerdict = useCallback((verdict: VerdictDto) => {
    if (active) setSelected(verdict);
  }, [active]);

  const refresh = surface.refresh;
  const refreshMatrix = useCallback(() => {
    clearDetail();
    resetPagination();
    refresh({ forceLoading: true });
  }, [clearDetail, refresh, resetPagination]);

  /* The page head's Refresh is a nonce, so a second press is a new refresh rather than a no-op. */
  const seenRefreshNonce = useRef(0);
  useEffect(() => {
    if (!active || refreshNonce <= 0 || refreshNonce === seenRefreshNonce.current) return;
    seenRefreshNonce.current = refreshNonce;
    refreshMatrix();
  }, [active, refreshMatrix, refreshNonce]);

  return (
    <CompatibilityMatrixFrame
      t={t}
      locale={locale}
      refreshing={surface.refreshing}
      showSkeleton={surface.state.showSkeleton}
      surfaceKind={surface.state.kind}
      surfaceError={surface.error}
      data={surface.data}
      filters={filters}
      verdicts={verdicts}
      rows={matrixRows}
      extra={extra}
      moreFailure={moreFailure}
      fetchingMore={pagination.fetching}
      subview={subview}
      selection={selected}
      detail={detail}
      onUpdateFilters={updateFilters}
      onSelectVerdict={selectVerdict}
      onLoadMore={loadMore}
      onCloseDetail={clearDetail}
      onSubviewChange={changeSubview}
    />
  );
}

/**
 * The board, once the read has settled enough to describe it.
 *
 * A cold read owns the area as a skeleton; everything else the board draws from its props. The
 * notices are this page's mapping of the read's kind — never a second reading of the same
 * failure.
 */

/**
 * The board, once the read has settled enough to describe it.
 *
 * A cold read owns the area as a skeleton; everything else the board draws from the inputs
 * below. The notices are this page's mapping of the read's kind — never a second reading of the
 * same failure — which is why the mapping happens here and only the strings travel on.
 */
function CompatibilityMatrixFrame({
  t,
  locale,
  refreshing,
  showSkeleton,
  surfaceKind,
  surfaceError,
  data,
  filters,
  verdicts,
  rows,
  extra,
  moreFailure,
  fetchingMore,
  subview,
  selection,
  detail,
  onUpdateFilters,
  onSelectVerdict,
  onLoadMore,
  onCloseDetail,
  onSubviewChange,
}: {
  t: ReturnType<typeof useI18n>["t"];
  locale: Locale;
  refreshing: boolean;
  showSkeleton: boolean;
  surfaceKind: string;
  surfaceError: unknown;
  data: LabPageData | undefined;
  filters: VerdictFilters;
  verdicts: VerdictDto[];
  rows: ReturnType<typeof buildMatrixRows>;
  extra: VerdictPageRows | null;
  moreFailure: string | null;
  fetchingMore: boolean;
  subview: CompatibilitySubview;
  selection: VerdictDto | null;
  detail: VerdictDetailState;
  onUpdateFilters: (updater: (current: VerdictFilters) => VerdictFilters) => void;
  onSelectVerdict: (verdict: VerdictDto) => void;
  onLoadMore: () => Promise<void>;
  onCloseDetail: () => void;
  onSubviewChange: (next: CompatibilitySubview) => void;
}) {
  if (showSkeleton) return <DataSurfaceSkeleton label={t("lab.loading")} rows={5} />;
  const notices = compatibilityPageNotices({
    surfaceKind,
    surfaceError,
    loadFailedLabel: t("lab.loadFailed"),
    refreshFailedStaleLabel: t("lab.refreshFailedStale"),
  });
  const status = data?.status;
  /* A suite the reader typed stays selectable even before anything loaded under it. */
  const suites = new Set(suiteIdsFromVerdicts(verdicts));
  const typedSuite = filters.suiteId.trim();
  if (typedSuite !== "") suites.add(typedSuite);
  const filtered: FilterOption[] = [
    { value: "", label: t("lab.filter.all") },
    ...[...suites].sort((left, right) => left.localeCompare(right)).map(suiteId => ({
      value: suiteId,
      label: suiteId,
    })),
  ];
  const input: CompatibilityBoardInput = {
    t,
    locale,
    projection: {
      refreshing,
      showSkeleton,
      unavailable: Boolean(status && !status.projectionAvailable),
      incompatible: status?.projectionIncompatible === true,
    },
    notices: {
      loadError: notices.loadError,
      staleRefreshWarning: notices.staleRefreshWarning,
    },
    filters,
    filterOptions: {
      narrowed: filtersAreActive(filters),
      layers: [
        { value: "", label: t("lab.filter.all") },
        ...PRODUCT_EVIDENCE_LAYERS.map(layer => ({ value: layer, label: t(LAYER_TEXT[layer].name) })),
      ],
      verdicts: [
        { value: "", label: t("lab.filter.all") },
        ...COMPATIBILITY_VERDICTS.map(verdict => ({ value: verdict, label: t(verdictText(verdict)) })),
      ],
      suites: filtered,
    },
    matrix: {
      rows,
      verdictCount: typeof status?.verdictCount === "number" ? status.verdictCount : verdicts.length,
    },
    records: {
      verdicts,
      total: typeof status?.verdictCount === "number" ? status.verdictCount : verdicts.length,
      hasMore: pageHasMoreVerdicts(extra, data?.hasMore),
      loadingMore: fetchingMore,
      moreError: moreFailure,
    },
    detail: { verdict: selection, data: detail.detail, loading: detail.loading, error: detail.error },
    subview,
    projectionData: data,
  };
  const actions: CompatibilityBoardActions = {
    changeFilters: onUpdateFilters,
    selectVerdict: onSelectVerdict,
    loadMore: () => { void onLoadMore(); },
    closeDetail: onCloseDetail,
    changeSubview: onSubviewChange,
  };
  return <CompatibilityMatrixBoard input={input} actions={actions} />;
}
