/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useRef, useState } from "react";
import { NumberStepper } from "../components/NumberStepper";
import { clampNumberDraft } from "../clamp-draft";
import { formatBytes } from "../format-bytes";
import { Select, ToastNotice } from "../ui";
import type { Locale, TFn } from "../i18n/shared";
import {
  bindManualCleanupPreview,
  clampManualCleanupPercent,
  localizedCatch,
  MANUAL_CLEANUP_CUSTOM_VALUE,
  MANUAL_CLEANUP_PERCENT_MAX,
  MANUAL_CLEANUP_PERCENT_MIN,
  MANUAL_CLEANUP_PERCENT_PRESETS,
  manualCleanupPreviewReady,
  manualCleanupSelectValue,
  mapCleanupError,
  parseManualCleanupPercent,
  type CleanupPreview,
  type CleanupResult,
  type ManualCleanupPreviewHold,
} from "./storage-cleanup-policy";

/** One label/value line of the archived-cleanup fact list. */
type CleanupFact = {
  readonly id: string;
  readonly label: string;
  readonly value: React.ReactNode;
  readonly mono?: boolean;
};

/** One button of the confirm dialog's footer. */
type DialogAction = {
  readonly id: string;
  readonly label: string;
  readonly tone: "ghost" | "danger" | "primary";
  readonly disabled: boolean;
  readonly onSelect: () => void;
};

/** The two lines the panel reports: what a cleanup did, and why it could not. */
type CleanupFeedback = {
  readonly status: string | null;
  readonly error: string | null;
};

/** The percent field's unit and the width its input reserves. */
const PERCENT_SUFFIX = "%";

/** How long a changed percent waits before the preview is re-read. */
const PREVIEW_DEBOUNCE_MS = 250;

/** The dialog styles live here because the modal is not on a board stylesheet. */
const CANDIDATE_LIST_STYLE = { maxHeight: 160, overflow: "auto", fontSize: "var(--text-caption)" } as const;
const TOGGLE_ROW_STYLE = { display: "flex", gap: 8, alignItems: "center", marginTop: 12 } as const;
const CAPTION_STYLE = { marginTop: 8, fontSize: "var(--text-caption)" } as const;
const DIALOG_ERROR_STYLE = { marginTop: 12, color: "var(--red)" } as const;
const DIALOG_ACTIONS_STYLE = { marginTop: 16 } as const;

const EMPTY_FEEDBACK: CleanupFeedback = { status: null, error: null };

/** One label/value line, toned like the rest of the Storage boards. */
function CleanupFactRow({ fact }: { fact: CleanupFact }) {
  return (
    <div className="stw-kv-row">
      <dt>{fact.label}</dt>
      <dd className={fact.mono ? "stw-kv-mono" : undefined}>{fact.value}</dd>
    </div>
  );
}

/** One footer button, ghost for the cancel and toned for the commit. */
function DialogActionButton({
  action,
  focusRef,
  onSelect,
}: {
  action: DialogAction;
  focusRef?: { readonly current: HTMLButtonElement | null };
  onSelect: () => void;
}) {
  const className = action.tone === "ghost"
    ? "btn btn-ghost"
    : action.tone === "danger" ? "btn btn-danger" : "btn";
  return (
    <button
      ref={focusRef}
      type="button"
      className={className}
      disabled={action.disabled}
      onClick={onSelect}
    >
      {action.label}
    </button>
  );
}

/** The candidates a cleanup would remove, capped with a line for the rest. */
function CandidateList({ t, preview }: { t: TFn; preview: CleanupPreview }) {
  if (preview.candidates.length === 0) return null;
  const shown = preview.candidates.slice(0, 8).map(candidate => candidate.relPath);
  const hidden = Math.max(0, preview.count - 8);
  return (
    <ul className="mono muted" style={CANDIDATE_LIST_STYLE}>
      {shown.map(relPath => <li key={relPath}>{relPath}</li>)}
      {preview.count > 8 && <li>{t("storage.cleanup.moreFiles", { n: String(hidden) })}</li>}
    </ul>
  );
}

/** The body that runs a previewed cleanup: the percent the preview was taken at, and its digest. */
function cleanupRequestBody(preview: CleanupPreview, permanent: boolean): {
  percent: number;
  mode: string;
  digest: string;
} {
  return {
    percent: preview.percent,
    mode: permanent ? "permanent" : "quarantine",
    digest: preview.digest,
  };
}

/** Ask what a cleanup at `percent` would remove, and keep whatever refusal came with it. */
async function requestPreview(
  apiBase: string,
  percent: number,
  signal: AbortSignal,
): Promise<{ preview?: CleanupPreview; failure: { error?: string; message?: string } }> {
  const response = await fetch(`${apiBase}/api/storage/cleanup/preview`, {
    method: "POST",
    headers: { "content-type": "application/json" },
    body: JSON.stringify({ percent }),
    signal,
  });
  const payload = await response.json().catch(() => ({})) as CleanupPreview & { error?: string; message?: string };
  if (!response.ok) {
    return { failure: { error: payload.error, message: payload.message } };
  }
  return { preview: payload, failure: {} };
}

function CleanupConfirmDialog({
  t, locale, preview, permanent, busy, error, cancelRef, onPermanent, onCancel, onConfirm,
}: {
  t: TFn;
  locale: Locale;
  preview: CleanupPreview;
  permanent: boolean;
  busy: boolean;
  error: string | null;
  cancelRef: { current: HTMLButtonElement | null };
  onPermanent: (value: boolean) => void;
  onCancel: () => void;
  onConfirm: () => void;
}) {
  const actions: DialogAction[] = [
    { id: "cancel", label: t("storage.cleanup.cancel"), tone: "ghost", disabled: busy, onSelect: onCancel },
    {
      id: "confirm",
      label: permanent ? t("storage.cleanup.confirmPermanent") : t("storage.cleanup.confirmQuarantine"),
      tone: permanent ? "danger" : "primary",
      disabled: busy || preview.count === 0,
      onSelect: onConfirm,
    },
  ];
  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="storage-cleanup-confirm-title"
      onClick={() => !busy && onCancel()}
    >
      <div className="modal-card" onClick={e => e.stopPropagation()}>
        <h3 id="storage-cleanup-confirm-title">{t("storage.cleanup.confirmTitle")}</h3>
        <p>
          {t("storage.cleanup.confirmBody", {
            count: String(preview.count),
            size: formatBytes(preview.bytes, locale),
            percent: String(preview.percent),
          })}
        </p>
        <CandidateList t={t} preview={preview} />
        <label style={TOGGLE_ROW_STYLE}>
          <input
            type="checkbox"
            checked={permanent}
            disabled={busy}
            onChange={e => onPermanent(e.target.checked)}
          />
          <span>{t("storage.cleanup.permanent")}</span>
        </label>
        <p className="muted" style={CAPTION_STYLE}>
          {permanent ? t("storage.cleanup.permanentWarn") : t("storage.cleanup.quarantineNote")}
        </p>
        {error && <p style={DIALOG_ERROR_STYLE}>{error}</p>}
        <div className="dialog-actions" style={DIALOG_ACTIONS_STYLE}>
          {actions.map(action => (
            <DialogActionButton
              key={action.id}
              action={action}
              focusRef={action.id === "cancel" ? cancelRef : undefined}
              onSelect={action.onSelect}
            />
          ))}
        </div>
      </div>
    </div>
  );
}

/** One percent above the preset rows, typed or stepped, with its own commit gesture. */
function CustomPercentField({
  t,
  draft,
  busy,
  onDraftChange,
  onDraftCommit,
  onStep,
}: {
  t: TFn;
  draft: string;
  busy: boolean;
  onDraftChange: (value: string) => void;
  onDraftCommit: () => void;
  onStep: (delta: number) => void;
}) {
  return (
    <span className="codex-auto-switch-input-wrap">
      <input
        className="input mono codex-auto-switch-input"
        type="number"
        min={MANUAL_CLEANUP_PERCENT_MIN}
        max={MANUAL_CLEANUP_PERCENT_MAX}
        step={1}
        inputMode="numeric"
        value={draft}
        disabled={busy}
        aria-label={t("storage.cleanup.slider")}
        onChange={e => onDraftChange(e.target.value)}
        onBlur={onDraftCommit}
        onKeyDown={event => {
          if (event.nativeEvent.isComposing) return;
          if (event.key !== "Enter") return;
          event.preventDefault();
          onDraftCommit();
        }}
      />
      <span className="codex-auto-switch-unit" aria-hidden="true">{PERCENT_SUFFIX}</span>
      <NumberStepper
        disabled={busy}
        incrementLabel={t("storage.policy.percentInc")}
        decrementLabel={t("storage.policy.percentDec")}
        onIncrement={() => onStep(1)}
        onDecrement={() => onStep(-1)}
      />
    </span>
  );
}

function ManualCleanupPercentPicker({
  t, percent, customMode, percentDraft, busy, onPreset, onCustomMode, onDraftChange, onDraftCommit, onStep,
}: {
  t: TFn;
  percent: number;
  customMode: boolean;
  percentDraft: string;
  busy: boolean;
  onPreset: (value: number) => void;
  onCustomMode: () => void;
  onDraftChange: (value: string) => void;
  onDraftCommit: () => void;
  onStep: (delta: number) => void;
}) {
  const options = [
    ...MANUAL_CLEANUP_PERCENT_PRESETS.map(value => ({
      value: String(value),
      label: t("storage.cleanup.oldestPercent", { percent: String(value) }),
    })),
    { value: MANUAL_CLEANUP_CUSTOM_VALUE, label: t("storage.cleanup.custom") },
  ];
  const selectValue = manualCleanupSelectValue(percent, customMode);
  const custom = selectValue === MANUAL_CLEANUP_CUSTOM_VALUE;
  return (
    <div className="storage-cleanup-remove">
      <Select
        value={selectValue}
        options={options}
        onChange={value => {
          if (value === MANUAL_CLEANUP_CUSTOM_VALUE) {
            onCustomMode();
            return;
          }
          onPreset(Number(value));
        }}
        disabled={busy}
        label={t("storage.cleanup.remove")}
        chevron="down"
      />
      {custom && (
        <CustomPercentField
          t={t}
          draft={percentDraft}
          busy={busy}
          onDraftChange={onDraftChange}
          onDraftCommit={onDraftCommit}
          onStep={onStep}
        />
      )}
    </div>
  );
}

export function ArchivedCleanupPanel({
  apiBase,
  locale,
  t,
  onDone,
  archivedCount,
  archivedBytes,
  storageGeneration,
}: {
  apiBase: string;
  locale: Locale;
  t: TFn;
  onDone: () => void;
  archivedCount: number;
  archivedBytes: number;
  storageGeneration: number;
}) {
  const [percent, setPercent] = useState(25);
  const [customMode, setCustomMode] = useState(false);
  const [percentDraft, setPercentDraft] = useState("25");
  const [previewEpoch, setPreviewEpoch] = useState(0);
  const [hold, setHold] = useState<ManualCleanupPreviewHold | null>(null);
  const preview = hold?.preview ?? null;
  const [confirmOpen, setConfirmOpen] = useState(false);
  const [permanent, setPermanent] = useState(false);
  const [busy, setBusy] = useState(false);
  const [feedback, setFeedback] = useState<CleanupFeedback>(EMPTY_FEEDBACK);
  const { status, error } = feedback;
  const cancelRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);

  const reportStatus = (value: string | null) => setFeedback(previous => ({ ...previous, status: value }));
  const reportError = (value: string | null) => setFeedback(previous => ({ ...previous, error: value }));

  const closeConfirm = useCallback((clearPreview = false) => {
    setConfirmOpen(false);
    setPermanent(false);
    if (clearPreview) setHold(null);
  }, []);

  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);

  useEffect(() => {
    if (!confirmOpen) return;
    previousFocusRef.current = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    cancelRef.current?.focus();
    const onKey = (event: WindowEventMap["keydown"]) => {
      if (event.key !== "Escape") return;
      if (busyRef.current) return;
      closeConfirm();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previousFocusRef.current?.focus();
    };
  }, [confirmOpen, closeConfirm]);

  useEffect(() => {
    const generation = storageGeneration;
    const controller = new AbortController();
    const timer = window.setTimeout(() => {
      void (async () => {
        const outcome = await requestPreview(apiBase, percent, controller.signal).catch(() => null);
        if (controller.signal.aborted) return;
        const json = outcome?.preview;
        if (json === undefined) {
          const failure = outcome?.failure ?? {};
          const refusal = mapCleanupError(t, failure.error, t("storage.cleanup.previewFailed"), undefined, failure.message);
          setHold(null);
          reportError(localizedCatch(new Error(refusal), t("storage.cleanup.previewFailed")));
          return;
        }
        setHold(bindManualCleanupPreview(json, generation));
        reportError(null);
      })();
    }, PREVIEW_DEBOUNCE_MS);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [apiBase, percent, previewEpoch, storageGeneration, t]);

  const previewReady = manualCleanupPreviewReady(hold, percent, storageGeneration);

  const runPreview = async () => {
    if (!previewReady || !preview) return;
    setConfirmOpen(true);
  };

  const runCleanup = async () => {
    if (!previewReady || !preview) return;
    setBusy(true);
    reportError(null);
    try {
      const response = await fetch(`${apiBase}/api/storage/cleanup`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify(cleanupRequestBody(preview, permanent)),
      });
      const json = await response.json().catch(() => ({})) as CleanupResult;
      if (!response.ok || !json.ok) {
        // A stale digest can never succeed again — send the reader back to Preview.
        if (json.error === "stale_preview") closeConfirm(true);
        throw new Error(mapCleanupError(t, json.error, json.message, json.trashDir, json.message));
      }
      closeConfirm(true);
      reportStatus(
        permanent
          ? t("storage.cleanup.donePermanent", {
            count: String(json.count),
            size: formatBytes(json.freedBytes ?? json.bytes, locale),
          })
          : t("storage.cleanup.doneQuarantine", {
            count: String(json.count),
            size: formatBytes(json.bytes, locale),
          }),
      );
      onDone();
    } catch (e) {
      // Keep the dialog open (except stale_preview) so the failure is visible.
      reportError(localizedCatch(e, t("storage.cleanup.cleanupFailed")));
    } finally {
      setBusy(false);
    }
  };

  const applyPercent = (value: number, nextCustom: boolean) => {
    const next = clampManualCleanupPercent(value);
    reportError(null);
    setCustomMode(nextCustom);
    setPercent(next);
    setPercentDraft(String(next));
  };

  const previewCell = previewReady && preview
    ? (
      <span className="stw-kv-mono">
        {t("storage.cleanup.previewSummary", {
          count: String(preview.count),
          size: formatBytes(preview.bytes, locale),
        })}
      </span>
    )
    : error && !confirmOpen
      ? (
        <span className="storage-cleanup-preview-fail">
          <span>{error}</span>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={busy}
            onClick={() => {
              reportError(null);
              setPreviewEpoch(n => n + 1);
            }}
          >
            {t("storage.cleanup.previewAgain")}
          </button>
        </span>
      )
      : "—";

  const facts: CleanupFact[] = [
    {
      id: "archived",
      label: t("storage.bucket.archived_sessions"),
      mono: true,
      value: <>{archivedCount.toLocaleString(locale)}{" · "}{formatBytes(archivedBytes, locale)}</>,
    },
    {
      id: "remove",
      label: t("storage.cleanup.remove"),
      value: (
        <ManualCleanupPercentPicker
          t={t}
          percent={percent}
          customMode={customMode}
          percentDraft={percentDraft}
          busy={busy}
          onPreset={value => applyPercent(value, false)}
          onCustomMode={() => setCustomMode(true)}
          onDraftChange={value => {
            setPercentDraft(value);
            const parsed = parseManualCleanupPercent(value);
            if (parsed === undefined) return;
            reportError(null);
            setPercent(parsed);
          }}
          onDraftCommit={() => {
            const parsed = parseManualCleanupPercent(percentDraft);
            applyPercent(parsed === undefined ? percent : parsed, true);
          }}
          onStep={delta => applyPercent(Number(clampNumberDraft(percentDraft, delta, MANUAL_CLEANUP_PERCENT_MIN, MANUAL_CLEANUP_PERCENT_MAX)), true)}
        />
      ),
    },
    { id: "preview", label: t("storage.cleanup.preview"), value: previewCell },
  ];

  return (
    <section className="storage-cleanup-pane">
      <p className="muted storage-manual-panel__help">{t("storage.cleanup.help")}</p>
      <dl className="stw-kv">
        {facts.map(fact => <CleanupFactRow key={fact.id} fact={fact} />)}
      </dl>
      <div className="storage-manual-panel__run">
        <button
          type="button"
          className="btn storage-action"
          disabled={busy || !previewReady || !preview || preview.count === 0}
          onClick={() => void runPreview()}
        >
          {t("storage.cleanup.run")}
        </button>
      </div>

      {status && (
        <ToastNotice
          tone="ok"
          dismissLabel={t("common.close")}
          onDismiss={() => reportStatus(null)}
        >
          {status}
        </ToastNotice>
      )}

      {confirmOpen && previewReady && preview && (
        <CleanupConfirmDialog
          t={t}
          locale={locale}
          preview={preview}
          permanent={permanent}
          busy={busy}
          error={error}
          cancelRef={cancelRef}
          onPermanent={setPermanent}
          onCancel={closeConfirm}
          onConfirm={() => void runCleanup()}
        />
      )}
    </section>
  );
}