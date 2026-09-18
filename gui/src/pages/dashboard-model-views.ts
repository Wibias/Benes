/**
 * Model list projections for the Dashboard and Control boards: provider grouping, search
 * filtering, and the describer options the vision picker offers.
 *
 * Every list here is derived client-side from wire responses. The listener publishes
 * catalog rows and eligible describer ids; the grouping, the labels and the selection
 * patches are this client's own view model, so nothing here reads a field the listener
 * does not send.
 */
import type { SidecarBackend, SidecarData, SidecarPatch } from "./control-board-settings";
import type { ModelInfo } from "./dashboard-shared";

/** Providers the built-in describer can address when the server sends no eligible list. */
const DESCRIBER_PROVIDERS = new Set(["openai", "anthropic"]);

export function groupDashboardModels(models: ModelInfo[]): Array<[string, ModelInfo[]]> {
  const byProvider = new Map<string, ModelInfo[]>();
  for (const model of models) {
    byProvider.set(model.provider, [...(byProvider.get(model.provider) ?? []), model]);
  }
  return [...byProvider].sort(([left], [right]) => left.localeCompare(right));
}

function matchesQuery(provider: string, model: ModelInfo, needle: string): boolean {
  return model.id.toLowerCase().includes(needle) || provider.toLowerCase().includes(needle);
}

/** A non-empty query keeps only the rows and groups it matches. */
export function filterDashboardModelGroups(
  grouped: Array<[string, ModelInfo[]]>,
  query: string,
): Array<[string, ModelInfo[]]> {
  const needle = query.trim().toLowerCase();
  if (!needle) return grouped;
  const matched: Array<[string, ModelInfo[]]> = [];
  for (const [provider, rows] of grouped) {
    const hits = rows.filter(model => matchesQuery(provider, model, needle));
    if (hits.length > 0) matched.push([provider, hits]);
  }
  return matched;
}

/** A non-empty search query expands every remaining group. Manual toggles live in the set. */
export function dashboardModelGroupExpanded(
  query: string,
  expandedProviders: ReadonlySet<string>,
  provider: string,
): boolean {
  if (query.trim().length > 0) return true;
  return expandedProviders.has(provider);
}

export function toggleExpandedProvider(
  expanded: ReadonlySet<string>,
  provider: string,
): Set<string> {
  const next = new Set(expanded);
  if (next.has(provider)) next.delete(provider);
  else next.add(provider);
  return next;
}

/**
 * The openai/anthropic routing ids, used only when a response omits `visionModels`.
 *
 * That key is how the server proves which describers are eligible. A response without it
 * predates the field, and this provider-name list is the documented degrade path for it: a
 * server that does send `[]` has proven nothing is eligible, and refilling the picker from
 * the catalog would put back exactly the text-only rows the field exists to remove.
 */
export function sidecarModelIds(models: ModelInfo[]): string[] {
  return models
    .filter(model => DESCRIBER_PROVIDERS.has(model.provider))
    .map(model => model.namespaced);
}

/**
 * Vision picker ids: the server's eligible list, plus a stored describer it no longer lists.
 *
 * The wire carries ids, the order is the server's, and the label is whatever the id reads as
 * (see `formatNamespacedModelId` at the call site). Keeping the stored id selectable means a
 * describer that went ineligible stays visible instead of silently changing what is saved.
 */
export function visionModelOptions(
  serverIds: string[] | undefined,
  models: ModelInfo[],
  current: string | undefined,
): string[] {
  const ids = serverIds ?? sidecarModelIds(models);
  if (current && !ids.includes(current)) return [current, ...ids];
  return [...ids];
}

export function visionSelectOptions(models: ModelInfo[], sidecar: SidecarData | null): string[] {
  return visionModelOptions(sidecar?.visionModels, models, sidecar?.vision?.model);
}

/** Choosing "" turns Vision off; choosing a model turns it on if it was Off. */
export function visionActivationFields(model: string, currentlyEnabled: boolean): { enabled?: boolean } {
  if (model === "") return { enabled: false };
  if (!currentlyEnabled) return { enabled: true };
  return {};
}

/**
 * The class the image-describe modality resolves to.
 *
 * `openai`, `anthropic`, and `vision_describe` all normalize to the describe class, and the
 * provider arrives in the model id, so naming the class is the one value that cannot
 * misstate which provider the caller meant. The listener rejects anything else.
 */
export const VISION_DESCRIBE_BACKEND: SidecarBackend = "vision_describe";

export function visionEnabledPatch(enabled: boolean): SidecarPatch {
  return { vision: { enabled } };
}

export function visionModelChangePatch(model: string, visionEnabled: boolean): SidecarPatch {
  if (model === "") return visionEnabledPatch(false);
  return {
    vision: {
      model,
      backend: VISION_DESCRIBE_BACKEND,
      ...visionActivationFields(model, visionEnabled),
    },
  };
}

/**
 * Shadow-call replacement options use the proxy's canonical routing id, keep an explicit
 * "off" row, and keep whatever the server still has selected.
 */
export function shadowCallModelOptions(models: ModelInfo[], current: string | undefined) {
  const routingIds = models.map(model => model.namespaced);
  const values = current && !routingIds.includes(current) ? [...routingIds, current] : routingIds;
  return [
    { value: "", label: "—" },
    ...values.map(namespaced => ({ value: namespaced, label: namespaced })),
  ];
}
