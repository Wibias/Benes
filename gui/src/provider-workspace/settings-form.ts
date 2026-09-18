export type SettingsPacingRule = {
  requestsPerMinute?: number;
  minIntervalMs?: number;
};

export type SettingsPacing = {
  enabled?: boolean;
  requestsPerMinute?: number;
  minIntervalMs?: number;
  models?: Record<string, SettingsPacingRule>;
};

export type SettingsSavePatch = {
  adapter?: string;
  baseUrl?: string;
  defaultModel?: string;
  authMode?: string;
  note?: string;
  allowPrivateNetwork?: boolean;
  liveModels?: boolean;
  apiKeyTransport?: "x-api-key" | "bearer" | "";
  requestPacing?: SettingsPacing;
};

export type SettingsSaveDecision =
  | { ok: false; messageKey: "pws.adapterBaseRequired" | "pws.pacingRuleRequired" }
  | { ok: true; patch: SettingsSavePatch };

export function numberDraft(value: number | undefined): string {
  return value === undefined ? "" : String(value);
}

export function positiveRpm(value: string): number | undefined {
  if (!value.trim()) return undefined;
  const parsed = Number(value);
  return Number.isFinite(parsed) && parsed >= 1 / 60 ? parsed : undefined;
}

export function positiveInteger(value: string): number | undefined {
  if (!value.trim()) return undefined;
  const parsed = Number(value);
  return Number.isSafeInteger(parsed) && parsed > 0 ? parsed : undefined;
}

export function pacingSignature(value: SettingsPacing | undefined): string {
  const models = Object.entries(value?.models ?? {})
    .sort(([left], [right]) => left.localeCompare(right))
    .map(([model, rule]) => [model, rule.requestsPerMinute ?? null, rule.minIntervalMs ?? null]);
  return JSON.stringify([
    value?.enabled === true,
    value?.requestsPerMinute ?? null,
    value?.minIntervalMs ?? null,
    models,
  ]);
}

export function settingsPacingDraft(input: {
  enabled: boolean;
  rpm: string;
  delay: string;
  models: Record<string, SettingsPacingRule>;
}): SettingsPacing {
  const rpm = positiveRpm(input.rpm);
  const delay = positiveInteger(input.delay);
  return {
    enabled: input.enabled,
    ...(rpm !== undefined ? { requestsPerMinute: rpm } : {}),
    ...(delay !== undefined ? { minIntervalMs: delay } : {}),
    ...(Object.keys(input.models).length > 0 ? { models: input.models } : {}),
  };
}

export function settingsSupportsApiKeyTransport(adapter: string, authMode: string): boolean {
  return adapter.trim() === "anthropic" && authMode === "key";
}

export function settingsPlainBaseUrlLocked(isPreset: boolean, choicesStatus: string): boolean {
  return isPreset && choicesStatus !== "error";
}

export function settingsFormDirty(input: {
  adapter: string;
  baseUrl: string;
  defaultModel: string;
  authMode: string;
  apiKeyTransport: string;
  note: string;
  allowPrivateNetwork: boolean;
  liveModels: boolean;
  savedLiveModels: boolean;
  itemAdapter: string;
  itemBaseUrl: string;
  itemDefaultModel: string | undefined;
  itemAuthMode: string | undefined;
  itemKeyOptional: boolean | undefined;
  itemApiKeyTransport: string | undefined;
  itemNote: string | undefined;
  itemAllowPrivateNetwork: boolean | undefined;
}): boolean {
  return input.adapter.trim() !== input.itemAdapter
    || input.baseUrl.trim() !== input.itemBaseUrl
    || input.defaultModel.trim() !== (input.itemDefaultModel ?? "")
    || input.authMode !== String(input.itemAuthMode ?? (input.itemKeyOptional ? "local" : "key"))
    || (settingsSupportsApiKeyTransport(input.adapter, input.authMode)
      && input.apiKeyTransport !== (input.itemApiKeyTransport ?? "x-api-key"))
    || input.note.trim() !== (input.itemNote ?? "")
    || input.allowPrivateNetwork !== (input.itemAllowPrivateNetwork ?? false)
    || input.liveModels !== input.savedLiveModels;
}

export function settingsSavePatch(input: {
  adapter: string;
  nextBaseUrl: string;
  defaultModel: string;
  authMode: string;
  apiKeyTransport: string;
  note: string;
  allowPrivateNetwork: boolean;
  liveModels: boolean;
  liveModelDiscoverySupported: boolean;
  itemLiveModels: boolean | undefined;
  itemApiKeyTransport: string | undefined;
  pacingEnabled: boolean;
  pacingDraft: SettingsPacing;
  dirty: boolean;
  pacingDirty: boolean;
}): SettingsSaveDecision {
  if (!input.adapter.trim() || !input.nextBaseUrl) {
    return { ok: false, messageKey: "pws.adapterBaseRequired" };
  }
  if (
    input.pacingEnabled
    && !input.pacingDraft.requestsPerMinute
    && !input.pacingDraft.minIntervalMs
    && !input.pacingDraft.models
  ) {
    return { ok: false, messageKey: "pws.pacingRuleRequired" };
  }
  const pacingOnly = input.pacingDirty && !input.dirty;
  if (pacingOnly) return { ok: true, patch: { requestPacing: input.pacingDraft } };

  const patch: SettingsSavePatch = {
    adapter: input.adapter.trim(),
    baseUrl: input.nextBaseUrl,
    defaultModel: input.defaultModel.trim(),
    authMode: input.authMode,
    note: input.note.trim(),
    allowPrivateNetwork: input.allowPrivateNetwork,
    ...(input.pacingDirty ? { requestPacing: input.pacingDraft } : {}),
  };
  if (input.liveModelDiscoverySupported && input.liveModels !== (input.itemLiveModels !== false)) {
    patch.liveModels = input.liveModels;
  }
  if (settingsSupportsApiKeyTransport(input.adapter, input.authMode)) {
    patch.apiKeyTransport = input.apiKeyTransport as "x-api-key" | "bearer";
  } else if (input.itemApiKeyTransport !== undefined) {
    patch.apiKeyTransport = "";
  }
  return { ok: true, patch };
}

export function settingsOverrideRule(modelId: string, rpmDraft: string, delayDraft: string): {
  modelId: string;
  rule: SettingsPacingRule;
} | null {
  const id = modelId.trim();
  const rpm = positiveRpm(rpmDraft);
  const delay = positiveInteger(delayDraft);
  if (!id || (rpm === undefined && delay === undefined)) return null;
  return {
    modelId: id,
    rule: {
      ...(rpm !== undefined ? { requestsPerMinute: rpm } : {}),
      ...(delay !== undefined ? { minIntervalMs: delay } : {}),
    },
  };
}

export function settingsInitialAuthMode(authMode: string | undefined, keyOptional: boolean | undefined): string {
  return String(authMode ?? (keyOptional ? "local" : "key"));
}

export function settingsChoicesStatus(hasApiBase: boolean): "idle" | "loading" {
  return hasApiBase ? "loading" : "idle";
}

export function settingsHasEndpointPicker(choicesStatus: string, choiceCount: number): boolean {
  return choicesStatus === "ready" && choiceCount > 0;
}

export function settingsOpenAiView(
  itemName: string,
  state: "absent" | "disabled" | "ready" | "invalid",
): { state: "absent" | "disabled" | "ready" | "invalid"; canonical: boolean } {
  if (itemName !== "openai") return { state: "invalid", canonical: false };
  return { state, canonical: state === "ready" || state === "disabled" };
}

export function settingsEndpointLabel(
  t: (key: "modal.endpoint.tokenPlan" | "modal.endpoint.payAsYouGo" | "modal.endpoint.custom") => string,
  id: string,
  fallback: string,
): string {
  if (id === "token-plan") return t("modal.endpoint.tokenPlan");
  if (id === "payg") return t("modal.endpoint.payAsYouGo");
  if (id === "custom") return t("modal.endpoint.custom");
  return fallback;
}
