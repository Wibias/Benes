import { managementErrorMessage } from "./claude-settings-error.ts";
import {
  CATALOG_LOAD_FAILED,
  claudeSettingsPutBody,
  decodeClaudeSettings,
  decodeModelsPayload,
  type ClaudeSettingsDraft,
} from "./claude-settings-state.ts";

export function modelsCatalogFromResponse(ok: boolean, body: unknown): unknown[] {
  if (!ok) {
    throw new Error(managementErrorMessage(body, CATALOG_LOAD_FAILED));
  }
  return decodeModelsPayload(body);
}

export async function readClaudeSettings(apiBase: string, signal?: AbortSignal) {
  const [settingsRes, modelsRes] = await Promise.all([
    fetch(`${apiBase}/api/claude-code`, { signal }),
    fetch(`${apiBase}/api/models`, { signal }),
  ]);
  if (!settingsRes.ok) {
    let body: unknown = null;
    try { body = await settingsRes.json(); } catch { /* ignore */ }
    throw new Error(managementErrorMessage(body, "Could not load Claude Code settings."));
  }
  const snapshot = decodeClaudeSettings(await settingsRes.json());
  let modelsBody: unknown = null;
  try { modelsBody = await modelsRes.json(); } catch { /* ignore */ }
  return { snapshot, modelsRaw: modelsCatalogFromResponse(modelsRes.ok, modelsBody) };
}

export async function saveClaudeSettings(
  apiBase: string,
  baseline: ClaudeSettingsDraft,
  draft: ClaudeSettingsDraft,
  signal?: AbortSignal,
) {
  const res = await fetch(`${apiBase}/api/claude-code`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(claudeSettingsPutBody(baseline, draft)),
    signal,
  });
  if (!res.ok) {
    let errBody: unknown = null;
    try { errBody = await res.json(); } catch { /* ignore */ }
    throw new Error(managementErrorMessage(errBody, "The change was refused."));
  }
  const refresh = await fetch(`${apiBase}/api/claude-code`, { signal });
  if (!refresh.ok) {
    let errBody: unknown = null;
    try { errBody = await refresh.json(); } catch { /* ignore */ }
    throw new Error(managementErrorMessage(errBody, "Could not load Claude Code settings."));
  }
  return decodeClaudeSettings(await refresh.json());
}
