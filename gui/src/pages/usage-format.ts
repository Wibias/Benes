import { cacheReadTokens, type UsageResponse, type UsageSummaryTotals } from "./usage-contract.ts";

export function formatPct1(ratio: number | undefined): string | undefined {
  if (typeof ratio !== "number" || !Number.isFinite(ratio) || ratio < 0) return undefined;
  return `${(ratio * 100).toFixed(1)}%`;
}

export function coverageShare(measured: number, requests: number): number | undefined {
  if (!Number.isFinite(measured) || !Number.isFinite(requests) || requests <= 0) return undefined;
  return measured / requests;
}

export function cacheReadShare(summary: UsageSummaryTotals): { ratio: number; reads: number; input: number } | undefined {
  const reads = cacheReadTokens(summary);
  if (typeof reads !== "number" || !Number.isFinite(reads) || reads < 0) return undefined;
  if (!(summary.inputTokens > 0)) return undefined;
  return { ratio: reads / summary.inputTokens, reads, input: summary.inputTokens };
}

export function uncoveredRequests(summary: UsageSummaryTotals): number {
  return Math.max(0, summary.requests - summary.measuredRequests);
}

export type HistoryDisplay =
  | { kind: "complete"; start: number | null; end: number | null }
  | { kind: "partial"; start: number | null; end: number | null; skippedPrefixBytes: number };

export function historyDisplay(data: Pick<UsageResponse, "historyTruncated" | "truncatedPrefixBytes" | "snapshotWindowStart" | "snapshotWindowEnd">): HistoryDisplay {
  if (data.historyTruncated) {
    return {
      kind: "partial",
      start: data.snapshotWindowStart ?? null,
      end: data.snapshotWindowEnd ?? null,
      skippedPrefixBytes: data.truncatedPrefixBytes,
    };
  }
  return {
    kind: "complete",
    start: data.snapshotWindowStart ?? null,
    end: data.snapshotWindowEnd ?? null,
  };
}

export function formatWindowInstant(value: number | null | undefined, locale?: string): string | undefined {
  if (typeof value !== "number" || !Number.isFinite(value)) return undefined;
  const at = new Date(value);
  if (!Number.isFinite(at.getTime())) return undefined;
  return new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", year: "numeric" }).format(at);
}

export function formatWindowRange(
  start: number | null | undefined,
  end: number | null | undefined,
  locale?: string,
  timeZone?: string,
): string | undefined {
  if (typeof start !== "number" || typeof end !== "number" || !Number.isFinite(start) || !Number.isFinite(end)) {
    return undefined;
  }
  const from = new Date(start);
  const to = new Date(end);
  if (!Number.isFinite(from.getTime()) || !Number.isFinite(to.getTime())) return undefined;
  const zone = timeZone ? { timeZone } : {};
  const yearOf = (value: Date) => new Intl.DateTimeFormat(locale, { year: "numeric", ...zone }).format(value);
  if (yearOf(from) === yearOf(to)) {
    const startText = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", ...zone }).format(from);
    const endText = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", year: "numeric", ...zone }).format(to);
    return `${startText} – ${endText}`;
  }
  const full = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", year: "numeric", ...zone });
  return `${full.format(from)} – ${full.format(to)}`;
}

export function formatPrefixBytes(bytes: number, locale?: string): string | undefined {
  if (!Number.isFinite(bytes) || bytes <= 0) return undefined;
  try {
    return new Intl.NumberFormat(locale, { notation: "compact", compactDisplay: "short" }).format(bytes);
  } catch {
    return String(Math.round(bytes));
  }
}

export function formatAxisDate(iso: string, locale?: string): string {
  const bits = iso.split("-");
  const nums = bits.map(Number);
  if (nums.some(n => !Number.isFinite(n))) return iso;
  if (bits.length === 2) {
    const at = new Date(nums[0], nums[1] - 1, 1);
    if (!Number.isFinite(at.getTime())) return iso;
    return new Intl.DateTimeFormat(locale, { month: "short", year: "numeric" }).format(at);
  }
  if (bits.length !== 3) return iso;
  const at = new Date(nums[0], nums[1] - 1, nums[2]);
  if (!Number.isFinite(at.getTime())) return iso;
  return new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" }).format(at);
}

export function shareOf(part: number, whole: number): number | undefined {
  if (!Number.isFinite(part) || !Number.isFinite(whole) || whole <= 0) return undefined;
  return part / whole;
}

export function niceCeiling(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 1;
  const exp = 10 ** Math.floor(Math.log10(value));
  const n = value / exp;
  const nice = n <= 1 ? 1 : n <= 2 ? 2 : n <= 5 ? 5 : 10;
  return nice * exp;
}

export function chartAxisTicks(maxValue: number): number[] {
  const top = niceCeiling(maxValue);
  return [top, top * 0.75, top * 0.5, top * 0.25, 0];
}
