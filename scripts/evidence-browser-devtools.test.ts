import assert from "node:assert/strict";
import { mkdir, writeFile } from "node:fs/promises";
import path from "node:path";
import { test } from "node:test";

import type { CdpWebSocketLike } from "./evidence-cdp-session.ts";
import * as runnerModule from "./evidence-browser-runner.ts";
import { createTestTempRoot } from "./test-temp-root.ts";

type RunnerWithDevToolsDiscovery = typeof runnerModule & {
  waitForDevToolsEndpoint?: (
    profilePath: string,
    startupTimeoutMs: number,
  ) => Promise<{
    port: number;
    browserPath: string;
    url: string;
  }>;
  createDevToolsConnector?: (
    openSocket: (
      url: string,
      timeoutMs: number,
    ) => Promise<CdpWebSocketLike & { close(): void }>,
  ) => (input: {
    profilePath: string;
    startupTimeoutMs: number;
    requestTimeoutMs: number;
  }) => Promise<{
    cdp: {
      request<T>(method: string, params?: Record<string, unknown>): Promise<T>;
    };
    dispose(): void;
  }>;
};

function waitForDevToolsEndpointApi(): NonNullable<
  RunnerWithDevToolsDiscovery["waitForDevToolsEndpoint"]
> {
  const waitForDevToolsEndpoint = (
    runnerModule as RunnerWithDevToolsDiscovery
  ).waitForDevToolsEndpoint;

  assert.equal(
    typeof waitForDevToolsEndpoint,
    "function",
    "bounded DevToolsActivePort discovery API must exist",
  );

  if (!waitForDevToolsEndpoint) {
    throw new Error("bounded DevToolsActivePort discovery API is unavailable");
  }

  return waitForDevToolsEndpoint;
}

function createDevToolsConnectorApi(): NonNullable<
  RunnerWithDevToolsDiscovery["createDevToolsConnector"]
> {
  const createDevToolsConnector = (
    runnerModule as RunnerWithDevToolsDiscovery
  ).createDevToolsConnector;

  assert.equal(
    typeof createDevToolsConnector,
    "function",
    "bounded DevTools CDP connector API must exist",
  );

  if (!createDevToolsConnector) {
    throw new Error("bounded DevTools CDP connector API is unavailable");
  }

  return createDevToolsConnector;
}

class FakeCdpSocket implements CdpWebSocketLike {
  readonly sentMethods: string[] = [];
  closeCount = 0;

  readonly #listeners = new Map<
    "message" | "close" | "error",
    Set<(event: unknown) => void>
  >();

  send(data: string): void {
    const request = JSON.parse(data) as {
      id: number;
      method: string;
    };
    this.sentMethods.push(request.method);

    queueMicrotask(() => {
      this.#emit("message", {
        data: JSON.stringify({
          id: request.id,
          result: { product: "Chrome/Test" },
        }),
      });
    });
  }

  addEventListener(
    type: "message" | "close" | "error",
    listener: (event: unknown) => void,
  ): void {
    const listeners = this.#listeners.get(type) ?? new Set();
    listeners.add(listener);
    this.#listeners.set(type, listeners);
  }

  removeEventListener(
    type: "message" | "close" | "error",
    listener: (event: unknown) => void,
  ): void {
    this.#listeners.get(type)?.delete(listener);
  }

  close(): void {
    this.closeCount += 1;
    this.#emit("close", {});
  }

  #emit(type: "message" | "close" | "error", event: unknown): void {
    for (const listener of this.#listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

test("waits for the current DevToolsActivePort and parses the browser endpoint", async () => {
  const waitForDevToolsEndpoint = waitForDevToolsEndpointApi();
  const parent = await createTestTempRoot("benes-browser-devtools-current-");
  const profilePath = path.join(parent, "automation", "chrome-user-data");
  const controlPath = path.join(profilePath, "DevToolsActivePort");

  await mkdir(profilePath, { recursive: true });

  const writeCurrent = new Promise<void>((resolve, reject) => {
    setTimeout(() => {
      void writeFile(
        controlPath,
        "9222\n/devtools/browser/current-run\n",
        "utf8",
      ).then(() => resolve(), reject);
    }, 10);
  });

  const endpoint = await waitForDevToolsEndpoint(profilePath, 100);
  await writeCurrent;

  assert.deepEqual(endpoint, {
    port: 9222,
    browserPath: "/devtools/browser/current-run",
    url: "ws://127.0.0.1:9222/devtools/browser/current-run",
  });
});

test("DevToolsActivePort discovery is bounded when Chrome never publishes the file", async () => {
  const waitForDevToolsEndpoint = waitForDevToolsEndpointApi();
  const parent = await createTestTempRoot("benes-browser-devtools-timeout-");
  const profilePath = path.join(parent, "automation", "chrome-user-data");

  await mkdir(profilePath, { recursive: true });

  await assert.rejects(
    waitForDevToolsEndpoint(profilePath, 15),
    /DevToolsActivePort discovery timed out after 15ms/i,
  );
});

test("opens the discovered browser endpoint and exposes bounded CDP requests", async () => {
  const createDevToolsConnector = createDevToolsConnectorApi();
  const parent = await createTestTempRoot("benes-browser-devtools-cdp-");
  const profilePath = path.join(parent, "automation", "chrome-user-data");
  const controlPath = path.join(profilePath, "DevToolsActivePort");
  const socket = new FakeCdpSocket();
  const opens: Array<{ url: string; timeoutMs: number }> = [];

  await mkdir(profilePath, { recursive: true });
  await writeFile(
    controlPath,
    "9333\n/devtools/browser/current-cdp\n",
    "utf8",
  );

  const connectCdp = createDevToolsConnector(async (url, timeoutMs) => {
    opens.push({ url, timeoutMs });
    return socket;
  });

  const connection = await connectCdp({
    profilePath,
    startupTimeoutMs: 100,
    requestTimeoutMs: 75,
  });

  const version = await connection.cdp.request<{ product: string }>(
    "Browser.getVersion",
  );

  assert.deepEqual(opens, [
    {
      url: "ws://127.0.0.1:9333/devtools/browser/current-cdp",
      timeoutMs: 100,
    },
  ]);
  assert.deepEqual(socket.sentMethods, ["Browser.getVersion"]);
  assert.deepEqual(version, { product: "Chrome/Test" });

  connection.dispose();
  assert.equal(socket.closeCount, 1);
});
