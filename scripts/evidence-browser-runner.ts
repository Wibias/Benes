import { readFile, unlink } from "node:fs/promises";
import path from "node:path";

import WebSocket from "ws";

import {
  EvidenceCdpSession,
  type CdpWebSocketLike,
} from "./evidence-cdp-session.ts";
import { startBrowserAutomation, withOverallDeadline } from "./evidence-browser-runtime.ts";
import { createEvidenceScratch, removeEvidenceScratch } from "./evidence-scratch.ts";

export type BrowserEvidenceContext = {
  scratchPath: string;
  browserPid: number;
  browserProfilePath: string;
  cdp: {
    request<T>(method: string, params?: Record<string, unknown>): Promise<T>;
  };
};

export type BrowserEvidenceOptions = {
  scratchRoot: string;
  automationProfileRoot: string;
  browserCommand: string;
  browserArgs?: readonly string[];
  overallTimeoutMs: number;
  cdpRequestTimeoutMs: number;
  startupTimeoutMs: number;
  gracefulShutdownTimeoutMs: number;
  additionalProtectedPaths?: readonly string[];
};

export type BrowserEvidenceRunner = <T>(
  options: BrowserEvidenceOptions,
  task: (context: BrowserEvidenceContext) => Promise<T>,
) => Promise<T>;

type EvidenceScratchLike = {
  runId: string;
  root: string;
  path: string;
  markerPath: string;
};

type BrowserAutomationSessionLike = {
  pid: number;
  profilePath: string;
  browserArgs: string[];
  shutdown(cdp: BrowserEvidenceContext["cdp"]): Promise<{ forced: boolean }>;
  terminateBeforeCdp(): Promise<void>;
};

export type BrowserEvidenceCdpConnection = {
  cdp: BrowserEvidenceContext["cdp"];
  dispose(): void;
};

export type BrowserEvidenceDependencies = {
  createScratch(
    root: string,
    options?: { additionalProtectedPaths?: readonly string[] },
  ): Promise<EvidenceScratchLike>;
  removeScratch(
    root: string,
    target: string,
    options?: { additionalProtectedPaths?: readonly string[] },
  ): Promise<void>;
  startBrowser(options: {
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
  }): Promise<BrowserAutomationSessionLike>;
  connectCdp(input: {
    profilePath: string;
    startupTimeoutMs: number;
    requestTimeoutMs: number;
  }): Promise<BrowserEvidenceCdpConnection>;
};

type DevToolsEndpoint = {
  port: number;
  browserPath: string;
  url: string;
};

type BrowserWebSocketEvent = "open" | "message" | "close" | "error";
type BrowserWebSocketListener = (event: unknown) => void;

type DevToolsSocket = {
  send(data: string): void;
  close(): void;
  addEventListener(
    type: BrowserWebSocketEvent,
    listener: BrowserWebSocketListener,
  ): void;
  removeEventListener?(
    type: BrowserWebSocketEvent,
    listener: BrowserWebSocketListener,
  ): void;
};

type DevToolsSocketConstructor = new (url: string) => DevToolsSocket;

type DevToolsSocketOpener = (
  url: string,
  timeoutMs: number,
) => Promise<DevToolsSocket>;

async function removeStaleDevToolsActivePort(profilePath: string): Promise<void> {
  const controlPath = path.join(profilePath, "DevToolsActivePort");
  try {
    await unlink(controlPath);
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== "ENOENT") throw error;
  }
}

async function readDevToolsEndpoint(profilePath: string): Promise<DevToolsEndpoint | null> {
  const controlPath = path.join(profilePath, "DevToolsActivePort");
  let raw: string;

  try {
    raw = await readFile(controlPath, "utf8");
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code === "ENOENT") return null;
    throw error;
  }

  const lines = raw
    .split(/\r?\n/u)
    .map((value) => value.trim())
    .filter(Boolean);
  const port = Number(lines[0]);
  const browserPath = lines[1];

  if (
    !Number.isSafeInteger(port)
    || port <= 0
    || port > 65_535
    || !browserPath
    || !browserPath.startsWith("/devtools/browser/")
  ) {
    throw new Error("DevToolsActivePort has invalid content");
  }

  return {
    port,
    browserPath,
    url: `ws://127.0.0.1:${port}${browserPath}`,
  };
}

export async function waitForDevToolsEndpoint(
  profilePath: string,
  startupTimeoutMs: number,
): Promise<DevToolsEndpoint> {
  if (!Number.isFinite(startupTimeoutMs) || startupTimeoutMs <= 0) {
    throw new Error("browser startup timeout must be greater than zero");
  }

  const deadline = Date.now() + startupTimeoutMs;

  for (;;) {
    const endpoint = await readDevToolsEndpoint(profilePath);
    if (endpoint) return endpoint;

    const remainingMs = deadline - Date.now();
    if (remainingMs <= 0) {
      throw new Error(`DevToolsActivePort discovery timed out after ${startupTimeoutMs}ms`);
    }

    await new Promise((resolve) => setTimeout(resolve, Math.min(10, remainingMs)));
  }
}

export function createBoundedWebSocketOpener(
  WebSocketImpl: DevToolsSocketConstructor,
): DevToolsSocketOpener {
  return async function openBoundedWebSocket(
    url: string,
    timeoutMs: number,
  ): Promise<DevToolsSocket> {
    const socket = new WebSocketImpl(url);

    return await new Promise<DevToolsSocket>((resolve, reject) => {
      let settled = false;

      const cleanup = (): void => {
        socket.removeEventListener?.("open", onOpen);
      };

      const onOpen = (): void => {
        if (settled) return;
        settled = true;
        clearTimeout(timer);
        cleanup();
        resolve(socket);
      };

      socket.addEventListener("open", onOpen);

      const timer = setTimeout(() => {
        if (settled) return;
        settled = true;
        cleanup();
        socket.close();
        reject(new Error(`WebSocket connection timed out after ${timeoutMs}ms`));
      }, timeoutMs);
    });
  };
}

const openBoundedNodeWebSocket = createBoundedWebSocketOpener(
  WebSocket as unknown as DevToolsSocketConstructor,
);

export async function openNodeWebSocket(
  url: string,
  timeoutMs: number,
): Promise<DevToolsSocket> {
  return await openBoundedNodeWebSocket(url, timeoutMs);
}

export function createDevToolsConnector(
  openSocket: DevToolsSocketOpener,
): BrowserEvidenceDependencies["connectCdp"] {
  return async function connectDevTools(input): Promise<BrowserEvidenceCdpConnection> {
    const endpoint = await waitForDevToolsEndpoint(
      input.profilePath,
      input.startupTimeoutMs,
    );
    const socket = await openSocket(endpoint.url, input.startupTimeoutMs);
    const session = new EvidenceCdpSession(
      socket as CdpWebSocketLike,
      input.requestTimeoutMs,
    );

    return {
      cdp: session,
      dispose(): void {
        session.dispose();
        socket.close();
      },
    };
  };
}

type SettledOutcome = { failed: false } | { failed: true; error: unknown };

// The canonical runner owns the browser profile and the debugging channel. A
// caller-provided copy of any of these would silently move the browser off the
// owned profile or off the bounded CDP transport.
const LIFECYCLE_CRITICAL_BROWSER_ARGS = new Set([
  "--user-data-dir",
  "--remote-debugging-port",
  "--remote-debugging-pipe",
]);

function assertCallerBrowserArgsAllowed(browserArgs: readonly string[] = []): void {
  for (const arg of browserArgs) {
    const flag = arg.trim().split("=", 1)[0] ?? arg;
    if (LIFECYCLE_CRITICAL_BROWSER_ARGS.has(flag)) {
      throw new Error(
        `caller must not supply lifecycle-critical browser argument: ${flag}`,
      );
    }
  }
}

async function settle(operation: () => Promise<void>): Promise<SettledOutcome> {
  try {
    await operation();
    return { failed: false };
  } catch (error) {
    return { failed: true, error };
  }
}

export function createBrowserEvidenceRunner(
  dependencies: BrowserEvidenceDependencies,
): BrowserEvidenceRunner {
  return async function runBrowserEvidenceWithDependencies<T>(
    options: BrowserEvidenceOptions,
    task: (context: BrowserEvidenceContext) => Promise<T>,
  ): Promise<T> {
    // Rejected before any lifecycle side effect: no scratch, no profile lease,
    // no browser process.
    assertCallerBrowserArgsAllowed(options.browserArgs);

    const scratch = await dependencies.createScratch(options.scratchRoot, {
      additionalProtectedPaths: options.additionalProtectedPaths,
    });

    const session = await dependencies.startBrowser({
      root: options.automationProfileRoot,
      runId: scratch.runId,
      command: options.browserCommand,
      args: ["--remote-debugging-port=0", ...(options.browserArgs ?? [])],
      gracefulTimeoutMs: options.gracefulShutdownTimeoutMs,
      additionalProtectedPaths: options.additionalProtectedPaths,
      beforeStart: async ({ profilePath }) => {
        await removeStaleDevToolsActivePort(profilePath);
      },
    });

    let connection: BrowserEvidenceCdpConnection;
    try {
      connection = await dependencies.connectCdp({
        profilePath: session.profilePath,
        startupTimeoutMs: options.startupTimeoutMs,
        requestTimeoutMs: options.cdpRequestTimeoutMs,
      });
    } catch (error) {
      // No usable CDP session exists, so the managed CDP shutdown path cannot
      // run; only the owned browser process tree may be terminated.
      const termination = await settle(async () => {
        await session.terminateBeforeCdp();
      });
      if (termination.failed) {
        throw new AggregateError(
          [error, termination.error],
          "browser CDP startup failed and pre-CDP browser termination failed",
        );
      }
      throw error;
    }

    // The task, the overall deadline, and the failure paths all converge on this
    // one shutdown so the owned browser is never shut down twice.
    let shutdownPromise: Promise<void> | undefined;
    const shutdownOnce = (): Promise<void> => {
      shutdownPromise ??= Promise.resolve().then(async () => {
        await session.shutdown(connection.cdp);
      });
      return shutdownPromise;
    };

    const work = async (): Promise<T> => {
      let result: T;
      try {
        result = await task({
          scratchPath: scratch.path,
          browserPid: session.pid,
          browserProfilePath: session.profilePath,
          cdp: connection.cdp,
        });
      } catch (taskError) {
        const shutdown = await settle(shutdownOnce);
        connection.dispose();
        if (shutdown.failed) {
          throw new AggregateError(
            [taskError, shutdown.error],
            "evidence task failed and browser shutdown failed",
          );
        }
        throw taskError;
      }

      const shutdown = await settle(shutdownOnce);
      connection.dispose();
      if (shutdown.failed) throw shutdown.error;

      return result;
    };

    const result = await withOverallDeadline(
      work(),
      options.overallTimeoutMs,
      shutdownOnce,
    );

    // Only a fully successful run may delete evidence; every failure path above
    // retains the scratch directory for diagnosis.
    await dependencies.removeScratch(options.scratchRoot, scratch.path, {
      additionalProtectedPaths: options.additionalProtectedPaths,
    });

    return result;
  };
}

const defaultBrowserEvidenceDependencies: BrowserEvidenceDependencies = {
  createScratch: createEvidenceScratch,
  removeScratch: removeEvidenceScratch,
  startBrowser: startBrowserAutomation,
  connectCdp: createDevToolsConnector(openNodeWebSocket),
};

export const runBrowserEvidence = createBrowserEvidenceRunner(
  defaultBrowserEvidenceDependencies,
);
