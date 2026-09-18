"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const { listAutomationTests } = require("./run-automation-tests.cjs");

describe("listAutomationTests", () => {
  it("includes every *.test.cjs next to this runner", () => {
    const listed = listAutomationTests().map((file) => path.basename(file));
    const onDisk = fs
      .readdirSync(__dirname)
      .filter((file) => file.endsWith(".test.cjs"))
      .sort();
    assert.deepEqual(listed, onDisk);
    assert.ok(listed.includes("issue-quality.test.cjs"));
    assert.ok(listed.includes("pr-quality.test.cjs"));
    assert.ok(listed.includes("issue-triage.test.cjs"));
    assert.ok(listed.includes("issue-triage-autoclose.test.cjs"));
    assert.ok(listed.includes("issue-triage-run.test.cjs"));
    assert.ok(listed.includes("issue-translation.test.cjs"));
    assert.ok(listed.includes("issue-translation-run.test.cjs"));
    assert.ok(listed.includes("issue-ai.test.cjs"));
  });
});
