import {
  isFreeProvider,
  providerKind,
  providerTier,
  sortWorkspaceItems,
  type ProviderSortMode,
  type WorkspaceItem,
  type WorkspaceProvider,
  type WorkspaceSections,
} from "./catalog.ts";
import type { ProviderQuotaReportView } from "./report.ts";
import type { ProviderWorkspaceAggregate, ProviderWorkspaceEvent, ProviderWorkspaceRow } from "./workspace.ts";

export type StatusFilter = { ready: boolean; needsSetup: boolean; disabled: boolean };
export type PricingFilter = { free: boolean; paid: boolean };
export type TypeFilter = { cloud: boolean; local: boolean; selfHosted: boolean; login: boolean };

export type ShellUsageTotals = { requests?: number; totalTokens?: number };

/** One model row of `GET /api/usage?range=30d` as the detail panes read it. */
interface UsageModelResponseRow {
  provider: string;
  model: string;
  resolvedModel?: string;
  requests: number;
  totalTokens: number;
  inputTokens: number;
  outputTokens: number;
  shareRatio: number;
  estimatedCostUsd?: number;
}

/** The same row as the shell stores it: the provider is already the map key. */
export type ShellUsageModelRow = Omit<UsageModelResponseRow, "provider">;

function asRecordValue(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

/** A key the payload left out is absent; a key it sent with the wrong type is a rejection. */
function optionalText(row: Record<string, unknown>, key: string): string | undefined | null {
  const value = row[key];
  if (value === undefined) return undefined;
  return typeof value === "string" ? value : null;
}

/**
 * One quota report. A report without a usable read time, without a quota payload,
 * or with mistyped provenance fields is rejected rather than repaired.
 */
export function parseQuotaReport(value: unknown): ProviderQuotaReportView | null {
  const row = asRecordValue(value);
  if (!row) return null;
  const { updatedAt } = row;
  if (typeof updatedAt !== "number" || !Number.isFinite(updatedAt)) return null;
  if (!("quota" in row)) return null;
  const label = optionalText(row, "label");
  const source = optionalText(row, "source");
  if (label === null || source === null) return null;
  const report: ProviderQuotaReportView = { updatedAt, quota: row.quota };
  if (label !== undefined) report.label = label;
  if (source !== undefined) report.source = source;
  if (row.entitlement !== undefined) report.entitlement = row.entitlement;
  if (row.aggregation !== undefined) report.aggregation = row.aggregation;
  return report;
}

/** Row array from either a bare array response or a `{ reports: [...] }` envelope. */
export function quotaReportRows(value: unknown): unknown[] {
  const rows = Array.isArray(value) ? value : asRecordValue(value)?.reports;
  return Array.isArray(rows) ? rows : [];
}

/** Fresh reports keyed by provider; a row without a provider id is skipped. */
export function freshQuotaReportsFromResponse(value: unknown): Record<string, ProviderQuotaReportView> {
  const reports: Record<string, ProviderQuotaReportView> = {};
  for (const raw of quotaReportRows(value)) {
    const report = parseQuotaReport(raw);
    const provider = asRecordValue(raw)?.provider;
    if (report && typeof provider === "string" && provider.trim() !== "") reports[provider] = report;
  }
  return reports;
}

/** Cached reports keyed by provider, or null when the cache holds nothing usable. */
export function quotaReportsFromCache(raw: unknown): Record<string, ProviderQuotaReportView> | null {
  const cached = asRecordValue(raw);
  if (!cached) return null;
  const reports: Record<string, ProviderQuotaReportView> = {};
  for (const [provider, value] of Object.entries(cached)) {
    const report = parseQuotaReport(value);
    if (report && provider.trim() !== "") reports[provider] = report;
  }
  return Object.keys(reports).length > 0 ? reports : null;
}

/**
 * Rail rows for the workspace aggregate. Lifecycle comes from the listener, so a
 * row whose base provider config is gone (or not loaded yet) is skipped, and only
 * a healthy row is priced.
 */
export function sectionsFromWorkspace(
  workspace: ProviderWorkspaceAggregate,
  providers: Record<string, WorkspaceProvider>,
  activeNeedsReauth: Readonly<Record<string, boolean>>,
): WorkspaceSections {
  const sections: WorkspaceSections = { ready: [], needsSetup: [], disabled: [] };
  for (const row of workspace.providers) {
    const base = providers[row.id];
    if (!base) continue;
    const item: WorkspaceItem = {
      name: row.id,
      ...base,
      access: row.access,
      workspaceLifecycle: row.lifecycle,
      ...(activeNeedsReauth[row.id] ? { activeNeedsReauth: true } : {}),
      ...(row.lifecycle === "healthy" ? { tier: providerTier(row.id, base) } : {}),
    };
    if (row.lifecycle === "disabled") sections.disabled.push(item);
    else if (row.lifecycle === "attention") sections.needsSetup.push(item);
    else sections.ready.push(item);
  }
  return sections;
}

/** Whether the rail is showing something other than every row in default order. */
export function workspaceFilterActive(input: {
  statusFilter: StatusFilter;
  pricingFilter: PricingFilter;
  typeFilter: TypeFilter;
  sortMode: ProviderSortMode;
}): boolean {
  const { statusFilter, pricingFilter, typeFilter, sortMode } = input;
  const statusesShown = statusFilter.ready && statusFilter.needsSetup && statusFilter.disabled;
  const pricesShown = pricingFilter.free && pricingFilter.paid;
  const kindsShown = typeFilter.cloud && typeFilter.local && typeFilter.selfHosted && typeFilter.login;
  if (statusesShown && pricesShown && kindsShown && sortMode === "az") return false;
  return true;
}

function matchesSearch(item: WorkspaceItem, needle: string): boolean {
  return item.name.toLowerCase().includes(needle) || item.adapter.toLowerCase().includes(needle);
}

export function itemMatchesWorkspaceFilters(
  item: WorkspaceItem,
  query: string,
  pricingFilter: PricingFilter,
  typeFilter: TypeFilter,
): boolean {
  const needle = query.trim().toLowerCase();
  if (needle !== "" && !matchesSearch(item, needle)) return false;
  const priced = isFreeProvider(item) ? pricingFilter.free : pricingFilter.paid;
  if (!priced) return false;
  return typeFilter[providerKind(item)] === true;
}

export function filterWorkspaceSections(
  sections: WorkspaceSections,
  search: string,
  statusFilter: StatusFilter,
  pricingFilter: PricingFilter,
  typeFilter: TypeFilter,
  sortMode: ProviderSortMode,
): WorkspaceSections {
  const filtered = (items: WorkspaceItem[]) => sortWorkspaceItems(
    items.filter(item => itemMatchesWorkspaceFilters(item, search, pricingFilter, typeFilter)),
    sortMode,
  );
  return {
    ready: statusFilter.ready ? filtered(sections.ready) : [],
    needsSetup: statusFilter.needsSetup ? filtered(sections.needsSetup) : [],
    disabled: statusFilter.disabled ? filtered(sections.disabled) : [],
  };
}

/**
 * The reserved OpenAI account row owns the API-key lane's model list, so the
 * `openai` row reads both and keeps the account lane's own order first.
 */
export function mergeProviderValues(
  bag: Record<string, string[]>,
  name: string,
  providers: Record<string, WorkspaceProvider>,
): string[] {
  const own = bag[name] ?? [];
  if (name !== "openai" || !providers["openai-apikey"]) return own;
  const extra = bag["openai-apikey"] ?? [];
  if (extra.length === 0) return own;
  const seen = new Set(own);
  const merged = [...own];
  for (const id of extra) {
    if (seen.has(id)) continue;
    seen.add(id);
    merged.push(id);
  }
  return merged;
}

export function providerHasLiveModels(
  name: string,
  liveModelCounts: Record<string, number>,
): boolean {
  const lane = name === "openai" ? (liveModelCounts["openai-apikey"] ?? 0) : 0;
  return (liveModelCounts[name] ?? 0) + lane > 0;
}

/** The API-key lane behind the reserved OpenAI account row, when it is configured. */
export function selectedItemApiLane(
  name: string,
  providers: Record<string, WorkspaceProvider>,
): WorkspaceProvider | undefined {
  if (name !== "openai") return undefined;
  return providers["openai-apikey"];
}

export function eventsForProvider(
  events: readonly ProviderWorkspaceEvent[],
  name: string,
): ProviderWorkspaceEvent[] {
  return events.filter(event => event.provider === name);
}

export function usageTotalsFromResponse(
  rows: Array<{ provider: string; requests: number; totalTokens?: number }> | undefined,
): Record<string, ShellUsageTotals> {
  const totals: Record<string, ShellUsageTotals> = {};
  for (const row of rows ?? []) {
    totals[row.provider] = { requests: row.requests, totalTokens: row.totalTokens };
  }
  return totals;
}

export function usageModelsFromResponse(
  rows: UsageModelResponseRow[] | undefined,
): Record<string, ShellUsageModelRow[]> {
  const byProvider: Record<string, ShellUsageModelRow[]> = {};
  for (const row of rows ?? []) {
    const stored: ShellUsageModelRow = {
      model: row.model,
      ...(row.resolvedModel ? { resolvedModel: row.resolvedModel } : {}),
      requests: row.requests,
      totalTokens: row.totalTokens,
      inputTokens: row.inputTokens,
      outputTokens: row.outputTokens,
      shareRatio: row.shareRatio,
      ...(row.estimatedCostUsd !== undefined ? { estimatedCostUsd: row.estimatedCostUsd } : {}),
    };
    if (byProvider[row.provider]) byProvider[row.provider]!.push(stored);
    else byProvider[row.provider] = [stored];
  }
  return byProvider;
}

/**
 * Which rail row the tab strip can reach: the remembered row while it is still
 * visible, else the selected row, else the first visible row.
 */
export function nextRailTabbableName(
  visibleRailNames: readonly string[],
  railFocusName: string | null,
  selectedName: string | null,
): string | null {
  if (railFocusName !== null && visibleRailNames.includes(railFocusName)) return railFocusName;
  if (selectedName !== null && visibleRailNames.includes(selectedName)) return selectedName;
  return visibleRailNames[0] ?? null;
}

/** Display labels shared by more than one rail row; those rows disambiguate. */
export function duplicateProviderDisplayNames(
  items: readonly WorkspaceItem[],
  labelFor: (name: string) => string,
): Set<string> {
  const seen = new Map<string, number>();
  for (const item of items) {
    const label = labelFor(item.name);
    seen.set(label, (seen.get(label) ?? 0) + 1);
  }
  const duplicates = new Set<string>();
  for (const [label, count] of seen) {
    if (count > 1) duplicates.add(label);
  }
  return duplicates;
}

export type WorkspaceRailGroupId = "connected" | "attention" | "disabled";

/** Rail rows split by lifecycle, in the order the rail renders the groups. */
export function workspaceRailGroups(filteredItems: readonly WorkspaceItem[]): Array<{
  id: WorkspaceRailGroupId;
  lifecycle: "healthy" | "attention" | "disabled";
  items: WorkspaceItem[];
}> {
  const ofLifecycle = (lifecycle: WorkspaceItem["workspaceLifecycle"]) => (
    filteredItems.filter(item => item.workspaceLifecycle === lifecycle)
  );
  return [
    { id: "connected", lifecycle: "healthy", items: ofLifecycle("healthy") },
    { id: "attention", lifecycle: "attention", items: ofLifecycle("attention") },
    { id: "disabled", lifecycle: "disabled", items: ofLifecycle("disabled") },
  ];
}

export function detailHasLiveModels(
  itemName: string,
  liveModelCounts: Record<string, number>,
): boolean {
  return providerHasLiveModels(itemName, liveModelCounts);
}

export function workspaceDownstream(
  row: ProviderWorkspaceRow | undefined,
): { harnesses: number | null; routes: number; subagents: number } {
  return row?.downstream ?? { harnesses: null, routes: 0, subagents: 0 };
}

/**
 * Loading, empty, and ready are decided from the aggregate alone: no aggregate
 * with no rows is still loading, an aggregate with no providers is the empty
 * board, and anything else has a rail to draw.
 */
export function workspaceBoardKind(
  workspace: ProviderWorkspaceAggregate | null,
  itemCount: number,
): "loading" | "empty" | "ready" {
  if (!workspace) return itemCount === 0 ? "loading" : "ready";
  if (workspace.summary.totalProviders === 0) return "empty";
  return "ready";
}

/** Fleet counters the Providers board head renders. */
export type WorkspaceKpis = {
  total: number;
  healthy: number;
  attention: number;
  disabled: number;
  models: number;
};

export function workspaceKpis(workspace: ProviderWorkspaceAggregate | null): WorkspaceKpis | null {
  if (!workspace) return null;
  return {
    total: workspace.summary.totalProviders,
    healthy: workspace.summary.healthy,
    attention: workspace.summary.attention,
    disabled: workspace.summary.disabled,
    models: workspace.summary.exposedModels,
  };
}

/** What the main pane shows: the config editor, the selected detail, or the fleet. */
export function workspaceMainKind(
  jsonOpen: boolean,
  hasSelection: boolean,
  hasWorkspace: boolean,
): "json" | "detail" | "dashboard" | "none" {
  if (jsonOpen) return "json";
  if (hasSelection) return "detail";
  if (hasWorkspace) return "dashboard";
  return "none";
}

/** Fleet overview and a selected rail row are mutually exclusive. */
export function railRowSelected(selectedName: string | null, itemName: string): boolean {
  return selectedName != null && selectedName === itemName;
}

/**
 * After POST /api/model-discovery the listener already replaced in-memory catalog
 * rows. Fleet available/unavailable and lastModelSync live on GET /api/providers/workspace,
 * so a models-only refresh would leave the board stale until the idle poll.
 */
export function bumpCatalogSyncEpochs(
  bumpModels: (fn: (epoch: number) => number) => void,
  bumpWorkspace: (fn: (epoch: number) => number) => void,
): void {
  bumpModels(epoch => epoch + 1);
  bumpWorkspace(epoch => epoch + 1);
}
