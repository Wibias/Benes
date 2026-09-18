/** Benes dashboard client for the Go proxy (`internal/server`). */
import { Fragment, useState } from "react";
import { useI18n } from "../i18n/shared";
import type { ClaudeInboundEntry } from "./debug-shared";
import { formatClaudeInboundTime } from "./debug-shared";

/** Shown wherever a request carried no value the recorder reports. */
const DASH = "—";

/** Shown in place of a tag when the request carried the field without a label for it. */
const UNLABELLED_TAG = "yes";

type TFn = ReturnType<typeof useI18n>["t"];

/**
 * The inbound table, described once.
 *
 * Every column is one entry in this list, so the header and the row cells cannot disagree
 * about order or wording: they are two renders of the same list, one reading titles and one
 * reading values.
 */
const INBOUND_COLUMNS = [
  {
    label: "debug.claudeInbound.time",
    className: "muted mono",
    cell: (entry: ClaudeInboundEntry) => formatClaudeInboundTime(entry.at),
  },
  {
    label: "debug.claudeInbound.endpoint",
    className: "mono",
    cell: (entry: ClaudeInboundEntry) => entry.endpoint,
  },
  {
    label: "debug.claudeInbound.model",
    className: "mono",
    cell: (entry: ClaudeInboundEntry) => entry.model,
  },
  {
    label: "debug.claudeInbound.resolvedModel",
    className: "mono",
    cell: (entry: ClaudeInboundEntry) => entry.resolvedModel ?? DASH,
  },
  {
    label: "debug.claudeInbound.stream",
    className: "mono",
    cell: (entry: ClaudeInboundEntry, t: TFn) => streamCell(entry, t),
  },
  {
    label: "debug.claudeInbound.thinkingEffort",
    className: "mono",
    cell: (entry: ClaudeInboundEntry) => thinkingEffort(entry),
  },
] as const;

/** How the stream cell reads. The recorder reports `stream` only when the request asked for one. */
function streamCell(entry: ClaudeInboundEntry, t: TFn): string {
  if (entry.stream === undefined) return DASH;
  return entry.stream ? t("debug.claudeInbound.streamOn") : t("debug.claudeInbound.streamOff");
}

/**
 * Thinking shape and effort cap the proxy resolved for this request.
 *
 * The thinking type and its budget describe one decision and read as one value; the effort cap
 * is a second decision, so the two are joined differently.
 */
function thinkingEffort(entry: ClaudeInboundEntry): string {
  const budget = entry.thinkingBudgetTokens;
  const shape = [entry.thinkingType, budget === undefined ? undefined : String(budget)]
    .filter(part => Boolean(part))
    .join(" ");
  const parts = [shape, entry.outputConfigEffort].filter(part => Boolean(part));
  return parts.length > 0 ? parts.join(" / ") : DASH;
}

/** A tag the recorder attached, its placeholder when it attached none, or "none". */
function tagOrNone(carried: boolean, tag: string | undefined, t: TFn): string {
  if (!carried) return t("debug.claudeInbound.none");
  return tag ?? UNLABELLED_TAG;
}

/** One label/value pair in an expanded row. */
function MetaRow({ label, value }: { label: string; value: React.ReactNode }) {
  return (
    <div>
      <dt>{label}</dt>
      <dd className="mono">{value}</dd>
    </div>
  );
}

/**
 * The detail rows this entry actually carries.
 *
 * The last two rows are always reported, because "carried no user id" and "carried no system
 * prompt" are themselves facts about the request; the rows above them would only say that the
 * recorder had nothing to attach.
 */
function metaRows(entry: ClaudeInboundEntry, t: TFn): Array<{ label: string; value: React.ReactNode }> {
  const rows: Array<{ label: string; value: React.ReactNode }> = [];
  if (entry.maxTokens !== undefined) {
    rows.push({ label: t("debug.claudeInbound.maxTokens"), value: entry.maxTokens });
  }
  if (entry.anthropicBeta) {
    rows.push({ label: t("debug.claudeInbound.beta"), value: entry.anthropicBeta });
  }
  const metadataKeys = entry.metadataKeys;
  if (metadataKeys && metadataKeys.length > 0) {
    rows.push({ label: t("debug.claudeInbound.metadata"), value: metadataKeys.join(", ") });
  }
  rows.push({
    label: t("debug.claudeInbound.userId"),
    value: tagOrNone(entry.hasMetadataUserId, entry.userIdTag, t),
  });
  rows.push({
    label: t("debug.claudeInbound.hasSystem"),
    value: tagOrNone(entry.hasSystem, entry.systemTag, t),
  });
  return rows;
}

/** One inbound request: its summary row, plus the detail row while it is open. */
function InboundRow({
  entry,
  open,
  onToggle,
  t,
}: {
  entry: ClaudeInboundEntry;
  open: boolean;
  onToggle: () => void;
  t: TFn;
}) {
  const toggleFromKeyboard = (event: React.KeyboardEvent<HTMLTableRowElement>) => {
    if (event.key !== "Enter" && event.key !== " ") return;
    event.preventDefault();
    onToggle();
  };

  return (
    <Fragment>
      <tr
        className={open ? "is-selected" : undefined}
        tabIndex={0}
        onClick={onToggle}
        onKeyDown={toggleFromKeyboard}
      >
        {INBOUND_COLUMNS.map(column => (
          <td key={column.label} className={column.className}>{column.cell(entry, t)}</td>
        ))}
      </tr>
      {open && (
        <tr className="debug-claude-expand">
          <td colSpan={INBOUND_COLUMNS.length}>
            <dl className="debug-claude-meta">
              {metaRows(entry, t).map(row => (
                <MetaRow key={row.label} label={row.label} value={row.value} />
              ))}
            </dl>
          </td>
        </tr>
      )}
    </Fragment>
  );
}

export type DebugClaudeInboundPanelProps = { entries: ClaudeInboundEntry[] };

export function DebugClaudeInboundPanel({ entries }: DebugClaudeInboundPanelProps) {
  const { t } = useI18n();
  const [openId, setOpenId] = useState<number | null>(null);

  return (
    <section className="debug-section">
      <div className="debug-section-head">
        <h3>{t("debug.claudeInbound.title")}</h3>
      </div>
      {entries.length === 0 ? (
        <p className="debug-empty muted">{t("debug.claudeInbound.empty")}</p>
      ) : (
        <table className="debug-claude-table">
          <thead>
            <tr>
              {INBOUND_COLUMNS.map(column => (
                <th key={column.label}>{t(column.label)}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {entries.map(entry => (
              <InboundRow
                key={entry.id}
                entry={entry}
                t={t}
                open={openId === entry.id}
                onToggle={() => setOpenId(openId === entry.id ? null : entry.id)}
              />
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}