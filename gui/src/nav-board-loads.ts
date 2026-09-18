import { readJsonOrThrow } from "./fetch-json";
import { readRequiredJson } from "./fetch-json";
import type { ModelOption, ProviderOption } from "./components/combo-workspace-types";
import { parseComboList, type ComboItem } from "./combo-workspace-data";
import {
  parseComboWorkspaceModels,
  parseComboWorkspaceProviders,
  type ComboWorkspaceConfigDto,
} from "./components/combo-workspace-utils";
import { hideRedundantChatGptForwardProviders } from "./provider-workspace/catalog";
import { writeSessionListCacheEntry } from "./session-list-cache";
import { combosWorkspaceCacheKey } from "./nav-board-resources";
import { fetchLabPageData, type LabPageData } from "./lab/lab-client";

export type DelegationModelOption = { provider: string; model: string; namespaced: string };

export type SubagentsModelsSnapshot = { available: string[]; chosen: string[] };

export type DelegationSnapshot = {
  guidanceEnabled: boolean;
  syncCodexDefaults: boolean;
  model: string;
  effort: string;
  efforts: string[];
  available: DelegationModelOption[];
};

type InjectionPayload = {
  multiAgentGuidanceEnabled?: boolean;
  syncCodexSubagentDefaults?: boolean;
  model?: string | null;
  effort?: string | null;
  efforts?: string[];
  available?: DelegationModelOption[];
};

export async function loadSubagentsModels(
  apiBase: string,
  signal?: AbortSignal,
): Promise<SubagentsModelsSnapshot> {
  const res = await fetch(`${apiBase}/api/subagent-models`, { signal });
  const response = await readJsonOrThrow<{ available?: string[]; chosen?: string[] }>(res, "subagent-models-unavailable");
  if (!response) throw new Error("subagent-models-unavailable");
  const available = response.available ?? [];
  const availableSet = new Set(available);
  return {
    available,
    chosen: (response.chosen ?? []).filter((model) => availableSet.has(model)),
  };
}

export async function loadDelegationSnapshot(
  apiBase: string,
  signal?: AbortSignal,
): Promise<DelegationSnapshot> {
  const res = await fetch(`${apiBase}/api/injection-model`, { signal });
  const data = await readRequiredJson<InjectionPayload>(res);
  return {
    // Go boolField omits-as-false; never treat a missing flag as on.
    guidanceEnabled: data.multiAgentGuidanceEnabled === true,
    syncCodexDefaults: data.syncCodexSubagentDefaults === true,
    model: data.model ?? "",
    effort: data.effort ?? "",
    efforts: Array.isArray(data.efforts) ? data.efforts : [],
    available: Array.isArray(data.available) ? data.available : [],
  };
}

export type CachedCombosPage = {
  combos: ComboItem[];
  providers: ProviderOption[];
  models: ModelOption[];
  cataloguedComboIds: string[];
};

export async function loadCombosWorkspace(
  apiBase: string,
  signal?: AbortSignal,
): Promise<CachedCombosPage> {
  const [combosRes, configRes, modelsRes] = await Promise.all([
    fetch(`${apiBase}/api/combos`, { signal }),
    fetch(`${apiBase}/api/config`, { signal }),
    fetch(`${apiBase}/api/models`, { signal }),
  ]);
  if (!combosRes.ok || !configRes.ok || !modelsRes.ok) {
    throw new Error("combo workspace load failed");
  }
  const combosJson = await combosRes.json() as unknown;
  const configJson = await configRes.json() as ComboWorkspaceConfigDto;
  const modelsRaw = await modelsRes.json() as unknown;
  const allProviders = configJson.providers ?? {};
  const parsedModels = parseComboWorkspaceModels(modelsRaw, allProviders);
  const next = {
    combos: parseComboList(combosJson),
    providers: parseComboWorkspaceProviders(
      allProviders,
      hideRedundantChatGptForwardProviders(allProviders),
    ),
    models: parsedModels.models,
    cataloguedComboIds: parsedModels.cataloguedComboIds,
  };
  writeSessionListCacheEntry(combosWorkspaceCacheKey(apiBase), next);
  return next;
}

export async function loadLabMatrixPage(apiBase: string, signal: AbortSignal): Promise<LabPageData> {
  return fetchLabPageData(apiBase, {}, signal);
}
