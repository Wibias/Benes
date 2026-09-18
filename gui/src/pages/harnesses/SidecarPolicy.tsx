import { useState } from "react";
import { useT, type TKey } from "../../i18n/shared";
import { describeRefusal } from "../integrations/refusal-copy";
import {
  harnessSidecarConfiguredKeys,
  harnessSidecarModalities,
  harnessSidecarRequestActive,
  harnessSidecarSelection,
  saveHarnessSidecarOverride,
  type HarnessSidecarSourceKey,
  type HarnessSidecarStateKey,
} from "./harness-api";
import {
  HARNESS_SIDECAR_SELECTIONS,
  type HarnessRecord,
  type HarnessSidecarModalityId,
  type HarnessSidecarSelection,
} from "./types";

const MODALITY_LABEL: Record<HarnessSidecarModalityId, TKey> = {
  webSearch: "harnesses.sidecar.webSearch",
  vision: "harnesses.sidecar.vision",
};

const SELECTION_LABEL: Record<HarnessSidecarSelection, TKey> = {
  global: "harnesses.sidecar.mode.global",
  enabled: "harnesses.sidecar.mode.enabled",
  disabled: "harnesses.sidecar.mode.disabled",
};

/**
 * Per-Harness sidecar policy. Everything rendered here comes from the server
 * response, and every edit is a patch for one modality plus a canonical refetch;
 * no local copy of the policy exists.
 */
export function SidecarPolicy({
  harness,
  apiBase,
  busy,
  onRefetch,
  onToast,
}: {
  harness: HarnessRecord;
  apiBase: string;
  busy: boolean;
  onRefetch: () => Promise<void> | void;
  onToast: (ok: boolean, text: string) => void;
}) {
  const t = useT();
  const [saving, setSaving] = useState<HarnessSidecarModalityId | null>(null);
  const policy = harness.sidecarPolicy;
  const modalities = harnessSidecarModalities(policy);
  if (!policy || modalities.length === 0) return null;
  const overridden = modalities.some((modality) => policy[modality].override !== null);

  const onSelect = async (modality: HarnessSidecarModalityId, selection: HarnessSidecarSelection) => {
    if (busy || saving) return;
    setSaving(modality);
    try {
      await saveHarnessSidecarOverride({
        apiBase,
        clientId: harness.id,
        modality,
        selection,
        reload: onRefetch,
      });
      onToast(true, t("harnesses.sidecar.saved"));
    } catch (error) {
      onToast(false, describeRefusal(t, error, t("harnesses.sidecar.saveFailed")));
    } finally {
      setSaving(null);
    }
  };

  return (
    <section className="harnesses-sidecar">
      <h4>{t("harnesses.sidecar.title")}</h4>
      <p className="harnesses-sidecar-hint">{t("harnesses.sidecar.hint")}</p>
      {overridden && !harnessSidecarRequestActive(harness) && (
        <p className="harnesses-sidecar-note">{t("harnesses.sidecar.notActive")}</p>
      )}
      {modalities.map((modality) => (
        <ModalityRow
          key={modality}
          modality={modality}
          selection={harnessSidecarSelection(policy, modality)}
          configuredKeys={harnessSidecarConfiguredKeys(policy, modality)}
          disabled={busy || saving !== null}
          onSelect={(selection) => void onSelect(modality, selection)}
        />
      ))}
    </section>
  );
}

function ModalityRow({
  modality,
  selection,
  configuredKeys,
  disabled,
  onSelect,
}: {
  modality: HarnessSidecarModalityId;
  selection: HarnessSidecarSelection;
  configuredKeys: { state: HarnessSidecarStateKey; source: HarnessSidecarSourceKey } | null;
  disabled: boolean;
  onSelect: (selection: HarnessSidecarSelection) => void;
}) {
  const t = useT();
  const label = t(MODALITY_LABEL[modality]);
  return (
    <div className="harnesses-sidecar-row">
      <div className="harnesses-sidecar-copy">
        <span className="harnesses-sidecar-title">{label}</span>
        {configuredKeys && (
          <span className="harnesses-sidecar-state">
            {t("harnesses.sidecar.configured", {
              state: t(configuredKeys.state),
              source: t(configuredKeys.source),
            })}
          </span>
        )}
      </div>
      <div className="segmented harnesses-sidecar-modes" role="radiogroup" aria-label={label}>
        {HARNESS_SIDECAR_SELECTIONS.map((option) => {
          const checked = option === selection;
          return (
            <button
              key={option}
              type="button"
              role="radio"
              aria-checked={checked}
              className={checked ? "btn btn-sm btn-primary" : "btn btn-sm btn-ghost"}
              disabled={disabled}
              onClick={() => onSelect(option)}
            >
              {t(SELECTION_LABEL[option])}
            </button>
          );
        })}
      </div>
    </div>
  );
}
