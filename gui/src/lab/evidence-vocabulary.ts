/**
 * Compatibility Matrix domain vocabulary.
 *
 * The Lab store schema reserves three evidence layers. Benes can only produce
 * the first two, so the matrix and its filters expose `PRODUCT_EVIDENCE_LAYERS`
 * while `task_effectiveness` stays reachable through the schema type only.
 */

export type EvidenceLayer =
  | "protocol_conformance"
  | "live_route_compatibility"
  | "task_effectiveness";

export type ProductEvidenceLayer = Exclude<EvidenceLayer, "task_effectiveness">;

export type CompatibilityVerdict =
  | "UNKNOWN"
  | "CLAIMED"
  | "PROBED"
  | "VERIFIED"
  | "DEGRADED"
  | "BLOCKED"
  | "UNSUPPORTED";

export type ArtifactStatus = "present" | "corrupt" | "purged_unavailable";

/** Every layer the projection schema can carry. */
export const EVIDENCE_LAYERS: readonly EvidenceLayer[] = [
  "protocol_conformance",
  "live_route_compatibility",
  "task_effectiveness",
];

/** Layers the matrix and its filters may offer. */
export const PRODUCT_EVIDENCE_LAYERS: readonly ProductEvidenceLayer[] = [
  "protocol_conformance",
  "live_route_compatibility",
];

export const COMPATIBILITY_VERDICTS: readonly CompatibilityVerdict[] = [
  "UNKNOWN",
  "CLAIMED",
  "PROBED",
  "VERIFIED",
  "DEGRADED",
  "BLOCKED",
  "UNSUPPORTED",
];

export const ARTIFACT_STATUSES: readonly ArtifactStatus[] = [
  "present",
  "corrupt",
  "purged_unavailable",
];

const EVIDENCE_LAYER_SET = new Set<string>(EVIDENCE_LAYERS);
const VERDICT_SET = new Set<string>(COMPATIBILITY_VERDICTS);
const ARTIFACT_STATUS_SET = new Set<string>(ARTIFACT_STATUSES);

export function isEvidenceLayer(value: unknown): value is EvidenceLayer {
  return typeof value === "string" && EVIDENCE_LAYER_SET.has(value);
}

export function isCompatibilityVerdict(value: unknown): value is CompatibilityVerdict {
  return typeof value === "string" && VERDICT_SET.has(value);
}

export function isArtifactStatus(value: unknown): value is ArtifactStatus {
  return typeof value === "string" && ARTIFACT_STATUS_SET.has(value);
}

/**
 * Community bundles arrive from outside this machine and are never counted as
 * evidence Benes produced, so the class is a wire literal rather than a
 * vocabulary the dashboard may widen.
 */
export const COMMUNITY_TRUST_CLASS = "community_untrusted_v1";

export type CommunityTrustClass = typeof COMMUNITY_TRUST_CLASS;

/** The only cryptographic outcome a community summary row may report. */
export const COMMUNITY_ROW_STATUS = "cryptographically_valid";

export type CommunityRowStatus = typeof COMMUNITY_ROW_STATUS;

export function isCommunityTrustClass(value: unknown): value is CommunityTrustClass {
  return value === COMMUNITY_TRUST_CLASS;
}

export function isCommunityRowStatus(value: unknown): value is CommunityRowStatus {
  return value === COMMUNITY_ROW_STATUS;
}

/** A digest exactly as published: 64 lowercase SHA-256 hex characters. */
export function isSha256Hex(value: unknown): value is string {
  return typeof value === "string" && /^[0-9a-f]{64}$/.test(value);
}

/** A record tally: a whole, non-negative number inside the safe range. */
export function isRecordCount(value: unknown): value is number {
  return typeof value === "number" && Number.isSafeInteger(value) && value >= 0;
}
