/**
 * provider-workspace/report.ts — boundary narrowing for the provider-quota and
 * model-list payloads the workspace detail reads (WP090).
 *
 * Everything here narrows untrusted listener JSON into the display shapes the
 * panels render: no React, no fetch, and nothing invented that the listener did
 * not actually send.
 */
import type { AccountQuota } from "../codex-quota-utils";

/** Where a quota report came from and when the listener read it. */
interface QuotaReportProvenance {
  label?: string;
  source?: string;
  updatedAt?: number;
  accountId?: string;
}

/** Nested payloads a report row may carry; each is narrowed where it is used. */
interface QuotaReportPayload {
  quota?: unknown;
  entitlement?: unknown;
  aggregation?: unknown;
}

/** One row of `GET /api/provider-quotas` as the workspace consumes it. */
export type ProviderQuotaReportView = QuotaReportProvenance & QuotaReportPayload;

/** Plan and credential expiry timestamps a report may carry. */
export interface EntitlementTimingView {
  billingPeriodEndsAt?: number;
  planRenewsAt?: number;
  subscriptionExpiresAt?: number;
  entitlementExpiresAt?: number;
  credentialExpiresAt?: number;
}

/** Recovery hint attached to one capacity window. */
interface WindowRecovery {
  nextRecoveryAt?: number;
  nextRecoveryPercent?: number;
}

/** One capacity window in a provider's aggregate. */
export interface CapacityWindowView extends WindowRecovery {
  usedPercent: number;
  incomplete?: boolean;
  excludedAccounts?: number;
}

/** Account arithmetic behind an aggregate window set. */
interface CapacityCoverage {
  incomplete: boolean;
  excludedAccounts: number;
  unknownPlanAccounts: number;
  partialWindowAccounts: number;
}

/** Aggregate windows plus the account the fallback presentation reads. */
interface CapacityWindows {
  fiveHour?: CapacityWindowView;
  weekly?: CapacityWindowView;
  monthly?: CapacityWindowView;
  customWindows?: Array<CapacityWindowView & { label: string }>;
  currentAccount?: { plan?: string | null; quota: AccountQuota | null };
}

/** `aggregation.kind = capacity-weighted-v1` narrowed for the capacity board. */
export interface ProviderCapacityAggregationView extends CapacityCoverage, CapacityWindows {
  presentation: "aggregate" | "effective-account-fallback" | "coverage-only";
}

function asRecord(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function asNumber(value: unknown): number | undefined {
  return typeof value === "number" && Number.isFinite(value) ? value : undefined;
}

function asBoolean(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

/**
 * The numeric fields a row actually carries. A key the listener omitted, or sent
 * as a non-finite value, stays absent rather than becoming a zero.
 */
function numericSubset<const K extends string>(
  row: Record<string, unknown>,
  keys: readonly K[],
): Partial<Record<K, number>> {
  const out: Partial<Record<K, number>> = {};
  for (const key of keys) {
    const value = asNumber(row[key]);
    if (value !== undefined) out[key] = value;
  }
  return out;
}

type CustomQuotaWindow = NonNullable<AccountQuota["customWindows"]>[number];

const QUOTA_NUMBER_KEYS = [
  "fiveHourPercent",
  "fiveHourResetAt",
  "weeklyPercent",
  "weeklyResetAt",
  "monthlyPercent",
  "monthlyResetAt",
] as const;

function customQuotaWindow(value: unknown): CustomQuotaWindow | null {
  const row = asRecord(value);
  if (!row || typeof row.label !== "string") return null;
  const percent = asNumber(row.percent);
  if (percent === undefined) return null;
  const resetAt = asNumber(row.resetAt);
  return {
    label: row.label,
    percent,
    ...(resetAt === undefined ? {} : { resetAt }),
  };
}

function customQuotaWindows(value: unknown): CustomQuotaWindow[] {
  if (!Array.isArray(value)) return [];
  const windows: CustomQuotaWindow[] = [];
  for (const entry of value) {
    const window = customQuotaWindow(entry);
    if (window) windows.push(window);
  }
  return windows;
}

function hasQuotaUsage(quota: AccountQuota): boolean {
  return quota.fiveHourPercent !== undefined
    || quota.weeklyPercent !== undefined
    || quota.monthlyPercent !== undefined
    || (quota.customWindows?.length ?? 0) > 0;
}

function quotaFromUnknown(quota: unknown, fallbackUpdatedAt?: number): AccountQuota | null {
  const row = asRecord(quota);
  if (!row) return null;
  const customWindows = customQuotaWindows(row.customWindows);
  const parsed: AccountQuota = {
    ...numericSubset(row, QUOTA_NUMBER_KEYS),
    ...(customWindows.length > 0 ? { customWindows } : {}),
    updatedAt: asNumber(row.updatedAt) ?? fallbackUpdatedAt ?? Date.now(),
  };
  return hasQuotaUsage(parsed) ? parsed : null;
}

/** Narrow an unknown quota payload into the AccountQuota display shape (null when unusable). */
export function accountQuotaFromReport(report?: ProviderQuotaReportView): AccountQuota | null {
  return quotaFromUnknown(report?.quota, report?.updatedAt);
}

const ENTITLEMENT_KEYS = [
  "billingPeriodEndsAt",
  "planRenewsAt",
  "subscriptionExpiresAt",
  "entitlementExpiresAt",
  "credentialExpiresAt",
] as const;

/** Strictly narrows typed plan/entitlement dates. Missing or non-finite values are omitted. */
export function entitlementFromReport(report?: ProviderQuotaReportView): EntitlementTimingView | null {
  const row = asRecord(report?.entitlement);
  if (!row) return null;
  const timing = numericSubset(row, ENTITLEMENT_KEYS);
  return Object.keys(timing).length > 0 ? timing : null;
}

const WINDOW_NUMBER_KEYS = ["excludedAccounts", "nextRecoveryAt", "nextRecoveryPercent"] as const;

function capacityWindow(value: unknown): CapacityWindowView | undefined {
  const row = asRecord(value);
  if (!row) return undefined;
  const usedPercent = asNumber(row.usedPercent);
  if (usedPercent === undefined) return undefined;
  const incomplete = asBoolean(row.incomplete);
  return {
    usedPercent,
    ...(incomplete === undefined ? {} : { incomplete }),
    ...numericSubset(row, WINDOW_NUMBER_KEYS),
  };
}

function customCapacityWindows(value: unknown): Array<CapacityWindowView & { label: string }> {
  if (!Array.isArray(value)) return [];
  const windows: Array<CapacityWindowView & { label: string }> = [];
  for (const entry of value) {
    const row = asRecord(entry);
    if (!row || typeof row.label !== "string") continue;
    const window = capacityWindow(row);
    if (window) windows.push({ label: row.label, ...window });
  }
  return windows;
}

function currentAccountFromUnknown(value: unknown): ProviderCapacityAggregationView["currentAccount"] | null {
  const row = asRecord(value);
  if (!row) return null;
  const account: NonNullable<ProviderCapacityAggregationView["currentAccount"]> = {
    quota: quotaFromUnknown(row.quota),
  };
  if (typeof row.plan === "string" || row.plan === null) account.plan = row.plan;
  return account;
}

/**
 * Aggregate windows imply an aggregate presentation; without any window the
 * report can only speak about coverage.
 */
function capacityPresentation(
  value: unknown,
  hasAggregateWindow: boolean,
): ProviderCapacityAggregationView["presentation"] {
  if (value === "aggregate" || value === "effective-account-fallback" || value === "coverage-only") {
    return value;
  }
  return hasAggregateWindow ? "aggregate" : "coverage-only";
}

/** The windows one aggregation payload carried. */
interface AggregateWindowSet {
  fiveHour?: CapacityWindowView;
  weekly?: CapacityWindowView;
  monthly?: CapacityWindowView;
  customWindows: Array<CapacityWindowView & { label: string }>;
  currentAccount: ProviderCapacityAggregationView["currentAccount"] | null;
}

function readAggregateWindows(row: Record<string, unknown>): AggregateWindowSet {
  return {
    fiveHour: capacityWindow(row.fiveHour),
    weekly: capacityWindow(row.weekly),
    monthly: capacityWindow(row.monthly),
    customWindows: customCapacityWindows(row.customWindows),
    currentAccount: currentAccountFromUnknown(row.currentAccount),
  };
}

function hasAnyWindow(set: AggregateWindowSet): boolean {
  return Boolean(set.fiveHour || set.weekly || set.monthly || set.customWindows.length > 0);
}

/** Only the windows the payload actually sent reach the view. */
function windowFields(set: AggregateWindowSet): CapacityWindows {
  const windows: CapacityWindows = {};
  if (set.fiveHour) windows.fiveHour = set.fiveHour;
  if (set.weekly) windows.weekly = set.weekly;
  if (set.monthly) windows.monthly = set.monthly;
  if (set.customWindows.length > 0) windows.customWindows = set.customWindows;
  if (set.currentAccount) windows.currentAccount = set.currentAccount;
  return windows;
}

/** Strictly narrows optional weighted-pool metadata; legacy reports return null. */
export function capacityAggregationFromReport(report?: ProviderQuotaReportView): ProviderCapacityAggregationView | null {
  const row = asRecord(report?.aggregation);
  if (!row || row.kind !== "capacity-weighted-v1" || row.scope !== "routable-known") return null;
  const excludedAccounts = asNumber(row.excludedAccounts);
  const unknownPlanAccounts = asNumber(row.unknownPlanAccounts);
  const incomplete = asBoolean(row.incomplete);
  if (excludedAccounts === undefined || unknownPlanAccounts === undefined || incomplete === undefined) return null;
  const set = readAggregateWindows(row);
  return {
    presentation: capacityPresentation(row.presentation, hasAnyWindow(set)),
    incomplete,
    excludedAccounts,
    unknownPlanAccounts,
    partialWindowAccounts: asNumber(row.partialWindowAccounts) ?? 0,
    ...windowFields(set),
  };
}

/** Human label for a quota report source id (e.g. "cursor:period-usage"). */
export function formatQuotaSourceLabel(source: string | undefined): string {
  const trimmed = source?.trim();
  if (!trimmed) return "";
  const [provider = "", path] = trimmed.split(":", 2);
  if (path === undefined) return trimmed;
  return `${provider} · ${path.replace(/-/g, " ")}`;
}

/**
 * Models-tab list derivation: live models, else configured static ids, else the
 * default model alone; custom ids always join, duplicates collapse, and the query
 * filters the result.
 *
 * `hasLiveModels` comes from the server on purpose. Inferring it by subtracting
 * custom ids from `base` misreads a live catalogue as custom-only whenever a
 * custom id also exists upstream, which would wrongly keep the configured
 * fallback authoritative.
 */
export function filterModels(
  base: string[],
  defaultModel: string | undefined,
  query: string,
  configuredModels: string[] | undefined,
  customModels: string[],
  hasLiveModels: boolean,
): string[] {
  const configured = configuredModels?.length ? configuredModels : (defaultModel ? [defaultModel] : []);
  const rows = hasLiveModels ? base : configured;
  const ids = [...new Set([...rows, ...customModels])];
  const needle = query.trim().toLowerCase();
  return needle ? ids.filter(id => id.toLowerCase().includes(needle)) : ids;
}
