import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  parseUsageResponse,
  cacheReadTokens,
  usageReadFailed,
  usageBreakdownFromPath,
  usageTabFromPath,
  usageTabHash,
} from "../src/pages/usage-contract.ts";
import { presentCost, costClassRows, formatUsageUsd, costChartState, COST_EMPTY_NO_METER, COST_EMPTY_NO_PRICE, COST_EMPTY_PROXY } from "../src/pages/usage-cost.ts";
import { topModelSeries, bucketDailyStacks, dailyCostStacks, pricedCostAmount, costTooltipHead, sparseAxisLabels } from "../src/pages/usage-series.ts";
import { cacheReadShare, coverageShare, formatAxisDate, formatPct1, formatWindowRange, historyDisplay, niceCeiling, chartAxisTicks } from "../src/pages/usage-format.ts";
const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

function cost(overrides = {}) {
  return {
    currency: "USD",
    exact: { amountUsd: 0, requests: 0 },
    estimated: { amountUsd: 0, requests: 0 },
    lowerBound: { amountUsd: 0, requests: 0 },
    stale: { amountUsd: 0, requests: 0 },
    pricedRequests: 0,
    unpricedRequests: 0,
    unmeteredRequests: 0,
    displayTotalSafe: false,
    ...overrides,
  };
}

test("parseUsageResponse drops entriesDropped and keeps structured cost", () => {
  const parsed = parseUsageResponse({
    range: "30d",
    surface: "all",
    since: 1,
    generatedAt: 2,
    summary: { requests: 10, measuredRequests: 8, inputTokens: 100, outputTokens: 20, totalTokens: 120, coverageRatio: 0.8 },
    days: [],
    models: [],
    providers: [],
    accounts: [{ accountLogLabel: "main", requests: 3, measuredRequests: 3, totalTokens: 40, usageCoverageRatio: 1, cost: cost({ displayTotalSafe: true, status: "exact", pricedRequests: 3, exact: { amountUsd: 1.5, requests: 3 } }) }],
    historyTruncated: false,
    truncatedPrefixBytes: 0,
    snapshotWindowStart: 10,
    snapshotWindowEnd: 20,
    surfaceAttribution: { codex: 4, claude: 2, claudeDesktop: 1, grok: 1, unattributed: 2 },
    cost: cost({ displayTotalSafe: true, status: "exact", pricedRequests: 8, exact: { amountUsd: 12.5, requests: 8 } }),
    entriesDropped: 99,
    entriesTruncated: true,
  });
  assert.ok(parsed);
  assert.equal("entriesDropped" in parsed, false);
  assert.equal(parsed.cost?.status, "exact");
  assert.equal(parsed.accounts[0]?.accountLogLabel, "main");
  assert.equal(parsed.surfaceAttribution?.claudeDesktop, 1);
});

test("older-proxy payloads without cost stay unavailable rather than crashing", () => {
  const parsed = parseUsageResponse({
    range: "today",
    surface: "codex",
    generatedAt: 1,
    summary: { requests: 2, totalTokens: 10 },
  });
  assert.ok(parsed);
  assert.equal(parsed.cost, undefined);
  assert.equal(parsed.surfaceAttribution, undefined);
  assert.deepEqual(parsed.accounts, []);
  assert.equal(presentCost(parsed.cost).kind, "unavailable");
});

test("cost presentation keeps exact/estimated/lower-bound/stale distinct", () => {
  assert.deepEqual(
    presentCost(cost({ displayTotalSafe: true, status: "exact", exact: { amountUsd: 141.22, requests: 10 }, pricedRequests: 10 })),
    { kind: "amount", status: "exact", amount: 141.22, prefix: "" },
  );
  assert.equal(
    presentCost(cost({ displayTotalSafe: true, status: "estimated", estimated: { amountUsd: 10, requests: 2 }, pricedRequests: 2 })).prefix,
    "≈",
  );
  assert.equal(
    presentCost(cost({ displayTotalSafe: true, status: "lower_bound", lowerBound: { amountUsd: 4, requests: 2 }, pricedRequests: 2 })).prefix,
    "≥",
  );
  assert.equal(
    presentCost(cost({ displayTotalSafe: true, status: "stale", stale: { amountUsd: 3, requests: 1 }, pricedRequests: 1 })).status,
    "stale",
  );
});

test("mixed and gapped cost never fabricate a unified total", () => {
  assert.equal(
    presentCost(cost({
      status: "mixed",
      exact: { amountUsd: 10, requests: 2 },
      estimated: { amountUsd: 4, requests: 1 },
      pricedRequests: 3,
    })).kind,
    "mixed",
  );
  const partial = presentCost(cost({ unpricedRequests: 3, exact: { amountUsd: 2, requests: 1 }, pricedRequests: 1 }));
  assert.deepEqual(partial, { kind: "partial", gapRequests: 3 });
  assert.deepEqual(presentCost(cost({ pricedRequests: 0, unpricedRequests: 60 })), { kind: "partial", gapRequests: 60 });
  assert.deepEqual(presentCost(cost({ pricedRequests: 0, unmeteredRequests: 5 })), { kind: "partial", gapRequests: 5 });
  assert.equal(presentCost(undefined).kind, "unavailable");
  assert.equal(formatUsageUsd(-1), "\u2014");
});

test("cost chart hides a $0 series when nothing in the range is priced", () => {
  const empty = costChartState(cost({ unpricedRequests: 360 }), 360);
  assert.deepEqual(empty, { kind: "empty", source: COST_EMPTY_NO_PRICE, count: 360, showConfigureHint: true });
  const unmeteredOnly = costChartState(cost({ unmeteredRequests: 8 }), 8);
  assert.deepEqual(unmeteredOnly, { kind: "empty", source: COST_EMPTY_NO_METER, count: 8, showConfigureHint: false });
  const missing = costChartState(undefined, 12);
  assert.deepEqual(missing, { kind: "empty", source: COST_EMPTY_PROXY, showConfigureHint: false });
  const partial = costChartState(cost({
    pricedRequests: 4,
    unpricedRequests: 2,
    unmeteredRequests: 1,
    exact: { amountUsd: 3, requests: 4 },
  }), 7);
  assert.deepEqual(partial, { kind: "partial", gapRequests: 3 });
  assert.equal(
    costChartState(cost({
      displayTotalSafe: true,
      status: "exact",
      pricedRequests: 4,
      exact: { amountUsd: 1.5, requests: 4 },
    }), 4).kind,
    "chart",
  );
  assert.equal(costChartState(cost(), 0).kind, "chart");
});

test("cost class rows keep unpriced and unmetered amounts absent", () => {
  const rows = costClassRows(cost({
    exact: { amountUsd: 12, requests: 4 },
    unpricedRequests: 2,
    unmeteredRequests: 1,
    pricedRequests: 4,
  }));
  assert.equal(rows.find(row => row.id === "unpriced")?.amountUsd, undefined);
  assert.equal(rows.find(row => row.id === "unmetered")?.amountUsd, undefined);
  assert.equal(rows.find(row => row.id === "exact")?.amountUsd, 12);
});

test("model mix keeps top four series and aggregates Other", () => {
  const models = [
    { provider: "openai", model: "a", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 60, inputTokens: 50, outputTokens: 10, shareRatio: 0.6 },
    { provider: "anthropic", model: "b", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 20, inputTokens: 16, outputTokens: 4, shareRatio: 0.2 },
    { provider: "openai", model: "c", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 10, inputTokens: 8, outputTokens: 2, shareRatio: 0.1 },
    { provider: "xai", model: "d", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 6, inputTokens: 5, outputTokens: 1, shareRatio: 0.06 },
    { provider: "google", model: "e", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 3, inputTokens: 2, outputTokens: 1, shareRatio: 0.03 },
    { provider: "mistral", model: "f", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 1, inputTokens: 1, outputTokens: 0, shareRatio: 0.01 },
  ];
  const series = topModelSeries(models, 100);
  assert.equal(series.length, 5);
  assert.equal(series[0].model, "a");
  assert.equal(series[4].other, true);
  assert.equal(series[4].totalTokens, 4);
  assert.equal(series[4].share, 0.04);
});

test("daily stacks reuse the same series keys and bucket long ranges", () => {
  const models = [
    { provider: "openai", model: "a", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 80, inputTokens: 70, outputTokens: 10, shareRatio: 0.8 },
    { provider: "xai", model: "z", requests: 1, attemptCount: 1, measuredRequests: 1, reportedRequests: 1, estimatedRequests: 0, totalTokens: 20, inputTokens: 18, outputTokens: 2, shareRatio: 0.2 },
  ];
  const series = topModelSeries(models, 100);
  const days = Array.from({ length: 50 }, (_, i) => ({
    date: `2026-07-${String((i % 28) + 1).padStart(2, "0")}`,
    requests: 1,
    measuredRequests: 1,
    reportedRequests: 1,
    totalTokens: 10,
    models: [{ model: "a", provider: "openai", requests: 1, totalTokens: 10 }],
  }));
  const stacked = bucketDailyStacks(days, series);
  assert.ok(stacked.length < days.length);
  assert.ok(stacked.every(row => row.segments.every(seg => series.some(item => item.key === seg.key))));
  assert.deepEqual([...sparseAxisLabels(3)], [0, 1, 2]);
  assert.equal(sparseAxisLabels(40).has(0), true);
  assert.equal(sparseAxisLabels(40).has(39), true);
});

test("cost stacks bucket long ranges without collapsing confidence classes", () => {
  const days = Array.from({ length: 50 }, (_, i) => ({
    date: `2026-07-${String((i % 28) + 1).padStart(2, "0")}`,
    requests: 3,
    measuredRequests: 3,
    reportedRequests: 3,
    totalTokens: 10,
    models: [],
    cost: cost({
      pricedRequests: 2,
      unpricedRequests: 1,
      exact: { amountUsd: 0.4, requests: 1 },
      estimated: { amountUsd: 0.2, requests: 1 },
    }),
  }));
  const weekly = dailyCostStacks(days);
  assert.ok(weekly.length < days.length);
  assert.ok(weekly.every(row => row.segments.some(seg => seg.id === "exact")));
  assert.ok(weekly.every(row => row.segments.some(seg => seg.id === "estimated")));
  assert.equal(weekly.reduce((sum, row) => sum + row.cost.exactAmount, 0), 20);
  assert.equal(weekly.reduce((sum, row) => sum + row.cost.estimatedAmount, 0), 10);
  assert.equal(weekly.reduce((sum, row) => sum + row.cost.unpricedRequests, 0), 50);
  assert.equal(weekly.reduce((sum, row) => sum + row.requests, 0), 150);
  assert.equal(weekly.some(row => row.cost.exactAmount > 0 && row.cost.estimatedAmount > 0 && row.mixed), true);
  const monthlyDays = Array.from({ length: 200 }, (_, i) => ({
    date: `2026-${String((i % 12) + 1).padStart(2, "0")}-01`,
    requests: 2,
    measuredRequests: 2,
    reportedRequests: 2,
    totalTokens: 4,
    models: [],
    cost: cost({
      pricedRequests: 1,
      unmeteredRequests: 1,
      stale: { amountUsd: 0.5, requests: 1 },
    }),
  }));
  const monthly = dailyCostStacks(monthlyDays);
  assert.ok(monthly.length < monthlyDays.length);
  assert.ok(monthly.every(row => row.label.length === 7));
  assert.equal(monthly.reduce((sum, row) => sum + row.cost.staleAmount, 0), 100);
  assert.equal(monthly.reduce((sum, row) => sum + row.cost.unmeteredRequests, 0), 200);
  assert.equal(pricedCostAmount(monthly[0].cost), monthly[0].cost.staleAmount);
});

test("mixed cost tooltips do not show an unsafe summed dollar total", () => {
  assert.deepEqual(
    costTooltipHead({
      exactAmount: 10,
      estimatedAmount: 4,
      lowerBoundAmount: 0,
      staleAmount: 0,
      unpricedRequests: 2,
      unmeteredRequests: 0,
    }),
    { kind: "mixed" },
  );
  assert.deepEqual(
    costTooltipHead({
      exactAmount: 1.5,
      estimatedAmount: 0,
      lowerBoundAmount: 0,
      staleAmount: 0,
      unpricedRequests: 0,
      unmeteredRequests: 0,
    }),
    { kind: "amount", status: "exact", amount: 1.5, prefix: "" },
  );
  assert.deepEqual(
    costTooltipHead({
      exactAmount: 3,
      estimatedAmount: 0,
      lowerBoundAmount: 0,
      staleAmount: 0,
      unpricedRequests: 2,
      unmeteredRequests: 0,
    }),
    { kind: "classes" },
  );
  assert.equal(
    costTooltipHead({
      exactAmount: 0,
      estimatedAmount: 8,
      lowerBoundAmount: 0,
      staleAmount: 0,
      unpricedRequests: 0,
      unmeteredRequests: 0,
    }).kind,
    "amount",
  );
  const src = readSrc("src/pages/usage-overview.tsx");
  assert.match(src, /costTooltipHead/);
  assert.equal(src.includes("pricedCostAmount(hovered"), false);
});

test("history read window collapses a same-year range", () => {
  const start = Date.UTC(2026, 7, 7);
  const end = Date.UTC(2026, 8, 5);
  assert.equal(formatWindowRange(start, end, "en-US", "UTC"), "Aug 7 – Sep 5, 2026");
  assert.equal(formatWindowRange(undefined, end, "en-US", "UTC"), undefined);
});

test("chart dates use locale-aware Intl formatting", () => {
  const day = new Date(2026, 7, 7);
  const month = new Date(2026, 7, 1);
  assert.equal(formatAxisDate("2026-08-07", "en-US"), new Intl.DateTimeFormat("en-US", { month: "short", day: "numeric" }).format(day));
  assert.equal(formatAxisDate("2026-08", "en-US"), new Intl.DateTimeFormat("en-US", { month: "short", year: "numeric" }).format(month));
  assert.notEqual(formatAxisDate("2026-08-07", "en-US"), formatAxisDate("2026-08-07", "de-DE"));
  const src = readSrc("src/pages/usage-format.ts");
  assert.equal(src.includes("Jan\", \"Feb\""), false);
});

test("chart axis uses a nice ceiling so bars and labels share a scale", () => {
  assert.equal(niceCeiling(456120), 500000);
  assert.deepEqual(chartAxisTicks(456120), [500000, 375000, 250000, 125000, 0]);
  assert.equal(niceCeiling(0), 1);
});

test("percentages and cache-read stay absent when optional values are missing", () => {
  assert.equal(formatPct1(0.984), "98.4%");
  assert.equal(formatPct1(undefined), undefined);
  assert.equal(coverageShare(8, 0), undefined);
  assert.equal(coverageShare(8, 10), 0.8);
  const missing = cacheReadShare({
    requests: 10, attemptCount: 10, measuredRequests: 10, reportedRequests: 10, unreportedRequests: 0,
    unsupportedRequests: 0, estimatedRequests: 0, inputTokens: 100, outputTokens: 10,
    reasoningOutputTokens: 0, totalTokens: 110, coverageRatio: 1,
  });
  assert.equal(missing, undefined);
  const present = cacheReadShare({
    requests: 10, attemptCount: 10, measuredRequests: 10, reportedRequests: 10, unreportedRequests: 0,
    unsupportedRequests: 0, estimatedRequests: 0, inputTokens: 100, outputTokens: 10,
    cacheReadInputTokens: 40, reasoningOutputTokens: 0, totalTokens: 110, coverageRatio: 1,
  });
  assert.equal(present?.ratio, 0.4);
  assert.equal(cacheReadTokens({ cacheReadInputTokens: 3, cachedInputTokens: 9 }), 3);
});

test("history display distinguishes complete from truncated without entriesDropped", () => {
  assert.deepEqual(
    historyDisplay({ historyTruncated: false, truncatedPrefixBytes: 0, snapshotWindowStart: 1, snapshotWindowEnd: 2 }),
    { kind: "complete", start: 1, end: 2 },
  );
  const partial = historyDisplay({ historyTruncated: true, truncatedPrefixBytes: 4096, snapshotWindowStart: 8, snapshotWindowEnd: 9 });
  assert.equal(partial.kind, "partial");
  if (partial.kind === "partial") assert.equal(partial.skippedPrefixBytes, 4096);
});

test("usage hashes map Overview / Breakdown / Coverage without KPI labels", () => {
  assert.equal(usageTabFromPath("usage"), "overview");
  assert.equal(usageTabFromPath("usage/breakdown"), "breakdown");
  assert.equal(usageBreakdownFromPath("usage/breakdown/accounts"), "accounts");
  assert.equal(usageTabFromPath("usage/coverage"), "coverage");
  assert.equal(usageTabHash("overview"), "usage");
  assert.equal(usageTabHash("breakdown", "providers"), "usage/breakdown/providers");
});

test("read_failed is distinct from empty usage", () => {
  const failed = parseUsageResponse({ error: "read_failed", summary: { requests: 0 } });
  assert.equal(usageReadFailed(failed), true);
  const empty = parseUsageResponse({ summary: { requests: 0 } });
  assert.equal(usageReadFailed(empty), false);
});

test("Usage board source does not keep heatmap, kebab, or entriesDropped", () => {
  const src = [
    readSrc("src/pages/Usage.tsx"),
    readSrc("src/pages/usage-overview.tsx"),
    readSrc("src/pages/usage-breakdown.tsx"),
    readSrc("src/pages/usage-coverage.tsx"),
    readSrc("src/pages/usage-contract.ts"),
  ].join("\n");
  assert.equal(src.includes("buildHeatmap"), false);
  assert.equal(src.includes("entriesDropped"), false);
  assert.equal(src.includes("View all models"), false);
  assert.equal(src.includes("kebab"), false);
  assert.equal(src.includes("7.86B"), false);
  assert.equal(src.includes("141.22"), false);
});

test("Breakdown cost cells explain Mixed and Partial without extra row text", () => {
  const src = readSrc("src/pages/usage-breakdown.tsx");
  assert.match(src, /title=\{t\("usage\.cost\.mixedNote"\)\}/);
  assert.match(src, /title=\{t\("usage\.cost\.partialNote"/);
  assert.match(src, /active \? \(dir === "desc" \? " ↓" : " ↑"\) : ""/);
});

test("Overview daily title stays Daily usage and coverage KPI uses measured counts", () => {
  const src = readSrc("src/pages/usage-overview.tsx");
  assert.match(src, /t\("usage\.section\.daily"\)/);
  assert.equal(src.includes("usage.section.dailyTokens"), false);
  assert.match(src, /usage\.kpi\.coverageMeasuredOf/);
  assert.match(src, /usage\.kpi\.requestsSub/);
});

test("Cost chart empty and partial copy does not imply unpriced traffic is $0", () => {
  const src = readSrc("src/pages/usage-overview.tsx");
  assert.match(src, /costChartState/);
  assert.match(src, /usage\.chart\.costUnpriced/);
  assert.match(src, /usage\.chart\.costUnmetered/);
  assert.match(src, /usage\.chart\.costProxyMissing/);
  assert.match(src, /usage\.chart\.costConfigure/);
  assert.match(src, /usage\.chart\.costPartial/);
  assert.match(src, /usage-chart-empty/);
  assert.match(src, /ChartHoverTip/);
  assert.match(src, /costTooltipHead/);
});

test("Providers and Accounts rename Total to Total tokens; Models keep Total", () => {
  const src = readSrc("src/pages/usage-breakdown.tsx");
  assert.equal((src.match(/t\("usage\.col\.total"\)/g) || []).length, 1);
  assert.equal((src.match(/t\("usage\.col\.totalTokens"\)/g) || []).length, 2);
});
