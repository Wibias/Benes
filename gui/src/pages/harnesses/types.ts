import type { TKey } from "../../i18n/shared";

export type HarnessId =
  | "claude-desktop"
  | "claude"
  | "codex"
  | "dsh"
  | "opencode"
  | "pi"
  | "prime"
  | "omp"
  | "hermes"
  | "openclaw"
  | "kimi"
  | "gajae"
  | "grok"
  | "mcode";

/**
 * Harnesses Benes reaches by writing a managed config file. These are the
 * clients the integration writer can address: it owns their config path, can
 * snapshot it, and can restore it.
 *
 * The native clients — Claude Code, Claude Desktop, Codex and Grok — are driven
 * through their own management endpoints instead, so they are deliberately not
 * file-managed identities. The order is the catalogue order the dashboard
 * lists them in.
 */
export const FILE_MANAGED_HARNESS_IDS = [
  "opencode",
  "pi",
  "prime",
  "omp",
  "hermes",
  "openclaw",
  "kimi",
  "gajae",
  "dsh",
  "mcode",
] as const satisfies readonly HarnessId[];

export type FileManagedHarnessId = (typeof FILE_MANAGED_HARNESS_IDS)[number];

export type HarnessIssue = "none" | "conflict" | "update-needed";
export type HarnessToken = "valid" | "expired" | "missing" | "none";
export type HarnessAuthKind = "none" | "oauth" | "api-key";
export type HarnessDetectMethod = "auto" | "manual";
export type HarnessFilterId =
  | "applied"
  | "not-applied"
  | "conflict"
  | "update-needed"
  | "not-installed";

export type HarnessCapabilityId =
  | "sendMessages"
  | "toolCalls"
  | "readFiles"
  | "writeFiles"
  | "shell"
  | "browser"
  | "mcp"
  | "sessions";

export type HarnessHistoryKind = "apply" | "disable" | "refresh" | "restore";

/**
 * Per-Harness sidecar policy is server-authoritative. These types mirror the
 * management API response; the dashboard never resolves policy itself and never
 * stores it in browser settings.
 */
export const HARNESS_SIDECAR_SELECTIONS = ["global", "enabled", "disabled"] as const;

export type HarnessSidecarSelection = (typeof HARNESS_SIDECAR_SELECTIONS)[number];
export type HarnessSidecarMode = Exclude<HarnessSidecarSelection, "global">;
export type HarnessSidecarModalityId = "webSearch" | "vision";
export type HarnessSidecarSource = "global" | "harness_override";

export interface HarnessSidecarConfigured {
  enabled: boolean;
  source: HarnessSidecarSource;
}

export interface HarnessSidecarModality {
  supported: boolean;
  override: HarnessSidecarMode | null;
  configured: HarnessSidecarConfigured | null;
}

export interface HarnessSidecarPolicy {
  identityStampable: boolean;
  webSearch: HarnessSidecarModality;
  vision: HarnessSidecarModality;
}

export interface HarnessAuth {
  kind: HarnessAuthKind;
  methodKey: TKey;
  clientId: string | null;
  scopes: string[];
  token: HarnessToken;
}

export interface HarnessSettings {
  autoDetect: boolean;
  autoApply: boolean;
  retainSnapshot: boolean;
  allowRestart: boolean;
}

/**
 * Server-projected summary of the model set a Harness carries into its own config, when the
 * listener publishes one. Benes decides model membership once, in the catalogue; a Harness
 * only reports how much of that projection is written, so nothing here is editable state.
 */
export interface HarnessModelRegistration {
  configPath: string;
  /** The managed region exists in that config. */
  present: boolean;
  /** Distinct model ids the catalogue offers. */
  catalogue: number;
  /** Distinct model ids the managed region carries. */
  registered: number;
  /** Present, carrying exactly the catalogue, and pointing at the listener that served this. */
  current: boolean;
  /**
   * Why the listener's last automatic write did not land, when it did not. This is diagnostic
   * process state, so a payload without it is not a decode failure.
   */
  lastAutomaticError: string | null;
}

export interface HarnessHistoryRow {
  id: string;
  kind: HarnessHistoryKind;
  snapshot: string | null;
  at: string;
  noteKey: TKey;
  restorable: boolean;
}

export interface HarnessRecord {
  id: HarnessId;
  nameKey: TKey;
  installed: boolean;
  applied: boolean;
  running: boolean | null;
  issue: HarnessIssue;
  drift: boolean;
  detectMethod: HarnessDetectMethod;
  detectPath: string | null;
  configPath: string | null;
  logPath: string | null;
  snapshotId: string | null;
  lastDetectedAt: string | null;
  lastAppliedAt: string | null;
  auth: HarnessAuth;
  capabilities: Record<HarnessCapabilityId, boolean>;
  settings: HarnessSettings;
  /** Server-projected sidecar policy; never part of locally persisted settings. */
  sidecarPolicy?: HarnessSidecarPolicy | null;
  /** Server-projected model registration; absent when the listener publishes none. */
  registration?: HarnessModelRegistration | null;
  history: HarnessHistoryRow[];
}

export type HarnessGroupId = "connected" | "available" | "not-installed";
