/** Benes dashboard client for the Go proxy (`internal/server`). */
/** Codex log-guard mutation mapping for StorageWorkspace. */

import { formatBytes } from "../../format-bytes.ts";
import { logGuardLabel, logGuardOperationLabel } from "../../i18n/log-guard.ts";
import type { Locale } from "../../i18n/shared.ts";
import type { CodexLogGuardAction, CodexLogGuardReport } from "./types.ts";

/**
 * Catalogue copy for every refusal the log-guard endpoints report.
 *
 * A refusal that names the log configuration reads from the guard's own copy; one that names an
 * operation reads from the operation copy. A code this table does not carry is answered with the
 * generic operation line, because the reader still needs to know the action did not happen.
 */
const REFUSAL_LABELS: ReadonlyArray<[string, (locale: Locale) => string]> = [
  ["trigger_collision", locale => logGuardLabel(locale, "error.trigger_collision")],
  ["config_write_failed", locale => logGuardLabel(locale, "error.config_write_failed")],
  ["codex_running", locale => logGuardOperationLabel(locale, "error.codex_running")],
  ["process_enumeration_failed", locale => logGuardOperationLabel(locale, "error.process_enumeration_failed")],
  ["busy", locale => logGuardOperationLabel(locale, "error.busy")],
  ["unsupported_schema", locale => logGuardOperationLabel(locale, "error.unsupported_schema")],
  ["unsafe_path", locale => logGuardOperationLabel(locale, "error.unsafe_path")],
  ["database_error", locale => logGuardOperationLabel(locale, "error.database_error")],
  ["auto_vacuum_not_incremental", locale => logGuardOperationLabel(locale, "error.auto_vacuum_not_incremental")],
  ["integrity_check_failed", locale => logGuardOperationLabel(locale, "error.integrity_check_failed")],
];

const REFUSAL_LABEL_BY_CODE = new Map(REFUSAL_LABELS);

export function mutationErrorLabel(locale: Locale, code: unknown): string {
  const label = typeof code === "string" ? REFUSAL_LABEL_BY_CODE.get(code) : undefined;
  return label === undefined
    ? logGuardOperationLabel(locale, "error.generic")
    : label(locale);
}

export function logGuardRequest(action: CodexLogGuardAction): { suffix: string; init: RequestInit } {
  if (action.action !== "protect") {
    return { suffix: action.action, init: { method: "POST" } };
  }
  return {
    suffix: "protect",
    init: {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify({ mode: action.mode }),
    },
  };
}

export interface CompactPostedReport {
  pagesReclaimed?: number;
  logicalBytesReclaimed?: number;
  physicalDatabaseBytesReclaimed?: number;
  complete?: boolean;
  stopReason?: string;
}

export function compactionSummary(postedReport: CompactPostedReport, locale: Locale): string {
  const pages = postedReport.pagesReclaimed ?? 0;
  const logical = formatBytes(postedReport.logicalBytesReclaimed ?? 0, locale);
  const physical = formatBytes(postedReport.physicalDatabaseBytesReclaimed ?? 0, locale);
  const state = logGuardLabel(locale, postedReport.complete ? "compactComplete" : "compactPartial");
  const interrupted = !postedReport.complete && postedReport.stopReason;
  const reason = interrupted ? ` (${postedReport.stopReason})` : "";
  return [
    `${state}${reason}`,
    `${pages.toLocaleString(locale)} ${logGuardLabel(locale, "pagesUnit")}`,
    `${logical} / ${physical}`,
  ].join(" — ");
}

export function scopedForGeneration<T>(
  scoped: { generation: number; value: T } | null,
  generation: number,
): T | null {
  return scoped?.generation === generation ? scoped.value : null;
}

export type LogGuardActionResult =
  | { kind: "error"; message: string }
  | { kind: "report"; report: CodexLogGuardReport; compaction?: string }
  | { kind: "compact"; compaction?: string; report?: CodexLogGuardReport };

async function readErrorMessage(response: Response, locale: Locale): Promise<string> {
  const errorPayload = await response.json().catch(() => ({})) as Record<string, unknown>;
  return mutationErrorLabel(locale, errorPayload.error);
}

async function refreshLogGuardReport(
  apiBase: string,
  fetchImpl: typeof fetch,
): Promise<CodexLogGuardReport | undefined> {
  try {
    const refreshed = await fetchImpl(`${apiBase}/api/storage/codex-logs`);
    if (!refreshed.ok) return undefined;
    return await refreshed.json() as CodexLogGuardReport;
  } catch {
    return undefined;
  }
}

export async function performLogGuardAction(opts: {
  apiBase: string;
  action: CodexLogGuardAction;
  locale: Locale;
  fetchImpl?: typeof fetch;
}): Promise<LogGuardActionResult> {
  const fetchImpl = opts.fetchImpl ?? fetch;
  const { suffix, init } = logGuardRequest(opts.action);
  try {
    const response = await fetchImpl(`${opts.apiBase}/api/storage/codex-logs/${suffix}`, init);
    if (!response.ok) return { kind: "error", message: await readErrorMessage(response, opts.locale) };
    if (opts.action.action !== "compact") {
      return { kind: "report", report: await response.json() as CodexLogGuardReport };
    }
    const posted = await response.json().catch(() => null) as { report?: CompactPostedReport } | null;
    const compaction = posted?.report ? compactionSummary(posted.report, opts.locale) : undefined;
    const report = await refreshLogGuardReport(opts.apiBase, fetchImpl);
    return report ? { kind: "report", report, compaction } : { kind: "compact", compaction };
  } catch {
    return { kind: "error", message: logGuardOperationLabel(opts.locale, "error.generic") };
  }
}