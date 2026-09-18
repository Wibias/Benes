/**
 * Aggregate Providers pane when no rail row is selected.
 * Divider sections only; every fleet value comes from /api/providers/workspace.
 */
import "../../styles/providers-workspace-hardening.css";
import { useMemo, useState } from "react";
import { useI18n, useT, type TFn } from "../../i18n/shared";
import { IconAlert, IconX } from "../../icons";
import { navigateHash } from "../../hash-routing";
import { formatProviderDisplayName } from "../../provider-icons";
import { canSyncModelDiscovery, postModelDiscoverySync } from "../../model-discovery-sync";
import {
  overviewIssueToneClass,
  providerEventSeverity,
  providerEventSeverityWordKey,
  type ProviderEventSeverity,
} from "../../provider-workspace/access-presentation";
import type { WorkspaceItem, WorkspaceSections } from "../../provider-workspace/catalog";
import { providerEventLabel } from "../../provider-workspace/events";
import { formatElapsedSince } from "../../relative-clock";
import type {
  ProviderWorkspaceAggregate,
  ProviderWorkspaceEvent,
  ProviderWorkspaceIssue,
} from "../../provider-workspace/workspace";
import { ProviderMark } from "../ProviderMark";

const ISSUE_PREVIEW = 4;
const EVENT_PREVIEW = 4;

type BoardEvent = {
  at: number;
  name: string;
  label: string;
  severity: ProviderEventSeverity;
};

function eventClock(at: number, locale: string): string {
  if (!at || !Number.isFinite(at)) return "—";
  const ms = at > 10_000_000_000 ? at : at * 1000;
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false }).format(new Date(ms));
}

function issueLabel(issue: ProviderWorkspaceIssue, t: TFn): string {
  switch (issue.code) {
    case "credential_missing":
      return t("prov.overview.needsAuth");
    case "credential_store_unavailable":
      return t("pws.accountsLoadFailed");
    case "credential_pool_paused":
      return t("prov.access.paused");
    case "credential_reauth_required":
      return t("pws.attention.reauth");
    case "quota_exhausted":
      return t("prov.overview.rateLimited");
    case "provider_health_failure":
      return t("prov.health.connection");
    case "catalogue_stale":
      return t("prov.overview.cataloguesStale");
    default:
      return issue.detail?.trim() || issue.code;
  }
}

function issueNeedsAccessReview(issue: ProviderWorkspaceIssue): boolean {
  return issue.code === "credential_missing" ||
    issue.code === "credential_store_unavailable" ||
    issue.code === "credential_pool_paused" ||
    issue.code === "credential_reauth_required";
}

async function syncFleetCatalogs(apiBase: string, names: string[]): Promise<boolean> {
  let ok = true;
  for (const name of names) {
    const result = await postModelDiscoverySync(apiBase, name);
    if (!result.ok && result.applicable !== false) ok = false;
  }
  return ok;
}

export default function ProviderOverviewDashboard({
  sections,
  attention,
  availability,
  downstream,
  onSelectProvider,
  onReviewProvider,
  recentEvents = [],
  onRetryModels,
  apiBase,
  onNotice,
}: {
  sections: WorkspaceSections;
  attention: ProviderWorkspaceIssue[];
  availability: ProviderWorkspaceAggregate["availability"];
  downstream: ProviderWorkspaceAggregate["downstream"];
  onSelectProvider: (name: string) => void;
  onReviewProvider: (name: string) => void;
  recentEvents?: ProviderWorkspaceEvent[];
  /** After discovery: selected-models and fleet workspace (available/unavailable, last sync). */
  onRetryModels?: () => void;
  modelsLoading?: boolean;
  apiBase?: string;
  onNotice?: (message: string, ok?: boolean) => void;
}) {
  const t = useT();
  const { locale } = useI18n();
  const [syncing, setSyncing] = useState(false);
  const allItems = useMemo(
    () => [...sections.ready, ...sections.needsSetup, ...sections.disabled],
    [sections],
  );
  const byName = useMemo(() => {
    const map = new Map<string, WorkspaceItem>();
    for (const item of allItems) map.set(item.name, item);
    return map;
  }, [allItems]);

  const previewIssues = attention.slice(0, ISSUE_PREVIEW);
  const lastSyncLabel = availability.lastModelSync == null
    ? "—"
    : formatElapsedSince(availability.lastModelSync, t);
  const events = useMemo((): BoardEvent[] => recentEvents
    .slice(-EVENT_PREVIEW)
    .reverse()
    .map(event => ({
      at: event.timestamp,
      name: event.provider,
      label: providerEventLabel(event, t),
      severity: providerEventSeverity(event.severity),
    })), [recentEvents, t]);

  const onSyncAll = () => {
    if (!canSyncModelDiscovery(apiBase, syncing)) return;
    const names = allItems.filter(item => item.disabled !== true).map(item => item.name);
    setSyncing(true);
    void (async () => {
      try {
        const ok = await syncFleetCatalogs(apiBase, names);
        onRetryModels?.();
        onNotice?.(ok ? t("prov.health.models") : t("prov.networkError"), ok);
      } catch {
        onNotice?.(t("prov.networkError"), false);
      } finally {
        setSyncing(false);
      }
    })();
  };

  return (
    <div className="providers-overview providers-overview-board">
      <h3 className="providers-overview-board-title">{t("prov.overview.fleet")}</h3>

      <section className="providers-block" aria-label={t("pws.attentionTitle")}>
        <h4>{t("pws.attentionTitle")}</h4>
        <div className="providers-overview-slot">
          {previewIssues.length === 0 ? (
            <p className="muted providers-overview-empty">{t("prov.overview.noAttention")}</p>
          ) : (
            <ul className="providers-overview-issues">
              {previewIssues.map(issue => {
                const item = byName.get(issue.provider);
                const reviewAccess = issueNeedsAccessReview(issue);
                const label = issueLabel(issue, t);
                return (
                  <li key={`${issue.provider}:${issue.code}:${issue.timestamp ?? 0}`} className="providers-overview-issue">
                    <ProviderMark
                      name={issue.provider}
                      adapter={item?.adapter ?? ""}
                      baseUrl={item?.baseUrl ?? ""}
                      className="providers-overview-issue-icon"
                    />
                    <div className="providers-overview-issue-body">
                      <div className="providers-overview-issue-line">
                        <span className="providers-overview-issue-name">{formatProviderDisplayName(issue.provider, t)}</span>
                        <span className={overviewIssueToneClass(issue.severity)}>{label}</span>
                      </div>
                      {issue.detail?.trim() && issue.detail.trim() !== label ? (
                        <div className="providers-overview-issue-detail">{issue.detail.trim()}</div>
                      ) : null}
                    </div>
                    <button
                      type="button"
                      className="providers-link"
                      onClick={() => reviewAccess ? onReviewProvider(issue.provider) : onSelectProvider(issue.provider)}
                    >
                      {reviewAccess ? t("prov.overview.review") : t("prov.overview.inspect")}
                    </button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
        {attention.length > ISSUE_PREVIEW && (
          <div className="providers-overview-footer">
            <button type="button" className="providers-link" onClick={() => navigateHash("logs")}>
              {t("prov.overview.viewIssues")}
            </button>
          </div>
        )}
      </section>

      <section className="providers-block" aria-label={t("prov.overview.availability")}>
        <h4>{t("prov.overview.availability")}</h4>
        <dl className="providers-overview-facts">
          <div>
            <dt>{t("prov.overview.modelsAvailable")}</dt>
            <dd>{availability.modelsAvailable}</dd>
          </div>
          <div>
            <dt>{t("prov.overview.modelsUnavailable")}</dt>
            <dd>{availability.modelsUnavailable}</dd>
          </div>
          <div>
            <dt>{t("prov.overview.cataloguesStale")}</dt>
            <dd>{availability.staleProviderCatalogues ?? "—"}</dd>
          </div>
        </dl>
        <div className="providers-overview-rule providers-overview-sync">
          <span>
            {t("prov.overview.lastSync")}
            {": "}
            <strong>{lastSyncLabel}</strong>
          </span>
          <button
            type="button"
            className="providers-link providers-link--plain"
            disabled={syncing}
            onClick={onSyncAll}
          >
            {t("prov.overview.syncAll")}
          </button>
        </div>
      </section>

      <section className="providers-block" aria-label={t("prov.overview.impact")}>
        <h4>{t("prov.overview.impact")}</h4>
        <dl className="providers-overview-facts">
          <div>
            <dt>{t("prov.overview.fleetHarnesses")}</dt>
            <dd>{downstream.harnessCount ?? "—"}</dd>
          </div>
          <div>
            <dt>{t("prov.overview.fleetRoutes")}</dt>
            <dd>{downstream.routeCount}</dd>
          </div>
          <div>
            <dt>{t("prov.overview.fleetSubagents")}</dt>
            <dd>{downstream.subAgentModelCount}</dd>
          </div>
        </dl>
        <div className="providers-overview-rule">
          <span className={downstream.affectedRouteCount > 0 ? "providers-overview-warn" : undefined}>
            {downstream.affectedRouteCount > 0 ? <IconAlert aria-hidden="true" /> : null}
            {downstream.affectedRouteCount === 1
              ? t("prov.overview.routesAffectedOne")
              : t("prov.overview.routesAffected", { n: downstream.affectedRouteCount })}
          </span>
          <button type="button" className="providers-link" onClick={() => navigateHash("models/routing")}>
            {t("prov.downstream.viewRouting")}
          </button>
        </div>
      </section>

      <section className="providers-block" aria-label={t("prov.health.title")}>
        <div className="providers-block-head">
          <h4>{t("prov.health.title")}</h4>
          <button type="button" className="providers-link" onClick={() => navigateHash("logs")}>
            {t("prov.events.view")}
          </button>
        </div>
        <div className="providers-overview-slot providers-overview-slot--events">
          {events.length > 0 ? (
            <ul className="providers-overview-events">
              {events.map(event => {
                const severity = event.severity;
                return (
                  <li key={`${event.at}:${event.name}:${event.label}`}>
                    <time>{eventClock(event.at, locale)}</time>
                    {severity.tone === "warn" ? <IconAlert className={severity.className} aria-hidden="true" /> : null}
                    {severity.tone === "error" ? <IconX className={severity.className} aria-hidden="true" /> : null}
                    {severity.tone === "off" ? null : (
                      <span className="sr-only">{t(providerEventSeverityWordKey(severity.tone))}</span>
                    )}
                    <span className="providers-overview-event-name">{formatProviderDisplayName(event.name, t)}</span>
                    <span>{event.label}</span>
                  </li>
                );
              })}
            </ul>
          ) : (
            <p className="muted providers-overview-empty">{t("prov.overview.noEvents")}</p>
          )}
        </div>
      </section>
    </div>
  );
}
