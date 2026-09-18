/**
 * Routing-profile codec.
 *
 * Converts between the `/api/routing-profiles` DTO and the string-valued editor
 * draft, and shapes the PUT body. Editor-only state (candidate list keys, the
 * `enabled` tri-state) never leaves this module.
 */

import {
  COMPATIBILITY_MIN_STATUSES,
  PROFILE_REQUIREMENT_FIELDS,
  PROFILE_SUITE_LAYERS,
  ROUTING_PROFILE_DEFAULT_ICON,
  UNKNOWN_EVIDENCE_MODES,
  newDraftCandidate,
  type CompatibilityMinStatus,
  type CompatibilitySuiteDraft,
  type OptionalBoolean,
  type ProfileSuiteLayer,
  type RequirementKind,
  type RoutingProfileCandidate,
  type RoutingProfileCompatibility,
  type RoutingProfileDraft,
  type RoutingProfileDraftCandidate,
  type RoutingProfileDraftCompatibility,
  type RoutingProfileDraftLimits,
  type RoutingProfileDraftOptimize,
  type RoutingProfileDraftRequirement,
  type RoutingProfileDto,
  type RoutingProfileLimits,
  type RoutingProfileOptimizeWeights,
  type RoutingProfileRequirement,
  type UnknownEvidenceMode,
} from "./profile-model.ts";

const SUITE_LAYER_SET: ReadonlySet<string> = new Set(PROFILE_SUITE_LAYERS);
const EVIDENCE_MODE_SET: ReadonlySet<string> = new Set(UNKNOWN_EVIDENCE_MODES);
const MIN_STATUS_SET: ReadonlySet<string> = new Set(COMPATIBILITY_MIN_STATUSES);

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isSuiteLayer(value: unknown): value is ProfileSuiteLayer {
  return typeof value === "string" && SUITE_LAYER_SET.has(value);
}

function isEvidenceMode(value: unknown): value is UnknownEvidenceMode {
  return typeof value === "string" && EVIDENCE_MODE_SET.has(value);
}

function isMinStatus(value: unknown): value is CompatibilityMinStatus {
  return typeof value === "string" && MIN_STATUS_SET.has(value);
}

/** Read one suite row; a malformed row is dropped rather than crashing the list. */
function readSuiteRow(row: unknown): CompatibilitySuiteDraft | null {
  if (!isRecord(row)) return null;
  const suiteId = typeof row.suiteId === "string" ? row.suiteId.trim() : "";
  if (suiteId === "" || !isSuiteLayer(row.evidenceLayer)) return null;
  return { suiteId, evidenceLayer: row.evidenceLayer };
}

export function normalizeCompatibilitySuites(raw: unknown): CompatibilitySuiteDraft[] {
  if (!Array.isArray(raw)) return [];
  const suites: CompatibilitySuiteDraft[] = [];
  for (const row of raw) {
    const suite = readSuiteRow(row);
    if (suite) suites.push(suite);
  }
  return suites;
}

/**
 * Normalize an optional compatibility DTO. Anything that is not an object is
 * treated as "no compatibility policy" instead of crashing the editor.
 */
export function normalizeCompatibilityDto(raw: unknown): RoutingProfileCompatibility | undefined {
  if (!isRecord(raw)) return undefined;
  const compatibility: RoutingProfileCompatibility = {
    requiredSuites: normalizeCompatibilitySuites(raw.requiredSuites),
  };
  if (isMinStatus(raw.minStatus)) compatibility.minStatus = raw.minStatus;
  if (typeof raw.maxEvidenceAgeMs === "number" && Number.isFinite(raw.maxEvidenceAgeMs) && raw.maxEvidenceAgeMs >= 0) {
    compatibility.maxEvidenceAgeMs = raw.maxEvidenceAgeMs;
  }
  if (isEvidenceMode(raw.unknownEvidence)) compatibility.unknownEvidence = raw.unknownEvidence;
  if (isEvidenceMode(raw.degradedEvidence)) compatibility.degradedEvidence = raw.degradedEvidence;
  return compatibility;
}

function draftText(value: string | number | undefined): string {
  return value === undefined ? "" : String(value);
}

/** Absent/true/false map onto the blank/"true"/"false" editor control. */
function draftTriState(value: boolean | undefined): OptionalBoolean {
  if (value === true) return "true";
  if (value === false) return "false";
  return "";
}

function omitBlank(value: string): string | undefined {
  const trimmed = value.trim();
  return trimmed === "" ? undefined : trimmed;
}

/** Blank editor input means "absent"; it never becomes zero. */
function wireNumber(value: string): number | undefined {
  const trimmed = value.trim();
  return trimmed === "" ? undefined : Number(trimmed);
}

function wireTriState(value: OptionalBoolean): boolean | undefined {
  if (value === "true") return true;
  if (value === "false") return false;
  return undefined;
}

export function routingProfileDraftFromDto(profile: RoutingProfileDto): RoutingProfileDraft {
  return {
    id: profile.id,
    alias: profile.alias ?? "",
    icon: profile.icon?.trim() || ROUTING_PROFILE_DEFAULT_ICON,
    candidates: profile.candidates.map(candidate => newDraftCandidate(candidate.provider, candidate.model)),
    require: draftRequirement(profile.require),
    optimize: draftWeights(profile.optimize),
    limits: draftLimits(profile.limits),
    unknownEvidence: { ...profile.unknownEvidence },
    compatibility: draftCompatibility(normalizeCompatibilityDto(profile.compatibility)),
  };
}

function draftRequirement(requirement: RoutingProfileRequirement): RoutingProfileDraftRequirement {
  return {
    minContextWindow: draftText(requirement.minContextWindow),
    minQuotaHeadroom: draftText(requirement.minQuotaHeadroom),
    tools: draftTriState(requirement.tools),
    imageInput: draftTriState(requirement.imageInput),
    structuredOutput: draftTriState(requirement.structuredOutput),
    reasoningEffort: requirement.reasoningEffort ?? "",
    serviceTier: requirement.serviceTier ?? "",
    localOnly: draftTriState(requirement.localOnly),
    remoteAllowed: draftTriState(requirement.remoteAllowed),
    encryptedCodexTasks: draftTriState(requirement.encryptedCodexTasks),
  };
}

function draftWeights(weights: RoutingProfileOptimizeWeights): RoutingProfileDraftOptimize {
  return {
    latency: String(weights.latency),
    health: String(weights.health),
    cost: String(weights.cost),
    quota: String(weights.quota),
  };
}

function draftLimits(limits: RoutingProfileLimits): RoutingProfileDraftLimits {
  return {
    maxEstimatedCostUsd: draftText(limits.maxEstimatedCostUsd),
    onUnknownCost: limits.onUnknownCost === "exclude" ? "exclude" : "allow",
  };
}

function draftCompatibility(
  compatibility: RoutingProfileCompatibility | undefined,
): RoutingProfileDraftCompatibility {
  return {
    enabled: compatibility !== undefined,
    requiredSuites: compatibility?.requiredSuites ?? [],
    minStatus: compatibility?.minStatus ?? "",
    maxEvidenceAgeMs: draftText(compatibility?.maxEvidenceAgeMs),
    unknownEvidence: compatibility?.unknownEvidence ?? "exclude",
    degradedEvidence: compatibility?.degradedEvidence ?? "penalize",
  };
}

function isOptionalBoolean(value: string): value is OptionalBoolean {
  return value === "" || value === "true" || value === "false";
}

/** Requirement readers keyed by the model's field kind. */
const REQUIREMENT_READERS: Record<RequirementKind, (cell: string) => unknown> = {
  number: cell => wireNumber(cell),
  flag: cell => (isOptionalBoolean(cell) ? wireTriState(cell) : undefined),
  text: cell => omitBlank(cell),
};

/**
 * Requirement fields in table order. A blank editor value means "not specified"
 * and is omitted; nothing is collapsed into an explicit server default.
 */
function wireRequirement(draft: RoutingProfileDraftRequirement): Record<string, unknown> {
  const wire: Record<string, unknown> = {};
  for (const [field, kind] of PROFILE_REQUIREMENT_FIELDS) {
    const value = REQUIREMENT_READERS[kind](draft[field]);
    if (value !== undefined) wire[field] = value;
  }
  return wire;
}

/** `allow` is the server default, so it is represented by omission. */
function wireLimits(draft: RoutingProfileDraftLimits): Record<string, unknown> {
  const wire: Record<string, unknown> = {};
  const cap = wireNumber(draft.maxEstimatedCostUsd);
  if (cap !== undefined) wire.maxEstimatedCostUsd = cap;
  if (draft.onUnknownCost === "exclude") wire.onUnknownCost = "exclude";
  return wire;
}

function wireCandidate(candidate: RoutingProfileDraftCandidate): RoutingProfileCandidate {
  return { provider: candidate.provider.trim(), model: candidate.model.trim() };
}

/** A disabled compatibility policy is omitted entirely, never sent as empty. */
function wireCompatibility(
  draft: RoutingProfileDraftCompatibility,
): Record<string, unknown> | undefined {
  if (!draft.enabled) return undefined;
  const wire: Record<string, unknown> = { requiredSuites: draft.requiredSuites };
  const minStatus = draft.minStatus || undefined;
  if (minStatus !== undefined) wire.minStatus = minStatus;
  const maxEvidenceAgeMs = wireNumber(draft.maxEvidenceAgeMs);
  if (maxEvidenceAgeMs !== undefined) wire.maxEvidenceAgeMs = maxEvidenceAgeMs;
  wire.unknownEvidence = draft.unknownEvidence;
  wire.degradedEvidence = draft.degradedEvidence;
  return wire;
}

export type RoutingProfileWriteMode = "create" | "update";

export type RoutingProfilePutBody = {
  mode: RoutingProfileWriteMode;
  id: string;
  expectedRevision?: string;
  profile: Record<string, unknown>;
};

export function routingProfilePutBody(
  draft: RoutingProfileDraft,
  mode: RoutingProfileWriteMode,
  expectedRevision?: string,
): RoutingProfilePutBody {
  const profile: Record<string, unknown> = {};
  const alias = omitBlank(draft.alias);
  if (alias !== undefined) profile.alias = alias;
  profile.icon = draft.icon.trim() || ROUTING_PROFILE_DEFAULT_ICON;
  profile.candidates = draft.candidates.map(wireCandidate);
  const requirement = wireRequirement(draft.require);
  if (Object.keys(requirement).length > 0) profile.require = requirement;
  profile.optimize = {
    latency: Number(draft.optimize.latency),
    health: Number(draft.optimize.health),
    cost: Number(draft.optimize.cost),
    quota: Number(draft.optimize.quota),
  };
  const limits = wireLimits(draft.limits);
  if (Object.keys(limits).length > 0) profile.limits = limits;
  profile.unknownEvidence = { ...draft.unknownEvidence };
  const compatibility = wireCompatibility(draft.compatibility);
  if (compatibility !== undefined) profile.compatibility = compatibility;

  return {
    mode,
    id: draft.id.trim(),
    ...(mode === "update" && expectedRevision ? { expectedRevision } : {}),
    profile,
  };
}

function isRoutingProfileDto(profile: unknown): profile is RoutingProfileDto {
  if (!isRecord(profile)) return false;
  if (![profile.id, profile.model, profile.revision].every(field => typeof field === "string")) return false;
  if (!Array.isArray(profile.candidates)) return false;
  return [profile.require, profile.optimize, profile.limits, profile.unknownEvidence].every(isRecord);
}

function normalizeRoutingProfileDto(profile: RoutingProfileDto): RoutingProfileDto {
  const { compatibility: rawCompatibility, ...rest } = profile;
  const compatibility = normalizeCompatibilityDto(rawCompatibility);
  const icon = typeof profile.icon === "string" && profile.icon.trim() !== ""
    ? profile.icon.trim()
    : null;
  const normalized = { ...rest, alias: profile.alias ?? null, icon };
  return compatibility === undefined ? normalized : { ...normalized, compatibility };
}

/** Structurally invalid rows are skipped rather than crashing the editor list. */
export function parseRoutingProfiles(raw: unknown): RoutingProfileDto[] {
  if (!isRecord(raw) || !Array.isArray(raw.profiles)) return [];
  const profiles: RoutingProfileDto[] = [];
  for (const entry of raw.profiles) {
    if (isRoutingProfileDto(entry)) profiles.push(normalizeRoutingProfileDto(entry));
  }
  return profiles;
}
