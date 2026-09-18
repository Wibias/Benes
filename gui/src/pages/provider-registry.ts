/**
 * Providers registry: config/OAuth loaders and mutation side effects.
 */
import { setClientResourceData } from "../client-resource.ts";
import { readJsonIfOk, readJsonOrThrow } from "../fetch-json.ts";
import type { TFn } from "../i18n/shared.ts";
import { RESERVED_OPENAI_FORWARD_ID } from "../provider-workspace/openai-forward-policy.ts";
import {
  applyNamedProviderPatch,
  deleteNamedProvider,
  setNamedProviderDefault,
  setNamedProviderDisabled,
  type ProviderUpdatePatch,
} from "../provider-workspace/provider-mutations.ts";
import { writeSessionListCache } from "../session-list-cache.ts";
import type { OAuthStatus, ProvidersConfig } from "./providers-shared.ts";

export async function loadProvidersConfig(
  apiBase: string,
  fetchImpl: typeof fetch = fetch,
): Promise<ProvidersConfig | undefined> {
  const response = await fetchImpl(`${apiBase}/api/config`);
  return readJsonOrThrow<ProvidersConfig>(response);
}

export async function loadOauthProviderStatus(
  apiBase: string,
  fetchImpl: typeof fetch = fetch,
): Promise<{ providers: string[]; status: Record<string, OAuthStatus> }> {
  const listResponse = await fetchImpl(`${apiBase}/api/oauth/providers`);
  const list = await readJsonOrThrow<{ providers?: string[] }>(listResponse);
  const providers = list?.providers ?? [];
  const entries = await Promise.all(providers.map(async name => {
    const statusResponse = await fetchImpl(`${apiBase}/api/oauth/status?provider=${encodeURIComponent(name)}`).catch(() => null);
    const status = statusResponse
      ? (await readJsonIfOk<OAuthStatus>(statusResponse) ?? { loggedIn: false })
      : { loggedIn: false };
    return [name, status] as const;
  }));
  return { providers, status: Object.fromEntries(entries) };
}

export function cacheProvidersConfig(cacheKey: string | undefined, data: ProvidersConfig): void {
  if (cacheKey) writeSessionListCache(cacheKey, data);
}

export function providersOauthResourceKey(apiBase: string): string {
  return `providers-oauth:${apiBase}`;
}

export async function reloadPublishedProvidersConfig(
  apiBase: string,
  cacheKey: string,
  fetchImpl: typeof fetch = fetch,
): Promise<ProvidersConfig | undefined> {
  const data = await loadProvidersConfig(apiBase, fetchImpl);
  if (!data) return undefined;
  cacheProvidersConfig(cacheKey, data);
  setClientResourceData(cacheKey, data);
  return data;
}

export async function reloadPublishedOauthStatus(
  apiBase: string,
  resourceKey: string,
  fetchImpl: typeof fetch = fetch,
): Promise<{ providers: string[]; status: Record<string, OAuthStatus> }> {
  const loaded = await loadOauthProviderStatus(apiBase, fetchImpl);
  setClientResourceData(resourceKey, loaded);
  return loaded;
}

export type RegistryRefresh = {
  apiBase: string;
  t: TFn;
  notify: (msg: string, ok?: boolean, options?: { offerHarnessSync?: boolean }) => void;
  refreshConfig: () => Promise<void> | void;
  refreshOauth: () => Promise<void> | void;
  invalidateQuotas: (force?: boolean) => void;
  refreshCodexAccount?: () => Promise<unknown> | unknown;
};

export async function confirmRemoveNamedProvider(
  ctx: RegistryRefresh,
  name: string,
): Promise<{ removed: boolean; defaultProvider?: string }> {
  const result = await deleteNamedProvider({ apiBase: ctx.apiBase, name, t: ctx.t });
  if (!result.ok) {
    ctx.notify(result.error, false);
    return { removed: false };
  }
  ctx.notify(
    result.defaultProvider
      ? ctx.t("prov.removedDefault", { name, defaultProvider: result.defaultProvider })
      : ctx.t("prov.removed", { name }),
    true,
    { offerHarnessSync: true },
  );
  await Promise.resolve(ctx.refreshConfig());
  await Promise.resolve(ctx.refreshOauth());
  ctx.invalidateQuotas(true);
  return { removed: true, defaultProvider: result.defaultProvider ?? undefined };
}

export async function setRegistryProviderDisabled(
  ctx: RegistryRefresh,
  name: string,
  disabled: boolean,
): Promise<boolean> {
  const result = await setNamedProviderDisabled({ apiBase: ctx.apiBase, name, disabled, t: ctx.t });
  if (!result.ok) {
    ctx.notify(result.error, false);
    return false;
  }
  ctx.notify(disabled ? ctx.t("prov.disabled", { name }) : ctx.t("prov.enabled", { name }), true, { offerHarnessSync: true });
  await Promise.resolve(ctx.refreshConfig());
  await Promise.resolve(ctx.refreshOauth());
  ctx.invalidateQuotas(true);
  return true;
}

export async function setRegistryDefaultProvider(ctx: RegistryRefresh, name: string): Promise<boolean> {
  const result = await setNamedProviderDefault({ apiBase: ctx.apiBase, name, t: ctx.t });
  if (!result.ok) {
    ctx.notify(result.error, false);
    return false;
  }
  ctx.notify(ctx.t("prov.setDefaultSuccess", { name }), true);
  await Promise.resolve(ctx.refreshConfig());
  return true;
}

export async function patchRegistryProvider(
  ctx: RegistryRefresh,
  name: string,
  patch: ProviderUpdatePatch,
): Promise<{ ok: boolean; error?: string }> {
  const result = await applyNamedProviderPatch({ apiBase: ctx.apiBase, name, patch, t: ctx.t });
  if (!result.ok) return result;
  await Promise.resolve(ctx.refreshConfig());
  if (Object.hasOwn(patch, "codexAccountMode")) {
    const refreshes: Promise<unknown>[] = [Promise.resolve(ctx.invalidateQuotas(true))];
    if (ctx.refreshCodexAccount) refreshes.push(Promise.resolve(ctx.refreshCodexAccount()));
    await Promise.all(refreshes);
  }
  return { ok: true };
}

export type ReservedOpenAiEnableFailure =
  | "codexAuth.enableOpenaiFailed"
  | "codexAuth.openaiPresetLoadFailed"
  | "codexAuth.openaiPresetUnavailable";

export class OpenAiEnableError extends Error {
  readonly i18nKey: ReservedOpenAiEnableFailure;

  constructor(i18nKey: ReservedOpenAiEnableFailure) {
    super(i18nKey);
    this.name = "OpenAiEnableError";
    this.i18nKey = i18nKey;
  }
}

async function enableReservedOpenAiRow(apiBase: string, fetchImpl: typeof fetch): Promise<void> {
  const response = await fetchImpl(`${apiBase}/api/providers?name=${RESERVED_OPENAI_FORWARD_ID}`, {
    method: "PATCH",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ disabled: false }),
  });
  if (!response.ok) throw new OpenAiEnableError("codexAuth.enableOpenaiFailed");
}

async function createReservedOpenAiFromCatalog(apiBase: string, fetchImpl: typeof fetch): Promise<void> {
  const catalog = await fetchImpl(`${apiBase}/api/provider-presets`);
  if (!catalog.ok) throw new OpenAiEnableError("codexAuth.openaiPresetLoadFailed");
  const parsed = await catalog.json() as { providers?: Array<{ id?: string; provider?: unknown }> };
  const seed = parsed.providers?.find(entry => entry.id === RESERVED_OPENAI_FORWARD_ID)?.provider;
  if (!seed || typeof seed !== "object" || Array.isArray(seed)) {
    throw new OpenAiEnableError("codexAuth.openaiPresetUnavailable");
  }
  const created = await fetchImpl(`${apiBase}/api/providers`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name: RESERVED_OPENAI_FORWARD_ID, provider: structuredClone(seed) }),
  });
  if (!created.ok) throw new OpenAiEnableError("codexAuth.enableOpenaiFailed");
}

export async function ensureOpenAiProvider(
  apiBase: string,
  state: "absent" | "disabled",
  fetchImpl: typeof fetch = fetch,
): Promise<void> {
  if (state === "disabled") {
    await enableReservedOpenAiRow(apiBase, fetchImpl);
    return;
  }
  await createReservedOpenAiFromCatalog(apiBase, fetchImpl);
}
