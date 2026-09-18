/**
 * Field sections of the Add Provider form.
 *
 * The pane owns ordering; this module owns the fields. Plain fields are declared
 * once as specs and drawn by one control, so adding a field is a table entry.
 * Which sections appear — and which auth panel replaces the key fields — comes
 * from `lib/add-provider-form-policy.ts`.
 */
import type { ReactNode } from "react";
import { IconExternal, IconKey } from "../icons";
import { useT, type TKey } from "../i18n/shared";
import type { CatalogPreset } from "./provider-catalog/provider-presets";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";
import { AddProviderField } from "./add-provider-modal-field";
import { baseUrlForChoice } from "../base-url-choice";
import {
  addProviderAuthPanel,
  addProviderBaseUrlPlaceholderHintVisible,
  addProviderEndpointLabelKey,
  addProviderSetupGuideVisible,
  addProviderShowsEndpointChoices,
  addProviderShowsPrivateNetworkHint,
} from "../lib/add-provider-form-policy";

/** The draft the Add Provider form edits, plus the writers the sections call. */
export type AddProviderDraft = ProviderPayloadForm;
export type AddProviderDraftWriter = (next: AddProviderDraft) => void;
export type AddProviderChoiceHandler = (choiceId: string) => void;
export type AddProviderVoidHandler = () => void;

/** Adapter ids the listener understands, in the order the picker offers them. */
const PROVIDER_ADAPTER_IDS = [
  "openai-responses",
  "openai-chat",
  "anthropic",
  "google",
  "azure-openai",
  "cursor",
];

/**
 * The auth panel and the setup guide are callouts: a tinted surface, a hairline
 * of the same hue, and the shared small radius. Green means "no secret of your
 * own is needed", amber means "local or unusual".
 */
const CALLOUT_GREEN = {
  background: "var(--green-soft)",
  border: "1px solid var(--green)",
  borderRadius: "var(--radius-sm)",
  color: "var(--green)",
} as const;

const CALLOUT_AMBER = {
  background: "var(--amber-soft)",
  border: "1px solid var(--amber)",
  borderRadius: "var(--radius-sm)",
  color: "var(--amber)",
} as const;

const GUIDE_LIST_STYLE = { margin: "8px 0 0", paddingLeft: 18, color: "var(--muted)" } as const;
const GUIDE_NOTE_STYLE = { color: "var(--muted)", marginTop: 6, fontStyle: "italic" } as const;
const GUIDE_WARN_STYLE = { color: "var(--amber)", marginTop: 6 } as const;
const FIELD_NOTE_STYLE = { color: "var(--amber)" } as const;
const DASHBOARD_LINK_STYLE = { display: "inline-flex", alignItems: "center", gap: 5 } as const;
const KEY_GLYPH_STYLE = { width: 14, height: 14 } as const;
const EXTERNAL_GLYPH_STYLE = { width: 13, height: 13 } as const;
const FORM_ACTION_STYLE = { display: "flex", gap: 8, marginTop: 4, alignItems: "center" } as const;
const FORM_ACTION_SPACER_STYLE = { flex: 1 } as const;

/** Draft keys drawn as a plain text or password input. */
type DraftTextKey = "name" | "baseUrl" | "defaultModel" | "apiKey";

type TextFieldSpec = {
  field: DraftTextKey;
  labelKey: TKey;
  placeholderKey: TKey;
  secret?: boolean;
};

const IDENTITY_FIELD: TextFieldSpec = { field: "name", labelKey: "modal.providerName", placeholderKey: "modal.namePlaceholder" };
const BASE_URL_FIELD: TextFieldSpec = { field: "baseUrl", labelKey: "modal.baseUrl", placeholderKey: "modal.baseUrlPlaceholder" };
const MODEL_FIELD: TextFieldSpec = { field: "defaultModel", labelKey: "modal.defaultModel", placeholderKey: "modal.defaultModelPlaceholder" };
const API_KEY_FIELD: TextFieldSpec = { field: "apiKey", labelKey: "modal.apiKey", placeholderKey: "modal.apiKeyPlaceholder", secret: true };

/** Transport the Anthropic API key may travel in. Native header is the default. */
const API_KEY_TRANSPORTS: Array<{ id: string; labelKey: TKey }> = [
  { id: "x-api-key", labelKey: "modal.apiKeyTransportNative" },
  { id: "bearer", labelKey: "modal.apiKeyTransportBearer" },
];

/** The tinted callout surface shared by the auth panel and the setup guide. */
function Callout({ tone, padding, children }: { tone: "green" | "amber"; padding: string; children: ReactNode }) {
  const relaxed = tone === "amber";
  return (
    <div
      className={relaxed ? "text-label leading-relaxed" : "text-label"}
      style={{ ...(relaxed ? CALLOUT_AMBER : CALLOUT_GREEN), padding }}
    >
      {children}
    </div>
  );
}

function withDraftText(draft: AddProviderDraft, field: DraftTextKey, value: string): AddProviderDraft {
  switch (field) {
    case "name":
      return { ...draft, name: value };
    case "baseUrl":
      return { ...draft, baseUrl: value };
    case "defaultModel":
      return { ...draft, defaultModel: value };
    case "apiKey":
      return { ...draft, apiKey: value };
  }
}

/** One plain field row. The only text-input markup in the Add Provider form. */
export function AddProviderTextField({
  spec,
  draft,
  write,
  readOnly = false,
}: {
  spec: TextFieldSpec;
  draft: AddProviderDraft;
  write: AddProviderDraftWriter;
  readOnly?: boolean;
}) {
  const t = useT();
  return (
    <AddProviderField label={t(spec.labelKey)}>
      <input
        className="input"
        type={spec.secret ? "password" : undefined}
        value={draft[spec.field]}
        readOnly={readOnly}
        placeholder={t(spec.placeholderKey)}
        onChange={event => write(withDraftText(draft, spec.field, event.target.value))}
      />
    </AddProviderField>
  );
}

function guideSteps(preset: CatalogPreset, t: ReturnType<typeof useT>) {
  return [
    <li key="open">
      {t("modal.setupStep1Prefix")}{" "}
      <a href={preset.dashboardUrl} target="_blank" rel="noreferrer">
        {t("modal.setupDashboardLink", { label: preset.label })}
      </a>{" "}
      {t("modal.setupStep1Suffix")}
    </li>,
    <li key="copy">{t("modal.setupStep2")}</li>,
    <li key="paste">{t("modal.setupStep3")}</li>,
  ];
}

/** The provider setup guide, its note, and the base-url placeholder warning. */
export function AddProviderSetupGuide({
  preset,
  draft,
  isReservedForward,
  isCustom,
  isLocal,
}: {
  preset: CatalogPreset;
  draft: AddProviderDraft;
  isReservedForward: boolean;
  isCustom: boolean;
  isLocal: boolean;
}) {
  const t = useT();
  if (!addProviderSetupGuideVisible({
    isReservedForward,
    isCustom,
    isLocal,
    keyOptional: preset.keyOptional,
    note: preset.note,
  })) {
    return null;
  }
  return (
    <details className="setup-guide">
      <summary>{t("modal.setupGuide")}</summary>
      <ol className="text-label leading-relaxed" style={GUIDE_LIST_STYLE}>{guideSteps(preset, t)}</ol>
      {preset.note && <div className="text-label" style={GUIDE_NOTE_STYLE}>{preset.note}</div>}
      {addProviderBaseUrlPlaceholderHintVisible(draft.baseUrl) && (
        <div className="text-label" style={GUIDE_WARN_STYLE}>{t("modal.baseUrlPlaceholderHint")}</div>
      )}
    </details>
  );
}

export function AddProviderIdentityFields({
  draft,
  dup,
  isReservedForward,
  write,
}: {
  draft: AddProviderDraft;
  dup: boolean;
  isReservedForward: boolean;
  write: AddProviderDraftWriter;
}) {
  const t = useT();
  return (
    <>
      <AddProviderTextField spec={IDENTITY_FIELD} draft={draft} write={write} readOnly={isReservedForward} />
      {dup && (
        <div className="add-provider-form-note">
          <div className="text-label" style={FIELD_NOTE_STYLE}>{t("modal.duplicateWarn", { name: draft.name.trim() })}</div>
        </div>
      )}
    </>
  );
}

export function AddProviderAdapterField({ draft, write }: { draft: AddProviderDraft; write: AddProviderDraftWriter }) {
  const t = useT();
  return (
    <AddProviderField label={t("modal.adapter")}>
      <select className="input" value={draft.adapter} onChange={event => write({ ...draft, adapter: event.target.value })}>
        {PROVIDER_ADAPTER_IDS.map(id => <option key={id} value={id}>{id}</option>)}
      </select>
    </AddProviderField>
  );
}

export function AddProviderEndpointFields({
  preset,
  draft,
  endpointChoice,
  write,
  chooseEndpoint,
}: {
  preset: CatalogPreset;
  draft: AddProviderDraft;
  endpointChoice: string;
  write: AddProviderDraftWriter;
  chooseEndpoint: AddProviderChoiceHandler;
}) {
  const t = useT();
  if (!addProviderShowsEndpointChoices(preset.baseUrlChoices)) {
    return <AddProviderTextField spec={BASE_URL_FIELD} draft={draft} write={write} />;
  }
  const choices = preset.baseUrlChoices!;
  return (
    <>
      <AddProviderField label={t("modal.endpoint")}>
        <select
          className="input"
          value={endpointChoice}
          onChange={event => {
            const choice = event.target.value;
            chooseEndpoint(choice);
            write({ ...draft, baseUrl: baseUrlForChoice(choices, choice, draft.baseUrl) });
          }}
        >
          {choices.map(choice => {
            const labelKey = addProviderEndpointLabelKey(choice.id);
            return (
              <option key={choice.id} value={choice.id}>
                {labelKey ? t(labelKey) : choice.label}
              </option>
            );
          })}
        </select>
      </AddProviderField>
      {endpointChoice === "custom" && <AddProviderTextField spec={BASE_URL_FIELD} draft={draft} write={write} />}
    </>
  );
}

export function AddProviderNetworkFields({ draft, write }: { draft: AddProviderDraft; write: AddProviderDraftWriter }) {
  const t = useT();
  return (
    <div className="add-provider-network">
      <label className="add-provider-network-control">
        <input
          type="checkbox"
          checked={draft?.allowPrivateNetwork ?? false}
          onChange={event => write({ ...draft, allowPrivateNetwork: event.target.checked })}
        />
        <span>{t("modal.allowPrivateNetwork")}</span>
      </label>
      {addProviderShowsPrivateNetworkHint(draft?.allowPrivateNetwork) && (
        <p className="muted text-hint add-provider-network-hint">{t("modal.allowPrivateNetworkHint")}</p>
      )}
    </div>
  );
}

/** Panels for the auth methods that carry no key field of their own. */
function authCallout(
  panel: { kind: string },
  preset: CatalogPreset,
  presetDescription: (candidate: CatalogPreset) => string | undefined,
  t: ReturnType<typeof useT>,
) {
  if (panel.kind === "forward") {
    return <Callout tone="green" padding="8px 10px">{presetDescription(preset)}</Callout>;
  }
  if (panel.kind === "local") {
    return <Callout tone="amber" padding="8px 10px">{t("modal.localHint")}</Callout>;
  }
  return (
    <Callout tone="green" padding="10px 12px">
      <strong>{t("modal.freeTierTitle")}</strong> — {preset.note ?? t("modal.freeTierDefault")}
    </Callout>
  );
}

/** The auth panel replaces the key fields entirely for forward/local/keyless rows. */
export function AddProviderAuthFields({
  preset,
  draft,
  presetDescription,
  write,
}: {
  preset: CatalogPreset;
  draft: AddProviderDraft;
  presetDescription: (candidate: CatalogPreset) => string | undefined;
  write: AddProviderDraftWriter;
}) {
  const t = useT();
  const panel = addProviderAuthPanel({
    authMode: draft.authMode,
    keyOptional: preset.keyOptional,
    dashboardUrl: preset.dashboardUrl,
    adapter: draft.adapter,
  });
  if (panel.kind !== "api-key") {
    return authCallout(panel, preset, presetDescription, t);
  }
  return (
    <>
      {panel.showDashboard && preset.dashboardUrl && (
        <a className="text-label" href={preset.dashboardUrl} target="_blank" rel="noreferrer" style={DASHBOARD_LINK_STYLE}>
          <IconKey style={KEY_GLYPH_STYLE} />{t("modal.getApiKey", { label: preset.label })}<IconExternal style={EXTERNAL_GLYPH_STYLE} />
        </a>
      )}
      <AddProviderTextField spec={API_KEY_FIELD} draft={draft} write={write} />
      {panel.showTransport && (
        <AddProviderField label={t("modal.apiKeyTransport")}>
          <select
            className="input"
            value={draft.apiKeyTransport ?? API_KEY_TRANSPORTS[0]!.id}
            onChange={event => write({ ...draft, apiKeyTransport: event.target.value === "bearer" ? "bearer" : undefined })}
          >
            {API_KEY_TRANSPORTS.map(transport => (
              <option key={transport.id} value={transport.id}>{t(transport.labelKey)}</option>
            ))}
          </select>
        </AddProviderField>
      )}
    </>
  );
}

export function AddProviderDefaultModelField({ draft, write }: { draft: AddProviderDraft; write: AddProviderDraftWriter }) {
  return <AddProviderTextField spec={MODEL_FIELD} draft={draft} write={write} />;
}

/** Standalone form actions, used only when the pane is not embedded in a wizard. */
export function AddProviderFormActions({
  saving,
  preset,
  submit,
  useOauthLogin,
  back,
}: {
  saving: boolean;
  preset: CatalogPreset;
  submit: AddProviderVoidHandler;
  useOauthLogin: AddProviderVoidHandler;
  back: AddProviderVoidHandler;
}) {
  const t = useT();
  const leading = [
    {
      key: "submit",
      className: "btn btn-primary",
      label: saving ? t("modal.adding") : t("modal.add"),
      disabled: saving,
      run: submit,
    },
    ...(preset.auth === "oauth" ? [{
      key: "oauth",
      className: "link-btn",
      label: t("modal.useOauthLogin"),
      disabled: false,
      run: useOauthLogin,
    }] : []),
  ];
  return (
    <div style={FORM_ACTION_STYLE}>
      {leading.map(action => (
        <button key={action.key} type="button" className={action.className} disabled={action.disabled} onClick={action.run}>
          {action.label}
        </button>
      ))}
      <div style={FORM_ACTION_SPACER_STYLE} />
      <button type="button" className="btn btn-ghost" onClick={back}>{t("modal.back")}</button>
    </div>
  );
}
