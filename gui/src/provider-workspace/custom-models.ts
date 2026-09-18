import { encodedModelIdCollides } from "../lib/slug-codec.ts";

export const CUSTOM_MODEL_CHIP_RENDER_CAP = 300;

export function parseCustomModelIds(rows: unknown, providerName: string): string[] | null {
  if (!Array.isArray(rows)) return null;
  return rows.flatMap(row => {
    if (!row || typeof row !== "object") return [];
    const model = row as { provider?: unknown; modelId?: unknown };
    return model.provider === providerName && typeof model.modelId === "string" ? [model.modelId] : [];
  });
}

export function knownCustomModelIds(input: {
  availableModels: readonly string[];
  customModelIds: readonly string[];
  configuredModels: readonly string[];
  defaultModel?: string;
}): string[] {
  return [
    ...input.availableModels,
    ...input.customModelIds,
    ...input.configuredModels,
    ...(input.defaultModel ? [input.defaultModel] : []),
  ];
}

export function customModelInvalid(input: {
  customModelsReady: boolean;
  trimmedCustomModelId: string;
  availableModels: readonly string[];
  customModelIds: readonly string[];
  configuredModels: readonly string[];
  defaultModel?: string;
  knownModelIds: readonly string[];
}): boolean {
  const id = input.trimmedCustomModelId;
  return !input.customModelsReady
    || !id
    || input.availableModels.includes(id)
    || input.customModelIds.includes(id)
    || input.configuredModels.includes(id)
    || input.defaultModel === id
    || encodedModelIdCollides(id, input.knownModelIds);
}

export function modelsEmptyBase(input: {
  availableModels: readonly string[];
  configuredModels: readonly string[];
  customModelIds: readonly string[];
  defaultModel?: string;
}): boolean {
  return input.availableModels.length === 0
    && input.configuredModels.length === 0
    && input.customModelIds.length === 0
    && !input.defaultModel;
}

export function showingConfiguredModelsFallback(
  availableCount: number,
  configuredCount: number,
): boolean {
  return availableCount === 0 && configuredCount > 0;
}

export function visibleModelChips<T>(models: readonly T[], cap = CUSTOM_MODEL_CHIP_RENDER_CAP): {
  capped: boolean;
  visible: readonly T[];
} {
  const capped = models.length > cap;
  return { capped, visible: capped ? models.slice(0, cap) : models };
}
