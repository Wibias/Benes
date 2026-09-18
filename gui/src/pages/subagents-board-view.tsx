/** Presentational shell for the Subagents board after resources have settled. */
import { Notice, ToastNotice } from "../ui";
import type { TFn } from "../i18n/shared";
import SubagentsWorkspace from "../components/subagents-workspace/SubagentsWorkspace";
import type {
  DelegationModelOption,
  DelegationPatch,
  UltraModePatch,
  UltraModeState,
} from "./subagents-delegation-contract.ts";

export type SubagentsBoardViewModel = {
  t: TFn;
  toast: { message: string; ok: boolean; dismiss: () => void };
  loadErrorVisible: boolean;
  roster: {
    available: string[];
    chosen: string[];
    reorder: (models: string[]) => void;
    refresh: () => void;
  };
  fallbacks: {
    ids: string[];
    busy: boolean;
    save: (models: string[]) => void;
  };
  delegation: {
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
  };
};

function renderToastNode(model: SubagentsBoardViewModel) {
  if (!model.toast.message) return null;
  return (
    <ToastNotice tone={model.toast.ok ? "ok" : "err"} onDismiss={model.toast.dismiss} dismissLabel={model.t("common.close")}>
      {model.toast.message}
    </ToastNotice>
  );
}

function renderLoadErrorNode(model: SubagentsBoardViewModel) {
  if (!model.loadErrorVisible) return null;
  return <Notice tone="err">{model.t("sub.loadFail")}</Notice>;
}

export function SubagentsBoardView(model: SubagentsBoardViewModel) {
  const toastNode = renderToastNode(model);
  const errorNode = renderLoadErrorNode(model);

  return (
    <>
      {toastNode}
      {errorNode}
      <SubagentsWorkspace
        available={model.roster.available}
        chosen={model.roster.chosen}
        onReorder={model.roster.reorder}
        onRefresh={model.roster.refresh}
        fallbacks={model.fallbacks.ids}
        fallbackBusy={model.fallbacks.busy}
        onFallbacks={model.fallbacks.save}
        delegation={model.delegation}
      />
    </>
  );
}

export function SubagentsBoardColdFailure(props: {
  t: TFn;
  reason: string;
  onRetry: () => void;
}) {
  return (
    <>
      <Notice tone="err">{props.reason}</Notice>
      <button type="button" className="btn btn-ghost btn-sm" onClick={props.onRetry}>
        {props.t("common.retry")}
      </button>
    </>
  );
}
