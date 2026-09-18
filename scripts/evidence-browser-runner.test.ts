import assert from "node:assert/strict";
import { access, mkdir, readFile, writeFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";

import {
  createBrowserEvidenceRunner,
  type BrowserEvidenceDependencies,
  type BrowserEvidenceOptions,
} from "./evidence-browser-runner.ts";
import { startBrowserAutomation } from "./evidence-browser-runtime.ts";
import {
  createEvidenceScratch,
  removeEvidenceScratch,
} from "./evidence-scratch.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

const RUN_ID = "78ecfe4c-d72d-4f38-965b-8c750724722a";
const SCRATCH_ROOT = "/safe/scratch";
const AUTOMATION_PROFILE_ROOT = "/safe/automation";
const SCRATCH_PATH = `${SCRATCH_ROOT}/${RUN_ID}`;
const PROFILE_PATH = `${AUTOMATION_PROFILE_ROOT}/chrome-user-data`;

const FORBIDDEN_CALLER_BROWSER_ARGS = [
  "--user-data-dir",
  "--user-data-dir=/tmp/caller-owned-profile",
  "--remote-debugging-port",
  "--remote-debugging-port=9222",
  "--remote-debugging-pipe",
];

function guardAfter(ms: number): Promise<never> {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error("test deadline guard expired")), ms);
  });
}

function runnerOptions(overrides: Partial<BrowserEvidenceOptions> = {}): BrowserEvidenceOptions {
  return {
    scratchRoot: SCRATCH_ROOT,
    automationProfileRoot: AUTOMATION_PROFILE_ROOT,
    browserCommand: "chrome",
    overallTimeoutMs: 1_000,
    cdpRequestTimeoutMs: 100,
    startupTimeoutMs: 100,
    gracefulShutdownTimeoutMs: 100,
    ...overrides,
  };
}

function createScratchStub(calls: string[]): BrowserEvidenceDependencies["createScratch"] {
  return async function createScratch(root) {
    calls.push("scratch:create");
    assert.equal(root, SCRATCH_ROOT);
    return {
      runId: RUN_ID,
      root: SCRATCH_ROOT,
      path: SCRATCH_PATH,
      markerPath: `${SCRATCH_PATH}/.benes-evidence-owner.json`,
    };
  };
}

function createBrowserStub(
  calls: string[],
  overrides: {
    shutdown?(cdp: { request<T>(method: string, params?: Record<string, unknown>): Promise<T> }): Promise<void>;
    terminateBeforeCdp?(): Promise<void>;
  } = {},
): BrowserEvidenceDependencies["startBrowser"] {
  return async function startBrowser() {
    calls.push("browser:start");
    return {
      pid: 9001,
      profilePath: PROFILE_PATH,
      browserArgs: [`--user-data-dir=${PROFILE_PATH}`, "--remote-debugging-port=0"],
      async terminateBeforeCdp() {
        calls.push("browser:terminate-before-cdp");
        await overrides.terminateBeforeCdp?.();
      },
      async shutdown(cdp) {
        calls.push("browser:shutdown");
        await overrides.shutdown?.(cdp);
        return { forced: false };
      },
    };
  };
}

function createCdpStub(
  calls: string[],
  overrides: { connect?(): Promise<void> } = {},
): BrowserEvidenceDependencies["connectCdp"] {
  return async function connectCdp() {
    calls.push("cdp:connect");
    await overrides.connect?.();
    return {
      cdp: {
        async request<T>(method: string) {
          calls.push(`cdp:${method}`);
          return undefined as T;
        },
      },
      dispose() {
        calls.push("cdp:dispose");
      },
    };
  };
}

test("canonical browser evidence runner factory exists", () => {
  assert.equal(
    typeof createBrowserEvidenceRunner,
    "function",
    "canonical evidence browser runner factory must exist",
  );
});

test("owns scratch, browser session, task, graceful shutdown, and successful cleanup", async () => {
  const scratchRoot = "/safe/scratch";
  const automationProfileRoot = "/safe/automation";
  const runId = "78ecfe4c-d72d-4f38-965b-8c750724722a";
  const scratchPath = `${scratchRoot}/${runId}`;
  const profilePath = `${automationProfileRoot}/chrome-user-data`;
  const calls: string[] = [];

  const dependencies: BrowserEvidenceDependencies = {
    async createScratch(root) {
      calls.push("scratch:create");
      assert.equal(root, scratchRoot);
      return {
        runId,
        root: scratchRoot,
        path: scratchPath,
        markerPath: `${scratchPath}/.benes-evidence-owner.json`,
      };
    },
    async removeScratch(root, target) {
      calls.push("scratch:remove");
      assert.equal(root, scratchRoot);
      assert.equal(target, scratchPath);
    },
    async startBrowser(options) {
      calls.push("browser:start");
      assert.equal(options.root, automationProfileRoot);
      assert.equal(options.runId, runId);
      assert.equal(options.command, "chrome");
      assert.deepEqual(options.args, ["--remote-debugging-port=0", "about:blank"]);
      return {
        pid: 9001,
        profilePath,
        browserArgs: [
          `--user-data-dir=${profilePath}`,
          "--remote-debugging-port=0",
          "about:blank",
        ],
        async terminateBeforeCdp() {
          calls.push("browser:terminate-before-cdp");
        },
        async shutdown(cdp) {
          calls.push("browser:shutdown");
          await cdp.request("Browser.close");
          return { forced: false };
        },
      };
    },
    async connectCdp(input) {
      calls.push("cdp:connect");
      assert.equal(input.profilePath, profilePath);
      assert.equal(input.startupTimeoutMs, 100);
      assert.equal(input.requestTimeoutMs, 100);
      return {
        cdp: {
          async request(method) {
            calls.push(`cdp:${method}`);
            return { product: "Chrome/Test" } as never;
          },
        },
        dispose() {
          calls.push("cdp:dispose");
        },
      };
    },
  };

  const run = createBrowserEvidenceRunner(dependencies);

  const result = await run(
    {
      scratchRoot,
      automationProfileRoot,
      browserCommand: "chrome",
      browserArgs: ["about:blank"],
      overallTimeoutMs: 1_000,
      cdpRequestTimeoutMs: 100,
      startupTimeoutMs: 100,
      gracefulShutdownTimeoutMs: 100,
    },
    async (context) => {
      calls.push("task");
      assert.equal(context.scratchPath, scratchPath);
      assert.equal(context.browserPid, 9001);
      assert.equal(context.browserProfilePath, profilePath);
      await context.cdp.request("Browser.getVersion");
      return "ok";
    },
  );

  assert.equal(result, "ok");
  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "task",
    "cdp:Browser.getVersion",
    "browser:shutdown",
    "cdp:Browser.close",
    "cdp:dispose",
    "scratch:remove",
  ]);
});

test("keeps scratch, runs managed shutdown, and disposes CDP when the task fails after connecting", async () => {
  const calls: string[] = [];
  const taskError = new Error("evidence task failed");
  let shutdowns = 0;

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    startBrowser: createBrowserStub(calls, {
      async shutdown(cdp) {
        shutdowns += 1;
        await cdp.request("Browser.close");
      },
    }),
    connectCdp: createCdpStub(calls),
  });

  await assert.rejects(
    run(runnerOptions(), async () => {
      throw taskError;
    }),
    (error) => error === taskError,
  );

  assert.equal(shutdowns, 1);
  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:shutdown",
    "cdp:Browser.close",
    "cdp:dispose",
  ]);
});

test("aggregates the task failure and the browser shutdown failure instead of hiding either", async () => {
  const calls: string[] = [];
  const taskError = new Error("evidence task failed");
  const shutdownError = new Error("browser shutdown failed");

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    startBrowser: createBrowserStub(calls, {
      async shutdown() {
        throw shutdownError;
      },
    }),
    connectCdp: createCdpStub(calls),
  });

  await assert.rejects(
    run(runnerOptions(), async () => {
      throw taskError;
    }),
    (error) => {
      assert.ok(
        error instanceof AggregateError,
        "the shutdown failure must not replace the primary task failure",
      );
      assert.equal(error.errors[0], taskError);
      assert.equal(error.errors[1], shutdownError);
      return true;
    },
  );

  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:shutdown",
    "cdp:dispose",
  ]);
});

test("overall timeout converges on one managed shutdown and retains scratch", async () => {
  const calls: string[] = [];
  const taskError = new Error("task failed after the deadline");
  const never = new Promise<never>(() => {});
  let abandonTask: ((error: Error) => void) | undefined;

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    startBrowser: createBrowserStub(calls, {
      async shutdown(cdp) {
        await cdp.request("Browser.close");
      },
    }),
    connectCdp: createCdpStub(calls),
  });

  await assert.rejects(
    Promise.race([
      run(
        runnerOptions({ overallTimeoutMs: 20 }),
        async () => {
          await new Promise<never>((_, reject) => {
            abandonTask = reject;
          });
          return await never;
        },
      ),
      guardAfter(2_000),
    ]),
    /evidence run timed out after 20ms/i,
  );

  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:shutdown",
    "cdp:Browser.close",
  ]);

  abandonTask?.(taskError);
  await new Promise((resolve) => setTimeout(resolve, 30));

  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:shutdown",
    "cdp:Browser.close",
    "cdp:dispose",
  ]);
});

test("pre-CDP termination handles a CDP startup failure without attempting CDP shutdown", async () => {
  const calls: string[] = [];
  const cdpError = new Error("CDP startup failed");

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    startBrowser: createBrowserStub(calls),
    connectCdp: createCdpStub(calls, {
      async connect() {
        throw cdpError;
      },
    }),
  });

  await assert.rejects(
    run(runnerOptions(), async () => "unused"),
    (error) => error === cdpError,
  );

  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:terminate-before-cdp",
  ]);
});

test("aggregates the CDP startup failure and the pre-CDP termination failure", async () => {
  const calls: string[] = [];
  const cdpError = new Error("CDP startup failed");
  const terminationError = new Error("owned termination failed");

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    startBrowser: createBrowserStub(calls, {
      async terminateBeforeCdp() {
        throw terminationError;
      },
    }),
    connectCdp: createCdpStub(calls, {
      async connect() {
        throw cdpError;
      },
    }),
  });

  await assert.rejects(
    run(runnerOptions(), async () => "unused"),
    (error) => {
      assert.ok(
        error instanceof AggregateError,
        "the termination failure must not replace the CDP startup failure",
      );
      assert.equal(error.errors[0], cdpError);
      assert.equal(error.errors[1], terminationError);
      return true;
    },
  );

  assert.deepEqual(calls, [
    "scratch:create",
    "browser:start",
    "cdp:connect",
    "browser:terminate-before-cdp",
  ]);
});

test("a browser start failure retains scratch and never connects CDP", async () => {
  const calls: string[] = [];
  const startError = new Error("browser start failed");

  const run = createBrowserEvidenceRunner({
    createScratch: createScratchStub(calls),
    async removeScratch() {
      calls.push("scratch:remove");
    },
    async startBrowser() {
      calls.push("browser:start");
      throw startError;
    },
    connectCdp: createCdpStub(calls),
  });

  await assert.rejects(
    run(runnerOptions(), async () => "unused"),
    (error) => error === startError,
  );

  assert.deepEqual(calls, ["scratch:create", "browser:start"]);
});

test("rejects lifecycle-critical caller browser arguments before any lifecycle side effect", async () => {
  for (const argument of FORBIDDEN_CALLER_BROWSER_ARGS) {
    const calls: string[] = [];

    const run = createBrowserEvidenceRunner({
      createScratch: createScratchStub(calls),
      async removeScratch() {
        calls.push("scratch:remove");
      },
      startBrowser: createBrowserStub(calls),
      connectCdp: createCdpStub(calls),
    });

    await assert.rejects(
      run(runnerOptions({ browserArgs: [argument] }), async () => "unused"),
      /lifecycle-critical browser argument/i,
      `caller-supplied ${argument} must be rejected`,
    );

    assert.deepEqual(
      calls,
      [],
      `caller-supplied ${argument} must not create scratch, a profile lease, or a browser process`,
    );
  }
});

test("sequential successful runs reuse one persistent profile and distinct throwaway scratch", async () => {
  const parent = await createTestTempRoot("benes-browser-runner-reuse-");
  const scratchRoot = path.join(parent, "scratch");
  const automationProfileRoot = path.join(parent, "automation");

  const run = createBrowserEvidenceRunner({
    createScratch: createEvidenceScratch,
    removeScratch: removeEvidenceScratch,
    async startBrowser(options) {
      return await startBrowserAutomation({
        root: options.root,
        runId: options.runId,
        command: options.command,
        args: options.args,
        gracefulTimeoutMs: options.gracefulTimeoutMs,
        additionalProtectedPaths: options.additionalProtectedPaths,
        beforeStart: options.beforeStart,
        async startProcess() {
          return {
            pid: 9701,
            async waitForExit() {
              return {};
            },
            async forceTerminate() {},
          };
        },
      });
    },
    connectCdp: createCdpStub([]),
  });

  const options = runnerOptions({ scratchRoot, automationProfileRoot });
  const scratchPaths: string[] = [];
  const profilePaths: string[] = [];

  for (const expected of ["first", "second"]) {
    const result = await run(options, async (context) => {
      scratchPaths.push(context.scratchPath);
      profilePaths.push(context.browserProfilePath);
      return expected;
    });
    assert.equal(result, expected);
  }

  assert.equal(profilePaths[0], profilePaths[1], "both runs must reuse one persistent automation profile");
  assert.notEqual(scratchPaths[0], scratchPaths[1], "each run must own a distinct evidence scratch directory");
  assert.match(scratchPaths[0] ?? "", /[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);
  assert.match(scratchPaths[1] ?? "", /[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i);

  for (const scratchPath of scratchPaths) {
    await assert.rejects(access(scratchPath), "successful runs must delete their own evidence scratch");
  }

  await access(profilePaths[0] ?? "");
});

test("removes stale DevToolsActivePort before starting Chrome", async () => {
  const parent = await createTestTempRoot("benes-browser-runner-devtools-stale-");
  const scratchRoot = path.join(parent, "scratch");
  const automationProfileRoot = path.join(parent, "automation");
  const profilePath = path.join(automationProfileRoot, "chrome-user-data");
  const controlPath = path.join(profilePath, "DevToolsActivePort");
  const runId = "7419f84e-6da7-4ca9-8eab-787e5e67fc5d";
  const scratchPath = path.join(scratchRoot, runId);

  await mkdir(profilePath, { recursive: true });
  await writeFile(controlPath, "1111\n/devtools/browser/stale\n", "utf8");

  const run = createBrowserEvidenceRunner({
    async createScratch() {
      return {
        runId,
        root: scratchRoot,
        path: scratchPath,
        markerPath: path.join(scratchPath, ".benes-evidence-owner.json"),
      };
    },
    async removeScratch() {},
    async startBrowser(options) {
      assert.equal(
        typeof options.beforeStart,
        "function",
        "canonical runner must prepare the persistent profile before Chrome starts",
      );

      await options.beforeStart?.({
        profilePath,
        browserArgs: [
          `--user-data-dir=${profilePath}`,
          "--remote-debugging-port=0",
        ],
      });

      await assert.rejects(readFile(controlPath, "utf8"), /ENOENT/);

      return {
        pid: 9101,
        profilePath,
        browserArgs: [
          `--user-data-dir=${profilePath}`,
          "--remote-debugging-port=0",
        ],
        async terminateBeforeCdp() {},
        async shutdown() {
          return { forced: false };
        },
      };
    },
    async connectCdp() {
      return {
        cdp: {
          async request() {
            return undefined as never;
          },
        },
        dispose() {},
      };
    },
  });

  await run(
    {
      scratchRoot,
      automationProfileRoot,
      browserCommand: "chrome",
      overallTimeoutMs: 1_000,
      cdpRequestTimeoutMs: 100,
      startupTimeoutMs: 100,
      gracefulShutdownTimeoutMs: 100,
    },
    async () => "ok",
  );
});
