/** Connection, models, and next-step sections of the add-provider setup pane. */
import { useState } from "react";
import { useT } from "../i18n/shared";
import { IconChevron, IconEye, IconEyeOff, IconGlobe, IconKey, IconLock } from "../icons";
import { ProviderMark } from "./ProviderMark";
import { LoginUrlBlock } from "./login-url-block";
import { presetModelIds, modelPreviewLabels, setupDocsUrl, type CatalogPreset } from "./provider-catalog/provider-presets";
import { familyMethodTitle } from "./provider-catalog/catalog-policy";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";
import { baseUrlForChoice } from "../base-url-choice";
import {
  addProviderConnectionCapabilities,
  addProviderEndpointLabelKey,
  addProviderKeyConnectEnabled,
  addProviderShowsEndpointChoices,
} from "../lib/add-provider-form-policy";

export function AddProviderOauthMethod({
  preset,
  form,
  oauthReady,
  onFormChange,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  oauthBusy?: boolean;
  oauthReady: boolean;
  oauthId?: string;
  onFormChange: (next: ProviderPayloadForm) => void;
  onRequestLogin?: (providerId: string) => void;
}) {
  const t = useT();
  return (
    <>
      <div className={`add-provider-method${form.authMode === "oauth" ? " is-on" : ""}`}>
        <label className="add-provider-method-pick">
          <input
            type="radio"
            name="add-provider-method"
            checked={form.authMode === "oauth"}
            onChange={() => onFormChange({ ...form, authMode: "oauth" })}
          />
          <span>
            <strong>{t("modal.connections.oauth")}</strong>
            <span className="muted">{t("modal.connections.oauthHint", { label: preset.label })}</span>
          </span>
        </label>
      </div>
      {!oauthReady && (
        <p className="add-provider-warn">{t("modal.oauthComingSoon", { label: preset.label })}</p>
      )}
    </>
  );
}

export function AddProviderKeyMethod({
  preset,
  form,
  saving,
  onFormChange,
  onConnectKey,
  hidePicker = false,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  saving: boolean;
  onFormChange: (next: ProviderPayloadForm) => void;
  onConnectKey: () => void;
  hidePicker?: boolean;
}) {
  const t = useT();
  const [showKey, setShowKey] = useState(false);
  return (
    <>
      {!hidePicker && (
      <div className={`add-provider-method${form.authMode === "key" ? " is-on" : ""}`}>
        <label className="add-provider-method-pick">
          <input
            type="radio"
            name="add-provider-method"
            checked={form.authMode === "key"}
            onChange={() => onFormChange({ ...form, authMode: "key" })}
          />
          <span>
            <strong>{t("modal.badge.apiKey")}</strong>
            <span className="muted">{t("modal.connections.apiKeyHint", { label: preset.label })}</span>
          </span>
        </label>
      </div>
      )}
      {!preset.keyOptional && (
        <div className="add-provider-key-row">
          <div className="add-provider-key-field">
            <input
              className="input"
              type={showKey ? "text" : "password"}
              value={form.apiKey}
              onChange={e => onFormChange({ ...form, authMode: "key", apiKey: e.target.value })}
              placeholder={t("modal.apiKey")}
            />
            <button
              type="button"
              className="btn btn-ghost btn-icon"
              aria-label={showKey ? t("modal.hideApiKey") : t("modal.showApiKey")}
              onClick={() => setShowKey(v => !v)}
            >
              {showKey ? <IconEyeOff /> : <IconEye />}
            </button>
          </div>
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={!addProviderKeyConnectEnabled({
              authMode: form.authMode,
              apiKey: form.apiKey,
              saving,
              keyOptional: preset.keyOptional,
            })}
            onClick={onConnectKey}
          >
            {saving ? t("modal.adding") : t("modal.connect")}
          </button>
        </div>
      )}
    </>
  );
}

export function AddProviderSetupEndpoint({
  preset,
  form,
  endpointChoice,
  onFormChange,
  onEndpointChoiceChange,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  endpointChoice: string;
  onFormChange: (next: ProviderPayloadForm) => void;
  onEndpointChoiceChange: (choiceId: string) => void;
}) {
  const t = useT();
  if (!addProviderShowsEndpointChoices(preset.baseUrlChoices)) return null;
  return (
    <label className="modal-field" style={{ display: "block", marginTop: 8 }}>
      <span className="field-label">{t("modal.endpoint")}</span>
      <select
        className="input"
        value={endpointChoice}
        onChange={e => {
          const id = e.target.value;
          onEndpointChoiceChange(id);
          onFormChange({
            ...form,
            baseUrl: baseUrlForChoice(preset.baseUrlChoices, id, form.baseUrl),
          });
        }}
      >
        {preset.baseUrlChoices!.map(c => {
          const key = addProviderEndpointLabelKey(c.id);
          return (
            <option key={c.id} value={c.id}>
              {key ? t(key) : c.label}
            </option>
          );
        })}
      </select>
    </label>
  );
}

export function AddProviderOauthBusyFields({
  oauthUrl,
  oauthId,
  oauthMsg,
  oauthMsgTone,
  manualCode,
  manualCodeBusy,
  manualCodeMsg,
  manualCodeOk,
  onManualCodeChange,
  onSubmitManualCode,
}: {
  oauthUrl: string;
  oauthId: string;
  oauthMsg: string;
  oauthMsgTone: "ok" | "warn";
  manualCode: string;
  manualCodeBusy: boolean;
  manualCodeMsg: string;
  manualCodeOk: boolean;
  onManualCodeChange: (value: string) => void;
  onSubmitManualCode: (providerId: string) => void;
}) {
  const t = useT();
  return (
    <>
      <LoginUrlBlock url={oauthUrl} />
      <div className="add-provider-manual">
        <p className="muted text-label">{t("prov.pasteRedirectHint")}</p>
        <div className="add-provider-key-row">
          <input
            className="input"
            value={manualCode}
            onChange={e => onManualCodeChange(e.target.value)}
            placeholder={t("prov.pasteRedirect")}
            disabled={manualCodeBusy}
          />
          <button
            type="button"
            className="btn btn-ghost"
            disabled={manualCodeBusy || !manualCode.trim() || !oauthId}
            onClick={() => oauthId && onSubmitManualCode(oauthId)}
          >
            {manualCodeBusy ? t("prov.pasteSubmitting") : t("prov.pasteSubmit")}
          </button>
        </div>
        {manualCodeMsg && (
          <p className={manualCodeOk ? "add-provider-ok" : "add-provider-warn"}>{manualCodeMsg}</p>
        )}
      </div>
      {oauthMsg && (
        <p className={oauthMsgTone === "warn" ? "add-provider-warn" : "muted"}>{oauthMsg}</p>
      )}
    </>
  );
}

export function AddProviderConnectionHints({
  preset,
  form,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
}) {
  const t = useT();
  if (form.authMode === "forward") {
    return <p className="muted">{t("modal.connections.forwardHint")}</p>;
  }
  if (form.authMode === "local") {
    return <p className="muted">{t("modal.connections.localHint")}</p>;
  }
  if (preset.keyOptional && form.authMode === "key") {
    return <p className="muted">{preset.note ?? t("modal.freeTierDefault")}</p>;
  }
  return null;
}

export function AddProviderConnectionMethods({
  preset,
  form,
  oauthSupported,
  oauthBusy,
  oauthMsg,
  oauthMsgTone,
  oauthUrl,
  manualCode,
  manualCodeBusy,
  manualCodeMsg,
  manualCodeOk,
  saving,
  error,
  dup,
  onFormChange,
  onEndpointChoiceChange,
  onRequestLogin,
  onManualCodeChange,
  onSubmitManualCode,
  onConnectKey,
  endpointChoice,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  oauthSupported: string[];
  oauthBusy: boolean;
  oauthMsg: string;
  oauthMsgTone: "ok" | "warn";
  oauthUrl: string;
  manualCode: string;
  manualCodeBusy: boolean;
  manualCodeMsg: string;
  manualCodeOk: boolean;
  saving: boolean;
  error: string;
  dup: boolean;
  endpointChoice: string;
  onFormChange: (next: ProviderPayloadForm) => void;
  onEndpointChoiceChange: (choiceId: string) => void;
  onRequestLogin: (providerId: string) => void;
  onManualCodeChange: (value: string) => void;
  onSubmitManualCode: (providerId: string) => void;
  onConnectKey: () => void;
}) {
  const t = useT();
  const { canOauth, canKey, oauthId, oauthReady } = addProviderConnectionCapabilities({
    auth: preset.auth,
    oauthProvider: preset.oauthProvider,
    oauthSupported,
  });
  return (
    <section className="add-provider-block">
      <h4>{t("modal.connections.title")}</h4>
      {canOauth && (
        <AddProviderOauthMethod
          preset={preset}
          form={form}
          oauthBusy={oauthBusy}
          oauthReady={oauthReady}
          oauthId={oauthId}
          onFormChange={onFormChange}
          onRequestLogin={onRequestLogin}
        />
      )}
      {canKey && (
        <AddProviderKeyMethod
          preset={preset}
          form={form}
          saving={saving}
          onFormChange={onFormChange}
          onConnectKey={onConnectKey}
        />
      )}
      <AddProviderSetupEndpoint
        preset={preset}
        form={form}
        endpointChoice={endpointChoice}
        onFormChange={onFormChange}
        onEndpointChoiceChange={onEndpointChoiceChange}
      />
      <AddProviderConnectionHints preset={preset} form={form} />
      {oauthBusy && (
        <AddProviderOauthBusyFields
          oauthUrl={oauthUrl}
          oauthId={oauthId}
          oauthMsg={oauthMsg}
          oauthMsgTone={oauthMsgTone}
          manualCode={manualCode}
          manualCodeBusy={manualCodeBusy}
          manualCodeMsg={manualCodeMsg}
          manualCodeOk={manualCodeOk}
          onManualCodeChange={onManualCodeChange}
          onSubmitManualCode={onSubmitManualCode}
        />
      )}
      {!oauthBusy && oauthMsg && (
        <p className={oauthMsgTone === "warn" ? "add-provider-warn" : "muted"}>{oauthMsg}</p>
      )}
      {error && <p className="add-provider-error" role="alert">{error}</p>}
      {dup && <p className="add-provider-warn">{t("modal.duplicateWarn", { name: form.name.trim() })}</p>}
    </section>
  );
}

export function AddProviderSetupModels({
  preset,
  catalog,
}: {
  preset: CatalogPreset;
  catalog: CatalogPreset[];
}) {
  const t = useT();
  const modelLabels = modelPreviewLabels(presetModelIds(preset, catalog));
  const docsUrl = setupDocsUrl(preset, catalog);
  if (modelLabels.length > 0) {
    return (
      <section className="add-provider-block add-provider-models">
        <h4>{t("modal.modelsAfter")}</h4>
        <p>{modelLabels.join(" • ")}</p>
        {docsUrl && (
          <a className="providers-link add-provider-docs" href={docsUrl} target="_blank" rel="noreferrer">
            {t("modal.viewDocs")}
            <IconChevron />
          </a>
        )}
      </section>
    );
  }
  if (docsUrl) {
    return (
      <a className="providers-link add-provider-docs" href={docsUrl} target="_blank" rel="noreferrer">
        {t("modal.viewDocs")}
        <IconChevron />
      </a>
    );
  }
  return null;
}

export function AddProviderMethodRail({
  preset,
  form,
  familyMembers,
  existingNames,
  canOauth,
  canKey,
  onFormChange,
  onSelectPreset,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  familyMembers: CatalogPreset[];
  existingNames: string[];
  canOauth: boolean;
  canKey: boolean;
  onFormChange: (next: ProviderPayloadForm) => void;
  onSelectPreset: (preset: CatalogPreset) => void;
}) {
  const t = useT();
  const family = { id: familyMembers[0]?.id ?? preset.id, label: preset.label, members: familyMembers };
  if (familyMembers.length > 1) {
    const configured = new Set(existingNames.map(name => name.toLowerCase()));
    return (
      <aside className="add-provider-method-rail">
        <h4>{t("modal.connections.method")}</h4>
        <p className="muted">{t("modal.connections.choose")}</p>
        {familyMembers.map(member => {
          const selected = member.id === preset.id;
          return (
            <label key={member.id} className="add-provider-method-option">
              <input
                type="radio"
                name="add-provider-method"
                checked={selected}
                onChange={() => {
                  if (!selected) onSelectPreset(member);
                }}
              />
              <ProviderMark name={member.id} adapter={member.adapter} baseUrl={member.baseUrl} className="add-provider-method-icon" />
              <span>
                <strong>{familyMethodTitle(member, family, t)}</strong>
                {familyMethodTitle(member, family, t) !== member.label && (
                  <span className="muted">{member.label}</span>
                )}
                {configured.has(member.id.toLowerCase()) && (
                  <span className="muted">{t("modal.connections.configured")}</span>
                )}
              </span>
            </label>
          );
        })}
      </aside>
    );
  }
  return (
    <aside className="add-provider-method-rail">
      <h4>{t("modal.connections.method")}</h4>
      <p className="muted">{t("modal.connections.choose")}</p>
      {canOauth && (
        <label className="add-provider-method-option">
          <input
            type="radio"
            name="add-provider-method"
            checked={form.authMode === "oauth"}
            onChange={() => onFormChange({ ...form, authMode: "oauth" })}
          />
          <ProviderMark name={preset.id} adapter={preset.adapter} baseUrl={preset.baseUrl} className="add-provider-method-icon" />
          <span>
            <strong>{t("modal.connections.oauth")}</strong>
            <span className="muted">{t("modal.connections.oauthHint", { label: preset.label })}</span>
          </span>
        </label>
      )}
      {canKey && (
        <label className="add-provider-method-option">
          <input
            type="radio"
            name="add-provider-method"
            checked={form.authMode === "key"}
            onChange={() => onFormChange({ ...form, authMode: "key" })}
          />
          <span className="add-provider-method-icon" aria-hidden="true"><IconKey /></span>
          <span>
            <strong>{t("modal.badge.apiKey")}</strong>
            <span className="muted">{t("modal.connections.apiKeyHint", { label: preset.label })}</span>
          </span>
        </label>
      )}
    </aside>
  );
}

export function AddProviderSelectedAuth({
  preset,
  form,
  oauthBusy,
  oauthReady,
  oauthId,
  oauthMsg,
  oauthMsgTone,
  oauthUrl,
  manualCode,
  manualCodeBusy,
  manualCodeMsg,
  manualCodeOk,
  saving,
  error,
  dup,
  endpointChoice,
  onFormChange,
  onEndpointChoiceChange,
  onRequestLogin,
  onCancelLogin,
  onManualCodeChange,
  onSubmitManualCode,
  onConnectKey,
}: {
  preset: CatalogPreset;
  form: ProviderPayloadForm;
  oauthBusy: boolean;
  oauthReady: boolean;
  oauthId: string;
  oauthMsg: string;
  oauthMsgTone: "ok" | "warn";
  oauthUrl: string;
  manualCode: string;
  manualCodeBusy: boolean;
  manualCodeMsg: string;
  manualCodeOk: boolean;
  saving: boolean;
  error: string;
  dup: boolean;
  endpointChoice: string;
  onFormChange: (next: ProviderPayloadForm) => void;
  onEndpointChoiceChange: (choiceId: string) => void;
  onRequestLogin: (providerId: string) => void;
  onCancelLogin?: () => void;
  onManualCodeChange: (value: string) => void;
  onSubmitManualCode: (providerId: string) => void;
  onConnectKey: () => void;
}) {
  const t = useT();
  return (
    <section className="add-provider-block">
      <h4>{t("modal.connections.authentication")}</h4>
      {form.authMode === "oauth" && (
        <>
          <p className="muted">{t("modal.connections.redirect", { label: preset.label })}</p>
          {!oauthReady && (
            <p className="add-provider-warn">{t("modal.oauthComingSoon", { label: preset.label })}</p>
          )}
          <div className="add-provider-oauth-connect-row">
            <button
              type="button"
              className="btn btn-ghost add-provider-oauth-connect"
              disabled={oauthBusy || !oauthReady}
              onClick={() => {
                onFormChange({ ...form, authMode: "oauth" });
                if (oauthId) onRequestLogin(oauthId);
              }}
            >
              <IconGlobe />
              {oauthBusy ? t("modal.waitingBrowser") : t("modal.connectOauth")}
            </button>
            {oauthBusy && onCancelLogin && (
              <button type="button" className="btn btn-ghost add-provider-oauth-cancel" onClick={onCancelLogin}>
                {t("common.cancel")}
              </button>
            )}
          </div>
          <p className="add-provider-auth-note">
            <IconLock />
            {t("modal.connections.secureWindow")}
          </p>
          {oauthBusy && (
            <AddProviderOauthBusyFields
              oauthUrl={oauthUrl}
              oauthId={oauthId}
              oauthMsg={oauthMsg}
              oauthMsgTone={oauthMsgTone}
              manualCode={manualCode}
              manualCodeBusy={manualCodeBusy}
              manualCodeMsg={manualCodeMsg}
              manualCodeOk={manualCodeOk}
              onManualCodeChange={onManualCodeChange}
              onSubmitManualCode={onSubmitManualCode}
            />
          )}
          {!oauthBusy && oauthMsg && (
            <p className={oauthMsgTone === "warn" ? "add-provider-warn" : "muted"}>{oauthMsg}</p>
          )}
        </>
      )}
      {form.authMode === "key" && (
        <AddProviderKeyMethod
          preset={preset}
          form={form}
          saving={saving}
          onFormChange={onFormChange}
          onConnectKey={onConnectKey}
          hidePicker
        />
      )}
      <AddProviderSetupEndpoint
        preset={preset}
        form={form}
        endpointChoice={endpointChoice}
        onFormChange={onFormChange}
        onEndpointChoiceChange={onEndpointChoiceChange}
      />
      <AddProviderConnectionHints preset={preset} form={form} />
      {error && <p className="add-provider-error" role="alert">{error}</p>}
      {dup && <p className="add-provider-warn">{t("modal.duplicateWarn", { name: form.name.trim() })}</p>}
    </section>
  );
}
