/**
 * Config-only provider classification for the Providers workspace.
 * `/api/providers/workspace` lifecycle, when present, overrides these bins.
 */
import {
  CODEX_FORWARD_BASE_URL,
  isCanonicalCodexForwardShape,
  isCanonicalCodexForwardUrl,
  normalizeProviderBaseUrl,
} from "./openai-forward-policy.ts";

export interface WorkspaceProvider {
  adapter: string;
  baseUrl: string;
  hasApiKey?: boolean;
  hasHeaders?: boolean;
  defaultModel?: string;
  apiKeyTransport?: "x-api-key" | "bearer";
  reasoningWireFormat?: "gateway-object";
  models?: string[];
  liveModels?: boolean;
  authMode?: "key" | "forward" | "oauth" | "local" | string;
  keyOptional?: boolean;
  freeTier?: boolean;
  disabled?: boolean;
  note?: string;
  allowPrivateNetwork?: boolean;
  requestPacingMs?: number;
  requestPacing?: {
    enabled?: boolean;
    requestsPerMinute?: number;
    minIntervalMs?: number;
    models?: Record<string, { requestsPerMinute?: number; minIntervalMs?: number }>;
  };
  codexAccountMode?: "direct" | "pool";
  defaultAccess?: "oauth" | "api";
  access?: import("./auth").ProviderAccessDescriptor;
}

export type ProviderTier = "free" | "paid" | "accounts";
export type ProviderWorkspaceLifecycle = "healthy" | "attention" | "disabled";
export type ProviderStatus = "ready" | "needs-setup" | "disabled";
export type ProviderBoardGroup = "connected" | "attention" | "disabled";
export type ProviderSortMode = "az" | "za" | "free-paid" | "paid-free" | "accounts-first";
export type ProviderKind = "cloud" | "local" | "selfHosted" | "login";

const LOGIN_AUTH = new Set(["oauth", "forward"]);
export const SELF_HOSTED_HINTS = ["ollama", "vllm", "lm-studio", "lmstudio", "litellm", "localai"];

export interface WorkspaceItem extends WorkspaceProvider {
  name: string;
  tier?: ProviderTier;
  activeNeedsReauth?: boolean;
  workspaceLifecycle?: ProviderWorkspaceLifecycle;
}

export interface WorkspaceSections {
  ready: WorkspaceItem[];
  needsSetup: WorkspaceItem[];
  disabled: WorkspaceItem[];
}

const CANONICAL_FORWARD_PROVIDER = "openai";
const READY_AUTH = new Set(["oauth", "forward", "local"]);
const LIFECYCLE_STATUS: Record<ProviderWorkspaceLifecycle, ProviderStatus> = {
  disabled: "disabled",
  attention: "needs-setup",
  healthy: "ready",
};
const LIFECYCLE_GROUP: Record<ProviderWorkspaceLifecycle, ProviderBoardGroup> = {
  disabled: "disabled",
  attention: "attention",
  healthy: "connected",
};

const STATIC_MODEL_CATALOG_TRANSPORTS: Readonly<Record<string, { adapter: string; baseUrl: string }>> = {
  "cline-pass": { adapter: "openai-chat", baseUrl: `${new URL(CODEX_FORWARD_BASE_URL).protocol}//api.cline.bot/api/v1` },
  "mimo-free": {
    adapter: "mimo-free",
    baseUrl: `${new URL(CODEX_FORWARD_BASE_URL).protocol}//api.xiaomimimo.com/api/free-ai/openai/chat`,
  },
};

export function isLocalProvider(item: WorkspaceProvider): boolean {
  return item.authMode === "local" || hasLoopbackBaseUrl(item.baseUrl);
}

function mentionsSelfHostedRuntime(item: WorkspaceProvider & { name?: string }): boolean {
  const haystack = `${item.name ?? ""} ${item.adapter} ${item.baseUrl}`.toLowerCase();
  return SELF_HOSTED_HINTS.some(token => haystack.includes(token));
}

export function providerKind(item: WorkspaceProvider & { name?: string }): ProviderKind {
  if (LOGIN_AUTH.has((item.authMode ?? "").toLowerCase())) return "login";
  if (isLocalProvider(item)) return "local";
  if (mentionsSelfHostedRuntime(item)) return "selfHosted";
  return "cloud";
}

export function hasLoopbackBaseUrl(baseUrl: string): boolean {
  try {
    const hostname = new URL(baseUrl).hostname.replace(/^\[|\]$/g, "").toLowerCase();
    return hostname === "localhost" || hostname === "127.0.0.1" || hostname === "::1";
  } catch {
    return false;
  }
}

function credentialsArePresent(provider: WorkspaceProvider): boolean {
  return provider.keyOptional === true
    || READY_AUTH.has(provider.authMode ?? "")
    || hasLoopbackBaseUrl(provider.baseUrl)
    || provider.hasApiKey === true;
}

export function providerSupportsLiveModelDiscovery(name: string, provider: WorkspaceProvider): boolean {
  const frozen = STATIC_MODEL_CATALOG_TRANSPORTS[name];
  if (!frozen || provider.adapter !== frozen.adapter) return true;
  return normalizeProviderBaseUrl(provider.baseUrl) !== normalizeProviderBaseUrl(frozen.baseUrl);
}

export function isAccountProvider(name: string, provider: WorkspaceProvider): boolean {
  return name === CANONICAL_FORWARD_PROVIDER && isCanonicalCodexForwardShape(provider);
}

export function isFreeProvider(provider: WorkspaceProvider): boolean {
  return provider.freeTier === true
    || provider.keyOptional === true
    || provider.authMode === "local"
    || hasLoopbackBaseUrl(provider.baseUrl);
}

export function providerTier(name: string, provider: WorkspaceProvider): ProviderTier {
  if (isAccountProvider(name, provider)) return "accounts";
  if (isFreeProvider(provider)) return "free";
  return "paid";
}

function compareNames(left: WorkspaceItem, right: WorkspaceItem): number {
  return left.name.localeCompare(right.name, undefined, { sensitivity: "base" });
}

function tierOf(item: WorkspaceItem): ProviderTier {
  return item.tier ?? providerTier(item.name, item);
}

export function sortWorkspaceItems(items: WorkspaceItem[], mode: ProviderSortMode): WorkspaceItem[] {
  const copy = [...items];
  switch (mode) {
    case "az":
      return copy.sort(compareNames);
    case "za":
      return copy.sort((left, right) => compareNames(right, left));
    case "free-paid":
      return copy.sort((left, right) => Number(tierOf(left) !== "free") - Number(tierOf(right) !== "free") || compareNames(left, right));
    case "paid-free":
      return copy.sort((left, right) => Number(tierOf(left) === "free") - Number(tierOf(right) === "free") || compareNames(left, right));
    case "accounts-first": {
      const rank = (item: WorkspaceItem) => {
        const tier = tierOf(item);
        if (tier === "accounts") return 0;
        if (tier === "free") return 1;
        return 2;
      };
      return copy.sort((left, right) => rank(left) - rank(right) || compareNames(left, right));
    }
    default:
      return copy;
  }
}

export function buildProviderWorkspace(
  providers: Record<string, WorkspaceProvider>,
): WorkspaceSections {
  const sections: WorkspaceSections = { ready: [], needsSetup: [], disabled: [] };
  for (const [name, row] of Object.entries(providers)) {
    if (row.disabled) {
      sections.disabled.push({ name, ...row });
      continue;
    }
    if (credentialsArePresent(row)) {
      sections.ready.push({ name, ...row, tier: providerTier(name, row) });
    } else {
      sections.needsSetup.push({ name, ...row });
    }
  }
  return sections;
}

export function applyActiveAccountReauth(
  sections: WorkspaceSections,
  activeNeedsReauth: Readonly<Record<string, boolean>>,
): WorkspaceSections {
  const flagged = new Set(
    Object.entries(activeNeedsReauth).filter(([, needs]) => needs).map(([name]) => name),
  );
  if (flagged.size === 0) return sections;
  const mark = (items: WorkspaceItem[]) => items.map(item => (
    flagged.has(item.name) ? { ...item, activeNeedsReauth: true } : item
  ));
  return {
    ready: mark(sections.ready),
    needsSetup: mark(sections.needsSetup),
    disabled: sections.disabled,
  };
}

export function binProviderStatus(provider: WorkspaceProvider | WorkspaceItem): ProviderStatus {
  if ("workspaceLifecycle" in provider && provider.workspaceLifecycle) {
    return LIFECYCLE_STATUS[provider.workspaceLifecycle];
  }
  if (provider.disabled) return "disabled";
  if ("activeNeedsReauth" in provider && provider.activeNeedsReauth) return "needs-setup";
  return credentialsArePresent(provider) ? "ready" : "needs-setup";
}

export function providerBoardGroup(item: WorkspaceItem): ProviderBoardGroup {
  if (item.workspaceLifecycle) return LIFECYCLE_GROUP[item.workspaceLifecycle];
  if (item.disabled === true) return "disabled";
  if (item.activeNeedsReauth || binProviderStatus(item) !== "ready") return "attention";
  return "connected";
}

export function hideRedundantChatGptForwardProviders<T extends WorkspaceProvider>(
  providers: Record<string, T>,
): Record<string, T> {
  let next = providers;
  const openai = providers.openai;
  const chatgpt = providers.chatgpt;
  if (openai && chatgpt && isAccountProvider("openai", openai) && isCanonicalCodexForwardUrl(chatgpt.baseUrl) && isCanonicalCodexForwardShape(chatgpt)) {
    next = { ...providers };
    delete next.chatgpt;
  }
  const folded = next.openai;
  if (folded && next["openai-apikey"] && isAccountProvider("openai", folded)) {
    if (next === providers) next = { ...providers };
    delete next["openai-apikey"];
  }
  return next;
}
