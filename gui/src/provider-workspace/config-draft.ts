import { isAccountProvider, providerSupportsLiveModelDiscovery, type WorkspaceItem, type WorkspaceProvider } from "./catalog.ts";
import { type ConfigPacingRule, type ConfigPacingView } from "./config-pacing.ts";

export type ConfigurationDraftFields = {
  adapter: string;
  baseUrl: string;
  defaultModel: string;
  note: string;
  allowPrivateNetwork: boolean;
  liveModels: boolean;
  pacingEnabled: boolean;
  pacingRpm: string;
  pacingDelay: string;
  pacingModels: Record<string, ConfigPacingRule>;
};

export function configurationPacingContext(
  item: WorkspaceItem,
  apiLane: WorkspaceProvider | undefined,
): {
  openaiLogical: boolean;
  pacingSource: WorkspaceItem;
  pacingName: string;
  discoverySupported: boolean;
  savedLiveModels: boolean;
} {
  const openaiLogical = isAccountProvider(item.name, item);
  const pacingSource: WorkspaceItem = openaiLogical && apiLane
    ? { name: "openai-apikey", ...apiLane }
    : item;
  const discoverySupported = providerSupportsLiveModelDiscovery(item.name, item);
  return {
    openaiLogical,
    pacingSource,
    pacingName: pacingSource.name,
    discoverySupported,
    savedLiveModels: discoverySupported ? item.liveModels !== false : false,
  };
}

export function configurationDraftFields(
  item: WorkspaceItem,
  pacing: ConfigPacingView,
  discoverySupported: boolean,
): ConfigurationDraftFields {
  return {
    adapter: item.adapter,
    baseUrl: item.baseUrl,
    defaultModel: item.defaultModel ?? "",
    note: item.note ?? "",
    allowPrivateNetwork: item.allowPrivateNetwork ?? false,
    liveModels: discoverySupported ? item.liveModels !== false : false,
    pacingEnabled: pacing.enabled,
    pacingRpm: pacing.rpm != null ? String(pacing.rpm) : "",
    pacingDelay: pacing.minMs != null ? String(pacing.minMs) : "",
    pacingModels: pacing.models,
  };
}

export function configurationModelOptions(
  availableModels: readonly string[],
  itemDefaultModel: string | undefined,
  defaultModel: string,
): string[] {
  const set = new Set(availableModels);
  if (itemDefaultModel) set.add(itemDefaultModel);
  if (defaultModel.trim()) set.add(defaultModel.trim());
  return [...set].sort((a, b) => a.localeCompare(b));
}

export function configurationOverrideDraft(id: string, rpm: string, delay: string): boolean {
  return Boolean(id.trim() || rpm.trim() || delay.trim());
}
