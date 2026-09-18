"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  decodeGateRecord,
  encodeGateRecord,
  blankGateRecord,
  blendLegacyRecords,
  completionDrifted,
  openReviewFindings,
  pickCodeRabbitReview,
  durableOutsideDiffIds,
  composeGateBody,
  GATE_HTML,
} = require("./pr-quality-reconcile.cjs");

const HEAD = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const OUTSIDE_A = "cr-comment:v1:c0ffee00aabbccddeeff0011";
const OUTSIDE_B = "cr-comment:v1:aabbccddeeff001122334455";

function rabbit(overrides = {}) {
  return {
    id: 9001,
    commit_id: HEAD,
    submitted_at: "2026-09-02T12:00:00Z",
    user: { login: "coderabbitai[bot]" },
    body: `**Actionable comments posted: 4**\n\n> [!CAUTION]\n> Some comments are outside the diff and can’t be posted inline due to platform limitations.\n> <details>\n> <summary>⚠️ Outside diff range comments (2)</summary>\n> <!-- ${OUTSIDE_A} -->\n> <!-- ${OUTSIDE_B} -->\n> </details>`,
    ...overrides,
  };
}

describe("bot-owned gate records", () => {
  it("round-trips the frozen gate marker", () => {
    const state = blankGateRecord();
    const body = `${GATE_HTML}\n${encodeGateRecord(state)}`;
    assert.deepEqual(decodeGateRecord(body), state);
  });

  it("blends leftover enforcer and readiness markers", () => {
    const blended = blendLegacyRecords(
      { active: true, autoDraftedByBot: true, titlePrefixedByBot: true },
      { maintainersPinged: true, completedAtHeadSha: HEAD },
    );
    assert.equal(blended.active, true);
    assert.equal(blended.autoDraftedByBot, true);
    assert.equal(blended.completedAtHeadSha, HEAD);
  });

  it("treats a completed checklist on a new head as stale", () => {
    assert.equal(
      completionDrifted({
        checklistRequired: true,
        checklistComplete: true,
        readinessPresent: true,
        completionHeadSha: "old",
        eventHeadSha: HEAD,
        liveHeadSha: HEAD,
        eventAction: "synchronize",
      }),
      true,
    );
    assert.equal(
      completionDrifted({
        checklistRequired: true,
        checklistComplete: true,
        readinessPresent: true,
        completionHeadSha: HEAD,
        eventHeadSha: HEAD,
        liveHeadSha: HEAD,
        eventAction: "synchronize",
      }),
      false,
    );
  });
});

describe("CodeRabbit outside-diff findings", () => {
  it("blocks unresolved outside-diff identities on the live head", () => {
    assert.deepEqual(durableOutsideDiffIds({ reviews: [rabbit()], liveHeadSha: HEAD }), [
      OUTSIDE_A,
      OUTSIDE_B,
    ]);
    assert.deepEqual(
      openReviewFindings({ threads: [], reviews: [rabbit()], liveHeadSha: HEAD }),
      {
        code: "review_findings",
        unresolved: 2,
        byBot: { "coderabbitai[bot]": 2 },
      },
    );
  });

  it("clears when a later live-head review has no outside-diff markers", () => {
    assert.deepEqual(
      openReviewFindings({
        threads: [],
        reviews: [
          rabbit({ id: 9000, submitted_at: "2026-08-07T05:00:00Z" }),
          rabbit({
            id: 9002,
            submitted_at: "2026-08-07T07:00:00Z",
            body: "**Actionable comments posted: 0**",
          }),
        ],
        liveHeadSha: HEAD,
      }),
      { code: null, unresolved: 0, byBot: {} },
    );
  });

  it("does not invent findings when CodeRabbit is absent", () => {
    assert.deepEqual(
      openReviewFindings({ threads: [], reviews: [], liveHeadSha: HEAD }),
      { code: null, unresolved: 0, byBot: {} },
    );
  });

  it("ignores CodeRabbit reviews of a different head", () => {
    assert.equal(
      pickCodeRabbitReview({
        reviews: [rabbit({ commit_id: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" })],
        liveHeadSha: HEAD,
      }),
      null,
    );
    assert.deepEqual(
      openReviewFindings({
        threads: [],
        reviews: [rabbit({ commit_id: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" })],
        liveHeadSha: HEAD,
      }),
      { code: null, unresolved: 0, byBot: {} },
    );
  });
});

describe("bot comment ownership", () => {
  it("writes a gate comment that still carries the frozen HTML marker", () => {
    const lines = composeGateBody(blankGateRecord(), {
      status: "DRAFT",
      statusReason: "wrong target branch.",
      actions: ["Point this PR at `dev`."],
      readiness: { present: false, complete: false, checked: 0, total: 0, items: [] },
      checklistRequired: false,
      notices: [],
    });
    const body = lines.join("\n");
    assert.match(body, /<!-- benes-pr-gate -->/);
    assert.match(body, /<!-- benes-pr-gate-state:/);
    assert.doesNotMatch(body, /human-owned/);
  });

  it("does not treat a human comment as bot-owned gate state", () => {
    const human = "Please retarget this PR.\n<!-- benes-pr-gate -->";
    assert.equal(decodeGateRecord(human), null);
  });
});
