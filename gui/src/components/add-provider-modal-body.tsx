/**
 * Body of the Add Provider modal: the provider picker, the connection setup for
 * the selected row, and the Terms-of-service gate that guards an OAuth start.
 *
 * The body owns no state. It renders whichever of the two panes the wizard step
 * asks for and forwards the session it was handed.
 */
import type { Dispatch, SetStateAction } from "react";
import OAuthTosWarningModal from "./OAuthTosWarningModal";
import ProviderCatalog from "./provider-catalog/ProviderCatalog";
import type { AccountLoginRow, AccountLoginStatus } from "./provider-catalog/ProviderCatalog";
import type { CatalogPreset } from "./provider-catalog/provider-presets";
import { AddProviderSetupPane } from "./add-provider-setup-pane";
import type { AddProviderCommand } from "./add-provider-session";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";

/** Which pane the current wizard step shows. */
export type AddProviderPane = "catalog" | "setup" | "both";

export type AddProviderCatalogBodyProps = {
  pane?: AddProviderPane;
  presets: CatalogPreset[];
  presetsLoading: boolean;
  initialTier?: "accounts" | "free" | "paid";
  preset: CatalogPreset | null;
  existingNames: string[];
  accountRows?: AccountLoginRow[];
  accountStatus?: Record<string, AccountLoginStatus>;
  selectedAccount: AccountLoginRow | null;
  form: ProviderPayloadForm | null;
  endpointChoice: string;
  error: string;
  saving: boolean;
  dup: boolean;
  isCustom: boolean;
  isLocal: boolean;
  isReservedForward: boolean;
  presetDescription: (candidate: CatalogPreset) => string | undefined;
  oauthSupported: string[];
  oauthBusy: boolean;
  oauthMsg: string;
  oauthMsgTone: "ok" | "warn";
  oauthUrl: string;
  oauthUrlProvider: string | null;
  manualCode: string;
  manualCodeBusy: boolean;
  manualCodeMsg: string;
  manualCodeOk: boolean;
  accountBusy?: string | null;
  onChoosePreset: (preset: CatalogPreset) => void;
  onSelectCustom: () => void;
  dispatch: Dispatch<AddProviderCommand>;
  setSelectedAccount: Dispatch<SetStateAction<AccountLoginRow | null>>;
  onRequestLogin: (providerId: string) => void;
  onCancelLogin?: () => void;
  onSubmitManualCode: (providerId: string) => void;
  onConnectKey: () => void;
  onAccountLogin?: (provider: string, addAccount?: boolean) => void;
  onAccountCancelLogin?: (provider: string) => void;
  onAccountLogout?: (provider: string) => void;
  onAccountManage?: (provider: string) => void;
};

type AddProviderBodyHandlers = Pick<
  AddProviderCatalogBodyProps,
  | "onChoosePreset"
  | "onSelectCustom"
  | "dispatch"
  | "setSelectedAccount"
  | "onRequestLogin"
  | "onCancelLogin"
  | "onSubmitManualCode"
  | "onConnectKey"
  | "onAccountLogin"
  | "onAccountCancelLogin"
  | "onAccountLogout"
  | "onAccountManage"
>;

/** Access switch inside the picker: Accounts pins the first row, anything else clears it. */
function selectCatalogAccess(
  next: string,
  accountRows: AccountLoginRow[] | undefined,
  selectedAccount: AccountLoginRow | null,
  handlers: Pick<AddProviderBodyHandlers, "dispatch" | "setSelectedAccount">,
) {
  if (next === "accounts") {
    handlers.dispatch({ op: "returnToCatalog" });
    handlers.setSelectedAccount(accountRows?.[0] ?? null);
    return;
  }
  if (selectedAccount) handlers.setSelectedAccount(null);
}

function AddProviderCatalogPane(props: AddProviderCatalogBodyProps) {
  const { dispatch, setSelectedAccount, accountRows, selectedAccount } = props;
  return (
    <ProviderCatalog
      presets={props.presets}
      presetsLoading={props.presetsLoading}
      initialTier={props.initialTier}
      selectedId={props.preset?.id}
      existingNames={props.existingNames}
      onSelectPreset={props.onChoosePreset}
      onSelectCustom={props.onSelectCustom}
      accountRows={accountRows}
      accountStatus={props.accountStatus}
      selectedAccountId={selectedAccount?.id}
      onSelectAccount={row => {
        dispatch({ op: "returnToCatalog" });
        setSelectedAccount(row);
      }}
      onAccessChange={next => selectCatalogAccess(next, accountRows, selectedAccount, { dispatch, setSelectedAccount })}
    />
  );
}

function AddProviderSetupStage(props: AddProviderCatalogBodyProps) {
  const { selectedAccount, accountStatus, accountBusy } = props;
  return (
    <AddProviderSetupPane
      preset={props.preset}
      form={props.form}
      endpointChoice={props.endpointChoice}
      error={props.error}
      saving={props.saving}
      dup={props.dup}
      isCustom={props.isCustom}
      isLocal={props.isLocal}
      isReservedForward={props.isReservedForward}
      presetDescription={props.presetDescription}
      oauthSupported={props.oauthSupported}
      oauthBusy={props.oauthBusy}
      oauthMsg={props.oauthMsg}
      oauthMsgTone={props.oauthMsgTone}
      oauthUrl={props.oauthUrlProvider === props.preset?.oauthProvider ? props.oauthUrl : ""}
      manualCode={props.manualCode}
      manualCodeBusy={props.manualCodeBusy}
      manualCodeMsg={props.manualCodeMsg}
      manualCodeOk={props.manualCodeOk}
      selectedAccount={selectedAccount}
      accountStatus={selectedAccount ? accountStatus?.[selectedAccount.id] : undefined}
      accountBusy={selectedAccount ? accountBusy === selectedAccount.id : false}
      onFormChange={next => props.dispatch({ op: "updateDraft", form: next })}
      onEndpointChoiceChange={choice => props.dispatch({ op: "chooseEndpoint", choice })}
      onRequestLogin={props.onRequestLogin}
      onCancelLogin={props.onCancelLogin}
      onManualCodeChange={code => props.dispatch({ op: "updateManualCode", code })}
      onSubmitManualCode={props.onSubmitManualCode}
      onConnectKey={props.onConnectKey}
      catalog={props.presets}
      existingNames={props.existingNames}
      onSelectPreset={props.onChoosePreset}
      onAccountLogin={props.onAccountLogin}
      onAccountCancelLogin={props.onAccountCancelLogin}
      onAccountLogout={props.onAccountLogout}
      onAccountManage={props.onAccountManage}
    />
  );
}

export function AddProviderCatalogBody(props: AddProviderCatalogBodyProps) {
  if (props.pane === "catalog") {
    return <div className="add-provider-body is-catalog"><AddProviderCatalogPane {...props} /></div>;
  }
  if (props.pane === "setup") {
    return <div className="add-provider-body is-setup"><AddProviderSetupStage {...props} /></div>;
  }
  return (
    <div className="add-provider-body">
      <AddProviderCatalogPane {...props} />
      <AddProviderSetupStage {...props} />
    </div>
  );
}

export function AddProviderTosOverlay({
  oauthTosPending,
  presetLabel,
  dispatch,
  onContinue,
}: {
  oauthTosPending: string | null;
  presetLabel?: string;
  dispatch: Dispatch<AddProviderCommand>;
  onContinue: (providerId: string) => void;
}) {
  if (!oauthTosPending) return null;
  return (
    <OAuthTosWarningModal
      key={oauthTosPending}
      providerId={oauthTosPending}
      providerLabel={presetLabel ?? oauthTosPending}
      onCancel={() => dispatch({ op: "resolveTos", providerId: null })}
      onContinue={() => {
        const id = oauthTosPending;
        if (!id) return;
        dispatch({ op: "resolveTos", providerId: null });
        onContinue(id);
      }}
    />
  );
}
