export type NativeIntegrationClientId = "claude" | "grok" | "codex" | "claude-desktop";
export type NativeIntegrationState = "absent" | "current" | "unsafe";
export type NativeRefusalReason =
  | "not_installed"
  | "orphaned_marker"
  | "home_mismatch"
  | "config_busy"
  | "write_failed"
  | "metadata_unreadable"
  | "cleanup_incomplete"
  | "desired_state_changed";

export type NativeStatus = {
  readonly clientId: NativeIntegrationClientId;
  readonly state: NativeIntegrationState;
  readonly installed: boolean;
  readonly configPath: string;
  readonly desiredEnabled: boolean;
  readonly disableBlocked: { readonly reason: NativeRefusalReason; readonly message: string } | null;
};

export type NativeStatusListEnvelope = {
  readonly clients: readonly NativeStatus[];
};

export type NativeToggleEnvelope = {
  readonly ok: true;
  readonly clientId: NativeIntegrationClientId;
  readonly changed: boolean;
  readonly state: NativeIntegrationState;
  readonly message: string;
  readonly desiredEnabled: boolean;
  readonly reason?: string;
};

export type NativeRefusalEnvelope = {
  readonly error: string;
  readonly code: "native_integration_refused" | "native_integration_failed";
  readonly clientId: NativeIntegrationClientId;
  readonly reason: NativeRefusalReason;
  readonly message: string;
  readonly desiredEnabled?: boolean;
  readonly residualPaths?: readonly string[];
};

export type NativeErrorEnvelope = {
  readonly error?: string;
  readonly code?: string;
  readonly clientId?: NativeIntegrationClientId;
  readonly reason?: string;
  readonly message?: string;
};

export type NativeErrorBody = NativeErrorEnvelope | NativeRefusalEnvelope;

const CLIENT_SET = new Set(["claude", "grok", "codex", "claude-desktop"]);
const STATE_SET = new Set(["absent", "current", "unsafe"]);
const CODE_SET = new Set(["native_integration_refused", "native_integration_failed"]);
const REASON_SET = new Set([
  "not_installed",
  "orphaned_marker",
  "home_mismatch",
  "config_busy",
  "write_failed",
  "metadata_unreadable",
  "cleanup_incomplete",
  "desired_state_changed",
]);

function dict(value: unknown): Record<string, unknown> | null {
  if (value === null || typeof value !== "object" || Array.isArray(value)) return null;
  return value as Record<string, unknown>;
}

function str(value: unknown): string | undefined {
  return typeof value === "string" ? value : undefined;
}

function flag(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function knownClient(value: unknown): NativeIntegrationClientId | undefined {
  return CLIENT_SET.has(String(value)) ? value as NativeIntegrationClientId : undefined;
}

function knownState(value: unknown): NativeIntegrationState | undefined {
  return STATE_SET.has(String(value)) ? value as NativeIntegrationState : undefined;
}

function knownReason(value: unknown): NativeRefusalReason | undefined {
  return REASON_SET.has(String(value)) ? value as NativeRefusalReason : undefined;
}

function endpoint(apiBase: string, client?: NativeIntegrationClientId): string {
  const root = `${apiBase}/api/native-integrations`;
  return client ? `${root}/${encodeURIComponent(client)}` : root;
}

async function readPayload(response: Response): Promise<unknown> {
  const raw = typeof response.text === "function" ? await response.text() : "";
  if (!raw.trim()) return null;
  return JSON.parse(raw) as unknown;
}

export function isNativeRefusalEnvelope(body: unknown): body is NativeRefusalEnvelope {
  const row = dict(body);
  if (!row) return false;
  if (str(row.error) === undefined || str(row.message) === undefined) return false;
  if (!CODE_SET.has(String(row.code))) return false;
  if (!knownClient(row.clientId) || !knownReason(row.reason)) return false;
  return true;
}

export class NativeApiError extends Error {
  readonly refusal: NativeRefusalEnvelope | null;
  readonly status: number;
  readonly body: NativeErrorBody;

  constructor(status: number, body: NativeErrorBody) {
    const refusal = isNativeRefusalEnvelope(body) ? body : null;
    super(refusal?.message ?? body.error ?? body.message ?? `HTTP ${status}`);
    this.name = "NativeApiError";
    this.status = status;
    this.body = body;
    this.refusal = refusal;
  }
}

function decodeBlocked(value: unknown): NativeStatus["disableBlocked"] {
  const row = dict(value);
  if (!row) return null;
  const reason = knownReason(row.reason);
  const message = str(row.message);
  if (!reason || message === undefined) return null;
  return { reason, message };
}

function decodeStatus(value: unknown): NativeStatus | null {
  const row = dict(value);
  if (!row) return null;
  const clientId = knownClient(row.clientId);
  const state = knownState(row.state);
  const installed = flag(row.installed);
  const configPath = str(row.configPath);
  const desiredEnabled = flag(row.desiredEnabled);
  if (!clientId || !state || installed === undefined || configPath === undefined || desiredEnabled === undefined) {
    return null;
  }
  return {
    clientId,
    state,
    installed,
    configPath,
    desiredEnabled,
    disableBlocked: decodeBlocked(row.disableBlocked),
  };
}

function decodeToggle(value: unknown): NativeToggleEnvelope | null {
  const row = dict(value);
  if (!row || row.ok !== true) return null;
  const clientId = knownClient(row.clientId);
  const state = knownState(row.state);
  const changed = flag(row.changed);
  const desiredEnabled = flag(row.desiredEnabled);
  const message = str(row.message);
  if (!clientId || !state || changed === undefined || desiredEnabled === undefined || message === undefined) {
    return null;
  }
  const reason = str(row.reason);
  return { ok: true, clientId, changed, state, message, desiredEnabled, ...(reason ? { reason } : {}) };
}

export async function loadNativeIntegrations(apiBase: string, signal?: AbortSignal) {
  try {
    const response = await fetch(endpoint(apiBase), { signal });
    if (!response.ok) return null;
    const payload = dict(await readPayload(response));
    if (!payload || !Array.isArray(payload.clients)) return null;
    const clients = payload.clients.map(decodeStatus).filter((item): item is NativeStatus => item !== null);
    return { clients };
  } catch {
    return null;
  }
}

export async function toggleNativeIntegration(
  apiBase: string,
  client: NativeIntegrationClientId,
  enabled: boolean,
  signal?: AbortSignal,
) {
  const response = await fetch(endpoint(apiBase, client), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
    signal,
  });
  let payload: unknown;
  try {
    payload = await readPayload(response);
  } catch {
    payload = {};
  }
  if (!response.ok) {
    throw new NativeApiError(response.status, dict(payload) ?? {});
  }
  const decoded = decodeToggle(payload);
  if (!decoded) throw new NativeApiError(response.status, dict(payload) ?? {});
  return decoded;
}
