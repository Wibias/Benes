/** Benes dashboard client for the Go proxy (`internal/server`). */
import { formatTokens } from "../format-tokens";
import { navigateHash } from "../hash-routing";
import type { TFn } from "../i18n/shared";

const CHART_TICKS = ["00:00", "06:00", "12:00", "18:00", "24:00"];

function PlaneStat({ kicker, primary, secondary, mono, muted }: {
  kicker: string;
  primary: string;
  secondary?: string;
  mono?: boolean;
  muted?: boolean;
}) {
  return (
    <section className="plane-stat">
      <div className="plane-kicker">{kicker}</div>
      <div className={`plane-stat-primary${muted ? " plane-stat-primary--muted" : ""}`}>{primary}</div>
      {secondary ? <div className={`plane-stat-secondary${mono ? " plane-stat-mono" : ""}`}>{secondary}</div> : null}
    </section>
  );
}

export function PlaneStatsRow({
  t, addr, defaults, issues,
}: {
  t: TFn;
  addr: string;
  defaults: string[];
  issues: Array<{ text: string }>;
}) {
  // The defaults panel names provider default models; with none configured it says so rather
  // than borrowing the traffic empty state.
  const defaultPrimary = defaults[0] ?? t("dash.noDefaults");
  const defaultSecondary = defaults.slice(1).join(" · ");
  const issuePrimary = issues[0]?.text ?? t("dash.noIssues");
  const issueSecondary = issues.slice(1).map(issue => issue.text).join(" · ");
  return (
    <div className="plane-stats">
      <PlaneStat kicker={t("dash.plane")} primary={t("dash.listening")} secondary={addr} mono />
      <PlaneStat kicker={t("dash.live")} primary={t("dash.sessionsLive", { n: 0 })} secondary={`0 ${t("dash.active")} · 0 ${t("dash.idle")}`} />
      <PlaneStat kicker={t("dash.defaults")} primary={defaultPrimary} secondary={defaultSecondary || undefined} />
      <PlaneStat kicker={t("dash.issues")} primary={issuePrimary} secondary={issueSecondary || undefined} muted={!issues.length} />
    </div>
  );
}

export function PlaneSplit({ t, locale, requests, tokens, hasUsage, fails }: {
  t: TFn;
  locale: string;
  requests: number;
  tokens: number;
  hasUsage: boolean;
  fails: number;
}) {
  return (
    <div className="plane-split">
      <div className="plane-split-main">
        <section className="plane-block">
          <div className="plane-block-head">
            <div className="plane-kicker">{t("dash.last24hByHarness")}</div>
            <button type="button" className="btn btn-ghost" onClick={() => navigateHash("harnesses")}>
              {t("dash.openHarnesses")}
            </button>
          </div>
          <table className="plane-table">
            <thead>
              <tr>
                <th>{t("dash.colHarness")}</th>
                <th>{t("dash.colReq")}</th>
                <th>{t("dash.colTokens")}</th>
                <th>{t("dash.colFail")}</th>
                <th>{t("dash.dominantRoute")}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td colSpan={5} className="plane-table-empty">{t("dash.noTraffic")}</td>
              </tr>
            </tbody>
          </table>
        </section>
        <section className="plane-block plane-chart" aria-hidden="true">
          <div className="plane-chart-plot">
            <span className="plane-chart-baseline" />
          </div>
          <div className="plane-chart-ticks">
            {CHART_TICKS.map(tick => (
              <span key={tick}>{tick}</span>
            ))}
          </div>
          <div className="plane-chart-summary">
            {t("dash.requestsSummary", {
              requests: hasUsage ? String(requests) : "0",
              tokens: hasUsage ? formatTokens(tokens, locale) : "0",
              fails: String(fails),
            })}
          </div>
        </section>
      </div>
      <div className="plane-split-side">
        <section className="plane-block">
          <div className="plane-kicker">{t("dash.tokensByModel")}</div>
          <p className="plane-empty">{t("dash.noTraffic")}</p>
        </section>
        <section className="plane-block">
          <div className="plane-kicker">{t("dash.lastRoutes")}</div>
          <table className="plane-table">
            <thead>
              <tr>
                <th>{t("dash.colTime")}</th>
                <th>{t("dash.colPath")}</th>
                <th>{t("dash.colModel")}</th>
                <th>{t("dash.colSource")}</th>
                <th>{t("dash.colResult")}</th>
              </tr>
            </thead>
            <tbody>
              <tr>
                <td colSpan={5} className="plane-table-empty">{t("dash.noTraffic")}</td>
              </tr>
            </tbody>
          </table>
        </section>
      </div>
    </div>
  );
}

/** The running bundle's build version. The listener publishes no version of its own. */
export function PlaneFooter({ version }: { version: string }) {
  return (
    <footer className="plane-footer">
      <span>Benes {version}</span>
    </footer>
  );
}
