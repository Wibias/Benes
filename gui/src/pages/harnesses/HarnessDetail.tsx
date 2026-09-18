import { useState } from "react";
import {
  IconCheck,
  IconCopy,
  IconFile,
  IconFolder,
  IconRefresh,
  IconUndo,
} from "../../icons";
import { useT, type TKey } from "../../i18n/shared";
import { Switch } from "../../ui";
import { HARNESS_ICON } from "./icons";
import { authTokenKey, fileName, formatStamp, harnessHeaderAction, historyTone } from "./presentation";
import { ClaudeSettings } from "./claude-settings";
import { ClaudeDesktopSettings } from "./claude-desktop-settings";
import { SidecarPolicy } from "./SidecarPolicy";
import { harnessShowsSettings, type HarnessDetailTab } from "./hash";
import type {
  HarnessCapabilityId,
  HarnessId,
  HarnessModelRegistration,
  HarnessRecord,
  HarnessSettings,
} from "./types";

function noToast() {}

const CAP_LABEL: Record<HarnessCapabilityId, TKey> = {
  sendMessages: "harnesses.cap.sendMessages",
  toolCalls: "harnesses.cap.toolCalls",
  readFiles: "harnesses.cap.readFiles",
  writeFiles: "harnesses.cap.writeFiles",
  shell: "harnesses.cap.shell",
  browser: "harnesses.cap.browser",
  mcp: "harnesses.cap.mcp",
  sessions: "harnesses.cap.sessions",
};

const CAP_ORDER: HarnessCapabilityId[] = [
  "sendMessages",
  "toolCalls",
  "readFiles",
  "writeFiles",
  "shell",
  "browser",
  "mcp",
  "sessions",
];

const HISTORY_KIND: Record<HarnessRecord["history"][number]["kind"], TKey> = {
  apply: "harnesses.badge.applied",
  disable: "harnesses.history.disabled",
  refresh: "harnesses.actions.reapply",
  restore: "harnesses.history.restored",
};

const HISTORY_PREVIEW = 3;

export function HarnessMark({ id, compact }: { id: HarnessId; compact?: boolean }) {
  const icon = HARNESS_ICON[id];
  const classes = [
    "harnesses-mark",
    compact ? "is-compact" : "",
    icon.mono ? "is-mono" : "",
  ].filter(Boolean).join(" ");
  return (
    <span className={classes} data-id={id} aria-hidden="true">
      <img src={icon.src} alt="" />
    </span>
  );
}

export function HarnessDetail({
  harness,
  busy,
  tab = "overview",
  apiBase,
  onApply,
  onDisable,
  onRefresh,
  onRestore,
  onSetting,
  onCopy,
  onOpenPath,
  onTab,
  onToast,
  onSidecarRefetch,
}: {
  harness: HarnessRecord;
  busy: boolean;
  tab?: HarnessDetailTab;
  apiBase?: string;
  onApply: () => void;
  onDisable: () => void;
  onRefresh: () => void;
  onRestore: (opId: string) => void;
  onSetting: (patch: Partial<HarnessSettings>) => void;
  onCopy: (value: string) => void;
  onOpenPath: (target: "log" | "config", path: string) => void;
  onTab?: (tab: HarnessDetailTab) => void;
  onToast?: (ok: boolean, text: string) => void;
  onSidecarRefetch: () => Promise<void> | void;
}) {
  const t = useT();
  const empty = t("harnesses.none");
  const [historyOpen, setHistoryOpen] = useState(false);
  const latestRestore = harness.history.find((row) => row.restorable);
  const historyRows = historyOpen ? harness.history : harness.history.slice(0, HISTORY_PREVIEW);
  const showsSettings = harnessShowsSettings(harness.id);
  const settingsOpen = showsSettings && tab === "settings" && onTab != null && onToast != null;
  return (
    <article className="harnesses-detail">
      <HarnessDetailHead harness={harness} busy={busy} onApply={onApply} onDisable={onDisable} />
      {showsSettings && onTab && (
        <ClaudeDetailTabs tab={tab} onTab={onTab} />
      )}
      {settingsOpen ? (
        harness.id === "claude-desktop" ? (
          <ClaudeDesktopSettings apiBase={apiBase ?? ""} onCopy={onCopy} onToast={onToast} />
        ) : (
          <ClaudeSettings apiBase={apiBase ?? ""} onCopy={onCopy} onToast={onToast} />
        )
      ) : (
        <HarnessOverview
          harness={harness}
          busy={busy}
          empty={empty}
          latestRestore={latestRestore}
          historyRows={historyRows}
          historyOpen={historyOpen}
          onSetting={onSetting}
          onRefresh={onRefresh}
          onRestore={onRestore}
          onCopy={onCopy}
          onOpenPath={onOpenPath}
          onOpenHistory={() => setHistoryOpen(true)}
          apiBase={apiBase ?? ""}
          onSidecarRefetch={onSidecarRefetch}
          onToast={onToast ?? noToast}
        />
      )}
    </article>
  );
}

function ClaudeDetailTabs({
  tab,
  onTab,
}: {
  tab: HarnessDetailTab;
  onTab: (tab: HarnessDetailTab) => void;
}) {
  const t = useT();
  return (
    <div className="page-tabs harnesses-detail-tabs" role="tablist" aria-label={t("harnesses.tabsLabel")}>
      {(["overview", "settings"] as const).map((id) => {
        const selected = tab === id;
        return (
          <button
            key={id}
            type="button"
            role="tab"
            aria-selected={selected}
            tabIndex={selected ? 0 : -1}
            className={`page-tab${selected ? " page-tab--active" : ""}`}
            onClick={() => onTab(id)}
          >
            {t(id === "overview" ? "harnesses.tab.overview" : "harnesses.tab.settings")}
          </button>
        );
      })}
    </div>
  );
}

function HarnessOverview({
  harness,
  busy,
  empty,
  latestRestore,
  historyRows,
  historyOpen,
  onSetting,
  onRefresh,
  onRestore,
  onCopy,
  onOpenPath,
  onOpenHistory,
  apiBase,
  onSidecarRefetch,
  onToast,
}: {
  harness: HarnessRecord;
  busy: boolean;
  empty: string;
  latestRestore: HarnessRecord["history"][number] | undefined;
  historyRows: HarnessRecord["history"];
  historyOpen: boolean;
  onSetting: (patch: Partial<HarnessSettings>) => void;
  onRefresh: () => void;
  onRestore: (opId: string) => void;
  onCopy: (value: string) => void;
  onOpenPath: (target: "log" | "config", path: string) => void;
  onOpenHistory: () => void;
  apiBase: string;
  onSidecarRefetch: () => Promise<void> | void;
  onToast: (ok: boolean, text: string) => void;
}) {
  return (
    <>
      <HarnessFacts harness={harness} empty={empty} onCopy={onCopy} />
      {harness.registration && (
        <HarnessRegistration registration={harness.registration} />
      )}
      <HarnessCaps harness={harness} />
      <div className="harnesses-work">
        <HarnessConfig harness={harness} onSetting={onSetting} />
        <HarnessActions
          harness={harness}
          busy={busy}
          empty={empty}
          latestRestore={latestRestore}
          onRefresh={onRefresh}
          onRestore={onRestore}
          onCopy={onCopy}
          onOpenPath={onOpenPath}
        />
      </div>
      <SidecarPolicy
        harness={harness}
        apiBase={apiBase}
        busy={busy}
        onRefetch={onSidecarRefetch}
        onToast={onToast}
      />
      <HarnessHistory
        harness={harness}
        empty={empty}
        busy={busy}
        historyRows={historyRows}
        historyOpen={historyOpen}
        onOpen={onOpenHistory}
        onRestore={onRestore}
        onCopy={onCopy}
      />
    </>
  );
}

function HarnessDetailHead({
  harness,
  busy,
  onApply,
  onDisable,
}: {
  harness: HarnessRecord;
  busy: boolean;
  onApply: () => void;
  onDisable: () => void;
}) {
  const t = useT();
  const action = harnessHeaderAction(harness);
  return (
    <header className="harnesses-detail-head">
      <div className="harnesses-detail-identity">
        <HarnessMark id={harness.id} />
        <div>
          <div className="harnesses-detail-title">
            <h3>{t(harness.nameKey)}</h3>
          </div>
          {harness.drift && (
            <p className="harnesses-drift">
              <span className="dot dot-amber" />
              {t("harnesses.drift")}
            </p>
          )}
        </div>
      </div>
      <div className="page-head-actions">
        {action.kind === "disable" && (
          <button type="button" className="btn btn-ghost harnesses-disable" onClick={onDisable} disabled={!action.enabled || busy}>
            {t("harnesses.disable")}
          </button>
        )}
        {action.kind === "apply" && (
          <button type="button" className="btn harnesses-connect" onClick={onApply} disabled={!action.enabled || busy}>
            {t("harnesses.apply")}
          </button>
        )}
      </div>
    </header>
  );
}

function HarnessFacts({ harness, empty, onCopy }: { harness: HarnessRecord; empty: string; onCopy: (value: string) => void }) {
  const t = useT();
  return (
    <div className="harnesses-facts">
      <HarnessStatusChips harness={harness} empty={empty} />
      <section className="harnesses-fact">
        <h4>{t("harnesses.detect.title")}</h4>
        <dl>
          <Fact label={t("harnesses.detect.method")} value={t(harness.detectMethod === "auto" ? "harnesses.detect.auto" : "harnesses.detect.manual")} />
          <Fact label={t("harnesses.detect.path")} value={harness.detectPath ?? empty} mono />
          <Fact label={t("harnesses.detect.config")} value={harness.configPath ?? empty} mono />
          <Fact
            label={t("harnesses.detect.snapshot")}
            value={harness.snapshotId ?? empty}
            mono
            action={harness.snapshotId ? () => onCopy(harness.snapshotId ?? "") : undefined}
            actionLabel={t("harnesses.copy")}
          />
        </dl>
      </section>
      <HarnessAuthFacts harness={harness} empty={empty} />
    </div>
  );
}

function statusStateChip(harness: HarnessRecord): {
  key: "harnesses.badge.notInstalled" | "harnesses.badge.notApplied" | "harnesses.badge.applied" | "harnesses.badge.conflict" | "harnesses.badge.update";
  dot: string;
} {
  if (!harness.installed) return { key: "harnesses.badge.notInstalled", dot: "dot-muted" };
  if (harness.issue === "conflict") return { key: "harnesses.badge.conflict", dot: "dot-red" };
  if (harness.issue === "update-needed") return { key: "harnesses.badge.update", dot: "dot-amber" };
  if (harness.applied) return { key: "harnesses.badge.applied", dot: "dot-green" };
  return { key: "harnesses.badge.notApplied", dot: "dot-muted" };
}

function HarnessStatusChips({ harness, empty }: { harness: HarnessRecord; empty: string }) {
  const t = useT();
  const state = statusStateChip(harness);
  return (
    <section className="harnesses-fact">
      <h4>{t("harnesses.status.title")}</h4>
      <div className="harnesses-chips">
        {harness.running === true && (
          <div className="harnesses-chip"><span className="dot dot-green" />{t("harnesses.status.runningOn")}</div>
        )}
        <div className="harnesses-chip">
          <span className={`dot ${state.dot}`} />
          {t(state.key)}
        </div>
      </div>
      <dl>
        <Fact label={t("harnesses.status.lastDetected")} value={formatStamp(harness.lastDetectedAt, empty)} muted />
        <Fact label={t("harnesses.status.lastApplied")} value={formatStamp(harness.lastAppliedAt, empty)} muted />
      </dl>
    </section>
  );
}

function tokenDotClass(token: HarnessRecord["auth"]["token"]): string {
  if (token === "valid") return "dot-green";
  if (token === "expired") return "dot-amber";
  return "";
}

function HarnessAuthFacts({ harness, empty }: { harness: HarnessRecord; empty: string }) {
  const t = useT();
  const tokenClass = tokenDotClass(harness.auth.token);
  return (
    <section className="harnesses-fact">
      <h4>{t("harnesses.auth.title")}</h4>
      <dl>
        <Fact label={t("harnesses.auth.method")} value={t(harness.auth.methodKey)} />
        <Fact label={t("harnesses.auth.clientId")} value={harness.auth.clientId ?? empty} mono />
        <Fact label={t("harnesses.auth.scopes")} value={harness.auth.scopes.length ? harness.auth.scopes.join(", ") : empty} />
        <div className="harnesses-fact-row">
          <dt>{t("harnesses.auth.token")}</dt>
          <dd>
            <span className={`dot${tokenClass ? ` ${tokenClass}` : ""}`} />
            {t(authTokenKey(harness.auth.token))}
          </dd>
        </div>
      </dl>
    </section>
  );
}

/**
 * How much of the Benes catalogue this Harness's own config carries, when the listener
 * publishes that summary. It is read-only by design: the catalogue decides model membership,
 * and re-applying the Harness is what writes a newer projection.
 */
function HarnessRegistration({ registration }: { registration: HarnessModelRegistration }) {
  const t = useT();
  const stateKey = registration.current
    ? "harnesses.registration.current"
    : registration.present
      ? "harnesses.registration.stale"
      : "harnesses.registration.absent";
  // When the listener could not write the projection it says why, and that exact reason is what
  // the operator needs; the generic copy only covers the states with no failure to report.
  const stateText = registration.current
    ? t(stateKey)
    : registration.lastAutomaticError ?? t(stateKey);
  return (
    <section className="harnesses-registration">
      <h4>{t("harnesses.registration.title")}</h4>
      <p className="harnesses-registration-count">
        {t("harnesses.registration.count", {
          registered: registration.registered,
          catalogue: registration.catalogue,
        })}
      </p>
      <p className={`harnesses-registration-state${registration.current ? "" : " is-stale"}`}>
        <span className={`dot ${registration.current ? "dot-green" : "dot-amber"}`} />
        {stateText}
      </p>
    </section>
  );
}

function HarnessCaps({ harness }: { harness: HarnessRecord }) {
  const t = useT();
  return (
    <section className="harnesses-caps">
      <h4>{t("harnesses.caps.title")}</h4>
      <ul>
        {CAP_ORDER.map((id) => (
          <li key={id} className={`harnesses-cap${harness.capabilities[id] ? "" : " is-off"}`}>
            <span className="harnesses-cap-check"><IconCheck /></span>
            {t(CAP_LABEL[id])}
          </li>
        ))}
      </ul>
    </section>
  );
}

function HarnessConfig({ harness, onSetting }: { harness: HarnessRecord; onSetting: (patch: Partial<HarnessSettings>) => void }) {
  const t = useT();
  const name = t(harness.nameKey);
  return (
    <section>
      <h4>{t("harnesses.config.title")}</h4>
      <SettingRow
        label={t("harnesses.config.autoDetect")}
        hint={t("harnesses.config.autoDetectHint", { name })}
        on={harness.settings.autoDetect}
        onToggle={() => onSetting({ autoDetect: !harness.settings.autoDetect })}
      />
      <SettingRow
        label={t("harnesses.config.autoApply")}
        hint={t("harnesses.config.autoApplyHint", { name })}
        on={harness.settings.autoApply}
        onToggle={() => onSetting({ autoApply: !harness.settings.autoApply })}
      />
      <SettingRow
        label={t("harnesses.config.retainSnapshot")}
        hint={t("harnesses.config.retainSnapshotHint")}
        on={harness.settings.retainSnapshot}
        onToggle={() => onSetting({ retainSnapshot: !harness.settings.retainSnapshot })}
      />
      <SettingRow
        label={t("harnesses.config.allowRestart")}
        hint={t("harnesses.config.allowRestartHint", { name })}
        on={harness.settings.allowRestart}
        onToggle={() => onSetting({ allowRestart: !harness.settings.allowRestart })}
      />
    </section>
  );
}

function HarnessActions({
  harness,
  busy,
  empty,
  latestRestore,
  onRefresh,
  onRestore,
  onCopy,
  onOpenPath,
}: {
  harness: HarnessRecord;
  busy: boolean;
  empty: string;
  latestRestore: HarnessRecord["history"][number] | undefined;
  onRefresh: () => void;
  onRestore: (opId: string) => void;
  onCopy: (value: string) => void;
  onOpenPath: (target: "log" | "config", path: string) => void;
}) {
  const t = useT();
  const name = t(harness.nameKey);
  const openOrCopy = (event: { shiftKey: boolean }, target: "log" | "config", path: string | null) => {
    if (!path) return;
    if (event.shiftKey) {
      onCopy(path);
      return;
    }
    onOpenPath(target, path);
  };
  return (
    <section>
      <h4>{t("harnesses.actions.title")}</h4>
      <div className="harnesses-actions">
        <button type="button" className="harnesses-action" onClick={onRefresh} disabled={!harness.applied || busy}>
          <IconRefresh />
          <span className="harnesses-action-copy">
            <span>{t("harnesses.actions.reapply")}</span>
            <span className="harnesses-action-hint">{t("harnesses.actions.reapplyHint", { name })}</span>
          </span>
        </button>
        <button
          type="button"
          className="harnesses-action"
          disabled={!latestRestore || busy}
          onClick={() => latestRestore && onRestore(latestRestore.id)}
        >
          <IconUndo />
          <span className="harnesses-action-copy">
            <span>{t("harnesses.actions.restore")}</span>
            <span className="harnesses-action-hint">{t("harnesses.actions.restoreHint", { name })}</span>
          </span>
        </button>
        <button
          type="button"
          className="harnesses-action"
          disabled={!harness.configPath}
          title={t("harnesses.actions.copyPathHint")}
          onClick={(event) => openOrCopy(event, "config", harness.configPath)}
        >
          <IconFile />
          <span className="harnesses-action-copy">
            <span>{t("harnesses.actions.viewConfig")}</span>
            <span className="harnesses-action-hint">
              {harness.configPath ? t("harnesses.actions.viewConfigFile", { file: fileName(harness.configPath) }) : empty}
            </span>
          </span>
        </button>
        <button
          type="button"
          className="harnesses-action"
          disabled={!harness.logPath}
          title={t("harnesses.actions.copyPathHint")}
          onClick={(event) => openOrCopy(event, "log", harness.logPath)}
        >
          <IconFolder />
          <span className="harnesses-action-copy">
            <span>{t("harnesses.actions.openLogs")}</span>
            <span className="harnesses-action-hint">{harness.logPath ?? empty}</span>
          </span>
        </button>
      </div>
    </section>
  );
}

function HarnessHistory({
  harness,
  empty,
  busy,
  historyRows,
  historyOpen,
  onOpen,
  onRestore,
  onCopy,
}: {
  harness: HarnessRecord;
  empty: string;
  busy: boolean;
  historyRows: HarnessRecord["history"];
  historyOpen: boolean;
  onOpen: () => void;
  onRestore: (opId: string) => void;
  onCopy: (value: string) => void;
}) {
  const t = useT();
  if (harness.history.length === 0) {
    return (
      <section className="harnesses-history">
        <h4>{t("harnesses.history.title")}</h4>
        <p className="page-sub">{t("harnesses.history.empty")}</p>
      </section>
    );
  }
  return (
    <section className="harnesses-history">
      <h4>{t("harnesses.history.title")}</h4>
      <table>
        <thead>
          <tr>
            <th>{t("harnesses.history.state")}</th>
            <th>{t("harnesses.history.snapshot")}</th>
            <th>{t("harnesses.history.applied")}</th>
            <th>{t("harnesses.history.note")}</th>
            <th>{t("harnesses.history.actions")}</th>
          </tr>
        </thead>
        <tbody>
          {historyRows.map((row) => (
            <tr key={row.id}>
              <td>
                <span className={`harnesses-pill harnesses-pill--${historyTone(row.kind)}`}>
                  {t(HISTORY_KIND[row.kind])}
                </span>
              </td>
              <td><code>{row.snapshot ?? empty}</code></td>
              <td>{formatStamp(row.at, empty)}</td>
              <td>{t(row.noteKey)}</td>
              <td>
                <div className="harnesses-history-ops">
                  {row.restorable && (
                    <button
                      type="button"
                      className="btn btn-ghost btn-icon"
                      onClick={() => onRestore(row.id)}
                      disabled={busy}
                      aria-label={t("harnesses.history.undo")}
                      title={t("harnesses.history.undo")}
                    >
                      <IconUndo />
                    </button>
                  )}
                  {row.snapshot && (
                    <button
                      type="button"
                      className="btn btn-ghost btn-icon"
                      onClick={() => onCopy(row.snapshot ?? "")}
                      aria-label={t("harnesses.copy")}
                      title={t("harnesses.copy")}
                    >
                      <IconCopy />
                    </button>
                  )}
                </div>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
      {harness.history.length > HISTORY_PREVIEW && !historyOpen && (
        <p className="harnesses-history-more">
          <button type="button" onClick={onOpen}>{t("harnesses.history.viewAll")}</button>
        </p>
      )}
    </section>
  );
}

function Fact({
  label,
  value,
  mono,
  muted,
  action,
  actionLabel,
}: {
  label: string;
  value: string;
  mono?: boolean;
  muted?: boolean;
  action?: () => void;
  actionLabel?: string;
}) {
  return (
    <div className="harnesses-fact-row">
      <dt>{label}</dt>
      <dd className={[mono ? "mono" : "", muted ? "is-muted" : ""].filter(Boolean).join(" ") || undefined}>
        {value}
        {action && (
          <button type="button" className="btn btn-ghost btn-icon" onClick={action} aria-label={actionLabel}>
            <IconCopy />
          </button>
        )}
      </dd>
    </div>
  );
}

function SettingRow({
  label,
  hint,
  on,
  onToggle,
}: {
  label: string;
  hint: string;
  on: boolean;
  onToggle: () => void;
}) {
  return (
    <div className="harnesses-setting">
      <div className="harnesses-setting-copy">
        <span className="harnesses-setting-title">{label}</span>
        <span className="harnesses-setting-hint">{hint}</span>
      </div>
      <Switch on={on} onClick={onToggle} label={label} />
    </div>
  );
}
