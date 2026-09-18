/**
 * Provider Access credential HTTP and wire DTOs for /api/oauth/accounts and /api/providers/keys.
 * Codex accounts remain on the #236 controller.
 */
import { readJsonOrThrow, readManagementError } from "../fetch-json.ts";
import type { AccountQuota } from "../codex-quota-utils.ts";

export type ApiKeyRow = {
  id: string;
  label?: string;
  masked: string;
  active: boolean;
};

export type OAuthAccountHealthStatus = "healthy" | "cooldown" | "reauth_required" | "warning";

export type OAuthAccountRow = {
  id: string;
  alias?: string;
  email?: string;
  active: boolean;
  needsReauth?: boolean;
  health?: { status: OAuthAccountHealthStatus; reason?: string; until?: string };
  healthLabel?: string;
  healthSummary?: string;
  healthAction?: string;
  quota?: AccountQuota | null;
  quotaUnavailable?: boolean;
};

export type ProviderAccountSnapshot = {
  activeAccountId: string | null;
  accounts: OAuthAccountRow[];
};

export function oauthCredentialProviders(
  providers: Record<string, { authMode?: string }> | undefined,
): string[] {
  if (!providers) return [];
  return Object.entries(providers)
    .filter(([, row]) => row.authMode === "oauth")
    .map(([name]) => name);
}

export function apiKeyCredentialProviders(
  providers: Record<string, { authMode?: string; hasApiKey?: boolean }> | undefined,
): string[] {
  if (!providers) return [];
  return Object.entries(providers)
    .filter(([, row]) => row.hasApiKey && row.authMode !== "oauth" && row.authMode !== "forward")
    .map(([name]) => name);
}

type FetchImpl = typeof fetch;

function asSnapshot(data: { activeAccountId?: string | null; accounts?: OAuthAccountRow[] } | undefined): ProviderAccountSnapshot {
  return { activeAccountId: data?.activeAccountId ?? null, accounts: data?.accounts ?? [] };
}

export function applyAccountQuotaEnrichment(
  cheap: ProviderAccountSnapshot,
  rich: { activeAccountId?: string | null; accounts?: OAuthAccountRow[] },
): ProviderAccountSnapshot {
  return {
    activeAccountId: rich.activeAccountId ?? cheap.activeAccountId,
    accounts: Array.isArray(rich.accounts) ? rich.accounts : cheap.accounts,
  };
}

export async function readOAuthAccountSnapshot(
  apiBase: string,
  provider: string,
  opts: { quota?: boolean; signal?: AbortSignal; fetchImpl?: FetchImpl } = {},
): Promise<ProviderAccountSnapshot> {
  const fetchImpl = opts.fetchImpl ?? fetch;
  const quota = opts.quota ? "&quota=1" : "";
  const response = await fetchImpl(
    `${apiBase}/api/oauth/accounts?provider=${encodeURIComponent(provider)}${quota}`,
    { signal: opts.signal },
  );
  const data = await readJsonOrThrow<{ activeAccountId?: string | null; accounts?: OAuthAccountRow[] }>(response);
  return asSnapshot(data);
}

export async function putActiveOAuthAccount(
  apiBase: string,
  provider: string,
  accountId: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}/api/oauth/accounts/active`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, accountId }),
  });
}

export async function deleteOAuthAccount(
  apiBase: string,
  provider: string,
  accountId: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(
    `${apiBase}/api/oauth/accounts?provider=${encodeURIComponent(provider)}&id=${encodeURIComponent(accountId)}`,
    { method: "DELETE" },
  );
}

export async function putOAuthAccountAlias(
  apiBase: string,
  provider: string,
  accountId: string,
  alias: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}/api/oauth/accounts/alias`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider, accountId, alias }),
  });
}

export async function readApiKeyRows(
  apiBase: string,
  name: string,
  fetchImpl: FetchImpl = fetch,
): Promise<ApiKeyRow[]> {
  try {
    const response = await fetchImpl(`${apiBase}/api/providers/keys?name=${encodeURIComponent(name)}`);
    const data = await readJsonOrThrow<{ keys?: ApiKeyRow[] }>(response);
    return data?.keys ?? [];
  } catch {
    return [];
  }
}

export async function putActiveApiKey(
  apiBase: string,
  name: string,
  id: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}/api/providers/keys/active`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, id }),
  });
}

export async function postApiKey(
  apiBase: string,
  name: string,
  key: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}/api/providers/keys`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, key }),
  });
}

export async function deleteApiKey(
  apiBase: string,
  name: string,
  id: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(
    `${apiBase}/api/providers/keys?name=${encodeURIComponent(name)}&id=${encodeURIComponent(id)}`,
    { method: "DELETE" },
  );
}

export async function putApiKeyAlias(
  apiBase: string,
  name: string,
  id: string,
  alias: string,
  fetchImpl: FetchImpl = fetch,
): Promise<Response> {
  return fetchImpl(`${apiBase}/api/providers/keys/alias`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ name, id, alias }),
  });
}

export async function managementFailureCopy(response: Response, fallback: string): Promise<string> {
  return readManagementError(response, fallback);
}

export async function loadOAuthAccountBoard(
  apiBase: string,
  providers: readonly string[],
  opts: { quota?: boolean; signal?: AbortSignal; fetchImpl?: FetchImpl } = {},
): Promise<Record<string, ProviderAccountSnapshot>> {
  const unique = [...new Set(providers)];
  const entries = await Promise.all(unique.map(async name => (
    [name, await readOAuthAccountSnapshot(apiBase, name, opts)] as const
  )));
  return Object.fromEntries(entries);
}

export async function loadApiKeyBoard(
  apiBase: string,
  providers: readonly string[],
  fetchImpl?: FetchImpl,
): Promise<Record<string, ApiKeyRow[]>> {
  const unique = [...new Set(providers)];
  const entries = await Promise.all(unique.map(async name => (
    [name, await readApiKeyRows(apiBase, name, fetchImpl ?? fetch)] as const
  )));
  return Object.fromEntries(entries);
}
