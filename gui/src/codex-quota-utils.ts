/** Benes dashboard client for the Go proxy (`internal/server`). */

/** An extra window a provider reports that the three aggregates do not cover. */
export interface QuotaWindow {
  label: string;
  percent: number;
  resetAt?: number;
}

/** The aggregate quota windows a Codex/ChatGPT account reports, in display order. */
export interface AccountQuota {
  weeklyPercent?: number;
  fiveHourPercent?: number;
  monthlyPercent?: number;
  weeklyResetAt?: number;
  fiveHourResetAt?: number;
  monthlyResetAt?: number;
  customWindows?: QuotaWindow[];
  resetCredits?: number;
  updatedAt: number;
}

/**
 * ChatGPT / Codex hand out *remaining* quota, so a percentage is already what the operator
 * wants to read. WHAM's `used_percent` is the consumed side of the same window; this is the
 * conversion for those callers.
 */
export function remainingQuotaPercent(used: number): number {
  if (!Number.isFinite(used)) return 0;
  return Math.max(0, Math.min(100, Math.round(100 - used)));
}

type PercentKey = "fiveHourPercent" | "weeklyPercent" | "monthlyPercent";
type ResetKey = "fiveHourResetAt" | "weeklyResetAt" | "monthlyResetAt";

/**
 * One aggregate window: where it lands in `AccountQuota`, and every name the payloads use
 * for it.
 *
 * `/api/codex-auth/accounts` spells the 5-hour window `shortPercent` / `shortResetAt`,
 * while every reader downstream expects the `fiveHour*` spelling. Declaring the aliases
 * beside the window keeps that translation in one place rather than in a second pass over
 * the row.
 */
interface QuotaWindowSpec {
  readonly percentKey: PercentKey;
  readonly resetKey: ResetKey;
  readonly percentNames: readonly string[];
  readonly resetNames: readonly string[];
}

const AGGREGATE_WINDOWS: readonly QuotaWindowSpec[] = [
  {
    percentKey: "fiveHourPercent",
    resetKey: "fiveHourResetAt",
    percentNames: ["fiveHourPercent", "shortPercent"],
    resetNames: ["fiveHourResetAt", "shortResetAt"],
  },
  {
    percentKey: "weeklyPercent",
    resetKey: "weeklyResetAt",
    percentNames: ["weeklyPercent"],
    resetNames: ["weeklyResetAt"],
  },
  {
    percentKey: "monthlyPercent",
    resetKey: "monthlyResetAt",
    percentNames: ["monthlyPercent"],
    resetNames: ["monthlyResetAt"],
  },
];

/**
 * The windows a 30-day plan keeps, taken from the same table rather than restated.
 *
 * ChatGPT Go and Free meter 30 days at a time, so the rolling aggregates the payload still
 * carries would draw bars for limits that do not exist.
 */
const THIRTY_DAY_WINDOWS = AGGREGATE_WINDOWS.filter(window => window.percentKey === "monthlyPercent");

/** The plan names that are metered 30 days at a time. */
const THIRTY_DAY_PLANS = new Set(["go", "free"]);

function plainRow(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

/** A finite number, or nothing. Every quota field goes through here. */
function finiteQuotaNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function firstQuotaNumber(row: Record<string, unknown>, names: readonly string[]): number | undefined {
  for (const name of names) {
    const found = finiteQuotaNumber(row[name]);
    if (found !== undefined) return found;
  }
  return undefined;
}

/**
 * Project an `/api/codex-auth/accounts` quota row into `AccountQuota`.
 *
 * A row that reports no aggregate percentage at all is not a quota; returning null there is
 * what lets the caller tell "this account has no quota API" apart from "this account is at
 * zero". Reset instants are kept independently of their percentage, so a window the server
 * has not measured yet still shows when it will turn over.
 */
export function accountQuotaFromCodexAccounts(quota: unknown): AccountQuota | null {
  const row = plainRow(quota);
  if (row === null) return null;

  const projected: AccountQuota = { updatedAt: firstQuotaNumber(row, ["updatedAt"]) ?? 0 };
  let measured = false;
  for (const window of AGGREGATE_WINDOWS) {
    const percent = firstQuotaNumber(row, window.percentNames);
    if (percent !== undefined) {
      measured = true;
      projected[window.percentKey] = percent;
    }
    const reset = firstQuotaNumber(row, window.resetNames);
    if (reset !== undefined) projected[window.resetKey] = reset;
  }
  if (!measured) return null;

  const resetCredits = finiteQuotaNumber(row.resetCredits);
  if (resetCredits !== undefined) projected.resetCredits = resetCredits;
  return projected;
}

/** ChatGPT Go and Free meter 30 days at a time, so they have no rolling windows. */
export function isThirtyDayOnlyPlan(plan: string | null | undefined): boolean {
  return THIRTY_DAY_PLANS.has(plan?.trim().toLowerCase() ?? "");
}

export function normalizeQuotaForPlan(quota: AccountQuota | null, plan: string | null | undefined): AccountQuota | null {
  if (quota === null || !isThirtyDayOnlyPlan(plan)) return quota;
  const thirtyDay: AccountQuota = { updatedAt: quota.updatedAt };
  for (const window of THIRTY_DAY_WINDOWS) {
    const percent = quota[window.percentKey];
    if (percent !== undefined) thirtyDay[window.percentKey] = percent;
    const reset = quota[window.resetKey];
    if (reset !== undefined) thirtyDay[window.resetKey] = reset;
  }
  if (quota.resetCredits !== undefined) thirtyDay.resetCredits = quota.resetCredits;
  return thirtyDay;
}

/**
 * The Access table's quota cell: the weekly window, or nothing.
 *
 * The 5-hour window already has its own bars above the table, so folding it into the same
 * cell would print one limit twice and push the row height around. A row with no weekly
 * percentage gets no cell rather than a zero.
 */
export function accessWeeklyQuotaCell(
  quota: Pick<AccountQuota, "weeklyPercent" | "weeklyResetAt"> | null | undefined,
): { remainingPercent: number; resetAt?: number } | null {
  const weekly = finiteQuotaNumber(quota?.weeklyPercent);
  if (weekly === undefined) return null;
  const resetAt = finiteQuotaNumber(quota?.weeklyResetAt);
  return {
    remainingPercent: remainingQuotaPercent(weekly),
    ...(resetAt !== undefined ? { resetAt } : {}),
  };
}
