import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { hashBelongsToPage, resolveAppHashChange } from "../src/app-routing.ts";
import {
  sessionsHashForCombo,
  sessionsHashForPolicy,
  sessionsHashIsAllowed,
} from "../src/pages/sessions-hash-filter.ts";
import {
  modelsCatalogMutationStable,
  shouldApplyModelsCatalogLoad,
} from "../src/pages/models-change-sync.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

test("Usage Overview Breakdown Coverage hashes survive app-route normalization", () => {
  assert.equal(hashBelongsToPage("usage", "usage"), true);
  assert.equal(hashBelongsToPage("usage/breakdown", "usage"), true);
  assert.equal(hashBelongsToPage("usage/breakdown/providers", "usage"), true);
  assert.equal(hashBelongsToPage("usage/breakdown/accounts", "usage"), true);
  assert.equal(hashBelongsToPage("usage/coverage", "usage"), true);
  assert.deepEqual(resolveAppHashChange("usage/breakdown"), { page: "usage", replaceTo: null });
  assert.deepEqual(resolveAppHashChange("usage/coverage"), { page: "usage", replaceTo: null });
  assert.equal(hashBelongsToPage("usage/foo", "usage"), false);
  assert.equal(hashBelongsToPage("usage/breakdown/foo", "usage"), false);
  assert.equal(hashBelongsToPage("usage/coverage/foo", "usage"), false);
  assert.deepEqual(resolveAppHashChange("usage/foo"), { page: "usage", replaceTo: "usage" });
  assert.deepEqual(resolveAppHashChange("usage/breakdown/foo"), { page: "usage", replaceTo: "usage" });
  assert.deepEqual(resolveAppHashChange("usage/coverage/foo"), { page: "usage", replaceTo: "usage" });
});

test("Sessions policy and combo deep-links survive app-route normalization", () => {
  const policyHash = sessionsHashForPolicy("primary");
  const comboHash = sessionsHashForCombo("fast");
  assert.equal(policyHash, "sessions?policy=primary");
  assert.equal(comboHash, "sessions?combo=fast");
  assert.equal(hashBelongsToPage(policyHash, "sessions"), true);
  assert.equal(hashBelongsToPage(comboHash, "sessions"), true);
  assert.deepEqual(resolveAppHashChange(policyHash), { page: "sessions", replaceTo: null });
  assert.deepEqual(resolveAppHashChange(comboHash), { page: "sessions", replaceTo: null });
  assert.equal(sessionsHashIsAllowed("sessions", new URLSearchParams("policy=a&combo=b")), false);
});

test("Sessions workspace consumes the routing hash and exposes a flat active-filter banner", () => {
  const hook = readSrc("src", "pages", "use-sessions-workspace.ts");
  const page = readSrc("src", "pages", "Sessions.tsx");
  const styles = readSrc("src", "styles-sessions-workspace.css");
  assert.match(hook, /sessionsPolicyFromHash/);
  assert.match(hook, /sessionsComboFromHash/);
  assert.match(hook, /window\.addEventListener\("hashchange"/);
  assert.match(hook, /policy:\s*routingFilter\.policyId \|\| filters\.policy/);
  assert.match(hook, /combo:\s*routingFilter\.comboId \|\| filters\.combo/);
  assert.match(page, /SessionsRoutingBanner/);
  assert.match(styles, /\.sessions-routing-filter/);
});

test("Models rejects catalog loads across active or settled optimistic mutations", () => {
  assert.equal(modelsCatalogMutationStable(7, 7, 0), true);
  assert.equal(modelsCatalogMutationStable(7, 7, 1), false);
  assert.equal(modelsCatalogMutationStable(7, 8, 0), false);
  assert.equal(shouldApplyModelsCatalogLoad(3, 3, 7, 7, 0), true);
  assert.equal(shouldApplyModelsCatalogLoad(3, 4, 7, 7, 0), false);
  assert.equal(shouldApplyModelsCatalogLoad(3, 3, 7, 8, 0), false);
  assert.equal(shouldApplyModelsCatalogLoad(3, 3, 7, 7, 1), false);
});

test("Models publishes optimistic catalog truth before the trailing refresh", () => {
  const models = readSrc("src", "pages", "Models.tsx");
  const writes = readSrc("src", "pages", "use-models-change-writes.ts");
  const fetchStart = models.indexOf("const fetchCatalog");
  const applyStart = models.indexOf("const applyCatalog");
  const resourceStart = models.indexOf("const catalogResource");
  assert.ok(fetchStart >= 0 && applyStart > fetchStart && resourceStart > applyStart);
  const fetchCatalogSource = models.slice(fetchStart, applyStart);
  const applyCatalogSource = models.slice(applyStart, resourceStart);
  assert.equal(fetchCatalogSource.includes("writeSessionListCache"), false);
  assert.match(applyCatalogSource, /catalogSnapshotRef\.current = next/);
  assert.match(applyCatalogSource, /writeSessionListCache\(cacheKey, next\)/);
  assert.match(models, /catalogMutationEpochRef/);
  assert.match(models, /catalogMutationPendingRef/);
  assert.match(models, /modelsCatalogMutationStable/);
  assert.match(models, /shouldApplyModelsCatalogLoad/);
  assert.match(models, /setClientResourceData\(cacheKey, next\)/);
  // Exact publish-before-write timing is covered executably in models-change-writes.test.ts.
  assert.match(writes, /nextDisabledModels/);
  assert.match(writes, /nextSelectedAllowlist/);
  assert.match(writes, /publishCatalogMutation/);
  assert.match(writes, /settleCatalogMutation/);
  assert.match(writes, /catalogSync\.schedule/);
});

test("Dashboard Overview is DashboardPlaneBoard; legacy Overview subtree stays gone", () => {
  const dashboard = readSrc("src", "pages", "Dashboard.tsx");
  assert.match(dashboard, /from "\.\/dashboard-plane-board"/);
  assert.match(dashboard, /<DashboardPlaneBoard \{\.\.\.d\} \/>/);
  assert.equal(dashboard.includes("dashboard-overview-"), false);
  assert.equal(dashboard.includes("dashboard-maintenance"), false);
  assert.equal(dashboard.includes("dashboard-sidecar-cards"), false);
  assert.equal(dashboard.includes("MemoryObservabilityCard"), false);
  const gone = [
    ["src", "pages", "dashboard-overview-section.tsx"],
    ["src", "pages", "dashboard-overview-panels.tsx"],
    ["src", "pages", "dashboard-overview-head.tsx"],
    ["src", "pages", "dashboard-overview-sections.tsx"],
    ["src", "pages", "dashboard-overview-stats.tsx"],
    ["src", "pages", "dashboard-maintenance.tsx"],
    ["src", "pages", "dashboard-sidecar-cards.tsx"],
    ["src", "components", "MemoryObservabilityCard.tsx"],
    ["src", "components", "memory-observability", "memory-card-sections.tsx"],
    ["src", "components", "memory-observability", "memory-metrics.ts"],
  ];
  for (const parts of gone) {
    assert.equal(existsSync(path.join(guiRoot, ...parts)), false, parts.join("/"));
  }
});

test("dashboard box radii cap at 4px; buttons stay pill", () => {
  const css = readSrc("src", "styles.css");
  assert.match(css, /--radius:\s*4px/);
  assert.match(css, /--radius-sm:\s*4px/);
  assert.match(css, /--radius-xs:\s*4px/);
  assert.match(css, /--radius-lg:\s*4px/);
  assert.match(css, /--radius-pill:\s*999px/);
  assert.match(css, /--radius-round:\s*50%/);
  assert.equal(css.includes("--radius: 12px"), false);
  assert.equal(css.includes("--radius-sm: 8px"), false);
  assert.equal(css.includes("--radius-xs: 6px"), false);
  assert.equal(css.includes("--radius-lg: 16px"), false);
  assert.equal(css.includes("--radius-pill: 4px"), false);
});
