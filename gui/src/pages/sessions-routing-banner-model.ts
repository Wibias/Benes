/** Sessions hash-routing filter chip: copy + visibility only. Display uses raw ids. */

export const SESSIONS_ROUTING_BANNER_ROOT_CLASS = "sessions-routing-filter";
export const SESSIONS_ROUTING_BANNER_CLEAR_HASH = "sessions";

export type SessionsRoutingBannerKind = "absent" | "policy" | "combo";

export type SessionsRoutingBannerCopyKey =
  | "sessions.filter.routing.active.policy"
  | "sessions.filter.routing.active.combo";

export type SessionsRoutingBannerClearKey = "sessions.filter.routing.clear";

export type SessionsRoutingBannerProjection = {
  kind: SessionsRoutingBannerKind;
  /** Raw id shown in the active-filter sentence; empty when absent. */
  subjectId: string;
  sentenceKey: SessionsRoutingBannerCopyKey | null;
  clearKey: SessionsRoutingBannerClearKey;
  clearHash: typeof SESSIONS_ROUTING_BANNER_CLEAR_HASH;
};

const CLEAR_KEY: SessionsRoutingBannerClearKey = "sessions.filter.routing.clear";

/**
 * Policy wins when both ids are present. Empty strings are treated as unset.
 * Ids are not normalised — callers pass the hash/query value as stored.
 */
export function projectSessionsRoutingBanner(
  policyId: string,
  comboId: string,
): SessionsRoutingBannerProjection {
  if (policyId) {
    return {
      kind: "policy",
      subjectId: policyId,
      sentenceKey: "sessions.filter.routing.active.policy",
      clearKey: CLEAR_KEY,
      clearHash: SESSIONS_ROUTING_BANNER_CLEAR_HASH,
    };
  }
  if (comboId) {
    return {
      kind: "combo",
      subjectId: comboId,
      sentenceKey: "sessions.filter.routing.active.combo",
      clearKey: CLEAR_KEY,
      clearHash: SESSIONS_ROUTING_BANNER_CLEAR_HASH,
    };
  }
  return {
    kind: "absent",
    subjectId: "",
    sentenceKey: null,
    clearKey: CLEAR_KEY,
    clearHash: SESSIONS_ROUTING_BANNER_CLEAR_HASH,
  };
}

export function sessionsRoutingBannerIsVisible(
  projection: SessionsRoutingBannerProjection,
): boolean {
  return projection.kind !== "absent" && projection.sentenceKey !== null;
}
