import { useCallback, useEffect, useLayoutEffect, useMemo, useReducer, useRef } from "react";
import ProviderWorkspaceShell from "../components/provider-workspace/ProviderWorkspaceShell";
import ProviderDetails from "../components/provider-workspace/ProviderDetails";
import { ProviderOverlays } from "../components/provider-workspace/provider-overlays";
import type { WorkspaceProvider } from "../provider-workspace/catalog";
import { oauthTosRisk } from "../oauth-tos-risk";
import { useT } from "../i18n/shared";
import { providerToastDismissMs } from "../lib/provider-notice-policy";
import { ProvidersNoticeBanner, ProvidersWorkspaceBoot } from "./providers-status";
import { syncEnabledHarnessModelLists } from "../lib/provider-harness-sync-apply";
import { useProviderCredentials } from "../hooks/useProviderCredentials";
import { useCodexAccountPool } from "../hooks/useCodexAccountPool";
import { useJsonConfigEditor } from "../hooks/useJsonConfigEditor";
import { useProvidersOAuth } from "./use-providers-oauth";
import { useProviderAccessDeepLink } from "./use-provider-access-deep-link";
import { OPENAI_ACCOUNT_ACCESS_HASH } from "../provider-workspace/provider-access-hash";
import { useProviderRegistry } from "./use-provider-registry";
import {
  accountLoginLabel,
  executeAccountLogin,
  listAccountLoginTargets,
  mirrorForwardLoginStatus,
  planAccountLogin,
} from "../provider-workspace/provider-account-login";
import {
  confirmRemoveNamedProvider,
  ensureOpenAiProvider,
  OpenAiEnableError,
  patchRegistryProvider,
  setRegistryDefaultProvider,
  setRegistryProviderDisabled,
} from "./provider-registry";
import {
  INITIAL_CHROME,
  INITIAL_NOTICE,
  mergeCodexOauthStatus,
  reduceChrome,
  reduceNotice,
  successorDefaultName,
} from "./providers-chrome";
import type { CodexAccountMutationCompletion } from "../codex-account-mutation";

export default function Providers({ apiBase }: { apiBase: string }) {
  const t = useT();
  const [chrome, dispatchChrome] = useReducer(reduceChrome, INITIAL_CHROME);
  const [notice, dispatchNotice] = useReducer(reduceNotice, INITIAL_NOTICE);
  const accessDeepLink = useProviderAccessDeepLink();

  const aliveRef = useRef(true);
  const removeBusyRef = useRef(false);
  const registryResources = useProviderRegistry(apiBase, t("prov.loadConfigFail"));
  const {
    configResource,
    config,
    oauthProviders,
    oauthStatus,
    setOauthStatus,
    reloadConfig,
    reloadOauth,
  } = registryResources;

  const notify = useCallback((msg: string, ok: boolean = true, options?: { offerHarnessSync?: boolean }) => {
    dispatchNotice({ type: "show", text: msg, ok, harness: options?.offerHarnessSync });
  }, []);

  useEffect(() => { aliveRef.current = true; return () => { aliveRef.current = false; }; }, []);
  useEffect(() => {
    if (!notice.text) return;
    const timer = window.setTimeout(() => dispatchNotice({ type: "clear" }), providerToastDismissMs({
      ok: notice.ok,
      offerHarnessSync: notice.harness,
    }));
    return () => window.clearTimeout(timer);
  }, [notice.text, notice.ok, notice.revision, notice.harness]);

  // The retired `#codex-auth` hash lands on Providers → OpenAI → Access through the same
  // reveal-accounts path the workspace already uses, so the deep link adds no page state.
  // Waiting for the config keeps a cold bookmark from selecting a provider that is not
  // in the workspace yet; an unknown name simply selects nothing.
  const appliedAccessDeepLinkRef = useRef(0);
  useEffect(() => {
    if (!config || !accessDeepLink) return;
    if (appliedAccessDeepLinkRef.current === accessDeepLink.token) return;
    appliedAccessDeepLinkRef.current = accessDeepLink.token;
    dispatchChrome({ type: "reveal-accounts", name: accessDeepLink.provider });
  }, [config, accessDeepLink]);

  const invalidateQuotas = useCallback((force = false) => {
    dispatchChrome({ type: "invalidate-quotas", force });
  }, []);

  const codexPool = useCodexAccountPool(apiBase);
  const oauthStatusWithCodex = useMemo(
    () => mergeCodexOauthStatus(oauthStatus, codexPool.accounts, codexPool.loadState, codexPool.activeNeedsReauth),
    [oauthStatus, codexPool.accounts, codexPool.loadState, codexPool.activeNeedsReauth],
  );

  const credentials = useProviderCredentials({
    apiBase,
    t: t as unknown as Parameters<typeof useProviderCredentials>[0]["t"],
    config,
    aliveRef,
    notify,
    refreshConfig: reloadConfig,
    refreshOauth: reloadOauth,
    invalidateQuotas,
    codexActiveNeedsReauth: codexPool.activeNeedsReauth,
  });

  const jsonEditor = useJsonConfigEditor({
    apiBase, config, notify, fetchConfig: reloadConfig,
    fetchProviderQuotas: async (refresh?: boolean) => { invalidateQuotas(refresh); },
    onSaved: () => dispatchChrome({ type: "bump-models" }),
    t: t as unknown as Parameters<typeof useJsonConfigEditor>[0]["t"],
  });

  const registry = {
    apiBase, t, notify, refreshConfig: reloadConfig, refreshOauth: reloadOauth,
    invalidateQuotas, refreshCodexAccount: () => codexPool.load(true),
  };

  const busyRef = useRef(chrome.busy);
  const hintRef = useRef(chrome.loginHint);
  useLayoutEffect(() => {
    busyRef.current = chrome.busy;
    hintRef.current = chrome.loginHint;
  }, [chrome.busy, chrome.loginHint]);

  const { cancelLoginOAuth, loginOAuth, logoutOAuth } = useProvidersOAuth({
    apiBase, t, aliveRef,
    accountSets: credentials.accountSets,
    setAccountSets: credentials.setAccountSets,
    setBusy: update => {
      const next = typeof update === "function" ? update(busyRef.current) : update;
      dispatchChrome({ type: "busy", name: next });
    },
    setStatus: update => {
      const text = typeof update === "function" ? update(notice.text) : update;
      dispatchNotice({ type: "show", text, ok: false });
    },
    setLoginInfo: update => {
      const next = typeof update === "function" ? update(hintRef.current) : update;
      dispatchChrome({ type: "hint", hint: next });
    },
    setOauthStatus, notify,
    fetchConfig: reloadConfig, fetchOauth: reloadOauth,
    fetchAccountSets: credentials.fetchAccountSets,
    fetchProviderQuotas: async (refresh?: boolean) => { invalidateQuotas(refresh); },
    bumpModelsRefresh: () => dispatchChrome({ type: "bump-models" }),
    onLoginSettled: name => dispatchChrome({ type: "reveal-accounts", name }),
  });

  const requestLoginOAuth = (provider: string, addAccount = false) => {
    if (chrome.busy === provider) return;
    if (oauthTosRisk(provider)) {
      dispatchChrome({ type: "tos", value: { provider, addAccount } });
      return;
    }
    void loginOAuth(provider, addAccount);
  };

  const statusToast = (
    <ProvidersNoticeBanner
      notice={notice.text ? {
        text: notice.text,
        tone: notice.tone,
        actionLabel: notice.harness ? t("prov.syncNow") : undefined,
        actionBusy: notice.syncBusy,
      } : null}
      closeLabel={t("common.close")}
      onDismiss={() => dispatchNotice({ type: "clear" })}
      onAction={notice.harness ? () => {
        if (notice.syncBusy) return;
        dispatchNotice({ type: "sync-busy", on: true });
        void syncEnabledHarnessModelLists(apiBase).then(result => {
          if (!aliveRef.current) return;
          notify(t(result.messageKey), result.ok);
        });
      } : undefined}
    />
  );

  if (!config) {
    return (
      <>
        {statusToast}
        <ProvidersWorkspaceBoot
          title={t("nav.providers")}
          status={configResource.error ? t("prov.loadConfigFail") : t("prov.loadingConfig")}
        />
      </>
    );
  }

  const addModalAccountRows = listAccountLoginTargets(config, oauthProviders).map(target => (
    target.channel === "codex"
      ? {
          id: target.providerId,
          label: accountLoginLabel(target, t),
          kind: "codex" as const,
          href: `#${OPENAI_ACCOUNT_ACCESS_HASH}` as const,
        }
      : {
          id: target.providerId,
          label: accountLoginLabel(target, t),
          kind: "oauth" as const,
        }
  ));
  const accountLoginStatus = mirrorForwardLoginStatus(config.providers, oauthStatusWithCodex);

  const onAccountLogin = async (provider: string, addAccount = false) => {
    await executeAccountLogin(
      planAccountLogin(config, provider, oauthProviders),
      provider,
      addAccount,
      {
        busy: chrome.busy,
        markBusy: name => dispatchChrome({ type: "busy", name }),
        ensureReserved: async state => {
          await ensureOpenAiProvider(apiBase, state);
          await reloadConfig();
        },
        startCodexLogin: () => dispatchChrome({ type: "codex-login", open: true }),
        startOauth: requestLoginOAuth,
        onInvalid: () => notify(t("codexAuth.openaiMissing"), false),
        onEnsureFailed: error => {
          if (error instanceof OpenAiEnableError) notify(t(error.i18nKey), false);
          else notify(error instanceof Error ? error.message : t("prov.saveFailed"), false);
        },
        stillMounted: () => aliveRef.current,
      },
    );
  };

  return (
    <>
      {statusToast}
      <ProviderWorkspaceShell
        onRemoveProvider={name => dispatchChrome({ type: "ask-remove", name })}
        providers={config.providers as Record<string, WorkspaceProvider>}
        apiBase={apiBase}
        defaultProvider={config.defaultProvider}
        selectedName={chrome.selected}
        onSelect={name => dispatchChrome({ type: "select", name })}
        onAddProvider={intent => dispatchChrome({ type: "open-add", intent: intent ?? null })}
        onReviewProvider={name => dispatchChrome({ type: "reveal-accounts", name })}
        onNotice={notify}
        jsonEditor={{
          visible: jsonEditor.jsonEditorOpen,
          text: jsonEditor.draft,
          dirty: jsonEditor.jsonIsDirty,
          setText: jsonEditor.setDraft,
          save: () => jsonEditor.saveConfig(),
          close: jsonEditor.requestCloseJsonEditor,
          restore: jsonEditor.restoreJsonEditor,
        }}
        jsonSaving={jsonEditor.jsonSaving}
        modelsRefreshToken={chrome.modelsEpoch}
        activeAccountNeedsReauth={credentials.activeAccountNeedsReauth}
        quotaRefreshEpoch={chrome.quotaEpoch}
        quotaForceRefresh={chrome.quotaForce}
        detail={(item, data) => {
          const loginStatus = accountLoginStatus[item.name] ?? oauthStatus[item.name];
          return (
            <ProviderDetails
              key={item.name}
              item={item}
              apiBase={apiBase}
              telemetry={{
                usageTotals: data.usageTotals,
                modelUsage: data.modelUsage,
                quotaReport: data.quotaReport,
                availableModels: data.availableModels,
                hasLiveModels: data.hasLiveModels,
                selectedModels: data.selectedModels,
                modelsLoading: data.modelsLoading,
                modelsLoadFailed: data.modelsLoadFailed,
                onRetryModels: data.onRetryModels,
                downstream: data.downstream,
                lastValidated: data.lastValidated,
                apiLane: data.apiLane,
                recentEvents: data.recentEvents,
              }}
              access={{
                oauthEmail: loginStatus?.email,
                oauth: loginStatus,
                accounts: credentials.accountSets[item.name]?.accounts ?? [],
                keys: credentials.keyPools[item.name === "openai" ? "openai-apikey" : item.name] ?? credentials.keyPools[item.name] ?? [],
                accountLoadState: credentials.accountLoadStates[item.name] ?? (item.authMode === "oauth" ? "idle" : "ready"),
                accountsFocusToken: chrome.accountsFocus.token,
                accountsFocusProvider: chrome.accountsFocus.provider,
                switchingAccountId: credentials.switchingAccount?.provider === item.name ? credentials.switchingAccount.accountId : null,
                busyProvider: chrome.busy,
                loginHint: chrome.loginHint,
                authHandlers: {
                  onLogin: requestLoginOAuth,
                  onCancelLogin: cancelLoginOAuth,
                  onLogout: logoutOAuth,
                  onReauth: (provider, accountId) => loginOAuth(provider, true, accountId),
                  onSwitchAccount: credentials.switchAccount,
                  onRemoveAccount: credentials.removeAccount,
                  onRetryAccounts: async provider => { await credentials.fetchAccountSets([provider]); },
                  onAddApiKey: credentials.addApiKeyValue,
                  onSwitchApiKey: credentials.switchApiKey,
                  onRemoveApiKey: credentials.removeApiKey,
                  onEditAlias: credentials.editCredentialAlias,
                },
                codexController: codexPool,
              }}
              lifecycle={{
                isDefault: item.name === config.defaultProvider,
                onRemoveProvider: name => dispatchChrome({ type: "ask-remove", name }),
                onSetDisabled: (name, disabled) => { void setRegistryProviderDisabled(registry, name, disabled); },
                onSetDefault: name => { void setRegistryDefaultProvider(registry, name); },
                onEditConfig: jsonEditor.openJsonEditor,
                onNotice: notify,
                onRefreshQuotas: () => invalidateQuotas(true),
                onUpdateProvider: (name, patch) => patchRegistryProvider(registry, name, patch),
              }}
              onBack={() => dispatchChrome({ type: "select", name: null })}
            />
          );
        }}
      />
      <ProviderOverlays
        add={chrome.addOpen ? {
          apiBase,
          existingNames: Object.keys(config.providers),
          intent: chrome.addIntent,
          accounts: {
            rows: addModalAccountRows,
            status: accountLoginStatus,
            busy: chrome.busy,
          },
          onClose: () => {
            if (chrome.busy) void cancelLoginOAuth(chrome.busy);
            dispatchChrome({ type: "close-add" });
          },
          onAdded: (name) => {
            dispatchChrome({ type: "close-add" });
            notify(t("prov.added", { name }), true, { offerHarnessSync: true });
            void reloadConfig();
            void reloadOauth();
            invalidateQuotas(true);
            dispatchChrome({ type: "bump-models" });
          },
          onLogin: onAccountLogin,
          onCancelLogin: (provider) => { void cancelLoginOAuth(provider); },
          onLogout: (provider) => { void logoutOAuth(provider); },
          onManage: name => dispatchChrome({ type: "reveal-accounts", name }),
          onReopen: reloadOauth,
        } : null}
        codex={chrome.codexLogin ? {
          apiBase,
          onClose: () => dispatchChrome({ type: "codex-login", open: false }),
          onAdded: (completion: CodexAccountMutationCompletion) => {
            dispatchChrome({ type: "codex-login", open: false });
            if (completion.catalogRefreshPending) {
              dispatchNotice({ type: "show", text: t("codexAuth.catalogRefreshPending"), ok: false, tone: "warn" });
            } else {
              notify(t("codexAuth.accountAdded"), true);
            }
            void reloadConfig();
            void reloadOauth();
            invalidateQuotas(true);
            dispatchChrome({ type: "bump-models" });
          },
        } : null}
        remove={chrome.removeName ? {
          name: chrome.removeName,
          defaultName: successorDefaultName(config, chrome.removeName),
          onCancel: () => dispatchChrome({ type: "close-remove" }),
          onConfirm: () => {
            if (!chrome.removeName || removeBusyRef.current) return;
            const name = chrome.removeName;
            removeBusyRef.current = true;
            dispatchChrome({ type: "close-remove" });
            void confirmRemoveNamedProvider(registry, name).then(result => {
              if (result.removed && chrome.selected === name) dispatchChrome({ type: "select", name: null });
            }).finally(() => { removeBusyRef.current = false; });
          },
        } : null}
        unsaved={jsonEditor.jsonLeaveOpen ? {
          saving: jsonEditor.jsonSaving,
          onCancel: () => { if (!jsonEditor.jsonSaving) jsonEditor.setJsonLeaveOpen(false); },
          onDiscard: jsonEditor.discardJsonEditor,
          onSave: () => { void jsonEditor.saveConfig(); },
        } : null}
        tos={chrome.tos ? {
          provider: chrome.tos.provider,
          addAccount: chrome.tos.addAccount,
          onCancel: () => dispatchChrome({ type: "tos", value: null }),
          onContinue: () => {
            const pending = chrome.tos;
            if (!pending) return;
            dispatchChrome({ type: "tos", value: null });
            void loginOAuth(pending.provider, pending.addAccount);
          },
        } : null}
      />
    </>
  );
}
