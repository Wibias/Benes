/** Locale-aware credit dates and USD estimates for dashboard account copy. */

const MISSING = "\u2014";
const creditDay = new Map<string, Intl.DateTimeFormat>();
const creditClock = new Map<string, Intl.DateTimeFormat>();
const usdMoney = new Map<string, Intl.NumberFormat>();
const numberCache = new Map<string, Intl.NumberFormat>();

function instant(iso: string): number | null {
  const ms = Date.parse(iso);
  return Number.isFinite(ms) ? ms : null;
}

function dateParts(includeTime: boolean): Intl.DateTimeFormatOptions {
  const parts: Intl.DateTimeFormatOptions = {};
  parts.month = "short";
  parts.day = "numeric";
  parts.year = "numeric";
  if (!includeTime) return parts;
  parts.hour = "2-digit";
  parts.minute = "2-digit";
  return parts;
}

function usdParts(): Intl.NumberFormatOptions {
  const parts: Intl.NumberFormatOptions = {};
  parts.style = "currency";
  parts.currency = "USD";
  parts.minimumFractionDigits = 4;
  parts.maximumFractionDigits = 4;
  return parts;
}

function dayFormatter(locale: string | undefined): Intl.DateTimeFormat {
  const tag = locale ?? "";
  const existing = creditDay.get(tag);
  if (existing) return existing;
  const created = new Intl.DateTimeFormat(locale, dateParts(false));
  creditDay.set(tag, created);
  return created;
}

function clockFormatter(locale: string | undefined): Intl.DateTimeFormat {
  const tag = locale ?? "";
  const existing = creditClock.get(tag);
  if (existing) return existing;
  const created = new Intl.DateTimeFormat(locale, dateParts(true));
  creditClock.set(tag, created);
  return created;
}

export function cachedNumberFormat(
  locale: string | undefined,
  options?: Intl.NumberFormatOptions,
): Intl.NumberFormat {
  const fields = options
    ? Object.keys(options).sort().map((key) => `${key}:${String((options as Record<string, unknown>)[key])}`)
    : [];
  const tag = `${locale ?? ""}\u241f${fields.join(",")}`;
  const existing = numberCache.get(tag);
  if (existing) return existing;
  const created = new Intl.NumberFormat(locale, options);
  numberCache.set(tag, created);
  return created;
}

export function formatCreditDate(iso: string, locale?: string): string {
  const ms = instant(iso);
  if (ms === null) return MISSING;
  return dayFormatter(locale).format(ms);
}

export function formatCreditDateTime(iso: string, locale?: string): string {
  const ms = instant(iso);
  if (ms === null) return MISSING;
  return clockFormatter(locale).format(ms);
}

export function formatEstimatedUsdValue(value: number, locale?: string): string {
  if (!Number.isFinite(value) || value < 0) return MISSING;
  const tag = locale ?? "";
  const existing = usdMoney.get(tag);
  const formatter = existing ?? new Intl.NumberFormat(locale, usdParts());
  if (!existing) usdMoney.set(tag, formatter);
  return `~${formatter.format(value)}`;
}
const DAY_MS = 86_400_000;

/** Whole days from now until `iso`, floored at 0. Drives credit-expiry copy. */
export function creditDaysRemaining(iso: string): number {
  const remaining = new Date(iso).getTime() - Date.now();
  return Math.max(0, Math.ceil(remaining / DAY_MS));
}

