import type { TKey } from "../i18n/shared";
import type { StartupHealthData, TrayStatusData } from "./startup-shared";
import { serviceStateKey, shimNoKey, shimYesKey } from "./startup-page-state.ts";

export function controlProtectionLabel(data: StartupHealthData): TKey {
  if (data.status === "protected") return "control.protected";
  if (data.protection === "none") return "startup.protection.none";
  return "control.protected";
}

export function controlServiceLabel(data: StartupHealthData): TKey {
  if (data.serviceRunning) return "control.running";
  if (data.serviceViable) return "control.running";
  if (!data.serviceInstalled) return "startup.notInstalled";
  return serviceStateKey(data);
}

export function controlShimLabel(data: StartupHealthData): TKey {
  if (data.shimHealthy) return shimYesKey(data) === "startup.healthy" ? "control.running" : shimYesKey(data);
  return shimNoKey(data);
}

export function routingModeOptions(
  current: StartupHealthData["routingKind"],
  labels: Record<StartupHealthData["routingKind"], string>,
): Array<{ value: string; label: string }> {
  const kinds: StartupHealthData["routingKind"][] = [
    "benes-local",
    "native",
    "custom-local",
    "custom-remote",
    "unknown",
  ];
  return kinds
    .filter(kind => kind === current || kind === "benes-local" || kind === "native")
    .map(kind => ({ value: kind, label: labels[kind] }));
}

export function nextTrayAction(tray: TrayStatusData | null): "install" | "start" | "stop" | null {
  if (!tray || !tray.installed) return "install";
  if (tray.running) return "stop";
  return "start";
}

export function traySwitchOn(tray: TrayStatusData | null): boolean {
  return Boolean(tray?.installed && tray.running && !tray.stale);
}
