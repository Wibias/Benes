/**
 * Focused contracts for the reauthored restart ownership (Wave A of #236).
 *
 * The pure policy/parse layer is exercised directly; the hook's pending-authority
 * rule is exposed as a pure unit because this repository has no arbitrary-hook
 * renderer harness.
 */
import assert from "node:assert/strict";
import test from "node:test";

import type { TFn } from "../src/i18n/shared.ts";
import { classifyCodexRestart, requestCodexRestart } from "../src/codex-restart.ts";
import type { CodexRestartResponse } from "../src/codex-restart.ts";
import {
  clearRunningAttempt,
  codexRestartAlertText,
  isRestartSettled,
} from "../src/use-codex-restart.ts";

function translate(key: string, vars?: Record<string, string | number>): string {
  return vars ? `${key}(${Object.values(vars).join(",")})` : key;
}
const t = translate as unknown as TFn;

function body(overrides: Partial<CodexRestartResponse> = {}): CodexRestartResponse {
  return {
    success: true,
    stateBefore: "stale",
    synced: false,
    requested: [11],
    stopped: [11],
    surviving: [],
    failed: [],
    code: "stopped",
    ...overrides,
  };
}

test("classifyCodexRestart maps each code to its announcement", () => {
  assert.deepEqual(classifyCodexRestart(body()), { kind: "stopped", stoppedCount: 1 });
  assert.deepEqual(
    classifyCodexRestart(body({ code: "nothing_running", requested: [], stopped: [] })),
    { kind: "nothing-running" },
  );
  assert.deepEqual(
    classifyCodexRestart(body({ code: "enumeration_unavailable", requested: [], stopped: [] })),
    { kind: "enumeration-unavailable" },
  );
  assert.deepEqual(
    classifyCodexRestart(body({ success: false, code: "partially_stopped", surviving: [11], stopped: [] })),
    { kind: "partial", survivingCount: 1 },
  );
});

test("isRestartSettled accepts only the two no-stale-server codes", () => {
  assert.equal(isRestartSettled("stopped"), true);
  assert.equal(isRestartSettled("nothing_running"), true);
  assert.equal(isRestartSettled("enumeration_unavailable"), false);
  assert.equal(isRestartSettled("partially_stopped"), false);
});

test("a stale completion cannot clear a newer attempt's pending flag", () => {
  assert.equal(clearRunningAttempt(3, 3), 0);
  assert.equal(clearRunningAttempt(3, 2), 3);
  assert.equal(clearRunningAttempt(0, 1), 0);
});

test("codexRestartAlertText localizes each announcement", () => {
  assert.equal(codexRestartAlertText(t, body()), "dash.codexRestartDone(1)");
  assert.equal(
    codexRestartAlertText(t, body({ code: "nothing_running", requested: [], stopped: [] })),
    "dash.codexRestartNothing",
  );
  assert.equal(
    codexRestartAlertText(t, body({ code: "enumeration_unavailable", requested: [], stopped: [] })),
    "dash.codexRestartUnknown",
  );
  assert.equal(
    codexRestartAlertText(t, body({ success: false, code: "partially_stopped", surviving: [1, 2], stopped: [] })),
    "dash.codexRestartPartial(2)",
  );
});

test("requestCodexRestart posts to the restart route and returns the parsed body", async () => {
  const seen: Array<{ url: string; method: string | undefined }> = [];
  const outcome = await requestCodexRestart("http://x", {
    fetchFn: async (url, init) => {
      seen.push({ url: String(url), method: init?.method });
      return new Response(JSON.stringify(body()), { status: 200 });
    },
  });
  assert.equal(outcome.ok, true);
  assert.equal(outcome.result?.code, "stopped");
  assert.deepEqual(seen, [{ url: "http://x/api/system/codex-restart", method: "POST" }]);
});

test("requestCodexRestart reports an HTTP rejection through formatFailure", async () => {
  const outcome = await requestCodexRestart("http://x", {
    fetchFn: async () => new Response("nope", { status: 503 }),
    formatFailure: status => `failed:${status}`,
  });
  assert.deepEqual(outcome, { ok: false, message: "failed:503" });
});

test("requestCodexRestart reports a malformed body through formatMalformed", async () => {
  const notJson = await requestCodexRestart("http://x", {
    fetchFn: async () => new Response("<<<", { status: 200 }),
    formatMalformed: () => "malformed",
  });
  assert.deepEqual(notJson, { ok: false, message: "malformed" });

  const wrongShape = await requestCodexRestart("http://x", {
    fetchFn: async () => new Response(JSON.stringify({ success: true }), { status: 200 }),
    formatMalformed: () => "malformed",
  });
  assert.deepEqual(wrongShape, { ok: false, message: "malformed" });

  const contradictory = await requestCodexRestart("http://x", {
    fetchFn: async () => new Response(JSON.stringify(body({ success: false, code: "stopped" })), { status: 200 }),
    formatMalformed: () => "malformed",
  });
  assert.deepEqual(contradictory, { ok: false, message: "malformed" });
});

test("requestCodexRestart separates transport failure from a timeout", async () => {
  const dropped = await requestCodexRestart("http://x", {
    fetchFn: async () => { throw new Error("ECONNREFUSED"); },
    formatUnreachable: () => "unreachable",
    formatTimeout: () => "timeout",
  });
  assert.deepEqual(dropped, { ok: false, message: "unreachable" });

  const timedOut = await requestCodexRestart("http://x", {
    fetchFn: async () => { throw new DOMException("aborted", "TimeoutError"); },
    formatUnreachable: () => "unreachable",
    formatTimeout: () => "timeout",
  });
  assert.deepEqual(timedOut, { ok: false, message: "timeout" });
});

test("requestCodexRestart default copy is stable without caller formatters", async () => {
  const outcome = await requestCodexRestart("http://x", {
    fetchFn: async () => new Response("x", { status: 500 }),
  });
  assert.equal(outcome.ok, false);
  assert.equal(outcome.message, "Failed to restart Codex (HTTP 500).");
});
