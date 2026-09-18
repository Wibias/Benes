/**
 * Benes dashboard source. What the Routing profiles editor reads, and what one read means.
 *
 * The editor answers five questions that do not fail alike. The profile list is the read: a
 * page without it has nothing to show, so a refused list fails the load. The other four — the
 * profile's analytics, the provider config, the live model catalog, and the Lab's suite catalog
 * — each answer something the editor can do without, so a refused one costs only that answer.
 * That difference is a property of the source, stated once here, rather than a branch per call
 * site.
 *
 * Neither the payload shapes nor the profile codec are decided here: `routing-profile-decode`
 * reads the candidate sources, `lab/lab-catalog` reads the suite catalog, `routing-profiles-format`
 * reads the analytics, and `routing-profile/profile-codec` reads the profiles themselves.
 */
import { readLabCatalog, type LabCatalogSuite } from "../lab/lab-catalog.ts";
import { parseRoutingProfiles, routingProfileDraftFromDto } from "../routing-profile/profile-codec.ts";
import type { ModelOption, RoutingProfileDraft, RoutingProfileDto } from "../routing-profile/profile-model.ts";
import {
  enabledProviderNames,
  mergeRoutingModels,
  type RoutingProviderConfig,
} from "./routing-profile-decode.ts";
import { parseRoutingAnalytics, type Analytics } from "./routing-profiles-format.ts";

/** What one read of the five sources produced. */
export interface RoutingProfileSources {
  readonly profiles: RoutingProfileDto[];
  readonly analytics: Analytics | null;
  readonly providers: Record<string, RoutingProviderConfig>;
  /** The raw `/api/models` answer; the candidate merge reads its rows. */
  readonly modelsPayload: unknown;
  /** `null` when the Lab answered with something that is not a catalog. */
  readonly catalogSuites: LabCatalogSuite[] | null;
}

/** The editor's read inputs, derived once per load. */
export interface RoutingProfilesData {
  readonly profiles: RoutingProfileDto[];
  readonly analytics: Analytics | null;
  readonly providerNames: string[];
  readonly models: ModelOption[];
  readonly catalogSuites: LabCatalogSuite[];
  readonly catalogError: boolean;
}

/** Which profile is open, and the draft under the cursor. */
export interface RoutingEditorState {
  readonly selected: RoutingProfileDto | null;
  readonly draft: RoutingProfileDraft | null;
  readonly editing: boolean;
}

export const EMPTY_ROUTING_DATA: RoutingProfilesData = {
  profiles: [],
  analytics: null,
  providerNames: [],
  models: [],
  catalogSuites: [],
  catalogError: false,
};

export const EMPTY_ROUTING_EDITOR: RoutingEditorState = {
  selected: null,
  draft: null,
  editing: false,
};

/** The analytics route, scoped to one profile when a read is about one. */
function analyticsUrl(apiBase: string, profileId: string | null): string {
  const route = `${apiBase}/api/routing-analytics`;
  return profileId === null ? route : `${route}?profile=${encodeURIComponent(profileId)}`;
}

/**
 * One source that may answer with nothing.
 *
 * A refused response and a body this build cannot read are the same answer — nothing — because
 * either way the editor gets no value from the source and must fall back.
 */
async function readSource<TValue>(
  url: string,
  signal: AbortSignal,
  read: (payload: unknown) => TValue,
): Promise<TValue | null> {
  const response = await fetch(url, { signal });
  if (!response.ok) return null;
  try {
    return read(await response.json());
  } catch {
    return null;
  }
}

/** The provider entries a `/api/config` answer carries, or none when it carries no map. */
function readProviders(payload: unknown): Record<string, RoutingProviderConfig> {
  if (typeof payload !== "object" || payload === null || Array.isArray(payload)) return {};
  const providers = (payload as Record<string, unknown>).providers;
  if (typeof providers !== "object" || providers === null || Array.isArray(providers)) return {};
  return providers as Record<string, RoutingProviderConfig>;
}

/** Everything one load reads. A refused profile list rejects; every other source falls back. */
export async function fetchRoutingProfileSources(
  apiBase: string,
  signal: AbortSignal,
  profileId: string | null,
): Promise<RoutingProfileSources> {
  const listings = await fetch(`${apiBase}/api/routing-profiles`, { signal });
  if (!listings.ok) throw new Error(`load-${listings.status}`);
  const [profilesPayload, analytics, providers, modelsPayload, catalogSuites] = await Promise.all([
    listings.json() as Promise<unknown>,
    readSource(analyticsUrl(apiBase, profileId), signal, parseRoutingAnalytics),
    readSource(`${apiBase}/api/config`, signal, readProviders),
    readSource<unknown>(`${apiBase}/api/models`, signal, payload => payload),
    readSource(`${apiBase}/api/lab/catalog`, signal, readLabCatalog),
  ]);
  return {
    profiles: parseRoutingProfiles(profilesPayload),
    analytics,
    providers: providers ?? {},
    modelsPayload,
    catalogSuites,
  };
}

/** The editor's inputs, as one read of the sources leaves them. */
export function routingProfilesData(sources: RoutingProfileSources): RoutingProfilesData {
  return {
    profiles: sources.profiles,
    analytics: sources.analytics,
    providerNames: enabledProviderNames(sources.providers),
    models: mergeRoutingModels(sources.modelsPayload, sources.providers),
    catalogSuites: sources.catalogSuites ?? [],
    catalogError: sources.catalogSuites === null,
  };
}

/**
 * A draft whose candidates still name models the catalog offers.
 *
 * A candidate that fell out of the catalog would be saved as a reference the router can no
 * longer resolve, so it is moved to its provider's first remaining model. A provider with no
 * models left is left alone: dropping the row would silently rewrite the profile.
 */
export function draftCoercedToModels(
  draft: RoutingProfileDraft | null,
  models: ModelOption[],
): RoutingProfileDraft | null {
  if (draft === null || models.length === 0) return draft;
  let changed = false;
  const candidates = draft.candidates.map(candidate => {
    const offered = models.filter(model => model.provider === candidate.provider);
    if (offered.length === 0 || offered.some(model => model.id === candidate.model)) return candidate;
    changed = true;
    return { ...candidate, model: offered.map(model => model.id).sort((a, b) => a.localeCompare(b))[0]! };
  });
  return changed ? { ...draft, candidates } : draft;
}

/**
 * The editor one settled load leaves behind.
 *
 * A read the reader did not ask for — a background reload — must not close an open draft, so
 * only the models are reconciled under it. A read that settled on a profile (a save, a delete,
 * an explicit selection) replaces the editor with that profile instead.
 */
export function editorAfterLoad(input: {
  readonly editor: RoutingEditorState;
  readonly models: ModelOption[];
  readonly settledOn: RoutingProfileDto | null;
  readonly replacesEditor: boolean;
}): RoutingEditorState {
  const draft = input.replacesEditor
    ? input.settledOn === null ? null : routingProfileDraftFromDto(input.settledOn)
    : input.editor.draft;
  return {
    selected: input.replacesEditor ? input.settledOn : input.editor.selected,
    draft: draftCoercedToModels(draft, input.models),
    editing: input.replacesEditor ? false : input.editor.editing,
  };
}

/** Whether a dry run on screen still describes the profile a reload settled on. */
export function dryRunSurvivesReload(
  current: { readonly id: string; readonly revision: string } | null,
  refreshed: { readonly id: string; readonly revision: string } | null,
): boolean {
  if (current === null || refreshed === null) return false;
  return current.id === refreshed.id && current.revision === refreshed.revision;
}
