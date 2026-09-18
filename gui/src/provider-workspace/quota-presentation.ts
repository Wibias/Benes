/**
 * Project AccountQuota into paint-ready meter records.
 * React receives this surface; it does not recompute fill, tone, or reset copy.
 */
import type { Locale, TFn } from "../i18n/shared.ts";
import { type AccountQuota, normalizeQuotaForPlan } from "../codex-quota-utils.ts";

export type QuotaWindowKey = "fiveHour" | "weekly" | "monthly";

const SPENT_AT = 99.5;
const MIN_FILL = 0.04;

const LOCALE_BCP47: Record<Locale, string> = {
  en: "en-GB",
  de: "de-DE",
  fr: "fr-FR",
  ko: "ko-KR",
  zh: "zh-CN",
  "zh-TW": "zh-TW",
  ru: "ru-RU",
  ja: "ja-JP",
  tr: "tr-TR",
};

type WindowFact = {
  sort: number;
  id: string;
  standard?: QuotaWindowKey;
  rawCustom?: string;
  used: number;
  resetAt?: number;
};

function finite(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function toMs(stamp: number): number {
  return stamp < 10_000_000_000 ? stamp * 1000 : stamp;
}

const STANDARD_WINDOWS: ReadonlyArray<{
  id: QuotaWindowKey;
  sort: number;
  used: (quota: AccountQuota) => number | undefined;
  reset: (quota: AccountQuota) => number | undefined;
}> = [
  { id: "fiveHour", sort: 0, used: quota => finite(quota.fiveHourPercent), reset: quota => finite(quota.fiveHourResetAt) },
  { id: "weekly", sort: 1, used: quota => finite(quota.weeklyPercent), reset: quota => finite(quota.weeklyResetAt) },
  { id: "monthly", sort: 4, used: quota => finite(quota.monthlyPercent), reset: quota => finite(quota.monthlyResetAt) },
];

const CUSTOM_SORT: Record<string, number> = {
  "5h": 0,
  "First-party models": 2,
  "API usage": 3,
};

function factsFromQuota(quota: AccountQuota): WindowFact[] {
  const standard = STANDARD_WINDOWS.flatMap(window => {
    const used = window.used(quota);
    if (used === undefined) return [];
    return [{ sort: window.sort, id: window.id, standard: window.id, used, resetAt: window.reset(quota) }];
  });
  const custom = (quota.customWindows ?? []).flatMap(extra => {
    if (typeof extra.percent !== "number" || !Number.isFinite(extra.percent)) return [];
    return [{
      sort: CUSTOM_SORT[extra.label] ?? 5,
      id: `custom:${extra.label}`,
      rawCustom: extra.label,
      used: extra.percent,
      resetAt: extra.resetAt,
    }];
  });
  const facts: WindowFact[] = [...standard, ...custom];
  return facts.toSorted((left, right) => left.sort - right.sort);
}

function shortCopy(fact: WindowFact, t: TFn): string {
  if (fact.standard === "fiveHour") return t("codexAuth.fiveHour");
  if (fact.standard === "weekly") return t("codexAuth.weekly");
  if (fact.standard === "monthly") return t("codexAuth.monthly");
  if (fact.rawCustom === "First-party models") return t("quota.cursorFirstParty");
  if (fact.rawCustom === "API usage") return t("quota.cursorApiUsage");
  if (fact.rawCustom === "Total subscription credits") return t("quota.totalSubscriptionCredits");
  return fact.rawCustom ?? fact.id;
}

function longCopy(fact: WindowFact, t: TFn): string {
  if (fact.standard === "fiveHour") return t("quota.fiveHourLimit");
  if (fact.standard === "weekly") return t("quota.weeklyLimit");
  if (fact.standard === "monthly") return t("quota.monthlyLimit");
  return shortCopy(fact, t);
}

export function quotaFillRatio(used: number): number {
  const clamped = Math.min(100, Math.max(0, used));
  if (clamped <= 0) return 0;
  return Math.max(MIN_FILL, Math.round(clamped) / 100);
}

export function quotaIsSpent(used: number): boolean {
  return used >= SPENT_AT;
}

export function quotaNeedsWarning(used: number, threshold: number): boolean {
  return threshold > 0 && used >= threshold;
}

export function maxQuotaUtilisation(quota: AccountQuota | null): number {
  if (!quota) return -1;
  const values = factsFromQuota(quota).map(fact => fact.used);
  return values.length === 0 ? -1 : Math.max(...values);
}

export function soonestExhaustedResetAt(quota: AccountQuota | null, now = Date.now()): number | undefined {
  if (!quota) return undefined;
  let chosen: number | undefined;
  let chosenMs = Number.POSITIVE_INFINITY;
  for (const fact of factsFromQuota(quota)) {
    if (!quotaIsSpent(fact.used) || typeof fact.resetAt !== "number") continue;
    const ms = toMs(fact.resetAt);
    if (ms <= now || ms >= chosenMs) continue;
    chosen = fact.resetAt;
    chosenMs = ms;
  }
  return chosen;
}

function localeTag(locale: Locale): string {
  return LOCALE_BCP47[locale];
}

function formatClock(when: Date, locale: Locale): string {
  return new Intl.DateTimeFormat(localeTag(locale), { hour: "2-digit", minute: "2-digit", hour12: false }).format(when);
}

function calendarDay(when: Date, locale: Locale, withYear: boolean): string {
  return new Intl.DateTimeFormat(localeTag(locale), {
    day: "numeric",
    month: "short",
    ...(withYear ? { year: "numeric" as const } : {}),
  }).format(when);
}

function clockParts(resetAt: number | undefined, t: TFn, locale: Locale, now = Date.now()): { day: string; time: string } {
  if (typeof resetAt !== "number" || !Number.isFinite(resetAt)) return { day: "", time: "" };
  const when = new Date(toMs(resetAt));
  const nowDate = new Date(now);
  const time = formatClock(when, locale);
  const today = when.toDateString() === nowDate.toDateString();
  return { day: today ? t("codexAuth.today") : calendarDay(when, locale, false), time };
}

type ResetCopy =
  | { kind: "none" }
  | { kind: "tomorrow"; time: string }
  | { kind: "elapsed"; date: string; time: string }
  | { kind: "minutes"; n: number }
  | { kind: "hours"; n: number }
  | { kind: "today"; time: string }
  | { kind: "later"; date: string; time: string };

function classifyStackedReset(resetAt: number | undefined, locale: Locale, now: number): ResetCopy {
  if (typeof resetAt !== "number" || !Number.isFinite(resetAt)) return { kind: "none" };
  const ms = toMs(resetAt);
  const when = new Date(ms);
  const time = formatClock(when, locale);
  const nowDate = new Date(now);
  const dayDiff = Math.round(
    (Date.UTC(when.getFullYear(), when.getMonth(), when.getDate())
      - Date.UTC(nowDate.getFullYear(), nowDate.getMonth(), nowDate.getDate())) / 86_400_000,
  );
  const date = calendarDay(when, locale, when.getFullYear() !== nowDate.getFullYear());
  if (dayDiff === 1) return { kind: "tomorrow", time };
  if (ms <= now) return { kind: "elapsed", date, time };
  const minutes = Math.round((ms - now) / 60_000);
  if (minutes < 60) return { kind: "minutes", n: Math.max(1, minutes) };
  const hours = Math.round(minutes / 60);
  if (hours < 12 && dayDiff === 0) return { kind: "hours", n: Math.max(1, hours) };
  if (dayDiff === 0) return { kind: "today", time };
  return { kind: "later", date, time };
}

function stackedReset(resetAt: number | undefined, t: TFn, locale: Locale, now = Date.now()): string {
  const copy = classifyStackedReset(resetAt, locale, now);
  switch (copy.kind) {
    case "none":
      return "";
    case "tomorrow":
      return t("quota.resetsTomorrow", { time: copy.time });
    case "elapsed":
    case "later":
      return t("quota.resetsAt", { date: copy.date, time: copy.time, when: `${copy.date}, ${copy.time}` });
    case "minutes":
      return t("quota.resetsRelativeMinutes", { n: copy.n });
    case "hours":
      return t("quota.resetsRelativeHours", { n: copy.n });
    case "today":
      return t("quota.resetsToday", { time: copy.time });
  }
}

export function formatAccessQuotaReset(
  resetAt: number | undefined,
  t: (key: "quota.resetsInCompact" | "quota.resetsRelativeMinutes", vars?: Record<string, string | number>) => string,
  now = Date.now(),
): string {
  if (typeof resetAt !== "number" || !Number.isFinite(resetAt)) return "";
  const ms = toMs(resetAt);
  if (ms <= now) return "";
  const minutes = Math.max(1, Math.round((ms - now) / 60_000));
  if (minutes < 60) return t("quota.resetsRelativeMinutes", { n: minutes });
  const hours = Math.floor(minutes / 60);
  const days = Math.floor(hours / 24);
  const remHours = hours % 24;
  if (days === 0) return t("quota.resetsInCompact", { wait: `${hours}h` });
  if (remHours === 0) return t("quota.resetsInCompact", { wait: `${days}d` });
  return t("quota.resetsInCompact", { wait: `${days}d ${remHours}h` });
}

export type QuotaMeter = {
  id: string;
  caption: string;
  limitCaption: string;
  fill: number;
  tone: "ok" | "warn";
  warn: boolean;
  spent: boolean;
  usedLabel: string;
  resetWord: string;
  resetDay: string;
  resetTime: string;
  resetTitle?: string;
  valueLabel: string;
  stackedReset: string;
  partial: boolean;
  spentCopy: string;
  partialCopy: string;
  partialA11y: string;
};

export type QuotaSurface =
  | { mode: "none" }
  | { mode: "wait"; layout: "compact" | "stacked"; className?: string; loadingLabel: string }
  | { mode: "live"; layout: "compact" | "stacked"; className?: string; meters: QuotaMeter[] };

export function projectQuotaSurface(input: {
  quota: AccountQuota | null;
  plan?: string | null;
  threshold: number;
  t: TFn;
  locale: Locale;
  layout?: "compact" | "stacked";
  pending?: boolean;
  className?: string;
  incompleteWindowKeys?: ReadonlySet<QuotaWindowKey>;
  incompleteCustomWindowLabels?: ReadonlySet<string>;
  now?: number;
}): QuotaSurface {
  const layout = input.layout ?? "compact";
  const planQuota = normalizeQuotaForPlan(input.quota, input.plan);
  const facts = planQuota ? factsFromQuota(planQuota) : [];
  if (facts.length === 0) {
    return input.pending
      ? { mode: "wait", layout, className: input.className, loadingLabel: input.t("common.loading") }
      : { mode: "none" };
  }
  const meters = facts.map(fact => {
    const warn = quotaNeedsWarning(fact.used, input.threshold);
    const spent = quotaIsSpent(fact.used);
    const clock = clockParts(fact.resetAt, input.t, input.locale, input.now);
    const hasClock = Boolean(clock.day || clock.time);
    const title = hasClock
      ? `${input.t("codexAuth.resets")} ${clock.day} ${clock.time}`.replace(/\s+/g, " ").trim()
      : undefined;
    const partial = fact.standard
      ? input.incompleteWindowKeys?.has(fact.standard) === true
      : fact.rawCustom
        ? input.incompleteCustomWindowLabels?.has(fact.rawCustom) === true
        : false;
    const caption = shortCopy(fact, input.t);
    const limitCaption = longCopy(fact, input.t);
    return {
      id: fact.id,
      caption,
      limitCaption,
      fill: quotaFillRatio(fact.used),
      tone: warn || spent ? "warn" as const : "ok" as const,
      warn,
      spent,
      usedLabel: input.t("quota.usedPercent", { pct: Math.round(fact.used) }),
      resetWord: hasClock ? input.t("codexAuth.resets") : "",
      resetDay: clock.day,
      resetTime: clock.time,
      resetTitle: title,
      valueLabel: spent
        ? `${Math.round(fact.used)}% · ${input.t("quota.limitReached")}`
        : `${Math.round(fact.used)}%`,
      stackedReset: stackedReset(fact.resetAt, input.t, input.locale, input.now),
      partial,
      spentCopy: input.t("quota.limitReached"),
      partialCopy: input.t("pws.capacity.windowPartial"),
      partialA11y: input.t("pws.capacity.windowPartialA11y", { window: limitCaption }),
    } satisfies QuotaMeter;
  });
  return { mode: "live", layout, className: input.className, meters };
}
