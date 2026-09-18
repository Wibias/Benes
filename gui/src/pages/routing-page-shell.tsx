/**
 * Routing page chrome: the page head, the Codex staleness banner, the tab strip, and one panel
 * per surface. The workspaces arrive as children, so this shell owns layout and the failure
 * boundary and nothing about Profiles, Combos or Compatibility state.
 */
import type { ReactNode } from "react";
import { CodexStaleBanner } from "../components/codex-stale-banner";
import type { CodexRestartController } from "../use-codex-restart";
import type { AppServerStateOutcome } from "../listener-commands";
import ErrorBoundary from "../components/ErrorBoundary";
import { IconRefresh } from "../icons";
import type { TFn, TKey } from "../i18n/shared";
import Combos from "./Combos";
import RoutingProfiles from "./RoutingProfiles";
import RequestPolicyServiceTierSetting from "./RequestPolicyServiceTierSetting";
import CompatibilityMatrix from "./CompatibilityMatrix";
import { RoutingTabStrip } from "./routing-tab-strip";
import {
  ROUTING_PROFILES_PANEL_ID,
  isRoutingDataSurface,
  routingPanelDomId,
  routingTabDomId,
  type RoutingPrimaryTab,
  type RoutingSurface,
} from "./routing-tab";

/** The copy every surface boundary renders; a surface that throws loses itself, not the page. */
function routingBoundaryCopy(t: TFn, tab: TKey) {
  return {
    pageName: t(tab),
    title: t("errorBoundary.title"),
    message: t("errorBoundary.message"),
    detailsLabel: t("errorBoundary.details"),
    reloadLabel: t("errorBoundary.reload"),
  };
}

/**
 * One surface's tabpanel. Every Routing surface is a child of exactly this wrapper, so the
 * tab/panel pairing (`id` ↔ `aria-labelledby`) and the `hidden` rule are stated once.
 */
function RoutingSurfacePanel({
  surface,
  fill = false,
  hidden,
  children,
}: {
  /** The primary tab this panel belongs to; also the tab that labels it. */
  surface: RoutingPrimaryTab;
  /** The data surfaces fill the viewport; Compatibility scrolls with the page. */
  fill?: boolean;
  hidden: boolean;
  children?: ReactNode;
}) {
  return (
    <div
      className={fill ? "models-tab-panel models-tab-panel--fill" : "models-tab-panel"}
      role="tabpanel"
      id={surface === "profiles" ? ROUTING_PROFILES_PANEL_ID : routingPanelDomId(surface)}
      aria-labelledby={routingTabDomId(surface)}
      hidden={hidden}
    >
      {children}
    </div>
  );
}

export function RoutingPageShell({
  t,
  surface,
  appServerState,
  codexController,
  onRefresh,
  refreshNonce,
  mounted,
  apiBase,
  onSelectSurface,
}: {
  t: TFn;
  surface: RoutingSurface;
  appServerState: AppServerStateOutcome["state"];
  codexController: CodexRestartController;
  onRefresh: () => void;
  refreshNonce: number;
  mounted: ReadonlySet<RoutingSurface>;
  apiBase: string;
  onSelectSurface: (surface: RoutingSurface) => void;
}) {
  // Which surface is on screen, and which ones a reader has already opened. A visited surface
  // stays mounted behind the visible one, so returning to it shows what it had.
  const showsCombos = surface === "combos";
  const showsCompatibility = surface === "compatibility";
  const showsProfiles = isRoutingDataSurface(surface);
  const dataOpened = [...mounted].some(isRoutingDataSurface);

  return (
    <>
      <header className="page-head">
        <h2>{t("nav.routing")}</h2>
        <div className="page-head-actions">
          <button
            type="button"
            className="btn btn-ghost btn-icon"
            onClick={onRefresh}
            aria-label={t("startup.refresh")}
          >
            <IconRefresh />
          </button>
        </div>
      </header>
      <CodexStaleBanner state={appServerState} controller={codexController} />
      <RoutingTabStrip surface={surface} onSelect={onSelectSurface} />

      <RoutingSurfacePanel surface="combos" fill hidden={!showsCombos}>
        {mounted.has("combos") && (
          <ErrorBoundary {...routingBoundaryCopy(t, "models.tab.combos")}>
            <Combos apiBase={apiBase} active={showsCombos} refreshNonce={refreshNonce} />
          </ErrorBoundary>
        )}
      </RoutingSurfacePanel>

      <RoutingSurfacePanel surface="profiles" fill hidden={!showsProfiles}>
        {dataOpened && (
          <ErrorBoundary {...routingBoundaryCopy(t, "routing.tab.profiles")}>
            {surface === "profiles" && <RequestPolicyServiceTierSetting apiBase={apiBase} active />}
            <RoutingProfiles
              apiBase={apiBase}
              active={showsProfiles}
              surface={surface === "evaluation" || surface === "analytics" ? surface : "profiles"}
              refreshNonce={refreshNonce}
              onOpenEvaluation={() => onSelectSurface("evaluation")}
              onOpenAnalytics={() => onSelectSurface("analytics")}
              onOpenOverview={() => onSelectSurface("profiles")}
            />
          </ErrorBoundary>
        )}
      </RoutingSurfacePanel>

      <RoutingSurfacePanel surface="compatibility" hidden={!showsCompatibility}>
        {mounted.has("compatibility") && (
          <ErrorBoundary {...routingBoundaryCopy(t, "models.tab.compatibility")}>
            <CompatibilityMatrix
              apiBase={apiBase}
              active={showsCompatibility}
              refreshNonce={refreshNonce}
            />
          </ErrorBoundary>
        )}
      </RoutingSurfacePanel>
    </>
  );
}
