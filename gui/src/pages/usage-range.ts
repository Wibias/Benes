export type UsageRange = "all" | "30d" | "7d" | "today" | "yesterday" | "custom";
export type UsageSurface = "all" | "codex" | "claude" | "grok";

const instantPattern = /^(\d{4}-\d{2}-\d{2})(?:[T ](\d{2}):(\d{2})(?::(\d{2}))?)?$/;

export type UsageRangeQuery =
  | { ok: true; query: string }
  | { ok: false; error: "malformed" | "reversed" };

export function isoDateInZone(at: Date, timeZone: string): string {
  return new Intl.DateTimeFormat("en-CA", { timeZone, year: "numeric", month: "2-digit", day: "2-digit" }).format(at);
}

export function shiftIsoDate(iso: string, days: number): string {
  const [year, month, day] = iso.split("-").map(Number);
  return new Date(Date.UTC(year, month - 1, day + days)).toISOString().slice(0, 10);
}

export function defaultCustomRange(timeZone: string): { start: string; end: string } {
  const endDay = isoDateInZone(new Date(), timeZone);
  return { start: isoDateTime(shiftIsoDate(endDay, -6), "00:00"), end: isoDateTime(endDay, "23:59") };
}

export function customRangeError(start: string, end: string): "malformed" | "reversed" | null {
  if (!instantPattern.test(start.trim()) || !instantPattern.test(end.trim())) {
    return "malformed";
  }
  if (normalizeInstant(end) < normalizeInstant(start)) {
    return "reversed";
  }
  return null;
}

export function exclusiveEndParam(inclusiveEnd: string): string {
  const match = instantPattern.exec(inclusiveEnd.trim());
  if (!match) {
    return inclusiveEnd;
  }
  if (!match[2]) {
    return shiftIsoDate(match[1], 1);
  }
  const year = Number(match[1].slice(0, 4));
  const month = Number(match[1].slice(5, 7));
  const day = Number(match[1].slice(8, 10));
  const hour = Number(match[2]);
  const minute = Number(match[3]);
  const next = new Date(year, month - 1, day, hour, minute + 1);
  return formatLocalDateTime(next);
}

export function usageSearchParams(
  range: UsageRange,
  surface: UsageSurface,
  customStart: string,
  customEnd: string,
  timeZone: string,
): UsageRangeQuery {
  const params = new URLSearchParams();
  params.set("surface", surface);
  params.set("tz", timeZone);
  if (range === "custom") {
    const error = customRangeError(customStart, customEnd);
    if (error) {
      return { ok: false, error };
    }
    params.set("start", customStart.trim());
    params.set("end", exclusiveEndParam(customEnd));
  } else {
    params.set("range", range);
  }
  return { ok: true, query: params.toString() };
}

function isoDateTime(day: string, time: string): string {
  return [day, time].join(String.fromCharCode(84));
}

function normalizeInstant(raw: string): string {
  const match = instantPattern.exec(raw.trim());
  if (!match) {
    return raw;
  }
  if (!match[2]) {
    return isoDateTime(match[1], "00:00");
  }
  return isoDateTime(match[1], `${match[2]}:${match[3]}`);
}

function formatLocalDateTime(value: Date): string {
  const year = String(value.getFullYear()).padStart(4, "0");
  const month = String(value.getMonth() + 1).padStart(2, "0");
  const day = String(value.getDate()).padStart(2, "0");
  const hour = String(value.getHours()).padStart(2, "0");
  const minute = String(value.getMinutes()).padStart(2, "0");
  return isoDateTime(`${year}-${month}-${day}`, `${hour}:${minute}`);
}
