/** Verify-step catalog and quota: live discovery beats the preset seed. */
import { remainingQuotaPercent } from "../codex-quota-utils.ts";
import { postModelDiscoverySync } from "../model-discovery-sync.ts";
import { quotaAccessPolicy, type QuotaPolicyItem } from "../provider-workspace/quota-policy.ts";
import { accountQuotaFromReport } from "../provider-workspace/report.ts";
import { freshQuotaReportsFromResponse } from "../provider-workspace/workspace-shell.ts";

export type VerifyQuotaWindow = {
  remainingPercent: number;
  labelKey?: "quota.fiveHourLimit" | "quota.weeklyLimit" | "quota.monthlyLimit";
  label?: string;
};

export type VerifyQuotaKind = "loading" | "none" | "empty" | "windows";

export type AddProviderVerifyLive = {
  models?: string[];
  windows: VerifyQuotaWindow[];
  creditsRemaining?: number;
};

export function verifyDisplayedModels(
  live: readonly string[] | undefined,
  seed: readonly string[],
  loading = false,
): string[] {
  if (live && live.length > 0) return [...live];
  if (loading) return [];
  return [...seed];
}

export function verifyQuotaWindows(quota: {
  fiveHourPercent?: number;
  weeklyPercent?: number;
  monthlyPercent?: number;
  customWindows?: { label: string; percent: number }[];
}): VerifyQuotaWindow[] {
  const out: VerifyQuotaWindow[] = [];
  if (typeof quota.fiveHourPercent === "number" && Number.isFinite(quota.fiveHourPercent)) {
    out.push({ labelKey: "quota.fiveHourLimit", remainingPercent: remainingQuotaPercent(quota.fiveHourPercent) });
  }
  if (typeof quota.weeklyPercent === "number" && Number.isFinite(quota.weeklyPercent)) {
    out.push({ labelKey: "quota.weeklyLimit", remainingPercent: remainingQuotaPercent(quota.weeklyPercent) });
  }
  if (typeof quota.monthlyPercent === "number" && Number.isFinite(quota.monthlyPercent)) {
    out.push({ labelKey: "quota.monthlyLimit", remainingPercent: remainingQuotaPercent(quota.monthlyPercent) });
  }
  for (const window of quota.customWindows ?? []) {
    if (typeof window.label !== "string" || window.label.trim() === "") continue;
    if (typeof window.percent !== "number" || !Number.isFinite(window.percent)) continue;
    out.push({ label: window.label, remainingPercent: remainingQuotaPercent(window.percent) });
  }
  return out;
}

export function verifyQuotaKind(input: {
  testing: boolean;
  reportsQuota: boolean;
  windows: readonly VerifyQuotaWindow[];
  hasCredits?: boolean;
}): VerifyQuotaKind {
  if (input.testing) return "loading";
  if (input.windows.length > 0 || input.hasCredits) return "windows";
  if (input.reportsQuota) return "empty";
  return "none";
}

export function providerReportsQuota(item: QuotaPolicyItem): boolean {
  return quotaAccessPolicy(item).kind !== "none";
}

export function creditsRemainingFromQuota(quota: unknown): number | undefined {
  if (!quota || typeof quota !== "object" || Array.isArray(quota)) return undefined;
  const credits = (quota as { creditsUsd?: unknown }).creditsUsd;
  if (!credits || typeof credits !== "object" || Array.isArray(credits)) return undefined;
  const row = credits as { remaining?: unknown; unlimited?: unknown };
  if (row.unlimited === true) return undefined;
  const remaining = row.remaining;
  if (typeof remaining !== "number" || !Number.isFinite(remaining) || remaining < 0) return undefined;
  return remaining;
}

export function formatCreditsUsd(remaining: number): string {
  return `$${remaining.toFixed(2)}`;
}

export function verifyWindowLabel(
  window: VerifyQuotaWindow,
  t: (key: "quota.fiveHourLimit" | "quota.weeklyLimit" | "quota.monthlyLimit") => string,
): string {
  if (window.labelKey) return t(window.labelKey);
  return window.label ?? "";
}

export type AddProviderVerifyEnsureLive = AddProviderVerifyLive & {
  configured: boolean;
};

/** OAuth Verify: create the config row (409 = already present), then discover. */
export async function ensureConfiguredThenLoadAddProviderVerifyLive(
  apiBase: string,
  postBody: { name: string; provider: unknown },
  fetchImpl: typeof fetch = fetch,
): Promise<AddProviderVerifyEnsureLive> {
  try {
    const res = await fetchImpl(`${apiBase}/api/providers`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(postBody),
    });
    if (!res.ok && res.status !== 409) return { configured: false, windows: [] };
  } catch {
    return { configured: false, windows: [] };
  }
  const live = await loadAddProviderVerifyLive(apiBase, postBody.name, fetchImpl);
  return { ...live, configured: true };
}

export async function loadAddProviderVerifyLive(
  apiBase: string,
  provider: string,
  fetchImpl: typeof fetch = fetch,
): Promise<AddProviderVerifyLive> {
  const [sync, quotaPayload] = await Promise.all([
    postModelDiscoverySync(apiBase, provider, fetchImpl).catch(() => ({ ok: false as const, applicable: false })),
    fetchImpl(`${apiBase}/api/provider-quotas?refresh=1`)
      .then(async (res) => {
        if (!res.ok) return null;
        return await res.json().catch(() => null);
      })
      .catch(() => null),
  ]);
  const report = freshQuotaReportsFromResponse(quotaPayload)[provider];
  const quota = accountQuotaFromReport(report);
  return {
    ...(sync.ok && sync.models && sync.models.length > 0 ? { models: sync.models } : {}),
    windows: quota ? verifyQuotaWindows(quota) : [],
    creditsRemaining: creditsRemainingFromQuota(report?.quota),
  };
}
