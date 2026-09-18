/**
 * Routing-profile editor model.
 *
 * One ordered field table per profile section is the single source of truth for
 * the wire types, the editor mirror types, the blank draft, and the codec. The
 * DTO and the draft stay separate on purpose: only the codec may cross them.
 */

import { ROUTING_PROFILE_DEFAULT_ICON } from "../pages/routing-profile-icon-data.ts";

/** Re-exported so the model, the codec and the form share one icon default. */
export { ROUTING_PROFILE_DEFAULT_ICON };

export const UNKNOWN_EVIDENCE_MODES = ["allow", "penalize", "exclude"] as const;
export type UnknownEvidenceMode = (typeof UNKNOWN_EVIDENCE_MODES)[number];

export const UNKNOWN_COST_CAP_MODES = ["allow", "exclude"] as const;
export type UnknownCostCapMode = (typeof UNKNOWN_COST_CAP_MODES)[number];

/** Suite layers the profile editor offers. `task_effectiveness` is not one. */
export const PROFILE_SUITE_LAYERS = ["protocol_conformance", "live_route_compatibility"] as const;
export type ProfileSuiteLayer = (typeof PROFILE_SUITE_LAYERS)[number];

export const COMPATIBILITY_MIN_STATUSES = ["PROBED", "VERIFIED"] as const;
export type CompatibilityMinStatus = (typeof COMPATIBILITY_MIN_STATUSES)[number];

export const OPTIONAL_BOOLEAN_VALUES = ["", "true", "false"] as const;
/** Tri-state editor control: blank means "not specified" and is omitted on the wire. */
export type OptionalBoolean = (typeof OPTIONAL_BOOLEAN_VALUES)[number];

export const OPTIMIZE_WEIGHTS = ["latency", "health", "cost", "quota"] as const;
export type OptimizeWeight = (typeof OPTIMIZE_WEIGHTS)[number];

export const UNKNOWN_EVIDENCE_TARGETS = ["capability", "health", "quota", "cost"] as const;
export type UnknownEvidenceTarget = (typeof UNKNOWN_EVIDENCE_TARGETS)[number];

type RequirementKindName = "number" | "flag" | "text";

/**
 * Requirement fields in wire order, keyed by how the value crosses the two
 * representations: `number` is a numeric requirement, `flag` a tri-state
 * boolean, `text` a free-text requirement. Order is the contract.
 */
export const PROFILE_REQUIREMENT_FIELDS = [
  ["minContextWindow", "number"],
  ["minQuotaHeadroom", "number"],
  ["tools", "flag"],
  ["imageInput", "flag"],
  ["structuredOutput", "flag"],
  ["reasoningEffort", "text"],
  ["serviceTier", "text"],
  ["localOnly", "flag"],
  ["remoteAllowed", "flag"],
  ["encryptedCodexTasks", "flag"],
] as const satisfies ReadonlyArray<readonly [string, RequirementKindName]>;

type RequirementEntry = (typeof PROFILE_REQUIREMENT_FIELDS)[number];
export type RequirementField = RequirementEntry[0];
export type RequirementKind = RequirementEntry[1];

/** The kind declared for one requirement field. */
type KindOf<Field extends RequirementField> = Extract<RequirementEntry, readonly [Field, RequirementKindName]>[1];

type WireValue<TKind> = TKind extends "number" ? number : TKind extends "flag" ? boolean : string;
type EditorValue<TKind> = TKind extends "number" ? string : TKind extends "flag" ? OptionalBoolean : string;

export type CompatibilitySuiteDraft = {
  suiteId: string;
  evidenceLayer: ProfileSuiteLayer;
};

export type RoutingProfileCandidate = {
  provider: string;
  model: string;
};

/**
 * Draft-only candidate carrying a stable client-side identity for list keys.
 * The key never reaches the server: the PUT serializer strips it.
 */
export type RoutingProfileDraftCandidate = RoutingProfileCandidate & { key: string };

export type ModelOption = {
  provider: string;
  id: string;
};

/** Candidate eligibility only; the generic request policy owns wire tiers. */
export type RoutingProfileRequirement = Partial<{
  [Field in RequirementField]: WireValue<KindOf<Field>>;
}>;

export type RoutingProfileOptimizeWeights = Record<OptimizeWeight, number>;

export type RoutingProfileLimits = {
  maxEstimatedCostUsd?: number;
  onUnknownCost?: UnknownCostCapMode;
};

export type RoutingProfileUnknownEvidence = Record<UnknownEvidenceTarget, UnknownEvidenceMode>;

export type RoutingProfileCompatibility = {
  requiredSuites: CompatibilitySuiteDraft[];
  minStatus?: CompatibilityMinStatus;
  maxEvidenceAgeMs?: number;
  unknownEvidence?: UnknownEvidenceMode;
  degradedEvidence?: UnknownEvidenceMode;
};

export type RoutingProfileDto = {
  id: string;
  alias: string | null;
  icon: string | null;
  model: string;
  revision: string;
  candidates: RoutingProfileCandidate[];
  require: RoutingProfileRequirement;
  optimize: RoutingProfileOptimizeWeights;
  limits: RoutingProfileLimits;
  unknownEvidence: RoutingProfileUnknownEvidence;
  compatibility?: RoutingProfileCompatibility;
};

export type RoutingProfileDraftRequirement = {
  [Field in RequirementField]: EditorValue<KindOf<Field>>;
};

export type RoutingProfileDraftOptimize = Record<OptimizeWeight, string>;

export type RoutingProfileDraftLimits = {
  maxEstimatedCostUsd: string;
  onUnknownCost: UnknownCostCapMode;
};

export type RoutingProfileDraftCompatibility = {
  enabled: boolean;
  requiredSuites: CompatibilitySuiteDraft[];
  minStatus: "" | CompatibilityMinStatus;
  maxEvidenceAgeMs: string;
  unknownEvidence: UnknownEvidenceMode;
  degradedEvidence: UnknownEvidenceMode;
};

export type RoutingProfileDraft = {
  id: string;
  alias: string;
  icon: string;
  candidates: RoutingProfileDraftCandidate[];
  require: RoutingProfileDraftRequirement;
  optimize: RoutingProfileDraftOptimize;
  limits: RoutingProfileDraftLimits;
  unknownEvidence: RoutingProfileUnknownEvidence;
  compatibility: RoutingProfileDraftCompatibility;
};

/** Shipped new-profile weights, in weight order. */
const DEFAULT_WEIGHT_VALUES: ReadonlyArray<readonly [OptimizeWeight, string]> = [
  ["latency", "0.55"],
  ["health", "0.25"],
  ["cost", "0.1"],
  ["quota", "0.1"],
];

/** Shipped new-profile unknown-evidence policy, in target order. */
const DEFAULT_UNKNOWN_EVIDENCE_VALUES: ReadonlyArray<readonly [UnknownEvidenceTarget, UnknownEvidenceMode]> = [
  ["capability", "exclude"],
  ["health", "penalize"],
  ["quota", "penalize"],
  ["cost", "penalize"],
];

/** Presentation defaults for an absent compatibility policy. */
const DEFAULT_COMPATIBILITY = {
  minStatus: "",
  unknownEvidence: "exclude",
  degradedEvidence: "penalize",
} as const;

function recordFrom<TValue>(entries: ReadonlyArray<readonly [string, TValue]>): Record<string, TValue> {
  const record: Record<string, TValue> = {};
  for (const [key, value] of entries) record[key] = value;
  return record;
}

/** Fresh weight object for a new draft. */
export function newDraftOptimize(): RoutingProfileDraftOptimize {
  return recordFrom(DEFAULT_WEIGHT_VALUES) as RoutingProfileDraftOptimize;
}

/** Fresh unknown-evidence object for a new draft. */
export function newDraftUnknownEvidence(): RoutingProfileUnknownEvidence {
  return recordFrom(DEFAULT_UNKNOWN_EVIDENCE_VALUES) as RoutingProfileUnknownEvidence;
}

/**
 * Blank requirement for a new draft. Every key comes from the same field table
 * the draft type is derived from, so the result is complete by construction.
 */
function newDraftRequirement(): RoutingProfileDraftRequirement {
  const blank: Record<string, string> = recordFrom(
    PROFILE_REQUIREMENT_FIELDS.map(([field]) => [field, ""] as const),
  );
  return blank as unknown as RoutingProfileDraftRequirement;
}

let candidateSequence = 0;

function nextCandidateKey(): string {
  candidateSequence += 1;
  return `candidate-${candidateSequence}`;
}

/** Create a draft candidate with a fresh, editor-stable key. */
export function newDraftCandidate(provider: string, model: string): RoutingProfileDraftCandidate {
  return { provider, model, key: nextCandidateKey() };
}

export function newRoutingProfileDraft(provider = "", model = ""): RoutingProfileDraft {
  return {
    id: "",
    alias: "",
    icon: ROUTING_PROFILE_DEFAULT_ICON,
    candidates: [newDraftCandidate(provider, model)],
    require: newDraftRequirement(),
    optimize: newDraftOptimize(),
    limits: { maxEstimatedCostUsd: "", onUnknownCost: "allow" },
    unknownEvidence: newDraftUnknownEvidence(),
    compatibility: {
      enabled: false,
      requiredSuites: [],
      minStatus: DEFAULT_COMPATIBILITY.minStatus,
      maxEvidenceAgeMs: "",
      unknownEvidence: DEFAULT_COMPATIBILITY.unknownEvidence,
      degradedEvidence: DEFAULT_COMPATIBILITY.degradedEvidence,
    },
  };
}
