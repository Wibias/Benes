/**
 * Providers where signing a subscription login into a third-party proxy (Benes)
 * carries elevated Terms-of-Service or account-action risk.
 *
 * `high`: the provider's own docs or ToS restrict subscription OAuth to its official app.
 * `elevated`: an unofficial or reverse-engineered bridge, where abuse detection can
 * suspend access.
 */

export type OAuthTosRiskLevel = "high" | "elevated";

type OAuthTosTitleKey = "oauthTos.highTitle" | "oauthTos.elevatedTitle";
type OAuthTosBodyKey = "oauthTos.highBody" | "oauthTos.elevatedBody";

interface OAuthTosRiskEntry {
  readonly level: OAuthTosRiskLevel;
  readonly titleKey: OAuthTosTitleKey;
  readonly bodyKey: OAuthTosBodyKey;
}

function entry(level: OAuthTosRiskLevel): OAuthTosRiskEntry {
  return level === "high"
    ? { level, titleKey: "oauthTos.highTitle", bodyKey: "oauthTos.highBody" }
    : { level, titleKey: "oauthTos.elevatedTitle", bodyKey: "oauthTos.elevatedBody" };
}

const HIGH_RISK = entry("high");
const ELEVATED_RISK = entry("elevated");

/** Provider id → risk entry. Ids are matched lowercase after trimming. */
const RISK_BY_PROVIDER_ID: Readonly<Record<string, OAuthTosRiskEntry>> = {
  anthropic: HIGH_RISK,
  "google-antigravity": HIGH_RISK,
  "github-copilot": ELEVATED_RISK,
  cursor: ELEVATED_RISK,
};

function riskEntry(providerId: string): OAuthTosRiskEntry | null {
  const id = providerId.trim().toLowerCase();
  if (!Object.hasOwn(RISK_BY_PROVIDER_ID, id)) return null;
  return RISK_BY_PROVIDER_ID[id] ?? null;
}

export function oauthTosRisk(providerId: string): OAuthTosRiskLevel | null {
  return riskEntry(providerId)?.level ?? null;
}

export function oauthTosRiskTitleKey(level: OAuthTosRiskLevel): OAuthTosTitleKey {
  return level === "high" ? "oauthTos.highTitle" : "oauthTos.elevatedTitle";
}

export function oauthTosRiskBodyKey(level: OAuthTosRiskLevel): OAuthTosBodyKey {
  return level === "high" ? "oauthTos.highBody" : "oauthTos.elevatedBody";
}
