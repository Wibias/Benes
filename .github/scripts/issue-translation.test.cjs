"use strict";

const { describe, it } = require("node:test");
const assert = require("node:assert/strict");
const {
  MARKER,
  END_MARKER,
  LEDGER_V2_OPEN,
  LEDGER_V3_OPEN,
  TRANSLATION_SUMMARY,
  CURRENT_SOURCE_PREFIX,
  HISTORICAL_V2_PREFIX,
  authorTextWithoutBotTranslation,
  stripTranslationBlock,
  attachGeneratedBlock,
  translationMarkupReason,
  parseModelJson,
  planTranslationAttempt,
  decideCommentEligibility,
  commentPrefilterReason,
  currentSourceIdentity,
  historicalV2Identity,
  TRANSLATION_POLICY,
  translationBlockIsClosed,
} = require("./issue-translation.cjs");
const {
  foldAttemptIntoLedger,
  renderLedgerComment,
  recordFromCommentBody,
  readAuthoritativeLedger,
} = require("./issue-translation-ledger.cjs");

describe("generated block ownership", () => {
  it("keeps author markdown and only rewrites the generated block", () => {
    const author = "POST /v1/responses hung after the first combo target.";
    const body = attachGeneratedBlock(author, "Die Kombination blieb nach dem ersten Ziel stehen.");
    assert.equal(body.includes(MARKER), true);
    assert.equal(body.includes(END_MARKER), true);
    assert.equal(body.includes(TRANSLATION_SUMMARY), true);
    assert.equal(body.includes("Translated Message"), false);
    assert.equal(stripTranslationBlock(body), author);
    assert.equal(authorTextWithoutBotTranslation(body), author);
  });

  it("does not treat unmarked author markdown as generated", () => {
    const author = "Combo failover should hop before visible output.";
    assert.equal(authorTextWithoutBotTranslation(author), author);
    assert.equal(stripTranslationBlock(author), author);
  });

  it("leaves an unclosed historical bot marker untouched", () => {
    const body = `Author stays.\n${MARKER}\nthis never closed`;
    assert.equal(translationBlockIsClosed(body), false);
    assert.equal(translationMarkupReason(body), "malformed_translation_markup");
    assert.equal(stripTranslationBlock(body), body);
    assert.equal(authorTextWithoutBotTranslation(body), body);
  });

  it("refuses to write after a malformed opener so a later parse cannot eat author text", () => {
    const before = "Author text before the marker.";
    const after = "Author text after the unclosed marker.";
    const body = `${before}\n${MARKER}\nunclosed historical content\n${after}`;
    assert.equal(translationMarkupReason(body), "malformed_translation_markup");
    assert.throws(
      () => attachGeneratedBlock(body, "Failover hops before visible output."),
      (error) => error && error.code === "malformed_translation_markup",
    );
    assert.equal(body.includes(before), true);
    assert.equal(body.includes(after), true);
    const secondPass = authorTextWithoutBotTranslation(body);
    assert.equal(secondPass, body);
    assert.equal(secondPass.includes(after), true);
    assert.equal(stripTranslationBlock(body).includes(after), true);
  });

  it("refuses a stray closer and nested or multiple bot ranges", () => {
    const stray = `Author before.\n${END_MARKER}\nAuthor after.`;
    assert.equal(translationMarkupReason(stray), "malformed_translation_markup");
    assert.throws(() => attachGeneratedBlock(stray, "English."), (error) => error.code === "malformed_translation_markup");
    assert.equal(stripTranslationBlock(stray), stray);

    const nested = `Author.\n${MARKER}\ninner\n${MARKER}\nmore\n${END_MARKER}\n${END_MARKER}`;
    assert.equal(translationMarkupReason(nested), "malformed_translation_markup");

    const valid = attachGeneratedBlock("First author block.", "One.");
    const doubled = `${valid}\n${MARKER}\nsecond\n${END_MARKER}`;
    assert.equal(translationMarkupReason(doubled), "malformed_translation_markup");
  });

  it("preserves author text on both sides of a closed bot block", () => {
    const body = [
      "before the bot wrote",
      MARKER,
      "<details>",
      `<summary>${TRANSLATION_SUMMARY}</summary>`,
      "English text",
      "</details>",
      END_MARKER,
      "after the bot wrote",
    ].join("\n");
    assert.equal(stripTranslationBlock(body), "before the bot wrote\n\nafter the bot wrote");
  });

  it("is idempotent across repeated writes", () => {
    const author = "Failover hops until the first visible stream event.";
    const first = attachGeneratedBlock(author, "Hop until output is visible.");
    const second = attachGeneratedBlock(first, "Hop until output is visible.");
    assert.equal(first, second);
    assert.equal(stripTranslationBlock(second), author);
  });
});

describe("translation-result validation", () => {
  it("accepts JSON or fenced JSON and fails closed on garbage", () => {
    assert.equal(parseModelJson('{"requires_translation":false}').requires_translation, false);
    assert.equal(
      parseModelJson("```json\n{\"requires_translation\":true,\"translated_body\":\"ok\"}\n```").translated_body,
      "ok",
    );
    assert.equal(parseModelJson("not json"), null);
  });
});

describe("Benes translation planner", () => {
  const sourceBody = "Codex on 127.0.0.1:23100 saw combo failover skip the second target.";

  it("declares the 20-character / 60-second / 10-per-hour budget as current policy", () => {
    assert.equal(TRANSLATION_POLICY.minAuthorChars, 20);
    assert.equal(TRANSLATION_POLICY.minGapMs, 60_000);
    assert.equal(TRANSLATION_POLICY.maxAttemptsPerHour, 10);
    assert.equal(TRANSLATION_POLICY.issueAuthor, "title_and_body");
    assert.equal(TRANSLATION_POLICY.commentAuthor, "body_only");
  });

  it("skips PRs, bots, and control-ledger comments", () => {
    assert.equal(commentPrefilterReason({ body: "hi" }, { pull_request: {} }), "pull_request");
    assert.equal(
      commentPrefilterReason({ user: { login: "github-actions[bot]" }, body: "hi" }),
      "bot",
    );
    assert.equal(
      commentPrefilterReason({
        user: { login: "Wibias" },
        body: "<!-- benes-issue-inline-translator-control -->",
      }),
      "control_ledger",
    );
  });

  it("skips short author source", () => {
    assert.equal(
      planTranslationAttempt({ sourceTitle: "x", sourceBody: "short" }).skip,
      "too_short",
    );
  });

  it("treats issue title plus body as author content at the exact threshold", () => {
    const title = "abcd";
    const body19 = "123456789012345"; // 4 + 1 + 15 = 20
    const body18 = "12345678901234"; // 19
    assert.equal(planTranslationAttempt({ sourceTitle: title, sourceBody: body18 }).skip, "too_short");
    assert.equal(planTranslationAttempt({ sourceTitle: title, sourceBody: body19 }).allow, true);
  });

  it("does not count a comment id toward minAuthorChars", () => {
    const longId = 12345678901;
    const short = decideCommentEligibility({
      comment: { id: longId, user: { login: "Wibias" }, body: "x" },
    });
    assert.equal(short.ok, false);
    assert.equal(short.reason, "too_short");

    const nineteen = "1234567890123456789";
    assert.equal(nineteen.length, 19);
    assert.equal(
      decideCommentEligibility({
        comment: { id: longId, user: { login: "Wibias" }, body: nineteen },
      }).reason,
      "too_short",
    );

    const twenty = "12345678901234567890";
    assert.equal(twenty.length, 20);
    const ok = decideCommentEligibility({
      comment: { id: longId, user: { login: "Wibias" }, body: twenty },
    });
    assert.equal(ok.ok, true);
    assert.equal(ok.sourceKey, `comment:${longId}`);
    assert.ok(ok.identity.startsWith(CURRENT_SOURCE_PREFIX));
    assert.equal(ok.identity.length, CURRENT_SOURCE_PREFIX.length + 64);
  });

  it("skips a source already translated at the current identity", () => {
    const identity = currentSourceIdentity({ title: "combo", body: sourceBody });
    assert.equal(
      planTranslationAttempt({
        sourceTitle: "combo",
        sourceBody,
        priorState: { finished: { issue: identity }, lastTryMs: 1, tryMs: [], commitReady: true },
      }).skip,
      "already_done",
    );
  });

  it("still treats a historical v2 identity as already done", () => {
    const v2 = historicalV2Identity({ title: "combo", body: sourceBody });
    assert.equal(
      planTranslationAttempt({
        sourceTitle: "combo",
        sourceBody,
        priorState: {
          finished: { issue: `${HISTORICAL_V2_PREFIX}${v2}` },
          lastTryMs: 1,
          tryMs: [],
          commitReady: true,
        },
      }).skip,
      "already_done",
    );
  });

  it("enforces the gap and hourly budget", () => {
    assert.equal(
      planTranslationAttempt({
        sourceTitle: "combo",
        sourceBody,
        now: 1_000,
        priorState: { finished: {}, lastTryMs: 900, tryMs: [], commitReady: false },
      }).skip,
      "too_soon",
    );
    const tryMs = Array.from({ length: 10 }, (_, i) => 10_000 + i);
    assert.equal(
      planTranslationAttempt({
        sourceTitle: "combo",
        sourceBody,
        now: 70_000,
        priorState: { finished: {}, lastTryMs: 1, tryMs, commitReady: false },
      }).skip,
      "hourly_budget",
    );
  });

  it("allows a new long source with empty ledger", () => {
    const plan = planTranslationAttempt({ sourceTitle: "combo", sourceBody });
    assert.equal(plan.allow, true);
    assert.equal(plan.identity, currentSourceIdentity({ title: "combo", body: sourceBody }));
    assert.ok(plan.identity.startsWith(CURRENT_SOURCE_PREFIX));
  });
});

describe("translation ledger v3 writes and v2 read compatibility", () => {
  const sourceBody = "Codex on 127.0.0.1:23100 saw combo failover skip the second target.";
  const identity = currentSourceIdentity({ title: "combo", body: sourceBody });

  it("writes native v3 state and never emits v2 fields", () => {
    const state = foldAttemptIntoLedger({
      priorState: null,
      attempt: { identity, sourceKey: "issue", commitReady: true, language: "German" },
      now: 50_000,
    });
    const body = renderLedgerComment(state);
    assert.equal(body.includes(LEDGER_V3_OPEN), true);
    assert.equal(body.includes(LEDGER_V2_OPEN), false);
    assert.equal(body.includes("sourceHash"), false);
    assert.equal(body.includes("sourceHashes"), false);
    assert.equal(body.includes("attemptedAt"), false);
    assert.equal(body.includes("requiresTranslation"), false);
    assert.equal(body.includes("detectedLanguage"), false);
    assert.equal(body.includes("Automated translation bookkeeping"), false);
    const roundTrip = recordFromCommentBody(body, 50_000);
    assert.equal(roundTrip.finished.issue, identity);
    assert.equal(roundTrip.lastTryMs, 50_000);
    assert.equal(roundTrip.commitReady, true);
    assert.equal(roundTrip.language, "German");
  });

  it("decodes a historical v2 control comment into the normalized view", () => {
    const v2hash = historicalV2Identity({ title: "combo", body: sourceBody });
    const v2record = {
      v: 2,
      sourceHash: v2hash,
      sourceHashes: { issue: v2hash },
      attemptedAt: 40_000,
      recent: [40_000],
      requiresTranslation: true,
      detectedLanguage: "German",
    };
    const encoded = Buffer.from(JSON.stringify(v2record), "utf8").toString("base64url");
    const comment = {
      id: 9,
      user: { login: "github-actions[bot]" },
      body: [
        "<!-- benes-issue-inline-translator-control -->",
        `${LEDGER_V2_OPEN}${encoded} -->`,
        "",
        "<sub>Automated translation bookkeeping — detected language: German.</sub>",
      ].join("\n"),
    };
    const decoded = readAuthoritativeLedger([comment], 8, 40_000);
    assert.equal(decoded.finished.issue, `${HISTORICAL_V2_PREFIX}${v2hash}`);
    assert.equal(decoded.lastTryMs, 40_000);
    assert.equal(
      planTranslationAttempt({
        sourceTitle: "combo",
        sourceBody,
        priorState: decoded,
      }).skip,
      "already_done",
    );
  });
});
