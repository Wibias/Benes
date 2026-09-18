/**
 * Field-level patch application for the vision sidecar settings.
 *
 * A patch carries only what the user changed, so `undefined` means "keep the stored value"
 * while `backend: null` means "forget the stored backend". Only the fields
 * `PUT /api/sidecar-settings` acts on can be patched here: the listener keeps `reasoning`,
 * `timeoutMs`, and `maxDescriptionsPerTurn` as stored configuration but ignores them on PUT,
 * so an overlay built here must not imply a save changed them.
 */
import type { SidecarBackend, SidecarSetting } from "./control-board-settings";

/** Scalars a patch may replace; `backend` is separate because `null` clears it. */
const PATCHED_SCALARS = ["model", "enabled"] as const;

type PatchedScalar = (typeof PATCHED_SCALARS)[number];

/** A patch carries only the fields the user touched, so every scalar is optional. */
export type SidecarSettingUpdate = Partial<Pick<SidecarSetting, PatchedScalar>> & {
  backend?: SidecarBackend | null;
};

function replacementScalars(update: SidecarSettingUpdate): Partial<SidecarSetting> {
  const replacements: Partial<SidecarSetting> = {};
  for (const field of PATCHED_SCALARS) {
    const value = update[field];
    if (value !== undefined) Object.assign(replacements, { [field]: value });
  }
  return replacements;
}

/** Defined scalars replace; an omitted backend stays, and `null` clears it. */
export function mergeSidecarSetting(
  current: SidecarSetting,
  update?: SidecarSettingUpdate,
): SidecarSetting {
  if (!update) return { ...current };
  const merged: SidecarSetting = { ...current, ...replacementScalars(update) };
  if (update.backend === null) {
    delete merged.backend;
    return merged;
  }
  if (update.backend !== undefined) merged.backend = update.backend;
  return merged;
}
