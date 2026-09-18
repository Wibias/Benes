/**
 * Listener reads behind the Providers board.
 *
 * The board draws on four independent reads: the workspace aggregate (fleet
 * lifecycle), the selected-model snapshot, 30-day usage, and the per-provider
 * quota reports. Each read owns its own session cache and its own reload trigger,
 * so refreshing one never fans out into a duplicate read of another.
 */
import { useCallback, useEffect, useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { useKeyedClientResource } from "../../client-resource";
import { usageSummary30dResourceKey } from "../../usage-summary-resource";
import { readJsonIfOk, readJsonOrThrow } from "../../fetch-json";
import { clearSessionListCache, readSessionListCache, writeSessionListCache } from "../../session-list-cache";
import { providersWorkspaceCacheKey } from "../../nav-board-resources";
import { startVisibilityPoll } from "../../visibility-poll";
import {
  countAvailableModels,
  decodeModelSelection,
  type ProviderAvailableModels,
  type ProviderLiveModelCounts,
  type ProviderSelectedModels,
  type ProviderUsageTotals,
} from "../../provider-workspace/model-selection";
import type { ProviderQuotaReportView } from "../../provider-workspace/report";
import {
  parseProviderWorkspaceAggregate,
  type ProviderWorkspaceAggregate,
} from "../../provider-workspace/workspace";
import { authoritativeWorkspaceRefreshFailure } from "../../provider-workspace/workspace-refresh-state";
import {
  freshQuotaReportsFromResponse,
  quotaReportsFromCache,
  usageModelsFromResponse,
  usageTotalsFromResponse,
  type ShellUsageModelRow,
} from "../../provider-workspace/workspace-shell";

const QUOTA_IDLE_POLL_MS = 30_000;
const WORKSPACE_IDLE_POLL_MS = 30_000;
const QUOTA_SESSION_KEY_PREFIX = "benes.providers.quotas.v1";
const USAGE_SESSION_KEY_PREFIX = "benes.providers.usage.v1";

export interface WorkspaceAggregateRead {
  workspace: ProviderWorkspaceAggregate | null;
  /** The last aggregate read failed; cached lifecycle truth is not authoritative. */
  failed: boolean;
  reload: () => void;
  bumpEpoch: Dispatch<SetStateAction<number>>;
}

/** Fleet lifecycle. The route is authoritative, so a rejected read discards the cache. */
export function useWorkspaceAggregateRead(input: {
  apiBase: string;
  revisionKey: string;
  refreshToken: number;
}): WorkspaceAggregateRead {
  const { apiBase, revisionKey, refreshToken } = input;
  const cacheKey = useMemo(() => providersWorkspaceCacheKey(apiBase), [apiBase]);
  const [workspace, setWorkspace] = useState<ProviderWorkspaceAggregate | null>(
    () => parseProviderWorkspaceAggregate(readSessionListCache<unknown>(cacheKey)),
  );
  const [failed, setFailed] = useState(false);
  const [epoch, setEpoch] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const applyFailure = () => {
      const failure = authoritativeWorkspaceRefreshFailure();
      setWorkspace(failure.workspace);
      setFailed(failure.failed);
      if (failure.discardCache) clearSessionListCache(cacheKey);
    };
    void (async () => {
      try {
        const response = await fetch(`${apiBase}/api/providers/workspace`);
        const parsed = parseProviderWorkspaceAggregate(await readJsonOrThrow(response));
        if (cancelled) return;
        if (!parsed) {
          applyFailure();
          return;
        }
        setWorkspace(parsed);
        setFailed(false);
        writeSessionListCache(cacheKey, parsed);
      } catch {
        if (!cancelled) applyFailure();
      }
    })();
    return () => { cancelled = true; };
  }, [apiBase, cacheKey, revisionKey, refreshToken, epoch]);

  useEffect(() => startVisibilityPoll(() => setEpoch(current => current + 1), WORKSPACE_IDLE_POLL_MS), []);
  const reload = useCallback(() => setEpoch(current => current + 1), []);
  return { workspace, failed, reload, bumpEpoch: setEpoch };
}

export interface ModelSelectionRead {
  modelCounts: Record<string, number>;
  available: ProviderAvailableModels;
  liveCounts: ProviderLiveModelCounts;
  selected: ProviderSelectedModels;
  loading: boolean;
  failed: boolean;
  bumpEpoch: Dispatch<SetStateAction<number>>;
}

/**
 * Selected-model snapshot. Deferred by a microtask so a mount cannot block the
 * first paint, and the whole snapshot is one state value because the three maps
 * always come from the same response.
 */
export function useModelSelectionRead(input: {
  apiBase: string;
  refreshToken: number;
}): ModelSelectionRead {
  const { apiBase, refreshToken } = input;
  const [modelCounts, setModelCounts] = useState<Record<string, number>>({});
  const [snapshot, setSnapshot] = useState(() => decodeModelSelection(null));
  const [loading, setLoading] = useState(false);
  const [failed, setFailed] = useState(false);
  const [epoch, setEpoch] = useState(0);

  useEffect(() => {
    let cancelled = false;
    const timer = window.setTimeout(() => {
      setLoading(true);
      void (async () => {
        try {
          const data = await readJsonOrThrow(await fetch(`${apiBase}/api/selected-models`));
          if (cancelled) return;
          setModelCounts(countAvailableModels(data));
          setSnapshot(decodeModelSelection(data));
          setFailed(false);
        } catch {
          if (!cancelled) setFailed(true);
        } finally {
          if (!cancelled) setLoading(false);
        }
      })();
    }, 0);
    return () => {
      cancelled = true;
      window.clearTimeout(timer);
    };
  }, [apiBase, refreshToken, epoch]);

  return {
    modelCounts,
    available: snapshot.available,
    liveCounts: snapshot.liveCounts,
    selected: snapshot.selected,
    loading,
    failed,
    bumpEpoch: setEpoch,
  };
}

interface UsagePayload {
  providers?: Array<{ provider: string; requests: number; totalTokens?: number }>;
  models?: Array<{
    provider: string;
    model: string;
    resolvedModel?: string;
    requests: number;
    totalTokens: number;
    inputTokens: number;
    outputTokens: number;
    shareRatio: number;
    estimatedCostUsd?: number;
  }>;
}

export interface ProviderUsageRead {
  totals: Record<string, ProviderUsageTotals>;
  models: Record<string, ShellUsageModelRow[]>;
}

function usageSessionKey(apiBase: string): string {
  return `${USAGE_SESSION_KEY_PREFIX}:${apiBase}`;
}

/** 30-day usage indexed for the detail panes; the last-good cached slice paints first. */
export function useProviderUsageRead(apiBase: string): ProviderUsageRead {
  const cacheKey = usageSessionKey(apiBase);
  const cached = useMemo(
    () => readSessionListCache<ProviderUsageRead>(cacheKey),
    [cacheKey],
  );
  const [usage, setUsage] = useState<ProviderUsageRead>(() => ({
    totals: cached?.totals ?? {},
    models: cached?.models ?? {},
  }));
  const resource = useKeyedClientResource(usageSummary30dResourceKey(apiBase), [apiBase], async (signal) => {
    const response = await fetch(apiBase + "/api/usage?range=30d", { signal });
    if (!response.ok) throw new Error(String(response.status));
    return await response.json();
  }, { deadlineMs: 60_000 });

  useEffect(() => {
    // Deferred by a microtask so the publish lands outside the effect body, which is
    // the shape react-doctor can verify, and so a burst of resource updates collapses
    // into the last one instead of rendering once per update.
    const timer = window.setTimeout(() => {
      const payload = resource.data as UsagePayload | undefined;
      if (!payload) return;
      const totals = usageTotalsFromResponse(payload.providers);
      const models = usageModelsFromResponse(payload.models);
      setUsage({ totals, models });
      writeSessionListCache(cacheKey, { totals, models });
    }, 0);
    return () => window.clearTimeout(timer);
  }, [cacheKey, resource.data]);

  return usage;
}

export interface ProviderQuotaRead {
  reports: Record<string, ProviderQuotaReportView>;
  /** `force` asks for a fresh upstream read; `refreshWorkspaceAfter` re-reads the aggregate. */
  read: (force: boolean, refreshWorkspaceAfter: boolean) => Promise<void>;
}

function quotaSessionKey(apiBase: string): string {
  return `${QUOTA_SESSION_KEY_PREFIX}:${apiBase}`;
}

/** Quota reports keyed by provider, with the last-good slice kept when a read fails. */
export function useProviderQuotaRead(input: {
  apiBase: string;
  forceRefresh: boolean;
  refreshEpoch: number;
  onInvalidated: () => void;
}): ProviderQuotaRead {
  const { apiBase, forceRefresh, refreshEpoch, onInvalidated } = input;
  const cacheKey = quotaSessionKey(apiBase);
  const [reports, setReports] = useState<Record<string, ProviderQuotaReportView>>(
    () => quotaReportsFromCache(readSessionListCache<unknown>(cacheKey)) ?? {},
  );

  const read = useCallback(async (force: boolean, refreshWorkspaceAfter: boolean) => {
    try {
      const response = await fetch(`${apiBase}/api/provider-quotas${force ? "?refresh=1" : ""}`);
      const payload = await readJsonIfOk<{ reports?: unknown[] }>(response);
      if (payload == null) return;
      const next = freshQuotaReportsFromResponse(payload);
      setReports(next);
      writeSessionListCache(cacheKey, next);
      // Only an explicit quota invalidation needs an immediate aggregate re-read.
      // Passive quota polling and the workspace's own poll stay independent so a
      // single timer tick cannot fan out into duplicate workspace GETs.
      if (refreshWorkspaceAfter) onInvalidated();
    } catch {
      // Keep last-good quota reports and workspace truth.
    }
  }, [apiBase, cacheKey, onInvalidated]);

  useEffect(() => {
    const timer = window.setTimeout(() => { void read(forceRefresh, refreshEpoch > 0); }, 0);
    return () => window.clearTimeout(timer);
  }, [refreshEpoch, forceRefresh, read]);

  useEffect(
    () => startVisibilityPoll(() => { void read(false, false); }, QUOTA_IDLE_POLL_MS),
    [read],
  );

  return { reports, read };
}
