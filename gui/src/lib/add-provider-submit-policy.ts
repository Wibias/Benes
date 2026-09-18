/** Add-provider submit validation, wizard step, and primary-action policy. */
import type { TKey } from "../i18n/shared";
import type { ProviderPayloadForm } from "../provider-workspace/provider-create-request";
import type { CatalogPreset } from "../components/provider-catalog/provider-presets";

export type AddProviderSubmitErrorKey =
  | "modal.nameRequired"
  | "modal.baseUrlRequired"
  | "modal.baseUrlPlaceholderError";

export type AddProviderSubmitValidation =
  | { ok: true }
  | { ok: false; errorKey: AddProviderSubmitErrorKey };

/** Name, then base URL, then placeholder — the same order the modal used to short-circuit. */
export function validateAddProviderSubmit(input: {
  reserved: boolean;
  name: string;
  resolvedBaseUrl: string;
}): AddProviderSubmitValidation {
  if (!input.reserved && !input.name.trim()) return { ok: false, errorKey: "modal.nameRequired" };
  if (!input.reserved && !input.resolvedBaseUrl) return { ok: false, errorKey: "modal.baseUrlRequired" };
  if (!input.reserved && /\{[^}]*\}/.test(input.resolvedBaseUrl)) {
    return { ok: false, errorKey: "modal.baseUrlPlaceholderError" };
  }
  return { ok: true };
}

export function addProviderPostErrorMessage(
  data: { error?: string } | undefined,
  status: number,
  failedStatus: (vars: { status: number }) => string,
): string {
  return data?.error || failedStatus({ status });
}

export type ProviderTestMessageKind = "raw" | "not-applicable" | "ok";

export function decodeProviderTestOutcome(
  testResOk: boolean,
  test: { ok?: boolean; applicable?: boolean; message?: string; error?: string },
): { ok: boolean; messageKind: ProviderTestMessageKind; rawMessage?: string } {
  const ok = test.ok !== false && testResOk;
  const rawMessage = test.message || test.error;
  if (rawMessage) return { ok, messageKind: "raw", rawMessage };
  if (test.applicable === false) return { ok, messageKind: "not-applicable" };
  return { ok, messageKind: "ok" };
}

export function providerTestMessage(
  outcome: { messageKind: ProviderTestMessageKind; rawMessage?: string },
  t: (key: TKey) => string,
): string {
  if (outcome.messageKind === "raw") return outcome.rawMessage ?? "";
  if (outcome.messageKind === "not-applicable") return t("pws.connectionNotApplicable");
  return t("pws.connectionOk");
}

export type AddProviderWizardStep = "provider" | "connection" | "verify";

export function addProviderWizardStep(input: {
  phase: AddProviderWizardStep;
  hasVerify: boolean;
}): AddProviderWizardStep {
  if (input.hasVerify) return "verify";
  return input.phase;
}

export function addProviderWizardRetreat(step: AddProviderWizardStep): AddProviderWizardStep {
  if (step === "verify") return "connection";
  if (step === "connection") return "provider";
  return "provider";
}

export function addProviderWizardPane(step: AddProviderWizardStep): "catalog" | "setup" {
  return step === "connection" ? "setup" : "catalog";
}

export function addProviderShowBack(step: AddProviderWizardStep): boolean {
  return step !== "provider";
}

export function addProviderHidePrimary(step: AddProviderWizardStep, hasAccount: boolean): boolean {
  return step === "connection" && hasAccount;
}

export function addProviderProviderChoiceReady(hasPreset: boolean, hasAccount: boolean): boolean {
  return hasPreset || hasAccount;
}

export function addProviderAuthMethodKey(
  authMode: string | undefined,
): "modal.badge.oauth" | "modal.badge.local" | "modal.badge.codexLogin" | "modal.badge.apiKey" {
  if (authMode === "oauth") return "modal.badge.oauth";
  if (authMode === "local") return "modal.badge.local";
  if (authMode === "forward") return "modal.badge.codexLogin";
  return "modal.badge.apiKey";
}

export function addProviderVerifyAllowsBack(verify: { testing: boolean; ok?: boolean } | null): boolean {
  if (!verify) return true;
  return !verify.testing;
}

export function addProviderVerifyPrimaryEnabled(verify: { testing: boolean; ok?: boolean } | null): boolean {
  return Boolean(verify && !verify.testing && verify.ok);
}

export function addProviderConnectionKeyLocked(input: {
  authMode?: string;
  apiKey?: string;
  keyOptional?: boolean;
}): boolean {
  return input.authMode === "key" && !input.keyOptional && !(input.apiKey ?? "").trim();
}

export function addProviderPrimaryDisabled(input: {
  saving: boolean;
  oauthBusy: boolean;
  verifyTesting?: boolean;
  verifyOk?: boolean;
  hasForm: boolean;
  hasVerify: boolean;
  hasAccount?: boolean;
  oauthMode: boolean;
  oauthProvider?: string;
  oauthSupported: string[];
  phase: AddProviderWizardStep;
  authMode?: string;
  apiKey?: string;
  keyOptional?: boolean;
}): boolean {
  if (input.phase === "provider") {
    return !addProviderProviderChoiceReady(input.hasForm, Boolean(input.hasAccount));
  }
  if (input.phase === "verify" || input.hasVerify) {
    return !addProviderVerifyPrimaryEnabled({
      testing: Boolean(input.verifyTesting || input.saving),
      ok: input.verifyOk,
    });
  }
  if (input.phase === "connection" && input.oauthMode) return true;
  if (addProviderConnectionKeyLocked(input)) return true;
  return Boolean(
    input.saving
    || input.oauthBusy
    || (!input.hasForm && !input.hasVerify)
    || (
      input.oauthMode
      && !input.hasVerify
      && !(input.oauthProvider && input.oauthSupported.includes(input.oauthProvider))
    ),
  );
}

export type AddProviderPrimaryLabelKind = "waiting" | "connect-oauth" | "adding" | "connect" | "continue" | "done";

export function addProviderPrimaryLabelKind(
  oauthMode: boolean,
  oauthBusy: boolean,
  saving: boolean,
  phase: AddProviderWizardStep = "connection",
): AddProviderPrimaryLabelKind {
  if (phase === "provider") return "continue";
  if (phase === "verify") return saving ? "adding" : "done";
  if (phase === "connection") return saving ? "adding" : "continue";
  if (oauthMode) return oauthBusy ? "waiting" : "connect-oauth";
  return saving ? "adding" : "continue";
}

const PRIMARY_LABEL_KEYS = {
  waiting: "modal.waitingBrowser",
  "connect-oauth": "modal.connectOauth",
  adding: "modal.adding",
  connect: "modal.connect",
  continue: "modal.continue",
  done: "modal.add",
} as const;

export function addProviderPrimaryLabelKey(kind: AddProviderPrimaryLabelKind): typeof PRIMARY_LABEL_KEYS[AddProviderPrimaryLabelKind] {
  return PRIMARY_LABEL_KEYS[kind];
}

export type AddProviderPrimaryAction = "finish-verify" | "oauth-login" | "submit" | "advance-provider";

export function addProviderPrimaryAction(input: {
  hasVerify: boolean;
  oauthMode: boolean;
  oauthProvider?: string;
  phase: AddProviderWizardStep;
}): AddProviderPrimaryAction {
  if (input.phase === "provider") return "advance-provider";
  if (input.hasVerify) return "finish-verify";
  if (input.oauthMode && input.oauthProvider) return "oauth-login";
  return "submit";
}

export type AddProviderInitialSelection = "wait" | "first-account" | "first-preset";

export function addProviderInitialSelection(input: {
  hasPreset: boolean;
  hasSelectedAccount: boolean;
  initialCustom: boolean;
  presetsLoading: boolean;
  initialTier?: "accounts" | "free" | "paid";
}): AddProviderInitialSelection {
  if (input.hasPreset || input.hasSelectedAccount || input.initialCustom || input.presetsLoading) {
    return "wait";
  }
  if (input.initialTier === "accounts") return "first-account";
  return "first-preset";
}

export function firstFeaturedCatalogPreset<T extends { id: string }>(
  presets: T[],
  featuredIds: readonly string[],
): T | undefined {
  const featured = featuredIds
    .map(id => presets.find(row => row.id === id))
    .find((row): row is T => !!row);
  return featured ?? presets.find(row => row.id !== "custom");
}

export function addProviderChoosePresetForm(
  preset: Pick<CatalogPreset, "id" | "adapter" | "baseUrl" | "responsesPath" | "auth" | "defaultModel">,
  resolvedBaseUrl: string,
): ProviderPayloadForm {
  return {
    name: preset.id === "custom" ? "" : preset.id,
    adapter: preset.adapter,
    baseUrl: resolvedBaseUrl,
    responsesPath: preset.responsesPath,
    authMode: preset.auth,
    apiKey: "",
    apiKeyTransport: undefined,
    defaultModel: preset.defaultModel ?? "",
    allowPrivateNetwork: false,
  };
}
