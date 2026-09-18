/**
 * Focused behavioural tests for the Claude Desktop settings surface.
 *
 * They assert product states and transitions against the runtime contract
 * #255 / merged #328 landed. There is deliberately no model-routing case here:
 * the corrected scope for #327 removed those requirements because Claude
 * Desktop's supported local configuration has no such surface.
 */
import assert from "node:assert/strict";
import { existsSync, readFileSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import test from "node:test";

import {
  CLAUDE_DESKTOP_FORBIDDEN_DESIRED_KEYS,
  CLAUDE_DESKTOP_LOAD_FAILED,
  claudeDesktopActions,
  claudeDesktopBlocked,
  claudeDesktopDesiredBody,
  claudeDesktopDesire,
  claudeDesktopMutationConfirmed,
  claudeDesktopStateKey,
  claudeDesktopTone,
  decodeClaudeDesktopStatus,
} from "../src/pages/harnesses/claude-desktop-state.ts";
import {
  readClaudeDesktopStatus,
  runClaudeDesktopLifecycle,
  saveClaudeDesktopDesired,
} from "../src/pages/harnesses/claude-desktop-io.ts";
import { harnessHash, harnessShowsSettings, parseHarnessHash } from "../src/pages/harnesses/hash.ts";

const ROOT = path.join(path.dirname(fileURLToPath(import.meta.url)), "..");
const HARNESSES_DIR = path.join(ROOT, "src/pages/harnesses");

/** Synthetic, non-secret fixture state. No user configuration is read. */
const CONFIG_PATH = "C:\\Users\\example\\AppData\\Roaming\\Claude\\claude_desktop_config.json";

function payload(overrides: Record<string, unknown> = {}): Record<string, unknown> {
  return {
    clientId: "claude-desktop",
    hostSupported: true,
    installed: true,
    configurable: true,
    configPath: CONFIG_PATH,
    state: "not_applied",
    managedProjectionAvailable: true,
    desiredEnabled: true,
    observedKind: "no_managed_entry",
    applied: false,
    stale: false,
    restartRequired: false,
    refusal: null,
    ...overrides,
  };
}

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

test("decode keeps desired, applied and stale as separate answers", () => {
  const status = decodeClaudeDesktopStatus(payload({ desiredEnabled: true, applied: false, stale: false }));
  assert.equal(status.desiredEnabled, true);
  assert.equal(status.applied, false);
  assert.equal(status.stale, false);
});

test("decode fails closed on an unknown disposition or a malformed payload", () => {
  assert.throws(() => decodeClaudeDesktopStatus(payload({ state: "probably_fine" })), {
    message: CLAUDE_DESKTOP_LOAD_FAILED,
  });
  assert.throws(() => decodeClaudeDesktopStatus(payload({ applied: "yes" })), {
    message: CLAUDE_DESKTOP_LOAD_FAILED,
  });
  assert.throws(() => decodeClaudeDesktopStatus(null), { message: CLAUDE_DESKTOP_LOAD_FAILED });
  assert.throws(() => decodeClaudeDesktopStatus([]), { message: CLAUDE_DESKTOP_LOAD_FAILED });
});

test("desired but not applied offers Apply and says it is unapplied", () => {
  const status = decodeClaudeDesktopStatus(payload({ state: "not_applied" }));
  assert.equal(claudeDesktopDesire(status), "saved_not_applied");
  assert.equal(claudeDesktopBlocked(status), null);
  assert.deepEqual(claudeDesktopActions(status), {
    setDesired: true,
    apply: true,
    reapply: false,
    disable: false,
  });
});

test("applied and current offers no misleading work", () => {
  const status = decodeClaudeDesktopStatus(payload({
    state: "applied",
    observedKind: "managed_entry",
    applied: true,
    appliedFingerprint: "a".repeat(64),
  }));
  assert.equal(claudeDesktopDesire(status), "applied");
  assert.equal(claudeDesktopBlocked(status), "current");
  assert.deepEqual(claudeDesktopActions(status), {
    setDesired: true,
    apply: false,
    reapply: false,
    disable: true,
  });
});

test("a managed entry that stops matching the desired projection is stale and offers Re-apply", () => {
  const status = decodeClaudeDesktopStatus(payload({
    state: "stale",
    stale: true,
    observedKind: "managed_entry",
    desiredFingerprint: "b".repeat(64),
    observedFingerprint: "c".repeat(64),
  }));
  assert.equal(status.applied, false);
  assert.equal(status.stale, true);
  assert.notEqual(status.desiredFingerprint, status.observedFingerprint);
  assert.equal(claudeDesktopDesire(status), "stale");
  assert.deepEqual(claudeDesktopActions(status), {
    setDesired: true,
    apply: false,
    reapply: true,
    disable: true,
  });
});

test("externally drifted native state reaches the same truthful stale disposition", () => {
  // The landed contract reports one stale disposition for both a changed desire
  // and a foreign edit, so the surface must not invent a distinction between them.
  const afterDesiredChange = decodeClaudeDesktopStatus(payload({ state: "stale", stale: true, observedKind: "managed_entry" }));
  const externallyDrifted = decodeClaudeDesktopStatus(payload({ state: "stale", stale: true, observedKind: "foreign_entry" }));
  assert.equal(externallyDrifted.state, afterDesiredChange.state);
  assert.equal(claudeDesktopActions(externallyDrifted).reapply, true);
  assert.equal(claudeDesktopActions(afterDesiredChange).reapply, true);
});

test("today's production disposition offers no lifecycle action and surfaces the refusal", () => {
  const status = decodeClaudeDesktopStatus(payload({
    state: "no_managed_projection",
    managedProjectionAvailable: false,
    desiredEnabled: false,
    observedKind: "no_mcp_servers",
    refusal: {
      code: "benes_mcp_runtime_unavailable",
      message: "Benes ships no Claude Desktop MCP runtime, so there is no managed projection to apply.",
    },
  }));
  assert.equal(status.refusal?.code, "benes_mcp_runtime_unavailable");
  assert.equal(claudeDesktopBlocked(status), "no_projection");
  assert.deepEqual(claudeDesktopActions(status), {
    setDesired: true,
    apply: false,
    reapply: false,
    disable: false,
  });
});

test("unsupported host fails closed and hides the editable control", () => {
  const status = decodeClaudeDesktopStatus(payload({
    state: "unsupported_host",
    hostSupported: false,
    installed: false,
    configurable: false,
    configPath: null,
    managedProjectionAvailable: false,
    desiredEnabled: false,
    observedKind: "unobserved",
    refusal: { code: "unsupported_host", message: "Windows and macOS only." },
  }));
  assert.equal(status.hostSupported, false);
  assert.equal(status.configPath, null);
  assert.equal(claudeDesktopActions(status).setDesired, false);
  assert.equal(claudeDesktopBlocked(status), "unsupported_host");
});

test("installed and configurable are separate answers", () => {
  const notInstalled = decodeClaudeDesktopStatus(payload({
    state: "not_installed",
    installed: false,
    configurable: false,
    managedProjectionAvailable: false,
    desiredEnabled: false,
    refusal: { code: "not_installed", message: "not installed" },
  }));
  assert.equal(notInstalled.hostSupported, true);
  assert.equal(notInstalled.installed, false);
  assert.equal(claudeDesktopActions(notInstalled).setDesired, false);
  assert.equal(claudeDesktopBlocked(notInstalled), "not_installed");

  const unreadable = decodeClaudeDesktopStatus(payload({
    state: "config_unavailable",
    configurable: false,
    managedProjectionAvailable: false,
    refusal: { code: "config_malformed", message: "not a JSON object" },
  }));
  assert.equal(unreadable.installed, true);
  assert.equal(unreadable.configurable, false);
  assert.equal(claudeDesktopActions(unreadable).setDesired, false);
  assert.equal(claudeDesktopBlocked(unreadable), "config_unavailable");
});

test("restart is reported only when the runtime says a mutation awaits it", () => {
  const quiet = decodeClaudeDesktopStatus(payload());
  assert.equal(quiet.restartRequired, false);
  const awaiting = decodeClaudeDesktopStatus(payload({ restartRequired: true }));
  assert.equal(awaiting.restartRequired, true);
});

test("a successful apply is claimed only after the re-read status confirms it", async () => {
  const original = globalThis.fetch;
  const calls: string[] = [];
  try {
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      calls.push((init?.method ?? "GET") + " " + url);
      if (url.endsWith("/api/claude-desktop/apply")) {
        return jsonResponse(200, {
          ok: true,
          clientId: "claude-desktop",
          changed: true,
          applied: true,
          configPath: CONFIG_PATH,
          restartRequired: true,
        });
      }
      return jsonResponse(200, payload({
        state: "applied",
        observedKind: "managed_entry",
        applied: true,
        appliedFingerprint: "d".repeat(64),
      }));
    };
    const outcome = await runClaudeDesktopLifecycle("http://127.0.0.1:23100", "apply");
    assert.equal(outcome.confirmed, true);
    assert.equal(outcome.status.applied, true);
    assert.equal(outcome.mutation.restartRequired, true);
    assert.deepEqual(calls, [
      "POST http://127.0.0.1:23100/api/claude-desktop/apply",
      "GET http://127.0.0.1:23100/api/claude-desktop",
    ]);
  } finally {
    globalThis.fetch = original;
  }
});

test("re-apply is confirmed by the status that follows it", async () => {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.endsWith("/api/claude-desktop/apply")) {
        return jsonResponse(200, { ok: true, changed: true, applied: true, restartRequired: false });
      }
      return jsonResponse(200, payload({
        state: "applied",
        observedKind: "managed_entry",
        applied: true,
        appliedFingerprint: "e".repeat(64),
      }));
    };
    const stale = decodeClaudeDesktopStatus(payload({ state: "stale", stale: true, observedKind: "managed_entry" }));
    assert.equal(claudeDesktopActions(stale).reapply, true);
    const outcome = await runClaudeDesktopLifecycle("http://127.0.0.1:23100", "apply");
    assert.equal(outcome.confirmed, true);
    assert.equal(claudeDesktopMutationConfirmed("apply", outcome.status), true);
  } finally {
    globalThis.fetch = original;
  }
});

test("a 200 the runtime status does not confirm is never reported as success", async () => {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async (input) => {
      const url = String(input);
      if (url.endsWith("/api/claude-desktop/apply")) {
        return jsonResponse(200, { ok: true, changed: true, applied: true, restartRequired: false });
      }
      return jsonResponse(200, payload({
        state: "no_managed_projection",
        managedProjectionAvailable: false,
        applied: false,
        observedKind: "no_mcp_servers",
        refusal: { code: "benes_mcp_runtime_unavailable", message: "no projection" },
      }));
    };
    const outcome = await runClaudeDesktopLifecycle("http://127.0.0.1:23100", "apply");
    assert.equal(outcome.mutation.applied, true);
    assert.equal(outcome.status.applied, false);
    assert.equal(outcome.confirmed, false);
    assert.equal(claudeDesktopMutationConfirmed("apply", outcome.status), false);
  } finally {
    globalThis.fetch = original;
  }
});

test("a refused apply surfaces the exact runtime refusal and reconciles nothing optimistically", async () => {
  const original = globalThis.fetch;
  const calls: string[] = [];
  try {
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      calls.push((init?.method ?? "GET") + " " + url);
      if (url.endsWith("/api/claude-desktop/apply")) {
        return jsonResponse(409, {
          error: {
            code: "benes_mcp_runtime_unavailable",
            message: "Benes ships no Claude Desktop MCP runtime, so there is no managed projection to apply.",
          },
        });
      }
      return jsonResponse(200, payload());
    };
    await assert.rejects(
      () => runClaudeDesktopLifecycle("http://127.0.0.1:23100", "apply"),
      { message: "Benes ships no Claude Desktop MCP runtime, so there is no managed projection to apply." },
    );
    assert.deepEqual(calls, ["POST http://127.0.0.1:23100/api/claude-desktop/apply"]);
  } finally {
    globalThis.fetch = original;
  }
});

test("disable is confirmed by a status with no Benes-owned entry left", async () => {
  const original = globalThis.fetch;
  const calls: string[] = [];
  try {
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      calls.push((init?.method ?? "GET") + " " + url);
      if (url.endsWith("/api/claude-desktop/disable")) {
        return jsonResponse(200, { ok: true, changed: true, applied: false, restartRequired: true });
      }
      return jsonResponse(200, payload({
        state: "not_applied",
        desiredEnabled: false,
        observedKind: "no_managed_entry",
        applied: false,
        stale: false,
      }));
    };
    const outcome = await runClaudeDesktopLifecycle("http://127.0.0.1:23100", "disable");
    assert.equal(outcome.confirmed, true);
    assert.equal(outcome.status.applied, false);
    assert.equal(outcome.status.stale, false);
    assert.deepEqual(calls, [
      "POST http://127.0.0.1:23100/api/claude-desktop/disable",
      "GET http://127.0.0.1:23100/api/claude-desktop",
    ]);
  } finally {
    globalThis.fetch = original;
  }
});

test("restore is the runtime's own exact-entry removal rather than a second lifecycle", async () => {
  // #255 models restore as Disable removing only the Benes-owned entry, so the
  // surface offers that one action and invents no snapshot route for it.
  const residual = decodeClaudeDesktopStatus(payload({ state: "stale", stale: true, observedKind: "foreign_entry" }));
  assert.equal(claudeDesktopMutationConfirmed("disable", residual), false);
  assert.equal(claudeDesktopMutationConfirmed("disable", decodeClaudeDesktopStatus(payload())), true);
  // The io module addresses the canonical status route and one mutation prefix;
  // the apply and disable cases above prove which mutations reach the wire.
  const io = readFileSync(path.join(HARNESSES_DIR, "claude-desktop-io.ts"), "utf8");
  assert.equal(io.includes("/api/claude-desktop"), true);
  assert.equal(io.includes("/api/models"), false);
  assert.equal(io.includes("snapshot"), false);
});

test("desired state is written with enablement alone and can be read back", async () => {
  const original = globalThis.fetch;
  const bodies: string[] = [];
  try {
    globalThis.fetch = async (input, init) => {
      const url = String(input);
      if (init?.method === "PUT") {
        bodies.push(String(init.body));
        return jsonResponse(200, payload({ desiredEnabled: true }));
      }
      return jsonResponse(200, payload({ desiredEnabled: false }));
    };
    const saved = await saveClaudeDesktopDesired("http://127.0.0.1:23100", true);
    assert.equal(saved.desiredEnabled, true);
    assert.deepEqual(bodies, ['{"enabled":true}']);
    const body = claudeDesktopDesiredBody(true) as Record<string, unknown>;
    assert.deepEqual(Object.keys(body), ["enabled"]);
    for (const key of CLAUDE_DESKTOP_FORBIDDEN_DESIRED_KEYS) {
      assert.equal(key in body, false, key);
    }
  } finally {
    globalThis.fetch = original;
  }
});

test("a refused desired write keeps the previous truth", async () => {
  const original = globalThis.fetch;
  try {
    globalThis.fetch = async () => jsonResponse(400, {
      error: { code: "invalid_body", message: "enabled must be the only field" },
    });
    await assert.rejects(
      () => saveClaudeDesktopDesired("http://127.0.0.1:23100", true),
      { message: "enabled must be the only field" },
    );
    await assert.rejects(
      () => readClaudeDesktopStatus("http://127.0.0.1:23100"),
      { message: "enabled must be the only field" },
    );
  } finally {
    globalThis.fetch = original;
  }
});

test("every disposition maps to one sentence and one tone", () => {
  const states = [
    ["unsupported_host", "blocked"],
    ["not_installed", "blocked"],
    ["config_unavailable", "blocked"],
    ["no_managed_projection", "blocked"],
    ["not_applied", "pending"],
    ["applied", "ok"],
    ["stale", "attention"],
  ] as const;
  for (const [state, tone] of states) {
    const status = decodeClaudeDesktopStatus(payload({ state }));
    assert.equal(claudeDesktopTone(status), tone, state);
    assert.equal(typeof claudeDesktopStateKey(state), "string", state);
  }
});

test("Claude Desktop joins Claude on the Harnesses settings deep link", () => {
  assert.equal(harnessShowsSettings("claude"), true);
  assert.equal(harnessShowsSettings("claude-desktop"), true);
  assert.equal(harnessShowsSettings("codex"), false);
  assert.equal(harnessShowsSettings("grok"), false);
  assert.equal(harnessShowsSettings(null), false);
  assert.equal(harnessHash("claude-desktop", "settings"), "harnesses/claude-desktop/settings");
  assert.equal(harnessHash("claude-desktop"), "harnesses/claude-desktop");
  assert.equal(harnessHash("codex", "settings"), "harnesses/codex");
  assert.deepEqual(parseHarnessHash("#harnesses/claude-desktop/settings"), {
    id: "claude-desktop",
    tab: "settings",
  });
  assert.deepEqual(parseHarnessHash("#harnesses/claude-desktop"), {
    id: "claude-desktop",
    tab: "overview",
  });
  assert.deepEqual(parseHarnessHash("#harnesses/codex/settings"), { id: "codex", tab: "overview" });
});

test("Harnesses stays the only Claude Desktop settings surface", () => {
  const detail = readFileSync(path.join(HARNESSES_DIR, "HarnessDetail.tsx"), "utf8");
  assert.equal(detail.includes("ClaudeDesktopSettings"), true);
  assert.equal(detail.includes("harnessShowsSettings"), true);
  assert.equal(existsSync(path.join(ROOT, "src/pages/ClaudeDesktop.tsx")), false);
  assert.equal(existsSync(path.join(HARNESSES_DIR, "claude-desktop-board.tsx")), false);
  assert.equal(existsSync(path.join(HARNESSES_DIR, "claude-desktop-lane.ts")), false);
});

test("the surface adds no model, endpoint or credential control", () => {
  const controlTokens = [
    "/api/models",
    "/api/claude-code",
    "model-display",
    "modelLabel",
    "baseUrl",
    "apiKey",
  ];
  for (const name of ["claude-desktop-settings.tsx", "claude-desktop-io.ts"]) {
    const source = readFileSync(path.join(HARNESSES_DIR, name), "utf8");
    for (const forbidden of [...controlTokens, "tierModels", "modelMap"]) {
      assert.equal(source.includes(forbidden), false, name + " references " + forbidden);
    }
  }
  // The state module names the forbidden key names once, in the list that
  // forbids them; it must still reach no catalogue and no credential route.
  const state = readFileSync(path.join(HARNESSES_DIR, "claude-desktop-state.ts"), "utf8");
  for (const forbidden of ["/api/models", "/api/claude-code", "model-display", "modelLabel"]) {
    assert.equal(state.includes(forbidden), false, "claude-desktop-state.ts references " + forbidden);
  }
  assert.equal(state.includes("CLAUDE_DESKTOP_FORBIDDEN_DESIRED_KEYS"), true);
});

test("the surface keeps its actions keyboard-operable and its refusals readable", () => {
  const surface = readFileSync(path.join(HARNESSES_DIR, "claude-desktop-settings.tsx"), "utf8");
  assert.equal(surface.includes('role="group"'), true);
  assert.equal(surface.includes("aria-describedby"), true);
  assert.equal(surface.includes("aria-live"), true);
  assert.equal(surface.includes("aria-label"), true);
  assert.equal(surface.includes("<button"), true);
  assert.equal(surface.includes("disabled={"), true);
  assert.equal(surface.includes("onClick="), true);
  const css = readFileSync(path.join(ROOT, "src/styles-harnesses.css"), "utf8");
  assert.equal(css.includes("harnesses-desktop-actions"), true);
  assert.equal(css.includes("harnesses-desktop-path"), true);
  assert.equal(css.includes("overflow-wrap: anywhere"), true);
});
