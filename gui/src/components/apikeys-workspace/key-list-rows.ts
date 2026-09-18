/**
 * Benes dashboard source. The Keys panel's rows, as the table prints them.
 *
 * Derivation lives here, apart from the table, so the three attribution answers
 * are checkable without rendering: a board whose listener never reported an
 * attribution window says so, a key whose traffic the listener cannot attribute
 * says that instead, and a real window with a real zero prints zero. Collapsing
 * those three into one "unknown" is the failure this separation prevents, and a
 * missing last-use is "never used" — which is a different statement from a date
 * nobody could parse.
 *
 * A key row never carries the secret: the server's truncated prefix is printed
 * in its display form, and the listener's own spelling is kept only so a reader
 * who copied it can still find the row.
 */
import { UNKNOWN_DATE_LABEL, formatKeyTimestamp, redactApiKeyPrefix } from "../../api-access/key-display";
import type { ApiKeyEntry } from "../../api-access/keys";
import type { TFn } from "../../i18n/shared";

/** One key as a row: the printed values already resolved. */
export interface KeyRowModel {
  readonly id: string;
  readonly name: string;
  /** The server's truncated prefix, in its display form. */
  readonly prefix: string;
  readonly createdAt: string;
  readonly requests: string;
  readonly lastUsed: string;
  readonly selected: boolean;
  /** The listener's own prefix string, kept for matching only. Never printed. */
  readonly searchPrefix: string;
}

/** The rows the table prints, in the listener's order. */
export function deriveKeyRows(
  keys: ApiKeyEntry[],
  options: {
    attributionSince?: string;
    localeTag?: string;
    selectedId: string | null;
    t: TFn;
  },
): KeyRowModel[] {
  const { attributionSince, localeTag, selectedId, t } = options;
  return keys.map(key => {
    const usage = key.usage;
    const base = {
      id: key.id,
      name: key.name,
      prefix: redactApiKeyPrefix(key.prefix),
      createdAt: formatKeyTimestamp(key.createdAt, localeTag),
      selected: key.id === selectedId,
      searchPrefix: key.prefix,
    };
    // Unattributable traffic is not zero traffic: the listener either could not
    // separate this key from another, or never reported a window to count within.
    if (usage.kind === "ambiguous" || attributionSince === undefined) {
      return {
        ...base,
        requests: t(usage.kind === "ambiguous" ? "api.attribution.railAmbiguous" : "api.attribution.unavailable"),
        lastUsed: UNKNOWN_DATE_LABEL,
      };
    }
    return {
      ...base,
      requests: usage.requests7d.toLocaleString(localeTag),
      lastUsed: usage.lastUsedAt
        ? formatKeyTimestamp(usage.lastUsedAt, localeTag)
        : t("api.attribution.neverUsed"),
    };
  });
}

/** The rows a search leaves. Both prefix spellings match, because both are shown. */
export function filterKeyRows(rows: KeyRowModel[], query: string): KeyRowModel[] {
  const needle = query.trim().toLowerCase();
  if (!needle) return rows;
  return rows.filter(row =>
    row.name.toLowerCase().includes(needle)
    || row.prefix.toLowerCase().includes(needle)
    || row.searchPrefix.toLowerCase().includes(needle),
  );
}