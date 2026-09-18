import {
  FILE_MANAGED_HARNESS_IDS,
  type FileManagedHarnessId,
  type HarnessModelRegistration,
} from "../harnesses/types.ts";

export type IntegrationClientId = FileManagedHarnessId;
export type IntegrationState = "absent" | "current" | "stale" | "conflict" | "unsafe";
export type IntegrationReason =
  | "unparseable"
  | "not-regular-file"
  | "foreign-edit"
  | "unowned-key"
  | "blocked-container"
  | "unresolvable-path";
export type IntegrationRefusalReason =
  | "not_installed"
  | "conflict"
  | "unsafe"
  | "non_loopback"
  | "drift_requires_confirm"
  | "snapshot_expired"
  | "write_failed";
export type IntegrationRefusalCode =
  | "integration_unsafe"
  | "integration_conflict"
  | "integration_drift_confirmation_required"
  | "integration_snapshot_expired"
  | "integration_mutation_failed";

export type IntegrationStatus = {
  readonly clientId: FileManagedHarnessId;
  readonly state: IntegrationState;
  readonly installed: boolean;
  readonly configPath: string;
  readonly appliedAt?: string;
  readonly lastOpId?: string;
  readonly reason?: IntegrationReason;
  readonly snapshotCount: number;
  readonly retentionDegraded: boolean;
};

export type IntegrationStateListEnvelope = {
  readonly clients: readonly IntegrationStatus[];
};

export type IntegrationJournalRow = {
  readonly opId: string;
  readonly clientId: IntegrationClientId;
  readonly kind: "apply" | "disable" | "refresh" | "restore";
  readonly at: string;
  readonly configPath: string;
  readonly snapshot: "none" | "stored" | "expired";
  readonly undoable: boolean;
};

export type IntegrationJournalEnvelope = {
  readonly operations: readonly IntegrationJournalRow[];
};

export type IntegrationMutationResult = {
  readonly ok: true;
  readonly clientId: FileManagedHarnessId;
  readonly changed: boolean;
  readonly state: IntegrationState;
  readonly opId?: string;
  readonly message: string;
};

export type IntegrationToggleResult = IntegrationMutationResult;
export type IntegrationRestoreResult = IntegrationMutationResult;
export type IntegrationMutationEnvelope = IntegrationMutationResult;

export type IntegrationRefusalEnvelope = {
  readonly error: string;
  readonly code: IntegrationRefusalCode;
  readonly clientId: FileManagedHarnessId;
  readonly state: IntegrationState;
  readonly reason: IntegrationRefusalReason;
  readonly message: string;
  readonly snapshotPath?: string;
  readonly residual?: boolean;
};

export type IntegrationErrorEnvelope = {
  readonly error?: string;
  readonly code?: string;
  readonly clientId?: FileManagedHarnessId;
  readonly state?: string;
  readonly reason?: string;
  readonly message?: string;
  readonly opId?: string;
  readonly snapshotPath?: string;
  readonly residual?: boolean;
  readonly validClients?: readonly FileManagedHarnessId[];
  readonly hint?: string;
};

export type IntegrationErrorBody = IntegrationErrorEnvelope | IntegrationRefusalEnvelope;

const KIND_SET = new Set(["apply", "disable", "refresh", "restore"]);
const SNAPSHOT_SET = new Set(["none", "stored", "expired"]);
const STATE_SET = new Set(["absent", "current", "stale", "conflict", "unsafe"]);
const CODE_SET = new Set([
  "integration_unsafe",
  "integration_conflict",
  "integration_drift_confirmation_required",
  "integration_snapshot_expired",
  "integration_mutation_failed",
]);
const REASON_SET = new Set([
  "not_installed",
  "conflict",
  "unsafe",
  "non_loopback",
  "drift_requires_confirm",
  "snapshot_expired",
  "write_failed",
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

/** Counts are trusted only as non-negative integers; anything else is an unusable payload. */
function num(value: unknown): number | undefined {
  return typeof value === "number" && Number.isInteger(value) && value >= 0 ? value : undefined;
}

function knownClient(value: unknown): FileManagedHarnessId | undefined {
  return FILE_MANAGED_HARNESS_IDS.includes(value as FileManagedHarnessId)
    ? value as FileManagedHarnessId
    : undefined;
}

function knownState(value: unknown): IntegrationState | undefined {
  return STATE_SET.has(String(value)) ? value as IntegrationState : undefined;
}

function writerPath(apiBase: string, tail = ""): string {
  return `${apiBase}/api/client-integrations${tail}`;
}

async function bodyText(response: Response): Promise<string> {
  return typeof response.text === "function" ? response.text() : "";
}

async function bodyObject(response: Response): Promise<Record<string, unknown>> {
  try {
    const raw = await bodyText(response);
    if (!raw.trim()) return {};
    return dict(JSON.parse(raw)) ?? {};
  } catch {
    return {};
  }
}

export function isIntegrationRefusalEnvelope(body: unknown): body is IntegrationRefusalEnvelope {
  const row = dict(body);
  if (!row) return false;
  if (str(row.error) === undefined || str(row.message) === undefined) return false;
  if (!CODE_SET.has(String(row.code)) || !REASON_SET.has(String(row.reason))) return false;
  if (!knownClient(row.clientId) || !knownState(row.state)) return false;
  return true;
}

export class IntegrationApiError extends Error {
  readonly refusal: IntegrationRefusalEnvelope | null;
  readonly status: number;
  readonly body: IntegrationErrorBody;

  constructor(status: number, body: IntegrationErrorBody) {
    const refusal = isIntegrationRefusalEnvelope(body) ? body : null;
    super(refusal?.message ?? body.error ?? body.message ?? `HTTP ${status}`);
    this.name = "IntegrationApiError";
    this.status = status;
    this.body = body;
    this.refusal = refusal;
  }
}

async function mustParse<T>(response: Response, decode: (value: unknown) => T | null): Promise<T> {
  if (!response.ok) throw new IntegrationApiError(response.status, await bodyObject(response));
  let parsed: unknown;
  try {
    const raw = await bodyText(response);
    parsed = raw.trim() ? JSON.parse(raw) : null;
  } catch {
    throw new IntegrationApiError(response.status, {});
  }
  const decoded = decode(parsed);
  if (decoded == null) throw new IntegrationApiError(response.status, dict(parsed) ?? {});
  return decoded;
}

async function optionalProbe<T>(request: Promise<Response>, decode: (value: unknown) => T | null): Promise<T | null> {
  try {
    const response = await request;
    if (!response.ok) return null;
    const raw = await bodyText(response);
    if (!raw.trim()) return null;
    return decode(JSON.parse(raw));
  } catch {
    return null;
  }
}

function requiredStatus(row: Record<string, unknown>): IntegrationStatus | null {
  const clientId = knownClient(row.clientId);
  const state = knownState(row.state);
  const configPath = str(row.configPath);
  const installed = flag(row.installed);
  const snapshotCount = typeof row.snapshotCount === "number" ? row.snapshotCount : undefined;
  const retentionDegraded = flag(row.retentionDegraded);
  if (!clientId || !state || configPath === undefined || installed === undefined) return null;
  if (snapshotCount === undefined || retentionDegraded === undefined) return null;
  return { clientId, state, installed, configPath, snapshotCount, retentionDegraded };
}

function decodeStatus(value: unknown): IntegrationStatus | null {
  const row = dict(value);
  const base = row ? requiredStatus(row) : null;
  if (!row || !base) return null;
  const appliedAt = str(row.appliedAt);
  const lastOpId = str(row.lastOpId);
  const reason = str(row.reason) ? row.reason as IntegrationReason : undefined;
  return {
    ...base,
    ...(appliedAt ? { appliedAt } : {}),
    ...(lastOpId ? { lastOpId } : {}),
    ...(reason ? { reason } : {}),
  };
}

function decodeJournalRow(value: unknown): IntegrationJournalRow | null {
  const row = dict(value);
  if (!row) return null;
  const opId = str(row.opId);
  const clientId = knownClient(row.clientId);
  const kind = KIND_SET.has(String(row.kind)) ? row.kind as IntegrationJournalRow["kind"] : undefined;
  const at = str(row.at);
  const configPath = str(row.configPath);
  const snapshot = SNAPSHOT_SET.has(String(row.snapshot)) ? row.snapshot as IntegrationJournalRow["snapshot"] : undefined;
  const undoable = flag(row.undoable);
  if (!opId || !clientId || !kind || !at || configPath === undefined || !snapshot || undoable === undefined) return null;
  return { opId, clientId, kind, at, configPath, snapshot, undoable };
}

function decodeMutation(value: unknown): IntegrationMutationResult | null {
  const row = dict(value);
  if (!row || row.ok !== true) return null;
  const clientId = knownClient(row.clientId);
  const state = knownState(row.state);
  const changed = flag(row.changed);
  const message = str(row.message);
  if (!clientId || !state || changed === undefined || message === undefined) return null;
  const opId = str(row.opId);
  return { ok: true, clientId, changed, state, message, ...(opId ? { opId } : {}) };
}

export async function loadIntegrationStates(apiBase: string, signal?: AbortSignal) {
  return mustParse(await fetch(writerPath(apiBase), { signal }), (value) => {
    const row = dict(value);
    if (!row || !Array.isArray(row.clients)) return null;
    const clients = row.clients.map(decodeStatus).filter((item): item is IntegrationStatus => item !== null);
    return { clients };
  });
}

export async function loadIntegrationJournal(
  apiBase: string,
  client?: FileManagedHarnessId,
  signal?: AbortSignal,
) {
  const query = client ? `?client=${encodeURIComponent(client)}` : "";
  return mustParse(await fetch(writerPath(apiBase, `/journal${query}`), { signal }), (value) => {
    const row = dict(value);
    if (!row || !Array.isArray(row.operations)) return null;
    const operations = row.operations.map(decodeJournalRow).filter((item): item is IntegrationJournalRow => item !== null);
    return { operations };
  });
}

export async function toggleIntegration(
  apiBase: string,
  client: FileManagedHarnessId,
  enabled: boolean,
  signal?: AbortSignal,
) {
  return mustParse(
    await fetch(writerPath(apiBase, `/${encodeURIComponent(client)}`), {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ enabled }),
      signal,
    }),
    decodeMutation,
  );
}

export async function restoreIntegration(
  apiBase: string,
  opId: string,
  confirmDrift = false,
  signal?: AbortSignal,
) {
  return mustParse(
    await fetch(writerPath(apiBase, "/restore"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ opId, confirmDrift }),
      signal,
    }),
    decodeMutation,
  );
}

export function loadCodexRoutingStatus(apiBase: string, signal?: AbortSignal) {
  return optionalProbe(fetch(`${apiBase}/api/startup-health`, { signal }), (value) => {
    const row = dict(value);
    if (!row) return null;
    return {
      routingInjected: row.routingInjected === true,
      status: str(row.status),
      recommendedCommand: str(row.recommendedCommand) ?? null,
    };
  });
}

export function loadClaudeCodeStatus(apiBase: string, signal?: AbortSignal) {
  return optionalProbe(fetch(`${apiBase}/api/claude-code`, { signal }), (value) => {
    const row = dict(value);
    if (!row) return null;
    return {
      enabled: row.enabled === true,
      authMode: str(row.authMode),
    };
  });
}

export function loadClaudeDesktopStatus(apiBase: string, signal?: AbortSignal) {
  return optionalProbe(fetch(`${apiBase}/api/claude-desktop/status`, { signal }), (value) => {
    const row = dict(value);
    if (!row) return null;
    const desiredEnabled = flag(row.desiredEnabled);
    const installed = flag(row.installed);
    const observedKind = str(row.observedKind);
    if (desiredEnabled === undefined || installed === undefined || observedKind === undefined) return null;
    return {
      desiredEnabled,
      installed,
      observedKind,
      applied: row.applied === true,
      stale: row.stale === true,
      activeProfile: typeof row.activeProfile === "boolean" ? row.activeProfile : null,
      appliedAt: str(row.appliedAt) ?? null,
    };
  });
}

export function loadGrokFenceStatus(apiBase: string, signal?: AbortSignal) {
  return optionalProbe(fetch(`${apiBase}/api/grok`, { signal }), (value) => {
    const registration = decodeGrokRegistration(value);
    return registration ? { present: registration.present, registration } : null;
  });
}

/**
 * `GET /api/grok` publishes a derived projection summary: how much of the canonical Benes
 * catalogue the Grok managed block carries. It publishes no Grok-side model list, because
 * the catalogue is the only place model membership is decided.
 */
export function decodeGrokRegistration(value: unknown): HarnessModelRegistration | null {
  const row = dict(value);
  if (!row) return null;
  const configPath = str(row.configPath);
  const catalogue = num(row.catalogue);
  const registered = num(row.registered);
  if (configPath === undefined || catalogue === undefined || registered === undefined) return null;
  if (typeof row.present !== "boolean" || typeof row.current !== "boolean") return null;
  return {
    configPath,
    present: row.present,
    catalogue,
    registered,
    current: row.current,
    // Diagnostic, not a claim: absent means the last automatic write landed.
    lastAutomaticError: str(row.lastAutomaticError) ?? null,
  };
}
