/**
 * The Claude Desktop settings surface on the canonical Harnesses board.
 *
 * Harnesses is the only Claude Desktop surface: this component consumes the
 * runtime contract #255 / merged #328 landed and adds no second lifecycle. It
 * never treats a saved preference as applied native state, never reports
 * success the runtime has not confirmed, and offers no model, family, endpoint,
 * or credential control because Claude Desktop's supported local configuration
 * has no such surface.
 */
import { useCallback, useEffect, useId, useRef, useState, type ReactNode } from "react";
import { IconCopy, IconRefresh } from "../../icons";
import { useT, type TFn, type TKey } from "../../i18n/shared";
import { Notice, Switch } from "../../ui";
import { describeRefusal } from "../integrations/refusal-copy";
import {
  claudeDesktopActions,
  claudeDesktopBlocked,
  claudeDesktopBlockedKey,
  claudeDesktopDesire,
  claudeDesktopDesireKey,
  claudeDesktopStateKey,
  claudeDesktopTone,
  type ClaudeDesktopMutation,
  type ClaudeDesktopRefusal,
  type ClaudeDesktopStatus,
  type ClaudeDesktopTone,
} from "./claude-desktop-state";
import {
  readClaudeDesktopStatus,
  runClaudeDesktopLifecycle,
  saveClaudeDesktopDesired,
} from "./claude-desktop-io";

type ToastFn = (ok: boolean, text: string) => void;

/** Tone is a secondary cue. The sentence beside it is what states the meaning. */
const TONE_DOT: Record<ClaudeDesktopTone, string> = {
  ok: "dot-green",
  attention: "dot-amber",
  pending: "dot-muted",
  blocked: "dot-red",
};

function say(t: TFn, value: boolean, whenTrue: TKey, whenFalse: TKey): string {
  return t(value ? whenTrue : whenFalse);
}

/**
 * Loads the canonical status and owns the two writes the contract exposes.
 *
 * Nothing here reports success on its own: a lifecycle mutation re-reads the
 * status first, and only that re-read decides what the surface may claim.
 */
type LoadResult =
  | { ok: true; status: ClaudeDesktopStatus }
  | { ok: false; message: string };

/** One read of the canonical status, reported without touching React state. */
async function loadStatus(apiBase: string, signal: AbortSignal, t: TFn): Promise<LoadResult> {
  try {
    return { ok: true, status: await readClaudeDesktopStatus(apiBase, signal) };
  } catch (error) {
    return { ok: false, message: describeRefusal(t, error, t("harnesses.claudeDesktop.loadFailed")) };
  }
}

/**
 * Loads the canonical status and owns the two writes the contract exposes.
 *
 * Nothing here reports success on its own: a lifecycle mutation re-reads the
 * status first, and only that re-read decides what the surface may claim. State
 * is only applied after an await, so mounting never cascades a render.
 */
function useClaudeDesktopRuntime(apiBase: string, onToast: ToastFn) {
  const t = useT();
  const [status, setStatus] = useState<ClaudeDesktopStatus | null>(null);
  const [loading, setLoading] = useState(true);
  const [loadError, setLoadError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [restartPending, setRestartPending] = useState(false);
  const generation = useRef(0);
  const inflight = useRef<AbortController | null>(null);
  const writing = useRef(false);

  useEffect(() => {
    const ac = new AbortController();
    inflight.current = ac;
    const gen = generation.current + 1;
    generation.current = gen;
    void (async () => {
      const result = await loadStatus(apiBase, ac.signal, t);
      if (generation.current !== gen || ac.signal.aborted) return;
      if (result.ok) {
        setStatus(result.status);
        setLoadError(null);
      } else {
        setLoadError(result.message);
      }
      setLoading(false);
    })();
    return () => {
      generation.current += 1;
      ac.abort();
    };
  }, [apiBase, t]);

  const reload = useCallback(() => {
    inflight.current?.abort();
    const ac = new AbortController();
    inflight.current = ac;
    const gen = generation.current + 1;
    generation.current = gen;
    setLoading(true);
    void (async () => {
      const result = await loadStatus(apiBase, ac.signal, t);
      if (generation.current !== gen || ac.signal.aborted) return;
      if (result.ok) {
        setStatus(result.status);
        setLoadError(null);
      } else {
        setLoadError(result.message);
      }
      setLoading(false);
    })();
  }, [apiBase, t]);

  /** A refusal is reported as itself, and the surface reconciles with the runtime. */
  const refuse = useCallback((error: unknown) => {
    onToast(false, describeRefusal(t, error, t("harnesses.actionFailed")));
    reload();
  }, [onToast, reload, t]);

  const setDesired = useCallback(async (enabled: boolean) => {
    if (writing.current) return;
    writing.current = true;
    setBusy(true);
    try {
      setStatus(await saveClaudeDesktopDesired(apiBase, enabled, inflight.current?.signal));
      onToast(true, t("harnesses.settingsSaved"));
    } catch (error) {
      refuse(error);
    } finally {
      writing.current = false;
      setBusy(false);
    }
  }, [apiBase, onToast, refuse, t]);

  const mutate = useCallback(async (mutation: ClaudeDesktopMutation) => {
    if (writing.current) return;
    writing.current = true;
    setBusy(true);
    try {
      const outcome = await runClaudeDesktopLifecycle(apiBase, mutation, inflight.current?.signal);
      setStatus(outcome.status);
      setRestartPending(outcome.mutation.restartRequired);
      if (outcome.confirmed) {
        onToast(true, t(mutation === "apply" ? "harnesses.claudeDesktop.applied" : "harnesses.claudeDesktop.disabled"));
      } else {
        onToast(false, t(claudeDesktopStateKey(outcome.status.state)));
      }
    } catch (error) {
      refuse(error);
    } finally {
      writing.current = false;
      setBusy(false);
    }
  }, [apiBase, onToast, refuse, t]);

  return { status, loading, loadError, busy, restartPending, reload, setDesired, mutate };
}
export function ClaudeDesktopSettings({
  apiBase,
  onCopy,
  onToast,
}: {
  apiBase: string;
  onCopy: (value: string) => void;
  onToast: ToastFn;
}) {
  const t = useT();
  const runtime = useClaudeDesktopRuntime(apiBase, onToast);

  if (runtime.loading && !runtime.status) return <p className="page-sub">{t("common.loading")}</p>;
  if (!runtime.status) {
    return (
      <div className="harnesses-claude-status">
        <p className="page-sub">{runtime.loadError ?? t("harnesses.claudeDesktop.loadFailed")}</p>
        <button type="button" className="btn btn-ghost btn-sm" onClick={runtime.reload}>
          {t("harnesses.claudeDesktop.retry")}
        </button>
      </div>
    );
  }

  const status = runtime.status;
  return (
    <div className="harnesses-claude-settings harnesses-desktop-settings">
      <ClaudeDesktopStatusSection
        status={status}
        busy={runtime.busy}
        restartRequired={status.restartRequired || runtime.restartPending}
        onCopy={onCopy}
        onReload={runtime.reload}
      />
      {status.refusal && <ClaudeDesktopRefusalSection refusal={status.refusal} />}
      <ClaudeDesktopDesiredSection
        status={status}
        busy={runtime.busy}
        onDesired={(enabled) => { void runtime.setDesired(enabled); }}
      />
      <ClaudeDesktopActionsSection
        status={status}
        busy={runtime.busy}
        onMutate={(mutation) => { void runtime.mutate(mutation); }}
      />
    </div>
  );
}

function ClaudeDesktopStatusSection({
  status,
  busy,
  restartRequired,
  onCopy,
  onReload,
}: {
  status: ClaudeDesktopStatus;
  busy: boolean;
  restartRequired: boolean;
  onCopy: (value: string) => void;
  onReload: () => void;
}) {
  const t = useT();
  return (
    <section className="harnesses-claude-section">
      <div className="harnesses-claude-section-head">
        <h4>{t("harnesses.claudeDesktop.status.title")}</h4>
        <button type="button" className="btn btn-ghost btn-sm" onClick={onReload} disabled={busy}>
          <IconRefresh />
          {t("harnesses.claudeDesktop.status.reload")}
        </button>
      </div>
      <p className="harnesses-claude-lede harnesses-desktop-state" aria-live="polite">
        <span className={"dot " + TONE_DOT[claudeDesktopTone(status)]} />
        {t(claudeDesktopStateKey(status.state))}
      </p>
      <DesktopFact
        label={t("harnesses.claudeDesktop.status.host")}
        value={say(t, status.hostSupported,
          "harnesses.claudeDesktop.status.host.supported",
          "harnesses.claudeDesktop.status.host.unsupported")}
      />
      <DesktopFact
        label={t("harnesses.claudeDesktop.status.installed")}
        value={say(t, status.installed,
          "harnesses.claudeDesktop.status.installed.yes",
          "harnesses.claudeDesktop.status.installed.no")}
      />
      <DesktopFact
        label={t("harnesses.claudeDesktop.status.configurable")}
        value={say(t, status.configurable,
          "harnesses.claudeDesktop.status.configurable.yes",
          "harnesses.claudeDesktop.status.configurable.no")}
      />
      <DesktopFact label={t("harnesses.claudeDesktop.status.path")} mono>
        <NativePath path={status.configPath} onCopy={onCopy} />
      </DesktopFact>
      {restartRequired && (
        <p className="harnesses-claude-lede harnesses-desktop-state">
          <span className="dot dot-amber" />
          {t("harnesses.claudeDesktop.status.restart")}
        </p>
      )}
    </section>
  );
}

/** The safe native target only. No configuration contents are ever rendered. */
function NativePath({ path, onCopy }: { path: string | null; onCopy: (value: string) => void }) {
  const t = useT();
  if (!path) return <span className="harnesses-claude-hint">{t("harnesses.none")}</span>;
  return (
    <>
      <code className="harnesses-desktop-path">{path}</code>
      <button
        type="button"
        className="btn btn-ghost btn-icon"
        onClick={() => onCopy(path)}
        aria-label={t("harnesses.copy")}
        title={t("harnesses.copy")}
      >
        <IconCopy />
      </button>
    </>
  );
}

function ClaudeDesktopRefusalSection({ refusal }: { refusal: ClaudeDesktopRefusal }) {
  const t = useT();
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claudeDesktop.refusal.title")}</h4>
      <Notice tone="err">{refusal.code + " — " + refusal.message}</Notice>
    </section>
  );
}

function ClaudeDesktopDesiredSection({
  status,
  busy,
  onDesired,
}: {
  status: ClaudeDesktopStatus;
  busy: boolean;
  onDesired: (enabled: boolean) => void;
}) {
  const t = useT();
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claudeDesktop.desired.title")}</h4>
      <p className="harnesses-claude-lede">{t("harnesses.claudeDesktop.desired.hint")}</p>
      <div className="harnesses-claude-row">
        <div className="harnesses-claude-copy">
          <div className="harnesses-claude-label">{t("harnesses.claudeDesktop.desired.enable")}</div>
          <p className="harnesses-claude-hint">{t("harnesses.claudeDesktop.desired.enableHint")}</p>
          <p className="harnesses-desktop-desire">
            <span className="sr-only">{t("harnesses.claudeDesktop.desired.stateLabel")}: </span>
            {t(claudeDesktopDesireKey(claudeDesktopDesire(status)))}
          </p>
        </div>
        <div className="harnesses-claude-control">
          <DesiredControl status={status} busy={busy} onDesired={onDesired} />
        </div>
      </div>
    </section>
  );
}

/**
 * Desired enablement is editable only where the runtime can hold it. Nothing
 * that cannot be stored is rendered as a control.
 */
function DesiredControl({
  status,
  busy,
  onDesired,
}: {
  status: ClaudeDesktopStatus;
  busy: boolean;
  onDesired: (enabled: boolean) => void;
}) {
  const t = useT();
  if (!claudeDesktopActions(status).setDesired) {
    return (
      <span className="harnesses-claude-hint">
        {say(t, status.desiredEnabled, "harnesses.claudeDesktop.desired.on", "harnesses.claudeDesktop.desired.off")}
      </span>
    );
  }
  return (
    <Switch
      on={status.desiredEnabled}
      onClick={() => {
        if (busy) return;
        onDesired(!status.desiredEnabled);
      }}
      label={t("harnesses.claudeDesktop.desired.enable")}
    />
  );
}

function ClaudeDesktopActionsSection({
  status,
  busy,
  onMutate,
}: {
  status: ClaudeDesktopStatus;
  busy: boolean;
  onMutate: (mutation: ClaudeDesktopMutation) => void;
}) {
  const t = useT();
  const blockedId = useId();
  const actions = claudeDesktopActions(status);
  const blocked = claudeDesktopBlocked(status);
  return (
    <section className="harnesses-claude-section">
      <h4>{t("harnesses.claudeDesktop.actions.title")}</h4>
      <p className="harnesses-claude-lede">{t("harnesses.claudeDesktop.actions.hint")}</p>
      <div
        className="harnesses-desktop-actions"
        role="group"
        aria-label={t("harnesses.claudeDesktop.actions.title")}
        aria-describedby={blocked ? blockedId : undefined}
      >
        <ActionButton
          className="btn btn-primary btn-sm"
          offered={actions.apply}
          busy={busy}
          label={t("harnesses.claudeDesktop.actions.apply")}
          onClick={() => onMutate("apply")}
        />
        <ActionButton
          className="btn btn-ghost btn-sm"
          offered={actions.reapply}
          busy={busy}
          label={t("harnesses.claudeDesktop.actions.reapply")}
          onClick={() => onMutate("apply")}
        />
        <ActionButton
          className="btn btn-ghost btn-sm"
          offered={actions.disable}
          busy={busy}
          label={t("harnesses.claudeDesktop.actions.disable")}
          onClick={() => onMutate("disable")}
        />
      </div>
      {blocked && (
        <p id={blockedId} className="harnesses-claude-hint">{t(claudeDesktopBlockedKey(blocked))}</p>
      )}
      <p className="harnesses-claude-hint">{t("harnesses.claudeDesktop.actions.disableHint")}</p>
    </section>
  );
}

function ActionButton({
  className,
  offered,
  busy,
  label,
  onClick,
}: {
  className: string;
  offered: boolean;
  busy: boolean;
  label: string;
  onClick: () => void;
}) {
  return (
    <button type="button" className={className} onClick={onClick} disabled={!offered || busy}>
      {label}
    </button>
  );
}

function DesktopFact({
  label,
  value,
  mono,
  children,
}: {
  label: string;
  value?: string;
  mono?: boolean;
  children?: ReactNode;
}) {
  return (
    <div className={"harnesses-claude-row" + (mono ? " harnesses-desktop-path-row" : "")}>
      <div className="harnesses-claude-copy">
        <div className="harnesses-claude-label">{label}</div>
      </div>
      <div className={"harnesses-claude-control" + (mono ? " harnesses-desktop-path-cell" : "")}>
        {children ?? <span className="harnesses-desktop-value">{value}</span>}
      </div>
    </div>
  );
}
