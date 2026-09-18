/**
 * Compatibility Matrix data contract.
 *
 * Pins the Lab read contract: strict page parsing, the pagination state
 * machine, filter shaping, and matrix aggregation. These are behaviour
 * contracts, exercised through the public modules.
 */

import assert from "node:assert/strict";
import test from "node:test";

import {
  parseArtifactMetadata,
  parseCommunityEvidenceContext,
  parseLabEvent,
  parseLabStatus,
  parseObservationDto,
  parseSubjectRow,
  parseVerdictDto,
} from "../src/lab/lab-records.ts";
import {
  LabDataContractError,
  readLabPage,
  walkPages,
  type LabCollection,
} from "../src/lab/lab-pages.ts";
import {
  fetchLabPageData,
  fetchMoreVerdicts,
  fetchVerdictDetail,
} from "../src/lab/lab-client.ts";
import {
  buildMatrixRows,
  filterVerdicts,
  matrixRowKey,
  preferredMatrixVerdict,
  verdictQueryFromFilters,
  type VerdictDto,
} from "../src/lab/evidence-matrix.ts";

const API = "http://127.0.0.1:23100";

const VERDICT_COLLECTION: LabCollection<VerdictDto> = { key: "verdicts", readRow: parseVerdictDto };
const SUBJECT_COLLECTION: LabCollection<{ subjectId: string; subjectKind: string }> = {
  key: "subjects",
  readRow: parseSubjectRow,
};
const OBSERVATION_COLLECTION: LabCollection<Record<string, unknown>> = {
  key: "observations",
  readRow: parseObservationDto,
};

function readVerdictPage(raw: unknown) {
  return readLabPage(raw, VERDICT_COLLECTION);
}

function readSubjectPage(raw: unknown) {
  return readLabPage(raw, SUBJECT_COLLECTION);
}

function readObservationPage(raw: unknown) {
  return readLabPage(raw, OBSERVATION_COLLECTION);
}

function verdict(overrides: Partial<VerdictDto> = {}): VerdictDto {
  return {
    projectionKey: "v1",
    subjectId: "subject-1",
    evidenceLayer: "protocol_conformance",
    suiteId: "suite-a",
    suiteVersion: "1",
    suiteManifestDigest: "digest",
    projectionSpecVersion: "1",
    verdict: "VERIFIED",
    asOf: 1_000,
    scenarioManifestDigests: [],
    claimSourceDigest: null,
    contributingEventIds: [],
    contradictingEventIds: [],
    notes: [],
    ...overrides,
  };
}

function observation(overrides: Record<string, unknown> = {}) {
  return {
    eventId: "e1",
    subjectId: "subject-1",
    evidenceLayer: "protocol_conformance",
    suiteId: "suite-a",
    suiteVersion: "1",
    suiteManifestDigest: "digest",
    scenarioId: "scenario",
    scenarioVersion: "1",
    scenarioManifestDigest: "digest",
    outcome: "pass",
    completedAt: 5,
    executionMode: "local_classifier",
    excluded: false,
    exclusionReason: null,
    ...overrides,
  };
}

// ---------------------------------------------------------------------------
// Record validity
// ---------------------------------------------------------------------------

test("status accepts the minimal projection envelope", () => {
  assert.deepEqual(parseLabStatus({ projectionAvailable: false }), { projectionAvailable: false });
  assert.equal(parseLabStatus({ projectionAvailable: true, verdictCount: 4 })?.verdictCount, 4);
});

test("status rejects a non-boolean availability and non-finite counters", () => {
  assert.equal(parseLabStatus({ projectionAvailable: "yes" }), null);
  assert.equal(parseLabStatus({}), null);
  assert.equal(parseLabStatus([]), null);
  assert.equal(parseLabStatus({ projectionAvailable: true, verdictCount: Number.NaN }), null);
  assert.equal(parseLabStatus({ projectionAvailable: true, builtAtMs: Number.POSITIVE_INFINITY }), null);
  assert.equal(parseLabStatus({ projectionAvailable: true, corruptionCount: "0" }), null);
  assert.equal(parseLabStatus({ projectionAvailable: true, projectionSpecVersion: 7 }), null);
});

test("verdict records require identity, suite metadata and evidence lists", () => {
  assert.ok(parseVerdictDto(verdict()));
  assert.equal(parseVerdictDto(verdict({ projectionKey: "" })), null);
  assert.equal(parseVerdictDto(verdict({ evidenceLayer: "made_up" as never })), null);
  assert.equal(parseVerdictDto(verdict({ verdict: "MAYBE" as never })), null);
  assert.equal(parseVerdictDto(verdict({ asOf: Number.NaN })), null);
  assert.equal(parseVerdictDto(verdict({ contributingEventIds: [1] as never })), null);
  assert.equal(parseVerdictDto(verdict({ claimSourceDigest: undefined as never })), null);
  assert.equal(parseVerdictDto(verdict({ notes: "none" as never })), null);
});

test("observation records require scenario identity and a finite completion time", () => {
  assert.ok(parseObservationDto(observation()));
  assert.equal(parseObservationDto(observation({ completedAt: "5" })), null);
  assert.equal(parseObservationDto(observation({ completedAt: Number.NaN })), null);
  assert.equal(parseObservationDto(observation({ excluded: "false" })), null);
  assert.equal(parseObservationDto(observation({ exclusionReason: undefined })), null);
  assert.equal(parseObservationDto(observation({ executionMode: 3 })), null);
  assert.equal(parseObservationDto(observation({ evidenceLayer: "task_effectiveness" }))?.evidenceLayer === "task_effectiveness", true);
});

test("event detail strips unstructured payload material", () => {
  const parsed = parseLabEvent({
    event: {
      eventKind: "observation",
      eventId: "e1",
      recordedAt: 5,
      producer: "benes",
      producerVersion: "1",
      excluded: false,
      exclusionReason: null,
      subjectId: "subject-1",
      payload_json: "{\"secret\":\"raw scenario material\"}",
      note: "kept",
    },
  });
  assert.ok(parsed);
  assert.equal("payload_json" in parsed, false);
  assert.equal(JSON.stringify(parsed).includes("raw scenario material"), false);
  assert.equal(JSON.stringify(parsed).includes("kept"), true);
  assert.equal(parsed.subjectId, "subject-1");
  assert.equal(parseLabEvent({ event: { eventId: "e1" } }), null);
  assert.equal(parseLabEvent({}), null);
});

test("artifact metadata validates the closed status vocabulary", () => {
  const artifact = {
    digest: "abc",
    artifactClass: null,
    mediaType: "application/json",
    byteCount: 12,
    status: "present",
    lastError: null,
  };
  assert.deepEqual(parseArtifactMetadata({ artifact }), artifact);
  assert.equal(parseArtifactMetadata({ artifact: { ...artifact, status: "gone" } }), null);
  assert.equal(parseArtifactMetadata({ artifact: { ...artifact, byteCount: Number.NaN } }), null);
  assert.equal(parseArtifactMetadata({ artifact: { ...artifact, byteCount: null } })?.byteCount, null);
  assert.equal(parseArtifactMetadata({ artifact: { ...artifact, lastError: 4 } }), null);
});

// ---------------------------------------------------------------------------
// Community evidence
// ---------------------------------------------------------------------------

const BUNDLE_DIGEST = "a".repeat(64);
const PUBLISHER_KEY_DIGEST = "b".repeat(64);

function communityRow(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    trustClass: "community_untrusted_v1",
    status: "cryptographically_valid",
    bundleId: BUNDLE_DIGEST,
    publisherKeyId: PUBLISHER_KEY_DIGEST,
    activeRecordCount: 2,
    revokedRecordCount: 0,
    ...overrides,
  };
}

function communityContext(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    evidence: [communityRow()],
    trustClass: "community_untrusted_v1",
    locallyVerified: false,
    ...overrides,
  };
}

test("community evidence stays untrusted context, never local verification", () => {
  const context = communityContext();
  assert.deepEqual(parseCommunityEvidenceContext(context), context);
  const multiple = communityContext({ evidence: [communityRow(), communityRow({ bundleId: PUBLISHER_KEY_DIGEST })] });
  assert.equal(parseCommunityEvidenceContext(multiple)?.evidence.length, 2);
  const parsed = parseCommunityEvidenceContext(multiple);
  assert.equal(parsed?.trustClass, "community_untrusted_v1");
  assert.equal(parsed?.locallyVerified, false);
  assert.equal(parsed?.evidence[0]?.status, "cryptographically_valid");
});

test("a malformed community row invalidates the whole envelope", () => {
  const good = communityRow();
  const malformed = [
    { ...good, trustClass: "community_trusted_v1" },
    { ...good, trustClass: undefined },
    { ...good, status: "cryptographically_invalid" },
    { ...good, status: undefined },
    { ...good, bundleId: "A".repeat(64) },
    { ...good, bundleId: "a".repeat(63) },
    { ...good, bundleId: "z".repeat(64) },
    { ...good, bundleId: "not-a-digest" },
    { ...good, publisherKeyId: "b".repeat(65) },
    { ...good, publisherKeyId: "b".repeat(64).toUpperCase() },
    { ...good, activeRecordCount: -1 },
    { ...good, revokedRecordCount: 1.5 },
    { ...good, activeRecordCount: Number.MAX_SAFE_INTEGER + 2 },
    { ...good, revokedRecordCount: "0" },
    { ...good, activeRecordCount: Number.NaN },
    { ...good, extra: 1 },
    null,
    [],
    "row",
  ];
  for (const row of malformed) {
    assert.equal(
      parseCommunityEvidenceContext(communityContext({ evidence: [communityRow(), row] })),
      null,
      `expected the envelope to fail for ${JSON.stringify(row)}`,
    );
  }
  assert.deepEqual(parseCommunityEvidenceContext(communityContext({ evidence: [] })), communityContext({ evidence: [] }));
});

test("the community envelope is closed and bounded", () => {
  assert.equal(parseCommunityEvidenceContext(null), null);
  assert.equal(parseCommunityEvidenceContext([]), null);
  assert.equal(parseCommunityEvidenceContext("context"), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ extra: true })), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ trustClass: "community_trusted_v1" })), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ locallyVerified: true })), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ locallyVerified: undefined })), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ evidence: {} })), null);
  assert.equal(parseCommunityEvidenceContext(communityContext({ evidence: [null] })), null);

  const cap = Array.from({ length: 4096 }, () => communityRow());
  assert.equal(parseCommunityEvidenceContext(communityContext({ evidence: cap }))?.evidence.length, 4096);
  const overCap = [...cap, communityRow()];
  assert.equal(parseCommunityEvidenceContext(communityContext({ evidence: overCap })), null);
});

// ---------------------------------------------------------------------------
// Pagination contract
// ---------------------------------------------------------------------------

test("a terminal page needs no cursor and reports no more pages", () => {
  const page = readVerdictPage({ verdicts: [verdict()], hasMore: false });
  assert.equal(page.rows.length, 1);
  assert.equal(page.hasMore, false);
  assert.equal(page.nextCursor, undefined);
  assert.equal(readVerdictPage({ verdicts: [] }).hasMore, false);
});

test("a continuing page carries a usable cursor", () => {
  const page = readVerdictPage({ verdicts: [verdict()], hasMore: true, nextCursor: "50" });
  assert.equal(page.hasMore, true);
  assert.equal(page.nextCursor, "50");
});

test("malformed pagination metadata is rejected, never coerced", () => {
  assert.throws(() => readVerdictPage({ verdicts: [], hasMore: "yes" }), LabDataContractError);
  assert.throws(() => readVerdictPage({ verdicts: [], nextCursor: 50 }), LabDataContractError);
  assert.throws(() => readVerdictPage({ verdicts: [], hasMore: true }), LabDataContractError);
  assert.throws(() => readVerdictPage({ verdicts: [], hasMore: true, nextCursor: "" }), LabDataContractError);
  assert.throws(() => readVerdictPage({ verdicts: {} }), LabDataContractError);
  assert.throws(() => readVerdictPage(null), LabDataContractError);
});

test("a contract failure carries no user-facing message", () => {
  const error = new LabDataContractError("pagination cursor did not advance");
  assert.equal(error.message, "");
  assert.equal(error.reason, "pagination cursor did not advance");
  assert.equal(error.name, "LabDataContractError");
  assert.equal(error instanceof Error, true);
});

test("one unreadable row invalidates the whole page", () => {
  assert.throws(
    () => readVerdictPage({ verdicts: [verdict(), { projectionKey: "only" }], hasMore: false }),
    LabDataContractError,
  );
  assert.throws(() => readSubjectPage({ subjects: [{ subjectId: "s" }] }), LabDataContractError);
  assert.throws(
    () => readObservationPage({ observations: [observation({ completedAt: "later" })] }),
    LabDataContractError,
  );
  assert.equal(readSubjectPage({ subjects: [{ subjectId: "s", subjectKind: "model" }] }).rows.length, 1);
});

test("a cursor that does not advance fails instead of looping", async () => {
  await assert.rejects(
    () => walkPages(async () => ({ rows: [], hasMore: true, nextCursor: "same" })),
    LabDataContractError,
  );
  await assert.rejects(
    () => walkPages(async cursor => ({ rows: [], hasMore: true, nextCursor: cursor ?? "first" })),
    LabDataContractError,
  );
});

test("page collection is bounded and reports truncation", async () => {
  let hops = 0;
  const collected = await walkPages(async () => {
    hops += 1;
    return { rows: [hops], hasMore: true, nextCursor: String(hops) };
  });
  assert.equal(collected.truncated, true);
  assert.equal(collected.rows.length, 200);
  assert.equal(hops, 200);
});

test("a complete walk reports every page and no truncation", async () => {
  const collected = await walkPages(async cursor =>
    cursor === undefined
      ? { rows: ["a"], hasMore: true, nextCursor: "1" }
      : { rows: ["b"], hasMore: false });
  assert.deepEqual(collected, { rows: ["a", "b"], truncated: false });
});

// ---------------------------------------------------------------------------
// Query shaping
// ---------------------------------------------------------------------------

type Reply = { status?: number; body?: unknown };

function communityBody() {
  return { evidence: [], trustClass: "community_untrusted_v1", locallyVerified: false };
}

function labReply(url: string, verdicts: unknown[] = [], subjects: unknown[] = []): Reply {
  if (url.includes("/api/lab/status")) return { body: { projectionAvailable: true, verdictCount: verdicts.length } };
  if (url.includes("/api/lab/verdicts")) return { body: { verdicts, hasMore: false } };
  if (url.includes("/api/lab/subjects")) return { body: { subjects, hasMore: false } };
  if (url.includes("/api/lab/public/community")) return { body: communityBody() };
  return { status: 404, body: { error: "not found" } };
}

type FetchState = { current: number; peak: number; calls: string[] };

async function withLabFetch(
  responder: (url: string, state: FetchState) => Reply | Promise<Reply>,
  run: (state: FetchState) => Promise<void>,
): Promise<void> {
  const original = globalThis.fetch;
  const state: FetchState = { current: 0, peak: 0, calls: [] };
  globalThis.fetch = (async (input: unknown, init?: { signal?: AbortSignal }) => {
    if (init?.signal?.aborted) throw new DOMException("Aborted", "AbortError");
    const url = String(input);
    state.calls.push(url);
    state.current += 1;
    state.peak = Math.max(state.peak, state.current);
    try {
      const reply = await responder(url, state);
      const status = reply.status ?? 200;
      return {
        ok: status >= 200 && status < 300,
        status,
        text: async () => JSON.stringify(reply.body ?? {}),
      };
    } finally {
      state.current -= 1;
    }
  }) as unknown as typeof fetch;
  try {
    await run(state);
  } finally {
    globalThis.fetch = original;
  }
}

async function withFetch(
  handler: (url: string) => Reply | Promise<Reply>,
  run: () => Promise<void>,
): Promise<void> {
  await withLabFetch(handler, () => run());
}

test("Lab reads send the fixed page size and only populated filters", async () => {
  const seen: string[] = [];
  await withFetch(url => {
    seen.push(url);
    return labReply(url);
  }, async () => {
    const signal = new AbortController().signal;
    await fetchLabPageData(API, {}, signal);
    await fetchMoreVerdicts(API, { layer: "live_route_compatibility", verdict: "DEGRADED", suiteId: "codex" }, "50", signal);
  });
  const plain = seen.find(url => url.includes("/api/lab/verdicts") && !url.includes("layer="));
  assert.equal(plain, `${API}/api/lab/verdicts?limit=50`);
  assert.equal(seen.find(url => url.includes("/api/lab/subjects")), `${API}/api/lab/subjects?limit=50`);
  const filtered = seen.find(url => url.includes("layer="));
  assert.ok(filtered?.includes("limit=50"));
  assert.ok(filtered?.includes("layer=live_route_compatibility"));
  assert.ok(filtered?.includes("verdict=DEGRADED"));
  assert.ok(filtered?.includes("suiteId=codex"));
  assert.ok(filtered?.includes("cursor=50"));
  assert.equal(filtered?.includes("subjectId"), false);
});

test("free-text subject search stays client-side", () => {
  assert.deepEqual(verdictQueryFromFilters({ layer: "", verdict: "", subjectQuery: "gpt", suiteId: "" }), {});
  assert.deepEqual(
    verdictQueryFromFilters({ layer: "protocol_conformance", verdict: "VERIFIED", subjectQuery: "gpt", suiteId: " codex " }),
    { layer: "protocol_conformance", verdict: "VERIFIED", suiteId: "codex" },
  );
  const rows = [verdict({ subjectId: "openai/gpt-5" }), verdict({ projectionKey: "v2", subjectId: "anthropic/claude" })];
  assert.deepEqual(
    filterVerdicts(rows, { layer: "", verdict: "", subjectQuery: "GPT-5", suiteId: "" }).map(row => row.subjectId),
    ["openai/gpt-5"],
  );
  assert.deepEqual(
    filterVerdicts(rows, { layer: "live_route_compatibility", verdict: "", subjectQuery: "", suiteId: "" }),
    [],
  );
  assert.equal(filterVerdicts(rows, { layer: "", verdict: "", subjectQuery: "", suiteId: "suite-a" }).length, 2);
});

test("an unavailable projection returns an honest empty matrix", async () => {
  const seen: string[] = [];
  await withFetch(url => {
    seen.push(url);
    if (url.includes("/api/lab/status")) return { body: { projectionAvailable: false } };
    if (url.includes("/api/lab/public/community")) return { body: communityBody() };
    return { body: {} };
  }, async () => {
    const data = await fetchLabPageData(API, {}, new AbortController().signal);
    assert.equal(data.status.projectionAvailable, false);
    assert.deepEqual(data.verdicts, []);
    assert.deepEqual(data.subjects, []);
    assert.equal(data.hasMore, false);
    assert.equal(data.community?.trustClass, "community_untrusted_v1");
  });
  assert.equal(seen.some(url => url.includes("/api/lab/verdicts")), false);
});

test("community context is best-effort but an abort still propagates", async () => {
  const failingCommunity = (url: string) => (url.includes("/api/lab/public/community") ? { status: 503, body: {} } : labReply(url));
  await withFetch(failingCommunity, async () => {
    const data = await fetchLabPageData(API, {}, new AbortController().signal);
    assert.equal(data.community, null);
    assert.equal(data.status.projectionAvailable, true);
  });

  const controller = new AbortController();
  await withFetch(url => {
    if (url.includes("/api/lab/public/community")) {
      controller.abort();
      return { status: 503, body: {} };
    }
    return labReply(url);
  }, async () => {
    await assert.rejects(() => fetchLabPageData(API, {}, controller.signal));
  });
});


// ---------------------------------------------------------------------------
// Matrix aggregation
// ---------------------------------------------------------------------------

test("one row per Subject+Suite, subject shown only on the first suite row", () => {
  const rows = buildMatrixRows([
    verdict({ projectionKey: "a", subjectId: "s1", suiteId: "suite-a" }),
    verdict({ projectionKey: "b", subjectId: "s1", evidenceLayer: "live_route_compatibility", suiteId: "suite-a", verdict: "DEGRADED", asOf: 2 }),
    verdict({ projectionKey: "c", subjectId: "s1", evidenceLayer: "task_effectiveness", suiteId: "suite-b", verdict: "CLAIMED" }),
    verdict({ projectionKey: "d", subjectId: "s2", suiteId: "suite-a" }),
  ], [{ subjectId: "s1", subjectKind: "model" }]);
  assert.deepEqual(rows.map(row => [row.subjectId, row.suiteId, row.showSubject]), [
    ["s1", "suite-a", true],
    ["s1", "suite-b", false],
    ["s2", "suite-a", true],
  ]);
  assert.equal(rows[0]?.byLayer.protocol_conformance?.projectionKey, "a");
  assert.equal(rows[0]?.byLayer.live_route_compatibility?.projectionKey, "b");
  assert.equal(rows[1]?.byLayer.task_effectiveness?.projectionKey, "c");
  assert.equal(rows[0]?.subjectKind, "model");
  assert.equal(rows[2]?.subjectKind, "");
});

test("a competing verdict for the same layer keeps the newest asOf", () => {
  const older = verdict({ projectionKey: "old", asOf: 10 });
  const newer = verdict({ projectionKey: "new", asOf: 20 });
  const kept = buildMatrixRows([newer, older], []);
  assert.equal(kept[0]?.byLayer.protocol_conformance?.projectionKey, "new");
  const tied = buildMatrixRows([older, verdict({ projectionKey: "tied", asOf: 10 })], []);
  assert.equal(tied[0]?.byLayer.protocol_conformance?.projectionKey, "tied");
});

test("row identity survives ids that contain delimiter characters", () => {
  assert.notEqual(matrixRowKey("a:b", "c"), matrixRowKey("a", "b:c"));
  assert.notEqual(matrixRowKey("a\"b", "c"), matrixRowKey("a", "\"b\"c"));
  const rows = buildMatrixRows([
    verdict({ projectionKey: "1", subjectId: "a:b", suiteId: "c" }),
    verdict({ projectionKey: "2", subjectId: "a", suiteId: "b:c" }),
    verdict({ projectionKey: "3", subjectId: "größe/日本語", suiteId: "suite" }),
  ], []);
  assert.deepEqual(rows.map(row => matrixRowKey(row.subjectId, row.suiteId)).sort(), [
    matrixRowKey("a", "b:c"),
    matrixRowKey("a:b", "c"),
    matrixRowKey("größe/日本語", "suite"),
  ].sort());
  assert.equal(new Set(rows.map(row => matrixRowKey(row.subjectId, row.suiteId))).size, 3);
});

test("matrix ordering is deterministic", () => {
  const input = [
    verdict({ projectionKey: "1", subjectId: "b", suiteId: "z" }),
    verdict({ projectionKey: "2", subjectId: "b", suiteId: "a" }),
    verdict({ projectionKey: "3", subjectId: "a", suiteId: "z" }),
  ];
  const first = buildMatrixRows(input, []);
  assert.deepEqual(first.map(row => `${row.subjectId}/${row.suiteId}`), ["a/z", "b/a", "b/z"]);
  assert.deepEqual(
    first.map(row => `${row.subjectId}/${row.suiteId}`),
    buildMatrixRows([...input].reverse(), []).map(row => `${row.subjectId}/${row.suiteId}`),
  );
});

test("the preferred verdict prefers derived, then protocol, then any reserved layer", () => {
  const rows = buildMatrixRows([
    verdict({ projectionKey: "p", evidenceLayer: "protocol_conformance" }),
    verdict({ projectionKey: "l", evidenceLayer: "live_route_compatibility", asOf: 2 }),
    verdict({ projectionKey: "t", evidenceLayer: "task_effectiveness", asOf: 2 }),
  ], []);
  assert.equal(preferredMatrixVerdict(rows[0]!)?.projectionKey, "l");
  const protocolOnly = buildMatrixRows([verdict({ projectionKey: "p", evidenceLayer: "protocol_conformance" })], []);
  assert.equal(preferredMatrixVerdict(protocolOnly[0]!)?.projectionKey, "p");
  const reservedOnly = buildMatrixRows([verdict({ projectionKey: "t", evidenceLayer: "task_effectiveness" })], []);
  assert.equal(preferredMatrixVerdict(reservedOnly[0]!)?.projectionKey, "t");
  assert.equal(preferredMatrixVerdict({
    subjectId: "s",
    subjectKind: "",
    suiteId: "suite",
    showSubject: true,
    byLayer: { protocol_conformance: null, live_route_compatibility: null, task_effectiveness: null },
  }), null);
});


// ---------------------------------------------------------------------------
// Verdict detail
// ---------------------------------------------------------------------------

type DetailOptions = {
  missingEventIds?: readonly string[];
  production?: unknown;
};

function passiveProduction(subjectId: string) {
  return {
    verificationStatus: "not_verification",
    summary: {
      subjectId,
      verificationStatus: "not_verification",
      recentProductionAttempts: 0,
      recentSuccessfulAttempts: 0,
      recentRouteErrorSignals: 0,
    },
  };
}

function eventDetailBody(eventId: string) {
  return {
    event: {
      eventKind: "observation",
      eventId,
      recordedAt: 1,
      producer: "benes",
      producerVersion: "1",
      excluded: false,
      exclusionReason: null,
    },
  };
}

function eventIdFromUrl(url: string): string {
  return decodeURIComponent(url.slice(url.indexOf("/events/") + "/events/".length));
}

function detailReply(row: VerdictDto, options: DetailOptions = {}) {
  return async (url: string): Promise<Reply> => {
    if (url.includes("/api/lab/subjects/")) {
      return { body: { subject: { subjectId: row.subjectId, subjectKind: "model" } } };
    }
    if (url.includes("/api/lab/observations")) {
      return { body: { observations: [observation({ subjectId: row.subjectId })], hasMore: false } };
    }
    if (url.includes("/api/lab/events/")) {
      const eventId = eventIdFromUrl(url);
      if (options.missingEventIds?.includes(eventId)) return { status: 404, body: { error: "not found" } };
      return { body: eventDetailBody(eventId) };
    }
    if (url.includes("/api/lab/production-signals")) {
      return { body: options.production ?? passiveProduction(row.subjectId) };
    }
    return { status: 404, body: { error: "not found" } };
  };
}

test("detail loading deduplicates contributing and contradicting references", async () => {
  const row = verdict({ contributingEventIds: ["e1", "e2"], contradictingEventIds: ["e2", "e3"] });
  await withLabFetch(detailReply(row), async state => {
    const detail = await fetchVerdictDetail(API, row, new AbortController().signal);
    assert.equal(state.calls.filter(url => url.includes("/api/lab/events/")).length, 3);
    assert.deepEqual(detail.events.map(event => event.eventId).sort(), ["e1", "e2", "e3"]);
  });
});

test("one unreadable reference leaves the rest of the detail intact", async () => {
  const row = verdict({ contributingEventIds: ["e1", "gone", "e3"] });
  await withLabFetch(detailReply(row, { missingEventIds: ["gone"] }), async () => {
    const detail = await fetchVerdictDetail(API, row, new AbortController().signal);
    assert.deepEqual(detail.events.map(event => event.eventId).sort(), ["e1", "e3"]);
    assert.equal(detail.subject.subjectKind, "model");
    assert.equal(detail.observations.length, 1);
    assert.deepEqual(detail.artifacts, []);
    assert.equal(detail.production?.verificationStatus, "not_verification");
    assert.equal(detail.production?.summary.verificationStatus, "not_verification");
  });
});

test("referenced event resolution is bounded in count and concurrency", async () => {
  const many = Array.from({ length: 250 }, (_, index) => `e${index}`);
  const wide = verdict({ contributingEventIds: many });
  await withLabFetch(detailReply(wide), async state => {
    const detail = await fetchVerdictDetail(API, wide, new AbortController().signal);
    assert.equal(detail.events.length, 200);
    assert.equal(state.calls.filter(url => url.includes("/api/lab/events/")).length, 200);
  });

  const few = Array.from({ length: 20 }, (_, index) => `e${index}`);
  const narrow = verdict({ contributingEventIds: few });
  const base = detailReply(narrow);
  let eventsInFlight = 0;
  let eventPeak = 0;
  const tracked = async (url: string): Promise<Reply> => {
    if (url.includes("/api/lab/events/")) {
      eventsInFlight += 1;
      eventPeak = Math.max(eventPeak, eventsInFlight);
      await new Promise(resolve => setTimeout(resolve, 5));
      eventsInFlight -= 1;
      return { body: eventDetailBody(eventIdFromUrl(url)) };
    }
    return base(url);
  };
  await withLabFetch(tracked, async () => {
    const detail = await fetchVerdictDetail(API, narrow, new AbortController().signal);
    assert.equal(detail.events.length, 20);
  });
  assert.equal(eventPeak, 6);
  assert.equal(eventsInFlight, 0);
});

test("an aborted detail read surfaces the abort instead of hiding it", async () => {
  const row = verdict({ contributingEventIds: ["e1", "e2"] });
  const controller = new AbortController();
  controller.abort();
  await withLabFetch(detailReply(row), async () => {
    await assert.rejects(() => fetchVerdictDetail(API, row, controller.signal), /Aborted/);
  });
});

test("production signals are never promoted to verification evidence", async () => {
  const row = verdict({ contributingEventIds: [] });
  const promoted = {
    verificationStatus: "verified",
    summary: {
      subjectId: "s",
      verificationStatus: "verified",
      recentProductionAttempts: 1,
      recentSuccessfulAttempts: 1,
      recentRouteErrorSignals: 0,
    },
  };
  await withLabFetch(detailReply(row, { production: promoted }), async () => {
    const detail = await fetchVerdictDetail(API, row, new AbortController().signal);
    assert.equal(detail.production, null);
  });

  const malformed = {
    verificationStatus: "not_verification",
    summary: {
      subjectId: "s",
      verificationStatus: "not_verification",
      recentProductionAttempts: "0",
      recentSuccessfulAttempts: 0,
      recentRouteErrorSignals: 0,
    },
  };
  await withLabFetch(detailReply(row, { production: malformed }), async () => {
    const detail = await fetchVerdictDetail(API, row, new AbortController().signal);
    assert.equal(detail.production, null);
  });
});
