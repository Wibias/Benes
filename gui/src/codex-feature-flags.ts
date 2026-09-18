/**
 * Benes dashboard client for the Go proxy (`internal/server`).
 * Pure response handling for the Codex feature-flag toggles behind
 * `GET`/`PUT /api/codex-auth/features/<flag>`.
 *
 * The toggle components own transport, polling, and optimistic flip; this module owns what
 * a response *means*. Keeping it here makes the meaning testable without a DOM, and keeps
 * the shapes in one place now that more than one flag uses the same endpoint family.
 */

import type { TFn, TKey } from "./i18n/shared";

/** Body of a feature-flag read or write. `unknown` where the listener may change shape. */
export interface FeatureFlagResponse {
  enabled?: unknown;
  changed?: unknown;
  ok?: unknown;
}

const SAVED_KEY = "codexAuth.requestUserInputUpdated";
const SAVED_RESTART_KEY = "codexAuth.requestUserInputUpdatedRestart";
const SAVE_FAILED_KEY = "codexAuth.requestUserInputUpdateFailed";

/**
 * `readJsonOrThrow` reports a failed response as `HTTP 500`. That is a status, not an
 * explanation, so it is replaced with the catalog sentence; anything else the transport
 * said (a DNS failure, a refused connection) is shown to the operator verbatim.
 */
const STATUS_ONLY_MESSAGE = /^HTTP \d{3}$/;

/** Only a literal `true` counts as enabled — an omitted field is not an opt-in. */
export function featureFlagEnabled(payload: FeatureFlagResponse): boolean {
  return payload.enabled === true;
}

/** The listener must confirm the write; a `200` with an unconfirmed body is not success. */
export function featureFlagWriteAccepted(payload: FeatureFlagResponse): boolean {
  return payload.ok === true;
}

/** Which success sentence fits the write: Codex needs a restart, or the change is live. */
export function featureFlagSavedMessageKey(payload: FeatureFlagResponse): TKey {
  return payload.changed === true ? SAVED_RESTART_KEY : SAVED_KEY;
}

/** Message for a failed write: the transport's own words when it had any, else the catalog text. */
export function featureFlagFailureMessage(t: TFn, error: unknown): string {
  if (error instanceof Error && error.message !== "" && !STATUS_ONLY_MESSAGE.test(error.message)) {
    return error.message;
  }
  return t(SAVE_FAILED_KEY);
}
