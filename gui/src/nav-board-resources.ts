/**
 * Shared cache / resource identities for boards the shell warms before the tab mounts.
 * Pages must use these exact keys so a prefetch populate is the first paint.
 */

export function harnessBoardResourceKey(apiBase: string): string {
  return `harnesses:${apiBase}`;
}

export function harnessBoardSessionKey(apiBase: string): string {
  return `benes.harnesses.v1:${apiBase}`;
}

export function subagentsModelsResourceKey(apiBase: string): string {
  return `benes.subagents.v1:${apiBase}`;
}

export function subagentsDelegationSessionKey(apiBase: string): string {
  return `benes.subagents.delegation.v1:${apiBase}`;
}

export function providersConfigCacheKey(apiBase: string): string {
  return `benes.providers.config.v1:${apiBase}`;
}

export function providersWorkspaceCacheKey(apiBase: string): string {
  return `benes.providers.workspace.v2:${apiBase}`;
}

export function combosWorkspaceCacheKey(apiBase: string): string {
  return `benes.combos.workspace.v1:${apiBase}`;
}

export function labMatrixResourceKey(apiBase: string, queryKey = "{}"): string {
  return `lab-matrix:${apiBase}:${queryKey}`;
}

export function fabricTasksResourceKey(apiBase: string): string {
  return `fabric-tasks:${apiBase}`;
}

/** Last known guidance, or off until /api/injection-model answers. Never optimistic on. */
export function paintedGuidanceEnabled(cached: boolean | null | undefined): boolean {
  return cached === true;
}
