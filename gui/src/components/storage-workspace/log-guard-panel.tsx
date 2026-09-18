/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useState } from "react";
import { formatBytes } from "../../format-bytes";
import {
  logGuardLabel,
  logGuardOperationLabel,
  logGuardProtectionModeLabel,
  logGuardProtectionStateLabel,
  logGuardSchemaStateLabel,
} from "../../i18n/log-guard";
import type { Locale, TFn } from "../../i18n/shared";
import type { CodexLogGuardAction, CodexLogGuardReport } from "./types";

/** One label/value line of a log-guard fact list. */
type GuardFact = {
  readonly id: string;
  readonly label: React.ReactNode;
  readonly value: React.ReactNode;
};

/** One protection control: the request it sends, and the copy it shows. */
type ProtectionControl = {
  readonly testId: string;
  readonly label: string;
  readonly action: CodexLogGuardAction;
  /** Set for the two mode buttons, which read as pressed while their mode is the desired one. */
  readonly pressed?: boolean;
  readonly disabled: boolean;
};

/** The fact list every guard block renders, so one renderer owns the markup. */
function GuardFacts({ facts }: { facts: GuardFact[] }) {
  return (
    <dl className="stw-kv">
      {facts.map(fact => (
        <div key={fact.id} className="stw-kv-row">
          <dt>{fact.label}</dt>
          <dd className="stw-kv-mono">{fact.value}</dd>
        </div>
      ))}
    </dl>
  );
}

/** One block of the guard panel: a named section with optional heading and test id. */
function GuardSection({
  testId,
  title,
  children,
}: {
  testId?: string;
  title?: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <div className="stw-section" data-testid={testId}>
      {title !== undefined && <h4 className="stw-section-title">{title}</h4>}
      {children}
    </div>
  );
}

/** A refusal or failure the guard reported. */
function GuardError({ message }: { message: string | null }) {
  if (message === null) return null;
  return <p className="err" role="alert">{message}</p>;
}

/** What the last compaction reclaimed, once the listener has reported it. */
function CompactionResult({ compaction }: { compaction: string | null }) {
  if (compaction === null) return null;
  return (
    <GuardSection>
      <p className="stw-kv-mono" role="status" data-testid="log-guard-compact-result">{compaction}</p>
    </GuardSection>
  );
}

/**
 * What the guard reports about the database.
 *
 * The rows are built in the order they read: what the schema is, how much space the database and
 * its sidecars take, what inspection found, and where sqlite actually lives. The counters that
 * only the database detail can vouch for are left out of the embedded panel, which is the Logs DB
 * bucket's own page and says those things elsewhere.
 */
function LogGuardMetrics({
  report, locale, t, embedded,
}: {
  report: CodexLogGuardReport;
  locale: Locale;
  t: TFn;
  embedded?: boolean;
}) {
  const metrics = report.metrics;
  const capabilities = [report.capabilities.protection, report.capabilities.reclaim];
  const inspectOnly = capabilities.some(capability => capability.state === "unsupported");
  const inspectionOnlyNote = inspectOnly && report.schema.state === "unsupported"
    ? [
      <span key="sep" aria-hidden="true"> · </span>,
      <code key="only">{logGuardLabel(locale, "inspectionOnly")}</code>,
    ]
    : [];
  const sqliteHomeCopy = report.externalSqliteHome
    ? logGuardLabel(locale, "externalSqliteHome")
    : "CODEX_HOME";
  const countedFacts: GuardFact[] = metrics === null ? [] : [
    ...(embedded
      ? []
      : [{ id: "rows", label: t("storage.col.rows"), value: metrics.totalRows.toLocaleString(locale) }]),
    { id: "trace", label: <code>TRACE</code>, value: `${(metrics.traceShare * 100).toFixed(1)}%` },
    ...(embedded
      ? []
      : [{ id: "freelist", label: <code>freelist</code>, value: formatBytes(metrics.reclaimableBytes, locale) }]),
  ];
  const facts: GuardFact[] = [
    {
      id: "schema",
      label: t("dash.status"),
      value: [
        <code key="state">{logGuardSchemaStateLabel(locale, report.schema.state)}</code>,
        ...inspectionOnlyNote,
      ],
    },
    ...(embedded
      ? []
      : [{
        id: "db",
        label: t("storage.bucket.logs_db"),
        value: formatBytes(report.files.databaseBytes, locale),
      }]),
    { id: "wal", label: <code>WAL</code>, value: formatBytes(report.files.walBytes, locale) },
    { id: "shm", label: <code>SHM</code>, value: formatBytes(report.files.shmBytes, locale) },
    ...countedFacts,
    { id: "home", label: <code>sqlite_home</code>, value: <code>{sqliteHomeCopy}</code> },
  ];
  return <GuardFacts facts={facts} />;
}

/** One protection button: ghost for a mode switch, primary for the repair. */
function ProtectionButton({
  control,
  primary,
  onAction,
}: {
  control: ProtectionControl;
  primary?: boolean;
  onAction: (action: CodexLogGuardAction) => void;
}) {
  return (
    <button
      type="button"
      className={primary ? "btn btn-sm" : "btn btn-ghost btn-sm"}
      data-testid={control.testId}
      disabled={control.disabled}
      aria-pressed={control.pressed}
      onClick={() => onAction(control.action)}
    >
      {control.label}
    </button>
  );
}

/**
 * How the guard is set up, and what can be changed about it.
 *
 * Every control is disabled while a mutation is in flight, and the repair is offered only once the
 * observed mode has actually drifted: there is nothing to repair while the two modes agree.
 */
function LogGuardProtection({
  report, locale, t, busy, error, onAction,
}: {
  report: CodexLogGuardReport;
  locale: Locale;
  t: TFn;
  busy: boolean;
  error: string | null;
  onAction: (action: CodexLogGuardAction) => void;
}) {
  const protection = report.protection;
  if (!protection) return null;
  const mutationDisabled = busy || report.capabilities.protection.state !== "supported";
  const applyingCopy = busy ? logGuardOperationLabel(locale, "applying") : null;
  const modes: ProtectionControl[] = [
    {
      testId: "log-guard-protect-compat",
      label: logGuardLabel(locale, "compat"),
      action: { action: "protect", mode: "compat" },
      pressed: protection.desiredMode === "compat",
      disabled: mutationDisabled,
    },
    {
      testId: "log-guard-protect-quiet",
      label: logGuardLabel(locale, "quiet"),
      action: { action: "protect", mode: "quiet" },
      pressed: protection.desiredMode === "quiet",
      disabled: mutationDisabled,
    },
    {
      testId: "log-guard-unprotect",
      label: logGuardLabel(locale, "disable"),
      action: { action: "unprotect" },
      disabled: mutationDisabled || protection.desiredMode === "off",
    },
  ];
  const repair: ProtectionControl = {
    testId: "log-guard-repair",
    label: logGuardLabel(locale, "repair"),
    action: { action: "repair" },
    disabled: mutationDisabled,
  };
  const facts: GuardFact[] = [
    {
      id: "configured",
      label: t("storage.logGuard.configured"),
      value: logGuardProtectionModeLabel(locale, protection.desiredMode),
    },
    {
      id: "observed",
      label: t("storage.logGuard.observed"),
      value: logGuardProtectionModeLabel(locale, protection.observedMode),
    },
    {
      id: "state",
      label: t("storage.logGuard.state"),
      value: logGuardProtectionStateLabel(locale, protection.state),
    },
  ];
  return (
    <GuardSection testId="log-guard-protection" title={logGuardLabel(locale, "protection")}>
      <GuardFacts facts={facts} />
      <div className="storage-policy-actions">
        {modes.map(control => (
          <ProtectionButton key={control.testId} control={control} onAction={onAction} />
        ))}
        {protection.state === "drifted" && (
          <ProtectionButton control={repair} primary onAction={onAction} />
        )}
        {applyingCopy !== null && <span className="muted" role="status">{applyingCopy}</span>}
      </div>
      <GuardError message={error} />
    </GuardSection>
  );
}

/** The two-step compact control, whose arming the section that offers it still owns. */
function CompactControls({
  locale,
  busy,
  armed,
  onArm,
  onCancel,
  onCompact,
}: {
  locale: Locale;
  busy: boolean;
  armed: boolean;
  onArm: () => void;
  onCancel: () => void;
  onCompact: () => void;
}) {
  if (!armed) {
    return (
      <button
        type="button"
        className="btn btn-ghost btn-sm"
        data-testid="log-guard-compact"
        disabled={busy}
        onClick={onArm}
      >
        {logGuardLabel(locale, "compact")}
      </button>
    );
  }
  return (
    <>
      <button
        type="button"
        className="btn btn-sm"
        data-testid="log-guard-compact-confirm"
        disabled={busy}
        onClick={onCompact}
      >
        {logGuardLabel(locale, "confirmCompact")}
      </button>
      <button type="button" className="btn btn-ghost btn-sm" disabled={busy} onClick={onCancel}>
        {logGuardLabel(locale, "cancel")}
      </button>
    </>
  );
}

/** Space the database could give back, offered only while there is some and reclaim is supported. */
function LogGuardReclaim({
  report, locale, busy, onAction,
}: {
  report: CodexLogGuardReport;
  locale: Locale;
  busy: boolean;
  onAction: (action: CodexLogGuardAction) => void;
}) {
  const metrics = report.metrics;
  const protection = report.protection;
  const reclaimableBytes = metrics?.reclaimableBytes ?? 0;
  const reclaimAvailable = protection !== undefined
    && report.capabilities.reclaim.state === "supported"
    && reclaimableBytes > 0;
  const [confirmCompact, setConfirmCompact] = useState(false);
  if (!reclaimAvailable) return null;
  return (
    <GuardSection testId="log-guard-reclaim">
      <div className="storage-policy-actions">
        <CompactControls
          locale={locale}
          busy={busy}
          armed={confirmCompact}
          onArm={() => setConfirmCompact(true)}
          onCancel={() => setConfirmCompact(false)}
          onCompact={() => {
            setConfirmCompact(false);
            onAction({ action: "compact" });
          }}
        />
      </div>
    </GuardSection>
  );
}

/** The five targets the trace rows sit under most often. */
function LogGuardTopTargets({
  metrics,
  locale,
}: {
  metrics: NonNullable<CodexLogGuardReport["metrics"]>;
  locale: Locale;
}) {
  const rows = metrics.topTargets.slice(0, 5).map(entry => ({
    path: entry.target,
    rows: entry.rows.toLocaleString(locale),
  }));
  if (rows.length === 0) return null;
  return (
    <GuardSection title={<code>target</code>}>
      {rows.map(row => (
        <div key={row.path} className="stw-file-row">
          <span className="stw-file-path" title={row.path}><code>{row.path}</code></span>
          <span className="stw-file-size">{row.rows}</span>
        </div>
      ))}
    </GuardSection>
  );
}

export function CodexLogGuardPanel({
  report, locale, t, busy, error, compaction, onAction, embedded = false,
}: {
  report: CodexLogGuardReport;
  locale: Locale;
  t: TFn;
  busy: boolean;
  error: string | null;
  compaction: string | null;
  onAction: (action: CodexLogGuardAction) => void;
  embedded?: boolean;
}) {
  const metrics = report.metrics;
  const snapshotHint = `immutable=1 · snapshot=${report.snapshot}`;
  return (
    <div className="stw-section" data-testid="codex-log-guard">
      {!embedded && <h3 className="stw-section-title">{t("storage.bucket.logs_db")}</h3>}
      <LogGuardMetrics report={report} locale={locale} t={t} embedded={embedded} />
      <LogGuardProtection report={report} locale={locale} t={t} busy={busy} error={error} onAction={onAction} />
      <LogGuardReclaim report={report} locale={locale} busy={busy} onAction={onAction} />
      <CompactionResult compaction={compaction} />
      {metrics && <LogGuardTopTargets metrics={metrics} locale={locale} />}
      <p className="stw-hint stw-hint--quiet"><code>{snapshotHint}</code></p>
    </div>
  );
}

export function CodexLogGuardUnavailablePanel({
  locale, t, embedded = false,
}: {
  locale: Locale;
  t: TFn;
  embedded?: boolean;
}) {
  return (
    <div className="stw-section" data-testid="codex-log-guard-unavailable">
      {!embedded && <h3 className="stw-section-title">{t("storage.bucket.logs_db")}</h3>}
      <p className="stw-hint">{logGuardLabel(locale, "inspectionUnavailable")}</p>
    </div>
  );
}