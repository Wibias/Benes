import { useEffect, useId, useRef, type ReactNode } from "react";
import AddProviderModal from "../AddProviderModal";
import AddCodexAccountModal from "../AddCodexAccountModal";
import OAuthTosWarningModal from "../OAuthTosWarningModal";
import { useT } from "../../i18n/shared";
import type { AddProviderIntent } from "./workspace-board-frames";
import type { AccountLoginRow, AccountLoginStatus } from "../provider-catalog/ProviderCatalog";
import { oauthLabel } from "../../pages/providers-shared";
import type { CodexAccountMutationCompletion } from "../../codex-account-mutation";

function ProviderDecisionDialog({
  title,
  label,
  children,
  actions,
  onDismiss,
  lockDismiss = false,
}: {
  title: string;
  label: string;
  children: ReactNode;
  actions: ReactNode;
  onDismiss: () => void;
  lockDismiss?: boolean;
}) {
  const dialogRef = useRef<HTMLDialogElement>(null);
  const titleId = useId();

  useEffect(() => {
    const node = dialogRef.current;
    if (!node) return;
    if (!node.open) node.showModal();
    const onBackdrop = (event: MouseEvent) => {
      if (lockDismiss || event.target !== node) return;
      const box = node.getBoundingClientRect();
      const inside = event.clientX >= box.left
        && event.clientX <= box.right
        && event.clientY >= box.top
        && event.clientY <= box.bottom;
      if (!inside) onDismiss();
    };
    const onEscape = (event: Event) => {
      event.preventDefault();
      if (!lockDismiss) onDismiss();
    };
    node.addEventListener("click", onBackdrop);
    node.addEventListener("cancel", onEscape);
    return () => {
      node.removeEventListener("click", onBackdrop);
      node.removeEventListener("cancel", onEscape);
      if (node.open) node.close();
    };
  }, [lockDismiss, onDismiss]);

  return (
    <dialog
      ref={dialogRef}
      className="provider-confirm"
      role="alertdialog"
      aria-modal="true"
      aria-labelledby={titleId}
      aria-label={label}
    >
      <form
        className="provider-confirm-form"
        method="dialog"
        onSubmit={event => event.preventDefault()}
      >
        <h3 id={titleId} className="provider-confirm-title">{title}</h3>
        <div className="provider-confirm-body">{children}</div>
        <div className="provider-confirm-actions">{actions}</div>
      </form>
    </dialog>
  );
}

export function UnsavedLeaveDialog({
  onSave,
  onDiscard,
  onCancel,
  saving = false,
}: {
  onSave: () => void;
  onDiscard: () => void;
  onCancel: () => void;
  saving?: boolean;
}) {
  const t = useT();
  return (
    <ProviderDecisionDialog
      label={t("pws.unsavedLeaveTitle")}
      title={t("pws.unsavedLeaveTitle")}
      onDismiss={onCancel}
      lockDismiss={saving}
      actions={(
        <>
          <button type="button" className="btn btn-ghost" onClick={onCancel} disabled={saving}>
            {t("common.cancel")}
          </button>
          <button type="button" className="btn btn-ghost" onClick={onDiscard} disabled={saving}>
            {t("pws.discardSettings")}
          </button>
          <button type="button" className="btn btn-primary" onClick={onSave} disabled={saving}>
            {saving ? t("pws.saving") : t("pws.saveSettings")}
          </button>
        </>
      )}
    >
      {t("pws.unsavedLeaveBody")}
    </ProviderDecisionDialog>
  );
}

export type ProviderOverlayHost = {
  add: null | {
    apiBase: string;
    existingNames: string[];
    intent: AddProviderIntent | null;
    accounts: {
      rows: AccountLoginRow[];
      status: Record<string, AccountLoginStatus>;
      busy: string | null;
    };
    onClose: () => void;
    onAdded: (name: string) => void;
    onLogin: (provider: string, addAccount?: boolean) => void;
    onCancelLogin: (provider: string) => void;
    onLogout: (provider: string) => void;
    onManage?: (provider: string) => void;
    onReopen: () => void;
  };
  codex: null | {
    apiBase: string;
    onClose: () => void;
    onAdded: (completion: CodexAccountMutationCompletion) => void;
  };
  remove: null | {
    name: string;
    defaultName: string | null;
    onConfirm: () => void;
    onCancel: () => void;
  };
  unsaved: null | {
    saving: boolean;
    onSave: () => void;
    onDiscard: () => void;
    onCancel: () => void;
  };
  tos: null | {
    provider: string;
    addAccount: boolean;
    onCancel: () => void;
    onContinue: () => void;
  };
};

export function ProviderOverlays({ add, codex, remove, unsaved, tos }: ProviderOverlayHost) {
  const t = useT();
  return (
    <>
      {add ? (
        <AddProviderModal
          apiBase={add.apiBase}
          existingNames={add.existingNames}
          initialTier={add.intent?.tier}
          initialCustom={add.intent?.custom}
          onClose={add.onClose}
          onAdded={add.onAdded}
          accountRows={add.accounts.rows}
          accountStatus={add.accounts.status}
          accountBusy={add.accounts.busy}
          onAccountLogin={add.onLogin}
          onAccountCancelLogin={add.onCancelLogin}
          onAccountLogout={add.onLogout}
          onAccountManage={add.onManage}
          onOpen={add.onReopen}
        />
      ) : null}
      {codex ? (
        <AddCodexAccountModal
          apiBase={codex.apiBase}
          onClose={codex.onClose}
          onAdded={codex.onAdded}
        />
      ) : null}
      {remove ? (
        <ProviderDecisionDialog
          label={t("pws.removeConfirmTitle")}
          title={t("pws.removeConfirmTitle")}
          onDismiss={remove.onCancel}
          actions={(
            <>
              <button type="button" className="btn btn-ghost" onClick={remove.onCancel}>{t("common.cancel")}</button>
              <button type="button" className="btn btn-danger" onClick={remove.onConfirm}>{t("pws.removeConfirm")}</button>
            </>
          )}
        >
          {remove.defaultName
            ? t("pws.removeDefaultConfirmBody", { name: remove.name, defaultProvider: remove.defaultName })
            : t("pws.removeConfirmBody", { name: remove.name })}
        </ProviderDecisionDialog>
      ) : null}
      {unsaved ? (
        <UnsavedLeaveDialog
          saving={unsaved.saving}
          onCancel={unsaved.onCancel}
          onDiscard={unsaved.onDiscard}
          onSave={unsaved.onSave}
        />
      ) : null}
      {tos ? (
        <OAuthTosWarningModal
          key={`${tos.provider}:${tos.addAccount ? "add" : "login"}`}
          providerId={tos.provider}
          providerLabel={oauthLabel(tos.provider)}
          onCancel={tos.onCancel}
          onContinue={tos.onContinue}
        />
      ) : null}
    </>
  );
}
