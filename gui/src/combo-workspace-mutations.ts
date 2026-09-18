/**
 * Combos write flow.
 *
 * The route renders React, and a `.tsx` page cannot be reached from a
 * `node:test`. The request shapes, the outcome decoding and the notice copy are
 * not React, so they live here: the page keeps the surface wiring, the cache
 * seeding and the rendering, and asks this module what to send and what to say.
 */

import {
  comboModelId,
  comboMutationSucceeded,
  toPutBody,
  type ComboItem,
} from "./combo-workspace-data.ts";
import type { TFn } from "./i18n/shared";

/** One request the workspace issues, ready for `fetch`. */
export interface ComboWrite {
  readonly url: string;
  readonly init: RequestInit;
}

/** A write that failed always says why; the route shows that reason. */
export type ComboWriteResult = { ok: true } | { ok: false; error: string };

export type ComboWriteFailureKey = "cws.saveFailed" | "cws.removeFailed";

/** `PUT /api/combos` — create, save, or rename one combo. */
export function comboPut(apiBase: string, item: ComboItem, renameFrom?: string): ComboWrite {
  return {
    url: `${apiBase}/api/combos`,
    init: {
      method: "PUT",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(toPutBody(item, renameFrom ? { renameFrom } : {})),
    },
  };
}

/** `DELETE /api/combos?id=…` */
export function comboDelete(apiBase: string, id: string): ComboWrite {
  return {
    url: `${apiBase}/api/combos?id=${encodeURIComponent(id)}`,
    init: { method: "DELETE" },
  };
}

/**
 * A combo write answers with JSON whether it succeeded or refused, so a
 * non-ok response is read as well: the refusal body carries the reason the
 * editor shows.
 */
async function readComboWrite(res: Response): Promise<unknown> {
  if (res.ok) return res.json() as Promise<unknown>;
  return res.json().catch(() => null) as Promise<unknown>;
}

/**
 * Issue one write and reduce it to the outcome the editor branches on. The
 * listener's own refusal reason wins over the route's fallback copy.
 */
export async function runComboWrite(
  write: ComboWrite,
  t: TFn,
  failureKey: ComboWriteFailureKey,
): Promise<ComboWriteResult> {
  try {
    const res = await fetch(write.url, write.init);
    const outcome = comboMutationSucceeded(await readComboWrite(res));
    if (res.ok && outcome.ok) return { ok: true };
    const reason = !outcome.ok && outcome.error ? outcome.error : t(failureKey);
    return { ok: false, error: reason };
  } catch {
    return { ok: false, error: t(failureKey) };
  }
}

/** Copy for a save that landed: a rename names both ids, a create names its id. */
export function comboWriteNotice(
  t: TFn,
  item: ComboItem,
  isCreate: boolean,
  renameFrom?: string,
): string {
  if (renameFrom) return t("cws.renamed", { from: comboModelId(renameFrom), to: item.model });
  return isCreate ? t("cws.created", { model: item.model }) : t("cws.saved");
}

export function comboRemoveNotice(t: TFn, id: string): string {
  return t("cws.removed", { id });
}
