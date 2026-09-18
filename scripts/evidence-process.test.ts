import assert from "node:assert/strict";
import { spawn } from "node:child_process";
import { describe, test } from "node:test";

import * as evidenceProcess from "./evidence-process.ts";

const {
  runBoundedTerminationCommand,
  runOwnedProcess,
  windowsTerminationArgs,
} = evidenceProcess;

type OwnedProcessResult = Awaited<ReturnType<typeof runOwnedProcess>>;

type OwnedProcessHandle = {
  pid: number;
  waitForExit(): Promise<OwnedProcessResult>;
  forceTerminate(): Promise<void>;
};

type EvidenceProcessWithHandle = typeof evidenceProcess & {
  startOwnedProcess?: (
    command: string,
    args: readonly string[],
    options?: {
      cwd?: string;
      env?: NodeJS.ProcessEnv;
      killGraceMs?: number;
      terminationTimeoutMs?: number;
      platform?: NodeJS.Platform;
      terminateTree?: (pid: number) => Promise<void>;
    },
  ) => Promise<OwnedProcessHandle>;
};

function startOwnedProcessApi(): NonNullable<EvidenceProcessWithHandle["startOwnedProcess"]> {
  const startOwnedProcess = (evidenceProcess as EvidenceProcessWithHandle).startOwnedProcess;
  assert.equal(typeof startOwnedProcess, "function", "owned process handle API must exist");
  if (!startOwnedProcess) throw new Error("owned process handle API is unavailable");
  return startOwnedProcess;
}

function pidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function guardAfter(ms: number): Promise<never> {
  return new Promise((_, reject) => {
    setTimeout(() => reject(new Error("test deadline guard expired")), ms);
  });
}

describe("owned evidence process lifecycle", () => {
  test("returns output for a normally exiting child", async () => {
    const result = await runOwnedProcess(process.execPath, ["-e", "process.stdout.write('ok')"], {
      timeoutMs: 2_000,
    });

    assert.equal(result.exitCode, 0);
    assert.equal(result.stdout, "ok");
    assert.equal(result.timedOut, false);
    assert.ok(result.pid > 0);
  });

  test("exposes a caller-controlled owned process handle for graceful browser shutdown", async () => {
    const startOwnedProcess = startOwnedProcessApi();
    const handle = await startOwnedProcess(
      process.execPath,
      ["-e", "setTimeout(() => { process.stdout.write('done'); process.exit(0); }, 20)"],
      { terminationTimeoutMs: 500 },
    );

    assert.ok(handle.pid > 0);
    const result = await Promise.race([handle.waitForExit(), guardAfter(1_000)]);
    assert.equal(result.pid, handle.pid);
    assert.equal(result.stdout, "done");
    assert.equal(result.exitCode, 0);
    assert.equal(result.timedOut, false);
  });

  test("caller-controlled force termination targets only the recorded owned pid", async () => {
    const startOwnedProcess = startOwnedProcessApi();
    const unrelated = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
      stdio: "ignore",
    });
    assert.ok(unrelated.pid);

    const terminated: number[] = [];
    let handle: OwnedProcessHandle | undefined;
    try {
      handle = await startOwnedProcess(
        process.execPath,
        ["-e", "setInterval(() => {}, 1000)"],
        {
          terminationTimeoutMs: 500,
          terminateTree: async (pid) => {
            terminated.push(pid);
            process.kill(pid, "SIGKILL");
          },
        },
      );

      await handle.forceTerminate();
      const result = await Promise.race([handle.waitForExit(), guardAfter(1_000)]);

      assert.deepEqual(terminated, [handle.pid]);
      assert.equal(result.pid, handle.pid);
      assert.equal(pidAlive(unrelated.pid), true);
    } finally {
      if (handle && pidAlive(handle.pid)) process.kill(handle.pid, "SIGKILL");
      if (unrelated.pid && pidAlive(unrelated.pid)) process.kill(unrelated.pid, "SIGKILL");
    }
  });

  test("timeout terminates only the recorded owned pid", async () => {
    const unrelated = spawn(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
      stdio: "ignore",
    });
    assert.ok(unrelated.pid);

    const terminated: number[] = [];
    try {
      const result = await runOwnedProcess(process.execPath, ["-e", "setInterval(() => {}, 1000)"], {
        timeoutMs: 30,
        terminateTree: async (pid) => {
          terminated.push(pid);
          process.kill(pid, "SIGKILL");
        },
      });

      assert.equal(result.timedOut, true);
      assert.deepEqual(terminated, [result.pid]);
      assert.notEqual(result.pid, unrelated.pid);
      assert.equal(pidAlive(unrelated.pid), true);
    } finally {
      if (unrelated.pid && pidAlive(unrelated.pid)) process.kill(unrelated.pid, "SIGKILL");
    }
  });

  test("timeout remains bounded when the owned tree terminator never settles", async () => {
    const never = new Promise<void>(() => {});
    const run = runOwnedProcess(
      process.execPath,
      ["-e", "setTimeout(() => process.exit(0), 250)"],
      {
        timeoutMs: 10,
        terminationTimeoutMs: 20,
        terminateTree: async () => await never,
      },
    );

    await assert.rejects(
      Promise.race([run, guardAfter(100)]),
      /owned process termination timed out after 20ms/i,
    );
  });

  test("timeout remains bounded when the terminator returns without closing the child", async () => {
    const run = runOwnedProcess(
      process.execPath,
      ["-e", "setTimeout(() => process.exit(0), 250)"],
      {
        timeoutMs: 10,
        terminationTimeoutMs: 20,
        terminateTree: async () => {},
      },
    );

    await assert.rejects(
      Promise.race([run, guardAfter(100)]),
      /owned process termination timed out after 20ms/i,
    );
  });

  test("termination helper process is itself bounded when it hangs", async () => {
    const run = runBoundedTerminationCommand(
      process.execPath,
      ["-e", "setInterval(() => {}, 1000)"],
      25,
    );

    await assert.rejects(
      Promise.race([run, guardAfter(250)]),
      /termination command timed out after 25ms/i,
    );
  });

  test("Windows tree termination arguments address a pid, never an executable name", () => {
    assert.deepEqual(windowsTerminationArgs(51924), ["/PID", "51924", "/T", "/F"]);
  });
});
