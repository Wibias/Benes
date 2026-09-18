/** Shared Subagents delegation / Ultra-mode contracts. Benes-owned shapes only. */

export { ULTRA_MODE_PRESET } from "./subagents-ultra-mode-preset.ts";

export const SUBAGENTS_TOAST_OK_MS = 4500;
export const SUBAGENTS_TOAST_ERR_MS = 8000;

type ProviderId = string;
type ModelId = string;
type NamespacedModelId = string;

export type DelegationModelOption = {
  provider: ProviderId;
  model: ModelId;
  namespaced: NamespacedModelId;
};

export type DelegationPatch = {
  multiAgentGuidanceEnabled?: boolean;
  syncCodexSubagentDefaults?: boolean;
  model?: NamespacedModelId | null;
  effort?: string | null;
};

export type MultiAgentMode = "v1" | "default" | "v2";

type UltraModeFlags = {
  enabled: boolean;
  multiAgentV2Enabled: boolean;
};

type UltraModeSurface = {
  hintText: string | null;
  multiAgentMode: MultiAgentMode;
};

export type UltraModeState = UltraModeFlags & UltraModeSurface;

export type UltraModePatch = {
  multiAgentModeHintText?: string | null;
  multiAgentMode?: MultiAgentMode;
};

const KNOWN_MODES: readonly MultiAgentMode[] = ["v1", "default", "v2"];

export function normalizeMultiAgentMode(raw: unknown): MultiAgentMode {
  if (typeof raw !== "string") return "default";
  for (const mode of KNOWN_MODES) {
    if (mode === raw) return mode;
  }
  return "default";
}

/** Effective V2 surface = enabled && multiAgentMode === "v2". */
export function deriveUltraModeState(input: {
  enabled?: boolean;
  multiAgentMode?: unknown;
  multiAgentModeHintText?: string | null;
}): UltraModeState {
  const multiAgentMode = normalizeMultiAgentMode(input.multiAgentMode);
  const enabled = input.enabled === true;
  return {
    enabled,
    hintText: input.multiAgentModeHintText ?? null,
    multiAgentMode,
    multiAgentV2Enabled: enabled && multiAgentMode === "v2",
  };
}

export function ultraModeHintActive(hintText: string | null | undefined): boolean {
  return typeof hintText === "string" && hintText.trim().length > 0;
}

export function describeUltraModeState(state: UltraModeState): string {
  return [
    state.enabled ? "enabled" : "disabled",
    `mode=${state.multiAgentMode}`,
    state.multiAgentV2Enabled ? "v2-surface" : "non-v2-surface",
    ultraModeHintActive(state.hintText) ? "hint-on" : "hint-off",
  ].join("|");
}

export function isSupportedMultiAgentMode(value: string): value is MultiAgentMode {
  return (KNOWN_MODES as readonly string[]).includes(value);
}
