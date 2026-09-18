import { useCallback } from "react";
import { useDataSurface } from "./data-surface";
import { readJsonOrThrow } from "./fetch-json";
import { loadHarnessBoard } from "./pages/harnesses/live";
import { parseProviderWorkspaceAggregate } from "./provider-workspace/workspace";
import type { ProvidersConfig } from "./pages/providers-shared";
import {
  harnessBoardResourceKey,
  harnessBoardSessionKey,
  providersConfigCacheKey,
  providersWorkspaceCacheKey,
  subagentsDelegationSessionKey,
  subagentsModelsResourceKey,
  combosWorkspaceCacheKey,
  labMatrixResourceKey,
  fabricTasksResourceKey,
} from "./nav-board-resources";
import { loadDelegationSnapshot, loadSubagentsModels, loadCombosWorkspace, loadLabMatrixPage } from "./nav-board-loads";
import { fetchFabricTasks } from "./pages/tasks-api";

function useFabricTasksPrefetch(apiBase: string, fabricEnabled: boolean) {
  const loadTasks = useCallback((signal: AbortSignal) => fetchFabricTasks(apiBase, signal), [apiBase]);
  useDataSurface(fabricTasksResourceKey(apiBase), [apiBase], loadTasks, {
    enabled: fabricEnabled,
    isEmpty: tasks => tasks.length === 0,
  });
}

export function useNavBoardPrefetch(apiBase: string, fabricEnabled = false) {
  const loadHarness = useCallback((signal: AbortSignal) => loadHarnessBoard(apiBase, signal), [apiBase]);
  useDataSurface(harnessBoardResourceKey(apiBase), [apiBase], loadHarness, {
    isEmpty: () => false,
    sessionCacheKey: harnessBoardSessionKey(apiBase),
  });

  const loadModels = useCallback((signal: AbortSignal) => loadSubagentsModels(apiBase, signal), [apiBase]);
  useDataSurface(subagentsModelsResourceKey(apiBase), [apiBase], loadModels, {
    isEmpty: () => false,
    sessionCacheKey: subagentsModelsResourceKey(apiBase),
  });

  const loadDelegation = useCallback((signal: AbortSignal) => loadDelegationSnapshot(apiBase, signal), [apiBase]);
  useDataSurface(subagentsDelegationSessionKey(apiBase), [apiBase], loadDelegation, {
    isEmpty: () => false,
    sessionCacheKey: subagentsDelegationSessionKey(apiBase),
  });

  const loadConfig = useCallback(async (signal: AbortSignal) => {
    const res = await fetch(`${apiBase}/api/config`, { signal });
    const data = await readJsonOrThrow<ProvidersConfig>(res);
    if (!data) throw new Error("providers-config-unavailable");
    return data;
  }, [apiBase]);
  useDataSurface(providersConfigCacheKey(apiBase), [apiBase], loadConfig, {
    isEmpty: () => false,
    sessionCacheKey: providersConfigCacheKey(apiBase),
  });

  const loadWorkspace = useCallback(async (signal: AbortSignal) => {
    const res = await fetch(`${apiBase}/api/providers/workspace`, { signal });
    const parsed = parseProviderWorkspaceAggregate(await readJsonOrThrow(res));
    if (!parsed) throw new Error("providers-workspace-unavailable");
    return parsed;
  }, [apiBase]);
  useDataSurface(providersWorkspaceCacheKey(apiBase), [apiBase], loadWorkspace, {
    isEmpty: () => false,
    sessionCacheKey: providersWorkspaceCacheKey(apiBase),
  });

  const loadCombos = useCallback((signal: AbortSignal) => loadCombosWorkspace(apiBase, signal), [apiBase]);
  useDataSurface(combosWorkspaceCacheKey(apiBase), [apiBase], loadCombos, {
    isEmpty: () => false,
    sessionCacheKey: combosWorkspaceCacheKey(apiBase),
  });

  const loadLab = useCallback((signal: AbortSignal) => loadLabMatrixPage(apiBase, signal), [apiBase]);
  useDataSurface(labMatrixResourceKey(apiBase), [apiBase], loadLab, {
    isEmpty: data => data.verdicts.length === 0,
  });

  useFabricTasksPrefetch(apiBase, fabricEnabled);
}
