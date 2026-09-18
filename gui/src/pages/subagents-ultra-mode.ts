/** Ultra-mode (/api/v2) transport helpers for Subagents. */

import { readJsonOrThrow } from "../fetch-json.ts";
import {
  deriveUltraModeState,
  type UltraModePatch,
  type UltraModeState,
} from "./subagents-delegation-contract.ts";

const ULTRA_ROUTE = "/api/v2";

type UltraWirePayload = {
  enabled?: boolean;
  multiAgentMode?: "v1" | "default" | "v2";
  multiAgentModeHintText?: string | null;
};

function ultraUrl(apiBase: string): string {
  return `${apiBase}${ULTRA_ROUTE}`;
}

export async function fetchUltraModeState(
  apiBase: string,
  failMessage: string,
  signal?: AbortSignal,
): Promise<UltraModeState> {
  const response = await fetch(ultraUrl(apiBase), { signal });
  const payload = await readJsonOrThrow<UltraWirePayload>(response, failMessage);
  if (!payload) throw new Error(failMessage);
  return deriveUltraModeState({
    enabled: payload.enabled,
    multiAgentMode: payload.multiAgentMode,
    multiAgentModeHintText: payload.multiAgentModeHintText,
  });
}

export async function putUltraModePatch(
  apiBase: string,
  patch: UltraModePatch,
  failMessage: string,
): Promise<void> {
  const response = await fetch(ultraUrl(apiBase), {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  await readJsonOrThrow(response, failMessage);
}

export function ultraSaveToastKey(patch: UltraModePatch): "models.v2Applied" | "sub.ultraModeSaved" {
  return Object.hasOwn(patch, "multiAgentMode") && patch.multiAgentMode !== undefined
    ? "models.v2Applied"
    : "sub.ultraModeSaved";
}
