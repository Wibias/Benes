import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";

import {
  acquireBrowserAutomationProfile,
  startBrowserAutomation,
} from "./evidence-browser-runtime.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

type SessionWithPreCdpTermination = Awaited<ReturnType<typeof startBrowserAutomation>> & {
  terminateBeforeCdp?: () => Promise<void>;
};

function preCdpTerminationApi(session: SessionWithPreCdpTermination): () => Promise<void> {
  const terminateBeforeCdp = session.terminateBeforeCdp;
  assert.equal(
    typeof terminateBeforeCdp,
    "function",
    "pre-CDP termination API must exist on the browser automation session",
  );
  if (!terminateBeforeCdp) throw new Error("pre-CDP termination API is unavailable");
  return terminateBeforeCdp.bind(session);
}

test("pre-CDP termination kills the owned browser and releases the persistent profile lease", async () => {
  const parent = await createTestTempRoot("benes-browser-pre-cdp-terminate-");
  const root = path.join(parent, "automation");
  const calls: string[] = [];

  const session = await startBrowserAutomation({
    root,
    runId: "b6b1b4d4-6f1f-4a3c-9a3e-1c1d5f4b2c31",
    command: "chrome",
    gracefulTimeoutMs: 50,
    async startProcess() {
      return {
        pid: 8383,
        async waitForExit() {
          calls.push("wait");
          return {};
        },
        async forceTerminate() {
          calls.push("force");
        },
      };
    },
  });

  await preCdpTerminationApi(session)();

  assert.deepEqual(calls, ["force"]);

  const next = await acquireBrowserAutomationProfile({
    root,
    runId: "f901e7cb-2f4c-4b0f-8d1c-33a4bf0a5df1",
  });
  assert.equal(next.profilePath, session.profilePath);
  await next.release();
});

test("failed pre-CDP termination keeps the persistent profile lease fail-closed", async () => {
  const parent = await createTestTempRoot("benes-browser-pre-cdp-terminate-error-");
  const root = path.join(parent, "automation");
  const calls: string[] = [];

  const session = await startBrowserAutomation({
    root,
    runId: "a1d85f0e-3c9a-4c86-8c1e-0f2e6a7b9d42",
    command: "chrome",
    gracefulTimeoutMs: 50,
    async startProcess() {
      return {
        pid: 8484,
        async waitForExit() {
          calls.push("wait");
          return {};
        },
        async forceTerminate() {
          calls.push("force");
          throw new Error("owned termination failed");
        },
      };
    },
  });

  await assert.rejects(preCdpTerminationApi(session)(), /owned termination failed/i);

  assert.deepEqual(calls, ["force"]);

  await assert.rejects(
    acquireBrowserAutomationProfile({
      root,
      runId: "5c7a0f67-9b1d-4b58-9c0f-2b3a4d5e6f70",
    }),
    /lease|in use/i,
  );
});

test("profile preparation failure releases the lease and never starts Chrome", async () => {
  const parent = await createTestTempRoot("benes-browser-before-start-error-");
  const root = path.join(parent, "automation");
  let starts = 0;

  await assert.rejects(
    startBrowserAutomation({
      root,
      runId: "c0dfcdfc-21e4-40fa-a9cb-91cbe9fc3fab",
      command: "chrome",
      gracefulTimeoutMs: 50,
      async beforeStart() {
        throw new Error("profile preparation failed");
      },
      async startProcess() {
        starts += 1;
        throw new Error("unexpected browser start");
      },
    } as Parameters<typeof startBrowserAutomation>[0] & {
      beforeStart: () => Promise<void>;
    }),
    /profile preparation failed/i,
  );

  assert.equal(starts, 0);

  const next = await acquireBrowserAutomationProfile({
    root,
    runId: "fc3120fa-09e4-42df-9404-941d9a42eebb",
  });
  await next.release();
});
