/**
 * Combos overlays: destructive removal and the unsaved-edit navigation guard.
 *
 * Both are native `<dialog>` modals with the same shell, so `ComboDialog` owns
 * the element, the `showModal` mount, Escape handling and the backdrop button;
 * each caller supplies copy plus the buttons it wants. Escape (`cancel`) and
 * the backdrop always resolve to the caller's non-committing action, and the
 * buttons are data rather than markup so both dialogs keep the same shape.
 */

import { useCallback } from "react";
import { useT } from "../i18n/shared";

/** One button in a combo dialog. Class names stay explicit: CSS keys off them. */
interface ComboDialogAction {
  readonly label: string;
  readonly className: string;
  readonly testId?: string;
  readonly onPress: () => void;
}

/** Per-dialog presentation. Only the `pwi-*` card classes differ. */
const DIALOG_VARIANTS = {
  remove: {
    card: "pwi-remove-confirm-card",
    title: "pwi-remove-confirm-title",
    description: "pwi-remove-confirm-desc",
    actions: "pwi-remove-confirm-actions",
  },
  unsaved: {
    card: "pwi-json-unsaved-card",
    title: "pwi-json-unsaved-title",
    description: "pwi-json-unsaved-desc",
    actions: "pwi-json-unsaved-actions",
  },
} as const;

type DialogVariant = keyof typeof DIALOG_VARIANTS;

interface ComboDialogProps {
  variant: DialogVariant;
  titleId: string;
  title: string;
  description: string;
  actions: readonly ComboDialogAction[];
  onDismiss: () => void;
}

function ComboDialog({
  variant,
  titleId,
  title,
  description,
  actions,
  onDismiss,
}: ComboDialogProps) {
  const t = useT();
  const classes = DIALOG_VARIANTS[variant];
  const openOnMount = useCallback((node: HTMLDialogElement | null) => {
    if (node && !node.open) node.showModal();
  }, []);
  const handleCancel = useCallback((event: React.SyntheticEvent) => {
    event.preventDefault();
    onDismiss();
  }, [onDismiss]);

  return (
    <dialog className="modal-overlay cwi-dialog" aria-labelledby={titleId} onCancel={handleCancel} ref={openOnMount}>
      <button type="button" className="modal-backdrop-dismiss" aria-label={t("common.close")} tabIndex={-1} onClick={onDismiss} />
      <div className={`modal-card ${classes.card}`} onClick={(event) => event.stopPropagation()}>
        <h3 id={titleId} className={classes.title}>{title}</h3>
        <p className={`muted ${classes.description}`}>{description}</p>
        <div className={classes.actions}>
          {actions.map((action) => (
            <button
              key={action.label}
              type="button"
              className={action.className}
              data-testid={action.testId}
              onClick={action.onPress}
            >
              {action.label}
            </button>
          ))}
        </div>
      </div>
    </dialog>
  );
}

interface RemoveComboDialogProps {
  model: string;
  onCancel: () => void;
  onConfirm: () => void;
}

export function RemoveComboDialog({ model, onCancel, onConfirm }: RemoveComboDialogProps) {
  const t = useT();
  return (
    <ComboDialog
      variant="remove"
      titleId="cwi-remove-title"
      title={t("cws.removeConfirmTitle", { model })}
      description={t("cws.removeConfirmDesc")}
      onDismiss={onCancel}
      actions={[
        { label: t("common.cancel"), className: "btn btn-ghost", onPress: onCancel },
        { label: t("common.remove"), className: "btn pwi-remove-confirm-danger", onPress: onConfirm },
      ]}
    />
  );
}

interface UnsavedLeaveDialogProps {
  onKeep: () => void;
  onDiscard: () => void;
}

export function UnsavedLeaveDialog({ onKeep, onDiscard }: UnsavedLeaveDialogProps) {
  const t = useT();
  return (
    <ComboDialog
      variant="unsaved"
      titleId="cwi-unsaved-title"
      title={t("cws.unsavedTitle")}
      description={t("cws.unsavedDesc")}
      onDismiss={onKeep}
      actions={[
        {
          label: t("cws.keepEditing"),
          className: "btn btn-primary",
          testId: "cwi-unsaved-keep",
          onPress: onKeep,
        },
        {
          label: t("common.discard"),
          className: "btn btn-danger",
          testId: "cwi-unsaved-discard",
          onPress: onDiscard,
        },
      ]}
    />
  );
}

