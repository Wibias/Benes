/**
 * Dashboard reads against the Go listener's management API.
 *
 * Each read owns one surface and one failure policy: the overview clears on failure, the
 * catalog and usage throw so the client resource keeps the last good snapshot, the startup
 * probe reports an unreadable answer as `error`, and settings commit only while no mutation
 * moved the row underneath the poll.
 */
import { readAbandoned, readJsonIfOk, readRequiredJson } from "../fetch-json.ts";
import {
  beginPollEpoch,
  mapStartupHealthProbe,
  settingsPollMayCommit,
  type EpochRef,
  type SettingsPollWindow,
  type StartupHealthPayload,
  type StartupHealthProbe,
} from "../startup-health-ui.ts";
import type {
  HealthData,
  ModelInfo,
  ProviderCredentialRow,
  PublicProviderConfig,
  SettingsData,
  UsageSummary30d,
} from "./dashboard-shared.ts";

/**
 * One Dashboard provider row.
 *
 * `/api/providers` carries identity and credential presence; the adapter, base URL, and
 * default model live in the public per-provider config on `/api/config`. A provider the
 * public config does not describe stays present with those fields absent, and the board
 * renders a dash rather than an invented value.
 */
export type DashboardProviderRow = {
  name: string;
  hasApiKey: boolean;
  adapter?: string;
  baseUrl?: string;
  defaultModel?: string;
};

/**
 * One project-config warning row as this board renders it.
 *
 * The listener answers `/api/diagnostics/project-config` with `{ warnings, grouped }`, so a
 * path and the group that filed it are the whole row; the codex-home scan has no bypass note
 * to send.
 */
export interface ProjectCodexConfigGroup {
  path: string;
  issues: string[];
}

export type DashboardOverviewPoll = {
  health: HealthData | null;
  providers: DashboardProviderRow[];
  error: boolean;
};

/** Absent when the poll lost authority — callers must keep prior settings/cache. */
export type DashboardSettingsPoll = {
  settings: SettingsData | undefined;
};

/**
 * The settings poll's refs: the epoch counters come from the poll-policy owner, and the
 * write-in-flight flag (a boolean, not a counter) is declared here because only this page
 * allocates it.
 */
type InFlightRef = { current: boolean };

export type DashboardEpochRefs = {
  settingsRequestEpochRef: EpochRef;
  settingsMutationEpochRef: EpochRef;
  settingsMutationInFlightRef: InFlightRef;
};

/** The listener paths this module reads. Declared once so the poll surface stays auditable. */
const LISTENER_PATH = {
  startupHealth: "/api/startup-health",
  projectConfig: "/api/diagnostics/project-config",
  models: "/api/models",
  usage: "/api/usage?range=30d",
  providers: "/api/providers",
  providerConfig: "/api/config",
  healthz: "/healthz",
  settings: "/api/settings",
} as const;

function get(apiBase: string, path: string, signal: AbortSignal): Promise<Response> {
  return fetch(`${apiBase}${path}`, { signal });
}

/**
 * One listener read: the request, the status gate, and the empty-body rejection a required
 * JSON read needs. The gate belongs to the response decoder, not to each caller.
 */
async function readJson<T>(apiBase: string, path: string, signal: AbortSignal): Promise<T> {
  return readRequiredJson<T>(await get(apiBase, path, signal));
}

/**
 * `/api/startup-health`. The probe answers with the chip status plus whether the server is
 * still resolving a stale diagnostic; `stale` travels with the status so the caller can
 * re-ask in seconds rather than waiting for the next poll tick.
 *
 * An unreadable answer becomes `error` for the chip, but a read the caller abandoned does
 * not: that failure belongs to the abandoned generation, and reporting it as `error` briefly
 * shows "Could not read startup protection" after a refresh or remount race.
 */
export async function fetchStartupHealth(apiBase: string, signal: AbortSignal): Promise<StartupHealthProbe> {
  try {
    const payload = await readJson<StartupHealthPayload>(apiBase, LISTENER_PATH.startupHealth, signal);
    const status = mapStartupHealthProbe(payload);
    if (!status) throw new Error("invalid startup health response");
    return { status, stale: payload.diagnosticStale === true };
  } catch (failure) {
    if (readAbandoned(signal, failure)) throw failure;
    return { status: "error", stale: false };
  }
}

/** One warning path, filed under the group code that reported it. */
function configRow(raw: unknown, inheritedIssues: string[]): ProjectCodexConfigGroup | null {
  if (typeof raw !== "string") return null;
  const path = raw.trim();
  return path ? { path, issues: inheritedIssues } : null;
}

/**
 * `/api/diagnostics/project-config` rows, from the coded map or the flat list.
 *
 * `grouped` is the coded map and `warnings` is the uncoded list; either half may carry the
 * rows. Anything without a usable path is dropped rather than turned into an invented one,
 * because a fabricated entry reads as a real project-config problem.
 */
export function normalizeProjectConfigGroups(raw: unknown): ProjectCodexConfigGroup[] {
  const rowsFrom = (rows: unknown, issues: string[]): ProjectCodexConfigGroup[] => {
    if (!Array.isArray(rows)) return [];
    return rows.flatMap(row => {
      const group = configRow(row, issues);
      return group ? [group] : [];
    });
  };
  if (Array.isArray(raw)) return rowsFrom(raw, []);
  if (!raw || typeof raw !== "object") return [];
  return Object.entries(raw as Record<string, unknown>).flatMap(([code, rows]) =>
    rowsFrom(rows, code ? [code] : []),
  );
}

export async function fetchProjectConfigDiagnostics(
  apiBase: string,
  signal: AbortSignal,
): Promise<ProjectCodexConfigGroup[]> {
  try {
    const payload = await readJsonIfOk<{ grouped?: unknown; warnings?: unknown }>(
      await get(apiBase, LISTENER_PATH.projectConfig, signal),
    );
    return normalizeProjectConfigGroups(payload?.grouped ?? payload?.warnings);
  } catch {
    return [];
  }
}

/**
 * Throws on a non-OK or empty body so the catalog resource retains its prior snapshot
 * instead of reading an HTTP error as a successfully empty list.
 */
export async function fetchDashboardModels(apiBase: string, signal: AbortSignal): Promise<ModelInfo[]> {
  return readJson<ModelInfo[]>(apiBase, LISTENER_PATH.models, signal);
}

/**
 * Usage can be expensive. Keeping it in its own resource means it cannot delay
 * health/provider/settings commits, and a failed refresh retains the last good usage
 * snapshot. `/api/usage?range=30d` is the only window the dashboard shows.
 */
export async function fetchDashboardUsage(apiBase: string, signal: AbortSignal): Promise<UsageSummary30d> {
  return readJson<UsageSummary30d>(apiBase, LISTENER_PATH.usage, signal);
}

function publicProviderFields(config: PublicProviderConfig | undefined) {
  const fields: Pick<DashboardProviderRow, "adapter" | "baseUrl" | "defaultModel"> = {};
  if (typeof config?.adapter === "string" && config.adapter.trim()) fields.adapter = config.adapter;
  if (typeof config?.baseUrl === "string" && config.baseUrl.trim()) fields.baseUrl = config.baseUrl;
  if (typeof config?.defaultModel === "string" && config.defaultModel.trim()) {
    fields.defaultModel = config.defaultModel;
  }
  return fields;
}

/** Join `/api/providers` rows with the public per-provider config that presents them. */
export function mergeDashboardProviderRows(
  listed: ProviderCredentialRow[],
  configProviders: Record<string, PublicProviderConfig> | undefined,
): DashboardProviderRow[] {
  return listed.map(row => ({
    name: row.name,
    hasApiKey: row.hasApiKey === true,
    ...publicProviderFields(configProviders?.[row.name]),
  }));
}

/** `/healthz` plus `/api/providers` and `/api/config` — the rows the Overview KPIs read. */
export async function fetchDashboardOverview(
  apiBase: string,
  signal: AbortSignal,
): Promise<DashboardOverviewPoll> {
  try {
    const [healthResponse, providerResponse, configResponse] = await Promise.all([
      get(apiBase, LISTENER_PATH.healthz, signal),
      get(apiBase, LISTENER_PATH.providers, signal),
      get(apiBase, LISTENER_PATH.providerConfig, signal),
    ]);
    const health = await readRequiredJson<HealthData>(healthResponse);
    const listed = await readRequiredJson<ProviderCredentialRow[]>(providerResponse);
    // A missing public config is a degraded row set, not a failed overview: the providers
    // still exist, and the presentation fields stay dashes instead of invented values.
    const config = configResponse.ok
      ? await readJsonIfOk<{ providers?: Record<string, PublicProviderConfig> }>(configResponse)
      : undefined;
    return { health, providers: mergeDashboardProviderRows(listed, config?.providers), error: false };
  } catch {
    return { health: null, providers: [], error: true };
  }
}

/** The live settings epoch triple, in the poll-policy owner's own window type. */
function currentSettingsEpochs(epochs: DashboardEpochRefs): SettingsPollWindow {
  return {
    request: epochs.settingsRequestEpochRef.current,
    mutation: epochs.settingsMutationEpochRef.current,
    mutationInFlight: epochs.settingsMutationInFlightRef.current,
  };
}

/**
 * Codex auto-start and the listen address. The read commits only while neither the request
 * epoch nor the mutation epoch moved during it, so a slower answer cannot overwrite a row
 * the user just wrote.
 */
export async function fetchDashboardSettings(
  apiBase: string,
  signal: AbortSignal,
  epochs: DashboardEpochRefs,
): Promise<DashboardSettingsPoll> {
  const atFetch = beginPollEpoch(epochs.settingsRequestEpochRef, epochs.settingsMutationEpochRef);
  const nextSettings = await readJson<SettingsData>(apiBase, LISTENER_PATH.settings, signal);
  if (!settingsPollMayCommit(atFetch, currentSettingsEpochs(epochs))) return { settings: undefined };
  return { settings: nextSettings };
}

