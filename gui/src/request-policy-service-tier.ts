/** Canonical Benes request-policy client model. */
export const SERVICE_TIER_OPTIONS = ["", "auto", "default", "flex", "priority"] as const;

export type RequestPolicyServiceTier = (typeof SERVICE_TIER_OPTIONS)[number];

export type SettingsRequestPolicyBody = {
  requestPolicy?: {
    serviceTier?: unknown;
  } | null;
};

export function readRequestPolicyServiceTier(body: unknown): RequestPolicyServiceTier {
  if (!body || typeof body !== "object" || Array.isArray(body)) return "";
  const policy = (body as SettingsRequestPolicyBody).requestPolicy;
  if (!policy || typeof policy !== "object" || Array.isArray(policy)) return "";
  const tier = policy.serviceTier;
  return typeof tier === "string" && SERVICE_TIER_OPTIONS.includes(tier as RequestPolicyServiceTier)
    ? tier as RequestPolicyServiceTier
    : "";
}

export function requestPolicyPutBody(tier: RequestPolicyServiceTier): { requestPolicy: { serviceTier?: string } } {
  return tier === ""
    ? { requestPolicy: {} }
    : { requestPolicy: { serviceTier: tier } };
}

export function isRequestPolicyServiceTier(value: string): value is RequestPolicyServiceTier {
  return SERVICE_TIER_OPTIONS.includes(value as RequestPolicyServiceTier);
}
