/** Benes dashboard client for the Go proxy (`internal/server`). */
import { IconArrowDown, IconChevron } from "../icons";
import type { TFn } from "../i18n/shared";
import { formatElapsedSince } from "../relative-clock";
import { formatExactCount, formatSessionDate, sessionListIdentity, type SessionSummary } from "./sessions-contract";
import { protocolOptionLabel } from "./sessions-protocol-label";

export function SessionsList({
  t,
  locale,
  sessions,
  selectedId,
  hasMore,
  loadingMore,
  onSelect,
  onLoadOlder,
}: {
  t: TFn;
  locale: string;
  sessions: SessionSummary[];
  selectedId: string | null;
  hasMore: boolean;
  loadingMore: boolean;
  onSelect: (id: string) => void;
  onLoadOlder: () => void;
}) {
  return (
    <div className="sessions-list-wrap">
      <table className="sessions-table" aria-label={t("sessions.list.label")}>
        <thead>
          <tr>
            <th>{t("sessions.col.identity")}</th>
            <th>{t("sessions.col.protocol")}</th>
            <th>{t("sessions.col.started")}</th>
            <th>{t("sessions.col.lastActivity")}</th>
            <th>{t("sessions.col.requests")}</th>
            <th />
          </tr>
        </thead>
        <tbody>
          {sessions.map(row => {
            const selected = row.id === selectedId;
            const identity = sessionListIdentity(row);
            const protocols = row.protocols.map(id => protocolOptionLabel(t, id)).join(", ");
            return (
              <tr
                key={row.id}
                className={selected ? "is-selected" : undefined}
                aria-selected={selected}
                tabIndex={0}
                onClick={() => onSelect(row.id)}
                onKeyDown={event => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onSelect(row.id);
                  }
                }}
              >
                <td>
                  <span className="sessions-id-primary">{identity.primary}</span>
                  <span className="sessions-id-secondary">{identity.secondary}</span>
                </td>
                <td>{protocols || "\u2014"}</td>
                <td className="sessions-started">{formatSessionDate(row.startedAt, locale)}</td>
                <td>{formatElapsedSince(Date.parse(row.lastActivityAt), t)}</td>
                <td className="sessions-num">{formatExactCount(row.requestCount, locale)}</td>
                <td className="sessions-row-chevron" aria-hidden="true"><IconChevron /></td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {hasMore && (
        <button type="button" className="sessions-load-older" onClick={onLoadOlder} disabled={loadingMore}>
          {t("sessions.loadOlder")}
          <IconArrowDown aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
