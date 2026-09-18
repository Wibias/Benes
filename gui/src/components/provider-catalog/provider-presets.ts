/**
 * Preset-row data for the Add Provider catalog.
 *
 * Owns the `GET /api/provider-presets` row shape and the preset-derived
 * presentation data the modal needs: featured rail order, seed model ids, the
 * vendor docs link, and the model-preview labels. Row classification and the
 * Access/Connection filter predicates live in ./catalog-policy.ts. No React,
 * no fetch.
 */

import type { ProviderPayload } from "../../provider-workspace/provider-create-request";

/** Row shape returned by GET /api/provider-presets (mirrors DerivedProviderPreset). */
export interface CatalogPreset {
  id: string;
  label: string;
  adapter: string;
  baseUrl: string;
  responsesPath?: string;
  defaultModel?: string;
  /** Seed model ids from the preset (same list `cmd/benes/init_providers.json` ships). */
  models?: string[];
  /** "oauth": account login · "forward": ChatGPT passthrough · "key": API key · "local": local scaffold. */
  auth: "oauth" | "forward" | "key" | "local";
  /** OAuth registry id (for auth === "oauth"). */
  oauthProvider?: string;
  /** Where to create/copy the API key (for auth === "key" catalog providers). */
  dashboardUrl?: string;
  note?: string;
  /** API key is optional — provider works without one (keyless free). */
  keyOptional?: boolean;
  /** Free pricing — may still require an API key (e.g. NVIDIA NIM). */
  freeTier?: boolean;
  /**
   * Endpoint picker (e.g. Qwen Cloud). Choice without `baseUrl` = Custom (show text field).
   */
  baseUrlChoices?: Array<{ id: string; label: string; baseUrl?: string }>;
  codexAccountMode?: "direct" | "pool";
  provider?: ProviderPayload;
}

/** Preferred rail order. Remaining presets follow alphabetically so the full catalog stays visible. */
export const FEATURED_PRESET_IDS = [
  "command-code",
  "anthropic",
  "openai-apikey",
  "openrouter",
  "deepseek",
  "groq",
  "mimo-free",
  "lm-studio",
  "litellm",
] as const;

/** Docs URL for the selected vendor: own dashboard, else a sibling on the same adapter/base. */
export function setupDocsUrl(preset: CatalogPreset, catalog: CatalogPreset[]): string | undefined {
  if (preset.dashboardUrl) return preset.dashboardUrl;
  return catalog.find(row => (
    row.id !== preset.id
    && row.adapter === preset.adapter
    && row.baseUrl === preset.baseUrl
    && row.dashboardUrl
  ))?.dashboardUrl;
}

const ANTHROPIC_SEED_MODELS = [
  "claude-fable-5",
  "claude-sonnet-5",
  "claude-opus-5",
  "claude-opus-4-8",
  "claude-opus-4-7",
  "claude-opus-4-6",
  "claude-sonnet-4-6",
  "claude-haiku-4-5",
];

export function presetModelIds(preset: CatalogPreset, catalog: CatalogPreset[]): string[] {
  if (preset.models && preset.models.length > 0) return preset.models;
  const sibling = catalog.find(row => (
    row.id !== preset.id
    && row.adapter === preset.adapter
    && row.baseUrl === preset.baseUrl
    && row.models
    && row.models.length > 0
  ));
  if (sibling?.models?.length) return sibling.models;
  if (preset.adapter === "anthropic") return ANTHROPIC_SEED_MODELS;
  return preset.defaultModel ? [preset.defaultModel] : [];
}

/** Family labels for the mock models line (Claude Opus • Claude Sonnet • …). */
export function modelPreviewLabels(ids: string[]): string[] {
  const claudeFound = new Map<string, string>();
  const rest: string[] = [];
  const seenRest = new Set<string>();
  for (const id of ids) {
    const claude = /^claude-([a-z]+)/i.exec(id);
    if (claude) {
      const family = claude[1]!.toLowerCase();
      if (!claudeFound.has(family)) {
        claudeFound.set(family, `Claude ${family.slice(0, 1).toUpperCase()}${family.slice(1)}`);
      }
      continue;
    }
    if (!seenRest.has(id)) {
      seenRest.add(id);
      rest.push(id);
    }
  }
  const labels: string[] = [];
  for (const key of ["opus", "sonnet", "haiku", "fable"]) {
    const label = claudeFound.get(key);
    if (label) labels.push(label);
  }
  for (const [key, label] of claudeFound) {
    if (!["opus", "sonnet", "haiku", "fable"].includes(key)) labels.push(label);
  }
  const previewLimit = 8;
  if (rest.length <= previewLimit) labels.push(...rest);
  else labels.push(...rest.slice(0, 6));
  return labels;
}
