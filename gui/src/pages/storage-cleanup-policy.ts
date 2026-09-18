/** Benes dashboard client for the Go proxy (`internal/server`). */
/**
 * Pure decode and draft/body mapping for the Storage cleanup policy.
 *
 * Everything here is a value in and a value out: the drafts the form edits, the body the listener
 * accepts, and the copy each refusal maps to. Responses are snapshots, so their shapes are
 * read-only.
 */

import type { TFn, TKey } from "../i18n/shared";

export const STORAGE_POLICY_GB = 1024 ** 3;

export const MANUAL_CLEANUP_PERCENT_MIN = 1;
export const MANUAL_CLEANUP_PERCENT_MAX = 100;
export const MANUAL_CLEANUP_PERCENT_PRESETS = [10, 25, 50] as const;
export const MANUAL_CLEANUP_PERCENTS = MANUAL_CLEANUP_PERCENT_PRESETS;
export const MANUAL_CLEANUP_CUSTOM_VALUE = "custom";

export interface CleanupPreview {
  readonly percent: number;
  readonly count: number;
  readonly bytes: number;
  readonly digest: string;
  readonly candidates: ReadonlyArray<{ readonly relPath: string; readonly bytes: number; readonly physicalRelPaths?: readonly string[] }>;
  readonly truncated?: boolean;
}

export type ManualCleanupPreviewHold = {
  readonly preview: CleanupPreview;
  readonly generation: number;
};

/** One whole percent inside the range the listener accepts, or `undefined` for anything else. */
export function parseManualCleanupPercent(raw: string): number | undefined {
  const parsed = Number(raw.trim());
  if (raw.trim() === "" || !Number.isFinite(parsed)) return undefined;
  const rounded = Math.round(parsed);
  if (rounded < MANUAL_CLEANUP_PERCENT_MIN || rounded > MANUAL_CLEANUP_PERCENT_MAX) return undefined;
  return rounded;
}

/** The nearest whole percent inside the range, for a value that may be unusable. */
export function clampManualCleanupPercent(value: number): number {
  if (!Number.isFinite(value)) return MANUAL_CLEANUP_PERCENT_MIN;
  return Math.min(
    MANUAL_CLEANUP_PERCENT_MAX,
    Math.max(MANUAL_CLEANUP_PERCENT_MIN, Math.round(value)),
  );
}

export function isManualCleanupPreset(percent: number): boolean {
  return (MANUAL_CLEANUP_PERCENT_PRESETS as readonly number[]).includes(percent);
}

export function manualCleanupSelectValue(percent: number, customMode: boolean): string {
  if (customMode || !isManualCleanupPreset(percent)) return MANUAL_CLEANUP_CUSTOM_VALUE;
  return String(percent);
}

export function bindManualCleanupPreview(
  preview: CleanupPreview,
  generation: number,
): ManualCleanupPreviewHold {
  return { preview, generation };
}

/** A preview may be executed only while it still describes the scan on screen and a percent. */
export function manualCleanupPreviewReady(
  hold: ManualCleanupPreviewHold | null,
  percent: number,
  storageGeneration: number | undefined,
): boolean {
  if (!hold || storageGeneration === undefined) return false;
  if (hold.generation !== storageGeneration) return false;
  if (hold.preview.percent !== percent) return false;
  return hold.preview.digest !== "";
}

export interface CleanupResult {
  readonly ok: boolean;
  readonly mode: "quarantine" | "permanent";
  readonly count: number;
  readonly bytes: number;
  readonly freedBytes?: number;
  readonly removedArchivedBytes?: number;
  readonly trashDir?: string;
  readonly error?: string;
  readonly message?: string;
  readonly partial?: boolean;
}

export interface TrashEntry {
  readonly id: string;
  readonly epoch: string;
  readonly fileCount: number;
  readonly bytes: number;
  readonly quarantinedAt?: number;
  readonly mode?: "quarantine" | "permanent";
  readonly partial?: boolean;
}

export interface TrashRecovery {
  readonly id?: string;
  readonly status: string;
  readonly error: string;
}

export interface TrashList {
  readonly entries: TrashEntry[];
  readonly recoveryNeeded?: TrashRecovery[];
}

export interface RestoreResult {
  readonly ok: boolean;
  readonly count: number;
  readonly bytes: number;
  readonly alreadyRestored?: number;
  readonly totalCount?: number;
  readonly totalBytes?: number;
  readonly trashDir?: string;
  readonly error?: string;
  readonly message?: string;
  readonly partial?: boolean;
}

/** The job block the listener reports beside a policy; the board shows it, it does not edit it. */
export interface CleanupJob {
  readonly status: "idle" | "running";
  readonly reason?: string;
  readonly startedAt?: number;
  readonly finishedAt?: number;
  readonly lastError?: string;
  readonly lastOutcome?: CleanupJobOutcome;
}

export interface CleanupPolicy {
  readonly enabled: boolean;
  readonly trigger: { readonly archivedBytesOver: number };
  readonly target: { readonly reduceToBytes?: number; readonly removeOldestPercent?: number };
  readonly schedule: "startup" | "daily" | "weekly" | "manual";
  readonly mode: "quarantine" | "permanent";
  readonly lastRun?: { readonly at: number; readonly freedBytes: number; readonly removed: number };
  readonly nextRun?: number;
  readonly job?: CleanupJob;
}

export interface CleanupJobOutcome {
  readonly ok: boolean;
  readonly skipped?: string;
  readonly deferred?: string;
  readonly error?: string;
  readonly mode?: string;
  readonly freedBytes?: number;
  readonly removed?: number;
}

export type CleanupTargetMode = "percent" | "reduce";

export interface CleanupPolicyDrafts {
  readonly thresholdGb: string;
  readonly targetMode: CleanupTargetMode;
  readonly percent: string;
  readonly reduceGb: string;
}

export type CachedCleanupPolicy = CleanupPolicyDrafts & { readonly policy: CleanupPolicy };

/** The policy without its job block: the job is state the listener owns, not configuration. */
export function policyFieldsFromResponse(json: CleanupPolicy): CleanupPolicy {
  const policy = { ...json };
  delete policy.job;
  return policy;
}

/** GiB as a field draft, rounded to two decimals and never negative. */
function gbDraft(bytes: number): string {
  const gib = Math.max(0, bytes / STORAGE_POLICY_GB);
  return String(Math.round(gib * 100) / 100);
}

export function draftsFromPolicyResponse(json: CleanupPolicy): CachedCleanupPolicy {
  const policy = policyFieldsFromResponse(json);
  const thresholdGb = gbDraft(json.trigger.archivedBytesOver);
  const reduceToBytes = json.target.reduceToBytes;
  if (reduceToBytes !== undefined) {
    return {
      policy,
      thresholdGb,
      targetMode: "reduce",
      percent: "25",
      reduceGb: gbDraft(reduceToBytes),
    };
  }
  const removeOldest = json.target.removeOldestPercent ?? 25;
  return {
    policy,
    thresholdGb,
    targetMode: "percent",
    percent: String(Math.min(100, Math.max(1, Math.floor(removeOldest)))),
    reduceGb: "4",
  };
}

/** A reduce-to target, or `null` when the draft is not a usable size. */
function reduceTargetFrom(raw: string): CleanupPolicy["target"] | null {
  const trimmed = raw.trim();
  if (trimmed === "") return null;
  const reduce = Number(trimmed);
  if (!Number.isFinite(reduce) || reduce < 0) return null;
  return { reduceToBytes: Math.floor(reduce * STORAGE_POLICY_GB) };
}

/** A remove-oldest target, or `null` when the draft is not a usable percentage. */
function percentTargetFrom(raw: string): CleanupPolicy["target"] | null {
  const percent = Number(raw);
  if (!Number.isFinite(percent) || percent < 1 || percent > 100) return null;
  return { removeOldestPercent: Math.min(100, Math.max(1, Math.floor(percent))) };
}

/** The target the current draft asks for, whichever of the two modes it names. */
function policyTargetFromDrafts(drafts: CleanupPolicyDrafts): CleanupPolicy["target"] | null {
  return drafts.targetMode === "reduce"
    ? reduceTargetFrom(drafts.reduceGb)
    : percentTargetFrom(drafts.percent);
}

/** The body a save or a run sends, or `null` while a draft is not yet usable. */
export function cleanupPolicyBody(
  policy: CleanupPolicy | null,
  drafts: CleanupPolicyDrafts,
): CleanupPolicy | null {
  if (!policy) return null;
  const thresholdRaw = drafts.thresholdGb.trim();
  if (thresholdRaw === "") return null;
  const threshold = Number(thresholdRaw);
  if (!Number.isFinite(threshold) || threshold < 0) return null;
  const target = policyTargetFromDrafts(drafts);
  if (!target) return null;
  return {
    enabled: policy.enabled,
    trigger: { archivedBytesOver: Math.floor(threshold * STORAGE_POLICY_GB) },
    target,
    schedule: policy.schedule,
    mode: policy.mode,
  };
}

export type RunOutcomeView =
  | { kind: "status"; key: "storage.policy.skippedDisabled" | "storage.policy.skippedUnder" | "storage.policy.skippedEmpty" }
  | { kind: "error"; key: "storage.cleanup.err.codex_busy" | "storage.policy.runFailed" }
  | { kind: "done"; mode: "permanent" | "quarantine"; removed: number; freedBytes: number };

/** The line a run reports when it decided there was nothing to clean. */
const SKIPPED_COPY = new Map<string, "storage.policy.skippedDisabled" | "storage.policy.skippedUnder" | "storage.policy.skippedEmpty">([
  ["disabled", "storage.policy.skippedDisabled"],
  ["under_threshold", "storage.policy.skippedUnder"],
  ["nothing_selected", "storage.policy.skippedEmpty"],
]);

export function runOutcomeView(outcome: CleanupJobOutcome): RunOutcomeView {
  const skipped = outcome.skipped === undefined ? undefined : SKIPPED_COPY.get(outcome.skipped);
  if (skipped !== undefined) return { kind: "status", key: skipped };
  if (outcome.deferred === "codex_busy" || outcome.error === "codex_busy") {
    return { kind: "error", key: "storage.cleanup.err.codex_busy" };
  }
  if (!outcome.ok) return { kind: "error", key: "storage.policy.runFailed" };
  return {
    kind: "done",
    mode: outcome.mode === "permanent" ? "permanent" : "quarantine",
    removed: outcome.removed ?? 0,
    freedBytes: outcome.freedBytes ?? 0,
  };
}

/** The outcome a poll can attribute to the run it started, once that job has settled. */
export function jobMatchesStart(
  job: CleanupJob | undefined,
  startedAt: number,
): CleanupJobOutcome | undefined {
  if (!job || job.status === "running") return undefined;
  if (job.startedAt === startedAt && job.lastOutcome) return job.lastOutcome;
  if (job.finishedAt && job.finishedAt >= startedAt && job.lastOutcome) return job.lastOutcome;
  return undefined;
}

/** Copy for each refusal the cleanup endpoints report. */
const CLEANUP_ERROR_COPY = new Map<string, TKey>([
  ["codex_busy", "storage.cleanup.err.codex_busy"],
  ["stale_preview", "storage.cleanup.err.stale_preview"],
  ["restore_pending_overlap", "storage.cleanup.err.restore_pending_overlap"],
  ["referenced_history", "storage.cleanup.err.referenced_history"],
  ["invalid_digest", "storage.cleanup.err.invalid_digest"],
  ["invalid_mode", "storage.cleanup.err.invalid_mode"],
  ["db_reconcile_failed", "storage.cleanup.err.db_reconcile_failed"],
  ["cleanup_failed", "storage.cleanup.err.cleanup_failed"],
]);

/** A refusal the listener phrased as "too large", which the board reports as a truncated scan. */
export function isTruncatedCleanupRefusal(message: string | undefined): boolean {
  if (!message) return false;
  return message.includes("too large to preview safely") || message.includes("too large to clean safely");
}

export function mapCleanupError(
  t: TFn,
  code: string | undefined,
  fallback?: string,
  trashDir?: string,
  message?: string,
): string {
  if (isTruncatedCleanupRefusal(message) || isTruncatedCleanupRefusal(fallback)) {
    return t("storage.cleanup.err.truncated");
  }
  if (code === "fs_failed") {
    return trashDir
      ? t("storage.cleanup.err.fs_failed_trash", { trashDir })
      : t("storage.cleanup.err.fs_failed");
  }
  const copy = code === undefined ? undefined : CLEANUP_ERROR_COPY.get(code);
  if (copy !== undefined) return t(copy);
  return fallback ?? t("storage.cleanup.cleanupFailed");
}

/** Copy for each refusal the restore endpoints report. */
const RESTORE_ERROR_COPY = new Map<string, TKey>([
  ["codex_busy", "storage.trash.err.codex_busy"],
  ["invalid_trash", "storage.trash.err.invalid_trash"],
  ["missing_trash", "storage.trash.err.missing_trash"],
  ["dest_exists", "storage.trash.err.dest_exists"],
  ["fs_failed", "storage.trash.err.fs_failed"],
  ["db_reconcile_failed", "storage.trash.err.db_reconcile_failed"],
  ["storage_mutation_busy", "storage.trash.err.storage_mutation_busy"],
  ["restore_failed", "storage.trash.err.restore_failed"],
  ["restore_worker_timeout", "storage.trash.err.restore_worker_timeout"],
  ["restore_worker_aborted", "storage.trash.err.restore_worker_aborted"],
]);

export function mapRestoreError(
  t: TFn,
  code: string | undefined,
  fallback?: string,
): string {
  if (code === "restore_worker_failed") {
    return fallback ?? t("storage.trash.err.restore_worker_failed");
  }
  const copy = code === undefined ? undefined : RESTORE_ERROR_COPY.get(code);
  if (copy !== undefined) return t(copy);
  return fallback ?? t("storage.trash.restoreFailed");
}

/** Messages the transport produces verbatim and no reader can act on. */
const EXACT_TRANSPORT_FAILURES = ["Failed to fetch"];
const CONTAINED_TRANSPORT_FAILURES = ["NetworkError", "network error", "JSON", "Unexpected end of"];

export function localizedCatch(error: unknown, fallback: string): string {
  if (!(error instanceof Error)) return fallback;
  const message = error.message;
  const transportFailure = EXACT_TRANSPORT_FAILURES.includes(message)
    || CONTAINED_TRANSPORT_FAILURES.some(fragment => message.includes(fragment));
  if (transportFailure) return fallback;
  return message || fallback;
}

/** A response may replace the screen only while it is still the newest one and nobody is editing. */
export function shouldApplyLoadedPolicy(input: {
  aborted: boolean;
  generation: number;
  currentGeneration: number;
  dirty: boolean;
  editing: boolean;
}): boolean {
  if (input.aborted || input.generation !== input.currentGeneration) return false;
  return !input.dirty && !input.editing;
}

export type RunStartInterpretation =
  | { kind: "already_running"; policy?: CleanupPolicy }
  | { kind: "run_failed"; policy?: CleanupPolicy }
  | { kind: "started"; startedAt: number; policy?: CleanupPolicy };

export function interpretRunStartResponse(
  status: number,
  json: {
    ok?: boolean;
    started?: boolean;
    error?: string;
    job?: CleanupJob;
    policy?: CleanupPolicy;
  },
): RunStartInterpretation {
  if (status === 409 || json.error === "already_running") {
    return { kind: "already_running", policy: json.policy };
  }
  if (status < 200 || status >= 300) return { kind: "run_failed", policy: json.policy };
  const startedAt = json.job?.startedAt;
  if (!json.started || !startedAt) return { kind: "run_failed", policy: json.policy };
  return { kind: "started", startedAt, policy: json.policy };
}