/** One decoded snapshot of GET /api/selected-models. */

export type ModelSelectionSnapshot = {
  available: Record<string, string[]>;
  selected: Record<string, string[]>;
  liveCounts: Record<string, number>;
};

export type ProviderUsageTotals = {
  requests?: number;
  totalTokens?: number;
};

export type ProviderAvailableModels = ModelSelectionSnapshot["available"];
export type ProviderSelectedModels = ModelSelectionSnapshot["selected"];
export type ProviderLiveModelCounts = ModelSelectionSnapshot["liveCounts"];

function record(value: unknown): Record<string, unknown> | null {
  return value !== null && typeof value === "object" && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

function collectStringLists(field: unknown): Record<string, string[]> {
  const source = record(field);
  if (!source) return {};
  return Object.fromEntries(
    Object.entries(source).flatMap(([provider, ids]) => {
      if (!Array.isArray(ids)) return [];
      return [[provider, ids.filter((id): id is string => typeof id === "string")] as const];
    }),
  );
}

function collectLiveCounts(field: unknown): Record<string, number> {
  const source = record(field);
  if (!source) return {};
  return Object.fromEntries(
    Object.entries(source).flatMap(([provider, value]) => {
      if (typeof value !== "number" || !Number.isFinite(value) || value < 0) return [];
      return [[provider, Math.floor(value)] as const];
    }),
  );
}

export function decodeModelSelection(raw: unknown): ModelSelectionSnapshot {
  const root = record(raw) ?? {};
  return {
    available: collectStringLists(root.available),
    selected: collectStringLists(root.selected),
    liveCounts: collectLiveCounts(root.liveModelCounts),
  };
}

export function countAvailableModels(raw: unknown): Record<string, number> {
  return Object.fromEntries(
    Object.entries(decodeModelSelection(raw).available).map(([provider, models]) => [provider, models.length]),
  );
}
