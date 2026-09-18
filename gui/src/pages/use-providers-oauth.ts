import { useCallback, useRef } from "react";
import type { TFn, TKey } from "../i18n/shared";
import { readJsonIfOk } from "../fetch-json";
import type { OAuthAccount, OAuthStatus } from "./providers-shared";
import { oauthLabel } from "./providers-shared";
import {
  providersOAuthAccountFetchList,
  providersOAuthCompletedTarget,
  providersOAuthIdentityOutcome,
  providersOAuthLoginBody,
  providersOAuthLoginInfo,
  providersOAuthPollProgress,
  providersOAuthReauthTargetId,
  providersOAuthSameIdentityAdd,
  providersOAuthSeededAccountSet,
  providersOAuthStartFailedMessage,
  type ProvidersOAuthStatusPayload,
} from "../lib/providers-oauth-policy";

/** The accounts the board knows for one provider, and which one is active. */
type AccountSet = { activeAccountId: string | null; accounts: OAuthAccount[] };

/** Copy the login wait renders while the browser is open. */
type ProvidersOAuthLoginHint = {
  provider: string;
  url?: string;
  instructions?: string;
  deviceCode?: string;
};

/** Board state this flow writes back into. */
type Setter<T> = React.Dispatch<React.SetStateAction<T>>;

type Notifier = (msg: string, ok: boolean) => void;

type ProvidersOAuthState = {
  setBusy: Setter<string | null>;
  setStatus: Setter<string>;
  setLoginInfo: Setter<ProvidersOAuthLoginHint | null>;
  setOauthStatus: Setter<Record<string, OAuthStatus>>;
  setAccountSets: Setter<Record<string, AccountSet>>;
};

type AsyncTask = () => Promise<void>;
type VoidTask = () => void;
type AccountListReader = (providers: string[]) => Promise<unknown>;
type QuotaRefresh = (refresh?: boolean) => Promise<void>;

/** Listener reads the flow performs once a login settles. */
type ProvidersOAuthReaders = {
  fetchConfig: AsyncTask;
  fetchOauth: AsyncTask;
  fetchAccountSets: AccountListReader;
  fetchProviderQuotas: QuotaRefresh;
  bumpModelsRefresh: VoidTask;
  /** Select the provider and open Accounts after a successful login. */
  onLoginSettled?: (provider: string) => void;
};

/**
 * Everything the OAuth flow reads and writes outside itself. Providers.tsx
 * injects these, so this hook owns the flow and the board owns its own state.
 */
type ProvidersOAuthPorts = ProvidersOAuthState & ProvidersOAuthReaders & {
  aliveRef: React.MutableRefObject<boolean>;
  t: TFn;
  notify: Notifier;
};

export type ProvidersOAuthInput = ProvidersOAuthPorts & {
  apiBase: string;
  accountSets: Record<string, AccountSet>;
};

/**
 * One login in flight. `generation` is bumped by every new start and every
 * cancel, so a late poll tick from a superseded attempt can never write back.
 */
type ProvidersOAuthFlow = ProvidersOAuthPorts & {
  apiBase: string;
  accountSets: Record<string, AccountSet>;
  generations: React.MutableRefObject<Map<string, number>>;
  provider: string;
  generation: number;
  addAccount: boolean;
  reauthTargetId: string | undefined;
};

/** Ticks a login may run before the flow asks the listener to drop it. */
const PROVIDERS_OAUTH_POLL_TICKS = 150;
const PROVIDERS_OAUTH_POLL_INTERVAL_MS = 2_000;

function flowIsCurrent(flow: ProvidersOAuthFlow): boolean {
  return flow.generations.current.get(flow.provider) === flow.generation && flow.aliveRef.current;
}

/** Drop one provider's busy flag and pending hint, and touch nothing else. */
function clearProviderLoginState(input: {
  provider: string;
  setBusy: ProvidersOAuthState["setBusy"];
  setLoginInfo: ProvidersOAuthState["setLoginInfo"];
}) {
  input.setBusy(current => current === input.provider ? null : current);
  input.setLoginInfo(current => current?.provider === input.provider ? null : current);
}

function providerLabel(flow: { provider: string; t: TFn }): string {
  return oauthLabel(flow.provider);
}

/** Anything that can raise a provider-labelled notice on the board. */
type ProviderMessageSink = {
  provider: string;
  t: TFn;
  notify: (msg: string, ok: boolean) => void;
};

/** Every OAuth notice names the provider it belongs to, so the label is bound here. */
function notifyProvider(sink: ProviderMessageSink, key: TKey, ok: boolean, extra?: { error?: string; cmd?: string }) {
  sink.notify(sink.t(key, { provider: providerLabel(sink), ...extra }), ok);
}

function notifyProviderError(sink: ProviderMessageSink, error: string) {
  notifyProvider(sink, "prov.loginError", false, { error });
}

/** JSON POST shared by the login start, the cancel, and the paste-code call. */
async function postProvidersOAuthJson(url: string, body: unknown): Promise<Response> {
  return fetch(url, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
}

/** Post the login start and surface its hint. False means the flow stops here. */
async function openProvidersOAuthLogin(flow: ProvidersOAuthFlow): Promise<boolean> {
  const response = await postProvidersOAuthJson(
    `${flow.apiBase}/api/oauth/login`,
    providersOAuthLoginBody(flow.provider, flow.addAccount, flow.reauthTargetId),
  );
  if (!flowIsCurrent(flow)) return false;
  if (!response.ok) {
    const payload = await response.json().catch(() => ({})) as { error?: string };
    flow.notify(
      providersOAuthStartFailedMessage(payload, flow.t("prov.loginFailStart", { provider: providerLabel(flow) })),
      false,
    );
    return false;
  }
  const payload = await response.json() as { url?: string; instructions?: string; deviceCode?: string };
  const hint = providersOAuthLoginInfo(payload, flow.provider);
  if (hint) flow.setLoginInfo(hint);
  return true;
}

function publishProvidersOAuthStatus(flow: ProvidersOAuthFlow, status: ProvidersOAuthStatusPayload) {
  flow.setOauthStatus(previous => ({ ...previous, [flow.provider]: status }));
}

function reportProvidersOAuthRefusal(
  flow: ProvidersOAuthFlow,
  status: ProvidersOAuthStatusPayload,
  cancelled: boolean,
  error: string,
) {
  publishProvidersOAuthStatus(flow, status);
  if (cancelled) notifyProvider(flow, "prov.loginCancelled", false);
  else notifyProviderError(flow, error);
  flow.setLoginInfo(null);
}

/** A reauth that lands on a different identity is refused, not silently accepted. */
function providersOAuthIdentityRefusal(flow: ProvidersOAuthFlow, status: ProvidersOAuthStatusPayload): boolean {
  const target = providersOAuthCompletedTarget(status.accounts, flow.reauthTargetId, status.activeAccountId);
  const identity = providersOAuthIdentityOutcome({ reauthTargetId: flow.reauthTargetId, target });
  if (identity === "ok") return false;
  const errorKey = identity === "missing" ? "prov.reauthAccountMissing" : "prov.reauthIdentityMismatch";
  notifyProviderError(flow, flow.t(errorKey));
  flow.setLoginInfo(null);
  return true;
}

/** The browser came back: seed the new account, then re-read and report. */
async function acceptProvidersOAuthCompletion(
  flow: ProvidersOAuthFlow,
  status: ProvidersOAuthStatusPayload,
  statusCount: number,
  baselineCount: number,
) {
  publishProvidersOAuthStatus(flow, status);
  if (providersOAuthIdentityRefusal(flow, status)) return;
  const seeded = providersOAuthSeededAccountSet(status.accounts, status.activeAccountId);
  if (seeded) {
    flow.setAccountSets(current => ({ ...current, [flow.provider]: seeded }));
  }
  flow.setLoginInfo(null);
  flow.onLoginSettled?.(flow.provider);
  await flow.fetchAccountSets(providersOAuthAccountFetchList(Object.keys(flow.accountSets), flow.provider));
  if (!flowIsCurrent(flow)) return;
  const sameIdentity = providersOAuthSameIdentityAdd(flow.addAccount, flow.reauthTargetId, statusCount, baselineCount);
  if (sameIdentity) notifyProvider(flow, "prov.loginSameAccount", false);
  else notifyProvider(flow, "prov.loginOk", true, { cmd: "benes sync" });
  void flow.fetchConfig();
  void flow.fetchProviderQuotas(true);
  flow.bumpModelsRefresh();
}

/**
 * Watch the status endpoint until the listener answers. Returns true once the
 * login has been settled one way or the other, false when the tick budget ran
 * out and the caller still has to time the attempt out.
 */
async function watchProvidersOAuthLogin(flow: ProvidersOAuthFlow): Promise<boolean> {
  const baselineCount = flow.accountSets[flow.provider]?.accounts.length ?? 0;
  for (let tick = 0; tick < PROVIDERS_OAUTH_POLL_TICKS && flowIsCurrent(flow); tick++) {
    await new Promise(resolve => setTimeout(resolve, PROVIDERS_OAUTH_POLL_INTERVAL_MS));
    if (!flowIsCurrent(flow)) return true;
    const response = await fetch(`${flow.apiBase}/api/oauth/status?provider=${flow.provider}`).catch(() => null);
    const status = response ? ((await readJsonIfOk<ProvidersOAuthStatusPayload>(response)) ?? null) : null;
    const progress = providersOAuthPollProgress(status, {
      addAccount: flow.addAccount,
      reauthTargetId: flow.reauthTargetId,
      baselineCount,
    });
    if (progress.kind === "continue") continue;
    if (progress.kind === "status-error") {
      reportProvidersOAuthRefusal(flow, status!, progress.cancelled, progress.error);
      return true;
    }
    await acceptProvidersOAuthCompletion(flow, status!, progress.statusCount, baselineCount);
    return true;
  }
  return false;
}

/** The listener never answered: drop the attempt rather than leave it pinned. */
async function timeOutProvidersOAuthLogin(flow: ProvidersOAuthFlow) {
  if (!flowIsCurrent(flow)) return;
  await postProvidersOAuthJson(`${flow.apiBase}/api/oauth/login/cancel`, { provider: flow.provider }).catch(() => {});
  notifyProvider(flow, "prov.loginTimeout", false);
  flow.setLoginInfo(null);
}

export function useProvidersOAuth(input: ProvidersOAuthInput) {
  const { apiBase, accountSets, aliveRef, t, setBusy, setStatus, setLoginInfo, setOauthStatus, notify } = input;
  const { setAccountSets, fetchConfig, fetchOauth, fetchAccountSets, fetchProviderQuotas, bumpModelsRefresh, onLoginSettled } = input;
  const generations = useRef(new Map<string, number>());

  const ports: ProvidersOAuthPorts = {
    aliveRef, t, setBusy, setStatus, setLoginInfo, setOauthStatus, setAccountSets, notify,
    fetchConfig, fetchOauth, fetchAccountSets, fetchProviderQuotas, bumpModelsRefresh, onLoginSettled,
  };

  const bumpGeneration = useCallback((provider: string) => {
    const next = (generations.current.get(provider) ?? 0) + 1;
    generations.current.set(provider, next);
    return next;
  }, []);

  const clearProviderBusyState = useCallback((provider: string) => {
    clearProviderLoginState({ provider, setBusy, setLoginInfo });
  }, [setBusy, setLoginInfo]);

  const cancelLoginOAuth = useCallback(async (provider: string) => {
    const generation = bumpGeneration(provider);
    try {
      await postProvidersOAuthJson(`${apiBase}/api/oauth/login/cancel`, { provider });
    } catch { /* the cancel is best effort; the generation guard does the work */ }
    if (!aliveRef.current) return;
    if (generations.current.get(provider) === generation) clearProviderBusyState(provider);
    notifyProvider({ provider, t, notify }, "prov.loginCancelled", false);
  }, [aliveRef, apiBase, bumpGeneration, clearProviderBusyState, notify, t]);

  const loginOAuth = async (provider: string, addAccount = false, accountId?: string) => {
    const generation = bumpGeneration(provider);
    setBusy(provider);
    setStatus("");
    setLoginInfo(null);
    const flow: ProvidersOAuthFlow = {
      ...ports,
      apiBase,
      accountSets,
      generations,
      provider,
      generation,
      addAccount,
      reauthTargetId: providersOAuthReauthTargetId(accountId),
    };
    try {
      if (!await openProvidersOAuthLogin(flow)) return;
      if (!await watchProvidersOAuthLogin(flow)) await timeOutProvidersOAuthLogin(flow);
    } catch {
      if (generations.current.get(provider) === generation) {
        notifyProvider({ provider, t, notify }, "prov.loginRequestFail", false);
      }
    } finally {
      if (aliveRef.current && generations.current.get(provider) === generation) setBusy(null);
    }
  };

  const logoutOAuth = async (provider: string) => {
    bumpGeneration(provider);
    clearProviderBusyState(provider);
    try {
      const response = await fetch(`${apiBase}/api/oauth/logout?provider=${encodeURIComponent(provider)}`, { method: "POST" });
      if (!response.ok) {
        notifyProvider({ provider, t, notify }, "prov.logoutFail", false);
        return;
      }
      const reads = [
        fetchAccountSets([provider]),
        fetchOauth(),
        fetchConfig(),
        fetchProviderQuotas(true),
      ];
      await Promise.all(reads);
      bumpModelsRefresh();
      notifyProvider({ provider, t, notify }, "prov.logoutOk", true);
    } catch {
      notifyProvider({ provider, t, notify }, "prov.logoutFail", false);
    }
  };

  return { cancelLoginOAuth, loginOAuth, logoutOAuth };
}
