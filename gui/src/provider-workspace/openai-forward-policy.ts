/**
 * Reserved ChatGPT/Codex forward identity and which configured rows join the account pool.
 */
export const CODEX_FORWARD_BASE_URL = "https://chatgpt.com/backend-api/codex";
export const RESERVED_OPENAI_FORWARD_ID = "openai";

export type OpenAiAccountState = "absent" | "disabled" | "ready" | "invalid";
export type CodexPresetDescriptionKey = "prov.openaiPoolDesc" | "prov.openaiDirectDesc";

export type CodexForwardRow = {
  adapter?: string;
  authMode?: string;
  baseUrl?: string;
  disabled?: boolean;
};

export type CodexForwardPreset = {
  id: string;
  codexAccountMode?: "direct" | "pool";
};

function originAndPath(value: string): string | undefined {
  let parsed: URL;
  try {
    parsed = new URL(value.trim());
  } catch {
    return undefined;
  }
  const blocked = Boolean(parsed.username || parsed.password || parsed.search || parsed.hash);
  if (blocked) return undefined;
  const path = parsed.pathname.replace(/\/+$/, "");
  return parsed.origin + path;
}

export function normalizeProviderBaseUrl(value: string): string | undefined {
  return originAndPath(value);
}

export function isCanonicalCodexForwardUrl(baseUrl: string | undefined): boolean {
  return typeof baseUrl === "string" && originAndPath(baseUrl) === CODEX_FORWARD_BASE_URL;
}

export function isCanonicalCodexForwardShape(provider: CodexForwardRow): boolean {
  const adapterOk = provider.adapter === "openai-responses";
  const authOk = provider.authMode === "forward";
  return adapterOk && authOk && isCanonicalCodexForwardUrl(provider.baseUrl);
}

export function isReservedCodexForwardPreset(preset: CodexForwardPreset): boolean {
  return preset.id === RESERVED_OPENAI_FORWARD_ID;
}

export function openAiAccountProviderState(provider: CodexForwardRow | undefined): OpenAiAccountState {
  if (provider === undefined) return "absent";
  if (!isCanonicalCodexForwardShape(provider)) return "invalid";
  return provider.disabled === true ? "disabled" : "ready";
}

export function codexAccountProviderNames(
  providers: Record<string, { authMode?: string }>,
): string[] {
  const extras = Object.keys(providers)
    .filter(name => name !== RESERVED_OPENAI_FORWARD_ID && providers[name]?.authMode === "forward")
    .toSorted((left, right) => left.localeCompare(right));
  return [RESERVED_OPENAI_FORWARD_ID].concat(extras);
}

export function codexPresetDescriptionKey(preset: CodexForwardPreset): CodexPresetDescriptionKey | null {
  if (!isReservedCodexForwardPreset(preset)) return null;
  return preset.codexAccountMode === "direct" ? "prov.openaiDirectDesc" : "prov.openaiPoolDesc";
}
