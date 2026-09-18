/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { AppServerStateOutcome } from "../listener-commands";
import type { CodexRestartController } from "../use-codex-restart";
import { useI18n } from "../i18n/shared";

/**
 * Shown only when the proxy says a running Codex app-server predates the current
 * catalog. `fresh`, `not_running`, `unknown`, and a failed reading all render
 * nothing: telling a user "we could not tell" on a page about models is noise, and
 * the sidebar control stays available regardless.
 *
 * The banner owns no transport, no pending state, and no refresh logic. The page
 * passes one controller whose `onSettled` already re-reads staleness, so this
 * button, the page-head button, and the sidebar button all clear the banner the
 * same way.
 */

/** What the proxy last reported about the Codex app-server it started. */
type AppServerReading = AppServerStateOutcome["state"];

/** The one reading that earns a banner; every other reading renders nothing. */
const STALE_READING: AppServerReading = "stale";

interface CodexStaleBannerProps {
  state: AppServerReading;
  controller: CodexRestartController;
}

interface StaleNoticeProps {
  text: string;
  /** Names the restart action, in whichever of its two states applies right now. */
  actionLabel: string;
  restarting: boolean;
  onRestart: () => void;
}

function restartLabelKey(restarting: boolean): "dash.codexRestart" | "dash.codexRestarting" {
  return restarting ? "dash.codexRestarting" : "dash.codexRestart";
}

/** The notice itself is copy-free: the caller has already decided a restart is worth offering. */
function StaleNotice({ text, actionLabel, restarting, onRestart }: StaleNoticeProps) {
  return (
    <div role="status" className="codex-stale-banner">
      <span className="codex-stale-banner-text">{text}</span>
      <button type="button" className="btn btn-sm" disabled={restarting} onClick={onRestart}>
        {actionLabel}
      </button>
    </div>
  );
}

export function CodexStaleBanner({ state, controller }: CodexStaleBannerProps) {
  const { t } = useI18n();
  const restarting = controller.restarting;
  if (state !== STALE_READING) return null;
  return (
    <StaleNotice
      text={t("models.staleBanner")}
      actionLabel={t(restartLabelKey(restarting))}
      restarting={restarting}
      onRestart={() => {
        void controller.restart();
      }}
    />
  );
}
