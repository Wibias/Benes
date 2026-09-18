import type { HarnessId, HarnessSettings } from "./types";
import {
  loadClaudeCodeStatus,
  loadClaudeDesktopStatus,
  loadCodexRoutingStatus,
  loadGrokFenceStatus,
  loadIntegrationJournal,
  loadIntegrationStates,
  restoreIntegration,
  toggleIntegration,
  type IntegrationJournalRow,
} from "../integrations/integration-api";
import { loadNativeIntegrations, toggleNativeIntegration } from "../integrations/native-api";
import { loadHarnessProbes, saveHarnessSettings } from "./harness-api";
import {
  isFileId,
  isNativeId,
  overlayHarnessesAt,
  type HarnessLiveBundle,
} from "./live-overlay";
import { decodeStoredSettings, type SettingsMap } from "./settings-decode";

export type { HarnessLiveBundle };

const SETTINGS_KEY = "benes.harnesses.settings.v1";

function defaultSettings(): HarnessSettings {
  return {
    autoDetect: true,
    autoApply: true,
    retainSnapshot: true,
    allowRestart: false,
  };
}

export function readStoredSettings(): SettingsMap {
  try {
    const raw = localStorage.getItem(SETTINGS_KEY);
    if (!raw) return {};
    const parsed: unknown = JSON.parse(raw);
    return decodeStoredSettings(parsed);
  } catch {
    return {};
  }
}

export function persistSettings(id: HarnessId, patch: Partial<HarnessSettings>): SettingsMap {
  const current = readStoredSettings();
  const next: SettingsMap = {
    ...current,
    [id]: { ...(current[id] ?? defaultSettings()), ...patch },
  };
  localStorage.setItem(SETTINGS_KEY, JSON.stringify(next));
  return next;
}

export async function persistSettingsRemote(
  apiBase: string,
  id: HarnessId,
  patch: Partial<HarnessSettings>,
): Promise<SettingsMap> {
  const next = persistSettings(id, patch);
  await saveHarnessSettings(apiBase, id, { ...defaultSettings(), ...next[id] });
  return next;
}

export function overlayHarnesses(live: HarnessLiveBundle) {
  return overlayHarnessesAt(live, new Date().toISOString());
}

export function emptyLive(): HarnessLiveBundle {
  return {
    files: [],
    natives: [],
    journal: [],
    claude: null,
    desktop: null,
    registrations: {},
    codex: null,
    settings: readStoredSettings(),
    probes: [],
  };
}

export async function loadHarnessBoard(apiBase: string, signal?: AbortSignal) {
  const [files, natives, journal, claude, desktop, grok, codex, probes] = await Promise.all([
    loadIntegrationStates(apiBase, signal).then((body) => body.clients).catch(() => null),
    loadNativeIntegrations(apiBase, signal).then((body) => body?.clients ?? []).catch(() => null),
    loadIntegrationJournal(apiBase, undefined, signal).then((body) => body.operations).catch(() => [] as IntegrationJournalRow[]),
    loadClaudeCodeStatus(apiBase, signal).catch(() => null),
    loadClaudeDesktopStatus(apiBase, signal).catch(() => null),
    loadGrokFenceStatus(apiBase, signal).catch(() => null),
    loadCodexRoutingStatus(apiBase, signal).catch(() => null),
    loadHarnessProbes(apiBase, signal).catch(() => []),
  ]);
  if (files === null && natives === null) {
    throw new Error("harness-status-unavailable");
  }
  return overlayHarnesses({
    files: files ?? [],
    natives: natives ?? [],
    journal,
    claude,
    desktop,
    // Keyed by the client the listener published it for; the id is the endpoint's, not ours.
    registrations: grok?.registration ? { grok: grok.registration } : {},
    codex,
    settings: readStoredSettings(),
    probes,
  });
}

export async function applyHarnessRemote(apiBase: string, id: HarnessId): Promise<void> {
  if (isNativeId(id)) {
    await toggleNativeIntegration(apiBase, id, true);
    return;
  }
  if (isFileId(id)) {
    await toggleIntegration(apiBase, id, true);
  }
}

export async function disableHarnessRemote(apiBase: string, id: HarnessId): Promise<void> {
  if (isNativeId(id)) {
    await toggleNativeIntegration(apiBase, id, false);
    return;
  }
  if (isFileId(id)) {
    await toggleIntegration(apiBase, id, false);
  }
}

export async function refreshHarnessRemote(apiBase: string, id: HarnessId): Promise<void> {
  await applyHarnessRemote(apiBase, id);
}

export async function restoreHarnessRemote(apiBase: string, opId: string): Promise<void> {
  await restoreIntegration(apiBase, opId);
}
