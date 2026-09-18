/** Benes dashboard client for the Go proxy (`internal/server`). */
import { catalogValue, type Locale } from "./i18n/catalogs.ts";

const SECONDS_PER_MINUTE = 60;
const MINUTES_PER_HOUR = 60;
const HOURS_PER_DAY = 24;

/** Below this a raw second count reads better than a rounded unit. */
const INLINE_SECOND_LIMIT = 5 * SECONDS_PER_MINUTE;

interface UptimeUnits {
  day: string;
  hour: string;
  minute: string;
  second: string;
}

/** Unit suffixes come from the active locale; a duration never translates a number. */
function uptimeUnits(locale: Locale): UptimeUnits {
  return {
    day: catalogValue(locale, "uptime.day"),
    hour: catalogValue(locale, "uptime.hour"),
    minute: catalogValue(locale, "uptime.minute"),
    second: catalogValue(locale, "uptime.second"),
  };
}

function uptimeNumber(value: number, suffix: string): string {
  return `${value}${suffix}`;
}

/** "3h 20m" while the smaller part is non-zero, otherwise just "3h". */
function uptimeTwoPart(major: number, majorUnit: string, minor: number, minorUnit: string): string {
  const head = uptimeNumber(major, majorUnit);
  return minor > 0 ? `${head} ${uptimeNumber(minor, minorUnit)}` : head;
}

/** A non-finite sample is unknown, not zero, and prints as the em dash. */
export function formatUptime(seconds: number, locale: Locale): string {
  if (!Number.isFinite(seconds)) return "—";
  const total = Math.max(0, Math.floor(seconds));
  const units = uptimeUnits(locale);

  if (total < INLINE_SECOND_LIMIT) return uptimeNumber(total, units.second);

  const minutes = Math.floor(total / SECONDS_PER_MINUTE);
  if (minutes < MINUTES_PER_HOUR) return uptimeNumber(minutes, units.minute);

  const hours = Math.floor(minutes / MINUTES_PER_HOUR);
  if (hours < HOURS_PER_DAY) {
    return uptimeTwoPart(hours, units.hour, minutes % MINUTES_PER_HOUR, units.minute);
  }

  const days = Math.floor(hours / HOURS_PER_DAY);
  return uptimeTwoPart(days, units.day, hours % HOURS_PER_DAY, units.hour);
}
