/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { ComboItem } from "../combo-workspace-data";

/** Ids and aliases of every combo except the one currently being edited. */
export function otherComboIdentity(
  combos: readonly ComboItem[],
  baselineId: string,
): { ids: string[]; aliases: string[] } {
  const ids: string[] = [];
  const aliases: string[] = [];
  for (const combo of combos) {
    if (combo.id === baselineId) continue;
    ids.push(combo.id);
    if (combo.alias) aliases.push(combo.alias);
  }
  return { ids, aliases };
}

export function followComboAfterSave(
  saved: ComboItem,
  baselineId: string,
): { selectedId: string; localBaseline: ComboItem | null } {
  if (saved.id !== baselineId) {
    return { selectedId: saved.id, localBaseline: null };
  }
  return { selectedId: baselineId, localBaseline: saved };
}
