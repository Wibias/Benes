import type { HarnessId, HarnessSettings } from "./types";

const HARNESS_IDS = new Set<string>([
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
]);

export type SettingsMap = Partial<Record<HarnessId, Partial<HarnessSettings>>>;

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function isHarnessId(value: string): value is HarnessId {
  return HARNESS_IDS.has(value);
}

function optionalBoolean(value: unknown): boolean | undefined {
  return typeof value === "boolean" ? value : undefined;
}

function decodeHarnessSettings(value: unknown): Partial<HarnessSettings> | null {
  if (!isRecord(value)) return null;
  const next: Partial<HarnessSettings> = {};
  const autoDetect = optionalBoolean(value.autoDetect);
  const autoApply = optionalBoolean(value.autoApply);
  const retainSnapshot = optionalBoolean(value.retainSnapshot);
  const allowRestart = optionalBoolean(value.allowRestart);
  if (autoDetect !== undefined) next.autoDetect = autoDetect;
  if (autoApply !== undefined) next.autoApply = autoApply;
  if (retainSnapshot !== undefined) next.retainSnapshot = retainSnapshot;
  if (allowRestart !== undefined) next.allowRestart = allowRestart;
  return next;
}

/** Session JSON is untrusted. Only known harness ids and boolean settings survive. */
export function decodeStoredSettings(raw: unknown): SettingsMap {
  if (!isRecord(raw)) return {};
  const next: SettingsMap = {};
  for (const [id, value] of Object.entries(raw)) {
    if (!isHarnessId(id)) continue;
    const settings = decodeHarnessSettings(value);
    if (settings) next[id] = settings;
  }
  return next;
}
