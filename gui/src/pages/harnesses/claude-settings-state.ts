export const AUTO_COMPACT_DEFAULT = 829_800;
export const AUTO_COMPACT_MIN = 100_000;
export const AUTO_COMPACT_MAX = 1_000_000;
export const CATALOG_LOAD_FAILED = "Could not load the model catalog.";

export type ClaudeAuthMode = "auto" | "subscription" | "proxy";

export type ClaudeSettingsDraft = {
  authMode: ClaudeAuthMode;
  autoContext: boolean;
  autoCompactWindow: number | null;
  injectAgents: boolean;
  smallFastModel: string;
  opus: string;
  sonnet: string;
  haiku: string;
  fable: string;
};

export type ClaudeSettingsSnapshot = {
  draft: ClaudeSettingsDraft;
  port: number;
};

const FAMILY_KEYS = ["opus", "sonnet", "haiku", "fable"] as const;
type FamilyKey = (typeof FAMILY_KEYS)[number];

function isRecord(value: unknown): value is Record<string, unknown> {
  return !!value && typeof value === "object" && !Array.isArray(value);
}

function readString(value: unknown): string {
  return typeof value === "string" ? value.trim() : "";
}

function readFamily(block: Record<string, unknown> | null, key: FamilyKey): string {
  return readString(block?.[key]);
}

function familyFromPayload(payload: Record<string, unknown>): Pick<ClaudeSettingsDraft, FamilyKey> {
  const canonical = isRecord(payload.tierModels) ? payload.tierModels : null;
  const legacy = isRecord(payload.modelMap) ? payload.modelMap : null;
  const pick = (key: FamilyKey) => readFamily(canonical, key) || readFamily(legacy, key);
  return {
    opus: pick("opus"),
    sonnet: pick("sonnet"),
    haiku: pick("haiku"),
    fable: pick("fable"),
  };
}

function decodeAuthMode(value: unknown): ClaudeAuthMode {
  return value === "subscription" || value === "proxy" ? value : "auto";
}

function decodeCompactWindow(value: unknown): number | null {
  if (value == null || value === "") return null;
  const n = typeof value === "number" ? value : Number(value);
  if (!Number.isInteger(n) || n < AUTO_COMPACT_MIN || n > AUTO_COMPACT_MAX) return null;
  return n;
}

export function emptyClaudeSettingsDraft(): ClaudeSettingsDraft {
  return {
    authMode: "auto",
    autoContext: true,
    autoCompactWindow: null,
    injectAgents: true,
    smallFastModel: "",
    opus: "",
    sonnet: "",
    haiku: "",
    fable: "",
  };
}

export function decodeClaudeSettings(payload: unknown): ClaudeSettingsSnapshot {
  const rec = isRecord(payload) ? payload : {};
  const families = familyFromPayload(rec);
  const port = typeof rec.port === "number" && rec.port > 0 ? rec.port : 23100;
  return {
    port,
    draft: {
      authMode: decodeAuthMode(rec.authMode),
      autoContext: rec.autoContext !== false,
      autoCompactWindow: decodeCompactWindow(rec.autoCompactWindow),
      injectAgents: rec.injectAgents !== false,
      smallFastModel: readString(rec.smallFastModel),
      ...families,
    },
  };
}

export function formatCompactTokens(value: number): string {
  return `${Math.round(value / 1_000)}k`;
}

export function parseCompactInput(raw: string): number | null {
  const text = raw.trim().toLowerCase();
  if (!text) return null;
  const k = text.endsWith("k");
  const n = Number(k ? text.slice(0, -1) : text);
  if (!Number.isFinite(n)) return null;
  const tokens = k ? Math.round(n * 1_000) : n;
  if (!Number.isInteger(tokens) || tokens < AUTO_COMPACT_MIN || tokens > AUTO_COMPACT_MAX) return null;
  return tokens;
}

export function compactFieldValue(override: number | null): string {
  return String(override ?? AUTO_COMPACT_DEFAULT);
}

export function compactInputToOverride(raw: string, baseline: number | null): number | null | "invalid" {
  const parsed = parseCompactInput(raw);
  if (parsed == null) return "invalid";
  if (baseline == null && parsed === AUTO_COMPACT_DEFAULT) return null;
  return parsed;
}

export function familyWriteObject(draft: ClaudeSettingsDraft): Record<string, string> {
  const out: Record<string, string> = {};
  if (draft.opus) out.opus = draft.opus;
  if (draft.sonnet) out.sonnet = draft.sonnet;
  if (draft.haiku) out.haiku = draft.haiku;
  if (draft.fable) out.fable = draft.fable;
  return out;
}

function familiesEqual(a: ClaudeSettingsDraft, b: ClaudeSettingsDraft): boolean {
  return a.opus === b.opus && a.sonnet === b.sonnet && a.haiku === b.haiku && a.fable === b.fable;
}

export function claudeSettingsDirty(baseline: ClaudeSettingsDraft, draft: ClaudeSettingsDraft): boolean {
  return Object.keys(claudeSettingsPutBody(baseline, draft)).length > 0;
}

export function claudeSettingsPutBody(
  baseline: ClaudeSettingsDraft,
  draft: ClaudeSettingsDraft,
): Record<string, unknown> {
  const body: Record<string, unknown> = {};
  if (draft.authMode !== baseline.authMode) body.authMode = draft.authMode;
  if (draft.autoContext !== baseline.autoContext) body.autoContext = draft.autoContext;
  if (draft.autoCompactWindow !== baseline.autoCompactWindow) {
    body.autoCompactWindow = draft.autoCompactWindow;
  }
  if (draft.injectAgents !== baseline.injectAgents) body.injectAgents = draft.injectAgents;
  if (draft.smallFastModel !== baseline.smallFastModel) body.smallFastModel = draft.smallFastModel;
  if (!familiesEqual(baseline, draft)) body.tierModels = familyWriteObject(draft);
  return body;
}

export type CatalogPickerOption = { value: string; id: string };

export function decodeModelsPayload(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  if (isRecord(raw) && Array.isArray(raw.models)) return raw.models;
  throw new Error(CATALOG_LOAD_FAILED);
}

export function acceptSettingsWrite(currentGeneration: number, startedGeneration: number, aborted = false): boolean {
  return !aborted && currentGeneration === startedGeneration;
}

function catalogRows(raw: unknown): unknown[] {
  if (Array.isArray(raw)) return raw;
  if (isRecord(raw) && Array.isArray(raw.models)) return raw.models;
  return [];
}

function routedPickerOption(row: unknown): CatalogPickerOption | null {
  if (!isRecord(row) || row.disabled === true) return null;
  const provider = readString(row.provider);
  const id = readString(row.id);
  if (provider === "combo" || provider === "policy") return null;
  const namespaced = readString(row.namespaced);
  const value = namespaced || (provider && id ? `${provider}/${id}` : "");
  if (!value || value.startsWith("combo/") || value.startsWith("policy/")) return null;
  return { value, id: id || (value.includes("/") ? value.slice(value.lastIndexOf("/") + 1) : value) };
}

function extraPickerId(value: string): string {
  return value.includes("/") ? value.slice(value.lastIndexOf("/") + 1) : value;
}

export function catalogPickerOptions(raw: unknown, extras: readonly string[] = []): CatalogPickerOption[] {
  const seen = new Set<string>();
  const options: CatalogPickerOption[] = [];
  const push = (value: string, id: string) => {
    const next = value.trim();
    if (!next || seen.has(next)) return;
    seen.add(next);
    options.push({ value: next, id: id.trim() || next });
  };
  for (const row of catalogRows(raw)) {
    const option = routedPickerOption(row);
    if (option) push(option.value, option.id);
  }
  for (const extra of extras) {
    const value = extra.trim();
    push(value, extraPickerId(value));
  }
  return options;
}

export function pickerValue(stored: string, options: readonly CatalogPickerOption[]): string {
  if (!stored) return "";
  if (options.some((option) => option.value === stored)) return stored;
  const byId = options.filter((option) => option.id === stored);
  return byId.length === 1 ? byId[0]!.value : stored;
}

export function manualSetupLines(draft: ClaudeSettingsDraft, port: number): string[] {
  const base = `http://127.0.0.1:${port > 0 ? port : 23100}`;
  const lines = [
    `export ANTHROPIC_BASE_URL=${base}`,
    "export CLAUDE_CODE_ENABLE_GATEWAY_MODEL_DISCOVERY=1",
  ];
  if (draft.authMode === "proxy") {
    lines.splice(1, 0, "export ANTHROPIC_AUTH_TOKEN=benes-proxy");
    lines.push("export CLAUDE_CODE_PROVIDER_MANAGED_BY_HOST=1");
  } else if (draft.authMode === "subscription") {
    lines.splice(1, 0, "# no ANTHROPIC_AUTH_TOKEN: Claude subscription login stays active");
  } else {
    lines.splice(1, 0, "# auth mode auto: benes claude resolves proxy vs subscription at launch");
  }
  if (draft.autoContext) {
    lines.push(`export CLAUDE_CODE_AUTO_COMPACT_WINDOW=${draft.autoCompactWindow ?? AUTO_COMPACT_DEFAULT}`);
  }
  const env: Array<[string, string]> = [
    ["ANTHROPIC_DEFAULT_OPUS_MODEL", draft.opus],
    ["ANTHROPIC_DEFAULT_SONNET_MODEL", draft.sonnet],
    ["ANTHROPIC_DEFAULT_HAIKU_MODEL", draft.haiku || draft.smallFastModel],
    ["ANTHROPIC_DEFAULT_FABLE_MODEL", draft.fable],
    ["ANTHROPIC_SMALL_FAST_MODEL", draft.haiku || draft.smallFastModel],
  ];
  for (const [name, value] of env) {
    if (value) lines.push(`export ${name}=${value}`);
  }
  lines.push("benes claude");
  return lines;
}

export const CLAUDE_SETTINGS_FORBIDDEN_PUT_KEYS = [
  "enabled",
  "systemEnv",
  "fastMode",
  "webSearchSidecar",
  "visionSidecar",
  "aliases",
  "blockedSkills",
  "available",
  "modelMap",
] as const;
