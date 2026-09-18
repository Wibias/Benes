/**
 * The provider recent-event list.
 *
 * The Overview health block and the Management credential activity block read the same
 * events from `/api/providers/workspace`, so they render them through one list and one
 * severity mapping (`providerEventSeverity`). A warn/error row carries a glyph coloured
 * by the canonical token as well as a screen-reader word, because colour alone does not
 * tell a reader what happened.
 */
import { IconAlert, IconX } from "../../icons";
import { useT } from "../../i18n/shared";
import {
  providerEventSeverity,
  providerEventSeverityWordKey,
} from "../../provider-workspace/access-presentation";

export type ProviderEventEntry = {
  key: string;
  at: string;
  label: string;
  severity: string;
};

export function ProviderEvents({ events }: { events: readonly ProviderEventEntry[] }) {
  const t = useT();
  return (
    <ul className="providers-events">
      {events.map(event => {
        const severity = providerEventSeverity(event.severity);
        const Glyph = severity.tone === "error" ? IconX : IconAlert;
        return (
          <li key={event.key}>
            <time>{event.at}</time>
            <span>
              {severity.tone === "off" ? null : (
                <>
                  <Glyph className={severity.className} aria-hidden="true" />
                  <span className="sr-only">{t(providerEventSeverityWordKey(severity.tone))}</span>
                </>
              )}
              {event.label}
            </span>
          </li>
        );
      })}
    </ul>
  );
}
