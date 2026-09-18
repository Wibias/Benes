"use strict";

const BOT_LOGIN = "github-actions[bot]";
const BOT_MARKER = "<!-- benes-issue-quality-bot -->";
const STATE_PATTERN = /<!-- benes-issue-quality-state:([\s\S]*?) -->/;
const CONTRIBUTING_URL = "https://github.com/Wibias/Benes/contributing/";

function stateTag(state) {
  return `<!-- benes-issue-quality-state:${JSON.stringify(state)} -->`;
}

function parseBotState(commentBody) {
  const match = String(commentBody || "").match(STATE_PATTERN);
  if (!match) return null;
  try {
    return JSON.parse(match[1]);
  } catch {
    return null;
  }
}

function findBotComment(comments) {
  return (
    (comments || []).find(
      (comment) => comment.user?.login === BOT_LOGIN && comment.body?.includes(BOT_MARKER),
    ) || null
  );
}

function composeComment(state, lines) {
  return [BOT_MARKER, stateTag(state), "", ...lines].join("\n");
}

function readBotRecord(commentBody) {
  if (!commentBody || !String(commentBody).includes(BOT_MARKER)) {
    return { kind: "absent", state: null };
  }
  const state = parseBotState(commentBody);
  if (!state || typeof state !== "object") {
    return { kind: "unreadable", state: null };
  }
  if (state.maintainerOverride) return { kind: "override", state };
  if (state.active) return { kind: "bot-close", state };
  return { kind: "inactive", state };
}

function inactiveState(botState) {
  return { ...botState, active: false };
}

function activeCloseState({ kind, closedAt, stateReason }) {
  return {
    version: 2,
    active: true,
    kind,
    closedAt,
    stateReason: stateReason || "not_planned",
  };
}

function maintainerOverrideState(botState, kind) {
  return {
    version: 2,
    active: false,
    kind: botState?.kind || kind || null,
    closedAt: botState?.closedAt || null,
    stateReason: botState?.stateReason || null,
    maintainerOverride: true,
  };
}

function botOwnsClose(record, issue) {
  if (record.kind === "unreadable") return false;
  if (record.kind !== "bot-close") return false;
  const state = record.state;
  if (issue.state !== "closed") return false;
  if (state.closedAt && issue.closed_at && state.closedAt !== issue.closed_at) return false;
  if (state.stateReason && issue.state_reason && state.stateReason !== issue.state_reason) return false;
  const closer = issue.closed_by;
  return !closer || closer === BOT_LOGIN;
}

function passingNote(softPass) {
  return softPass
    ? "The form still uses unusual headings, but it now has enough structured evidence to keep open."
    : "The required evidence is now present.";
}

function reopenCommentBody(state, { softPass }) {
  return composeComment(state, [
    "### Reopened — evidence is now sufficient",
    "",
    passingNote(softPass),
  ]);
}

function humanOwnedCloseCommentBody(state) {
  return composeComment(state, [
    "### Quality check is satisfied",
    "",
    "The report now has the required evidence. It stays closed because a person, not this check, closed it.",
  ]);
}

function maintainerOverrideCommentBody(state) {
  return composeComment(state, [
    "### Maintainer override",
    "",
    "A maintainer reopened this issue. Automatic close will not run again on this issue.",
  ]);
}

function bullets(items) {
  return (items || []).map((item) => `- ${item}`).join("\n");
}

function closeCommentBody(state, { missingTemplate, reasons, guidance }) {
  const heading = missingTemplate
    ? "### Closed — use an issue form"
    : "### Closed — missing evidence";
  const intro = missingTemplate
    ? "This issue was closed because it was not opened with a recognized form (Bug report, Feature request, Documentation, or Provider compatibility). Freeform and API-opened issues cannot skip that requirement."
    : "This issue was closed because the structured report is missing evidence needed to act on it.";

  return composeComment(state, [
    heading,
    "",
    intro,
    "",
    bullets(reasons),
    "",
    "Edit the issue to add:",
    "",
    bullets(guidance),
    "",
    `See the [Contributing guide](${CONTRIBUTING_URL}) for details.`,
    "",
    "A later edit that supplies the evidence reopens the issue, unless a maintainer has taken over its state.",
  ]);
}

async function loadBotComment({ github, owner, repo, issueNumber }) {
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: issueNumber,
    per_page: 100,
  });
  const botComment = findBotComment(comments);
  return {
    botComment,
    botState: botComment ? parseBotState(botComment.body) : null,
    botRecord: readBotRecord(botComment?.body),
  };
}

async function upsertBotComment({ github, owner, repo, issueNumber, botComment, body }) {
  if (botComment) {
    if (botComment.body === body) return { wrote: false, id: botComment.id };
    await github.rest.issues.updateComment({
      owner,
      repo,
      comment_id: botComment.id,
      body,
    });
    return { wrote: true, id: botComment.id };
  }
  const created = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body,
  });
  return { wrote: true, id: created.data.id };
}

module.exports = {
  BOT_LOGIN,
  BOT_MARKER,
  STATE_PATTERN,
  CONTRIBUTING_URL,
  stateTag,
  parseBotState,
  findBotComment,
  composeComment,
  readBotRecord,
  inactiveState,
  activeCloseState,
  maintainerOverrideState,
  botOwnsClose,
  reopenCommentBody,
  humanOwnedCloseCommentBody,
  maintainerOverrideCommentBody,
  closeCommentBody,
  loadBotComment,
  upsertBotComment,
};
