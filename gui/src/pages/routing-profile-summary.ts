import type { RoutingProfileDto, UnknownEvidenceMode } from "../routing-profile/profile-model";
import type { TFn } from "../i18n/shared";

export function uniqueCandidateProviders(profile: RoutingProfileDto): string[] {
  const seen = new Set<string>();
  const names: string[] = [];
  for (const candidate of profile.candidates) {
    const name = candidate.provider.trim();
    if (!name || seen.has(name)) continue;
    seen.add(name);
    names.push(name);
  }
  return names;
}

export function candidateCountLabel(t: TFn, count: number): string {
  if (count === 1) return t("routing.candidateCountOne");
  return t("routing.candidateCount", { n: String(count) });
}

export function profileTitle(profile: RoutingProfileDto): string {
  const alias = profile.alias?.trim();
  return alias || profile.id;
}

export function profileSubtitle(profile: RoutingProfileDto): string {
  const model = profile.model?.trim();
  if (model) return model;
  return `policy/${profile.id}`;
}

export function profileMatchesQuery(profile: RoutingProfileDto, query: string): boolean {
  const needle = query.trim().toLowerCase();
  if (!needle) return true;
  const haystack = [
    profile.id,
    profile.alias ?? "",
    profile.model,
    ...uniqueCandidateProviders(profile),
    ...profile.candidates.map(candidate => candidate.model),
  ]
    .join(" ")
    .toLowerCase();
  return haystack.includes(needle);
}

/** Optimize weights are 0–1 fractions in the DTO. */
export function weightPercentLabel(value: number): string {
  if (!Number.isFinite(value)) return "—";
  return `${Math.round(value * 100)} %`;
}

export function contextWindowLabel(tokens: number | undefined, fallback: string): string {
  if (tokens === undefined || !Number.isFinite(tokens) || tokens <= 0) return fallback;
  if (tokens >= 1_000_000) {
    const millions = tokens / 1_000_000;
    const label = Number.isInteger(millions) ? String(millions) : millions.toFixed(2).replace(/0+$/, "").replace(/\.$/, "");
    return `${label}M`;
  }
  if (tokens >= 1000) {
    const thousands = tokens / 1000;
    const label = Number.isInteger(thousands) ? String(thousands) : thousands.toFixed(1).replace(/\.0$/, "");
    return `${label}k`;
  }
  return String(tokens);
}

export function usdCostLabel(value: number | undefined, fallback: string): string {
  if (value === undefined || !Number.isFinite(value)) return fallback;
  return `$${value.toFixed(2)}`;
}

export function evidenceAgeLabel(
  maxEvidenceAgeMs: number | undefined,
  fallback: string,
  daysLabel: (days: number) => string,
): string {
  if (maxEvidenceAgeMs === undefined || !Number.isFinite(maxEvidenceAgeMs) || maxEvidenceAgeMs <= 0) {
    return fallback;
  }
  const days = Math.max(1, Math.round(maxEvidenceAgeMs / 86_400_000));
  return daysLabel(days);
}

export function optionalRequirementLabel(
  value: boolean | undefined,
  labels: { optional: string; required: string; off: string },
): string {
  if (value === true) return labels.required;
  if (value === false) return labels.off;
  return labels.optional;
}

export function evidenceModeLabel(
  mode: UnknownEvidenceMode | undefined,
  labels: { allow: string; warn: string; skip: string },
  fallback: string,
): string {
  if (mode === "allow") return labels.allow;
  if (mode === "penalize") return labels.warn;
  if (mode === "exclude") return labels.skip;
  return fallback;
}

export function requiredSuiteLabel(profile: RoutingProfileDto, fallback: string): string {
  const suites = profile.compatibility?.requiredSuites ?? [];
  if (suites.length === 0) return fallback;
  return suites.map(suite => suite.suiteId).join(", ");
}

export function minStatusLabel(
  status: "PROBED" | "VERIFIED" | undefined,
  labels: { probed: string; verified: string },
  fallback: string,
): string {
  if (status === "VERIFIED") return labels.verified;
  if (status === "PROBED") return labels.probed;
  return fallback;
}

export type OverviewKv = { key: string; label: string; value: string };

/** Requirements rows — only materially configured (non-default) values. */
export function overviewRequirementRows(
  profile: RoutingProfileDto,
  labels: {
    tools: string;
    image: string;
    structured: string;
    minContext: string;
    reasoningEffort: string;
    serviceTier: string;
    minQuotaHeadroom: string;
    localOnly: string;
    remoteAllowed: string;
    encryptedCodexTasks: string;
    optional: string;
    required: string;
    off: string;
    unavailable: string;
  },
): OverviewKv[] {
  const optional = {
    optional: labels.optional,
    required: labels.required,
    off: labels.off,
  };
  const rows: OverviewKv[] = [];
  const pushBool = (key: string, label: string, value: boolean | undefined) => {
    if (value === undefined) return;
    rows.push({ key, label, value: optionalRequirementLabel(value, optional) });
  };
  pushBool("tools", labels.tools, profile.require.tools);
  pushBool("imageInput", labels.image, profile.require.imageInput);
  pushBool("structuredOutput", labels.structured, profile.require.structuredOutput);
  if (profile.require.minContextWindow !== undefined) {
    rows.push({
      key: "minContextWindow",
      label: labels.minContext,
      value: contextWindowLabel(profile.require.minContextWindow, labels.unavailable),
    });
  }
  const reasoning = profile.require.reasoningEffort?.trim();
  if (reasoning) {
    rows.push({
      key: "reasoningEffort",
      label: labels.reasoningEffort,
      value: reasoning.charAt(0).toUpperCase() + reasoning.slice(1),
    });
  }
  const serviceTier = profile.require.serviceTier?.trim();
  if (serviceTier) {
    rows.push({
      key: "serviceTier",
      label: labels.serviceTier,
      value: serviceTier.charAt(0).toUpperCase() + serviceTier.slice(1),
    });
  }
  if (profile.require.minQuotaHeadroom !== undefined && Number.isFinite(profile.require.minQuotaHeadroom)) {
    rows.push({
      key: "minQuotaHeadroom",
      label: labels.minQuotaHeadroom,
      value: `${Math.round(profile.require.minQuotaHeadroom * 100)}%`,
    });
  }
  pushBool("localOnly", labels.localOnly, profile.require.localOnly);
  pushBool("remoteAllowed", labels.remoteAllowed, profile.require.remoteAllowed);
  pushBool("encryptedCodexTasks", labels.encryptedCodexTasks, profile.require.encryptedCodexTasks);
  return rows;
}

/** Compatibility gate rows — Off when absent; configured details when present. */
export function overviewCompatibilityRows(
  profile: RoutingProfileDto,
  labels: {
    gates: string;
    suites: string;
    minStatus: string;
    maxAge: string;
    unknown: string;
    degraded: string;
    allow: string;
    warn: string;
    skip: string;
    probed: string;
    verified: string;
    off: string;
    unavailable: string;
    days: (n: number) => string;
  },
): OverviewKv[] {
  const compat = profile.compatibility;
  if (!compat) {
    return [{ key: "compatibility", label: labels.gates, value: labels.off }];
  }
  const modeLabels = { allow: labels.allow, warn: labels.warn, skip: labels.skip };
  const rows: OverviewKv[] = [
    {
      key: "requiredSuites",
      label: labels.suites,
      value: requiredSuiteLabel(profile, labels.unavailable),
    },
  ];
  if (compat.minStatus) {
    rows.push({
      key: "minStatus",
      label: labels.minStatus,
      value: minStatusLabel(compat.minStatus, { probed: labels.probed, verified: labels.verified }, labels.unavailable),
    });
  }
  if (compat.maxEvidenceAgeMs !== undefined) {
    rows.push({
      key: "maxEvidenceAgeMs",
      label: labels.maxAge,
      value: evidenceAgeLabel(compat.maxEvidenceAgeMs, labels.unavailable, labels.days),
    });
  }
  if (compat.unknownEvidence) {
    rows.push({
      key: "compatUnknownEvidence",
      label: labels.unknown,
      value: evidenceModeLabel(compat.unknownEvidence, modeLabels, labels.unavailable),
    });
  }
  if (compat.degradedEvidence) {
    rows.push({
      key: "degradedEvidence",
      label: labels.degraded,
      value: evidenceModeLabel(compat.degradedEvidence, modeLabels, labels.unavailable),
    });
  }
  return rows;
}

/** Ordered candidate summary for Overview — provider and model kept distinct. */
export function overviewCandidateRows(
  profile: RoutingProfileDto,
): Array<{ index: number; provider: string; model: string }> {
  return profile.candidates.map((candidate, index) => ({
    index: index + 1,
    provider: candidate.provider,
    model: candidate.model,
  }));
}

/** Limits rows — only values the DTO actually carries. */
export function overviewLimitRows(
  profile: RoutingProfileDto,
  labels: {
    maxCost: string;
    onUnknownCost: string;
    allow: string;
    exclude: string;
    unavailable: string;
  },
): OverviewKv[] {
  const rows: OverviewKv[] = [];
  if (profile.limits.maxEstimatedCostUsd !== undefined) {
    rows.push({
      key: "maxEstimatedCostUsd",
      label: labels.maxCost,
      value: usdCostLabel(profile.limits.maxEstimatedCostUsd, labels.unavailable),
    });
  }
  if (profile.limits.onUnknownCost === "allow" || profile.limits.onUnknownCost === "exclude") {
    rows.push({
      key: "onUnknownCost",
      label: labels.onUnknownCost,
      value: profile.limits.onUnknownCost === "allow" ? labels.allow : labels.exclude,
    });
  }
  return rows;
}

/** Unknown-evidence policy rows (always present on the DTO). */
export function overviewUnknownEvidenceRows(
  profile: RoutingProfileDto,
  labels: {
    capability: string;
    health: string;
    quota: string;
    cost: string;
    allow: string;
    warn: string;
    skip: string;
    unavailable: string;
  },
): OverviewKv[] {
  const modeLabels = { allow: labels.allow, warn: labels.warn, skip: labels.skip };
  const unknown = profile.unknownEvidence;
  return [
    {
      key: "unknownCapability",
      label: labels.capability,
      value: evidenceModeLabel(unknown.capability, modeLabels, labels.unavailable),
    },
    {
      key: "unknownHealth",
      label: labels.health,
      value: evidenceModeLabel(unknown.health, modeLabels, labels.unavailable),
    },
    {
      key: "unknownQuota",
      label: labels.quota,
      value: evidenceModeLabel(unknown.quota, modeLabels, labels.unavailable),
    },
    {
      key: "unknownCost",
      label: labels.cost,
      value: evidenceModeLabel(unknown.cost, modeLabels, labels.unavailable),
    },
  ];
}
