#!/usr/bin/env node
/**
 * Prove the OS keyring can create, read, and delete a throwaway credential.
 *
 * The probe is assembled from four independent concerns: a plan that supplies
 * the injected dependencies, a round trip that writes and verifies one secret,
 * a buffer ledger that owns every sensitive allocation, and a composition step
 * that reports the primary failure separately from the cleanup failure.
 *
 * The secret is random, compared in constant time, and erased on every exit
 * path. Nothing in this module interpolates secret material into an error.
 */
import { randomBytes, randomUUID, timingSafeEqual } from "node:crypto";
import { isMainModule } from "./node-runtime.ts";

/** napi-rs AsyncEntry surface used by the probe and by tests. */
export type AsyncEntryLike = {
  setSecret(secret: Uint8Array, signal?: AbortSignal): Promise<void>;
  getSecret(signal?: AbortSignal): Promise<Uint8Array | null>;
  deleteCredential(signal?: AbortSignal): Promise<boolean>;
};

export type KeyringProbeOptions = {
  open?: (service: string, account: string) => Promise<AsyncEntryLike>;
  randomBytes?: (size: number) => Buffer;
  uuid?: () => string;
  timeoutMs?: number;
};

export type KeyringProbeFailure = Error & { cleanupError?: unknown };

const CREDENTIAL_BYTES = 32;
const DEFAULT_TIMEOUT_MS = 8_000;
const MISMATCH = "keyring smoke: stored secret did not match the value that was written.";
const UNDELETED = "keyring smoke: could not delete the temporary credential.";

async function openNativeEntry(service: string, account: string): Promise<AsyncEntryLike> {
  const { AsyncEntry } = await import("@napi-rs/keyring");
  return new AsyncEntry(service, account);
}

/**
 * Ownership ledger for the sensitive buffers this module allocates. Erasing
 * happens in one place, so a new intermediate value cannot be forgotten, and it
 * happens on the failing path as well as on success.
 */
class BufferLedger {
  private readonly held: Uint8Array[] = [];

  keep<T extends Uint8Array>(bytes: T): T {
    this.held.push(bytes);
    return bytes;
  }

  erase(): void {
    for (const bytes of this.held) bytes.fill(0);
    this.held.length = 0;
  }
}

type Outcome = { ok: true } | { ok: false; reason: unknown };

async function attempt(work: () => Promise<void>): Promise<Outcome> {
  try {
    await work();
    return { ok: true };
  } catch (reason) {
    return { ok: false, reason };
  }
}

function asReason(value: unknown, fallback: string): Error {
  return value instanceof Error ? value : new Error(fallback);
}

/** Keep the primary failure primary, and keep the cleanup failure reachable. */
function compose(primary: unknown, cleanup: unknown): KeyringProbeFailure {
  const failure = asReason(primary, "keyring smoke: probe failed.") as KeyringProbeFailure;
  failure.cleanupError = asReason(cleanup, UNDELETED);
  failure.message = `${failure.message} (temporary credential may still be present)`;
  return failure;
}

/** One write/read round trip. A length mismatch is a mismatch, never a throw. */
async function writeAndVerify(
  entry: AsyncEntryLike,
  payload: Buffer,
  bound: () => AbortSignal,
  ledger: BufferLedger,
): Promise<void> {
  await entry.setSecret(payload, bound());
  const readBack = await entry.getSecret(bound());
  if (readBack) ledger.keep(readBack);
  const observed = ledger.keep(readBack ? Buffer.from(readBack) : Buffer.alloc(0));
  const comparable = observed.byteLength === payload.byteLength;
  if (!comparable || !timingSafeEqual(observed, payload)) throw new Error(MISMATCH);
}

async function release(entry: AsyncEntryLike, signal: AbortSignal): Promise<void> {
  if (!(await entry.deleteCredential(signal))) throw new Error(UNDELETED);
}

export async function probeOsKeyring(options: KeyringProbeOptions = {}): Promise<void> {
  const open = options.open ?? openNativeEntry;
  const mint = options.randomBytes ?? randomBytes;
  const uuid = options.uuid ?? randomUUID;
  const budget = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const bound = (): AbortSignal => AbortSignal.timeout(budget);

  const ledger = new BufferLedger();
  const payload = ledger.keep(mint(CREDENTIAL_BYTES));
  // Temporary, collision resistant, and deliberately unrelated to any real
  // account: the credentials under test are throwaway.
  const service = `benes.keyring-smoke.${uuid()}`;
  const account = `ci-${uuid()}`;

  let opened: AsyncEntryLike | undefined;
  const roundtrip = await attempt(async () => {
    opened = await open(service, account);
    await writeAndVerify(opened, payload, bound, ledger);
  });

  const removal: Outcome = opened ? await attempt(() => release(opened!, bound())) : { ok: true };

  ledger.erase();

  if (!roundtrip.ok && !removal.ok) throw compose(roundtrip.reason, removal.reason);
  if (!roundtrip.ok) throw roundtrip.reason;
  if (!removal.ok) throw asReason(removal.reason, UNDELETED);
}

if (isMainModule(import.meta.url)) {
  await probeOsKeyring();
  console.log("keyring smoke: create, read, and delete succeeded.");
}
