import assert from "node:assert/strict";
import test from "node:test";

import {
  deriveCodexRuntimeNotice,
  installResultMessageKey,
  retireFailedInstallResult,
  serviceStateKey,
  shellChain,
  shimNoKey,
  shimYesKey,
  startupHeroDetailKey,
  startupHeroTone,
  startupRoutingKey,
  startupTrayActions,
  startupTrayBadgeKind,
} from "../src/pages/startup-page-state.ts";
import type { StartupHealthData } from "../src/pages/startup-shared.ts";

function health(overrides: Partial<StartupHealthData> = {}): StartupHealthData {
  return {
    status: "protected",
    routingKind: "benes-local",
    routingInjected: true,
    localRoutingDependency: true,
    autostartEnabled: true,
    rebootSafe: true,
    protection: "service",
    serviceInstalled: true,
    serviceViable: true,
    serviceEnabled: true,
    serviceRunning: true,
    serviceStale: false,
    serviceConflict: false,
    serviceSupported: true,
    shimInstalled: false,
    shimHealthy: false,
    shimCoverage: "none",
    platform: "linux",
    recommendedCommand: null,
    diagnosticStale: false,
    commands: {
      installService: "benes service install",
      repairService: "benes service repair",
      installShim: "benes shim install",
      restoreNative: "benes restore",
    },
    ...overrides,
  };
}

function t(key: string, vars?: Record<string, unknown>) {
  if (!vars) return key;
  return `${key}:${Object.entries(vars).map(([name, value]) => `${name}=${value}`).join(",")}`;
}

test("shell chains join with the separator the target shell accepts", () => {
  assert.equal(shellChain(["benes doctor", "benes sync"], "win32"), "benes doctor; benes sync");
  assert.equal(shellChain(["benes doctor", "benes sync"], "linux"), "benes doctor && benes sync");
  assert.equal(shellChain(["benes doctor", "benes sync"], undefined), "benes doctor && benes sync");
  assert.equal(shellChain(["benes sync"], "win32"), "benes sync");
  assert.equal(shellChain([], "win32"), "");
});

test("a clamped runtime without removed efforts still warns and offers the plain sync fix", () => {
  const notice = deriveCodexRuntimeNotice({ catalogClamp: { active: true } }, t, "linux");
  assert.equal(notice.warning, "startup.codexRuntime.clampHidden:version=unknown");
  assert.equal(notice.fix, "benes sync");
});

test("a newer runtime binary asks for a doctor sync on the current platform", () => {
  const notice = deriveCodexRuntimeNotice({ version: "9.9.9", newerAvailable: { path: "/opt/codex" } }, t, "linux");

test("install failures retire only on the success signal their action needs", () => {
  const failedShim = { kind: "error" as const, action: "install-shim" as const };
  assert.ok(retireFailedInstallResult(failedShim, health({ status: "at-risk" })));
  assert.equal(
    retireFailedInstallResult(failedShim, health({ status: "at-risk", shimInstalled: true, shimHealthy: true })),
    null,
  );

  const failedService = { kind: "error" as const, action: "install-service" as const };
  assert.equal(retireFailedInstallResult(failedService, health({ serviceViable: true })), null);
  assert.ok(retireFailedInstallResult(failedService, health({ serviceViable: false })));

  assert.equal(retireFailedInstallResult(null, health()), null);
  const success = { kind: "success" as const, action: "install-shim" as const };
  assert.deepEqual(retireFailedInstallResult(success, health()), success);
});

test("routing kinds map to their copy keys", () => {
  const kinds = ["native", "benes-local", "custom-local", "custom-remote", "unknown"] as const;
  assert.deepEqual(
    kinds.map(startupRoutingKey),
    [
      "startup.routing.native",
      "startup.routing.proxy",
      "startup.routing.customLocal",
      "startup.routing.customRemote",
      "startup.routing.unknown",
    ],
  );
});

test("hero tone and detail follow failure before the reported status", () => {
  assert.equal(startupHeroTone(true, "protected"), "risk");
  assert.equal(startupHeroTone(true, "native"), "risk");
  assert.equal(startupHeroTone(false, "at-risk"), "risk");
  assert.equal(startupHeroTone(false, "protected"), "safe");
  assert.equal(startupHeroTone(false, "native"), "native");
  assert.equal(startupHeroDetailKey(true, health({ status: "native" })), "startup.staleData");
  assert.equal(startupHeroDetailKey(false, health({ status: "protected" })), "startup.safeDetail");
});

test("service state copy keeps its conflict, stale, installed, unsupported order", () => {
  assert.equal(serviceStateKey(health({ serviceConflict: true, serviceStale: true })), "startup.conflict");
  assert.equal(serviceStateKey(health({ serviceStale: true, serviceInstalled: true })), "startup.stale");
  assert.equal(serviceStateKey(health({ serviceInstalled: true })), "startup.unhealthy");
  assert.equal(serviceStateKey(health({ serviceInstalled: false, serviceSupported: true })), "startup.notInstalled");
  assert.equal(serviceStateKey(health({ serviceInstalled: false, serviceSupported: false })), "startup.unsupported");
});

test("shim copy distinguishes full coverage, installed-but-disabled, and missing", () => {
  assert.equal(shimYesKey(health({ shimCoverage: "full" })), "startup.healthy");
  assert.equal(shimYesKey(health({ shimCoverage: "cli-only" })), "startup.cliOnly");
  assert.equal(shimNoKey(health()), "startup.notInstalled");
  assert.equal(shimNoKey(health({ shimInstalled: true, shimHealthy: true })), "startup.stale");
  assert.equal(
    shimNoKey(health({ shimInstalled: true, shimHealthy: true, autostartEnabled: false })),
    "startup.installedDisabled",
  );
  assert.equal(shimNoKey(health({ shimInstalled: true, shimHealthy: false })), "startup.stale");
});

test("tray actions stay disabled until a status is known", () => {
  const tray = { supported: true, installed: false, running: false, stale: false, summary: "" };
  assert.deepEqual(startupTrayActions(tray, false, true), {
    install: false, start: false, stop: false, uninstall: false,
  });
  assert.deepEqual(startupTrayActions(tray, true, false), {
    install: false, start: false, stop: false, uninstall: false,
  });
  assert.equal(startupTrayBadgeKind(null, false, false), "unavailable");
  assert.equal(startupTrayBadgeKind(tray, false, false), "notInstalled");
  assert.equal(startupTrayBadgeKind({ ...tray, installed: true }, false, false), "stopped");
});

test("install result copy distinguishes repair from a first install", () => {
  assert.equal(installResultMessageKey({ kind: "success", action: "install-service" }), "startup.serviceInstalled");
  assert.equal(installResultMessageKey({ kind: "success", action: "install-shim" }), "startup.shimInstalled");
  assert.equal(
    installResultMessageKey({ kind: "success", action: "install-shim", repair: true }),
    "startup.shimRepaired",
  );
  assert.equal(
    installResultMessageKey({ kind: "error", action: "install-shim", repair: true }),
    "startup.installFailed",
  );
});
  assert.equal(notice.warning, "startup.codexRuntime.olderBinary:version=9.9.9");
  assert.equal(notice.fix, "benes doctor --fix-codex-runtime && benes sync");
});

test("an unknown runtime version never invents one", () => {
  const notice = deriveCodexRuntimeNotice({ newerAvailable: { path: "/opt/codex" } }, t, "linux");
  assert.equal(notice.warning, "startup.codexRuntime.olderBinary:version=unknown");
});

test("a clamped runtime prefers the clamped version over the reported one", () => {
  const notice = deriveCodexRuntimeNotice(
    { version: "2.0.0", catalogClamp: { active: true, runtimeVersion: "1.0.0", removedEfforts: ["xhigh", "max"] } },
    t,
    "linux",
  );
  assert.equal(notice.warning, "startup.codexRuntime.clampHiddenWithEfforts:version=1.0.0,efforts=xhigh, max");
});