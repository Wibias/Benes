import assert from "node:assert/strict";
import test from "node:test";

import { requestProxyStop } from "../src/listener-commands.ts";

test("POST targets /api/stop", async () => {
  let method = "";
  let url = "";
  const fetchFn = (async (input: RequestInfo | URL, init?: RequestInit) => {
    url = String(input);
    method = String(init?.method);
    return Response.json({ success: true });
  }) as typeof fetch;
  const outcome = await requestProxyStop("http://127.0.0.1:23100", { fetchFn });
  assert.equal(url, "http://127.0.0.1:23100/api/stop");
  assert.equal(method, "POST");
  assert.deepEqual(outcome, { accepted: true });
});

test("transport drop, abort, and timeout after start are accepted", async () => {
  const drop = (async () => { throw new TypeError("network"); }) as typeof fetch;
  assert.deepEqual(await requestProxyStop("", { fetchFn: drop }), { accepted: true });

  const abort = (async () => { throw new DOMException("Aborted", "AbortError"); }) as typeof fetch;
  assert.deepEqual(await requestProxyStop("", { fetchFn: abort }), { accepted: true });
});

test("a received non-2xx or success:false response is authoritative failure", async () => {
  const httpFail = (async () => new Response("nope", { status: 500 })) as typeof fetch;
  const httpOutcome = await requestProxyStop("", {
    fetchFn: httpFail,
    formatFailure: (status) => `custom ${status}`,
  });
  assert.equal(httpOutcome.accepted, false);
  assert.equal(httpOutcome.message, "custom 500");

  const flagged = (async () => Response.json({ success: false, message: " restore me " })) as typeof fetch;
  const flaggedOutcome = await requestProxyStop("", { fetchFn: flagged });
  assert.deepEqual(flaggedOutcome, { accepted: false, message: "restore me" });
});

test("server message precedes error, then the caller formatter", async () => {
  const both = (async () => Response.json({ success: false, message: "msg", error: "err" }, { status: 409 })) as typeof fetch;
  assert.equal((await requestProxyStop("", { fetchFn: both })).message, "msg");

  const errorOnly = (async () => Response.json({ success: false, error: "  err  " }, { status: 409 })) as typeof fetch;
  assert.equal((await requestProxyStop("", { fetchFn: errorOnly })).message, "err");
});

test("a successful received response is accepted even with an empty body", async () => {
  const empty = (async () => new Response("", { status: 200 })) as typeof fetch;
  assert.deepEqual(await requestProxyStop("", { fetchFn: empty }), { accepted: true });
});
