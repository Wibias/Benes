import { readJsonIfOk, readJsonOrThrow } from "../../fetch-json.ts";
import type { TKey } from "../../i18n/shared";
import type {
  HarnessId,
  HarnessIssue,
  HarnessSettings,
  HarnessSidecarConfigured,
  HarnessSidecarMode,
  HarnessSidecarModality,
  HarnessSidecarModalityId,
  HarnessSidecarPolicy,
  HarnessSidecarSelection,
  HarnessSidecarSource,
  HarnessToken,
} from "./types";

export interface HarnessProbe {
  clientId: HarnessId;
  detectPath: string | null;
  logPath: string | null;
  running: boolean | null;
  token: HarnessToken;
  settings: HarnessSettings;
  /**
   * Server-projected sidecar policy. It deliberately stays outside `settings`
   * so no browser-persistence path can ever serialize it.
   */
  sidecarPolicy: HarnessSidecarPolicy | null;
}

interface HarnessProbeList {
  clients?: unknown[];
}

function isHarnessId(value: string): value is HarnessId {
  return [
    "claude-desktop",
    "claude",
    "codex",
    "dsh",
    "opencode",
    "pi",
    "prime",
    "omp",
    "hermes",
    "openclaw",
    "kimi",
    "gajae",
    "grok",
    "mcode",
  ].includes(value);
}

export async function loadHarnessProbes(apiBase: string, signal?: AbortSignal): Promise<HarnessProbe[]> {
  const body = await readJsonIfOk<HarnessProbeList>(
    await fetch(`${apiBase}/api/harnesses`, { signal }),
  );
  if (!body || !Array.isArray(body.clients)) return [];
  return body.clients
    .map(decodeHarnessProbe)
    .filter((probe): probe is HarnessProbe => probe !== null);
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function optionalPath(value: unknown): string | null {
  return typeof value === "string" && value ? value : null;
}

function decodeHarnessToken(value: unknown): HarnessToken {
  return value === "valid" || value === "expired" || value === "missing" ? value : "none";
}

function decodeHarnessSettings(raw: unknown): HarnessSettings {
  const record = isRecord(raw) ? raw : {};
  return {
    autoDetect: typeof record.autoDetect === "boolean" ? record.autoDetect : true,
    autoApply: typeof record.autoApply === "boolean" ? record.autoApply : true,
    retainSnapshot: typeof record.retainSnapshot === "boolean" ? record.retainSnapshot : true,
    allowRestart: typeof record.allowRestart === "boolean" ? record.allowRestart : false,
  };
}

function decodeSidecarMode(value: unknown): HarnessSidecarMode | null {
  return value === "enabled" || value === "disabled" ? value : null;
}

function decodeSidecarOverrides(raw: unknown): Record<HarnessSidecarModalityId, HarnessSidecarMode | null> {
  const record = isRecord(raw) ? raw : {};
  return { webSearch: decodeSidecarMode(record.webSearch), vision: decodeSidecarMode(record.vision) };
}

function decodeSidecarConfigured(raw: unknown): HarnessSidecarConfigured | null {
  if (!isRecord(raw) || typeof raw.enabled !== "boolean") return null;
  if (raw.source !== "global" && raw.source !== "harness_override") return null;
  return { enabled: raw.enabled, source: raw.source };
}

function decodeSidecarModality(
  supported: unknown,
  configured: unknown,
  override: HarnessSidecarMode | null,
): HarnessSidecarModality {
  return {
    supported: supported === true,
    override,
    configured: decodeSidecarConfigured(configured),
  };
}

/** Server response is untrusted: only known modalities, modes, and sources survive. */
export function decodeHarnessSidecarPolicy(raw: unknown, overrides?: unknown): HarnessSidecarPolicy | null {
  if (!isRecord(raw)) return null;
  const capabilities = isRecord(raw.capabilities) ? raw.capabilities : {};
  const configured = isRecord(raw.configured) ? raw.configured : {};
  const stored = decodeSidecarOverrides(overrides);
  return {
    identityStampable: capabilities.identityStampable === true,
    webSearch: decodeSidecarModality(capabilities.webSearch, configured.webSearch, stored.webSearch),
    vision: decodeSidecarModality(capabilities.vision, configured.vision, stored.vision),
  };
}

export function decodeHarnessProbe(raw: unknown): HarnessProbe | null {
  if (!isRecord(raw)) return null;
  const clientId = typeof raw.clientId === "string" ? raw.clientId : "";
  if (!isHarnessId(clientId)) return null;
  const settings = isRecord(raw.settings) ? raw.settings : {};
  return {
    clientId,
    detectPath: optionalPath(raw.detectPath),
    logPath: optionalPath(raw.logPath),
    running: typeof raw.running === "boolean" ? raw.running : null,
    token: decodeHarnessToken(raw.token),
    settings: decodeHarnessSettings(settings),
    sidecarPolicy: decodeHarnessSidecarPolicy(raw.sidecarPolicy, settings.sidecars),
  };
}

export async function saveHarnessSettings(
  apiBase: string,
  id: HarnessId,
  settings: HarnessSettings,
): Promise<void> {
  await readJsonOrThrow(
    await fetch(`${apiBase}/api/harnesses/settings`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        clientId: id,
        autoDetect: settings.autoDetect,
        autoApply: settings.autoApply,
        retainSnapshot: settings.retainSnapshot,
        allowRestart: settings.allowRestart,
      }),
    }),
    "harness settings could not be saved",
  );
}

export type HarnessRevealTarget = "log" | "config";

export async function revealHarnessPath(
  apiBase: string,
  id: HarnessId,
  target: HarnessRevealTarget,
): Promise<void> {
  await readJsonOrThrow(
    await fetch(`${apiBase}/api/harnesses/reveal`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ clientId: id, target }),
    }),
    "could not open path",
  );
}

const SIDECAR_MODALITY_ORDER: readonly HarnessSidecarModalityId[] = ["webSearch", "vision"];

/**
 * Narrow literal keys: the i18n liveness gate rejects a `t(...)` call whose key
 * type widens back to the whole catalogue.
 */
const SIDECAR_CONFIGURED_STATE = {
  enabled: "harnesses.sidecar.mode.enabled",
  disabled: "harnesses.sidecar.mode.disabled",
} as const satisfies Record<HarnessSidecarMode, TKey>;

const SIDECAR_CONFIGURED_SOURCE = {
  global: "harnesses.sidecar.source.global",
  harness_override: "harnesses.sidecar.source.harnessOverride",
} as const satisfies Record<HarnessSidecarSource, TKey>;

export type HarnessSidecarStateKey = (typeof SIDECAR_CONFIGURED_STATE)[HarnessSidecarMode];
export type HarnessSidecarSourceKey = (typeof SIDECAR_CONFIGURED_SOURCE)[HarnessSidecarSource];

/** Modalities the server says this Harness can actually decide. */
export function harnessSidecarModalities(policy: HarnessSidecarPolicy | null | undefined): HarnessSidecarModalityId[] {
  if (!policy?.identityStampable) return [];
  return SIDECAR_MODALITY_ORDER.filter((id) => policy[id].supported);
}

export function harnessSidecarSelection(
  policy: HarnessSidecarPolicy | null | undefined,
  id: HarnessSidecarModalityId,
): HarnessSidecarSelection {
  return policy?.[id].override ?? "global";
}

/** Configured-effective line, derived from the server response. */
export function harnessSidecarConfiguredKeys(
  policy: HarnessSidecarPolicy | null | undefined,
  id: HarnessSidecarModalityId,
): { state: HarnessSidecarStateKey; source: HarnessSidecarSourceKey } | null {
  const configured = policy?.[id].configured ?? null;
  if (!configured) return null;
  return {
    state: configured.enabled ? SIDECAR_CONFIGURED_STATE.enabled : SIDECAR_CONFIGURED_STATE.disabled,
    source: SIDECAR_CONFIGURED_SOURCE[configured.source],
  };
}

/**
 * A Harness carries its identity stamp only while it is applied and current.
 * Otherwise a saved override is not what requests actually resolve.
 */
export function harnessSidecarRequestActive(harness: {
  installed: boolean;
  applied: boolean;
  issue: HarnessIssue;
}): boolean {
  return harness.installed && harness.applied && harness.issue === "none";
}

export interface HarnessSidecarPatchBody {
  clientId: string;
  sidecars: Partial<Record<HarnessSidecarModalityId, HarnessSidecarMode | null>>;
}

/**
 * Omission and `null` mean different things to this API, so an edit carries only
 * the modality being changed and leaves the other stored override untouched.
 */
export function harnessSidecarPatchBody(
  clientId: HarnessId,
  id: HarnessSidecarModalityId,
  selection: HarnessSidecarSelection,
): HarnessSidecarPatchBody {
  const sidecars: HarnessSidecarPatchBody["sidecars"] = {};
  sidecars[id] = selection === "global" ? null : selection;
  return { clientId, sidecars };
}

export interface HarnessSidecarSave {
  apiBase: string;
  clientId: HarnessId;
  modality: HarnessSidecarModalityId;
  selection: HarnessSidecarSelection;
  /** Runs after success and failure: canonical server state is the only authority. */
  reload: () => Promise<void> | void;
}

export async function saveHarnessSidecarOverride(input: HarnessSidecarSave): Promise<void> {
  try {
    await readJsonOrThrow(
      await fetch(`${input.apiBase}/api/harnesses/settings`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(harnessSidecarPatchBody(input.clientId, input.modality, input.selection)),
      }),
      "harness sidecar policy could not be saved",
    );
  } finally {
    await input.reload();
  }
}
