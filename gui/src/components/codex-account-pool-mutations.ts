/** Named mutation outcomes for the Codex account-pool surface. */
import type { TFn } from "../i18n/shared";
import type { NoticeTone } from "../ui";
import { readJsonIfOk } from "../fetch-json";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import type { CodexAccountModeState } from "../codex-multi-state";
import type { CodexAccountPoolController } from "../hooks/useCodexAccountPool";
import { redeemResetCredit } from "./codex-account-pool-handlers";
import {
  decodeResetCreditsPayload,
  poolAccountDisplayKind,
  poolMutationBusy,
  poolPauseExhaustedToastKind,
  poolPauseToastKey,
  poolPriorityUnchanged,
  poolRemoveToastKind,
  poolSwitchToastKind,
} from "../lib/codex-account-pool-action-policy";
import type { ResetCredit } from "../lib/codex-account-reset-policy";

/**
 * What every pool action needs: the one shared controller, the rows it may act on, and the
 * two ways it reports back.
 *
 * The container builds this once and each action then takes only its own subject, so the
 * subject is what reads in the call site instead of a run of positional plumbing.
 */
export interface PoolActionContext {
  controller: CodexAccountPoolController;
  accounts: CodexAccountEntry[];
  accountModeState: CodexAccountModeState | null;
  /** Translator for toast copy. */
  t: TFn;
  /** Shows one toast; success is the default tone. */
  announce: (text: string, tone?: NoticeTone) => void;
}

/** A pool result only carries a reason on the refusal branch. */
function refusalReason(result: { ok: boolean }): string | undefined {
  const reason = (result as { reason?: unknown }).reason;
  return typeof reason === "string" ? reason : undefined;
}

/** The operator's own name for an account, falling back to the listener's identity. */
function accountLabel(account: CodexAccountEntry): string {
  return account.alias ?? account.email;
}

/** What to call an account the caller only has an id for. */
function labelForId(ctx: PoolActionContext, id: string): string {
  return ctx.accounts.find(account => account.id === id)?.email
    ?? ctx.t("pws.accountOrdinal", { count: "1" });
}

/**
 * Switch the pool's active account.
 *
 * A refused claim is not a failure to announce — whichever action holds the gate answers
 * for itself — so only a request the listener rejected raises the error copy.
 */
export async function switchPoolAccount(ctx: PoolActionContext, id: string | null): Promise<boolean> {
  const result = await ctx.controller.switchAccount(id);
  if (!result.ok) {
    if (!poolMutationBusy(result.ok, refusalReason(result))) {
      ctx.announce(ctx.t("codexAuth.switchFailed"), "err");
    }
    return false;
  }
  const selectedId = result.activeId;
  const name = poolAccountDisplayKind(selectedId) === "pool"
    ? (ctx.accounts.find(account => account.id === selectedId)?.email ?? ctx.t("pws.accountOrdinal", { count: "1" }))
    : ctx.t("codexAuth.mainAccount");
  ctx.announce(poolSwitchToastKind(ctx.accountModeState) === "prepared"
    ? ctx.t("codexAuth.poolPreparedToast", { email: name })
    : ctx.t("codexAuth.switched", { email: name }));
  return true;
}

/** Rename an account. A dismissed prompt is not an edit. */
export async function savePoolAccountAlias(ctx: PoolActionContext, account: CodexAccountEntry): Promise<void> {
  const entered = window.prompt(ctx.t("prov.aliasPrompt"), account.alias ?? "");
  if (entered === null) return;
  const result = await ctx.controller.saveAlias(account.id, entered);
  ctx.announce(ctx.t(result.ok ? "prov.aliasSaved" : "prov.aliasSaveFailed"), result.ok ? "ok" : "err");
}

/**
 * Pause or resume one account.
 *
 * Reports whether the write was attempted at all: the caller keeps its confirmation
 * dialog open when the gate refused.
 */
export async function togglePoolAccountPaused(ctx: PoolActionContext, account: CodexAccountEntry): Promise<boolean> {
  const paused = !account.paused;
  const result = await ctx.controller.setAccountPaused(account.id, paused);
  if (poolMutationBusy(result.ok, refusalReason(result))) return false;
  ctx.announce(
    ctx.t(poolPauseToastKey(result.ok, paused), { email: accountLabel(account) }),
    result.ok ? "ok" : "err",
  );
  return true;
}

/** Move one account's selection order. Re-selecting the same order writes nothing. */
export async function changePoolAccountPriority(
  ctx: PoolActionContext,
  account: CodexAccountEntry,
  priority: number,
): Promise<void> {
  if (poolPriorityUnchanged(priority, account.priority)) return;
  const result = await ctx.controller.setAccountPriority(account.id, priority);
  if (poolMutationBusy(result.ok, refusalReason(result))) return;
  ctx.announce(
    ctx.t(result.ok ? "accountPool.priorityUpdated" : "accountPool.priorityUpdateFailed", {
      email: accountLabel(account),
    }),
    result.ok ? "ok" : "err",
  );
}

/**
 * Remove an account, behind an explicit confirmation.
 *
 * A successful removal is silent unless the listener says its catalog still has to catch
 * up, because the row disappearing is the confirmation.
 */
export async function removePoolAccount(ctx: PoolActionContext, id: string): Promise<void> {
  if (!window.confirm(ctx.t("codexAuth.removeConfirm", { id: labelForId(ctx, id) }))) return;
  const result = await ctx.controller.removeAccount(id);
  const kind = poolRemoveToastKind(result.ok, result.ok ? result.catalogRefreshPending : undefined);
  if (kind === "failed") ctx.announce(ctx.t("codexAuth.removeFailed"), "err");
  else if (kind === "refresh-pending") ctx.announce(ctx.t("codexAuth.catalogRefreshPending"), "warn");
}

/** Pause every account whose quota is spent, reporting how many were caught. */
export async function pauseExhaustedPoolAccounts(ctx: PoolActionContext): Promise<void> {
  const result = await ctx.controller.pauseExhaustedAccounts();
  const kind = poolPauseExhaustedToastKind(
    result.ok,
    result.ok ? undefined : refusalReason(result),
    result.ok ? result.pausedCount : 0,
  );
  if (kind === "busy") return;
  const message = kind === "succeeded"
    ? ctx.t("codexAuth.pauseExhaustedSucceeded", { count: String(result.ok ? result.pausedCount : 0) })
    : kind === "none"
      ? ctx.t("codexAuth.pauseExhaustedNone")
      : ctx.t("codexAuth.pauseExhaustedFailed");
  ctx.announce(message, result.ok ? "ok" : "err");
}

/**
 * Read the credits an account still holds.
 *
 * The list is decoration on top of the modal's own count, so a failed read reports null
 * and the modal renders without it rather than blocking the confirmation.
 */
export async function loadResetCredits(apiBase: string, accountId: string): Promise<ResetCredit[] | null> {
  try {
    const response = await fetch(
      `${apiBase}/api/codex-auth/reset-credits?accountId=${encodeURIComponent(accountId)}`,
    );
    return decodeResetCreditsPayload(await readJsonIfOk<{ credits?: ResetCredit[] }>(response));
  } catch {
    return null;
  }
}

/**
 * Redeem one reset credit and report whether the modal should close.
 *
 * The redeem helper reloads the quota before it builds its toast, so the count it reports
 * is the listener's, not the one the modal was opened with.
 */
export async function redeemPoolAccountCredit(
  ctx: PoolActionContext,
  apiBase: string,
  accountId: string,
  reload: (refresh?: boolean) => Promise<boolean>,
): Promise<boolean> {
  const result = await redeemResetCredit(apiBase, accountId, ctx.t, reload);
  if (result.toast !== undefined) ctx.announce(result.toast, result.ok ? "ok" : "err");
  return result.close === true;
}
