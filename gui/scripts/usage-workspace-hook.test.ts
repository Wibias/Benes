import assert from "node:assert/strict";
import test from "node:test";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

import {
  buildUsageWorkspaceControlDefaults,
  resolveUsageViewerTimeZone,
} from "../src/pages/usage-workspace-defaults.ts";
import {
  clearUsageWorkspaceHeldMemory,
  readUsageWorkspaceHeld,
  usageWorkspaceHeldIdentity,
  writeUsageWorkspaceHeld,
} from "../src/pages/usage-workspace-held.ts";
import {
  loadUsageWorkspacePayload,
  planUsageWorkspaceFetch,
  usageWorkspaceRequestUrl,
} from "../src/pages/usage-workspace-request.ts";
import { usageSearchParams } from "../src/pages/usage-range.ts";
import type { UsageResponse } from "../src/pages/usage-contract.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function sampleUsage(): UsageResponse {
  return {
    range: "30d",
    surface: "all",
    generatedAt: 1,
    summary: {
      requests: 1,
      measuredRequests: 1,
      inputTokens: 1,
      outputTokens: 1,
      totalTokens: 2,
      coverageRatio: 1,
    },
    days: [],
    models: [],
    providers: [],
    accounts: [],
    historyTruncated: false,
    truncatedPrefixBytes: 0,
  };
}

test("usage workspace defaults are 30d / all / empty model query", () => {
  const defaults = buildUsageWorkspaceControlDefaults("UTC");
  assert.equal(defaults.range, "30d");
  assert.equal(defaults.surface, "all");
  assert.equal(defaults.modelQuery, "");
  assert.equal(defaults.timeZone, "UTC");
  assert.ok(defaults.customStart);
  assert.ok(defaults.customEnd);
});

test("viewer timezone falls back to UTC when unresolved", () => {
  assert.equal(resolveUsageViewerTimeZone(""), "UTC");
  assert.equal(resolveUsageViewerTimeZone(null), "UTC");
  assert.equal(resolveUsageViewerTimeZone("Europe/Berlin"), "Europe/Berlin");
});

test("invalid custom range plans no fetch", () => {
  const malformed = usageSearchParams("custom", "all", "bad", "also-bad", "UTC");
  const plan = planUsageWorkspaceFetch(malformed);
  assert.equal(plan.ready, false);
  if (!plan.ready) assert.equal(plan.reason, "malformed");

  const reversed = usageSearchParams("custom", "all", "2026-09-10T00:00", "2026-09-01T00:00", "UTC");
  const reversedPlan = planUsageWorkspaceFetch(reversed);
  assert.equal(reversedPlan.ready, false);
  if (!reversedPlan.ready) assert.equal(reversedPlan.reason, "reversed");
});

test("valid range plans a query owned by usageSearchParams", () => {
  const rangeQuery = usageSearchParams("30d", "all", "", "", "UTC");
  const plan = planUsageWorkspaceFetch(rangeQuery);
  assert.equal(plan.ready, true);
  if (!plan.ready) return;
  assert.equal(plan.query, rangeQuery.ok ? rangeQuery.query : "");
  assert.match(plan.query, /range=30d/);
  assert.match(plan.query, /surface=all/);
  assert.match(plan.query, /tz=UTC/);
});

test("request URL is GET /api/usage with the planned query", () => {
  assert.equal(
    usageWorkspaceRequestUrl("http://127.0.0.1:23100", "range=30d&surface=all&tz=UTC"),
    "http://127.0.0.1:23100/api/usage?range=30d&surface=all&tz=UTC",
  );
});

test("held cache identity isolates apiBase and query", () => {
  clearUsageWorkspaceHeldMemory();
  const a = usageWorkspaceHeldIdentity("http://a", "range=30d");
  const b = usageWorkspaceHeldIdentity("http://b", "range=30d");
  const c = usageWorkspaceHeldIdentity("http://a", "range=7d");
  assert.notEqual(a, b);
  assert.notEqual(a, c);
  writeUsageWorkspaceHeld("http://a", "range=30d", sampleUsage());
  assert.ok(readUsageWorkspaceHeld("http://a", "range=30d"));
  assert.equal(readUsageWorkspaceHeld("http://b", "range=30d"), null);
  assert.equal(readUsageWorkspaceHeld("http://a", "range=7d"), null);
  assert.equal(readUsageWorkspaceHeld("http://a", ""), null);
});

test("HTTP failure and malformed payloads fail closed", async () => {
  const original = globalThis.fetch;
  globalThis.fetch = (async () => new Response("nope", { status: 500, statusText: "ERR" })) as typeof fetch;
  await assert.rejects(
    () => loadUsageWorkspacePayload("http://127.0.0.1:9", "range=30d", new AbortController().signal),
    /500/,
  );
  globalThis.fetch = (async () =>
    new Response(JSON.stringify(["not-an-object"]), {
      status: 200,
      headers: { "content-type": "application/json" },
    })) as typeof fetch;
  await assert.rejects(
    () => loadUsageWorkspacePayload("http://127.0.0.1:9", "range=30d", new AbortController().signal),
    /invalid_usage/,
  );
  globalThis.fetch = original;
});

test("successful load writes held cache for last-good reads", async () => {
  clearUsageWorkspaceHeldMemory();
  const payload = sampleUsage();
  const original = globalThis.fetch;
  globalThis.fetch = (async () =>
    new Response(JSON.stringify(payload), {
      status: 200,
      headers: { "content-type": "application/json" },
    })) as typeof fetch;
  const loaded = await loadUsageWorkspacePayload(
    "http://127.0.0.1:9",
    "range=30d&surface=all&tz=UTC",
    new AbortController().signal,
  );
  assert.equal(loaded.summary.requests, 1);
  assert.deepEqual(
    readUsageWorkspaceHeld("http://127.0.0.1:9", "range=30d&surface=all&tz=UTC")?.summary.requests,
    1,
  );
  globalThis.fetch = original;
});

test("orchestration hook stays thin and preserves live→held→null data path", () => {
  const src = readFileSync(path.join(guiRoot, "src/pages/use-usage-workspace.ts"), "utf8");
  assert.match(src, /resource\.state\.data \?\? held \?\? null/);
  assert.match(src, /enabled:\s*fetchPlan\.ready/);
  assert.match(src, /initialData:\s*held \?\? undefined/);
  assert.match(src, /usageSearchParams/);
  assert.match(src, /loadUsageWorkspacePayload/);
  assert.equal(src.includes("window.location"), false);
});
