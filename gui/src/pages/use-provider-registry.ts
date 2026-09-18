import { useCallback, useEffect, useMemo, useRef, useState, type Dispatch, type SetStateAction } from "react";
import { useKeyedClientResource } from "../client-resource.ts";
import { readSessionListCache } from "../session-list-cache.ts";
import { providersConfigCacheKey } from "../nav-board-resources.ts";
import { usageSummary30dResourceKey } from "../usage-summary-resource.ts";
import type { OAuthStatus, ProvidersConfig } from "./providers-shared.ts";
import {
  cacheProvidersConfig,
  loadOauthProviderStatus,
  loadProvidersConfig,
  providersOauthResourceKey,
  reloadPublishedOauthStatus,
  reloadPublishedProvidersConfig,
} from "./provider-registry.ts";

export function useProviderRegistry(apiBase: string, loadFailedCopy: string) {
  const configCacheKey = providersConfigCacheKey(apiBase);
  const seeded = readSessionListCache<ProvidersConfig>(configCacheKey);
  const oauthKey = providersOauthResourceKey(apiBase);

  const configResource = useKeyedClientResource(
    configCacheKey,
    [apiBase],
    async (signal) => {
      const data = await loadProvidersConfig(apiBase, (input, init) => fetch(input, { ...init, signal }));
      if (!data) throw new Error(loadFailedCopy);
      cacheProvidersConfig(configCacheKey, data);
      return data;
    },
    { initialData: seeded ?? undefined },
  );
  const oauthResource = useKeyedClientResource(
    oauthKey,
    [apiBase],
    async (signal) => loadOauthProviderStatus(apiBase, (input, init) => fetch(input, { ...init, signal })),
  );

  useKeyedClientResource(
    `add-provider-presets:${apiBase}`,
    [apiBase],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/provider-presets`, { signal });
      if (!response.ok) throw new Error(String(response.status));
      const body = await response.json() as { providers?: unknown[] };
      return Array.isArray(body.providers) && body.providers.length > 0 ? body.providers : null;
    },
  );
  useKeyedClientResource(
    usageSummary30dResourceKey(apiBase),
    [apiBase],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/usage?range=30d`, { signal });
      if (!response.ok) throw new Error(String(response.status));
      return await response.json() as { providers?: Array<{ provider: string; requests: number }> };
    },
    { deadlineMs: 60_000 },
  );

  const primedFor = useRef<string | null>(null);
  useEffect(() => {
    if (primedFor.current === apiBase) return;
    primedFor.current = apiBase;
    queueMicrotask(() => {
      configResource.refresh();
      oauthResource.refresh();
    });
  }, [apiBase, configResource, oauthResource]);

  const [oauthDelta, setOauthDelta] = useState<Record<string, OAuthStatus>>({});
  const oauthStatus = useMemo(
    () => ({ ...oauthResource.data?.status, ...oauthDelta }),
    [oauthResource.data?.status, oauthDelta],
  );
  const setOauthStatus = useCallback<Dispatch<SetStateAction<Record<string, OAuthStatus>>>>((update) => {
    setOauthDelta(previous => {
      const current = { ...oauthResource.data?.status, ...previous };
      return typeof update === "function" ? update(current) : update;
    });
  }, [oauthResource.data?.status]);

  const reloadConfig = useCallback(async () => {
    await reloadPublishedProvidersConfig(apiBase, configCacheKey);
  }, [apiBase, configCacheKey]);
  const reloadOauth = useCallback(async () => {
    setOauthDelta({});
    await reloadPublishedOauthStatus(apiBase, oauthKey);
  }, [apiBase, oauthKey]);

  return {
    configCacheKey,
    seeded,
    configResource,
    oauthResource,
    config: configResource.data ?? seeded,
    oauthProviders: oauthResource.data?.providers ?? [],
    oauthStatus,
    setOauthStatus,
    reloadConfig,
    reloadOauth,
  };
}
