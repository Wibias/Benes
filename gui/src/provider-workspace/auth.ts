/** Benes dashboard client for the Go proxy (`internal/server`). */
import type { TFn } from "../i18n/shared";
import { maskEmailAddress } from "../lib/privacy";
import { accessDescriptorPolicy } from "./access-policy";
import { isAccountProvider, isLocalProvider, type WorkspaceItem } from "./catalog";

/** The credential lanes a panel can render. Codex accounts are their own lane owner. */
export type ProviderAuthSurface = "oauth-accounts" | "api-keys" | null;

export type AccessMethodKind = "oauth" | "api-key";

export interface ProviderAccessMethod {
  /** Stable access-lane id. Credential kind is carried separately in `kind`. */
  id: string;
  kind: AccessMethodKind;
  connectionId: string;
  supportsMultiple?: boolean;
  quotaAvailable?: boolean;
  selectionOrder?: boolean;
  poolSupported?: boolean;
  connectionPresent?: boolean;
}

export interface SelectionCapabilities {
  supported: boolean;
  mode?: string;
  strategy?: string;
  strategies?: string[];
  autoSwitchThreshold?: number;
  stickyLimit?: number;
  showAutoSwitch?: boolean;
  showStickyLimit?: boolean;
  methodId?: string;
}

export interface ProviderAccessDescriptor {
  methods: ProviderAccessMethod[];
  defaultMethodId?: string;
  defaultAccess?: boolean;
  selection?: SelectionCapabilities | null;
  activitySupported?: boolean;
}

export const OPENAI_API_CONNECTION = "openai-apikey";

/** OAuth login state the Access surface renders for one provider. */
export interface ProviderOAuthLoginView {
  loggedIn: boolean;
  email?: string;
  error?: string;
  needsReauth?: boolean;
}

export interface OAuthAccountIdentity {
  id: string;
  alias?: string;
  email?: string;
}

/**
 * Capability contract for the Access tab. Prefer the workspace descriptor but
 * overlay config/key state that can refresh earlier than the workspace aggregate.
 */
export function deriveAccessDescriptor(item: WorkspaceItem, apiLanePresent = false): ProviderAccessDescriptor {
  return accessDescriptorPolicy(item, {
    accountProvider: isAccountProvider(item.name, item),
    localProvider: isLocalProvider(item),
    apiLanePresent,
  });
}

export function accessTabVisible(descriptor: ProviderAccessDescriptor): boolean {
  return descriptor.methods.length > 0;
}

/** The lane of a given kind the descriptor lets a caller select over. */
function pooledLane(
  descriptor: ProviderAccessDescriptor,
  kind: AccessMethodKind,
): ProviderAccessMethod | undefined {
  return descriptor.methods.find(method => method.kind === kind && method.poolSupported);
}

/**
 * Credential selection only earns a control when there is more than one row to
 * choose between in the lane that is currently active.
 */
export function showCredentialSelection(
  descriptor: ProviderAccessDescriptor,
  oauthCount: number,
  keyCount: number,
): boolean {
  const selection = descriptor.selection;
  if (!selection?.supported) return false;
  const oauth = pooledLane(descriptor, "oauth");
  if (oauth && oauthCount > 1) return true;
  const keys = pooledLane(descriptor, "api-key");
  if (!keys || selection.methodId !== keys.id) return false;
  return keyCount > 1;
}

/**
 * Copy key for the methods sentence in the credential-selection table; only the
 * kinds the descriptor actually offers appear, and the joining kind wins.
 */
export function accessMethodsCopyKey(
  descriptor: ProviderAccessDescriptor,
): "prov.access.methodsOauthKeys" | "prov.access.methodsOauth" | "prov.access.methodsApiKeys" | null {
  const kinds = new Set(descriptor.methods.map(method => method.kind));
  if (kinds.has("oauth")) {
    return kinds.has("api-key") ? "prov.access.methodsOauthKeys" : "prov.access.methodsOauth";
  }
  return kinds.has("api-key") ? "prov.access.methodsApiKeys" : null;
}

/** Positional fallback when an account carries neither alias nor email. */
function accountOrdinal<T extends OAuthAccountIdentity>(
  accounts: readonly T[],
  account: OAuthAccountIdentity,
  t: TFn,
): string {
  const index = accounts.findIndex(candidate => candidate.id === account.id);
  return t("pws.accountOrdinal", { count: String(index >= 0 ? index + 1 : 1) });
}

/** Human-safe label for OAuth account rows; opaque storage ids stay private. */
export function oauthAccountDisplayLabel<T extends OAuthAccountIdentity>(
  accounts: readonly T[],
  account: OAuthAccountIdentity,
  t: TFn,
): string {
  return account.alias?.trim() || account.email?.trim() || accountOrdinal(accounts, account, t);
}

/** Access table label: censored login email when the API has one. */
export function oauthAccessAccountLabel<T extends OAuthAccountIdentity>(
  accounts: readonly T[],
  account: OAuthAccountIdentity,
  t: TFn,
): string {
  return maskEmailAddress(account.email) ?? oauthAccountDisplayLabel(accounts, account, t);
}
