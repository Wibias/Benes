/** Held Usage payloads keyed by apiBase + query. Memory first, then session cache. */

import { readSessionListCache, writeSessionListCache } from "../session-list-cache.ts";
import type { UsageResponse } from "./usage-contract.ts";

const HELD_PREFIX = "benes.usage.v3";

const memoryHeld = new Map<string, UsageResponse>();

export function usageWorkspaceHeldIdentity(apiBase: string, query: string): string {
  return `${HELD_PREFIX}:${apiBase}:${query}`;
}

export function readUsageWorkspaceHeld(
  apiBase: string,
  query: string,
): UsageResponse | null {
  if (!query) return null;
  const identity = usageWorkspaceHeldIdentity(apiBase, query);
  const fromMemory = memoryHeld.get(identity);
  if (fromMemory) return fromMemory;
  return readSessionListCache<UsageResponse>(identity);
}

export function writeUsageWorkspaceHeld(
  apiBase: string,
  query: string,
  payload: UsageResponse,
): void {
  const identity = usageWorkspaceHeldIdentity(apiBase, query);
  memoryHeld.set(identity, payload);
  writeSessionListCache(identity, payload);
}

/** Test-only: drop in-memory held entries (session storage left alone). */
export function clearUsageWorkspaceHeldMemory(): void {
  memoryHeld.clear();
}
