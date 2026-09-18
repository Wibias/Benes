/** Benes dashboard client for the Go proxy (`internal/server`). */
import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { formatBytes } from "../format-bytes";
import type { Locale, TFn, TKey } from "../i18n/shared";
import { ToastNotice } from "../ui";
import { DataSurfaceSkeleton, DataSurfaceStatus } from "../components/data-surface";
import { useDataSurface } from "../data-surface";
import {
  localizedCatch,
  mapRestoreError,
  type RestoreResult,
  type TrashEntry,
  type TrashList,
} from "./storage-cleanup-policy";
import { timestampDateTimeDisplay } from "./storage-report-view";
import {
  quarantineChromeKind,
  recoveryReasonLabel,
  trashListFailureMessage,
  trashRows,
  type TrashRow,
  type TrashRowStatus,
} from "./storage-trash-view";

/** How each row status reads, and how it is toned in a row or a fact list. */
const STATUS_PRESENTATION: Record<TrashRowStatus, { readonly label: TKey; readonly className: string }> = {
  ready: { label: "storage.trash.status.ready", className: "storage-status--ready" },
  partial: { label: "storage.trash.status.partial", className: "storage-status--partial" },
  recovery_needed: { label: "storage.trash.status.recovery", className: "storage-status--recovery" },
};

/** Catalogue copy for the two modes a quarantined entry can be held in. */
const MODE_COPY: Record<string, TKey> = {
  permanent: "storage.trash.mode.permanent",
  quarantine: "storage.trash.mode.quarantine",
};

function statusLabel(t: TFn, status: TrashRowStatus): string {
  return t(STATUS_PRESENTATION[status].label);
}

function statusClass(status: TrashRowStatus): string {
  return STATUS_PRESENTATION[status].className;
}

function trashModeLabel(t: TFn, mode: TrashEntry["mode"] | undefined): string {
  const copy = mode === undefined ? undefined : MODE_COPY[mode];
  return copy === undefined ? "—" : t(copy);
}

/** A number the listener may not have reported reads as a dash; a present one is formatted. */
function dashOr(value: number | undefined, render: (value: number) => string): string {
  return value === undefined ? "—" : render(value);
}

/** One label/value line of a quarantine detail block. */
function DetailFact({
  label,
  value,
  tone,
}: {
  label: string;
  value: React.ReactNode;
  tone?: string;
}) {
  return (
    <div className="stw-kv-row">
      <dt>{label}</dt>
      <dd className={tone ?? "stw-kv-mono"}>{value}</dd>
    </div>
  );
}

/** One transient line the panel reports: what a restore did, or why it could not. */
function PanelNotice({
  tone,
  message,
  dismissLabel,
  onDismiss,
}: {
  tone: "ok" | "err";
  message: string | null;
  dismissLabel: string;
  onDismiss: () => void;
}) {
  if (message === null) return null;
  return (
    <ToastNotice tone={tone} dismissLabel={dismissLabel} onDismiss={onDismiss}>
      {message}
    </ToastNotice>
  );
}

function QuarantineListError({
  t, message, onRetry,
}: {
  t: TFn;
  message: string;
  onRetry: () => void;
}) {
  return (
    <p className="storage-manual-panel__status" style={{ color: "var(--red)" }} role="alert">
      {message}
      {" "}
      <button type="button" className="btn btn-ghost btn-sm" onClick={onRetry}>{t("common.retry")}</button>
    </p>
  );
}

/** What a restore that finished reports, in the board's own words. */
function restoreDoneStatus(json: RestoreResult, t: TFn, locale: Locale): string {
  const size = formatBytes(json.bytes, locale);
  if (json.partial) {
    return t("storage.trash.donePartial", { count: String(json.count), size });
  }
  if (json.alreadyRestored) {
    return t("storage.trash.doneAlready", {
      count: String(json.count),
      already: String(json.alreadyRestored),
      size,
    });
  }
  return t("storage.trash.done", { count: String(json.count), size });
}

function QuarantineMasterBody({
  t, locale, chrome, rows, selectedId, formatWhen, listError, onSelect, onRetry,
}: {
  t: TFn;
  locale: Locale;
  chrome: ReturnType<typeof quarantineChromeKind>;
  rows: TrashRow[];
  selectedId: string | null;
  formatWhen: (row: TrashRow) => string;
  listError: string;
  onSelect: (id: string) => void;
  onRetry: () => void;
}) {
  if (chrome === "loading") return <DataSurfaceSkeleton label={t("storage.trash.loading")} rows={2} />;
  if (chrome === "error") return <QuarantineListError t={t} message={listError} onRetry={onRetry} />;
  if (chrome === "empty") return <p className="stw-empty">{t("storage.empty.quarantine")}</p>;
  return (
    <QuarantineMasterList
      t={t}
      locale={locale}
      rows={rows}
      selectedId={selectedId}
      formatWhen={formatWhen}
      modeLabel={mode => trashModeLabel(t, mode)}
      onSelect={onSelect}
    />
  );
}

/** The table of held entries: one row per entry, in the order the listener reported them. */
function QuarantineMasterList({
  t, locale, rows, selectedId, formatWhen, modeLabel, onSelect,
}: {
  t: TFn;
  locale: Locale;
  rows: TrashRow[];
  selectedId: string | null;
  formatWhen: (row: TrashRow) => string;
  modeLabel: (mode: TrashEntry["mode"] | undefined) => string;
  onSelect: (id: string) => void;
}) {
  const columns = [
    t("storage.col.created"),
    t("storage.col.mode"),
    t("storage.col.files"),
    t("storage.col.size"),
    t("storage.col.status"),
  ];
  return (
    <div className="storage-quarantine-table" role="table" aria-label={t("storage.trash.title")}>
      <div className="storage-quarantine-head" role="row">
        {columns.map(label => <span key={label}>{label}</span>)}
      </div>
      {rows.map(row => {
        const cells = [
          { id: "created", className: "muted", text: formatWhen(row) },
          { id: "mode", className: "muted", text: modeLabel(row.mode) },
          { id: "files", className: "num", text: dashOr(row.fileCount, count => count.toLocaleString(locale)) },
          { id: "size", className: "num mono", text: dashOr(row.bytes, bytes => formatBytes(bytes, locale)) },
          { id: "status", className: statusClass(row.status), text: statusLabel(t, row.status) },
        ];
        const current = selectedId === row.id;
        return (
          <button
            key={row.id}
            type="button"
            role="row"
            className={current
              ? "storage-quarantine-row storage-quarantine-row--selected"
              : "storage-quarantine-row"}
            onClick={() => onSelect(row.id)}
            aria-current={current ? "true" : undefined}
          >
            {cells.map(cell => (
              <span key={cell.id} className={cell.className}>{cell.text}</span>
            ))}
          </button>
        );
      })}
    </div>
  );
}

/** Why an entry needs recovery, and what the reader can do about it. */
function QuarantineRecoveryDetail({ t, selected }: { t: TFn; selected: TrashRow }) {
  return (
    <>
      <h3 className="stw-detail-title">{t("storage.trash.recoveryTitle")}</h3>
      <dl className="stw-kv">
        <DetailFact label={t("storage.trash.col.entryId")} value={selected.recoveryId ?? "—"} />
        <DetailFact
          label={t("storage.col.status")}
          value={t("storage.trash.status.recovery")}
          tone={statusClass("recovery_needed")}
        />
        <DetailFact
          label={t("storage.trash.col.reason")}
          value={recoveryReasonLabel(t, selected.recoveryError, selected.recoveryStatus)}
          tone=""
        />
      </dl>
      <p className="storage-cleanup-truncated" role="status">{t("storage.trash.recoveryHelp")}</p>
    </>
  );
}

/** The held entry the reader selected, or the note that explains why it cannot be restored. */
function QuarantineDetail({
  t, locale, selected, formatWhen, modeLabel, busy, showError, onRestore,
}: {
  t: TFn;
  locale: Locale;
  selected: TrashRow | null;
  formatWhen: (row: TrashRow) => string;
  modeLabel: (mode: TrashEntry["mode"] | undefined) => string;
  busy: boolean;
  showError: boolean;
  onRestore: (entry: TrashEntry) => void;
}) {
  if (showError || !selected) return null;
  if (selected.kind === "recovery") return <QuarantineRecoveryDetail t={t} selected={selected} />;
  const entry = selected.entry;
  return (
    <>
      <h3 className="stw-detail-title">{formatWhen(selected)}</h3>
      <dl className="stw-kv">
        <DetailFact label={t("storage.col.files")} value={dashOr(selected.fileCount, count => count.toLocaleString(locale))} />
        <DetailFact label={t("storage.col.size")} value={dashOr(selected.bytes, bytes => formatBytes(bytes, locale))} />
        <DetailFact label={t("storage.col.mode")} value={modeLabel(selected.mode)} tone="" />
        <DetailFact
          label={t("storage.col.status")}
          value={statusLabel(t, selected.status)}
          tone={statusClass(selected.status)}
        />
        <DetailFact label={t("storage.col.created")} value={formatWhen(selected)} tone="" />
      </dl>
      {entry ? (
        <>
          <button type="button" className="btn storage-action" disabled={busy} onClick={() => onRestore(entry)}>
            {t("storage.trash.restore")}
          </button>
          <p className="muted storage-restore-help">{t("storage.trash.restoreHelp")}</p>
        </>
      ) : null}
    </>
  );
}

export function QuarantineTrashPanel({
  apiBase,
  locale,
  t,
  onDone,
  reloadToken,
  onEntriesChange,
}: {
  apiBase: string;
  locale: Locale;
  t: TFn;
  onDone: () => void;
  reloadToken: number;
  onEntriesChange?: (entries: TrashEntry[]) => void;
}) {
  const [busy, setBusy] = useState(false);
  const [confirmEntry, setConfirmEntry] = useState<TrashEntry | null>(null);
  const [selectedId, setSelectedId] = useState<string | null>(null);
  const [status, setStatus] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const cancelRef = useRef<HTMLButtonElement>(null);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const busyRef = useRef(false);

  useEffect(() => {
    busyRef.current = busy;
  }, [busy]);

  const closeConfirm = useCallback(() => setConfirmEntry(null), []);

  useEffect(() => {
    if (!confirmEntry) return;
    previousFocusRef.current = document.activeElement as HTMLElement | null;
    cancelRef.current?.focus();
    const onKey = (e: WindowEventMap["keydown"]) => {
      if (e.key === "Escape" && !busyRef.current) closeConfirm();
    };
    window.addEventListener("keydown", onKey);
    return () => {
      window.removeEventListener("keydown", onKey);
      previousFocusRef.current?.focus();
    };
  }, [confirmEntry, closeConfirm]);

  const loadTrash = useCallback(async (signal: AbortSignal): Promise<TrashList> => {
    const res = await fetch(`${apiBase}/api/storage/trash`, { signal });
    if (!res.ok) {
      const payload = await res.json().catch(() => ({})) as { error?: unknown; message?: unknown };
      throw new Error(trashListFailureMessage(payload, t("storage.trash.listFailed")));
    }
    const json = await res.json() as TrashList;
    const list: TrashList = {
      entries: Array.isArray(json.entries) ? json.entries : [],
      recoveryNeeded: Array.isArray(json.recoveryNeeded) ? json.recoveryNeeded : [],
    };
    onEntriesChange?.(list.entries);
    return list;
  }, [apiBase, onEntriesChange, t]);
  const trashResource = useDataSurface<TrashList>(
    `storage-trash:${apiBase}`,
    [apiBase, reloadToken],
    loadTrash,
    { isEmpty: list => trashRows(list).length === 0 },
  );
  const trashState = trashResource.state;
  const rows = useMemo(() => trashRows(trashState.data), [trashState.data]);
  const selected = rows.find(row => row.id === selectedId) ?? rows[0] ?? null;

  const runRestore = async () => {
    if (!confirmEntry) return;
    setBusy(true);
    setError(null);
    try {
      const res = await fetch(`${apiBase}/api/storage/trash/restore`, {
        method: "POST",
        headers: { "content-type": "application/json" },
        body: JSON.stringify({ id: confirmEntry.id }),
      });
      const json = await res.json().catch(() => ({})) as RestoreResult;
      if (!res.ok || !json.ok) {
        throw new Error(mapRestoreError(t, json.error, json.message));
      }
      closeConfirm();
      setStatus(restoreDoneStatus(json, t, locale));
      onDone();
    } catch (e) {
      setError(localizedCatch(e, t("storage.trash.restoreFailed")));
    } finally {
      setBusy(false);
    }
  };

  const formatWhen = (row: TrashRow) => timestampDateTimeDisplay(row.createdAt, locale);
  const chrome = quarantineChromeKind({
    showSkeleton: trashState.showSkeleton,
    showError: trashState.showError,
    rowCount: rows.length,
  });
  const listError = trashState.error instanceof Error ? trashState.error.message : t("storage.trash.listFailed");
  const split = chrome === "split";
  const modeLabel = (mode: TrashEntry["mode"] | undefined) => trashModeLabel(t, mode);

  return (
    <section className={split ? "storage-quarantine" : "storage-quarantine storage-quarantine--solo"}>
      <div className="storage-quarantine-master">
        <h3 className="storage-cleanup-section__title">{t("storage.trash.title")}</h3>
        <p className="muted storage-manual-panel__help">{t("storage.trash.help")}</p>
        <PanelNotice
          tone="ok"
          message={status}
          dismissLabel={t("common.close")}
          onDismiss={() => setStatus(null)}
        />
        <PanelNotice
          tone="err"
          message={confirmEntry ? null : error}
          dismissLabel={t("common.close")}
          onDismiss={() => setError(null)}
        />
        {trashState.refreshing && !trashState.showSkeleton && (
          <DataSurfaceStatus live={!trashState.showError}>{t("storage.trash.loading")}</DataSurfaceStatus>
        )}
        <QuarantineMasterBody
          t={t}
          locale={locale}
          chrome={chrome}
          rows={rows}
          selectedId={selected?.id ?? null}
          formatWhen={formatWhen}
          listError={listError}
          onSelect={setSelectedId}
          onRetry={() => trashResource.refresh()}
        />
      </div>
      {split && (
        <aside className="storage-quarantine-detail" aria-label={selected ? formatWhen(selected) : t("storage.trash.title")}>
          <QuarantineDetail
            t={t}
            locale={locale}
            selected={selected}
            formatWhen={formatWhen}
            modeLabel={modeLabel}
            busy={busy}
            showError={false}
            onRestore={entry => {
              setError(null);
              setConfirmEntry(entry);
            }}
          />
        </aside>
      )}
      {confirmEntry && (
        <RestoreConfirmDialog
          t={t}
          locale={locale}
          entry={confirmEntry}
          busy={busy}
          error={error}
          cancelRef={cancelRef}
          onCancel={closeConfirm}
          onConfirm={() => void runRestore()}
        />
      )}
    </section>
  );
}

function RestoreConfirmDialog({
  t, locale, entry, busy, error, cancelRef, onCancel, onConfirm,
}: {
  t: TFn;
  locale: Locale;
  entry: TrashEntry;
  busy: boolean;
  error: string | null;
  cancelRef: { current: HTMLButtonElement | null };
  onCancel: () => void;
  onConfirm: () => void;
}) {
  return (
    <div
      className="modal-overlay"
      role="dialog"
      aria-modal="true"
      aria-labelledby="storage-trash-confirm-title"
      onClick={() => !busy && onCancel()}
    >
      <div className="modal-card" onClick={e => e.stopPropagation()}>
        <h3 id="storage-trash-confirm-title">{t("storage.trash.confirmTitle")}</h3>
        <p>
          {t("storage.trash.confirmBody", {
            count: String(entry.fileCount),
            size: formatBytes(entry.bytes, locale),
            id: entry.id,
          })}
        </p>
        <p className="muted" style={{ marginTop: 8 }}>{t("storage.trash.restoreHelp")}</p>
        {error && <p style={{ marginTop: 12, color: "var(--red)" }}>{error}</p>}
        <div className="dialog-actions" style={{ marginTop: 16 }}>
          <button ref={cancelRef} type="button" className="btn btn-ghost" disabled={busy} onClick={onCancel}>
            {t("storage.trash.cancel")}
          </button>
          <button type="button" className="btn storage-action" disabled={busy} onClick={onConfirm}>
            {t("storage.trash.confirmRestore")}
          </button>
        </div>
      </div>
    </div>
  );
}