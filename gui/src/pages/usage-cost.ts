import type { UsageCostSummary } from "./usage-contract.ts";

export type CostStatusKey =
  | "exact"
  | "estimated"
  | "lower_bound"
  | "stale"
  | "mixed"
  | "partial"
  | "unavailable";

export type CostPresentation =
  | { kind: "amount"; status: "exact" | "estimated" | "lower_bound" | "stale"; amount: number; prefix: "" | "≈" | "≥" }
  | { kind: "mixed" }
  | { kind: "partial"; gapRequests: number }
  | { kind: "unavailable" };

const SAFE_STATUSES = new Set(["exact", "estimated", "lower_bound", "stale"]);

function pricedClasses(cost: UsageCostSummary): number {
  return [cost.exact, cost.estimated, cost.lowerBound, cost.stale].filter(bucket => bucket.requests > 0).length;
}

function amountForStatus(cost: UsageCostSummary): number | undefined {
  if (cost.status === "exact") return cost.exact.amountUsd;
  if (cost.status === "estimated") return cost.estimated.amountUsd;
  if (cost.status === "lower_bound") return cost.lowerBound.amountUsd;
  if (cost.status === "stale") return cost.stale.amountUsd;
  return undefined;
}

export const COST_EMPTY_PROXY = 0;
export const COST_EMPTY_NO_PRICE = 1;
export const COST_EMPTY_NO_METER = 2;

export type CostChartState =
  | { kind: "empty"; source: typeof COST_EMPTY_PROXY; showConfigureHint: false }
  | { kind: "empty"; source: typeof COST_EMPTY_NO_PRICE; count: number; showConfigureHint: true }
  | { kind: "empty"; source: typeof COST_EMPTY_NO_METER; count: number; showConfigureHint: false }
  | { kind: "partial"; gapRequests: number }
  | { kind: "chart" };

export function costChartState(
  cost: UsageCostSummary | undefined,
  requestCount: number,
): CostChartState {
  if (!cost) {
    if (requestCount <= 0) return { kind: "chart" };
    return { kind: "empty", source: COST_EMPTY_PROXY, showConfigureHint: false };
  }
  const priced = cost.pricedRequests;
  const unpriced = cost.unpricedRequests;
  const unmetered = cost.unmeteredRequests;
  const gaps = unpriced + unmetered;
  if (priced <= 0) {
    if (unpriced > 0) return { kind: "empty", source: COST_EMPTY_NO_PRICE, count: unpriced, showConfigureHint: true };
    if (unmetered > 0) return { kind: "empty", source: COST_EMPTY_NO_METER, count: unmetered, showConfigureHint: false };
    if (requestCount <= 0 && gaps <= 0) return { kind: "chart" };
    return { kind: "empty", source: COST_EMPTY_PROXY, showConfigureHint: false };
  }
  if (gaps > 0) return { kind: "partial", gapRequests: gaps };
  return { kind: "chart" };
}

export function presentCost(cost: UsageCostSummary | undefined): CostPresentation {
  if (!cost) return { kind: "unavailable" };
  const gaps = cost.unpricedRequests + cost.unmeteredRequests;
  const classes = pricedClasses(cost);
  if (cost.displayTotalSafe && cost.status && SAFE_STATUSES.has(cost.status)) {
    const amount = amountForStatus(cost);
    if (typeof amount === "number" && Number.isFinite(amount)) {
      const prefix = cost.status === "estimated" ? "≈" : cost.status === "lower_bound" ? "≥" : "";
      return {
        kind: "amount",
        status: cost.status as "exact" | "estimated" | "lower_bound" | "stale",
        amount,
        prefix,
      };
    }
  }
  if (cost.status === "mixed" || classes > 1) return { kind: "mixed" };
  if (gaps > 0) return { kind: "partial", gapRequests: gaps };
  return { kind: "unavailable" };
}

export function formatUsageUsd(amount: number, locale?: string): string {
  if (!Number.isFinite(amount) || amount < 0) return "\u2014";
  const digits = amount >= 1 ? 2 : amount >= 0.01 ? 2 : 4;
  try {
    return new Intl.NumberFormat(locale, {
      style: "currency",
      currency: "USD",
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
    }).format(amount);
  } catch {
    return `$${amount.toFixed(digits)}`;
  }
}

export function formatPresentedAmount(presentation: Extract<CostPresentation, { kind: "amount" }>, locale?: string): string {
  return `${presentation.prefix}${formatUsageUsd(presentation.amount, locale)}`;
}

export function costStatusKey(presentation: CostPresentation): CostStatusKey {
  if (presentation.kind === "amount") return presentation.status;
  return presentation.kind;
}

export function costClassRows(cost: UsageCostSummary): Array<{
  id: "exact" | "estimated" | "lower_bound" | "stale" | "unpriced" | "unmetered";
  requests: number;
  amountUsd: number | undefined;
}> {
  return [
    { id: "exact", requests: cost.exact.requests, amountUsd: cost.exact.requests > 0 ? cost.exact.amountUsd : undefined },
    { id: "estimated", requests: cost.estimated.requests, amountUsd: cost.estimated.requests > 0 ? cost.estimated.amountUsd : undefined },
    { id: "lower_bound", requests: cost.lowerBound.requests, amountUsd: cost.lowerBound.requests > 0 ? cost.lowerBound.amountUsd : undefined },
    { id: "stale", requests: cost.stale.requests, amountUsd: cost.stale.requests > 0 ? cost.stale.amountUsd : undefined },
    { id: "unpriced", requests: cost.unpricedRequests, amountUsd: undefined },
    { id: "unmetered", requests: cost.unmeteredRequests, amountUsd: undefined },
  ];
}
