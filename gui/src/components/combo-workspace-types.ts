/**
 * View models the Combos workspace hands between the page shell and its panes.
 *
 * Decoding, validation and the picker/catalog helpers live in
 * `combo-workspace-data.ts` and `combo-workspace-utils.ts`; this module only
 * names the shapes. Every field is read-only: the panes observe catalog rows
 * and never edit one in place, and the combos they came from are owned by the
 * `combo-workspace-data.ts` draft pipeline.
 */

import type { ComboItem } from "../combo-workspace-data";

/** Connection facts `/api/config` reports for one provider. */
export interface ProviderRoutingFacts {
  readonly authMode?: string;
  readonly adapter?: string;
  readonly baseUrl?: string;
}

/** One provider picker row: identity, pickability, and routing facts. */
export interface ProviderOption extends ProviderRoutingFacts {
  readonly hiddenFromPicker?: boolean;
  readonly disabled?: boolean;
  readonly name: string;
}

/** One model picker row, already scoped to its provider id. */
export interface ModelOption {
  readonly provider: string;
  readonly id: string;
  readonly namespaced?: string;
  readonly reasoningEfforts?: readonly string[];
  readonly inputModalities?: readonly string[];
}

/** What a `/api/combos` write reports back. */
export type ComboMutationOutcome = { ok: true } | { ok: false; error?: string };

export type ComboSaveHandler = (
  item: ComboItem,
  isCreate: boolean,
  renameFrom?: string,
) => Promise<ComboMutationOutcome>;

export type ComboRemoveHandler = (id: string) => Promise<ComboMutationOutcome>;

/** Catalog rows the workspace renders, plus which combos the catalog lists. */
export interface ComboWorkspaceData {
  combos: ComboItem[];
  providers: ProviderOption[];
  models: ModelOption[];
  /** Combo ids currently present in the live catalog (`provider === "combo"`). */
  cataloguedComboIds?: ReadonlySet<string>;
}

/** Refresh state and the write handlers the page owns. */
export interface ComboWorkspaceActions {
  loading?: boolean;
  onRefresh: () => void;
  onSave: ComboSaveHandler;
  onRemove: ComboRemoveHandler;
}

export type ComboWorkspaceProps = ComboWorkspaceData & ComboWorkspaceActions;
