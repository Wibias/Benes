/**
 * The canonical Claude Desktop lifecycle over HTTP.
 *
 * Every call here targets the runtime contract #255 / merged #328 landed. No
 * model, endpoint, or credential route exists to call, because Claude Desktop's
 * supported native configuration has no such surface.
 */
import { managementErrorMessage } from "./claude-settings-error.ts";
import {
  CLAUDE_DESKTOP_LOAD_FAILED,
  CLAUDE_DESKTOP_MUTATION_FAILED,
  CLAUDE_DESKTOP_SAVE_FAILED,
  claudeDesktopDesiredBody,
  claudeDesktopMutationConfirmed,
  decodeClaudeDesktopStatus,
  type ClaudeDesktopMutation,
  type ClaudeDesktopMutationReport,
  type ClaudeDesktopStatus,
} from "./claude-desktop-state.ts";

async function readBody(response: Response): Promise<unknown> {
  try {
    return await response.json();
  } catch {
    // A refusal without a JSON body still has to fail closed.
    return null;
  }
}

export async function readClaudeDesktopStatus(
  apiBase: string,
  signal?: AbortSignal,
): Promise<ClaudeDesktopStatus> {
  const response = await fetch(`${apiBase}/api/claude-desktop`, { signal });
  const body = await readBody(response);
  if (!response.ok) throw new Error(managementErrorMessage(body, CLAUDE_DESKTOP_LOAD_FAILED));
  return decodeClaudeDesktopStatus(body);
}

/**
 * Stores desired enablement only. The returned document is the runtime status,
 * which is the same authority the first read used, so a save cannot be mistaken
 * for an applied native projection.
 */
export async function saveClaudeDesktopDesired(
  apiBase: string,
  enabled: boolean,
  signal?: AbortSignal,
): Promise<ClaudeDesktopStatus> {
  const response = await fetch(`${apiBase}/api/claude-desktop`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(claudeDesktopDesiredBody(enabled)),
    signal,
  });
  const body = await readBody(response);
  if (!response.ok) throw new Error(managementErrorMessage(body, CLAUDE_DESKTOP_SAVE_FAILED));
  return decodeClaudeDesktopStatus(body);
}

function decodeMutationReport(body: unknown): ClaudeDesktopMutationReport {
  const row = typeof body === "object" && body !== null ? (body as Record<string, unknown>) : {};
  return {
    changed: row.changed === true,
    applied: row.applied === true,
    restartRequired: row.restartRequired === true,
  };
}

export interface ClaudeDesktopLifecycleOutcome {
  /** The runtime status re-read after the operation, never the write's echo. */
  status: ClaudeDesktopStatus;
  mutation: ClaudeDesktopMutationReport;
  /** True only when that re-read confirms the native result. */
  confirmed: boolean;
}

/**
 * Runs one lifecycle mutation and then reconciles against the runtime.
 *
 * The mutation keeps its own refusal: a refused apply is reported as the refusal
 * the runtime returned, and it leaves no optimistic success behind.
 */
export async function runClaudeDesktopLifecycle(
  apiBase: string,
  mutation: ClaudeDesktopMutation,
  signal?: AbortSignal,
): Promise<ClaudeDesktopLifecycleOutcome> {
  const response = await fetch(`${apiBase}/api/claude-desktop/${mutation}`, { method: "POST", signal });
  const body = await readBody(response);
  if (!response.ok) throw new Error(managementErrorMessage(body, CLAUDE_DESKTOP_MUTATION_FAILED));
  const report = decodeMutationReport(body);
  const status = await readClaudeDesktopStatus(apiBase, signal);
  return { status, mutation: report, confirmed: claudeDesktopMutationConfirmed(mutation, status) };
}
