"use strict";

const evaluate = require("./issue-intake-evaluate.cjs");
const areas = require("./issue-intake-areas.cjs");
const bot = require("./issue-intake-bot.cjs");
const { splitTranslationBlock } = require("./issue-translation.cjs");

const TRUSTED = new Set(["OWNER", "MEMBER", "COLLABORATOR"]);
const MAINTAINER = new Set(["admin", "maintain", "write"]);
const DEFAULT_BRANCH_OK = /^[A-Za-z0-9._/-]+$/;

function inspectDispatchTrust(eventName, ref, defaultBranch) {
  if (eventName !== "workflow_dispatch") return { ok: true, detail: null };
  if (typeof defaultBranch !== "string" || !DEFAULT_BRANCH_OK.test(defaultBranch)) {
    return {
      ok: false,
      detail: "workflow_dispatch refused: repository default branch is missing or invalid.",
    };
  }
  if (typeof ref !== "string" || ref.trim() === "") {
    return {
      ok: false,
      detail: "workflow_dispatch refused: selected ref is missing.",
    };
  }
  const allowed = `refs/heads/${defaultBranch}`;
  if (ref !== allowed) {
    return {
      ok: false,
      detail: `workflow_dispatch refused: selected ref is not ${allowed}.`,
    };
  }
  return { ok: true, detail: null };
}

function inspectDispatchIssue(issue, issueNumber, eventName) {
  if (eventName !== "workflow_dispatch") return { ok: true, detail: null };
  if (issue?.pull_request) {
    return {
      ok: false,
      detail: `workflow_dispatch refused: issue_number ${issueNumber} is a pull request.`,
    };
  }
  return { ok: true, detail: null };
}

function resolveIssueNumber(context, core) {
  if (context.eventName !== "workflow_dispatch") return context.payload.issue.number;
  const raw = context.payload.inputs.issue_number;
  const parsed = Number(raw);
  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    core.setFailed(`workflow_dispatch refused: issue_number ${raw} is not a positive integer.`);
    return null;
  }
  return parsed;
}

function decideTransition({
  issue,
  botRecord,
  trustedAuthor,
  maintainerActor,
  eventAction,
  verdict,
}) {
  const passing = Boolean(verdict.valid || verdict.softPass);
  const open = issue.state === "open";

  if (passing) {
    if (!open && bot.botOwnsClose(botRecord, issue)) {
      return { type: "reopen", comment: "reopen" };
    }
    if (!open) {
      return { type: "idle", comment: maintainerActor ? "human-owned-close" : null };
    }
    if (botRecord.kind === "bot-close") {
      return { type: "idle", comment: "reopen" };
    }
    return { type: "idle", comment: null };
  }

  if (trustedAuthor) {
    return { type: "idle", comment: null, skip: "trusted-author" };
  }
  if (eventAction === "reopened" && maintainerActor) {
    return { type: "override", comment: "override" };
  }
  if (botRecord.kind === "override") {
    return { type: "idle", comment: null, skip: "maintainer-override" };
  }
  if (botRecord.kind === "unreadable" && !open) {
    return { type: "idle", comment: null, skip: "unreadable-marker" };
  }
  return { type: "close", comment: "close", stateReason: "not_planned" };
}

async function actorIsMaintainer(github, { owner, repo, username }) {
  try {
    const { data } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner,
      repo,
      username,
    });
    return MAINTAINER.has(data.permission);
  } catch {
    return false;
  }
}

async function ensureAreaLabel({ github, core, owner, repo, name }) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name });
    return true;
  } catch (error) {
    if (error.status !== 404) {
      core.warning(`Failed to look up label "${name}": ${error.message || error}`);
      return false;
    }
  }
  const meta = areas.AREA_LABELS[name];
  if (!meta) return false;
  try {
    await github.rest.issues.createLabel({
      owner,
      repo,
      name,
      color: meta.color,
      description: meta.description,
    });
    core.info(`Created label "${name}".`);
    return true;
  } catch (error) {
    if (error.status === 422) {
      core.info(`Label "${name}" appeared concurrently; continuing.`);
      return true;
    }
    core.warning(`Failed to create label "${name}": ${error.message || error}`);
    return false;
  }
}

async function applyKindLabel({ github, core, owner, repo, issueNumber, labels, resolvedKind }) {
  const name = evaluate.labelForKind(resolvedKind);
  if (!name || labels.includes(name)) return;
  try {
    await github.rest.issues.addLabels({
      owner,
      repo,
      issue_number: issueNumber,
      labels: [name],
    });
    labels.push(name);
    core.info(`Applied kind label "${name}".`);
  } catch (error) {
    core.warning(`Failed to apply kind label "${name}": ${error.message || error}`);
  }
}

async function applyAreaLabels({
  github,
  core,
  owner,
  repo,
  issueNumber,
  labels,
  title,
  sourceBody,
  heuristicBody,
  translationText,
}) {
  const detected = areas.detectAreaLabels({
    title,
    body: sourceBody,
    heuristicBody,
    translationText,
    labels,
  });
  const toAdd = [];
  for (const name of detected.filter((candidate) => !labels.includes(candidate))) {
    if (await ensureAreaLabel({ github, core, owner, repo, name })) toAdd.push(name);
  }
  if (toAdd.length === 0) return;
  try {
    await github.rest.issues.addLabels({
      owner,
      repo,
      issue_number: issueNumber,
      labels: toAdd,
    });
    labels.push(...toAdd);
    core.info(`#${issueNumber}: applied area label(s): ${toAdd.join(", ")}`);
  } catch (error) {
    core.warning(`Failed to apply area labels: ${error.message || error}`);
  }
}

async function applyAdditiveLabels(args) {
  await applyKindLabel(args);
  await applyAreaLabels(args);
}

function resolveKind({ title, sourceBody, issueLabels, botState, core }) {
  const activeBotKind = botState?.active ? botState.kind || null : null;
  const detected = evaluate.recognizeKind({
    title,
    body: sourceBody,
    labels: issueLabels,
    storedKind: activeBotKind,
  });
  const fromLabels = evaluate.kindFromLabels(issueLabels);
  if (!detected && !fromLabels) {
    core.info("Issue is not a structured form; requiring a template for non-trusted authors.");
  }
  return {
    activeBotKind,
    resolvedKind: detected ?? fromLabels,
  };
}

function commentBodyFor(kind, state, result, softPass, resolvedKind) {
  if (kind === "reopen") return bot.reopenCommentBody(state, { softPass });
  if (kind === "human-owned-close") return bot.humanOwnedCloseCommentBody(state);
  if (kind === "override") return bot.maintainerOverrideCommentBody(state);
  if (kind === "close") {
    return bot.closeCommentBody(state, {
      missingTemplate: !resolvedKind,
      reasons: result.reasons,
      guidance: result.guidance,
    });
  }
  return null;
}

async function applyTransition({ github, core, session, issue, botRecord, transition, result, resolvedKind, softPass }) {
  const { owner, repo, issueNumber } = session;
  let nextState = botRecord.state;

  if (transition.type === "override") {
    nextState = bot.maintainerOverrideState(botRecord.state, resolvedKind);
  } else if (transition.type === "reopen") {
    try {
      await github.rest.issues.update({
        owner,
        repo,
        issue_number: issueNumber,
        state: "open",
      });
    } catch (error) {
      core.warning(`Could not reopen issue #${issueNumber}: ${error.message || error}`);
      return;
    }
    nextState = bot.inactiveState(botRecord.state);
  } else if (transition.type === "close") {
    let closedIssue;
    try {
      await github.rest.issues.update({
        owner,
        repo,
        issue_number: issueNumber,
        state: "closed",
        state_reason: transition.stateReason,
      });
      const fetched = await github.rest.issues.get({
        owner,
        repo,
        issue_number: issueNumber,
      });
      closedIssue = fetched.data;
    } catch (error) {
      core.warning(`Could not close issue #${issueNumber}: ${error.message || error}`);
      return;
    }
    nextState = bot.activeCloseState({
      kind: resolvedKind,
      closedAt: closedIssue.closed_at,
      stateReason: closedIssue.state_reason,
    });
  } else if (transition.comment === "reopen" || transition.comment === "human-owned-close") {
    nextState = bot.inactiveState(botRecord.state);
  }

  const body = commentBodyFor(transition.comment, nextState, result, softPass, resolvedKind);
  if (!body) return;
  try {
    await bot.upsertBotComment({
      ...session,
      body,
    });
  } catch (error) {
    core.warning(`Could not write issue-quality comment on #${issueNumber}: ${error.message || error}`);
  }
}

async function runIssueQualityGate({ github, context, core }) {
  const issueNumber = resolveIssueNumber(context, core);
  if (issueNumber == null) return;

  const dispatch = inspectDispatchTrust(
    context.eventName,
    context.ref,
    context.payload.repository?.default_branch,
  );
  if (!dispatch.ok) {
    core.setFailed(dispatch.detail);
    return;
  }

  const { owner, repo } = context.repo;
  const { data: issue } = await github.rest.issues.get({
    owner,
    repo,
    issue_number: issueNumber,
  });

  const asIssue = inspectDispatchIssue(issue, issueNumber, context.eventName);
  if (!asIssue.ok) {
    core.setFailed(asIssue.detail);
    return;
  }

  const translation = splitTranslationBlock(issue.body || "");
  const sourceBody = translation.sourceBody;
  const issueLabels = areas.issueLabelNames(issue);
  const { botComment, botState, botRecord } = await bot.loadBotComment({
    github,
    owner,
    repo,
    issueNumber,
  });
  const { activeBotKind, resolvedKind } = resolveKind({
    title: issue.title,
    sourceBody,
    issueLabels,
    botState,
    core,
  });

  const maintainer = await actorIsMaintainer(github, {
    owner,
    repo,
    username: context.actor,
  });
  const session = { github, owner, repo, issueNumber, botComment };

  await applyAdditiveLabels({
    github,
    core,
    owner,
    repo,
    issueNumber,
    labels: issueLabels,
    resolvedKind,
    title: issue.title,
    sourceBody,
    heuristicBody: sourceBody,
    translationText: areas.translationPlainText(translation.block),
  });

  const result = evaluate.evaluateIssue({
    title: issue.title,
    body: sourceBody,
    labels: issueLabels,
    storedKind: activeBotKind || resolvedKind || null,
  });
  const eventAction = context.eventName === "issues" ? context.payload.action : "";
  const transition = decideTransition({
    issue: {
      state: issue.state,
      closed_at: issue.closed_at,
      state_reason: issue.state_reason,
      closed_by: issue.closed_by?.login ?? null,
    },
    botRecord,
    trustedAuthor: TRUSTED.has(issue.author_association),
    maintainerActor: maintainer,
    eventAction,
    verdict: result,
  });

  if (transition.skip === "trusted-author") {
    core.info("Issue author is a trusted collaborator; skipping enforcement.");
    return;
  }
  if (transition.skip === "maintainer-override") {
    core.info("Automatic close is disabled after a maintainer reopen.");
    return;
  }
  if (transition.skip === "unreadable-marker") {
    core.info("Stored bot marker is unreadable; leaving issue state unchanged.");
    return;
  }

  await applyTransition({
    github,
    core,
    session,
    issue,
    botRecord,
    transition,
    result,
    resolvedKind,
    softPass: result.softPass,
  });
}

async function backfillOpenIssueAreaLabels({ github, context, core }) {
  const dispatch = inspectDispatchTrust(
    context.eventName,
    context.ref,
    context.payload.repository?.default_branch,
  );
  if (!dispatch.ok) {
    core.setFailed(dispatch.detail);
    return;
  }

  const { owner, repo } = context.repo;
  const openIssues = await github.paginate(github.rest.issues.listForRepo, {
    owner,
    repo,
    state: "open",
    per_page: 100,
  });
  const issues = openIssues.filter((item) => !item.pull_request);
  core.info(`Backfilling area labels on ${issues.length} open issue(s).`);

  let updated = 0;
  for (const issue of issues) {
    const labels = areas.issueLabelNames(issue);
    const before = labels.length;
    const translation = splitTranslationBlock(issue.body || "");
    await applyAreaLabels({
      github,
      core,
      owner,
      repo,
      issueNumber: issue.number,
      labels,
      title: issue.title,
      sourceBody: translation.sourceBody,
      heuristicBody: translation.sourceBody,
      translationText: areas.translationPlainText(translation.block),
    });
    if (labels.length > before) updated += 1;
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  core.info(`Backfill complete: updated ${updated} / ${issues.length} open issue(s).`);
}

module.exports = {
  runIssueQualityGate,
  backfillOpenIssueAreaLabels,
  applyAdditiveLabels,
  applyAreaLabels,
  applyKindLabel,
  ensureAreaLabel,
  inspectDispatchTrust,
  inspectDispatchIssue,
  decideTransition,
};
