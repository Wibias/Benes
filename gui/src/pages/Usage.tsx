import { useEffect, useState, type ReactNode } from "react";
import { useI18n, type TFn } from "../i18n/shared";
import { Notice } from "../ui";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { UsageFilters } from "./usage-filters";
import { UsageOverview } from "./usage-overview";
import { UsageBreakdown } from "./usage-breakdown";
import { UsageCoverage } from "./usage-coverage";
import { useUsageWorkspace } from "./use-usage-workspace";
import { usageReadFailed, type UsageBoardTab, type UsageBreakdownTab, type UsageResponse } from "./usage-contract";
import {
  readUsageBreakdownFromHash,
  readUsageTabFromHash,
  selectUsageTab,
  usageTabKeyDown,
} from "./usage-tab";
import type { UsageRange } from "./usage-range";

function emptyUsagePanel(data: UsageResponse, range: UsageRange, onShowAll: () => void, t: TFn): ReactNode {
  const noLedger = data.snapshotWindowStart == null && data.snapshotWindowEnd == null;
  return (
    <div className="usage-empty">
      <div>{noLedger ? t("usage.emptyTitle") : t("usage.emptyRange")}</div>
      {noLedger ? <p className="muted">{t("usage.emptyBody")}</p> : range !== "all" && (
        <button type="button" className="usage-empty-action" onClick={onShowAll}>
          {t("usage.viewAvailable")}
        </button>
      )}
    </div>
  );
}

function usageBoardBody({
  rangeQuery, state, data, tab, breakdown, board, locale, t, onSelectBreakdown, onRetry,
}: {
  rangeQuery: { ok: true } | { ok: false; error: "malformed" | "reversed" };
  state: { showSkeleton: boolean; kind: string; error?: unknown };
  data: UsageResponse | null;
  tab: UsageBoardTab;
  breakdown: UsageBreakdownTab;
  board: { modelQuery: string; setModelQuery: (value: string) => void; range: UsageRange; setRange: (range: UsageRange) => void };
  locale: import("../i18n/shared").Locale;
  t: TFn;
  onSelectBreakdown: (tab: UsageBreakdownTab) => void;
  onRetry: () => void;
}): ReactNode {
  if (!rangeQuery.ok) {
    return <Notice tone="err">{rangeQuery.error === "reversed" ? t("usage.range.reversed") : t("usage.range.malformed")}</Notice>;
  }
  if (state.showSkeleton && !data) return <DataSurfaceSkeleton label={t("usage.loading")} rows={5} />;
  if (state.kind === "failed-cold") {
    return (
      <Notice tone="err">
        {state.error instanceof Error ? `${t("usage.loadError")} ${state.error.message}` : t("usage.loadError")}{" "}
        <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
      </Notice>
    );
  }
  if (usageReadFailed(data)) return <Notice tone="err">{t("usage.readFailed")}</Notice>;
  if (data && data.summary.requests === 0) {
    return emptyUsagePanel(data, board.range, () => board.setRange("all"), t);
  }
  if (!data) return null;
  if (tab === "overview") return <UsageOverview data={data} locale={locale} t={t} />;
  if (tab === "coverage") return <UsageCoverage data={data} locale={locale} t={t} />;
  return (
    <UsageBreakdown
      data={data}
      tab={breakdown}
      onTab={onSelectBreakdown}
      modelQuery={board.modelQuery}
      onModelQuery={board.setModelQuery}
      locale={locale}
      t={t}
    />
  );
}

export default function Usage({ apiBase }: { apiBase: string }) {
  const { t, locale } = useI18n();
  const [tab, setTab] = useState<UsageBoardTab>(readUsageTabFromHash);
  const [breakdown, setBreakdown] = useState<UsageBreakdownTab>(readUsageBreakdownFromHash);
  const board = useUsageWorkspace(apiBase);
  const { rangeQuery, resource, data } = board;
  const { state } = resource;

  useEffect(() => {
    const onHash = () => {
      setTab(readUsageTabFromHash());
      setBreakdown(readUsageBreakdownFromHash());
    };
    window.addEventListener("hashchange", onHash);
    window.addEventListener("popstate", onHash);
    return () => {
      window.removeEventListener("hashchange", onHash);
      window.removeEventListener("popstate", onHash);
    };
  }, []);

  const onSelectTab = (next: UsageBoardTab) => {
    setTab(next);
    selectUsageTab(next, breakdown);
  };
  const onSelectBreakdown = (next: UsageBreakdownTab) => {
    setBreakdown(next);
    selectUsageTab("breakdown", next);
  };

  const body = usageBoardBody({
    rangeQuery,
    state,
    data,
    tab,
    breakdown,
    board,
    locale,
    t,
    onSelectBreakdown,
    onRetry: () => resource.refresh(),
  });

  return (
    <div className="usage-board">
      <div className="page-head usage-head">
        <h2>{t("usage.title")}</h2>
        <UsageFilters
          surface={board.surface}
          range={board.range}
          customStart={board.customStart}
          customEnd={board.customEnd}
          onSurface={board.setSurface}
          onRange={board.setRange}
          onCustomStart={board.setCustomStart}
          onCustomEnd={board.setCustomEnd}
          t={t}
        />
      </div>
      <div className="page-tabs" role="tablist" aria-label={t("usage.title")}>
        <button
          type="button"
          role="tab"
          id="usage-tab-overview"
          className={`page-tab${tab === "overview" ? " page-tab--active" : ""}`}
          aria-selected={tab === "overview"}
          onClick={() => onSelectTab("overview")}
          onKeyDown={event => usageTabKeyDown(event, tab, breakdown)}
        >
          {t("usage.section.overview")}
        </button>
        <button
          type="button"
          role="tab"
          id="usage-tab-breakdown"
          className={`page-tab${tab === "breakdown" ? " page-tab--active" : ""}`}
          aria-selected={tab === "breakdown"}
          onClick={() => onSelectTab("breakdown")}
          onKeyDown={event => usageTabKeyDown(event, tab, breakdown)}
        >
          {t("usage.section.breakdown")}
        </button>
        <button
          type="button"
          role="tab"
          id="usage-tab-coverage"
          className={`page-tab${tab === "coverage" ? " page-tab--active" : ""}`}
          aria-selected={tab === "coverage"}
          onClick={() => onSelectTab("coverage")}
          onKeyDown={event => usageTabKeyDown(event, tab, breakdown)}
        >
          {t("usage.section.coverage")}
        </button>
      </div>
      {state.showError && !usageReadFailed(data) && <Notice tone="err">{t("usage.loadError")}</Notice>}
      <div className="usage-board-body">{body}</div>
    </div>
  );
}
