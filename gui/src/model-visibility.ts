/** Benes dashboard client for the Go proxy (`internal/server`). */

export type ProviderModelMap = Record<string, string[]>;
export type ModelVisibilityScope = "models" | "provider";
export type ModelPresetMode = "preset" | "all" | "custom";
export type ModelPresetMap = Record<string, ModelPresetRow>;
export type NewModelPolicy = "on" | "off";

export interface ModelVisibilityTarget {
  id: string;
  native?: boolean;
}

export type ModelVisibilityWrite = {
  scope: ModelVisibilityScope;
  provider: string;
  targets: ModelVisibilityTarget[];
  enabled: boolean;
  disabled: Iterable<string>;
  selected?: ProviderModelMap | null;
};

export interface ModelPresetRow {
  mode: ModelPresetMode;
  available: boolean;
  version?: number;
  matched: number;
  catalog: number;
  selected?: string[];
  warning?: string;
}

export interface ModelDiscoveryArrival {
  id: string;
  state: "auto-disabled" | "enabled";
}

export interface ModelDiscoveryProviderRow {
  policy: NewModelPolicy;
  arrivals: ModelDiscoveryArrival[];
  skip: boolean;
}

export interface ModelDiscoveryState {
  newModelPolicy: NewModelPolicy;
  providers: Record<string, ModelDiscoveryProviderRow>;
}

type UnknownRecord = Record<string, unknown>;
type JsonParser<T> = (value: unknown) => T;

function recordOrThrow(value: unknown, message: string): UnknownRecord {
  if (value === null || typeof value !== "object" || Array.isArray(value)) {
    throw new Error(message);
  }
  return value as UnknownRecord;
}

function stringArrayOrThrow(value: unknown): string[] {
  if (!Array.isArray(value)) throw new Error("invalid model list");
  const unique = new Set<string>();
  for (const item of value) {
    if (typeof item !== "string") throw new Error("invalid model list");
    unique.add(item);
  }
  return [...unique];
}

async function getParsed<T>(
  apiBase: string,
  path: string,
  label: string,
  parse: JsonParser<T>,
  fetchImpl: typeof fetch,
  signal?: AbortSignal,
): Promise<T> {
  const response = await fetchImpl(`${apiBase}${path}`, signal === undefined ? undefined : { signal });
  if (!response.ok) throw new Error(`${label} HTTP ${response.status}`);
  return parse(await response.json());
}

function putJson(
  apiBase: string,
  path: string,
  body: unknown,
  fetchImpl: typeof fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}${path}`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

export function parseSelectedModels(value: unknown): ProviderModelMap {
  const root = recordOrThrow(value, "invalid selected models response");
  const providers = recordOrThrow(root.selected, "invalid selected models response");
  const result: ProviderModelMap = {};
  for (const [provider, ids] of Object.entries(providers)) {
    result[provider] = stringArrayOrThrow(ids);
  }
  return result;
}

export function fetchSelectedModels(
  apiBase: string,
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<ProviderModelMap> {
  return getParsed(
    apiBase,
    "/api/selected-models",
    "selected models",
    parseSelectedModels,
    fetchImpl,
    signal,
  );
}

export function modelIncluded(
  selected: ProviderModelMap,
  provider: string,
  modelId: string,
  native = false,
): boolean {
  if (native) return true;
  const configured = selected[provider];
  return configured === undefined || configured.length === 0 || configured.includes(modelId);
}

export function modelVisible(
  selected: ProviderModelMap,
  provider: string,
  modelId: string,
  native: boolean,
  blocked: boolean,
): boolean {
  if (blocked) return false;
  return modelIncluded(selected, provider, modelId, native);
}

function canonicalTargetId(provider: string, modelId: string): string | null {
  const id = modelId.trim();
  if (id.length === 0) return null;
  return id.includes("/") ? id : `${provider}/${id}`;
}

function unqualifiedId(id: string): string {
  const slash = id.indexOf("/");
  return slash > 0 && slash < id.length - 1 ? id.slice(slash + 1) : id;
}

function normalizedDisabled(current: Iterable<string>): Set<string> {
  const result = new Set<string>();
  for (const value of current) {
    const id = String(value).trim();
    if (id.length > 0) result.add(id);
  }
  return result;
}

export function nextDisabledModels(
  current: Iterable<string>,
  provider: string,
  targets: ModelVisibilityTarget[],
  enabled: boolean,
): string[] {
  const result = normalizedDisabled(current);
  for (const target of targets) {
    const canonical = canonicalTargetId(provider, target.id);
    if (canonical === null) continue;

    if (!enabled) {
      result.add(canonical);
      continue;
    }

    result.delete(canonical);
    result.delete(unqualifiedId(canonical));
    result.delete(target.id.trim());
  }
  return Array.from(result).sort();
}

function customTargetIds(targets: ModelVisibilityTarget[]): string[] {
  const ids = new Set<string>();
  for (const target of targets) {
    if (target.native === true) continue;
    const id = target.id.trim();
    if (id.length > 0) ids.add(id);
  }
  return [...ids];
}

export function nextSelectedAllowlist(
  selected: ProviderModelMap | null | undefined,
  scope: ModelVisibilityScope,
  provider: string,
  targets: ModelVisibilityTarget[],
  enabled: boolean,
): string[] | undefined {
  if (!enabled) return undefined;

  const requested = customTargetIds(targets);
  if (requested.length === 0) return undefined;

  const existing = selected?.[provider];
  if (existing === undefined || existing.length === 0) return undefined;
  if (scope === "provider") return [];

  const merged = new Set(existing);
  const before = merged.size;
  for (const id of requested) merged.add(id);
  return merged.size === before ? undefined : [...merged];
}

export async function putModelVisibility(
  apiBase: string,
  write: ModelVisibilityWrite,
  fetchImpl: typeof fetch = fetch,
): Promise<Response> {
  const disabled = nextDisabledModels(
    write.disabled,
    write.provider,
    write.targets,
    write.enabled,
  );
  const disabledResponse = await putJson(
    apiBase,
    "/api/disabled-models",
    { models: disabled },
    fetchImpl,
  );
  if (!disabledResponse.ok) return disabledResponse;

  const allowlist = nextSelectedAllowlist(
    write.selected,
    write.scope,
    write.provider,
    write.targets,
    write.enabled,
  );
  if (allowlist === undefined) return disabledResponse;

  return putJson(
    apiBase,
    "/api/selected-models",
    { provider: write.provider, models: allowlist },
    fetchImpl,
  );
}

export function shouldApplyLoadGeneration(request: number, current: number): boolean {
  return request === current;
}

function presetMode(value: unknown): ModelPresetMode {
  switch (value) {
    case "preset":
    case "all":
    case "custom":
      return value;
    default:
      return "custom";
  }
}

function parsePresetRow(value: unknown): ModelPresetRow {
  const raw = recordOrThrow(value, "invalid model presets response");
  if (typeof raw.matched !== "number" || typeof raw.catalog !== "number") {
    throw new Error("invalid model presets response");
  }

  const row: ModelPresetRow = {
    mode: presetMode(raw.mode),
    available: raw.available === true,
    matched: raw.matched,
    catalog: raw.catalog,
  };
  if (typeof raw.version === "number") row.version = raw.version;
  if (typeof raw.warning === "string" && raw.warning.length > 0) row.warning = raw.warning;
  if (Array.isArray(raw.selected) && raw.selected.every(item => typeof item === "string")) {
    row.selected = raw.selected;
  }
  return row;
}

export function parseModelPresets(value: unknown): ModelPresetMap {
  const root = recordOrThrow(value, "invalid model presets response");
  const providers = recordOrThrow(root.providers, "invalid model presets response");
  const result: ModelPresetMap = {};
  for (const [provider, raw] of Object.entries(providers)) {
    result[provider] = parsePresetRow(raw);
  }
  return result;
}

export function fetchModelPresets(
  apiBase: string,
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<ModelPresetMap> {
  return getParsed(
    apiBase,
    "/api/model-presets",
    "model presets",
    parseModelPresets,
    fetchImpl,
    signal,
  );
}

export function putModelPreset(
  apiBase: string,
  provider: string,
  mode: "preset" | "all",
  fetchImpl: typeof fetch = fetch,
): Promise<Response> {
  return putJson(apiBase, "/api/model-presets", { provider, mode }, fetchImpl);
}

function discoveryPolicy(value: unknown): NewModelPolicy {
  return value === "off" ? "off" : "on";
}

function parseArrival(value: unknown): ModelDiscoveryArrival {
  const raw = recordOrThrow(value, "invalid model discovery response");
  if (typeof raw.id !== "string") throw new Error("invalid model discovery response");
  return {
    id: raw.id,
    state: raw.state === "enabled" ? "enabled" : "auto-disabled",
  };
}

function parseDiscoveryProvider(value: unknown): ModelDiscoveryProviderRow {
  const raw = recordOrThrow(value, "invalid model discovery response");
  if (!Array.isArray(raw.arrivals)) throw new Error("invalid model discovery response");
  return {
    policy: discoveryPolicy(raw.policy),
    arrivals: raw.arrivals.map(parseArrival),
    skip: raw.skip === true,
  };
}

export function parseModelDiscovery(value: unknown): ModelDiscoveryState {
  const root = recordOrThrow(value, "invalid model discovery response");
  const rawProviders = recordOrThrow(root.providers, "invalid model discovery response");
  const providers: Record<string, ModelDiscoveryProviderRow> = {};
  for (const [provider, raw] of Object.entries(rawProviders)) {
    providers[provider] = parseDiscoveryProvider(raw);
  }
  return {
    newModelPolicy: discoveryPolicy(root.newModelPolicy),
    providers,
  };
}

export function fetchModelDiscovery(
  apiBase: string,
  fetchImpl: typeof fetch = fetch,
  signal?: AbortSignal,
): Promise<ModelDiscoveryState> {
  return getParsed(
    apiBase,
    "/api/model-discovery",
    "model discovery",
    parseModelDiscovery,
    fetchImpl,
    signal,
  );
}

export function putModelDiscoveryPolicy(
  apiBase: string,
  policy: NewModelPolicy,
  fetchImpl: typeof fetch = fetch,
): Promise<Response> {
  return putJson(apiBase, "/api/model-discovery", { newModelPolicy: policy }, fetchImpl);
}

export function arrivalOffCount(row: ModelDiscoveryProviderRow | undefined): number {
  if (row === undefined) return 0;
  let count = 0;
  for (const arrival of row.arrivals) {
    if (arrival.state === "auto-disabled") count += 1;
  }
  return count;
}
