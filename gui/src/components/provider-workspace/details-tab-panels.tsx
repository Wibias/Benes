import ProviderOverview from "./ProviderOverview";
import ProviderAccess from "./ProviderAccess";
import ProviderConfiguration from "./ProviderConfiguration";
import type { CodexAccountPoolController } from "../../hooks/useCodexAccountPool";
import type { ProviderQuotaReportView } from "../../provider-workspace/report";
import type { WorkspaceItem, WorkspaceProvider } from "../../provider-workspace/catalog";
import type { AccountLoadState } from "../../hooks/useProviderCredentials";
import type { LoginHint } from "../../pages/providers-chrome";
import type { ApiKeyRow, OAuthAccountRow } from "../../provider-workspace/provider-credential-api";
import type { ProviderOAuthLoginView } from "../../provider-workspace/auth";
import type { ProviderAuthHandlers } from "./ProviderAccess";
import type { ProviderPatchApplier } from "../../provider-workspace/provider-mutations";
import type { DetailConfigSaveRegistrar } from "../../provider-workspace/detail-tabs";
import type { ProviderWorkspaceEvent } from "../../provider-workspace/workspace";
import { overviewReauthAccountId } from "../../provider-workspace/connection-test";

export type DetailsTab = "overview" | "access" | "configuration";

/** Provider + fleet data the detail panels read for the selected row. */
interface DetailPanelData {
  item: WorkspaceItem;
  apiBase: string;
  availableModels: string[];
  selectedModels: string[];
  quotaReport?: ProviderQuotaReportView;
  downstream?: { harnesses: number | null; routes: number; subagents: number };
  lastValidated?: number | null;
  recentEvents?: ProviderWorkspaceEvent[];
  apiLane?: WorkspaceProvider;
}

/** Credential rows the Access surfaces render. */
interface DetailPanelCredentials {
  oauthEmail?: string;
  oauth?: ProviderOAuthLoginView;
  accounts?: OAuthAccountRow[];
  keys?: ApiKeyRow[];
  accountLoadState?: AccountLoadState;
  switchingAccountId?: string | null;
}

/** Login activity currently in flight for this provider. */
interface DetailPanelActivity {
  busyProvider?: string | null;
  loginHint?: LoginHint | null;
}

/** Credential and account-pool bindings the Access pane wires up. */
interface DetailPanelBindings {
  authHandlers?: ProviderAuthHandlers;
  codexController?: CodexAccountPoolController;
  onCodexActiveNeedsReauthChange?: (needs: boolean) => void;
}

/** Mutations and tab-state callbacks the panels issue. */
interface DetailPanelActions {
  onUpdateProvider?: ProviderPatchApplier;
  onNotice?: (message: string, ok?: boolean) => void;
  switchTab: (next: DetailsTab) => void;
  setConfigDirty: (dirty: boolean) => void;
  registerConfigSave: DetailConfigSaveRegistrar;
}

/** One detail view model, assembled from the shell's props. */
export interface DetailsTabPanelsProps extends DetailPanelData, DetailPanelCredentials, DetailPanelActivity, DetailPanelBindings, DetailPanelActions {
  tab: DetailsTab;
  showAccess: boolean;
  configEpoch: number;
}

/**
 * Cancel affordance for a login that is still waiting on the browser; absent when
 * the caller has no cancel handle to offer.
 */
function cancelLoginAction(model: DetailsTabPanelsProps): (() => void) | undefined {
  const cancel = model.authHandlers?.onCancelLogin;
  if (!cancel) return undefined;
  const name = model.item.name;
  return () => { void cancel(name); };
}

/**
 * Reauth affordance for a provider whose active credential is flagged: an OAuth
 * provider reauths that account in place, anything else defers to the Access tab.
 */
function reauthAction(model: DetailsTabPanelsProps): (() => void) | undefined {
  if (!model.item.activeNeedsReauth) return undefined;
  const name = model.item.name;
  if (model.item.authMode === "oauth") {
    const accounts = model.accounts ?? [];
    const reauth = model.authHandlers?.onReauth;
    return () => { void reauth?.(name, overviewReauthAccountId(accounts)); };
  }
  return () => model.switchTab("access");
}

export function DetailsTabPanels(model: DetailsTabPanelsProps) {
  if (model.tab === "overview") return <OverviewPanel model={model} />;
  if (model.tab === "access") return <AccessPanel model={model} />;
  return <ConfigurationPanel model={model} />;
}

function OverviewPanel({ model }: { model: DetailsTabPanelsProps }) {
  return (
    <ProviderOverview
      item={model.item}
      quotaReport={model.quotaReport}
      oauthEmail={model.oauthEmail}
      oauth={model.oauth}
      accounts={model.accounts}
      keys={model.keys}
      availableModels={model.availableModels}
      selectedModels={model.selectedModels}
      downstream={model.downstream}
      lastValidated={model.lastValidated}
      onManageAccess={model.showAccess ? () => model.switchTab("access") : undefined}
      recentEvents={model.recentEvents}
      apiLanePresent={Boolean(model.apiLane) || Boolean(model.keys?.length)}
      reauthBusy={model.busyProvider === model.item.name}
      onCancelLogin={cancelLoginAction(model)}
      onReauthenticate={reauthAction(model)}
    />
  );
}

function AccessPanel({ model }: { model: DetailsTabPanelsProps }) {
  return (
    <ProviderAccess
      item={model.item}
      apiBase={model.apiBase}
      oauth={model.oauth}
      accounts={model.accounts}
      keys={model.keys}
      accountLoadState={model.accountLoadState}
      switchingAccountId={model.switchingAccountId}
      busy={model.busyProvider === model.item.name}
      loginHint={model.loginHint}
      authHandlers={model.authHandlers}
      onCodexActiveNeedsReauthChange={model.onCodexActiveNeedsReauthChange}
      codexController={model.codexController}
      apiLane={model.apiLane ? { name: "openai-apikey", ...model.apiLane } : undefined}
      recentEvents={model.recentEvents}
      onUpdateProvider={model.onUpdateProvider}
    />
  );
}

function ConfigurationPanel({ model }: { model: DetailsTabPanelsProps }) {
  return (
    <ProviderConfiguration
      key={`${model.item.name}:${model.configEpoch}`}
      item={model.item}
      availableModels={model.availableModels}
      onUpdateProvider={model.onUpdateProvider}
      onNotice={model.onNotice}
      onDirtyChange={model.setConfigDirty}
      onRegisterSave={model.registerConfigSave}
      apiLane={model.apiLane}
      apiBase={model.apiBase}
    />
  );
}
