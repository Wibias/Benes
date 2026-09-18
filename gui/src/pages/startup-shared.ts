/**
 * Control board payload contracts for the listener's startup plane.
 *
 * `/api/startup-health` answers in two generations: the current diagnostic row and the
 * older `ok`/`proxy` summary a stale listener still serves. Both normalize into one
 * `StartupHealthData`, so the board never branches on the payload generation.
 */
import type { TKey } from "../i18n/shared";

export type StartupStatus = "native" | "protected" | "at-risk";
export type StartupProtection = "service" | "shim" | "none";
export type StartupRoutingKind =
  | "native"
  | "benes-local"
  | "custom-local"
  | "custom-remote"
  | "unknown";
export type StartupShimCoverage = "full" | "cli-only" | "none";
export type StartupInstallAction = "install-service" | "install-shim";

/** Copyable recovery commands; their text belongs to the listener, not the board. */
export type StartupCommands = {
  installService: string;
  repairService: string;
  installShim: string;
  restoreNative: string;
};

/**
 * The boolean half of the diagnostic row. Listing it once means a new listener flag is a
 * single entry here instead of a field repeated across the type, the stub, and the tests.
 */
const STARTUP_FLAGS = [
  "routingInjected",
  "localRoutingDependency",
  "autostartEnabled",
  "rebootSafe",
  "serviceInstalled",
  "serviceViable",
  "serviceEnabled",
  "serviceRunning",
  "serviceStale",
  "serviceConflict",
  "serviceSupported",
  "shimInstalled",
  "shimHealthy",
  "diagnosticStale",
] as const;

export type StartupFlag = (typeof STARTUP_FLAGS)[number];
type StartupFlagValues = Record<StartupFlag, boolean>;

export type StartupHealthData = StartupFlagValues & {
  status: StartupStatus;
  routingKind: StartupRoutingKind;
  protection: StartupProtection;
  shimCoverage: StartupShimCoverage;
  platform: string;
  recommendedCommand: string | null;
  commands: StartupCommands;
};

export type TrayStatusData = {
  supported: boolean;
  installed: boolean;
  running: boolean;
  stale: boolean;
  summary: string;
};

function asRecord(value: unknown): Record<string, unknown> | null {
  return typeof value === "object" && value !== null ? value as Record<string, unknown> : null;
}

function readText(row: Record<string, unknown>, key: string): string | null {
  const value = row[key];
  return typeof value === "string" ? value : null;
}

/** The current listener identifies itself with a status, a platform, and a command block. */
function isDiagnosticRow(row: Record<string, unknown>): boolean {
  const commands = asRecord(row.commands);
  if (!commands) return false;
  return readText(row, "status") !== null
    && readText(row, "routingKind") !== null
    && readText(row, "platform") !== null
    && readText(commands, "restoreNative") !== null;
}
const LEGACY_COMMANDS: StartupCommands = {
  installService: "benes service install",
  repairService: "benes service repair",
  installShim: "benes codex-shim install",
  restoreNative: "benes restore",
};

/**
 * The `ok`/`proxy` generation reports no per-service detail, so one boolean covers the
 * whole service group and the host's autostart is simply assumed to be on.
 */
function legacyFlagValues(ok: boolean, serving: boolean, shimInstalled: boolean): StartupFlagValues {
  return {
    routingInjected: serving,
    localRoutingDependency: serving,
    autostartEnabled: true,
    rebootSafe: ok,
    serviceInstalled: serving,
    serviceViable: serving,
    serviceEnabled: serving,
    serviceRunning: serving,
    serviceStale: false,
    serviceConflict: false,
    serviceSupported: true,
    shimInstalled,
    shimHealthy: shimInstalled,
    diagnosticStale: false,
  };
}

function readLegacyHealth(row: Record<string, unknown>): StartupHealthData | null {
  if (typeof row.ok !== "boolean" || typeof row.proxy !== "string") return null;
  const serving = row.proxy === "running";
  const shim = asRecord(row.shim);
  const shimInstalled = shim !== null && shim.installed === true;
  const browserAgent = typeof navigator === "undefined" ? "" : navigator.userAgent;
  return {
    ...legacyFlagValues(row.ok, serving, shimInstalled),
    status: row.ok && serving ? "protected" : "at-risk",
    routingKind: serving ? "benes-local" : "unknown",
    protection: shimInstalled ? "shim" : "none",
    shimCoverage: shimInstalled ? "cli-only" : "none",
    platform: /windows/i.test(browserAgent) ? "win32" : "linux",
    recommendedCommand: null,
    commands: { ...LEGACY_COMMANDS },
  };
}

export function parseStartupHealthData(value: unknown): StartupHealthData | null {
  const row = asRecord(value);
  if (!row) return null;
  if (isDiagnosticRow(row)) return row as unknown as StartupHealthData;
  return readLegacyHealth(row);
}

const TRAY_FLAGS = ["supported", "installed", "running", "stale"] as const;

export function isTrayStatusData(value: unknown): value is TrayStatusData {
  const row = asRecord(value);
  if (!row) return false;
  if (!TRAY_FLAGS.every(flag => typeof row[flag] === "boolean")) return false;
  return typeof row.summary === "string";
}

/**
 * The board's whole status vocabulary in one table. The hero badge, the hero summary, and
 * the protection chip are views of it, so a new status cannot reach only one of them.
 */
const BOARD_COPY = {
  status: {
    native: "startup.status.native",
    protected: "startup.status.protected",
    "at-risk": "startup.status.atRisk",
  },
  summary: {
    native: "startup.summary.native",
    protected: "startup.summary.protected",
    "at-risk": "startup.summary.atRisk",
  },
  protection: {
    service: "startup.protection.service",
    shim: "startup.protection.shim",
    none: "startup.protection.none",
  },
} as const satisfies {
  status: Record<StartupStatus, TKey>;
  summary: Record<StartupStatus, TKey>;
  protection: Record<StartupProtection, TKey>;
};

export const STATUS_KEYS: Record<StartupStatus, TKey> = { ...BOARD_COPY.status };
export const SUMMARY_KEYS: Record<StartupStatus, TKey> = { ...BOARD_COPY.summary };
export const PROTECTION_KEYS: Record<StartupProtection, TKey> = { ...BOARD_COPY.protection };