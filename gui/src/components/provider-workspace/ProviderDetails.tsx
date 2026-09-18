/**
 * ProviderDetails — the detail header + tab shell. Owns tab state and composes
 * the Overview, Access, and Configuration owners; the header actions are listener
 * calls decoded by `provider-workspace/detail-actions`.
 */
import { useLayoutEffect, useMemo, useRef, useState } from "react";
import { useT } from "../../i18n/shared";
import { binProviderStatus, isFreeProvider, isLocalProvider, type WorkspaceItem } from "../../provider-workspace/catalog";
import { accessTabVisible, deriveAccessDescriptor, type ProviderOAuthLoginView } from "../../provider-workspace/auth";
import {
  detailStatusPill,
  detailStatusPillKind,
} from "../../provider-workspace/connection-test";
import {
  closeDetailsOverflow,
  closeOverflowDetails,
  detailAccessCredentialPresent,
  scopedAccountsFocusToken,
} from "../../provider-workspace/detail-tabs";
import { detailTabDomIds, detailTabEntries } from "../../provider-workspace/details-view";
import {
  probeProviderConnection,
  syncProviderModels,
  type DetailActionOutcome,
} from "../../provider-workspace/detail-actions";
import type { CodexAccountPoolController } from "../../hooks/useCodexAccountPool";
import type { ProviderQuotaReportView } from "../../provider-workspace/report";
import type { AccountLoadState } from "../../hooks/useProviderCredentials";
import type { LoginHint } from "../../pages/providers-chrome";
import type { ApiKeyRow, OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderPatchApplier } from "../../provider-workspace/provider-mutations";
import type { ProviderUsageTotals } from "../../provider-workspace/model-selection";
import type { ProviderAuthHandlers } from "./ProviderAccess";
import type { ShellUsageModelRow as ProviderModelUsageRow } from "../../provider-workspace/workspace-shell";
import type { WorkspaceEvent } from "./ProviderWorkspaceShell";
import type { WorkspaceProvider } from "../../provider-workspace/catalog";
import { DetailHeader } from "./details-header";
import { DetailTabList } from "./details-tab-list";
import { DetailsTabPanels } from "./details-tab-panels";
import { DetailsLeaveDialogs } from "./details-leave-dialogs";
import { useDetailTabs } from "./use-detail-tabs";

/** Model-catalogue telemetry the shell read for the selected provider. */
export interface ProviderDetailsModels {
  availableModels: string[];
  selectedModels: string[];
  hasLiveModels: boolean;
  modelsLoading?: boolean;
  modelsLoadFailed?: boolean;
  onRetryModels?: () => void;
}

/** Usage, quota, and fleet-activity telemetry for the selected provider. */
export interface ProviderDetailsActivity {
  usageTotals?: ProviderUsageTotals;
  modelUsage?: ProviderModelUsageRow[];
  quotaReport?: ProviderQuotaReportView;
  lastValidated?: number | null;
  recentEvents?: WorkspaceEvent[];
  downstream?: { harnesses: number | null; routes: number; subagents: number };
}

export type ProviderDetailsTelemetry = ProviderDetailsModels & ProviderDetailsActivity & {
  apiLane?: WorkspaceProvider;
};

/** Credential rows the Access pane renders and the lane it has selected. */
export interface ProviderDetailsCredentialRows {
  oauthEmail?: string;
  oauth?: ProviderOAuthLoginView;
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
}

/** Login activity and credential mutations the Access pane issues. */
export interface ProviderDetailsCredentialActions {
  authHandlers?: ProviderAuthHandlers;
  busyProvider?: string | null;
  loginHint?: LoginHint | null;
}

export type ProviderDetailsAccess = ProviderDetailsCredentialRows & ProviderDetailsCredentialActions & {
  accountsFocusToken?: number;
  accountsFocusProvider?: string | null;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
  codexController?: CodexAccountPoolController;
};

export type ProviderDetailsLifecycle = {
  isDefault?: boolean;
  onRemoveProvider?: (name: string) => void;
  onSetDisabled?: (name: string, disabled: boolean) => void;
  onSetDefault?: (name: string) => void;
  onEditConfig?: () => void;
  onNotice?: (message: string, ok?: boolean) => void;
  onRefreshQuotas?: () => void;
  onUpdateProvider?: ProviderPatchApplier;
};

type HeaderAction = "validate" | "sync";

export default function ProviderDetails({
  item,
  apiBase,
  telemetry,
  access,
  lifecycle,
  onBack,
}: {
  item: WorkspaceItem;
  apiBase: string;
  telemetry: ProviderDetailsTelemetry;
  access: ProviderDetailsAccess;
  lifecycle: ProviderDetailsLifecycle;
  onBack?: () => void;
}) {
  const {
    quotaReport,
    availableModels,
    selectedModels,
    modelsLoading = false,
    onRetryModels,
    downstream,
    lastValidated,
    apiLane,
    recentEvents = [],
  } = telemetry;
  const {
    oauthEmail,
    oauth,
    accounts,
    accountsFocusToken = 0,
    accountsFocusProvider = null,
    keys,
    accountLoadState,
    switchingAccountId,
    busyProvider,
    loginHint,
    authHandlers,
    onCodexActiveNeedsReauthChange,
    codexController,
  } = access;
  const {
    isDefault,
    onRemoveProvider,
    onSetDisabled,
    onEditConfig,
    onNotice,
    onRefreshQuotas,
    onUpdateProvider,
  } = lifecycle;
  const t = useT();
  const boardStatus = binProviderStatus(item);
  const [menuBusy, setMenuBusy] = useState<HeaderAction | null>(null);
  const isDisabled = item.disabled === true;
  const free = useMemo(() => isFreeProvider(item), [item]);
  const local = useMemo(() => isLocalProvider(item), [item]);
  const showAccess = accessTabVisible(deriveAccessDescriptor(item, detailAccessCredentialPresent(apiLane, keys)));
  const tabs = useMemo(() => detailTabEntries(showAccess, t), [t, showAccess]);
  const {
    tab,
    setTab,
    pendingTab,
    pendingBack,
    leaveSaving,
    configEpoch,
    setConfigDirty,
    setPendingTab,
    setPendingBack,
    registerConfigSave,
    switchTab,
    requestBack,
    finishLeave,
    discardAndSwitch,
    discardAndLeave,
  } = useDetailTabs(showAccess, scopedAccountsFocusToken(accountsFocusProvider, item.name, accountsFocusToken), onBack);
  const overflowRef = useRef<HTMLDetailsElement>(null);
  const onSwitchTab = (next: typeof tab) => {
    closeOverflowDetails(overflowRef.current);
    switchTab(next);
  };

  useLayoutEffect(() => {
    closeOverflowDetails(overflowRef.current);
  }, [tab]);

  const statusPill = detailStatusPill(detailStatusPillKind(boardStatus), {
    connected: t("pws.status.connected"),
    disabled: t("prov.disabledBadge"),
    attention: t("pws.status.needsAttention"),
  });

  /**
   * Both header actions share the same busy latch and notice path: the decoded
   * outcome decides the toast tone and which reads are invalidated afterwards.
   */
  const runHeaderAction = (
    event: React.MouseEvent<HTMLElement>,
    kind: HeaderAction,
    run: () => Promise<DetailActionOutcome>,
  ) => {
    closeDetailsOverflow(event);
    if (menuBusy) return;
    setMenuBusy(kind);
    void run()
      .then(outcome => {
        if (outcome.refreshModels) onRetryModels?.();
        onNotice?.(outcome.message, outcome.ok);
        if (outcome.refreshQuotas) onRefreshQuotas?.();
      })
      .finally(() => setMenuBusy(null));
  };

  const panelIds = detailTabDomIds(tab);

  return (
    <div className="pws-detail" data-tab={tab}>
      <DetailHeader
        item={item}
        statusClass={statusPill.cls}
        statusLabel={statusPill.label}
        local={local}
        free={free}
        onBack={onBack ? requestBack : undefined}
        overflowRef={overflowRef}
        menuBusy={menuBusy}
        modelsLoading={modelsLoading}
        isDefault={isDefault}
        isDisabled={isDisabled}
        onValidate={event => { runHeaderAction(event, "validate", () => probeProviderConnection(apiBase, item.name, t)); }}
        onSyncModels={event => { runHeaderAction(event, "sync", () => syncProviderModels(apiBase, item.name, t)); }}
        onToggleDisabled={onSetDisabled ? event => { closeDetailsOverflow(event); onSetDisabled(item.name, !isDisabled); } : undefined}
        onRemove={onRemoveProvider ? event => { closeDetailsOverflow(event); onRemoveProvider(item.name); } : undefined}
        onEditConfig={onEditConfig ? event => { closeDetailsOverflow(event); onEditConfig(); } : undefined}
      />
      <DetailTabList tabs={tabs} tab={tab} onSwitch={onSwitchTab} />
      <div
        className="pws-detail-panel"
        role="tabpanel"
        id={panelIds.panelId}
        aria-labelledby={panelIds.tabId}
        tabIndex={0}
      >
        <DetailsTabPanels
          tab={tab}
          item={item}
          quotaReport={quotaReport}
          availableModels={availableModels}
          selectedModels={selectedModels}
          downstream={downstream}
          lastValidated={lastValidated}
          oauthEmail={oauthEmail}
          apiBase={apiBase}
          oauth={oauth}
          accounts={accounts}
          keys={keys}
          accountLoadState={accountLoadState}
          switchingAccountId={switchingAccountId}
          busyProvider={busyProvider}
          loginHint={loginHint}
          authHandlers={authHandlers}
          onCodexActiveNeedsReauthChange={onCodexActiveNeedsReauthChange}
          codexController={codexController}
          showAccess={showAccess}
          switchTab={onSwitchTab}
          recentEvents={recentEvents}
          apiLane={apiLane}
          onUpdateProvider={onUpdateProvider}
          onNotice={onNotice}
          configEpoch={configEpoch}
          setConfigDirty={setConfigDirty}
          registerConfigSave={registerConfigSave}
        />
      </div>
      <DetailsLeaveDialogs
        pendingTab={pendingTab}
        pendingBack={pendingBack}
        leaveSaving={leaveSaving}
        onCancelTab={() => setPendingTab(null)}
        onDiscardTab={() => discardAndSwitch(pendingTab)}
        onSaveTab={() => {
          void finishLeave(() => {
            const next = pendingTab;
            setPendingTab(null);
            if (next) setTab(next);
          }, () => setPendingTab(null));
        }}
        onCancelBack={() => setPendingBack(false)}
        onDiscardBack={discardAndLeave}
        onSaveBack={() => {
          void finishLeave(() => {
            setPendingBack(false);
            onBack?.();
          }, () => setPendingBack(false));
        }}
      />
    </div>
  );
}
