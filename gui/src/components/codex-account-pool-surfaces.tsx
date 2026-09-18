/** The Codex account-pool lane: the OAuth account table plus the dialogs its actions open. */
import type { ReactNode } from "react";
import { useT } from "../i18n/shared";
import { type NoticeTone } from "../ui";
import AddCodexAccountModal from "./AddCodexAccountModal";
import type { CodexAccountPoolController } from "../hooks/useCodexAccountPool";
import type { CodexAccountModeState } from "../codex-multi-state";
import { CodexAccountSwitchModal } from "./codex-account-switch-modal";
import { CodexAccountResetModal } from "./codex-account-reset-modal";
import OAuthAccountsTable from "./provider-workspace/OAuthAccountsTable";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import type { CodexAccountMutationCompletion } from "../codex-account-mutation";
import type { CopyOutcome } from "./use-copy-feedback";
import type { ResetPopupView } from "../lib/codex-account-reset-policy";

/** What the lane can be asked to do. The lane owns no policy; it forwards intent. */
export type CodexAccountPoolSurfaceActions = {
  onRefresh: () => void;
  onPauseExhausted: () => void;
  onAddAccount: () => void;
  onTogglePause: (account: CodexAccountEntry) => void;
  onPriorityChange: (account: CodexAccountEntry, priority: number) => void;
  onReauth: (id: string) => void;
  onEditAlias: (account: CodexAccountEntry) => void;
  onRemove: (id: string) => void;
  onSwitch: (account: CodexAccountEntry) => void;
  onOpenReset: (account: CodexAccountEntry) => void;
  onCopyDoctor: (accountId: string) => void;
  onConfirmSwitch: () => void;
  onCancelSwitch: () => void;
  onCloseAdd: () => void;
  onAccountAdded: (completion: CodexAccountMutationCompletion) => void;
  onCloseReset: () => void;
  onShowResetConfirm: () => void;
  onCancelResetConfirm: () => void;
  onRedeem: () => void;
};

/** The state the table paints from. */
export interface CodexAccountPoolSurfaceView {
  apiBase: string;
  controller: CodexAccountPoolController;
  accountModeState: CodexAccountModeState | null;
  refreshingQuota: boolean;
  /** The lane's newest notice, rendered under the table. */
  actionFeedback: string | null;
  actionFeedbackTone: NoticeTone | null;
  /** Replaces the OAuth table title when Access shows the credential-method switcher. */
  heading?: ReactNode;
  main: CodexAccountEntry | undefined;
  doctorCopyOutcomeFor: (accountId: string) => CopyOutcome | null;
}

/** Which dialogs are open. At most one reset popup, plus the add dialog. */
export interface CodexAccountPoolDialogs {
  confirm: CodexAccountEntry | null;
  add: { open: boolean; reauthId: string | null };
  reset: ResetPopupView | null;
}

export function CodexAccountPoolEmbeddedSurface({
  view,
  dialogs,
  actions,
}: {
  view: CodexAccountPoolSurfaceView;
  dialogs: CodexAccountPoolDialogs;
  actions: CodexAccountPoolSurfaceActions;
}) {
  const t = useT();
  const { controller, accountModeState, heading } = view;

  return (
    <div>
      <OAuthAccountsTable
        controller={controller}
        refreshingQuota={view.refreshingQuota}
        pausingExhausted={controller.pausingExhausted}
        actionFeedback={view.actionFeedback}
        actionFeedbackTone={view.actionFeedbackTone}
        heading={heading}
        switchActionLabel={t(accountModeState === "direct" ? "codexAuth.prepareForPool" : "codexAuth.setAsNext")}
        doctorCopyOutcomeFor={view.doctorCopyOutcomeFor}
        onRefresh={actions.onRefresh}
        onPauseExhausted={actions.onPauseExhausted}
        onAddAccount={actions.onAddAccount}
        onTogglePause={actions.onTogglePause}
        onPriorityChange={actions.onPriorityChange}
        onReauth={actions.onReauth}
        onEditAlias={actions.onEditAlias}
        onRemove={actions.onRemove}
        onSwitch={actions.onSwitch}
        onOpenReset={actions.onOpenReset}
        onCopyDoctor={actions.onCopyDoctor}
      />
      {dialogs.confirm !== null && (
        <CodexAccountSwitchModal
          confirm={dialogs.confirm}
          mainEmail={view.main?.email}
          accountModeState={accountModeState}
          switchingId={controller.switchingId}
          orderBusy={controller.priorityUpdatingId !== null}
          onCancel={actions.onCancelSwitch}
          onConfirm={actions.onConfirmSwitch}
        />
      )}
      {dialogs.reset !== null && (
        <CodexAccountResetModal
          popup={dialogs.reset}
          onClose={actions.onCloseReset}
          onShowConfirm={actions.onShowResetConfirm}
          onCancelConfirm={actions.onCancelResetConfirm}
          onRedeem={actions.onRedeem}
        />
      )}
      {dialogs.add.open && (
        <AddCodexAccountModal
          apiBase={view.apiBase}
          reauthAccountId={dialogs.add.reauthId ?? undefined}
          onClose={actions.onCloseAdd}
          onAdded={actions.onAccountAdded}
        />
      )}
    </div>
  );
}
