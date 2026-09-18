/**
 * Right pane of the add-provider modal: selected provider header, real
 * connection methods, optional models/docs, and the honest next-step list.
 */
import { useT } from "../i18n/shared";
import { ProviderMark } from "./ProviderMark";
import { type CatalogPreset } from "./provider-catalog/provider-presets";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";
import { AddProviderFormPane } from "./add-provider-form-pane";
import type { AccountLoginRow, AccountLoginStatus } from "./provider-catalog/ProviderCatalog";
import { AccountSetup } from "./add-provider-account-setup";
import {
  AddProviderMethodRail,
  AddProviderSelectedAuth,
} from "./add-provider-setup-sections";
import { addProviderConnectionCapabilities } from "../lib/add-provider-form-policy";
import { catalogFamilyForPreset } from "./provider-catalog/catalog-families";
import { familyAccessLabel, familyConnectionLabel, presetIsLocal, presetTier, railSubtitle } from "./provider-catalog/catalog-policy";

const EMPTY_CATALOG: CatalogPreset[] = [];
const EMPTY_NAMES: string[] = [];
const NOOP = () => undefined;

export function AddProviderSetupPane({
  preset,
  form,
  endpointChoice,
  error,
  saving,
  dup,
  isCustom,
  isLocal,
  isReservedForward,
  presetDescription,
  oauthSupported,
  oauthBusy,
  oauthMsg,
  oauthMsgTone,
  oauthUrl,
  manualCode,
  manualCodeBusy,
  manualCodeMsg,
  manualCodeOk,
  selectedAccount,
  accountStatus,
  accountBusy,
  onFormChange,
  onEndpointChoiceChange,
  onRequestLogin,
  onCancelLogin,
  onManualCodeChange,
  onSubmitManualCode,
  onConnectKey,
  catalog = EMPTY_CATALOG,
  existingNames = EMPTY_NAMES,
  onSelectPreset,
  onAccountLogin,
  onAccountCancelLogin,
  onAccountLogout,
  onAccountManage,
}: {
  preset: CatalogPreset | null;
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
  manualCode: string;
  manualCodeBusy: boolean;
  manualCodeMsg: string;
  manualCodeOk: boolean;
  selectedAccount?: AccountLoginRow | null;
  accountStatus?: AccountLoginStatus;
  accountBusy?: boolean;
  onFormChange: (next: ProviderPayloadForm) => void;
  onEndpointChoiceChange: (choiceId: string) => void;
  onRequestLogin: (providerId: string) => void;
  onCancelLogin?: () => void;
  onManualCodeChange: (value: string) => void;
  onSubmitManualCode: (providerId: string) => void;
  onConnectKey: () => void;
  catalog?: CatalogPreset[];
  existingNames?: string[];
  onSelectPreset?: (preset: CatalogPreset) => void;
  onAccountLogin?: (provider: string, addAccount?: boolean) => void;
  onAccountCancelLogin?: (provider: string) => void;
  onAccountLogout?: (provider: string) => void;
  onAccountManage?: (provider: string) => void;
}) {
  const t = useT();

  if (selectedAccount) {
    return (
      <AccountSetup
        row={selectedAccount}
        status={accountStatus}
        busy={accountBusy === true}
        onLogin={onAccountLogin}
        onCancelLogin={onAccountCancelLogin}
        onLogout={onAccountLogout}
        onManage={onAccountManage}
      />
    );
  }

  if (!preset || !form) {
    return <p className="muted add-provider-empty add-provider-setup">{t("modal.catalogLoading")}</p>;
  }

  const family = catalogFamilyForPreset(preset.id, catalog);
  const familyMembers = family?.members ?? [preset];
  const title = family && family.members.length > 1 ? family.label : preset.label;
  const meta = family && family.members.length > 1
    ? `${familyAccessLabel(family, t)} · ${familyConnectionLabel(family, t)}`
    : railSubtitle({
      auth: preset.auth,
      local: presetIsLocal(preset),
      tier: presetTier(preset),
      id: preset.id,
      adapter: preset.adapter,
    }, t);
  const caps = addProviderConnectionCapabilities({
    auth: preset.auth,
    oauthProvider: preset.oauthProvider,
    oauthSupported,
    keyOnOauth: familyMembers.length <= 1,
  });

  const setup = (
    <div className="add-provider-setup">
      <header className="add-provider-setup-head">
        <ProviderMark name={preset.id} adapter={preset.adapter} baseUrl={preset.baseUrl} className="add-provider-setup-icon" />
        <div>
          <div className="add-provider-setup-title-row">
            <h3>{title}</h3>
          </div>
          <p className="muted">{meta}</p>
        </div>
      </header>

      {isCustom ? (
        <AddProviderFormPane
          preset={preset}
          presetDescription={presetDescription}
          embedded
          state={{
            draft: form,
            endpointChoice,
            error,
            saving,
            dup,
            isCustom,
            isLocal,
            isReservedForward,
          }}
          handlers={{
            write: onFormChange,
            chooseEndpoint: onEndpointChoiceChange,
            submit: NOOP,
            useOauthLogin: NOOP,
            back: NOOP,
          }}
        />
      ) : (
        <AddProviderSelectedAuth
          preset={preset}
          form={form}
          oauthBusy={oauthBusy}
          oauthReady={caps.oauthReady}
          oauthId={caps.oauthId}
          oauthMsg={oauthMsg}
          oauthMsgTone={oauthMsgTone}
          oauthUrl={oauthUrl}
          manualCode={manualCode}
          manualCodeBusy={manualCodeBusy}
          manualCodeMsg={manualCodeMsg}
          manualCodeOk={manualCodeOk}
          saving={saving}
          error={error}
          dup={dup}
          endpointChoice={endpointChoice}
          onFormChange={onFormChange}
          onEndpointChoiceChange={onEndpointChoiceChange}
          onRequestLogin={onRequestLogin}
          onCancelLogin={onCancelLogin}
          onManualCodeChange={onManualCodeChange}
          onSubmitManualCode={onSubmitManualCode}
          onConnectKey={onConnectKey}
        />
      )}
    </div>
  );

  if (isCustom) return setup;
  return (
    <div className="add-provider-connection">
      <AddProviderMethodRail
        preset={preset}
        form={form}
        familyMembers={familyMembers}
        existingNames={existingNames}
        canOauth={caps.canOauth}
        canKey={caps.canKey}
        onFormChange={onFormChange}
        onSelectPreset={onSelectPreset ?? (() => undefined)}
      />
      {setup}
    </div>
  );
}
