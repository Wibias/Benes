/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";
import { readJsonIfOk } from "../fetch-json";
import { consumeResetCreditBody } from "../lib/codex-account-reset-policy";

/** Codes that mean the credit is gone, whether this call consumed it or an earlier one did. */
const REDEEMED_CODES: ReadonlySet<string> = new Set(["reset", "already_redeemed"]);

/** Refusal codes and the copy that explains each one. */
const REFUSAL_KEYS = {
  nothing_to_reset: "codexAuth.resetNothingToReset",
  no_credit: "codexAuth.resetNoCredit",
} as const;

const RESET_ERROR_KEY = "codexAuth.resetError";

export interface ResetCreditOutcome {
  ok: boolean;
  toast?: string;
  close?: boolean;
}

interface ResetCreditResponse {
  code: string;
  remaining?: unknown;
}

/** Authoritative remaining count, or the generic success line when the API omits it. */
function remainingToast(translate: TFn, reported: unknown): string {
  if (typeof reported !== "number" || !Number.isFinite(reported)) {
    return translate("codexAuth.resetSuccessGeneric");
  }
  return translate("codexAuth.resetSuccess", { remaining: String(Math.max(0, reported)) });
}

function refusal(translate: TFn, code: string): ResetCreditOutcome {
  const key = code === "nothing_to_reset"
    ? REFUSAL_KEYS.nothing_to_reset
    : code === "no_credit"
      ? REFUSAL_KEYS.no_credit
      : RESET_ERROR_KEY;
  return { ok: false, close: true, toast: translate(key) };
}

/**
 * Redeem one earned reset credit for an account.
 *
 * `reload(true)` runs before the toast is built so the reported remaining count comes
 * from the refreshed quota snapshot rather than the modal's stale one.
 */
export async function redeemResetCredit(
  apiBase: string,
  accountId: string,
  translate: TFn,
  reload: (refresh?: boolean) => Promise<boolean>,
): Promise<ResetCreditOutcome> {
  const request: RequestInit = {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(consumeResetCreditBody(accountId, crypto.randomUUID())),
  };
  try {
    const response = await fetch(`${apiBase}/api/codex-auth/reset-credits/consume`, request);
    const body = await readJsonIfOk<ResetCreditResponse>(response);
    if (!body) return { ok: false, toast: translate(RESET_ERROR_KEY) };

    if (!REDEEMED_CODES.has(body.code)) return refusal(translate, body.code);

    await reload(true);
    return { ok: true, close: true, toast: remainingToast(translate, body.remaining) };
  } catch {
    return { ok: false, toast: translate(RESET_ERROR_KEY) };
  }
}
