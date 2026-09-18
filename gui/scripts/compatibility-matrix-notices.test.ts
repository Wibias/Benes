import assert from "node:assert/strict";
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { compatibilityPageNotices } from "../src/pages/compatibility-matrix-view.ts";
import { catalogValue } from "../src/i18n/catalogs.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");

function localizeFetchError(e: unknown, fallback: string): string {
  if (!(e instanceof Error)) return fallback;
  const msg = e.message;
  if (msg === "Failed to fetch" || msg.includes("NetworkError") || msg.includes("network error")) {
    return fallback;
  }
  return msg || fallback;
}

const LOAD_FAILED = "Could not load compatibility data";
const STALE = "Could not refresh compatibility data. Showing the last available projection.";

function noticesFor(kind: string, error: unknown = new Error("Failed to fetch")) {
  return compatibilityPageNotices({
    surfaceKind: kind,
    surfaceError: error,
    localizeFetchError,
    loadFailedLabel: LOAD_FAILED,
    refreshFailedStaleLabel: STALE,
  });
}

test("cold failure maps to blocking loadFailed notice only", () => {
  const notices = noticesFor("failed-cold");
  assert.equal(notices.loadError, LOAD_FAILED);
  assert.equal(notices.staleRefreshWarning, null);
});

test("successful populated load clears both notices", () => {
  const notices = noticesFor("ready-populated");
  assert.equal(notices.loadError, null);
  assert.equal(notices.staleRefreshWarning, null);
});

test("failed refresh with stale data maps to non-blocking stale warning only", () => {
  const notices = noticesFor("failed-with-stale");
  assert.equal(notices.loadError, null);
  assert.equal(notices.staleRefreshWarning, STALE);
  assert.notEqual(notices.staleRefreshWarning, LOAD_FAILED);
});

test("subsequent successful refresh clears stale warning (poll and manual refresh share kind)", () => {
  assert.equal(noticesFor("failed-with-stale").staleRefreshWarning, STALE);
  // Quiet poll and manual Refresh both settle through classifyDataSurface kinds;
  // in-flight refresh must not keep claiming a cold load failure.
  const mid = noticesFor("loading-with-stale-data");
  assert.equal(mid.loadError, null);
  assert.equal(mid.staleRefreshWarning, null);
  const after = noticesFor("ready-populated");
  assert.equal(after.loadError, null);
  assert.equal(after.staleRefreshWarning, null);
});

test("CompatibilityMatrix maps DataSurface kind via compatibilityPageNotices", () => {
  const src = fs.readFileSync(path.join(guiRoot, "src", "pages", "CompatibilityMatrix.tsx"), "utf8");
  assert.match(src, /compatibilityPageNotices\(/);
  assert.match(src, /surfaceKind=\{surface\.state\.kind\}/);
  assert.match(src, /lab\.refreshFailedStale/);
  assert.doesNotMatch(src, /showError=\{surface\.state\.showError\}/);
});

test("Compatibility notices render cold alert vs stale warn status", () => {
  const src = fs.readFileSync(path.join(guiRoot, "src", "pages", "compatibility-matrix-sections.tsx"), "utf8");
  assert.match(src, /staleRefreshWarning && !loadError/);
  assert.match(src, /tone="warn" role="status"/);
  assert.match(src, /loadError && <Notice tone="err" role="alert"/);
});

test("classifyDataSurface keeps failed-with-stale distinct from failed-cold", () => {
  const src = fs.readFileSync(path.join(guiRoot, "src", "data-surface.ts"), "utf8");
  assert.match(src, /kind: "failed-with-stale"/);
  assert.match(src, /kind: "failed-cold"/);
});

test("canonical lab catalog exposes refreshFailedStale overlay", () => {
  assert.equal(catalogValue("en", "lab.refreshFailedStale"), STALE);
  assert.match(catalogValue("de", "lab.refreshFailedStale"), /aktualisiert|Projektion/i);
});
