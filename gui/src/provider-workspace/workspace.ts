import type { ProviderAccessDescriptor } from "./auth";

export type WorkspaceLifecycle = "healthy" | "attention" | "disabled";

export type ProviderWorkspaceIssue = {
  provider: string;
  code: string;
  severity: string;
  detail?: string;
  timestamp?: number;
};

export type ProviderWorkspaceEvent = {
  provider: string;
  type: string;
  detail?: string;
  severity: string;
  timestamp: number;
};

export type ProviderWorkspaceRow = {
  id: string;
  connections: string[];
  hidden: string[];
  lifecycle: WorkspaceLifecycle;
  modelCount: number;
  access: ProviderAccessDescriptor;
  disabled: boolean;
  lastValidated: number | null;
  downstream: {
    harnesses: number | null;
    routes: number;
    subagents: number;
  };
};

export type ProviderWorkspaceAggregate = {
  summary: {
    totalProviders: number;
    healthy: number;
    attention: number;
    disabled: number;
    exposedModels: number;
  };
  providers: ProviderWorkspaceRow[];
  attention: ProviderWorkspaceIssue[];
  availability: {
    modelsAvailable: number;
    modelsUnavailable: number;
    staleProviderCatalogues: number | null;
    lastModelSync: number | null;
  };
  downstream: {
    harnessCount: number | null;
    routeCount: number;
    subAgentModelCount: number;
    affectedRouteCount: number;
  };
  recentEvents: ProviderWorkspaceEvent[];
};

function finiteNumber(value: unknown): value is number {
  return typeof value === "number" && Number.isFinite(value);
}

function finiteCount(value: unknown): value is number {
  return finiteNumber(value) && Number.isInteger(value) && value >= 0;
}

function nullableFiniteNumber(value: unknown): value is number | null {
  return value === null || finiteNumber(value);
}

function nullableCount(value: unknown): value is number | null {
  return value === null || finiteCount(value);
}

function recordFromUnknown(value: unknown): Record<string, unknown> | null {
  return value && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function accessDescriptorFromUnknown(value: unknown): ProviderAccessDescriptor | null {
  // The aggregate treats access as a server-owned descriptor. Preserve the existing
  // wire contract here: validate only the object container, not future descriptor fields.
  if (!value || typeof value !== "object" || Array.isArray(value)) return null;
  return value as ProviderAccessDescriptor;
}

function stringArrayFromUnknown(value: unknown): string[] | null {
  if (value == null) return [];
  return Array.isArray(value) && value.every(item => typeof item === "string")
    ? value as string[]
    : null;
}

function lifecycleFromUnknown(value: unknown): WorkspaceLifecycle | null {
  return value === "healthy" || value === "attention" || value === "disabled"
    ? value
    : null;
}

function parseSummary(value: unknown): ProviderWorkspaceAggregate["summary"] | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const totalProviders = row.totalProviders;
  const healthy = row.healthy;
  const attention = row.attention;
  const disabled = row.disabled;
  const exposedModels = row.exposedModels;
  if (!finiteCount(totalProviders) || !finiteCount(healthy) || !finiteCount(attention)) return null;
  if (!finiteCount(disabled) || !finiteCount(exposedModels)) return null;
  if (totalProviders !== healthy + attention + disabled) return null;
  return { totalProviders, healthy, attention, disabled, exposedModels };
}

function parseAvailability(value: unknown): ProviderWorkspaceAggregate["availability"] | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const modelsAvailable = row.modelsAvailable;
  const modelsUnavailable = row.modelsUnavailable;
  const staleProviderCatalogues = row.staleProviderCatalogues;
  const lastModelSync = row.lastModelSync;
  if (!finiteCount(modelsAvailable) || !finiteCount(modelsUnavailable)) return null;
  if (!nullableCount(staleProviderCatalogues) || !nullableFiniteNumber(lastModelSync)) return null;
  return { modelsAvailable, modelsUnavailable, staleProviderCatalogues, lastModelSync };
}

function parseAggregateDownstream(value: unknown): ProviderWorkspaceAggregate["downstream"] | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const harnessCount = row.harnessCount;
  const routeCount = row.routeCount;
  const subAgentModelCount = row.subAgentModelCount;
  const affectedRouteCount = row.affectedRouteCount;
  if (!nullableCount(harnessCount) || !finiteCount(routeCount)) return null;
  if (!finiteCount(subAgentModelCount) || !finiteCount(affectedRouteCount)) return null;
  return { harnessCount, routeCount, subAgentModelCount, affectedRouteCount };
}

function parseProviderDownstream(value: unknown): ProviderWorkspaceRow["downstream"] | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const harnesses = row.harnesses;
  const routes = row.routes;
  const subagents = row.subagents;
  if (!nullableCount(harnesses) || !finiteCount(routes) || !finiteCount(subagents)) return null;
  return { harnesses, routes, subagents };
}

function parseProviderRow(value: unknown): ProviderWorkspaceRow | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const { id, modelCount, access, disabled, lastValidated } = row;
  if (typeof id !== "string" || !id.trim()) return null;
  const connections = stringArrayFromUnknown(row.connections);
  const hidden = stringArrayFromUnknown(row.hidden);
  const lifecycle = lifecycleFromUnknown(row.lifecycle);
  if (!connections || !hidden || !lifecycle) return null;
  if (!finiteCount(modelCount) || typeof disabled !== "boolean") return null;
  if (!nullableFiniteNumber(lastValidated)) return null;
  const accessDescriptor = accessDescriptorFromUnknown(access);
  const downstream = parseProviderDownstream(row.downstream);
  if (!accessDescriptor || !downstream) return null;
  return {
    id,
    connections,
    hidden,
    lifecycle,
    modelCount,
    access: accessDescriptor,
    disabled,
    lastValidated,
    downstream,
  };
}

function parseIssue(value: unknown): ProviderWorkspaceIssue | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const { provider, code, severity, detail, timestamp } = row;
  if (typeof provider !== "string" || typeof code !== "string" || typeof severity !== "string") return null;
  if (detail !== undefined && typeof detail !== "string") return null;
  if (timestamp !== undefined && !finiteNumber(timestamp)) return null;
  return {
    provider,
    code,
    severity,
    ...(typeof detail === "string" ? { detail } : {}),
    ...(finiteNumber(timestamp) ? { timestamp } : {}),
  };
}

function parseEvent(value: unknown): ProviderWorkspaceEvent | null {
  const row = recordFromUnknown(value);
  if (!row) return null;
  const { provider, type, severity, timestamp, detail } = row;
  if (typeof provider !== "string" || typeof type !== "string" || typeof severity !== "string") return null;
  if (!finiteNumber(timestamp)) return null;
  if (detail !== undefined && typeof detail !== "string") return null;
  return {
    provider,
    type,
    severity,
    timestamp,
    ...(typeof detail === "string" ? { detail } : {}),
  };
}

function parseArray<T>(value: unknown, parser: (entry: unknown) => T | null): T[] | null {
  if (!Array.isArray(value)) return null;
  const parsed: T[] = [];
  for (const entry of value) {
    const next = parser(entry);
    if (!next) return null;
    parsed.push(next);
  }
  return parsed;
}

function parseRecentEvents(value: unknown): ProviderWorkspaceEvent[] | null {
  if (value === null) return [];
  return parseArray(value, parseEvent);
}

export function parseProviderWorkspaceAggregate(value: unknown): ProviderWorkspaceAggregate | null {
  const root = recordFromUnknown(value);
  if (!root) return null;
  const summary = parseSummary(root.summary);
  const availability = parseAvailability(root.availability);
  const downstream = parseAggregateDownstream(root.downstream);
  const providers = parseArray(root.providers, parseProviderRow);
  const attention = parseArray(root.attention, parseIssue);
  const recentEvents = parseRecentEvents(root.recentEvents);
  if (!summary || !availability || !downstream || !providers || !attention || !recentEvents) return null;
  return { summary, providers, attention, availability, downstream, recentEvents };
}