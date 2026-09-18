"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { chooseCloseTarget } = require("./issue-triage.cjs");

const SHARED =
  "POST /v1/responses returned HTTP 410 after the first combo target and before any output.";

describe("duplicate close target", () => {
  it("returns the shared proof line and otherwise stays closed-safe", () => {
    const match = chooseCloseTarget({
      currentIssue: { number: 40, title: "combo hop", body: SHARED },
      candidateIssues: [{ number: 12, title: "earlier", body: SHARED }],
      nominatedIds: ["12"],
    });
    assert.equal(match.number, "12");
    assert.equal(match.signature, SHARED);
    assert.equal(
      chooseCloseTarget({
        currentIssue: { number: 40, title: "combo hop", body: SHARED },
        candidateIssues: [{ number: 12, title: "other", body: "unrelated quota text" }],
        nominatedIds: ["12"],
      }),
      null,
    );
  });
});
