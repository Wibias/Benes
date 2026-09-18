/**
 * Benes dashboard source. Where the routing editor's candidate list comes from.
 *
 * The editor offers a candidate per enabled model, and the answer is assembled from what the
 * listener publishes: the live `/api/models` catalog plus each enabled provider's stored
 * entry in `GET /api/config`. The reads here are permissive on purpose — a row the editor
 * cannot name is skipped, because it is no reason to refuse the rows it can — while the shape
 * each read depends on is stated once, in the field readers below.
 *
 * The Lab's suite catalog is not read here: it is Lab data, and `lab/lab-catalog` owns it.
 */
import type { ModelOption } from "../routing-profile/profile-model.ts";

/** One provider's stored entry in `GET /api/config`. */
export type RoutingProviderConfig = {
  disabled?: boolean;
  defaultModel?: string;
  models?: unknown;
};

/** Provider names the router owns itself; neither is a candidate the editor may offer. */
const ROUTER_NAMESPACES: ReadonlySet<string> = new Set(["combo", "policy"]);

/** A listener row, as the object a named field can be read from. */
function rowOf(value: unknown): Record<string, unknown> | null {
  // Boundary read: the payload is untrusted JSON, so the shape is checked before use.
  return typeof value === "object" && value !== null && !Array.isArray(value)
    ? value as Record<string, unknown>
    : null;
}

/** One row's trimmed text field, or "" when the row did not carry one. */
function textOf(row: Record<string, unknown>, field: string): string {
  const value = row[field];
  return typeof value === "string" ? value.trim() : "";
}

/** The rows of a `/api/models` answer, which may be a bare list or carry a `models` list. */
function modelRowsOf(payload: unknown): unknown[] {
  if (Array.isArray(payload)) return payload;
  const row = rowOf(payload);
  return row !== null && Array.isArray(row.models) ? row.models : [];
}

/** Candidates already offered, by provider. */
type OfferedModels = Map<string, Set<string>>;

/** Record a provider/model pair, answering whether it is one this read had not offered yet. */
function offerOnce(offered: OfferedModels, provider: string, id: string): boolean {
  const ids = offered.get(provider) ?? new Set<string>();
  const fresh = !ids.has(id);
  ids.add(id);
  offered.set(provider, ids);
  return fresh;
}

/** One candidate row, or `null` when it does not name a model the editor could offer. */
function candidateOf(row: unknown): ModelOption | null {
  const fields = rowOf(row);
  if (fields === null || fields.disabled === true) return null;
  const provider = textOf(fields, "provider");
  const id = textOf(fields, "id");
  if (provider === "" || id === "" || ROUTER_NAMESPACES.has(provider)) return null;
  return { provider, id };
}

/** Every candidate the live catalog names, once each, in the order it named them. */
export function parseRoutingModels(payload: unknown): ModelOption[] {
  const offered: OfferedModels = new Map();
  const models: ModelOption[] = [];
  for (const row of modelRowsOf(payload)) {
    const candidate = candidateOf(row);
    if (candidate === null) continue;
    if (offerOnce(offered, candidate.provider, candidate.id)) models.push(candidate);
  }
  return models;
}

/**
 * The model ids the live catalog disables.
 *
 * A disabled model is recorded under every spelling a candidate could be named by — its bare
 * id, the namespaced id it may also be published as, and its provider-qualified id — because
 * the editor matches a candidate against whichever of them it was offered as.
 */
function disabledModelIds(payload: unknown): Set<string> {
  const disabled = new Set<string>();
  for (const row of modelRowsOf(payload)) {
    const fields = rowOf(row);
    if (fields === null || fields.disabled !== true) continue;
    const provider = textOf(fields, "provider");
    const id = textOf(fields, "id");
    const namespaced = textOf(fields, "namespaced");
    for (const spelling of [id, namespaced, provider && id ? `${provider}/${id}` : ""]) {
      if (spelling !== "") disabled.add(spelling);
    }
  }
  return disabled;
}

/** One id from a provider's stored catalog, which lists either ids or rows carrying them. */
function storedModelId(entry: unknown): string {
  if (typeof entry === "string") return entry.trim();
  const fields = rowOf(entry);
  return fields === null ? "" : textOf(fields, "id");
}

/** The enabled providers' stored entries, as name and entry pairs. */
function enabledProviders(
  providers: Record<string, RoutingProviderConfig>,
): Array<[string, RoutingProviderConfig]> {
  return Object.entries(providers).filter(([, provider]) => provider.disabled !== true);
}

/** The enabled providers, by name. */
export function enabledProviderNames(providers: Record<string, RoutingProviderConfig>): string[] {
  return enabledProviders(providers)
    .map(([name]) => name)
    .sort((left, right) => (left < right ? -1 : left > right ? 1 : 0));
}

/** Each enabled provider's announced default model, where it announced one. */
export function enabledProviderDefaults(
  providers: Record<string, RoutingProviderConfig>,
): Record<string, string> {
  const defaults: Record<string, string> = {};
  for (const [name, provider] of enabledProviders(providers)) {
    if (typeof provider.defaultModel === "string") defaults[name] = provider.defaultModel.trim();
  }
  return defaults;
}

/**
 * The candidates the editor may offer.
 *
 * A candidate is any enabled model that either the live catalog or an enabled provider's
 * stored catalog names, sorted by provider and then id so the list does not depend on which
 * read answered first. A model the live catalog disables is left out in every spelling.
 */
export function mergeRoutingModels(
  modelsPayload: unknown,
  providers: Record<string, RoutingProviderConfig>,
): ModelOption[] {
  const disabled = disabledModelIds(modelsPayload);
  const offered: OfferedModels = new Map();
  const models: ModelOption[] = [];
  const offer = (provider: string, id: string) => {
    if (provider === "" || id === "" || ROUTER_NAMESPACES.has(provider)) return;
    if (disabled.has(id) || disabled.has(`${provider}/${id}`)) return;
    if (offerOnce(offered, provider, id)) models.push({ provider, id });
  };
  for (const row of modelRowsOf(modelsPayload)) {
    const candidate = candidateOf(row);
    if (candidate !== null) offer(candidate.provider, candidate.id);
  }
  for (const [name, provider] of enabledProviders(providers)) {
    if (!Array.isArray(provider.models)) continue;
    for (const entry of provider.models) offer(name.trim(), storedModelId(entry));
  }
  return models.sort((left, right) =>
    left.provider.localeCompare(right.provider) || left.id.localeCompare(right.id));
}

/** One provider's candidates, in id order. */
export function modelOptionsForProvider(models: ModelOption[], provider: string): ModelOption[] {
  const name = provider.trim();
  return models
    .filter(model => model.provider === name)
    .sort((left, right) => left.id.localeCompare(right.id));
}

/** The first candidate a provider offers by id, or "" when it offers none. */
export function firstModelForProvider(models: ModelOption[], provider: string): string {
  return modelOptionsForProvider(models, provider)[0]?.id ?? "";
}

/**
 * Which profile the editor opens with.
 *
 * A named profile wins — the one asked for, else the one already open — and the first profile
 * the answer listed is the fallback. Nothing at all is the answer when there are none.
 */
export function selectedProfileAfterLoad<T extends { id: string }>(
  profiles: T[],
  currentId: string | null,
  preferredId?: string,
): T | null {
  const named = preferredId ?? currentId;
  return (named === null ? undefined : profiles.find(profile => profile.id === named))
    ?? profiles[0]
    ?? null;
}

/** What the dry-run form declares about the request it is asking about. */
export interface RoutingDryRunInput {
  readonly context: string;
  readonly tools: boolean;
  readonly image: boolean;
  readonly structured: boolean;
}

/**
 * The evidence `POST /api/routing-profiles/dry-run` is asked with.
 *
 * Only what the reader declared is sent: an unparsed or non-positive context window is left
 * out rather than sent as zero, and a flag that is off is absent rather than false, so the
 * routing decision reads "not stated" instead of a value the editor invented.
 */
export function routingDryRunEvidence(input: RoutingDryRunInput): Record<string, number | boolean> {
  const tokens = Number(input.context.trim());
  return {
    ...(Number.isFinite(tokens) && tokens > 0 ? { contextWindow: tokens } : {}),
    ...(input.tools ? { toolsRequired: true } : {}),
    ...(input.image ? { imageInputRequired: true } : {}),
    ...(input.structured ? { structuredOutputRequired: true } : {}),
  };
}
