import type { ReactNode } from "react";
import { useT } from "../../i18n/shared";
import type { WorkspaceItem, WorkspaceProvider } from "../../provider-workspace/catalog";
import type { ProviderWorkspaceAggregate, ProviderWorkspaceRow } from "../../provider-workspace/workspace";
import { workspaceMainKind } from "../../provider-workspace/workspace-shell";
import type { ProviderAvailableModels, ProviderLiveModelCounts, ProviderSelectedModels, ProviderUsageTotals } from "../../provider-workspace/model-selection";
import type { ProviderQuotaReportView } from "../../provider-workspace/report";
import type { ShellUsageModelRow as ProviderModelUsageRow } from "../../provider-workspace/workspace-shell";
import ProviderOverviewDashboard from "./ProviderOverviewDashboard";
import ProviderConfigJson, { type ProviderJsonSession } from "./ProviderConfigJson";
import { detailSlotData, type DetailSlotData } from "./workspace-detail-slot";

export function WorkspaceMainPane({
  jsonEditor,
  jsonSaving,
  selectedItem,
  workspace,
  detail,
  rowsByID,
  usageTotals,
  usageModels,
  quotaReports,
  availableModels,
  selectedModels,
  liveModelCounts,
  modelsLoading,
  modelsLoadFailed,
  retryModels,
  providers,
  sections,
  onSelect,
  onReviewProvider,
  apiBase,
  onNotice,
}: {
  jsonEditor?: ProviderJsonSession;
  jsonSaving?: boolean;
  selectedItem: WorkspaceItem | null;
  workspace: ProviderWorkspaceAggregate | null;
  detail?: (item: WorkspaceItem, data: DetailSlotData) => ReactNode;
  rowsByID: Map<string, ProviderWorkspaceRow>;
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
  sections: { ready: WorkspaceItem[]; needsSetup: WorkspaceItem[]; disabled: WorkspaceItem[] };
  onSelect: (name: string | null) => void;
  onReviewProvider?: (name: string) => void;
  apiBase: string;
  onNotice?: (message: string, ok?: boolean) => void;
}) {
  const t = useT();
  const kind = workspaceMainKind(Boolean(jsonEditor?.visible), Boolean(selectedItem), Boolean(workspace));
  if (kind === "json" && jsonEditor) {
    return (
      <ProviderConfigJson
        session={jsonEditor}
        providerName={t("nav.providers")}
        saving={jsonSaving ?? false}
        commit={() => { void jsonEditor.save(); }}
      />
    );
  }
  if (kind === "detail" && selectedItem) {
    return detail?.(selectedItem, detailSlotData(selectedItem, rowsByID.get(selectedItem.name), {
      usageTotals,
      usageModels,
      quotaReports,
      availableModels,
      selectedModels,
      liveModelCounts,
      modelsLoading,
      modelsLoadFailed,
      retryModels,
      providers,
      events: workspace?.recentEvents ?? [],
    })) ?? null;
  }
  if (kind === "dashboard" && workspace) {
    return (
      <ProviderOverviewDashboard
        sections={sections}
        attention={workspace.attention}
        availability={workspace.availability}
        downstream={workspace.downstream}
        onSelectProvider={name => onSelect(name)}
        onReviewProvider={name => (onReviewProvider ?? onSelect)(name)}
        onRetryModels={retryModels}
        modelsLoading={modelsLoading}
        recentEvents={workspace.recentEvents}
        apiBase={apiBase}
        onNotice={onNotice}
      />
    );
  }
  return null;
}
