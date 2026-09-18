"use strict";

const crypto = require("node:crypto");

const GENERATED_OPEN = "<!-- benes-issue-inline-translator -->";
const GENERATED_CLOSE = "<!-- /benes-issue-inline-translator -->";
const LEDGER_MARKER = "<!-- benes-issue-inline-translator-control -->";
const LEDGER_V2_OPEN = "<!-- benes-issue-inline-translator-control-state-v2:";
const LEDGER_V3_OPEN = "<!-- benes-issue-inline-translator-control-state-v3:";
const ACTIONS_BOT = "github-actions[bot]";
const ISSUE_BODY_MAX = 65536;
const ISSUE_SOURCE_KEY = "issue";
const ZERO_SOURCE_HASH = "0000000000000000";
const TRANSLATION_SUMMARY = "English translation";
const CURRENT_SOURCE_PREFIX = "sha256:";
const HISTORICAL_V2_PREFIX = "v2:";

const TRANSLATION_POLICY = Object.freeze({
  minAuthorChars: 20,
  minGapMs: 60_000,
  maxAttemptsPerHour: 10,
  // Issues: title + author body. Comments: comment body only. `comment:<id>` is identity, not length.
  issueAuthor: "title_and_body",
  commentAuthor: "body_only",
});

function asText(value) {
  return value == null ? "" : String(value);
}

function sha256Hex(preimage) {
  return crypto.createHash("sha256").update(preimage, "utf8").digest("hex");
}

function currentSourceIdentity({ title = "", body = "" } = {}) {
  return `${CURRENT_SOURCE_PREFIX}${sha256Hex(
    `benes-translation-source-v1\ntitle:${asText(title)}\nbody:\n${asText(body)}`,
  )}`;
}

function historicalV2Identity({ title = "", body = "" } = {}) {
  return sha256Hex(`title:${asText(title)}\nbody:\n${asText(body)}`).slice(0, 16);
}

function sourceIsFinished(finished, sourceKey, { title, body }) {
  const stored = finished?.[sourceKey];
  if (typeof stored !== "string" || stored === "") return false;
  if (stored === currentSourceIdentity({ title, body })) return true;
  return stored === `${HISTORICAL_V2_PREFIX}${historicalV2Identity({ title, body })}`;
}

function authorCharCount({ title = "", body = "", authorText = null } = {}) {
  if (authorText != null) return asText(authorText).trim().length;
  return `${asText(title).trim()}\n${asText(body).trim()}`.trim().length;
}

function translationMarkupReason(body) {
  const text = asText(body);
  let index = 0;
  let open = false;
  let pairs = 0;
  while (index < text.length) {
    const openAt = text.indexOf(GENERATED_OPEN, index);
    const closeAt = text.indexOf(GENERATED_CLOSE, index);
    const nextOpen = openAt === -1 ? Number.POSITIVE_INFINITY : openAt;
    const nextClose = closeAt === -1 ? Number.POSITIVE_INFINITY : closeAt;
    if (nextOpen === Number.POSITIVE_INFINITY && nextClose === Number.POSITIVE_INFINITY) break;
    if (nextOpen < nextClose) {
      if (open) return "malformed_translation_markup";
      open = true;
      index = openAt + GENERATED_OPEN.length;
      continue;
    }
    if (!open) return "malformed_translation_markup";
    open = false;
    pairs += 1;
    if (pairs > 1) return "malformed_translation_markup";
    index = closeAt + GENERATED_CLOSE.length;
  }
  if (open) return "malformed_translation_markup";
  return null;
}

function authorTextWithoutBotTranslation(body) {
  const text = asText(body);
  if (translationMarkupReason(text)) return text;
  const openAt = text.indexOf(GENERATED_OPEN);
  if (openAt === -1) return text.replace(/\s+$/, "");
  const closeAt = text.indexOf(GENERATED_CLOSE, openAt + GENERATED_OPEN.length);
  const before = text.slice(0, openAt).replace(/\s+$/, "");
  const after = text
    .slice(closeAt + GENERATED_CLOSE.length)
    .replace(/^\s+/, "")
    .replace(/\s+$/, "");
  if (!before) return after;
  if (!after) return before;
  return `${before}\n\n${after}`;
}

function stripTranslationBlock(body) {
  return authorTextWithoutBotTranslation(body);
}

function splitTranslationBlock(body) {
  const text = asText(body);
  const sourceBody = authorTextWithoutBotTranslation(text);
  const openAt = text.indexOf(GENERATED_OPEN);
  if (openAt === -1 || !translationBlockIsClosed(text)) {
    return { found: false, block: "", sourceBody };
  }
  const closeAt = text.indexOf(GENERATED_CLOSE, openAt + GENERATED_OPEN.length);
  return {
    found: true,
    block: text.slice(openAt, closeAt + GENERATED_CLOSE.length),
    sourceBody,
  };
}

function translationBlockIsClosed(body) {
  return translationMarkupReason(body) == null;
}

function neutralizeMentions(text) {
  return asText(text).replace(/(^|[\s(])@([A-Za-z0-9][A-Za-z0-9-]{0,38})/g, "$1@\u200b$2");
}

function cleanModelBody(raw) {
  return neutralizeMentions(
    asText(raw).split(GENERATED_OPEN).join("").split(GENERATED_CLOSE).join(""),
  ).trim();
}

function renderBotTranslationBlock(translatedBody) {
  return [
    GENERATED_OPEN,
    "<details>",
    `<summary>${TRANSLATION_SUMMARY}</summary>`,
    "",
    cleanModelBody(translatedBody),
    "",
    "</details>",
    GENERATED_CLOSE,
  ].join("\n");
}

function attachGeneratedBlock(sourceBody, translatedBody) {
  const reason = translationMarkupReason(sourceBody);
  if (reason) {
    const error = new Error(reason);
    error.code = reason;
    throw error;
  }
  const author = authorTextWithoutBotTranslation(sourceBody);
  const block = renderBotTranslationBlock(translatedBody);
  const next = author ? `${author}\n\n${block}\n` : `${block}\n`;
  if (next.length > ISSUE_BODY_MAX) {
    throw new Error("Issue body is over GitHub's 65536-character cap after fitting the translation.");
  }
  return next;
}

function composeTranslatedComment(sourceBody, translatedBody, detectedLanguage) {
  const lang = sanitizeLanguageTag(detectedLanguage);
  const lines = [];
  if (lang && lang !== "non-English") lines.push(`Language: ${lang}`, "");
  lines.push(asText(translatedBody));
  return attachGeneratedBlock(sourceBody, lines.join("\n"));
}

function sanitizeLanguageTag(value) {
  const cleaned = asText(value)
    .replace(/[^\p{L}\p{N} ()-]/gu, "")
    .replace(/\s+/g, " ")
    .trim()
    .slice(0, 64);
  return cleaned || "non-English";
}

function languageLooksEnglish(value) {
  const tag = sanitizeLanguageTag(value).toLowerCase();
  return tag === "english" || tag === "en" || tag === "eng";
}

function sourceStillMatches({ preparedIdentity, liveTitle, liveBody }) {
  return currentSourceIdentity({ title: liveTitle || "", body: liveBody || "" }) === preparedIdentity;
}

function parseModelJson(raw) {
  const text = asText(raw).trim();
  if (!text) return null;
  const tryParse = (value) => {
    try {
      const parsed = JSON.parse(value);
      if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) return null;
      return parsed;
    } catch {
      return null;
    }
  };
  const unfenced = text.replace(/^```(?:json)?\s*/i, "").replace(/\s*```\s*$/, "").trim();
  const repaired = unfenced.replace(/\\'/g, "'");
  return tryParse(text) || tryParse(unfenced) || tryParse(repaired);
}

function absentTranslationFields({ sourceTitle = "", sourceBody = "", translatedTitle = "", translatedBody = "" }) {
  const missing = [];
  if (asText(sourceTitle).trim() && !asText(translatedTitle).trim()) missing.push("title");
  if (asText(sourceBody).trim() && !asText(translatedBody).trim()) missing.push("body");
  return missing;
}

function commentPrefilterReason(comment, issue = null) {
  if (issue?.pull_request) return "pull_request";
  const login = asText(comment?.user?.login);
  if (asText(comment?.user?.type) === "Bot" || login.toLowerCase().endsWith("[bot]") || login === ACTIONS_BOT) {
    return "bot";
  }
  if (asText(comment?.body).includes(LEDGER_MARKER)) return "control_ledger";
  return null;
}

function emptyLedgerView() {
  return {
    finished: {},
    lastTryMs: 0,
    tryMs: [],
    commitReady: false,
    language: null,
  };
}

function viewLedger(priorState, now) {
  const state = priorState && typeof priorState === "object" ? priorState : emptyLedgerView();
  return {
    finished: { ...(state.finished || {}) },
    lastTryMs: typeof state.lastTryMs === "number" ? state.lastTryMs : 0,
    tryMs: (state.tryMs || []).filter((ts) => now - ts < 3_600_000),
    commitReady: state.commitReady === true,
    language: state.language ?? null,
  };
}

function planTranslationAttempt({
  sourceTitle = "",
  sourceBody = "",
  sourceKey = ISSUE_SOURCE_KEY,
  authorText = null,
  priorState = null,
  now = Date.now(),
  policy = TRANSLATION_POLICY,
  prefilter = null,
} = {}) {
  if (prefilter) return { allow: false, skip: prefilter };
  if (authorCharCount({ title: sourceTitle, body: sourceBody, authorText }) < policy.minAuthorChars) {
    return { allow: false, skip: "too_short" };
  }
  const identity = currentSourceIdentity({ title: sourceTitle, body: sourceBody });
  const ledger = viewLedger(priorState, now);
  if (sourceIsFinished(ledger.finished, sourceKey, { title: sourceTitle, body: sourceBody })) {
    return { allow: false, skip: "already_done" };
  }
  if (ledger.lastTryMs && now - ledger.lastTryMs < policy.minGapMs) {
    return { allow: false, skip: "too_soon" };
  }
  if (ledger.tryMs.length >= policy.maxAttemptsPerHour) {
    return { allow: false, skip: "hourly_budget" };
  }
  return { allow: true, identity, sourceKey, tryMs: ledger.tryMs };
}

function decideIssueEligibility(args) {
  const markup = translationMarkupReason(args?.rawBody != null ? args.rawBody : args?.sourceBody);
  if (markup) return { ok: false, reason: markup };
  const plan = planTranslationAttempt(args);
  if (!plan.allow) return { ok: false, reason: plan.skip };
  return { ok: true, identity: plan.identity, sourceKey: plan.sourceKey, tryMs: plan.tryMs };
}

function decideCommentEligibility({
  comment,
  issue = null,
  priorState = null,
  now = Date.now(),
  policy = TRANSLATION_POLICY,
} = {}) {
  const skip = commentPrefilterReason(comment, issue);
  if (skip) return { ok: false, reason: skip };
  const markup = translationMarkupReason(comment?.body);
  if (markup) return { ok: false, reason: markup };
  const commentId = comment?.id;
  if (!Number.isSafeInteger(commentId) || commentId <= 0) return { ok: false, reason: "bad_comment_id" };
  const sourceBody = authorTextWithoutBotTranslation(comment.body || "");
  const sourceKey = `comment:${commentId}`;
  const plan = planTranslationAttempt({
    sourceTitle: sourceKey,
    sourceBody,
    sourceKey,
    authorText: sourceBody,
    priorState,
    now,
    policy,
  });
  if (!plan.allow) return { ok: false, reason: plan.skip };
  return {
    ok: true,
    identity: plan.identity,
    sourceKey: plan.sourceKey,
    sourceBody,
    sourceTitle: sourceKey,
    commentId,
  };
}

function languageForLedgerPersist({ detectedLanguage, commitReady } = {}) {
  if (commitReady !== true) return null;
  const tag = sanitizeLanguageTag(detectedLanguage || "");
  if (!tag || tag === "non-English" || tag.toLowerCase() === "unknown") return null;
  return tag;
}

module.exports = {
  MARKER: GENERATED_OPEN,
  END_MARKER: GENERATED_CLOSE,
  GENERATED_OPEN,
  GENERATED_CLOSE,
  LEDGER_MARKER,
  LEDGER_V2_OPEN,
  LEDGER_V3_OPEN,
  ACTIONS_BOT,
  ISSUE_BODY_MAX,
  ISSUE_SOURCE_KEY,
  ZERO_SOURCE_HASH,
  TRANSLATION_POLICY,
  RATE_DEFAULTS: TRANSLATION_POLICY,
  TRANSLATION_SUMMARY,
  CURRENT_SOURCE_PREFIX,
  HISTORICAL_V2_PREFIX,
  currentSourceIdentity,
  historicalV2Identity,
  sourceIsFinished,
  authorCharCount,
  authorTextWithoutBotTranslation,
  translationMarkupReason,
  planTranslationAttempt,
  splitTranslationBlock,
  stripTranslationBlock,
  translationBlockIsClosed,
  attachGeneratedBlock,
  composeTranslatedComment,
  parseModelJson,
  absentTranslationFields,
  commentPrefilterReason,
  decideIssueEligibility,
  decideCommentEligibility,
  sourceStillMatches,
  liveSourceStillMatchesPrepare: ({ preparedHash, preparedIdentity, liveTitle, liveBody }) =>
    sourceStillMatches({
      preparedIdentity: preparedIdentity || preparedHash,
      liveTitle,
      liveBody,
    }),
  languageForLedgerPersist,
  sanitizeLanguageTag,
  languageLooksEnglish,
};
