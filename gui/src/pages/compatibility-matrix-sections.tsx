/** Benes dashboard source. The Compatibility board, as the matrix page renders it.
 *
 * The board takes its inputs grouped by the part of the board they describe — the projection's
 * own state, the notices, the filter bar, the matrix, the records, the detail — rather than as
 * one flat list of forty props, so a section cannot read a value that belongs to another one.
 * It renders what the page resolved and decides nothing about the Lab's evidence.
 *
 * The DOM is the product here: classes, headings, and the column order are the surface other
 * work binds to, so the rows and panes below are derived data rendered into that same markup.
 */
import { useEffect, useRef, type KeyboardEvent } from "react";
import { DataSurfaceStatus } from "../components/data-surface";
import { IconX } from "../icons";
import { labSupplement } from "../i18n/lab";
import type { Locale, TFn, TKey } from "../i18n/shared";
import {
  PRODUCT_EVIDENCE_LAYERS,
  formatAsOf,
  formatBuiltAt,
  matrixRowKey,
  preferredMatrixVerdict,
  shortSubjectId,
  type CompatibilityVerdict,
  type EvidenceLayer,
  type MatrixRow,
  type ProductEvidenceLayer,
  type VerdictDto,
  type VerdictFilters,
} from "../lab/evidence-matrix";
import type { LabPageData, VerdictDetailData } from "../lab/lab-client";
import { LAYER_TEXT, verdictText } from "./compatibility-matrix-labels";
import { EmptyState, Notice, Select } from "../ui";

export type CompatibilitySubview = "matrix" | "records";

const COMPATIBILITY_SUBVIEWS: Array<{ id: CompatibilitySubview; labelKey: TKey }> = [
  { id: "matrix", labelKey: "lab.tab.matrix" },
  { id: "records", labelKey: "lab.tab.records" },
];

/** One option in the filter bar. */
export interface FilterOption {
  readonly value: string;
  readonly label: string;
}

/** The board's inputs, grouped by the part of the board they describe. */
export interface CompatibilityBoardInput {
  readonly t: TFn;
  readonly locale: Locale;
  /** The projection read itself: how it is going, and whether there is one to draw. */
  readonly projection: {
    readonly refreshing: boolean;
    readonly showSkeleton: boolean;
    readonly unavailable: boolean;
    readonly incompatible: boolean;
  };
  /** What the read's outcome means, already mapped from its kind. */
  readonly notices: {
    readonly loadError: string | null;
    readonly staleRefreshWarning: string | null;
  };
  /** The filters the bar currently holds. */
  readonly filters: VerdictFilters;
  /** The options the bar offers, and whether anything is narrowing the board. */
  readonly filterOptions: {
    readonly narrowed: boolean;
    readonly layers: readonly FilterOption[];
    readonly verdicts: readonly FilterOption[];
    readonly suites: readonly FilterOption[];
  };
  readonly matrix: {
    readonly rows: readonly MatrixRow[];
    readonly verdictCount: number;
  };
  readonly records: {
    readonly verdicts: readonly VerdictDto[];
    readonly total: number;
    readonly hasMore: boolean;
    readonly loadingMore: boolean;
    readonly moreError: string | null;
  };
  readonly detail: {
    readonly verdict: VerdictDto | null;
    readonly data: VerdictDetailData | null;
    readonly loading: boolean;
    readonly error: string | null;
  };
  readonly subview: CompatibilitySubview;
  readonly projectionData: LabPageData | undefined;
}

/** What the board asks the page to do. */
export interface CompatibilityBoardActions {
  readonly changeFilters: (updater: (current: VerdictFilters) => VerdictFilters) => void;
  readonly selectVerdict: (verdict: VerdictDto) => void;
  readonly loadMore: () => void;
  readonly closeDetail: () => void;
  readonly changeSubview: (next: CompatibilitySubview) => void;
}

/** One verdict as it is drawn, wherever it is drawn. */
interface VerdictLine {
  readonly label: string;
  readonly verdict: CompatibilityVerdict;
  readonly className: string;
  readonly selected: boolean;
}

function verdictLine(
  verdict: CompatibilityVerdict,
  t: TFn,
  selected = false,
): VerdictLine {
  return {
    verdict,
    label: t(verdictText(verdict)),
    className: `lab-verdict-line${selected ? " lab-verdict-line--selected" : ""}`,
    selected,
  };
}

/**
 * One verdict, as a line: a dot, then the verdict's own name.
 *
 * A line that can open the verdict beside the board is a button, so the whole row is reachable
 * by keyboard and the row's own click does not fire twice.
 */
function VerdictStatus({
  line,
  onSelect,
}: {
  line: VerdictLine;
  onSelect?: () => void;
}) {
  if (onSelect === undefined) {
    return (
      <span className={line.className} data-verdict={line.verdict} title={line.label}>
        <span className="lab-verdict-dot" aria-hidden="true" />
        <span className="lab-verdict-status">{line.label}</span>
      </span>
    );
  }
  return (
    <button
      type="button"
      className={line.className}
      data-verdict={line.verdict}
      title={line.label}
      aria-label={line.label}
      aria-pressed={line.selected}
      onClick={event => {
        event.stopPropagation();
        onSelect();
      }}
    >
      <span className="lab-verdict-dot" aria-hidden="true" />
      <span className="lab-verdict-status">{line.label}</span>
    </button>
  );
}

/** The projection's own line: how many subjects and verdicts it carries, and when it was built. */
function ProjectionSummary({ data, t, locale }: { data: LabPageData; t: TFn; locale: Locale }) {
  const { status } = data;
  return (
    <div className="lab-projection-summary" aria-label={t("lab.statusTitle")}>
      <span className="lab-projection-counts">
        {t("lab.summary.counts", {
          subjects: status.subjectCount ?? 0,
          verdicts: status.verdictCount ?? 0,
        })}
      </span>
      {status.builtAtMs ? (
        <span className="lab-projection-built">
          {t("lab.builtAt")} {formatBuiltAt(status.builtAtMs, locale)}
        </span>
      ) : null}
    </div>
  );
}
/** One evidence observation, as the pane lists it. */
interface ObservationRow {
  readonly id: string;
  readonly scenario: string;
  readonly outcome: string;
  readonly at: string;
}

/** One evidence event, as the pane lists it. */
interface EventRow {
  readonly id: string;
  readonly short: string;
  readonly kind: string;
}

/** One production signal, as the pane lists it. */
interface SignalRow {
  readonly label: string;
  readonly value: string;
  /** Printed as machine text, and given the same text as a hover title. */
  readonly mono?: boolean;
  readonly title?: string;
}

/** Every event the verdict's own digests point at, whether or not the pane holds it. */
function expectedEventCount(verdict: VerdictDto): number {
  return new Set([...verdict.contributingEventIds, ...verdict.contradictingEventIds]).size;
}

/**
 * The evidence the pane shows for one verdict.
 *
 * The rows are resolved here — including the timestamps, which the pane only prints — so the
 * markup below is a list of facts rather than a chain of reads into the Lab's payload.
 */
function DetailEvidence({
  detail,
  expected,
  t,
  locale,
}: {
  detail: VerdictDetailData;
  expected: number;
  t: TFn;
  locale: Locale;
}) {
  const observations: ObservationRow[] = detail.observations.map(observation => ({
    id: observation.eventId,
    scenario: observation.scenarioId,
    outcome: observation.outcome,
    at: formatAsOf(observation.completedAt, locale),
  }));
  const events: EventRow[] = detail.events.map(event => ({
    id: event.eventId,
    short: shortSubjectId(event.eventId),
    kind: event.eventKind,
  }));
  const showEvents = events.length > 0 || expected > 0;
  const showEvidence = observations.length > 0 || showEvents;
  if (!showEvidence) return null;
  return (
    <section className="lab-detail-section">
      <h4>{t("lab.detailEvidence")}</h4>
      {observations.length > 0 && (
        <ul className="lab-detail-list">
          {observations.map(row => (
            <li key={row.id}>
              <span className="mono" title={row.scenario}>{row.scenario}</span>
              <span>{row.outcome}</span>
              <span className="muted">{row.at}</span>
            </li>
          ))}
        </ul>
      )}
      {showEvents && (
        <>
          <h4>
            {t("lab.detailEvents")}
            {events.length < expected ? ` (${events.length}/${expected})` : ""}
          </h4>
          <ul className="lab-detail-list">
            {events.map(row => (
              <li key={row.id}>
                <span className="mono" title={row.id}>{row.short}</span>
                <span>{row.kind}</span>
              </li>
            ))}
          </ul>
        </>
      )}
    </section>
  );
}

/**
 * The production signals the proxy observed for this verdict's subject.
 *
 * They are deliberately not evidence: the section says so, because a route that carried traffic
 * yesterday is not a conformance result.
 */
function DetailProduction({
  production,
  t,
  locale,
}: {
  production: NonNullable<VerdictDetailData["production"]>;
  t: TFn;
  locale: Locale;
}) {
  const { summary } = production;
  const rows: SignalRow[] = [
    { label: t("lab.production.attempts"), value: String(summary.recentProductionAttempts) },
    { label: t("lab.production.successes"), value: String(summary.recentSuccessfulAttempts) },
    { label: t("lab.production.routeErrors"), value: String(summary.recentRouteErrorSignals) },
  ];
  if (summary.lastObservedProductionAttempt !== undefined) {
    rows.push({
      label: t("lab.production.lastObserved"),
      value: formatAsOf(summary.lastObservedProductionAttempt, locale),
    });
  }
  return (
    <section className="lab-detail-section" data-testid="lab-production-signals">
      <h4>{t("lab.production.title")}</h4>
      <p className="muted">{t("lab.production.notVerification")}</p>
      <dl className="lab-detail-meta">
        {rows.map(row => (
          <div key={row.label}>
            <dt>{row.label}</dt>
            <dd>{row.value}</dd>
          </div>
        ))}
      </dl>
    </section>
  );
}

/** The notes the projection attached to this verdict. */
function DetailNotes({ notes, t }: { notes: readonly string[]; t: TFn }) {
  if (notes.length === 0) return null;
  return (
    <section className="lab-detail-section">
      <h4>{t("lab.detailNotes")}</h4>
      <ul className="lab-detail-list">
        {notes.map(note => <li key={note}>{note}</li>)}
      </ul>
    </section>
  );
}
/**
 * One verdict's detail, beside the board.
 *
 * Escape closes it and focus moves into it when it opens, so a reader who opened it with the
 * keyboard can read it and leave it without reaching for the pointer.
 */
function DetailPane({
  verdict,
  detail,
  loading,
  error,
  t,
  locale,
  onClose,
}: {
  verdict: VerdictDto;
  detail: VerdictDetailData | null;
  loading: boolean;
  error: string | null;
  t: TFn;
  locale: Locale;
  onClose: () => void;
}) {
  const paneRef = useRef<HTMLElement | null>(null);
  useEffect(() => {
    const onKey = (event: globalThis.KeyboardEvent) => {
      if (event.key === "Escape") {
        event.preventDefault();
        onClose();
      }
    };
    window.addEventListener("keydown", onKey);
    paneRef.current?.focus({ preventScroll: true });
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const expected = expectedEventCount(verdict);
  return (
    <aside ref={paneRef} className="lab-detail-pane" aria-label={t("lab.detailTitle")} tabIndex={-1}>
      <div className="lab-detail-head">
        <h3 className="mono" title={verdict.subjectId}>{verdict.subjectId}</h3>
        <button
          type="button"
          className="btn btn-ghost btn-icon btn-sm"
          onClick={onClose}
          aria-label={t("lab.detailClose")}
        >
          <IconX />
        </button>
      </div>

      <div className="lab-detail-status">
        <VerdictStatus line={verdictLine(verdict.verdict, t)} />
        <div className="lab-detail-status-layer muted">
          {t(LAYER_TEXT[verdict.evidenceLayer].name)}
        </div>
      </div>

      <dl className="lab-detail-meta lab-detail-meta--compact">
        <div>
          <dt>{t("lab.col.layer")}</dt>
          <dd>{t(LAYER_TEXT[verdict.evidenceLayer].name)}</dd>
        </div>
        <div>
          <dt>{t("lab.col.suite")}</dt>
          <dd className="mono" title={verdict.suiteId}>{verdict.suiteId}</dd>
        </div>
        <div>
          <dt>{t("lab.col.suiteVersion")}</dt>
          <dd className="mono">{verdict.suiteVersion || "-"}</dd>
        </div>
        <div>
          <dt>{t("lab.col.asOf")}</dt>
          <dd>{formatAsOf(verdict.asOf, locale)}</dd>
        </div>
      </dl>

      {loading && <DataSurfaceStatus busy>{t("common.loading")}</DataSurfaceStatus>}
      {error && <Notice tone="err">{error}</Notice>}
      {detail && (
        <>
          <DetailEvidence detail={detail} expected={expected} t={t} locale={locale} />
          {detail.production && <DetailProduction production={detail.production} t={t} locale={locale} />}
          <DetailNotes notes={verdict.notes} t={t} />
        </>
      )}
    </aside>
  );
}
/** What the projection read left to say, in the order it earns attention. */
function CompatibilityMatrixNotices({
  t,
  projection,
  notices,
}: {
  t: TFn;
  projection: CompatibilityBoardInput["projection"];
  notices: CompatibilityBoardInput["notices"];
}) {
  const { refreshing, showSkeleton, unavailable, incompatible } = projection;
  const { loadError, staleRefreshWarning } = notices;
  return (
    <>
      {refreshing && !showSkeleton && (
        <DataSurfaceStatus busy live={!loadError}>{t("common.loading")}</DataSurfaceStatus>
      )}
      {loadError && <Notice tone="err" role="alert">{loadError}</Notice>}
      {staleRefreshWarning && !loadError && <Notice tone="warn" role="status">{staleRefreshWarning}</Notice>}
      {incompatible && <Notice tone="err" role="alert">{t("lab.projectionIncompatible")}</Notice>}
      {unavailable && !incompatible && <EmptyState title={t("lab.projectionUnavailable")} />}
    </>
  );
}

/** One dropdown in the filter bar; the three differ only in what they narrow. */
function FilterSelect({
  id,
  labelKey,
  value,
  options,
  onPick,
  t,
}: {
  id: string;
  labelKey: TKey;
  value: string;
  options: readonly FilterOption[];
  onPick: (value: string) => void;
  t: TFn;
}) {
  return (
    <div className={`lab-filter-field lab-filter-field--${id.replace("lab-filter-", "")}`}>
      <label htmlFor={id}>{t(labelKey)}</label>
      <Select
        id={id}
        value={value}
        options={[...options]}
        onChange={onPick}
        label={t(labelKey)}
        portal={false}
        matchParent
        chevron="down"
        style={{ width: "100%", display: "block" }}
      />
    </div>
  );
}

/** The filter bar: the subject box narrows on screen, the three dropdowns narrow the read. */
function MatrixFilters({
  t,
  filters,
  filterOptions,
  changeFilters,
}: {
  t: TFn;
  filters: VerdictFilters;
  filterOptions: CompatibilityBoardInput["filterOptions"];
  changeFilters: CompatibilityBoardActions["changeFilters"];
}) {
  const { layers, verdicts, suites } = filterOptions;
  return (
    <div className="lab-filters" role="search" aria-label={t("lab.filter.bar")}>
      <div className="lab-filter-field lab-filter-field--grow">
        <label htmlFor="lab-filter-subject">{t("lab.filter.subject")}</label>
        <input
          id="lab-filter-subject"
          className="input"
          type="search"
          value={filters.subjectQuery}
          placeholder={t("lab.filter.subjectPlaceholder")}
          aria-label={t("lab.filter.subject")}
          onChange={event => changeFilters(current => ({ ...current, subjectQuery: event.target.value }))}
        />
      </div>
      <FilterSelect
        id="lab-filter-layer"
        labelKey="lab.filter.layer"
        value={filters.layer}
        options={layers}
        onPick={layer => changeFilters(current => ({ ...current, layer: layer as EvidenceLayer | "" }))}
        t={t}
      />
      <FilterSelect
        id="lab-filter-verdict"
        labelKey="lab.filter.verdict"
        value={filters.verdict}
        options={verdicts}
        onPick={verdict => changeFilters(current => ({ ...current, verdict: verdict as CompatibilityVerdict | "" }))}
        t={t}
      />
      <FilterSelect
        id="lab-filter-suite"
        labelKey="lab.filter.suite"
        value={filters.suiteId}
        options={suites}
        onPick={suiteId => changeFilters(current => ({ ...current, suiteId }))}
        t={t}
      />
    </div>
  );
}

/** The Matrix | Records tabs, moved with the arrow keys the tabs pattern gives them. */
function CompatibilitySubviewTabs({
  t,
  subview,
  changeSubview,
}: {
  t: TFn;
  subview: CompatibilitySubview;
  changeSubview: CompatibilityBoardActions["changeSubview"];
}) {
  const step = (event: KeyboardEvent<HTMLButtonElement>, id: CompatibilitySubview) => {
    const index = COMPATIBILITY_SUBVIEWS.findIndex(tab => tab.id === id);
    const last = COMPATIBILITY_SUBVIEWS.length - 1;
    const move = (() => {
      if (event.key === "ArrowLeft") return COMPATIBILITY_SUBVIEWS[(index - 1 + last + 1) % (last + 1)];
      if (event.key === "ArrowRight") return COMPATIBILITY_SUBVIEWS[(index + 1) % (last + 1)];
      if (event.key === "Home") return COMPATIBILITY_SUBVIEWS[0];
      if (event.key === "End") return COMPATIBILITY_SUBVIEWS[last];
      return null;
    })();
    if (move === null) return;
    event.preventDefault();
    changeSubview(move.id);
  };
  return (
    <div className="page-tabs lab-subview-tabs" role="tablist" aria-label={t("lab.subviewsLabel")}>
      {COMPATIBILITY_SUBVIEWS.map(tab => {
        const selected = tab.id === subview;
        return (
          <button
            key={tab.id}
            type="button"
            role="tab"
            id={`lab-subview-${tab.id}`}
            aria-selected={selected}
            aria-controls={`lab-subview-panel-${tab.id}`}
            tabIndex={selected ? 0 : -1}
            className={selected ? "page-tab page-tab--active" : "page-tab"}
            onClick={() => changeSubview(tab.id)}
            onKeyDown={event => step(event, tab.id)}
          >
            {t(tab.labelKey)}
          </button>
        );
      })}
    </div>
  );
}
/** One matrix row, as the grid draws it. */
interface MatrixGridRow {
  readonly key: string;
  readonly subjectId: string;
  /** The label to print, or `undefined` when the row continues the subject above it. */
  readonly subject?: string;
  readonly suiteId: string;
  readonly selected: boolean;
  /** The verdict the row opens when it is clicked, or `null` when it holds none. */
  readonly opens: VerdictDto | null;
  readonly cells: ReadonlyArray<{
    readonly layer: ProductEvidenceLayer;
    readonly cell: VerdictDto | null;
    readonly selected: boolean;
  }>;
}

/**
 * The grid, one row per Subject+Suite, with the selection the pane is showing.
 *
 * A row is selected when the open verdict is one of its own — its subject and suite, not merely
 * its key — so the highlight follows the pane rather than whichever verdict was clicked last.
 */
function matrixGrid(rows: readonly MatrixRow[], open: VerdictDto | null): MatrixGridRow[] {
  return rows.map(row => {
    const selected = open !== null
      && open.subjectId === row.subjectId
      && open.suiteId === row.suiteId;
    return {
      key: matrixRowKey(row.subjectId, row.suiteId),
      subjectId: row.subjectId,
      suiteId: row.suiteId,
      selected,
      opens: preferredMatrixVerdict(row),
      ...(row.showSubject ? { subject: shortSubjectId(row.subjectId) } : {}),
      cells: PRODUCT_EVIDENCE_LAYERS.map(layer => ({
        layer,
        cell: row.byLayer[layer] ?? null,
        selected: open !== null && open.projectionKey === row.byLayer[layer]?.projectionKey,
      })),
    };
  });
}

function CompatibilityMatrixGrid({
  t,
  matrix,
  selection,
  selectVerdict,
}: {
  t: TFn;
  matrix: CompatibilityBoardInput["matrix"];
  selection: VerdictDto | null;
  selectVerdict: CompatibilityBoardActions["selectVerdict"];
}) {
  return (
    <div
      className="lab-matrix-block lab-matrix-block--bare"
      id="lab-subview-panel-matrix"
      role="tabpanel"
      aria-labelledby="lab-subview-matrix"
    >
      <div className="lab-matrix-scroll">
        <table className="lab-matrix">
          <thead>
            <tr>
              <th>{t("lab.col.subject")}</th>
              <th>{t("lab.col.suite")}</th>
              {PRODUCT_EVIDENCE_LAYERS.map(layer => (
                <th key={layer}>{t(LAYER_TEXT[layer].column)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {matrixGrid(matrix.rows, selection).map(row => {
              const open = () => { if (row.opens !== null) selectVerdict(row.opens); };
              return (
                <tr key={row.key} className={row.selected ? "selected" : undefined}>
                  <td className="subject" title={row.subjectId}>
                    {row.subject === undefined ? null : (
                      <button
                        type="button"
                        className="lab-row-select"
                        aria-pressed={row.selected}
                        onClick={open}
                      >
                        {row.subject}
                      </button>
                    )}
                  </td>
                  <td className="mono suite" title={row.suiteId}>
                    <button
                      type="button"
                      className="lab-row-select"
                      aria-pressed={row.selected}
                      aria-label={row.suiteId}
                      onClick={open}
                    >
                      {row.suiteId}
                    </button>
                  </td>
                  {row.cells.map(cell => (
                    <td key={cell.layer}>
                      {cell.cell === null ? (
                        <span className="muted">-</span>
                      ) : (
                        <VerdictStatus
                          line={verdictLine(cell.cell.verdict, t, cell.selected)}
                          onSelect={() => { if (cell.cell !== null) selectVerdict(cell.cell); }}
                        />
                      )}
                    </td>
                  ))}
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

/** One verdict record, as the records table draws it. */
interface RecordRow {
  readonly key: string;
  readonly subjectId: string;
  readonly subject: string;
  readonly selectLabel: string;
  readonly layer: string;
  readonly suiteId: string;
  readonly verdict: CompatibilityVerdict;
  readonly asOf: string;
  readonly selected: boolean;
}

function recordRows(
  verdicts: readonly VerdictDto[],
  open: VerdictDto | null,
  locale: Locale,
  t: TFn,
): RecordRow[] {
  return verdicts.map(verdict => {
    const layer = verdict.evidenceLayer;
    return {
      key: verdict.projectionKey,
      subjectId: verdict.subjectId,
      subject: shortSubjectId(verdict.subjectId),
      selectLabel: labSupplement(locale, "selectVerdict", {
        subject: shortSubjectId(verdict.subjectId),
      }),
      layer: t(layer === "task_effectiveness" ? LAYER_TEXT[layer].name : LAYER_TEXT[layer].column),
      suiteId: verdict.suiteId,
      verdict: verdict.verdict,
      asOf: formatAsOf(verdict.asOf, locale),
      selected: open !== null && open.projectionKey === verdict.projectionKey,
    };
  });
}

function CompatibilityMatrixVerdictList({
  t,
  locale,
  records,
  selection,
  selectVerdict,
}: {
  t: TFn;
  locale: Locale;
  records: CompatibilityBoardInput["records"];
  selection: VerdictDto | null;
  selectVerdict: CompatibilityBoardActions["selectVerdict"];
}) {
  return (
    <div
      className="lab-matrix-block lab-matrix-block--bare"
      id="lab-subview-panel-records"
      role="tabpanel"
      aria-labelledby="lab-subview-records"
    >
      <div className="lab-matrix-scroll">
        <table className="lab-detail-table">
          <thead>
            <tr>
              <th>{t("lab.col.subject")}</th>
              <th>{t("lab.col.layer")}</th>
              <th>{t("lab.col.suite")}</th>
              <th>{t("lab.col.verdict")}</th>
              <th>{t("lab.col.asOf")}</th>
            </tr>
          </thead>
          <tbody>
            {recordRows(records.verdicts, selection, locale, t).map(row => (
              <tr key={row.key} className={row.selected ? "selected" : undefined}>
                <td className="mono subject" title={row.subjectId}>
                  <button
                    type="button"
                    className="lab-row-select"
                    data-verdict-detail={row.key}
                    aria-pressed={row.selected}
                    aria-label={row.selectLabel}
                    onClick={event => {
                      event.stopPropagation();
                      const verdict = records.verdicts.find(entry => entry.projectionKey === row.key);
                      if (verdict !== undefined) selectVerdict(verdict);
                    }}
                  >
                    {row.subject}
                  </button>
                </td>
                <td className="layer">{row.layer}</td>
                <td className="mono suite" title={row.suiteId}>{row.suiteId}</td>
                <td>
                  <VerdictStatus line={verdictLine(row.verdict, t)} />
                </td>
                <td className="as-of">{row.asOf}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}
/** The records footer: how much of the list is on screen, and how to reach the rest. */
function CompatibilityMatrixLoadMore({
  t,
  records,
  loadMore,
}: {
  t: TFn;
  records: CompatibilityBoardInput["records"];
  loadMore: CompatibilityBoardActions["loadMore"];
}) {
  const shown = records.verdicts.length;
  const total = Math.max(records.total, shown);
  return (
    <>
      {records.moreError && <Notice tone="err">{records.moreError}</Notice>}
      <div className="lab-records-footer">
        <span className="lab-records-showing muted">
          {t("lab.showing.counts", { shown, total })}
        </span>
        {records.hasMore ? (
          <button
            type="button"
            className="btn btn-ghost"
            disabled={records.loadingMore}
            onClick={loadMore}
          >
            {records.loadingMore ? t("common.loading") : t("lab.loadMore")}
          </button>
        ) : null}
      </div>
    </>
  );
}

/** The board once there is a projection to draw. */
function CompatibilityMatrixLoaded({
  input,
  actions,
}: {
  input: CompatibilityBoardInput;
  actions: CompatibilityBoardActions;
}) {
  const { t, locale, projectionData, filters, matrix, records, detail, subview } = input;
  if (projectionData === undefined) return null;
  const filtersActive = input.filterOptions.narrowed;
  const open = detail.verdict;
  const empty = matrix.rows.length === 0 && records.verdicts.length === 0;
  return (
    <div className={`lab-layout${open === null ? "" : " lab-layout--with-detail"}`}>
      <div className="lab-main">
        <ProjectionSummary data={projectionData} t={t} locale={locale} />
        <MatrixFilters
          t={t}
          filters={filters}
          filterOptions={input.filterOptions}
          changeFilters={actions.changeFilters}
        />
        <CompatibilitySubviewTabs t={t} subview={subview} changeSubview={actions.changeSubview} />
        {empty ? (
          <EmptyState title={filtersActive ? t("lab.emptyFiltered") : t("lab.empty")} />
        ) : (
          <>
            {subview === "matrix" ? (
              <CompatibilityMatrixGrid
                t={t}
                matrix={matrix}
                selection={open}
                selectVerdict={actions.selectVerdict}
              />
            ) : null}
            {subview === "records" ? (
              <>
                <CompatibilityMatrixVerdictList
                  t={t}
                  locale={locale}
                  records={records}
                  selection={open}
                  selectVerdict={actions.selectVerdict}
                />
                <CompatibilityMatrixLoadMore t={t} records={records} loadMore={actions.loadMore} />
              </>
            ) : null}
          </>
        )}
      </div>
      {open !== null && (
        <DetailPane
          verdict={open}
          detail={detail.data}
          loading={detail.loading}
          error={detail.error}
          t={t}
          locale={locale}
          onClose={actions.closeDetail}
        />
      )}
    </div>
  );
}

export function CompatibilityMatrixBoard({
  input,
  actions,
}: {
  input: CompatibilityBoardInput;
  actions: CompatibilityBoardActions;
}) {
  const { t, projection, projectionData } = input;
  return (
    <div className="lab-page">
      <CompatibilityMatrixNotices t={t} projection={projection} notices={input.notices} />
      {projectionData !== undefined
        && projectionData.status.projectionAvailable
        && !projection.incompatible && <CompatibilityMatrixLoaded input={input} actions={actions} />}
    </div>
  );
}
