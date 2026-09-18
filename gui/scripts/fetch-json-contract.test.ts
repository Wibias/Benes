import assert from "node:assert/strict";
import test from "node:test";

import { readJsonIfOk, readJsonOrThrow, readManagementError } from "../src/fetch-json.ts";

function jsonResponse(body: unknown, status: number): Response {
  return Response.json(body, { status });
}

test("readJsonOrThrow parses a valid OK JSON body", async () => {
  const payload = await readJsonOrThrow<{ ok: boolean }>(jsonResponse({ ok: true }, 200));
  assert.deepEqual(payload, { ok: true });
});

test("readJsonOrThrow returns undefined for 204 and empty OK bodies", async () => {
  assert.equal(await readJsonOrThrow(new Response(null, { status: 204 })), undefined);
  assert.equal(await readJsonOrThrow(new Response("", { status: 200 })), undefined);
  assert.equal(await readJsonOrThrow(new Response("  \n", { status: 200 })), undefined);
});

test("readJsonOrThrow throws on malformed OK JSON", async () => {
  await assert.rejects(
    () => readJsonOrThrow(new Response("{not-json", { status: 200, headers: { "Content-Type": "application/json" } })),
    SyntaxError,
  );
});

test("readJsonOrThrow accepts a Response-like double that only exposes json()", async () => {
  const mock = {
    ok: true,
    status: 200,
    json: async () => ({ hello: "world" }),
  } as unknown as Response;
  assert.deepEqual(await readJsonOrThrow<{ hello: string }>(mock), { hello: "world" });
});

test("readJsonIfOk returns payload, empty undefined, malformed null, and non-OK null", async () => {
  assert.deepEqual(await readJsonIfOk(jsonResponse({ a: 1 }, 200)), { a: 1 });
  assert.equal(await readJsonIfOk(new Response(null, { status: 204 })), undefined);
  assert.equal(await readJsonIfOk(new Response("", { status: 200 })), undefined);
  assert.equal(await readJsonIfOk(new Response("{not-json", { status: 200 })), null);
  assert.equal(await readJsonIfOk(new Response("nope", { status: 500 })), null);
});

test("readJsonOrThrow prefers error, then message, then HTTP status / caller fallback", async () => {
  await assert.rejects(() => readJsonOrThrow(jsonResponse({ error: "locked" }, 503), "fallback"), { message: "locked" });
  await assert.rejects(
    () => readJsonOrThrow(jsonResponse({ message: "repair marker" }, 500), "fallback"),
    { message: "repair marker" },
  );
  await assert.rejects(
    () => readJsonOrThrow(jsonResponse({ error: "err", message: "msg" }, 500), "fallback"),
    { message: "err" },
  );
  await assert.rejects(
    () => readJsonOrThrow(jsonResponse({ detail: "other" }, 404), "fallback"),
    { message: "fallback" },
  );
  await assert.rejects(() => readJsonOrThrow(jsonResponse({ detail: "other" }, 418)), { message: "HTTP 418" });
  await assert.rejects(() => readJsonOrThrow(new Response("not-json", { status: 500 }), "fallback"), { message: "fallback" });
});

test("readJsonOrThrow does not trim server error copy", async () => {
  await assert.rejects(
    () => readJsonOrThrow(jsonResponse({ error: "  spaced  " }, 400), "fallback"),
    { message: "  spaced  " },
  );
  await assert.rejects(
    () => readJsonOrThrow(jsonResponse({ error: "" }, 400), "fallback"),
    { message: "fallback" },
  );
});

test("readManagementError trims a usable error string and otherwise returns the fallback", async () => {
  assert.equal(await readManagementError(jsonResponse({ error: "  boom  " }, 400), "fallback"), "boom");
  assert.equal(await readManagementError(jsonResponse({ error: "   " }, 400), "fallback"), "fallback");
  assert.equal(await readManagementError(jsonResponse({ message: "ignored" }, 400), "fallback"), "fallback");
  assert.equal(await readManagementError(new Response("not-json", { status: 500 }), "fallback"), "fallback");
});
