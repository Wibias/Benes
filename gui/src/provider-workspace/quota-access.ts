/**
 * Which limit windows Overview may render for a provider.
 *
 * Mirrors `probeProvider` in `internal/quota/fetch.go` plus Codex forward
 * reports from `codexQuotaProviderIDs`. Live `/api/provider-quotas` rows still
 * win: this only fills empty bars when the provider has an access point but
 * nothing has been fetched yet (logged out, missing key, not validated).
 */
import type { AccountQuota } from "../codex-quota-utils";
import type { WorkspaceItem } from "./catalog";
import { quotaAccessPolicy, type QuotaAccess, type QuotaWindowKey } from "./quota-policy";
import { accountQuotaFromReport, type ProviderQuotaReportView } from "./report";

export type { QuotaAccess, QuotaWindowKey } from "./quota-policy";

export function quotaAccessForProvider(
  item: Pick<WorkspaceItem, "name" | "baseUrl" | "authMode" | "adapter">,
): QuotaAccess {
  return quotaAccessPolicy(item);
}

function emptyQuota(windows: readonly QuotaWindowKey[]): AccountQuota {
  return {
    ...(windows.includes("fiveHour") ? { fiveHourPercent: 0 } : {}),
    ...(windows.includes("weekly") ? { weeklyPercent: 0 } : {}),
    ...(windows.includes("monthly") ? { monthlyPercent: 0 } : {}),
    updatedAt: 0,
  };
}

function overlayLiveWindows(live: AccountQuota, windows: readonly QuotaWindowKey[]): AccountQuota {
  return {
    updatedAt: live.updatedAt,
    ...(windows.includes("fiveHour") ? {
      fiveHourPercent: live.fiveHourPercent ?? 0,
      ...(live.fiveHourResetAt !== undefined ? { fiveHourResetAt: live.fiveHourResetAt } : {}),
    } : {}),
    ...(windows.includes("weekly") ? {
      weeklyPercent: live.weeklyPercent ?? 0,
      ...(live.weeklyResetAt !== undefined ? { weeklyResetAt: live.weeklyResetAt } : {}),
    } : {}),
    ...(windows.includes("monthly") ? {
      monthlyPercent: live.monthlyPercent ?? 0,
      ...(live.monthlyResetAt !== undefined ? { monthlyResetAt: live.monthlyResetAt } : {}),
    } : {}),
    ...(live.customWindows && live.customWindows.length > 0 ? { customWindows: live.customWindows } : {}),
  };
}

/** Bars for Overview: live report windows, else the provider's known empty slots. */
export function overviewQuotaBars(
  item: Pick<WorkspaceItem, "name" | "baseUrl" | "authMode" | "adapter">,
  report?: ProviderQuotaReportView,
): { show: boolean; quota: AccountQuota | null; pending: boolean } {
  const live = accountQuotaFromReport(report);
  const access = quotaAccessForProvider(item);
  if (live) {
    if (access.kind === "windows") return { show: true, quota: overlayLiveWindows(live, access.windows), pending: false };
    return { show: true, quota: live, pending: false };
  }
  if (access.kind === "windows") return { show: true, quota: emptyQuota(access.windows), pending: false };
  if (access.kind === "custom") return { show: true, quota: null, pending: true };
  return { show: false, quota: null, pending: false };
}
