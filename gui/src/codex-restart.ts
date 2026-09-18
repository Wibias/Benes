import type {
  CodexRestartCode,
  CodexRestartResponse,
} from "./lib/codex-restart-contract.ts";
import { isCodexRestartResponse } from "./lib/codex-restart-contract.ts";

// Re-exported so callers import the vocabulary from one place.
export type { CodexRestartCode, CodexRestartResponse };

// Enumeration can shell out to ps, procfs, or PowerShell CIM, and the request also
// rewrites the catalog first, so this is slower than an ordinary management call.
const DEFAULT_TIMEOUT_MS = 30_000;

/** Caller-supplied, already-localized copy for one failure mode. */
type RestartFailureText = () => string;

/** Caller-supplied copy for an HTTP rejection; receives the status code. */
type RestartStatusText = (status: number) => string;

export interface CodexRestartOutcome {
  ok: boolean;
  result?: CodexRestartResponse;
  /** Localized by the caller through the format* options. */
  message?: string;
}

export interface CodexRestartOptions {
  fetchFn?: typeof fetch;
  timeoutMs?: number;
  formatFailure?: RestartStatusText;
  formatUnreachable?: RestartFailureText;
  formatMalformed?: RestartFailureText;
  /**
   * Separate from `formatUnreachable`: a timeout does NOT mean nothing happened.
   * The request is abandoned client-side, but the proxy never sees the abort, so
   * a catalog sync that ran long can still stop app-servers afterwards. Telling
   * the user "could not reach the proxy" there would be a lie they act on.
   */
  formatTimeout?: RestartFailureText;
}

/**
 * How a successful restart is announced, without naming any translation key.
 *
 * Kept here as data so the wording lives with the hook that has `t(...)`, and so the
 * classifier itself is testable without a language provider.
 */
export type CodexRestartAnnouncement =
  | { kind: "stopped"; stoppedCount: number }
  | { kind: "nothing-running" }
  | { kind: "enumeration-unavailable" }
  | { kind: "partial"; survivingCount: number };

export function classifyCodexRestart(result: CodexRestartResponse): CodexRestartAnnouncement {
  switch (result.code) {
    case "stopped":
      return { kind: "stopped", stoppedCount: result.stopped.length };
    case "nothing_running":
      return { kind: "nothing-running" };
    case "enumeration_unavailable":
      return { kind: "enumeration-unavailable" };
    default:
      return { kind: "partial", survivingCount: result.surviving.length };
  }
}

function rejected(message: string): CodexRestartOutcome {
  return { ok: false, message };
}

/** True when the failure is the local abort, not the proxy going missing. */
function abortedLocally(reason: unknown): boolean {
  if (typeof DOMException !== "undefined" && reason instanceof DOMException) {
    return reason.name === "AbortError" || reason.name === "TimeoutError";
  }
  if (!(reason instanceof Error)) return false;
  return reason.name === "AbortError" || reason.name === "TimeoutError";
}

export async function requestCodexRestart(
  apiBase: string,
  overrides: CodexRestartOptions = {},
): Promise<CodexRestartOutcome> {
  const fetchFn = overrides.fetchFn ?? fetch;
  const timeoutMs = overrides.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const failureText = overrides.formatFailure ?? (status => `Failed to restart Codex (HTTP ${status}).`);
  const unreachableText = overrides.formatUnreachable ?? (() => "Could not reach the proxy.");
  const malformedText = overrides.formatMalformed ?? (() => "The proxy returned an unexpected response.");
  const timeoutText = overrides.formatTimeout ?? (() => "The proxy did not answer in time. It may still be working.");

  const signal = AbortSignal.timeout(timeoutMs);

  let reply: Response;
  try {
    reply = await fetchFn(`${apiBase}/api/system/codex-restart`, { method: "POST", signal });
  } catch (reason) {
    // Unlike POST /api/stop, a dropped connection here is a real failure: this route
    // does not kill the process serving it, so silence means something broke. A
    // timeout is reported separately because the work may still be running.
    return rejected(abortedLocally(reason) ? timeoutText() : unreachableText());
  }

  if (!reply.ok) return rejected(failureText(reply.status));

  let body: unknown;
  try {
    body = await reply.json();
  } catch (reason) {
    // A body that arrives late enough to trip the same timeout is not a malformed
    // response; say so honestly rather than blaming the payload.
    return rejected(abortedLocally(reason) ? timeoutText() : malformedText());
  }

  // A parseable 2xx body of the wrong shape must not reach the caller: the caller
  // indexes into the pid arrays, and a contradictory body would be reported as a
  // success that never happened.
  if (!isCodexRestartResponse(body)) return rejected(malformedText());
  return { ok: true, result: body };
}
