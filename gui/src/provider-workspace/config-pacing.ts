export type ConfigPacingRule = {
  requestsPerMinute?: number;
  minIntervalMs?: number;
};

export type ConfigPacingSource = {
  requestPacing?: {
    enabled?: boolean;
    requestsPerMinute?: number;
    minIntervalMs?: number;
    models?: Record<string, ConfigPacingRule>;
  };
  requestPacingMs?: number;
};

export type ConfigPacingView = {
  enabled: boolean;
  rpm: number | undefined;
  minMs: number | undefined;
  models: Record<string, ConfigPacingRule>;
};

export type ConfigPacingPayload = {
  enabled: boolean;
  requestsPerMinute?: number;
  minIntervalMs?: number;
  models?: Record<string, ConfigPacingRule>;
};

export function pacingFromItem(item: ConfigPacingSource): ConfigPacingView {
  const rpm = item.requestPacing?.requestsPerMinute;
  const minMs = item.requestPacing?.minIntervalMs ?? item.requestPacingMs;
  const models = { ...item.requestPacing?.models };
  const hasLimit = (rpm != null && rpm > 0) || (minMs != null && minMs > 0) || Object.keys(models).length > 0;
  const enabled = item.requestPacing?.enabled === true || (item.requestPacing?.enabled !== false && hasLimit);
  return { enabled, rpm, minMs, models };
}

export function configPacingPayload(input: {
  enabled: boolean;
  rpm: string;
  delay: string;
  models: Record<string, ConfigPacingRule>;
}): ConfigPacingPayload {
  const rpm = Number(input.rpm);
  const delay = Number(input.delay);
  return {
    enabled: input.enabled,
    ...(Number.isFinite(rpm) && rpm > 0 ? { requestsPerMinute: rpm } : {}),
    ...(Number.isSafeInteger(delay) && delay > 0 ? { minIntervalMs: delay } : {}),
    ...(Object.keys(input.models).length > 0 ? { models: input.models } : {}),
  };
}

export function configPacingMissingRule(body: ConfigPacingPayload): boolean {
  return Boolean(body.enabled && !body.requestsPerMinute && !body.minIntervalMs && !body.models);
}

export function configOverrideRule(rpmDraft: string, delayDraft: string): ConfigPacingRule | null {
  const rpm = Number(rpmDraft);
  const delay = Number(delayDraft);
  const rule: ConfigPacingRule = {
    ...(Number.isFinite(rpm) && rpm > 0 ? { requestsPerMinute: rpm } : {}),
    ...(Number.isSafeInteger(delay) && delay > 0 ? { minIntervalMs: delay } : {}),
  };
  if (rule.requestsPerMinute == null && rule.minIntervalMs == null) return null;
  return rule;
}

export function configGeneralPatch(input: {
  openaiLogical: boolean;
  adapter: string;
  itemAdapter: string;
  baseUrl: string;
  itemBaseUrl: string;
  defaultModel: string;
  note: string;
}): {
  adapter?: string;
  baseUrl?: string;
  defaultModel: string;
  note: string;
} {
  return {
    ...(input.openaiLogical ? {} : {
      adapter: input.adapter.trim() || input.itemAdapter,
      baseUrl: input.baseUrl.trim() || input.itemBaseUrl,
    }),
    defaultModel: input.defaultModel.trim(),
    note: input.note.trim(),
  };
}

export function configDiscoveryPatch(input: {
  allowPrivateNetwork: boolean;
  discoverySupported: boolean;
  liveModels: boolean;
  savedLiveModels: boolean;
}): {
  allowPrivateNetwork: boolean;
  liveModels?: boolean;
} {
  return {
    allowPrivateNetwork: input.allowPrivateNetwork,
    ...(input.discoverySupported && input.liveModels !== input.savedLiveModels
      ? { liveModels: input.liveModels }
      : {}),
  };
}

export function configDash(value: string | number | undefined): string {
  if (typeof value === "number" && Number.isFinite(value)) return String(value);
  const trimmed = typeof value === "string" ? value.trim() : "";
  return trimmed ? trimmed : "—";
}

export function configOnOff(t: (key: "pws.enabledLabel" | "pws.disabledLabel") => string, on: boolean): { label: string; on: boolean } {
  return { label: on ? t("pws.enabledLabel") : t("pws.disabledLabel"), on };
}
