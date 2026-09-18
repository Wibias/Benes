/** Reset-credit availability, confirm-copy, and popup-shape policy for the reset modal. */

import type { CodexAccountEntry } from "../components/codex-account-pool-types";

/** One earned credit as the listener lists it. */
export type ResetCredit = { granted_at: string; expires_at: string };

/**
 * The reset popup, exactly as the lane tracks it.
 *
 * Declared with the rest of the reset policy rather than beside either consumer, so the
 * lane and the modal cannot drift into two shapes for the same popup.
 */
export interface ResetPopupView {
  account: CodexAccountEntry;
  credits: ResetCredit[] | null;
  loadingCredits: boolean;
  confirming: boolean;
  redeeming: boolean;
}

export function resetCreditCount(quota: { resetCredits?: number } | null | undefined): number {
  return quota?.resetCredits ?? 0;
}

export function resetCreditsAvailable(count: number): boolean {
  return count > 0;
}

export function resetConfirmCredit<T>(credits: T[] | null | undefined): T | undefined {
  return credits?.[0];
}

export type CodexResetModalView = "available" | "empty" | "confirm";

export function codexResetModalView(
  resetConfirm: boolean,
  creditCount: number,
): CodexResetModalView {
  if (resetConfirm) return "confirm";
  return resetCreditsAvailable(creditCount) ? "available" : "empty";
}

const redeemRequestIdPattern =
  /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;

export function isRedeemRequestId(value: string): boolean {
  return redeemRequestIdPattern.test(value);
}

export function consumeResetCreditBody(
  accountId: string,
  redeemRequestId: string,
): { accountId: string; redeemRequestId: string } {
  return { accountId, redeemRequestId };
}
