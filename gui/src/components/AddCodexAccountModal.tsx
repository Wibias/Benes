/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import type { ReactNode } from "react";
import { useT } from "../i18n/shared";
import { IconGlobe } from "../icons";
import {
  addAccountReducer,
  initialAddAccountState,
  type AddAccountState,
} from "./add-codex-account-reducer";
import { useAddCodexAccountOAuth } from "./use-add-codex-account-oauth";
import { LoginUrlBlock } from "./login-url-block";
import type { CodexAccountMutationCompletion } from "../codex-account-mutation";

/** Class vocabulary for the dialog, kept in one place. */
const DIALOG_CLASS = "modal-overlay";
const CARD_CLASS = "modal-card";
const DESCRIPTION_CLASS = "modal-desc";
const FIELD_LABEL_CLASS = "field-label";
const INPUT_CLASS = "input";
const LABELED_INPUT_CLASS = "input text-label";
const METHOD_ROW_CLASS = "list-row";
const GHOST_BUTTON_CLASS = "btn btn-ghost";
const ERROR_NOTICE_CLASS = "notice notice-err";
const OK_NOTICE_CLASS = "notice notice-ok";
const WARN_NOTICE_CLASS = "notice-warn";
const MUTED_LABEL_CLASS = "muted text-label";
const SPINNER_CLASS = "spin";
const METHOD_TITLE_CLASS = "title";
const METHOD_SUB_CLASS = "sub";

const CARD_WIDTH = 440;
const SPINNER_SIZE = 24;

/** Everything the dialog treats as tabbable when it opens. */
const FOCUSABLE_SELECTOR = "input:not([disabled]), button:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

/** Show the dialog modally and put the caret on its first control. */
function openAndFocus(dialog: HTMLDialogElement | null): void {
  if (dialog === null) return;
  if (!dialog.open) dialog.showModal();
  dialog.querySelector<HTMLElement>(FOCUSABLE_SELECTOR)?.focus();
}

function CancelButton({ label, onClose }: { label: string; onClose: () => void }) {
  return (
    <button type="button" className={GHOST_BUTTON_CLASS} onClick={onClose} style={{ width: "100%" }}>
      {label}
    </button>
  );
}

function ErrorNotice({ message, offset }: { message: string; offset: number }) {
  return (
    <div className={ERROR_NOTICE_CLASS} style={{ marginTop: offset }}>
      {message}
    </div>
  );
}

function MethodRow({ label, description, icon, onStart }: {
  label: string;
  description: string;
  icon: ReactNode;
  onStart: () => void;
}) {
  return (
    <button type="button" className={METHOD_ROW_CLASS} onClick={onStart} style={{ marginBottom: 8 }}>
      <div style={{ display: "flex", alignItems: "center", gap: 10 }}>
        {icon}
        <div>
          <div className={METHOD_TITLE_CLASS}>{label}</div>
          <div className={METHOD_SUB_CLASS}>{description}</div>
        </div>
      </div>
    </button>
  );
}

function PickPhase({
  state,
  onIdChange,
  onStart,
  onClose,
}: {
  state: Extract<AddAccountState, { phase: "choosing" }>;
  onIdChange: (value: string) => void;
  onStart: () => void;
  onClose: () => void;
}) {
  const translate = useT();
  return (
    <>
      <h3 style={{ marginBottom: 4 }}>{translate("codexAuth.addTitle")}</h3>
      <p className={DESCRIPTION_CLASS}>{translate("codexAuth.addPickDesc")}</p>

      <label className={FIELD_LABEL_CLASS} htmlFor="codex-account-id-input">
        {translate("codexAuth.addIdLabel")}
      </label>
      <input
        id="codex-account-id-input"
        className={INPUT_CLASS}
        placeholder={translate("codexAuth.addIdPlaceholder")}
        value={state.accountId}
        onChange={event => onIdChange(event.target.value)}
        style={{ marginBottom: 12 }}
      />

      <MethodRow
        label={translate("codexAuth.oauthLogin")}
        description={translate("codexAuth.oauthDesc")}
        icon={<IconGlobe width={20} />}
        onStart={onStart}
      />

      {state.error !== null && <ErrorNotice message={state.error} offset={8} />}

      <CancelButton label={translate("codexAuth.cancel")} onClose={onClose} />
    </>
  );
}

function AuthorizePhase({
  state,
  reauthAccountId,
  onManualCodeChange,
  onSubmitManualCode,
  onClose,
}: {
  state: Extract<AddAccountState, { phase: "authorizing" }>;
  reauthAccountId?: string;
  onManualCodeChange: (value: string) => void;
  onSubmitManualCode: () => void;
  onClose: () => void;
}) {
  const translate = useT();
  const busy = state.manualCodeStatus === "submitting";
  const awaiting = state.manualCodeStatus === "awaiting";
  const heading = reauthAccountId ? translate("codexAuth.reauthenticate") : translate("codexAuth.oauthLogin");
  const submitDisabled = busy || awaiting || state.manualCode.trim() === "" || state.flowId === null;

  return (
    <>
      <h3 style={{ marginBottom: 4 }}>{heading}</h3>
      <p className={DESCRIPTION_CLASS}>{translate("codexAuth.oauthWaiting")}</p>
      <LoginUrlBlock url={state.authUrl} />
      <div style={{ display: "flex", flexDirection: "column", gap: 6, marginTop: 12 }}>
        <div className={MUTED_LABEL_CLASS}>{translate("prov.pasteRedirectHint")}</div>
        <div style={{ display: "flex", gap: 8 }}>
          <input
            type="text"
            autoComplete="off"
            spellCheck={false}
            value={state.manualCode}
            onChange={event => onManualCodeChange(event.target.value)}
            onKeyDown={event => {
              if (event.key === "Enter") {
                event.preventDefault();
                onSubmitManualCode();
              }
            }}
            placeholder={translate("prov.pasteRedirect")}
            aria-label={translate("prov.pasteRedirect")}
            disabled={busy || awaiting}
            className={LABELED_INPUT_CLASS}
            style={{ flex: 1 }}
          />
          <button
            className={GHOST_BUTTON_CLASS}
            type="button"
            disabled={submitDisabled}
            onClick={onSubmitManualCode}
          >
            {busy ? translate("codexAuth.oauthSubmittingCode") : translate("prov.pasteSubmit")}
          </button>
        </div>
      </div>
      {state.notice !== null && (
        <div
          className={state.notice.tone === "warn" ? WARN_NOTICE_CLASS : OK_NOTICE_CLASS}
          role="status"
          aria-live="polite"
          style={{ marginTop: 12 }}
        >
          {state.notice.message}
        </div>
      )}
      {state.error !== null && <ErrorNotice message={state.error} offset={12} />}
      <div style={{ textAlign: "center", padding: "24px 0" }}>
        <span className={SPINNER_CLASS} style={{ width: SPINNER_SIZE, height: SPINNER_SIZE }} />
      </div>
      <CancelButton label={translate("codexAuth.cancel")} onClose={onClose} />
    </>
  );
}

/**
 * Add or re-authenticate a Codex account.
 *
 * The dialog owns presentation and focus only; the phase machine lives in
 * `add-codex-account-reducer` and the authorization lifecycle in
 * `use-add-codex-account-oauth`. Both phases render inline, so the modal is the single
 * owner of the flow's markup.
 */
export default function AddCodexAccountModal({
  apiBase, onClose, onAdded, reauthAccountId,
}: {
  apiBase: string;
  onClose: () => void;
  onAdded: (completion: CodexAccountMutationCompletion) => void;
  reauthAccountId?: string;
}) {
  const translate = useT();
  const [state, dispatch] = useReducer(addAccountReducer, reauthAccountId, initialAddAccountState);
  const dialogRef = useRef<HTMLDialogElement>(null);

  const oauth = useAddCodexAccountOAuth({ apiBase, reauthAccountId, state, dispatch, t: translate });
  const { bindCallbacks, closeModal, startOAuth, submitManualCode } = oauth;

  useEffect(() => {
    bindCallbacks(onAdded, onClose);
  }, [bindCallbacks, onAdded, onClose]);

  useEffect(() => {
    // Remembered before the dialog steals focus so Escape returns the caret where the
    // operator left it.
    const restoreTo = document.activeElement as HTMLElement | null;
    openAndFocus(dialogRef.current);
    return () => {
      restoreTo?.focus();
    };
  }, []);

  const handleCancel = useCallback((event: React.SyntheticEvent) => {
    event.preventDefault();
    closeModal();
  }, [closeModal]);

  const dialogLabel = reauthAccountId ? translate("codexAuth.reauthenticate") : translate("codexAuth.addTitle");

  return (
    <dialog ref={dialogRef} aria-label={dialogLabel} className={DIALOG_CLASS} onCancel={handleCancel}>
      <div className={CARD_CLASS} style={{ maxWidth: CARD_WIDTH }}>
        {state.phase === "choosing" ? (
          <PickPhase
            state={state}
            onIdChange={value => dispatch({ kind: "account-id-changed", accountId: value })}
            onStart={() => { void startOAuth(state.accountId); }}
            onClose={closeModal}
          />
        ) : (
          <AuthorizePhase
            state={state}
            reauthAccountId={reauthAccountId}
            onManualCodeChange={value => dispatch({ kind: "manual-code-changed", manualCode: value })}
            onSubmitManualCode={() => { void submitManualCode(); }}
            onClose={closeModal}
          />
        )}
      </div>
    </dialog>
  );
}
