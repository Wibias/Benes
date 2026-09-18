/** Badge, action, and quota-mode policy for Codex account cards. */
import type { CodexAccountModeState } from "../codex-multi-state";
import type { CodexAccountEntry } from "../components/codex-account-pool-types";

type HealthStatus = "healthy" | "cooldown" | "reauth_required" | "warning";

export type CodexAccountHealthFlags = {
  healthStatus: HealthStatus | undefined;
  showReauth: boolean;
  inCooldown: boolean;
};

export function codexAccountHealthFlags(
  account: { needsReauth?: boolean; health?: { status?: HealthStatus } | null } | null | undefined,
): CodexAccountHealthFlags {
  const healthStatus = account?.health?.status;
  return {
    healthStatus,
    showReauth: Boolean(account?.needsReauth) || healthStatus === "reauth_required",
    inCooldown: healthStatus === "cooldown",
  };
}

export function poolCardIsNext(account: { id: string; paused: boolean }, activeId: string | null): boolean {
  return !account.paused && activeId === account.id;
}

export function poolCardDotClass(
  showReauth: boolean,
  isNext: boolean,
): "dot-amber" | "dot-blue" | "dot-muted" {
  if (showReauth) return "dot-amber";
  if (isNext) return "dot-blue";
  return "dot-muted";
}

export function mainCardDotClass(showReauth: boolean): "dot-amber" | "dot-green" {
  return showReauth ? "dot-amber" : "dot-green";
}

export function codexAccountShowsSwitch(input: {
  paused: boolean;
  isActive: boolean;
  showReauth: boolean;
  inCooldown: boolean;
}): boolean {
  return !input.paused && !input.isActive && !input.showReauth && !input.inCooldown;
}

export function poolCardNextBadgeKey(
  accountModeState: CodexAccountModeState | null,
): "codexAuth.poolPrepared" | "codexAuth.nextSession" {
  return accountModeState === "direct" ? "codexAuth.poolPrepared" : "codexAuth.nextSession";
}

export function poolCardShowsNeedsReauthBadge(
  showReauth: boolean,
  healthLabel: string | null | undefined,
): boolean {
  return Boolean(showReauth && !healthLabel);
}

export function poolCardShowsNextBadge(
  isNext: boolean,
  showReauth: boolean,
  inCooldown: boolean,
): boolean {
  return isNext && !showReauth && !inCooldown;
}

export function poolCardShowsPinned(
  id: string,
  pinnedId: string | null | undefined,
  paused: boolean,
): boolean {
  return id === pinnedId && !paused;
}

export type CodexAccountQuotaMode = "reauth" | "cooldown" | "bars";

export function codexAccountQuotaMode(
  showReauth: boolean,
  inCooldown: boolean,
): CodexAccountQuotaMode {
  if (showReauth) return "reauth";
  if (inCooldown) return "cooldown";
  return "bars";
}

export function mainAccountSessionBadge(
  paused: boolean,
  isMainActive: boolean,
  accountModeState: CodexAccountModeState | null,
): {
  show: boolean;
  className: "badge-primary" | "badge-muted";
  key: "codexAuth.poolPrepared" | "codexAuth.nextSession" | "codexAuth.current";
} {
  if (paused) {
    return { show: false, className: "badge-muted", key: "codexAuth.current" };
  }
  if (isMainActive) {
    return {
      show: true,
      className: "badge-primary",
      key: accountModeState === "direct" ? "codexAuth.poolPrepared" : "codexAuth.nextSession",
    };
  }
  return { show: true, className: "badge-muted", key: "codexAuth.current" };
}

export function mainCardSwitchEntry(
  main: CodexAccountEntry | undefined,
  fallbackEmail: string,
): CodexAccountEntry {
  return {
    id: "__main__",
    email: main?.email || fallbackEmail,
    plan: main?.plan,
    isMain: true,
    paused: main?.paused ?? false,
    priority: main?.priority ?? 0,
    hasCredential: true,
    quota: main?.quota ?? null,
  };
}

export function mainQuotaPending(main: { quota: unknown } | undefined): boolean {
  return main != null && main.quota == null;
}

export function poolQuotaPending(quota: unknown): boolean {
  return quota == null;
}

export function poolCardOrderDisabled(
  priorityUpdatingId: string | null,
  switchingId: string | null,
): boolean {
  return priorityUpdatingId !== null || switchingId !== null;
}
