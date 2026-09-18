import { useCallback, useMemo, useState, type Dispatch, type SetStateAction } from "react";
import { formatNamespacedModelId } from "../provider-icons";
import { useDataSurface } from "../data-surface";
import type { TFn } from "../i18n/shared";
import {
  type ModelInfo,
  type SettingsData,
} from "./dashboard-shared";
import { readRequiredJson } from "../fetch-json";
import { mergeSidecarSetting } from "./dashboard-sidecar-merge";
import {
  shadowCallModelOptions,
  visionEnabledPatch,
  visionModelChangePatch,
  visionSelectOptions,
} from "./dashboard-model-views";

export type ContextProjectionData = { mode: string };
export type FabricSettingsData = { enabled: boolean };

/**
 * The Control board's settings boundaries: the sidecar and shadow-call configuration the
 * listener keeps on disk. These are the shapes this board reads and writes, declared next to
 * the reads themselves like the two settings above.
 */

/**
 * The `backend` values `/api/sidecar-settings` accepts, per modality.
 *
 * Web search accepts `openai` (dedicated search) and `anthropic` (provider-native search), or
 * the two class names directly. Image description accepts `openai`, `anthropic`, or
 * `vision_describe`; all three resolve to the describe class, and the provider travels in the
 * model id. The listener answers with whatever is stored, so this union covers all of them.
 */
export type SidecarBackend =
  | "openai"
  | "anthropic"
  | "dedicated_search"
  | "provider_native_search"
  | "vision_describe";

/** One sidecar section as `GET /api/sidecar-settings` publishes it. */
export interface SidecarSetting {
  backend?: SidecarBackend;
  model: string;
  enabled?: boolean;
  /** Web search only: forward the routed model's own output while the search runs. */
  streamRoutedModelOutput?: boolean;
  /** Vision only. The listener stores these; PUT does not accept them. */
  reasoning?: string;
  timeoutMs?: number;
  maxDescriptionsPerTurn?: number;
}

export interface SidecarData {
  webSearch: SidecarSetting;
  vision: SidecarSetting;
  /**
   * Eligible describers as `provider/model` ids, in server order.
   *
   * `undefined` and `[]` mean different things: a response without the key predates the field
   * and the picker falls back to the provider-name list, while `[]` is the server saying it
   * proved nothing eligible. Display labels are the client's own view model.
   */
  visionModels?: string[];
}

/** Body `PUT /api/sidecar-settings` accepts from this board (vision selection only). */
export interface SidecarPatch {
  vision?: {
    backend?: SidecarBackend;
    model?: string;
    enabled?: boolean;
  };
}

/** `GET`/`PUT /api/shadow-call-settings`. */
export interface ShadowCallData { enabled: boolean; model: string; sourceModels?: string[] }

type ControlExtras = {
  settings: SettingsData | null;
  shadowCall: ShadowCallData | null;
  sidecar: SidecarData | null;
  contextProjection: ContextProjectionData | null;
  fabric: FabricSettingsData | null;
  models: ModelInfo[];
};

type ControlOverlay = {
  settings: SettingsData | null;
  shadowCall: ShadowCallData | null;
  sidecar: SidecarData | null;
  contextProjection: ContextProjectionData | null;
  fabric: FabricSettingsData | null;
};

const EMPTY_MODELS: ModelInfo[] = [];
const EMPTY_OVERLAY: ControlOverlay = {
  settings: null, shadowCall: null, sidecar: null, contextProjection: null, fabric: null,
};

function normalizeProjectionMode(mode: string | undefined): string {
  switch (mode) {
    case "shadow":
    case "duplicate":
    case "recovery":
      return mode;
    case "on":
      return "recovery";
    default:
      return "off";
  }
}

async function fetchControlExtras(apiBase: string, signal: AbortSignal): Promise<ControlExtras> {
  const [settingsRes, shadowRes, sidecarRes, projectionRes, modelsRes, fabricRes] = await Promise.all([
    fetch(`${apiBase}/api/settings`, { signal }),
    fetch(`${apiBase}/api/shadow-call-settings`, { signal }),
    fetch(`${apiBase}/api/sidecar-settings`, { signal }),
    fetch(`${apiBase}/api/context-projection`, { signal }),
    fetch(`${apiBase}/api/models`, { signal }),
    fetch(`${apiBase}/api/fabric-settings`, { signal }),
  ]);
  const modelsJson = modelsRes.ok ? await modelsRes.json() as unknown : [];
  const projectionJson = projectionRes.ok ? await projectionRes.json() as { mode?: string } : null;
  const fabricJson = fabricRes.ok ? await fabricRes.json() as { enabled?: boolean } : null;
  return {
    settings: settingsRes.ok ? await settingsRes.json() as SettingsData : null,
    shadowCall: shadowRes.ok ? await shadowRes.json() as ShadowCallData : null,
    sidecar: sidecarRes.ok ? await sidecarRes.json() as SidecarData : null,
    contextProjection: projectionJson
      ? { mode: normalizeProjectionMode(projectionJson.mode) }
      : null,
    fabric: fabricJson ? { enabled: Boolean(fabricJson.enabled) } : null,
    models: Array.isArray(modelsJson) ? modelsJson as ModelInfo[] : [],
  };
}

function patchedSidecar(current: SidecarData, patch: SidecarPatch): SidecarData {
  return { ...current, vision: mergeSidecarSetting(current.vision, patch.vision) };
}

async function putAutostart(apiBase: string, next: boolean): Promise<boolean> {
  const res = await fetch(`${apiBase}/api/settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ codexAutoStart: next }),
  });
  const body = await readRequiredJson<{ codexAutoStart: boolean }>(res, "save failed");
  return body.codexAutoStart;
}

async function putShadow(apiBase: string, patch: Partial<ShadowCallData>): Promise<void> {
  const res = await fetch(`${apiBase}/api/shadow-call-settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  if (!res.ok) throw new Error("shadow-call save failed");
}

async function putSidecar(apiBase: string, patch: SidecarPatch): Promise<SidecarData> {
  const res = await fetch(`${apiBase}/api/sidecar-settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(patch),
  });
  return await readRequiredJson<SidecarData>(res, "save failed");
}

function useControlExtras(apiBase: string, refreshNonce: number): ControlExtras | undefined {
  const loadExtras = useCallback(
    (signal: AbortSignal) => fetchControlExtras(apiBase, signal),
    [apiBase],
  );
  return useDataSurface<ControlExtras>(
    `control-board-extras:${apiBase}`,
    [apiBase, refreshNonce],
    loadExtras,
    { isEmpty: () => false },
  ).state.data;
}

function useControlOverlay(refreshNonce: number): [ControlOverlay, Dispatch<SetStateAction<ControlOverlay>>] {
  const [overlay, setOverlay] = useState<ControlOverlay>(EMPTY_OVERLAY);
  const [seenNonce, setSeenNonce] = useState(refreshNonce);
  if (seenNonce !== refreshNonce) {
    setSeenNonce(refreshNonce);
    setOverlay(EMPTY_OVERLAY);
  }
  return [overlay, setOverlay];
}

async function runToggleAutostart(
  apiBase: string,
  liveSettings: SettingsData,
  setOverlay: Dispatch<SetStateAction<ControlOverlay>>,
  setSaving: (value: boolean) => void,
): Promise<void> {
  const next = !liveSettings.codexAutoStart;
  setSaving(true);
  setOverlay(current => ({ ...current, settings: { ...liveSettings, codexAutoStart: next } }));
  try {
    const saved = await putAutostart(apiBase, next);
    setOverlay(current => ({
      ...current,
      settings: { ...(current.settings ?? liveSettings), codexAutoStart: saved },
    }));
  } catch {
    setOverlay(current => ({
      ...current,
      settings: { ...(current.settings ?? liveSettings), codexAutoStart: !next },
    }));
  } finally {
    setSaving(false);
  }
}

async function runSaveShadow(
  apiBase: string,
  liveShadow: ShadowCallData,
  patch: Partial<ShadowCallData>,
  setOverlay: Dispatch<SetStateAction<ControlOverlay>>,
  setSaving: (value: boolean) => void,
): Promise<void> {
  setOverlay(current => ({ ...current, shadowCall: { ...liveShadow, ...patch } }));
  setSaving(true);
  try {
    await putShadow(apiBase, patch);
  } catch {
    setOverlay(current => ({ ...current, shadowCall: liveShadow }));
  } finally {
    setSaving(false);
  }
}

async function putContextProjection(apiBase: string, mode: string): Promise<string> {
  const res = await fetch(`${apiBase}/api/context-projection`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ mode }),
  });
  const body = await readRequiredJson<{ mode?: string }>(res, "save failed");
  return normalizeProjectionMode(body.mode ?? mode);
}

async function runSaveContextProjection(
  apiBase: string,
  live: ContextProjectionData,
  mode: string,
  setOverlay: Dispatch<SetStateAction<ControlOverlay>>,
  setSaving: (value: boolean) => void,
): Promise<void> {
  const next = normalizeProjectionMode(mode);
  setOverlay(current => ({ ...current, contextProjection: { mode: next } }));
  setSaving(true);
  try {
    const saved = await putContextProjection(apiBase, next);
    setOverlay(current => ({ ...current, contextProjection: { mode: saved } }));
  } catch {
    setOverlay(current => ({ ...current, contextProjection: live }));
  } finally {
    setSaving(false);
  }
}

async function putFabric(apiBase: string, enabled: boolean): Promise<void> {
  const res = await fetch(`${apiBase}/api/fabric-settings`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ enabled }),
  });
  if (!res.ok) throw new Error("fabric save failed");
}

async function runSaveFabric(
  apiBase: string,
  live: FabricSettingsData,
  enabled: boolean,
  setOverlay: Dispatch<SetStateAction<ControlOverlay>>,
  setSaving: (value: boolean) => void,
): Promise<void> {
  setOverlay(current => ({ ...current, fabric: { enabled } }));
  setSaving(true);
  try {
    await putFabric(apiBase, enabled);
  } catch {
    setOverlay(current => ({ ...current, fabric: live }));
  } finally {
    setSaving(false);
  }
}
async function runSaveSidecar(
  apiBase: string,
  liveSidecar: SidecarData,
  patch: SidecarPatch,
  setOverlay: Dispatch<SetStateAction<ControlOverlay>>,
  setSaving: (value: boolean) => void,
): Promise<void> {
  setOverlay(current => ({ ...current, sidecar: patchedSidecar(liveSidecar, patch) }));
  setSaving(true);
  try {
    const body = await putSidecar(apiBase, patch);
    setOverlay(current => ({
      ...current,
      sidecar: body,
    }));
  } catch {
    setOverlay(current => ({ ...current, sidecar: liveSidecar }));
  } finally {
    setSaving(false);
  }
}

function pickLive<T>(local: T | null, server: T | null | undefined): T | null {
  return local ?? server ?? null;
}

function busyGuard<T>(value: T | null, busy: boolean): value is T {
  return value !== null && !busy;
}

export function useControlBoardSettings(apiBase: string, refreshNonce: number, t: TFn) {
  const server = useControlExtras(apiBase, refreshNonce);
  const [overlay, setOverlay] = useControlOverlay(refreshNonce);
  const [settingsSaving, setSettingsSaving] = useState(false);
  const [shadowSaving, setShadowSaving] = useState(false);
  const [sidecarSaving, setSidecarSaving] = useState(false);
  const [projectionSaving, setProjectionSaving] = useState(false);
  const [fabricSaving, setFabricSaving] = useState(false);
  const liveSettings = pickLive(overlay.settings, server?.settings);
  const liveShadow = pickLive(overlay.shadowCall, server?.shadowCall);
  const liveSidecar = pickLive(overlay.sidecar, server?.sidecar);
  const liveProjection = pickLive(overlay.contextProjection, server?.contextProjection);
  const liveFabric = pickLive(overlay.fabric, server?.fabric);
  const models = server?.models ?? EMPTY_MODELS;

  const toggleAutostart = () => {
    if (!busyGuard(liveSettings, settingsSaving)) return;
    void runToggleAutostart(apiBase, liveSettings, setOverlay, setSettingsSaving);
  };
  const saveShadow = (patch: Partial<ShadowCallData>) => {
    if (!busyGuard(liveShadow, shadowSaving)) return;
    void runSaveShadow(apiBase, liveShadow, patch, setOverlay, setShadowSaving);
  };
  const saveSidecar = (patch: SidecarPatch) => {
    if (!busyGuard(liveSidecar, sidecarSaving)) return;
    void runSaveSidecar(apiBase, liveSidecar, patch, setOverlay, setSidecarSaving);
  };
  const saveContextProjection = (mode: string) => {
    if (!busyGuard(liveProjection, projectionSaving)) return;
    void runSaveContextProjection(apiBase, liveProjection, mode, setOverlay, setProjectionSaving);
  };
  const saveFabric = (enabled: boolean) => {
    if (!busyGuard(liveFabric, fabricSaving)) return;
    void runSaveFabric(apiBase, liveFabric, enabled, setOverlay, setFabricSaving);
  };

  const shadowOptions = useMemo(
    () => shadowCallModelOptions(models, liveShadow?.model).map(option => (
      option.value === ""
        ? option
        : { ...option, label: formatNamespacedModelId(option.value, t) }
    )),
    [models, liveShadow?.model, t],
  );
  const visionOptions = useMemo(
    () => visionSelectOptions(models, liveSidecar).map(value => ({
      value,
      label: formatNamespacedModelId(value, t),
    })),
    [models, liveSidecar, t],
  );
  const visionOn = liveSidecar?.vision.enabled !== false && Boolean(liveSidecar?.vision.model);

  return {
    settings: liveSettings,
    settingsSaving,
    shadowCall: liveShadow,
    shadowSaving,
    sidecar: liveSidecar,
    sidecarSaving,
    contextProjection: liveProjection,
    projectionSaving,
    fabric: liveFabric,
    fabricSaving,
    shadowOptions,
    visionOptions,
    visionOn,
    toggleAutostart,
    saveShadow,
    saveContextProjection,
    saveFabric,
    saveVisionModel: (value: string) => {
      saveSidecar(visionModelChangePatch(value, visionOn));
    },
    toggleVision: () => {
      saveSidecar(visionEnabledPatch(!visionOn));
    },
  };
}
