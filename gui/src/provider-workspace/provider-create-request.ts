/**
 * Encode an Add Provider draft or reserved Codex seed into the management create body.
 * Empty secrets never leave the client.
 */
import {
  isReservedCodexForwardPreset,
  type CodexForwardPreset,
} from "./openai-forward-policy.ts";

export type ProviderPayloadForm = {
  name: string;
  adapter: string;
  baseUrl: string;
  responsesPath?: string;
  authMode: "key" | "forward" | "oauth" | "local";
  apiKey: string;
  apiKeyTransport?: "x-api-key" | "bearer";
  defaultModel: string;
  allowPrivateNetwork?: boolean;
};

export type ProviderPayload = {
  adapter: string;
  baseUrl: string;
  responsesPath?: string;
  apiKey?: string;
  apiKeyTransport?: "x-api-key" | "bearer";
  defaultModel?: string;
  authMode?: "key" | "forward" | "oauth";
  codexAccountMode?: "pool" | "direct";
  allowPrivateNetwork?: boolean;
};

export type ProviderPostPreset = CodexForwardPreset & {
  provider?: ProviderPayload;
};

export type ProviderWrite =
  | { mode: "clone-seed"; id: string; seed: ProviderPayload }
  | { mode: "draft"; id: string; draft: ProviderPayloadForm };

function keep(value: string | undefined): string | undefined {
  const next = value?.trim();
  return next ? next : undefined;
}

export function encodeProviderDraft(draft: ProviderPayloadForm): ProviderPayload {
  const adapter = draft.adapter.trim();
  const auth = draft.authMode === "local" ? undefined : draft.authMode;
  const secret = auth === "key" ? keep(draft.apiKey) : undefined;
  const bearer = adapter === "anthropic" && auth === "key" && draft.apiKeyTransport === "bearer";
  return {
    adapter,
    baseUrl: draft.baseUrl.trim(),
    ...(keep(draft.responsesPath) ? { responsesPath: keep(draft.responsesPath) } : {}),
    ...(auth ? { authMode: auth } : {}),
    ...(secret ? { apiKey: secret } : {}),
    ...(bearer ? { apiKeyTransport: "bearer" as const } : {}),
    ...(keep(draft.defaultModel) ? { defaultModel: keep(draft.defaultModel) } : {}),
    ...(draft.allowPrivateNetwork ? { allowPrivateNetwork: true } : {}),
  };
}

export function toProviderWrite(preset: ProviderPostPreset, draft: ProviderPayloadForm): ProviderWrite {
  if (!isReservedCodexForwardPreset(preset)) {
    return { mode: "draft", id: draft.name.trim(), draft };
  }
  if (!preset.provider) throw new Error(`Missing canonical provider seed for ${preset.id}`);
  return { mode: "clone-seed", id: preset.id, seed: preset.provider };
}

export function encodeProviderWrite(write: ProviderWrite): { name: string; provider: ProviderPayload } {
  return write.mode === "clone-seed"
    ? { name: write.id, provider: structuredClone(write.seed) }
    : { name: write.id, provider: encodeProviderDraft(write.draft) };
}

export function encodeProviderCreate(
  preset: ProviderPostPreset,
  draft: ProviderPayloadForm,
): { name: string; provider: ProviderPayload } {
  return encodeProviderWrite(toProviderWrite(preset, draft));
}
