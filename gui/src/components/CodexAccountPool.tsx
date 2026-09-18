/**
 * Codex account pool — the OAuth-accounts lane of Providers → OpenAI → Access.
 *
 * Providers owns the shared controller and the Access page owns credential selection, so
 * this component owns account presentation and account actions only. It deliberately mounts
 * no rotation or auto-switch policy stack: a second owner would race the page's own one.
 */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import type { ReactNode } from "react";
import { useT } from "../i18n/shared";
import { type NoticeTone } from "../ui";
import { useCodexAccountPool, type CodexAccountPoolController } from "../hooks/useCodexAccountPool";
import type { CodexAccountModeState } from "../codex-multi-state";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import { accountNeedsReauth } from "../oauth-health-display";
import { useCopyFeedback } from "./use-copy-feedback";
import type { CodexAccountMutationCompletion } from "../codex-account-mutation";
import {
  poolAccountAddedFeedbackKind,
  poolActiveNonMainAccount,
  poolSwitchTargetId,
} from "../lib/codex-account-pool-action-policy";
import type { ResetPopupView } from "../lib/codex-account-reset-policy";
import {
  changePoolAccountPriority,
  loadResetCredits,
  pauseExhaustedPoolAccounts,
  redeemPoolAccountCredit,
  removePoolAccount,
  savePoolAccountAlias,
  switchPoolAccount,
  togglePoolAccountPaused,
  type PoolActionContext,
} from "./codex-account-pool-mutations";
import {
  CodexAccountPoolEmbeddedSurface,
  type CodexAccountPoolSurfaceActions,
} from "./codex-account-pool-surfaces";

// Single definition lives with the controller that owns this data (WP3).
export type { CodexAccountEntry } from "../hooks/useCodexAccountPool";

/** The command the lane offers to copy for a credential the operator has to repair. */
const DOCTOR_CMD = "benes doctor";

/** How long an action's toast stays up. */
const TOAST_VISIBLE_MS = 5000;

/** The add dialog starts closed; re-authentication reuses it with an account id. */
const CLOSED_ADD_DIALOG = { open: false, reauthId: null } as const;

export interface CodexAccountPoolProps {
  apiBase: string;
  accountModeState?: CodexAccountModeState | null;
  /** Replaces the OAuth table title when Access shows the credential-method switcher. */
  accessHeading?: ReactNode;
  onActiveNeedsReauthChange?: (needs: boolean) => void;
  controller?: CodexAccountPoolController;
}

/**
 * The lane's one toast.
 *
 * A new message replaces the previous one and restarts the timer, so two actions finishing
 * close together cannot leave the first one on screen with the second one's tone.
 */
function useActionToast() {
  const [toast, setToast] = useState<{ text: string; tone: NoticeTone } | null>(null);
  const timerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

  const stopTimer = useCallback(() => {
    if (timerRef.current === null) return;
    clearTimeout(timerRef.current);
    timerRef.current = null;
  }, []);

  useEffect(() => stopTimer, [stopTimer]);

  const announce = useCallback((text: string, tone: NoticeTone = "ok") => {
    stopTimer();
    setToast({ text, tone });
    timerRef.current = setTimeout(() => {
      setToast(null);
      timerRef.current = null;
    }, TOAST_VISIBLE_MS);
  }, [stopTimer]);

  return { toast, announce };
}

/**
 * The add / re-authenticate dialog, and the pause lease that keeps it still.
 *
 * A poll repainting the table under an open dialog would move the controls the operator is
 * aiming at, so the lease is taken here rather than at the call sites that open the dialog:
 * every route into it then holds the refresh for exactly as long as the dialog exists.
 */
function useAddDialog(controller: CodexAccountPoolController) {
  const [dialog, setDialog] = useState<{ open: boolean; reauthId: string | null }>(CLOSED_ADD_DIALOG);
  const close = useCallback(() => setDialog(CLOSED_ADD_DIALOG), []);
  const openFor = useCallback((reauthId: string | null) => setDialog({ open: true, reauthId }), []);

  useEffect(() => {
    if (!dialog.open) return;
    const token = controller.pauseRefresh();
    return () => controller.resumeRefresh(token);
  }, [controller, dialog.open]);

  return { dialog, openFor, close };
}

/**
 * The reset-credit popup.
 *
 * Opening paints the account immediately and fills the credit list when the read lands; the
 * request id is what stops a slow list from a closed popup landing on the next one. The list
 * is decoration over the modal's own count, so a failed read leaves it empty rather than
 * blocking the confirmation.
 */
function useResetPopup(apiBase: string) {
  const [popup, setPopup] = useState<ResetPopupView | null>(null);
  const requestRef = useRef(0);

  const open = useCallback((account: CodexAccountEntry) => {
    const request = requestRef.current + 1;
    requestRef.current = request;
    setPopup({ account, credits: null, loadingCredits: true, confirming: false, redeeming: false });
    void (async () => {
      const credits = await loadResetCredits(apiBase, account.id);
      if (requestRef.current !== request) return;
      setPopup(current => (current === null ? current : { ...current, credits, loadingCredits: false }));
    })();
  }, [apiBase]);

  const close = useCallback(() => {
    requestRef.current += 1;
    setPopup(null);
  }, []);

  const amend = useCallback((patch: Partial<ResetPopupView>) => {
    setPopup(current => (current === null ? current : { ...current, ...patch }));
  }, []);

  return { popup, open, close, amend };
}

/**
 * The Codex OAuth-account lane.
 *
 * accountModeState arrives as a prop (the workspace owns the config read) and controller is
 * the Providers-owned pool, so a mutation on Overview is immediately visible on Access. Only
 * an unowned mount falls back to its own controller.
 */
export default function CodexAccountPool({
  apiBase,
  accountModeState = null,
  accessHeading,
  onActiveNeedsReauthChange,
  controller: injectedController,
}: CodexAccountPoolProps) {
  const t = useT();
  const ownController = useCodexAccountPool(apiBase, !injectedController);
  const controller = injectedController ?? ownController;
  const { accounts, activeId, load } = controller;

  const { toast, announce } = useActionToast();
  const addDialog = useAddDialog(controller);
  const reset = useResetPopup(apiBase);
  const doctorCopy = useCopyFeedback<string>();

  const [confirmSwitch, setConfirmSwitch] = useState<CodexAccountEntry | null>(null);
  const [refreshingQuota, setRefreshingQuota] = useState(false);

  const activePoolNeedsReauth = useMemo(() => {
    const account = poolActiveNonMainAccount(accounts, activeId);
    return !account?.paused && accountNeedsReauth(account);
  }, [accounts, activeId]);

  useEffect(() => {
    onActiveNeedsReauthChange?.(activePoolNeedsReauth);
  }, [activePoolNeedsReauth, onActiveNeedsReauthChange]);

  const refreshQuotas = useCallback(async () => {
    setRefreshingQuota(true);
    try {
      const ok = await load(true);
      announce(t(ok ? "codexAuth.quotaRefreshed" : "codexAuth.quotaRefreshFailed"), ok ? "ok" : "err");
    } finally {
      setRefreshingQuota(false);
    }
  }, [announce, load, t]);

  const handleAccountAdded = useCallback((completion: CodexAccountMutationCompletion) => {
    void controller.syncAfterAccountAdded();
    const kind = poolAccountAddedFeedbackKind(completion.catalogRefreshPending);
    announce(
      t(kind === "pending" ? "codexAuth.catalogRefreshPending" : "codexAuth.accountAdded"),
      kind === "pending" ? "warn" : "ok",
    );
    addDialog.close();
  }, [addDialog, announce, controller, t]);

  // The one context every action reads, so no action has to be handed the controller, the
  // rows, the translator and the toast separately.
  const actionContext: PoolActionContext = { controller, accounts, accountModeState, t, announce };

  const actions: CodexAccountPoolSurfaceActions = {
    onRefresh: () => { void refreshQuotas(); },
    onPauseExhausted: () => { void pauseExhaustedPoolAccounts(actionContext); },
    onAddAccount: () => addDialog.openFor(null),
    onTogglePause: account => {
      void togglePoolAccountPaused(actionContext, account).then(applied => {
        // The row changed under the dialog, so a confirmation for it is no longer the
        // operator's question.
        if (applied) setConfirmSwitch(current => (current?.id === account.id ? null : current));
      });
    },
    onPriorityChange: (account, priority) => {
      void changePoolAccountPriority(actionContext, account, priority);
    },
    onReauth: id => addDialog.openFor(id),
    onEditAlias: account => { void savePoolAccountAlias(actionContext, account); },
    onRemove: id => { void removePoolAccount(actionContext, id); },
    onSwitch: setConfirmSwitch,
    onOpenReset: account => reset.open(account),
    onCopyDoctor: accountId => { doctorCopy.copy(DOCTOR_CMD, accountId); },
    onConfirmSwitch: () => {
      const target = confirmSwitch;
      if (target === null) return;
      void switchPoolAccount(actionContext, poolSwitchTargetId(target.id)).then(done => {
        if (done) setConfirmSwitch(null);
      });
    },
    onCancelSwitch: () => setConfirmSwitch(null),
    onCloseAdd: addDialog.close,
    onAccountAdded: handleAccountAdded,
    onCloseReset: reset.close,
    onShowResetConfirm: () => reset.amend({ confirming: true }),
    onCancelResetConfirm: () => reset.amend({ confirming: false }),
    onRedeem: () => {
      const popup = reset.popup;
      if (popup === null) return;
      reset.amend({ redeeming: true });
      void redeemPoolAccountCredit(actionContext, apiBase, popup.account.id, load).then(closes => {
        reset.amend({ redeeming: false });
        if (closes) reset.close();
      });
    },
  };

  return (
    <CodexAccountPoolEmbeddedSurface
      view={{
        apiBase,
        controller,
        accountModeState,
        refreshingQuota,
        actionFeedback: toast?.text ?? null,
        actionFeedbackTone: toast?.tone ?? null,
        heading: accessHeading,
        main: accounts.find(account => account.isMain),
        doctorCopyOutcomeFor: doctorCopy.outcomeFor,
      }}
      dialogs={{ confirm: confirmSwitch, add: addDialog.dialog, reset: reset.popup }}
      actions={actions}
    />
  );
}
