/**
 * Benes dashboard source. The Keys panel's list half.
 *
 * The table is column-driven: the header row and every body row read the same
 * column list, so a column cannot appear in one and be missing from the other.
 * The rows themselves are derived in `./key-list-rows`, which is where the three
 * attribution answers are kept apart and where the prefix is reduced to its
 * display form; this file prints what that module decided.
 */
import { useMemo, useState, type ReactNode } from "react";
import { IconSearch } from "../../icons";
import { useT, type TFn } from "../../i18n/shared";
import type { ApiKeyEntry } from "../../api-access/keys";
import { deriveKeyRows, filterKeyRows, type KeyRowModel } from "./key-list-rows";

/** The list's columns, in render order. */
const KEY_COLUMNS = [
  { id: "name", headerKey: "api.colName" },
  { id: "prefix", headerKey: "api.colPrefix" },
  { id: "created", headerKey: "api.colCreated" },
  { id: "requests", headerKey: "api.colRequests7d" },
  { id: "lastUsed", headerKey: "api.colLastUsed" },
] as const;

type KeyColumnId = (typeof KEY_COLUMNS)[number]["id"];

/**
 * One body cell. The name column is the row's selection control; the rest print
 * their row value, with the server-derived prefix shown as code.
 */
function cellFor(
  column: KeyColumnId,
  row: KeyRowModel,
  busy: boolean,
  onSelect: (id: string) => void,
): ReactNode {
  if (column === "name") {
    return (
      <button
        type="button"
        className="awi-row-select"
        disabled={busy}
        aria-pressed={row.selected}
        onClick={() => onSelect(row.id)}
      >
        {row.name}
      </button>
    );
  }
  if (column === "prefix") return <code>{row.prefix}</code>;
  if (column === "created") return row.createdAt;
  if (column === "requests") return row.requests;
  return row.lastUsed;
}

export default function ApiKeysListPanel({
  keys,
  keysLoadFailed,
  attributionSince,
  localeTag,
  busy,
  selectedId,
  onSelect,
}: {
  keys: ApiKeyEntry[];
  keysLoadFailed: boolean;
  attributionSince?: string;
  localeTag?: string;
  busy: boolean;
  selectedId: string | null;
  onSelect: (id: string) => void;
}) {
  const t = useT();
  const [query, setQuery] = useState("");
  const rows = useMemo(
    () => deriveKeyRows(keys, { attributionSince, localeTag, selectedId, t }),
    [attributionSince, keys, localeTag, selectedId, t],
  );
  const visible = useMemo(() => filterKeyRows(rows, query), [query, rows]);

  return (
    <div className="awi-master" aria-busy={busy || undefined}>
      <label className="awi-search">
        <IconSearch aria-hidden="true" />
        <input
          type="search"
          value={query}
          onChange={event => setQuery(event.target.value)}
          placeholder={t("api.searchKeys")}
          aria-label={t("api.searchKeys")}
        />
      </label>
      {emptyNotice(rows.length, visible.length, keysLoadFailed, t) ?? (
        <div className="awi-table-wrap">
          <table className="awi-table">
            <thead>
              <tr>
                {KEY_COLUMNS.map(column => <th key={column.id}>{t(column.headerKey)}</th>)}
              </tr>
            </thead>
            <tbody>
              {visible.map(row => (
                <tr key={row.id} className={row.selected ? "is-selected" : undefined}>
                  {KEY_COLUMNS.map(column => (
                    <td key={column.id}>{cellFor(column.id, row, busy, onSelect)}</td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </div>
  );
}

/** The one sentence this list shows instead of a table, or `null` to show rows. */
function emptyNotice(total: number, visible: number, loadFailed: boolean, t: TFn): ReactNode | null {
  if (total === 0) return <p className="muted small">{loadFailed ? t("api.keysLoadFailed") : t("api.noKeys")}</p>;
  if (visible === 0) return <p className="muted small">{t("api.keysNoMatch")}</p>;
  return null;
}
