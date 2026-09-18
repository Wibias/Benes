/**
 * Providers page chrome: selection, overlays, quota invalidation, and notice toasts.
 * Overlay flags are independent because Add Provider, Codex login, remove, ToS, and
 * unsaved-leave can each be requested from different surfaces.
 */
import type { NoticeTone } from "../ui";
import type { AddProviderIntent } from "../components/provider-workspace/workspace-board-frames";
export type LoginHint = {
  provider: string;
  url?: string;
  instructions?: string;
  deviceCode?: string;
};
import type { CodexAccountEntry } from "../hooks/useCodexAccountPool";
import type { OAuthStatus, ProvidersConfig } from "./providers-shared";

export type ProvidersChrome = {
  selected: string | null;
  busy: string | null;
  loginHint: LoginHint | null;
  addOpen: boolean;
  addIntent: AddProviderIntent | null;
  removeName: string | null;
  codexLogin: boolean;
  tos: { provider: string; addAccount: boolean } | null;
  accountsFocus: { token: number; provider: string | null };
  modelsEpoch: number;
  quotaEpoch: number;
  quotaForce: boolean;
};

export type ProvidersNotice = {
  text: string;
  ok: boolean;
  tone: NoticeTone;
  revision: number;
  harness: boolean;
  syncBusy: boolean;
};

export const INITIAL_CHROME: ProvidersChrome = {
  selected: null,
  busy: null,
  loginHint: null,
  addOpen: false,
  addIntent: null,
  removeName: null,
  codexLogin: false,
  tos: null,
  accountsFocus: { token: 0, provider: null },
  modelsEpoch: 0,
  quotaEpoch: 0,
  quotaForce: false,
};

export const INITIAL_NOTICE: ProvidersNotice = {
  text: "",
  ok: false,
  tone: "err",
  revision: 0,
  harness: false,
  syncBusy: false,
};

export type ChromeEvent =
  | { type: "select"; name: string | null }
  | { type: "busy"; name: string | null }
  | { type: "hint"; hint: LoginHint | null }
  | { type: "open-add"; intent?: AddProviderIntent | null }
  | { type: "close-add" }
  | { type: "ask-remove"; name: string }
  | { type: "close-remove" }
  | { type: "codex-login"; open: boolean }
  | { type: "tos"; value: { provider: string; addAccount: boolean } | null }
  | { type: "reveal-accounts"; name: string }
  | { type: "bump-models" }
  | { type: "invalidate-quotas"; force?: boolean };

export function reduceChrome(state: ProvidersChrome, event: ChromeEvent): ProvidersChrome {
  switch (event.type) {
    case "select":
      return { ...state, selected: event.name };
    case "busy":
      return { ...state, busy: event.name };
    case "hint":
      return { ...state, loginHint: event.hint };
    case "open-add":
      return { ...state, addOpen: true, addIntent: event.intent ?? null };
    case "close-add":
      return { ...state, addOpen: false, addIntent: null };
    case "ask-remove":
      return { ...state, removeName: event.name };
    case "close-remove":
      return { ...state, removeName: null };
    case "codex-login":
      return { ...state, codexLogin: event.open };
    case "tos":
      return { ...state, tos: event.value };
    case "reveal-accounts":
      return {
        ...state,
        addOpen: false,
        addIntent: null,
        selected: event.name,
        accountsFocus: { token: state.accountsFocus.token + 1, provider: event.name },
      };
    case "bump-models":
      return { ...state, modelsEpoch: state.modelsEpoch + 1 };
    case "invalidate-quotas":
      return { ...state, quotaEpoch: state.quotaEpoch + 1, quotaForce: Boolean(event.force) };
    default:
      return state;
  }
}

export type NoticeEvent =
  | { type: "show"; text: string; ok: boolean; tone?: NoticeTone; harness?: boolean }
  | { type: "clear" }
  | { type: "sync-busy"; on: boolean };

export function reduceNotice(state: ProvidersNotice, event: NoticeEvent): ProvidersNotice {
  if (event.type === "clear") {
    return { ...INITIAL_NOTICE, revision: state.revision };
  }
  if (event.type === "sync-busy") {
    return { ...state, syncBusy: event.on };
  }
  return {
    text: event.text,
    ok: event.ok,
    tone: event.tone ?? (event.ok ? "ok" : "err"),
    revision: state.revision + 1,
    harness: Boolean(event.ok && event.harness),
    syncBusy: false,
  };
}

export function mergeCodexOauthStatus(
  oauthStatus: Record<string, OAuthStatus>,
  accounts: readonly CodexAccountEntry[],
  loadState: string,
  activeNeedsReauth: boolean,
): Record<string, OAuthStatus> {
  if (accounts.length === 0 && loadState === "loading") return oauthStatus;
  const main = accounts.find(account => account.isMain) ?? accounts[0];
  const mainIsReal = Boolean(main?.email && main.email !== "Codex App login");
  const poolLoggedIn = accounts.some(account => !account.isMain && (account.hasCredential || account.email));
  const email = mainIsReal
    ? main?.email
    : accounts.find(account => !account.isMain && account.email)?.email;
  return {
    ...oauthStatus,
    openai: {
      loggedIn: mainIsReal || poolLoggedIn,
      ...(email ? { email } : {}),
      ...(activeNeedsReauth ? { needsReauth: true } : {}),
    },
  };
}

export function successorDefaultName(config: ProvidersConfig, removing: string | null): string | null {
  if (!removing || removing !== config.defaultProvider) return null;
  return Object.entries(config.providers).find(([name, row]) => name !== removing && row.disabled !== true)?.[0] ?? null;
}
