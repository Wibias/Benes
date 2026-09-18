import { homedir } from "node:os";
import { lstat, mkdir, readFile, realpath, unlink, writeFile } from "node:fs/promises";
import path from "node:path";

import { createEvidenceScratch, type EvidenceScratch } from "./evidence-scratch.ts";
import { startOwnedProcess } from "./evidence-process.ts";

const AUTOMATION_PROFILE_NAME = "chrome-user-data";
const AUTOMATION_PROFILE_MARKER = ".benes-browser-profile-owner.json";
const AUTOMATION_PROFILE_LEASE = ".benes-browser-profile.lease";
const UUID_RE = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/i;

type BrowserRunOptions = {
  scratchRoot: string;
  runId?: string;
  browserProfilePath?: string;
};

type BrowserAutomationProfileOptions = {
  root: string;
  runId: string;
  additionalProtectedPaths?: readonly string[];
};

type BrowserAutomationProfileMarker = {
  version: 1;
  root: string;
  profilePath: string;
};

type BrowserAutomationProfileLeaseMarker = {
  version: 1;
  runId: string;
};

type BrowserGracefulShutdownOptions = {
  requestClose(): Promise<void>;
  waitForExit(): Promise<void>;
  forceTerminate(): Promise<void>;
  gracefulTimeoutMs: number;
};

type BrowserCdpController = {
  request<T>(method: string, params?: Record<string, unknown>): Promise<T>;
};

type OwnedBrowserProcessHandle = {
  pid: number;
  waitForExit(): Promise<unknown>;
  forceTerminate(): Promise<void>;
};

type StartBrowserAutomationOptions = {
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
};

export type EvidenceBrowserRun = {
  scratch: EvidenceScratch;
  browserProfilePath: string;
  browserArgs: string[];
};

export type BrowserAutomationProfileLease = {
  profilePath: string;
  markerPath: string;
  leasePath: string;
  browserArgs: string[];
  release(): Promise<void>;
};

export type BrowserShutdownResult = {
  forced: boolean;
};

export type BrowserAutomationSession = {
  pid: number;
  profilePath: string;
  browserArgs: string[];
  shutdown(cdp: BrowserCdpController): Promise<BrowserShutdownResult>;
  terminateBeforeCdp(): Promise<void>;
};

function samePath(a: string, b: string): boolean {
  const left = path.resolve(a);
  const right = path.resolve(b);
  return process.platform === "win32"
    ? left.localeCompare(right, undefined, { sensitivity: "accent" }) === 0
    : left === right;
}

function directChild(parent: string, child: string): boolean {
  return samePath(path.dirname(path.resolve(child)), path.resolve(parent));
}

function isInsideOrEqual(parent: string, child: string): boolean {
  const resolvedParent = path.resolve(parent);
  const resolvedChild = path.resolve(child);
  if (samePath(resolvedParent, resolvedChild)) return true;
  const relative = path.relative(resolvedParent, resolvedChild);
  return relative !== "" && relative !== ".." && !relative.startsWith(`..${path.sep}`) && !path.isAbsolute(relative);
}

function protectedPaths(additionalProtectedPaths: readonly string[] = []): string[] {
  const values = new Set<string>();
  for (const candidate of [process.env.HOME, process.env.USERPROFILE, homedir(), ...additionalProtectedPaths]) {
    if (!candidate) continue;
    values.add(path.resolve(candidate));
  }
  return [...values];
}

function assertAutomationProfileRootAllowed(root: string, additionalProtectedPaths: readonly string[] = []): string {
  if (!path.isAbsolute(root)) {
    throw new Error("browser automation profile root must be absolute");
  }
  const resolvedRoot = path.resolve(root);
  if (samePath(path.parse(resolvedRoot).root, resolvedRoot)) {
    throw new Error("browser automation profile root must not be a drive/filesystem root");
  }
  for (const protectedPath of protectedPaths(additionalProtectedPaths)) {
    if (isInsideOrEqual(protectedPath, resolvedRoot)) {
      throw new Error(`browser automation profile root is inside protected path: ${protectedPath}`);
    }
  }
  return resolvedRoot;
}

async function nearestExistingAncestor(pathValue: string): Promise<string> {
  let current = path.resolve(pathValue);
  for (;;) {
    try {
      await lstat(current);
      return current;
    } catch (error) {
      if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
    }
    const parent = path.dirname(current);
    if (samePath(parent, current)) {
      throw new Error(`no existing ancestor for browser automation profile path: ${pathValue}`);
    }
    current = parent;
  }
}

async function assertCreationPathHasNoSymlink(pathValue: string, label: string): Promise<void> {
  const ancestor = await nearestExistingAncestor(pathValue);
  const actual = await realpath(ancestor);
  if (!samePath(ancestor, actual)) {
    throw new Error(`${label} resolves through a symlink/reparse point: ${ancestor}`);
  }
}

async function assertCanonicalDirectory(pathValue: string, label: string): Promise<void> {
  const info = await lstat(pathValue);
  if (info.isSymbolicLink()) {
    throw new Error(`${label} must not be a symlink/reparse point`);
  }
  if (!info.isDirectory()) {
    throw new Error(`${label} must be a directory`);
  }
  const actual = await realpath(pathValue);
  if (!samePath(pathValue, actual)) {
    throw new Error(`${label} resolves through a symlink/reparse point`);
  }
}

async function ensureAutomationProfileDirectory(profilePath: string): Promise<void> {
  await assertCreationPathHasNoSymlink(profilePath, "browser automation profile");
  try {
    await mkdir(profilePath, { recursive: false });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
  }
  await assertCanonicalDirectory(profilePath, "browser automation profile");
}

function parseAutomationProfileMarker(raw: string): BrowserAutomationProfileMarker {
  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    throw new Error("browser automation profile ownership marker is invalid JSON");
  }
  if (!parsed || typeof parsed !== "object") {
    throw new Error("browser automation profile ownership marker has invalid shape");
  }
  const marker = parsed as Partial<BrowserAutomationProfileMarker>;
  if (marker.version !== 1 || typeof marker.root !== "string" || typeof marker.profilePath !== "string") {
    throw new Error("browser automation profile ownership marker has invalid shape");
  }
  return marker as BrowserAutomationProfileMarker;
}

async function ensureAutomationProfileMarker(
  root: string,
  profilePath: string,
  markerPath: string,
): Promise<void> {
  const expected: BrowserAutomationProfileMarker = { version: 1, root, profilePath };
  try {
    await writeFile(markerPath, `${JSON.stringify(expected)}\n`, { encoding: "utf8", flag: "wx" });
    return;
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "EEXIST") throw error;
  }

  const marker = parseAutomationProfileMarker(await readFile(markerPath, "utf8"));
  if (!samePath(marker.root, root) || !samePath(marker.profilePath, profilePath)) {
    throw new Error("browser automation profile ownership marker does not match requested profile");
  }
}

export async function acquireBrowserAutomationProfile(
  options: BrowserAutomationProfileOptions,
): Promise<BrowserAutomationProfileLease> {
  if (!UUID_RE.test(options.runId)) {
    throw new Error("browser automation profile run id must be a UUID v4");
  }

  const root = assertAutomationProfileRootAllowed(options.root, options.additionalProtectedPaths);
  await assertCreationPathHasNoSymlink(root, "browser automation profile root");
  await mkdir(root, { recursive: true });
  await assertCanonicalDirectory(root, "browser automation profile root");

  const profilePath = path.join(root, AUTOMATION_PROFILE_NAME);
  const markerPath = path.join(root, AUTOMATION_PROFILE_MARKER);
  const leasePath = path.join(root, AUTOMATION_PROFILE_LEASE);

  await ensureAutomationProfileDirectory(profilePath);
  await ensureAutomationProfileMarker(root, profilePath, markerPath);

  const leaseMarker: BrowserAutomationProfileLeaseMarker = { version: 1, runId: options.runId };
  try {
    await writeFile(leasePath, `${JSON.stringify(leaseMarker)}\n`, { encoding: "utf8", flag: "wx" });
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "EEXIST") {
      throw new Error("browser automation profile lease is already in use");
    }
    throw error;
  }

  let released = false;
  return {
    profilePath,
    markerPath,
    leasePath,
    browserArgs: [`--user-data-dir=${profilePath}`],
    async release(): Promise<void> {
      if (released) return;
      const raw = await readFile(leasePath, "utf8");
      const parsed = JSON.parse(raw) as Partial<BrowserAutomationProfileLeaseMarker>;
      if (parsed.version !== 1 || parsed.runId !== options.runId) {
        throw new Error("browser automation profile lease ownership changed before release");
      }
      await unlink(leasePath);
      released = true;
    },
  };
}

export async function shutdownBrowserGracefully(
  options: BrowserGracefulShutdownOptions,
): Promise<BrowserShutdownResult> {
  if (!Number.isFinite(options.gracefulTimeoutMs) || options.gracefulTimeoutMs <= 0) {
    throw new Error("browser graceful shutdown timeout must be greater than zero");
  }

  try {
    await options.requestClose();
  } catch (error) {
    await options.forceTerminate();
    throw error;
  }

  let timer: NodeJS.Timeout | undefined;
  const exited = await Promise.race([
    options.waitForExit().then(() => true),
    new Promise<boolean>((resolve) => {
      timer = setTimeout(() => resolve(false), options.gracefulTimeoutMs);
    }),
  ]);
  if (timer) clearTimeout(timer);

  if (exited) {
    return { forced: false };
  }

  await options.forceTerminate();
  return { forced: true };
}

export async function startBrowserAutomation(
  options: StartBrowserAutomationOptions,
): Promise<BrowserAutomationSession> {
  const lease = await acquireBrowserAutomationProfile({
    root: options.root,
    runId: options.runId,
    additionalProtectedPaths: options.additionalProtectedPaths,
  });
  const browserArgs = [...lease.browserArgs, ...(options.args ?? [])];
  const startProcess = options.startProcess ?? ((command, args) => startOwnedProcess(command, args));

  let processHandle: OwnedBrowserProcessHandle;
  try {
    await options.beforeStart?.({
      profilePath: lease.profilePath,
      browserArgs,
    });
    processHandle = await startProcess(options.command, browserArgs);
  } catch (error) {
    await lease.release();
    throw error;
  }

  return {
    pid: processHandle.pid,
    profilePath: lease.profilePath,
    browserArgs,
    // No usable CDP session exists yet, so only the owned process tree may be
    // terminated; the lease stays held unless that termination succeeds.
    async terminateBeforeCdp(): Promise<void> {
      await processHandle.forceTerminate();
      await lease.release();
    },
    async shutdown(cdp: BrowserCdpController): Promise<BrowserShutdownResult> {
      let safeToReleaseLease = false;
      let forceTerminationSucceeded = false;
      try {
        const result = await shutdownBrowserGracefully({
          gracefulTimeoutMs: options.gracefulTimeoutMs,
          async requestClose() {
            await cdp.request("Browser.close");
          },
          async waitForExit() {
            await processHandle.waitForExit();
          },
          async forceTerminate() {
            await processHandle.forceTerminate();
            forceTerminationSucceeded = true;
          },
        });
        safeToReleaseLease = true;
        return result;
      } catch (error) {
        if (forceTerminationSucceeded) safeToReleaseLease = true;
        throw error;
      } finally {
        if (safeToReleaseLease) await lease.release();
      }
    },
  };
}

export async function createBrowserRun(options: BrowserRunOptions): Promise<EvidenceBrowserRun> {
  const scratch = await createEvidenceScratch(options.scratchRoot, { runId: options.runId });
  const browserProfilePath = path.resolve(
    options.browserProfilePath ?? path.join(scratch.path, "chrome-user-data"),
  );

  if (!directChild(scratch.path, browserProfilePath)) {
    throw new Error("browser profile must be a direct child of owned scratch");
  }

  await mkdir(browserProfilePath, { recursive: false });
  return {
    scratch,
    browserProfilePath,
    browserArgs: [`--user-data-dir=${browserProfilePath}`],
  };
}

export async function withOverallDeadline<T>(
  operation: Promise<T>,
  timeoutMs: number,
  onTimeout: () => void | Promise<void>,
  teardownTimeoutMs = 5_000,
): Promise<T> {
  if (!Number.isFinite(timeoutMs) || timeoutMs <= 0) {
    throw new Error("evidence run timeout must be greater than zero");
  }
  if (!Number.isFinite(teardownTimeoutMs) || teardownTimeoutMs <= 0) {
    throw new Error("evidence timeout teardown deadline must be greater than zero");
  }

  return await new Promise<T>((resolve, reject) => {
    let settled = false;
    const timer = setTimeout(() => {
      if (settled) return;
      settled = true;

      let teardownSettled = false;
      const teardownTimer = setTimeout(() => {
        if (teardownSettled) return;
        teardownSettled = true;
        reject(new Error(
          `evidence run timed out after ${timeoutMs}ms; timeout teardown timed out after ${teardownTimeoutMs}ms`,
        ));
      }, teardownTimeoutMs);

      void Promise.resolve().then(onTimeout).then(
        () => {
          if (teardownSettled) return;
          teardownSettled = true;
          clearTimeout(teardownTimer);
          reject(new Error(`evidence run timed out after ${timeoutMs}ms`));
        },
        (error) => {
          if (teardownSettled) return;
          teardownSettled = true;
          clearTimeout(teardownTimer);
          reject(new Error(
            `evidence run timed out after ${timeoutMs}ms; timeout teardown failed: ${error instanceof Error ? error.message : String(error)}`,
          ));
        },
      );
    }, timeoutMs);

    operation.then(
      (value) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        resolve(value);
      },
      (error) => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        reject(error);
      },
    );
  });
}
