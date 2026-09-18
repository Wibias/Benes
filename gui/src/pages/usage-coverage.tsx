import type { Locale, TFn } from "../i18n/shared";
import { costClassRows, formatUsageUsd } from "./usage-cost";
import {
  formatPct1,
  formatPrefixBytes,
  formatWindowRange,
  historyDisplay,
  shareOf,
} from "./usage-format";
import type { UsageResponse, UsageSummaryTotals, UsageSurfaceAttribution } from "./usage-contract";

function count(n: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(n);
}

function DistBar({
  items,
}: {
  items: Array<{ id: string; value: number; colorClass: string }>;
}) {
  return (
    <div className="usage-mix-bar" aria-hidden="true">
      {items.map(item => (
        <div key={item.id} className={item.colorClass} style={{ flexGrow: item.value }} />
      ))}
    </div>
  );
}

function Measurement({ summary, locale, t }: { summary: UsageSummaryTotals; locale: string; t: TFn }) {
  const rows = [
    { id: "reported", value: summary.reportedRequests, colorClass: "usage-fill-reported" },
    { id: "estimated", value: summary.estimatedRequests, colorClass: "usage-fill-estimated" },
    { id: "unsupported", value: summary.unsupportedRequests, colorClass: "usage-fill-unsupported" },
    { id: "unreported", value: summary.unreportedRequests, colorClass: "usage-fill-unreported" },
  ];
  const total = rows.reduce((sum, row) => sum + row.value, 0);
  const coverage = formatPct1(summary.coverageRatio) ?? "\u2014";
  return (
    <section className="usage-cover-cell" aria-labelledby="usage-measure-title">
      <h3 id="usage-measure-title">{t("usage.coverage.measurement")}</h3>
      <DistBar items={rows} />
      <div className="usage-cover-legend">
        {rows.map(row => (
          <div key={row.id}>
            <span className={`usage-tone-${row.id}`}>{t(
              row.id === "reported" ? "usage.quality.reported"
                : row.id === "estimated" ? "usage.quality.estimated"
                  : row.id === "unsupported" ? "usage.quality.unsupported"
                    : "usage.quality.unreported",
            )}</span>
            <span>{count(row.value, locale)}</span>
            <span className="muted">{formatPct1(shareOf(row.value, total)) ?? "\u2014"}</span>
          </div>
        ))}
      </div>
      <p className="muted">
        {t("usage.coverage.measuredLine", {
          pct: coverage,
          measured: count(summary.measuredRequests, locale),
          requests: count(summary.requests, locale),
        })}
      </p>
    </section>
  );
}

function CostQuality({ data, locale, t }: { data: UsageResponse; locale: string; t: TFn }) {
  if (!data.cost) {
    return (
      <section className="usage-cover-cell" aria-labelledby="usage-costq-title">
        <h3 id="usage-costq-title">{t("usage.coverage.costQuality")}</h3>
        <p>{t("usage.cost.unavailable")}</p>
        <p className="muted">{t("usage.chart.costProxyMissing")}</p>
      </section>
    );
  }
  const rows = costClassRows(data.cost);
  const total = rows.reduce((sum, row) => sum + row.requests, 0);
  const labels: Record<string, string> = {
    exact: t("usage.cost.status.exact"),
    estimated: t("usage.cost.status.estimated"),
    lower_bound: t("usage.cost.status.lower_bound"),
    stale: t("usage.cost.status.stale"),
    unpriced: t("usage.cost.status.unpriced"),
    unmetered: t("usage.cost.status.unmetered"),
  };
  return (
    <section className="usage-cover-cell" aria-labelledby="usage-costq-title">
      <h3 id="usage-costq-title">{t("usage.coverage.costQuality")}</h3>
      <DistBar items={rows.map(row => ({
        id: row.id,
        value: row.requests,
        colorClass: `usage-fill-cost-${row.id.replace("_", "-")}`,
      }))} />
      <div className="usage-cover-legend">
        {rows.map(row => (
          <div key={row.id}>
            <span>{labels[row.id]}</span>
            <span>{count(row.requests, locale)}</span>
            <span className="muted">{formatPct1(shareOf(row.requests, total)) ?? "\u2014"}</span>
            <span className="mono">{row.amountUsd === undefined ? "\u2014" : formatUsageUsd(row.amountUsd, locale)}</span>
          </div>
        ))}
      </div>
    </section>
  );
}

function SurfaceBlock({
  attribution, locale, t,
}: {
  attribution: UsageSurfaceAttribution | undefined;
  locale: string;
  t: TFn;
}) {
  if (!attribution) {
    return (
      <section className="usage-cover-cell" aria-labelledby="usage-surface-title">
        <h3 id="usage-surface-title">{t("usage.coverage.surface")}</h3>
        <p className="muted">{t("usage.coverage.surfaceUnavailable")}</p>
      </section>
    );
  }
  const rows = [
    { id: "codex", value: attribution.codex, colorClass: "usage-fill-surface-codex", label: t("logs.filter.surface.codex") },
    { id: "claude", value: attribution.claude, colorClass: "usage-fill-surface-claude", label: t("logs.filter.surface.claude") },
    { id: "claudeDesktop", value: attribution.claudeDesktop, colorClass: "usage-fill-surface-claude-desktop", label: t("usage.surface.claudeDesktop") },
    { id: "grok", value: attribution.grok, colorClass: "usage-fill-surface-grok", label: t("logs.filter.surface.grok") },
    { id: "unattributed", value: attribution.unattributed, colorClass: "usage-fill-surface-unattributed", label: t("usage.surface.unattributed") },
  ];
  const total = rows.reduce((sum, row) => sum + row.value, 0);
  return (
    <section className="usage-cover-cell" aria-labelledby="usage-surface-title">
      <h3 id="usage-surface-title">{t("usage.coverage.surface")}</h3>
      <DistBar items={rows} />
      <div className="usage-cover-legend">
        {rows.map(row => (
          <div key={row.id}>
            <span>{row.label}</span>
            <span>{count(row.value, locale)}</span>
            <span className="muted">{formatPct1(shareOf(row.value, total)) ?? "\u2014"}</span>
          </div>
        ))}
      </div>
      <p className="muted">{t("usage.coverage.surfaceNote")}</p>
    </section>
  );
}

function HistoryCell({ data, locale, t }: { data: UsageResponse; locale: string; t: TFn }) {
  const history = historyDisplay(data);
  const windowText = formatWindowRange(history.start, history.end, locale);
  const skipped = history.kind === "partial" ? formatPrefixBytes(history.skippedPrefixBytes, locale) : undefined;
  return (
    <section className="usage-cover-cell" aria-labelledby="usage-hist-title">
      <h3 id="usage-hist-title">{t("usage.history.title")}</h3>
      <div className="usage-quality-row">
        <span>{t("usage.history.status")}</span>
        <span className={history.kind === "complete" ? "usage-tone-reported" : "usage-tone-unreported"}>
          {history.kind === "complete" ? t("usage.history.complete") : t("usage.history.partial")}
        </span>
      </div>
      <div className="usage-quality-row">
        <span>{t("usage.history.window")}</span>
        <span>{windowText ?? "\u2014"}</span>
      </div>
      <p className="muted">{history.kind === "complete" ? t("usage.history.completeNote") : t("usage.history.partialNote")}</p>
      {skipped && <p className="muted">{t("usage.history.skippedPrefix", { bytes: skipped })}</p>}
    </section>
  );
}

export function UsageCoverage({
  data, locale, t,
}: {
  data: UsageResponse;
  locale: Locale;
  t: TFn;
}) {
  return (
    <div className="usage-coverage">
      <Measurement summary={data.summary} locale={locale} t={t} />
      <CostQuality data={data} locale={locale} t={t} />
      <SurfaceBlock attribution={data.surfaceAttribution} locale={locale} t={t} />
      <HistoryCell data={data} locale={locale} t={t} />
    </div>
  );
}
