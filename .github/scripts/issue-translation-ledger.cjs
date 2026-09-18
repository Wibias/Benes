"use strict";

const {
  LEDGER_MARKER,
  LEDGER_V2_OPEN,
  LEDGER_V3_OPEN,
  ACTIONS_BOT,
  ISSUE_SOURCE_KEY,
  ZERO_SOURCE_HASH,
  HISTORICAL_V2_PREFIX,
  sanitizeLanguageTag,
} = require("./issue-translation.cjs");

const HTML_COMMENT_CLOSE = " -->";
const LEDGER_CLOCK_SLACK_MS = 5 * 60 * 1000;

function isVerifiedLedgerComment(comment) {
  return comment?.user?.login === ACTIONS_BOT && String(comment?.body || "").includes(LEDGER_MARKER);
}

function packLedgerPayload(record) {
  return Buffer.from(JSON.stringify(record), "utf8").toString("base64url");
}

function decodeJsonPayload(encoded) {
  try {
    return JSON.parse(Buffer.from(String(encoded), "base64url").toString("utf8"));
  } catch {
    return null;
  }
}

function sliceMarkerPayload(body, openMarker) {
  const text = String(body || "");
  const open = text.indexOf(openMarker);
  if (open === -1) return null;
  const from = open + openMarker.length;
  const closeAt = text.indexOf(HTML_COMMENT_CLOSE, from);
  if (closeAt === -1) return null;
  return text.slice(from, closeAt);
}

function emptyNormalized() {
  return {
    finished: {},
    lastTryMs: 0,
    tryMs: [],
    commitReady: false,
    language: null,
  };
}

function decodeHistoricalV2(parsed, now) {
  if (!parsed || parsed.v !== 2) return null;
  if (typeof parsed.sourceHash !== "string" || parsed.sourceHash.length !== 16) return null;
  if (typeof parsed.attemptedAt !== "number" || parsed.attemptedAt > now + LEDGER_CLOCK_SLACK_MS) return null;
  if (!Array.isArray(parsed.recent)) return null;
  if (typeof parsed.requiresTranslation !== "boolean") return null;
  const finished = {};
  for (const [key, value] of Object.entries(parsed.sourceHashes || {})) {
    if (typeof value === "string" && value.length === 16 && value !== ZERO_SOURCE_HASH) {
      finished[key] = `${HISTORICAL_V2_PREFIX}${value}`;
    }
  }
  if (parsed.sourceHash !== ZERO_SOURCE_HASH && !finished[ISSUE_SOURCE_KEY]) {
    finished[ISSUE_SOURCE_KEY] = `${HISTORICAL_V2_PREFIX}${parsed.sourceHash}`;
  }
  const language =
    parsed.detectedLanguage == null ? null : sanitizeLanguageTag(parsed.detectedLanguage);
  return {
    finished,
    lastTryMs: parsed.attemptedAt,
    tryMs: parsed.recent.filter((ts) => typeof ts === "number"),
    commitReady: Object.keys(finished).length > 0,
    language: language && language.toLowerCase() !== "unknown" ? language : null,
  };
}

function decodeNativeV3(parsed, now) {
  if (!parsed || parsed.v !== 3) return null;
  if (!parsed.finished || typeof parsed.finished !== "object" || Array.isArray(parsed.finished)) return null;
  if (typeof parsed.lastTryMs !== "number" || parsed.lastTryMs > now + LEDGER_CLOCK_SLACK_MS) return null;
  if (!Array.isArray(parsed.tryMs)) return null;
  if (typeof parsed.commitReady !== "boolean") return null;
  const finished = {};
  for (const [key, value] of Object.entries(parsed.finished)) {
    if (typeof value === "string" && value !== "") finished[key] = value;
  }
  const language =
    typeof parsed.language === "string" && parsed.language.trim()
      ? sanitizeLanguageTag(parsed.language)
      : null;
  return {
    finished,
    lastTryMs: parsed.lastTryMs,
    tryMs: parsed.tryMs.filter((ts) => typeof ts === "number"),
    commitReady: parsed.commitReady,
    language: language && language.toLowerCase() !== "unknown" ? language : null,
  };
}

function recordFromCommentBody(body, now = Date.now()) {
  const v3raw = sliceMarkerPayload(body, LEDGER_V3_OPEN);
  if (v3raw) {
    const native = decodeNativeV3(decodeJsonPayload(v3raw), now);
    if (native) return native;
  }
  const v2raw = sliceMarkerPayload(body, LEDGER_V2_OPEN);
  if (v2raw) return decodeHistoricalV2(decodeJsonPayload(v2raw), now);
  return null;
}

function verifiedLedgerComments(comments) {
  return (Array.isArray(comments) ? comments : []).filter(isVerifiedLedgerComment);
}

function readAuthoritativeLedger(comments, _issueNumber, now = Date.now()) {
  let best = null;
  for (const comment of verifiedLedgerComments(comments)) {
    const record = recordFromCommentBody(comment.body, now);
    if (!record) continue;
    if (!best || record.lastTryMs >= best.lastTryMs) best = record;
  }
  return best;
}

function oldestStickyLedgerComment(comments) {
  let sticky = null;
  for (const comment of verifiedLedgerComments(comments)) {
    if (!Number.isSafeInteger(comment?.id) || comment.id <= 0) continue;
    if (!sticky || comment.id < sticky.id) sticky = comment;
  }
  return sticky;
}

function nativeV3Record(state) {
  const record = {
    v: 3,
    finished: { ...(state.finished || {}) },
    lastTryMs: state.lastTryMs,
    tryMs: [...(state.tryMs || [])],
    commitReady: state.commitReady === true,
    language: state.language ?? null,
  };
  return record;
}

function renderLedgerComment(state) {
  const record = nativeV3Record(state);
  const lines = [
    LEDGER_MARKER,
    `${LEDGER_V3_OPEN}${packLedgerPayload(record)}${HTML_COMMENT_CLOSE}`,
    "",
  ];
  if (record.language) {
    lines.push(`<sub>Benes translation control. Language: ${sanitizeLanguageTag(record.language)}.</sub>`);
  } else {
    lines.push("<sub>Benes translation control.</sub>");
  }
  return lines.join("\n");
}

function foldAttemptIntoLedger({ priorState = null, attempt, now = Date.now() }) {
  const prior = priorState && typeof priorState === "object" ? priorState : emptyNormalized();
  const tryMs = [...(prior.tryMs || []).filter((ts) => now - ts < 3_600_000), now].slice(-32);
  const finished = { ...(prior.finished || {}) };
  const commitReady = attempt?.commitReady === true;
  if (commitReady && attempt?.identity && (attempt.sourceKey || ISSUE_SOURCE_KEY)) {
    finished[attempt.sourceKey || ISSUE_SOURCE_KEY] = attempt.identity;
  }
  return {
    finished,
    lastTryMs: now,
    tryMs,
    commitReady,
    language: attempt?.language ?? null,
  };
}

async function commitLedgerAttempt({
  github,
  owner,
  repo,
  issue_number,
  comments,
  priorState,
  attempt,
}) {
  const state = foldAttemptIntoLedger({ priorState, attempt });
  const body = renderLedgerComment(state);
  const sticky = oldestStickyLedgerComment(comments);
  if (sticky) {
    await github.rest.issues.updateComment({
      owner,
      repo,
      comment_id: sticky.id,
      body,
    });
    return;
  }
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number,
    body,
  });
}

module.exports = {
  LEDGER_CLOCK_SLACK_MS,
  readAuthoritativeLedger,
  commitLedgerAttempt,
  foldAttemptIntoLedger,
  renderLedgerComment,
  recordFromCommentBody,
  decodeHistoricalV2,
  decodeNativeV3,
};
