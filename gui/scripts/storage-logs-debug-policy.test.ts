import assert from "node:assert/strict";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import test from "node:test";

import {
  diagnosticsFromOxlintJson,
  summarizeStructuralDiagnostics,
} from "./check-structural-baseline.ts";
import {
  sanitizeLogEntryRouteDecision,
  validCachedLogs,
  validCachedRouteDecision,
} from "../src/pages/log-route-decision.ts";
import {
  cleanupPolicyBody,
  draftsFromPolicyResponse,
  interpretRunStartResponse,
  jobMatchesStart,
  localizedCatch,
  mapCleanupError,
  mapRestoreError,
  policyFieldsFromResponse,
  runOutcomeView,
  shouldApplyLoadedPolicy,
  STORAGE_POLICY_GB,
} from "../src/pages/storage-cleanup-policy.ts";
import { executeCleanupRun, pollCleanupJob } from "../src/pages/storage-cleanup-run.ts";
import {
  compactionSummary,
  logGuardRequest,
  mutationErrorLabel,
  performLogGuardAction,
  scopedForGeneration,
} from "../src/components/storage-workspace/log-guard-policy.ts";

const scriptDir = path.dirname(fileURLToPath(import.meta.url));
const guiRoot = path.resolve(scriptDir, "..");
const oxlintBin = path.join(guiRoot, "node_modules", "oxlint", "bin", "oxlint");
const configPath = path.join(guiRoot, ".oxlintrc.json");
const targetFiles = [
  path.join(guiRoot, "src", "pages", "log-route-decision.ts"),
  path.join(guiRoot, "src", "pages", "storage-cleanup-policy.ts"),
  path.join(guiRoot, "src", "pages", "storage-cleanup-run.ts"),
  path.join(guiRoot, "src", "pages", "logs-shared.ts"),
  path.join(guiRoot, "src", "pages", "diagnostics-contract.ts"),
  path.join(guiRoot, "src", "pages", "diagnostics-filters.tsx"),
  path.join(guiRoot, "src", "pages", "diagnostics-table.tsx"),
  path.join(guiRoot, "src", "pages", "diagnostics-detail.tsx"),
  path.join(guiRoot, "src", "pages", "use-diagnostics-workspace.ts"),
  path.join(guiRoot, "src", "pages", "Logs.tsx"),
  path.join(guiRoot, "src", "pages", "debug-logs.ts"),
  path.join(guiRoot, "src", "pages", "debug-shared.ts"),
  path.join(guiRoot, "src", "pages", "debug-settings-panel.tsx"),
  path.join(guiRoot, "src", "pages", "debug-settings-gate.tsx"),
  path.join(guiRoot, "src", "pages", "debug-log-viewer.tsx"),
  path.join(guiRoot, "src", "pages", "debug-claude-inbound-panel.tsx"),
  path.join(guiRoot, "src", "pages", "Debug.tsx"),
  path.join(guiRoot, "src", "pages", "Storage.tsx"),
  path.join(guiRoot, "src", "pages", "storage-archived-cleanup.tsx"),
  path.join(guiRoot, "src", "pages", "storage-quarantine-panel.tsx"),
  path.join(guiRoot, "src", "pages", "storage-policy-panel.tsx"),
  path.join(guiRoot, "src", "pages", "storage-policy-fields.tsx"),
  path.join(guiRoot, "src", "pages", "storage-cleanup-card.tsx"),
  path.join(guiRoot, "src", "pages", "storage-tab.ts"),
  path.join(guiRoot, "src", "pages", "storage-tab-strip.tsx"),
  path.join(guiRoot, "src", "pages", "storage-report-view.ts"),
  path.join(guiRoot, "src", "pages", "storage-trash-view.ts"),
  path.join(guiRoot, "src", "pages", "storage-board-panels.tsx"),
  path.join(guiRoot, "src", "pages", "use-storage-page.ts"),
  path.join(guiRoot, "src", "components", "storage-workspace", "storage-detail-pane.tsx"),
  path.join(guiRoot, "src", "components", "storage-workspace", "use-log-guard.ts"),
  path.join(guiRoot, "src", "components", "storage-workspace", "types.ts"),
  path.join(guiRoot, "src", "components", "storage-workspace", "log-guard-policy.ts"),
  path.join(guiRoot, "src", "components", "storage-workspace", "log-guard-panel.tsx"),
  path.join(guiRoot, "src", "components", "storage-workspace", "storage-workspace-sections.tsx"),
  path.join(guiRoot, "src", "components", "storage-workspace", "StorageWorkspace.tsx"),
];

function structuralDiagnostics() {
  const result = spawnSync(
    process.execPath,
    [oxlintBin, "-c", configPath, "--format=json", ...targetFiles],
    { cwd: guiRoot, encoding: "utf8" },
  );
  if (result.error) throw result.error;
  const stdout = String(result.stdout ?? "").trim();
  assert.ok(stdout, `Oxlint storage-logs-debug scan produced no output:\n${String(result.stderr ?? "")}`);
  return summarizeStructuralDiagnostics(diagnosticsFromOxlintJson(JSON.parse(stdout), guiRoot));
}

function t(key, vars = {}) {
  return vars && Object.keys(vars).length > 0 ? `${key}:${JSON.stringify(vars)}` : key;
}

function policy(overrides = {}) {
  return {
    enabled: true,
    trigger: { archivedBytesOver: 5 * STORAGE_POLICY_GB },
    target: { removeOldestPercent: 25 },
    schedule: "weekly",
    mode: "quarantine",
    ...overrides,
  };
}

test("storage, logs, and debug files contain no structural debt", () => {
  assert.deepEqual(structuralDiagnostics(), []);
});

test("validCachedRouteDecision preserves undefined, nested, and rejection contracts", () => {
  assert.equal(validCachedRouteDecision(undefined), true);
  assert.equal(validCachedRouteDecision(null), false);
  assert.equal(validCachedRouteDecision("x"), false);
  assert.equal(validCachedRouteDecision({}), true);
  assert.equal(validCachedRouteDecision({ routeKind: "combo" }), true);
  assert.equal(validCachedRouteDecision({ routeKind: 1 }), false);
  assert.equal(validCachedRouteDecision({ profile: null }), false);
  assert.equal(validCachedRouteDecision({ profile: { id: "a", revision: "1" } }), true);
  assert.equal(validCachedRouteDecision({ profile: { id: 1 } }), false);
  assert.equal(validCachedRouteDecision({ selected: null }), false);
  assert.equal(validCachedRouteDecision({ selected: { provider: "openai", model: "gpt", reason: "direct" } }), true);
  assert.equal(validCachedRouteDecision({ selected: { reason: 4 } }), false);
  assert.equal(validCachedRouteDecision({ candidates: "nope" }), false);
  assert.equal(validCachedRouteDecision({ candidates: [] }), true);
  assert.equal(validCachedRouteDecision({ candidates: [null] }), false);
  assert.equal(validCachedRouteDecision({ candidates: [{ provider: "a", model: "b", eligible: true }] }), true);
  assert.equal(validCachedRouteDecision({ candidates: [{ eligible: "yes" }] }), false);
  assert.equal(validCachedRouteDecision({ candidates: [{ exclusions: "x" }] }), false);
  assert.equal(validCachedRouteDecision({ candidates: [{ exclusions: [null] }] }), false);
  assert.equal(validCachedRouteDecision({ candidates: [{ exclusions: [{ code: 1 }] }] }), false);
  assert.equal(validCachedRouteDecision({
    routeKind: "combo",
    profile: { id: "p", revision: "r" },
    selected: { provider: "openai", model: "gpt", reason: "first" },
    candidates: [{ provider: "openai", model: "gpt", eligible: false, exclusions: [{ code: "quota" }] }],
  }), true);
});

test("session-cache log rows keep valid route decisions and drop invalid ones", () => {
  const base = { timestamp: 1, model: "gpt", provider: "openai", status: 200, durationMs: 12 };
  assert.equal(validCachedLogs(null), null);
  assert.equal(validCachedLogs({}), null);
  assert.deepEqual(validCachedLogs([base]), [base]);
  assert.equal(validCachedLogs([{ ...base, model: 1 }]), null);
  assert.equal(validCachedLogs([{ ...base, routeDecision: { selected: { model: 1 } } }]), null);
  const kept = { ...base, routeDecision: { routeKind: "direct" } };
  assert.equal(sanitizeLogEntryRouteDecision(kept), kept);
  assert.deepEqual(
    sanitizeLogEntryRouteDecision({ ...base, routeDecision: { profile: null } }),
    base,
  );
  assert.deepEqual(sanitizeLogEntryRouteDecision(base), base);
});

test("cleanup policy drafts and body preserve reduce/percent and reject blanks", () => {
  const reduce = draftsFromPolicyResponse(policy({
    target: { reduceToBytes: 4 * STORAGE_POLICY_GB },
    job: { status: "idle" },
  }));
  assert.equal(reduce.targetMode, "reduce");
  assert.equal(reduce.reduceGb, "4");
  assert.equal("job" in reduce.policy, false);
  assert.deepEqual(policyFieldsFromResponse(policy({ job: { status: "running" } })).job, undefined);

  const percent = draftsFromPolicyResponse(policy({ target: { removeOldestPercent: 80 } }));
  assert.equal(percent.targetMode, "percent");
  assert.equal(percent.percent, "80");

  const body = cleanupPolicyBody(percent.policy, percent);
  assert.deepEqual(body?.target, { removeOldestPercent: 80 });
  assert.equal(cleanupPolicyBody(null, percent), null);
  assert.equal(cleanupPolicyBody(percent.policy, { ...percent, thresholdGb: " " }), null);
  assert.equal(cleanupPolicyBody(percent.policy, { ...percent, percent: "0" }), null);
  assert.equal(cleanupPolicyBody(percent.policy, { ...percent, targetMode: "reduce", reduceGb: "" }), null);
  const reduceBody = cleanupPolicyBody(reduce.policy, reduce);
  assert.deepEqual(reduceBody?.target, { reduceToBytes: 4 * STORAGE_POLICY_GB });
});

test("run-now start, poll match, and outcome mapping stay aligned", () => {
  assert.equal(interpretRunStartResponse(409, { policy: policy() }).kind, "already_running");
  assert.equal(interpretRunStartResponse(500, { error: "already_running" }).kind, "already_running");
  assert.equal(interpretRunStartResponse(500, {}).kind, "run_failed");
  assert.equal(interpretRunStartResponse(200, { started: true }).kind, "run_failed");
  assert.deepEqual(
    interpretRunStartResponse(200, { started: true, job: { startedAt: 9 } }),
    { kind: "started", startedAt: 9, policy: undefined },
  );

  const outcome = { ok: true, lastOutcome: { ok: true, removed: 2, freedBytes: 10 } };
  assert.deepEqual(jobMatchesStart({ status: "running", startedAt: 1, lastOutcome: { ok: true } }, 1), undefined);
  assert.deepEqual(jobMatchesStart({ status: "idle", startedAt: 1, lastOutcome: outcome.lastOutcome }, 1), outcome.lastOutcome);
  assert.deepEqual(jobMatchesStart({ status: "idle", startedAt: 2, finishedAt: 8, lastOutcome: outcome.lastOutcome }, 7), outcome.lastOutcome);
  assert.equal(jobMatchesStart({ status: "idle", startedAt: 2, finishedAt: 6, lastOutcome: outcome.lastOutcome }, 7), undefined);

  assert.deepEqual(runOutcomeView({ ok: true, skipped: "disabled" }), { kind: "status", key: "storage.policy.skippedDisabled" });
  assert.deepEqual(runOutcomeView({ ok: true, skipped: "under_threshold" }), { kind: "status", key: "storage.policy.skippedUnder" });
  assert.deepEqual(runOutcomeView({ ok: true, skipped: "nothing_selected" }), { kind: "status", key: "storage.policy.skippedEmpty" });
  assert.deepEqual(runOutcomeView({ ok: false, deferred: "codex_busy" }), { kind: "error", key: "storage.cleanup.err.codex_busy" });
  assert.deepEqual(runOutcomeView({ ok: false, error: "codex_busy" }), { kind: "error", key: "storage.cleanup.err.codex_busy" });
  assert.deepEqual(runOutcomeView({ ok: false }), { kind: "error", key: "storage.policy.runFailed" });
  assert.deepEqual(runOutcomeView({ ok: true, mode: "permanent", removed: 3, freedBytes: 9 }), {
    kind: "done",
    mode: "permanent",
    removed: 3,
    freedBytes: 9,
  });
  assert.deepEqual(runOutcomeView({ ok: true }), {
    kind: "done",
    mode: "quarantine",
    removed: 0,
    freedBytes: 0,
  });
});

test("loaded policy refresh does not clobber drafts", () => {
  assert.equal(shouldApplyLoadedPolicy({
    aborted: false,
    generation: 2,
    currentGeneration: 2,
    dirty: true,
    editing: false,
  }), false);
  assert.equal(shouldApplyLoadedPolicy({
    aborted: false,
    generation: 2,
    currentGeneration: 2,
    dirty: false,
    editing: true,
  }), false);
  assert.equal(shouldApplyLoadedPolicy({
    aborted: true,
    generation: 2,
    currentGeneration: 2,
    dirty: false,
    editing: false,
  }), false);
  assert.equal(shouldApplyLoadedPolicy({
    aborted: false,
    generation: 1,
    currentGeneration: 2,
    dirty: false,
    editing: false,
  }), false);
  assert.equal(shouldApplyLoadedPolicy({
    aborted: false,
    generation: 2,
    currentGeneration: 2,
    dirty: false,
    editing: false,
  }), true);
});

test("cleanup and restore error maps plus localizedCatch keep fallbacks", () => {
  assert.equal(mapCleanupError(t, "stale_preview"), "storage.cleanup.err.stale_preview");
  assert.equal(mapCleanupError(t, "fs_failed", undefined, "/tmp/trash"), "storage.cleanup.err.fs_failed_trash:{\"trashDir\":\"/tmp/trash\"}");
  assert.equal(mapCleanupError(t, "fs_failed"), "storage.cleanup.err.fs_failed");
  assert.equal(mapCleanupError(t, "unknown", "fallback"), "fallback");
  assert.equal(mapRestoreError(t, "restore_worker_failed"), "storage.trash.err.restore_worker_failed");
  assert.equal(mapRestoreError(t, "restore_worker_failed", "detail"), "detail");
  assert.equal(mapRestoreError(t, "missing_trash"), "storage.trash.err.missing_trash");
  assert.equal(localizedCatch(new Error("Failed to fetch"), "fb"), "fb");
  assert.equal(localizedCatch(new Error("Unexpected end of"), "fb"), "fb");
  assert.equal(localizedCatch(new Error("real"), "fb"), "real");
  assert.equal(localizedCatch("x", "fb"), "fb");
});

test("cleanup run-now poll ignores failed ticks and keeps the matching outcome", async () => {
  const calls = [];
  const fetchImpl = async (url) => {
    calls.push(url);
    if (calls.length === 1) return new Response("nope", { status: 500 });
    return Response.json({
      ...policy(),
      job: { status: "idle", startedAt: 11, lastOutcome: { ok: true, removed: 4 } },
    });
  };
  let clock = 0;
  const polled = await pollCleanupJob({
    apiBase: "http://x",
    startedAt: 11,
    deadline: 10,
    signal: new AbortController().signal,
    sleep: async () => undefined,
    now: () => {
      clock += 1;
      return clock <= 2 ? 0 : 11;
    },
    fetchImpl,
  });
  assert.equal(polled.aborted, false);
  assert.equal(polled.outcome?.removed, 4);
  assert.equal(calls.length, 2);
});

test("cleanup run-now PUT-then-POST preserves already-running and save failure", async () => {
  const saveFail = await executeCleanupRun({
    apiBase: "http://x",
    body: policy(),
    signal: new AbortController().signal,
    sleep: async () => undefined,
    fetchImpl: async () => new Response("nope", { status: 500 }),
  });
  assert.equal(saveFail.kind, "save_failed");

  let step = 0;
  const running = await executeCleanupRun({
    apiBase: "http://x",
    body: policy(),
    signal: new AbortController().signal,
    sleep: async () => undefined,
    fetchImpl: async () => {
      step += 1;
      if (step === 1) return Response.json({ policy: policy() });
      return Response.json({ error: "already_running", policy: policy({ enabled: false }) }, { status: 409 });
    },
  });
  assert.equal(running.kind, "already_running");
});

test("log-guard mutation labels, request bodies, and compact summaries stay exact", async () => {
  assert.match(mutationErrorLabel("en", "auto_vacuum_not_incremental"), /incremental vacuum/i);
  assert.match(mutationErrorLabel("en", "integrity_check_failed"), /integrity/i);
  assert.match(mutationErrorLabel("en", "trigger_collision"), /./);
  assert.match(mutationErrorLabel("en", "mystery"), /Could not update Codex log storage/);
  assert.deepEqual(logGuardRequest({ action: "protect", mode: "quiet" }), {
    suffix: "protect",
    init: {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ mode: "quiet" }),
    },
  });
  assert.deepEqual(logGuardRequest({ action: "compact" }), { suffix: "compact", init: { method: "POST" } });
  const summary = compactionSummary({
    complete: false,
    stopReason: "page_budget",
    pagesReclaimed: 3,
    logicalBytesReclaimed: 1024,
    physicalDatabaseBytesReclaimed: 0,
  }, "en");
  assert.match(summary, /partial/i);
  assert.match(summary, /page_budget/);
  assert.match(summary, /3/);
  assert.equal(scopedForGeneration({ generation: 9, value: "ok" }, 9), "ok");
  assert.equal(scopedForGeneration({ generation: 8, value: "ok" }, 9), null);

  const compactNoRefresh = await performLogGuardAction({
    apiBase: "",
    action: { action: "compact" },
    locale: "en",
    fetchImpl: async (url) => {
      if (String(url).endsWith("/compact")) {
        return Response.json({ report: { complete: true, pagesReclaimed: 1, logicalBytesReclaimed: 1, physicalDatabaseBytesReclaimed: 1 } });
      }
      return new Response("down", { status: 500 });
    },
  });
  assert.equal(compactNoRefresh.kind, "compact");
  assert.match(String(compactNoRefresh.compaction), /complete/i);
});
