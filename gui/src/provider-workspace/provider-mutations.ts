import { readManagementError } from "../fetch-json.ts";
import type { TFn } from "../i18n/shared.ts";
import type { WorkspaceItem } from "./catalog.ts";

export type ProviderUpdatePatch = {
  setDefault?: true;
  adapter?: string;
  baseUrl?: string;
  defaultModel?: string;
  apiKey?: string;
  apiKeyTransport?: "x-api-key" | "bearer" | "";
  authMode?: string;
  note?: string;
  disabled?: boolean;
  allowPrivateNetwork?: boolean;
  liveModels?: boolean;
  requestPacing?: WorkspaceItem["requestPacing"] | null;
  codexAccountMode?: "direct" | "pool";
  defaultAccess?: "oauth" | "api";
};

/**
 * Applies one provider patch through the management API. Resolves with the
 * listener's own refusal text so the caller can surface it unchanged.
 */
export type ProviderPatchApplier = (
  name: string,
  patch: ProviderUpdatePatch,
) => Promise<{ ok: boolean; error?: string }>;

type ProviderErrorBody = { code?: unknown; combos?: unknown; error?: unknown };
type FetchImpl = typeof fetch;
type MutationOk = { ok: true };
type MutationFail = { ok: false; error: string };

function managementRowHref(origin: string, id: string): string {
  return `${origin}/api/providers?name=${encodeURIComponent(id)}`;
}

function joinedComboIds(raw: unknown): string {
  return Array.isArray(raw) ? raw.filter((id): id is string => typeof id === "string").join(", ") : "";
}

export function decodeProviderMutationCopy(body: ProviderErrorBody, t: TFn, fallback: string): string {
  if (body.code === "last_provider") return t("prov.removeLastProvider");
  if (body.code === "provider_has_dependent_combos") {
    return t("prov.removeHasDependentCombos", { combos: joinedComboIds(body.combos) || "—" });
  }
  if (body.code === "default_provider_disabled") return t("prov.defaultDisabled");
  return typeof body.error === "string" && body.error.trim() ? body.error.trim() : fallback;
}

async function readErrorBody(response: Response): Promise<ProviderErrorBody> {
  return await response.json().catch(() => ({})) as ProviderErrorBody;
}

export async function deleteNamedProvider(input: {
  apiBase: string;
  name: string;
  t: TFn;
  fetchImpl?: FetchImpl;
}): Promise<{ ok: true; defaultProvider: string | null } | MutationFail> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const fallback = input.t("prov.removeFail", { name: input.name });
  try {
    const response = await fetchImpl(managementRowHref(input.apiBase, input.name), { method: "DELETE" });
    if (!response.ok) {
      return { ok: false, error: decodeProviderMutationCopy(await readErrorBody(response), input.t, fallback) };
    }
    const body = await response.json().catch(() => ({})) as { defaultProvider?: unknown };
    return {
      ok: true,
      defaultProvider: typeof body.defaultProvider === "string" ? body.defaultProvider : null,
    };
  } catch {
    return { ok: false, error: fallback };
  }
}

export async function setNamedProviderDisabled(input: {
  apiBase: string;
  name: string;
  disabled: boolean;
  t: TFn;
  fetchImpl?: FetchImpl;
}): Promise<MutationOk | MutationFail> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const fallback = input.disabled
    ? input.t("prov.disableFail", { name: input.name })
    : input.t("prov.enableFail", { name: input.name });
  try {
    const response = await fetchImpl(managementRowHref(input.apiBase, input.name), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ disabled: input.disabled }),
    });
    if (!response.ok) return { ok: false, error: await readManagementError(response, fallback) };
    return { ok: true };
  } catch {
    return { ok: false, error: fallback };
  }
}

export async function setNamedProviderDefault(input: {
  apiBase: string;
  name: string;
  t: TFn;
  fetchImpl?: FetchImpl;
}): Promise<MutationOk | MutationFail> {
  const fetchImpl = input.fetchImpl ?? fetch;
  const fallback = input.t("prov.setDefaultFail", { name: input.name });
  try {
    const response = await fetchImpl(managementRowHref(input.apiBase, input.name), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ setDefault: true }),
    });
    if (!response.ok) {
      return { ok: false, error: decodeProviderMutationCopy(await readErrorBody(response), input.t, fallback) };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: fallback };
  }
}

export async function applyNamedProviderPatch(input: {
  apiBase: string;
  name: string;
  patch: ProviderUpdatePatch;
  t: TFn;
  fetchImpl?: FetchImpl;
}): Promise<MutationOk | MutationFail> {
  const fetchImpl = input.fetchImpl ?? fetch;
  try {
    const response = await fetchImpl(managementRowHref(input.apiBase, input.name), {
      method: "PATCH",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input.patch),
    });
    if (!response.ok) {
      return { ok: false, error: await readManagementError(response, input.t("prov.updateFail")) };
    }
    return { ok: true };
  } catch {
    return { ok: false, error: input.t("prov.networkError") };
  }
}
