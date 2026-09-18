import type { TKey } from "../../i18n/shared";
import {
  FILE_MANAGED_HARNESS_IDS,
  type FileManagedHarnessId,
  type HarnessAuth,
  type HarnessHistoryRow,
  type HarnessId,
  type HarnessIssue,
  type HarnessModelRegistration,
  type HarnessRecord,
  type HarnessSettings,
} from "./types.ts";
import type { HarnessProbe } from "./harness-api";
import type { SettingsMap } from "./settings-decode";
import type { IntegrationJournalRow, IntegrationStatus } from "../integrations/integration-api";
import type { NativeIntegrationClientId, NativeStatus } from "../integrations/native-api";
import { seedHarnesses } from "./catalog.ts";

const NATIVE_IDS = new Set<HarnessId>(["claude", "grok", "codex", "claude-desktop"]);
const FILE_IDS = new Set<string>(FILE_MANAGED_HARNESS_IDS);

const NOTE_BY_KIND: Record<IntegrationJournalRow["kind"], TKey> = {
  apply: "harnesses.note.userApply",
  disable: "harnesses.note.userDisable",
  refresh: "harnesses.note.refresh",
  restore: "harnesses.note.restore",
};

const FILE_ISSUE: Record<IntegrationStatus["state"], HarnessIssue> = {
  conflict: "conflict",
  unsafe: "conflict",
  stale: "update-needed",
  current: "none",
  absent: "none",
};

type OverlayFields = {
  installed: boolean;
  applied: boolean;
  issue: HarnessIssue;
  drift: boolean;
  configPath: string | null;
  snapshotId?: string | null;
  lastDetectedAt: string | null;
  lastAppliedAt?: string | null;
};

export type HarnessLiveBundle = {
  files: IntegrationStatus[];
  natives: NativeStatus[];
  journal: IntegrationJournalRow[];
  claude: { enabled: boolean; authMode?: string } | null;
  desktop: {
    desiredEnabled: boolean;
    installed: boolean;
    observedKind: string;
    applied: boolean;
    stale: boolean;
    activeProfile: boolean | null;
    appliedAt: string | null;
  } | null;
  /**
   * Server-projected model registration, keyed by the client the listener publishes it for.
   * A Harness that carries the Benes catalogue into its own config reports how much of that
   * projection is written; a Harness the listener says nothing about carries no entry.
   */
  registrations: Record<string, HarnessModelRegistration>;
  codex: { routingInjected: boolean; status?: string; recommendedCommand: string | null } | null;
  settings: SettingsMap;
  probes: HarnessProbe[];
};

export function isNativeId(id: HarnessId): id is NativeIntegrationClientId {
  return NATIVE_IDS.has(id);
}

export function isFileId(id: HarnessId): id is FileManagedHarnessId {
  return FILE_IDS.has(id);
}

function emptyAuth(): HarnessAuth {
  return {
    kind: "none",
    methodKey: "harnesses.auth.none",
    clientId: null,
    scopes: [],
    token: "none",
  };
}

function historyFromJournal(rows: IntegrationJournalRow[]): HarnessHistoryRow[] {
  return rows.map((row) => ({
    id: row.opId,
    kind: row.kind,
    snapshot: row.snapshot === "stored" ? row.opId : null,
    at: row.at,
    noteKey: NOTE_BY_KIND[row.kind],
    restorable: row.undoable && row.snapshot === "stored",
  }));
}

export function overlayAuth(
  seed: HarnessAuth,
  id: HarnessId,
  claude: HarnessLiveBundle["claude"],
): HarnessAuth {
  if (id !== "claude" && id !== "claude-desktop") {
    if (id === "codex") return { ...seed, token: "missing" };
    return emptyAuth();
  }
  if (claude?.authMode === "subscription") {
    return {
      kind: "oauth",
      methodKey: "harnesses.auth.claudeOAuth",
      clientId: seed.clientId,
      scopes: seed.scopes,
      token: "missing",
    };
  }
  if (claude?.authMode) {
    return {
      kind: "api-key",
      methodKey: "harnesses.auth.none",
      clientId: null,
      scopes: [],
      token: "missing",
    };
  }
  return { ...seed, token: seed.kind === "none" ? "none" : "missing" };
}

export function overlayFile(status: IntegrationStatus | undefined, detectedAt: string): OverlayFields {
  if (!status) {
    return {
      installed: false,
      applied: false,
      issue: "none",
      drift: false,
      configPath: null,
      snapshotId: null,
      lastDetectedAt: null,
      lastAppliedAt: null,
    };
  }
  const applied = status.installed && (status.state === "current" || status.state === "stale");
  return {
    installed: status.installed,
    applied,
    issue: FILE_ISSUE[status.state],
    drift: status.state === "stale",
    configPath: status.configPath || null,
    snapshotId: status.lastOpId ?? null,
    lastDetectedAt: detectedAt,
    lastAppliedAt: status.appliedAt ?? null,
  };
}

export function overlayNative(status: NativeStatus | undefined, detectedAt: string): OverlayFields {
  if (!status) {
    return {
      installed: false,
      applied: false,
      issue: "none",
      drift: false,
      configPath: null,
      lastDetectedAt: null,
    };
  }
  return {
    installed: status.installed,
    applied: status.state === "current",
    issue: status.state === "unsafe" ? "conflict" : "none",
    drift: false,
    configPath: status.configPath || null,
    lastDetectedAt: detectedAt,
  };
}

function overlayDesktopNative(
  seed: HarnessRecord,
  live: HarnessLiveBundle,
  fields: OverlayFields,
  detectedAt: string,
): { fields: OverlayFields; lastAppliedAt: string | null } {
  if (seed.id !== "claude-desktop" || !live.desktop) {
    return { fields, lastAppliedAt: null };
  }
  return {
    fields: {
      ...fields,
      installed: live.desktop.installed,
      applied: live.desktop.applied,
      issue: live.desktop.applied && live.desktop.stale ? "update-needed" : fields.issue,
      drift: live.desktop.applied && live.desktop.stale,
      lastDetectedAt: detectedAt,
    },
    lastAppliedAt: live.desktop.appliedAt,
  };
}

function overlayCodexNative(
  seed: HarnessRecord,
  live: HarnessLiveBundle,
  natives: ReadonlyMap<string, NativeStatus>,
  fields: OverlayFields,
  detectedAt: string,
): OverlayFields {
  if (seed.id !== "codex" || natives.has(seed.id) || !live.codex?.routingInjected) return fields;
  return {
    ...fields,
    installed: true,
    applied: true,
    lastDetectedAt: detectedAt,
  };
}

/** A published registration is presence truth for the Harness that carries it. */
function overlayModelRegistration(
  seed: HarnessRecord,
  live: HarnessLiveBundle,
  natives: ReadonlyMap<string, NativeStatus>,
  fields: OverlayFields,
  detectedAt: string,
): OverlayFields {
  const registration = live.registrations[seed.id];
  if (natives.has(seed.id) || !registration) return fields;
  return {
    ...fields,
    installed: registration.present,
    lastDetectedAt: detectedAt,
  };
}

function applyProbe(record: HarnessRecord, probe: HarnessProbe | undefined, settings: HarnessSettings): HarnessRecord {
  return {
    ...record,
    detectPath: probe?.detectPath ?? null,
    logPath: probe?.logPath ?? null,
    running: probe ? probe.running : null,
    settings: probe ? { ...settings, ...probe.settings } : settings,
    sidecarPolicy: probe?.sidecarPolicy ?? null,
    auth: {
      ...record.auth,
      token: probe?.token ?? (record.auth.kind === "none" ? "none" : "missing"),
    },
  };
}

export function overlaySeed(
  seed: HarnessRecord,
  live: HarnessLiveBundle,
  files: ReadonlyMap<string, IntegrationStatus>,
  natives: ReadonlyMap<string, NativeStatus>,
  journalByClient: ReadonlyMap<string, IntegrationJournalRow[]>,
  probes: ReadonlyMap<string, HarnessProbe>,
  detectedAt: string,
): HarnessRecord {
  const probe = probes.get(seed.id);
  const settings = { ...seed.settings, ...live.settings[seed.id] };
  const auth = overlayAuth(seed.auth, seed.id, live.claude);
  const journal = journalByClient.get(seed.id) ?? [];
  const history = journal.length > 0 ? historyFromJournal(journal) : [];
  if (isFileId(seed.id)) {
    return applyProbe({
      ...seed,
      ...overlayFile(files.get(seed.id), detectedAt),
      running: null,
      registration: live.registrations[seed.id] ?? seed.registration ?? null,
      auth,
      settings,
      history,
    }, probe, settings);
  }
  const nativeBase = overlayNative(natives.get(seed.id), detectedAt);
  const desktop = overlayDesktopNative(seed, live, nativeBase, detectedAt);
  const withCodex = overlayCodexNative(seed, live, natives, desktop.fields, detectedAt);
  const liveFields = overlayModelRegistration(seed, live, natives, withCodex, detectedAt);
  return applyProbe({
    ...seed,
    ...liveFields,
    running: null,
    registration: live.registrations[seed.id] ?? seed.registration ?? null,
    lastAppliedAt: desktop.lastAppliedAt ?? seed.lastAppliedAt,
    auth,
    settings,
    history,
  }, probe, settings);
}

export function overlayHarnessesAt(live: HarnessLiveBundle, detectedAt: string): HarnessRecord[] {
  const files = new Map(live.files.map((row) => [row.clientId, row]));
  const natives = new Map(live.natives.map((row) => [row.clientId, row]));
  const journalByClient = new Map<string, IntegrationJournalRow[]>();
  for (const row of live.journal) {
    const list = journalByClient.get(row.clientId) ?? [];
    list.push(row);
    journalByClient.set(row.clientId, list);
  }
  const probes = new Map((live.probes ?? []).map((row) => [row.clientId, row]));
  return seedHarnesses().map((seed) => overlaySeed(seed, live, files, natives, journalByClient, probes, detectedAt));
}
