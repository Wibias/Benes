/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Terms-of-Service warning shown by the dashboard before it starts provider OAuth.
 *
 * `oauth-tos-dialog` answers whether a provider needs the warning at all and which parts of
 * it apply, so this file only renders that answer — an unmarked provider renders nothing.
 * Continue stays locked until the operator acknowledges, and one dialog accepts only one
 * submission: a second click arriving before the browser tears the dialog down must not
 * start a second authorize flow.
 */

import { useEffect, useId, useRef, useState, type MouseEvent, type ReactNode, type SyntheticEvent } from "react";
import { useT } from "../i18n/shared";
import { oauthTosRiskBodyKey, oauthTosRiskTitleKey } from "../oauth-tos-risk";
import { oauthTosDialogPlan, type OAuthTosDialogPlan } from "../oauth-tos-dialog";
import { IconAlert } from "../icons";

/** Body paragraphs the warning can show: a risk level's generic one, or Anthropic's own. */
type TosBodyKey = ReturnType<typeof oauthTosRiskBodyKey> | "oauthTos.anthropicBody";

/** The two things an overlay host can decide: leave the warning, or authorize anyway. */
interface WarningActions {
  onCancel: () => void;
  onContinue: () => void;
}

/** What the two overlay hosts pass in; `onContinue` starts the authorize flow. */
interface WarningProps extends WarningActions {
  providerId: string;
  providerLabel: string;
}

interface DialogProps {
  plan: OAuthTosDialogPlan;
  providerLabel: string;
  onDismiss: () => void;
  onAuthorize: () => void;
}

interface RiskParagraphProps {
  /** Element the dialog points `aria-describedby` at. */
  bodyId: string;
  bodyKey: TosBodyKey;
  providerLabel: string;
}

interface AcknowledgementRowProps {
  confirmed: boolean;
  onConfirmed: (next: boolean) => void;
}

interface DialogActionProps {
  variant: "btn-ghost" | "btn-primary";
  /** Omitted on the cancel action, which is never disabled. */
  disabled?: boolean;
  onClick: () => void;
  children: ReactNode;
}

interface DialogButtonsProps {
  canContinue: boolean;
  onDismiss: () => void;
  onAuthorize: () => void;
}

/** Anthropic's body copy replaces the level's generic paragraph; see `oauth-tos-dialog`. */
function bodyKeyFor(plan: OAuthTosDialogPlan): TosBodyKey {
  return plan.usesProviderBody ? "oauthTos.anthropicBody" : oauthTosRiskBodyKey(plan.level);
}

/** The dismiss button is a sibling of the card, not its parent, so this is belt-and-braces. */
function keepCardClicks(event: MouseEvent<HTMLDivElement>): void {
  event.stopPropagation();
}

function RiskParagraph({ bodyId, bodyKey, providerLabel }: RiskParagraphProps) {
  const t = useT();
  return (
    <div className="notice-warn oauth-tos-body">
      <IconAlert aria-hidden="true" />
      <p id={bodyId} className="modal-desc">{t(bodyKey, { provider: providerLabel })}</p>
    </div>
  );
}

function AcknowledgementRow({ confirmed, onConfirmed }: AcknowledgementRowProps) {
  const t = useT();
  return (
    <label className="oauth-tos-ack">
      <input
        type="checkbox"
        checked={confirmed}
        aria-required="true"
        onChange={event => onConfirmed(event.currentTarget.checked)}
      />
      <span className="text-label">{t("oauthTos.acknowledge")}</span>
    </label>
  );
}

/** The two footer buttons are one control in two tones. */
function DialogAction({ variant, disabled, onClick, children }: DialogActionProps) {
  return (
    <button type="button" className={`btn ${variant}`} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  );
}

function DialogButtons({ canContinue, onDismiss, onAuthorize }: DialogButtonsProps) {
  const t = useT();
  return (
    <div className="modal-actions">
      <DialogAction variant="btn-ghost" onClick={onDismiss}>
        {t("common.cancel")}
      </DialogAction>
      <DialogAction variant="btn-primary" disabled={!canContinue} onClick={onAuthorize}>
        {t("oauthTos.continue")}
      </DialogAction>
    </div>
  );
}

function TosWarningDialog({ plan, providerLabel, onDismiss, onAuthorize }: DialogProps) {
  const t = useT();
  const dialogNode = useRef<HTMLDialogElement | null>(null);
  const headingId = useId();
  const bodyId = useId();
  /** Mirrors `flowStarted`: a second click can land before React re-renders the button. */
  const flowStartedRef = useRef(false);
  const [ackConfirmed, setAckConfirmed] = useState(false);
  const [flowStarted, setFlowStarted] = useState(false);

  // A native modal dialog owns focus trapping and the backdrop, so no portal is needed.
  useEffect(() => {
    const node = dialogNode.current;
    // Effects re-run against the same node when React remounts them, so open only once.
    if (!node || node.open) return;
    node.showModal();
  }, []);

  // Escape arrives as the native "cancel" event; the caller decides what closing means.
  const dismissOnEscape = (event: SyntheticEvent) => {
    event.preventDefault();
    onDismiss();
  };

  const startAuthorize = () => {
    if (!ackConfirmed || flowStartedRef.current) return;
    flowStartedRef.current = true;
    setFlowStarted(true);
    onAuthorize();
  };

  return (
    <dialog
      ref={dialogNode}
      className="modal-overlay"
      aria-labelledby={headingId}
      aria-describedby={bodyId}
      onCancel={dismissOnEscape}
    >
      <button
        type="button"
        tabIndex={-1}
        className="modal-backdrop-dismiss"
        aria-label={t("common.close")}
        onClick={onDismiss}
      />
      <div className="modal-card oauth-tos-card" onClick={keepCardClicks}>
        <h3 id={headingId}>{t(oauthTosRiskTitleKey(plan.level), { provider: providerLabel })}</h3>
        <RiskParagraph bodyId={bodyId} bodyKey={bodyKeyFor(plan)} providerLabel={providerLabel} />
        {plan.offersApiKeyPath ? (
          <p className="muted text-label oauth-tos-hint">{t("oauthTos.saferPath")}</p>
        ) : null}
        <AcknowledgementRow confirmed={ackConfirmed} onConfirmed={setAckConfirmed} />
        <DialogButtons
          canContinue={ackConfirmed && !flowStarted}
          onDismiss={onDismiss}
          onAuthorize={startAuthorize}
        />
      </div>
    </dialog>
  );
}

export default function OAuthTosWarningModal({ providerId, providerLabel, onCancel, onContinue }: WarningProps) {
  const plan = oauthTosDialogPlan(providerId);
  if (plan === null) return null;
  return (
    <TosWarningDialog
      plan={plan}
      providerLabel={providerLabel}
      onDismiss={onCancel}
      onAuthorize={onContinue}
    />
  );
}
