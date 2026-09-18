/** Toast, selection, and reset-credit policy for the Codex account pool. */
import type { TKey } from "../i18n/shared";

export function poolActiveNonMainAccount<T extends { id: string }>(
  accounts: T[],
  activeId: string | null,
): T | null {
  if (!activeId || activeId === "__main__") return null;
  return accounts.find(account => account.id === activeId) ?? null;
}

export function poolSwitchTargetId(confirmId: string): string {
  return confirmId === "__main__" ? "__main__" : confirmId;
}

export function poolAccountDisplayKind(selectedId: string | null): "main" | "pool" {
  return selectedId && selectedId !== "__main__" ? "pool" : "main";
}

export function poolSwitchToastKind(
  accountModeState: string | null,
): "prepared" | "switched" {
  return accountModeState === "direct" ? "prepared" : "switched";
}

export function poolMutationBusy(ok: boolean, reason: string | undefined): boolean {
  return !ok && reason === "busy";
}

export function poolPauseToastKey(ok: boolean, paused: boolean): TKey {
  if (ok) return paused ? "codexAuth.pauseSucceeded" : "codexAuth.resumeSucceeded";
  return paused ? "codexAuth.pauseFailed" : "codexAuth.resumeFailed";
}

export function poolPriorityUnchanged(next: number, current: number): boolean {
  return next === current;
}

export type PoolRemoveToastKind = "failed" | "refresh-pending" | "silent";

export function poolRemoveToastKind(
  ok: boolean,
  catalogRefreshPending?: boolean,
): PoolRemoveToastKind {
  if (!ok) return "failed";
  if (catalogRefreshPending) return "refresh-pending";
  return "silent";
}

export type PoolPauseExhaustedToastKind = "busy" | "failed" | "none" | "succeeded";

export function poolPauseExhaustedToastKind(
  ok: boolean,
  reason: string | undefined,
  pausedCount: number,
): PoolPauseExhaustedToastKind {
  if (!ok && reason === "busy") return "busy";
  if (!ok) return "failed";
  return pausedCount > 0 ? "succeeded" : "none";
}

export function poolAccountAddedFeedbackKind(
  catalogRefreshPending: boolean,
): "pending" | "added" {
  return catalogRefreshPending ? "pending" : "added";
}

export type ResetCreditStamp = { granted_at: string; expires_at: string };

export function sortResetCredits(credits: ResetCreditStamp[]): ResetCreditStamp[] {
  return credits.toSorted(
    (left, right) => new Date(left.granted_at).getTime() - new Date(right.granted_at).getTime(),
  );
}

export function decodeResetCreditsPayload(
  data: { credits?: ResetCreditStamp[] } | null | undefined,
): ResetCreditStamp[] | null {
  if (!data) return null;
  return sortResetCredits(data.credits ?? []);
}

export function redeemResultClosesModal(close: boolean | undefined): boolean {
  return close === true;
}
