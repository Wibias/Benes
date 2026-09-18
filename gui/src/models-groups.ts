/** Benes dashboard client for the Go proxy (`internal/server`). */

export type ProviderDiscoverySummary =
  | { status: "ok" }
  | { status: "failed"; reason: "http"; httpStatus: number }
  | {
      status: "failed";
      reason: "blocked" | "invalid_response" | "network" | "provider";
      httpStatus?: never;
    };

export interface ConfiguredProviderSummary {
  name: string;
  authMode?: string;
  disabled?: boolean;
  liveModels?: boolean;
  models?: string[];
  contextWindow?: number;
  modelContextWindows?: Record<string, number>;
  discovery?: ProviderDiscoverySummary;
}

export interface ProviderModelGroup<Row> {
  provider: string;
  rows: Row[];
  native: boolean;
  nativeProviderGroup: boolean;
  liveModels: boolean;
  catalogProbe: boolean;
  configuredModels: string[];
  contextWindow?: number;
  modelContextWindows?: Record<string, number>;
  discovery?: ProviderDiscoverySummary;
}

export type ModelContextWindowPatch = {
  provider: string;
  windows: Record<string, number | null>;
};

type ProviderConfigOverlay = {
  authMode?: string;
  liveModels?: boolean;
  modelContextWindows?: Record<string, number>;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function readContextWindows(value: unknown): Record<string, number> | undefined {
  const record = asRecord(value);
  if (!record) return undefined;

  const accepted: Record<string, number> = {};
  for (const [rawId, rawTokens] of Object.entries(record)) {
    const id = rawId.trim();
    if (!id) continue;
    if (typeof rawTokens !== "number" || !Number.isFinite(rawTokens) || rawTokens <= 0) continue;
    accepted[id] = rawTokens;
  }

  return Object.keys(accepted).length === 0 ? undefined : accepted;
}

function readProviderOverlay(value: unknown): ProviderConfigOverlay | null {
  const record = asRecord(value);
  if (!record) return null;

  const overlay: ProviderConfigOverlay = {};
  if (typeof record.authMode === "string") overlay.authMode = record.authMode;
  if (typeof record.liveModels === "boolean") overlay.liveModels = record.liveModels;

  const windows = readContextWindows(record.modelContextWindows);
  if (windows) overlay.modelContextWindows = windows;
  return overlay;
}

export function catalogProbeSupported(authMode: string | undefined, liveModels: boolean): boolean {
  if (!liveModels) return false;
  const mode = typeof authMode === "string" ? authMode.trim().toLowerCase() : "";
  return mode !== "forward";
}

export function mergeProviderCatalogMeta(
  listed: ConfiguredProviderSummary[],
  configProviders: Record<string, unknown> | undefined,
): ConfiguredProviderSummary[] {
  if (!configProviders) return listed;

  return listed.map(provider => {
    const overlay = readProviderOverlay(configProviders[provider.name]);
    if (!overlay) return provider;
    return { ...provider, ...overlay };
  });
}

function applyWindowPatch(
  provider: ConfiguredProviderSummary,
  windows: Record<string, number | null>,
): ConfiguredProviderSummary {
  const next = { ...provider.modelContextWindows };

  for (const [rawId, value] of Object.entries(windows)) {
    const id = rawId.trim();
    if (!id) continue;

    if (value == null) {
      delete next[id];
      continue;
    }
    if (Number.isFinite(value) && value > 0) next[id] = value;
  }

  return {
    ...provider,
    modelContextWindows: Object.keys(next).length === 0 ? undefined : next,
  };
}

export function applyProviderModelContextWindows(
  providers: ConfiguredProviderSummary[],
  provider: string,
  windows: Record<string, number | null>,
): ConfiguredProviderSummary[] {
  return providers.map(item => (
    item.name === provider ? applyWindowPatch(item, windows) : item
  ));
}

export function overlayPendingModelContextWindows(
  providers: ConfiguredProviderSummary[],
  pending: readonly ModelContextWindowPatch[],
): ConfiguredProviderSummary[] {
  return pending.reduce<ConfiguredProviderSummary[]>(
    (state, patch) => applyProviderModelContextWindows(state, patch.provider, patch.windows),
    providers,
  );
}

export function modelContextWindowsPatchApplied(
  providers: ConfiguredProviderSummary[],
  provider: string,
  windows: Record<string, number | null>,
): boolean {
  const row = providers.find(item => item.name === provider);
  if (!row) return false;
  const current = row.modelContextWindows ?? {};

  return Object.entries(windows).every(([id, value]) => (
    value == null ? current[id] == null : current[id] === value
  ));
}

export function remainingModelContextWindowPatches<T extends ModelContextWindowPatch>(
  providers: ConfiguredProviderSummary[],
  pending: readonly T[],
): T[] {
  return pending.filter(patch => (
    !modelContextWindowsPatchApplied(providers, patch.provider, patch.windows)
  ));
}

function projectProviderGroup<Row extends { provider: string; native?: boolean }>(
  provider: string,
  rows: Row[],
  configured: ConfiguredProviderSummary | undefined,
): ProviderModelGroup<Row> {
  const nativeProviderGroup = rows.some(row => row.native === true);
  const liveModels = configured?.liveModels !== false;

  return {
    provider,
    rows,
    native: rows.length > 0 && rows.every(row => row.native === true),
    nativeProviderGroup,
    liveModels,
    catalogProbe: !nativeProviderGroup && catalogProbeSupported(configured?.authMode, liveModels),
    configuredModels: configured?.models ?? [],
    contextWindow: configured?.contextWindow,
    modelContextWindows: configured?.modelContextWindows,
    discovery: configured?.discovery,
  };
}

function compareProviderGroups<Row>(a: ProviderModelGroup<Row>, b: ProviderModelGroup<Row>): number {
  if (a.nativeProviderGroup !== b.nativeProviderGroup) {
    return a.nativeProviderGroup ? -1 : 1;
  }
  return a.provider.localeCompare(b.provider);
}

export function buildProviderModelGroups<Row extends { provider: string; native?: boolean }>(
  rows: Row[],
  providers: ConfiguredProviderSummary[],
): ProviderModelGroup<Row>[] {
  const disabled = new Set(
    providers.filter(provider => provider.disabled === true).map(provider => provider.name),
  );
  const configured = Object.fromEntries(
    providers.map(provider => [provider.name, provider] as const),
  ) as Record<string, ConfiguredProviderSummary>;
  const rowsByProvider = rows.reduce<Record<string, Row[]>>((acc, row) => {
    if (disabled.has(row.provider)) return acc;
    (acc[row.provider] ??= []).push(row);
    return acc;
  }, {});

  for (const provider of providers) {
    if (provider.disabled === true) continue;
    rowsByProvider[provider.name] ??= [];
  }

  return Object.entries(rowsByProvider)
    .map(([provider, providerRows]) => (
      projectProviderGroup(provider, providerRows, configured[provider])
    ))
    .sort(compareProviderGroups);
}
