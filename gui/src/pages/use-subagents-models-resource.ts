/** Models roster resource for the Subagents board. */
import { useCallback, useRef, useState } from "react";
import { readSessionListCache, writeSessionListCache } from "../session-list-cache.ts";
import { useDataSurface, type DataSurfaceResource } from "../data-surface.ts";
import { loadSubagentsModels, type SubagentsModelsSnapshot } from "../nav-board-loads.ts";
import { subagentsModelsResourceKey } from "../nav-board-resources.ts";

type RosterSnapshot = SubagentsModelsSnapshot;

function readRosterSeed(cacheKey: string): RosterSnapshot | null {
  return readSessionListCache<RosterSnapshot>(cacheKey);
}

function writeRosterSeed(cacheKey: string, snapshot: RosterSnapshot): void {
  writeSessionListCache(cacheKey, snapshot);
}

export type SubagentsModelsResource = {
  cacheKey: string;
  chosen: string[];
  setChosen: (models: string[]) => void;
  available: string[];
  snapshot: RosterSnapshot | null | undefined;
  resource: DataSurfaceResource<RosterSnapshot>;
  loadSubagents: (signal?: AbortSignal) => Promise<RosterSnapshot>;
  persistGate: { current: boolean };
};

export function useSubagentsModelsResource(apiBase: string): SubagentsModelsResource {
  const cacheKey = subagentsModelsResourceKey(apiBase);
  const seed = readRosterSeed(cacheKey);
  const [chosen, setChosen] = useState<string[]>(() => seed?.chosen ?? []);
  const persistGate = useRef(false);

  const loadSubagents = useCallback(async (signal?: AbortSignal): Promise<RosterSnapshot> => {
    const live = await loadSubagentsModels(apiBase, signal);
    if (!persistGate.current) {
      setChosen(live.chosen);
      writeRosterSeed(cacheKey, live);
    }
    return live;
  }, [apiBase, cacheKey]);

  const resource = useDataSurface<RosterSnapshot>(
    cacheKey,
    [apiBase],
    loadSubagents,
    {
      isEmpty: () => false,
      initialData: seed ?? undefined,
      sessionCacheKey: cacheKey,
    },
  );

  const snapshot = resource.state.data ?? seed;
  const available = snapshot?.available ?? [];

  return {
    cacheKey,
    chosen,
    setChosen,
    available,
    snapshot,
    resource,
    loadSubagents,
    persistGate,
  };
}
