/**
 * Wire views for the Providers surfaces.
 *
 * The provider record is the one the workspace already types: `WorkspaceProvider`
 * in `provider-workspace/catalog.ts` owns the provider config view, so this module
 * carries the surrounding `GET /api/config` envelope and the OAuth/quota payloads
 * the Providers surfaces read back rather than a second copy of the field list.
 */
import type { AccountQuota } from "../codex-quota-utils";
import type { WorkspaceProvider } from "../provider-workspace/catalog";

/** Envelope every `GET /api/config` response carries beside the provider map. */
interface ConfigEnvelope {
  port: number;
  defaultProvider: string;
}

export interface ProvidersConfig extends ConfigEnvelope {
  providers: Record<string, WorkspaceProvider>;
}

/** Login outcome the OAuth status endpoint reports for one provider. */
export interface OAuthLoginOutcome {
  loggedIn: boolean;
  needsReauth?: boolean;
  pending?: boolean;
  done?: boolean;
}

/** Local failure text for a login that ended without a usable credential. */
export interface OAuthLoginFailure {
  error?: string;
}

/** Account identity the status endpoint echoes back for the signed-in login. */
export interface OAuthActiveIdentity {
  email?: string;
  activeAccountId?: string | null;
}

/** `GET /api/oauth/status` for one provider. */
export type OAuthStatus = OAuthLoginOutcome & OAuthLoginFailure & OAuthActiveIdentity;

/** One row of `GET /api/provider-quotas` as the workspace consumes it. */
export interface ProviderQuotaReport {
  provider: string;
  /** Quota windows exactly as the listener shaped them; narrowed at the view. */
  quota: AccountQuota;
  source: string;
  updatedAt: number;
}

/** Stored identity of one account in a provider's credential pool. */
export interface OAuthAccountIdentityRow {
  id: string;
  alias?: string;
  email?: string;
}

/** Per-account health the credential pool reports alongside the identity. */
export interface OAuthAccountHealth {
  active: boolean;
  needsReauth?: boolean;
  expiresAt?: number;
}

/** One account row returned for a provider by the OAuth account store. */
export type OAuthAccount = OAuthAccountIdentityRow & OAuthAccountHealth;

/**
 * Login-provider display labels. A Map keeps the lookup off the prototype, so an
 * id such as `constructor` cannot inherit a label from Object.
 */
const OAUTH_PROVIDER_LABELS = new Map<string, string>([
  ["xai", "xAI (Grok)"],
  ["anthropic", "Anthropic (Claude)"],
  ["kimi", "Kimi (Moonshot)"],
  ["google-antigravity", "Google Antigravity"],
  ["github-copilot", "GitHub Copilot"],
  ["cursor", "Cursor"],
]);

/** Display label for an OAuth provider id; unknown ids keep their own name. */
export function oauthLabel(id: string): string {
  return OAUTH_PROVIDER_LABELS.get(id) ?? id;
}
