import { useState, type ReactNode } from "react";
import { useT } from "../../i18n/shared";
import { IconCheck, IconMore, IconTrash } from "../../icons";
import type { WorkspaceItem } from "../../provider-workspace/catalog";
import type { ApiKeyRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderAuthHandlers } from "./ProviderAccess";

/** Header copy keys of the Access-tab key table, in column order. */
type KeyColumnHeader =
  | "prov.access.colAlias"
  | "prov.access.colKey"
  | "prov.access.colApiDefault"
  | "prov.access.colState"
  | "prov.access.colLastUsed";

interface KeyColumn {
  id: string;
  /** Absent for the spacer and for the row-menu column. */
  header?: KeyColumnHeader;
  /** The row-menu column owns the table's own overflow control. */
  menu?: boolean;
}

const KEY_COLUMNS: KeyColumn[] = [
  { id: "alias", header: "prov.access.colAlias" },
  { id: "key", header: "prov.access.colKey" },
  { id: "spacer" },
  { id: "apiDefault", header: "prov.access.colApiDefault" },
  { id: "state", header: "prov.access.colState" },
  { id: "lastUsed", header: "prov.access.colLastUsed" },
  { id: "menu", menu: true },
];

/**
 * Draft lifecycle for one new key. The form stays open until the API accepts the
 * value, and the busy latch drops on both the accepted and the rejected path.
 */
function useKeyDraft(
  itemName: string,
  addApiKey: (name: string, key: string) => Promise<boolean>,
) {
  const [draft, setDraft] = useState("");
  const [open, setOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const close = () => {
    setOpen(false);
    setDraft("");
  };

  const submit = async () => {
    const candidate = draft.trim();
    if (candidate === "") return;
    setBusy(true);
    try {
      const accepted = await addApiKey(itemName, candidate);
      if (accepted) close();
    } finally {
      setBusy(false);
    }
  };

  return { draft, setDraft, open, busy, submit, close, begin: () => setOpen(true) };
}

function AddKeyForm({
  draft, busy, onDraft, onSubmit, onCancel,
}: {
  draft: string;
  busy: boolean;
  onDraft: (value: string) => void;
  onSubmit: () => void;
  onCancel: () => void;
}) {
  const t = useT();
  return (
    <div className="pwi-auth-add-key">
      <input className="input" type="password" value={draft} onChange={event => onDraft(event.target.value)}
        placeholder={t("modal.apiKeyPlaceholder")} autoComplete="off" disabled={busy} />
      <button type="button" className="providers-link" onClick={onSubmit} disabled={busy || draft.trim() === ""}>
        {busy ? t("pws.saving") : t("pws.addKey")}
      </button>
      <button type="button" className="providers-link providers-link--plain" onClick={onCancel}>{t("common.cancel")}</button>
    </div>
  );
}

export function ApiKeysPanel({
  item, keys, authHandlers, omitChrome, heading,
}: {
  item: WorkspaceItem;
  keys: ApiKeyRow[];
  authHandlers: ProviderAuthHandlers;
  omitChrome: boolean;
  heading?: ReactNode;
}) {
  const t = useT();
  const draft = useKeyDraft(item.name, authHandlers.onAddApiKey);

  return (
    <section className={omitChrome ? undefined : "providers-block pwi-auth-section"} aria-label={omitChrome ? undefined : t("pws.apiKeys")}>
      <ApiKeysHead omitChrome={omitChrome} addingKey={draft.open} heading={heading} onAdd={draft.begin} />
      <div className="pwi-auth-body">
        {omitChrome
          ? <ApiKeyRowsTable itemName={item.name} keys={keys} authHandlers={authHandlers} />
          : <ApiKeyRowsList itemName={item.name} keys={keys} authHandlers={authHandlers} />}
        {draft.open && (
          <AddKeyForm
            draft={draft.draft}
            busy={draft.busy}
            onDraft={draft.setDraft}
            onSubmit={() => void draft.submit()}
            onCancel={draft.close}
          />
        )}
      </div>
    </section>
  );
}

function ApiKeysHead({
  omitChrome, addingKey, onAdd, heading,
}: {
  omitChrome: boolean;
  addingKey: boolean;
  onAdd: () => void;
  heading?: ReactNode;
}) {
  const t = useT();
  const add = !addingKey ? (
    <button type="button" className={omitChrome ? "providers-link providers-link--plain" : "providers-link"} onClick={onAdd}>
      {omitChrome ? t("prov.access.addApiKey") : t("pws.addKey")}
    </button>
  ) : null;
  return (
    <div className="providers-block-head">
      {omitChrome ? (heading ?? <h4>{t("prov.access.apiKeys")}</h4>) : <h4>{t("pws.apiKeys")}</h4>}
      {omitChrome ? <div className="providers-access-head-actions">{add}</div> : add}
    </div>
  );
}

/** The Access-tab row menu: activate, relabel, or remove one key. */
function KeyRowMenu({
  itemName, row, authHandlers,
}: {
  itemName: string;
  row: ApiKeyRow;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  const entries = [
    { id: "activate", label: t("prov.accountActive"), disabled: row.active, run: () => authHandlers.onSwitchApiKey(itemName, row) },
    { id: "alias", label: t("prov.editAlias"), disabled: false, run: () => authHandlers.onEditAlias(itemName, "api-key", row.id, row.label) },
    { id: "remove", label: t("common.remove"), disabled: false, run: () => authHandlers.onRemoveApiKey(itemName, row) },
  ];
  return (
    <details className="providers-menu">
      <summary aria-label={t("prov.menu.more")}><IconMore /></summary>
      <div className="providers-menu-list">
        {entries.map(entry => (
          <button key={entry.id} type="button" disabled={entry.disabled} onClick={() => void entry.run()}>
            {entry.label}
          </button>
        ))}
      </div>
    </details>
  );
}

function KeyTableHeader() {
  const t = useT();
  return (
    <div className="providers-access-table-head" role="row">
      {KEY_COLUMNS.map(column => (
        <span
          key={column.id}
          role="columnheader"
          className={column.menu ? "providers-access-row-menu" : undefined}
          aria-label={column.menu ? t("prov.menu.more") : undefined}
        >
          {column.header ? t(column.header) : null}
        </span>
      ))}
    </div>
  );
}

function EmptyKeyTableRow() {
  return (
    <li className="providers-access-table-row providers-access-table-row--empty" role="row">
      {KEY_COLUMNS.map(column => (
        <span
          key={column.id}
          role="cell"
          className={column.id === "state" ? "providers-access-order-cell" : (column.menu ? "providers-access-row-menu" : undefined)}
        >
          {column.menu
            ? <span className="providers-menu-icon" aria-hidden="true"><IconMore /></span>
            : (column.id === "spacer" ? null : "—")}
        </span>
      ))}
    </li>
  );
}

/** One key row: label, masked value, default check, state, and the row menu. */
function KeyTableRow({
  itemName, row, authHandlers,
}: {
  itemName: string;
  row: ApiKeyRow;
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  return (
    <li className="providers-access-table-row" role="row">
      <span role="cell">{row.label || "—"}</span>
      <span role="cell"><code>{row.masked}</code></span>
      <span role="cell" />
      <span role="cell">
        {row.active
          ? <IconCheck className="providers-access-default-check" aria-label={t("prov.access.colApiDefault")} />
          : "—"}
      </span>
      <span role="cell" className="providers-access-order-cell">
        <span className="providers-pill-dot is-ok" aria-hidden="true" />
        {t("prov.access.keyActive")}
      </span>
      <span role="cell">—</span>
      <span role="cell" className="providers-access-row-menu">
        <KeyRowMenu itemName={itemName} row={row} authHandlers={authHandlers} />
      </span>
    </li>
  );
}

function ApiKeyRowsTable({
  itemName, keys, authHandlers,
}: {
  itemName: string;
  keys: ApiKeyRow[];
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  return (
    <>
      <div className="providers-access-table" role="table" aria-label={t("prov.access.apiKeys")}>
        <KeyTableHeader />
        <ul className="providers-access-table-body">
          {keys.length === 0 && <EmptyKeyTableRow />}
          {keys.map(row => (
            <KeyTableRow key={row.id} itemName={itemName} row={row} authHandlers={authHandlers} />
          ))}
        </ul>
      </div>
      <div className="providers-overview-footer" aria-hidden="true" />
    </>
  );
}

function ApiKeyRowsList({
  itemName, keys, authHandlers,
}: {
  itemName: string;
  keys: ApiKeyRow[];
  authHandlers: ProviderAuthHandlers;
}) {
  const t = useT();
  if (keys.length === 0) return null;
  return (
    <ul className="pwi-auth-list">
      {keys.map(row => {
        const label = row.label ?? row.masked;
        return (
          <li key={row.id} className={`pwi-auth-row${row.active ? " pwi-auth-row--active" : ""}`}>
            <button type="button" className="pwi-auth-row-main"
              onClick={() => void authHandlers.onSwitchApiKey(itemName, row)}
              disabled={row.active}>
              <span className={`pwi-auth-dot ${row.active ? "pwi-auth-dot--ok" : "pwi-auth-dot--off"}`} aria-hidden="true" />
              <span className="pwi-auth-row-copy">
                <span className="pwi-auth-row-label">{label}</span>
                {row.label && <code className="pwi-auth-row-secondary">{row.masked} · {t("prov.accountId")}: {row.id}</code>}
              </span>
              {row.active && <span className="badge badge-primary">{t("prov.accountActive")}</span>}
            </button>
            <button type="button" className="btn btn-ghost btn-sm"
              onClick={() => void authHandlers.onEditAlias(itemName, "api-key", row.id, row.label)}>
              {t("prov.editAlias")}
            </button>
            <button type="button" className="btn btn-ghost btn-sm pwi-auth-row-remove"
              aria-label={`${t("common.remove")} — ${label}`}
              title={`${t("common.remove")} — ${label}`}
              onClick={() => void authHandlers.onRemoveApiKey(itemName, row)}>
              <IconTrash style={{ width: 13, height: 13 }} aria-hidden="true" />
            </button>
          </li>
        );
      })}
    </ul>
  );
}
