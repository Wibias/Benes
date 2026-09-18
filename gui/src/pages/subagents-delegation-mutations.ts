/** Coalesced PUT /api/injection-model with server re-read after each write.

 * Single mutation authority for Subagents delegation settings. The React hook
 * owns resource integration only; all save/reload sequencing lives here.
 *
 * Pending-on-failure pin: when the mutation loop exits (PUT failure or
 * unexpected throw), any coalesced pending patch is dropped. A later save()
 * starts fresh. Successful drains clear pending before the next iteration.
 *
 * Production adapters (fetch / loadDelegationSnapshot / setClientResourceData)
 * are wired at the hook boundary so this module stays a pure, DI-testable
 * controller without pulling the nav-board load graph into unit tests.
 */

import type { DelegationPatch } from "./subagents-delegation-contract.ts";

/** Authority snapshot published after each successful write/reload. */
export type DelegationMutationSnapshot = {
  guidanceEnabled: boolean;
  syncCodexDefaults: boolean;
  model: string;
  effort: string;
  efforts: string[];
  available: ReadonlyArray<{ provider: string; model: string; namespaced: string }>;
};

export type DelegationMutationTransport = {
  put: (patch: DelegationPatch) => Promise<void>;
  readAuthority: () => Promise<DelegationMutationSnapshot>;
  publish: (snapshot: DelegationMutationSnapshot) => void;
  setSaving: (value: boolean) => void;
};

export type DelegationMutationController = {
  save: (patch: DelegationPatch) => Promise<void>;
  reload: () => Promise<void>;
};

/**
 * Create the coalescing mutation controller.
 * Transport is injected so executable tests can drive put/read/publish with
 * deferred-promise fakes.
 */
export function createDelegationMutationController(
  transport: DelegationMutationTransport,
): DelegationMutationController {
  let saving = false;
  let pending: DelegationPatch | null = null;

  const reload = async () => {
    try {
      transport.publish(await transport.readAuthority());
    } catch {
      /* keep the last committed UI state */
    }
  };

  const save = async (patch: DelegationPatch) => {
    if (saving) {
      pending = { ...pending, ...patch };
      return;
    }
    saving = true;
    transport.setSaving(true);
    let next: DelegationPatch = patch;
    try {
      for (;;) {
        // In-flight body is the captured `next`; later coalesce only updates `pending`.
        await transport.put(next);
        // Re-read rather than trusting the patch: the server clamps effort to what
        // the chosen model actually supports, so the echo can differ from what was sent.
        transport.publish(await transport.readAuthority());
        if (!pending) break;
        next = pending;
        pending = null;
      }
    } catch {
      /* keep the last committed UI state; no synthetic publish */
      pending = null;
    } finally {
      saving = false;
      transport.setSaving(false);
    }
  };

  return { save, reload };
}

export function serializeDelegationPatch(patch: DelegationPatch): string {
  return JSON.stringify(patch);
}
