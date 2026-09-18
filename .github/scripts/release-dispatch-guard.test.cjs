"use strict";

const { describe, test } = require("node:test");
const assert = require("node:assert/strict");
const { inspectReleaseTrigger, PUBLISHABLE_REFS } = require("./release-dispatch-guard.cjs");

const AUDITED = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const LATER = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";

function check(patch) {
  return inspectReleaseTrigger({
    event: "workflow_dispatch",
    ref: "refs/heads/main",
    auditedSha: AUDITED,
    observedSha: AUDITED,
    ...patch,
  });
}

describe("release trigger inspection", () => {
  test("publishable refs are only main and preview", () => {
    assert.deepEqual(PUBLISHABLE_REFS, ["refs/heads/main", "refs/heads/preview"]);
  });

  test("main with matching audited SHA is allowed", () => {
    assert.deepEqual(check({}), { ok: true, reason: null });
  });

  test("preview with matching audited SHA is allowed", () => {
    assert.equal(check({ ref: "refs/heads/preview" }).ok, true);
  });

  test("push events are refused", () => {
    const result = check({ event: "push" });
    assert.equal(result.ok, false);
    assert.match(result.reason, /workflow_dispatch/);
  });

  test("dev is not a publishable ref", () => {
    const result = check({ ref: "refs/heads/dev" });
    assert.equal(result.ok, false);
    assert.match(result.reason, /main and preview/);
  });

  test("missing audited SHA is refused", () => {
    const result = check({ auditedSha: "" });
    assert.equal(result.ok, false);
    assert.match(result.reason, /auditedSha is required/);
  });

  test("abbreviated SHA is refused", () => {
    const result = check({ auditedSha: "aaaaaaaaaaaaaaa" });
    assert.equal(result.ok, false);
    assert.match(result.reason, /40-character/);
  });

  test("SHA mismatch after audit is refused", () => {
    const result = check({ observedSha: LATER });
    assert.equal(result.ok, false);
    assert.match(result.reason, /moved after the audit/);
  });
});
