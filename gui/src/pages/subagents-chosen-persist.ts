/** Chosen subagent roster persist with generation racing and FEATURED_MAX clamp. */

import { readJsonOrThrow } from "../fetch-json.ts";
import { writeSessionListCache } from "../session-list-cache.ts";
import type { SubagentsModelsSnapshot } from "../nav-board-loads.ts";
import { FEATURED_MAX } from "../components/subagents-workspace/roster.ts";

export { FEATURED_MAX };

export type ChosenPersistHooks = {
  getGeneration: () => number;
  bumpGeneration: () => number;
  setInFlight: (value: boolean) => void;
  setChosen: (models: string[]) => void;
  onSuccessToast?: (appliedCount: number) => void;
  onFailureToast: (message: string) => void;
  reload: () => void;
  networkErrorMessage: string;
  saveFailedMessage: string;
};

export async function persistChosenSubagentModels(options: {
  apiBase: string;
  cacheKey: string;
  available: string[];
  models: string[];
  toastOk: boolean;
  hooks: ChosenPersistHooks;
}): Promise<void> {
  const next = options.models.slice(0, FEATURED_MAX);
  const gen = options.hooks.bumpGeneration();
  options.hooks.setInFlight(true);
  writeSessionListCache(options.cacheKey, { available: options.available, chosen: next } satisfies SubagentsModelsSnapshot);
  try {
    const response = await fetch(`${options.apiBase}/api/subagent-models`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ models: next }),
    });
    const body = await readJsonOrThrow<{ applied?: string[] }>(response, options.hooks.saveFailedMessage);
    if (gen !== options.hooks.getGeneration()) return;
    const applied = body?.applied ?? next;
    options.hooks.setChosen(applied);
    writeSessionListCache(options.cacheKey, {
      available: options.available,
      chosen: applied,
    } satisfies SubagentsModelsSnapshot);
    if (options.toastOk) options.hooks.onSuccessToast?.(applied.length);
  } catch (error) {
    if (gen !== options.hooks.getGeneration()) return;
    options.hooks.onFailureToast(
      error instanceof Error && error.message ? error.message : options.hooks.networkErrorMessage,
    );
    options.hooks.setInFlight(false);
    options.hooks.reload();
  } finally {
    if (gen === options.hooks.getGeneration()) options.hooks.setInFlight(false);
  }
}

export function clampFeaturedModels(models: string[]): string[] {
  return models.slice(0, FEATURED_MAX);
}
