import { newHistoryId, newSnapshotId } from "./catalog";
import type { HarnessHistoryRow, HarnessRecord, HarnessSettings } from "./types";

function nowIso(): string {
  return new Date().toISOString();
}

function prependHistory(harness: HarnessRecord, row: Omit<HarnessHistoryRow, "id">): HarnessRecord {
  return {
    ...harness,
    history: [{ id: newHistoryId(), ...row }, ...harness.history].slice(0, 20),
  };
}

export function applyHarness(harness: HarnessRecord): HarnessRecord {
  if (!harness.installed || harness.issue === "conflict") return harness;
  const snapshot = newSnapshotId();
  const at = nowIso();
  return prependHistory({
    ...harness,
    applied: true,
    issue: "none",
    drift: false,
    snapshotId: snapshot,
    lastAppliedAt: at,
  }, {
    kind: "apply",
    snapshot,
    at,
    noteKey: "harnesses.note.userApply",
    restorable: true,
  });
}

export function disableHarness(harness: HarnessRecord): HarnessRecord {
  if (!harness.applied || harness.issue === "conflict") return harness;
  const at = nowIso();
  return prependHistory({
    ...harness,
    applied: false,
    drift: false,
  }, {
    kind: "disable",
    snapshot: harness.snapshotId,
    at,
    noteKey: "harnesses.note.userDisable",
    restorable: Boolean(harness.snapshotId),
  });
}

export function refreshHarness(harness: HarnessRecord): HarnessRecord {
  if (!harness.applied) return harness;
  const snapshot = newSnapshotId();
  const at = nowIso();
  return prependHistory({
    ...harness,
    issue: "none",
    drift: false,
    snapshotId: snapshot,
    lastAppliedAt: at,
  }, {
    kind: "refresh",
    snapshot,
    at,
    noteKey: "harnesses.note.refresh",
    restorable: true,
  });
}

export function restoreHarness(harness: HarnessRecord, snapshot: string): HarnessRecord {
  const at = nowIso();
  return prependHistory({
    ...harness,
    applied: true,
    installed: true,
    issue: "none",
    drift: false,
    snapshotId: snapshot,
    lastAppliedAt: at,
  }, {
    kind: "restore",
    snapshot,
    at,
    noteKey: "harnesses.note.restore",
    restorable: true,
  });
}

export function patchSettings(harness: HarnessRecord, patch: Partial<HarnessSettings>): HarnessRecord {
  return { ...harness, settings: { ...harness.settings, ...patch } };
}

export function stampDetected(harness: HarnessRecord): HarnessRecord {
  return { ...harness, lastDetectedAt: nowIso() };
}
