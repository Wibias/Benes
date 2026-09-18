import assert from "node:assert/strict";
import { test } from "node:test";

import type { BrowserEvidenceRunner } from "./evidence-browser-runner.ts";

test("productive browser evidence runner wires the real default dependencies", async () => {
  const module = await import("./evidence-browser-runner.ts");
  const runBrowserEvidence = (
    module as typeof module & { runBrowserEvidence?: BrowserEvidenceRunner }
  ).runBrowserEvidence;

  assert.equal(
    typeof runBrowserEvidence,
    "function",
    "productive browser evidence runner API must exist",
  );

  if (!runBrowserEvidence) {
    throw new Error("productive browser evidence runner API is unavailable");
  }

  await assert.rejects(
    runBrowserEvidence(
      {
        scratchRoot: "relative-scratch",
        automationProfileRoot: "relative-automation",
        browserCommand: "chrome",
        overallTimeoutMs: 1_000,
        cdpRequestTimeoutMs: 100,
        startupTimeoutMs: 100,
        gracefulShutdownTimeoutMs: 100,
      },
      async () => "unused",
    ),
    /scratch root must be absolute/i,
  );
});
