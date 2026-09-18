import { useEffect, useMemo, useState } from "react";
import { useKeyedClientResource } from "../client-resource";
import { useI18n } from "../i18n/shared";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache";
import {
  PROJECT_CONFIG_DIAGNOSTICS_POLL_MS,
  STARTUP_HEALTH_STALE_RETRY_MS,
  probeNeedsFastRetry,
  type StartupHealthProbe,
  type StartupHealthStatus,
} from "../startup-health-ui";
import { usageSummary30dResourceKey } from "../usage-summary-resource";
import {
  fetchDashboardModels,
  fetchDashboardOverview,
  fetchDashboardSettings,
  fetchDashboardUsage,
  fetchProjectConfigDiagnostics,
  fetchStartupHealth,
  type DashboardEpochRefs,
  type DashboardOverviewPoll,
  type DashboardProviderRow,
  type DashboardSettingsPoll,
} from "./dashboard-core-poll";
import {
  filterDashboardModelGroups,
  groupDashboardModels,
} from "./dashboard-model-views";
import {
  readDashboardSectionFromHash,
  type DashboardSection,
} from "./dashboard-tab-nav";
import {
  type HealthData,
  type ModelInfo,
  type SettingsData,
  type UsageSummary30d,
} from "./dashboard-shared";

const OVERVIEW_CACHE = "benes.dash.overview.v1:";
const USAGE_CACHE = "benes.dash.usage30d.v1:";
const STARTUP_CACHE = "benes.dash.startup.v1:";
const CONTROLS_CACHE = "benes.dash.controls.v1:";

type CachedOverview = {
  health: HealthData;
  providers: DashboardProviderRow[];
};

type CachedControls = {
  settings?: SettingsData | null;
};

type SettingsSnapshot = DashboardSettingsPoll;
type ProbeSnapshot = StartupHealthProbe;

const EMPTY_MODELS: ModelInfo[] = [];

function idleEpochRefs(): DashboardEpochRefs {
  return {
    settingsRequestEpochRef: { current: 0 },
    settingsMutationEpochRef: { current: 0 },
    settingsMutationInFlightRef: { current: false },
  };
}

function persistOverview(apiBase: string, health: HealthData, providers: DashboardProviderRow[]) {
  writeSessionListCache(`${OVERVIEW_CACHE}${apiBase}`, { health, providers });
}

function persistUsage(apiBase: string, usage: UsageSummary30d) {
  writeSessionListCache(`${USAGE_CACHE}${apiBase}`, usage);
}

function persistStartup(apiBase: string, status: StartupHealthStatus) {
  writeSessionListCache(`${STARTUP_CACHE}${apiBase}`, status);
}

function persistSettings(apiBase: string, settings: SettingsData) {
  const previous = readSessionListCache<CachedControls>(`${CONTROLS_CACHE}${apiBase}`) ?? {};
  writeSessionListCache(`${CONTROLS_CACHE}${apiBase}`, { ...previous, settings });
}

function cachedStartupStatus(apiBase: string): StartupHealthStatus | null {
  const cached = readSessionListCache<StartupHealthStatus>(`${STARTUP_CACHE}${apiBase}`);
  return cached === "error" ? null : cached;
}

function overviewSeed(cached: CachedOverview | null): DashboardOverviewPoll | undefined {
  if (!cached) return undefined;
  return { health: cached.health, providers: cached.providers, error: false };
}

function startupSeed(cached: StartupHealthStatus | null): ProbeSnapshot | undefined {
  if (!cached) return undefined;
  return { status: cached, stale: false };
}

function settingsSeed(cached: SettingsData | null): SettingsSnapshot | undefined {
  if (!cached) return undefined;
  return { settings: cached };
}

type DashboardResourceSpec<T> = {
  identity: string;
  watch: readonly unknown[];
  load: (signal: AbortSignal) => Promise<T>;
  intervalMs?: number;
  active?: boolean;
  timeoutMs?: number;
  hideSafe?: boolean;
  seed?: T;
};

function useDashboardResource<T>(spec: DashboardResourceSpec<T>) {
  return useKeyedClientResource(spec.identity, spec.watch, spec.load, {
    pollMs: spec.intervalMs,
    enabled: spec.active,
    deadlineMs: spec.timeoutMs,
    pauseWhenHidden: spec.hideSafe,
    initialData: spec.seed,
  });
}

function useDashboardSection() {
  const [selectedSection, setSelectedSection] = useState<DashboardSection>(readDashboardSectionFromHash);
  useEffect(() => {
    const onHash = () => setSelectedSection(readDashboardSectionFromHash());
    window.addEventListener("hashchange", onHash);
    return () => window.removeEventListener("hashchange", onHash);
  }, []);
  return selectedSection;
}

function useOverviewSnapshot(apiBase: string) {
  const cached = useMemo(
    () => readSessionListCache<CachedOverview>(`${OVERVIEW_CACHE}${apiBase}`),
    [apiBase],
  );
  const poll = useDashboardResource({
    identity: `dashboard-overview:${apiBase}`,
    watch: [apiBase],
    load: (signal) => fetchDashboardOverview(apiBase, signal),
    intervalMs: 5000,
    seed: overviewSeed(cached),
  });
  useEffect(() => {
    const snapshot = poll.data;
    if (snapshot?.health) persistOverview(apiBase, snapshot.health, snapshot.providers);
  }, [poll.data, apiBase]);
  const health = poll.data?.health ?? null;
  const providers = poll.data?.providers ?? [];
  const error = poll.data?.error === true;
  return {
    health,
    providers,
    error,
    overviewReady: health !== null || poll.data !== undefined,
  };
}

function useStartupHealthSnapshot(apiBase: string) {
  const [epochs] = useState(idleEpochRefs);
  const cached = useMemo(() => cachedStartupStatus(apiBase), [apiBase]);
  const poll = useDashboardResource({
    identity: `dashboard-startup-health:${apiBase}`,
    watch: [apiBase],
    load: (signal): Promise<ProbeSnapshot> => fetchStartupHealth(apiBase, signal),
    intervalMs: 30_000,
    seed: startupSeed(cached),
  });
  const refresh = poll.refresh;
  const stale = probeNeedsFastRetry(poll.data);
  useEffect(() => {
    if (!stale) return;
    const timer = window.setTimeout(() => { void refresh(); }, STARTUP_HEALTH_STALE_RETRY_MS);
    return () => window.clearTimeout(timer);
  }, [stale, refresh]);
  useEffect(() => {
    const probe = poll.data;
    if (!probe) return;
    if (probe.status !== "error" && !probe.stale) persistStartup(apiBase, probe.status);
  }, [poll.data, apiBase]);
  useEffect(() => () => {
    epochs.settingsRequestEpochRef.current += 1;
  }, [epochs]);

  const cachedSettings = useMemo(
    () => readSessionListCache<CachedControls>(`${CONTROLS_CACHE}${apiBase}`)?.settings ?? null,
    [apiBase],
  );
  const settingsPoll = useDashboardResource({
    identity: `dashboard-settings:${apiBase}`,
    watch: [apiBase],
    load: (signal): Promise<SettingsSnapshot> => fetchDashboardSettings(apiBase, signal, epochs),
    intervalMs: 5000,
    seed: settingsSeed(cachedSettings),
  });
  useEffect(() => {
    const settings = settingsPoll.data?.settings;
    if (settings) persistSettings(apiBase, settings);
  }, [settingsPoll.data, apiBase]);

  return {
    // `/api/startup-health` is the only owner of this state; `/api/settings` never carries it.
    startupHealth: poll.data?.status ?? cached,
    settings: settingsPoll.data?.settings ?? cachedSettings,
  };
}

function useUsageSnapshot(apiBase: string, overviewReady: boolean) {
  const cached = useMemo(
    () => readSessionListCache<UsageSummary30d>(`${USAGE_CACHE}${apiBase}`),
    [apiBase],
  );
  const poll = useDashboardResource({
    identity: usageSummary30dResourceKey(apiBase),
    watch: [apiBase],
    load: (signal) => fetchDashboardUsage(apiBase, signal),
    active: overviewReady,
    timeoutMs: 60_000,
    seed: cached ?? undefined,
  });
  useEffect(() => {
    if (poll.data !== undefined) persistUsage(apiBase, poll.data);
  }, [poll.data, apiBase]);
  return poll.data ?? null;
}

function useModelsCatalog(apiBase: string, overviewReady: boolean, error: boolean) {
  const [modelQuery, setModelQuery] = useState("");
  const [expandedProviders, setExpandedProviders] = useState<Set<string>>(new Set());
  const poll = useDashboardResource({
    identity: `dashboard-models:${apiBase}`,
    watch: [apiBase, error],
    load: (signal) => fetchDashboardModels(apiBase, signal),
    active: overviewReady && !error,
  });
  const models = poll.data ?? EMPTY_MODELS;
  const filteredGroups = useMemo(
    () => filterDashboardModelGroups(groupDashboardModels(models), modelQuery),
    [models, modelQuery],
  );
  return {
    models,
    modelsLoading: poll.loading,
    modelQuery,
    setModelQuery,
    expandedProviders,
    setExpandedProviders,
    filteredGroups,
  };
}

function useProjectConfigWarnings(apiBase: string, overviewReady: boolean) {
  const poll = useDashboardResource({
    identity: `dashboard-diagnostics:${apiBase}`,
    watch: [apiBase],
    load: (signal) => fetchProjectConfigDiagnostics(apiBase, signal),
    intervalMs: PROJECT_CONFIG_DIAGNOSTICS_POLL_MS,
    active: overviewReady,
  });
  return poll.data ?? [];
}

export function useDashboardData(apiBase: string) {
  const { locale, t } = useI18n();
  const selectedSection = useDashboardSection();
  const overview = useOverviewSnapshot(apiBase);
  const { startupHealth, settings } = useStartupHealthSnapshot(apiBase);
  const usage30d = useUsageSnapshot(apiBase, overview.overviewReady);
  const catalog = useModelsCatalog(apiBase, overview.overviewReady, overview.error);
  const projectConfigWarnings = useProjectConfigWarnings(apiBase, overview.overviewReady);
  return {
    locale,
    t,
    selectedSection,
    health: overview.health,
    providers: overview.providers,
    error: overview.error,
    startupHealth,
    settings,
    usage30d,
    projectConfigWarnings,
    ...catalog,
  };
}
