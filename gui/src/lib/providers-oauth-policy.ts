/** Login-body, poll-completion, and identity policy for Providers OAuth. */
import type { OAuthAccount, OAuthStatus } from "../pages/providers-shared";

export function providersOAuthReauthTargetId(accountId?: string): string | undefined {
  return accountId?.trim() || undefined;
}

export function providersOAuthLoginBody(
  provider: string,
  addAccount: boolean,
  accountId?: string,
): Record<string, unknown> {
  const reauthTargetId = providersOAuthReauthTargetId(accountId);
  return {
    provider,
    ...(addAccount || reauthTargetId ? { addAccount: true } : {}),
    ...(reauthTargetId ? { accountId: reauthTargetId, reauth: true } : {}),
  };
}

export type ProvidersOAuthLoginInfo = {
  provider: string;
  url?: string;
  instructions?: string;
  deviceCode?: string;
};

export function providersOAuthLoginInfo(
  data: { url?: string; instructions?: string; deviceCode?: string },
  provider: string,
): ProvidersOAuthLoginInfo | null {
  if (data.url || data.instructions || data.deviceCode) {
    return {
      provider,
      url: data.url,
      instructions: data.instructions,
      deviceCode: data.deviceCode,
    };
  }
  return null;
}

export function providersOAuthErrorIsCancelled(error: string): boolean {
  return /cancel/i.test(error);
}

export type ProvidersOAuthStatusPayload = OAuthStatus & {
  accounts?: OAuthAccount[];
  activeAccountId?: string | null;
  pending?: boolean;
};

export type ProvidersOAuthPollProgress =
  | { kind: "continue" }
  | { kind: "status-error"; error: string; cancelled: boolean }
  | { kind: "completed"; statusCount: number };

export function providersOAuthPollProgress(
  status: ProvidersOAuthStatusPayload | null,
  input: { addAccount: boolean; reauthTargetId?: string; baselineCount: number },
): ProvidersOAuthPollProgress {
  if (!status) return { kind: "continue" };
  if (status.error) {
    return {
      kind: "status-error",
      error: status.error,
      cancelled: providersOAuthErrorIsCancelled(status.error),
    };
  }
  if (status.pending) return { kind: "continue" };
  const statusCount = status.accounts?.length ?? 0;
  const completed = input.addAccount || input.reauthTargetId
    ? (statusCount > input.baselineCount || status.done === true)
    : (status.loggedIn || status.done === true);
  if (!completed) return { kind: "continue" };
  return { kind: "completed", statusCount };
}

export function providersOAuthCompletedTarget(
  accounts: OAuthAccount[] | undefined,
  reauthTargetId: string | undefined,
  activeAccountId: string | null | undefined,
): OAuthAccount | undefined {
  if (reauthTargetId) return accounts?.find(account => account.id === reauthTargetId);
  return accounts?.find(account => account.active)
    ?? accounts?.find(account => account.id === activeAccountId);
}

export type ProvidersOAuthIdentityOutcome = "missing" | "mismatch" | "ok";

export function providersOAuthIdentityOutcome(input: {
  reauthTargetId?: string;
  target?: { needsReauth?: boolean };
}): ProvidersOAuthIdentityOutcome {
  if (input.reauthTargetId && !input.target) return "missing";
  if (input.target?.needsReauth) return "mismatch";
  return "ok";
}

export function providersOAuthSeededAccountSet(
  accounts: OAuthAccount[] | undefined,
  activeAccountId: string | null | undefined,
): { activeAccountId: string | null; accounts: OAuthAccount[] } | null {
  if (!accounts) return null;
  const activeFromRow = accounts.find(account => account.active)?.id ?? null;
  return {
    activeAccountId: activeAccountId ?? activeFromRow,
    accounts,
  };
}

export function providersOAuthSameIdentityAdd(
  addAccount: boolean,
  reauthTargetId: string | undefined,
  statusCount: number,
  baselineCount: number,
): boolean {
  return addAccount && !reauthTargetId && statusCount <= baselineCount;
}

export function providersOAuthAccountFetchList(
  knownProviders: string[],
  provider: string,
): string[] {
  return knownProviders.includes(provider) ? knownProviders : [...knownProviders, provider];
}

export function providersOAuthStartFailedMessage(
  data: { error?: string } | undefined,
  fallback: string,
): string {
  return data?.error || fallback;
}
