/** Benes dashboard client for the Go proxy (`internal/server`). */
import { Notice } from "../ui";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { useI18n } from "../i18n/shared";
import { DebugCapturePanel } from "./debug-settings-panel";
import type { DebugFlag, DebugSettings } from "./debug-shared";

/** Load state reported for the settings request. */
export type DebugLoadState = { showError: boolean; showSkeleton: boolean };

export type DebugSettingsGateProps = {
  debug: DebugSettings | null;
  debugState: DebugLoadState;
  debugBusy: boolean;
  onRetry: () => void;
  onSetFlag: (flag: DebugFlag, enabled: boolean) => void;
  onReset: () => void;
};

/** What the pane shows while it has no settings to render yet. */
type GateState = "blocked" | "skeleton" | "empty";

/**
 * Decide what the pane shows before it has settings.
 *
 * A failed first load outranks a skeleton: the reader needs the retry control more than a
 * placeholder that will never fill in.
 */
function gateState(load: DebugLoadState): GateState {
  if (load.showError) return "blocked";
  return load.showSkeleton ? "skeleton" : "empty";
}

/** Retry control shared by the blocking and the inline failure notices. */
function RetryControl({ label, onClick }: { label: string; onClick: () => void }) {
  return (
    <button type="button" className="btn btn-ghost btn-sm" onClick={onClick}>
      {label}
    </button>
  );
}

export function DebugSettingsGate(props: DebugSettingsGateProps) {
  const { t } = useI18n();
  const { debug, debugState, debugBusy, onRetry, onSetFlag, onReset } = props;

  if (!debug) {
    const state = gateState(debugState);
    if (state === "blocked") {
      return (
        <div className="notice notice-err" role="alert">
          <span>{t("debug.loadFailed")}</span>
          <RetryControl label={t("common.retry")} onClick={onRetry} />
        </div>
      );
    }
    if (state === "skeleton") return <DataSurfaceSkeleton label={t("debug.loading")} rows={3} />;
    return null;
  }

  return (
    <>
      <DebugCapturePanel
        debug={debug}
        debugBusy={debugBusy}
        onSetFlag={onSetFlag}
        onReset={onReset}
      />
      {debugState.showError && <Notice tone="err">{t("debug.loadFailed")}</Notice>}
    </>
  );
}