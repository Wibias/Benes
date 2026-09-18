/** Benes dashboard client for the Go proxy (`internal/server`). */

/** Primitive kinds a declared wire field can have. */
type WireScalar = "number" | "string" | "boolean";

/** The record type described by a name-to-kind table. */
type FieldsOf<Declaration extends Record<string, WireScalar>> = {
  [Field in keyof Declaration]: Declaration[Field] extends "number"
    ? number
    : Declaration[Field] extends "boolean" ? boolean : string;
};

/** The textual log streams the output viewer can show, in toolbar order. */
export const DEBUG_STREAMS = ["provider", "usage", "injection"] as const;

export type LogStream = typeof DEBUG_STREAMS[number];

/**
 * One row per capture flag, in the order the capture panel renders them.
 *
 * `setting` is the key the server reports the flag's effective value under — the flag name
 * itself for every stream except the provider stream, which the server calls `enabled`.
 * `stream` is the textual log stream the flag gates, or `null` when it gates no stream.
 */
const CAPTURE_FLAGS = [
  { flag: "debug", setting: "enabled", stream: "provider" },
  { flag: "usage", setting: "usage", stream: "usage" },
  { flag: "injection", setting: "injection", stream: "injection" },
  { flag: "claude", setting: "claude", stream: null },
] as const satisfies ReadonlyArray<{ flag: string; setting: string; stream: LogStream | null }>;

type CaptureFlagRow = typeof CAPTURE_FLAGS[number];

export type DebugFlag = CaptureFlagRow["flag"];

/** The effective switch values the server reports, keyed by the settings key it uses. */
type DebugSwitchValues = Record<CaptureFlagRow["setting"], boolean>;

export interface DebugSettings extends DebugSwitchValues {
  runtimeOverride: Partial<Record<DebugFlag, boolean>>;
  env: Record<DebugFlag, boolean>;
}

/** Fields the server reports for one captured log line. */
const DEBUG_LOG_ENTRY_FIELDS = { seq: "number", at: "number", line: "string" } as const;

export type DebugLogEntry = FieldsOf<typeof DEBUG_LOG_ENTRY_FIELDS>;

/** Scalar fields the Claude inbound recorder always reports for a request. */
const CLAUDE_INBOUND_FIELDS = {
  id: "number",
  at: "number",
  endpoint: "string",
  model: "string",
  hasMetadataUserId: "boolean",
  hasSystem: "boolean",
} as const;

/** Shape the recorder attaches only when the inbound request actually carried it. */
type ClaudeInboundShape = Partial<Record<"thinkingType" | "outputConfigEffort", string>>
  & Partial<Record<"stream", boolean>>
  & Partial<Record<"metadataKeys", string[]>>;

/** Optional labels the recorder may attach to an inbound entry. */
type ClaudeTagFields = Partial<Record<"resolvedModel" | "anthropicBeta" | "userIdTag" | "systemTag", string>>;

/** Optional request-shape numbers the recorder may attach to an inbound entry. */
type ClaudeSizeFields = Partial<Record<"maxTokens" | "thinkingBudgetTokens", number>>;

export type ClaudeInboundEntry = FieldsOf<typeof CLAUDE_INBOUND_FIELDS>
  & ClaudeInboundShape
  & ClaudeTagFields
  & ClaudeSizeFields;

function captureFlagRow(flag: DebugFlag): CaptureFlagRow | undefined {
  return CAPTURE_FLAGS.find(candidate => candidate.flag === flag);
}

function clockTime(at: number): string {
  return new Date(at).toLocaleTimeString();
}

export function formatLogTime(at: number): string {
  return at > 0 ? `[${clockTime(at)}] ` : "";
}

export function formatClaudeInboundTime(at: number): string {
  return clockTime(at);
}

export function isDebugFlagEnabled(debug: DebugSettings, flag: DebugFlag): boolean {
  const row = captureFlagRow(flag);
  return row ? Boolean(debug[row.setting]) : false;
}

export function isStreamEnabled(debug: DebugSettings | null, stream: LogStream): boolean {
  if (!debug) return false;
  const row = CAPTURE_FLAGS.find(candidate => candidate.stream === stream);
  return row ? Boolean(debug[row.setting]) : false;
}

export function enabledTextualStreams(debug: DebugSettings | null): LogStream[] {
  if (!debug) return [];
  return DEBUG_STREAMS.filter(stream => isStreamEnabled(debug, stream));
}

export function isClaudeInboundApplicable(debug: { claude?: unknown } | null | undefined): boolean {
  return typeof debug?.claude === "boolean";
}

/**
 * Where a flag's effective value came from.
 *
 * A runtime override outranks the environment only when it disagrees with it: a value that
 * matches the environment flag is attributed to the environment either way, so the dashboard
 * never claims a runtime override that the effective value does not reveal. Without an
 * override the flag reads `env` only when the environment turned it on.
 */
export function debugFlagSource(debug: DebugSettings, flag: DebugFlag): "runtime" | "env" | null {
  const envOn = Boolean(debug.env[flag]);
  const override = debug.runtimeOverride[flag];
  if (override === undefined) return envOn && isDebugFlagEnabled(debug, flag) ? "env" : null;
  if (override !== envOn) return "runtime";
  return envOn ? "env" : null;
}

export function hasRuntimeOverrides(debug: DebugSettings | null | undefined): boolean {
  const overrides = debug?.runtimeOverride;
  return overrides ? Object.values(overrides).some(value => value !== undefined) : false;
}