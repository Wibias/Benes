import assert from "node:assert/strict";
import { describe, test } from "node:test";

import { CdpRequestRegistry } from "./cdp-request-registry.ts";
import { EvidenceCdpSession, type CdpWebSocketLike } from "./evidence-cdp-session.ts";

class FakeSocket implements CdpWebSocketLike {
  readonly sent: string[] = [];
  readonly #listeners = new Map<string, Set<(event: unknown) => void>>();

  send(data: string): void {
    this.sent.push(data);
  }

  addEventListener(type: "message" | "close" | "error", listener: (event: unknown) => void): void {
    let listeners = this.#listeners.get(type);
    if (!listeners) {
      listeners = new Set();
      this.#listeners.set(type, listeners);
    }
    listeners.add(listener);
  }

  removeEventListener(type: "message" | "close" | "error", listener: (event: unknown) => void): void {
    this.#listeners.get(type)?.delete(listener);
  }

  emit(type: "message" | "close" | "error", event: unknown = {}): void {
    for (const listener of this.#listeners.get(type) ?? []) listener(event);
  }
}

describe("CdpRequestRegistry", () => {
  test("times out a request that never receives a response and drops it from pending", async () => {
    const registry = new CdpRequestRegistry();
    const request = registry.request("Runtime.evaluate", { expression: "1" }, () => {}, 15);

    await assert.rejects(request, /Runtime\.evaluate.*timed out/i);
    assert.equal(registry.size, 0);
  });

  test("resolves a matching response and clears its timer", async () => {
    const registry = new CdpRequestRegistry();
    let sentId = 0;
    const request = registry.request<{ value: number }>(
      "Runtime.evaluate",
      { expression: "1" },
      (message) => { sentId = message.id; },
      1_000,
    );

    assert.equal(registry.size, 1);
    assert.equal(registry.resolveMessage({ id: sentId, result: { value: 1 } }), true);
    assert.deepEqual(await request, { value: 1 });
    assert.equal(registry.size, 0);
  });

  test("rejectAll rejects every pending request and clears the registry", async () => {
    const registry = new CdpRequestRegistry();
    const first = registry.request("Page.navigate", {}, () => {}, 1_000);
    const second = registry.request("Runtime.evaluate", {}, () => {}, 1_000);

    registry.rejectAll(new Error("socket closed"));

    await assert.rejects(first, /socket closed/i);
    await assert.rejects(second, /socket closed/i);
    assert.equal(registry.size, 0);
  });

  test("a synchronous send failure rejects without leaving a pending request", async () => {
    const registry = new CdpRequestRegistry();
    const request = registry.request("Page.navigate", {}, () => {
      throw new Error("send failed");
    }, 1_000);

    await assert.rejects(request, /send failed/i);
    assert.equal(registry.size, 0);
  });
});

describe("EvidenceCdpSession", () => {
  test("socket close rejects every pending request through the bounded registry", async () => {
    const socket = new FakeSocket();
    const session = new EvidenceCdpSession(socket, 1_000);
    const first = session.request("Page.navigate", {});
    const second = session.request("Runtime.evaluate", {});

    socket.emit("close");

    await assert.rejects(first, /CDP socket closed/i);
    await assert.rejects(second, /CDP socket closed/i);
    assert.equal(session.pendingCount, 0);
  });

  test("socket error rejects every pending request through the bounded registry", async () => {
    const socket = new FakeSocket();
    const session = new EvidenceCdpSession(socket, 1_000);
    const request = session.request("Runtime.evaluate", {});

    socket.emit("error");

    await assert.rejects(request, /CDP socket error/i);
    assert.equal(session.pendingCount, 0);
  });

  test("socket message resolves the matching request", async () => {
    const socket = new FakeSocket();
    const session = new EvidenceCdpSession(socket, 1_000);
    const request = session.request<{ value: number }>("Runtime.evaluate", { expression: "1" });
    const sent = JSON.parse(socket.sent[0]) as { id: number };

    socket.emit("message", {
      data: JSON.stringify({ id: sent.id, result: { value: 1 } }),
    });

    assert.deepEqual(await request, { value: 1 });
    assert.equal(session.pendingCount, 0);
  });
});
