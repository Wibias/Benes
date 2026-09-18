import { useState, type ReactNode } from "react";
import { IconCheck, IconCopy } from "../icons";
import { formatProviderDisplayName } from "../provider-icons";
import { formatTokens } from "../format-tokens";
import { modelLabel } from "../model-display";
import { copyTextToClipboard } from "../copy-feedback";
import type { TFn } from "../i18n/shared";
import { formatLogDateTime, statusColor } from "./logs-shared";
import { protocolOptionLabel } from "./sessions-protocol-label";
import {
  formatByteCount,
  formatDiagnosticsCost,
  formatRecordedDurationMs,
  parseTimestampMs,
  type DiagnosticsAttempt,
  type DiagnosticsCost,
  type DiagnosticsRequestDetail,
  type DiagnosticsRouting,
  type DiagnosticsTiming,
  type DiagnosticsUsage,
} from "./diagnostics-contract";

function Fact({ label, value, mono, color }: { label: string; value?: ReactNode; mono?: boolean; color?: string }) {
  if (value === undefined || value === null || value === "") return null;
  return (
    <div className="diagnostics-fact">
      <dt>{label}</dt>
      <dd className={mono ? "mono" : undefined} style={color ? { color } : undefined}>{value}</dd>
    </div>
  );
}

function CopyFact({ label, value, copiedLabel, copyLabel, copied, onCopy }: {
  label: string;
  value: string;
  copyLabel: string;
  copiedLabel: string;
  copied: boolean;
  onCopy: (id: string) => void;
}) {
  const action = copied ? copiedLabel : copyLabel;
  return (
    <div className="diagnostics-fact">
      <dt>{label}</dt>
      <dd className="diagnostics-id-row">
        <span className="mono diagnostics-id-text" title={value}>{value}</span>
        <button
          type="button"
          className="diagnostics-copy"
          aria-label={action}
          title={action}
          onClick={() => onCopy(value)}
        >
          {copied ? <IconCheck aria-hidden="true" /> : <IconCopy aria-hidden="true" />}
        </button>
      </dd>
    </div>
  );
}

function routeKindLabel(t: TFn, kind: string): string {
  if (kind === "direct") return t("logs.detail.route.direct");
  if (kind === "combo") return t("logs.detail.route.combo");
  if (kind === "policy") return t("logs.detail.route.policy");
  return kind;
}

function BasicSection({
  detail, localeTag, serverTimeZone, t, copied, onCopy,
}: {
  detail: DiagnosticsRequestDetail;
  localeTag?: string;
  serverTimeZone?: string;
  t: TFn;
  copied: string | null;
  onCopy: (id: string) => void;
}) {
  const at = parseTimestampMs(detail.timestamp);
  const timestamp = at === undefined ? detail.timestamp : formatLogDateTime(at, localeTag, serverTimeZone);
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.section.basic")}</h4>
      <dl className="diagnostics-facts">
        <Fact label={t("logs.detail.timestamp")} value={timestamp} mono />
        <CopyFact
          label={t("logs.col.request")}
          value={detail.requestId}
          copyLabel={t("logs.detail.copyRequestId")}
          copiedLabel={t("logs.detail.copied")}
          copied={copied === detail.requestId}
          onCopy={onCopy}
        />
        {detail.correlationId && (
          <CopyFact
            label={t("logs.detail.correlationId")}
            value={detail.correlationId}
            copyLabel={t("logs.detail.copyId")}
            copiedLabel={t("logs.detail.copied")}
            copied={copied === detail.correlationId}
            onCopy={onCopy}
          />
        )}
        {detail.sessionId && (
          <CopyFact
            label={t("logs.filter.session")}
            value={detail.sessionId}
            copyLabel={t("logs.detail.copyId")}
            copiedLabel={t("logs.detail.copied")}
            copied={copied === detail.sessionId}
            onCopy={onCopy}
          />
        )}
        {detail.protocol && <Fact label={t("logs.filter.protocol")} value={protocolOptionLabel(t, detail.protocol)} />}
        <Fact label={t("logs.detail.method")} value={detail.method} mono />
        <Fact label={t("logs.detail.path")} value={detail.path} mono />
        <Fact label={t("logs.col.status")} value={detail.status} mono color={statusColor(detail.status)} />
        {detail.errorCode && <Fact label={t("logs.col.error")} value={detail.errorCode} mono />}
        {detail.failure?.cause && <Fact label={t("logs.detail.failure.cause")} value={detail.failure.cause} mono />}
        {detail.failure?.side && <Fact label={t("logs.detail.failure.side")} value={detail.failure.side} mono />}
        {detail.failure?.stage && <Fact label={t("logs.detail.failure.stage")} value={detail.failure.stage} mono />}
      </dl>
    </section>
  );
}

function RoutingSection({ routing, t }: { routing: DiagnosticsRouting | undefined; t: TFn }) {
  if (!routing) {
    return (
      <section className="diagnostics-block">
        <h4>{t("logs.detail.route.section")}</h4>
        <p className="diagnostics-empty-inline muted">{t("logs.detail.route.unknown")}</p>
      </section>
    );
  }
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.route.section")}</h4>
      <dl className="diagnostics-facts">
        {routing.kind && <Fact label={t("logs.detail.route.kind")} value={routeKindLabel(t, routing.kind)} />}
        {routing.requestedModel && <Fact label={t("logs.detail.route.requested")} value={modelLabel(routing.requestedModel)} mono />}
        {routing.requestedProvider && (
          <Fact label={t("logs.col.provider")} value={formatProviderDisplayName(routing.requestedProvider, t)} />
        )}
        {routing.resolvedModel && <Fact label={t("logs.detail.route.resolvedModel")} value={modelLabel(routing.resolvedModel)} mono />}
        {routing.provider && <Fact label={t("logs.detail.route.resolvedProvider")} value={formatProviderDisplayName(routing.provider, t)} />}
        {routing.kind === "policy" && routing.policyId && (
          <Fact label={t("logs.detail.route.profile")} value={routing.policyId} mono />
        )}
        {routing.kind === "combo" && routing.comboId && (
          <Fact label={t("logs.detail.route.comboId")} value={routing.comboId} mono />
        )}
        {routing.committedMember && (
          <Fact label={t("logs.detail.route.committed")} value={routing.committedMember} mono />
        )}
      </dl>
    </section>
  );
}

function timingValue(ms: number | undefined, t: TFn): string | undefined {
  return formatRecordedDurationMs(ms, t("uptime.second"));
}

function pushTimingRow(
  rows: Array<{ label: string; value: string }>,
  label: string,
  ms: number | undefined,
  t: TFn,
): void {
  const value = timingValue(ms, t);
  if (value === undefined) return;
  rows.push({ label, value });
}

function timingRows(timing: DiagnosticsTiming, t: TFn): Array<{ label: string; value: string }> {
  const rows: Array<{ label: string; value: string }> = [];
  pushTimingRow(rows, t("logs.col.duration"), timing.totalMs, t);
  pushTimingRow(rows, t("logs.detail.timing.headers"), timing.headersMs, t);
  pushTimingRow(rows, t("logs.detail.timing.firstByte"), timing.firstByteMs, t);
  pushTimingRow(rows, t("logs.detail.ttft"), timing.ttftMs, t);
  pushTimingRow(rows, t("logs.detail.timing.firstDownstream"), timing.firstDownstreamMs, t);
  pushTimingRow(rows, t("logs.detail.timing.upstreamEnd"), timing.upstreamEndMs, t);
  pushTimingRow(rows, t("logs.detail.timing.downstreamEnd"), timing.downstreamEndMs, t);
  return rows;
}

function PerformanceSection({ detail, locale, t }: { detail: DiagnosticsRequestDetail; locale: string; t: TFn }) {
  const rows = timingRows(detail.timing, t);
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.section.performance")}</h4>
      <dl className="diagnostics-facts">
        {rows.map(row => <Fact key={row.label} label={row.label} value={row.value} mono />)}
        {detail.requestBytes !== undefined && (
          <Fact label={t("logs.detail.bytes.request")} value={formatByteCount(detail.requestBytes, locale)} mono />
        )}
        {detail.responseBytes !== undefined && (
          <Fact label={t("logs.detail.bytes.response")} value={formatByteCount(detail.responseBytes, locale)} mono />
        )}
      </dl>
    </section>
  );
}

function tokenFact(label: string, value: number | undefined, locale: string) {
  if (value === undefined) return null;
  return <Fact key={label} label={label} value={formatTokens(value, locale)} mono />;
}

function UsageSection({ usage, locale, t }: { usage: DiagnosticsUsage | undefined; locale: string; t: TFn }) {
  const status = usage?.status ?? "unreported";
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.section.usage")}</h4>
      <dl className="diagnostics-facts">
        <Fact label={t("logs.detail.usage.status")} value={t(`logs.tokens.${status === "estimated" || status === "unreported" || status === "unsupported" || status === "reported" ? status : "unreported"}`)} />
        {tokenFact(t("logs.tokens.input"), usage?.inputTokens, locale)}
        {tokenFact(t("logs.tokens.cached"), usage?.cachedInputTokens, locale)}
        {tokenFact(t("logs.tokens.cacheRead"), usage?.cacheReadInputTokens, locale)}
        {tokenFact(t("logs.tokens.cacheWrite"), usage?.cacheCreationInputTokens, locale)}
        {tokenFact(t("logs.tokens.output"), usage?.outputTokens, locale)}
        {tokenFact(t("logs.tokens.reasoning"), usage?.reasoningOutputTokens, locale)}
        {tokenFact(t("logs.detail.totalTokens"), usage?.totalTokens, locale)}
      </dl>
    </section>
  );
}

function costKindLabel(t: TFn, kind: string): string {
  if (kind === "exact") return t("logs.detail.cost.exact");
  if (kind === "estimated") return t("logs.detail.cost.estimated");
  if (kind === "unavailable") return t("logs.detail.cost.unavailable");
  return kind;
}

function CostPriceFacts({ cost, t }: { cost: DiagnosticsCost; t: TFn }) {
  const price = cost.price;
  return (
    <>
      {cost.reason && <Fact label={t("logs.detail.unavailableReason")} value={cost.reason} mono />}
      {price?.source && <Fact label={t("logs.detail.priceSource")} value={price.source} mono />}
      {price?.provider && price.modelId && (
        <Fact label={t("logs.detail.matchedKey")} value={`${price.provider}/${price.modelId}`} mono />
      )}
      {price?.confidence && <Fact label={t("logs.detail.cost.confidence")} value={price.confidence} />}
    </>
  );
}

function CostSection({ cost, locale, t }: { cost: DiagnosticsCost | undefined; locale: string; t: TFn }) {
  const amount = formatDiagnosticsCost(cost, locale);
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.section.cost")}</h4>
      <dl className="diagnostics-facts">
        {amount && <Fact label={t("logs.detail.costTotal")} value={amount} mono />}
        <Fact label={t("logs.detail.cost.kind")} value={costKindLabel(t, cost?.kind ?? "unavailable")} />
        {cost && <CostPriceFacts cost={cost} t={t} />}
      </dl>
    </section>
  );
}

function AttemptsSection({ attempts, t }: { attempts: DiagnosticsAttempt[]; t: TFn }) {
  return (
    <section className="diagnostics-block">
      <h4>{t("logs.detail.section.attempts")}</h4>
      {attempts.length === 0 ? (
        <p className="diagnostics-empty-inline muted">{t("logs.detail.attempts.empty")}</p>
      ) : (
        <table className="diagnostics-attempts">
          <thead>
            <tr>
              <th className="num">#</th>
              <th>{t("logs.detail.attempt.target")}</th>
              <th>{t("logs.col.status")}</th>
              <th>{t("logs.detail.attempt.reason")}</th>
            </tr>
          </thead>
          <tbody>
            {attempts.map(attempt => (
              <tr key={`${attempt.ordinal}-${attempt.member ?? ""}`}>
                <td className="num mono">{attempt.ordinal}</td>
                <td className="mono">{attempt.member ?? "\u2014"}</td>
                <td className="mono">{attempt.status ?? "\u2014"}</td>
                <td className="mono">{attempt.decision ?? attempt.code ?? "\u2014"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

export function DiagnosticsDetail({
  t,
  locale,
  localeTag,
  serverTimeZone,
  detail,
  loading,
  error,
}: {
  t: TFn;
  locale: string;
  localeTag?: string;
  serverTimeZone?: string;
  detail: DiagnosticsRequestDetail | null;
  loading: boolean;
  error: string | null;
}) {
  const [copied, setCopied] = useState<string | null>(null);
  const onCopy = (id: string) => {
    void copyTextToClipboard(id).then(ok => {
      if (!ok) return;
      setCopied(id);
      window.setTimeout(() => setCopied(current => current === id ? null : current), 1200);
    });
  };
  if (!detail) {
    const empty = error === "request_not_retained"
      ? t("logs.detail.notRetained")
      : error
        ? t("logs.loadError")
        : loading
          ? t("logs.detail.loading")
          : t("logs.detail.empty");
    return (
      <section className="diagnostics-detail" aria-label={t("logs.detailTitle")}>
        <div className="diagnostics-detail-head">
          <h3>{t("logs.detailTitle")}</h3>
        </div>
        <p className="diagnostics-empty-inline muted">{empty}</p>
      </section>
    );
  }
  return (
    <section className="diagnostics-detail" aria-busy={loading} aria-label={t("logs.detailTitle")}>
      <div className="diagnostics-detail-head">
        <h3>{t("logs.detailTitle")}</h3>
        <span className="mono muted diagnostics-id-text" title={detail.requestId}>{detail.requestId}</span>
      </div>
      <BasicSection
        detail={detail}
        localeTag={localeTag}
        serverTimeZone={serverTimeZone}
        t={t}
        copied={copied}
        onCopy={onCopy}
      />
      <RoutingSection routing={detail.routing} t={t} />
      <PerformanceSection detail={detail} locale={locale} t={t} />
      <UsageSection usage={detail.usage} locale={locale} t={t} />
      <CostSection cost={detail.cost} locale={locale} t={t} />
      <AttemptsSection attempts={detail.attempts} t={t} />
    </section>
  );
}
