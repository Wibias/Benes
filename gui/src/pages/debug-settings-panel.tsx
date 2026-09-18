/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useI18n } from "../i18n/shared";
import { IconRefresh } from "../icons";
import { Select, Switch } from "../ui";
import type { DebugFlag, DebugSettings, LogStream } from "./debug-shared";
import {
  debugFlagSource,
  enabledTextualStreams,
  hasRuntimeOverrides,
  isClaudeInboundApplicable,
  isDebugFlagEnabled,
} from "./debug-shared";

const TEXTUAL_FLAGS: DebugFlag[] = ["debug", "usage", "injection"];

function sourceLabel(t: ReturnType<typeof useI18n>["t"], source: "runtime" | "env" | null): string | null {
  if (source === "runtime") return t("debug.source.runtime");
  if (source === "env") return t("debug.source.env");
  return null;
}

function captureFlagLabel(t: ReturnType<typeof useI18n>["t"], flag: DebugFlag): string {
  if (flag === "debug") return t("debug.debug");
  if (flag === "usage") return t("debug.usage");
  if (flag === "injection") return t("debug.injection");
  return t("debug.claude");
}

function streamOptionLabel(t: ReturnType<typeof useI18n>["t"], stream: LogStream): string {
  if (stream === "provider") return t("debug.streamProvider");
  if (stream === "usage") return t("debug.streamUsage");
  return t("debug.streamInjection");
}

function CaptureItem({
  t,
  debug,
  flag,
  busy,
  onSetFlag,
}: {
  t: ReturnType<typeof useI18n>["t"];
  debug: DebugSettings;
  flag: DebugFlag;
  busy: boolean;
  onSetFlag: (flag: DebugFlag, enabled: boolean) => void;
}) {
  const checked = isDebugFlagEnabled(debug, flag);
  const source = sourceLabel(t, debugFlagSource(debug, flag));
  const label = captureFlagLabel(t, flag);
  return (
    <div className="debug-capture-item">
      <Switch
        on={checked}
        disabled={busy}
        label={label}
        onClick={() => onSetFlag(flag, !checked)}
      />
      <div className="debug-capture-copy">
        <span>{label}</span>
        {source && <span className="muted">{source}</span>}
      </div>
    </div>
  );
}

export function DebugCapturePanel({
  debug,
  debugBusy,
  onSetFlag,
  onReset,
}: {
  debug: DebugSettings;
  debugBusy: boolean;
  onSetFlag: (flag: DebugFlag, enabled: boolean) => void;
  onReset: () => void;
}) {
  const { t } = useI18n();
  const flags: DebugFlag[] = isClaudeInboundApplicable(debug)
    ? [...TEXTUAL_FLAGS, "claude"]
    : TEXTUAL_FLAGS;
  return (
    <section className="debug-section debug-capture">
      <div className="debug-section-head">
        <h3>{t("debug.capture")}</h3>
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          disabled={debugBusy || !hasRuntimeOverrides(debug)}
          onClick={onReset}
        >
          {t("debug.reset")}
        </button>
      </div>
      <div className="debug-capture-row">
        {flags.map(flag => (
          <CaptureItem
            key={flag}
            t={t}
            debug={debug}
            flag={flag}
            busy={debugBusy}
            onSetFlag={onSetFlag}
          />
        ))}
      </div>
    </section>
  );
}

export function DebugOutputToolbar({
  debug,
  stream,
  refreshing,
  follow,
  onStreamChange,
  onRefresh,
  onFollowChange,
}: {
  debug: DebugSettings | null;
  stream: LogStream;
  refreshing: boolean;
  follow: boolean;
  onStreamChange: (stream: LogStream) => void;
  onRefresh: () => void;
  onFollowChange: (follow: boolean) => void;
}) {
  const { t } = useI18n();
  const streams = enabledTextualStreams(debug);
  const streamEnabled = streams.includes(stream);
  const hasStreams = streams.length > 0;
  return (
    <div className="debug-output-toolbar">
      {hasStreams && (
        <div className="debug-stream-field">
          <span>{t("debug.stream")}</span>
          <Select
            value={streamEnabled ? stream : streams[0]}
            options={streams.map(value => ({ value, label: streamOptionLabel(t, value) }))}
            onChange={value => onStreamChange(value as LogStream)}
            label={t("debug.stream")}
            chevron="down"
          />
        </div>
      )}
      <div className="debug-output-actions">
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          disabled={refreshing || !hasStreams}
          onClick={onRefresh}
        >
          <IconRefresh /> {t("debug.refresh")}
        </button>
        {hasStreams && (
          <label className="muted text-control diagnostics-auto-refresh">
            <input type="checkbox" checked={follow} onChange={event => onFollowChange(event.target.checked)} />
            {t("debug.follow")}
          </label>
        )}
      </div>
    </div>
  );
}
