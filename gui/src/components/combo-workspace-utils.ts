/**
 * Pure helpers for the Combos workspace.
 *
 * No React and no network. This module owns the rules that sit between the
 * Combos API payloads and the components that render them: resolving the picker
 * catalog, projecting provider rows, editing the ordered target list, the rail's
 * annotations, the quiet pane's copy, and the route's load notices. They live
 * here rather than inside the components because a `.tsx` pane cannot be reached
 * from a `node:test`, and every one of these rules is user-visible.
 */

import {
  COMBO_NAMESPACE,
  comboClientModelId,
  comboDisplayTitle,
  comboHasLegacyConfig,
  comboPublicModelId,
  type ComboAttentionItem,
  type ComboItem,
  type ComboTarget,
} from "../combo-workspace-data.ts";
import type { WorkspaceProvider } from "../provider-workspace/catalog";
import type { TFn } from "../i18n/shared";
import type { ModelOption, ProviderOption } from "./combo-workspace-types";

/**
 * ChatGPT/Codex passthrough keeps no `/models` catalog of its own — those GPT
 * slugs are published under the `openai` preset. The marker identifies the
 * Codex backend, so a plain OpenAI API-key row is never aliased.
 */
const FORWARD_BACKEND_MARKER = "chatgpt.com/backend-api/codex";
const FORWARD_PASSTHROUGH_PROVIDER_IDS = new Set(["openai", "chatgpt"]);

function isForwardPassthrough(provider: ProviderOption | undefined): boolean {
  if (!provider) return false;
  if (!FORWARD_PASSTHROUGH_PROVIDER_IDS.has(provider.name.toLowerCase())) return false;
  if ((provider.authMode ?? "").toLowerCase() !== "forward") return false;
  if ((provider.adapter ?? "").toLowerCase() !== "openai-responses") return false;
  const base = (provider.baseUrl ?? "").replace(/\/+$/, "");
  return base === "" || base.includes(FORWARD_BACKEND_MARKER);
}

/** Catalog provider keys whose model rows answer for `provider`. */
function catalogProviderKeys(provider: string, providers: ProviderOption[]): ReadonlySet<string> {
  const keys = new Set<string>([provider]);
  const configured = providers.find((row) => row.name === provider);
  if (provider.toLowerCase() === "chatgpt" || isForwardPassthrough(configured)) {
    keys.add("openai");
  }
  return keys;
}

/** Pickable provider rows: configured, not hidden, alphabetical. */
export function comboPickerProviders(providers: ProviderOption[]): ProviderOption[] {
  return providers
    .filter((provider) => !provider.disabled && !provider.hiddenFromPicker)
    .sort((left, right) => left.name.localeCompare(right.name));
}

/**
 * Picker rows plus the row this target already stores, so re-opening a stored
 * target never silently drops a disabled or hidden provider selection.
 */
export function comboProviderChoices(providers: ProviderOption[], current: string): ProviderOption[] {
  const pickable = comboPickerProviders(providers);
  if (!current || pickable.some((provider) => provider.name === current)) return pickable;
  const stored = providers.find((provider) => provider.name === current);
  return stored ? [...pickable, stored] : pickable;
}

/** Sorted, deduplicated model ids the catalog offers for `provider`. */
export function comboModelIds(
  models: ModelOption[],
  provider: string,
  providers: ProviderOption[],
): string[] {
  if (!provider) return [];
  const keys = catalogProviderKeys(provider, providers);
  const ids = new Set<string>();
  for (const model of models) {
    if (model.id && keys.has(model.provider)) ids.add(model.id);
  }
  return [...ids].toSorted((left, right) => left.localeCompare(right));
}

/** Ids for the model picker, keeping a stored id the live catalog no longer lists. */
export function comboModelChoices(
  models: ModelOption[],
  target: ComboTarget,
  providers: ProviderOption[],
): string[] {
  const ids = comboModelIds(models, target.provider, providers);
  const stored = target.model;
  if (!stored || ids.includes(stored)) return ids;
  return [stored, ...ids];
}

/** Model a provider switch should select: the first catalog entry, else blank. */
export function comboFirstModelId(
  models: ModelOption[],
  provider: string,
  providers: ProviderOption[],
): string {
  return comboModelIds(models, provider, providers)[0] ?? "";
}

/** Move a target to another position. Out-of-range and no-op moves return `targets`. */
export function comboMoveTarget(targets: ComboTarget[], from: number, to: number): ComboTarget[] {
  const inRange = from >= 0 && to >= 0 && from < targets.length && to < targets.length;
  if (!inRange || from === to) return targets;
  const reordered = targets.slice();
  const [moved] = reordered.splice(from, 1);
  if (!moved) return targets;
  reordered.splice(to, 0, moved);
  return reordered;
}

/** Merge one field change into a single target, leaving the other rows untouched. */
export function comboPatchTarget(
  targets: ComboTarget[],
  index: number,
  patch: Partial<ComboTarget>,
): ComboTarget[] {
  if (index < 0 || index >= targets.length) return targets;
  return targets.map((target, position) => (
    position === index ? { ...target, ...patch } : target
  ));
}

/** Drop one target. A combo keeps at least one target, so the last row stays. */
export function comboRemoveTarget(targets: ComboTarget[], index: number): ComboTarget[] {
  if (targets.length <= 1 || index < 0 || index >= targets.length) return targets;
  return targets.filter((_, position) => position !== index);
}

/** Placeholder key the model select shows before a model is chosen. */
export type ComboModelPlaceholder =
  | "cws.target.pickProviderFirst"
  | "cws.target.noModels"
  | "cws.target.pickModel";

/**
 * Everything one target row renders, resolved once per render pass. The
 * component below stays a pure projection of these rows, which keeps the
 * picker rules in one testable place.
 */
export interface ComboTargetRow {
  readonly index: number;
  readonly target: ComboTarget;
  /** React key: the UI-only stable key, else the provider/model pair. */
  readonly key: string;
  readonly providerChoices: ProviderOption[];
  readonly modelChoices: string[];
  readonly modelPlaceholder: ComboModelPlaceholder;
  readonly canMoveUp: boolean;
  readonly canMoveDown: boolean;
  readonly canRemove: boolean;
}

function modelPlaceholder(target: ComboTarget, choices: string[]): ComboModelPlaceholder {
  if (!target.provider) return "cws.target.pickProviderFirst";
  return choices.length === 0 ? "cws.target.noModels" : "cws.target.pickModel";
}

export function comboTargetRows(
  targets: ComboTarget[],
  providers: ProviderOption[],
  models: ModelOption[],
): ComboTargetRow[] {
  return targets.map((target, index) => {
    const modelChoices = comboModelChoices(models, target, providers);
    return {
      index,
      target,
      key: target.clientKey ?? `${target.provider}:${target.model}`,
      providerChoices: comboProviderChoices(providers, target.provider),
      modelChoices,
      modelPlaceholder: modelPlaceholder(target, modelChoices),
      canMoveUp: index > 0,
      canMoveDown: index < targets.length - 1,
      canRemove: targets.length > 1,
    };
  });
}

/** Row classes: the failover grid plus the active drag/drop affordances. */
export function comboTargetRowClass(dragging: boolean, dropping: boolean): string {
  const classes = ["cwi-target-row", "cwi-target-row--failover"];
  if (dragging) classes.push("cwi-target-row--dragging");
  if (dropping) classes.push("cwi-target-row--drop");
  return classes.join(" ");
}

/** Copy the quiet Combos pane shows while nothing is open. */
export interface ComboOverviewCopy {
  readonly title: "cws.selectTitle" | "cws.emptyTitle";
  readonly body: "cws.selectBody" | "cws.emptyBody";
  /** Set only on first run, where the pane also offers the Create action. */
  readonly footer: "cws.emptyFooter" | null;
}

export function combosOverviewCopy(hasCombos: boolean): ComboOverviewCopy {
  if (hasCombos) {
    return { title: "cws.selectTitle", body: "cws.selectBody", footer: null };
  }
  return { title: "cws.emptyTitle", body: "cws.emptyBody", footer: "cws.emptyFooter" };
}

/* ------------------------------------------------------------------- rail */

/** Annotation one rail row may carry, or null when the row needs no flag. */
export type ComboRailNote =
  | "cws.rail.noTargets"
  | "cws.rail.notInCatalog"
  | "cws.rail.legacyStrategy"
  | "cws.rail.unsupportedStrategy"
  | "cws.rail.legacyConfig";

/**
 * Rail annotation for one combo. Order is the contract: a broken member list
 * outranks a catalog gap, which outranks the stored strategy and any legacy
 * field the failover-only editor cannot rewrite.
 */
export function comboRailNote(
  item: ComboItem,
  attention: ComboAttentionItem["reason"] | undefined,
): ComboRailNote | null {
  if (attention === "empty-targets") return "cws.rail.noTargets";
  if (attention === "catalog-omitted") return "cws.rail.notInCatalog";
  if (item.strategy === "round-robin") return "cws.rail.legacyStrategy";
  if (item.strategy === "unsupported") return "cws.rail.unsupportedStrategy";
  return comboHasLegacyConfig(item) ? "cws.rail.legacyConfig" : null;
}

/** Target-count copy: singular for a lone member, counted otherwise. */
export type ComboTargetCountCopy =
  | { readonly key: "cws.targetCountOne" }
  | { readonly key: "cws.targetCount"; readonly count: number };

export function comboTargetCountCopy(targets: number): ComboTargetCountCopy {
  return targets === 1
    ? { key: "cws.targetCountOne" }
    : { key: "cws.targetCount", count: targets };
}

/**
 * Note the target editor shows above the list when the draft stores a strategy
 * the failover-only editor cannot rewrite. A failover draft needs no note.
 */
export function legacyTargetNote(item: ComboItem, t: TFn): string | null {
  if (item.strategy === "round-robin") return t("cws.legacy.editRoundRobin");
  if (item.strategy === "unsupported") return t("cws.strategy.unsupportedHint");
  return null;
}

/* ----------------------------------------------------------------- editor */

/**
 * Stored state a background refresh could change. The editor compares this key
 * to decide whether an incoming baseline is genuinely new; two combos that
 * differ in any of these fields must produce different keys.
 */
export function comboEditorSyncKey(baseline: ComboItem): string {
  const targets = baseline.targets
    .map((row) => `${row.provider}/${row.model}:${row.weight ?? 1}`)
    .join(",");
  const policy = [
    baseline.alias ?? "",
    baseline.nativeAlias,
    baseline.displayName ?? "",
    baseline.strategy,
    baseline.stickyLimit,
    baseline.defaultEffort,
    baseline.imageInput ?? "auto",
  ];
  return [baseline.id, ...policy, targets].join("|");
}

/**
 * The body a save sends: identity fields trimmed, a create always failover, and
 * the client-facing id resolved from the nickname/native-alias rules.
 */
export function comboSavePayload(draft: ComboItem, isCreate: boolean): ComboItem {
  const id = draft.id.trim();
  const alias = draft.alias?.trim() || null;
  return {
    ...draft,
    id,
    alias,
    displayName: draft.displayName?.trim() || null,
    strategy: isCreate ? "failover" : draft.strategy,
    model: comboClientModelId(id, alias, draft.nativeAlias),
  };
}

/** Editor heading: the stored title on edit, the pending id while creating. */
export function comboEditorTitle(
  isCreate: boolean,
  draft: ComboItem,
  baseline: ComboItem,
  createTitle: string,
): string {
  if (!isCreate) return comboDisplayTitle(baseline);
  return draft.id.trim() ? comboPublicModelId(draft.id, draft.alias) : createTitle;
}

/* ---------------------------------------------------------- picker catalog */

/** `/api/config` payload as far as the Combos load reads it. */
export type ComboWorkspaceConfigDto = {
  providers?: Record<string, WorkspaceProvider>;
};

/** Configured provider facts, keyed by provider name. */
export type ComboProviderFacts = Record<string, WorkspaceProvider>;

/** Picker rows plus the combo ids the live catalog currently lists. */
export interface ComboCatalogPage {
  models: ModelOption[];
  cataloguedComboIds: string[];
}

function asFields(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function textOf(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function stringsOf(value: unknown): string[] | undefined {
  if (!Array.isArray(value)) return undefined;
  const kept: string[] = [];
  for (const entry of value) {
    if (typeof entry === "string") kept.push(entry);
  }
  return kept;
}

/** A modality list with nothing left after trimming is not a list. */
function modalitiesOf(value: unknown): string[] | undefined {
  const listed = stringsOf(value);
  if (listed === undefined) return undefined;
  const kept = listed.map((entry) => entry.trim()).filter((entry) => entry.length > 0);
  return kept.length > 0 ? kept : undefined;
}

function modelOptionOf(
  provider: string,
  id: string,
  fields: Record<string, unknown>,
): ModelOption {
  const option: {
    provider: string;
    id: string;
    namespaced?: string;
    reasoningEfforts?: string[];
    inputModalities?: string[];
  } = { provider, id };
  if (typeof fields.namespaced === "string") option.namespaced = fields.namespaced;
  const efforts = stringsOf(fields.reasoningEfforts);
  if (efforts !== undefined) option.reasoningEfforts = efforts;
  const modalities = modalitiesOf(fields.inputModalities);
  if (modalities !== undefined) option.inputModalities = modalities;
  return option;
}

type CatalogRow = { comboId: string } | { model: ModelOption };

/** `/api/models` answers with the bare array or a `{ models: [...] }` envelope. */
function catalogRowsOf(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  const fields = asFields(payload);
  if (fields === null || !Array.isArray(fields.models)) return [];
  return fields.models;
}

/**
 * One catalog row. A row that names neither a provider nor an id contributes
 * nothing, a `combo` row marks a combo the catalog already lists, and a
 * disabled row offers no pickable model.
 */
function catalogRowOf(value: unknown): CatalogRow | null {
  const fields = asFields(value);
  if (fields === null) return null;
  const provider = textOf(fields.provider);
  const id = textOf(fields.id);
  if (provider.length === 0 || id.length === 0) return null;
  if (provider === COMBO_NAMESPACE) return { comboId: id };
  if (fields.disabled === true) return null;
  return { model: modelOptionOf(provider, id, fields) };
}

/**
 * A configured `defaultModel` stays pickable even when the live catalog omits
 * it, so every enabled provider contributes its default unless a row already
 * carries that provider/model pair.
 */
function appendConfiguredDefaults(
  models: ModelOption[],
  allProviders: ComboProviderFacts,
): void {
  for (const [name, provider] of Object.entries(allProviders)) {
    if (provider.disabled === true) continue;
    const id = textOf(provider.defaultModel);
    if (id.length === 0) continue;
    if (models.some((model) => model.provider === name && model.id === id)) continue;
    models.push({ provider: name, id, namespaced: `${name}/${id}` });
  }
}

export function parseComboWorkspaceModels(
  raw: unknown,
  allProviders: ComboProviderFacts,
): ComboCatalogPage {
  const models: ModelOption[] = [];
  const catalogued = new Set<string>();
  for (const row of catalogRowsOf(raw)) {
    const parsed = catalogRowOf(row);
    if (parsed === null) continue;
    if ("comboId" in parsed) {
      catalogued.add(parsed.comboId);
      continue;
    }
    models.push(parsed.model);
  }
  appendConfiguredDefaults(models, allProviders);
  return { models, cataloguedComboIds: [...catalogued] };
}

/**
 * One picker row per configured provider. `hiddenFromPicker` is the
 * foregrounding decision `hideRedundantChatGptForwardProviders` already made,
 * so a provider the workspace folded away stays unpickable here too.
 */
export function parseComboWorkspaceProviders(
  allProviders: ComboProviderFacts,
  visibleProviders: Record<string, unknown>,
): ProviderOption[] {
  const rows: ProviderOption[] = [];
  for (const [name, provider] of Object.entries(allProviders)) {
    rows.push({
      name,
      disabled: provider.disabled === true,
      hiddenFromPicker: !Object.hasOwn(visibleProviders, name),
      authMode: provider.authMode,
      adapter: provider.adapter,
      baseUrl: provider.baseUrl,
    });
  }
  return rows;
}

/* ------------------------------------------------------------------- page */

/** Notices the Combos route shows for its current load state. */
export type CombosPageNotices = {
  /** Cold failure: no usable data; blocking error copy. */
  loadError: string | null;
  /** Failed refresh with retained usable data; non-blocking warning. */
  staleRefreshWarning: string | null;
};

/**
 * Map a DataSurface kind to route notices. Only a cold failure blocks; a
 * refresh that kept usable data warns instead, and every other kind clears
 * both — including an in-flight refresh that follows a stale failure.
 */
export function combosPageNotices(input: {
  surfaceKind: string;
  surfaceError: unknown;
  loadFailedLabel: string;
  refreshFailedStaleLabel: string;
}): CombosPageNotices {
  if (input.surfaceKind === "failed-cold") {
    const reason = input.surfaceError instanceof Error ? input.surfaceError.message : "";
    return { loadError: reason || input.loadFailedLabel, staleRefreshWarning: null };
  }
  if (input.surfaceKind === "failed-with-stale") {
    return { loadError: null, staleRefreshWarning: input.refreshFailedStaleLabel };
  }
  return { loadError: null, staleRefreshWarning: null };
}
