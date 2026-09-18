/** Fallback-model list hook for the Subagents board. */
import { useCallback, useEffect, useState } from "react";
import type { TFn } from "../i18n/shared.ts";
import { loadSubagentFallbacks, saveSubagentFallbacks } from "./subagents-fallback-models.ts";

export function useSubagentsFallbacks(
  apiBase: string,
  t: TFn,
  showToast: (ok: boolean, message: string) => void,
) {
  const [fallbacks, setFallbacks] = useState<string[]>([]);
  const [fallbackBusy, setFallbackBusy] = useState(false);

  const reloadFallbacks = useCallback(async (signal?: AbortSignal) => {
    setFallbacks(await loadSubagentFallbacks(apiBase, t("sub.loadFail"), signal));
  }, [apiBase, t]);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        await reloadFallbacks(controller.signal);
      } catch {
        if (!controller.signal.aborted) setFallbacks([]);
      }
    })();
    return () => controller.abort();
  }, [reloadFallbacks]);

  const saveFallbacks = useCallback(async (models: string[]) => {
    if (fallbackBusy) return;
    setFallbackBusy(true);
    try {
      setFallbacks(await saveSubagentFallbacks(apiBase, models, t("sub.saveFailed")));
    } catch (error) {
      showToast(false, error instanceof Error && error.message ? error.message : t("sub.networkError"));
    } finally {
      setFallbackBusy(false);
    }
  }, [apiBase, fallbackBusy, showToast, t]);

  return {
    fallbacks,
    fallbackBusy,
    setFallbacks,
    reloadFallbacks,
    saveFallbacks,
  };
}
