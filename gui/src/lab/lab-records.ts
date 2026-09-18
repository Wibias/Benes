/**
 * Compatibility Matrix wire records.
 *
 * Every `GET /api/lab/*` payload is validated against a declarative shape
 * before it is handed to the dashboard. A record that fails its shape is
 * rejected outright; unknown extra keys are preserved so the raw projection
 * stays available to callers that read them.
 */

import {
  isArtifactStatus,
  isCommunityRowStatus,
  isCommunityTrustClass,
  isCompatibilityVerdict,
  isEvidenceLayer,
  isRecordCount,
  isSha256Hex,
  type ArtifactStatus,
  type CommunityRowStatus,
  type CommunityTrustClass,
  type CompatibilityVerdict,
  type EvidenceLayer,
} from "./evidence-vocabulary.ts";

type Cells = Record<string, unknown>;

type FieldKind =
  | "text"
  | "filled"
  | "textOrNull"
  | "flag"
  | "number"
  | "numberOrNull"
  | "textList"
  | "layer"
  | "verdict"
  | "artifact"
  | "communityTrust"
  | "communityStatus"
  | "sha256"
  | "count"
  | "exactlyFalse"
  | "communityEvidence";

type RecordShape = {
  readonly required: Readonly<Record<string, FieldKind>>;
  readonly optional: Readonly<Record<string, FieldKind>>;
  /** Fields the reader carries through without validating them. */
  readonly passthrough?: Readonly<Record<string, FieldKind>>;
  /** A closed record admits no key the shape does not name. */
  readonly closed?: true;
};

/** Wire value implied by one schema field kind. */
type KindValue<TKind> =
  TKind extends "text" ? string
  : TKind extends "filled" ? string
  : TKind extends "textOrNull" ? string | null
  : TKind extends "flag" ? boolean
  : TKind extends "number" ? number
  : TKind extends "numberOrNull" ? number | null
  : TKind extends "textList" ? string[]
  : TKind extends "layer" ? EvidenceLayer
  : TKind extends "verdict" ? CompatibilityVerdict
  : TKind extends "artifact" ? ArtifactStatus
  : TKind extends "communityTrust" ? CommunityTrustClass
  : TKind extends "communityStatus" ? CommunityRowStatus
  : TKind extends "sha256" ? string
  : TKind extends "count" ? number
  : TKind extends "exactlyFalse" ? false
  : TKind extends "communityEvidence" ? CommunityEvidenceSummaryRowDto[]
  : never;

type WireFields<TFields extends Readonly<Record<string, FieldKind>>> = {
  [K in keyof TFields]: KindValue<TFields[K]>;
};

/** The wire record type implied by a validation shape. */
type RecordOf<
  TRequired extends Readonly<Record<string, FieldKind>>,
  TOptional extends Readonly<Record<string, FieldKind>>,
  TPassthrough extends Readonly<Record<string, FieldKind>> = Record<never, never>,
> = WireFields<TRequired> & Partial<WireFields<TOptional>> & Partial<WireFields<TPassthrough>>;

/** Subject detail is an open projection: only `subjectKind` is guaranteed. */
export type SubjectDetailDto = {
  subjectKind: string;
  subjectSchemaVersion?: number;
  [key: string]: unknown;
};

/** Event detail deliberately drops `payload_json`; see `parseLabEvent`. */

export type PassiveProductionSummaryDto = {
  verificationStatus: "not_verification";
  summary: {
    subjectId: string;
    verificationStatus: "not_verification";
    recentProductionAttempts: number;
    recentSuccessfulAttempts: number;
    recentRouteErrorSignals: number;
    lastObservedProductionAttempt?: number;
  };
};


const LAB_STATUS_SHAPE = {
  required: { projectionAvailable: "flag" },
  optional: {
    projectionIncompatible: "flag",
    projectionSpecVersion: "text",
    sqliteSchemaVersion: "number",
    builtAtMs: "number",
    eventCount: "number",
    subjectCount: "number",
    observationCount: "number",
    claimCount: "number",
    verdictCount: "number",
    artifactCount: "number",
    corruptionCount: "number",
  },
} as const satisfies RecordShape;

const VERDICT_SHAPE = {
  required: {
    projectionKey: "filled",
    subjectId: "filled",
    evidenceLayer: "layer",
    suiteId: "filled",
    suiteVersion: "text",
    suiteManifestDigest: "text",
    projectionSpecVersion: "text",
    verdict: "verdict",
    asOf: "number",
    scenarioManifestDigests: "textList",
    claimSourceDigest: "textOrNull",
    contributingEventIds: "textList",
    contradictingEventIds: "textList",
    notes: "textList",
  },
  optional: {},
} as const satisfies RecordShape;

const SUBJECT_ROW_SHAPE = {
  required: { subjectId: "text", subjectKind: "text" },
  optional: {},
} as const satisfies RecordShape;

/** Subject detail is an open projection: only `subjectKind` is guaranteed. */
const SUBJECT_DETAIL_SHAPE = {
  required: { subjectKind: "text" },
  optional: {},
} as const satisfies RecordShape;

const OBSERVATION_SHAPE = {
  required: {
    eventId: "text",
    subjectId: "text",
    evidenceLayer: "layer",
    suiteId: "text",
    suiteVersion: "text",
    suiteManifestDigest: "text",
    scenarioId: "text",
    scenarioVersion: "text",
    scenarioManifestDigest: "text",
    outcome: "text",
    completedAt: "number",
    executionMode: "text",
    excluded: "flag",
    exclusionReason: "textOrNull",
  },
  optional: {},
} as const satisfies RecordShape;

/** Event detail carries no scenario identity in the current server contract. */
const EVENT_SHAPE = {
  required: {
    eventKind: "text",
    eventId: "text",
    recordedAt: "number",
    producer: "text",
    producerVersion: "text",
    excluded: "flag",
    exclusionReason: "textOrNull",
  },
  optional: {},
  /** Present in the server payload; the generic event reader does not validate them. */
  passthrough: {
    subjectId: "text",
    evidenceLayer: "layer",
    suiteId: "text",
    outcome: "text",
  },
} as const satisfies RecordShape;

const ARTIFACT_SHAPE = {
  required: {
    digest: "text",
    artifactClass: "textOrNull",
    mediaType: "textOrNull",
    byteCount: "numberOrNull",
    status: "artifact",
    lastError: "textOrNull",
  },
  optional: {},
} as const satisfies RecordShape;

const PRODUCTION_SHAPE = {
  required: {
    subjectId: "text",
    verificationStatus: "text",
    recentProductionAttempts: "number",
    recentSuccessfulAttempts: "number",
    recentRouteErrorSignals: "number",
  },
  optional: { lastObservedProductionAttempt: "number" },
} as const satisfies RecordShape;

/**
 * One summary row of a published community bundle.
 *
 * The row is closed: a summary Benes cannot read in full is not evidence it may
 * shorten. Both digests must be exactly what the publisher signed, and the two
 * tallies are whole counts — a negative or fractional record count means the
 * summary did not come from the projector.
 */
const COMMUNITY_ROW_SHAPE = {
  closed: true,
  required: {
    trustClass: "communityTrust",
    status: "communityStatus",
    bundleId: "sha256",
    publisherKeyId: "sha256",
    activeRecordCount: "count",
    revokedRecordCount: "count",
  },
  optional: {},
} as const satisfies RecordShape;

/** Most community rows one read may carry before the read is malformed. */
const COMMUNITY_EVIDENCE_LIMIT = 4096;

/**
 * `GET /api/lab/public/community`: untrusted, read-only context beside a local
 * verdict. The envelope is closed, and `locallyVerified` can only ever be
 * false — this read never promotes a community bundle into local evidence.
 */
const COMMUNITY_ENVELOPE_SHAPE = {
  closed: true,
  required: {
    evidence: "communityEvidence",
    trustClass: "communityTrust",
    locallyVerified: "exactlyFalse",
  },
  optional: {},
} as const satisfies RecordShape;

/** Unstructured scenario material that must never reach the GUI. */
const EVENT_DETAIL_OMISSIONS = new Set(["payload_json"]);

export type LabStatusDto = RecordOf<typeof LAB_STATUS_SHAPE.required, typeof LAB_STATUS_SHAPE.optional>;
export type VerdictDto = RecordOf<typeof VERDICT_SHAPE.required, typeof VERDICT_SHAPE.optional>;
export type SubjectListItemDto = RecordOf<typeof SUBJECT_ROW_SHAPE.required, typeof SUBJECT_ROW_SHAPE.optional>;
export type ObservationDto = RecordOf<typeof OBSERVATION_SHAPE.required, typeof OBSERVATION_SHAPE.optional>;
export type LabEventDto = RecordOf<
  typeof EVENT_SHAPE.required,
  typeof EVENT_SHAPE.optional,
  typeof EVENT_SHAPE.passthrough
>;
export type ArtifactMetadataDto = RecordOf<typeof ARTIFACT_SHAPE.required, typeof ARTIFACT_SHAPE.optional>;
export type CommunityEvidenceSummaryRowDto = RecordOf<
  typeof COMMUNITY_ROW_SHAPE.required,
  typeof COMMUNITY_ROW_SHAPE.optional
>;
export type CommunityEvidenceContextDto = RecordOf<
  typeof COMMUNITY_ENVELOPE_SHAPE.required,
  typeof COMMUNITY_ENVELOPE_SHAPE.optional
>;

function accepts(kind: FieldKind, value: unknown): boolean {
  switch (kind) {
    case "text":
      return typeof value === "string";
    case "filled":
      return typeof value === "string" && value.length > 0;
    case "textOrNull":
      return value === null || typeof value === "string";
    case "flag":
      return typeof value === "boolean";
    case "number":
      return typeof value === "number" && Number.isFinite(value);
    case "numberOrNull":
      return value === null || (typeof value === "number" && Number.isFinite(value));
    case "textList":
      return Array.isArray(value) && value.every(entry => typeof entry === "string");
    case "layer":
      return isEvidenceLayer(value);
    case "verdict":
      return isCompatibilityVerdict(value);
    case "artifact":
      return isArtifactStatus(value);
    case "communityTrust":
      return isCommunityTrustClass(value);
    case "communityStatus":
      return isCommunityRowStatus(value);
    case "sha256":
      return isSha256Hex(value);
    case "count":
      return isRecordCount(value);
    case "exactlyFalse":
      return value === false;
    case "communityEvidence":
      return Array.isArray(value)
        && value.length <= COMMUNITY_EVIDENCE_LIMIT
        && value.every(entry => isPlainObject(entry) && conforms(entry, COMMUNITY_ROW_SHAPE));
  }
}

export function isPlainObject(value: unknown): value is Cells {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function conforms(cells: Cells, shape: RecordShape): boolean {
  for (const [key, kind] of Object.entries(shape.required)) {
    if (!(key in cells) || !accepts(kind, cells[key])) return false;
  }
  for (const [key, kind] of Object.entries(shape.optional ?? {})) {
    if (cells[key] === undefined) continue;
    if (!accepts(kind, cells[key])) return false;
  }
  if (shape.closed && !admitsOnly(cells, shape)) return false;
  return true;
}

/** A closed record admits exactly the keys its shape names. */
function admitsOnly(cells: Cells, shape: RecordShape): boolean {
  const named = new Set([
    ...Object.keys(shape.required),
    ...Object.keys(shape.optional ?? {}),
    ...Object.keys(shape.passthrough ?? {}),
  ]);
  return Object.keys(cells).every(key => named.has(key));
}

function asDto<T>(cells: Cells, shape: RecordShape): T | null {
  return conforms(cells, shape) ? (cells as T) : null;
}

function unwrap(raw: unknown, key: string): Cells | null {
  if (!isPlainObject(raw)) return null;
  const inner = raw[key];
  return isPlainObject(inner) ? inner : null;
}

function withoutOmitted(cells: Cells, omissions: ReadonlySet<string>): Cells {
  const kept: Cells = {};
  for (const [key, value] of Object.entries(cells)) {
    if (!omissions.has(key)) kept[key] = value;
  }
  return kept;
}

export function parseLabStatus(raw: unknown): LabStatusDto | null {
  return isPlainObject(raw) ? asDto<LabStatusDto>(raw, LAB_STATUS_SHAPE) : null;
}

export function parseVerdictDto(raw: unknown): VerdictDto | null {
  return isPlainObject(raw) ? asDto<VerdictDto>(raw, VERDICT_SHAPE) : null;
}

export function parseSubjectRow(raw: unknown): SubjectListItemDto | null {
  return isPlainObject(raw) ? asDto<SubjectListItemDto>(raw, SUBJECT_ROW_SHAPE) : null;
}

export function parseSubjectDetail(raw: unknown): SubjectDetailDto | null {
  const subject = unwrap(raw, "subject");
  return subject ? asDto<SubjectDetailDto>(subject, SUBJECT_DETAIL_SHAPE) : null;
}

export function parseObservationDto(raw: unknown): ObservationDto | null {
  return isPlainObject(raw) ? asDto<ObservationDto>(raw, OBSERVATION_SHAPE) : null;
}

export function parseLabEvent(raw: unknown): LabEventDto | null {
  const event = unwrap(raw, "event");
  if (!event) return null;
  return asDto<LabEventDto>(withoutOmitted(event, EVENT_DETAIL_OMISSIONS), EVENT_SHAPE);
}

export function parseArtifactMetadata(raw: unknown): ArtifactMetadataDto | null {
  const artifact = unwrap(raw, "artifact");
  return artifact ? asDto<ArtifactMetadataDto>(artifact, ARTIFACT_SHAPE) : null;
}

export function parsePassiveProductionSummary(raw: unknown): PassiveProductionSummaryDto | null {
  if (!isPlainObject(raw) || raw.verificationStatus !== "not_verification") return null;
  const summary = unwrap(raw, "summary");
  if (!summary || summary.verificationStatus !== "not_verification") return null;
  if (!conforms(summary, PRODUCTION_SHAPE)) return null;
  return {
    verificationStatus: "not_verification",
    summary: summary as PassiveProductionSummaryDto["summary"],
  };
}

/**
 * Read the community-evidence envelope.
 *
 * Every row is validated against the closed row shape before the envelope is
 * handed on; one unreadable row fails the whole read, because a shortened list
 * would read as "the publisher published less" instead of "Benes could not
 * read this".
 */
export function parseCommunityEvidenceContext(raw: unknown): CommunityEvidenceContextDto | null {
  return isPlainObject(raw) ? asDto<CommunityEvidenceContextDto>(raw, COMMUNITY_ENVELOPE_SHAPE) : null;
}
