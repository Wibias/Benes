/**
 * Provider Access credentials: #232 resources for OAuth snapshots, quota enrichment, and API keys.
 * Codex account state is owned by useCodexAccountPool (#236).
 */
import { useCallback, useMemo, useRef, useState, type MutableRefObject, type SetStateAction } from "react";
import { accountNeedsReauth } from "../oauth-health-display.ts";
import { buildActiveAccountNeedsReauthMap } from "../provider-workspace/account-attention.ts";
import {
  activateStoredApiKey,
  activateStoredOAuthAccount,
  dropStoredApiKey,
  dropStoredOAuthAccount,
  renameStoredCredential,
  storeApiKeySecret,
  type CredentialActionResult,
} from "../provider-workspace/provider-credential-actions.ts";
import {
  apiKeyCredentialProviders,
  applyAccountQuotaEnrichment,
  loadApiKeyBoard,
  loadOAuthAccountBoard,
  oauthCredentialProviders,
  type ApiKeyRow,
  type OAuthAccountRow,
  type ProviderAccountSnapshot,
} from "../provider-workspace/provider-credential-api.ts";
import { setClientResourceData, useKeyedClientResource } from "../client-resource.ts";

export type AccountLoadState = "idle" | "loading" | "ready" | "error";

function boardKey(apiBase: string, kind: string, providers: readonly string[]): string {
  return `${kind}:${apiBase}:${providers.join(",")}`;
}

export function useProviderCredentials(deps: {
  apiBase: string;
  t: (key: string, ...args: unknown[]) => string;
  config: { providers: Record<string, { authMode?: string; hasApiKey?: boolean }> } | null;
  aliveRef: MutableRefObject<boolean>;
  notify: (msg: string, ok?: boolean) => void;
  refreshConfig: () => Promise<void> | void;
  refreshOauth: () => Promise<void> | void;
  invalidateQuotas: (force?: boolean) => void;
  codexActiveNeedsReauth: boolean;
}) {
  const { apiBase, t, config, aliveRef, notify, refreshConfig, refreshOauth, invalidateQuotas, codexActiveNeedsReauth } = deps;
  const oauthProviders = useMemo(() => oauthCredentialProviders(config?.providers), [config]);
  const keyProviders = useMemo(() => apiKeyCredentialProviders(config?.providers), [config]);
  const cheapKey = boardKey(apiBase, "provider-oauth-accounts", oauthProviders);
  const quotaKey = boardKey(apiBase, "provider-oauth-quotas", oauthProviders);
  const keysKey = boardKey(apiBase, "provider-api-keys", keyProviders);

  const cheap = useKeyedClientResource(
    cheapKey,
    [cheapKey],
    async (signal) => loadOAuthAccountBoard(apiBase, oauthProviders, { signal }),
    { enabled: oauthProviders.length > 0 },
  );
  const enriched = useKeyedClientResource(
    quotaKey,
    [quotaKey],
    async (signal) => loadOAuthAccountBoard(apiBase, oauthProviders, { quota: true, signal }),
    { enabled: oauthProviders.length > 0 },
  );
  const keys = useKeyedClientResource(
    keysKey,
    [keysKey],
    async (signal) => {
      void signal;
      return loadApiKeyBoard(apiBase, keyProviders);
    },
    { enabled: keyProviders.length > 0 },
  );

  const accountSets = useMemo(() => {
    const base = cheap.data ?? {};
    const extra = enriched.data ?? {};
    const next: Record<string, ProviderAccountSnapshot> = { ...base };
    for (const [name, rich] of Object.entries(extra)) {
      next[name] = applyAccountQuotaEnrichment(base[name] ?? { activeAccountId: null, accounts: [] }, rich);
    }
    return next;
  }, [cheap.data, enriched.data]);

  const accountLoadStates = useMemo(() => {
    const states: Record<string, AccountLoadState> = {};
    for (const name of oauthProviders) {
      if (cheap.error) states[name] = "error";
      else if (cheap.data?.[name]) states[name] = "ready";
      else if (cheap.loading) states[name] = "loading";
      else states[name] = "idle";
    }
    return states;
  }, [cheap.data, cheap.error, cheap.loading, oauthProviders]);

  const [switching, setSwitching] = useState<{ provider: string; accountId: string } | null>(null);
  const inflight = useRef(false);

  const republishAccounts = useCallback(async () => {
    const nextCheap = await loadOAuthAccountBoard(apiBase, oauthProviders);
    setClientResourceData(cheapKey, nextCheap);
    const nextRich = await loadOAuthAccountBoard(apiBase, oauthProviders, { quota: true });
    setClientResourceData(quotaKey, nextRich);
    return true;
  }, [apiBase, cheapKey, oauthProviders, quotaKey]);

  const republishKeys = useCallback(async (names = keyProviders) => {
    const next = await loadApiKeyBoard(apiBase, names);
    setClientResourceData(keysKey, { ...keys.data, ...next });
  }, [apiBase, keyProviders, keys.data, keysKey]);

  const fetchAccountSets = useCallback(async (providers: string[]) => {
    void providers;
    try {
      await republishAccounts();
      return true;
    } catch {
      return false;
    }
  }, [republishAccounts]);

  const setAccountSets = useCallback((update: SetStateAction<Record<string, ProviderAccountSnapshot>>) => {
    const current = cheap.data ?? {};
    const next = typeof update === "function" ? update(current) : update;
    setClientResourceData(cheapKey, next);
  }, [cheap.data, cheapKey]);

  const copy = t as (key: string, vars?: Record<string, string | number>) => string;

  const applyResult = async (result: CredentialActionResult, afterAccounts = false) => {
    if (result.status === "ignored") return result.status;
    if (result.status === "failed") {
      notify(result.message, false);
      return result.status;
    }
    if (result.board === "accounts") {
      const refreshed = await fetchAccountSets([]);
      await Promise.resolve(refreshOauth());
      invalidateQuotas(true);
      if (afterAccounts && !refreshed) {
        notify(copy("pws.accountsLoadFailed"), false);
        return "failed";
      }
    } else if (result.board === "keys") {
      await republishKeys();
      invalidateQuotas(true);
    } else {
      const extra = keyProviders;
      await Promise.all([republishKeys(extra), Promise.resolve(refreshConfig())]);
      invalidateQuotas(true);
    }
    notify(result.message, true);
    return result.status;
  };

  const switchAccount = async (provider: string, account: OAuthAccountRow) => {
    if (inflight.current) return;
    inflight.current = true;
    setSwitching({ provider, accountId: account.id });
    try {
      const result = await activateStoredOAuthAccount({
        apiBase,
        provider,
        account,
        roster: accountSets[provider]?.accounts ?? [account],
        blocked: false,
        t: copy,
      });
      await applyResult(result, true);
    } finally {
      inflight.current = false;
      if (aliveRef.current) setSwitching(null);
    }
  };

  const switchApiKey = async (provider: string, entry: ApiKeyRow) => {
    await applyResult(await activateStoredApiKey({ apiBase, provider, entry, t: copy }));
  };

  const removeApiKey = async (provider: string, entry: ApiKeyRow) => {
    await applyResult(await dropStoredApiKey({
      apiBase, provider, entry, t: copy, confirm: message => window.confirm(message),
    }));
  };

  const addApiKeyValue = async (provider: string, rawKey: string): Promise<boolean> => {
    const names = keyProviders.includes(provider) ? keyProviders : [...keyProviders, provider];
    const result = await storeApiKeySecret({ apiBase, provider, secret: rawKey, t: copy });
    if (result.status !== "ok") {
      await applyResult(result);
      return false;
    }
    notify(result.message, true);
    await Promise.all([republishKeys(names), Promise.resolve(refreshConfig())]);
    invalidateQuotas(true);
    return true;
  };

  const editCredentialAlias = async (provider: string, type: "oauth" | "api-key", id: string, current?: string) => {
    await applyResult(await renameStoredCredential({
      apiBase,
      provider,
      kind: type,
      id,
      current,
      t: copy,
      prompt: (message, initial) => window.prompt(message, initial),
    }));
  };

  const removeAccount = async (provider: string, account: OAuthAccountRow) => {
    await applyResult(await dropStoredOAuthAccount({
      apiBase,
      provider,
      account,
      roster: accountSets[provider]?.accounts ?? [account],
      t: copy,
      confirm: message => window.confirm(message),
    }), true);
  };

  const activeAccountNeedsReauth = useMemo(
    () => buildActiveAccountNeedsReauthMap(accountSets, codexActiveNeedsReauth, accountNeedsReauth),
    [accountSets, codexActiveNeedsReauth],
  );

  return {
    accountSets,
    setAccountSets,
    accountLoadStates,
    switchingAccount: switching,
    keyPools: keys.data ?? {},
    fetchAccountSets,
    switchAccount,
    switchApiKey,
    removeApiKey,
    addApiKeyValue,
    editCredentialAlias,
    removeAccount,
    activeAccountNeedsReauth,
  };
}
