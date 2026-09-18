/** Wizard chrome for the add-provider modal: steps, verify pane, and footer. */
import type { ReactNode } from "react";
import { IconBoxes, IconChevron, IconCpu, IconLock, IconX } from "../icons";
import { useT } from "../i18n/shared";
import { ProviderMark } from "./ProviderMark";
import type { CatalogPreset } from "./provider-catalog/provider-presets";
import type { AddProviderWizardStep } from "../lib/add-provider-submit-policy";
import {
  formatCreditsUsd,
  providerReportsQuota,
  verifyDisplayedModels,
  verifyQuotaKind,
  verifyWindowLabel,
  type VerifyQuotaWindow,
} from "../lib/add-provider-verify-policy";

const WIZARD_STEPS = ["provider", "connection", "verify"] as const;

function wizardStepLabelKey(step: (typeof WIZARD_STEPS)[number]): "modal.step.provider" | "modal.step.connection" | "modal.step.verify" {
  if (step === "provider") return "modal.step.provider";
  if (step === "connection") return "modal.step.connection";
  return "modal.step.verify";
}

export function AddProviderWizardHead({
  wizardStep,
  onClose,
}: {
  wizardStep: AddProviderWizardStep;
  onClose: () => void;
}) {
  const t = useT();
  return (
    <div className="modal-head add-provider-head">
      <h3>{t("modal.add")}</h3>
      <ol className="add-provider-steps" aria-label={t("modal.add")}>
        {WIZARD_STEPS.map((step, index) => (
          <li key={step} className={wizardStep === step ? "is-current" : undefined} aria-current={wizardStep === step ? "step" : undefined}>
            {index > 0 && <IconChevron />}
            <span>{t(wizardStepLabelKey(step))}</span>
          </li>
        ))}
      </ol>
      <button type="button" className="btn btn-ghost btn-icon" aria-label={t("common.close")} onClick={onClose}><IconX /></button>
    </div>
  );
}

function verifyAuthValue(
  testing: boolean,
  ok: boolean | undefined,
  t: ReturnType<typeof useT>,
): string {
  if (testing) return t("modal.verify.testing");
  if (ok) return t("modal.verify.validated");
  return t("modal.verify.failed");
}

function verifyModelsCopy(count: number, t: ReturnType<typeof useT>): string {
  if (count === 1) return t("modal.verify.modelsDiscoveredOne");
  return t("modal.verify.modelsDiscovered", { count });
}

function verifyStoredCopy(authMode: string | undefined, t: ReturnType<typeof useT>): string {
  if (authMode === "key") return t("modal.verify.keyStored");
  return t("modal.verify.oauthStored");
}

function verifyDiscoveredCount(models: readonly string[], seed: readonly string[]): number {
  if (models.length > 0) return models.length;
  return seed.length;
}

function verifyFailureMessage(testing: boolean, message: string | undefined, ok: boolean | undefined): string | undefined {
  if (testing || ok || !message) return undefined;
  return message;
}

function AddProviderVerifyHead({ preset, meta }: { preset?: CatalogPreset | null; meta?: string }) {
  if (!preset) return null;
  return (
    <header className="add-provider-setup-head">
      <ProviderMark name={preset.id} adapter={preset.adapter} baseUrl={preset.baseUrl} className="add-provider-setup-icon" />
      <div>
        <div className="add-provider-setup-title-row">
          <h3>{preset.label}</h3>
        </div>
        {meta ? <p className="muted">{meta}</p> : null}
      </div>
    </header>
  );
}

function AddProviderVerifyModels({
  models,
  testing,
  quotaLoading,
  t,
}: {
  models: string[];
  testing: boolean;
  quotaLoading?: boolean;
  t: ReturnType<typeof useT>;
}) {
  if (models.length > 0) {
    return (
      <ul className="add-provider-verify-model-list">
        {models.map((id) => <li key={id}>{id}</li>)}
      </ul>
    );
  }
  if (testing || quotaLoading) {
    return <p className="muted">{t("modal.verify.testing")}</p>;
  }
  return null;
}

function verifyQuotaCopy(input: {
  quotaLoading?: boolean;
  reportsQuota: boolean;
  windows: readonly VerifyQuotaWindow[];
  creditsRemaining?: number;
  t: ReturnType<typeof useT>;
}): string {
  const kind = verifyQuotaKind({
    testing: Boolean(input.quotaLoading),
    reportsQuota: input.reportsQuota,
    windows: input.windows,
    hasCredits: input.creditsRemaining !== undefined,
  });
  if (kind === "loading") return input.t("common.loading");
  if (kind === "none") return input.t("modal.verify.quotaNone");
  if (kind === "empty") return input.t("pws.dashboard.noQuota");
  const parts = input.windows.map((window) => input.t("modal.verify.quotaWindowLeft", {
    label: verifyWindowLabel(window, input.t),
    pct: window.remainingPercent,
  }));
  if (input.creditsRemaining !== undefined) {
    parts.push(input.t("modal.verify.quotaCreditsLeft", { amount: formatCreditsUsd(input.creditsRemaining) }));
  }
  return parts.join(" · ");
}

export function AddProviderVerifyPane({
  testing,
  ok,
  message,
  preset,
  meta,
  methodLabel,
  modelIds,
  liveModels,
  quotaLoading,
  quotaWindows,
  creditsRemaining,
  providerName,
  authMode,
}: {
  testing: boolean;
  ok?: boolean;
  message?: string;
  preset?: CatalogPreset | null;
  meta?: string;
  methodLabel?: string;
  modelIds?: string[];
  liveModels?: string[];
  quotaLoading?: boolean;
  quotaWindows?: VerifyQuotaWindow[];
  creditsRemaining?: number;
  providerName?: string;
  authMode?: string;
}) {
  const t = useT();
  const seed = modelIds ?? [];
  const models = verifyDisplayedModels(liveModels, seed, quotaLoading);
  const windows = quotaWindows ?? [];
  const failure = verifyFailureMessage(testing, message, ok);
  const quotaCopy = verifyQuotaCopy({
    quotaLoading,
    reportsQuota: providerReportsQuota({
      name: providerName ?? preset?.id ?? "",
      baseUrl: preset?.baseUrl,
      authMode,
    }),
    windows,
    creditsRemaining,
    t,
  });
  return (
    <div className="add-provider-verify">
      <AddProviderVerifyHead preset={preset} meta={meta} />
      <section className="add-provider-verify-facts">
        <h4>{t("modal.verify.heading")}</h4>
        <dl>
          <dt>{t("modal.filter.connection")}</dt>
          <dd>{methodLabel}</dd>
          <dt>{t("modal.verify.authentication")}</dt>
          <dd>{verifyAuthValue(testing, ok, t)}</dd>
          <dt>{t("modal.verify.modelsFound")}</dt>
          <dd>{models.length}</dd>
          <dt>{t("modal.verify.quota")}</dt>
          <dd>{quotaCopy}</dd>
        </dl>
        {failure ? <p className="add-provider-warn">{failure}</p> : null}
      </section>
      <section className="add-provider-verify-models">
        <h4>{t("modal.verify.models")}</h4>
        <AddProviderVerifyModels models={models} testing={testing} quotaLoading={quotaLoading} t={t} />
      </section>
      <section className="add-provider-verify-next-block">
        <h4>{t("modal.verify.whatHappens")}</h4>
        <ul className="add-provider-next">
          <li><IconBoxes />{t("modal.verify.addToBenes")}</li>
          <li><IconCpu />{verifyModelsCopy(verifyDiscoveredCount(models, seed), t)}</li>
          <li><IconLock />{verifyStoredCopy(authMode, t)}</li>
        </ul>
      </section>
    </div>
  );
}

export function AddProviderModalPages({
  verify,
  preset,
  meta,
  methodLabel,
  modelIds,
  authMode,
  catalog,
}: {
  verify: {
    name: string;
    testing: boolean;
    ok?: boolean;
    message?: string;
    liveModels?: string[];
    quotaLoading?: boolean;
    quotaWindows?: VerifyQuotaWindow[];
    creditsRemaining?: number;
  } | null;
  preset?: CatalogPreset | null;
  meta?: string;
  methodLabel: string;
  modelIds?: string[];
  authMode?: string;
  catalog: ReactNode;
}) {
  if (verify) {
    return (
      <AddProviderVerifyPane
        testing={verify.testing}
        ok={verify.ok}
        message={verify.message}
        preset={preset}
        meta={meta}
        methodLabel={methodLabel}
        modelIds={modelIds}
        liveModels={verify.liveModels}
        quotaLoading={verify.quotaLoading}
        quotaWindows={verify.quotaWindows}
        creditsRemaining={verify.creditsRemaining}
        providerName={verify.name}
        authMode={authMode}
      />
    );
  }
  return catalog;
}

export function AddProviderModalFooter({
  hidePrimary,
  primaryDisabled,
  primaryLabel,
  primaryEmphasis = false,
  showBack,
  backDisabled,
  onClose,
  onPrimary,
  onBack,
}: {
  hidePrimary: boolean;
  primaryDisabled: boolean;
  primaryLabel: string;
  primaryEmphasis?: boolean;
  showBack?: boolean;
  backDisabled?: boolean;
  onClose: () => void;
  onPrimary: () => void;
  onBack?: () => void;
}) {
  const t = useT();
  return (
    <footer className="add-provider-foot">
      <button type="button" className="btn btn-ghost" onClick={onClose}>{t("common.cancel")}</button>
      {showBack && onBack && (
        <button type="button" className="btn btn-ghost" disabled={backDisabled} onClick={onBack}>
          {t("modal.back")}
        </button>
      )}
      {!hidePrimary && (
        <button
          type="button"
          className={`btn btn-primary${primaryEmphasis ? " add-provider-continue" : ""}`}
          disabled={primaryDisabled}
          onClick={onPrimary}
        >
          {primaryLabel}
        </button>
      )}
    </footer>
  );
}
