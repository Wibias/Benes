/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";
import { navigateHash } from "../hash-routing";
import { logsHashForSession } from "./logs-session-filter";
import { SessionFact } from "./sessions-fact";
import {
  coverageView,
  formatExactCount,
  formatSessionCost,
  formatSessionTimestamp,
  joinIdList,
  type CoverageView,
  type SessionDetail,
} from "./sessions-contract";

export function SessionsDetail({
  t,
  locale,
  detail,
  loading,
}: {
  t: TFn;
  locale: string;
  detail: SessionDetail | null;
  loading: boolean;
}) {
  if (!detail) {
    return (
      <section className="sessions-detail" aria-label={t("sessions.detail.title")}>
        <div className="sessions-detail-head">
          <h3>{t("sessions.detail.title")}</h3>
        </div>
        <p className="sessions-detail-empty">{loading ? t("sessions.detail.loading") : t("sessions.detail.empty")}</p>
      </section>
    );
  }
  const session = detail.session;
  const usage = detail.aggregates.usage;
  const utc = t("sessions.timestampUtc");
  const rows: Array<{ label: string; view: CoverageView }> = [
    { label: t("sessions.usage.input"), view: coverageView(usage.inputTokens, value => formatExactCount(value, locale)) },
    { label: t("sessions.usage.cached"), view: coverageView(usage.cachedInputTokens, value => formatExactCount(value, locale)) },
    { label: t("sessions.usage.output"), view: coverageView(usage.outputTokens, value => formatExactCount(value, locale)) },
    { label: t("sessions.usage.total"), view: coverageView(usage.totalTokens, value => formatExactCount(value, locale)) },
    { label: t("sessions.usage.cost"), view: formatSessionCost(usage.cost, locale) },
  ];
  return (
    <section className="sessions-detail" aria-busy={loading} aria-label={t("sessions.detail.title")}>
      <div className="sessions-detail-head">
        <h3>{t("sessions.detail.title")}</h3>
        <button
          type="button"
          className="providers-link"
          onClick={() => navigateHash(logsHashForSession(session.id))}
        >
          {t("sessions.detail.diagnostics")}
        </button>
      </div>
      <dl className="sessions-facts">
        <SessionFact label={t("sessions.field.namespace")} value={session.namespace} />
        <SessionFact label={t("sessions.field.externalId")} value={session.externalId} />
        <SessionFact label={t("sessions.field.started")} value={formatSessionTimestamp(session.startedAt, locale, utc)} />
        <SessionFact label={t("sessions.field.lastActivity")} value={formatSessionTimestamp(session.lastActivityAt, locale, utc)} />
        <SessionFact label={t("sessions.field.requests")} value={formatExactCount(session.requestCount, locale)} />
      </dl>
      <section className="sessions-block">
        <h4>{t("sessions.usage")}</h4>
        <dl className="sessions-facts">
          {rows.map(row => (
            <div key={row.label} className="sessions-fact">
              <dt>{row.label}</dt>
              <dd>
                <span>{row.view.valueText ?? "\u2014"}</span>
                {row.view.attributedLine && (
                  <span className="sessions-coverage">
                    {t("sessions.usage.attributed", {
                      attributed: formatExactCount(row.view.attributedRequests, locale),
                      total: formatExactCount(row.view.totalRequests, locale),
                    })}
                  </span>
                )}
              </dd>
            </div>
          ))}
        </dl>
      </section>
      <section className="sessions-block">
        <h4>{t("sessions.routing")}</h4>
        <dl className="sessions-facts">
          <SessionFact label={t("sessions.routing.models")} value={joinIdList(detail.aggregates.models)} />
          <SessionFact label={t("sessions.routing.providers")} value={joinIdList(detail.aggregates.providers)} />
          <SessionFact label={t("sessions.routing.policies")} value={joinIdList(detail.aggregates.policyIds)} />
          <SessionFact label={t("sessions.routing.combos")} value={joinIdList(detail.aggregates.comboIds)} />
          <SessionFact label={t("sessions.routing.failovers")} value={String(detail.aggregates.failoverRequestCount)} />
        </dl>
      </section>
    </section>
  );
}
