export type StarState = "starred" | "not-starred" | "unauthenticated";

export interface StarStatus {
  state?: StarState;
  url?: string;
}

export interface UpdateBadge {
  updateAvailable?: boolean;
  latestVersion?: string | null;
  /** True when no cached registry answer exists, so "no update" is unproven. */
  unknown?: boolean;
}

export type StarOverride = { state: StarState; basedOn: StarState | null };

export function resolvedStarState(
  polledState: StarState | null,
  override: StarOverride | null,
): StarState {
  if (override !== null && override.basedOn === polledState) return override.state;
  return polledState ?? "not-starred";
}

export function starClickMode(starState: StarState, starring: boolean): "ignore" | "open-repo" | "post" {
  if (starState === "starred" || starring) return "ignore";
  if (starState === "unauthenticated") return "open-repo";
  return "post";
}

export function starOverrideFromResponse(
  data: (StarStatus & { ok?: boolean }) | null,
  polledState: StarState | null,
): { override: StarOverride | null; openRepo: boolean } {
  if (data?.ok === true) return { override: { state: "starred", basedOn: polledState }, openRepo: false };
  return {
    override: data?.state ? { state: data.state, basedOn: polledState } : null,
    openRepo: true,
  };
}

export function updateAvailableFromBadge(badge: UpdateBadge | null | undefined): boolean {
  return badge?.updateAvailable === true;
}
