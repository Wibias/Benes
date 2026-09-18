import { useMemo, useState } from "react";
import { formatTokens } from "../format-tokens";
import { formatProviderDisplayName } from "../provider-icons";
import { modelLabel } from "../model-display";
import type { Locale, TFn } from "../i18n/shared";
import {
  COST_EMPTY_NO_METER,
  COST_EMPTY_NO_PRICE,
  COST_EMPTY_PROXY,
  presentCost,
  formatPresentedAmount,
  formatUsageUsd,
  costChartState,
  type CostChartState,
} from "./usage-cost";
import {
  bucketDailyStacks,
  costTooltipHead,
  dailyCostStacks,
  sparseAxisLabels,
  topModelSeries,
  type DayCostMeta,
  type DayStack,
} from "./usage-series";
import {
  cacheReadShare,
  chartAxisTicks,
  formatAxisDate,
  formatPct1,
  formatWindowRange,
  historyDisplay,
  uncoveredRequests,
} from "./usage-format";
import type { UsageResponse } from "./usage-contract";

function count(n: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(n);
}

function listPriceCopy(
  summary: UsageResponse["cost"],
  requestCount: number,
  locale: string,
  t: TFn,
): { primary: string; secondary: string } {
  const state = costChartState(summary, requestCount);
  if (state.kind === "empty") {
    if (state.source === COST_EMPTY_NO_PRICE) {
      return { primary: t("usage.cost.unavailable"), secondary: t("usage.chart.costUnpriced", { count: count(state.count, locale) }) };
    }
    if (state.source === COST_EMPTY_NO_METER) {
      return { primary: t("usage.cost.unavailable"), secondary: t("usage.chart.costUnmetered", { count: count(state.count, locale) }) };
    }
    return { primary: t("usage.cost.unavailable"), secondary: t("usage.chart.costProxyMissing") };
  }
  if (state.kind === "partial") {
    return { primary: t("usage.cost.partial"), secondary: t("usage.cost.partialNote", { count: count(state.gapRequests, locale) }) };
  }
  const cost = presentCost(summary);
  if (cost.kind === "amount") {
    const secondary = cost.status === "exact" ? t("usage.cost.status.exact")
      : cost.status === "estimated" ? t("usage.cost.status.estimated")
        : cost.status === "lower_bound" ? t("usage.cost.status.lower_bound")
          : t("usage.cost.status.stale");
    return { primary: formatPresentedAmount(cost, locale), secondary };
  }
  if (cost.kind === "mixed") return { primary: t("usage.cost.mixed"), secondary: t("usage.cost.mixedNote") };
  return { primary: "\u2014", secondary: t("usage.cost.unavailable") };
}

function costClassTooltipLines(cost: DayCostMeta, locale: string, t: TFn): Array<{ id: string; text: string }> {
  const rows: Array<{ id: string; amount: number; prefix: "" | "≈" | "≥"; label: "usage.cost.status.exact" | "usage.cost.status.estimated" | "usage.cost.status.lower_bound" | "usage.cost.status.stale" }> = [
    { id: "exact", amount: cost.exactAmount, prefix: "", label: "usage.cost.status.exact" },
    { id: "estimated", amount: cost.estimatedAmount, prefix: "≈", label: "usage.cost.status.estimated" },
    { id: "lower_bound", amount: cost.lowerBoundAmount, prefix: "≥", label: "usage.cost.status.lower_bound" },
    { id: "stale", amount: cost.staleAmount, prefix: "", label: "usage.cost.status.stale" },
  ];
  return rows.filter(row => row.amount > 0).map(row => ({
    id: row.id,
    text: `${t(row.label)} ${row.prefix}${formatUsageUsd(row.amount, locale)}`,
  }));
}

function CostEmptyCopy({
  state, locale, t,
}: {
  state: Extract<CostChartState, { kind: "empty" }>;
  locale: string;
  t: TFn;
}) {
  return (
    <div className="usage-chart-empty">
      <div>{t("usage.cost.unavailable")}</div>
      {state.source === COST_EMPTY_NO_PRICE && (
        <p className="muted">{t("usage.chart.costUnpriced", { count: count(state.count, locale) })}</p>
      )}
      {state.source === COST_EMPTY_NO_METER && (
        <p className="muted">{t("usage.chart.costUnmetered", { count: count(state.count, locale) })}</p>
      )}
      {state.source === COST_EMPTY_PROXY && (
        <p className="muted">{t("usage.chart.costProxyMissing")}</p>
      )}
      {state.showConfigureHint && <p className="muted">{t("usage.chart.costConfigure")}</p>}
    </div>
  );
}

function CostHoverBody({ cost, locale, t }: { cost: DayCostMeta; locale: string; t: TFn }) {
  const head = costTooltipHead(cost);
  return (
    <>
      {head.kind === "amount" && (
        <div>
          {`${head.prefix}${formatUsageUsd(head.amount, locale)}`}
          {head.status === "stale" ? ` ${t("usage.cost.status.stale")}` : ""}
        </div>
      )}
      {head.kind === "mixed" && <div>{t("usage.cost.mixed")}</div>}
      {head.kind !== "amount" && costClassTooltipLines(cost, locale, t).map(row => (
        <div key={row.id} className="muted">{row.text}</div>
      ))}
      {cost.unpricedRequests > 0 && (
        <div className="muted">{t("usage.cost.status.unpriced")} · {count(cost.unpricedRequests, locale)}</div>
      )}
      {cost.unmeteredRequests > 0 && (
        <div className="muted">{t("usage.cost.status.unmetered")} · {count(cost.unmeteredRequests, locale)}</div>
      )}
    </>
  );
}

function ChartHoverTip({
  hovered, mode, locale, t,
}: {
  hovered: DayStack;
  mode: "tokens" | "cost";
  locale: string;
  t: TFn;
}) {
  return (
    <div className="usage-chart-tip" role="tooltip">
      <div>{formatAxisDate(hovered.label, locale)}</div>
      {mode === "cost" && hovered.cost ? (
        <CostHoverBody cost={hovered.cost} locale={locale} t={t} />
      ) : (
        <>
          <div>{t("usage.heatmap.tooltipTokens", { tokens: formatTokens(hovered.totalTokens, locale) })}</div>
          <div className="muted">{t("usage.heatmap.tooltipRequests", { requests: hovered.requests })}</div>
          {hovered.segments.map(seg => (
            <div key={seg.key} className="muted">
              {seg.name ?? t("usage.series.other")}
              {" "}
              {formatTokens(seg.tokens, locale)}
            </div>
          ))}
        </>
      )}
    </div>
  );
}

function Kpi({
  label, primary, secondary,
}: {
  label: string;
  primary: string;
  secondary: string;
}) {
  return (
    <div className="usage-kpi">
      <div className="usage-kpi-label">{label}</div>
      <div className="usage-kpi-value">{primary}</div>
      <div className="usage-kpi-sub muted">{secondary}</div>
    </div>
  );
}

function HistoryBlock({ data, locale, t }: { data: UsageResponse; locale: string; t: TFn }) {
  const history = historyDisplay(data);
  const windowText = formatWindowRange(history.start, history.end, locale) ?? "\u2014";
  return (
    <div className="usage-history">
      <div className="usage-history-head">
        <h3>{t("usage.history.title")}</h3>
        <span className={history.kind === "complete" ? "usage-tone-reported" : "usage-tone-unreported"}>
          {history.kind === "complete" ? t("usage.history.complete") : t("usage.history.partial")}
        </span>
      </div>
      <div className="usage-history-window muted">{windowText}</div>
      <p className="muted usage-history-note">
        {history.kind === "complete" ? t("usage.history.completeNote") : t("usage.history.partialNote")}
      </p>
    </div>
  );
}

function DailyChart({
  stacks, locale, t, mode, onMode, costState,
}: {
  stacks: DayStack[];
  locale: string;
  t: TFn;
  mode: "tokens" | "cost";
  onMode: (mode: "tokens" | "cost") => void;
  costState: CostChartState;
}) {
  const [hover, setHover] = useState<number | null>(null);
  const max = Math.max(1, ...stacks.map(row => (
    mode === "tokens" ? row.totalTokens : row.segments.reduce((sum, seg) => sum + seg.tokens, 0)
  )));
  const labels = sparseAxisLabels(stacks.length);
  const hovered = hover === null ? null : stacks[hover];
  const ticks = chartAxisTicks(max);
  const top = ticks[0] ?? 1;
  const tickLabel = (value: number) => (mode === "cost" ? formatUsageUsd(value, locale) : formatTokens(value, locale));
  const emptyCost = mode === "cost" && costState.kind === "empty" ? costState : null;
  return (
    <section className="usage-section" aria-labelledby="usage-daily-title">
      <div className="usage-section-head">
        <h3 id="usage-daily-title">{t("usage.section.daily")}</h3>
        <div className="usage-text-toggle" role="group" aria-label={t("usage.section.daily")}>
          <button type="button" className={mode === "tokens" ? "is-active" : undefined} onClick={() => onMode("tokens")}>
            {t("usage.chart.tokens")}
          </button>
          <button type="button" className={mode === "cost" ? "is-active" : undefined} onClick={() => onMode("cost")}>
            {t("usage.chart.cost")}
          </button>
        </div>
      </div>
      <div className="usage-chart" role={emptyCost ? undefined : "img"} aria-labelledby="usage-daily-title">
        <div className="usage-chart-y" aria-hidden="true">
          {emptyCost ? ticks.map(tick => <span key={tick} />) : ticks.map(tick => <span key={tick}>{tickLabel(tick)}</span>)}
        </div>
        <div className="usage-chart-pane">
          <div className="usage-chart-plot-wrap">
            {emptyCost ? (
              <CostEmptyCopy state={emptyCost} locale={locale} t={t} />
            ) : (
              <>
                <div className="usage-chart-grid" aria-hidden="true">
                  {ticks.map(tick => <i key={tick} />)}
                </div>
                <div className="usage-chart-plot">
                  {stacks.map((row, index) => {
                    const total = mode === "tokens" ? row.totalTokens : row.segments.reduce((sum, seg) => sum + seg.tokens, 0);
                    const height = `${Math.max(0, Math.min(100, (total / top) * 100))}%`;
                    return (
                      <div
                        key={`${row.date}-${index}`}
                        className="usage-chart-col"
                        onMouseEnter={() => setHover(index)}
                        onMouseLeave={() => setHover(current => (current === index ? null : current))}
                      >
                        <div className="usage-chart-track">
                          <div className="usage-chart-stack" style={{ height }}>
                            {row.segments.map(seg => (
                              <div key={seg.key} className="usage-chart-seg" style={{ flexGrow: Math.max(seg.tokens, 0), background: seg.color }} />
                            ))}
                          </div>
                        </div>
                      </div>
                    );
                  })}
                </div>
              </>
            )}
          </div>
          <div className="usage-chart-xrow">
            {stacks.map((row, index) => (
              <span key={`${row.date}-x`} className="usage-chart-x muted">{labels.has(index) ? formatAxisDate(row.label, locale) : ""}</span>
            ))}
          </div>
        </div>
        {!emptyCost && hovered && <ChartHoverTip hovered={hovered} mode={mode} locale={locale} t={t} />}
      </div>
      {mode === "cost" && costState.kind === "partial" && (
        <p className="muted usage-chart-note">
          {t("usage.chart.costPartial", { count: count(costState.gapRequests, locale) })}
        </p>
      )}
    </section>
  );
}

export function UsageOverview({
  data, locale, t,
}: {
  data: UsageResponse;
  locale: Locale;
  t: TFn;
}) {
  const [chartMode, setChartMode] = useState<"tokens" | "cost">("tokens");
  const summary = data.summary;
  const series = useMemo(() => topModelSeries(data.models, summary.totalTokens), [data.models, summary.totalTokens]);
  const tokenStacks = useMemo(() => bucketDailyStacks(data.days, series), [data.days, series]);
  const costStacks = useMemo(() => dailyCostStacks(data.days).map(row => ({
    date: row.date,
    label: row.label,
    requests: row.requests,
    totalTokens: 0,
    segments: row.segments.map(seg => ({ key: seg.id, tokens: seg.amount, color: seg.color })),
    cost: row.cost,
  })), [data.days]);
  const cache = cacheReadShare(summary);
  const uncovered = uncoveredRequests(summary);
  const coveragePct = formatPct1(summary.coverageRatio) ?? "\u2014";
  const topModels = data.models.toSorted((a, b) => b.totalTokens - a.totalTokens).slice(0, 5);
  const topProviders = data.providers.toSorted((a, b) => b.totalTokens - a.totalTokens).slice(0, 5);
  const quality = [
    { id: "reported", count: summary.reportedRequests, tone: "reported" },
    { id: "estimated", count: summary.estimatedRequests, tone: "estimated" },
    { id: "unsupported", count: summary.unsupportedRequests, tone: "unsupported" },
    { id: "unreported", count: summary.unreportedRequests, tone: "unreported" },
  ] as const;
  const qualityTotal = quality.reduce((sum, row) => sum + row.count, 0);
  const price = listPriceCopy(data.cost, summary.requests, locale, t);

  return (
    <div className="usage-overview">
      <div className="usage-kpis" role="group" aria-label={t("usage.title")}>
        <Kpi
          label={t("usage.card.totalTokens")}
          primary={formatTokens(summary.totalTokens, locale)}
          secondary={t("usage.kpi.tokensSub", {
            input: formatTokens(summary.inputTokens, locale),
            output: formatTokens(summary.outputTokens, locale),
          })}
        />
        <Kpi
          label={t("usage.card.requests")}
          primary={count(summary.requests, locale)}
          secondary={uncovered > 0
            ? t("usage.kpi.requestsSubUncovered", { measured: count(summary.measuredRequests, locale), uncovered: count(uncovered, locale) })
            : t("usage.kpi.requestsSub", { measured: count(summary.measuredRequests, locale) })}
        />
        <Kpi
          label={t("usage.kpi.cacheRead")}
          primary={cache ? formatPct1(cache.ratio) ?? "\u2014" : "\u2014"}
          secondary={cache
            ? t("usage.kpi.cacheReadSub", { reads: formatTokens(cache.reads, locale), input: formatTokens(cache.input, locale) })
            : t("usage.kpi.cacheReadMissing")}
        />
        <Kpi
          label={t("usage.card.coverage")}
          primary={coveragePct}
          secondary={summary.requests > 0 && summary.measuredRequests !== summary.requests
            ? t("usage.kpi.coverageMeasuredOf", { measured: count(summary.measuredRequests, locale), requests: count(summary.requests, locale) })
            : t("usage.kpi.requestsSub", { measured: count(summary.measuredRequests, locale) })}
        />
        <Kpi
          label={t("usage.kpi.listPrice")}
          primary={price.primary}
          secondary={price.secondary}
        />
      </div>

      <section className="usage-section" aria-labelledby="usage-mix-title">
        <h3 id="usage-mix-title">{t("usage.section.mix")}</h3>
        <div className="usage-mix-bar" aria-hidden="true">
          {series.map(item => (
            <div key={item.key} style={{ flexGrow: item.totalTokens, background: item.color }} />
          ))}
        </div>
        <div className="usage-mix-legend">
          {series.map(item => (
            <span key={item.key}>
              <i style={{ background: item.color }} />
              {item.other ? t("usage.series.other") : modelLabel(item.model)}
              {" "}
              {formatPct1(item.share) ?? "\u2014"}
            </span>
          ))}
        </div>
      </section>

      <DailyChart
        stacks={chartMode === "tokens" ? tokenStacks : costStacks}
        locale={locale}
        t={t}
        mode={chartMode}
        onMode={setChartMode}
        costState={costChartState(data.cost, summary.requests)}
      />

      <div className="usage-bottom">
        <section className="usage-bottom-col" aria-labelledby="usage-top-models">
          <h3 id="usage-top-models">{t("usage.section.topModels")}</h3>
          <table className="usage-mini-table">
            <thead>
              <tr>
                <th>{t("logs.col.model")}</th>
                <th className="num">{t("usage.col.tokens")}</th>
                <th className="num">{t("usage.col.share")}</th>
              </tr>
            </thead>
            <tbody>
              {topModels.map(row => (
                <tr key={`${row.provider}/${row.model}`}>
                  <td className="mono">{modelLabel(row.model)}</td>
                  <td className="num mono">{formatTokens(row.totalTokens, locale)}</td>
                  <td className="num">{formatPct1(row.shareRatio) ?? "\u2014"}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
        <section className="usage-bottom-col" aria-labelledby="usage-top-providers">
          <h3 id="usage-top-providers">{t("usage.section.topProviders")}</h3>
          <table className="usage-mini-table">
            <thead>
              <tr>
                <th>{t("logs.col.provider")}</th>
                <th className="num">{t("usage.col.tokens")}</th>
                <th className="num">{t("usage.col.requests")}</th>
              </tr>
            </thead>
            <tbody>
              {topProviders.map(row => (
                <tr key={row.provider}>
                  <td>{formatProviderDisplayName(row.provider, t)}</td>
                  <td className="num mono">{formatTokens(row.totalTokens, locale)}</td>
                  <td className="num">{count(row.requests, locale)}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </section>
        <section className="usage-bottom-col" aria-labelledby="usage-quality">
          <h3 id="usage-quality">{t("usage.section.quality")}</h3>
          <div className="usage-quality-rows">
            <div className="usage-quality-row usage-quality-head">
              <span>{t("usage.quality.category")}</span>
              <span className="num">{t("usage.col.requests")}</span>
              <span className="num">{t("usage.col.share")}</span>
            </div>
            {quality.map(row => (
              <div key={row.id} className="usage-quality-row">
                <span className="usage-quality-cat">
                  <i className={`usage-dot usage-fill-${row.tone}`} />
                  {row.id === "reported" ? t("usage.quality.reported")
                    : row.id === "estimated" ? t("usage.quality.estimated")
                      : row.id === "unsupported" ? t("usage.quality.unsupported")
                        : t("usage.quality.unreported")}
                </span>
                <span className="num">{count(row.count, locale)}</span>
                <span className="num muted">{formatPct1(qualityTotal > 0 ? row.count / qualityTotal : undefined) ?? "\u2014"}</span>
              </div>
            ))}
          </div>
          <HistoryBlock data={data} locale={locale} t={t} />
        </section>
      </div>
    </div>
  );
}
