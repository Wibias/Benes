/** Ultra-mode resource hook for the Subagents board (reducer-owned state). */
import { useCallback, useEffect, useReducer, useRef } from "react";
import type { TFn } from "../i18n/shared.ts";
import type { UltraModePatch } from "./subagents-delegation-contract.ts";
import { fetchUltraModeState } from "./subagents-ultra-mode.ts";
import {
  IDLE_ULTRA_MODE,
  isStaleUltraGeneration,
  reduceUltraBoard,
} from "./subagents-ultra-board-state.ts";
import { runUltraModeRetry, runUltraModeSave } from "./subagents-ultra-commands.ts";

export function useSubagentsUltraMode(
  apiBase: string,
  t: TFn,
  showToast: (ok: boolean, message: string) => void,
  setStatus: (value: string | ((current: string) => string)) => void,
) {
  const [board, dispatch] = useReducer(reduceUltraBoard, {
    mode: IDLE_ULTRA_MODE,
    saving: false,
    loadFailed: false,
  });
  const loadGeneration = useRef(0);
  const boundApiBase = useRef(apiBase);

  useEffect(() => {
    boundApiBase.current = apiBase;
    loadGeneration.current += 1;
  }, [apiBase]);

  const loadUltraMode = useCallback(async (signal?: AbortSignal) => {
    if (boundApiBase.current !== apiBase) return false;
    const generation = ++loadGeneration.current;
    const next = await fetchUltraModeState(apiBase, t("sub.ultraModeLoadFail"), signal);
    if (isStaleUltraGeneration(signal, generation, loadGeneration.current, boundApiBase.current, apiBase)) {
      return false;
    }
    dispatch({ type: "hydrate", mode: next });
    return true;
  }, [apiBase, t]);

  useEffect(() => {
    const controller = new AbortController();
    void loadUltraMode(controller.signal).catch(() => {
      if (!controller.signal.aborted) {
        dispatch({ type: "load-failed" });
        showToast(false, t("sub.ultraModeLoadFail"));
      }
    });
    return () => { controller.abort(); };
  }, [loadUltraMode, showToast, t]);

  const saveUltraMode = useCallback(async (patch: UltraModePatch) => {
    if (board.saving) return;
    await runUltraModeSave({
      apiBase,
      patch,
      t,
      boundApiBase: boundApiBase.current,
      loadUltraMode,
      showToast,
      setStatus: (value) => setStatus(value),
      begin: () => dispatch({ type: "save-begin" }),
      end: () => dispatch({ type: "save-end" }),
    });
  }, [apiBase, board.saving, loadUltraMode, setStatus, showToast, t]);

  const retryUltraMode = useCallback(async () => {
    await runUltraModeRetry({
      t,
      loadUltraMode,
      showToast,
      setStatus,
      markFailed: () => dispatch({ type: "load-failed" }),
    });
  }, [loadUltraMode, setStatus, showToast, t]);

  return {
    ultraMode: board.mode,
    ultraSaving: board.saving,
    ultraLoadFailed: board.loadFailed,
    loadUltraMode,
    saveUltraMode,
    retryUltraMode,
  };
}
