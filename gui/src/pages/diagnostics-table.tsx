import { IconArrowDown } from "../icons";
import { formatProviderDisplayName } from "../provider-icons";
import { formatTokens } from "../format-tokens";
import { modelLabel } from "../model-display";
import type { TFn } from "../i18n/shared";
import { formatLogDateParts, statusColor } from "./logs-shared";
import {
  formatRecordedDurationMs,
  parseTimestampMs,
  type DiagnosticsRequestSummary,
} from "./diagnostics-contract";

function dash(value: string | undefined): string {
  return value && value.trim() !== "" ? value : "\u2014";
}

export function DiagnosticsTable({
  t,
  locale,
  localeTag,
  serverTimeZone,
  rows,
  selectedId,
  hasOlder,
  loadingOlder,
  onSelect,
  onLoadOlder,
}: {
  t: TFn;
  locale: string;
  localeTag?: string;
  serverTimeZone?: string;
  rows: DiagnosticsRequestSummary[];
  selectedId: string | null;
  hasOlder: boolean;
  loadingOlder: boolean;
  onSelect: (id: string) => void;
  onLoadOlder: () => void;
}) {
  return (
    <div className="diagnostics-list-wrap">
      <table className="diagnostics-table" aria-label={t("logs.list.label")}>
        <thead>
          <tr>
            <th>{t("logs.col.time")}</th>
            <th>{t("logs.col.model")}</th>
            <th>{t("logs.col.provider")}</th>
            <th>{t("logs.col.status")}</th>
            <th className="num">{t("logs.col.duration")}</th>
            <th className="num diagnostics-col-tokens">{t("logs.col.tokens")}</th>
          </tr>
        </thead>
        <tbody>
          {rows.map(row => {
            const selected = row.requestId === selectedId;
            const at = parseTimestampMs(row.timestamp);
            const when = at === undefined
              ? { date: row.timestamp, time: "" }
              : formatLogDateParts(at, localeTag, serverTimeZone);
            const model = row.resolvedModel ? modelLabel(row.resolvedModel) : "\u2014";
            return (
              <tr
                key={row.requestId}
                className={selected ? "is-selected" : undefined}
                aria-selected={selected}
                tabIndex={0}
                onClick={() => onSelect(row.requestId)}
                onKeyDown={event => {
                  if (event.key === "Enter" || event.key === " ") {
                    event.preventDefault();
                    onSelect(row.requestId);
                  }
                }}
              >
                <td className="diagnostics-time">
                  <span>{when.time || when.date}</span>
                  {when.time && <span className="muted">{when.date}</span>}
                </td>
                <td className="mono">{model}</td>
                <td>{row.provider ? formatProviderDisplayName(row.provider, t) : "\u2014"}</td>
                <td className="mono" style={{ color: statusColor(row.status) }}>{row.status}</td>
                <td className="num mono">{formatRecordedDurationMs(row.durationMs, t("uptime.second")) ?? "\u2014"}</td>
                <td className="num mono diagnostics-col-tokens">
                  {row.totalTokens === undefined
                    ? dash(row.usageStatus)
                    : `${row.usageStatus === "estimated" ? "~" : ""}${formatTokens(row.totalTokens, locale)}`}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
      {hasOlder && (
        <button type="button" className="diagnostics-load-older" onClick={onLoadOlder} disabled={loadingOlder}>
          {t("logs.loadOlder")}
          <IconArrowDown aria-hidden="true" />
        </button>
      )}
    </div>
  );
}
