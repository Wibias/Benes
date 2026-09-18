"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const { planQualityGate, botDraftOwnershipAfterExecute, FRESHNESS_BEHIND_MAX } = require("./pr-quality-plan.cjs");
const { openReviewFindings } = require("./pr-quality-reviews.cjs");
const { renderRevalidationNotice } = require("./pr-quality-render.cjs");

const HEAD = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
const OUTSIDE = "cr-comment:v1:c0ffee00aabbccddeeff0011";

function snap(extra) {
  return {
    title: "fix(combo): hop 410",
    draft: false,
    headSha: HEAD,
    eventHeadSha: HEAD,
    eventAction: "opened",
    authorHasWrite: false,
    failures: [],
    readiness: { present: true, complete: true, checked: 4, total: 4, items: [] },
    stored: {
      version: 1,
      active: false,
      autoDraftedByBot: false,
      titlePrefixedByBot: false,
      maintainersPinged: false,
      completedAtHeadSha: HEAD,
      reviewReadyLabeled: false,
    },
    findings: { code: null, unresolved: 0, byBot: {} },
    threadsUnreadable: false,
    behindBase: 0,
    behindUnknown: false,
    ...extra,
  };
}

describe("quality gate planner", () => {
  it("fails a wrong base and prefixes the title", () => {
    const plan = planQualityGate(snap({
      title: "fix(combo): hop 410",
      failures: [{ code: "wrong_base" }],
    }));
    assert.equal(plan.kind, "fail");
    assert.equal(plan.failJob, true);
    assert.equal(plan.convertToDraft, true);
    assert.equal(plan.prefixTitle, true);
  });

  it("fails a thin description", () => {
    assert.equal(planQualityGate(snap({ failures: [{ code: "bad_description" }] })).kind, "fail");
  });

  it("fails a missing screenshot", () => {
    assert.equal(planQualityGate(snap({ failures: [{ code: "missing_ui_screenshot" }] })).kind, "fail");
  });

  it("holds a contributor with an incomplete checklist", () => {
    const plan = planQualityGate(snap({
      readiness: { present: true, complete: false, checked: 1, total: 4, items: [] },
    }));
    assert.equal(plan.kind, "hold");
    assert.equal(plan.convertToDraft, true);
    assert.equal(plan.wantReadyLabel, false);
  });

  it("resets a completed checklist after a new head", () => {
    const plan = planQualityGate(snap({
      headSha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
      stored: {
        version: 1,
        active: false,
        autoDraftedByBot: false,
        titlePrefixedByBot: false,
        maintainersPinged: true,
        completedAtHeadSha: HEAD,
        reviewReadyLabeled: true,
      },
    }));
    assert.equal(plan.resetChecklist, true);
    assert.equal(plan.kind, "hold");
    assert.equal(plan.nextState.completedAtHeadSha, null);
    assert.equal(plan.nextState.maintainersPinged, false);
  });

  it("lets a contributor re-attest on the new head after a stale completion", () => {
    const headA = HEAD;
    const headB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
    const readyA = planQualityGate(snap({
      headSha: headA,
      stored: {
        version: 1,
        active: false,
        autoDraftedByBot: false,
        titlePrefixedByBot: false,
        maintainersPinged: false,
        completedAtHeadSha: headA,
        reviewReadyLabeled: false,
      },
    }));
    assert.equal(readyA.kind, "ready");
    assert.equal(readyA.nextState.completedAtHeadSha, headA);
    assert.equal(readyA.nextState.maintainersPinged, true);

    const staleB = planQualityGate(snap({
      headSha: headB,
      eventHeadSha: headB,
      stored: readyA.nextState,
    }));
    assert.equal(staleB.kind, "hold");
    assert.equal(staleB.resetChecklist, true);
    assert.equal(staleB.nextState.completedAtHeadSha, null);
    assert.equal(staleB.nextState.maintainersPinged, false);

    const readyB = planQualityGate(snap({
      headSha: headB,
      eventHeadSha: headB,
      stored: staleB.nextState,
    }));
    assert.equal(readyB.kind, "ready");
    assert.equal(readyB.resetChecklist, false);
    assert.equal(readyB.nextState.completedAtHeadSha, headB);
    assert.equal(readyB.nextState.maintainersPinged, true);

    const stayReady = planQualityGate(snap({
      headSha: headB,
      eventHeadSha: headB,
      stored: readyB.nextState,
    }));
    assert.equal(stayReady.kind, "ready");
    assert.equal(stayReady.resetChecklist, false);
    assert.equal(stayReady.nextState.completedAtHeadSha, headB);
  });

  it("holds when ancestry freshness is unknown", () => {
    const plan = planQualityGate(snap({ behindUnknown: true }));
    assert.equal(plan.kind, "hold");
    assert.ok(plan.untickClaims.length > 0);
  });

  it("holds on an unresolved bot finding", () => {
    const plan = planQualityGate(snap({
      findings: { code: "review_findings", unresolved: 1, byBot: { "coderabbitai[bot]": 1 } },
    }));
    assert.equal(plan.kind, "hold");
  });

  it("marks a complete contributor PR ready", () => {
    const plan = planQualityGate(snap({}));
    assert.equal(plan.kind, "ready");
    assert.equal(plan.wantReadyLabel, true);
    assert.equal(plan.markReady, false);
  });

  it("recovers a bot-owned maintainer draft and preserves a human draft on hold", () => {
    const recover = planQualityGate(snap({
      authorHasWrite: true,
      draft: true,
      stored: {
        version: 1,
        active: true,
        autoDraftedByBot: true,
        titlePrefixedByBot: false,
        maintainersPinged: false,
        completedAtHeadSha: null,
        reviewReadyLabeled: false,
      },
    }));
    assert.equal(recover.kind, "recover");
    assert.equal(recover.markReady, true);

    const human = planQualityGate(snap({
      draft: true,
      readiness: { present: true, complete: false, checked: 0, total: 4, items: [] },
    }));
    assert.equal(human.kind, "hold");
    assert.equal(human.preserveHumanDraft, true);
    assert.equal(human.convertToDraft, false);
    assert.equal(human.nextState.autoDraftedByBot, false);
  });

  it("keeps bot draft ownership across a later blocked run and recovers it", () => {
    const firstFail = planQualityGate(snap({
      draft: false,
      failures: [{ code: "bad_description" }],
    }));
    assert.equal(firstFail.kind, "fail");
    assert.equal(firstFail.convertToDraft, true);
    assert.equal(firstFail.nextState.autoDraftedByBot, true);
    assert.equal(
      botDraftOwnershipAfterExecute({
        plan: firstFail,
        convertDraftErrored: false,
        readyConverted: false,
      }),
      true,
    );

    const alreadyDraft = planQualityGate(snap({
      draft: true,
      failures: [{ code: "bad_description" }],
      stored: firstFail.nextState,
    }));
    assert.equal(alreadyDraft.kind, "fail");
    assert.equal(alreadyDraft.convertToDraft, false);
    assert.equal(alreadyDraft.nextState.autoDraftedByBot, true);
    assert.equal(
      botDraftOwnershipAfterExecute({
        plan: alreadyDraft,
        convertDraftErrored: false,
        readyConverted: false,
      }),
      true,
    );

    const recover = planQualityGate(snap({
      authorHasWrite: true,
      draft: true,
      stored: alreadyDraft.nextState,
    }));
    assert.equal(recover.kind, "recover");
    assert.equal(recover.markReady, true);
    assert.equal(recover.nextState.autoDraftedByBot, true);
    assert.equal(
      botDraftOwnershipAfterExecute({
        plan: recover,
        convertDraftErrored: false,
        readyConverted: true,
      }),
      false,
    );
  });

  it("never claims a human draft and never owns a failed conversion", () => {
    const human = planQualityGate(snap({
      draft: true,
      failures: [{ code: "wrong_base" }],
    }));
    assert.equal(human.preserveHumanDraft, true);
    assert.equal(human.nextState.autoDraftedByBot, false);

    const failedConvert = planQualityGate(snap({
      draft: false,
      failures: [{ code: "wrong_base" }],
    }));
    assert.equal(
      botDraftOwnershipAfterExecute({
        plan: failedConvert,
        convertDraftErrored: true,
        readyConverted: false,
      }),
      false,
    );
  });

  it("uses the shared freshness threshold for planning and copy", () => {
    assert.equal(FRESHNESS_BEHIND_MAX, 10);
    const hold = planQualityGate(snap({ behindBase: FRESHNESS_BEHIND_MAX + 1 }));
    assert.equal(hold.kind, "hold");
    assert.equal(hold.freshness.includes("latest_dev"), true);
    const ok = planQualityGate(snap({ behindBase: FRESHNESS_BEHIND_MAX }));
    assert.equal(ok.freshness.includes("latest_dev"), false);
    const copy = renderRevalidationNotice({ freshness: ["latest_dev"] }).join("\n");
    assert.match(copy, new RegExp(String(FRESHNESS_BEHIND_MAX)));
    assert.doesNotMatch(copy, /The checklist has been reset/);
    assert.doesNotMatch(copy, /Resolve every open review conversation/);
  });
});

describe("CodeRabbit finding composition", () => {
  it("keeps cr-comment:v1 outside-diff identities", () => {
    const result = openReviewFindings({
      threads: [],
      reviews: [{
        id: 1,
        commit_id: HEAD,
        submitted_at: "2026-09-02T12:00:00Z",
        user: { login: "coderabbitai[bot]" },
        body: `outside diff range comments\n<!-- ${OUTSIDE} -->`,
      }],
      liveHeadSha: HEAD,
    });
    assert.equal(result.unresolved, 1);
  });

  it("does not add the actionable-count total on top of inline threads", () => {
    const result = openReviewFindings({
      threads: [{ isResolved: false, author: { login: "coderabbitai[bot]" } }],
      reviews: [{
        id: 1,
        commit_id: HEAD,
        submitted_at: "2026-09-02T12:00:00Z",
        user: { login: "coderabbitai[bot]" },
        body: "**Actionable comments posted: 4**",
      }],
      liveHeadSha: HEAD,
    });
    assert.equal(result.unresolved, 1);
  });

  it("uses the actionable-count total only when there are no inline threads or cr-comment identities", () => {
    const result = openReviewFindings({
      threads: [],
      reviews: [{
        id: 1,
        commit_id: HEAD,
        submitted_at: "2026-09-02T12:00:00Z",
        user: { login: "coderabbitai[bot]" },
        body: "**Actionable comments posted: 2**",
      }],
      liveHeadSha: HEAD,
    });
    assert.equal(result.unresolved, 2);
  });
});
