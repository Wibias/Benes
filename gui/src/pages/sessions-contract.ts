/** Benes dashboard client for the Go proxy (`internal/server`). */
/** Sessions list/detail contract helpers. Display-only — no surface/client inference. */

export const SESSION_SEARCH_DEBOUNCE_MS = 250;

export type SessionProtocolId = "responses" | "chat_completions" | "anthropic_messages";

export type SessionSummary = {
  id: string;
  namespace: string;
  externalId?: string;
  startedAt: string;
  lastActivityAt: string;
  requestCount: number;
  protocols: string[];
};

export type SessionFieldCoverage = {
  value?: number;
  attributedRequests: number;
  totalRequests: number;
  complete: boolean;
};

export type SessionCostCoverage = SessionFieldCoverage & {
  currency?: string;
  currencies: string[];
};

export type SessionAggregates = {
  usage: {
    inputTokens: SessionFieldCoverage;
    cachedInputTokens: SessionFieldCoverage;
    outputTokens: SessionFieldCoverage;
    totalTokens: SessionFieldCoverage;
    cost: SessionCostCoverage;
  };
  protocols: string[];
  models: string[];
  providers: string[];
  policyIds: string[];
  comboIds: string[];
  failoverRequestCount: number;
  hadFailover: boolean;
};

export type SessionDetail = {
  session: SessionSummary;
  aggregates: SessionAggregates;
};

export type SessionListResult = {
  sessions: SessionSummary[];
  hasMore: boolean;
  nextCursor?: string;
};

export type SessionFilterValues = {
  protocols: string[];
  namespaces: string[];
  providers: string[];
  models: string[];
  policyIds: string[];
  comboIds: string[];
};

export type SessionListQuery = {
  q?: string;
  namespace?: string;
  protocol?: string;
  provider?: string;
  model?: string;
  policy?: string;
  combo?: string;
  cursor?: string;
  limit?: number;
};

export type CoverageView = {
  valueText: string | null;
  attributedLine: boolean;
  attributedRequests: number;
  totalRequests: number;
};

const PROTOCOL_LABELS: Record<SessionProtocolId, string> = {
  responses: "Responses",
  chat_completions: "Chat Completions",
  anthropic_messages: "Messages",
};

export function isKnownSessionProtocol(value: string): value is SessionProtocolId {
  return value === "responses" || value === "chat_completions" || value === "anthropic_messages";
}

/** Presentation-only protocol labels. Never infers Codex/Claude/OpenAI. */
export function protocolDisplayLabel(protocol: string): string {
  if (isKnownSessionProtocol(protocol)) return PROTOCOL_LABELS[protocol];
  return protocol;
}

export function protocolSummaryLabel(protocols: string[]): string {
  if (protocols.length === 0) return "";
  return protocols.map(protocolDisplayLabel).join(", ");
}

export function sessionListIdentity(row: SessionSummary): { primary: string; secondary: string } {
  return {
    primary: row.namespace,
    secondary: row.externalId && row.externalId.trim() !== "" ? row.externalId : row.id,
  };
}

export function joinIdList(ids: string[]): string | null {
  const values = ids.map(id => id.trim()).filter(Boolean);
  return values.length === 0 ? null : values.join(", ");
}

export function coverageView(field: SessionFieldCoverage, formatValue: (value: number) => string): CoverageView {
  const hasValue = typeof field.value === "number" && Number.isFinite(field.value);
  return {
    valueText: hasValue ? formatValue(field.value as number) : null,
    attributedLine: !field.complete && (hasValue || field.totalRequests > 0),
    attributedRequests: field.attributedRequests,
    totalRequests: field.totalRequests,
  };
}

export function formatExactCount(value: number, locale: string): string {
  return new Intl.NumberFormat(locale).format(value);
}

export function formatSessionCost(cost: SessionCostCoverage, locale: string): CoverageView {
  const currencies = cost.currencies.filter(code => code.trim() !== "");
  if (currencies.length > 1) {
    return {
      valueText: null,
      attributedLine: !cost.complete,
      attributedRequests: cost.attributedRequests,
      totalRequests: cost.totalRequests,
    };
  }
  const currency = (cost.currency ?? currencies[0] ?? "").trim();
  const hasValue = typeof cost.value === "number" && Number.isFinite(cost.value) && currency !== "";
  return {
    valueText: hasValue ? formatCurrencyAmount(cost.value as number, currency, locale) : null,
    attributedLine: !cost.complete && (hasValue || cost.totalRequests > 0),
    attributedRequests: cost.attributedRequests,
    totalRequests: cost.totalRequests,
  };
}

export function formatCurrencyAmount(value: number, currency: string, locale: string): string {
  const amount = new Intl.NumberFormat(locale, {
    minimumFractionDigits: 4,
    maximumFractionDigits: 4,
  }).format(value);
  try {
    const money = new Intl.NumberFormat(locale, {
      style: "currency",
      currency,
      minimumFractionDigits: 4,
      maximumFractionDigits: 4,
    }).format(value);
    return `${money} ${currency}`;
  } catch {
    return `${amount} ${currency}`;
  }
}

export function formatSessionDate(iso: string, locale: string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return iso;
  return new Date(ms).toLocaleDateString(locale, { timeZone: "UTC", month: "short", day: "numeric", year: "numeric" });
}

export function formatSessionTimestamp(iso: string, locale: string, utcLabel?: string): string {
  const ms = Date.parse(iso);
  if (!Number.isFinite(ms)) return iso;
  const date = new Date(ms);
  const text = `${date.toLocaleDateString(locale, { timeZone: "UTC", month: "short", day: "numeric", year: "numeric" })} ${date.toLocaleTimeString(locale, { timeZone: "UTC", hour: "2-digit", minute: "2-digit", second: "2-digit", hour12: false })}`;
  return utcLabel ? `${text} ${utcLabel}` : text;
}

export function appendQuery(params: URLSearchParams, key: string, value: string | undefined): void {
  const trimmed = value?.trim() ?? "";
  if (trimmed) params.set(key, trimmed);
}

export function sessionsListPath(query: SessionListQuery): string {
  const params = new URLSearchParams();
  appendQuery(params, "q", query.q);
  appendQuery(params, "namespace", query.namespace);
  appendQuery(params, "protocol", query.protocol);
  appendQuery(params, "provider", query.provider);
  appendQuery(params, "model", query.model);
  appendQuery(params, "policy", query.policy);
  appendQuery(params, "combo", query.combo);
  appendQuery(params, "cursor", query.cursor);
  if (query.limit && query.limit > 0) params.set("limit", String(query.limit));
  const encoded = params.toString();
  return encoded ? `/api/sessions?${encoded}` : "/api/sessions";
}

export function sessionDetailPath(id: string): string {
  const params = new URLSearchParams();
  params.set("limit", "1");
  return `/api/sessions/${encodeURIComponent(id)}?${params.toString()}`;
}

export function sessionFiltersPath(): string {
  return "/api/sessions/filters";
}

export function apiErrorCode(body: unknown): string | undefined {
  if (!body || typeof body !== "object") return undefined;
  const error = (body as { error?: { code?: unknown } }).error;
  return typeof error?.code === "string" ? error.code : undefined;
}

export function isStoreUnavailable(code: string | undefined, status: number): boolean {
  return code === "store_unavailable" || status === 503;
}

export function parseStringList(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string");
}

export function parseSessionSummary(raw: unknown): SessionSummary | null {
  if (!raw || typeof raw !== "object") return null;
  const row = raw as Record<string, unknown>;
  if (typeof row.id !== "string" || typeof row.namespace !== "string") return null;
  if (typeof row.startedAt !== "string" || typeof row.lastActivityAt !== "string") return null;
  if (typeof row.requestCount !== "number") return null;
  const summary: SessionSummary = {
    id: row.id,
    namespace: row.namespace,
    startedAt: row.startedAt,
    lastActivityAt: row.lastActivityAt,
    requestCount: row.requestCount,
    protocols: parseStringList(row.protocols),
  };
  if (typeof row.externalId === "string" && row.externalId !== "") summary.externalId = row.externalId;
  return summary;
}

function parseFieldCoverage(raw: unknown): SessionFieldCoverage {
  const row = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
  const out: SessionFieldCoverage = {
    attributedRequests: typeof row.attributedRequests === "number" ? row.attributedRequests : 0,
    totalRequests: typeof row.totalRequests === "number" ? row.totalRequests : 0,
    complete: row.complete === true,
  };
  if (typeof row.value === "number" && Number.isFinite(row.value)) out.value = row.value;
  return out;
}

function parseCostCoverage(raw: unknown): SessionCostCoverage {
  const row = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
  const out: SessionCostCoverage = {
    ...parseFieldCoverage(raw),
    currencies: parseStringList(row.currencies),
  };
  if (typeof row.currency === "string" && row.currency !== "") out.currency = row.currency;
  return out;
}

export function parseSessionListResult(raw: unknown): SessionListResult {
  const row = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
  const sessions = Array.isArray(row.sessions)
    ? row.sessions.map(parseSessionSummary).filter((item): item is SessionSummary => item !== null)
    : [];
  const result: SessionListResult = { sessions, hasMore: row.hasMore === true };
  if (typeof row.nextCursor === "string" && row.nextCursor !== "") result.nextCursor = row.nextCursor;
  return result;
}

export function parseSessionFilterValues(raw: unknown): SessionFilterValues {
  const row = raw && typeof raw === "object" ? raw as Record<string, unknown> : {};
  return {
    protocols: parseStringList(row.protocols),
    namespaces: parseStringList(row.namespaces),
    providers: parseStringList(row.providers),
    models: parseStringList(row.models),
    policyIds: parseStringList(row.policyIds),
    comboIds: parseStringList(row.comboIds),
  };
}

export function parseSessionDetail(raw: unknown): SessionDetail | null {
  if (!raw || typeof raw !== "object") return null;
  const row = raw as Record<string, unknown>;
  const session = parseSessionSummary(row.session);
  if (!session) return null;
  const aggregatesRaw = row.aggregates && typeof row.aggregates === "object"
    ? row.aggregates as Record<string, unknown>
    : {};
  const usageRaw = aggregatesRaw.usage && typeof aggregatesRaw.usage === "object"
    ? aggregatesRaw.usage as Record<string, unknown>
    : {};
  return {
    session,
    aggregates: {
      usage: {
        inputTokens: parseFieldCoverage(usageRaw.inputTokens),
        cachedInputTokens: parseFieldCoverage(usageRaw.cachedInputTokens),
        outputTokens: parseFieldCoverage(usageRaw.outputTokens),
        totalTokens: parseFieldCoverage(usageRaw.totalTokens),
        cost: parseCostCoverage(usageRaw.cost),
      },
      protocols: parseStringList(aggregatesRaw.protocols),
      models: parseStringList(aggregatesRaw.models),
      providers: parseStringList(aggregatesRaw.providers),
      policyIds: parseStringList(aggregatesRaw.policyIds),
      comboIds: parseStringList(aggregatesRaw.comboIds),
      failoverRequestCount: typeof aggregatesRaw.failoverRequestCount === "number"
        ? aggregatesRaw.failoverRequestCount
        : 0,
      hadFailover: aggregatesRaw.hadFailover === true,
    },
  };
}

export function sessionFiltersActive(query: SessionListQuery): boolean {
  return Boolean(
    query.namespace?.trim()
    || query.protocol?.trim()
    || query.provider?.trim()
    || query.model?.trim()
    || query.policy?.trim()
    || query.combo?.trim(),
  );
}
