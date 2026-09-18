/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Delegation settings (`/api/injection-model`) as a standalone hook.
 *
 * Owns React / DataSurface integration only. Save/reload mutation authority
 * lives in createDelegationMutationController (one production loop).
 */
import { useCallback, useMemo, useState } from "react";
import { setClientResourceData } from "../client-resource.ts";
import { useDataSurface } from "../data-surface.ts";
import {
  loadDelegationSnapshot,
  type DelegationSnapshot,
} from "../nav-board-loads.ts";
import { paintedGuidanceEnabled, subagentsDelegationSessionKey } from "../nav-board-resources.ts";
import type { DelegationPatch } from "./subagents-delegation-contract.ts";
import {
  createDelegationMutationController,
  type DelegationMutationTransport,
} from "./subagents-delegation-mutations.ts";

export type {
  DelegationModelOption,
  DelegationPatch,
  MultiAgentMode,
  UltraModePatch,
  UltraModeState,
} from "./subagents-delegation-contract.ts";

export { ULTRA_MODE_PRESET, deriveUltraModeState, normalizeMultiAgentMode } from "./subagents-delegation-contract.ts";

function createProductionDelegationTransport(options: {
  apiBase: string;
  cacheKey: string;
  setSaving: (value: boolean) => void;
}): DelegationMutationTransport {
  return {
    put: async (patch) => {
      const res = await fetch(`${options.apiBase}/api/injection-model`, {
        method: "PUT",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify(patch),
      });
      if (!res.ok) throw new Error("injection save failed");
    },
    readAuthority: () => loadDelegationSnapshot(options.apiBase),
    publish: (snapshot) => {
      setClientResourceData(options.cacheKey, snapshot as DelegationSnapshot);
    },
    setSaving: options.setSaving,
  };
}

export function useSubagentDelegation(apiBase: string) {
  const cacheKey = subagentsDelegationSessionKey(apiBase);
  const load = useCallback((signal: AbortSignal) => loadDelegationSnapshot(apiBase, signal), [apiBase]);
  const resource = useDataSurface<DelegationSnapshot>(cacheKey, [apiBase], load, {
    isEmpty: () => false,
    sessionCacheKey: cacheKey,
  });
  const data = resource.state.data;
  const [saving, setSaving] = useState(false);

  // Controller is recreated when apiBase/cacheKey identity changes so an in-flight
  // mutation bound to the previous transport cannot publish into the new identity.
  const controller = useMemo(() => {
    return createDelegationMutationController(
      createProductionDelegationTransport({ apiBase, cacheKey, setSaving }),
    );
  }, [apiBase, cacheKey]);

  const save = useCallback(
    (patch: DelegationPatch) => controller.save(patch),
    [controller],
  );
  const reload = useCallback(() => controller.reload(), [controller]);

  return {
    loaded: data !== undefined || resource.hasSucceeded,
    saving,
    model: data?.model ?? "",
    effort: data?.effort ?? "",
    efforts: data?.efforts ?? [],
    available: data?.available ?? [],
    guidanceEnabled: paintedGuidanceEnabled(data?.guidanceEnabled),
    syncCodexDefaults: data?.syncCodexDefaults === true,
    save,
    reload,
  };
}
