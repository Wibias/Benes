/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * What the OAuth Terms-of-Service warning decides before it renders.
 *
 * `oauth-tos-risk` owns *which providers are risky and how risky*. This module owns the two
 * provider-specific answers the dialog then needs: whether the level's generic body would
 * describe the wrong problem, and whether the provider's own documentation already steers
 * users to an API key. It returns decisions rather than translation keys, so the keys stay
 * visible where the dialog renders them.
 */

import { oauthTosRisk, type OAuthTosRiskLevel } from "./oauth-tos-risk.ts";

/** Providers whose own documentation already steers users to an API key instead. */
const API_KEY_SAFER_PATH = new Set(["anthropic", "google-antigravity"]);

/**
 * The one provider whose body copy cannot be shared with its level's default: Anthropic's
 * restriction is about using a subscription outside the official client, so the generic
 * "high risk" paragraph would describe the wrong problem.
 */
const PROVIDER_BODY_ID = "anthropic";

export interface OAuthTosDialogPlan {
  level: OAuthTosRiskLevel;
  /** True when the dialog uses this provider's own body paragraph. */
  usesProviderBody: boolean;
  /** True when the dialog should add the "an API key is the supported path" note. */
  offersApiKeyPath: boolean;
}

/**
 * Resolve the warning for a provider, or `null` when the provider carries no elevated
 * risk — the caller renders nothing in that case.
 */
export function oauthTosDialogPlan(providerId: string): OAuthTosDialogPlan | null {
  const level = oauthTosRisk(providerId);
  if (level === null) return null;
  const id = providerId.trim().toLowerCase();
  return {
    level,
    usesProviderBody: id === PROVIDER_BODY_ID,
    offersApiKeyPath: API_KEY_SAFER_PATH.has(id),
  };
}
