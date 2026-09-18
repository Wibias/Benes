"use strict";

const { CODEX_BOT, CODE_RABBIT_BOT } = require("./pr-quality-frozen.cjs");

const ACTIONABLE_LINE = /\*\*Actionable comments posted:\s*(\d+)\*\*/i;
const CR_COMMENT_ID = /<!--\s*(cr-comment:v1:[a-f0-9]+)\s*-->/gi;
const BOT_REVIEWERS = [CODEX_BOT, CODE_RABBIT_BOT];

function submittedMs(review) {
  const parsed = Date.parse(String(review?.submitted_at ?? ""));
  return Number.isNaN(parsed) ? -Infinity : parsed;
}

function pickCodeRabbitReview({ reviews = [], liveHeadSha }) {
  if (!liveHeadSha || !Array.isArray(reviews) || reviews.length === 0) return null;
  let latest = null;
  let latestTime = -Infinity;
  let latestId = -1;
  for (const review of reviews) {
    if (review?.commit_id !== liveHeadSha) continue;
    if (review?.user?.login !== CODE_RABBIT_BOT) continue;
    const time = submittedMs(review);
    const id = Number(review?.id ?? -1);
    if (latest === null || time > latestTime || (time === latestTime && id > latestId)) {
      latest = review;
      latestTime = time;
      latestId = id;
    }
  }
  return latest;
}

function durableOutsideDiffIds({ reviews = [], liveHeadSha }) {
  const review = pickCodeRabbitReview({ reviews, liveHeadSha });
  const body = String(review?.body ?? "");
  if (!/outside diff range comments/i.test(body)) return [];
  const ids = [];
  const seen = new Set();
  for (const match of body.matchAll(CR_COMMENT_ID)) {
    const id = match[1].toLowerCase();
    if (seen.has(id)) continue;
    seen.add(id);
    ids.push(id);
  }
  return ids;
}

function legacyActionableCount({ reviews = [], liveHeadSha }) {
  const review = pickCodeRabbitReview({ reviews, liveHeadSha });
  const match = String(review?.body ?? "").match(ACTIONABLE_LINE);
  if (!match) return { code: null, unresolved: 0, byBot: {} };
  const count = Number(match[1]);
  if (!(count > 0)) return { code: null, unresolved: 0, byBot: {} };
  return {
    code: "review_findings",
    unresolved: count,
    byBot: { [CODE_RABBIT_BOT]: count },
  };
}

function openReviewFindings({ threads = [], reviews = [], liveHeadSha }) {
  const byBot = {};
  let unresolved = 0;
  for (const thread of threads) {
    const login = thread?.author?.login;
    if (!BOT_REVIEWERS.includes(login)) continue;
    if (thread.isResolved === true) continue;
    byBot[login] = (byBot[login] ?? 0) + 1;
    unresolved += 1;
  }
  const outsideIds = durableOutsideDiffIds({ reviews, liveHeadSha });
  if (outsideIds.length > 0) {
    byBot[CODE_RABBIT_BOT] = (byBot[CODE_RABBIT_BOT] ?? 0) + outsideIds.length;
    unresolved += outsideIds.length;
  } else if (unresolved === 0) {
    const legacy = legacyActionableCount({ reviews, liveHeadSha });
    if (legacy.code) return legacy;
  }
  if (unresolved > 0) return { code: "review_findings", unresolved, byBot };
  return { code: null, unresolved: 0, byBot };
}

function unreadFindings() {
  return { code: "review_findings", unresolved: 0, byBot: {} };
}

function threadAuthors(nodes) {
  return nodes.map((node) => ({
    isResolved: node.isResolved,
    author: node.comments?.nodes?.[0]?.author ?? null,
  }));
}

module.exports = {
  BOT_REVIEWERS,
  CODE_RABBIT_BOT,
  ACTIONABLE_LINE,
  CR_COMMENT_ID,
  pickCodeRabbitReview,
  durableOutsideDiffIds,
  legacyActionableCount,
  openReviewFindings,
  unreadFindings,
  threadAuthors,
};
