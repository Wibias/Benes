/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Display primitives the Diagnostics board shares between its request list and its inspector.
 *
 * Timestamps arrive as epoch milliseconds and are normally rendered in the server's zone, so
 * every formatter takes that zone explicitly and falls back to the viewer's zone when the
 * browser rejects it: a rejected zone must degrade one row, never blank the board.
 */

/** A timestamp split into the two stacked lines the request list renders. */
export interface LogDateParts {
  date: string;
  time: string;
}

/** The two halves of a split timestamp, each produced by its own locale reader. */
type LogDatePart = keyof LogDateParts;

const LOCALE_READERS: Record<
  LogDatePart,
  (date: Date, localeTag?: string, options?: Intl.DateTimeFormatOptions) => string
> = {
  date: (date, localeTag, options) => date.toLocaleDateString(localeTag, options),
  time: (date, localeTag, options) => date.toLocaleTimeString(localeTag, options),
};

/**
 * Status bands, probed in order. Blue is reserved for interaction everywhere in the dashboard,
 * so a status colour only ever reports health: 2xx is up, 4xx/5xx is down, and the remaining
 * informational/redirect classes stay unresolved.
 */
const STATUS_BANDS: Array<{ covers: (status: number) => boolean; tone: string }> = [
  { covers: status => status >= 200 && status < 300, tone: "var(--green)" },
  { covers: status => status >= 400, tone: "var(--red)" },
];

const UNRESOLVED_STATUS_TONE = "var(--amber)";

export function statusColor(status: number): string {
  const band = STATUS_BANDS.find(candidate => candidate.covers(status));
  return band ? band.tone : UNRESOLVED_STATUS_TONE;
}

function readLocalePart(part: LogDatePart, ts: number, localeTag?: string, timeZone?: string): string {
  const date = new Date(ts);
  const read = LOCALE_READERS[part];
  if (!timeZone) return read(date, localeTag);
  try {
    return read(date, localeTag, { timeZone });
  } catch {
    return read(date, localeTag);
  }
}

export function formatLogDateParts(ts: number, localeTag?: string, timeZone?: string): LogDateParts {
  return {
    date: readLocalePart("date", ts, localeTag, timeZone),
    time: readLocalePart("time", ts, localeTag, timeZone),
  };
}

export function formatLogDateTime(ts: number, localeTag?: string, timeZone?: string): string {
  const { date, time } = formatLogDateParts(ts, localeTag, timeZone);
  return `${date} ${time}`;
}

/**
 * Why a metric could not be reported, and the sentence the catalogue owes each reason.
 *
 * The inspector prints the reason token the listener sent rather than prose, so nothing reads
 * these at render time. They stay here as the module's published vocabulary: every reason the
 * board can meet has exactly one sentence in the catalogue, and the pairing is declared in one
 * place so the two cannot drift apart.
 */
const METRIC_REASON_KEY = {
  usage_missing: "logs.detail.reason.usage_missing",
  usage_unsupported: "logs.detail.reason.usage_unsupported",
  output_missing: "logs.detail.reason.output_missing",
  invalid_duration: "logs.detail.reason.invalid_duration",
  price_unmatched: "logs.detail.reason.price_unmatched",
  invalid_cache_breakdown: "logs.detail.reason.invalid_cache_breakdown",
  invalid_usage: "logs.detail.reason.invalid_usage",
  combo_attempt_unavailable: "logs.detail.reason.combo_attempt_unavailable",
} as const;

/** Why a cost could only be estimated, with the sentence the catalogue owes each reason. */
const ESTIMATE_REASON_KEY = {
  usage_estimated: "logs.detail.estimate.usage_estimated",
  cache_detail_missing: "logs.detail.estimate.cache_detail_missing",
  expected_price_overlay: "logs.detail.estimate.expected_price_overlay",
  provider_cost_overlay: "logs.detail.estimate.provider_cost_overlay",
} as const;

/** How a failed attempt recovered, keyed by the recovery kind the listener reports. */
const RECOVERY_KIND_KEY = {
  "transient-5xx": "logs.detail.attempt.recovery.transient5xx",
  "connection-reset": "logs.detail.attempt.recovery.connectionReset",
  "oauth-401": "logs.detail.attempt.recovery.oauth401",
  "key-429": "logs.detail.attempt.recovery.key429",
  "rate-limit-429": "logs.detail.attempt.recovery.rateLimit429",
  "anthropic-oauth-429": "logs.detail.attempt.recovery.anthropicOauth429",
  "image-413": "logs.detail.attempt.recovery.image413",
  "empty-completion": "logs.detail.attempt.recovery.emptyCompletion",
} as const;

/** How far a matched price was checked against the model it was matched to. */
const VERIFICATION_KEY = {
  verified: "logs.detail.verification.verified",
  "verified-derived": "logs.detail.verification.derived",
} as const;

/** Copy key for a metric the listener reported as unavailable. */
export function metricReasonKey(reason: keyof typeof METRIC_REASON_KEY): string {
  return METRIC_REASON_KEY[reason];
}

/** Copy key for a cost the listener could only estimate. */
export function estimateReasonKey(reason: keyof typeof ESTIMATE_REASON_KEY): string {
  return ESTIMATE_REASON_KEY[reason];
}

/** Copy key for a recovery kind; a kind the board does not know still needs a label. */
export function recoveryKindKey(kind: string): string {
  const known: Record<string, string> = RECOVERY_KIND_KEY;
  return known[kind] ?? "logs.detail.attempt.recovery.unknown";
}

/** Copy key for a price verdict; anything that is not a full match is shown as derived. */
export function verificationKey(status: string): string {
  return status === "verified"
    ? VERIFICATION_KEY.verified
    : VERIFICATION_KEY["verified-derived"];
}