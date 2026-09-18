import assert from "node:assert/strict";
import test from "node:test";

import {
  PROTECTION_KEYS,
  STATUS_KEYS,
  SUMMARY_KEYS,
  isTrayStatusData,
  parseStartupHealthData,
} from "../src/pages/startup-shared.ts";

function completeHealth(overrides: Record<string, unknown> = {}) {
  return {
    status: "at-risk",
    routingKind: "custom-remote",
    routingInjected: false,
    localRoutingDependency: false,
    autostartEnabled: false,
    rebootSafe: false,
    protection: "shim",
    serviceInstalled: true,
    serviceViable: false,
    serviceEnabled: false,
    serviceRunning: false,
    serviceStale: true,
    serviceConflict: false,
    serviceSupported: true,
    shimInstalled: true,
    shimHealthy: false,
    shimCoverage: "cli-only",
    platform: "win32",
    recommendedCommand: "benes service install",
    diagnosticStale: true,
    commands: {
      installService: "benes service install",
      repairService: "benes service repair",
      installShim: "benes shim install",
      restoreNative: "benes restore",
    },
    ...overrides,
  };
}

test("a complete startup-health payload keeps every field the Control board reads", () => {
  const parsed = parseStartupHealthData(completeHealth());
  assert.ok(parsed);
  assert.equal(parsed.status, "at-risk");
  assert.equal(parsed.routingKind, "custom-remote");
  assert.equal(parsed.platform, "win32");
  assert.equal(parsed.serviceStale, true);
  assert.equal(parsed.shimCoverage, "cli-only");
  assert.equal(parsed.diagnosticStale, true);
  assert.deepEqual(parsed.commands, {
    installService: "benes service install",
    repairService: "benes service repair",
    installShim: "benes shim install",
    restoreNative: "benes restore",
  });
});

test("a payload without the command block falls back to the legacy ok/proxy shape", () => {
  const legacy = {
    ok: true,
    proxy: "running",
    shim: { installed: true, summary: "ok" },
  };
  const parsed = parseStartupHealthData(legacy);
  assert.ok(parsed);
  assert.equal(parsed.status, "protected");
  assert.equal(parsed.routingKind, "benes-local");
  assert.equal(parsed.routingInjected, true);
  assert.equal(parsed.localRoutingDependency, true);
  assert.equal(parsed.autostartEnabled, true);
  assert.equal(parsed.rebootSafe, true);
  assert.equal(parsed.protection, "shim");
  assert.equal(parsed.serviceInstalled, true);
  assert.equal(parsed.serviceViable, true);
  assert.equal(parsed.serviceEnabled, true);
  assert.equal(parsed.serviceRunning, true);
  assert.equal(parsed.serviceStale, false);
  assert.equal(parsed.serviceConflict, false);
  assert.equal(parsed.serviceSupported, true);
  assert.equal(parsed.shimInstalled, true);
  assert.equal(parsed.shimHealthy, true);
  assert.equal(parsed.shimCoverage, "cli-only");
  assert.equal(parsed.recommendedCommand, null);
  assert.equal(parsed.diagnosticStale, false);
  assert.equal(parsed.platform, "linux");
  assert.deepEqual(parsed.commands, {
    installService: "benes service install",
    repairService: "benes service repair",
    installShim: "benes codex-shim install",
    restoreNative: "benes restore",
  });
});
test("the legacy fallback reports a stopped listener as an unknown at-risk routing", () => {
  const stopped = parseStartupHealthData({ ok: false, proxy: "stopped" });
  assert.ok(stopped);
  assert.equal(stopped.status, "at-risk");
  assert.equal(stopped.routingKind, "unknown");
  assert.equal(stopped.routingInjected, false);
  assert.equal(stopped.rebootSafe, false);
  assert.equal(stopped.protection, "none");
  assert.equal(stopped.serviceInstalled, false);
  assert.equal(stopped.shimInstalled, false);
  assert.equal(stopped.shimHealthy, false);
  assert.equal(stopped.shimCoverage, "none");
});

test("an unreadable payload parses to null instead of an invented health row", () => {
  assert.equal(parseStartupHealthData(null), null);
  assert.equal(parseStartupHealthData(undefined), null);
  assert.equal(parseStartupHealthData("native"), null);
  assert.equal(parseStartupHealthData([]), null);
  assert.equal(parseStartupHealthData({ nope: true }), null);
  assert.equal(parseStartupHealthData({ ok: true }), null);
  assert.equal(parseStartupHealthData({ proxy: "running" }), null);
  assert.equal(parseStartupHealthData({ ok: "true", proxy: "running" }), null);
});

test("a partially modern payload is not mistaken for a complete one", () => {
  const partial = completeHealth({ commands: { restoreNative: "benes restore" } });
  delete (partial as Record<string, unknown>).routingKind;
  assert.equal(parseStartupHealthData(partial), null);

  assert.equal(parseStartupHealthData(completeHealth({ commands: { restoreNative: 42 } })), null);

  const incomplete = completeHealth();
  delete (incomplete as Record<string, unknown>).platform;
  const degraded = parseStartupHealthData({ ...incomplete, ok: true, proxy: "running" });
  assert.equal(degraded?.status, "protected");
});

test("tray status is only accepted when every field is present and typed", () => {
  const tray = { supported: true, installed: false, running: false, stale: true, summary: "not installed" };
  assert.equal(isTrayStatusData(tray), true);
  for (const key of ["supported", "installed", "running", "stale"]) {
    assert.equal(isTrayStatusData({ ...tray, [key]: "yes" }), false, key);
    const missing = { ...tray } as Record<string, unknown>;
    delete missing[key];
    assert.equal(isTrayStatusData(missing), false, `missing ${key}`);
  }
  assert.equal(isTrayStatusData({ ...tray, summary: null }), false);
  assert.equal(isTrayStatusData(null), false);
  assert.equal(isTrayStatusData("tray"), false);
  assert.equal(isTrayStatusData({}), false);
});

test("Control board copy tables cover every status and protection value", () => {
  assert.deepEqual(STATUS_KEYS, {
    native: "startup.status.native",
    protected: "startup.status.protected",
    "at-risk": "startup.status.atRisk",
  });
  assert.deepEqual(SUMMARY_KEYS, {
    native: "startup.summary.native",
    protected: "startup.summary.protected",
    "at-risk": "startup.summary.atRisk",
  });
  assert.deepEqual(PROTECTION_KEYS, {
    service: "startup.protection.service",
    shim: "startup.protection.shim",
    none: "startup.protection.none",
  });
});