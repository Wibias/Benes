/** Chosen-roster persist orchestration for the Subagents page. */
import { useCallback, useRef, type MutableRefObject } from "react";
import type { TFn } from "../i18n/shared.ts";
import { FEATURED_MAX } from "../components/subagents-workspace/roster.ts";
import { persistChosenSubagentModels } from "./subagents-chosen-persist.ts";

export function useSubagentsRosterPersist(options: {
  apiBase: string;
  cacheKey: string;
  available: string[];
  setChosen: (models: string[]) => void;
  showToast: (ok: boolean, message: string) => void;
  setStatus: (value: string) => void;
  refresh: () => void;
  t: TFn;
  inFlightRef?: MutableRefObject<boolean>;
}) {
  const persistGen = useRef(0);
  const localInFlight = useRef(false);
  const inFlightRef = options.inFlightRef ?? localInFlight;

  const isPersistInFlight = useCallback(() => inFlightRef.current, [inFlightRef]);

  const persistChosen = useCallback(async (models: string[], toastOk = false) => {
    await persistChosenSubagentModels({
      apiBase: options.apiBase,
      cacheKey: options.cacheKey,
      available: options.available,
      models,
      toastOk,
      hooks: {
        getGeneration: () => persistGen.current,
        bumpGeneration: () => ++persistGen.current,
        setInFlight: (value) => { inFlightRef.current = value; },
        setChosen: options.setChosen,
        onSuccessToast: (n) => options.showToast(true, options.t("sub.saved", { n, cmd: "benes sync" })),
        onFailureToast: (message) => options.showToast(false, message),
        reload: options.refresh,
        networkErrorMessage: options.t("sub.networkError"),
        saveFailedMessage: options.t("sub.saveFailed"),
      },
    });
  }, [inFlightRef, options]);

  const reorder = useCallback((models: string[]) => {
    const next = models.slice(0, FEATURED_MAX);
    options.setStatus("");
    options.setChosen(next);
    void persistChosen(next, true);
  }, [options, persistChosen]);

  return { persistChosen, reorder, isPersistInFlight, FEATURED_MAX };
}
