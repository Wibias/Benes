import assert from "node:assert/strict";
import test from "node:test";

import { fetchCodexAppServerState } from "../src/listener-commands.ts";
import { CODEX_APP_SERVER_STATE_PATH } from "../src/lib/codex-restart-contract.ts";

const SILENT = { state: null, runningCount: 0 };

test("GET uses the canonical Codex app-server path and forwards the caller signal", async () => {
  const signal = new AbortController().signal;
  let seen = "";
  let forwarded: AbortSignal | undefined;
  const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    seen = String(input);
    forwarded = init?.signal;
    return Response.json({ state: "fresh", runningCount: 2 });
  }) as typeof fetch;
  const outcome = await fetchCodexAppServerState("http://127.0.0.1:23100", { fetchFn, signal });
  assert.equal(seen, `http://127.0.0.1:23100${CODEX_APP_SERVER_STATE_PATH}`);
  assert.equal(forwarded, signal);
  assert.deepEqual(outcome, { state: "fresh", runningCount: 2 });
});

test("non-OK, malformed, invalid, network, and abort readings stay silent", async () => {
  const notOk = (async () => new Response("nope", { status: 500 })) as typeof fetch;
  assert.deepEqual(await fetchCodexAppServerState("", { fetchFn: notOk }), SILENT);

  const malformed = (async () => new Response("{not-json", { status: 200 })) as typeof fetch;
  assert.deepEqual(await fetchCodexAppServerState("", { fetchFn: malformed }), SILENT);

  const invalid = (async () => Response.json({ state: "fresh" })) as typeof fetch;
  assert.deepEqual(await fetchCodexAppServerState("", { fetchFn: invalid }), SILENT);

  const network = (async () => { throw new Error("offline"); }) as typeof fetch;
  assert.deepEqual(await fetchCodexAppServerState("", { fetchFn: network }), SILENT);

  const abort = (async () => { throw new DOMException("Aborted", "AbortError"); }) as typeof fetch;
  assert.deepEqual(await fetchCodexAppServerState("", { fetchFn: abort }), SILENT);
});
