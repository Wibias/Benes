/**
 * Compatibility Matrix aggregation.
 *
 * Verdicts from the Lab projection are folded into one row per Subject+Suite so
 * the board can render a per-layer verdict table. Row identity is a structured
 * tuple, never a delimiter-joined string, because subject and suite ids may
 * contain arbitrary UTF-8.
 */

import type { SubjectListItemDto, VerdictDto } from "./lab-records.ts";
import type { CompatibilityVerdict, EvidenceLayer } from "./evidence-vocabulary.ts";

export {
  COMPATIBILITY_VERDICTS,
  EVIDENCE_LAYERS,
  PRODUCT_EVIDENCE_LAYERS,
} from "./evidence-vocabulary.ts";
export type {
  CompatibilityVerdict,
  EvidenceLayer,
  ProductEvidenceLayer,
} from "./evidence-vocabulary.ts";
export type { SubjectListItemDto, VerdictDto } from "./lab-records.ts";

/** One matrix table row: Subject + Suite, with at most one verdict per layer. */
export type MatrixRow = {
  subjectId: string;
  /** Empty means the subject page did not contain a matching row. */
  subjectKind: string;
  suiteId: string;
  /** Render the Subject cell only on the first suite row of each subject group. */
  showSubject: boolean;
  byLayer: Record<EvidenceLayer, VerdictDto | null>;
};

/** UI filter state. `subjectQuery` is a client-side substring match on subjectId. */
export type VerdictFilters = {
  layer: EvidenceLayer | "";
  verdict: CompatibilityVerdict | "";
  subjectQuery: string;
  suiteId: string;
};

/**
 * The filter bar before a reader has narrowed anything.
 *
 * The four blanks are one value, not four: every board starts here, and a caller that spelled
 * them itself could leave one out and quietly filter on it.
 */
export const UNFILTERED_VERDICT_FILTERS: VerdictFilters = {
  layer: "",
  verdict: "",
  subjectQuery: "",
  suiteId: "",
};

/** Exact server-side verdict filters. */
export type VerdictQueryFilters = {
  layer?: EvidenceLayer;
  verdict?: CompatibilityVerdict;
  subjectId?: string;
  suiteId?: string;
};

function sortIds(ids: Iterable<string>): string[] {
  return [...ids].toSorted((left, right) => left.localeCompare(right));
}

function emptyLayerSlots(): Record<EvidenceLayer, VerdictDto | null> {
  return {
    protocol_conformance: null,
    live_route_compatibility: null,
    task_effectiveness: null,
  };
}

function newerVerdict(held: VerdictDto | null, candidate: VerdictDto): VerdictDto {
  if (held === null) return candidate;
  return candidate.asOf >= held.asOf ? candidate : held;
}

/** Collision-safe React key for a Subject+Suite row. */
export function matrixRowKey(subjectId: string, suiteId: string): string {
  return JSON.stringify([subjectId, suiteId]);
}

export function buildMatrixRows(verdicts: VerdictDto[], subjects: SubjectListItemDto[]): MatrixRow[] {
  const kinds = new Map<string, string>();
  for (const subject of subjects) kinds.set(subject.subjectId, subject.subjectKind);

  const groups = new Map<string, Map<string, Record<EvidenceLayer, VerdictDto | null>>>();
  for (const verdict of verdicts) {
    let suites = groups.get(verdict.subjectId);
    if (!suites) {
      suites = new Map();
      groups.set(verdict.subjectId, suites);
    }
    let slots = suites.get(verdict.suiteId);
    if (!slots) {
      slots = emptyLayerSlots();
      suites.set(verdict.suiteId, slots);
    }
    slots[verdict.evidenceLayer] = newerVerdict(slots[verdict.evidenceLayer], verdict);
  }

  const rows: MatrixRow[] = [];
  for (const subjectId of sortIds(groups.keys())) {
    const suites = groups.get(subjectId);
    if (!suites) continue;
    const suiteIds = sortIds(suites.keys());
    for (const [index, suiteId] of suiteIds.entries()) {
      const slots = suites.get(suiteId);
      if (!slots) continue;
      rows.push({
        subjectId,
        subjectKind: kinds.get(subjectId) ?? "",
        suiteId,
        showSubject: index === 0,
        byLayer: slots,
      });
    }
  }
  return rows;
}

/** Prefer derived compatibility, then protocol, then any reserved layer. */
export function preferredMatrixVerdict(row: MatrixRow): VerdictDto | null {
  return row.byLayer.live_route_compatibility
    ?? row.byLayer.protocol_conformance
    ?? row.byLayer.task_effectiveness;
}

export type VerdictPredicate = (verdict: VerdictDto) => boolean;

/**
 * Filter dimensions the loaded verdicts must satisfy. `subjectQuery` is a
 * client-side substring match; the other three mirror the exact API filters.
 */
function verdictPredicates(filters: VerdictFilters): VerdictPredicate[] {
  const predicates: VerdictPredicate[] = [];
  if (filters.layer !== "") predicates.push(verdict => verdict.evidenceLayer === filters.layer);
  if (filters.verdict !== "") predicates.push(verdict => verdict.verdict === filters.verdict);
  const suiteId = filters.suiteId;
  if (suiteId !== "") predicates.push(verdict => verdict.suiteId === suiteId);
  const needle = filters.subjectQuery.trim().toLowerCase();
  if (needle !== "") predicates.push(verdict => verdict.subjectId.toLowerCase().includes(needle));
  return predicates;
}

export function filterVerdicts(verdicts: VerdictDto[], filters: VerdictFilters): VerdictDto[] {
  const predicates = verdictPredicates(filters);
  return verdicts.filter(verdict => predicates.every(predicate => predicate(verdict)));
}

export function suiteIdsFromVerdicts(verdicts: VerdictDto[]): string[] {
  const ids = new Set<string>();
  for (const verdict of verdicts) {
    const trimmed = verdict.suiteId.trim();
    if (trimmed.length > 0) ids.add(trimmed);
  }
  return sortIds(ids);
}

export function verdictQueryFromFilters(filters: VerdictFilters): VerdictQueryFilters {
  // `subjectQuery` stays client-side: a partial string is never an exact subjectId.
  const query: VerdictQueryFilters = {};
  if (filters.layer !== "") query.layer = filters.layer;
  if (filters.verdict !== "") query.verdict = filters.verdict;
  const suiteId = filters.suiteId.trim();
  if (suiteId !== "") query.suiteId = suiteId;
  return query;
}

/** Quiet placeholder for a timestamp the projection did not supply. */
const NO_TIMESTAMP = "-";

/** Subject id shown in full up to this length; longer ids are compacted. */
const SHORT_ID_LIMIT = 16;
const SHORT_ID_HEAD = 8;
const SHORT_ID_TAIL = 6;

export function shortSubjectId(subjectId: string): string {
  if (subjectId.length <= SHORT_ID_LIMIT) return subjectId;
  return `${subjectId.slice(0, SHORT_ID_HEAD)}.${subjectId.slice(-SHORT_ID_TAIL)}`;
}

export function formatAsOf(ms: number, locale: string): string {
  return Number.isFinite(ms) && ms > 0 ? new Date(ms).toLocaleString(locale) : NO_TIMESTAMP;
}

/** Quiet Built metadata: locale-aware date and time without seconds. */
export function formatBuiltAt(ms: number, locale: string): string {
  if (!Number.isFinite(ms) || ms <= 0) return NO_TIMESTAMP;
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    year: "numeric",
    hour: "numeric",
    minute: "2-digit",
  }).format(new Date(ms));
}
