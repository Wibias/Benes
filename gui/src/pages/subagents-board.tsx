/** Benes-owned Subagents board orchestration. */
import { useT } from "../i18n/shared";
import { DataSurfaceSkeleton } from "../components/data-surface";
import { setClientResourceData } from "../client-resource";
import { useSubagentDelegation } from "./use-subagent-delegation";
import { useSubagentsPageFeedback } from "./subagents-page-feedback.ts";
import { useSubagentsFallbacks } from "./use-subagents-fallbacks.ts";
import { useSubagentsUltraMode } from "./use-subagents-ultra-mode.ts";
import { useSubagentsRosterPersist } from "./use-subagents-roster-persist.ts";
import { useSubagentsModelsResource } from "./use-subagents-models-resource.ts";
import { SubagentsBoardColdFailure, SubagentsBoardView } from "./subagents-board-view.tsx";

function buildDelegationBinding(
  delegation: ReturnType<typeof useSubagentDelegation>,
  ultra: ReturnType<typeof useSubagentsUltraMode>,
) {
  return {
    model: delegation.model,
    effort: delegation.effort,
    efforts: delegation.efforts,
    available: delegation.available,
    guidanceEnabled: delegation.guidanceEnabled,
    syncCodexDefaults: delegation.syncCodexDefaults,
    saving: delegation.saving,
    onSave: (patch: Parameters<typeof delegation.save>[0]) => { void delegation.save(patch); },
    ultraMode: ultra.ultraMode,
    ultraSaving: ultra.ultraSaving,
    onUltraModeSave: (patch: Parameters<typeof ultra.saveUltraMode>[0]) => { void ultra.saveUltraMode(patch); },
    ultraLoadFailed: ultra.ultraLoadFailed,
    onUltraModeRetry: () => { void ultra.retryUltraMode(); },
  };
}

export function SubagentsBoard({ apiBase }: { apiBase: string }) {
  const t = useT();
  const feedback = useSubagentsPageFeedback();
  const delegation = useSubagentDelegation(apiBase);
  const ultra = useSubagentsUltraMode(apiBase, t, feedback.showToast, feedback.setStatus);
  const fallbackBoard = useSubagentsFallbacks(apiBase, t, feedback.showToast);
  const models = useSubagentsModelsResource(apiBase);

  const roster = useSubagentsRosterPersist({
    apiBase,
    cacheKey: models.cacheKey,
    available: models.available,
    setChosen: models.setChosen,
    showToast: feedback.showToast,
    setStatus: feedback.setStatus,
    refresh: () => { void models.resource.refresh(); },
    t,
    inFlightRef: models.persistGate,
  });

  const refreshAll = async () => {
    feedback.setStatus("");
    try {
      const next = await models.loadSubagents();
      setClientResourceData(models.cacheKey, next);
      await Promise.all([
        fallbackBoard.reloadFallbacks().catch(() => fallbackBoard.setFallbacks([])),
        ultra.loadUltraMode().catch(() => undefined),
        delegation.reload(),
      ]);
      feedback.showToast(true, t("sub.refreshed"));
    } catch (error) {
      feedback.showToast(false, error instanceof Error && error.message ? error.message : t("sub.refreshFail"));
      models.resource.refresh();
    }
  };

  if (models.resource.state.showSkeleton && !models.snapshot) {
    return <DataSurfaceSkeleton label={t("sub.loading")} rows={4} />;
  }

  if (models.resource.state.kind === "failed-cold") {
    const reason = models.resource.state.error instanceof Error
      ? models.resource.state.error.message
      : t("sub.loadFail");
    return <SubagentsBoardColdFailure t={t} reason={reason} onRetry={() => models.resource.refresh()} />;
  }

  return (
    <SubagentsBoardView
      t={t}
      toast={{ message: feedback.status, ok: feedback.ok, dismiss: feedback.clearStatus }}
      loadErrorVisible={models.resource.state.showError}
      roster={{
        available: models.available,
        chosen: models.chosen,
        reorder: roster.reorder,
        refresh: () => { void refreshAll(); },
      }}
      fallbacks={{
        ids: fallbackBoard.fallbacks,
        busy: fallbackBoard.fallbackBusy,
        save: (ids) => { void fallbackBoard.saveFallbacks(ids); },
      }}
      delegation={buildDelegationBinding(delegation, ultra)}
    />
  );
}
