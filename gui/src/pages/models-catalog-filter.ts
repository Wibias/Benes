/** Benes dashboard client for the Go proxy (`internal/server`). */
/** Catalog table filters: provider, search query, and visibility. */

export type CatalogVisibilityFilter = "all" | "visible" | "hidden";

export const CATALOG_CONTEXT_PRESETS = [32_000, 64_000, 128_000, 256_000] as const;
export const CONTEXT_MAX_VALUE = "max";
export const CONTEXT_MIXED_VALUE = "mixed";

export type ProviderContextSummary =
  | { kind: "empty" }
  | { kind: "mixed" }
  | { kind: "value"; tokens: number };

export function catalogSearchShortcutKey(): "models.searchShortcut" | "models.searchShortcutWin" {
  if (typeof navigator !== "undefined" && /Mac|iPhone|iPad/.test(navigator.platform)) {
    return "models.searchShortcut";
  }
  return "models.searchShortcutWin";
}

export function filterCatalogGroups<G extends { provider: string; rows: Array<{ id: string }> }>(
  groups: G[],
  input: {
    provider: string | null;
    query: string;
    visibility: CatalogVisibilityFilter;
    providerLabel: (provider: string) => string;
    isVisible: (provider: string, model: G["rows"][number]) => boolean;
  },
): G[] {
  const query = input.query.trim().toLowerCase();
  const filtered: G[] = [];
  for (const group of groups) {
    if (input.provider !== null && group.provider !== input.provider) continue;
    const providerHit = !query
      || group.provider.toLowerCase().includes(query)
      || input.providerLabel(group.provider).toLowerCase().includes(query);
    if (group.rows.length === 0) {
      if (query && !providerHit) continue;
      if (input.visibility !== "all") continue;
      filtered.push(group);
      continue;
    }
    const rows = group.rows.filter(model => {
      if (input.visibility === "visible" && !input.isVisible(group.provider, model)) return false;
      if (input.visibility === "hidden" && input.isVisible(group.provider, model)) return false;
      if (!query) return true;
      if (providerHit) return true;
      return model.id.toLowerCase().includes(query);
    });
    if (rows.length === 0) continue;
    filtered.push(rows === group.rows ? group : { ...group, rows });
  }
  return filtered;
}

export function catalogFooterCounts<Row extends { id: string }>(
  groups: Array<{ provider: string; rows: Row[] }>,
  isVisible: (provider: string, model: Row) => boolean,
): { providers: number; models: number; visible: number } {
  let models = 0;
  let visible = 0;
  for (const group of groups) {
    models += group.rows.length;
    for (const row of group.rows) {
      if (isVisible(group.provider, row)) visible += 1;
    }
  }
  return { providers: groups.length, models, visible };
}

export function gpt56Family(id: string): boolean {
  const key = id.trim().toLowerCase();
  return key === "gpt-5.6" || key.startsWith("gpt-5.6-");
}

/**
 * Saved 32/64/128/256k windows on every model are leftover from when Max had
 * collapsed to that preset. Clear them so the catalog shows advertised sizes.
 * A single-model lowering stays; mixed presets stay.
 */
export function staleCatalogPresetOverrides(
  overrides: Record<string, number> | undefined,
  advertisedById: Record<string, number>,
): Record<string, number | null> | null {
  const ids = Object.keys(advertisedById);
  if (!overrides || ids.length === 0) return null;
  const presetSet = new Set<number>(CATALOG_CONTEXT_PRESETS);
  const first = overrides[ids[0]!];
  if (typeof first !== "number" || !presetSet.has(first)) return null;
  for (const id of ids) {
    if (overrides[id] !== first) return null;
  }
  if (!ids.some(id => (advertisedById[id] ?? 0) > first)) return null;
  return Object.fromEntries(ids.map(id => [id, null]));
}

export function modelContextDisplayValue(input: {
  override?: number | null;
  advertised?: number | null;
  providerCap: number;
}): number {
  if (typeof input.override === "number" && input.override > 0) return input.override;
  if (typeof input.advertised === "number" && input.advertised > 0) return input.advertised;
  return input.providerCap;
}

export function modelAdvertisedMax(input: {
  advertised?: number | null;
  providerCap: number;
  nativeFloor?: number;
}): number {
  const advertised = typeof input.advertised === "number" && input.advertised > 0
    ? input.advertised
    : input.providerCap;
  if (typeof input.nativeFloor === "number" && input.nativeFloor > advertised) {
    return input.nativeFloor;
  }
  return advertised;
}

export function providerContextSummary(values: readonly number[]): ProviderContextSummary {
  if (values.length === 0) return { kind: "empty" };
  const first = values[0]!;
  for (const value of values) {
    if (value !== first) return { kind: "mixed" };
  }
  return { kind: "value", tokens: first };
}

export function allowedContextPresets(advertisedMax: number): number[] {
  if (!Number.isFinite(advertisedMax) || advertisedMax <= 0) return [...CATALOG_CONTEXT_PRESETS];
  return CATALOG_CONTEXT_PRESETS.filter(preset => preset <= advertisedMax);
}

/** Lowering presets plus that model's standard window, never the advertised max (Max). */
export function modelContextChoices(input: {
  advertisedMax: number;
  standard?: number | null;
}): number[] {
  const advertisedMax = input.advertisedMax;
  const choices = new Set(allowedContextPresets(advertisedMax).filter(preset => preset < advertisedMax));
  if (typeof input.standard === "number" && input.standard > 0 && input.standard < advertisedMax) {
    choices.add(input.standard);
  }
  return [...choices].sort((a, b) => a - b);
}

export function providerContextChoices(input: {
  advertisedMaxes: readonly number[];
  standards?: readonly (number | null | undefined)[];
}): number[] {
  const advertisedMaxes = input.advertisedMaxes;
  const choices = new Set(providerAllowedContextPresets(advertisedMaxes).filter(preset =>
    advertisedMaxes.every(max => !Number.isFinite(max) || max <= 0 || preset < max),
  ));
  const standards = input.standards ?? [];
  if (standards.length === advertisedMaxes.length && standards.length > 0) {
    const first = standards[0];
    if (typeof first === "number" && first > 0
      && standards.every((value, index) => value === first && first < (advertisedMaxes[index] ?? 0))) {
      choices.add(first);
    }
  }
  return [...choices].sort((a, b) => a - b);
}

export function contextSelectValue(input: {
  display: number;
  advertisedMax: number;
  override?: number | null;
}): string {
  const hasOverride = typeof input.override === "number" && input.override > 0;
  if (!hasOverride && input.display === input.advertisedMax && input.advertisedMax > 0) {
    return CONTEXT_MAX_VALUE;
  }
  return String(input.display);
}

export function contextMaxOptionLabel(advertisedMax: number, fallback: string): string {
  if (!Number.isFinite(advertisedMax) || advertisedMax <= 0) return fallback;
  return contextOptionLabel(advertisedMax);
}

export function isAdvertisedContextSelection(raw: string, advertisedMax: number): boolean {
  if (raw === CONTEXT_MAX_VALUE) return true;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value > 0 && value === advertisedMax;
}

export function providerAllowedContextPresets(advertisedMaxes: readonly number[]): number[] {
  if (advertisedMaxes.length === 0) return [...CATALOG_CONTEXT_PRESETS];
  return CATALOG_CONTEXT_PRESETS.filter(preset =>
    advertisedMaxes.every(max => !Number.isFinite(max) || max <= 0 || preset <= max),
  );
}

export function catalogContextSelectOptions(input: {
  presets: readonly number[];
  current?: number;
  mixed?: boolean;
  mixedLabel: string;
  maxLabel: string;
}): Array<{ value: string; label: string }> {
  const options: Array<{ value: string; label: string }> = [];
  if (input.mixed) options.push({ value: CONTEXT_MIXED_VALUE, label: input.mixedLabel });
  const presetSet = new Set(input.presets);
  if (!input.mixed && typeof input.current === "number" && input.current > 0 && !presetSet.has(input.current)) {
    options.push({ value: String(input.current), label: contextOptionLabel(input.current) });
  }
  for (const preset of input.presets) {
    options.push({ value: String(preset), label: contextOptionLabel(preset) });
  }
  options.push({ value: CONTEXT_MAX_VALUE, label: input.maxLabel });
  return options;
}

/** Provider-header context write: Mixed is a no-op; Max clears every override; a preset applies to every row. */
export function providerContextBulkWindows(
  raw: string,
  modelIds: readonly string[],
): Record<string, number | null> | null {
  if (raw === CONTEXT_MIXED_VALUE || modelIds.length === 0) return null;
  if (raw === CONTEXT_MAX_VALUE) {
    return Object.fromEntries(modelIds.map(id => [id, null]));
  }
  const value = Number(raw);
  if (!Number.isSafeInteger(value) || value <= 0) return null;
  return Object.fromEntries(modelIds.map(id => [id, value]));
}

function contextOptionLabel(tokens: number): string {
  if (!Number.isFinite(tokens) || tokens <= 0) return String(tokens);
  if (tokens % 1000 !== 0) return tokens.toLocaleString();
  if (tokens >= 1_000_000) return Number((tokens / 1_000_000).toFixed(2)) + "M";
  return `${tokens / 1000}k`;
}


