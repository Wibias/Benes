import assert from "node:assert/strict";
import test from "node:test";

import {
  IntegrationApiError,
  isIntegrationRefusalEnvelope,
  loadClaudeCodeStatus,
  loadClaudeDesktopStatus,
  loadCodexRoutingStatus,
  loadGrokFenceStatus,
  loadIntegrationJournal,
  loadIntegrationStates,
  restoreIntegration,
  toggleIntegration,
} from "../src/pages/integrations/integration-api.ts";
import {
  NativeApiError,
  isNativeRefusalEnvelope,
  loadNativeIntegrations,
  toggleNativeIntegration,
} from "../src/pages/integrations/native-api.ts";
import { describeRefusal } from "../src/pages/integrations/refusal-copy.ts";

type FetchHandler = (url: string, init?: RequestInit) => Promise<Response> | Response;

const API = "http://127.0.0.1:23100";

function jsonResponse(status: number, body: unknown): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function installFetch(handler: FetchHandler): () => void {
  const previous = globalThis.fetch;
  globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
    return handler(String(input), init);
  }) as typeof fetch;
  return () => {
    globalThis.fetch = previous;
  };
}

function translate(key: string, vars?: Record<string, string>): string {
  if (!vars) return key;
  return `${key}:${JSON.stringify(vars)}`;
}

const fileRefusal = {
  error: "refused",
  code: "integration_conflict",
  clientId: "opencode",
  state: "conflict",
  reason: "conflict",
  message: "file changed after write",
} as const;

const nativeRefusal = {
  error: "refused",
  code: "native_integration_refused",
  clientId: "grok",
  reason: "orphaned_marker",
  message: "marker is open",
} as const;

test("file refusal envelopes require reason, code, client, state, and message", () => {
  assert.equal(isIntegrationRefusalEnvelope(fileRefusal), true);
  assert.equal(isIntegrationRefusalEnvelope({ ...fileRefusal, reason: "nope" }), false);
  assert.equal(isIntegrationRefusalEnvelope({ ...fileRefusal, clientId: "claude" }), false);
  assert.equal(isIntegrationRefusalEnvelope({ ...fileRefusal, code: "nope" }), false);
  assert.equal(isIntegrationRefusalEnvelope(null), false);
});

test("native refusal envelopes require native code, client, reason, and message", () => {
  assert.equal(isNativeRefusalEnvelope(nativeRefusal), true);
  assert.equal(isNativeRefusalEnvelope({ ...nativeRefusal, clientId: "opencode" }), false);
  assert.equal(isNativeRefusalEnvelope({ ...nativeRefusal, code: "integration_conflict" }), false);
  assert.equal(isNativeRefusalEnvelope({ ...nativeRefusal, reason: "conflict" }), false);
});

test("file list load throws a typed error and native list load stays optional", async () => {
  const restore = installFetch(async (url) => {
    if (url.endsWith("/api/client-integrations")) return jsonResponse(503, { error: "down" });
    if (url.endsWith("/api/native-integrations")) return jsonResponse(503, { error: "down" });
    throw new Error(url);
  });
  try {
    await assert.rejects(
      () => loadIntegrationStates(API),
      (error: unknown) => error instanceof IntegrationApiError && error.status === 503 && error.refusal === null,
    );
    assert.equal(await loadNativeIntegrations(API), null);
  } finally {
    restore();
  }
});

test("file toggle and restore send JSON mutations to the loopback writer", async () => {
  const seen: Array<{ url: string; method?: string; body?: string }> = [];
  const restore = installFetch(async (url, init) => {
    seen.push({ url, method: init?.method, body: typeof init?.body === "string" ? init.body : undefined });
    return jsonResponse(200, {
      ok: true,
      clientId: "opencode",
      changed: true,
      state: "current",
      message: "ok",
    });
  });
  try {
    await toggleIntegration(API, "opencode", true);
    await restoreIntegration(API, "op-9", true);
    assert.equal(seen[0]?.url, `${API}/api/client-integrations/opencode`);
    assert.equal(seen[0]?.method, "PUT");
    assert.equal(seen[0]?.body, JSON.stringify({ enabled: true }));
    assert.equal(seen[1]?.url, `${API}/api/client-integrations/restore`);
    assert.equal(seen[1]?.method, "POST");
    assert.equal(seen[1]?.body, JSON.stringify({ opId: "op-9", confirmDrift: true }));
  } finally {
    restore();
  }
});

test("file writer refusals attach the envelope and journal requests encode the client", async () => {
  const restore = installFetch(async (url) => {
    if (url.includes("/journal")) {
      return jsonResponse(200, { operations: [{ opId: "op-1", clientId: "pi", kind: "apply", at: "t", configPath: "/x", snapshot: "stored", undoable: true }] });
    }
    return jsonResponse(409, fileRefusal);
  });
  try {
    await assert.rejects(
      () => toggleIntegration(API, "opencode", false),
      (error: unknown) => (
        error instanceof IntegrationApiError
        && error.refusal?.reason === "conflict"
        && error.message === "file changed after write"
      ),
    );
    const journal = await loadIntegrationJournal(API, "pi");
    assert.equal(journal.operations[0]?.clientId, "pi");
  } finally {
    restore();
  }
});

test("native toggle PUTs enabled and throws NativeApiError on a refusal", async () => {
  const restore = installFetch(async (url, init) => {
    assert.equal(url, `${API}/api/native-integrations/grok`);
    assert.equal(init?.method, "PUT");
    assert.equal(init?.body, JSON.stringify({ enabled: false }));
    return jsonResponse(409, nativeRefusal);
  });
  try {
    await assert.rejects(
      () => toggleNativeIntegration(API, "grok", false),
      (error: unknown) => error instanceof NativeApiError && error.refusal?.reason === "orphaned_marker",
    );
  } finally {
    restore();
  }
});

test("optional status probes fail closed instead of throwing", async () => {
  const restore = installFetch(async () => jsonResponse(500, { error: "no" }));
  try {
    assert.equal(await loadClaudeCodeStatus(API), null);
    assert.equal(await loadClaudeDesktopStatus(API), null);
    assert.equal(await loadGrokFenceStatus(API), null);
    assert.equal(await loadCodexRoutingStatus(API), null);
  } finally {
    restore();
  }
});

test("desktop and grok probes keep undeterminable fields from collapsing to false", async () => {
  const restore = installFetch(async (url) => {
    if (url.endsWith("/api/claude-desktop/status")) {
      return jsonResponse(200, { desiredEnabled: true, installed: true, observedKind: "ok", applied: 1 });
    }
    if (url.endsWith("/api/grok")) {
      return jsonResponse(200, { present: true });
    }
    if (url.endsWith("/api/startup-health")) {
      return jsonResponse(200, { routingInjected: "yes", recommendedCommand: 12 });
    }
    if (url.endsWith("/api/claude-code")) {
      return jsonResponse(200, { enabled: true, authMode: "subscription" });
    }
    throw new Error(url);
  });
  try {
    const desktop = await loadClaudeDesktopStatus(API);
    assert.equal(desktop?.applied, false);
    assert.equal(desktop?.activeProfile, null);
    const grok = await loadGrokFenceStatus(API);
    // The projection summary is only useful whole: a payload missing its counts is no claim
    // about the Grok config at all, rather than a block that looks registered with zero models.
    assert.equal(grok, null);
    const codex = await loadCodexRoutingStatus(API);
    assert.equal(codex?.routingInjected, false);
    assert.equal(codex?.recommendedCommand, null);
    const claude = await loadClaudeCodeStatus(API);
    assert.deepEqual(claude, { enabled: true, authMode: "subscription" });
  } finally {
    restore();
  }
});

test("describeRefusal localizes native reasons, busy codes, residual, and conflict copy", () => {
  assert.equal(
    describeRefusal(translate, new NativeApiError(409, nativeRefusal), "fallback", "/tmp/grok.json"),
    'integrations.native.error.orphanedMarker:{"path":"/tmp/grok.json"}',
  );
  assert.equal(
    describeRefusal(translate, new NativeApiError(409, { ...nativeRefusal, reason: "home_mismatch", message: "homes differ" })),
    "integrations.native.error.homeMismatch homes differ",
  );
  assert.equal(
    describeRefusal(translate, new IntegrationApiError(409, { error: "busy", code: "integration_mutation_busy" })),
    "integrations.error.busy",
  );
  assert.equal(
    describeRefusal(translate, new IntegrationApiError(409, { ...fileRefusal, snapshotPath: "/snap", residual: true })),
    'integrations.error.residual:{"message":"file changed after write","path":"/snap"}',
  );
  assert.equal(
    describeRefusal(translate, new IntegrationApiError(409, { ...fileRefusal, snapshotPath: "/snap" })),
    'integrations.error.recover:{"message":"file changed after write","path":"/snap"}',
  );
  assert.equal(
    describeRefusal(translate, new IntegrationApiError(409, fileRefusal)),
    "integrations.error.conflict file changed after write",
  );
  assert.equal(
    describeRefusal(
      translate,
      new IntegrationApiError(403, {
        error: "denied",
        code: "integration_mutation_failed",
        clientId: "opencode",
        state: "absent",
        reason: "non_loopback",
        message: "english server log",
      }),
    ),
    'integrations.error.nonLoopback:{"client":"opencode"}',
  );
  assert.equal(describeRefusal(translate, new Error("plain")), "plain");
  assert.equal(describeRefusal(translate, { nope: true }, "fallback"), "fallback");
});
