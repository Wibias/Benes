import assert from "node:assert/strict";
import { readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import { hashBelongsToPage, resolveAppHashChange } from "../src/app-routing.ts";
import {
  ALL_STORAGE_KEY,
  bucketsBySize,
  cleanupRefusedByTruncation,
  largestAcrossBuckets,
  resolvedPhysicalBytes,
  scanCompleteness,
  storageBoardChrome,
  storageSummary,
  timestampDateTimeDisplay,
  timestampDisplay,
  usableTimestamp,
} from "../src/pages/storage-report-view.ts";
import {
  readStorageTab,
  storageHashIsAllowed,
  storageTabHash,
} from "../src/pages/storage-tab.ts";
import {
  entryCreatedAt,
  quarantineChromeKind,
  recoveryReasonLabel,
  trashHasContent,
  trashListFailureMessage,
  trashRows,
} from "../src/pages/storage-trash-view.ts";
import {
  bindManualCleanupPreview,
  clampManualCleanupPercent,
  isTruncatedCleanupRefusal,
  MANUAL_CLEANUP_PERCENT_MAX,
  MANUAL_CLEANUP_PERCENT_MIN,
  manualCleanupPreviewReady,
  manualCleanupSelectValue,
  mapCleanupError,
  parseManualCleanupPercent,
} from "../src/pages/storage-cleanup-policy.ts";

const guiRoot = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");

function readSrc(...parts) {
  return readFileSync(path.join(guiRoot, ...parts), "utf8");
}

function t(key, vars = {}) {
  return vars && Object.keys(vars).length > 0 ? `${key}:${JSON.stringify(vars)}` : key;
}

const report = {
  codexHome: "C:\\\\codex",
  generatedAt: 1,
  total: { bytes: 1200, fileCount: 4, physicalBytes: 900 },
  truncated: false,
  buckets: [
    { key: "sessions", label: "Sessions", bytes: 500, fileCount: 2, largest: [{ path: "sessions/a.json", bytes: 400 }] },
    { key: "archived_sessions", label: "Archived", bytes: 700, fileCount: 2, physicalBytes: 400, largest: [{ path: "archived_sessions/b.json", bytes: 300 }] },
    { key: "logs_db", label: "Logs", bytes: 0, fileCount: 0 },
  ],
};

test("storage hashes allow only Overview, Cleanup, and Quarantine", () => {
  assert.equal(storageTabHash("overview"), "storage");
  assert.equal(storageTabHash("cleanup"), "storage/cleanup");
  assert.equal(storageTabHash("quarantine"), "storage/quarantine");
  assert.equal(readStorageTab("#storage"), "overview");
  assert.equal(readStorageTab("#storage/cleanup"), "cleanup");
  assert.equal(readStorageTab("#storage/quarantine"), "quarantine");
  assert.equal(storageHashIsAllowed("storage", new URLSearchParams()), true);
  assert.equal(storageHashIsAllowed("storage/cleanup", new URLSearchParams()), true);
  assert.equal(storageHashIsAllowed("storage/quarantine", new URLSearchParams()), true);
  assert.equal(storageHashIsAllowed("storage/foo", new URLSearchParams()), false);
  assert.equal(storageHashIsAllowed("storage/cleanup/foo", new URLSearchParams()), false);
  assert.equal(hashBelongsToPage("storage", "storage"), true);
  assert.equal(hashBelongsToPage("storage/cleanup", "storage"), true);
  assert.equal(hashBelongsToPage("storage/quarantine", "storage"), true);
  assert.deepEqual(resolveAppHashChange("storage/foo"), { page: "storage", replaceTo: "storage" });
  assert.deepEqual(resolveAppHashChange("storage/cleanup/foo"), { page: "storage", replaceTo: "storage" });
  assert.deepEqual(resolveAppHashChange("storage/cleanup"), { page: "storage", replaceTo: null });
});

test("summary metrics use physical bytes when present and logical when omitted", () => {
  const summary = storageSummary(report);
  assert.equal(summary.totalBytes, 1200);
  assert.equal(summary.physicalBytes, 900);
  assert.equal(summary.fileCount, 4);
  assert.equal(resolvedPhysicalBytes(500, 400), 400);
  assert.equal(resolvedPhysicalBytes(500, undefined), 500);
  assert.equal(resolvedPhysicalBytes(undefined, undefined), undefined);
  assert.equal(scanCompleteness(undefined), "complete");
  assert.equal(scanCompleteness(false), "complete");
  assert.equal(scanCompleteness(true), "partial");
  assert.equal(cleanupRefusedByTruncation(true), true);
  assert.equal(cleanupRefusedByTruncation(false), false);
});

test("All storage is a synthetic rail key and buckets stay size-sorted", () => {
  assert.equal(ALL_STORAGE_KEY, "all");
  assert.deepEqual(bucketsBySize(report.buckets).map(b => b.key), ["archived_sessions", "sessions", "logs_db"]);
  const largest = largestAcrossBuckets(report.buckets, 10);
  assert.equal(largest[0].path, "sessions/a.json");
  assert.equal(largest[0].bucketKey, "sessions");
});

test("shared timestamp formatter only treats missing or non-finite as unavailable", () => {
  assert.equal(usableTimestamp(undefined), undefined);
  assert.equal(usableTimestamp(null), undefined);
  assert.equal(usableTimestamp(Number.NaN), undefined);
  assert.equal(usableTimestamp(Number.POSITIVE_INFINITY), undefined);
  assert.equal(timestampDisplay(undefined, "en-US"), "—");
  assert.equal(timestampDisplay(null, "en-US"), "—");
  assert.equal(timestampDisplay(Number.NaN, "en-US"), "—");
  assert.equal(usableTimestamp(0), 0);
  assert.equal(usableTimestamp(-1), -1);
  assert.notEqual(timestampDisplay(0, "en-US"), "—");
  assert.notEqual(timestampDisplay(-1, "en-US"), "—");
  const pre1970 = Date.UTC(1969, 6, 20, 12);
  assert.equal(usableTimestamp(pre1970), pre1970);
  assert.notEqual(timestampDisplay(pre1970, "en-US"), "—");
  assert.match(timestampDisplay(pre1970, "en-US"), /1969/);
  const y1999 = Date.UTC(1999, 5, 15, 12);
  assert.equal(usableTimestamp(y1999), y1999);
  assert.match(timestampDisplay(y1999, "en-US"), /1999/);
  const reportView = readSrc("src", "pages", "storage-report-view.ts");
  assert.equal(reportView.includes("ms <= 0"), false);
  assert.equal(reportView.includes("DISPLAYABLE_TIMESTAMP_MIN_MS"), false);
  assert.equal(reportView.includes("Date.UTC(2001"), false);
  const sections = readSrc("src", "components", "storage-workspace", "storage-workspace-sections.tsx");
  assert.match(sections, /timestampDisplay\(oldest/);
  assert.match(sections, /timestampDisplay\(newest/);
});

test("quarantine missing or invalid created time still renders a dash", () => {
  assert.equal(entryCreatedAt({ id: "x", epoch: "", fileCount: 0, bytes: 0 }), undefined);
  assert.equal(entryCreatedAt({ id: "x", epoch: "0-aa", fileCount: 0, bytes: 0, quarantinedAt: 0 }), undefined);
  assert.equal(entryCreatedAt({ id: "x", epoch: "bad", fileCount: 0, bytes: 0, quarantinedAt: Number.NaN }), undefined);
  assert.equal(timestampDateTimeDisplay(undefined, "en-US"), "—");
  assert.equal(timestampDateTimeDisplay(entryCreatedAt({ id: "x", epoch: "nope", fileCount: 0, bytes: 0 }), "en-US"), "—");
  const recovery = trashRows({
    entries: [],
    recoveryNeeded: [{ status: "unreadable", error: "fs_failed" }],
  })[0];
  assert.equal(recovery.createdAt, undefined);
  assert.equal(timestampDateTimeDisplay(recovery.createdAt, "en-US"), "—");
  const trash = readSrc("src", "pages", "storage-trash-view.ts");
  assert.match(trash, /entry.quarantinedAt > 0/);
});

test("quarantine chrome never pairs a list error with the empty copy", () => {
  assert.equal(quarantineChromeKind({ showSkeleton: false, showError: true, rowCount: 0 }), "error");
  assert.equal(quarantineChromeKind({ showSkeleton: false, showError: false, rowCount: 0 }), "empty");
  assert.equal(quarantineChromeKind({ showSkeleton: false, showError: false, rowCount: 2 }), "split");
  assert.equal(quarantineChromeKind({ showSkeleton: true, showError: true, rowCount: 0 }), "loading");
  assert.equal(trashListFailureMessage({ error: "invalid_trash" }, "fallback"), "invalid_trash");
  const quarantine = readSrc("src", "pages", "storage-quarantine-panel.tsx");
  assert.match(quarantine, /storage-quarantine--solo/);
  assert.match(quarantine, /common.retry/);
  assert.match(quarantine, /chrome === "empty"/);
  assert.equal(quarantine.includes("if (!selected) return <p className=\"stw-empty\">"), false);
});

test("quarantine rows keep recoveryNeeded visible", () => {
  const rows = trashRows({
    entries: [
      { id: "1-aa", epoch: "1-aa", fileCount: 2, bytes: 10, mode: "quarantine", quarantinedAt: 10 },
      { id: "2-bb", epoch: "2-bb", fileCount: 1, bytes: 4, mode: "permanent", partial: true },
    ],
    recoveryNeeded: [{ id: "3-cc", status: "incomplete", error: "invalid_trash" }],
  });
  assert.equal(rows.length, 3);
  assert.equal(rows[0].status, "ready");
  assert.equal(rows[1].status, "partial");
  assert.equal(rows[2].status, "recovery_needed");
  assert.equal(trashHasContent({ entries: [], recoveryNeeded: [{ status: "unreadable", error: "fs_failed" }] }), true);
  assert.equal(trashHasContent({ entries: [], recoveryNeeded: [] }), false);
  const recovery = trashRows({
    entries: [],
    recoveryNeeded: [{ id: "1788300000000-deadbeef", status: "incomplete", error: "invalid_trash" }],
  })[0];
  assert.equal(recovery.kind, "recovery");
  assert.equal(recovery.recoveryId, "1788300000000-deadbeef");
  assert.equal(recovery.fileCount, undefined);
  assert.equal(recoveryReasonLabel(t, "invalid_trash"), "storage.trash.recovery.reason.invalid");
  assert.equal(recoveryReasonLabel(t, "C:\\\\tmp\\\\.trash\\\\x"), "storage.trash.recovery.reason.unreadable");
  assert.equal(recoveryReasonLabel(t, undefined, "unreadable"), "storage.trash.recovery.reason.unreadable");
});

test("truncated cleanup refusal stays mapped to the truncated copy", () => {
  assert.equal(isTruncatedCleanupRefusal("CODEX_HOME is too large to preview safely"), true);
  assert.equal(mapCleanupError(t, "cleanup_failed", "fallback", undefined, "CODEX_HOME is too large to clean safely"), "storage.cleanup.err.truncated");
});

test("Overview is a persistent rail/detail board without Back or Scan-status KPI", () => {
  const workspace = readSrc("src", "components", "storage-workspace", "StorageWorkspace.tsx");
  const detail = readSrc("src", "components", "storage-workspace", "storage-detail-pane.tsx");
  const sections = readSrc("src", "components", "storage-workspace", "storage-workspace-sections.tsx");
  const page = readSrc("src", "pages", "Storage.tsx");
  const styles = readSrc("src", "styles-storage-workspace.css");
  assert.match(workspace, /ALL_STORAGE_KEY/);
  assert.match(detail, /logs_db/);
  assert.match(detail, /embedded/);
  assert.equal(workspace.includes("onBack"), false);
  assert.equal(detail.includes("onBack"), false);
  assert.equal(workspace.includes("modal.back"), false);
  assert.match(sections, /storage.card.physical/);
  assert.equal(sections.includes("storage.card.home"), false);
  assert.equal(page.includes("storage.subtitle"), false);
  assert.equal(page.includes("Scan status"), false);
  assert.match(page, /storage.scan.partial/);
  assert.match(readSrc("src", "pages", "storage-tab.ts"), /storage\/cleanup/);
  assert.equal(styles.includes("stw-summary-card"), false);
  assert.equal(styles.includes("storage-cleanup-card"), false);
  assert.match(styles, /box-shadow: inset 2px 0 0 var\(--blue\)/);
});

test("Log Guard lives in Logs DB detail, not the generic overview", () => {
  const workspace = readSrc("src", "components", "storage-workspace", "StorageWorkspace.tsx");
  const detail = readSrc("src", "components", "storage-workspace", "storage-detail-pane.tsx");
  const panel = readSrc("src", "components", "storage-workspace", "log-guard-panel.tsx");
  assert.match(detail, /CodexLogGuardPanel/);
  assert.match(detail, /logs_db/);
  assert.equal(workspace.includes("CodexLogGuardPanel"), false);
  assert.match(workspace, /StorageDetailPane/);
  assert.match(panel, /storage.logGuard.configured/);
  assert.match(panel, /storage.logGuard.observed/);
  assert.match(panel, /desiredMode/);
  assert.match(panel, /observedMode/);
  assert.match(panel, /stw-hint--quiet/);
});

test("Cleanup owns policy plus run-now and refuses truncated scans", () => {
  const cleanup = readSrc("src", "pages", "storage-cleanup-card.tsx");
  const archived = readSrc("src", "pages", "storage-archived-cleanup.tsx");
  const policy = readSrc("src", "pages", "storage-policy-panel.tsx");
  const policyFields = readSrc("src", "pages", "storage-policy-fields.tsx");
  assert.match(cleanup, /AutoCleanupPolicyPanel/);
  assert.match(cleanup, /ArchivedCleanupPanel/);
  assert.match(cleanup, /truncated/);
  assert.equal(cleanup.includes("QuarantineTrashPanel"), false);
  assert.match(archived, /cleanup\/preview/);
  assert.match(archived, /digest: preview.digest/);
  assert.match(archived, /storage.cleanup.permanentWarn/);
  assert.match(archived, /oldestPercent/);
  assert.match(archived, /storageGeneration/);
  assert.match(archived, /manualCleanupPreviewReady/);
  assert.equal(archived.includes('type="range"'), false);
  assert.match(archived, /storage.cleanup.previewAgain/);
  assert.match(archived, /storage.cleanup.custom/);
  assert.match(archived, /NumberStepper/);
  assert.match(policy, /storage.policy.loadFailed/);
  assert.match(policy, /common.retry/);
  assert.match(policyFields, /reduceToBytes|targetReduce/);
  assert.equal(archived.includes("reduceToBytes"), false);
});

test("successful one-off cleanup discards the old digest until a new generation previews", () => {
  const preview = {
    percent: 25,
    count: 4,
    bytes: 40,
    digest: "old-digest",
    candidates: [],
  };
  let hold = bindManualCleanupPreview(preview, 1000);
  assert.equal(manualCleanupPreviewReady(hold, 25, 1000), true);
  hold = null;
  assert.equal(manualCleanupPreviewReady(hold, 25, 1000), false);
  const stale = bindManualCleanupPreview(preview, 1000);
  assert.equal(manualCleanupPreviewReady(stale, 25, 2000), false);
  const fresh = bindManualCleanupPreview({ ...preview, digest: "new-digest" }, 2000);
  assert.equal(manualCleanupPreviewReady(fresh, 25, 2000), true);
  assert.notEqual(fresh.preview.digest, "old-digest");
  const archived = readSrc("src", "pages", "storage-archived-cleanup.tsx");
  assert.match(archived, /storageGeneration, t/);
  assert.match(archived, /closeConfirm\(true\)/);
  assert.match(archived, /onDone\(\)/);
  assert.match(archived, /setHold\(null\)/);
  assert.match(archived, /bindManualCleanupPreview\(json, generation\)/);
});

test("manual cleanup preview and execute accept the full 1-100 percent range", () => {
  assert.equal(MANUAL_CLEANUP_PERCENT_MIN, 1);
  assert.equal(MANUAL_CLEANUP_PERCENT_MAX, 100);
  assert.equal(parseManualCleanupPercent("1"), 1);
  assert.equal(parseManualCleanupPercent("37"), 37);
  assert.equal(parseManualCleanupPercent("100"), 100);
  assert.equal(parseManualCleanupPercent("0"), undefined);
  assert.equal(parseManualCleanupPercent("101"), undefined);
  assert.equal(clampManualCleanupPercent(0), 1);
  assert.equal(clampManualCleanupPercent(101), 100);
  assert.equal(manualCleanupSelectValue(25, false), "25");
  assert.equal(manualCleanupSelectValue(37, false), "custom");
  assert.equal(manualCleanupSelectValue(1, false), "custom");
  assert.equal(manualCleanupSelectValue(100, false), "custom");
  for (const percent of [1, 37, 100]) {
    const hold = bindManualCleanupPreview({
      percent,
      count: 1,
      bytes: 8,
      digest: `digest-${percent}`,
      candidates: [],
    }, 9);
    assert.equal(manualCleanupPreviewReady(hold, percent, 9), true);
  }
});

test("scan_failed is a visible scan error with Retry on Overview and Cleanup", () => {
  assert.equal(storageBoardChrome({ showSkeleton: true, hasData: false, reportFailed: false }), "loading");
  assert.equal(storageBoardChrome({ showSkeleton: false, hasData: true, reportFailed: true }), "scan_error");
  assert.equal(storageBoardChrome({ showSkeleton: false, hasData: true, reportFailed: false }), "ready");
  assert.equal(storageBoardChrome({ showSkeleton: true, hasData: true, reportFailed: false }), "ready");
  const panels = readSrc("src", "pages", "storage-board-panels.tsx");
  const page = readSrc("src", "pages", "Storage.tsx");
  assert.match(panels, /storageBoardChrome/);
  assert.match(panels, /common.retry/);
  assert.match(panels, /StorageCleanupPanel/);
  assert.match(panels, /showSkeleton/);
  assert.match(panels, /reportFailed/);
  assert.match(page, /onRetry=\{page.refreshReport\}/);
  assert.equal(panels.includes("storage.empty.files"), false);
  const overviewFn = panels.slice(panels.indexOf("export function StorageOverviewPanel"), panels.indexOf("export function StorageCleanupPanel"));
  const cleanupFn = panels.slice(panels.indexOf("export function StorageCleanupPanel"), panels.indexOf("export function StorageQuarantinePanel"));
  assert.match(overviewFn, /StorageReportIssue/);
  assert.match(cleanupFn, /StorageReportIssue/);
  assert.match(cleanupFn, /storageGeneration=\{data.generatedAt\}/);
  const quarantineFn = panels.slice(panels.indexOf("export function StorageQuarantinePanel"));
  assert.equal(quarantineFn.includes("StorageReportIssue"), false);
});

test("Quarantine restore copy never claims a fallback archive location", () => {
  const quarantine = readSrc("src", "pages", "storage-quarantine-panel.tsx");
  const en = readSrc("src", "i18n", "en.ts");
  assert.match(quarantine, /recovery_needed/);
  assert.match(quarantine, /QuarantineRecoveryDetail/);
  assert.match(quarantine, /storage.trash.col.entryId/);
  assert.match(quarantine, /storage.trash.restoreHelp/);
  assert.match(quarantine, /storage-action/);
  const recoveryFn = quarantine.slice(quarantine.indexOf("function QuarantineRecoveryDetail"), quarantine.indexOf("function QuarantineDetail"));
  assert.equal(recoveryFn.includes("dashOrCount"), false);
  assert.equal(recoveryFn.includes("storage.col.files"), false);
  assert.equal(recoveryFn.includes("storage.col.created"), false);
  assert.match(en, /original archived-session paths/);
  assert.match(en, /reviewed and restored here/);
  assert.match(en, /"storage.trash.mode.quarantine": "Quarantine"/);
  assert.match(en, /"storage.trash.mode.permanent": "Permanent"/);
  assert.match(en, /"storage.policy.title": "Automatic cleanup"/);
  assert.match(en, /"storage.policy.runNow": "Run policy now"/);
  assert.equal(en.includes("standard archive location"), false);
  assert.equal(en.includes("permanent (incomplete)"), false);
  assert.equal(en.includes("CODEX_HOME/.trash"), false);
});
