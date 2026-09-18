/** Pure Models-page catalogue orchestration. */
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";
import {
  applyProviderModelContextWindows,
  buildProviderModelGroups,
  mergeProviderCatalogMeta,
  overlayPendingModelContextWindows,
  remainingModelContextWindowPatches,
  type ConfiguredProviderSummary,
  type ModelContextWindowPatch,
} from "../models-groups";
import {
  fetchModelDiscovery,
  fetchModelPresets,
  fetchSelectedModels,
  type ModelDiscoveryState,
  type ModelPresetMap,
  type ProviderModelMap,
} from "../model-visibility";
import {
  collectDisabledNamespaced,
  type ModelRow,
  type ProviderContextCapsResponse,
} from "./models-shared";
import { staleCatalogPresetOverrides } from "./models-catalog-filter";

const EMPTY_DISCOVERY: ModelDiscoveryState = { newModelPolicy: "on", providers: {} };
const DEFAULT_CONTEXT_CAP = 350_000;

export type ModelsCatalogSnapshot = {
  models: ModelRow[];
  providers: ConfiguredProviderSummary[];
  selectedModels: ProviderModelMap;
  modelPresets: ModelPresetMap;
  discovery: ModelDiscoveryState;
  disabled: string[];
  contextCaps: Record<string, number>;
  contextCapValue: number;
};

export type ModelsCatalogApplicationPlan = {
  selectedProvider: string | null;
  providers: ConfiguredProviderSummary[];
  pendingContextWrites: ModelContextWindowPatch[];
  staleContextWrites: ModelContextWindowPatch[];
};

function advertisedWindows(rows: readonly ModelRow[]): Record<string, number> {
  return Object.fromEntries(rows.map(row => [
    row.id,
    typeof row.contextWindow === "number" ? row.contextWindow : 0,
  ]));
}

export function planCatalogApplication(input: {
  models: ModelRow[];
  providers: ConfiguredProviderSummary[];
  selectedProvider: string | null;
  pendingContextWrites: readonly ModelContextWindowPatch[];
}): ModelsCatalogApplicationPlan {
  let pending = remainingModelContextWindowPatches(
    input.providers,
    input.pendingContextWrites,
  );
  let providers = overlayPendingModelContextWindows(input.providers, pending);
  const groups = buildProviderModelGroups(input.models, providers);
  const selectedProvider = input.selectedProvider !== null
    && groups.some(group => group.provider === input.selectedProvider)
    ? input.selectedProvider
    : null;
  const staleContextWrites: ModelContextWindowPatch[] = [];

  for (const group of groups) {
    const stale = staleCatalogPresetOverrides(
      group.modelContextWindows,
      advertisedWindows(group.rows),
    );
    if (!stale) continue;

    const patch: ModelContextWindowPatch = {
      provider: group.provider,
      windows: stale,
    };
    providers = applyProviderModelContextWindows(providers, patch.provider, patch.windows);
    pending = [...pending, patch];
    staleContextWrites.push(patch);
  }

  return {
    selectedProvider,
    providers,
    pendingContextWrites: pending,
    staleContextWrites,
  };
}

function contextCapValue(payload: ProviderContextCapsResponse): number {
  if (typeof payload.value === "number" && Number.isFinite(payload.value) && payload.value > 0) {
    return payload.value;
  }
  if (typeof payload.cap === "number" && Number.isFinite(payload.cap) && payload.cap > 0) {
    return payload.cap;
  }
  return DEFAULT_CONTEXT_CAP;
}

export async function fetchModelsCatalogSnapshot(
  apiBase: string,
  signal: AbortSignal,
  fetchImpl: typeof fetch = fetch,
): Promise<ModelsCatalogSnapshot> {
  const [modelsResponse, capsResponse, providersResponse, configResponse, selectedModels, modelPresets, discovery] = await Promise.all([
    fetchImpl(`${apiBase}/api/models`, { signal }),
    fetchImpl(`${apiBase}/api/provider-context-caps`, { signal }),
    fetchImpl(`${apiBase}/api/providers`, { signal }),
    fetchImpl(`${apiBase}/api/config`, { signal }),
    fetchSelectedModels(apiBase, fetchImpl, signal),
    fetchModelPresets(apiBase, fetchImpl, signal).catch(() => ({} as ModelPresetMap)),
    fetchModelDiscovery(apiBase, fetchImpl, signal).catch(() => EMPTY_DISCOVERY),
  ]);

  const [models, caps, listedProviders, config] = await Promise.all([
    readJsonOrThrow<ModelRow[]>(modelsResponse),
    readJsonOrThrow<ProviderContextCapsResponse>(capsResponse),
    readJsonOrThrow<ConfiguredProviderSummary[]>(providersResponse),
    readJsonIfOk<{ providers?: Record<string, unknown> }>(configResponse),
  ]);

  if (models === undefined || caps === undefined || listedProviders === undefined) {
    throw new Error("models payload missing");
  }
  if (signal.aborted) throw new Error("models request aborted");

  return {
    models,
    providers: mergeProviderCatalogMeta(listedProviders, config?.providers),
    selectedModels,
    modelPresets,
    discovery,
    disabled: [...collectDisabledNamespaced(models)],
    contextCaps: caps.caps ?? {},
    contextCapValue: contextCapValue(caps),
  };
}
