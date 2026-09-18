/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { MouseEvent } from "react";
import { IconTicket } from "../icons";

/** Ticket badge class vocabulary, shared with the badge component. */
export const TICKET_SLOT_CLASS = "badge badge-muted codex-ticket-badge-slot";
export const TICKET_BUTTON_CLASS = "badge badge-clickable";
export const TICKET_AMBER_CLASS = "badge-amber";
export const TICKET_MUTED_CLASS = "badge-muted";

const CREDIT_HEAD_CLASS = "credit-item-head";
const CREDIT_LABEL_CLASS = "credit-item-label";
const NEXT_BADGE_CLASS = "badge badge-amber text-micro";
const NEXT_BADGE_STYLE = { padding: "1px 6px" } as const;

/** Head row of one reset credit: ticket glyph, label, and the "next" flag. */
export function CreditHead({ label, nextBadge }: { label: string; nextBadge: string | null }) {
  return (
    <div className={CREDIT_HEAD_CLASS}>
      <IconTicket width={13} />
      <span className={CREDIT_LABEL_CLASS}>{label}</span>
      {nextBadge !== null && (
        <span className={NEXT_BADGE_CLASS} style={NEXT_BADGE_STYLE}>
          {nextBadge}
        </span>
      )}
    </div>
  );
}

/** Placeholder badge that holds the ticket slot's width before quota arrives. */
export function TicketSlot() {
  return (
    <span className={`${TICKET_SLOT_CLASS}`} aria-hidden="true">
      <IconTicket width={12} />0
    </span>
  );
}

export function TicketButton({ credits, tone, label, onActivate }: {
  credits: number;
  tone: string;
  label: string;
  onActivate: (event: MouseEvent) => void;
}) {
  return (
    <button type="button" className={`${TICKET_BUTTON_CLASS} ${tone}`} onClick={onActivate} aria-label={label}>
      <IconTicket width={12} />
      {credits}
    </button>
  );
}
