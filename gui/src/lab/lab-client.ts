/**
 * Compatibility Matrix read client for the Go listener (`internal/server`).
 *
 * Every read is a `LabRead` value - a route plus the reader for its body - and
 * one driver executes them. Requests go through the shared management fetch
 * owner (`readJsonOrThrow`); this module knows Lab routes and record shapes.
 */

import { readJsonOrThrow } from "../fetch-json.ts";
import {
  parseCommunityEvidenceContext,
  parseLabEvent,
  parseLabStatus,
  parseObservationDto,
  parsePassiveProductionSummary,
  parseSubjectDetail,
  parseSubjectRow,
  parseVerdictDto,
  type ArtifactMetadataDto,
  type CommunityEvidenceContextDto,
  type LabEventDto,
  type LabStatusDto,
  type ObservationDto,
  type PassiveProductionSummaryDto,
  type SubjectDetailDto,
  type SubjectListItemDto,
  type VerdictDto,
} from "./lab-records.ts";
import {
  LabDataContractError,
  LAB_PAGE_SIZE,
  readLabPage,
  walkPages,
  type CollectedRows,
  type LabCollection,
  type LabPage,
} from "./lab-pages.ts";
import type { VerdictQueryFilters } from "./evidence-matrix.ts";

/** Concurrent referenced-event fetches for one verdict detail. */
const EVENT_FETCH_WIDTH = 6;

/** Upper bound on referenced events resolved for one verdict detail. */
const EVENT_REFERENCE_LIMIT = 200;

const LAB_ROUTES = {
  status: "/api/lab/status",
  verdicts: "/api/lab/verdicts",
  subjects: "/api/lab/subjects",
  subjectBase: "/api/lab/subjects/",
  observations: "/api/lab/observations",
  eventBase: "/api/lab/events/",
  productionSignals: "/api/lab/production-signals",
  community: "/api/lab/public/community",
} as const;

type LabReader<TResult> = (raw: unknown) => TResult;

/** Server filters and cursor for one request that carries a query string. */
type LabQuery = {
  filters: Record<string, string | undefined>;
  cursor?: string;
};

/** One Lab read: where to go, how to read the body, and whether it is paged. */
export type LabRead<TResult> = {
  route: string;
  parse: LabReader<TResult>;
  query?: LabQuery;
};

/** Wrap a permissive record reader so a missing record is a contract error. */
function required<TResult>(parse: (raw: unknown) => TResult | null): LabReader<TResult> {
  return raw => {
    const value = parse(raw);
    if (value === null) throw new LabDataContractError("Lab record did not match its shape");
    return value;
  };
}

const READERS = {
  status: required(parseLabStatus),
  subject: required(parseSubjectDetail),
  event: required(parseLabEvent),
  production: required(parsePassiveProductionSummary),
  community: required(parseCommunityEvidenceContext),
} as const;

/** The paginated Lab collections the matrix reads, keyed by their array name. */
const COLLECTIONS = {
  verdicts: { key: "verdicts", readRow: parseVerdictDto },
  subjects: { key: "subjects", readRow: parseSubjectRow },
  observations: { key: "observations", readRow: parseObservationDto },
} as const satisfies Record<string, LabCollection<unknown>>;

/** The static Lab reads, keyed by what they return. */
const LAB_READS = {
  status: { route: LAB_ROUTES.status, parse: READERS.status },
  verdicts: { route: LAB_ROUTES.verdicts, parse: raw => readLabPage(raw, COLLECTIONS.verdicts) },
  subjects: { route: LAB_ROUTES.subjects, parse: raw => readLabPage(raw, COLLECTIONS.subjects) },
  observations: { route: LAB_ROUTES.observations, parse: raw => readLabPage(raw, COLLECTIONS.observations) },
  productionSignals: { route: LAB_ROUTES.productionSignals, parse: READERS.production },
  community: { route: LAB_ROUTES.community, parse: READERS.community },
} as const satisfies Record<string, LabRead<unknown>>;

function byId<TResult>(routeBase: string, id: string, parse: LabReader<TResult>): LabRead<TResult> {
  return { route: `${routeBase}${encodeURIComponent(id)}`, parse };
}

/** Turn a static read into a query-carrying read. */
function withQuery<TResult>(
  read: LabRead<TResult>,
  filters: Record<string, string | undefined>,
  cursor?: string,
): LabRead<TResult> {
  return cursor === undefined ? { ...read, query: { filters } } : { ...read, query: { filters, cursor } };
}

function readUrl(read: LabRead<unknown>): string {
  const request = read.query;
  if (request === undefined) return read.route;
  const pairs: Array<[string, string]> = [["limit", String(LAB_PAGE_SIZE)]];
  for (const [key, value] of Object.entries(request.filters)) {
    if (value) pairs.push([key, value]);
  }
  if (request.cursor) pairs.push(["cursor", request.cursor]);
  return `${read.route}?${new URLSearchParams(pairs).toString()}`;
}

/** Execute one Lab read. */
async function runLabRead<TResult>(
  apiBase: string,
  read: LabRead<TResult>,
  signal: AbortSignal,
): Promise<TResult> {
  const response = await fetch(apiBase + readUrl(read), { signal });
  const body = await readJsonOrThrow<unknown>(response);
  return read.parse(body);
}

function verdictsPage(filters: VerdictQueryFilters, cursor?: string): LabRead<LabPage<VerdictDto>> {
  return withQuery(LAB_READS.verdicts, {
    layer: filters.layer,
    verdict: filters.verdict,
    subjectId: filters.subjectId,
    suiteId: filters.suiteId,
  }, cursor);
}

function subjectsPage(cursor?: string): LabRead<LabPage<SubjectListItemDto>> {
  return withQuery(LAB_READS.subjects, {}, cursor);
}

function observationsPage(
  filters: { subjectId: string; layer?: string; suiteId?: string },
  cursor?: string,
): LabRead<LabPage<ObservationDto>> {
  return withQuery(LAB_READS.observations, {
    subjectId: filters.subjectId,
    layer: filters.layer,
    suiteId: filters.suiteId,
  }, cursor);
}

function subjectDetail(subjectId: string): LabRead<SubjectDetailDto> {
  return byId(LAB_ROUTES.subjectBase, subjectId, READERS.subject);
}

function eventDetail(eventId: string): LabRead<LabEventDto> {
  return byId(LAB_ROUTES.eventBase, eventId, READERS.event);
}

function productionSignals(subjectId: string): LabRead<PassiveProductionSummaryDto> {
  return withQuery(LAB_READS.productionSignals, { subjectId });
}

/** Collect every page of one Lab list, bounded and truncation-aware. */
function collectPages<TResult>(
  apiBase: string,
  signal: AbortSignal,
  pageAt: (cursor: string | undefined) => LabRead<LabPage<TResult>>,
): Promise<CollectedRows<TResult>> {
  return walkPages(cursor => runLabRead(apiBase, pageAt(cursor), signal));
}

export type LabPageData = {
  status: LabStatusDto;
  verdicts: VerdictDto[];
  subjects: SubjectListItemDto[];
  subjectsTruncated: boolean;
  hasMore: boolean;
  nextCursor?: string;
  community: CommunityEvidenceContextDto | null;
};

/** Only a real abort may escape a fan-out or best-effort read. */
function rethrowIfAborted(signal: AbortSignal, error: unknown): void {
  if (signal.aborted) throw error;
}

/** Stop a worker before it starts another read. */
function stopIfAborted(signal: AbortSignal): void {
  if (signal.aborted) throw new DOMException("Aborted", "AbortError");
}

/** Best-effort optional read: an unreadable optional never fails the page. */
function bestEffort<TResult>(read: Promise<TResult>, signal: AbortSignal): Promise<TResult | null> {
  return read.catch(error => {
    rethrowIfAborted(signal, error);
    return null;
  });
}

export async function fetchLabPageData(
  apiBase: string,
  filters: VerdictQueryFilters,
  signal: AbortSignal,
): Promise<LabPageData> {
  const [status, community] = await Promise.all([
    runLabRead(apiBase, LAB_READS.status, signal),
    bestEffort(runLabRead(apiBase, LAB_READS.community, signal), signal),
  ]);
  if (!status.projectionAvailable) {
    return { status, verdicts: [], subjects: [], subjectsTruncated: false, hasMore: false, community };
  }
  const [verdicts, subjects] = await Promise.all([
    runLabRead(apiBase, verdictsPage(filters), signal),
    collectPages(apiBase, signal, subjectsPage),
  ]);
  return {
    status,
    verdicts: verdicts.rows,
    subjects: subjects.rows,
    subjectsTruncated: subjects.truncated,
    hasMore: verdicts.hasMore,
    nextCursor: verdicts.hasMore ? verdicts.nextCursor : undefined,
    community,
  };
}

export function fetchMoreVerdicts(
  apiBase: string,
  filters: VerdictQueryFilters,
  cursor: string,
  signal: AbortSignal,
): Promise<LabPage<VerdictDto>> {
  return runLabRead(apiBase, verdictsPage(filters, cursor), signal);
}

export type VerdictDetailData = {
  subject: SubjectDetailDto;
  observations: ObservationDto[];
  observationsTruncated: boolean;
  events: LabEventDto[];
  /** Artifacts stay schema/API-capable but are omitted from the product UI. */
  artifacts: ArtifactMetadataDto[];
  production: PassiveProductionSummaryDto | null;
};

/**
 * Resolve the distinct referenced events with a bounded worker pool. One
 * unreadable reference does not sink the detail read; an abort still does.
 */
async function collectReferencedEvents(
  references: readonly string[],
  signal: AbortSignal,
  load: (eventId: string) => Promise<LabEventDto>,
): Promise<LabEventDto[]> {
  const pending = references.slice(0, EVENT_REFERENCE_LIMIT);
  const settled: LabEventDto[] = [];
  const cursor = { next: 0 };
  async function lane(): Promise<void> {
    while (cursor.next < pending.length) {
      stopIfAborted(signal);
      const eventId = pending[cursor.next];
      cursor.next += 1;
      try {
        settled.push(await load(eventId));
      } catch (error) {
        rethrowIfAborted(signal, error);
      }
    }
  }
  const lanes = Array.from({ length: Math.min(EVENT_FETCH_WIDTH, pending.length) }, () => lane());
  await Promise.all(lanes);
  return settled;
}

export async function fetchVerdictDetail(
  apiBase: string,
  verdict: VerdictDto,
  signal: AbortSignal,
): Promise<VerdictDetailData> {
  const references = [...new Set(verdict.contributingEventIds.concat(verdict.contradictingEventIds))];
  const observations = {
    subjectId: verdict.subjectId,
    layer: verdict.evidenceLayer,
    suiteId: verdict.suiteId,
  };
  const [subject, observationRows, events, production] = await Promise.all([
    runLabRead(apiBase, subjectDetail(verdict.subjectId), signal),
    collectPages(apiBase, signal, cursor => observationsPage(observations, cursor)),
    collectReferencedEvents(references, signal, eventId => runLabRead(apiBase, eventDetail(eventId), signal)),
    bestEffort(runLabRead(apiBase, productionSignals(verdict.subjectId), signal), signal),
  ]);
  return {
    subject,
    observations: observationRows.rows,
    observationsTruncated: observationRows.truncated,
    events,
    artifacts: [],
    production,
  };
}
