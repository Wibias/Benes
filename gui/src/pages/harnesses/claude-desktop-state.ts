/**
 * The Claude Desktop runtime contract as the Harnesses board consumes it.
 *
 * #255 / merged #328 are authoritative for every field here. The contract
 * reports one disposition instead of a set of booleans precisely because a
 * saved Benes preference must never be readable as applied native state, so
 * this module keeps desired enablement, applied state and staleness separate
 * all the way to the rendered sentence.
 *
 * Claude Desktop's supported local configuration exposes no inference or model
 * routing, so nothing here decodes, offers, or writes a model, family, endpoint
 * or credential field. There is no Claude Desktop model catalogue and there is
 * no fallback that could invent one.
 */
import type { TKey } from "../../i18n/shared";

export const CLAUDE_DESKTOP_LOAD_FAILED = "Could not read the Claude Desktop runtime status.";
export const CLAUDE_DESKTOP_SAVE_FAILED = "The desired state was refused.";
export const CLAUDE_DESKTOP_MUTATION_FAILED = "The native operation was refused.";

/** The complete set of dispositions the landed contract can report. */
export const CLAUDE_DESKTOP_STATES = [
  "unsupported_host",
  "not_installed",
  "config_unavailable",
  "no_managed_projection",
  "not_applied",
  "applied",
  "stale",
] as const;

export type ClaudeDesktopState = (typeof CLAUDE_DESKTOP_STATES)[number];

/** The exact machine-readable reason the runtime could not proceed. */
export interface ClaudeDesktopRefusal {
  code: string;
  message: string;
}

export interface ClaudeDesktopStatus {
  clientId: string;
  hostSupported: boolean;
  installed: boolean;
  configurable: boolean;
  /** The native configuration Benes may manage, absent when the host has none. */
  configPath: string | null;
  state: ClaudeDesktopState;
  /** False while Benes ships no projection to install. */
  managedProjectionAvailable: boolean;
  desiredEnabled: boolean;
  desiredFingerprint: string | null;
  observedKind: string;
  applied: boolean;
  appliedFingerprint: string | null;
  observedFingerprint: string | null;
  stale: boolean;
  restartRequired: boolean;
  refusal: ClaudeDesktopRefusal | null;
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function flag(value: unknown): boolean | null {
  return typeof value === "boolean" ? value : null;
}

function text(value: unknown): string | null {
  return typeof value === "string" && value.trim() ? value.trim() : null;
}

export function isClaudeDesktopState(value: unknown): value is ClaudeDesktopState {
  return typeof value === "string" && (CLAUDE_DESKTOP_STATES as readonly string[]).includes(value);
}

function decodeRefusal(value: unknown): ClaudeDesktopRefusal | null {
  if (!isRecord(value)) return null;
  const code = text(value.code);
  if (!code) return null;
  return { code, message: text(value.message) ?? code };
}

/**
 * Decodes the canonical status and fails closed on anything it cannot place.
 * An unrecognised disposition is refused rather than mapped onto a familiar
 * one, because the disposition is what decides which actions are offered.
 */
export function decodeClaudeDesktopStatus(payload: unknown): ClaudeDesktopStatus {
  if (!isRecord(payload)) throw new Error(CLAUDE_DESKTOP_LOAD_FAILED);
  if (!isClaudeDesktopState(payload.state)) throw new Error(CLAUDE_DESKTOP_LOAD_FAILED);
  const hostSupported = flag(payload.hostSupported);
  const installed = flag(payload.installed);
  const configurable = flag(payload.configurable);
  const managedProjectionAvailable = flag(payload.managedProjectionAvailable);
  const desiredEnabled = flag(payload.desiredEnabled);
  const applied = flag(payload.applied);
  const stale = flag(payload.stale);
  if (
    hostSupported === null ||
    installed === null ||
    configurable === null ||
    managedProjectionAvailable === null ||
    desiredEnabled === null ||
    applied === null ||
    stale === null
  ) {
    throw new Error(CLAUDE_DESKTOP_LOAD_FAILED);
  }
  return {
    clientId: text(payload.clientId) ?? "claude-desktop",
    hostSupported,
    installed,
    configurable,
    configPath: text(payload.configPath),
    state: payload.state,
    managedProjectionAvailable,
    desiredEnabled,
    desiredFingerprint: text(payload.desiredFingerprint),
    observedKind: text(payload.observedKind) ?? "",
    applied,
    appliedFingerprint: text(payload.appliedFingerprint),
    observedFingerprint: text(payload.observedFingerprint),
    stale,
    restartRequired: flag(payload.restartRequired) === true,
    refusal: decodeRefusal(payload.refusal),
  };
}

const STATE_KEY: Record<ClaudeDesktopState, TKey> = {
  unsupported_host: "harnesses.claudeDesktop.state.unsupportedHost",
  not_installed: "harnesses.claudeDesktop.state.notInstalled",
  config_unavailable: "harnesses.claudeDesktop.state.configUnavailable",
  no_managed_projection: "harnesses.claudeDesktop.state.noManagedProjection",
  not_applied: "harnesses.claudeDesktop.state.notApplied",
  applied: "harnesses.claudeDesktop.state.applied",
  stale: "harnesses.claudeDesktop.state.stale",
};

export function claudeDesktopStateKey(state: ClaudeDesktopState): TKey {
  return STATE_KEY[state];
}

/** Tone is a secondary cue; the sentence above it is what states the meaning. */
export type ClaudeDesktopTone = "ok" | "attention" | "pending" | "blocked";

export function claudeDesktopTone(status: ClaudeDesktopStatus): ClaudeDesktopTone {
  if (status.state === "applied") return "ok";
  if (status.state === "stale") return "attention";
  if (status.state === "not_applied") return "pending";
  return "blocked";
}

/**
 * Where the desired setting actually stands. "Saved" and "applied" are separate
 * answers, and the surface has to say which one is true.
 */
export type ClaudeDesktopDesire = "off" | "saved_not_applied" | "applied" | "stale";

export function claudeDesktopDesire(status: ClaudeDesktopStatus): ClaudeDesktopDesire {
  if (status.applied) return "applied";
  if (status.stale) return "stale";
  if (status.desiredEnabled) return "saved_not_applied";
  return "off";
}

const DESIRE_KEY: Record<ClaudeDesktopDesire, TKey> = {
  off: "harnesses.claudeDesktop.desire.off",
  saved_not_applied: "harnesses.claudeDesktop.desire.savedNotApplied",
  applied: "harnesses.claudeDesktop.desire.applied",
  stale: "harnesses.claudeDesktop.desire.stale",
};

export function claudeDesktopDesireKey(desire: ClaudeDesktopDesire): TKey {
  return DESIRE_KEY[desire];
}

export interface ClaudeDesktopActions {
  /** Desired enablement is storable here, so the control is offered. */
  setDesired: boolean;
  apply: boolean;
  reapply: boolean;
  disable: boolean;
}

/**
 * Which lifecycle actions the runtime can actually carry out.
 *
 * Apply is the first native projection, so it is offered only where nothing is
 * applied yet. Re-apply exists for the drift case. Nothing is offered while the
 * runtime ships no projection, because a button the runtime refuses is not a
 * workflow.
 */
export function claudeDesktopActions(status: ClaudeDesktopStatus): ClaudeDesktopActions {
  const configurable = status.configurable;
  const actionable = configurable && status.managedProjectionAvailable;
  return {
    setDesired: configurable,
    apply: actionable && status.desiredEnabled && status.state === "not_applied",
    reapply: actionable && status.desiredEnabled && status.state === "stale",
    disable: configurable && (status.applied || status.state === "stale"),
  };
}

/** Why the lifecycle actions are unavailable. Never inferred from colour. */
export type ClaudeDesktopBlocked =
  | "unsupported_host"
  | "not_installed"
  | "config_unavailable"
  | "no_projection"
  | "current"
  | "desired_off"
  | null;

export function claudeDesktopBlocked(status: ClaudeDesktopStatus): ClaudeDesktopBlocked {
  if (!status.hostSupported) return "unsupported_host";
  if (!status.installed) return "not_installed";
  if (status.state === "config_unavailable") return "config_unavailable";
  if (!status.managedProjectionAvailable) return "no_projection";
  if (status.applied) return "current";
  if (!status.desiredEnabled) return "desired_off";
  return null;
}

const BLOCKED_KEY: Record<Exclude<ClaudeDesktopBlocked, null>, TKey> = {
  unsupported_host: "harnesses.claudeDesktop.blocked.unsupportedHost",
  not_installed: "harnesses.claudeDesktop.blocked.notInstalled",
  config_unavailable: "harnesses.claudeDesktop.blocked.configUnavailable",
  no_projection: "harnesses.claudeDesktop.blocked.noProjection",
  current: "harnesses.claudeDesktop.blocked.current",
  desired_off: "harnesses.claudeDesktop.blocked.desiredOff",
};

export function claudeDesktopBlockedKey(blocked: Exclude<ClaudeDesktopBlocked, null>): TKey {
  return BLOCKED_KEY[blocked];
}

export type ClaudeDesktopMutation = "apply" | "disable";

export interface ClaudeDesktopMutationReport {
  changed: boolean;
  applied: boolean;
  restartRequired: boolean;
}

/**
 * Whether the canonical status confirms the native result of a mutation.
 *
 * A 2xx from the mutation endpoint is not confirmation: the status endpoint
 * derives applied state from the native document, and that is what decides
 * whether the surface may report success.
 */
export function claudeDesktopMutationConfirmed(
  mutation: ClaudeDesktopMutation,
  status: ClaudeDesktopStatus,
): boolean {
  if (mutation === "apply") return status.applied;
  return !status.applied && !status.stale;
}

/**
 * The only desired-state body this surface may send. Claude Desktop's supported
 * local configuration has no model, family, endpoint or credential surface, so
 * none of those keys exist here to be sent.
 */
export function claudeDesktopDesiredBody(enabled: boolean): { enabled: boolean } {
  return { enabled };
}

export const CLAUDE_DESKTOP_FORBIDDEN_DESIRED_KEYS = [
  "profile",
  "model",
  "models",
  "tierModels",
  "modelMap",
  "opus",
  "sonnet",
  "haiku",
  "fable",
  "baseUrl",
  "baseURL",
  "endpoint",
  "proxy",
  "apiKey",
  "key",
  "token",
  "authMode",
  "credentials",
  "systemEnv",
  "available",
] as const;
