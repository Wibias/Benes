import type { UsageCostSummary, UsageDay, UsageModel } from "./usage-contract.ts";

export const USAGE_SERIES_LIMIT = 4;

export const USAGE_SERIES_COLORS = [
  "var(--usage-series-1)",
  "var(--usage-series-2)",
  "var(--usage-series-3)",
  "var(--usage-series-4)",
] as const;

export const USAGE_SERIES_OTHER_COLOR = "var(--usage-series-other)";

export type UsageSeriesKey = string;

export interface UsageSeries {
  key: UsageSeriesKey;
  model: string;
  provider: string;
  totalTokens: number;
  share: number;
  color: string;
  other: boolean;
}

export function modelSeriesKey(provider: string, model: string): UsageSeriesKey {
  return `${provider}/${model}`;
}

export function topModelSeries(models: UsageModel[], totalTokens: number): UsageSeries[] {
  const ranked = models.toSorted((a, b) => b.totalTokens - a.totalTokens);
  const head = ranked.slice(0, USAGE_SERIES_LIMIT);
  const rest = ranked.slice(USAGE_SERIES_LIMIT);
  const series: UsageSeries[] = head.map((row, index) => ({
    key: modelSeriesKey(row.provider, row.model),
    model: row.model,
    provider: row.provider,
    totalTokens: row.totalTokens,
    share: totalTokens > 0 ? row.totalTokens / totalTokens : 0,
    color: USAGE_SERIES_COLORS[index] ?? USAGE_SERIES_OTHER_COLOR,
    other: false,
  }));
  const otherTokens = rest.reduce((sum, row) => sum + row.totalTokens, 0);
  if (otherTokens > 0) {
    series.push({
      key: "other",
      model: "other",
      provider: "",
      totalTokens: otherTokens,
      share: totalTokens > 0 ? otherTokens / totalTokens : 0,
      color: USAGE_SERIES_OTHER_COLOR,
      other: true,
    });
  }
  return series;
}

export interface DayStackSegment {
  key: UsageSeriesKey;
  tokens: number;
  color: string;
  name?: string;
}

export interface DayCostMeta {
  exactAmount: number;
  estimatedAmount: number;
  lowerBoundAmount: number;
  staleAmount: number;
  unpricedRequests: number;
  unmeteredRequests: number;
}

export interface DayStack {
  date: string;
  label: string;
  requests: number;
  totalTokens: number;
  segments: DayStackSegment[];
  cost?: DayCostMeta;
}

function stackDay(day: UsageDay, series: UsageSeries[]): DayStack {
  const known = new Map(series.filter(item => !item.other).map(item => [item.key, item]));
  const other = series.find(item => item.other);
  const used = new Map<UsageSeriesKey, number>();
  for (const row of day.models) {
    const key = modelSeriesKey(row.provider, row.model);
    if (known.has(key)) used.set(key, (used.get(key) ?? 0) + row.totalTokens);
    else if (other) used.set("other", (used.get("other") ?? 0) + row.totalTokens);
  }
  const segments: DayStackSegment[] = series
    .map(item => ({
      key: item.key,
      tokens: used.get(item.key) ?? 0,
      color: item.color,
      name: item.other ? undefined : item.model,
    }))
    .filter(item => item.tokens > 0);
  return {
    date: day.date,
    label: day.date,
    requests: day.requests,
    totalTokens: day.totalTokens,
    segments,
  };
}

function isoWeekStart(iso: string): string {
  const [year, month, day] = iso.split("-").map(Number);
  const at = new Date(year, month - 1, day);
  const weekday = at.getDay();
  at.setDate(at.getDate() - weekday);
  return `${at.getFullYear()}-${String(at.getMonth() + 1).padStart(2, "0")}-${String(at.getDate()).padStart(2, "0")}`;
}

function isoMonth(iso: string): string {
  return iso.slice(0, 7);
}

function mergeStacks(rows: DayStack[], label: string): DayStack {
  const used = new Map<UsageSeriesKey, { tokens: number; color: string; name?: string }>();
  for (const row of rows) {
    for (const seg of row.segments) {
      const prev = used.get(seg.key);
      used.set(seg.key, {
        tokens: (prev?.tokens ?? 0) + seg.tokens,
        color: seg.color,
        name: prev?.name ?? seg.name,
      });
    }
  }
  return {
    date: rows[0]?.date ?? label,
    label,
    requests: rows.reduce((sum, row) => sum + row.requests, 0),
    totalTokens: rows.reduce((sum, row) => sum + row.totalTokens, 0),
    segments: [...used.entries()].map(([key, value]) => ({
      key,
      tokens: value.tokens,
      color: value.color,
      name: value.name,
    })),
  };
}

function bucketByRange<T extends { date: string }>(rows: T[], merge: (group: T[], key: string) => T): T[] {
  if (rows.length <= 42) return rows;
  const monthly = rows.length > 180;
  const groups = new Map<string, T[]>();
  for (const row of rows) {
    const key = monthly ? isoMonth(row.date) : isoWeekStart(row.date);
    const list = groups.get(key) ?? [];
    list.push(row);
    groups.set(key, list);
  }
  return [...groups.entries()].map(([key, group]) => merge(group, key));
}

export function bucketDailyStacks(days: UsageDay[], series: UsageSeries[]): DayStack[] {
  return bucketByRange(days.map(day => stackDay(day, series)), mergeStacks);
}

export function sparseAxisLabels(count: number): Set<number> {
  if (count <= 8) return new Set(Array.from({ length: count }, (_, i) => i));
  const last = count - 1;
  const step = Math.max(1, Math.ceil(last / 6));
  const marks = new Set<number>([0, last]);
  for (let i = step; i < last; i += step) marks.add(i);
  return marks;
}

export interface DayCostStack {
  date: string;
  label: string;
  requests: number;
  mixed: boolean;
  segments: Array<{ id: string; amount: number; color: string }>;
  cost: DayCostMeta;
}

const COST_COLORS: Record<string, string> = {
  exact: "var(--usage-cost-exact)",
  estimated: "var(--usage-cost-estimated)",
  lower_bound: "var(--usage-cost-lower)",
  stale: "var(--usage-cost-stale)",
};

function costSegmentsFromMeta(cost: DayCostMeta): DayCostStack["segments"] {
  return [
    { id: "exact", amount: cost.exactAmount, color: COST_COLORS.exact },
    { id: "estimated", amount: cost.estimatedAmount, color: COST_COLORS.estimated },
    { id: "lower_bound", amount: cost.lowerBoundAmount, color: COST_COLORS.lower_bound },
    { id: "stale", amount: cost.staleAmount, color: COST_COLORS.stale },
  ].filter(row => row.amount > 0);
}

function costMetaFromSummary(cost: UsageCostSummary | undefined): DayCostMeta {
  return {
    exactAmount: cost?.exact.amountUsd ?? 0,
    estimatedAmount: cost?.estimated.amountUsd ?? 0,
    lowerBoundAmount: cost?.lowerBound.amountUsd ?? 0,
    staleAmount: cost?.stale.amountUsd ?? 0,
    unpricedRequests: cost?.unpricedRequests ?? 0,
    unmeteredRequests: cost?.unmeteredRequests ?? 0,
  };
}

function pricedClassCount(cost: DayCostMeta): number {
  return [cost.exactAmount, cost.estimatedAmount, cost.lowerBoundAmount, cost.staleAmount]
    .filter(amount => amount > 0).length;
}

function stackCostDay(day: UsageDay): DayCostStack {
  const cost = costMetaFromSummary(day.cost);
  return {
    date: day.date,
    label: day.date,
    requests: day.requests,
    mixed: day.cost ? !day.cost.displayTotalSafe : true,
    segments: costSegmentsFromMeta(cost),
    cost,
  };
}

function mergeCostStacks(rows: DayCostStack[], label: string): DayCostStack {
  const cost: DayCostMeta = {
    exactAmount: rows.reduce((sum, row) => sum + row.cost.exactAmount, 0),
    estimatedAmount: rows.reduce((sum, row) => sum + row.cost.estimatedAmount, 0),
    lowerBoundAmount: rows.reduce((sum, row) => sum + row.cost.lowerBoundAmount, 0),
    staleAmount: rows.reduce((sum, row) => sum + row.cost.staleAmount, 0),
    unpricedRequests: rows.reduce((sum, row) => sum + row.cost.unpricedRequests, 0),
    unmeteredRequests: rows.reduce((sum, row) => sum + row.cost.unmeteredRequests, 0),
  };
  return {
    date: rows[0]?.date ?? label,
    label,
    requests: rows.reduce((sum, row) => sum + row.requests, 0),
    mixed: rows.some(row => row.mixed) || pricedClassCount(cost) > 1,
    segments: costSegmentsFromMeta(cost),
    cost,
  };
}

export function dailyCostStacks(days: UsageDay[]): DayCostStack[] {
  return bucketByRange(days.map(stackCostDay), mergeCostStacks);
}

export function pricedCostAmount(cost: DayCostMeta): number {
  return cost.exactAmount + cost.estimatedAmount + cost.lowerBoundAmount + cost.staleAmount;
}

export type CostTooltipHead =
  | { kind: "amount"; status: "exact" | "estimated" | "lower_bound" | "stale"; amount: number; prefix: "" | "≈" | "≥" }
  | { kind: "mixed" }
  | { kind: "classes" };

export function costTooltipHead(cost: DayCostMeta): CostTooltipHead {
  const classes: Array<Extract<CostTooltipHead, { kind: "amount" }>> = [];
  if (cost.exactAmount > 0) classes.push({ kind: "amount", status: "exact", amount: cost.exactAmount, prefix: "" });
  if (cost.estimatedAmount > 0) classes.push({ kind: "amount", status: "estimated", amount: cost.estimatedAmount, prefix: "≈" });
  if (cost.lowerBoundAmount > 0) classes.push({ kind: "amount", status: "lower_bound", amount: cost.lowerBoundAmount, prefix: "≥" });
  if (cost.staleAmount > 0) classes.push({ kind: "amount", status: "stale", amount: cost.staleAmount, prefix: "" });
  const gaps = cost.unpricedRequests + cost.unmeteredRequests;
  if (classes.length > 1) return { kind: "mixed" };
  if (classes.length === 1 && gaps === 0) return classes[0];
  return { kind: "classes" };
}
