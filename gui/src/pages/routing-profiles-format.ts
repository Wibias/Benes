/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";
import {
  OPTIMIZE_WEIGHTS as OPTIMIZE_KEYS,
  PROFILE_REQUIREMENT_FIELDS,
  UNKNOWN_COST_CAP_MODES as UNKNOWN_COST_CAP_OPTIONS,
  UNKNOWN_EVIDENCE_MODES as UNKNOWN_EVIDENCE_OPTIONS,
  UNKNOWN_EVIDENCE_TARGETS as UNKNOWN_EVIDENCE_KEYS,
} from "../routing-profile/profile-model.ts";

export type DryRunCandidate = {
  provider: string;
  model: string;
  eligible: boolean;
  exclusions: Array<{ code: string; detail?: string }>;
  score?: { total: number; components: Record<string, number | undefined> };
  cost?: { capOutcome?: string; estimatedUsd?: number; incomplete?: boolean; limitUsd?: number };
};

export type Analytics = {
  totalRequests: number;
  successRate: number | null;
  fallbackRate: number | null;
  confidence: string | null;
  historyTruncated: boolean;
  cooldownTriggeringFailures: number;
  durationMs: { p50?: number; p95?: number; p99?: number; sampleCount: number };
  firstOutputMs: { p50?: number; p95?: number; p99?: number; sampleCount: number; coverage: number | null };
  breakdown: Array<{ provider: string; model: string; requests: number; successRate: number | null; p50DurationMs?: number }>;
};

export type AnalyticsPercentileKey = "p50" | "p95" | "p99";

export type AnalyticsPercentilePoint = {
  key: AnalyticsPercentileKey;
  value: number;
};

export type DryRunEvalResultKind = "selected" | "eligible" | "excluded";

export type ScoreComponentRow = {
  key: (typeof OPTIMIZE_KEYS)[number];
  value: number;
};

/** Only finite percentiles from a block with samples — never invent 0 for missing keys. */
export function presentAnalyticsPercentiles(
  block: { p50?: number; p95?: number; p99?: number; sampleCount: number } | null | undefined,
): AnalyticsPercentilePoint[] {
  if (!block || !Number.isFinite(block.sampleCount) || block.sampleCount <= 0) return [];
  const points: AnalyticsPercentilePoint[] = [];
  for (const key of ["p50", "p95", "p99"] as const) {
    const value = block[key];
    if (typeof value === "number" && Number.isFinite(value)) {
      points.push({ key, value });
    }
  }
  return points;
}

function optionalFiniteNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function optionalRate(value: unknown): number | null {
  if (value === null || value === undefined) return null;
  return typeof value === "number" && Number.isFinite(value) ? value : null;
}

function parsePercentileBlock(
  raw: unknown,
  withCoverage: boolean,
): Analytics["durationMs"] | Analytics["firstOutputMs"] | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const obj = raw as Record<string, unknown>;
  const sampleCount = optionalFiniteNumber(obj.sampleCount);
  if (sampleCount === undefined || sampleCount < 0) return null;
  const block: Analytics["durationMs"] & Partial<Analytics["firstOutputMs"]> = {
    sampleCount,
  };
  for (const key of ["p50", "p95", "p99"] as const) {
    const value = optionalFiniteNumber(obj[key]);
    if (value !== undefined) block[key] = value;
  }
  if (withCoverage) {
    (block as Analytics["firstOutputMs"]).coverage = optionalRate(obj.coverage);
  }
  return block;
}

function parseAnalyticsBreakdown(raw: unknown): Analytics["breakdown"] {
  if (!Array.isArray(raw)) return [];
  const rows: Analytics["breakdown"] = [];
  for (const item of raw) {
    if (!item || typeof item !== "object" || Array.isArray(item)) continue;
    const row = item as Record<string, unknown>;
    const provider = typeof row.provider === "string" ? row.provider : "";
    const model = typeof row.model === "string" ? row.model : "";
    const requests = optionalFiniteNumber(row.requests);
    if (!provider || !model || requests === undefined || requests < 0) continue;
    const parsed: Analytics["breakdown"][number] = {
      provider,
      model,
      requests,
      successRate: optionalRate(row.successRate),
    };
    const p50 = optionalFiniteNumber(row.p50DurationMs);
    if (p50 !== undefined) parsed.p50DurationMs = p50;
    rows.push(parsed);
  }
  return rows;
}

/** Narrow `/api/routing-analytics` without inventing rates or percentile zeros. */
export function parseRoutingAnalytics(raw: unknown): Analytics | null {
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) return null;
  const obj = raw as Record<string, unknown>;
  const totalRequests = optionalFiniteNumber(obj.totalRequests);
  if (totalRequests === undefined || totalRequests < 0) return null;
  const durationMs = parsePercentileBlock(obj.durationMs, false);
  const firstOutputMs = parsePercentileBlock(obj.firstOutputMs, true);
  if (!durationMs || !firstOutputMs) return null;
  const cooldown = optionalFiniteNumber(obj.cooldownTriggeringFailures);
  return {
    totalRequests,
    successRate: optionalRate(obj.successRate),
    fallbackRate: optionalRate(obj.fallbackRate),
    confidence: typeof obj.confidence === "string" ? obj.confidence : null,
    historyTruncated: obj.historyTruncated === true,
    cooldownTriggeringFailures: cooldown === undefined ? 0 : Math.max(0, Math.trunc(cooldown)),
    durationMs,
    firstOutputMs: firstOutputMs as Analytics["firstOutputMs"],
    breakdown: parseAnalyticsBreakdown(obj.breakdown),
  };
}

export type DryRunResult = {
  candidates: DryRunCandidate[];
  selectedIndex: number | null;
  trace?: { profile?: { revision?: string } };
};

/*
 * The vocabularies below are the model owner's, re-exported under the names the editor
 * already reads: the requirement fields, the optimize weights, and both unknown-evidence
 * vocabularies are declared once, in `routing-profile/profile-model`, so the editor cannot
 * offer a value the wire codec would reject.
 */
export { OPTIMIZE_KEYS, UNKNOWN_COST_CAP_OPTIONS, UNKNOWN_EVIDENCE_KEYS, UNKNOWN_EVIDENCE_OPTIONS };

type RequirementEntry = (typeof PROFILE_REQUIREMENT_FIELDS)[number];
type FlagEntry = Extract<RequirementEntry, readonly [string, "flag"]>;
type TextEntry = Extract<RequirementEntry, readonly [string, "text"]>;
type NumberEntry = Extract<RequirementEntry, readonly [string, "number"]>;

function fieldsOfKind<Entry extends RequirementEntry>
  (entries: readonly RequirementEntry[], kind: Entry[1]): Entry[0][] {
  return entries
    .filter((entry): entry is Entry => entry[1] === kind)
    .map(entry => entry[0]);
}

/** The requirement fields the editor groups, taken from the model's field table. */
export const BOOLEAN_REQUIREMENTS: FlagEntry[0][] = fieldsOfKind<FlagEntry>(PROFILE_REQUIREMENT_FIELDS, "flag");
export const STRING_REQUIREMENTS: TextEntry[0][] = fieldsOfKind<TextEntry>(PROFILE_REQUIREMENT_FIELDS, "text");
export const NUMERIC_REQUIREMENTS: NumberEntry[0][] = fieldsOfKind<NumberEntry>(PROFILE_REQUIREMENT_FIELDS, "number");

/**
 * The input bounds each numeric requirement is entered with.
 *
 * The model decides which requirements are numeric and in what order; how wide the editor
 * lets them be typed is the editor's own decision, so the bounds live here.
 */
export const NUMERIC_REQUIREMENT_SPEC: Record<NumberEntry[0], { min: number; max?: number; step: number | "any" }> = {
  minContextWindow: { min: 1, step: 1 },
  minQuotaHeadroom: { min: 0, max: 1, step: "any" },
};

export function suiteSelected(
  requiredSuites: { suiteId: string; evidenceLayer: string }[],
  suite: { suiteId: string; evidenceLayer: string },
): boolean {
  return requiredSuites.some(row =>
    row.suiteId === suite.suiteId && row.evidenceLayer === suite.evidenceLayer);
}

/** Durations: whole ms below 1000; seconds above with sensible precision (420ms, 1.1s). */
export function fmtMs(
  value: number | undefined,
  unavailable: string,
  units: { ms: string; s: string },
): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return unavailable;
  if (value < 1000) return `${Math.round(value)}${units.ms}`;
  const seconds = value / 1000;
  const digits = seconds >= 10 ? 1 : 2;
  const amount = seconds.toFixed(digits).replace(/\.0+$/, "").replace(/(\.\d*?)0+$/, "$1");
  return `${amount}${units.s}`;
}

/**
 * Rates as percent: keep one decimal when meaningful; drop trailing .0
 * (98.7%, 4.2%, 97.8% — not rounded-away integers).
 */
export function fmtRate(value: number | null | undefined, unavailable: string): string {
  if (value === null || value === undefined || !Number.isFinite(value)) return unavailable;
  const pct = value * 100;
  const rounded = Math.round(pct * 10) / 10;
  const text = Number.isInteger(rounded) ? String(rounded) : rounded.toFixed(1);
  return `${text}%`;
}

/** Locale-aware integer counts (sample counts, requests, KPIs). */
export function fmtCount(value: number | undefined, unavailable: string, locale?: string): string {
  if (value === undefined || !Number.isFinite(value) || value < 0) return unavailable;
  return Math.trunc(value).toLocaleString(locale);
}

/** User-facing enums: high → High (display only). */
export function friendlyEnumLabel(value: string | null | undefined, unavailable = ""): string {
  if (value === null || value === undefined) return unavailable;
  const trimmed = value.trim();
  if (!trimmed) return unavailable || trimmed;
  return trimmed.charAt(0).toUpperCase() + trimmed.slice(1);
}

export function dryRunEvalResultKind(
  index: number,
  selectedIndex: number | null,
  eligible: boolean,
): DryRunEvalResultKind {
  if (selectedIndex !== null && index === selectedIndex) return "selected";
  if (eligible) return "eligible";
  return "excluded";
}

export function fmtDryRunEvalResult(kind: DryRunEvalResultKind, t: TFn): string {
  switch (kind) {
    case "selected":
      return t("routing.evalResult.selected");
    case "eligible":
      return t("routing.evalResult.eligible");
    case "excluded":
      return t("routing.evalResult.excluded");
  }
}

export function presentScoreComponents(
  components: Record<string, number | undefined> | undefined,
): ScoreComponentRow[] {
  if (!components) return [];
  const rows: ScoreComponentRow[] = [];
  for (const key of OPTIMIZE_KEYS) {
    const value = components[key];
    if (typeof value !== "number" || !Number.isFinite(value)) continue;
    rows.push({ key, value });
  }
  return rows;
}

export function fmtCapOutcome(
  value: string | undefined,
  t: TFn,
  unavailable: string,
): string {
  switch (value) {
    case "satisfied":
      return t("routing.capOutcome.satisfied");
    case "exceeded":
      return t("routing.capOutcome.exceeded");
    case "unknown-allowed":
      return t("routing.capOutcome.unknown-allowed");
    case "unknown-excluded":
      return t("routing.capOutcome.unknown-excluded");
    default:
      return unavailable;
  }
}

export function fmtExclusion(code: string, t: TFn): string {
  switch (code) {
    case "capability-unsatisfied":
      return t("routing.exclusion.capability-unsatisfied");
    case "unknown-capability":
      return t("routing.exclusion.unknown-capability");
    case "cost-limit":
      return t("routing.exclusion.cost-limit");
    case "cost-limit-unknown":
      return t("routing.exclusion.cost-limit-unknown");
    case "cooldown":
      return t("routing.exclusion.cooldown");
    case "unknown-health":
      return t("routing.exclusion.unknown-health");
    case "unknown-quota":
      return t("routing.exclusion.unknown-quota");
    case "unknown-price":
      return t("routing.exclusion.unknown-price");
    case "quota-headroom":
      return t("routing.exclusion.quota-headroom");
    case "compatibility-unsatisfied":
      return t("routing.exclusion.compatibility-unsatisfied");
    default:
      return t("routing.exclusion.other", { code });
  }
}

export function fmtUsd(value: number | undefined, unavailable: string): string {
  if (value === undefined || !Number.isFinite(value)) return unavailable;
  return `$${value.toFixed(3)}`;
}

/** Quiet note when cost.incomplete is true; omit when false/absent. */
export function costIncompleteNote(
  incomplete: boolean | undefined,
  note: string,
): string | null {
  return incomplete === true ? note : null;
}

export function fmtScoreComponents(
  components: Record<string, number | undefined> | undefined,
  t: TFn,
  unavailable: string,
): string {
  const rows = presentScoreComponents(components);
  if (rows.length === 0) return components ? unavailable : "";
  const labels: Record<(typeof OPTIMIZE_KEYS)[number], "routing.optimize.latency" | "routing.optimize.health" | "routing.optimize.cost" | "routing.optimize.quota"> = {
    latency: "routing.optimize.latency",
    health: "routing.optimize.health",
    cost: "routing.optimize.cost",
    quota: "routing.optimize.quota",
  };
  return rows.map((row) => `${t(labels[row.key])} ${row.value.toFixed(3)}`).join(" | ");
}

export function fmtExclusionEntry(
  exclusion: { code: string; detail?: string },
  t: TFn,
): string {
  const label = fmtExclusion(exclusion.code, t);
  const detail = exclusion.detail?.trim();
  return detail ? `${label} (${detail})` : label;
}
