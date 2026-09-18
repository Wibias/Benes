/** Benes dashboard client for the Go proxy (`internal/server`). */
import {
  PlaneFooter,
  PlaneSplit,
  PlaneStatsRow,
} from "./dashboard-plane-sections";
import { defaultModelRows, issueRows, listenAddr } from "./dashboard-plane-data";
import type { useDashboardData } from "./use-dashboard-data";

type Dash = ReturnType<typeof useDashboardData>;

export function DashboardPlaneBoard(d: Dash) {
  const {
    t,
    locale,
    settings,
    providers,
    usage30d,
    startupHealth,
    projectConfigWarnings,
  } = d;
  const defaults = defaultModelRows(providers);
  const issues = issueRows(t, startupHealth, projectConfigWarnings);
  const requests = usage30d?.summary.requests ?? 0;
  const tokens = usage30d?.summary.totalTokens ?? 0;
  // The listener's liveness probe publishes no version, so the footer names the bundle that is
  // actually rendering. There is no uptime source at all, and none is shown.
  const version = typeof __APP_VERSION__ === "string" ? __APP_VERSION__ : "—";

  return (
    <div className="plane-board">
      <PlaneStatsRow t={t} addr={listenAddr(settings)} defaults={defaults} issues={issues} />
      <PlaneSplit t={t} locale={locale} requests={requests} tokens={tokens} hasUsage={requests > 0} fails={0} />
      <PlaneFooter version={version} />
    </div>
  );
}
