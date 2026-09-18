/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { VirtualItem, Virtualizer } from "@tanstack/react-virtual";
import { useI18n } from "../i18n/shared";
import type { DebugLogEntry, LogStream } from "./debug-shared";
import { formatLogTime } from "./debug-shared";

/** The virtualizer that measures the output pane rows. */
export type DebugLogVirtualizer = Virtualizer<HTMLDivElement, Element>;

export type DebugLogViewerProps = {
  /** Debug logging is on and the selected stream carries a capture flag. */
  visible: boolean;
  stream: LogStream;
  entries: DebugLogEntry[];
  scrollRef: React.RefObject<HTMLDivElement | null>;
  virtualizer: DebugLogVirtualizer;
};

/** The copy this pane can show in place of rows. */
type PlaceholderKey =
  | "debug.output.empty"
  | "debug.noLines.provider"
  | "debug.noLines.usage"
  | "debug.noLines.injection";

/**
 * Copy shown in place of rows while the selected stream has captured nothing yet.
 *
 * Every stream records a different kind of event, so "nothing here" needs its own explanation
 * per stream; the provider stream in particular stays silent for ordinary healthy traffic.
 */
const EMPTY_STREAM_KEY: Record<LogStream, PlaceholderKey> = {
  provider: "debug.noLines.provider",
  usage: "debug.noLines.usage",
  injection: "debug.noLines.injection",
};

/** The copy this pane owes the reader, or `null` once it has rows to render. */
function placeholderKey(
  visible: boolean,
  stream: LogStream,
  rowCount: number,
): PlaceholderKey | null {
  if (!visible) return "debug.output.empty";
  return rowCount === 0 ? EMPTY_STREAM_KEY[stream] : null;
}

/**
 * One absolutely positioned log line.
 *
 * The row is measured through the virtualizer's ref so a wrapped line raises the height that
 * row reserves; `data-index` is what ties the measured node back to the row it belongs to.
 */
function LogLine({
  item,
  entry,
  measure,
}: {
  item: VirtualItem;
  entry: DebugLogEntry;
  measure: (node: HTMLDivElement | null) => void;
}) {
  return (
    <div
      className="debug-log-line"
      ref={measure}
      data-index={item.index}
      style={{ transform: `translateY(${item.start}px)` }}
    >
      {formatLogTime(entry.at)}
      {entry.line}
    </div>
  );
}

export function DebugLogViewer({
  visible,
  stream,
  entries,
  scrollRef,
  virtualizer,
}: DebugLogViewerProps) {
  const { t } = useI18n();

  const placeholder = placeholderKey(visible, stream, entries.length);
  if (placeholder) return <p className="debug-empty muted">{t(placeholder)}</p>;

  const canvasStyle = { height: virtualizer.getTotalSize() };
  const rows = virtualizer.getVirtualItems();
  return (
    <div className="debug-log-viewer" ref={scrollRef}>
      <div className="debug-log-virtual" style={canvasStyle}>
        {rows.map(item => (
          <LogLine
            key={item.key}
            item={item}
            entry={entries[item.index]!}
            measure={virtualizer.measureElement}
          />
        ))}
      </div>
    </div>
  );
}