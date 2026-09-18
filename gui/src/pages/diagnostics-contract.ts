/** Structured Diagnostics request API (`GET /api/diagnostics/requests`). */

export const DIAGNOSTICS_PAGE_SIZE = 200;
export const DIAGNOSTICS_MAX_LIMIT = 2000;
export const DIAGNOSTICS_PROTOCOLS = ["responses", "chat_completions", "anthropic_messages"] as const;
export const DIAGNOSTICS_TIME_RANGES = ["all", "15m", "1h", "24h"] as const;

export type DiagnosticsTimeRange = (typeof DIAGNOSTICS_TIME_RANGES)[number];
export type DiagnosticsProtocol = (typeof DIAGNOSTICS_PROTOCOLS)[number];

export type DiagnosticsListQuery = {
  cursor?: string;
  limit?: number;
  sessionId?: string;
  status?: number;
  protocol?: string;
  provider?: string;
  model?: string;
  requestId?: string;
  correlationId?: string;
};

export type DiagnosticsRequestSummary = {
  requestId: string;
  timestamp: string;
  sessionId?: string;
  protocol?: string;
  method: string;
  path: string;
  resolvedModel?: string;
  provider?: string;
  status: number;
  durationMs: number;
  totalTokens?: number;
  usageStatus?: string;
};

export type DiagnosticsRouting = {
  kind?: string;
  requestedModel?: string;
  requestedProvider?: string;
  resolvedModel?: string;
  provider?: string;
  policyId?: string;
  comboId?: string;
  committedMember?: string;
};

export type DiagnosticsAttempt = {
  ordinal: number;
  member?: string;
  status?: number;
  code?: string;
  decision?: string;
};

export type DiagnosticsTiming = {
  totalMs: number;
  headersMs?: number;
  firstByteMs?: number;
  ttftMs?: number;
  firstDownstreamMs?: number;
  upstreamEndMs?: number;
  downstreamEndMs?: number;
};

export type DiagnosticsTimelineEvent = {
  stage?: string;
  side?: string;
  milestone?: string;
  elapsedMs: number;
  attempt?: number;
  ok: boolean;
  normalizedCause?: string;
};

export type DiagnosticsFailure = {
  side?: string;
  stage?: string;
  cause?: string;
};

export type DiagnosticsUsage = {
  status: string;
  inputTokens?: number;
  cachedInputTokens?: number;
  cacheReadInputTokens?: number;
  cacheCreationInputTokens?: number;
  outputTokens?: number;
  reasoningOutputTokens?: number;
  totalTokens?: number;
};

export type DiagnosticsPriceInfo = {
  provider?: string;
  modelId?: string;
  source?: string;
  verifiedAt?: number;
  confidence?: string;
};

export type DiagnosticsCost = {
  kind: string;
  currency?: string;
  total?: number;
  reason?: string;
  price?: DiagnosticsPriceInfo;
};

export type DiagnosticsRequestDetail = {
  requestId: string;
  correlationId?: string;
  sessionId?: string;
  timestamp: string;
  protocol?: string;
  method: string;
  path: string;
  status: number;
  durationMs: number;
  requestBytes?: number;
  responseBytes?: number;
  errorCode?: string;
  routing?: DiagnosticsRouting;
  attempts: DiagnosticsAttempt[];
  timing: DiagnosticsTiming;
  timeline: DiagnosticsTimelineEvent[];
  failure?: DiagnosticsFailure;
  usage?: DiagnosticsUsage;
  cost?: DiagnosticsCost;
};

export type DiagnosticsListResponse = {
  requests: DiagnosticsRequestSummary[];
  nextCursor: string;
  reset: boolean;
  historyTruncated: boolean;
};

export type DiagnosticsToolbarFilters = {
  timeRange: DiagnosticsTimeRange;
  status: string;
  protocol: string;
  provider: string;
  model: string;
};

export const EMPTY_DIAGNOSTICS_FILTERS: DiagnosticsToolbarFilters = {
  timeRange: "all",
  status: "",
  protocol: "",
  provider: "",
  model: "",
};

function isObject(value: unknown): value is Record<string, unknown> {
  return Boolean(value) && typeof value === "object" && !Array.isArray(value);
}

function optionalString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function requiredString(value: unknown): string | undefined {
  return typeof value === "string" && value.trim() !== "" ? value : undefined;
}

function optionalNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function requiredInt(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function setQueryValue(params: URLSearchParams, key: string, value: string | number | undefined): void {
  if (typeof value === "number") {
    params.set(key, String(value));
    return;
  }
  const trimmed = value?.trim() ?? "";
  if (trimmed) params.set(key, trimmed);
}

export function diagnosticsListPath(query: DiagnosticsListQuery): string {
  const params = new URLSearchParams();
  const limit = query.limit && query.limit > 0
    ? Math.min(query.limit, DIAGNOSTICS_MAX_LIMIT)
    : DIAGNOSTICS_PAGE_SIZE;
  params.set("limit", String(limit));
  setQueryValue(params, "cursor", query.cursor);
  setQueryValue(params, "sessionId", query.sessionId);
  setQueryValue(params, "status", query.status);
  setQueryValue(params, "protocol", query.protocol);
  setQueryValue(params, "provider", query.provider);
  setQueryValue(params, "model", query.model);
  setQueryValue(params, "requestId", query.requestId);
  setQueryValue(params, "correlationId", query.correlationId);
  return `/api/diagnostics/requests?${params.toString()}`;
}

export function diagnosticsDetailPath(requestId: string): string {
  return `/api/diagnostics/requests/${encodeURIComponent(requestId)}`;
}

export function parseDiagnosticsSummary(value: unknown): DiagnosticsRequestSummary | null {
  if (!isObject(value)) return null;
  const requestId = requiredString(value.requestId);
  const timestamp = requiredString(value.timestamp);
  const method = requiredString(value.method);
  const path = requiredString(value.path);
  const status = requiredInt(value.status);
  const durationMs = requiredInt(value.durationMs);
  if (!requestId || !timestamp || !method || !path || status === undefined || durationMs === undefined) {
    return null;
  }
  return {
    requestId,
    timestamp,
    sessionId: optionalString(value.sessionId),
    protocol: optionalString(value.protocol),
    method,
    path,
    resolvedModel: optionalString(value.resolvedModel),
    provider: optionalString(value.provider),
    status,
    durationMs,
    totalTokens: optionalNumber(value.totalTokens),
    usageStatus: optionalString(value.usageStatus),
  };
}

function parseRouting(value: unknown): DiagnosticsRouting | undefined {
  if (!isObject(value)) return undefined;
  const routing: DiagnosticsRouting = {
    kind: optionalString(value.kind),
    requestedModel: optionalString(value.requestedModel),
    requestedProvider: optionalString(value.requestedProvider),
    resolvedModel: optionalString(value.resolvedModel),
    provider: optionalString(value.provider),
    policyId: optionalString(value.policyId),
    comboId: optionalString(value.comboId),
    committedMember: optionalString(value.committedMember),
  };
  return Object.values(routing).some(Boolean) ? routing : undefined;
}

function parseAttempt(value: unknown): DiagnosticsAttempt | null {
  if (!isObject(value)) return null;
  const ordinal = requiredInt(value.ordinal);
  if (ordinal === undefined) return null;
  return {
    ordinal,
    member: optionalString(value.member),
    status: optionalNumber(value.status),
    code: optionalString(value.code),
    decision: optionalString(value.decision),
  };
}

function parseTiming(value: unknown, fallbackTotalMs: number): DiagnosticsTiming {
  const src = isObject(value) ? value : {};
  return {
    totalMs: optionalNumber(src.totalMs) ?? fallbackTotalMs,
    headersMs: optionalNumber(src.headersMs),
    firstByteMs: optionalNumber(src.firstByteMs),
    ttftMs: optionalNumber(src.ttftMs),
    firstDownstreamMs: optionalNumber(src.firstDownstreamMs),
    upstreamEndMs: optionalNumber(src.upstreamEndMs),
    downstreamEndMs: optionalNumber(src.downstreamEndMs),
  };
}

function parseTimelineEvent(value: unknown): DiagnosticsTimelineEvent | null {
  if (!isObject(value)) return null;
  const elapsedMs = requiredInt(value.elapsedMs);
  if (elapsedMs === undefined || typeof value.ok !== "boolean") return null;
  return {
    stage: optionalString(value.stage),
    side: optionalString(value.side),
    milestone: optionalString(value.milestone),
    elapsedMs,
    attempt: optionalNumber(value.attempt),
    ok: value.ok,
    normalizedCause: optionalString(value.normalizedCause),
  };
}

function parseFailure(value: unknown): DiagnosticsFailure | undefined {
  if (!isObject(value)) return undefined;
  const failure: DiagnosticsFailure = {
    side: optionalString(value.side),
    stage: optionalString(value.stage),
    cause: optionalString(value.cause),
  };
  return Object.values(failure).some(Boolean) ? failure : undefined;
}

function parseUsage(value: unknown): DiagnosticsUsage | undefined {
  if (!isObject(value)) return undefined;
  const status = optionalString(value.status) ?? "unreported";
  return {
    status,
    inputTokens: optionalNumber(value.inputTokens),
    cachedInputTokens: optionalNumber(value.cachedInputTokens),
    cacheReadInputTokens: optionalNumber(value.cacheReadInputTokens),
    cacheCreationInputTokens: optionalNumber(value.cacheCreationInputTokens),
    outputTokens: optionalNumber(value.outputTokens),
    reasoningOutputTokens: optionalNumber(value.reasoningOutputTokens),
    totalTokens: optionalNumber(value.totalTokens),
  };
}

function parsePrice(value: unknown): DiagnosticsPriceInfo | undefined {
  if (!isObject(value)) return undefined;
  const price: DiagnosticsPriceInfo = {
    provider: optionalString(value.provider),
    modelId: optionalString(value.modelId),
    source: optionalString(value.source),
    verifiedAt: optionalNumber(value.verifiedAt),
    confidence: optionalString(value.confidence),
  };
  return Object.values(price).some(v => v !== undefined) ? price : undefined;
}

function parseCost(value: unknown): DiagnosticsCost | undefined {
  if (!isObject(value)) return undefined;
  const kind = optionalString(value.kind);
  if (!kind) return undefined;
  return {
    kind,
    currency: optionalString(value.currency),
    total: optionalNumber(value.total),
    reason: optionalString(value.reason),
    price: parsePrice(value.price),
  };
}

export function parseDiagnosticsDetail(value: unknown): DiagnosticsRequestDetail | null {
  const summary = parseDiagnosticsSummary(value);
  if (!summary || !isObject(value)) return null;
  const attempts = Array.isArray(value.attempts)
    ? value.attempts.map(parseAttempt).filter((row): row is DiagnosticsAttempt => row !== null)
    : [];
  const timeline = Array.isArray(value.timeline)
    ? value.timeline.map(parseTimelineEvent).filter((row): row is DiagnosticsTimelineEvent => row !== null)
    : [];
  return {
    ...summary,
    correlationId: optionalString(value.correlationId),
    requestBytes: optionalNumber(value.requestBytes),
    responseBytes: optionalNumber(value.responseBytes),
    errorCode: optionalString(value.errorCode),
    routing: parseRouting(value.routing),
    attempts,
    timing: parseTiming(value.timing, summary.durationMs),
    timeline,
    failure: parseFailure(value.failure),
    usage: parseUsage(value.usage),
    cost: parseCost(value.cost),
  };
}

export function parseDiagnosticsList(value: unknown): DiagnosticsListResponse | null {
  if (!isObject(value) || !Array.isArray(value.requests)) return null;
  const requests = value.requests
    .map(parseDiagnosticsSummary)
    .filter((row): row is DiagnosticsRequestSummary => row !== null);
  return {
    requests,
    nextCursor: typeof value.nextCursor === "string" ? value.nextCursor : "",
    reset: value.reset === true,
    historyTruncated: value.historyTruncated === true,
  };
}

export function sortDiagnosticsNewestFirst(rows: DiagnosticsRequestSummary[]): DiagnosticsRequestSummary[] {
  return rows.toSorted((left, right) => {
    const delta = Date.parse(right.timestamp) - Date.parse(left.timestamp);
    if (delta !== 0) return delta;
    return right.requestId.localeCompare(left.requestId);
  });
}

export function mergeDiagnosticsRows(
  previous: DiagnosticsRequestSummary[],
  incoming: DiagnosticsRequestSummary[],
  mode: "snapshot" | "incremental",
): DiagnosticsRequestSummary[] {
  if (mode === "snapshot") return sortDiagnosticsNewestFirst(incoming).slice(0, DIAGNOSTICS_MAX_LIMIT);
  const byId = new Map<string, DiagnosticsRequestSummary>();
  for (const row of previous) byId.set(row.requestId, row);
  for (const row of incoming) byId.set(row.requestId, row);
  return sortDiagnosticsNewestFirst([...byId.values()]).slice(0, DIAGNOSTICS_MAX_LIMIT);
}

export function timeRangeMs(range: DiagnosticsTimeRange): number | null {
  if (range === "15m") return 15 * 60 * 1000;
  if (range === "1h") return 60 * 60 * 1000;
  if (range === "24h") return 24 * 60 * 60 * 1000;
  return null;
}

export function rowInTimeRange(row: DiagnosticsRequestSummary, range: DiagnosticsTimeRange, nowMs: number): boolean {
  const windowMs = timeRangeMs(range);
  if (windowMs === null) return true;
  const at = Date.parse(row.timestamp);
  if (!Number.isFinite(at)) return false;
  return nowMs - at <= windowMs;
}

export function uniqueSorted(values: Array<string | undefined>): string[] {
  return [...new Set(values.filter((value): value is string => Boolean(value)))].sort((left, right) => left.localeCompare(right));
}

export function toolbarFiltersActive(filters: DiagnosticsToolbarFilters): boolean {
  return filters.timeRange !== "all"
    || filters.status !== ""
    || filters.protocol !== ""
    || filters.provider !== ""
    || filters.model !== "";
}

export function serverListQueryFromFilters(
  filters: DiagnosticsToolbarFilters,
  sessionId: string,
  extras?: { cursor?: string; limit?: number },
): DiagnosticsListQuery {
  const status = filters.status.trim() === "" ? undefined : Number(filters.status);
  return {
    cursor: extras?.cursor,
    limit: extras?.limit,
    sessionId: sessionId.trim() || undefined,
    status: status !== undefined && Number.isInteger(status) ? status : undefined,
    protocol: filters.protocol.trim() || undefined,
    provider: filters.provider.trim() || undefined,
    model: filters.model.trim() || undefined,
  };
}

export function splitDuration(ms: number): { amount: string; unit: "ms" | "s" } | null {
  if (!Number.isFinite(ms) || ms < 0) return null;
  if (ms < 1000) return { amount: String(Math.round(ms)), unit: "ms" };
  const seconds = ms / 1000;
  const digits = seconds >= 10 ? 1 : 2;
  const amount = seconds.toFixed(digits).replace(/\.0+$/, "").replace(/(\.\d*?)0+$/, "$1");
  return { amount, unit: "s" };
}

export function formatDurationMs(ms: number, secondLabel: string): string {
  const split = splitDuration(ms);
  if (!split) return "\u2014";
  return split.unit === "ms" ? `${split.amount}ms` : `${split.amount}${secondLabel}`;
}

/** Format a recorded duration. `0` stays `0ms`; missing/invalid values stay absent. */
export function formatRecordedDurationMs(ms: number | undefined, secondLabel: string): string | undefined {
  if (typeof ms !== "number" || !Number.isFinite(ms) || ms < 0) return undefined;
  return formatDurationMs(ms, secondLabel);
}

export function formatByteCount(bytes: number, locale?: string): string {
  if (!Number.isFinite(bytes) || bytes < 0) return "\u2014";
  return new Intl.NumberFormat(locale).format(Math.round(bytes));
}

export function formatDiagnosticsCost(cost: DiagnosticsCost | undefined, locale?: string): string | undefined {
  if (!cost || typeof cost.total !== "number" || !Number.isFinite(cost.total) || !cost.currency) return undefined;
  const amount = new Intl.NumberFormat(locale, {
    minimumFractionDigits: 4,
    maximumFractionDigits: 4,
  }).format(cost.total);
  try {
    const money = new Intl.NumberFormat(locale, {
      style: "currency",
      currency: cost.currency,
      minimumFractionDigits: 4,
      maximumFractionDigits: 4,
    }).format(cost.total);
    return `${money} ${cost.currency}`;
  } catch {
    return `${amount} ${cost.currency}`;
  }
}

export function parseTimestampMs(value: string): number | undefined {
  const ms = Date.parse(value);
  return Number.isFinite(ms) ? ms : undefined;
}

export function isDiagnosticsTimeRange(value: string): value is DiagnosticsTimeRange {
  return (DIAGNOSTICS_TIME_RANGES as readonly string[]).includes(value);
}

export function nextSnapshotLimit(current: number): number {
  return Math.min(current + DIAGNOSTICS_PAGE_SIZE, DIAGNOSTICS_MAX_LIMIT);
}

export function snapshotHasOlder(returnedCount: number, limit: number): boolean {
  return returnedCount >= limit && limit < DIAGNOSTICS_MAX_LIMIT;
}
