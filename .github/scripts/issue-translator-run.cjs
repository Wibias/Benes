"use strict";

const { completeJson: requestJsonCompletion } = require("./issue-ai.cjs");
const {
  stripTranslationBlock,
  attachGeneratedBlock,
  composeTranslatedComment,
  parseModelJson,
  absentTranslationFields,
  decideIssueEligibility,
  decideCommentEligibility,
  sourceStillMatches,
  currentSourceIdentity,
  languageForLedgerPersist,
} = require("./issue-translation.cjs");
const { commitLedgerAttempt, readAuthoritativeLedger } = require("./issue-translation-ledger.cjs");

function attemptFrom(decision, fields) {
  return {
    identity: decision.identity,
    sourceKey: decision.sourceKey,
    commitReady: fields.commitReady === true,
    language: fields.language ?? null,
  };
}

async function persistLedger(github, owner, repo, issue_number, comments, decision, fields) {
  await commitLedgerAttempt({
    github,
    owner,
    repo,
    issue_number,
    comments,
    priorState: readAuthoritativeLedger(comments),
    attempt: attemptFrom(decision, fields),
  });
}

async function runIssueTranslation({ github, context, core }) {
  const token = process.env.BENES_ISSUE_AI_TOKEN || "";
  if (!token) {
    core.info("BENES_ISSUE_AI_TOKEN is not set; skipping translation.");
    return;
  }

  if (context.eventName === "issue_comment") {
    await translateComment({ github, context, core, token });
    return;
  }

  const { owner, repo } = context.repo;
  const issue_number =
    context.eventName === "workflow_dispatch"
      ? Number(context.payload.inputs.issue_number)
      : context.payload.issue.number;
  if (!Number.isSafeInteger(issue_number) || issue_number <= 0) {
    core.setFailed(`Invalid issue number: ${issue_number}`);
    return;
  }

  const { data: issue } = await github.rest.issues.get({ owner, repo, issue_number });
  if (issue.pull_request) {
    core.info(`#${issue_number} is a pull request; skipping translation.`);
    return;
  }

  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number,
    per_page: 100,
  });
  const sourceTitle = String(issue.title || "");
  const rawBody = issue.body || "";
  const sourceBody = stripTranslationBlock(rawBody);
  const decision = decideIssueEligibility({
    sourceTitle,
    sourceBody,
    rawBody,
    priorState: readAuthoritativeLedger(comments),
  });
  if (!decision.ok) {
    core.info(`Skip translation: ${decision.reason}`);
    return;
  }

  const completion = await requestJsonCompletion({
    token,
    baseUrl: process.env.BENES_ISSUE_AI_BASE_URL,
    model: process.env.BENES_ISSUE_AI_MODEL,
    system:
      "Return JSON only with requires_translation (boolean), detected_language, translated_title, translated_body. If the issue is already English, set requires_translation to false.",
    user: JSON.stringify({ title: sourceTitle, body: sourceBody }),
  });
  if (!completion.ok) {
    await persistLedger(github, owner, repo, issue_number, comments, decision, {
      commitReady: false,
      language: null,
    });
    core.info(`Translation model skipped: ${completion.reason}`);
    return;
  }

  const parsed = parseModelJson(completion.text);
  if (!parsed || (parsed.requires_translation !== true && parsed.requires_translation !== false)) {
    await persistLedger(github, owner, repo, issue_number, comments, decision, {
      commitReady: false,
      language: null,
    });
    core.info("Translation model returned invalid JSON.");
    return;
  }

  if (parsed.requires_translation === false) {
    await persistLedger(github, owner, repo, issue_number, comments, decision, {
      commitReady: true,
      language: languageForLedgerPersist({
        detectedLanguage: parsed.detected_language || "English",
        commitReady: true,
      }),
    });
    return;
  }

  const missing = absentTranslationFields({
    sourceTitle,
    sourceBody,
    translatedTitle: parsed.translated_title,
    translatedBody: parsed.translated_body,
  });
  if (missing.length) {
    await persistLedger(github, owner, repo, issue_number, comments, decision, {
      commitReady: false,
      language: null,
    });
    core.info(`Translation missing fields: ${missing.join(", ")}`);
    return;
  }

  const { data: latest } = await github.rest.issues.get({ owner, repo, issue_number });
  const liveTitle = String(latest.title || "");
  const liveBody = stripTranslationBlock(latest.body || "");
  if (!sourceStillMatches({
    preparedIdentity: currentSourceIdentity({ title: sourceTitle, body: sourceBody }),
    liveTitle,
    liveBody,
  })) {
    core.info("Issue changed after prepare; skipping apply.");
    return;
  }

  await github.rest.issues.update({
    owner,
    repo,
    issue_number,
    body: attachGeneratedBlock(liveBody, parsed.translated_body),
  });
  await persistLedger(github, owner, repo, issue_number, comments, decision, {
    commitReady: true,
    language: languageForLedgerPersist({
      detectedLanguage: parsed.detected_language,
      commitReady: true,
    }),
  });
}

async function translateComment({ github, context, core, token }) {
  const { owner, repo } = context.repo;
  const comment = context.payload.comment;
  const issue = context.payload.issue;
  if (!comment || !issue) return;

  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: issue.number,
    per_page: 100,
  });
  const decision = decideCommentEligibility({
    comment,
    issue,
    priorState: readAuthoritativeLedger(comments),
  });
  if (!decision.ok) {
    core.info(`Skip comment translation: ${decision.reason}`);
    return;
  }

  const completion = await requestJsonCompletion({
    token,
    baseUrl: process.env.BENES_ISSUE_AI_BASE_URL,
    model: process.env.BENES_ISSUE_AI_MODEL,
    system:
      "Return JSON only with requires_translation (boolean), detected_language, translated_body. If the comment is already English, set requires_translation to false.",
    user: JSON.stringify({ body: decision.sourceBody }),
  });
  if (!completion.ok) {
    await persistLedger(github, owner, repo, issue.number, comments, decision, {
      commitReady: false,
      language: null,
    });
    core.info(`Comment translation skipped: ${completion.reason}`);
    return;
  }

  const parsed = parseModelJson(completion.text);
  if (!parsed || parsed.requires_translation !== true) {
    const commitReady = parsed?.requires_translation === false;
    await persistLedger(github, owner, repo, issue.number, comments, decision, {
      commitReady,
      language: languageForLedgerPersist({
        detectedLanguage: parsed?.detected_language,
        commitReady,
      }),
    });
    return;
  }

  const missing = absentTranslationFields({
    sourceTitle: "",
    sourceBody: decision.sourceBody,
    translatedTitle: "",
    translatedBody: parsed.translated_body,
  });
  if (missing.length) {
    await persistLedger(github, owner, repo, issue.number, comments, decision, {
      commitReady: false,
      language: null,
    });
    return;
  }

  const { data: live } = await github.rest.issues.getComment({
    owner,
    repo,
    comment_id: decision.commentId,
  });
  const liveBody = stripTranslationBlock(live.body || "");
  if (!sourceStillMatches({
    preparedIdentity: decision.identity,
    liveTitle: decision.sourceTitle,
    liveBody,
  })) {
    core.info("Comment changed after prepare; skipping apply.");
    return;
  }

  await github.rest.issues.updateComment({
    owner,
    repo,
    comment_id: decision.commentId,
    body: composeTranslatedComment(liveBody, parsed.translated_body, parsed.detected_language),
  });
  await persistLedger(github, owner, repo, issue.number, comments, decision, {
    commitReady: true,
    language: languageForLedgerPersist({
      detectedLanguage: parsed.detected_language,
      commitReady: true,
    }),
  });
}

module.exports = {
  runIssueTranslation,
  executeIssueTranslationJob: runIssueTranslation,
};
