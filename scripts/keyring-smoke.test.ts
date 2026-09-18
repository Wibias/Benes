import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { probeOsKeyring, type AsyncEntryLike, type KeyringProbeFailure } from "./keyring-smoke.ts";

const SAMPLE = Buffer.from("abcdefghijklmnopqrstuvwxyz012345");

function memoryEntry(): AsyncEntryLike {
  let held: Uint8Array | null = null;
  return {
    async setSecret(secret) {
      held = Uint8Array.from(secret);
    },
    async getSecret() {
      return held ? Uint8Array.from(held) : null;
    },
    async deleteCredential() {
      held = null;
      return true;
    },
  };
}

function assertNoSecret(error: unknown) {
  const text = error instanceof Error ? `${error.message}${error.stack ?? ""}` : String(error);
  assert.doesNotMatch(text, /abcdefghijklmnopqrstuvwxyz012345/);
}

describe("probeOsKeyring", () => {
  test("accepts a matching write/read through an injected napi-shaped entry", async () => {
    await probeOsKeyring({
      open: async () => memoryEntry(),
      randomBytes: () => Buffer.from(SAMPLE),
      uuid: () => "fixed",
    });
  });

  test("rejects an entry that returns a different secret", async () => {
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return Uint8Array.from(Buffer.from("different-secret-value-01234567"));
      },
      async deleteCredential() {
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      /did not match/,
    );
  });

  test("still deletes the throwaway entry after a failed read", async () => {
    let deleted = false;
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        deleted = true;
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      /did not match/,
    );
    assert.equal(deleted, true);
  });

  test("successful roundtrip with a failed delete reports the cleanup error", async () => {
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return Uint8Array.from(SAMPLE);
      },
      async deleteCredential() {
        return false;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({
        open: async () => entry,
        randomBytes: () => Buffer.from(SAMPLE),
        uuid: () => "x",
      }),
      (error: unknown) => {
        assert.match((error as Error).message, /could not delete the temporary credential/);
        assertNoSecret(error);
        return true;
      },
    );
  });

  test("mismatch with a successful delete keeps the primary error only", async () => {
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      (error: unknown) => {
        assert.match((error as Error).message, /did not match/);
        assert.equal("cleanupError" in (error as object), false);
        assertNoSecret(error);
        return true;
      },
    );
  });

  test("mismatch with delete returning false keeps the primary error and exposes cleanup", async () => {
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        return false;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      (error: unknown) => {
        const failure = error as KeyringProbeFailure;
        assert.match(failure.message, /did not match/);
        assert.match(failure.message, /temporary credential may still be present/);
        assert.match((failure.cleanupError as Error).message, /could not delete the temporary credential/);
        assertNoSecret(error);
        return true;
      },
    );
  });

  test("mismatch with a thrown delete keeps the primary error and exposes the thrown cleanup", async () => {
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        throw new Error("keyring backend refused the delete");
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      (error: unknown) => {
        const failure = error as KeyringProbeFailure;
        assert.match(failure.message, /did not match/);
        assert.match(failure.message, /temporary credential may still be present/);
        assert.match((failure.cleanupError as Error).message, /backend refused the delete/);
        assertNoSecret(error);
        return true;
      },
    );
  });

  test("still deletes the throwaway entry when the write fails", async () => {
    let deleted = false;
    const entry: AsyncEntryLike = {
      async setSecret() {
        throw new Error("keyring backend refused the write");
      },
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        deleted = true;
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      /refused the write/,
    );
    assert.equal(deleted, true);
  });

  test("still deletes the throwaway entry when the read throws", async () => {
    let deleted = false;
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        throw new Error("keyring backend refused the read");
      },
      async deleteCredential() {
        deleted = true;
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x" }),
      /refused the read/,
    );
    assert.equal(deleted, true);
  });

  test("releases an opened entry exactly once", async () => {
    let deletions = 0;
    const entry: AsyncEntryLike = {
      async setSecret() {},
      async getSecret() {
        return null;
      },
      async deleteCredential() {
        deletions += 1;
        return true;
      },
    };
    await assert.rejects(() => probeOsKeyring({ open: async () => entry, uuid: () => "x" }));
    assert.equal(deletions, 1);
  });

  test("bounds every keyring operation with its own abort signal", async () => {
    const signals: Array<AbortSignal | undefined> = [];
    const entry: AsyncEntryLike = {
      async setSecret(_secret, signal) {
        signals.push(signal);
      },
      async getSecret(signal) {
        signals.push(signal);
        return null;
      },
      async deleteCredential(signal) {
        signals.push(signal);
        return true;
      },
    };
    await assert.rejects(
      () => probeOsKeyring({ open: async () => entry, uuid: () => "x", timeoutMs: 30 }),
    );
    assert.equal(signals.length, 3);
    assert.equal(signals.some((signal) => signal === undefined), false);
    assert.notEqual(signals[0], signals[1]);
    assert.notEqual(signals[0], signals[2]);
    assert.notEqual(signals[1], signals[2]);
    assert.equal(signals.some((signal) => signal!.aborted), false);
    await new Promise((resolve) => setTimeout(resolve, 80));
    assert.equal(signals.every((signal) => signal!.aborted), true);
  });

  test("erases the generated payload and the value read back after success", async () => {
    const payload = Buffer.from(SAMPLE);
    const readBack = Buffer.from(SAMPLE);
    await probeOsKeyring({
      open: async () => ({
        async setSecret() {},
        async getSecret() {
          return readBack;
        },
        async deleteCredential() {
          return true;
        },
      }),
      randomBytes: () => payload,
      uuid: () => "x",
    });
    assert.equal(payload.every((byte) => byte === 0), true);
    assert.equal(readBack.every((byte) => byte === 0), true);
  });

  test("erases the generated payload and the value read back after a mismatch", async () => {
    const payload = Buffer.from(SAMPLE);
    const readBack = Buffer.from("different-secret-value-01234567");
    await assert.rejects(
      () =>
        probeOsKeyring({
          open: async () => ({
            async setSecret() {},
            async getSecret() {
              return readBack;
            },
            async deleteCredential() {
              return true;
            },
          }),
          randomBytes: () => payload,
          uuid: () => "x",
        }),
      /did not match/,
    );
    assert.equal(payload.every((byte) => byte === 0), true);
    assert.equal(readBack.every((byte) => byte === 0), true);
  });

  test("reports an open failure without claiming a leaked credential", async () => {
    await assert.rejects(
      () =>
        probeOsKeyring({
          open: async () => {
            throw new Error("keyring backend unavailable");
          },
          uuid: () => "x",
        }),
      (error: unknown) => {
        assert.match((error as Error).message, /backend unavailable/);
        assert.doesNotMatch((error as Error).message, /may still be present/);
        assert.equal("cleanupError" in (error as object), false);
        assertNoSecret(error);
        return true;
      },
    );
  });
});
