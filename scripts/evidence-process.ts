import { spawn } from "node:child_process";

export type OwnedProcessResult = {
  pid: number;
  stdout: string;
  stderr: string;
  exitCode: number;
  timedOut: boolean;
};

type RunOwnedProcessOptions = {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  timeoutMs: number;
  killGraceMs?: number;
  terminationTimeoutMs?: number;
  platform?: NodeJS.Platform;
  terminateTree?: (pid: number) => Promise<void>;
};

type StartOwnedProcessOptions = {
  cwd?: string;
  env?: NodeJS.ProcessEnv;
  killGraceMs?: number;
  terminationTimeoutMs?: number;
  platform?: NodeJS.Platform;
  terminateTree?: (pid: number) => Promise<void>;
};

export type OwnedProcessHandle = {
  pid: number;
  waitForExit(): Promise<OwnedProcessResult>;
  forceTerminate(): Promise<void>;
};

function sleep(ms: number): Promise<void> {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

function pidAlive(pid: number): boolean {
  try {
    process.kill(pid, 0);
    return true;
  } catch {
    return false;
  }
}

function killGroup(pid: number, signal: NodeJS.Signals): void {
  try {
    process.kill(-pid, signal);
  } catch (error) {
    const code = (error as NodeJS.ErrnoException).code;
    if (code !== "ESRCH") throw error;
  }
}

export function windowsTerminationArgs(pid: number): string[] {
  if (!Number.isSafeInteger(pid) || pid <= 0) {
    throw new Error("owned process pid must be a positive integer");
  }
  return ["/PID", String(pid), "/T", "/F"];
}

export async function runBoundedTerminationCommand(
  command: string,
  args: readonly string[],
  timeoutMs: number,
): Promise<number | null> {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
    throw new Error("termination command timeout must be greater than zero");
  }

  return await new Promise<number | null>((resolve, reject) => {
    const proc = spawn(command, [...args], {
      windowsHide: true,
      stdio: "ignore",
    });
    let settled = false;

    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;
      let killFailure = "";
      try {
        if (!proc.kill("SIGKILL")) {
          killFailure = "; termination helper could not be killed";
        }
      } catch (error) {
        killFailure = `; termination helper kill failed: ${error instanceof Error ? error.message : String(error)}`;
      }
      proc.unref();
      reject(new Error(`termination command timed out after ${timeoutMs}ms${killFailure}`));
    }, timeoutMs);

    proc.once("error", (error) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      reject(error);
    });

    proc.once("close", (code) => {
      if (settled) return;
      settled = true;
      clearTimeout(timer);
      resolve(code);
    });
  });
}

async function terminateWindowsTree(pid: number, helperTimeoutMs: number): Promise<void> {
  const code = await runBoundedTerminationCommand("taskkill", windowsTerminationArgs(pid), helperTimeoutMs);
  if (code === 0 || !pidAlive(pid)) return;
  throw new Error(`taskkill failed for owned pid ${pid} with exit code ${code ?? 1}`);
}

async function terminatePosixTree(pid: number, killGraceMs: number): Promise<void> {
  killGroup(pid, "SIGTERM");
  if (killGraceMs > 0) await sleep(killGraceMs);
  killGroup(pid, "SIGKILL");
}

export async function startOwnedProcess(
  command: string,
  args: readonly string[],
  options: StartOwnedProcessOptions = {},
): Promise<OwnedProcessHandle> {
  const terminationTimeoutMs = options.terminationTimeoutMs ?? 5_000;
  if (!Number.isFinite(terminationTimeoutMs) || terminationTimeoutMs <= 0) {
    throw new Error("owned process termination timeout must be greater than zero");
  }

  const platform = options.platform ?? process.platform;
  const killGraceMs = options.killGraceMs ?? 1_000;
  const child = spawn(command, [...args], {
    cwd: options.cwd,
    env: options.env,
    windowsHide: true,
    detached: platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
  const pid = child.pid;
  if (!pid) {
    child.once("error", () => { /* prevent an unhandled spawn error after fail-closed rejection */ });
    child.kill();
    throw new Error(`failed to start owned process: ${command}`);
  }

  const helperTimeoutMs = Math.max(1, Math.floor(terminationTimeoutMs * 0.8));
  const terminateTree = options.terminateTree ?? (
    platform === "win32"
      ? (ownedPid: number) => terminateWindowsTree(ownedPid, helperTimeoutMs)
      : (ownedPid: number) => terminatePosixTree(ownedPid, killGraceMs)
  );

  let stdout = "";
  let stderr = "";
  child.stdout?.setEncoding("utf8");
  child.stderr?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => { stdout += chunk; });
  child.stderr?.on("data", (chunk: string) => { stderr += chunk; });

  const exit = new Promise<OwnedProcessResult>((resolve, reject) => {
    child.once("error", reject);
    child.once("close", (code) => {
      resolve({
        pid,
        stdout,
        stderr,
        exitCode: code ?? 1,
        timedOut: false,
      });
    });
  });
  void exit.catch(() => { /* waitForExit retains the original rejection */ });

  let termination: Promise<void> | undefined;
  return {
    pid,
    waitForExit(): Promise<OwnedProcessResult> {
      return exit;
    },
    forceTerminate(): Promise<void> {
      if (!pidAlive(pid)) return Promise.resolve();
      if (termination) return termination;

      termination = new Promise<void>((resolve, reject) => {
        let settled = false;
        const timer = setTimeout(() => {
          if (settled) return;
          settled = true;
          try { child.kill("SIGKILL"); } catch { /* child may already be gone */ }
          reject(new Error(`owned process termination timed out after ${terminationTimeoutMs}ms`));
        }, terminationTimeoutMs);

        void Promise.resolve().then(() => terminateTree(pid)).then(
          () => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            resolve();
          },
          (error) => {
            if (settled) return;
            settled = true;
            clearTimeout(timer);
            try { child.kill("SIGKILL"); } catch { /* child may already be gone */ }
            reject(error);
          },
        );
      });

      return termination;
    },
  };
}

export async function runOwnedProcess(
  command: string,
  args: readonly string[],
  options: RunOwnedProcessOptions,
): Promise<OwnedProcessResult> {
  if (!Number.isFinite(options.timeoutMs) || options.timeoutMs <= 0) {
    throw new Error("owned process timeout must be greater than zero");
  }

  const terminationTimeoutMs = options.terminationTimeoutMs ?? 5_000;
  if (!Number.isFinite(terminationTimeoutMs) || terminationTimeoutMs <= 0) {
    throw new Error("owned process termination timeout must be greater than zero");
  }

  const platform = options.platform ?? process.platform;
  const killGraceMs = options.killGraceMs ?? 1_000;
  const child = spawn(command, [...args], {
    cwd: options.cwd,
    env: options.env,
    windowsHide: true,
    detached: platform !== "win32",
    stdio: ["ignore", "pipe", "pipe"],
  });
  const pid = child.pid;
  if (!pid) {
    child.once("error", () => { /* prevent an unhandled spawn error after fail-closed rejection */ });
    child.kill();
    throw new Error(`failed to start owned process: ${command}`);
  }

  const helperTimeoutMs = Math.max(1, Math.floor(terminationTimeoutMs * 0.8));
  const terminateTree = options.terminateTree ?? (
    platform === "win32"
      ? (ownedPid: number) => terminateWindowsTree(ownedPid, helperTimeoutMs)
      : (ownedPid: number) => terminatePosixTree(ownedPid, killGraceMs)
  );

  let stdout = "";
  let stderr = "";
  let timedOut = false;
  let terminationError: Error | null = null;
  child.stdout?.setEncoding("utf8");
  child.stderr?.setEncoding("utf8");
  child.stdout?.on("data", (chunk: string) => { stdout += chunk; });
  child.stderr?.on("data", (chunk: string) => { stderr += chunk; });

  return await new Promise<OwnedProcessResult>((resolve, reject) => {
    let settled = false;
    let terminationTimer: NodeJS.Timeout | undefined;

    const clearTimers = (timer: NodeJS.Timeout): void => {
      clearTimeout(timer);
      if (terminationTimer) clearTimeout(terminationTimer);
    };

    const bestEffortDirectKill = (): void => {
      try { child.kill("SIGKILL"); } catch { /* child may already be gone */ }
    };

    const finishReject = (timer: NodeJS.Timeout, error: Error): void => {
      if (settled) return;
      settled = true;
      clearTimers(timer);
      reject(error);
    };

    const timer = setTimeout(() => {
      if (settled) return;
      timedOut = true;

      terminationTimer = setTimeout(() => {
        if (settled) return;
        bestEffortDirectKill();
        const suffix = terminationError ? `; prior termination failure: ${terminationError.message}` : "";
        finishReject(
          timer,
          new Error(`owned process termination timed out after ${terminationTimeoutMs}ms${suffix}`),
        );
      }, terminationTimeoutMs);

      void Promise.resolve().then(() => terminateTree(pid)).catch((error) => {
        if (settled) return;
        terminationError = error instanceof Error ? error : new Error(String(error));
        bestEffortDirectKill();
      });
    }, options.timeoutMs);

    child.once("error", (error) => {
      finishReject(timer, error);
    });

    child.once("close", (code) => {
      if (settled) return;
      settled = true;
      clearTimers(timer);
      if (terminationError) {
        reject(terminationError);
        return;
      }
      resolve({
        pid,
        stdout,
        stderr,
        exitCode: code ?? 1,
        timedOut,
      });
    });
  });
}
