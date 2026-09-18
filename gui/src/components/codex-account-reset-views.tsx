/** Available, empty, and confirm views for the Codex reset-credit modal. */
import { useI18n, type Locale, type TFn } from "../i18n/shared";
import { IconAlert, IconTicket } from "../icons";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import { CodexCreditItem } from "./codex-account-pool-helpers";
import { formatCreditDate } from "../intl-formatters";
import { resetConfirmCredit, resetCreditCount, type ResetCredit } from "../lib/codex-account-reset-policy";

/** Class vocabulary for the reset dialog, kept in one place. */
const TITLE_ID = "codex-reset-title";
const CARD_SUB_CLASS = "card-sub";
const MUTED_CLASS = "faint";
const MUTED_LABEL_CLASS = "faint text-label";
const CREDIT_LIST_CLASS = "credit-list";
const MODAL_DESC_CLASS = "modal-desc";
const MODAL_ACTIONS_CLASS = "modal-actions";
const CONFIRM_ICON_CLASS = "confirm-icon";
const PRIMARY_BUTTON_CLASS = "btn btn-primary";
const GHOST_BUTTON_CLASS = "btn btn-ghost";
const FIFO_NOTE_CLASS = "card-sub text-caption";

/** Glyph geometry; both icons keep the size the dialog was laid out with. */
const TICKET_GLYPH = { width: 16 } as const;
const ALERT_GLYPH = { width: 22 } as const;

/** Geometry the views share. Named because the same values recur across the three. */
const BODY_STYLE = { margin: "16px 0" } as const;
const LEADING_LINE_STYLE = { marginBottom: 12 } as const;
const WIDE_ACTION_STYLE = { marginTop: 12, width: "100%" } as const;
const CENTERED_NOTE_STYLE = { marginTop: 8, textAlign: "center" } as const;
const CONFIRM_BODY_STYLE = { textAlign: "center", padding: "12px 0" } as const;

/** Props every credit view carries: the account and whatever credits are known. */
interface ResetCreditsViewProps {
  resetPopup: CodexAccountEntry;
  creditDetails: ResetCredit[] | null;
  redeeming: boolean;
}

interface CreditsAvailableProps extends ResetCreditsViewProps {
  creditDetailsLoading: boolean;
  onShowConfirm: () => void;
}

interface ConfirmResetProps extends ResetCreditsViewProps {
  onCancelConfirm: () => void;
  onRedeem: () => void;
}

/** "email · plan", or just the email for an account the listener reports no plan for. */
function accountIdentityLine(account: CodexAccountEntry): string {
  return account.plan ? `${account.email} · ${account.plan}` : account.email;
}

/**
 * The ticket heading plus the account it belongs to.
 *
 * Both credit views open the same way, so the heading lives here rather than being
 * repeated per view; the id is what the dialog's aria-labelledby points at.
 */
function ResetCreditsHeader({ account }: { account: CodexAccountEntry }) {
  const { t } = useI18n();
  const title = t("codexAuth.resetCreditsTitle");
  return (
    <>
      <h3 id={TITLE_ID}><IconTicket {...TICKET_GLYPH} /> {title}</h3>
      <div className={CARD_SUB_CLASS}>{accountIdentityLine(account)}</div>
    </>
  );
}

/**
 * The granted/expiry rows, oldest first, with the next credit to spend called out.
 *
 * A key is derived from the two instants rather than the index so a refreshed list that
 * inserts a credit does not re-mount the rows around it.
 */
function CreditRows({ credits, locale, t }: { credits: ResetCredit[]; locale: Locale; t: TFn }) {
  return (
    <div className={CREDIT_LIST_CLASS}>
      {credits.map((credit, index) => (
        <CodexCreditItem
          key={`${credit.granted_at}:${credit.expires_at}`}
          index={index}
          grantedAt={credit.granted_at}
          expiresAt={credit.expires_at}
          isNext={index === 0}
          locale={locale}
          t={t}
        />
      ))}
    </div>
  );
}

export function CodexResetCreditsAvailableView({
  resetPopup,
  creditDetails,
  creditDetailsLoading,
  redeeming,
  onShowConfirm,
}: CreditsAvailableProps) {
  const { locale, t } = useI18n();
  const count = resetCreditCount(resetPopup.quota);
  const hasRows = creditDetails !== null && creditDetails.length > 0;
  return (
    <>
      <ResetCreditsHeader account={resetPopup} />
      <div style={BODY_STYLE}>
        <p style={LEADING_LINE_STYLE}>{t("codexAuth.resetCreditsAvailable", { count: String(count) })}</p>
        {creditDetailsLoading && <p className={MUTED_LABEL_CLASS}>{t("common.loading")}</p>}
        {hasRows && creditDetails !== null && <CreditRows credits={creditDetails} locale={locale} t={t} />}
        <button type="button" className={PRIMARY_BUTTON_CLASS} style={WIDE_ACTION_STYLE} onClick={onShowConfirm} disabled={redeeming}>
          {t("codexAuth.useOneCredit")}
        </button>
        <p className={FIFO_NOTE_CLASS} style={CENTERED_NOTE_STYLE}>{t("codexAuth.fifoNote")}</p>
      </div>
    </>
  );
}

export function CodexResetCreditsEmptyView({ resetPopup }: { resetPopup: CodexAccountEntry }) {
  const { t } = useI18n();
  return (
    <>
      <ResetCreditsHeader account={resetPopup} />
      <div style={BODY_STYLE}>
        <p className={MUTED_CLASS}>{t("codexAuth.noResetCredits")}</p>
        <p className={MODAL_DESC_CLASS}>{t("codexAuth.earnCreditsHint")}</p>
      </div>
    </>
  );
}

export function CodexResetConfirmView({
  resetPopup,
  creditDetails,
  redeeming,
  onCancelConfirm,
  onRedeem,
}: ConfirmResetProps) {
  const { locale, t } = useI18n();
  const nextCredit = resetConfirmCredit(creditDetails);
  const count = resetCreditCount(resetPopup.quota);
  const whichCredit = nextCredit
    ? t("codexAuth.confirmWhichCredit", { date: formatCreditDate(nextCredit.granted_at, locale) })
    : null;
  return (
    <>
      <div style={CONFIRM_BODY_STYLE}>
        <div className={CONFIRM_ICON_CLASS}><IconAlert {...ALERT_GLYPH} /></div>
        <h3 id={TITLE_ID}>{t("codexAuth.confirmResetTitle")}</h3>
        <p className={MODAL_DESC_CLASS}>{t("codexAuth.confirmResetDesc", { count: String(count) })}</p>
        {whichCredit !== null && <p className={MUTED_LABEL_CLASS}>{whichCredit}</p>}
        <p className={MUTED_LABEL_CLASS}>{t("codexAuth.irreversible")}</p>
      </div>
      <div className={MODAL_ACTIONS_CLASS}>
        <button type="button" className={GHOST_BUTTON_CLASS} onClick={onCancelConfirm}>
          {t("codexAuth.cancel")}
        </button>
        <button type="button" className={PRIMARY_BUTTON_CLASS} onClick={onRedeem} disabled={redeeming}>
          {t(redeeming ? "codexAuth.redeeming" : "codexAuth.useCredit")}
        </button>
      </div>
    </>
  );
}
