/**
 * State and presentation for the sidebar's GitHub/update row.
 *
 * The row reads two independent things — whether the repository is starred, and whether an
 * update exists — and owns one interaction, the star click. Both are modelled here as plain
 * data so the row component is left with nothing but markup and event wiring, and so the
 * interaction and the projection can be asserted without a renderer.
 *
 * The business rules themselves stay in `sidebar-github-star.ts`. This module composes them
 * rather than restating them.
 */
import type { TFn } from "../i18n/shared.ts";
import {
  resolvedStarState,
  updateAvailableFromBadge,
  type StarOverride,
  type StarState,
  type StarStatus,
  type UpdateBadge,
} from "./sidebar-github-star.ts";

/** The star button's interaction state: idle, mid-request, or holding a fresh override. */
export interface StarInteraction {
  readonly busy: boolean;
  readonly override: StarOverride | null;
}

/** Nothing pressed, nothing decided. */
export const IDLE_STAR_INTERACTION: StarInteraction = { busy: false, override: null };

export type StarEvent =
  | { readonly kind: "request-started" }
  | { readonly kind: "response-decided"; readonly override: StarOverride | null }
  | { readonly kind: "request-settled" };

/**
 * The star button has one pending flag and one decided override, and the flag is cleared only
 * by the settle event. Modelling it as a reducer is what keeps a late response for an older
 * request from re-enabling the button underneath a newer one: the response event carries data,
 * never the pending flag.
 */
export function starInteraction(state: StarInteraction, event: StarEvent): StarInteraction {
  switch (event.kind) {
    case "request-started":
      return { busy: true, override: state.override };
    case "response-decided":
      return event.override === null ? state : { busy: state.busy, override: event.override };
    case "request-settled":
      return { busy: false, override: state.override };
  }
}

/** Everything the row renders, derived once. */
export interface GithubRowView {
  /** Where the GitHub link and a fallback star click both point. */
  readonly repositoryUrl: string;
  /** The raw polled state, which is what a star response's override is based on. */
  readonly polledStarState: StarState | null;
  readonly starState: StarState;
  readonly starred: boolean;
  readonly starDisabled: boolean;
  readonly updateAvailable: boolean;
  readonly latestVersion: string | null;
}

export interface GithubRowInput {
  readonly starStatus: StarStatus | null;
  readonly updateBadge: UpdateBadge | null;
  readonly interaction: StarInteraction;
  readonly fallbackRepositoryUrl: string;
}

export function githubRowView(input: GithubRowInput): GithubRowView {
  const polledStarState = input.starStatus?.state ?? null;
  const starState = resolvedStarState(polledStarState, input.interaction.override);
  const starred = starState === "starred";
  return {
    repositoryUrl: input.starStatus?.url ?? input.fallbackRepositoryUrl,
    polledStarState,
    starState,
    starred,
    starDisabled: starred || input.interaction.busy,
    updateAvailable: updateAvailableFromBadge(input.updateBadge),
    latestVersion: input.updateBadge?.latestVersion ?? null,
  };
}

/** Accessible name and tooltip for the star orb. */
export function starOrbLabel(view: GithubRowView, t: TFn): string {
  if (view.starred) return t("sidebar.starred");
  if (view.starState === "unauthenticated") return t("sidebar.starUnauthenticated");
  return t("sidebar.star");
}

/** Accessible name and tooltip for the update orb. */
export function updateOrbLabel(view: GithubRowView, t: TFn): string {
  if (view.updateAvailable && view.latestVersion !== null) {
    return t("sidebar.updateAvailable", { version: view.latestVersion });
  }
  return t("sidebar.checkUpdate");
}
