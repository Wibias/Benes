"use strict";

const { WRONG_BASE_PREFIX, REVIEW_READY, CLAIM_BOX, FRESHNESS_BEHIND_MAX } = require("./pr-quality-frozen.cjs");
const { completionDrifted, blankGateRecord } = require("./pr-quality-legacy-state.cjs");

function planQualityGate(snapshot) {
  const failures = Array.isArray(snapshot.failures) ? snapshot.failures : [];
  const contributor = snapshot.authorHasWrite !== true;
  const readiness = snapshot.readiness || { present: false, complete: false, checked: 0, total: 0, items: [] };
  const stored = { ...blankGateRecord(), ...(snapshot.stored || {}) };
  const title = String(snapshot.title || "");
  const draft = Boolean(snapshot.draft);
  const headSha = snapshot.headSha || "";
  const wrongBase = failures.some((failure) => failure.code === "wrong_base");

  const staleHead = completionDrifted({
    checklistRequired: contributor,
    checklistComplete: readiness.present && readiness.complete,
    readinessPresent: readiness.present,
    completionHeadSha: stored.completedAtHeadSha,
    eventHeadSha: snapshot.eventHeadSha ?? headSha,
    liveHeadSha: headSha,
    eventAction: snapshot.eventAction,
  });

  const findingsOpen = Boolean(snapshot.findings?.code);
  const freshness = [];
  if (
    snapshot.behindUnknown ||
    (typeof snapshot.behindBase === "number" && snapshot.behindBase > FRESHNESS_BEHIND_MAX)
  ) {
    freshness.push("latest_dev");
  }
  if (findingsOpen || snapshot.threadsUnreadable) freshness.push("review_findings");

  const boxesComplete = readiness.present && readiness.complete && !staleHead && freshness.length === 0;
  const blocked = failures.length > 0;
  const holding = contributor && !boxesComplete;

  let kind = "idle";
  if (blocked) kind = "fail";
  else if (holding) kind = "hold";
  else if (contributor) kind = "ready";
  else if (stored.autoDraftedByBot && draft) kind = "recover";
  else if (stored.active) kind = "clear";

  const autoDraftedByBot =
    kind === "fail" || kind === "hold"
      ? draft
        ? Boolean(stored.autoDraftedByBot)
        : true
      : stored.autoDraftedByBot;

  return {
    kind,
    failures,
    contributor,
    injectChecklist: contributor && !readiness.present,
    dropChecklist: !contributor && readiness.present,
    resetChecklist: staleHead && readiness.present && readiness.complete,
    untickClaims: freshness.filter((code) => CLAIM_BOX[code] != null).map((code) => CLAIM_BOX[code]),
    prefixTitle: wrongBase && !title.startsWith(WRONG_BASE_PREFIX),
    stripOwnedPrefix:
      !wrongBase && stored.titlePrefixedByBot && title.startsWith(WRONG_BASE_PREFIX),
    convertToDraft: (kind === "fail" || kind === "hold") && !draft,
    preserveHumanDraft: (kind === "fail" || kind === "hold") && draft && !stored.autoDraftedByBot,
    markReady: (kind === "ready" || kind === "recover") && draft,
    wantReadyLabel: kind === "ready",
    failJob: kind === "fail",
    pingMaintainers: kind === "ready" && !stored.maintainersPinged,
    nextState: {
      ...stored,
      active: kind === "fail" || kind === "hold" || kind === "recover",
      autoDraftedByBot,
      titlePrefixedByBot: wrongBase ? true : stored.titlePrefixedByBot && title.startsWith(WRONG_BASE_PREFIX),
      maintainersPinged: staleHead ? false : kind === "ready" ? true : stored.maintainersPinged,
      completedAtHeadSha: staleHead ? null : kind === "ready" ? headSha : stored.completedAtHeadSha,
      reviewReadyLabeled: kind === "ready",
    },
    reviewReadyLabel: REVIEW_READY,
    staleHead,
    freshness,
  };
}

function botDraftOwnershipAfterExecute({ plan, convertDraftErrored, readyConverted }) {
  if (plan.convertToDraft) return !convertDraftErrored;
  if (plan.markReady && readyConverted) return false;
  return Boolean(plan.nextState.autoDraftedByBot);
}

module.exports = {
  planQualityGate,
  botDraftOwnershipAfterExecute,
  FRESHNESS_BEHIND_MAX,
};
