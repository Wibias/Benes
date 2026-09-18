/**
 * Which authentication path a Provider row should use.
 * Codex credential implementation stays in #236.
 */
import type { TFn } from "../i18n/shared.ts";
import { formatProviderDisplayName } from "../provider-icons.ts";
import {
  codexAccountProviderNames,
  openAiAccountProviderState,
} from "./openai-forward-policy.ts";
import type { OAuthStatus, ProvidersConfig } from "../pages/providers-shared.ts";
import { oauthLabel } from "../pages/providers-shared.ts";

export type AccountLoginTarget = {
  channel: "codex" | "oauth";
  providerId: string;
};

export function listAccountLoginTargets(
  config: ProvidersConfig,
  oauthProviderIds: readonly string[],
): AccountLoginTarget[] {
  const reserved = codexAccountProviderNames(config.providers).map(providerId => ({
    channel: "codex" as const,
    providerId,
  }));
  const oauth = [...oauthProviderIds]
    .toSorted((left, right) => left.localeCompare(right))
    .map(providerId => ({ channel: "oauth" as const, providerId }));
  const targets: AccountLoginTarget[] = [...reserved, ...oauth];
  return targets;
}

export function accountLoginLabel(target: AccountLoginTarget, t: TFn): string {
  return target.channel === "codex"
    ? formatProviderDisplayName(target.providerId, t)
    : oauthLabel(target.providerId);
}

export function mirrorForwardLoginStatus(
  providers: ProvidersConfig["providers"],
  oauthStatus: Record<string, OAuthStatus>,
): Record<string, OAuthStatus> {
  const openai = oauthStatus.openai;
  if (openai == null) return oauthStatus;
  const next: Record<string, OAuthStatus> = { ...oauthStatus };
  for (const name of Object.keys(providers)) {
    if (providers[name]?.authMode === "forward") next[name] = openai;
  }
  return next;
}

export type AccountLoginPlan =
  | { kind: "invalid-openai" }
  | { kind: "ensure-openai"; state: "absent" | "disabled" }
  | { kind: "codex-login" }
  | { kind: "oauth" }
  | { kind: "none" };

export function planAccountLogin(
  config: ProvidersConfig,
  provider: string,
  oauthProviders: readonly string[],
): AccountLoginPlan {
  if (provider === "openai") {
    const state = openAiAccountProviderState(config.providers.openai);
    if (state === "invalid") return { kind: "invalid-openai" };
    if (state === "absent" || state === "disabled") return { kind: "ensure-openai", state };
    return { kind: "codex-login" };
  }
  if (config.providers[provider]?.authMode === "forward") return { kind: "codex-login" };
  if (config.providers[provider]?.authMode === "oauth" || oauthProviders.includes(provider)) {
    return { kind: "oauth" };
  }
  return { kind: "none" };
}

export type AccountLoginRuntime = {
  busy: string | null;
  markBusy: (name: string | null) => void;
  ensureReserved: (state: "absent" | "disabled") => Promise<void>;
  startCodexLogin: () => void;
  startOauth: (provider: string, addAccount: boolean) => void;
  onInvalid: () => void;
  onEnsureFailed: (error: unknown) => void;
  stillMounted: () => boolean;
};

export async function executeAccountLogin(
  plan: AccountLoginPlan,
  provider: string,
  addAccount: boolean,
  runtime: AccountLoginRuntime,
): Promise<void> {
  if (plan.kind === "invalid-openai") {
    runtime.onInvalid();
    return;
  }
  if (plan.kind === "ensure-openai") {
    if (runtime.busy === "openai") return;
    runtime.markBusy("openai");
    try {
      await runtime.ensureReserved(plan.state);
    } catch (error) {
      runtime.onEnsureFailed(error);
      return;
    } finally {
      if (runtime.stillMounted()) runtime.markBusy(null);
    }
    runtime.startCodexLogin();
    return;
  }
  if (plan.kind === "codex-login") {
    runtime.startCodexLogin();
    return;
  }
  if (plan.kind === "oauth") runtime.startOauth(provider, addAccount);
}
