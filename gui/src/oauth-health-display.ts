/**
 * Dashboard helpers for OAuth account health badges and localized copy.
 *
 * Structured `health` comes from the management API; labels and summaries are rendered
 * through `t(...)` so non-English locales never show English API text. Clipboard writes
 * are owned by `copy-feedback` — this module only decides how health reads.
 */

import { displayAccountId } from "./lib/privacy.ts";
import type { TFn, TKey } from "./i18n/shared";

/** Badge tone per status. The keys are the status vocabulary. */
const TONE_BY_STATUS = {
  healthy: "ok",
  cooldown: "muted",
  reauth_required: "warn",
  warning: "warn",
} as const;

export type OAuthHealthStatus = keyof typeof TONE_BY_STATUS;

/** Badge class per tone. The keys are the tone vocabulary. */
const CLASS_BY_TONE = {
  ok: "badge badge-green",
  warn: "badge badge-amber",
  muted: "badge badge-muted",
} as const;

export type OAuthHealthBadgeTone = keyof typeof CLASS_BY_TONE;

export type OAuthHealthReason =
  | "rate_limit"
  | "quota"
  | "unauthorized"
  | "forbidden"
  | "refresh_failed"
  | "refresh_conflict"
  | "metadata_mismatch"
  | "stale_credentials";

export interface OAuthHealthView {
  status: OAuthHealthStatus;
  reason?: OAuthHealthReason | string;
  until?: string;
}

/** The account shape `accountNeedsReauth` reads. */
export interface OAuthHealthAccount {
  needsReauth?: boolean;
  health?: { status?: OAuthHealthStatus } | null;
}

export function oauthHealthBadgeTone(healthStatus: OAuthHealthStatus | undefined): OAuthHealthBadgeTone {
  return healthStatus === undefined ? "muted" : TONE_BY_STATUS[healthStatus];
}

export function oauthHealthBadgeClass(healthStatus: OAuthHealthStatus | undefined): string {
  return CLASS_BY_TONE[oauthHealthBadgeTone(healthStatus)];
}

/** Whether the UI should offer reauthenticate (not during cooldown-only). */
export function oauthHealthShowsReauth(healthStatus: OAuthHealthStatus | undefined): boolean {
  return healthStatus === "reauth_required";
}

/**
 * Aggregate/row reauth gate: legacy `needsReauth` OR canonical health projection.
 * Health-only `reauth_required` must still demote Providers overview attention state.
 */
export function accountNeedsReauth(account: OAuthHealthAccount | null | undefined): boolean {
  if (account === null || account === undefined) return false;
  return Boolean(account.needsReauth) || oauthHealthShowsReauth(account.health?.status);
}

/** Cooldown: show wait copy; do not urge probing or immediate retry. */
export function oauthHealthIsCooldown(healthStatus: OAuthHealthStatus | undefined): boolean {
  return healthStatus === "cooldown";
}

/** Non-healthy states where copying `benes doctor` is a useful next step. */
export function oauthHealthShowsDoctor(healthStatus: OAuthHealthStatus | undefined): boolean {
  return healthStatus === "warning" || healthStatus === "reauth_required";
}

const COOLDOWN_LABEL: Readonly<Record<string, TKey>> = {
  rate_limit: "pws.healthLabel.rateLimited",
};

const REAUTH_LABEL: Readonly<Record<string, TKey>> = {
  refresh_failed: "pws.healthLabel.refreshFailed",
};

const WARNING_LABEL: Readonly<Record<string, TKey>> = {
  refresh_conflict: "pws.healthLabel.credentialConflict",
  metadata_mismatch: "pws.healthLabel.metadataMismatch",
  stale_credentials: "pws.healthLabel.refreshFailed",
};

function labelFor(view: OAuthHealthView): TKey {
  const reason = typeof view.reason === "string" ? view.reason : "";
  if (view.status === "cooldown") return COOLDOWN_LABEL[reason] ?? "pws.healthLabel.quotaLimited";
  if (view.status === "reauth_required") return REAUTH_LABEL[reason] ?? "pws.healthLabel.reauthRequired";
  return WARNING_LABEL[reason] ?? "pws.healthLabel.reauthRequired";
}

export function oauthHealthLabelKey(view: OAuthHealthView | undefined): TKey | null {
  if (view === undefined || view.status === "healthy") return null;
  return labelFor(view);
}

export function formatOAuthHealthLabel(translate: TFn, view: OAuthHealthView | undefined): string | null {
  const key = oauthHealthLabelKey(view);
  return key === null ? null : translate(key);
}

function accountLabel(translate: TFn, id: string): string {
  return id === "__main__" ? translate("codexAuth.mainAccount") : displayAccountId(id);
}

export function formatOAuthHealthSummary(
  translate: TFn,
  provider: string,
  accountId: string,
  view: OAuthHealthView | undefined,
): string | null {
  if (view === undefined || view.status === "healthy") return null;
  const account = accountLabel(translate, accountId);
  if (view.status === "cooldown") {
    const until = view.until ? new Date(view.until).toLocaleString() : "";
    const summaryKey = view.reason === "rate_limit"
      ? "pws.healthSummary.rateLimited"
      : "pws.healthSummary.quotaLimited";
    return translate(summaryKey, { provider, account, until });
  }
  if (view.status === "reauth_required") {
    return translate("pws.healthSummary.reauthRequired", { provider, account });
  }
  const fallbackKey = view.reason === "refresh_conflict"
    ? "pws.healthSummary.credentialConflict"
    : view.reason === "metadata_mismatch"
      ? "pws.healthSummary.metadataMismatch"
      : "pws.healthSummary.staleCredentials";
  return translate(fallbackKey, { provider, account });
}

/** Scope resolution moved to useCopyFeedback; this only maps an outcome to copy. */
export function doctorCopyButtonLabel(
  translate: TFn,
  settled: "copied" | "unavailable" | null | undefined,
): string {
  if (!settled) return translate("pws.copyDoctor");
  return settled === "copied" ? translate("pws.doctorCopied") : translate("pws.doctorCopyUnavailable");
}
