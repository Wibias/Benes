/**
 * ProviderWorkspaceShell — provider rail plus the selected detail. Fleet-level
 * truth comes from /api/providers/workspace through the board reads; this module
 * owns the rail's filter state and composes the rail with the main pane.
 */
import { useCallback, useMemo, useState } from "react";
import { useT } from "../../i18n/shared";
import { type ProviderSortMode, type WorkspaceItem, type WorkspaceProvider } from "../../provider-workspace/catalog";
import { formatProviderDisplayName } from "../../provider-icons";
import { type ProviderJsonSession } from "./ProviderConfigJson";
import { ProviderWorkspaceRail } from "./shell-rail";
import {
  bumpCatalogSyncEpochs,
  duplicateProviderDisplayNames,
  filterWorkspaceSections,
  sectionsFromWorkspace,
  workspaceBoardKind,
  workspaceFilterActive,
  workspaceKpis,
  type PricingFilter,
  type StatusFilter,
  type TypeFilter,
} from "../../provider-workspace/workspace-shell";
import { ProviderWorkspaceBoard, type AddProviderIntent } from "./workspace-board-frames";
import { WorkspaceMainPane } from "./workspace-main-pane";
import type { DetailSlotData } from "./workspace-detail-slot";
import type { ProviderWorkspaceEvent } from "../../provider-workspace/workspace";
import {
  useModelSelectionRead,
  useProviderQuotaRead,
  useProviderUsageRead,
  useWorkspaceAggregateRead,
} from "./use-provider-workspace-reads";

export type { AddProviderIntent } from "./workspace-board-frames";
export type WorkspaceEvent = ProviderWorkspaceEvent;
export type { DetailSlotData };

/** Rail filter controls. The bar resets every field together. */
interface WorkspaceFilters {
  search: string;
  status: StatusFilter;
  pricing: PricingFilter;
  type: TypeFilter;
  sort: ProviderSortMode;
  open: boolean;
}

const ALL_WORKSPACE_FILTERS: WorkspaceFilters = {
  search: "",
  status: { ready: true, needsSetup: true, disabled: true },
  pricing: { free: true, paid: true },
  type: { cloud: true, local: true, selfHosted: true, login: true },
  sort: "az",
  open: false,
};

/** The rail passes either a replacement or an updater over the current value. */
function applied<T>(update: T | ((previous: T) => T), current: T): T {
  return typeof update === "function" ? (update as (previous: T) => T)(current) : update;
}

/** Names and callbacks the Providers page owns for the board. */
interface WorkspaceBoardBinding {
  providers: Record<string, WorkspaceProvider>;
  apiBase: string;
  defaultProvider: string;
  selectedName: string | null;
  onSelect: (name: string | null) => void;
}

/** Actions the rail, board head, and detail pane can trigger. */
interface WorkspaceBoardActions {
  onRemoveProvider?: (name: string) => void;
  onAddProvider: (intent?: AddProviderIntent) => void;
  onReviewProvider?: (name: string) => void;
  onNotice?: (message: string, ok?: boolean) => void;
}

export interface ProviderWorkspaceShellProps extends WorkspaceBoardBinding, WorkspaceBoardActions {
  jsonEditor?: ProviderJsonSession;
  jsonSaving?: boolean;
  modelsRefreshToken?: number;
  activeAccountNeedsReauth?: Record<string, boolean>;
  quotaRefreshEpoch?: number;
  quotaForceRefresh?: boolean;
  detail?: (item: WorkspaceItem, data: DetailSlotData) => React.ReactNode;
}

export default function ProviderWorkspaceShell(props: ProviderWorkspaceShellProps) {
  const {
    providers,
    apiBase,
    defaultProvider,
    selectedName,
    onSelect,
    onRemoveProvider,
    onAddProvider,
    onReviewProvider,
    onNotice,
    jsonEditor,
    jsonSaving = false,
    modelsRefreshToken = 0,
    activeAccountNeedsReauth,
    quotaRefreshEpoch = 0,
    quotaForceRefresh = false,
    detail,
  } = props;
  const t = useT();
  const [filters, setFilters] = useState<WorkspaceFilters>(ALL_WORKSPACE_FILTERS);
  const [railFocusName, setRailFocusName] = useState<string | null>(null);

  const providerRevision = useMemo(() => JSON.stringify(providers), [providers]);
  const fleet = useWorkspaceAggregateRead({
    apiBase,
    revisionKey: providerRevision,
    refreshToken: modelsRefreshToken,
  });
  const modelSelection = useModelSelectionRead({ apiBase, refreshToken: modelsRefreshToken });
  const usage = useProviderUsageRead(apiBase);
  const quota = useProviderQuotaRead({
    apiBase,
    forceRefresh: quotaForceRefresh,
    refreshEpoch: quotaRefreshEpoch,
    onInvalidated: fleet.reload,
  });

  /**
   * A discovery run already replaced the listener's catalog rows, and fleet
   * availability lives on the workspace aggregate, so both reads move together.
   */
  const reloadCatalog = useCallback(() => {
    bumpCatalogSyncEpochs(modelSelection.bumpEpoch, fleet.bumpEpoch);
  }, [modelSelection.bumpEpoch, fleet.bumpEpoch]);

  const patchFilters = useCallback((next: Partial<WorkspaceFilters>) => {
    setFilters(current => ({ ...current, ...next }));
  }, []);

  const sections = useMemo(() => (fleet.workspace
    ? sectionsFromWorkspace(fleet.workspace, providers, activeAccountNeedsReauth ?? {})
    : { ready: [], needsSetup: [], disabled: [] }),
  [fleet.workspace, providers, activeAccountNeedsReauth]);
  const allItems = useMemo(
    () => [...sections.ready, ...sections.needsSetup, ...sections.disabled],
    [sections],
  );
  const rowsByID = useMemo(
    () => new Map((fleet.workspace?.providers ?? []).map(row => [row.id, row] as const)),
    [fleet.workspace],
  );
  const filteredSections = useMemo(() => filterWorkspaceSections(
    sections, filters.search, filters.status, filters.pricing, filters.type, filters.sort,
  ), [sections, filters.search, filters.status, filters.pricing, filters.type, filters.sort]);
  const filterActive = workspaceFilterActive({
    statusFilter: filters.status,
    pricingFilter: filters.pricing,
    typeFilter: filters.type,
    sortMode: filters.sort,
  });
  const selectedItem = useMemo(
    () => (selectedName ? allItems.find(item => item.name === selectedName) ?? null : null),
    [selectedName, allItems],
  );
  const duplicateDisplayNames = useMemo(
    () => duplicateProviderDisplayNames(allItems, name => formatProviderDisplayName(name, t)),
    [allItems, t],
  );
  const kpis = workspaceKpis(fleet.workspace);
  const boardKind = workspaceBoardKind(fleet.workspace, allItems.length);
  const filteredItems = [...filteredSections.ready, ...filteredSections.needsSetup, ...filteredSections.disabled];
  const quotaReports = quota.reports;
  const usageTotals = usage.totals;
  const usageModels = usage.models;

  return (
    <ProviderWorkspaceBoard
      boardKind={boardKind}
      failed={fleet.failed}
      kpis={kpis}
      onAddProvider={onAddProvider}
    >
      <div className="pws-shell-container">
        <div className="pws-root">
          <ProviderWorkspaceRail
            search={filters.search}
            onSearch={value => patchFilters({ search: value })}
            filterOpen={filters.open}
            onFilterOpen={open => patchFilters({ open: applied(open, filters.open) })}
            filterActive={filterActive}
            statusFilter={filters.status}
            onStatusFilter={next => patchFilters({ status: applied(next, filters.status) })}
            pricingFilter={filters.pricing}
            onPricingFilter={next => patchFilters({ pricing: applied(next, filters.pricing) })}
            typeFilter={filters.type}
            onTypeFilter={next => patchFilters({ type: applied(next, filters.type) })}
            sortMode={filters.sort}
            onSortMode={mode => patchFilters({ sort: mode })}
            onResetFilters={() => setFilters(ALL_WORKSPACE_FILTERS)}
            sections={sections}
            filteredSections={filteredSections}
            filteredItems={filteredItems}
            selectedName={selectedItem?.name ?? null}
            onSelect={onSelect}
            onRemoveProvider={onRemoveProvider}
            defaultProvider={defaultProvider}
            modelCountFor={name => rowsByID.get(name)?.modelCount ?? modelSelection.modelCounts[name]}
            duplicateDisplayNames={duplicateDisplayNames}
            railFocusName={railFocusName}
            onRailFocus={setRailFocusName}
          />
          <main className="pws-main" aria-label={t("pws.workspaceMainAria")}>
            <WorkspaceMainPane
              jsonEditor={jsonEditor}
              jsonSaving={jsonSaving}
              selectedItem={selectedItem}
              workspace={fleet.workspace}
              detail={detail}
              rowsByID={rowsByID}
              usageTotals={usageTotals}
              usageModels={usageModels}
              quotaReports={quotaReports}
              availableModels={modelSelection.available}
              selectedModels={modelSelection.selected}
              liveModelCounts={modelSelection.liveCounts}
              modelsLoading={modelSelection.loading}
              modelsLoadFailed={modelSelection.failed}
              retryModels={reloadCatalog}
              providers={providers}
              sections={sections}
              onSelect={onSelect}
              onReviewProvider={onReviewProvider}
              apiBase={apiBase}
              onNotice={onNotice}
            />
          </main>
        </div>
      </div>
    </ProviderWorkspaceBoard>
  );
}
