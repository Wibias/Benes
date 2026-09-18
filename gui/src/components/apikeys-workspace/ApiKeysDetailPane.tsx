/**
 * Benes dashboard source. The selected key's detail pane.
 *
 * The pane prints what the listener said and nothing more: a key row never
 * carries the secret, so the prefix cell is the server's own truncated form
 * (`api-access/key-display`), and a timestamp nobody can parse reads as an em
 * dash rather than a guessed date.
 *
 * Both sections are the same shape — a title and a list of label/value rows — so
 * the pane renders one row list per section instead of a nested component per
 * subject, and the attribution section's two non-numeric states (no attribution
 * window at all, and a key whose traffic the listener cannot attribute) are a
 * note in place of the rows.
 */
import { useT, type TFn } from "../../i18n/shared";
import { formatKeyTimestamp, redactApiKeyPrefix } from "../../api-access/key-display";
import type { ApiKeyEntry } from "../../api-access/keys";

/** One label/value line in a detail section. */
interface DetailRow {
  readonly label: string;
  readonly value: string;
  /** Server-derived values are printed as code so they read as data, not prose. */
  readonly code?: boolean;
}

/** One control in the pane's action area. */
interface PaneControl {
  readonly key: string;
  readonly label: string;
  readonly className: string;
  readonly onClick: () => void;
  readonly disabled: boolean;
  readonly ariaLabel?: string;
}

export function ApiKeysDetailPane({
  selected,
  localeTag,
  attributionSince,
  historyTruncated,
  confirmDelete,
  confirmArmed,
  deleting,
  deletingFailed,
  onConfirmDelete,
  onCancelDelete,
  onRequestDelete,
}: {
  selected: ApiKeyEntry;
  localeTag?: string;
  attributionSince?: string;
  historyTruncated?: boolean;
  confirmDelete: boolean;
  confirmArmed: boolean;
  deleting: boolean;
  deletingFailed: boolean;
  onConfirmDelete: () => void;
  onCancelDelete: () => void;
  onRequestDelete: () => void;
}) {
  const t = useT();
  const identity: DetailRow[] = [
    { label: t("api.colPrefix"), value: redactApiKeyPrefix(selected.prefix), code: true },
    { label: t("api.colCreated"), value: formatKeyTimestamp(selected.createdAt, localeTag) },
  ];
  const usage = attribution(selected, attributionSince, historyTruncated, localeTag, t);

  return (
    <div className="awi-detail awi-detail--keys">
      <div className="awi-detail-head">
        <h3 className="awi-detail-title">{selected.name}</h3>
        <span className="awi-detail-actions">
          {deleteControls(t, {
            confirmDelete,
            confirmArmed,
            deleting,
            onConfirmDelete,
            onCancelDelete,
            onRequestDelete,
          }).map(control => (
            <button
              key={control.key}
              type="button"
              className={control.className}
              onClick={control.onClick}
              disabled={control.disabled}
              aria-label={control.ariaLabel}
            >
              {control.label}
            </button>
          ))}
        </span>
      </div>
      {confirmDelete && <p className="muted awi-delete-hint">{t("api.workspace.deleteConfirm")}</p>}
      {deletingFailed && <p className="awi-delete-error" role="alert">{t("api.deleteFailed")}</p>}
      <DetailSection title={t("api.section.key")} rows={identity} />
      <DetailSection title={t("api.section.usage")} rows={usage.rows} note={usage.note} />
    </div>
  );
}
/**
 * The delete affordance's two states.
 *
 * Arming is the caller's: this only decides which controls the armed state
 * shows, and the confirm control stays disabled until the caller says the
 * arming delay has passed.
 */
function deleteControls(
  t: TFn,
  state: {
    confirmDelete: boolean;
    confirmArmed: boolean;
    deleting: boolean;
    onConfirmDelete: () => void;
    onCancelDelete: () => void;
    onRequestDelete: () => void;
  },
): PaneControl[] {
  if (!state.confirmDelete) {
    return [{
      key: "request",
      label: t("api.workspace.deleteKey"),
      className: "btn btn-danger btn-sm",
      onClick: state.onRequestDelete,
      disabled: false,
      ariaLabel: t("api.deleteAria"),
    }];
  }
  return [
    {
      key: "confirm",
      label: state.deleting ? t("api.key.deleting") : t("api.confirm"),
      className: "btn btn-danger btn-sm awi-confirm-delete",
      onClick: state.onConfirmDelete,
      disabled: !state.confirmArmed || state.deleting,
    },
    {
      key: "cancel",
      label: t("common.cancel"),
      className: "btn btn-ghost btn-sm",
      onClick: state.onCancelDelete,
      disabled: state.deleting,
    },
  ];
}

/**
 * The key's usage section: rows when the listener attributed traffic to it, and
 * the reason it cannot when it did not. `historyTruncated` changes two labels
 * because the counts then describe the retained window rather than all time.
 */
function attribution(
  selected: ApiKeyEntry,
  attributionSince: string | undefined,
  historyTruncated: boolean | undefined,
  localeTag: string | undefined,
  t: TFn,
): { rows: DetailRow[]; note?: string } {
  if (!attributionSince) return { rows: [], note: t("api.attribution.unavailableDetail") };
  if (selected.usage.kind === "ambiguous") return { rows: [], note: t("api.attribution.ambiguous") };
  const usage = selected.usage;
  return {
    rows: [
      { label: t("api.attribution.requests7d"), value: usage.requests7d.toLocaleString(localeTag) },
      {
        label: historyTruncated ? t("api.attribution.totalRequestsAvailable") : t("api.attribution.totalRequests"),
        value: usage.totalRequests.toLocaleString(localeTag),
      },
      {
        label: t("api.attribution.lastUsed"),
        value: usage.lastUsedAt
          ? formatKeyTimestamp(usage.lastUsedAt, localeTag)
          : t("api.attribution.neverUsed"),
      },
      {
        label: historyTruncated ? t("api.attribution.sinceAvailable") : t("api.attribution.since"),
        value: formatKeyTimestamp(attributionSince, localeTag),
      },
    ],
  };
}

/** One detail section: a title, then either its rows or the reason it has none. */
function DetailSection({ title, rows, note }: { title: string; rows: DetailRow[]; note?: string }) {
  return (
    <div className="awi-section">
      <h4 className="awi-section-title">{title}</h4>
      {note !== undefined ? (
        <p className="muted">{note}</p>
      ) : (
        <dl className="awi-kv">
          {rows.map(row => (
            <div className="awi-kv-row" key={row.label}>
              <dt>{row.label}</dt>
              <dd>{row.code ? <code>{row.value}</code> : row.value}</dd>
            </div>
          ))}
        </dl>
      )}
    </div>
  );
}
