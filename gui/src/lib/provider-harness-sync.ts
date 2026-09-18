import type { HarnessId, HarnessIssue } from "../pages/harnesses/types.ts";

export type HarnessSyncTarget = {
  id: HarnessId;
  applied: boolean;
  issue: HarnessIssue;
};

export function harnessesEligibleForModelSync(harnesses: readonly HarnessSyncTarget[]): HarnessId[] {
  return harnesses.filter((row) => row.applied && row.issue !== "conflict").map((row) => row.id);
}

export async function applyHarnessModelLists(
  ids: readonly HarnessId[],
  apply: (id: HarnessId) => Promise<void>,
): Promise<{ attempted: number; failed: number }> {
  const results = await Promise.allSettled(ids.map((id) => apply(id)));
  return {
    attempted: ids.length,
    failed: results.filter((row) => row.status === "rejected").length,
  };
}
