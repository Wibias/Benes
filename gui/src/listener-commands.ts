import {
  CODEX_APP_SERVER_STATE_PATH,
  isCodexAppServerStateResponse,
  type CodexAppServerStateResponse,
} from "./lib/codex-restart-contract.ts";

/** GET /api/system/codex-app-server and POST /api/stop against the local listener. */

export type AppServerStateOutcome = {
  /** Null means "render nothing" — never a guess about what Codex is showing. */
  state: CodexAppServerStateResponse["state"] | null;
  runningCount: number;
};

export type ProxyStopOutcome =
  | { accepted: true }
  | { accepted: false; message: string };

export type ProxyStopOptions = {
  fetchFn?: typeof fetch;
  timeoutMs?: number;
  formatFailure?: (status: number) => string;
};

export type CodexAppServerStateOptions = {
  fetchFn?: typeof fetch;
  signal?: AbortSignal;
};

const STOP_PATH = "/api/stop";
const DEFAULT_STOP_MS = 15_000;
const QUIET_APP_SERVER: AppServerStateOutcome = { state: null, runningCount: 0 };

type TransportResult =
  | { reached: false }
  | { reached: true; ok: boolean; status: number; body: unknown };

async function invokeListener(
  send: typeof fetch,
  href: string,
  init: RequestInit,
): Promise<TransportResult> {
  try {
    const response = await send(href, init);
    const body = await response.json().catch(() => null);
    return { reached: true, ok: response.ok, status: response.status, body };
  } catch {
    return { reached: false };
  }
}

function textField(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function stopFailureCopy(
  body: unknown,
  status: number,
  formatFailure: (status: number) => string,
): string {
  const record = body && typeof body === "object" ? body as { message?: unknown; error?: unknown; success?: unknown } : null;
  const message = textField(record?.message);
  if (message) return message;
  const error = textField(record?.error);
  if (error) return error;
  return formatFailure(status);
}

function stopRejected(result: Extract<TransportResult, { reached: true }>): boolean {
  if (!result.ok) return true;
  if (!result.body || typeof result.body !== "object") return false;
  return (result.body as { success?: unknown }).success === false;
}

/**
 * GET the Codex app-server reading. Fetched on demand, never on a timer.
 * Any transport, HTTP, or contract failure stays silent.
 */
export async function fetchCodexAppServerState(
  apiBase: string,
  options: CodexAppServerStateOptions = {},
): Promise<AppServerStateOutcome> {
  const result = await invokeListener(options.fetchFn ?? fetch, `${apiBase}${CODEX_APP_SERVER_STATE_PATH}`, {
    signal: options.signal,
  });
  if (!result.reached || !result.ok) return QUIET_APP_SERVER;
  if (!isCodexAppServerStateResponse(result.body)) return QUIET_APP_SERVER;
  return { state: result.body.state, runningCount: result.body.runningCount };
}

/**
 * POST /api/stop. Once shutdown starts the socket may vanish, so a dropped
 * connection is accepted. A received non-2xx or `{ success: false }` is failure.
 */
export async function requestProxyStop(
  apiBase: string,
  options: ProxyStopOptions = {},
): Promise<ProxyStopOutcome> {
  const formatFailure = options.formatFailure ?? ((status: number) => `Failed to stop proxy (HTTP ${status}).`);
  const result = await invokeListener(options.fetchFn ?? fetch, `${apiBase}${STOP_PATH}`, {
    method: "POST",
    signal: AbortSignal.timeout(options.timeoutMs ?? DEFAULT_STOP_MS),
  });
  if (!result.reached) return { accepted: true };
  if (stopRejected(result)) {
    return { accepted: false, message: stopFailureCopy(result.body, result.status, formatFailure) };
  }
  return { accepted: true };
}
