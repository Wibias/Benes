/**
 * Decision copy and recovery notices for the Control page.
 *
 * Every export here answers one question about the current startup-health row: which
 * label the board shows, which repair the page offers, or whether a failed install is
 * still worth reporting. The tables keep the precedence rules in one place so the board
 * and the hero cannot disagree.
 */
import type { TFn, TKey } from "../i18n/shared";
import { startupRiskDetailKey } from "../startup-health-ui.ts";
import type {
  StartupHealthData,
  StartupInstallAction,
  StartupRoutingKind,
  StartupStatus,
  TrayStatusData,
} from "./startup-shared";

export type CodexRuntimeSettings = {
  version?: string | null;
  newerAvailable?: { path?: string; version?: string | null } | null;
  catalogClamp?: { active?: boolean; removedEfforts?: string[]; runtimeVersion?: string | null };
};

export type StartupInstallResult = {
  kind: "success" | "error";
  action: StartupInstallAction;
  repair?: boolean;
  detail?: string;
  forLocalRouting?: boolean;
};

export type TrayAction = "install" | "start" | "stop" | "uninstall";

export type TrayBadgeKind = "loading" | "unavailable" | "running" | "stale" | "stopped" | "notInstalled";

/** Windows PowerShell 5.x rejects `&&`; `;` is accepted by PowerShell and cmd. */
export function shellChain(commands: string[], platform: string | undefined): string {
  return commands.join(platform === "win32" ? "; " : " && ");
}

const ROUTING_KEYS: Record<StartupRoutingKind, TKey> = {
  native: "startup.routing.native",
  "benes-local": "startup.routing.proxy",
  "custom-local": "startup.routing.customLocal",
  "custom-remote": "startup.routing.customRemote",
  unknown: "startup.routing.unknown",
};

export function startupRoutingKey(kind: StartupRoutingKind): TKey {
  return ROUTING_KEYS[kind];
}

const STATUS_TONES: Record<StartupStatus, "risk" | "safe" | "native"> = {
  native: "native",
  protected: "safe",
  "at-risk": "risk",
};

/** A failed read outranks the last known status: stale data is itself a risk. */
export function startupHeroTone(failed: boolean, status: StartupStatus): "risk" | "safe" | "native" {
  return failed ? "risk" : STATUS_TONES[status];
}

export function startupHeroDetailKey(failed: boolean, data: StartupHealthData): TKey {
  if (failed) return "startup.staleData";
  return data.status === "at-risk" ? startupRiskDetailKey(data) : "startup.safeDetail";
}

type ServiceRule = { when: (data: StartupHealthData) => boolean; key: TKey };

const SERVICE_RULES: ServiceRule[] = [
  { when: data => data.serviceConflict, key: "startup.conflict" },
  { when: data => data.serviceStale, key: "startup.stale" },
  { when: data => data.serviceInstalled, key: "startup.unhealthy" },
  { when: data => data.serviceSupported, key: "startup.notInstalled" },
  { when: () => true, key: "startup.unsupported" },
];

export function serviceStateKey(data: StartupHealthData): TKey {
  const rule = SERVICE_RULES.find(candidate => candidate.when(data)) ?? SERVICE_RULES[SERVICE_RULES.length - 1];
  return rule ? rule.key : "startup.unsupported";
}

export function shimYesKey(data: StartupHealthData): TKey {
  return data.shimCoverage === "cli-only" ? "startup.cliOnly" : "startup.healthy";
}

export function shimNoKey(data: StartupHealthData): TKey {
  if (!data.shimInstalled) return "startup.notInstalled";
  return data.shimHealthy && !data.autostartEnabled ? "startup.installedDisabled" : "startup.stale";
}

const INSTALL_MESSAGES: Record<StartupInstallAction, { first: TKey; repaired: TKey }> = {
  "install-service": { first: "startup.serviceInstalled", repaired: "startup.serviceRepaired" },
  "install-shim": { first: "startup.shimInstalled", repaired: "startup.shimRepaired" },
};

export function installResultMessageKey(result: StartupInstallResult): TKey {
  if (result.kind !== "success") return "startup.installFailed";
  const messages = INSTALL_MESSAGES[result.action];
  return result.repair ? messages.repaired : messages.first;
}

const SATISFIED_INSTALLS: Record<StartupInstallAction, (next: StartupHealthData) => boolean> = {
  "install-service": next => next.serviceViable,
  "install-shim": next => next.shimInstalled && next.shimHealthy,
};

/**
 * A failed install stays on screen until the health row proves it landed — or until the
 * listener reports Benes-native routing, which retires a local-routing failure.
 */
export function retireFailedInstallResult(
  current: StartupInstallResult | null,
  next: StartupHealthData,
): StartupInstallResult | null {
  if (current?.kind !== "error") return current;
  if (next.status === "native" && current.forLocalRouting === true) return null;
  return SATISFIED_INSTALLS[current.action](next) ? null : current;
}

export function startupTrayActions(
  tray: TrayStatusData | null,
  trayLoading: boolean,
  trayError: boolean,
): Record<TrayAction, boolean> {
  const actions: Record<TrayAction, boolean> = { install: false, start: false, stop: false, uninstall: false };
  if (trayLoading || trayError || !tray) return actions;
  actions.install = !tray.installed && !tray.stale;
  actions.start = tray.installed && !tray.stale && !tray.running;
  actions.stop = tray.running && !tray.stale;
  actions.uninstall = tray.installed || tray.stale;
  return actions;
}

function trayBadgeFor(tray: TrayStatusData): TrayBadgeKind {
  if (tray.running && !tray.stale) return "running";
  if (tray.stale) return "stale";
  return tray.installed ? "stopped" : "notInstalled";
}

export function startupTrayBadgeKind(
  tray: TrayStatusData | null,
  trayLoading: boolean,
  trayError: boolean,
): TrayBadgeKind {
  if (trayLoading) return "loading";
  if (trayError || !tray) return "unavailable";
  return trayBadgeFor(tray);
}

function runtimeVersion(runtime: CodexRuntimeSettings, clamped: boolean): string {
  const clampedVersion = runtime.catalogClamp?.runtimeVersion;
  return (clamped ? clampedVersion : runtime.version) ?? runtime.version ?? "unknown";
}

function doctorSyncFix(platform: string | undefined): string {
  return shellChain(["benes doctor --fix-codex-runtime", "benes sync"], platform);
}

/**
 * A clamped Codex catalog hides reasoning efforts the binary cannot serve; a newer binary
 * on disk needs the doctor to move the pinned runtime. Neither state changes routing, so
 * both are notices with a copyable fix rather than blocking errors.
 */
export function deriveCodexRuntimeNotice(
  runtime: CodexRuntimeSettings | undefined,
  t: TFn,
  platform?: string,
): { warning: string | null; fix: string | null } {
  if (!runtime) return { warning: null, fix: null };
  const clamped = runtime.catalogClamp?.active === true;
  const newerOnDisk = Boolean(runtime.newerAvailable);
  if (!clamped && !newerOnDisk) return { warning: null, fix: null };

  const version = runtimeVersion(runtime, clamped);
  if (!clamped) {
    return { warning: t("startup.codexRuntime.olderBinary", { version }), fix: doctorSyncFix(platform) };
  }

  const efforts = (runtime.catalogClamp?.removedEfforts ?? []).join(", ");
  return {
    warning: efforts
      ? t("startup.codexRuntime.clampHiddenWithEfforts", { version, efforts })
      : t("startup.codexRuntime.clampHidden", { version }),
    fix: newerOnDisk ? doctorSyncFix(platform) : "benes sync",
  };
}