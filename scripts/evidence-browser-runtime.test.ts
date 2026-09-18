import assert from "node:assert/strict";
import { access } from "node:fs/promises";
import path from "node:path";
import { describe, test } from "node:test";

import * as browserRuntime from "./evidence-browser-runtime.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

const { createBrowserRun, withOverallDeadline } = browserRuntime;

type BrowserAutomationProfileLease = {
  profilePath: string;
  markerPath: string;
  leasePath: string;
  browserArgs: string[];
  release(): Promise<void>;
};

type BrowserShutdownResult = {
  forced: boolean;
};

type BrowserCdpController = {
  request<T>(method: string, params?: Record<string, unknown>): Promise<T>;
};

type OwnedBrowserProcessHandle = {
  pid: number;
  waitForExit(): Promise<unknown>;
  forceTerminate(): Promise<void>;
};

type BrowserAutomationSession = {
  pid: number;
  profilePath: string;
  browserArgs: string[];
  shutdown(cdp: BrowserCdpController): Promise<BrowserShutdownResult>;
};

type BrowserRuntimeWithPersistentProfile = typeof browserRuntime & {
  acquireBrowserAutomationProfile?: (options: {
    root: string;
    runId: string;
    additionalProtectedPaths?: readonly string[];
  }) => Promise<BrowserAutomationProfileLease>;
  shutdownBrowserGracefully?: (options: {
    requestClose(): Promise<void>;
    waitForExit(): Promise<void>;
    forceTerminate(): Promise<void>;
    gracefulTimeoutMs: number;
  }) => Promise<BrowserShutdownResult>;
  startBrowserAutomation?: (options: {
    root: string;
    runId: string;
    command: string;
    args?: readonly string[];
    gracefulTimeoutMs: number;
    additionalProtectedPaths?: readonly string[];
    beforeStart?: (context: {
      profilePath: string;
      browserArgs: readonly string[];
    }) => Promise<void>;
    startProcess?: (
      command: string,
      args: readonly string[],
    ) => Promise<OwnedBrowserProcessHandle>;
  }) => Promise<BrowserAutomationSession>;
};

function acquirePersistentProfileApi(): NonNullable<BrowserRuntimeWithPersistentProfile["acquireBrowserAutomationProfile"]> {
  const acquire = (browserRuntime as BrowserRuntimeWithPersistentProfile).acquireBrowserAutomationProfile;
  assert.equal(typeof acquire, "function", "persistent browser profile API must exist");
  if (!acquire) throw new Error("persistent browser profile API is unavailable");
  return acquire;
}

function shutdownBrowserGracefullyApi(): NonNullable<BrowserRuntimeWithPersistentProfile["shutdownBrowserGracefully"]> {
  const shutdown = (browserRuntime as BrowserRuntimeWithPersistentProfile).shutdownBrowserGracefully;
  assert.equal(typeof shutdown, "function", "graceful browser shutdown API must exist");
  if (!shutdown) throw new Error("graceful browser shutdown API is unavailable");
  return shutdown;
}

function startBrowserAutomationApi(): NonNullable<BrowserRuntimeWithPersistentProfile["startBrowserAutomation"]> {
  const start = (browserRuntime as BrowserRuntimeWithPersistentProfile).startBrowserAutomation;
  assert.equal(typeof start, "function", "integrated browser automation API must exist");
  if (!start) throw new Error("integrated browser automation API is unavailable");
  return start;
}

function guardAfter(ms: number): Promise<never> {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error("test deadline guard expired")), ms);
  });
}

describe("evidence browser runtime", () => {
  test("creates the browser user-data-dir below owned scratch", async () => {
    const parent = await createTestTempRoot("benes-browser-run-");
    const scratchRoot = path.join(parent, "scratch");

    const run = await createBrowserRun({ scratchRoot });

    assert.equal(path.dirname(run.browserProfilePath), run.scratch.path);
    assert.deepEqual(run.browserArgs, [`--user-data-dir=${run.browserProfilePath}`]);
    await access(run.browserProfilePath);
    await access(run.scratch.markerPath);
  });

  test("rejects a browser profile override outside owned scratch and leaves evidence scratch intact", async () => {
    const parent = await createTestTempRoot("benes-browser-outside-");
    const scratchRoot = path.join(parent, "scratch");
    const runId = "0de8cf83-01df-40f1-93f7-5e2a547a7650";
    const realProfile = path.join(parent, "Users", "ws", "AppData", "Local", "Google", "Chrome", "User Data");

    await assert.rejects(
      createBrowserRun({ scratchRoot, runId, browserProfilePath: realProfile }),
      /browser profile.*owned scratch/i,
    );

    await access(path.join(scratchRoot, runId));
    await access(path.join(scratchRoot, runId, ".benes-evidence-owner.json"));
  });

  test("reuses one persistent owned automation profile across runs", async () => {
    const acquireBrowserAutomationProfile = acquirePersistentProfileApi();
    const parent = await createTestTempRoot("benes-browser-persistent-");
    const root = path.join(parent, "automation");

    const first = await acquireBrowserAutomationProfile({
      root,
      runId: "1972b02a-b35e-4fe2-8a59-8f3a6cae4ff7",
    });

    assert.equal(path.dirname(first.profilePath), root);
    assert.deepEqual(first.browserArgs, [`--user-data-dir=${first.profilePath}`]);
    await access(first.markerPath);
    await access(first.leasePath);
    await first.release();

    const second = await acquireBrowserAutomationProfile({
      root,
      runId: "d06911b8-4e8b-4a9b-bb5d-4fc00ef14047",
    });

    assert.equal(second.profilePath, first.profilePath);
    assert.equal(second.markerPath, first.markerPath);
    await second.release();
  });

  test("fails closed while the persistent automation profile is leased", async () => {
    const acquireBrowserAutomationProfile = acquirePersistentProfileApi();
    const parent = await createTestTempRoot("benes-browser-lease-");
    const root = path.join(parent, "automation");

    const first = await acquireBrowserAutomationProfile({
      root,
      runId: "9622d55a-ded7-4c61-b7e5-9d79f47b5081",
    });

    await assert.rejects(
      acquireBrowserAutomationProfile({
        root,
        runId: "1f6735d1-3377-4218-a1df-76ca066f41f4",
      }),
      /lease|in use/i,
    );

    await first.release();

    const second = await acquireBrowserAutomationProfile({
      root,
      runId: "1f6735d1-3377-4218-a1df-76ca066f41f4",
    });
    await second.release();
  });

  test("rejects a persistent automation profile below a protected path", async () => {
    const acquireBrowserAutomationProfile = acquirePersistentProfileApi();
    const parent = await createTestTempRoot("benes-browser-protected-");
    const protectedProfile = path.join(parent, "Users", "ws");
    const root = path.join(protectedProfile, "BenesAutomation");

    await assert.rejects(
      acquireBrowserAutomationProfile({
        root,
        runId: "e3130b79-c3ab-4abf-b252-f9714189cce5",
        additionalProtectedPaths: [protectedProfile],
      }),
      /protected/i,
    );
  });

  test("runs browser profile preparation after lease acquisition and before process start", async () => {
    const startBrowserAutomation = startBrowserAutomationApi();
    const parent = await createTestTempRoot("benes-browser-before-start-");
    const root = path.join(parent, "automation");
    const calls: string[] = [];

    const session = await startBrowserAutomation({
      root,
      runId: "4f5d8b1c-8fa7-4938-9720-9e967182ee02",
      command: "chrome",
      gracefulTimeoutMs: 50,
      async beforeStart({ profilePath, browserArgs }) {
        calls.push("prepare");
        assert.equal(path.dirname(profilePath), root);
        assert.equal(browserArgs[0], `--user-data-dir=${profilePath}`);
      },
      async startProcess() {
        calls.push("start");
        return {
          pid: 8181,
          async waitForExit() {
            return {};
          },
          async forceTerminate() {},
        };
      },
    });

    assert.deepEqual(calls, ["prepare", "start"]);

    await session.shutdown({
      async request() {
        return undefined as never;
      },
    });
  });

  test("requests Browser.close and avoids force termination when the browser exits gracefully", async () => {
    const shutdownBrowserGracefully = shutdownBrowserGracefullyApi();
    const calls: string[] = [];

    const result = await shutdownBrowserGracefully({
      gracefulTimeoutMs: 50,
      async requestClose() {
        calls.push("close");
      },
      async waitForExit() {
        calls.push("wait");
      },
      async forceTerminate() {
        calls.push("force");
      },
    });

    assert.deepEqual(calls, ["close", "wait"]);
    assert.deepEqual(result, { forced: false });
  });

  test("falls back to owned force termination only after the graceful shutdown deadline", async () => {
    const shutdownBrowserGracefully = shutdownBrowserGracefullyApi();
    const calls: string[] = [];
    const never = new Promise<void>(() => {});

    const result = await Promise.race([
      shutdownBrowserGracefully({
        gracefulTimeoutMs: 10,
        async requestClose() {
          calls.push("close");
        },
        async waitForExit() {
          calls.push("wait");
          await never;
        },
        async forceTerminate() {
          calls.push("force");
        },
      }),
      guardAfter(100),
    ]);

    assert.deepEqual(calls, ["close", "wait", "force"]);
    assert.deepEqual(result, { forced: true });
  });

  test("integrates persistent profile, owned process, Browser.close, and lease release", async () => {
    const startBrowserAutomation = startBrowserAutomationApi();
    const acquireBrowserAutomationProfile = acquirePersistentProfileApi();
    const parent = await createTestTempRoot("benes-browser-integrated-");
    const root = path.join(parent, "automation");
    const calls: string[] = [];
    let exited = false;

    const session = await startBrowserAutomation({
      root,
      runId: "d1b1acda-ce15-440e-bd19-9ca8d6239abc",
      command: "chrome",
      args: ["about:blank"],
      gracefulTimeoutMs: 50,
      async startProcess(command, args) {
        calls.push(`start:${command}`);
        assert.match(args[0] ?? "", /^--user-data-dir=/);
        assert.equal(args.at(-1), "about:blank");
        return {
          pid: 4242,
          async waitForExit() {
            calls.push("wait");
            if (!exited) throw new Error("browser has not exited");
            return {};
          },
          async forceTerminate() {
            calls.push("force");
            exited = true;
          },
        };
      },
    });

    assert.equal(session.pid, 4242);
    assert.match(session.browserArgs[0] ?? "", /^--user-data-dir=/);

    const result = await session.shutdown({
      async request(method) {
        calls.push(`cdp:${method}`);
        assert.equal(method, "Browser.close");
        exited = true;
        return undefined as never;
      },
    });

    assert.deepEqual(result, { forced: false });
    assert.deepEqual(calls, ["start:chrome", "cdp:Browser.close", "wait"]);

    const next = await acquireBrowserAutomationProfile({
      root,
      runId: "8afce47b-a856-4638-bf1c-b2519afddf4a",
    });
    assert.equal(next.profilePath, session.profilePath);
    await next.release();
  });

  test("integrated browser automation falls back to owned termination and still releases the profile lease", async () => {
    const startBrowserAutomation = startBrowserAutomationApi();
    const acquireBrowserAutomationProfile = acquirePersistentProfileApi();
    const parent = await createTestTempRoot("benes-browser-integrated-timeout-");
    const root = path.join(parent, "automation");
    const calls: string[] = [];
    const never = new Promise<never>(() => {});

    const session = await startBrowserAutomation({
      root,
      runId: "13f8602c-a11b-43d9-8ca6-788cb91290fd",
      command: "chrome",
      gracefulTimeoutMs: 10,
      async startProcess() {
        return {
          pid: 5252,
          async waitForExit() {
            calls.push("wait");
            await never;
          },
          async forceTerminate() {
            calls.push("force");
          },
        };
      },
    });

    const result = await Promise.race([
      session.shutdown({
        async request(method) {
          calls.push(`cdp:${method}`);
          return undefined as never;
        },
      }),
      guardAfter(100),
    ]);

    assert.deepEqual(result, { forced: true });
    assert.deepEqual(calls, ["cdp:Browser.close", "wait", "force"]);

    const next = await acquireBrowserAutomationProfile({
      root,
      runId: "5bfe95b7-e6cb-4f30-b265-d3521acc44d1",
    });
    assert.equal(next.profilePath, session.profilePath);
    await next.release();
  });

  test("overall deadline rejects a hung run after invoking owned timeout teardown", async () => {
    let timedOut = 0;
    const never = new Promise<never>(() => {});

    await assert.rejects(
      withOverallDeadline(never, 15, async () => { timedOut += 1; }),
      /evidence run timed out/i,
    );
    assert.equal(timedOut, 1);
  });

  test("overall deadline remains bounded when timeout teardown never settles", async () => {
    const never = new Promise<never>(() => {});

    await assert.rejects(
      Promise.race([
        withOverallDeadline(never, 10, () => never, 20),
        guardAfter(100),
      ]),
      /timeout teardown timed out after 20ms/i,
    );
  });

  test("overall deadline stays idle when work completes", async () => {
    let timedOut = 0;
    const result = await withOverallDeadline(Promise.resolve("ok"), 1_000, async () => {
      timedOut += 1;
    });
    assert.equal(result, "ok");
    assert.equal(timedOut, 0);
  });
});
