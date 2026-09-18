/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useRef } from "react";
import { useT } from "../i18n/shared";
import type { TFn } from "../i18n/shared";
import { IconAlert } from "../icons";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import type { CodexAccountModeState } from "../codex-multi-state";

const DIALOG_CLASS = "modal-overlay";
const BACKDROP_CLASS = "modal-backdrop-dismiss";
const CARD_CLASS = "modal-card";
const TITLE_ID = "codex-switch-title";
const DESCRIPTION_CLASS = "modal-desc";
const IDENTITY_CLASS = "card";
const IDENTITY_STYLE = { margin: "12px 0" } as const;
const PLAN_CLASS = "badge badge-green";
const PLAN_STYLE = { marginLeft: 8 } as const;
const WARNING_CLASS = "notice-warn";
const ACTIONS_CLASS = "modal-actions";
const GHOST_BUTTON_CLASS = "btn btn-ghost";
const PRIMARY_BUTTON_CLASS = "btn btn-primary";

interface SwitchCopy {
  title: string;
  description: string;
  confirm: string;
  /** True when the target is the app login rather than a pooled account. */
  backToApp: boolean;
}

/** Copy for the dialog, derived once from the pool mode and the target row. */
function switchCopy(translate: TFn, mode: CodexAccountModeState | null, target: CodexAccountEntry): SwitchCopy {
  const direct = mode === "direct";
  const backToApp = !direct && target.id === "__main__";
  const titleKey = direct
    ? "codexAuth.preparePoolTitle"
    : backToApp
      ? "codexAuth.switchBack"
      : "codexAuth.switchTitle";
  const descriptionKey = direct
    ? "codexAuth.preparePoolDesc"
    : backToApp
      ? "codexAuth.switchBackDesc"
      : "codexAuth.switchDesc";
  return {
    title: translate(titleKey),
    description: translate(descriptionKey),
    confirm: translate(direct ? "codexAuth.prepareForPool" : "codexAuth.setAsNext"),
    backToApp,
  };
}

export interface CodexAccountSwitchModalProps extends SwitchDialogBase {
  /**
   * An in-flight selection-order write. It clears the pin this switch would set, so the
   * controller refuses to run the two together and drops the loser without a toast --
   * the button has to be unavailable rather than silently ineffective.
   */
  orderBusy?: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}

interface SwitchTarget {
  confirm: CodexAccountEntry;
  mainEmail?: string;
}

interface SwitchPoolState {
  accountModeState: CodexAccountModeState | null;
  switchingId: string | null;
}

interface SwitchDialogBase extends SwitchTarget, SwitchPoolState {}

function SwitchIdentity({ identity, plan }: { identity: string; plan?: string }) {
  return (
    <div className={IDENTITY_CLASS} style={IDENTITY_STYLE}>
      <strong>{identity}</strong>
      {plan && <span className={PLAN_CLASS} style={PLAN_STYLE}>{plan}</span>}
    </div>
  );
}

function SwitchActions({ cancelLabel, actionLabel, busy, onCancel, onConfirm }: {
  cancelLabel: string;
  actionLabel: string;
  busy: boolean;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div className={ACTIONS_CLASS}>
      <button type="button" className={GHOST_BUTTON_CLASS} onClick={onCancel}>{cancelLabel}</button>
      <button type="button" className={PRIMARY_BUTTON_CLASS} disabled={busy} onClick={onConfirm}>
        {actionLabel}
      </button>
    </div>
  );
}

export function CodexAccountSwitchModal(props: CodexAccountSwitchModalProps) {
  const { confirm, mainEmail, accountModeState, switchingId, orderBusy = false, onCancel, onConfirm } = props;
  const translate = useT();
  const dialogRef = useRef<HTMLDialogElement>(null);

  useEffect(() => {
    const dialog = dialogRef.current;
    if (dialog === null || dialog.open) return;
    dialog.showModal();
  }, []);

  const handleCancel = useCallback((synthetic: React.SyntheticEvent) => {
    synthetic.preventDefault();
    onCancel();
  }, [onCancel]);

  const copy = switchCopy(translate, accountModeState, confirm);
  const identity = copy.backToApp ? (mainEmail || translate("codexAuth.codexApp")) : confirm.email;
  const cancelLabel = translate("codexAuth.cancel");
  const busy = Boolean(switchingId) || orderBusy;
  const actionLabel = switchingId ? translate("pws.accountSwitching") : copy.confirm;
  const backdropProps = {
    className: BACKDROP_CLASS,
    "aria-label": translate("common.close"),
    tabIndex: -1,
    onClick: onCancel,
  };

  return (
    <dialog ref={dialogRef} className={DIALOG_CLASS} aria-labelledby={TITLE_ID} onCancel={handleCancel}>
      <button type="button" {...backdropProps} />
      <div className={CARD_CLASS} onClick={event => event.stopPropagation()} role="document">
        <h3 id={TITLE_ID}>{copy.title}</h3>
        <p className={DESCRIPTION_CLASS}>{copy.description}</p>
        <SwitchIdentity identity={identity} plan={confirm.plan} />
        {!copy.backToApp && (
          <div className={WARNING_CLASS}>
            <IconAlert width={14} /> {translate("codexAuth.cacheWarning")}
          </div>
        )}
        <SwitchActions
          cancelLabel={cancelLabel}
          actionLabel={actionLabel}
          busy={busy}
          onCancel={onCancel}
          onConfirm={onConfirm}
        />
      </div>
    </dialog>
  );
}
