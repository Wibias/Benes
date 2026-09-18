import { useEffect, useState } from "react";
import type { Locale } from "../../i18n/shared";
import { performLogGuardAction, scopedForGeneration } from "./log-guard-policy";
import type { CodexLogGuardAction, CodexLogGuardReport } from "./types";

const API_BASE = import.meta.env.VITE_API_BASE || "";

type Scoped<T> = { generation: number; value: T };

export function useLogGuard(generation: number, locale: Locale, externalBusy: boolean, onLogGuardAction?: (action: CodexLogGuardAction) => void) {
  const [fetched, setFetched] = useState<Scoped<CodexLogGuardReport> | null>(null);
  const [inspectFailed, setInspectFailed] = useState<Scoped<string> | null>(null);
  const [override, setOverride] = useState<Scoped<CodexLogGuardReport> | null>(null);
  const [internalBusy, setInternalBusy] = useState(false);
  const [error, setError] = useState<Scoped<string> | null>(null);
  const [compaction, setCompaction] = useState<Scoped<string> | null>(null);

  useEffect(() => {
    const controller = new AbortController();
    void (async () => {
      try {
        const res = await fetch(`${API_BASE}/api/storage/codex-logs`, { signal: controller.signal });
        if (!res.ok) {
          setInspectFailed({ generation, value: "inspect_failed" });
          setFetched(null);
          return;
        }
        setFetched({ generation, value: await res.json() as CodexLogGuardReport });
        setInspectFailed(null);
      } catch (caught) {
        if (controller.signal.aborted) return;
        void caught;
        setInspectFailed({ generation, value: "inspect_failed" });
        setFetched(null);
      }
    })();
    return () => controller.abort();
  }, [generation]);

  const runAction = (action: CodexLogGuardAction) => {
    if (onLogGuardAction) {
      onLogGuardAction(action);
      return;
    }
    if (internalBusy) return;
    void (async () => {
      setInternalBusy(true);
      setError(null);
      if (action.action === "compact") setCompaction(null);
      const result = await performLogGuardAction({ apiBase: API_BASE, action, locale });
      if (result.kind === "error") setError({ generation, value: result.message });
      else {
        if (result.report) setOverride({ generation, value: result.report });
        if (result.compaction) setCompaction({ generation, value: result.compaction });
      }
      setInternalBusy(false);
    })();
  };

  return {
    report: scopedForGeneration(override, generation) ?? scopedForGeneration(fetched, generation),
    inspectFailed: scopedForGeneration(inspectFailed, generation),
    error: scopedForGeneration(error, generation),
    compaction: scopedForGeneration(compaction, generation),
    busy: externalBusy || internalBusy,
    runAction,
  };
}
