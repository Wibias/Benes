/** Frontend contract for GET /api/usage, matching internal/usage.Summary. */

export type UsageRange = "all" | "30d" | "7d" | "today" | "yesterday" | "custom";
export type UsageSurface = "all" | "codex" | "claude" | "grok";
export type UsageBoardTab = "overview" | "breakdown" | "coverage";
export type UsageBreakdownTab = "models" | "providers" | "accounts";

export interface UsageCostBucket {
  amountUsd: number;
  requests: number;
}

export interface UsageCostSummary {
  currency?: string;
  exact: UsageCostBucket;
  estimated: UsageCostBucket;
  lowerBound: UsageCostBucket;
  stale: UsageCostBucket;
  pricedRequests: number;
  unpricedRequests: number;
  unmeteredRequests: number;
  displayTotalSafe: boolean;
  status?: string;
}

export interface UsageSummaryTotals {
  requests: number;
  attemptCount: number;
  measuredRequests: number;
  reportedRequests: number;
  unreportedRequests: number;
  unsupportedRequests: number;
  estimatedRequests: number;
  inputTokens: number;
  outputTokens: number;
  cachedInputTokens?: number;
  cacheReadInputTokens?: number;
  cacheCreationInputTokens?: number;
  reasoningOutputTokens: number;
  totalTokens: number;
  coverageRatio: number;
  estimatedCostUsd?: number;
  exactCostUsd?: number;
  lowerBoundCostUsd?: number;
  staleCostUsd?: number;
  pricedRequests?: number;
  unpricedRequests?: number;
  unmeteredRequests?: number;
}

export interface UsageDayModel {
  model: string;
  provider: string;
  requests: number;
  totalTokens: number;
}

export interface UsageDay {
  date: string;
  requests: number;
  measuredRequests: number;
  reportedRequests: number;
  totalTokens: number;
  cost?: UsageCostSummary;
  models: UsageDayModel[];
}

export interface UsageModel {
  provider: string;
  model: string;
  requests: number;
  attemptCount: number;
  measuredRequests: number;
  reportedRequests: number;
  estimatedRequests: number;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  shareRatio: number;
  cost?: UsageCostSummary;
}

export interface UsageProvider {
  provider: string;
  requests: number;
  attemptCount: number;
  measuredRequests: number;
  reportedRequests: number;
  estimatedRequests: number;
  totalTokens: number;
  shareRatio: number;
  cost?: UsageCostSummary;
}

export interface UsageAccount {
  account: string;
  accountLogLabel: string;
  requests: number;
  measuredRequests: number;
  reportedRequests: number;
  totalTokens: number;
  usageCoverageRatio: number;
  cost?: UsageCostSummary;
}

export interface UsageSurfaceAttribution {
  codex: number;
  claude: number;
  claudeDesktop: number;
  grok: number;
  unattributed: number;
}

export interface UsageResponse {
  range: UsageRange;
  surface: UsageSurface;
  since: number | null;
  until?: number | null;
  generatedAt: number;
  summary: UsageSummaryTotals;
  days: UsageDay[];
  models: UsageModel[];
  providers: UsageProvider[];
  accounts: UsageAccount[];
  historyTruncated: boolean;
  truncatedPrefixBytes: number;
  snapshotWindowStart?: number | null;
  snapshotWindowEnd?: number | null;
  surfaceAttribution?: UsageSurfaceAttribution;
  cost?: UsageCostSummary;
  error?: string;
}

const RANGES: readonly UsageRange[] = ["all", "30d", "7d", "today", "yesterday", "custom"];
const SURFACES: readonly UsageSurface[] = ["all", "codex", "claude", "grok"];

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function asInt(value: unknown): number {
  const n = asNumber(value);
  return n === undefined ? 0 : n;
}

function asOptionalInt(value: unknown): number | undefined {
  return asNumber(value);
}

function asString(value: unknown): string {
  return typeof value === "string" ? value : "";
}

function asBool(value: unknown): boolean {
  return value === true;
}

function parseBucket(value: unknown): UsageCostBucket {
  if (!isRecord(value)) return { amountUsd: 0, requests: 0 };
  return { amountUsd: asInt(value.amountUsd), requests: asInt(value.requests) };
}

export function parseUsageCost(value: unknown): UsageCostSummary | undefined {
  if (!isRecord(value)) return undefined;
  return {
    currency: asString(value.currency) || undefined,
    exact: parseBucket(value.exact),
    estimated: parseBucket(value.estimated),
    lowerBound: parseBucket(value.lowerBound),
    stale: parseBucket(value.stale),
    pricedRequests: asInt(value.pricedRequests),
    unpricedRequests: asInt(value.unpricedRequests),
    unmeteredRequests: asInt(value.unmeteredRequests),
    displayTotalSafe: asBool(value.displayTotalSafe),
    status: asString(value.status) || undefined,
  };
}

function parseSummary(value: unknown): UsageSummaryTotals {
  const row = isRecord(value) ? value : {};
  return {
    requests: asInt(row.requests),
    attemptCount: asInt(row.attemptCount),
    measuredRequests: asInt(row.measuredRequests),
    reportedRequests: asInt(row.reportedRequests),
    unreportedRequests: asInt(row.unreportedRequests),
    unsupportedRequests: asInt(row.unsupportedRequests),
    estimatedRequests: asInt(row.estimatedRequests),
    inputTokens: asInt(row.inputTokens),
    outputTokens: asInt(row.outputTokens),
    cachedInputTokens: asOptionalInt(row.cachedInputTokens),
    cacheReadInputTokens: asOptionalInt(row.cacheReadInputTokens),
    cacheCreationInputTokens: asOptionalInt(row.cacheCreationInputTokens),
    reasoningOutputTokens: asInt(row.reasoningOutputTokens),
    totalTokens: asInt(row.totalTokens),
    coverageRatio: asNumber(row.coverageRatio) ?? 0,
    estimatedCostUsd: asOptionalInt(row.estimatedCostUsd),
    exactCostUsd: asOptionalInt(row.exactCostUsd),
    lowerBoundCostUsd: asOptionalInt(row.lowerBoundCostUsd),
    staleCostUsd: asOptionalInt(row.staleCostUsd),
    pricedRequests: asOptionalInt(row.pricedRequests),
    unpricedRequests: asOptionalInt(row.unpricedRequests),
    unmeteredRequests: asOptionalInt(row.unmeteredRequests),
  };
}

function parseDayModel(value: unknown): UsageDayModel | null {
  if (!isRecord(value)) return null;
  return {
    model: asString(value.model),
    provider: asString(value.provider),
    requests: asInt(value.requests),
    totalTokens: asInt(value.totalTokens),
  };
}

function parseDay(value: unknown): UsageDay | null {
  if (!isRecord(value) || typeof value.date !== "string" || value.date === "") return null;
  const models = Array.isArray(value.models)
    ? value.models.map(parseDayModel).filter((row): row is UsageDayModel => row !== null)
    : [];
  return {
    date: value.date,
    requests: asInt(value.requests),
    measuredRequests: asInt(value.measuredRequests),
    reportedRequests: asInt(value.reportedRequests),
    totalTokens: asInt(value.totalTokens),
    cost: parseUsageCost(value.cost),
    models,
  };
}

function parseModel(value: unknown): UsageModel | null {
  if (!isRecord(value)) return null;
  return {
    provider: asString(value.provider),
    model: asString(value.model),
    requests: asInt(value.requests),
    attemptCount: asInt(value.attemptCount),
    measuredRequests: asInt(value.measuredRequests),
    reportedRequests: asInt(value.reportedRequests),
    estimatedRequests: asInt(value.estimatedRequests),
    totalTokens: asInt(value.totalTokens),
    inputTokens: asInt(value.inputTokens),
    outputTokens: asInt(value.outputTokens),
    shareRatio: asNumber(value.shareRatio) ?? 0,
    cost: parseUsageCost(value.cost),
  };
}

function parseProvider(value: unknown): UsageProvider | null {
  if (!isRecord(value)) return null;
  return {
    provider: asString(value.provider),
    requests: asInt(value.requests),
    attemptCount: asInt(value.attemptCount),
    measuredRequests: asInt(value.measuredRequests),
    reportedRequests: asInt(value.reportedRequests),
    estimatedRequests: asInt(value.estimatedRequests),
    totalTokens: asInt(value.totalTokens),
    shareRatio: asNumber(value.shareRatio) ?? 0,
    cost: parseUsageCost(value.cost),
  };
}

function parseAccount(value: unknown): UsageAccount | null {
  if (!isRecord(value)) return null;
  const label = asString(value.accountLogLabel) || asString(value.account);
  if (!label) return null;
  return {
    account: asString(value.account),
    accountLogLabel: label,
    requests: asInt(value.requests),
    measuredRequests: asInt(value.measuredRequests),
    reportedRequests: asInt(value.reportedRequests),
    totalTokens: asInt(value.totalTokens),
    usageCoverageRatio: asNumber(value.usageCoverageRatio) ?? 0,
    cost: parseUsageCost(value.cost),
  };
}

function parseSurfaceAttribution(value: unknown): UsageSurfaceAttribution | undefined {
  if (!isRecord(value)) return undefined;
  return {
    codex: asInt(value.codex),
    claude: asInt(value.claude),
    claudeDesktop: asInt(value.claudeDesktop),
    grok: asInt(value.grok),
    unattributed: asInt(value.unattributed),
  };
}

export function parseUsageResponse(value: unknown): UsageResponse | null {
  if (!isRecord(value)) return null;
  const range = RANGES.includes(value.range as UsageRange) ? value.range as UsageRange : "30d";
  const surface = SURFACES.includes(value.surface as UsageSurface) ? value.surface as UsageSurface : "all";
  return {
    range,
    surface,
    since: asOptionalInt(value.since) ?? null,
    until: asOptionalInt(value.until) ?? null,
    generatedAt: asInt(value.generatedAt),
    summary: parseSummary(value.summary),
    days: Array.isArray(value.days)
      ? value.days.map(parseDay).filter((row): row is UsageDay => row !== null)
      : [],
    models: Array.isArray(value.models)
      ? value.models.map(parseModel).filter((row): row is UsageModel => row !== null)
      : [],
    providers: Array.isArray(value.providers)
      ? value.providers.map(parseProvider).filter((row): row is UsageProvider => row !== null)
      : [],
    accounts: Array.isArray(value.accounts)
      ? value.accounts.map(parseAccount).filter((row): row is UsageAccount => row !== null)
      : [],
    historyTruncated: asBool(value.historyTruncated),
    truncatedPrefixBytes: asInt(value.truncatedPrefixBytes),
    snapshotWindowStart: asOptionalInt(value.snapshotWindowStart) ?? null,
    snapshotWindowEnd: asOptionalInt(value.snapshotWindowEnd) ?? null,
    surfaceAttribution: parseSurfaceAttribution(value.surfaceAttribution),
    cost: parseUsageCost(value.cost),
    error: asString(value.error) || undefined,
  };
}

export function cacheReadTokens(summary: UsageSummaryTotals): number | undefined {
  return summary.cacheReadInputTokens ?? summary.cachedInputTokens;
}

export function usageReadFailed(data: UsageResponse | null): boolean {
  return data?.error === "read_failed";
}

export function usageTabFromPath(path: string): UsageBoardTab {
  if (path === "usage/breakdown" || path.startsWith("usage/breakdown/")) return "breakdown";
  if (path === "usage/coverage") return "coverage";
  return "overview";
}

export function usageBreakdownFromPath(path: string): UsageBreakdownTab {
  if (path === "usage/breakdown/providers") return "providers";
  if (path === "usage/breakdown/accounts") return "accounts";
  return "models";
}

export function usageTabHash(tab: UsageBoardTab, breakdown: UsageBreakdownTab = "models"): string {
  if (tab === "coverage") return "usage/coverage";
  if (tab === "breakdown") {
    if (breakdown === "providers") return "usage/breakdown/providers";
    if (breakdown === "accounts") return "usage/breakdown/accounts";
    return "usage/breakdown";
  }
  return "usage";
}
