/** POST /api/model-discovery — fetch the live model list for one provider and persist it. */

export type ModelDiscoverySyncResult = {
  ok: boolean;
  applicable: boolean;
  error?: string;
  models?: string[];
};

type SyncPayload = {
  ok?: boolean;
  error?: string;
  applicable?: boolean;
  models?: unknown;
};

export async function postModelDiscoverySync(
  apiBase: string,
  provider: string,
  fetchImpl: typeof fetch = fetch,
): Promise<ModelDiscoverySyncResult> {
  const res = await fetchImpl(`${apiBase}/api/model-discovery`, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify({ provider }),
  });
  const payload = await res.json().catch(() => ({})) as SyncPayload;
  const error = typeof payload.error === "string" && payload.error.trim() !== "" ? payload.error : undefined;
  const models = Array.isArray(payload.models)
    ? payload.models.filter((id): id is string => typeof id === "string" && id.trim() !== "")
    : undefined;
  if (!res.ok || payload.ok === false) {
    return { ok: false, applicable: payload.applicable !== false, error };
  }
  return { ok: true, applicable: true, ...(models && models.length > 0 ? { models } : {}) };
}

/** Empty apiBase is same-origin (Vite HMR and packaged GUI). Only skip when the prop is missing. */
export function canSyncModelDiscovery(apiBase: string | undefined, syncing = false): apiBase is string {
  return typeof apiBase === "string" && !syncing;
}
