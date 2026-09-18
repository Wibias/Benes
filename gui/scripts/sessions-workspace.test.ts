import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { parseHashRoute } from "../src/hash-routing.ts";
import { logsHashForSession, logsHashIsAllowed, logsListUrl, logsSessionIdFromHash } from "../src/pages/logs-session-filter.ts";
import {
  coverageView,
  formatSessionCost,
  joinIdList,
  protocolDisplayLabel,
  protocolSummaryLabel,
  sessionDetailPath,
  sessionListIdentity,
  sessionsListPath,
} from "../src/pages/sessions-contract.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

test("list identity keeps namespace and externalId separate", () => {
  const identity = sessionListIdentity({
    id: "ses_aaa",
    namespace: "codex/thread",
    externalId: "thread_8b3f7e1a",
    startedAt: "2026-05-18T09:42:18.000Z",
    lastActivityAt: "2026-05-18T09:47:36.000Z",
    requestCount: 128,
    protocols: ["responses"],
  });
  assert.equal(identity.primary, "codex/thread");
  assert.equal(identity.secondary, "thread_8b3f7e1a");
  assert.notEqual(identity.primary, identity.secondary);
});

test("missing externalId falls back to durable session id without fabricating names", () => {
  const identity = sessionListIdentity({
    id: "ses_bbb",
    namespace: "responses/chain",
    startedAt: "2026-05-18T09:42:18.000Z",
    lastActivityAt: "2026-05-18T09:47:36.000Z",
    requestCount: 1,
    protocols: ["responses"],
  });
  assert.equal(identity.secondary, "ses_bbb");
  assert.equal(identity.primary.includes("claude/project"), false);
});

test("protocol ids are humanised without inferring a client or surface", () => {
  assert.equal(protocolDisplayLabel("responses"), "Responses");
  assert.equal(protocolDisplayLabel("chat_completions"), "Chat Completions");
  assert.equal(protocolDisplayLabel("anthropic_messages"), "Messages");
  assert.equal(protocolDisplayLabel("responses").includes("Codex"), false);
  assert.equal(protocolDisplayLabel("anthropic_messages").includes("Claude"), false);
  assert.equal(protocolDisplayLabel("chat_completions").includes("OpenAI"), false);
});

test("multi-protocol summaries stay comma-separated and truthful", () => {
  assert.equal(
    protocolSummaryLabel(["chat_completions", "responses"]),
    "Chat Completions, Responses",
  );
});

test("search and filters are sent as server-side query parameters", () => {
  const path = sessionsListPath({
    q: "KEEP-thread",
    namespace: "codex/thread",
    protocol: "responses",
    provider: "openai-apikey",
    model: "gpt-5.6",
    policy: "primary",
    combo: "fast",
  });
  assert.match(path, /[?&]q=KEEP-thread/);
  assert.match(path, /namespace=codex%2Fthread/);
  assert.match(path, /protocol=responses/);
  assert.match(path, /provider=openai-apikey/);
  assert.match(path, /model=gpt-5.6/);
  assert.match(path, /policy=primary/);
  assert.match(path, /combo=fast/);
  assert.equal(path.includes("policy/primary"), false);
});

test("cursor pagination appends nextCursor without numeric pages", () => {
  const path = sessionsListPath({ q: "thread", protocol: "responses", cursor: "abc123" });
  assert.match(path, /q=thread/);
  assert.match(path, /protocol=responses/);
  assert.match(path, /cursor=abc123/);
  assert.equal(path.includes("page="), false);
  assert.equal(path.includes("offset="), false);
});

test("complete usage omits attributed coverage; incomplete usage includes it", () => {
  const complete = coverageView({ value: 61775, attributedRequests: 128, totalRequests: 128, complete: true }, String);
  assert.equal(complete.valueText, "61775");
  assert.equal(complete.attributedLine, false);
  const incomplete = coverageView({ value: 142380, attributedRequests: 127, totalRequests: 128, complete: false }, String);
  assert.equal(incomplete.valueText, "142380");
  assert.equal(incomplete.attributedLine, true);
  const missing = coverageView({ attributedRequests: 0, totalRequests: 128, complete: false }, String);
  assert.equal(missing.valueText, null);
  assert.equal(missing.attributedLine, true);
});

test("cost is currency-qualified and mixed currencies are not summed", () => {
  const usd = formatSessionCost({
    value: 0.0412,
    currency: "USD",
    currencies: ["USD"],
    attributedRequests: 128,
    totalRequests: 128,
    complete: true,
  }, "en");
  assert.equal(usd.valueText, "$0.0412 USD");
  assert.equal(usd.attributedLine, false);
  const mixed = formatSessionCost({
    value: 3,
    currencies: ["USD", "EUR"],
    attributedRequests: 2,
    totalRequests: 2,
    complete: false,
  }, "en");
  assert.equal(mixed.valueText, null);
});

test("policy and combo ids render raw without reconstructed prefixes", () => {
  assert.equal(joinIdList(["primary"]), "primary");
  assert.equal(joinIdList(["fast"]), "fast");
  assert.equal(joinIdList(["primary"])?.includes("policy/"), false);
  assert.equal(joinIdList(["fast"])?.includes("combo/"), false);
  assert.equal(joinIdList([]), null);
});

test("Diagnostics navigation uses the durable session id on /api/diagnostics/requests", () => {
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

test("detail fetch does not page request history for the Sessions GUI", () => {
  assert.equal(sessionDetailPath("ses_abc"), "/api/sessions/ses_abc?limit=1");
});

test("Sessions GUI source keeps the contract: no request table, no Integrations nav, no invented surfaces", () => {
  const detail = readSrc("src", "pages", "sessions-detail.tsx");
  const list = readSrc("src", "pages", "sessions-list.tsx");
  const hook = readSrc("src", "pages", "use-sessions-workspace.ts");
  const page = readSrc("src", "pages", "Sessions.tsx");
  const sidebar = readSrc("src", "app-sidebar.tsx");
  const combined = `${detail}\n${list}\n${page}\n${hook}`;
  assert.match(detail, /sessions\.field\.namespace/);
  assert.match(detail, /sessions\.field\.externalId/);
  assert.equal(combined.includes("Namespace / External ID"), false);
  assert.equal(combined.includes("Protocol / Surface"), false);
  assert.equal(combined.includes("claude/project"), false);
  assert.equal(combined.includes("codex/task"), false);
  assert.equal(combined.includes("openai/analysis"), false);
  assert.equal(combined.includes("policy/"), false);
  assert.equal(combined.includes("combo/"), false);
  assert.equal(combined.includes("Failover details"), false);
  assert.equal(combined.includes("sessions.col.status"), false);
  assert.equal(combined.includes("sessions.activity"), false);
  assert.match(detail, /logsHashForSession\(session\.id\)/);
  assert.match(hook, /sessionsListPath/);
  assert.match(hook, /sessionFiltersPath/);
  assert.match(hook, /nextCursor/);
  assert.equal(page.includes("Showing"), false);
  assert.equal(sidebar.includes('id: "integrations"'), false);
  assert.match(sidebar, /tkey: "nav.api"/);
  const observation = sidebar.slice(sidebar.indexOf("nav.group.observation"), sidebar.indexOf("nav.group.system"));
  assert.match(observation, /id: "sessions"/);
  assert.equal(observation.includes('id: "api"'), false);
});
