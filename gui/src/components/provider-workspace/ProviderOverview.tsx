/**
 * ProviderOverview — connection, models, downstream use, recent events.
 */
import { useT, useI18n, type TFn } from "../../i18n/shared";
import { IconAlert } from "../../icons";
import { navigateHash } from "../../hash-routing";
import { type WorkspaceItem } from "../../provider-workspace/catalog";
import { latestProviderValidationAt, providerEventLabel } from "../../provider-workspace/events";
import { formatElapsedSince } from "../../relative-clock";
import { type ProviderQuotaReportView } from "../../provider-workspace/report";
import {
  accessMethodsCopyKey,
  deriveAccessDescriptor,
} from "../../provider-workspace/auth";
import {
  overviewConnected,
  overviewDefaultAccessKind,
  overviewLastValidatedAt,
  overviewListedModels,
  recentOverviewEvents,
} from "../../provider-workspace/access-presentation";
import { ProviderEvents, type ProviderEventEntry } from "./ProviderEvents";
import type { ApiKeyRow, OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { WorkspaceEvent } from "./ProviderWorkspaceShell";

const DEFAULT_ACCESS_COPY = {
  "openai-api": "prov.access.openaiApi",
  "chatgpt-pool": "prov.access.chatgptPool",
  valid: "prov.connection.valid",
  none: null,
} as const;

export default function ProviderOverview({
  item, oauthEmail, oauth,
  keys,
  onReauthenticate, onCancelLogin, reauthBusy = false,
  availableModels = [],
  selectedModels = [],
  downstream,
  lastValidated,
  onManageAccess,
  recentEvents = [],
  apiLanePresent = false,
}: {
  item: WorkspaceItem;
  /** Retained for caller compatibility; quota observation is not validation evidence. */
  quotaReport?: ProviderQuotaReportView;
  oauthEmail?: string;
  oauth?: { loggedIn?: boolean };
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  onReauthenticate?: () => void;
  onCancelLogin?: () => void;
  reauthBusy?: boolean;
  availableModels?: string[];
  selectedModels?: string[];
  downstream?: { harnesses: number | null; routes: number; subagents: number };
  /** Authoritative validation evidence from /api/providers/workspace. null means not validated. */
  lastValidated?: number | null;
  onManageAccess?: () => void;
  recentEvents?: WorkspaceEvent[];
  apiLanePresent?: boolean;
}) {
  const t = useT();
  const { locale } = useI18n();
  const access = deriveAccessDescriptor(item, apiLanePresent || Boolean(keys?.length));
  const methodsKey = accessMethodsCopyKey(access);
  const defaultKind = overviewDefaultAccessKind({
    defaultMethodId: access.defaultMethodId,
    defaultAccess: access.defaultAccess,
    connected: overviewConnected({
      oauthEmail,
      oauthLoggedIn: oauth?.loggedIn,
      hasActiveKey: keys?.some(entry => entry.active),
    }),
  });
  const defaultCopy = DEFAULT_ACCESS_COPY[defaultKind];
  const models = overviewListedModels(selectedModels, availableModels);
  const lastValidatedAt = overviewLastValidatedAt(lastValidated, latestProviderValidationAt(recentEvents));
  const events = recentOverviewEvents(recentEvents).map((event, index) => ({
    key: `${event.timestamp}:${event.type}:${index}`,
    at: eventClock(event.timestamp, locale),
    label: providerEventLabel(event, t),
    severity: event.severity,
  }));

  return (
    <div className="providers-overview">
      {item.activeNeedsReauth && (
        <OverviewAttention
          forward={item.authMode === "forward"}
          reauthBusy={reauthBusy}
          onReauthenticate={onReauthenticate}
          onCancelLogin={onCancelLogin}
        />
      )}
      <OverviewConnection
        t={t}
        methodsKey={methodsKey}
        defaultAccessLabel={defaultCopy ? t(defaultCopy) : "—"}
        lastValidatedAt={lastValidatedAt}
        onManageAccess={onManageAccess}
      />
      <OverviewModels listedCount={models.listed.length} unavailableCount={models.unavailableCount} />
      <OverviewDownstream downstream={downstream} />
      <OverviewHealth events={events} />
    </div>
  );
}

function eventClock(ms: number | undefined, locale: string): string {
  if (!ms || !Number.isFinite(ms)) return "—";
  return new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false }).format(new Date(ms > 10_000_000_000 ? ms : ms * 1000));
}

function OverviewAttention({
  forward, reauthBusy, onReauthenticate, onCancelLogin,
}: {
  forward: boolean;
  reauthBusy: boolean;
  onReauthenticate?: () => void;
  onCancelLogin?: () => void;
}) {
  const t = useT();
  return (
    <div className="pws-auth-summary pws-auth-summary--warn" role="status">
      <IconAlert style={{ width: 14, height: 14 }} aria-hidden="true" />
      <div className="pws-auth-summary-body">
        <span>
          <strong>{t("pws.status.needsAttention")}</strong>
          {" — "}
          {forward ? t("pws.attention.reauthForward") : t("pws.attention.reauth")}
        </span>
        {onReauthenticate && (
          <button type="button" className="btn btn-primary btn-sm" disabled={reauthBusy} onClick={() => onReauthenticate()}>
            {reauthBusy ? t("prov.waitingBrowser") : t("pws.reauthenticate")}
          </button>
        )}
        {reauthBusy && onCancelLogin && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => onCancelLogin()}>
            {t("common.cancel")}
          </button>
        )}
      </div>
    </div>
  );
}

function OverviewConnection({
  t, methodsKey, defaultAccessLabel, lastValidatedAt, onManageAccess,
}: {
  t: TFn;
  methodsKey: ReturnType<typeof accessMethodsCopyKey>;
  defaultAccessLabel: string;
  lastValidatedAt: number | null;
  onManageAccess?: () => void;
}) {
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.connection.title")}</h4>
        {onManageAccess && (
          <button type="button" className="providers-link" onClick={onManageAccess}>
            {t("prov.connection.manageAccess")}
          </button>
        )}
      </div>
      <div className="providers-overview-slot">
        <dl className="providers-facts">
          <div>
            <dt>{t("prov.access.methods")}</dt>
            <dd>{methodsKey ? t(methodsKey) : "—"}</dd>
          </div>
          <div>
            <dt>{t("prov.access.defaultAccess")}</dt>
            <dd>{defaultAccessLabel}</dd>
          </div>
          <div>
            <dt>{t("prov.connection.validated")}</dt>
            <dd>{lastValidatedAt == null ? t("time.notChecked") : formatElapsedSince(lastValidatedAt, t)}</dd>
          </div>
        </dl>
      </div>
    </section>
  );
}

function OverviewModels({ listedCount, unavailableCount }: { listedCount: number; unavailableCount: number }) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.modelsExposed.title")}</h4>
        <button type="button" className="providers-link" onClick={() => navigateHash("models")}>
          {t("prov.modelsExposed.view")}
        </button>
      </div>
      <div className="providers-overview-slot">
        <div className="providers-models-counts">
          <span>
            {t("prov.modelsExposed.activeModels", { n: String(listedCount) })}
            <span className="providers-models-sep" aria-hidden="true">|</span>
            {t("prov.modelsExposed.disabled", { n: String(unavailableCount) })}
          </span>
        </div>
      </div>
    </section>
  );
}

function OverviewDownstream({
  downstream,
}: {
  downstream?: { harnesses: number | null; routes: number; subagents: number };
}) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.downstream.title")}</h4>
        <div className="providers-down-links">
          <button type="button" className="providers-link" onClick={() => navigateHash("harnesses")}>
            {t("prov.downstream.viewHarnesses")}
          </button>
          <button type="button" className="providers-link" onClick={() => navigateHash("models/routing")}>
            {t("prov.downstream.viewRouting")}
          </button>
          <button type="button" className="providers-link" onClick={() => navigateHash("subagents")}>
            {t("prov.downstream.viewSubagents")}
          </button>
        </div>
      </div>
      <div className="providers-overview-slot">
        <div className="providers-down-metrics">
          <div className="providers-down-stat">
            <strong>{downstream?.harnesses == null ? "—" : String(downstream.harnesses)}</strong>
            <span>{t("prov.downstream.harnessesUsing")}</span>
          </div>
          <div className="providers-down-stat">
            <strong>{downstream ? String(downstream.routes) : "—"}</strong>
            <span>{t("prov.downstream.routesUsing")}</span>
          </div>
          <div className="providers-down-stat">
            <strong>{downstream ? String(downstream.subagents) : "—"}</strong>
            <span>{t("prov.downstream.subagents")}</span>
          </div>
        </div>
      </div>
    </section>
  );
}

function OverviewHealth({ events }: { events: ProviderEventEntry[] }) {
  const t = useT();
  return (
    <section className="providers-block">
      <div className="providers-block-head">
        <h4>{t("prov.health.title")}</h4>
        <button type="button" className="providers-link" onClick={() => navigateHash("logs")}>
          {t("prov.events.view")}
        </button>
      </div>
      <div className="providers-overview-slot providers-overview-slot--events">
        {events.length === 0 ? (
          <p className="muted providers-overview-empty">{t("prov.overview.noEvents")}</p>
        ) : (
          <ProviderEvents events={events} />
        )}
      </div>
    </section>
  );
}
