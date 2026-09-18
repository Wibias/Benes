/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useRef, type ReactNode, type SyntheticEvent } from "react";
import { useI18n, type TKey } from "../i18n/shared";
import {
  codexResetModalView,
  resetCreditCount,
  type ResetPopupView,
} from "../lib/codex-account-reset-policy";
import {
  CodexResetConfirmView,
  CodexResetCreditsAvailableView,
  CodexResetCreditsEmptyView,
} from "./codex-account-reset-views";

/** Class vocabulary and roles for the dialog shell, kept in one place. */
const DIALOG_CLASS = "modal-overlay";
const BACKDROP_CLASS = "modal-backdrop-dismiss";
const CARD_CLASS = "modal-card";
const CARD_ROLE = "document";
const CLOSE_KEY: TKey = "common.close";

export interface CodexAccountResetModalProps {
  popup: ResetPopupView;
  onClose: () => void;
  onShowConfirm: () => void;
  onCancelConfirm: () => void;
  onRedeem: () => void;
}

/** Shows the dialog modally once it is mounted, and hands the element back to the shell. */
function useModalDialog() {
  const elementRef = useRef<HTMLDialogElement>(null);
  useEffect(() => {
    const element = elementRef.current;
    if (element === null || element.open) return;
    element.showModal();
  }, []);
  return elementRef;
}

/**
 * The click-away layer.
 *
 * Focus-immune by design (it is not in the tab order) and labelled for assistive tech, so
 * the pointer and the screen reader both have a way out that is not the Escape key.
 */
function ResetBackdrop({ onClose }: { onClose: () => void }) {
  const { t } = useI18n();
  return (
    <button
      type="button"
      className={BACKDROP_CLASS}
      aria-label={t(CLOSE_KEY)}
      tabIndex={-1}
      onClick={onClose}
    />
  );
}

/** The dialog shell every reset view renders inside. */
function ResetDialogShell({ onClose, children }: { onClose: () => void; children: ReactNode }) {
  const dialogRef = useModalDialog();
  const dismiss = useCallback((event: SyntheticEvent) => {
    event.preventDefault();
    onClose();
  }, [onClose]);
  return (
    <dialog ref={dialogRef} className={DIALOG_CLASS} aria-labelledby="codex-reset-title" onCancel={dismiss}>
      <ResetBackdrop onClose={onClose} />
      <div className={CARD_CLASS} onClick={event => event.stopPropagation()} role={CARD_ROLE}>
        {children}
      </div>
    </dialog>
  );
}

/**
 * Which of the three bodies this state calls for.
 *
 * Confirming outranks having credits, which outranks having none — the reset policy decides
 * that order; this only maps the answer onto a view.
 */
function ResetBody({ popup, onShowConfirm, onCancelConfirm, onRedeem }: {
  popup: ResetPopupView;
  onShowConfirm: () => void;
  onCancelConfirm: () => void;
  onRedeem: () => void;
}) {
  const view = codexResetModalView(popup.confirming, resetCreditCount(popup.account.quota));
  if (view === "confirm") {
    return (
      <CodexResetConfirmView
        resetPopup={popup.account}
        creditDetails={popup.credits}
        redeeming={popup.redeeming}
        onCancelConfirm={onCancelConfirm}
        onRedeem={onRedeem}
      />
    );
  }
  if (view === "available") {
    return (
      <CodexResetCreditsAvailableView
        resetPopup={popup.account}
        creditDetails={popup.credits}
        creditDetailsLoading={popup.loadingCredits}
        redeeming={popup.redeeming}
        onShowConfirm={onShowConfirm}
      />
    );
  }
  return <CodexResetCreditsEmptyView resetPopup={popup.account} />;
}

export function CodexAccountResetModal({
  popup,
  onClose,
  onShowConfirm,
  onCancelConfirm,
  onRedeem,
}: CodexAccountResetModalProps) {
  return (
    <ResetDialogShell onClose={onClose}>
      <ResetBody
        popup={popup}
        onShowConfirm={onShowConfirm}
        onCancelConfirm={onCancelConfirm}
        onRedeem={onRedeem}
      />
    </ResetDialogShell>
  );
}
