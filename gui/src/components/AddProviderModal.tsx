/** Benes dashboard client for the Go proxy (`internal/server`). */
import { usageSummary30dResourceKey } from "../usage-summary-resource";
import { useCallback, useEffect, useMemo, useReducer, useRef, useState } from "react";
import { createPortal } from "react-dom";
import { useT } from "../i18n/shared";
import { useKeyedClientResource } from "../client-resource";
import {
  encodeProviderCreate,
  type ProviderPayload,
  type ProviderPayloadForm,
} from "../provider-workspace/provider-create-request";
import {
  codexPresetDescriptionKey,
  isReservedCodexForwardPreset,
} from "../provider-workspace/openai-forward-policy";
import { oauthTosRisk } from "../oauth-tos-risk";
import type { AccountLoginRow, AccountLoginStatus } from "./provider-catalog/ProviderCatalog";
import { FEATURED_PRESET_IDS, presetModelIds, type CatalogPreset } from "./provider-catalog/provider-presets";
import { catalogFamilyForPreset, preferredCatalogFamilyMember } from "./provider-catalog/catalog-families";
import { presetIsLocal, presetTier, railSubtitle } from "./provider-catalog/catalog-policy";
import { baseUrlForChoice, matchChoiceId, resolvedBaseUrlForChoice } from "../base-url-choice";
import { useAddProviderOAuth, type AddProviderOAuthSetters } from "./use-add-provider-oauth";
import {
  applyAddProviderCommand,
  createAddProviderSession,
  type AddProviderCommand,
} from "./add-provider-session";
import {
  AddProviderModalFooter,
  AddProviderModalPages,
  AddProviderWizardHead,
} from "./add-provider-modal-chrome";
import { AddProviderCatalogBody, AddProviderTosOverlay, type AddProviderCatalogBodyProps } from "./add-provider-modal-body";
import {
  addProviderAuthMethodKey,
  addProviderChoosePresetForm,
  addProviderHidePrimary,
  addProviderInitialSelection,
  addProviderPostErrorMessage,
  addProviderPrimaryAction,
  addProviderPrimaryDisabled,
  addProviderPrimaryLabelKey,
  addProviderPrimaryLabelKind,
  addProviderShowBack,
  addProviderVerifyAllowsBack,
  addProviderWizardPane,
  addProviderWizardRetreat,
  addProviderWizardStep,
  decodeProviderTestOutcome,
  firstFeaturedCatalogPreset,
  providerTestMessage,
  validateAddProviderSubmit,
  type AddProviderWizardStep,
} from "../lib/add-provider-submit-policy";
import {
  ensureConfiguredThenLoadAddProviderVerifyLive,
  loadAddProviderVerifyLive,
  type AddProviderVerifyLive,
  type VerifyQuotaWindow,
} from "../lib/add-provider-verify-policy";

export type ProviderConfig = ProviderPayload;

type Preset = CatalogPreset;

/** What the connection test and the live probe found for the posted provider. */
type VerifyState = {
  name: string;
  testing: boolean;
  ok?: boolean;
  message?: string;
  liveModels?: string[];
  quotaLoading?: boolean;
  quotaWindows?: VerifyQuotaWindow[];
  creditsRemaining?: number;
};

/** Accounts the Providers board hands the modal so its catalog can show logins. */
type AddProviderModalAccounts = {
  accountRows?: AccountLoginRow[];
  accountStatus?: Record<string, AccountLoginStatus>;
  accountBusy?: string | null;
};

/** Account actions the Providers board owns; the modal renders and forwards them. */
type AddProviderModalAccountActions = {
  onAccountLogin?: (provider: string, addAccount?: boolean) => void;
  onAccountCancelLogin?: (provider: string) => void;
  onAccountLogout?: (provider: string) => void;
  onAccountManage?: (provider: string) => void;
};

/** Wire shapes of the three listener reads this modal composes. */
type OAuthRegistryPayload = { providers?: string[] };
type PresetCatalogPayload = { providers?: Preset[] };
type UsageSummaryPayload = { providers?: Array<{ provider: string; requests: number }> };

export type AddProviderModalProps = AddProviderModalAccounts & AddProviderModalAccountActions & {
  apiBase: string;
  existingNames: string[];
  onClose: () => void;
  onAdded: (name: string) => void;
  initialTier?: "accounts" | "free" | "paid";
  initialCustom?: boolean;
  onOpen?: () => void;
};

export default function AddProviderModal(props: AddProviderModalProps) {
  const { apiBase, existingNames, onClose, onAdded, initialTier, initialCustom = false } = props;
  const t = useT();
  const fallbackPresets = useMemo<Preset[]>(() => [
    { id: "custom", label: t("modal.customProvider"), adapter: "openai-chat", baseUrl: "", auth: "key" },
  ], [t]);
  const [state, dispatch] = useReducer(
    applyAddProviderCommand,
    initialCustom,
    (custom) => createAddProviderSession(custom, t("modal.customProvider")),
  );
  const aliveRef = useRef(true);
  const previousFocusRef = useRef<HTMLElement | null>(null);
  const dialogRef = useRef<HTMLDivElement>(null);
  const { oauthSupported, presets, presetsLoading } = useAddProviderCatalogResources(apiBase, fallbackPresets);
  const {
    selection: { preset, form, endpointChoice },
    save: { inFlight: saving, error },
    oauth,
    manual,
  } = state;
  const [selectedAccount, setSelectedAccount] = useState<AccountLoginRow | null>(null);
  const [verify, setVerify] = useState<VerifyState | null>(null);
  const [phase, setPhase] = useState<AddProviderWizardStep>("provider");
  const wizardStep = addProviderWizardStep({ phase, hasVerify: Boolean(verify) });

  useAddProviderModalLifecycle({
    dialogRef,
    previousFocusRef,
    aliveRef,
    onOpen: props.onOpen,
    onClose,
    oauthTosPending: oauth.tosFor,
  });

  const finishOAuthIntoVerify = useCallback((name: string) => {
    setVerify({ name, testing: false, ok: true, message: t("pws.connectionOk"), quotaLoading: true });
    setPhase("verify");
    void (async () => {
      let postBody: { name: string; provider: ProviderPayload } | null = null;
      try {
        if (form) postBody = { ...encodeProviderCreate(preset ?? { id: "custom" }, form), name };
      } catch {
        postBody = null;
      }
      if (!postBody) {
        if (!aliveRef.current) return;
        setVerify((prev) => applyOAuthVerifyFailed(prev, name, t("modal.invalidPreset")));
        return;
      }
      const live = await ensureConfiguredThenLoadAddProviderVerifyLive(apiBase, postBody);
      if (!aliveRef.current) return;
      if (!live.configured) {
        setVerify((prev) => applyOAuthVerifyFailed(prev, name, t("modal.networkError")));
        return;
      }
      setVerify((prev) => applyVerifyLive(prev, name, live));
    })();
  }, [apiBase, form, preset, t]);

  const { loginOAuth, submitManualCode: submitManualCodeApi, cancelPendingLogin } = useAddProviderOAuth({
    apiBase,
    t,
    aliveRef,
    onAdded: finishOAuthIntoVerify,
  });

  const oauthSetters = createAddProviderOAuthSetters(dispatch, manual.notice);

  const presetDescription = (candidate: Preset): string | undefined => addProviderPresetDescription(candidate, t);

  const choosePreset = (candidate: Preset) => {
    void cancelPendingLogin();
    setSelectedAccount(null);
    setVerify(null);
    const choiceId = matchChoiceId(candidate.baseUrlChoices, candidate.baseUrl);
    dispatch({
      op: "selectPreset",
      preset: candidate,
      endpointChoice: choiceId,
      form: addProviderChoosePresetForm(candidate, resolvedPresetBaseUrl(candidate, choiceId)),
    });
  };

  const submit = (formOverride?: ProviderPayloadForm | null) => {
    void submitAddProvider({
      apiBase,
      preset,
      form: formOverride ?? form,
      endpointChoice,
      t,
      dispatch,
      setVerify,
    });
  };

  const requestLoginOAuth = (providerId: string) => {
    if (oauth.busy) return;
    if (oauthTosRisk(providerId)) {
      dispatch({ op: "resolveTos", providerId });
      return;
    }
    void loginOAuth(providerId, oauthSetters);
  };

  const submitManualCode = (providerId: string) => {
    void submitManualCodeApi(providerId, manual.value, manual.inFlight, {
      setManualCodeBusy: busy => dispatch({ op: "setManualCodeBusy", busy }),
      setManualCode: code => dispatch({ op: "updateManualCode", code }),
      setManualCodeOk: ok => dispatch({ op: "setManualCodeNotice", notice: manual.notice, ok }),
      setManualCodeMsg: msg => dispatch({ op: "setManualCodeNotice", notice: msg }),
    });
  };

  const isCustom = preset?.id === "custom";
  const isLocal = form?.authMode === "local";
  const isReservedForward = Boolean(preset && isReservedCodexForwardPreset(preset));
  const dup = addProviderDuplicateName(form, existingNames);
  const accountRows = props.accountRows;

  useEffect(() => {
    const selection = addProviderInitialSelection({
      hasPreset: Boolean(preset),
      hasSelectedAccount: Boolean(selectedAccount),
      initialCustom,
      presetsLoading,
      initialTier,
    });
    if (selection === "wait") return;
    if (selection === "first-account") {
      const firstAccount = accountRows?.[0];
      if (firstAccount) setSelectedAccount(firstAccount);
      return;
    }
    const first = firstFeaturedCatalogPreset(presets, FEATURED_PRESET_IDS);
    if (!first) return;
    const family = catalogFamilyForPreset(first.id, presets);
    choosePreset(family ? preferredCatalogFamilyMember(family, existingNames) : first);
    // choosePreset is recreated each render; selecting the first catalog row is mount/load only.
    // oxlint-disable-next-line react/exhaustive-deps
  }, [preset, selectedAccount, initialCustom, initialTier, presetsLoading, presets, accountRows, existingNames]);

  const primaryLabel = t(addProviderPrimaryLabelKey(
    addProviderPrimaryLabelKind(form?.authMode === "oauth", oauth.busy, saving, wizardStep),
  ));

  const runPrimary = () => {
    const action = addProviderPrimaryAction({
      hasVerify: Boolean(verify),
      oauthMode: form?.authMode === "oauth",
      oauthProvider: preset?.oauthProvider,
      phase: wizardStep,
    });
    if (action === "advance-provider") {
      setPhase("connection");
      return;
    }
    if (action === "finish-verify" && verify) {
      onAdded(verify.name);
      return;
    }
    if (action === "oauth-login" && preset?.oauthProvider) {
      requestLoginOAuth(preset.oauthProvider);
      return;
    }
    submit(null);
  };

  const runBack = () => {
    if (verify) {
      setVerify(null);
      setPhase("connection");
      return;
    }
    if (wizardStep === "connection") void cancelPendingLogin();
    setPhase(addProviderWizardRetreat(wizardStep));
  };

  const bodyProps: AddProviderCatalogBodyProps = {
    pane: addProviderWizardPane(wizardStep),
    presets,
    presetsLoading,
    initialTier,
    preset,
    existingNames,
    accountRows,
    accountStatus: props.accountStatus,
    selectedAccount,
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
    oauthBusy: oauth.busy,
    oauthMsg: oauth.notice,
    oauthMsgTone: oauth.noticeTone,
    oauthUrl: oauth.authorizeUrl,
    oauthUrlProvider: oauth.authorizeFor,
    manualCode: manual.value,
    manualCodeBusy: manual.inFlight,
    manualCodeMsg: manual.notice,
    manualCodeOk: manual.accepted,
    accountBusy: props.accountBusy,
    onChoosePreset: choosePreset,
    onSelectCustom: () => {
      choosePreset(fallbackPresets[0]!);
      setPhase("connection");
    },
    dispatch,
    setSelectedAccount,
    onRequestLogin: requestLoginOAuth,
    onCancelLogin: () => {
      void cancelPendingLogin();
      dispatch({ op: "cancelAuthorization" });
    },
    onSubmitManualCode: (providerId: string) => { void submitManualCode(providerId); },
    onConnectKey: () => connectAddProviderWithKey(form, dispatch, submit),
    onAccountLogin: props.onAccountLogin,
    onAccountCancelLogin: props.onAccountCancelLogin,
    onAccountLogout: props.onAccountLogout,
    onAccountManage: props.onAccountManage,
  };

  return createPortal(
    <>
    {/* Portaled to body: .app/.main overflow:hidden makes a nested overlay's
        backdrop-filter sample an empty layer and paint as a black sheet. */}
    {/* The backdrop must not dismiss this modal: a stray click — or a text
        selection drag that is released outside the card — would wipe every
        field the user already filled in. Close only via the × button,
        Escape, or a successful add. */}
    <div role="dialog" aria-modal="true" aria-label={t("modal.add")} className="modal-overlay add-provider-overlay">
      <div ref={dialogRef} className="modal-card add-provider-modal">
        <AddProviderWizardHead wizardStep={wizardStep} onClose={onClose} />
        <AddProviderModalPages
          verify={verify}
          preset={preset}
          meta={preset ? railSubtitle({ auth: preset.auth, local: presetIsLocal(preset), tier: presetTier(preset) }, t) : undefined}
          methodLabel={t(addProviderAuthMethodKey(form?.authMode))}
          modelIds={preset ? presetModelIds(preset, presets) : []}
          authMode={form?.authMode}
          catalog={<AddProviderCatalogBody {...bodyProps} />}
        />
        <AddProviderModalFooter
          hidePrimary={addProviderHidePrimary(wizardStep, Boolean(selectedAccount))}
          showBack={addProviderShowBack(wizardStep)}
          backDisabled={!addProviderVerifyAllowsBack(verify)}
          primaryDisabled={addProviderModalPrimaryDisabled({
            saving,
            oauthBusy: oauth.busy,
            verify,
            form,
            selectedAccount,
            preset,
            oauthSupported,
            wizardStep,
          })}
          primaryLabel={addProviderFooterPrimaryLabel(verify, primaryLabel, t("modal.add"))}
          primaryEmphasis
          onClose={onClose}
          onBack={runBack}
          onPrimary={runPrimary}
        />
      </div>
    </div>
    <AddProviderTosOverlay
      oauthTosPending={oauth.tosFor}
      presetLabel={preset?.label}
      dispatch={dispatch}
      onContinue={id => { void loginOAuth(id, oauthSetters); }}
    />
    </>,
    document.body,
  );
}

/**
 * The modal reads three listener resources through one keyed cache: the OAuth
 * registry, the preset catalog, and the shared 30-day usage summary. The summary
 * key is shared with the Dashboard and the Providers board, so its deadline has
 * to be raised here as well.
 */
function useAddProviderCatalogResources(apiBase: string, fallbackPresets: Preset[]) {
  const oauthProviders = useKeyedClientResource(
    `add-provider-oauth:${apiBase}`,
    [apiBase],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/oauth/providers`, { signal });
      if (!response.ok) return [] as string[];
      const payload = await response.json() as OAuthRegistryPayload;
      return payload.providers ?? [];
    },
  );
  const presetRows = useKeyedClientResource(
    `add-provider-presets:${apiBase}`,
    [apiBase],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/provider-presets`, { signal });
      if (!response.ok) throw new Error(String(response.status));
      const payload = await response.json() as PresetCatalogPayload;
      // An empty catalog is not an answer: keep the local Custom row instead.
      return Array.isArray(payload.providers) && payload.providers.length > 0 ? payload.providers : null;
    },
  );
  useKeyedClientResource(
    usageSummary30dResourceKey(apiBase),
    [apiBase],
    async (signal) => {
      const response = await fetch(`${apiBase}/api/usage?range=30d`, { signal });
      if (!response.ok) throw new Error(String(response.status));
      return await response.json() as UsageSummaryPayload;
    },
    { deadlineMs: 60_000 },
  );
  return {
    oauthSupported: oauthProviders.data ?? [],
    presets: presetRows.data ?? fallbackPresets,
    presetsLoading: presetRows.loading,
  };
}

/** Open hooks: remember the opener, focus the first field, close on Escape. */
function useAddProviderModalLifecycle({
  dialogRef,
  previousFocusRef,
  aliveRef,
  onOpen,
  onClose,
  oauthTosPending,
}: {
  dialogRef: React.RefObject<HTMLDivElement | null>;
  previousFocusRef: React.MutableRefObject<HTMLElement | null>;
  aliveRef: React.MutableRefObject<boolean>;
  onOpen?: () => void;
  onClose: () => void;
  oauthTosPending: string | null;
}) {
  useEffect(() => {
    aliveRef.current = true;
    previousFocusRef.current = document.activeElement as HTMLElement | null;
    onOpen?.();
    focusFirstModalControl(dialogRef.current);
    return () => {
      aliveRef.current = false;
      previousFocusRef.current?.focus();
    };
    // oxlint-disable-next-line react/react-compiler -- existing exhaustive-deps exception is intentional
    // eslint-disable-next-line react-hooks/exhaustive-deps -- mount-only open hook
  }, []);

  useEffect(() => {
    const onKey = (event: KeyboardEvent) => {
      if (event.key !== "Escape") return;
      // The consent gate owns Escape while it is up: closing under it drops the step.
      if (oauthTosPending) return;
      onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose, oauthTosPending]);
}

const MODAL_CONTROL_SELECTOR = "input:not([disabled]), button:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex='-1'])";

/** Put the caret where the user is about to type: the catalog search, else the first control. */
function focusFirstModalControl(dialog: HTMLDivElement | null) {
  if (!dialog) return;
  const target = dialog.querySelector<HTMLElement>(".add-provider-search input")
    ?? dialog.querySelector<HTMLElement>(MODAL_CONTROL_SELECTOR);
  target?.focus();
}

/**
 * The session reducer owns the modal's OAuth fields, so the login hook writes
 * through commands instead of its own state. One factory keeps that mapping,
 * including the accepted flag, in a single place.
 */
function createAddProviderOAuthSetters(
  dispatch: React.Dispatch<AddProviderCommand>,
  currentManualNotice: string,
): AddProviderOAuthSetters {
  return {
    setOauthBusy: busy => dispatch({ op: "startAuthorization", busy }),
    setOauthMsg: notice => dispatch({ op: "setAuthorizationNotice", notice }),
    setOauthMsgTone: tone => dispatch({ op: "setAuthorizationTone", tone }),
    setOauthUrl: (url, providerId) => dispatch({ op: "receiveAuthorizationUrl", url, providerId }),
    setManualCode: code => dispatch({ op: "updateManualCode", code }),
    setManualCodeBusy: busy => dispatch({ op: "setManualCodeBusy", busy }),
    setManualCodeMsg: notice => dispatch({ op: "setManualCodeNotice", notice }),
    setManualCodeOk: ok => dispatch({ op: "setManualCodeNotice", notice: currentManualNotice, ok }),
  };
}

/** The catalog default for a row: its chosen endpoint, else the row's own base url. */
function resolvedPresetBaseUrl(preset: Preset, choiceId: string): string {
  return preset.baseUrlChoices?.length
    ? baseUrlForChoice(preset.baseUrlChoices, choiceId, preset.baseUrl)
    : preset.baseUrl;
}

/** The base url a submit posts: the chosen endpoint, trimmed when the row has no choices. */
function submittedBaseUrl(preset: Preset | null, choiceId: string, draftBaseUrl: string): string {
  return preset?.baseUrlChoices?.length
    ? resolvedBaseUrlForChoice(preset.baseUrlChoices, choiceId, draftBaseUrl)
    : draftBaseUrl.trim();
}

function addProviderDuplicateName(form: ProviderPayloadForm | null, existingNames: string[]): boolean {
  const name = form?.name.trim() ?? "";
  return name !== "" && existingNames.includes(name);
}

/** The key lane is one click: pin the method, then post the draft. */
function connectAddProviderWithKey(
  form: ProviderPayloadForm | null,
  dispatch: React.Dispatch<AddProviderCommand>,
  submit: (formOverride?: ProviderPayloadForm | null) => void,
) {
  if (!form) return;
  const next = { ...form, authMode: "key" as const };
  dispatch({ op: "updateDraft", form: next });
  submit(next);
}

function addProviderModalPrimaryDisabled(input: {
  saving: boolean;
  oauthBusy: boolean;
  verify: VerifyState | null;
  form: ProviderPayloadForm | null;
  selectedAccount: AccountLoginRow | null;
  preset: Preset | null;
  oauthSupported: string[];
  wizardStep: AddProviderWizardStep;
}): boolean {
  return addProviderPrimaryDisabled({
    saving: input.saving,
    oauthBusy: input.oauthBusy,
    verifyTesting: input.verify?.testing,
    verifyOk: input.verify?.ok,
    hasForm: Boolean(input.form),
    hasVerify: Boolean(input.verify),
    hasAccount: Boolean(input.selectedAccount),
    oauthMode: input.form?.authMode === "oauth",
    oauthProvider: input.preset?.oauthProvider,
    oauthSupported: input.oauthSupported,
    phase: input.wizardStep,
    authMode: input.form?.authMode,
    apiKey: input.form?.apiKey,
    keyOptional: input.preset?.keyOptional,
  });
}

function addProviderPresetDescription(candidate: Preset, t: ReturnType<typeof useT>): string | undefined {
  const key = codexPresetDescriptionKey(candidate);
  if (key) return t(key);
  return candidate.note;
}

function addProviderFooterPrimaryLabel(
  verify: VerifyState | null,
  connectionLabel: string,
  doneLabel: string,
): string {
  return verify ? doneLabel : connectionLabel;
}

/** Owner of the create POST: validate, encode, post, then run the connection test. */
async function submitAddProvider(input: {
  apiBase: string;
  preset: Preset | null;
  form: ProviderPayloadForm | null;
  endpointChoice: string;
  t: ReturnType<typeof useT>;
  dispatch: React.Dispatch<AddProviderCommand>;
  setVerify: React.Dispatch<React.SetStateAction<VerifyState | null>>;
}) {
  const { apiBase, preset, form, endpointChoice, t, dispatch, setVerify } = input;
  if (!form) return;
  const reserved = preset ? isReservedCodexForwardPreset(preset) : false;
  const resolvedBaseUrl = submittedBaseUrl(preset, endpointChoice, form.baseUrl);
  const validation = validateAddProviderSubmit({ reserved, name: form.name, resolvedBaseUrl });
  if (!validation.ok) {
    dispatch({ op: "setSaveError", error: t(validation.errorKey) });
    return;
  }
  const postBody = encodeAddProviderPost(preset, { ...form, baseUrl: resolvedBaseUrl });
  if (!postBody) {
    dispatch({ op: "setSaveError", error: t("modal.invalidPreset") });
    return;
  }
  dispatch({ op: "startSaving" });
  dispatch({ op: "setSaveError", error: "" });
  try {
    const response = await fetch(`${apiBase}/api/providers`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(postBody),
    });
    if (!response.ok) {
      const payload = await response.json().catch(() => ({})) as { error?: string };
      const message = addProviderPostErrorMessage(payload, response.status, vars => t("modal.failedStatus", { status: vars.status }));
      dispatch({ op: "setSaveError", error: message });
      setVerify({ name: postBody.name, testing: false, ok: false, message });
      return;
    }
    await verifyPostedProvider({ apiBase, name: postBody.name, t, setVerify });
  } catch {
    dispatch({ op: "setSaveError", error: t("modal.networkError") });
    setVerify({ name: postBody.name, testing: false, ok: false, message: t("modal.networkError") });
  } finally {
    dispatch({ op: "finishSaving" });
  }
}

function encodeAddProviderPost(
  preset: Preset | null,
  form: ProviderPayloadForm,
): { name: string; provider: ProviderPayload } | null {
  try {
    return encodeProviderCreate(preset ?? { id: "custom" }, form);
  } catch {
    return null;
  }
}

/** Post-create connection test, run alongside the live quota and model probe. */
async function verifyPostedProvider(input: {
  apiBase: string;
  name: string;
  t: ReturnType<typeof useT>;
  setVerify: React.Dispatch<React.SetStateAction<VerifyState | null>>;
}) {
  const { apiBase, name, t, setVerify } = input;
  setVerify({ name, testing: true, quotaLoading: true });
  try {
    const [testResponse, live] = await Promise.all([
      fetch(`${apiBase}/api/providers/test?name=${encodeURIComponent(name)}`, { method: "POST" }),
      loadAddProviderVerifyLive(apiBase, name),
    ]);
    const test = await testResponse.json().catch(() => ({})) as {
      ok?: boolean;
      applicable?: boolean;
      message?: string;
      error?: string;
    };
    const outcome = decodeProviderTestOutcome(testResponse.ok, test);
    setVerify({
      name,
      testing: false,
      ok: outcome.ok,
      message: providerTestMessage(outcome, t),
      liveModels: live.models,
      quotaWindows: live.windows,
      creditsRemaining: live.creditsRemaining,
    });
  } catch {
    setVerify({ name, testing: false, ok: true, message: t("modal.next.workspace") });
  }
}

function applyVerifyLive(
  prev: VerifyState | null,
  name: string,
  live: AddProviderVerifyLive,
): VerifyState | null {
  if (!prev || prev.name !== name) return prev;
  return {
    ...prev,
    quotaLoading: false,
    liveModels: live.models,
    quotaWindows: live.windows,
    creditsRemaining: live.creditsRemaining,
  };
}

function applyOAuthVerifyFailed(
  prev: VerifyState | null,
  name: string,
  message: string,
): VerifyState | null {
  if (!prev || prev.name !== name) return prev;
  return { ...prev, quotaLoading: false, ok: false, message };
}
