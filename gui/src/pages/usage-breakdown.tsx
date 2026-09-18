import { useMemo, useState } from "react";
import { IconSearch } from "../icons";
import { formatTokens } from "../format-tokens";
import { formatProviderDisplayName, providerIconSrc } from "../provider-icons";
import { modelLabel } from "../model-display";
import type { Locale, TFn } from "../i18n/shared";
import { presentCost, formatPresentedAmount } from "./usage-cost";
import { coverageShare, formatPct1 } from "./usage-format";
import type { UsageAccount, UsageBreakdownTab, UsageModel, UsageProvider, UsageResponse } from "./usage-contract";
import type { UsageCostSummary } from "./usage-contract";

type SortDir = "asc" | "desc";

function count(n: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(n);
}

function CostTd({ cost, locale, t }: { cost: UsageCostSummary | undefined; locale: string; t: TFn }) {
  const presented = presentCost(cost);
  if (presented.kind === "amount") {
    return (
      <td className="num" title={presented.status === "stale" ? t("usage.cost.status.stale") : undefined}>
        {formatPresentedAmount(presented, locale)}
      </td>
    );
  }
  if (presented.kind === "mixed") {
    return <td className="num" title={t("usage.cost.mixedNote")}>{t("usage.cost.mixed")}</td>;
  }
  if (presented.kind === "partial") {
    return (
      <td className="num" title={t("usage.cost.partialNote", { count: String(presented.gapRequests) })}>
        {t("usage.cost.partial")}
      </td>
    );
  }
  return <td className="num">{"\u2014"}</td>;
}

function coverageCell(measured: number, requests: number): string {
  return formatPct1(coverageShare(measured, requests)) ?? "\u2014";
}

function SortHead({
  label, active, dir, onClick, numeric,
}: {
  label: string;
  active: boolean;
  dir: SortDir;
  onClick: () => void;
  numeric?: boolean;
}) {
  return (
    <th className={numeric ? "num" : undefined}>
      <button type="button" className={`usage-sort${active ? " is-active" : ""}`} onClick={onClick}>
        {label}
        {active ? (dir === "desc" ? " ↓" : " ↑") : ""}
      </button>
    </th>
  );
}

function compare(a: number | string, b: number | string, dir: SortDir): number {
  const order = dir === "asc" ? 1 : -1;
  if (typeof a === "number" && typeof b === "number") return (a - b) * order;
  return String(a).localeCompare(String(b)) * order;
}

function ProviderMark({ provider }: { provider: string }) {
  const src = providerIconSrc(provider);
  if (!src) return null;
  return <img className="usage-row-icon" src={src} alt="" aria-hidden="true" />;
}

function ModelsTable({
  models, query, onQuery, locale, t,
}: {
  models: UsageModel[];
  query: string;
  onQuery: (value: string) => void;
  locale: string;
  t: TFn;
}) {
  const [sort, setSort] = useState<{ key: string; dir: SortDir }>({ key: "total", dir: "desc" });
  const rows = useMemo(() => {
    const needle = query.trim().toLowerCase();
    const filtered = needle
      ? models.filter(row =>
        row.model.toLowerCase().includes(needle) ||
        row.provider.toLowerCase().includes(needle))
      : models;
    const value = (row: UsageModel): number | string => {
      if (sort.key === "model") return row.model;
      if (sort.key === "provider") return row.provider;
      if (sort.key === "requests") return row.requests;
      if (sort.key === "input") return row.inputTokens;
      if (sort.key === "output") return row.outputTokens;
      if (sort.key === "share") return row.shareRatio;
      if (sort.key === "coverage") return coverageShare(row.measuredRequests, row.requests) ?? -1;
      if (sort.key === "cost") {
        const presented = presentCost(row.cost);
        return presented.kind === "amount" ? presented.amount : -1;
      }
      return row.totalTokens;
    };
    return filtered.toSorted((a, b) => compare(value(a), value(b), sort.dir));
  }, [models, query, sort]);
  const toggle = (key: string) => {
    setSort(current => current.key === key
      ? { key, dir: current.dir === "desc" ? "asc" : "desc" }
      : { key, dir: key === "model" || key === "provider" ? "asc" : "desc" });
  };
  return (
    <>
      <label className="usage-search">
        <IconSearch width={14} height={14} aria-hidden="true" />
        <span className="sr-only">{t("usage.search.models")}</span>
        <input
          value={query}
          placeholder={t("usage.search.models")}
          onChange={event => onQuery(event.target.value)}
        />
      </label>
      <div className="usage-table-wrap">
        <table className="usage-table">
          <thead>
            <tr>
              <SortHead label={t("logs.col.model")} active={sort.key === "model"} dir={sort.dir} onClick={() => toggle("model")} />
              <SortHead label={t("logs.col.provider")} active={sort.key === "provider"} dir={sort.dir} onClick={() => toggle("provider")} />
              <SortHead label={t("usage.col.requests")} active={sort.key === "requests"} dir={sort.dir} onClick={() => toggle("requests")} numeric />
              <SortHead label={t("usage.col.input")} active={sort.key === "input"} dir={sort.dir} onClick={() => toggle("input")} numeric />
              <SortHead label={t("usage.col.output")} active={sort.key === "output"} dir={sort.dir} onClick={() => toggle("output")} numeric />
              <SortHead label={t("usage.col.total")} active={sort.key === "total"} dir={sort.dir} onClick={() => toggle("total")} numeric />
              <SortHead label={t("usage.col.share")} active={sort.key === "share"} dir={sort.dir} onClick={() => toggle("share")} numeric />
              <SortHead label={t("usage.card.coverage")} active={sort.key === "coverage"} dir={sort.dir} onClick={() => toggle("coverage")} numeric />
              <SortHead label={t("usage.col.listPrice")} active={sort.key === "cost"} dir={sort.dir} onClick={() => toggle("cost")} numeric />
            </tr>
          </thead>
          <tbody>
            {rows.map(row => (
              <tr key={`${row.provider}/${row.model}`}>
                <td className="mono">{modelLabel(row.model)}</td>
                <td>
                  <span className="usage-provider-cell">
                    <ProviderMark provider={row.provider} />
                    {formatProviderDisplayName(row.provider, t)}
                  </span>
                </td>
                <td className="num">{count(row.requests, locale)}</td>
                <td className="num mono">{formatTokens(row.inputTokens, locale)}</td>
                <td className="num mono">{formatTokens(row.outputTokens, locale)}</td>
                <td className="num mono">{formatTokens(row.totalTokens, locale)}</td>
                <td className="num">{formatPct1(row.shareRatio) ?? "\u2014"}</td>
                <td className="num">{coverageCell(row.measuredRequests, row.requests)}</td>
                <CostTd cost={row.cost} locale={locale} t={t} />
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </>
  );
}

function ProvidersTable({ providers, locale, t }: { providers: UsageProvider[]; locale: string; t: TFn }) {
  const [sort, setSort] = useState<{ key: string; dir: SortDir }>({ key: "total", dir: "desc" });
  const rows = useMemo(() => {
    const value = (row: UsageProvider): number | string => {
      if (sort.key === "provider") return row.provider;
      if (sort.key === "requests") return row.requests;
      if (sort.key === "attempts") return row.attemptCount;
      if (sort.key === "share") return row.shareRatio;
      if (sort.key === "coverage") return coverageShare(row.measuredRequests, row.requests) ?? -1;
      if (sort.key === "cost") {
        const presented = presentCost(row.cost);
        return presented.kind === "amount" ? presented.amount : -1;
      }
      return row.totalTokens;
    };
    return providers.toSorted((a, b) => compare(value(a), value(b), sort.dir));
  }, [providers, sort]);
  const toggle = (key: string) => {
    setSort(current => current.key === key
      ? { key, dir: current.dir === "desc" ? "asc" : "desc" }
      : { key, dir: key === "provider" ? "asc" : "desc" });
  };
  return (
    <div className="usage-table-wrap">
      <table className="usage-table">
        <thead>
          <tr>
            <SortHead label={t("logs.col.provider")} active={sort.key === "provider"} dir={sort.dir} onClick={() => toggle("provider")} />
            <SortHead label={t("usage.col.requests")} active={sort.key === "requests"} dir={sort.dir} onClick={() => toggle("requests")} numeric />
            <SortHead label={t("usage.col.attempts")} active={sort.key === "attempts"} dir={sort.dir} onClick={() => toggle("attempts")} numeric />
            <SortHead label={t("usage.col.totalTokens")} active={sort.key === "total"} dir={sort.dir} onClick={() => toggle("total")} numeric />
            <SortHead label={t("usage.col.share")} active={sort.key === "share"} dir={sort.dir} onClick={() => toggle("share")} numeric />
            <SortHead label={t("usage.card.coverage")} active={sort.key === "coverage"} dir={sort.dir} onClick={() => toggle("coverage")} numeric />
            <SortHead label={t("usage.col.listPrice")} active={sort.key === "cost"} dir={sort.dir} onClick={() => toggle("cost")} numeric />
          </tr>
        </thead>
        <tbody>
          {rows.map(row => (
            <tr key={row.provider}>
              <td>
                <span className="usage-provider-cell">
                  <ProviderMark provider={row.provider} />
                  {formatProviderDisplayName(row.provider, t)}
                </span>
              </td>
              <td className="num">{count(row.requests, locale)}</td>
              <td className="num">{count(row.attemptCount, locale)}</td>
              <td className="num mono">{formatTokens(row.totalTokens, locale)}</td>
              <td className="num">{formatPct1(row.shareRatio) ?? "\u2014"}</td>
              <td className="num">{coverageCell(row.measuredRequests, row.requests)}</td>
              <CostTd cost={row.cost} locale={locale} t={t} />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function AccountsTable({ accounts, locale, t }: { accounts: UsageAccount[]; locale: string; t: TFn }) {
  const [sort, setSort] = useState<{ key: string; dir: SortDir }>({ key: "total", dir: "desc" });
  if (accounts.length === 0) {
    return <p className="usage-empty-inline">{t("usage.accounts.empty")}</p>;
  }
  const rows = accounts.toSorted((a, b) => {
    const value = (row: UsageAccount): number | string => {
      if (sort.key === "account") return row.accountLogLabel;
      if (sort.key === "requests") return row.requests;
      if (sort.key === "measured") return row.measuredRequests;
      if (sort.key === "coverage") return row.usageCoverageRatio;
      if (sort.key === "cost") {
        const presented = presentCost(row.cost);
        return presented.kind === "amount" ? presented.amount : -1;
      }
      return row.totalTokens;
    };
    return compare(value(a), value(b), sort.dir);
  });
  const toggle = (key: string) => {
    setSort(current => current.key === key
      ? { key, dir: current.dir === "desc" ? "asc" : "desc" }
      : { key, dir: key === "account" ? "asc" : "desc" });
  };
  return (
    <div className="usage-table-wrap">
      <table className="usage-table">
        <thead>
          <tr>
            <SortHead label={t("usage.col.account")} active={sort.key === "account"} dir={sort.dir} onClick={() => toggle("account")} />
            <SortHead label={t("usage.col.requests")} active={sort.key === "requests"} dir={sort.dir} onClick={() => toggle("requests")} numeric />
            <SortHead label={t("usage.col.measured")} active={sort.key === "measured"} dir={sort.dir} onClick={() => toggle("measured")} numeric />
            <SortHead label={t("usage.card.coverage")} active={sort.key === "coverage"} dir={sort.dir} onClick={() => toggle("coverage")} numeric />
            <SortHead label={t("usage.col.totalTokens")} active={sort.key === "total"} dir={sort.dir} onClick={() => toggle("total")} numeric />
            <SortHead label={t("usage.col.listPrice")} active={sort.key === "cost"} dir={sort.dir} onClick={() => toggle("cost")} numeric />
          </tr>
        </thead>
        <tbody>
          {rows.map(row => (
            <tr key={row.accountLogLabel}>
              <td className="mono">{row.accountLogLabel}</td>
              <td className="num">{count(row.requests, locale)}</td>
              <td className="num">{count(row.measuredRequests, locale)}</td>
              <td className="num">{formatPct1(row.usageCoverageRatio) ?? "\u2014"}</td>
              <td className="num mono">{formatTokens(row.totalTokens, locale)}</td>
              <CostTd cost={row.cost} locale={locale} t={t} />
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

export function UsageBreakdown({
  data, tab, onTab, modelQuery, onModelQuery, locale, t,
}: {
  data: UsageResponse;
  tab: UsageBreakdownTab;
  onTab: (tab: UsageBreakdownTab) => void;
  modelQuery: string;
  onModelQuery: (value: string) => void;
  locale: Locale;
  t: TFn;
}) {
  return (
    <div className="usage-breakdown">
      <div className="page-tabs usage-subtabs" role="tablist" aria-label={t("usage.section.breakdown")}>
        <button type="button" role="tab" className={`page-tab${tab === "models" ? " page-tab--active" : ""}`} aria-selected={tab === "models"} onClick={() => onTab("models")}>
          {t("usage.section.models")}
        </button>
        <button type="button" role="tab" className={`page-tab${tab === "providers" ? " page-tab--active" : ""}`} aria-selected={tab === "providers"} onClick={() => onTab("providers")}>
          {t("usage.section.providers")}
        </button>
        <button type="button" role="tab" className={`page-tab${tab === "accounts" ? " page-tab--active" : ""}`} aria-selected={tab === "accounts"} onClick={() => onTab("accounts")}>
          {t("usage.section.accounts")}
        </button>
      </div>
      {tab === "models" && <ModelsTable models={data.models} query={modelQuery} onQuery={onModelQuery} locale={locale} t={t} />}
      {tab === "providers" && <ProvidersTable providers={data.providers} locale={locale} t={t} />}
      {tab === "accounts" && <AccountsTable accounts={data.accounts} locale={locale} t={t} />}
    </div>
  );
}
