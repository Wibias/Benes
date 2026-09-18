/**
 * Contract for the dashboard-driven Codex app-server restart.
 *
 * Scalar-only payload. A command line can contain a home directory and a username,
 * and an OS error message often embeds a path, so neither crosses this boundary.
 */

export const CODEX_RESTART_METHOD = "POST";
export const CODEX_RESTART_PATH = "/api/system/codex-restart";
export const CODEX_APP_SERVER_STATE_PATH = "/api/system/codex-app-server";

export type CodexAppServerState = "fresh" | "stale" | "not_running" | "unknown";

export type CodexRestartCode =
  | "stopped"
  | "nothing_running"
  | "enumeration_unavailable"
  | "partially_stopped";

/** GET response: a cheap reading with no side effects. It never signals. */
export interface CodexAppServerStateResponse {
  state: CodexAppServerState;
  runningCount: number;
}

/** POST response. All four arrays are pid lists — never command lines. */
export interface CodexRestartResponse {
  success: boolean;
  /** Classifier reading taken BEFORE any signal, so the UI can explain why it acted. */
  stateBefore: CodexAppServerState;
  /** Whether a catalog or cache write happened during this request. */
  synced: boolean;
  requested: number[];
  stopped: number[];
  surviving: number[];
  failed: number[];
  code: CodexRestartCode;
}

const APP_SERVER_STATES: readonly string[] = ["fresh", "stale", "not_running", "unknown"];
const RESTART_CODES: readonly string[] = [
  "stopped",
  "nothing_running",
  "enumeration_unavailable",
  "partially_stopped",
];

function isPidList(value: unknown): value is number[] {
  return Array.isArray(value)
    && value.every(entry => typeof entry === "number" && Number.isSafeInteger(entry) && entry > 0);
}

function hasRestartScalars(view: Record<string, unknown>): boolean {
  return typeof view.success === "boolean"
    && typeof view.synced === "boolean"
    && typeof view.stateBefore === "string"
    && APP_SERVER_STATES.includes(view.stateBefore)
    && typeof view.code === "string"
    && RESTART_CODES.includes(view.code);
}

function hasRestartPidLists(view: Record<string, unknown>): boolean {
  return isPidList(view.requested)
    && isPidList(view.stopped)
    && isPidList(view.surviving)
    && isPidList(view.failed);
}

function hasRestartShape(value: unknown): value is CodexRestartResponse {
  if (typeof value !== "object" || value === null) return false;
  const view = value as Record<string, unknown>;
  return hasRestartScalars(view) && hasRestartPidLists(view);
}

function restartOutcomeIsConsistent(response: CodexRestartResponse): boolean {
  if (response.success !== (response.code !== "partially_stopped")) return false;
  if (response.success && (response.surviving.length > 0 || response.failed.length > 0)) return false;
  if (!response.success && response.surviving.length === 0 && response.failed.length === 0) return false;
  const stoppedMustBeEmpty = response.code === "nothing_running"
    || response.code === "enumeration_unavailable";
  if (stoppedMustBeEmpty && response.stopped.length > 0) return false;
  return true;
}

export function isCodexRestartResponse(value: unknown): value is CodexRestartResponse {
  return hasRestartShape(value) && restartOutcomeIsConsistent(value);
}

export function isCodexAppServerStateResponse(
  value: unknown,
): value is CodexAppServerStateResponse {
  if (typeof value !== "object" || value === null) return false;
  const view = value as Record<string, unknown>;
  return typeof view.state === "string"
    && APP_SERVER_STATES.includes(view.state)
    && typeof view.runningCount === "number"
    && Number.isSafeInteger(view.runningCount)
    && view.runningCount >= 0;
}
