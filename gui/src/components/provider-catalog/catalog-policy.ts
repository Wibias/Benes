/**
 * Catalog picker policy: row classification, the Access/Connection filter
 * tables and predicates, account-status copy, empty-state kind, and rail
 * subtitle. The React tree in ProviderCatalog.tsx only renders these.
 */
import type { TFn, TKey } from "../../i18n/shared";
import type { CatalogFamily } from "./catalog-families";
import { isLocalProvider, providerTier, type ProviderTier, type WorkspaceProvider } from "../../provider-workspace/catalog.ts";
import type { CatalogPreset } from "./provider-presets.ts";

export type AccountLoginStatus = { loggedIn: boolean; email?: string; error?: string; needsReauth?: boolean };
export type AccountLoginRow = {
  id: string;
  label: string;
  kind: "oauth" | "key" | "codex";
  statusLabel?: string;
  href?: string;
};

export type CatalogTier = "accounts" | "free" | "paid";
export type CatalogEmptyKind = "loading" | "no-match" | null;

export type CatalogFilterOption<Value extends string> = {
  value: Value;
  labelKey: TKey;
};

export type CatalogAccessFilter = "all" | "paid" | "free" | "local" | "accounts";
export type CatalogConnectionFilter = "all" | "oauth" | "key" | "local";

export const CATALOG_ACCESS_FILTER_KEYS: CatalogFilterOption<CatalogAccessFilter>[] = [
  { value: "all", labelKey: "modal.filter.all" },
  { value: "paid", labelKey: "modal.access.paid" },
  { value: "free", labelKey: "modal.tab.free" },
  { value: "local", labelKey: "modal.badge.local" },
  { value: "accounts", labelKey: "modal.tab.accounts" },
];

export const CATALOG_CONNECTION_FILTER_KEYS: CatalogFilterOption<CatalogConnectionFilter>[] = [
  { value: "all", labelKey: "modal.filter.all" },
  { value: "oauth", labelKey: "modal.badge.oauth" },
  { value: "key", labelKey: "modal.badge.apiKey" },
  { value: "local", labelKey: "modal.badge.local" },
];

/**
 * Bridge a preset row to the WorkspaceProvider shape the tier predicates read.
 * The preset row names its connection column `auth`; the config shape calls it
 * `authMode`, and the optional booleans arrive as undefined rather than false.
 */
function presetTierInput(preset: CatalogPreset): WorkspaceProvider {
  return {
    adapter: preset.adapter,
    baseUrl: preset.baseUrl,
    authMode: preset.auth,
    freeTier: !!preset.freeTier,
    keyOptional: !!preset.keyOptional,
  };
}

/** Three-way tier for a catalog preset row (accounts wins over free; else paid). */
export function presetTier(preset: CatalogPreset): ProviderTier {
  return providerTier(preset.id, presetTierInput(preset));
}

export function presetIsLocal(preset: CatalogPreset): boolean {
  return isLocalProvider(presetTierInput(preset));
}

export function matchesAccessFilter(preset: CatalogPreset, filter: CatalogAccessFilter): boolean {
  if (filter === "all" || filter === "accounts") return true;
  const tier = presetTier(preset);
  if (filter === "local") return presetIsLocal(preset);
  if (filter === "free") return tier === "free";
  return tier === "paid" && !presetIsLocal(preset);
}

export function matchesConnectionFilter(preset: CatalogPreset, filter: CatalogConnectionFilter): boolean {
  if (filter === "all") return true;
  if (filter === "oauth") return preset.auth === "oauth";
  if (filter === "key") return preset.auth === "key" || preset.auth === "oauth";
  return presetIsLocal(preset) || preset.auth === "local";
}

export function accessFromTier(tier?: CatalogTier): CatalogAccessFilter {
  if (tier === "accounts") return "accounts";
  if (tier === "free") return "free";
  if (tier === "paid") return "paid";
  return "all";
}

export function isCatalogAccessFilter(value: string): value is CatalogAccessFilter {
  return CATALOG_ACCESS_FILTER_KEYS.some((row) => row.value === value);
}

export function isCatalogConnectionFilter(value: string): value is CatalogConnectionFilter {
  return CATALOG_CONNECTION_FILTER_KEYS.some((row) => row.value === value);
}

export function filterAccountRows(rows: AccountLoginRow[], query: string): AccountLoginRow[] {
  const q = query.trim().toLowerCase();
  if (!q) return rows;
  return rows.filter((row) => row.label.toLowerCase().includes(q) || row.id.toLowerCase().includes(q));
}

export function catalogAccountStatusText(
  row: AccountLoginRow,
  status: AccountLoginStatus | undefined,
  t: TFn,
): string {
  if (status?.loggedIn) return status.email ?? row.statusLabel ?? t("modal.accountLoggedIn");
  return status?.error ?? row.statusLabel ?? t("modal.accountLoggedOut");
}

export function catalogEmptyKind(input: {
  presetsLoading: boolean;
  showAccounts: boolean;
  accountCount: number;
  rowCount: number;
}): CatalogEmptyKind {
  if (input.presetsLoading && input.rowCount === 0 && !input.showAccounts) return "loading";
  if (input.showAccounts && input.accountCount === 0 && !input.presetsLoading) return "no-match";
  if (!input.showAccounts && !input.presetsLoading && input.rowCount === 0) return "no-match";
  return null;
}

function catalogAuthMethodKeys(
  auth: CatalogPreset["auth"],
  local: boolean,
  selfHosted: boolean,
): TKey[] {
  if (auth === "oauth") return ["modal.badge.oauth", "modal.badge.apiKey"];
  if (auth === "forward") return ["modal.badge.codexLogin"];
  if (selfHosted) return ["pws.type.selfHosted"];
  if (auth === "local" || local) return ["modal.badge.local"];
  return ["modal.badge.apiKey"];
}

export function catalogConnectionIsSelfHosted(input: { id?: string; adapter?: string }): boolean {
  const hay = `${input.id ?? ""} ${input.adapter ?? ""}`.toLowerCase();
  return hay.includes("litellm");
}

export function catalogAccessLabel(
  preset: { local: boolean; tier: "accounts" | "free" | "paid" },
  t: TFn,
): string {
  if (preset.local) return t("modal.badge.local");
  if (preset.tier === "free") return t("modal.badge.free");
  return t("modal.access.paid");
}

export function catalogConnectionLabel(
  preset: { auth: CatalogPreset["auth"]; local: boolean; id?: string; adapter?: string },
  t: TFn,
): string {
  return catalogAuthMethodKeys(
    preset.auth,
    preset.local,
    catalogConnectionIsSelfHosted(preset),
  ).map((key) => t(key)).join(" + ");
}

export function railSubtitle(
  preset: { auth: CatalogPreset["auth"]; local: boolean; tier: "accounts" | "free" | "paid"; id?: string; adapter?: string },
  t: TFn,
): string {
  const access = preset.id === "custom" ? t("modal.access.manual") : catalogAccessLabel(preset, t);
  return `${access} · ${catalogConnectionLabel(preset, t)}`;
}

function uniqueJoin(values: string[]): string {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const value of values) {
    if (seen.has(value)) continue;
    seen.add(value);
    out.push(value);
  }
  return out.join(" + ");
}

function memberAuthKeys(
  auth: CatalogPreset["auth"],
  local: boolean,
  selfHosted: boolean,
): TKey[] {
  if (auth === "oauth") return ["modal.badge.oauth"];
  return catalogAuthMethodKeys(auth, local, selfHosted);
}

export function familyAccessLabel(family: CatalogFamily<CatalogPreset>, t: TFn): string {
  return uniqueJoin(family.members.map(row => catalogAccessLabel({
    local: presetIsLocal(row),
    tier: presetTier(row),
  }, t)));
}

export function familyConnectionLabel(family: CatalogFamily<CatalogPreset>, t: TFn): string {
  if (family.members.length === 1) {
    const row = family.members[0]!;
    return catalogConnectionLabel({
      auth: row.auth,
      local: presetIsLocal(row),
      id: row.id,
      adapter: row.adapter,
    }, t);
  }
  return uniqueJoin(family.members.map(row => memberAuthKeys(
    row.auth,
    presetIsLocal(row),
    catalogConnectionIsSelfHosted(row),
  ).map(key => t(key)).join(" + ")));
}

export function familyMethodTitle(member: CatalogPreset, family: CatalogFamily<CatalogPreset>, t: TFn): string {
  const conn = memberAuthKeys(
    member.auth,
    presetIsLocal(member),
    catalogConnectionIsSelfHosted(member),
  ).map(key => t(key)).join(" + ");
  if (family.members.length === 1) {
    return catalogConnectionLabel({
      auth: member.auth,
      local: presetIsLocal(member),
      id: member.id,
      adapter: member.adapter,
    }, t);
  }
  const shared = family.members.filter(row => memberAuthKeys(
    row.auth,
    presetIsLocal(row),
    catalogConnectionIsSelfHosted(row),
  ).join() === memberAuthKeys(
    member.auth,
    presetIsLocal(member),
    catalogConnectionIsSelfHosted(member),
  ).join()).length > 1;
  return shared ? member.label : conn;
}
