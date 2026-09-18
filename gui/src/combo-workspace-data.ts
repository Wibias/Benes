/**
 * Combos workspace domain layer.
 *
 * Everything the Combos route does without React and without the network:
 * decode `/api/combos` rows into drafts, check a draft against the rules the Go
 * writer enforces, serialise a draft back to the PUT body, read the write
 * result, and project a draft for the rail and the overview panes. The picker
 * and catalog helpers live beside the components in `combo-workspace-utils`.
 *
 * Two invariants drive the shape of this module:
 *
 * 1. Presence is meaning. A missing `stickyLimit` is not `1`, an absent target
 *    weight is not `1`, and a stored round-robin/unsupported strategy is
 *    carried through a PUT untouched. The GUI may not invent a value the
 *    listener did not send.
 * 2. Editing rules are ordered. `validateComboDraft` reports the first rule a
 *    draft breaks, so the rule list below is the contract, not an
 *    implementation detail.
 */

import { SUPPORTED_NATIVE_OPENAI_SLUGS } from "./lib/native-models.ts";

export type ComboStrategy = "failover" | "round-robin" | "unsupported";
export type ComboEffort = "low" | "medium" | "high" | "xhigh" | "max" | "ultra";

/** Effort ladders this release can ask a provider for, in picker order. */
const COMBO_EFFORTS: readonly ComboEffort[] = ["low", "medium", "high", "xhigh", "max", "ultra"];

/** Every wire id and client-facing nickname this workspace shows lives under `combo/`. */
const COMBO_ID_PREFIX = "combo/";

/** Provider key and alias root the Combos namespace reserves. */
export const COMBO_NAMESPACE = "combo";

export interface ComboTarget {
  provider: string;
  model: string;
  weight?: number;
  /** UI-only stable key for React lists; never sent to the API. */
  clientKey?: string;
}

export interface ComboItem {
  id: string;
  /** Wire id shown to clients, e.g. combo/free */
  model: string;
  /** Optional public model name replacing the default combo/<id> slug; null = default. */
  alias: string | null;
  /** Explicit takeover of a bare OpenAI-native alias. */
  nativeAlias: boolean;
  /** Display-only catalog label used by native aliases. */
  displayName: string | null;
  strategy: ComboStrategy;
  /**
   * Original wire strategy when `strategy` is `"unsupported"`.
   * Carried through PUT so unsupported values are not rewritten to failover.
   */
  storedStrategy?: string;
  /**
   * Presence-sensitive: omitted when the API/disk record has no stickyLimit.
   * Do not invent `1` here — the editor normalizer used 1 internally only.
   */
  stickyLimit?: number;
  defaultEffort: ComboEffort | null;
  imageInput?: "auto" | "disabled";
  targets: ComboTarget[];
}

export interface ComboSections {
  failover: ComboItem[];
  roundRobin: ComboItem[];
  /** Stored strategies the GUI cannot edit (not failover / round-robin). */
  unsupported: ComboItem[];
}

export interface ComboAttentionItem {
  id: string;
  model: string;
  reason: "few-targets" | "empty-targets" | "catalog-omitted";
}

export const COMBO_ID_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}$/;
/** One optional "/" segment, each segment id-shaped — mirrors src/combos/types.ts. */
export const COMBO_ALIAS_RE = /^[a-zA-Z0-9][a-zA-Z0-9._-]{0,63}(\/[a-zA-Z0-9][a-zA-Z0-9._-]{0,63})?$/;
const NATIVE_OPENAI_FAMILY_RE = /^(?:gpt-|o1-|o3-|o4-|codex-)/;

let comboTargetKeySequence = 0;

function nextComboTargetKey(): string {
  comboTargetKeySequence += 1;
  return `ct-${comboTargetKeySequence}`;
}

export function newComboTarget(partial: Partial<ComboTarget> = {}): ComboTarget {
  const target: ComboTarget = {
    provider: partial.provider ?? "",
    model: partial.model ?? "",
    clientKey: partial.clientKey ?? nextComboTargetKey(),
  };
  if (partial.weight !== undefined) target.weight = partial.weight;
  return target;
}

function trimmedOrEmpty(value: string | null | undefined): string {
  return typeof value === "string" ? value.trim() : "";
}

/** `/api/combos` id space. */
export function comboModelId(id: string): string {
  return `${COMBO_ID_PREFIX}${id.trim()}`;
}

/** Id clients send. A nickname is ignored unless this combo owns a native OpenAI alias. */
export function comboClientModelId(
  id: string,
  alias: string | null | undefined,
  nativeAlias = false,
): string {
  const nickname = trimmedOrEmpty(alias);
  return nativeAlias && nickname.length > 0 ? nickname : comboModelId(id);
}

/** Screen title: nickname when set, otherwise combo/<id>. */
export function comboPublicModelId(id: string, alias: string | null | undefined): string {
  const nickname = trimmedOrEmpty(alias);
  return nickname.length > 0 ? nickname : comboModelId(id);
}

/** Field labels the overview and the rail both read. */
export function comboDisplayTitle(item: ComboItem): string {
  return comboPublicModelId(item.id, item.alias);
}

/** Client-facing model id; null when identical to the display title. */
export function comboDisplaySecondaryIdentity(item: ComboItem): string | null {
  const title = comboDisplayTitle(item);
  const client = comboClientModelId(item.id, item.alias, item.nativeAlias);
  if (client === title) return null;
  return client;
}

/** Apply an alias-field edit and discard hidden native-alias metadata once it becomes ordinary. */
export function updateComboAliasDraft(item: ComboItem, rawAlias: string): ComboItem {
  const edited: ComboItem = {
    ...item,
    alias: rawAlias.trim().length > 0 ? rawAlias : null,
    model: comboPublicModelId(item.id, rawAlias),
  };
  if (!leavesNativeAliasFamily(item, rawAlias)) return edited;
  edited.nativeAlias = false;
  edited.displayName = null;
  return edited;
}

/**
 * A native alias only means something while the nickname still names the
 * OpenAI-native family: blank, namespaced, or a non-native slug all drop the
 * takeover with it.
 */
function leavesNativeAliasFamily(item: ComboItem, rawAlias: string): boolean {
  if (!item.nativeAlias) return false;
  const nickname = rawAlias.trim();
  if (nickname.length === 0 || nickname.includes("/")) return true;
  return !NATIVE_OPENAI_FAMILY_RE.test(nickname);
}

export function normalizeStrategy(raw: unknown): ComboStrategy {
  if (raw === "unsupported") return "unsupported";
  if (typeof raw !== "string") return "failover";
  const label = raw.trim();
  if (label === "round-robin") return "round-robin";
  if (label.length === 0 || label === "failover") return "failover";
  return "unsupported";
}

/** Wire strategy string preserved for PUT when the GUI kind is unsupported. */
function normalizeStoredStrategy(raw: unknown, kind: ComboStrategy): string | undefined {
  if (kind !== "unsupported") return undefined;
  const wire = trimmedOrEmpty(typeof raw === "string" ? raw : null);
  return wire.length > 0 ? wire : undefined;
}

export function wireComboStrategy(item: ComboItem): string {
  if (item.strategy !== "unsupported") return item.strategy;
  return item.storedStrategy?.trim() || "failover";
}

/** Integers the API accepts; anything else counts as absent. */
function boundedInteger(value: unknown, minimum: number, maximum: number): number | undefined {
  if (typeof value !== "number" || !Number.isInteger(value)) return undefined;
  return value >= minimum && value <= maximum ? value : undefined;
}

export function normalizeStickyLimit(raw: unknown): number | undefined {
  return boundedInteger(raw, 1, 100);
}

function normalizeWeight(raw: unknown): number | undefined {
  return boundedInteger(raw, 1, 10000);
}

function normalizeDefaultEffort(raw: unknown): ComboEffort | null {
  return COMBO_EFFORTS.find((effort) => effort === raw) ?? null;
}

function wireRecord(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function wireText(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

/** Trimmed text, or null when the wire carried nothing readable. */
function optionalWireText(value: unknown): string | null {
  const text = wireText(value);
  return text.length > 0 ? text : null;
}

function imageInputFromWire(value: unknown): "auto" | "disabled" {
  return value === "disabled" ? "disabled" : "auto";
}

/**
 * One target row. A row without both a provider and a model is not a target,
 * and a weight outside the accepted range is dropped rather than clamped.
 */
function targetFromWire(value: unknown): ComboTarget | null {
  const row = wireRecord(value);
  if (row === null) return null;
  const provider = wireText(row.provider);
  const model = wireText(row.model);
  if (provider.length === 0 || model.length === 0) return null;
  const target = newComboTarget({ provider, model });
  const weight = normalizeWeight(row.weight);
  if (weight !== undefined) target.weight = weight;
  return target;
}

function targetsFromWire(value: unknown): ComboTarget[] {
  if (!Array.isArray(value)) return [];
  const targets: ComboTarget[] = [];
  for (const candidate of value) {
    const target = targetFromWire(candidate);
    if (target !== null) targets.push(target);
  }
  return targets;
}

/** One `/api/combos` row. A row without an id cannot be addressed, so it is dropped. */
function comboFromWire(value: unknown): ComboItem | null {
  const row = wireRecord(value);
  if (row === null) return null;
  const id = wireText(row.id);
  if (id.length === 0) return null;
  const alias = optionalWireText(row.alias);
  const explicitModel = wireText(row.model);
  const strategy = normalizeStrategy(row.strategy);
  const item: ComboItem = {
    id,
    model: explicitModel.length > 0 ? explicitModel : comboPublicModelId(id, alias),
    alias,
    nativeAlias: row.nativeAlias === true,
    displayName: optionalWireText(row.displayName),
    strategy,
    defaultEffort: normalizeDefaultEffort(row.defaultEffort),
    imageInput: imageInputFromWire(row.imageInput),
    targets: targetsFromWire(row.targets),
  };
  const stored = normalizeStoredStrategy(row.strategy, strategy);
  if (stored !== undefined) item.storedStrategy = stored;
  const stickyLimit = normalizeStickyLimit(row.stickyLimit);
  if (stickyLimit !== undefined) item.stickyLimit = stickyLimit;
  return item;
}

/** Ids sort case-insensitively so the rail order does not depend on case. */
function compareComboIds(left: ComboItem, right: ComboItem): number {
  return left.id.localeCompare(right.id, undefined, { sensitivity: "base" });
}

export function parseComboList(payload: unknown): ComboItem[] {
  const rows = wireRecord(payload)?.combos;
  if (!Array.isArray(rows)) return [];
  const items: ComboItem[] = [];
  for (const row of rows) {
    const item = comboFromWire(row);
    if (item !== null) items.push(item);
  }
  return items.sort(compareComboIds);
}

const SECTION_BY_STRATEGY = {
  failover: "failover",
  "round-robin": "roundRobin",
  unsupported: "unsupported",
} as const;

export function groupCombos(items: ComboItem[]): ComboSections {
  const sections: ComboSections = { failover: [], roundRobin: [], unsupported: [] };
  for (const item of items) sections[SECTION_BY_STRATEGY[item.strategy]].push(item);
  return sections;
}

/** Background baseline sync adopts only when the editor is clean (Routing Profiles contract). */
export function shouldAdoptComboBaselineOnSync(dirty: boolean): boolean {
  return !dirty;
}

/**
 * Format explicitly stored legacy weights with 1-based target positions.
 * Absence stays absence — never invents weight 1.
 * Example: `#2: 3` or `#1: 2, #3: 1`.
 */
export function formatLegacySparseWeights(
  targets: ReadonlyArray<{ weight?: number }>,
): string | null {
  const entries = targets
    .map((target, index) => ({ position: index + 1, weight: target.weight }))
    .filter((entry) => entry.weight !== undefined && entry.weight > 0)
    .map((entry) => `#${entry.position}: ${entry.weight}`);
  return entries.length > 0 ? entries.join(", ") : null;
}

function isWeightedTarget(target: ComboTarget): boolean {
  return target.weight !== undefined && target.weight > 0;
}

/** Any stored field the failover-only editor cannot express. */
export function comboHasLegacyConfig(item: ComboItem): boolean {
  if (item.strategy !== "failover") return true;
  if (item.stickyLimit !== undefined) return true;
  if (item.defaultEffort !== null) return true;
  if (item.imageInput === "disabled") return true;
  return item.targets.some(isWeightedTarget);
}

/**
 * Rail search: Combo identity only (id, nickname/public label, client model).
 * Hidden target provider/model strings are not searchable.
 */
export function filterCombos(items: ComboItem[], query: string): ComboItem[] {
  const needle = query.trim().toLowerCase();
  if (needle.length === 0) return items;
  return items.filter((item) => comboIdentityHaystack(item).includes(needle));
}

/** Identity fields joined so one `includes` covers every searched field. */
function comboIdentityHaystack(item: ComboItem): string {
  const fields = [
    item.id,
    item.model,
    item.alias ?? "",
    comboDisplayTitle(item),
    comboClientModelId(item.id, item.alias, item.nativeAlias),
  ];
  return fields.join("\u0000").toLowerCase();
}

/** Combo ids the live catalog lists; every other combo is flagged when set. */
export interface ComboAttentionScope {
  cataloguedComboIds?: ReadonlySet<string>;
}

function attentionReason(
  combo: ComboItem,
  scope: ComboAttentionScope,
): ComboAttentionItem["reason"] | null {
  if (combo.targets.length === 0) return "empty-targets";
  const catalogued = scope.cataloguedComboIds;
  if (catalogued === undefined) return null;
  return catalogued.has(combo.id) ? null : "catalog-omitted";
}

/**
 * Combos the rail should annotate. A single target is valid (a stable virtual
 * id, a nickname, or a native takeover), so only a missing target list — or a
 * combo the live `/api/models` catalog leaves out — is reported, never a
 * guessed modality or context failure.
 */
export function buildComboAttention(
  combos: ComboItem[],
  scope: ComboAttentionScope = {},
): ComboAttentionItem[] {
  const flagged: ComboAttentionItem[] = [];
  for (const combo of combos) {
    const reason = attentionReason(combo, scope);
    if (reason === null) continue;
    flagged.push({ id: combo.id, model: combo.model, reason });
  }
  return flagged;
}

function sameTarget(left: ComboTarget, right: ComboTarget): boolean {
  return left.provider === right.provider
    && left.model === right.model
    && left.weight === right.weight;
}

function sameTargets(left: readonly ComboTarget[], right: readonly ComboTarget[]): boolean {
  return left.length === right.length
    && left.every((target, index) => sameTarget(target, right[index]!));
}

/**
 * Ordered fields beside the target list. Presence-sensitive: an absent
 * `stickyLimit` is distinct from `1`, which is why the comparison is written
 * out rather than folded into a signature string.
 */
function sameIdentity(left: ComboItem, right: ComboItem): boolean {
  return left.id === right.id
    && left.alias === right.alias
    && left.nativeAlias === right.nativeAlias
    && left.displayName === right.displayName
    && left.strategy === right.strategy
    && left.storedStrategy === right.storedStrategy
    && left.stickyLimit === right.stickyLimit
    && left.defaultEffort === right.defaultEffort
    && (left.imageInput ?? "auto") === (right.imageInput ?? "auto");
}

export function draftEquals(left: ComboItem, right: ComboItem): boolean {
  return sameIdentity(left, right) && sameTargets(left.targets, right.targets);
}

type ComboWireTarget = { provider: string; model: string; weight?: number };

type ComboWireBody = {
  targets: ComboWireTarget[];
  strategy: string;
  stickyLimit?: number;
  defaultEffort: ComboEffort | null;
  imageInput?: "disabled";
  alias?: string;
  nativeAlias?: true;
  displayName?: string;
};

export type ComboPutBody = {
  id: string;
  renameFrom?: string;
  combo: ComboWireBody;
};

function wireTarget(target: ComboTarget): ComboWireTarget {
  const row: ComboWireTarget = {
    provider: target.provider.trim(),
    model: target.model.trim(),
  };
  if (target.weight !== undefined) row.weight = target.weight;
  return row;
}

/**
 * Only fields the GUI actually holds are emitted. An untouched combo must not
 * gain a stickyLimit, a weight, an alias or a display name on save.
 */
function wireCombo(item: ComboItem): ComboWireBody {
  const combo: ComboWireBody = {
    targets: item.targets.map(wireTarget),
    strategy: wireComboStrategy(item),
    defaultEffort: item.defaultEffort,
  };
  if (item.stickyLimit !== undefined) combo.stickyLimit = item.stickyLimit;
  if (item.imageInput === "disabled") combo.imageInput = "disabled";
  const alias = item.alias?.trim() ?? "";
  if (alias.length > 0) combo.alias = alias;
  if (item.nativeAlias) combo.nativeAlias = true;
  const displayName = item.displayName?.trim() ?? "";
  if (displayName.length > 0) combo.displayName = displayName;
  return combo;
}

export function toPutBody(item: ComboItem, options: { renameFrom?: string } = {}): ComboPutBody {
  const body: ComboPutBody = { id: item.id.trim(), combo: wireCombo(item) };
  if (options.renameFrom) body.renameFrom = options.renameFrom;
  return body;
}

/** `/api/combos` write result: an explicit failure wins over a success flag. */
export function comboMutationSucceeded(
  data: unknown,
): { ok: true } | { ok: false; error?: string } {
  const record = wireRecord(data);
  if (record === null) return { ok: false };
  const error = record.error;
  if (typeof error === "string" && error.trim().length > 0) return { ok: false, error };
  return record.success === true ? { ok: true } : { ok: false };
}

export type ComboDraftError =
  | "missingId"
  | "invalidId"
  | "duplicateId"
  | "reservedNamespace"
  | "providerCollision"
  | "invalidAlias"
  | "aliasReservedNamespace"
  | "aliasNativeFamily"
  | "unsupportedNativeAlias"
  | "missingNativeAliasDisplayName"
  | "invalidDisplayName"
  | "duplicateAlias"
  | "noTargets"
  | "incompleteTarget"
  | "unknownProvider"
  | "duplicateTarget"
  | "invalidStickyLimit"
  | "invalidWeight"
  | "noEnabledTarget";

type ComboDraftOptions = {
  existingIds: readonly string[];
  /** Aliases already taken by OTHER combos (callers exclude the edited combo). */
  existingAliases?: readonly string[];
  isCreate: boolean;
  providers: Readonly<Record<string, { disabled?: boolean }>>;
};

type DraftRule = (item: ComboItem, options: ComboDraftOptions) => ComboDraftError | null;

function requireAddressableId(item: ComboItem, options: ComboDraftOptions): ComboDraftError | null {
  const id = item.id.trim();
  if (id.length === 0) return "missingId";
  if (!COMBO_ID_RE.test(id)) return "invalidId";
  if (options.existingIds.includes(id)) return "duplicateId";
  if (Object.hasOwn(options.providers, COMBO_NAMESPACE)) return "reservedNamespace";
  if (Object.hasOwn(options.providers, id)) return "providerCollision";
  return null;
}

function requireUsableAlias(item: ComboItem, options: ComboDraftOptions): ComboDraftError | null {
  const nickname = trimmedOrEmpty(item.alias);
  if (nickname.length === 0) return null;
  if (!COMBO_ALIAS_RE.test(nickname)) return "invalidAlias";
  if (nickname === COMBO_NAMESPACE || nickname.startsWith(`${COMBO_NAMESPACE}/`)) {
    return "aliasReservedNamespace";
  }
  const bareNativeSlug = !nickname.includes("/") && NATIVE_OPENAI_FAMILY_RE.test(nickname);
  if (bareNativeSlug && !item.nativeAlias) return "aliasNativeFamily";
  if ((options.existingAliases ?? []).includes(nickname)) return "duplicateAlias";
  return null;
}

function hasControlCharacter(value: string): boolean {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

function requireSaneDisplayName(item: ComboItem): ComboDraftError | null {
  const label = item.displayName;
  if (label !== null && (label.trim().length > 128 || hasControlCharacter(label))) {
    return "invalidDisplayName";
  }
  if (!item.nativeAlias) return null;
  if (!SUPPORTED_NATIVE_OPENAI_SLUGS.has(trimmedOrEmpty(item.alias))) return "unsupportedNativeAlias";
  return label === null || label.trim().length === 0 ? "missingNativeAliasDisplayName" : null;
}

function requireResolvableTargets(
  item: ComboItem,
  options: ComboDraftOptions,
): ComboDraftError | null {
  if (item.targets.length === 0) return "noTargets";
  for (const target of item.targets) {
    const provider = target.provider.trim();
    if (provider.length === 0 || target.model.trim().length === 0) return "incompleteTarget";
    if (!Object.hasOwn(options.providers, provider)) return "unknownProvider";
  }
  return null;
}

function requireDistinctTargets(item: ComboItem): ComboDraftError | null {
  const seen = new Set<string>();
  for (const target of item.targets) {
    const key = `${target.provider.trim()}/${target.model.trim()}`;
    if (seen.has(key)) return "duplicateTarget";
    seen.add(key);
  }
  return null;
}

/**
 * Legacy ranges only matter while the draft still stores a round-robin
 * strategy; a failover draft carries no weight and no sticky window.
 */
function requireLegacyRanges(item: ComboItem): ComboDraftError | null {
  if (item.strategy !== "round-robin") return null;
  if (item.stickyLimit !== undefined && boundedInteger(item.stickyLimit, 1, 100) === undefined) {
    return "invalidStickyLimit";
  }
  for (const target of item.targets) {
    if (target.weight === undefined) continue;
    if (boundedInteger(target.weight, 1, 10000) === undefined) return "invalidWeight";
  }
  return null;
}

function requireEnabledTarget(
  item: ComboItem,
  options: ComboDraftOptions,
): ComboDraftError | null {
  for (const target of item.targets) {
    const configured = options.providers[target.provider.trim()];
    if (configured?.disabled !== true) return null;
  }
  return "noEnabledTarget";
}

/** Evaluated in order; the first rule a draft breaks is the reported error. */
const DRAFT_RULES: readonly DraftRule[] = [
  requireAddressableId,
  requireUsableAlias,
  requireSaneDisplayName,
  requireResolvableTargets,
  requireDistinctTargets,
  requireLegacyRanges,
  requireEnabledTarget,
];

export function validateComboDraft(
  item: ComboItem,
  options: ComboDraftOptions,
): ComboDraftError | null {
  for (const rule of DRAFT_RULES) {
    const error = rule(item, options);
    if (error !== null) return error;
  }
  return null;
}

/**
 * The stored half a brand-new combo starts from. Kept separate from the target
 * list because a create begins with one empty target and no stored nicknames.
 */
const EMPTY_COMBO_POLICY: Pick<ComboItem, "strategy" | "defaultEffort" | "imageInput"> = {
  strategy: "failover",
  defaultEffort: null,
  imageInput: "auto",
};

export function emptyDraft(id = ""): ComboItem {
  return {
    id,
    model: id.length > 0 ? comboModelId(id) : COMBO_ID_PREFIX,
    alias: null,
    nativeAlias: false,
    displayName: null,
    ...EMPTY_COMBO_POLICY,
    targets: [newComboTarget()],
  };
}

