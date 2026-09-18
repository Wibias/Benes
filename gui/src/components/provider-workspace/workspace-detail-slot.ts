import type { WorkspaceItem, WorkspaceProvider } from "../../provider-workspace/catalog";
import type { ProviderQuotaReportView } from "../../provider-workspace/report";
import type { ProviderAvailableModels, ProviderLiveModelCounts, ProviderSelectedModels, ProviderUsageTotals } from "../../provider-workspace/model-selection";
import type { ProviderWorkspaceEvent, ProviderWorkspaceRow } from "../../provider-workspace/workspace";
import {
  eventsForProvider,
  mergeProviderValues,
  providerHasLiveModels,
  selectedItemApiLane,
  workspaceDownstream,
} from "../../provider-workspace/workspace-shell";
import type { ShellUsageModelRow as ProviderModelUsageRow } from "../../provider-workspace/workspace-shell";

export interface DetailSlotData {
  usageTotals?: ProviderUsageTotals;
  modelUsage?: ProviderModelUsageRow[];
  quotaReport?: ProviderQuotaReportView;
  availableModels: string[];
  hasLiveModels: boolean;
  selectedModels: string[];
  modelsLoading: boolean;
  modelsLoadFailed: boolean;
  onRetryModels?: () => void;
  downstream: { harnesses: number | null; routes: number; subagents: number };
  lastValidated: number | null;
  apiLane?: WorkspaceProvider;
  recentEvents?: ProviderWorkspaceEvent[];
}

export function detailSlotData(
  item: WorkspaceItem,
  row: ProviderWorkspaceRow | undefined,
  data: {
    usageTotals: Record<string, ProviderUsageTotals>;
    usageModels: Record<string, ProviderModelUsageRow[]>;
    quotaReports: Record<string, ProviderQuotaReportView>;
    availableModels: ProviderAvailableModels;
    selectedModels: ProviderSelectedModels;
    liveModelCounts: ProviderLiveModelCounts;
    modelsLoading: boolean;
    modelsLoadFailed: boolean;
    retryModels: () => void;
    providers: Record<string, WorkspaceProvider>;
    events: ProviderWorkspaceEvent[];
  },
): DetailSlotData {
  const apiLane = selectedItemApiLane(item.name, data.providers);
  return {
    usageTotals: data.usageTotals[item.name],
    modelUsage: data.usageModels[item.name],
    quotaReport: data.quotaReports[item.name],
    availableModels: mergeProviderValues(data.availableModels, item.name, data.providers),
    hasLiveModels: providerHasLiveModels(item.name, data.liveModelCounts),
    selectedModels: mergeProviderValues(data.selectedModels, item.name, data.providers),
    modelsLoading: data.modelsLoading,
    modelsLoadFailed: data.modelsLoadFailed,
    onRetryModels: data.retryModels,
    downstream: workspaceDownstream(row),
    lastValidated: row?.lastValidated ?? null,
    ...(apiLane ? { apiLane } : {}),
    recentEvents: eventsForProvider(data.events, item.name),
  };
}
