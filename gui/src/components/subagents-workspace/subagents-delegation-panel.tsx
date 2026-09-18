/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Delegation settings for the Subagents tab — composition only.
 */
import { useT } from "../../i18n/shared";
import {
  ULTRA_MODE_PRESET,
  ultraModeHintActive,
  type DelegationModelOption,
  type DelegationPatch,
  type UltraModePatch,
  type UltraModeState,
} from "../../pages/subagents-delegation-contract.ts";
import {
  DelegationLoadFailed,
  DelegationModelSlice,
  DelegationPoliciesSlice,
  UltraModeEditor,
} from "./delegation-slices";

export type DelegationSectionSlice = "model" | "policies" | "editor";

export interface SubagentsDelegationPanelProps {
  model: string;
  effort: string;
  efforts: string[];
  available: DelegationModelOption[];
  guidanceEnabled: boolean;
  syncCodexDefaults: boolean;
  saving: boolean;
  onSave: (patch: DelegationPatch) => void;
  ultraMode: UltraModeState;
  ultraSaving: boolean;
  onUltraModeSave: (patch: UltraModePatch) => void;
  ultraLoadFailed: boolean;
  onUltraModeRetry: () => void;
  compact?: boolean;
  slices?: DelegationSectionSlice[];
}

export { ULTRA_MODE_PRESET };

const ALL_SLICES: DelegationSectionSlice[] = ["model", "policies", "editor"];

function resolveVisibleSlices(requested: DelegationSectionSlice[] | undefined): Set<DelegationSectionSlice> {
  return new Set(requested && requested.length > 0 ? requested : ALL_SLICES);
}

function panelRootClass(compact: boolean): string {
  return compact ? "swi-delegation swi-delegation--compact" : "swi-delegation";
}

function editorLabels(t: ReturnType<typeof useT>) {
  return {
    text: t("sub.ultraModeText"),
    preset: t("sub.ultraModePreset"),
    save: t("common.save"),
  };
}

export function SubagentsDelegationPanel(props: SubagentsDelegationPanelProps) {
  const t = useT();
  const compact = props.compact === true;
  const visible = resolveVisibleSlices(props.slices);
  const editorOpen = ultraModeHintActive(props.ultraMode.hintText);
  const rootClass = panelRootClass(compact);

  return (
    <div className={rootClass}>
      {props.ultraLoadFailed && visible.has("policies") ? (
        <DelegationLoadFailed compact={compact} onRetry={props.onUltraModeRetry} />
      ) : null}
      {visible.has("model") ? (
        <DelegationModelSlice
          model={props.model}
          effort={props.effort}
          efforts={props.efforts}
          available={props.available}
          saving={props.saving}
          onSave={props.onSave}
        />
      ) : null}
      {visible.has("policies") ? (
        <DelegationPoliciesSlice
          model={props.model}
          guidanceEnabled={props.guidanceEnabled}
          syncCodexDefaults={props.syncCodexDefaults}
          saving={props.saving}
          onSave={props.onSave}
          ultraMode={props.ultraMode}
          ultraSaving={props.ultraSaving}
          onUltraModeSave={props.onUltraModeSave}
          compact={compact}
        />
      ) : null}
      {visible.has("editor") && editorOpen ? (
        <div className="swi-delegation-row swi-ultra-mode-editor">
          <UltraModeEditor
            key={props.ultraMode.hintText ?? "empty-hint"}
            initialHint={props.ultraMode.hintText ?? ""}
            disabled={props.saving || props.ultraSaving}
            onSave={props.onUltraModeSave}
            preset={ULTRA_MODE_PRESET}
            compact={compact}
            labels={editorLabels(t)}
          />
        </div>
      ) : null}
    </div>
  );
}
