import { applyHarnessRemote, loadHarnessBoard } from "../pages/harnesses/live.ts";
import { applyHarnessModelLists, harnessesEligibleForModelSync } from "./provider-harness-sync.ts";
import { harnessSyncResultKey } from "./provider-notice-policy.ts";

export async function applyEnabledHarnessModelLists(apiBase: string): Promise<{ attempted: number; failed: number }> {
  const harnesses = await loadHarnessBoard(apiBase);
  return applyHarnessModelLists(
    harnessesEligibleForModelSync(harnesses),
    (id) => applyHarnessRemote(apiBase, id),
  );
}

export async function syncEnabledHarnessModelLists(apiBase: string): Promise<{
  ok: boolean;
  messageKey: ReturnType<typeof harnessSyncResultKey>;
}> {
  try {
    const result = await applyEnabledHarnessModelLists(apiBase);
    return { ok: result.failed === 0, messageKey: harnessSyncResultKey(result) };
  } catch {
    return { ok: false, messageKey: "prov.harnessSyncFail" };
  }
}
