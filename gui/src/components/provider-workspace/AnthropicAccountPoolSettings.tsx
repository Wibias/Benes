/**
 * Opt-in Anthropic OAuth account pool controls.
 * Experimental — shows a strong warning because the feature is not battle-tested.
 */
import { useCallback, useEffect, useState, type CSSProperties } from "react";
import { useT, type TFn } from "../../i18n/shared";
import {
  parseAccountPoolStickyLimitDraft,
  type AccountPoolStrategy,
} from "../../account-pool-strategy";
import AccountPoolStrategyControls from "../AccountPoolStrategyControls";
import {
  anthropicPoolSavedSnapshot,
  anthropicPoolSnapshotFromPayload,
  anthropicPoolToggleDisabled,
  anthropicPoolView,
  parseAnthropicThresholdDraft,
  type AnthropicPoolSnapshot,
} from "../../provider-workspace/anthropic-pool-state";

const CARD_STYLE: CSSProperties = { marginTop: 12 };
const SUMMARY_ROW_STYLE: CSSProperties = { alignItems: "flex-start", gap: 12 };
const SUMMARY_GROW_STYLE: CSSProperties = { flex: 1 };
const STATUS_STYLE: CSSProperties = { marginTop: 4 };
const WARNING_STYLE: CSSProperties = {
  marginTop: 10,
  padding: "10px 16px",
  border: "1px solid var(--border, #c9a227)",
  borderRadius: 4,
  background: "color-mix(in srgb, var(--amber) 12%, transparent)",
};
const HINT_STYLE: CSSProperties = { marginTop: 8 };
const ERROR_STYLE: CSSProperties = { marginTop: 8, color: "var(--red)" };
const THRESHOLD_FIELD_STYLE: CSSProperties = { display: "block", marginTop: 12 };
const THRESHOLD_HELP_STYLE: CSSProperties = { marginTop: 4 };

/**
 * The pool editor is one draft: the loaded snapshot, both editable drafts, and the
 * result of the write in flight. Holding them together keeps a failed write from
 * rolling back one field and leaving another at the attempted value.
 */
interface PoolEditorState {
  snapshot: AnthropicPoolSnapshot | null;
  thresholdDraft: string;
  stickyDraft: string;
  saving: boolean;
  error: string | null;
  loadFailed: boolean;
}

const EMPTY_POOL_EDITOR: PoolEditorState = {
  snapshot: null,
  thresholdDraft: "80",
  stickyDraft: "1",
  saving: false,
  error: null,
  loadFailed: false,
};

/** The draft fields a snapshot replaces; the in-flight fields are the caller's. */
function draftFromSnapshot(snapshot: AnthropicPoolSnapshot): Partial<PoolEditorState> {
  return {
    snapshot,
    thresholdDraft: String(snapshot.threshold),
    stickyDraft: String(snapshot.stickyLimit),
  };
}

/** The provider the pool endpoint is scoped to. */
const POOL_PROVIDER = "anthropic";

/** Every pool read and write goes through the same provider-scoped endpoint. */
function poolEndpoint(apiBase: string): string {
  return `${apiBase}/api/oauth/accounts/pool?provider=${POOL_PROVIDER}`;
}

const POOL_WRITE_OPTIONS: RequestInit = {
  method: "PUT",
  headers: { "content-type": "application/json" },
};

/** The write body the pool endpoint expects, with the provider it is scoped to. */
function poolWriteBody(next: AnthropicPoolSnapshot): string {
  return JSON.stringify({
    provider: POOL_PROVIDER,
    enabled: next.enabled,
    autoSwitchThreshold: next.threshold,
    strategy: next.strategy,
    stickyLimit: next.stickyLimit,
  });
}

/** Reads the pool settings; null when the listener did not answer with a usable snapshot. */
async function readPoolSnapshot(apiBase: string, signal: AbortSignal): Promise<AnthropicPoolSnapshot | null> {
  const response = await fetch(poolEndpoint(apiBase), { signal });
  if (!response.ok) return null;
  return anthropicPoolSnapshotFromPayload(await response.json().catch(() => null));
}

/**
 * Writes the pool settings and resolves the snapshot the listener saved. Only the
 * strategy and the sticky limit can come back different from what was asked for.
 */
async function writePoolSnapshot(
  apiBase: string,
  next: AnthropicPoolSnapshot,
): Promise<AnthropicPoolSnapshot | null> {
  const response = await fetch(poolEndpoint(apiBase), { ...POOL_WRITE_OPTIONS, body: poolWriteBody(next) });
  if (!response.ok) return null;
  return anthropicPoolSavedSnapshot(next, await response.json().catch(() => null));
}

/** Status line: a load failure beats loading, and an enabled pool states its threshold. */
function poolStatusCopy(
  editor: { loading: boolean; loadFailed: boolean; enabled: boolean; threshold: number },
  t: TFn,
): string {
  if (editor.loadFailed) return t("anthropicPool.loadFailed");
  if (editor.loading) return t("common.loading");
  return editor.enabled
    ? t("anthropicPool.enabledDesc", { threshold: editor.threshold })
    : t("anthropicPool.disabledDesc");
}

export default function AnthropicAccountPoolSettings({
  apiBase,
  accountCount,
}: {
  apiBase: string;
  accountCount: number;
}) {
  const t = useT();
  const [editor, setEditor] = useState<PoolEditorState>(EMPTY_POOL_EDITOR);
  const patchEditor = useCallback((next: Partial<PoolEditorState>) => {
    setEditor(current => ({ ...current, ...next }));
  }, []);

  useEffect(() => {
    let cancelled = false;
    const controller = new AbortController();
    // Every setter stays inside a `.then` callback guarded by the same `cancelled`
    // flag, which is the shape react-doctor's no-set-state-after-await-in-effect can
    // verify. Deferred by a microtask rather than a timer: a timer had to be
    // cancelled in cleanup, so a mount-then-unmount dropped the request entirely,
    // while the abort controller already covers the cancellation that matters.
    void Promise.resolve()
      .then(() => readPoolSnapshot(apiBase, controller.signal))
      .then(snapshot => {
        if (cancelled) return;
        if (snapshot) {
          setEditor(current => ({ ...current, ...draftFromSnapshot(snapshot), error: null, loadFailed: false }));
          return;
        }
        setEditor(current => ({ ...current, loadFailed: true }));
      })
      .catch(() => {
        if (cancelled || controller.signal.aborted) return;
        setEditor(current => ({ ...current, loadFailed: true }));
      });
    return () => {
      cancelled = true;
      controller.abort();
    };
  }, [apiBase]);

  const previous = editor.snapshot;

  /** A refused or failed write restores the last snapshot the listener confirmed. */
  const failWrite = useCallback(() => {
    setEditor(current => ({
      ...current,
      ...(previous ? draftFromSnapshot(previous) : {}),
      saving: false,
      error: t("anthropicPool.saveFailed"),
    }));
  }, [previous, t]);

  const save = useCallback(async (next: AnthropicPoolSnapshot) => {
    patchEditor({ snapshot: next, saving: true, error: null });
    let saved: AnthropicPoolSnapshot | null = null;
    try {
      saved = await writePoolSnapshot(apiBase, next);
    } catch {
      saved = null;
    }
    if (!saved) {
      failWrite();
      return;
    }
    setEditor(current => ({
      ...current,
      ...draftFromSnapshot(saved),
      thresholdDraft: String(next.threshold),
      saving: false,
    }));
  }, [apiBase, patchEditor, failWrite]);

  const view = anthropicPoolView(editor.snapshot);
  const loading = editor.snapshot === null && !editor.loadFailed;
  const toggleDisabled = anthropicPoolToggleDisabled({
    loading,
    saving: editor.saving,
    loadError: editor.loadFailed,
    enabled: view.enabled,
    accountCount,
  });

  const commitThreshold = () => {
    const parsed = parseAnthropicThresholdDraft(editor.thresholdDraft, view.threshold);
    if (parsed.kind === "invalid") {
      patchEditor({ thresholdDraft: String(view.threshold), error: t("anthropicPool.thresholdInvalid") });
      return;
    }
    if (parsed.kind === "next") {
      void save({ enabled: true, threshold: parsed.value, strategy: view.strategy, stickyLimit: view.stickyLimit });
    }
  };

  const commitStickyLimit = (nextDraft: string) => {
    const parsed = parseAccountPoolStickyLimitDraft(nextDraft ?? editor.stickyDraft);
    if (parsed === null) {
      patchEditor({ stickyDraft: String(view.stickyLimit), error: t("accountPool.stickyLimitInvalid") });
      return;
    }
    if (parsed === view.stickyLimit) {
      patchEditor({ stickyDraft: String(parsed) });
      return;
    }
    void save({ enabled: true, threshold: view.threshold, strategy: view.strategy, stickyLimit: parsed });
  };

  const changeStrategy = (next: AccountPoolStrategy) => {
    if (next === view.strategy) return;
    void save({ enabled: true, threshold: view.threshold, strategy: next, stickyLimit: view.stickyLimit });
  };

  return (
    <div className="card" style={CARD_STYLE} aria-busy={loading || editor.saving}>
      <PoolSummary
        status={poolStatusCopy({ loading, loadFailed: editor.loadFailed, enabled: view.enabled, threshold: view.threshold }, t)}
        enabled={view.enabled}
        toggleDisabled={toggleDisabled}
        onToggle={() => {
          void save({
            enabled: !view.enabled,
            threshold: view.threshold,
            strategy: view.strategy,
            stickyLimit: view.stickyLimit,
          });
        }}
      />
      <PoolNotices accountCount={accountCount} />
      {view.enabled && editor.snapshot && (
        <PoolFields
          draft={editor.thresholdDraft}
          stickyDraft={editor.stickyDraft}
          saving={editor.saving}
          strategy={view.strategy}
          onDraftChange={value => patchEditor({ thresholdDraft: value })}
          onStickyDraftChange={value => patchEditor({ stickyDraft: value })}
          onThresholdCommit={commitThreshold}
          onStrategyChange={changeStrategy}
          onStickyCommit={commitStickyLimit}
        />
      )}
      {editor.error && (
        <div role="alert" className="card-sub" style={ERROR_STYLE}>
          {editor.error}
        </div>
      )}
    </div>
  );
}

function PoolSummary({
  status, enabled, toggleDisabled, onToggle,
}: {
  status: string;
  enabled: boolean;
  toggleDisabled: boolean;
  onToggle: () => void;
}) {
  const t = useT();
  return (
    <div className="card-row" style={SUMMARY_ROW_STYLE}>
      <div style={SUMMARY_GROW_STYLE}>
        <strong>{t("anthropicPool.title")}</strong>
        <div className="card-sub" style={STATUS_STYLE}>{status}</div>
      </div>
      <button
        type="button"
        className={`toggle ${enabled ? "on" : ""}`}
        disabled={toggleDisabled}
        aria-pressed={enabled}
        aria-label={t("anthropicPool.title")}
        title={enabled ? t("anthropicPool.on") : t("anthropicPool.off")}
        onClick={onToggle}
      >
        <span className="toggle-knob" />
      </button>
    </div>
  );
}

function PoolNotices({ accountCount }: { accountCount: number }) {
  const t = useT();
  return (
    <>
      <div role="alert" className="card-sub" style={WARNING_STYLE}>
        {t("anthropicPool.experimentalWarning")}
      </div>
      {accountCount < 2 && (
        <div className="card-sub" style={HINT_STYLE}>{t("anthropicPool.needTwoAccounts")}</div>
      )}
    </>
  );
}

/** Percentage field the pool switches accounts at. */
function ThresholdField({
  value, disabled, onDraft, onCommit,
}: {
  value: string;
  disabled: boolean;
  onDraft: (value: string) => void;
  onCommit: () => void;
}) {
  const t = useT();
  return (
    <label className="field" style={THRESHOLD_FIELD_STYLE}>
      <span className="field-label">{t("anthropicPool.threshold")}</span>
      <input
        className="input mono"
        type="number"
        min={0}
        max={100}
        step={1}
        value={value}
        disabled={disabled}
        aria-label={t("anthropicPool.thresholdAria")}
        onChange={event => onDraft(event.target.value)}
        onBlur={onCommit}
      />
      <div className="card-sub" style={THRESHOLD_HELP_STYLE}>{t("anthropicPool.thresholdHelp")}</div>
    </label>
  );
}

function PoolFields({
  draft, stickyDraft, saving, strategy,
  onDraftChange, onStickyDraftChange, onThresholdCommit, onStrategyChange, onStickyCommit,
}: {
  draft: string;
  stickyDraft: string;
  saving: boolean;
  strategy: AccountPoolStrategy;
  onDraftChange: (value: string) => void;
  onStickyDraftChange: (value: string) => void;
  onThresholdCommit: () => void;
  onStrategyChange: (next: AccountPoolStrategy) => void;
  onStickyCommit: (nextDraft: string) => void;
}) {
  return (
    <>
      <ThresholdField value={draft} disabled={saving} onDraft={onDraftChange} onCommit={onThresholdCommit} />
      <AccountPoolStrategyControls
        strategy={strategy}
        stickyDraft={stickyDraft}
        disabled={saving}
        strategySelectId="anthropic-pool-strategy"
        stickyInputId="anthropic-pool-sticky-limit"
        onStrategyChange={onStrategyChange}
        onStickyDraftChange={onStickyDraftChange}
        onStickyCommit={onStickyCommit}
      />
    </>
  );
}
