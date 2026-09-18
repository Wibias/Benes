/**
 * Control page chrome: the page head, the Codex staleness banner, and the bounded health
 * panel. The board arrives as `healthPanel`, so this shell owns layout and the failure
 * boundary and nothing about health state.
 */
import type { ReactNode } from "react";
import { CodexStaleBanner } from "../components/codex-stale-banner";
import type { CodexRestartController } from "../use-codex-restart";
import type { AppServerStateOutcome } from "../listener-commands";
import ErrorBoundary from "../components/ErrorBoundary";
import { IconRefresh } from "../icons";
import type { TFn } from "../i18n/shared";

type ControlPageShellProps = {
  t: TFn;
  appServerState: AppServerStateOutcome["state"];
  codexController: CodexRestartController;
  healthPanel: ReactNode;
  healthRefreshing: boolean;
  onRefresh: () => void;
};

type RefreshActionProps = {
  t: TFn;
  busy: boolean;
  onRefresh: () => void;
};

function ControlRefreshAction({ t, busy, onRefresh }: RefreshActionProps) {
  return (
    <button type="button" className="btn btn-ghost btn-sm" onClick={onRefresh} disabled={busy}>
      <IconRefresh /> {t("startup.refresh")}
    </button>
  );
}

export function ControlPageShell({
  t,
  appServerState,
  codexController,
  healthPanel,
  healthRefreshing,
  onRefresh,
}: ControlPageShellProps) {
  const boundaryCopy = {
    pageName: t("nav.control"),
    title: t("errorBoundary.title"),
    message: t("errorBoundary.message"),
    detailsLabel: t("errorBoundary.details"),
    reloadLabel: t("errorBoundary.reload"),
  };

  return (
    <>
      <header className="page-head">
        <h2>{t("nav.control")}</h2>
        <div className="page-head-actions">
          <ControlRefreshAction t={t} busy={healthRefreshing} onRefresh={onRefresh} />
        </div>
      </header>
      <CodexStaleBanner state={appServerState} controller={codexController} />
      <section className="models-tab-panel control-health-panel">
        <ErrorBoundary {...boundaryCopy}>
          {healthPanel}
        </ErrorBoundary>
      </section>
    </>
  );
}