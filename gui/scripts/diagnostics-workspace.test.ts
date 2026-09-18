import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  DIAGNOSTICS_PAGE_SIZE,
  diagnosticsDetailPath,
  diagnosticsListPath,
  formatDiagnosticsCost,
  formatDurationMs,
  formatRecordedDurationMs,
  mergeDiagnosticsRows,
  parseDiagnosticsDetail,
  parseDiagnosticsList,
  parseDiagnosticsSummary,
  rowInTimeRange,
  serverListQueryFromFilters,
  snapshotHasOlder,
  toolbarFiltersActive,
} from "../src/pages/diagnostics-contract.ts";
import {
  debugFlagSource,
  enabledTextualStreams,
  hasRuntimeOverrides,
  isClaudeInboundApplicable,
} from "../src/pages/debug-shared.ts";
import { logsHashForSession, logsHashIsAllowed, logsListUrl, logsSessionIdFromHash } from "../src/pages/logs-session-filter.ts";
import { parseHashRoute } from "../src/hash-routing.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

function summary(overrides = {}) {
  return {
    requestId: "req_ab12",
    timestamp: "2026-09-04T12:00:00.000Z",
    method: "POST",
    path: "/v1/responses",
    status: 200,
    durationMs: 42,
    ...overrides,
  };
}

test("list URL uses the structured Diagnostics contract and never /api/logs", () => {
  const pathWithSession = diagnosticsListPath({
    sessionId: "ses_deadbeefdeadbeefdeadbeefdeadbeef",
    protocol: "responses",
    provider: "openai-apikey",
    model: "gpt-5.4",
    status: 502,
  });
  assert.match(pathWithSession, /^\/api\/diagnostics\/requests\?/);
  assert.match(pathWithSession, /sessionId=ses_deadbeefdeadbeefdeadbeefdeadbeef/);
  assert.match(pathWithSession, /protocol=responses/);
  assert.match(pathWithSession, /provider=openai-apikey/);
  assert.match(pathWithSession, /model=gpt-5.4/);
  assert.match(pathWithSession, /status=502/);
  assert.match(pathWithSession, /limit=200/);
  assert.equal(pathWithSession.includes("/api/logs"), false);
  assert.equal(diagnosticsDetailPath("req_ab12"), "/api/diagnostics/requests/req_ab12");
});

test("Diagnostics navigation still uses #logs?sessionId= and now lists through diagnostics", () => {
  assert.equal(logsHashForSession("ses_deadbeefdeadbeefdeadbeefdeadbeef"), "logs?sessionId=ses_deadbeefdeadbeefdeadbeefdeadbeef");
  assert.equal(
    logsListUrl("http://127.0.0.1:23100", "ses_deadbeefdeadbeefdeadbeefdeadbeef"),
    "http://127.0.0.1:23100/api/diagnostics/requests?limit=200&sessionId=ses_deadbeefdeadbeefdeadbeefdeadbeef",
  );
  assert.equal(logsSessionIdFromHash("#logs?sessionId=ses_abc"), "ses_abc");
  const route = parseHashRoute("#logs?sessionId=ses_abc");
  assert.equal(route.path, "logs");
  assert.equal(logsHashIsAllowed(route.path, route.query), true);
  assert.equal(logsHashIsAllowed("logs", new URLSearchParams("foo=1")), false);
});

test("summary parser keeps missing model/provider/tokens missing", () => {
  const parsed = parseDiagnosticsSummary(summary());
  assert.equal(parsed?.resolvedModel, undefined);
  assert.equal(parsed?.provider, undefined);
  assert.equal(parsed?.totalTokens, undefined);
  assert.equal(parsed?.usageStatus, undefined);
  assert.equal(parseDiagnosticsSummary({ ...summary(), requestId: 1 }), null);
});

test("detail parser does not invent routing, attempts, usage zeros, or cost", () => {
  const parsed = parseDiagnosticsDetail(summary());
  assert.equal(parsed?.routing, undefined);
  assert.deepEqual(parsed?.attempts, []);
  assert.equal(parsed?.usage, undefined);
  assert.equal(parsed?.cost, undefined);
  assert.equal(parsed?.failure, undefined);
  assert.equal(parsed?.correlationId, undefined);
});

test("policy and combo ids stay on their route kinds only", () => {
  const policy = parseDiagnosticsDetail({
    ...summary(),
    routing: { kind: "policy", policyId: "primary", requestedModel: "policy/primary", provider: "openai-apikey", resolvedModel: "gpt-5.4" },
  });
  assert.equal(policy?.routing?.kind, "policy");
  assert.equal(policy?.routing?.policyId, "primary");
  assert.equal(policy?.routing?.comboId, undefined);
  const combo = parseDiagnosticsDetail({
    ...summary(),
    routing: { kind: "combo", comboId: "fast", committedMember: "openai-apikey/gpt-5.4" },
  });
  assert.equal(combo?.routing?.comboId, "fast");
  assert.equal(combo?.routing?.committedMember, "openai-apikey/gpt-5.4");
  assert.equal(combo?.routing?.policyId, undefined);
});

test("usage and cost keep backend vocabulary and never zero-fill", () => {
  const usage = parseDiagnosticsDetail({
    ...summary(),
    usage: { status: "unreported" },
    cost: { kind: "unavailable", reason: "price_unmatched" },
  });
  assert.equal(usage?.usage?.status, "unreported");
  assert.equal(usage?.usage?.inputTokens, undefined);
  assert.equal(usage?.usage?.totalTokens, undefined);
  assert.equal(usage?.cost?.kind, "unavailable");
  assert.equal(usage?.cost?.total, undefined);
  assert.equal(formatDiagnosticsCost(usage?.cost), undefined);
  const exact = parseDiagnosticsDetail({
    ...summary(),
    usage: { status: "reported", inputTokens: 4, outputTokens: 2, totalTokens: 6 },
    cost: { kind: "exact", currency: "USD", total: 0.0031, price: { provider: "xai", modelId: "grok-4.6", source: "x.ai/docs", confidence: "exact" } },
  });
  assert.equal(exact?.usage?.inputTokens, 4);
  assert.equal(exact?.cost?.kind, "exact");
  assert.match(formatDiagnosticsCost(exact?.cost, "en") ?? "", /USD/);
});

test("timing milestones stay absent unless the backend returned them", () => {
  const parsed = parseDiagnosticsDetail({
    ...summary(),
    durationMs: 1240,
    timing: { totalMs: 1240, ttftMs: 20 },
  });
  assert.equal(parsed?.timing.totalMs, 1240);
  assert.equal(parsed?.timing.ttftMs, 20);
  assert.equal(parsed?.timing.headersMs, undefined);
  assert.equal(parsed?.timing.firstByteMs, undefined);
  assert.equal(formatDurationMs(1240, "s"), "1.24s");
  assert.equal(formatDurationMs(842, "s"), "842ms");
});

test("recorded 0 ms stays 0 ms and missing timings are not coerced to zero", () => {
  const parsed = parseDiagnosticsDetail({
    ...summary(),
    durationMs: 0,
    timing: { totalMs: 0, headersMs: 0, ttftMs: null, firstByteMs: undefined },
  });
  assert.equal(parsed?.timing.totalMs, 0);
  assert.equal(parsed?.timing.headersMs, 0);
  assert.equal(parsed?.timing.ttftMs, undefined);
  assert.equal(parsed?.timing.firstByteMs, undefined);
  assert.equal(formatDurationMs(0, "s"), "0ms");
  assert.equal(formatRecordedDurationMs(0, "s"), "0ms");
  assert.equal(formatRecordedDurationMs(undefined, "s"), undefined);
  assert.equal(formatRecordedDurationMs(Number.NaN, "s"), undefined);
});

test("incremental merge uses requestId and snapshot replace does not keep stale rows", () => {
  const older = parseDiagnosticsSummary(summary({ requestId: "req_old", timestamp: "2026-09-04T11:00:00.000Z" }));
  const newer = parseDiagnosticsSummary(summary({ requestId: "req_new", timestamp: "2026-09-04T12:00:00.000Z" }));
  const extra = parseDiagnosticsSummary(summary({ requestId: "req_extra", timestamp: "2026-09-04T12:01:00.000Z" }));
  const merged = mergeDiagnosticsRows([older, newer], [extra], "incremental");
  assert.deepEqual(merged.map(row => row.requestId), ["req_extra", "req_new", "req_old"]);
  const replaced = mergeDiagnosticsRows([older, newer], [newer], "snapshot");
  assert.deepEqual(replaced.map(row => row.requestId), ["req_new"]);
});

test("time range is client-side and list parser keeps cursor flags", () => {
  const row = parseDiagnosticsSummary(summary({ timestamp: "2026-09-04T12:00:00.000Z" }));
  const now = Date.parse("2026-09-04T12:30:00.000Z");
  assert.equal(rowInTimeRange(row, "all", now), true);
  assert.equal(rowInTimeRange(row, "1h", now), true);
  assert.equal(rowInTimeRange(row, "15m", now), false);
  const listed = parseDiagnosticsList({
    requests: [summary(), { requestId: 1 }],
    nextCursor: "MQ",
    reset: true,
    historyTruncated: true,
  });
  assert.equal(listed?.requests.length, 1);
  assert.equal(listed?.nextCursor, "MQ");
  assert.equal(listed?.reset, true);
  assert.equal(listed?.historyTruncated, true);
  assert.equal(snapshotHasOlder(DIAGNOSTICS_PAGE_SIZE, DIAGNOSTICS_PAGE_SIZE), true);
  assert.equal(snapshotHasOlder(12, DIAGNOSTICS_PAGE_SIZE), false);
});

test("server query omits empty filters and keeps sessionId", () => {
  const query = serverListQueryFromFilters({
    timeRange: "1h",
    status: "502",
    protocol: "responses",
    provider: "",
    model: "gpt-5.4",
  }, "ses_abc");
  assert.equal(query.status, 502);
  assert.equal(query.protocol, "responses");
  assert.equal(query.provider, undefined);
  assert.equal(query.model, "gpt-5.4");
  assert.equal(query.sessionId, "ses_abc");
  assert.equal(toolbarFiltersActive({ timeRange: "all", status: "", protocol: "", provider: "", model: "" }), false);
  assert.equal(toolbarFiltersActive({ timeRange: "1h", status: "", protocol: "", provider: "", model: "" }), true);
});

test("debug stream options are only enabled textual captures", () => {
  const debug = {
    enabled: true,
    usage: false,
    injection: true,
    claude: true,
    runtimeOverride: { debug: true },
    env: { debug: false, usage: false, injection: false, claude: false },
  };
  assert.deepEqual(enabledTextualStreams(debug), ["provider", "injection"]);
  assert.equal(isClaudeInboundApplicable(debug), true);
  assert.equal(isClaudeInboundApplicable({ ...debug, claude: undefined }), false);
  assert.deepEqual(enabledTextualStreams({ ...debug, enabled: false, injection: false }), []);
  assert.equal(hasRuntimeOverrides(debug), true);
  assert.equal(hasRuntimeOverrides({ ...debug, runtimeOverride: {} }), false);
  assert.equal(debugFlagSource(debug, "debug"), "runtime");
  assert.equal(debugFlagSource({ ...debug, runtimeOverride: { usage: false }, usage: false }, "usage"), null);
  assert.equal(debugFlagSource({
    ...debug,
    injection: false,
    runtimeOverride: { injection: false },
    env: { ...debug.env, injection: true },
  }, "injection"), "runtime");
  assert.equal(debugFlagSource({ ...debug, runtimeOverride: {}, env: { ...debug.env, injection: true } }, "injection"), "env");
  assert.equal(debugFlagSource({ ...debug, runtimeOverride: {} }, "usage"), null);
});

test("Diagnostics board locks page scroll and scrolls only the inspector column", () => {
  const css = readSrc("src", "styles-diagnostics-workspace.css");
  assert.match(css, /html:has\(\.main-inner--diagnostics\),\s*body:has\(\.main-inner--diagnostics\) \{\s*height: 100%;\s*overflow: hidden;/);
  assert.match(css, /\.app:has\(\.main-inner--diagnostics\) \{\s*height: 100dvh;\s*min-height: 100dvh;\s*overflow: hidden;/);
  assert.match(css, /\.main:has\(\.main-inner--diagnostics\) \{\s*height: 100dvh;\s*min-height: 0;\s*overflow: hidden;/);
  assert.match(css, /\.main-inner\.main-inner--diagnostics \{[^}]*overflow: hidden;/);
  assert.match(css, /\.diagnostics-board \{[^}]*overflow: hidden;/);
  assert.match(css, /\.diagnostics-split \{[^}]*overflow: hidden;/);
  assert.match(css, /\.diagnostics-detail \{[^}]*overflow: auto;/);
  assert.match(css, /\.diagnostics-detail \{[^}]*scrollbar-gutter: stable;/);
  assert.match(css, /\.diagnostics-detail \{[^}]*padding-right: 20px;/);
  assert.match(css, /\.diagnostics-detail \{[^}]*padding-left: 36px;/);
  assert.match(css, /\.diagnostics-master \{[^}]*overflow: hidden;/);
  assert.match(css, /::-webkit-scrollbar \{[^}]*width: 6px;/);
  assert.match(css, /\.debug-capture-row \{[^}]*grid-template-columns: repeat\(4, minmax\(0, 1fr\)\);/);
  assert.match(css, /\.debug-capture-copy \{[^}]*flex-direction: row;/);
});

test("Requests filters use the shared down-chevron Select, not a native arrowless select", () => {
  const filters = readSrc("src", "pages", "diagnostics-filters.tsx");
  assert.match(filters, /chevron="down"/);
  assert.match(filters, /<Select\b/);
  assert.equal(filters.includes("<select"), false);
});

test("Requests inspector source does not keep body tabs, notes, or context window", () => {
  const detail = readSrc("src", "pages", "diagnostics-detail.tsx");
  const page = readSrc("src", "pages", "Logs.tsx");
  assert.equal(detail.includes("logs.detailRaw"), false);
  assert.equal(detail.includes("Notes"), false);
  assert.equal(detail.includes("context window"), false);
  assert.equal(detail.includes("Overview"), false);
  assert.match(page, /DiagnosticsFilters/);
  assert.match(page, /DiagnosticsDetail/);
  assert.equal(page.includes("page-sub"), false);
  assert.match(detail, /IconCopy/);
  assert.equal(detail.includes("{copyLabel}"), false);
  assert.equal(detail.includes("{copiedLabel}"), false);
});

test("Debug capture is bounded and output hides stream when no textual capture is on", () => {
  const capture = readSrc("src", "pages", "debug-settings-panel.tsx");
  const page = readSrc("src", "pages", "Debug.tsx");
  const viewer = readSrc("src", "pages", "debug-log-viewer.tsx");
  assert.match(capture, /debug-capture/);
  assert.match(capture, /debug-capture-row/);
  assert.match(capture, /debug-capture-item/);
  assert.match(capture, /<Switch[\s\S]*debug-capture-copy/);
  assert.match(capture, /hasRuntimeOverrides\(debug\)/);
  assert.match(capture, /hasStreams &&/);
  assert.match(capture, /chevron="down"/);
  assert.equal(capture.includes("<select"), false);
  assert.match(page, /isClaudeInboundApplicable\(debug\) && debug\.claude/);
  assert.match(viewer, /debug\.output\.empty/);
  assert.equal(viewer.includes("prompt"), false);
  assert.equal(capture.includes("prompt"), false);
});
