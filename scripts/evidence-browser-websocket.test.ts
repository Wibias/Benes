import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { createServer } from "node:http";
import type { Socket } from "node:net";
import { test } from "node:test";

import * as runnerModule from "./evidence-browser-runner.ts";

type SocketEvent = "open" | "message" | "close" | "error";
type Listener = (event: unknown) => void;

type BoundedWebSocketLike = {
  send(data: string): void;
  close(): void;
  addEventListener(type: SocketEvent, listener: Listener): void;
  removeEventListener?(type: SocketEvent, listener: Listener): void;
};

type WebSocketConstructor = new (url: string) => BoundedWebSocketLike;

type RunnerWithBoundedWebSocket = typeof runnerModule & {
  createBoundedWebSocketOpener?: (
    WebSocketImpl: WebSocketConstructor,
  ) => (url: string, timeoutMs: number) => Promise<BoundedWebSocketLike>;
  openNodeWebSocket?: (
    url: string,
    timeoutMs: number,
  ) => Promise<BoundedWebSocketLike>;
};

function createBoundedWebSocketOpenerApi(): NonNullable<
  RunnerWithBoundedWebSocket["createBoundedWebSocketOpener"]
> {
  const createBoundedWebSocketOpener = (
    runnerModule as RunnerWithBoundedWebSocket
  ).createBoundedWebSocketOpener;

  assert.equal(
    typeof createBoundedWebSocketOpener,
    "function",
    "bounded WebSocket opener API must exist",
  );

  if (!createBoundedWebSocketOpener) {
    throw new Error("bounded WebSocket opener API is unavailable");
  }

  return createBoundedWebSocketOpener;
}

function openNodeWebSocketApi(): NonNullable<
  RunnerWithBoundedWebSocket["openNodeWebSocket"]
> {
  const openNodeWebSocket = (
    runnerModule as RunnerWithBoundedWebSocket
  ).openNodeWebSocket;

  assert.equal(
    typeof openNodeWebSocket,
    "function",
    "real Node WebSocket transport API must exist",
  );

  if (!openNodeWebSocket) {
    throw new Error("real Node WebSocket transport API is unavailable");
  }

  return openNodeWebSocket;
}

class FakeSocketBase implements BoundedWebSocketLike {
  closeCount = 0;
  readonly url: string;

  readonly #listeners = new Map<SocketEvent, Set<Listener>>();

  constructor(url: string) {
    this.url = url;
  }

  send(): void {}

  close(): void {
    this.closeCount += 1;
    this.emit("close", {});
  }

  addEventListener(type: SocketEvent, listener: Listener): void {
    const listeners = this.#listeners.get(type) ?? new Set<Listener>();
    listeners.add(listener);
    this.#listeners.set(type, listeners);
  }

  removeEventListener(type: SocketEvent, listener: Listener): void {
    this.#listeners.get(type)?.delete(listener);
  }

  protected emit(type: SocketEvent, event: unknown): void {
    for (const listener of this.#listeners.get(type) ?? []) {
      listener(event);
    }
  }
}

class OpeningSocket extends FakeSocketBase {
  static instances: OpeningSocket[] = [];

  constructor(url: string) {
    super(url);
    OpeningSocket.instances.push(this);
    queueMicrotask(() => this.emit("open", {}));
  }
}

class NeverOpeningSocket extends FakeSocketBase {
  static instances: NeverOpeningSocket[] = [];

  constructor(url: string) {
    super(url);
    NeverOpeningSocket.instances.push(this);
  }
}

test("bounded WebSocket opener resolves only after the socket opens", async () => {
  const createBoundedWebSocketOpener = createBoundedWebSocketOpenerApi();
  OpeningSocket.instances = [];
  const openSocket = createBoundedWebSocketOpener(OpeningSocket);

  const socket = await openSocket(
    "ws://127.0.0.1:9444/devtools/browser/current",
    100,
  );

  assert.equal(socket, OpeningSocket.instances[0]);
  assert.equal(
    OpeningSocket.instances[0]?.url,
    "ws://127.0.0.1:9444/devtools/browser/current",
  );
  assert.equal(OpeningSocket.instances[0]?.closeCount, 0);
});

test("bounded WebSocket opener closes a socket that never opens before the deadline", async () => {
  const createBoundedWebSocketOpener = createBoundedWebSocketOpenerApi();
  NeverOpeningSocket.instances = [];
  const openSocket = createBoundedWebSocketOpener(NeverOpeningSocket);

  await assert.rejects(
    openSocket("ws://127.0.0.1:9555/devtools/browser/never", 15),
    /WebSocket connection timed out after 15ms/i,
  );

  assert.equal(NeverOpeningSocket.instances.length, 1);
  assert.equal(NeverOpeningSocket.instances[0]?.closeCount, 1);
});

test("Node WebSocket transport completes a real local handshake", async () => {
  const openNodeWebSocket = openNodeWebSocketApi();
  const upgradedSockets = new Set<Socket>();
  let upgrades = 0;
  const server = createServer();

  server.on("upgrade", (request, socket) => {
    const key = request.headers["sec-websocket-key"];
    if (typeof key !== "string") {
      socket.destroy();
      return;
    }

    upgrades += 1;
    upgradedSockets.add(socket);
    socket.once("close", () => upgradedSockets.delete(socket));

    const accept = createHash("sha1")
      .update(`${key}258EAFA5-E914-47DA-95CA-C5AB0DC85B11`)
      .digest("base64");

    socket.write([
      "HTTP/1.1 101 Switching Protocols",
      "Upgrade: websocket",
      "Connection: Upgrade",
      `Sec-WebSocket-Accept: ${accept}`,
      "",
      "",
    ].join("\r\n"));
  });

  await new Promise<void>((resolve, reject) => {
    server.once("error", reject);
    server.listen(0, "127.0.0.1", resolve);
  });

  try {
    const address = server.address();
    assert.ok(address && typeof address === "object");

    const socket = await openNodeWebSocket(
      `ws://127.0.0.1:${address.port}/devtools/browser/local-test`,
      250,
    );

    assert.equal(upgrades, 1);
    socket.close();
  } finally {
    for (const socket of upgradedSockets) socket.destroy();
    await new Promise<void>((resolve) => server.close(() => resolve()));
  }
});
