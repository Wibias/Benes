import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  hashBelongsToPage,
  readPageFromHash,
  resolveAppHashChange,
} from "../src/app-routing.ts";
import {
  mountedRoutingSurfaces,
  readRoutingSurface,
  routingPrimaryTab,
  routingSurfaceHash,
} from "../src/pages/routing-tab.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const source = (relativePath) => readFileSync(path.join(guiRoot, relativePath), "utf8");

test("Routing canonical hashes resolve to the first-class routing page", () => {
  for (const hash of [
    "routing",
    "routing/compatibility",
    "routing/combos",
    "routing/evaluation",
    "routing/analytics",
  ]) {
    assert.equal(readPageFromHash(`#${hash}`), "routing", hash);
    assert.equal(hashBelongsToPage(hash, "routing"), true, hash);
    assert.deepEqual(resolveAppHashChange(hash), { page: "routing", replaceTo: null }, hash);
  }
});

test("legacy Routing hashes passively normalize to canonical routing destinations", () => {
  const cases = new Map([
    ["startup/routing", "routing"],
    ["startup/compatibility", "routing/compatibility"],
    ["startup/combos", "routing/combos"],
    ["startup/evaluation", "routing/evaluation"],
    ["startup/analytics", "routing/analytics"],
    ["models/routing", "routing"],
    ["models/compatibility", "routing/compatibility"],
    ["models/combos", "routing/combos"],
    ["lab", "routing/compatibility"],
    ["lab/verdicts", "routing/compatibility"],
    ["combos", "routing/combos"],
    ["combos/free", "routing/combos"],
    ["evaluation", "routing/evaluation"],
    ["evaluation/foo", "routing/evaluation"],
    ["analytics", "routing/analytics"],
    ["analytics/foo", "routing/analytics"],
  ]);
  for (const [legacy, canonical] of cases) {
    assert.deepEqual(
      resolveAppHashChange(legacy),
      { page: "routing", replaceTo: canonical },
      legacy,
    );
  }
});

test("Routing surface state keeps Evaluation and Analytics under Profiles", () => {
  assert.equal(readRoutingSurface("#routing"), "profiles");
  assert.equal(readRoutingSurface("#routing/evaluation"), "evaluation");
  assert.equal(readRoutingSurface("#routing/analytics"), "analytics");
  assert.equal(readRoutingSurface("#startup/evaluation"), "evaluation");
  assert.equal(readRoutingSurface("#startup/analytics"), "analytics");
  assert.equal(routingPrimaryTab("evaluation"), "profiles");
  assert.equal(routingPrimaryTab("analytics"), "profiles");
  assert.equal(routingPrimaryTab("compatibility"), "compatibility");
  assert.equal(routingSurfaceHash("profiles"), "routing");
  assert.equal(routingSurfaceHash("evaluation"), "routing/evaluation");
});

test("Routing keeps visited workspaces mounted across sub-surface navigation", () => {
  let mounted = mountedRoutingSurfaces(new Set(), "combos");
  assert.deepEqual([...mounted], ["combos"]);
  mounted = mountedRoutingSurfaces(mounted, "evaluation");
  assert.deepEqual(new Set(mounted), new Set(["combos", "evaluation", "profiles"]));
  mounted = mountedRoutingSurfaces(mounted, "compatibility");
  assert.deepEqual(new Set(mounted), new Set(["combos", "evaluation", "profiles", "compatibility"]));
  mounted = mountedRoutingSurfaces(mounted, "analytics");
  assert.deepEqual(new Set(mounted), new Set(["combos", "evaluation", "profiles", "compatibility", "analytics"]));
});

test("unknown Routing subpaths normalize to the Profiles root", () => {
  for (const hash of [
    "routing/nope",
    "routing/compatibility/nope",
    "routing/combos/nope",
    "routing/evaluation/nope",
    "routing/analytics/nope",
  ]) {
    assert.deepEqual(resolveAppHashChange(hash), {
      page: "routing",
      replaceTo: "routing",
    }, hash);
  }
});

test("Routing and Control have independent app/sidebar ownership", () => {
  const sidebar = source("src/app-sidebar.tsx");
  const appPage = source("src/app-page.tsx");
  const app = source("src/App.tsx");
  const appShell = source("src/app-shell.ts");

  assert.match(sidebar, /id:\s*["']routing["'][\s\S]*?tkey:\s*["']nav\.routing["']/);
  assert.doesNotMatch(sidebar, /id:\s*["']startup["'][\s\S]{0,120}?tkey:\s*["']nav\.routing["']/);
  assert.match(appPage, /case\s+["']routing["']:\s*return\s*<Routing\b/);
  assert.doesNotMatch(app, /readControlTab|controlTab/);
  assert.match(appShell, /startup:\s*["']nav\.control["']/);
  assert.match(appShell, /routing:\s*["']nav\.routing["']/);
});

test("Startup no longer owns Routing workspaces or resources", () => {
  const startup = source("src/pages/Startup.tsx");
  const routing = source("src/pages/Routing.tsx");
  assert.doesNotMatch(startup, /RoutingProfiles|CompatibilityMatrix|\bCombos\b/);
  assert.doesNotMatch(startup, /readControlTab|selectControlTab|isRoutingDataTab/);
  assert.doesNotMatch(routing, /\/api\/startup-health|\/api\/settings|\/api\/windows-tray/);
});
