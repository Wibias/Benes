/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { Locale, TFn } from "../i18n/shared";
import type { CodexAccountEntry } from "./codex-account-pool-types";
import { creditDaysRemaining, formatCreditDate, formatCreditDateTime } from "../intl-formatters";
import {
  CreditHead,
  TicketButton,
  TicketSlot,
  TICKET_AMBER_CLASS,
  TICKET_MUTED_CLASS,
} from "./codex-credit-atoms";

/** Class vocabulary for the credit row. */
const CREDIT_ITEM_CLASS = "credit-item";
const CREDIT_ITEM_NEXT_CLASS = "credit-item credit-next";
const CREDIT_DATES_CLASS = "credit-item-dates";
const CREDIT_URGENT_CLASS = "credit-urgent";

/** A credit expiring inside the week is called out. */
const URGENT_DAYS = 7;

/** Props every credit affordance shares. */
interface TranslateProp {
  t: TFn;
}

export interface CodexCreditItemProps extends TranslateProp {
  index: number;
  grantedAt: string;
  expiresAt: string;
  isNext: boolean;
  locale: Locale;
}

export function CodexCreditItem(props: CodexCreditItemProps) {
  const { index, grantedAt, expiresAt, isNext, locale, t } = props;
  const remainingDays = creditDaysRemaining(expiresAt);
  const position = index + 1;
  const label = isNext
    ? t("codexAuth.creditNext")
    : t("codexAuth.creditLabel", { n: String(position) });
  const dateRows: Array<{ key: string; text: string; urgent: boolean }> = [
    {
      key: "granted",
      text: t("codexAuth.creditGranted", { date: formatCreditDate(grantedAt, locale) }),
      urgent: false,
    },
    {
      key: "expires",
      text: t("codexAuth.creditExpires", {
        date: formatCreditDateTime(expiresAt, locale),
        days: String(remainingDays),
      }),
      urgent: remainingDays <= URGENT_DAYS,
    },
  ];
  return (
    <div className={isNext ? CREDIT_ITEM_NEXT_CLASS : CREDIT_ITEM_CLASS}>
      <CreditHead label={label} nextBadge={isNext ? t("codexAuth.creditNextBadge") : null} />
      <div className={CREDIT_DATES_CLASS}>
        {dateRows.map(row => (
          <span key={row.key} className={row.urgent ? CREDIT_URGENT_CLASS : ""}>{row.text}</span>
        ))}
      </div>
    </div>
  );
}

/** `undefined` until quota loads; `null` quota is reported separately by the caller. */
function resetCreditsOf(account: CodexAccountEntry): number | undefined {
  return account.quota?.resetCredits;
}

export interface CodexTicketBadgeProps extends TranslateProp {
  account: CodexAccountEntry;
  onClick: () => void;
}

export function CodexTicketBadge(props: CodexTicketBadgeProps) {
  const { account, onClick, t } = props;
  const credits = resetCreditsOf(account);
  // Reserve badge width while WHAM quota is still null so the card-head does not grow
  // when resetCredits arrives (0 or N). Quota loaded without resetCredits → no badge.
  if (account.quota === null) return <TicketSlot />;
  if (credits === undefined) return null;
  const spendable = credits > 0;
  return (
    <TicketButton
      credits={credits}
      tone={spendable ? TICKET_AMBER_CLASS : TICKET_MUTED_CLASS}
      label={t("codexAuth.resetCreditsAria", { count: String(credits) })}
      onActivate={event => { event.stopPropagation(); onClick(); }}
    />
  );
}

