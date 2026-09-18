/**
 * Benes dashboard source. Every request the API workspace makes.
 *
 * The page owns state and this module owns traffic, so a surface can be reasoned
 * about without a component in the way: reads go through the page's data
 * surfaces, writes go through `/api/keys`, and the probe posts to the proxy's
 * own listener with the one-time key the page is holding.
 *
 * Two owners are deliberately reused rather than restated. The clipboard is
 * `copy-feedback`'s, which already reports an honest `copied`/`unavailable`
 * outcome and carries the legacy path a plain-HTTP LAN dashboard needs. The
 * probe's body, URL, error sentence, and credential redaction come from
 * `api-access`, so this module never invents a second copy of either.
 */
import { createBoundedFetch } from "../bounded-fetch";
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json";
import { writeSessionListCacheEntry } from "../session-list-cache";
import { copyTextToClipboard } from "../copy-feedback";
import { externalModelId, readModelsCatalog, type ExternalModelRow } from "../api-access/model-catalog-view.ts";
import type { ApiEndpointInfo } from "../api-access/endpoints.ts";
import {
  probeFailure,
  modelTestErrorMessage,
  modelTestRequest,
  type GatewayInboundProtocol,
  type ModelProbe,
  type ModelProbeResults,
} from "../api-access/model-tests.ts";
import type { TFn } from "../i18n/shared";
import {
  decodeKeysPayload,
  type CachedKeysShape,
} from "./api-keys-decode.ts";

/** The three routes this workspace talks to; the listener owns their shapes. */
const KEYS_ROUTE = "/api/keys";
const MODELS_ROUTE = "/v1/models";

/** What a create answers with: the one plaintext key, and nothing else used. */
interface CreateKeyResponse {
  key?: unknown;
}

/**
 * One read or write against `/api/keys`. The route takes both its writes in the
 * body, so a mutation is the method plus those fields.
 */
function keyRequest(
  apiBase: string,
  method: "POST" | "DELETE",
  fields: Record<string, unknown>,
  signal?: AbortSignal,
): Promise<Response> {
  return fetch(`${apiBase}${KEYS_ROUTE}`, {
    method,
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(fields),
    ...(signal ? { signal } : {}),
  });
}

/** The key board, decoded and written back to the session cache. */
export async function loadKeysPayload(
  apiBase: string,
  signal: AbortSignal,
  cacheKey: string,
  t: TFn,
): Promise<CachedKeysShape> {
  const response = await fetch(`${apiBase}${KEYS_ROUTE}`, { signal });
  const payload = await readJsonIfOk<unknown>(response);
  const board = payload ? decodeKeysPayload(payload) : null;
  if (!board) throw new Error(t("api.keysLoadFailed"));
  writeSessionListCacheEntry(cacheKey, board);
  return board;
}

/** The catalogue as the Models panel lists it, cached the same way. */
export async function loadModelsCatalog(
  apiBase: string,
  signal: AbortSignal,
  cacheKey: string,
  t: TFn,
): Promise<ExternalModelRow[]> {
  const response = await fetch(`${apiBase}${MODELS_ROUTE}`, { signal });
  if (!response.ok) throw new Error(t("api.modelsLoadFailed"));
  const rows = readModelsCatalog(await response.json());
  if (!rows) throw new Error(t("api.modelsLoadFailed"));
  writeSessionListCacheEntry(cacheKey, rows);
  return rows;
}

/**
 * Create a key and hand back the plaintext exactly once.
 *
 * A blank name is the listener's `default` rather than an empty one; the
 * response is the only place the dashboard ever sees a full key.
 */
export async function createApiKey(apiBase: string, name: string, t: TFn): Promise<string | null> {
  const response = await keyRequest(apiBase, "POST", { name: name || "default" });
  const payload = await readJsonOrThrow<CreateKeyResponse>(response, t("api.createFailed"));
  const key = payload?.key;
  return typeof key === "string" && key.length > 0 ? key : null;
}

/** Remove a stored key. Bounded, because a hung delete must not pin the row. */
export async function deleteApiKey(apiBase: string, id: string, timeoutMs: number): Promise<boolean> {
  const bounded = createBoundedFetch(timeoutMs);
  try {
    const response = await keyRequest(apiBase, "DELETE", { id }, bounded.signal);
    return response.ok;
  } catch {
    return false;
  } finally {
    bounded.clear();
  }
}

/**
 * One live probe, on the wire the reader asked for.
 *
 * A refusal is reported through `probeFailure`, which is the only constructor
 * that redacts credential-shaped text, and the probe authenticates with the
 * freshly created key rather than any stored one.
 */
export async function testModelProtocol(
  model: ExternalModelRow,
  protocol: GatewayInboundProtocol,
  apiKey: string,
  endpoints: ApiEndpointInfo,
  t: TFn,
): Promise<ModelProbe> {
  const probe = modelTestRequest(protocol, externalModelId(model), endpoints);
  try {
    const response = await fetch(probe.url, {
      method: "POST",
      headers: { "Content-Type": "application/json", "x-benes-api-key": apiKey },
      body: JSON.stringify(probe.body),
    });
    if (!response.ok) return probeFailure(modelTestErrorMessage(await response.text(), response.status));
    return { status: "ok" };
  } catch (error) {
    return probeFailure(error instanceof Error ? error.message : t("api.testFailed"));
  }
}
/** How long a "Copied" reveal stays on screen. */
const COPY_REVEAL_MS = 2000;

/**
 * Create a key and publish the plaintext once.
 *
 * The busy flag is raised for the whole attempt so the dialog cannot fire two
 * creates, and every failure path — refused response or thrown request — ends in
 * the same action-error copy rather than a silent no-op.
 */
export async function runCreateKey(input: {
  creating: boolean;
  apiBase: string;
  name: string;
  t: TFn;
  setCreating: (value: boolean) => void;
  setActionError: (value: string | null) => void;
  setNewKey: (value: string | null) => void;
  setNewName: (value: string) => void;
  refresh: () => void;
}): Promise<boolean> {
  if (input.creating) return false;
  input.setCreating(true);
  input.setActionError(null);
  try {
    const key = await createApiKey(input.apiBase, input.name, input.t);
    if (key === null) {
      input.setActionError(input.t("api.createFailed"));
      return false;
    }
    input.setNewKey(key);
    input.setNewName("");
    input.refresh();
    return true;
  } catch {
    input.setActionError(input.t("api.createFailed"));
    return false;
  } finally {
    input.setCreating(false);
  }
}

/**
 * Copy the one-time key, and say so only when the clipboard confirms it.
 *
 * The outcome is the clipboard owner's, so a denied or absent clipboard raises
 * the action error instead of showing a "Copied" label over a failed write.
 */
export async function runCopyKey(input: {
  newKey: string | null;
  t: TFn;
  setActionError: (value: string | null) => void;
  setCopied: (value: boolean) => void;
}): Promise<void> {
  if (!input.newKey) return;
  input.setActionError(null);
  if (!await copyTextToClipboard(input.newKey)) {
    input.setCopied(false);
    input.setActionError(input.t("api.key.copyFailed"));
    return;
  }
  input.setCopied(true);
  window.setTimeout(() => input.setCopied(false), COPY_REVEAL_MS);
}

/** Copy a catalogue id, revealing the copied label until the timer expires. */
export async function runCopyModelId(input: {
  modelId: string;
  setCopiedModelId: (value: string | null | ((current: string | null) => string | null)) => void;
}): Promise<void> {
  if (!await copyTextToClipboard(input.modelId)) return;
  const { modelId, setCopiedModelId } = input;
  setCopiedModelId(modelId);
  window.setTimeout(
    () => setCopiedModelId(current => (current === modelId ? null : current)),
    COPY_REVEAL_MS,
  );
}

/**
 * Probe one model on one wire, publishing the chip through testing → result.
 *
 * A failed probe reports its redacted detail to the caller, which is where the
 * toast copy is composed; the chip keeps the same detail for its own tooltip.
 */
export async function runTestModel(input: {
  model: ExternalModelRow;
  protocol: GatewayInboundProtocol;
  apiKey: string | null;
  endpoints: ApiEndpointInfo;
  t: TFn;
  setModelTests: (update: (current: ModelProbeResults) => ModelProbeResults) => void;
  onFailure?: (detail: string) => void;
}): Promise<void> {
  const { model, protocol, apiKey, endpoints, t, setModelTests, onFailure } = input;
  if (!apiKey) return;
  const modelId = externalModelId(model);
  setModelTests(current => ({
    ...current,
    [modelId]: { ...current[modelId], [protocol]: { status: "testing" } },
  }));
  const result = await testModelProtocol(model, protocol, apiKey, endpoints, t);
  setModelTests(current => ({ ...current, [modelId]: { ...current[modelId], [protocol]: result } }));
  if (result.status === "error") onFailure?.(result.detail);
}
