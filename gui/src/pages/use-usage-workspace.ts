/** Usage board orchestration: controls + held seed + data-surface load. */
import { useCallback, useMemo, useState } from "react";
import { useDataSurface } from "../data-surface.ts";
import type { UsageResponse } from "./usage-contract.ts";
import { usageSearchParams } from "./usage-range.ts";
import { buildUsageWorkspaceControlDefaults } from "./usage-workspace-defaults.ts";
import {
  readUsageWorkspaceHeld,
  usageWorkspaceHeldIdentity,
  writeUsageWorkspaceHeld,
} from "./usage-workspace-held.ts";
import { parseUsageResponse } from "./usage-contract.ts";
import { planUsageWorkspaceFetch, usageWorkspaceRequestUrl } from "./usage-workspace-request.ts";

export function useUsageWorkspace(apiBase: string) {
  const defaults = buildUsageWorkspaceControlDefaults();
  const [range, setRange] = useState(defaults.range);
  const [surface, setSurface] = useState(defaults.surface);
  const [modelQuery, setModelQuery] = useState(defaults.modelQuery);
  const [customStart, setCustomStart] = useState(defaults.customStart);
  const [customEnd, setCustomEnd] = useState(defaults.customEnd);
  const timeZone = defaults.timeZone;

  const rangeQuery = useMemo(
    () => usageSearchParams(range, surface, customStart, customEnd, timeZone),
    [range, surface, customStart, customEnd, timeZone],
  );
  const fetchPlan = planUsageWorkspaceFetch(rangeQuery);
  const query = fetchPlan.ready ? fetchPlan.query : "";

  const loadUsage = useCallback(async (signal: AbortSignal): Promise<UsageResponse> => {
    if (!rangeQuery.ok) throw new Error(rangeQuery.error);
    const response = await fetch(usageWorkspaceRequestUrl(apiBase, query), { signal });
    if (!response.ok) throw new Error(`${response.status} ${response.statusText}`.trim());
    const parsed = parseUsageResponse(await response.json());
    if (!parsed) throw new Error("invalid_usage");
    writeUsageWorkspaceHeld(apiBase, query, parsed);
    return parsed;
  }, [apiBase, query, rangeQuery]);

  const resourceKey = usageWorkspaceHeldIdentity(apiBase, query);
  const held = query ? readUsageWorkspaceHeld(apiBase, query) : null;
  const resource = useDataSurface<UsageResponse>(
    resourceKey,
    [apiBase, query],
    loadUsage,
    { isEmpty: () => false, initialData: held ?? undefined, enabled: rangeQuery.ok },
  );

  return {
    range,
    setRange,
    surface,
    setSurface,
    customStart,
    setCustomStart,
    customEnd,
    setCustomEnd,
    modelQuery,
    setModelQuery,
    rangeQuery,
    resource,
    data: resource.state.data ?? held ?? null,
  };
}
