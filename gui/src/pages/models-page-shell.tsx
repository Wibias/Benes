/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ReactNode } from "react";
import { CodexStaleBanner } from "../components/codex-stale-banner";
import type { CodexRestartController } from "../use-codex-restart";
import type { AppServerStateOutcome } from "../listener-commands";
import ErrorBoundary from "../components/ErrorBoundary";
import type { TFn } from "../i18n/shared";
import { Notice } from "../ui";
import { DataSurfaceSkeleton } from "../components/data-surface";

type ModelsCatalogSurfaceProps = {
  t: TFn;
  cold: boolean;
  failure: string | null;
  content: ReactNode;
  retry: () => void;
};

function ModelsCatalogSurface({
  t,
  cold,
  failure,
  content,
  retry,
}: ModelsCatalogSurfaceProps) {
  if (cold) {
    return <DataSurfaceSkeleton label={t("models.loading")} rows={5} />;
  }

  if (failure) {
    return (
      <div>
        <Notice tone="err">{failure}</Notice>
        <button type="button" className="btn btn-ghost btn-sm" onClick={retry}>
          {t("common.retry")}
        </button>
      </div>
    );
  }

  return <>{content}</>;
}

export function ModelsPageShell({
  t,
  appServerState,
  codexController,
  catalogCold,
  catalogColdFailure,
  catalogPanel,
  onRetryCatalog,
}: {
  t: TFn;
  appServerState: AppServerStateOutcome["state"];
  codexController: CodexRestartController;
  catalogCold: boolean;
  catalogColdFailure: string | null;
  catalogPanel: ReactNode;
  onRetryCatalog: () => void;
}) {
  const boundaryCopy = {
    pageName: t("models.tab.catalog"),
    title: t("errorBoundary.title"),
    message: t("errorBoundary.message"),
    detailsLabel: t("errorBoundary.details"),
    reloadLabel: t("errorBoundary.reload"),
  };

  return (
    <>
      <header className="page-head">
        <h2>{t("nav.models")}</h2>
      </header>
      <CodexStaleBanner state={appServerState} controller={codexController} />
      <section className="models-tab-panel models-tab-panel--fill">
        <ErrorBoundary {...boundaryCopy}>
          <ModelsCatalogSurface
            t={t}
            cold={catalogCold}
            failure={catalogColdFailure}
            content={catalogPanel}
            retry={onRetryCatalog}
          />
        </ErrorBoundary>
      </section>
    </>
  );
}
