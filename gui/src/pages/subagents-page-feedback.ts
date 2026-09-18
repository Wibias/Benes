/** Page-level toast feedback timings for Subagents. */

import { useCallback, useEffect, useMemo, useState } from "react";
import {
  SUBAGENTS_TOAST_ERR_MS,
  SUBAGENTS_TOAST_OK_MS,
} from "./subagents-delegation-contract.ts";

type FeedbackSnapshot = {
  message: string;
  success: boolean;
  epoch: number;
};

export function useSubagentsPageFeedback() {
  const [snapshot, setSnapshot] = useState<FeedbackSnapshot>({
    message: "",
    success: false,
    epoch: 0,
  });

  const showToast = useCallback((nextOk: boolean, message: string) => {
    setSnapshot((prev) => ({
      message,
      success: nextOk,
      epoch: prev.epoch + 1,
    }));
  }, []);

  const clearStatus = useCallback(() => {
    setSnapshot((prev) => ({ ...prev, message: "" }));
  }, []);

  const setStatus = useCallback((value: string | ((current: string) => string)) => {
    setSnapshot((prev) => ({
      ...prev,
      message: typeof value === "function" ? value(prev.message) : value,
    }));
  }, []);

  useEffect(() => {
    if (!snapshot.message) return;
    const delay = snapshot.success ? SUBAGENTS_TOAST_OK_MS : SUBAGENTS_TOAST_ERR_MS;
    const timer = window.setTimeout(() => {
      setSnapshot((prev) => ({ ...prev, message: "" }));
    }, delay);
    return () => window.clearTimeout(timer);
  }, [snapshot.message, snapshot.success, snapshot.epoch]);

  return useMemo(() => ({
    status: snapshot.message,
    ok: snapshot.success,
    showToast,
    clearStatus,
    setStatus,
  }), [snapshot.message, snapshot.success, showToast, clearStatus, setStatus]);
}
