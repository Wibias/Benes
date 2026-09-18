/** Ultra-mode board state machine for Subagents. */
import type { UltraModeState } from "./subagents-delegation-contract.ts";

export type UltraBoardState = {
  mode: UltraModeState;
  saving: boolean;
  loadFailed: boolean;
};

export type UltraBoardEvent =
  | { type: "hydrate"; mode: UltraModeState }
  | { type: "load-failed" }
  | { type: "save-begin" }
  | { type: "save-end" };

export const IDLE_ULTRA_MODE: UltraModeState = {
  enabled: false,
  hintText: null,
  multiAgentMode: "default",
  multiAgentV2Enabled: false,
};

export function reduceUltraBoard(state: UltraBoardState, event: UltraBoardEvent): UltraBoardState {
  switch (event.type) {
    case "hydrate":
      return { mode: event.mode, saving: state.saving, loadFailed: false };
    case "load-failed":
      return { ...state, loadFailed: true };
    case "save-begin":
      return { ...state, saving: true };
    case "save-end":
      return { ...state, saving: false };
    default: return state;
  }
}

export function isStaleUltraGeneration(
  signal: AbortSignal | undefined,
  generation: number,
  currentGeneration: number,
  bound: string,
  apiBase: string,
): boolean {
  if (signal?.aborted) return true;
  if (generation !== currentGeneration) return true;
  return bound !== apiBase;
}
