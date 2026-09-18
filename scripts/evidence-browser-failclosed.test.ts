import assert from "node:assert/strict";
import path from "node:path";
import { test } from "node:test";

import {
  acquireBrowserAutomationProfile,
  startBrowserAutomation,
} from "./evidence-browser-runtime.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

test("CDP Browser.close failure force-terminates the owned browser before releasing its profile lease", async () => {
  const parent = await createTestTempRoot("benes-browser-close-error-");
  const root = path.join(parent, "automation");
  const calls: string[] = [];

  const session = await startBrowserAutomation({
    root,
    runId: "c8fb978a-8056-4d40-9f0e-9ebfd57bb7e5",
    command: "chrome",
    gracefulTimeoutMs: 50,
    async startProcess() {
      return {
        pid: 6262,
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

  await assert.rejects(
    session.shutdown({
      async request(method) {
        calls.push(`cdp:${method}`);
        throw new Error("cdp close failed");
      },
    }),
    /cdp close failed/i,
  );

  assert.deepEqual(calls, ["cdp:Browser.close", "force"]);

  const next = await acquireBrowserAutomationProfile({
    root,
    runId: "db8b7381-a25b-4fef-b0ff-69dc56e20c79",
  });
  assert.equal(next.profilePath, session.profilePath);
  await next.release();
});

test("failed owned force termination keeps the persistent browser profile lease fail-closed", async () => {
  const parent = await createTestTempRoot("benes-browser-force-error-");
  const root = path.join(parent, "automation");
  const calls: string[] = [];

  const session = await startBrowserAutomation({
    root,
    runId: "8dd2969b-a8b8-4d69-a247-0797448e1b44",
    command: "chrome",
    gracefulTimeoutMs: 50,
    async startProcess() {
      return {
        pid: 7272,
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

  await assert.rejects(
    session.shutdown({
      async request(method) {
        calls.push(`cdp:${method}`);
        throw new Error("cdp close failed");
      },
    }),
    /owned termination failed/i,
  );

  assert.deepEqual(calls, ["cdp:Browser.close", "force"]);

  await assert.rejects(
    acquireBrowserAutomationProfile({
      root,
      runId: "28d73b89-c2fd-4236-b9b6-291648f86bbb",
    }),
    /lease|in use/i,
  );
});
