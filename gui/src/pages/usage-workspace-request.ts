/** Usage board request path: range query ownership stays in usage-range. */

import { parseUsageResponse, type UsageResponse } from "./usage-contract.ts";
import type { UsageRangeQuery } from "./usage-range.ts";
import { writeUsageWorkspaceHeld } from "./usage-workspace-held.ts";

export type UsageWorkspaceFetchPlan =
  | { ready: true; query: string }
  | { ready: false; reason: "malformed" | "reversed" };

export function planUsageWorkspaceFetch(rangeQuery: UsageRangeQuery): UsageWorkspaceFetchPlan {
  if (!rangeQuery.ok) {
    return { ready: false, reason: rangeQuery.error };
  }
  return { ready: true, query: rangeQuery.query };
}

export function usageWorkspaceRequestUrl(apiBase: string, query: string): string {
  return `${apiBase}/api/usage?${query}`;
}

export async function loadUsageWorkspacePayload(
  apiBase: string,
  query: string,
  signal: AbortSignal,
): Promise<UsageResponse> {
  const response = await fetch(usageWorkspaceRequestUrl(apiBase, query), { signal });
  if (!response.ok) {
    throw new Error(`${response.status} ${response.statusText}`.trim());
  }
  const parsed = parseUsageResponse(await response.json());
  if (!parsed) {
    throw new Error("invalid_usage");
  }
  writeUsageWorkspaceHeld(apiBase, query, parsed);
  return parsed;
}
