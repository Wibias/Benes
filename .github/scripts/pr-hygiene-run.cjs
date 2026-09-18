"use strict";

const path = require("node:path");

function script(name) {
  return path.join(process.cwd(), ".github", "scripts", name);
}

async function ensureLabel(github, { owner, repo, name, color, description }) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name });
    return;
  } catch (error) {
    if (error.status !== 404) throw error;
  }
  try {
    await github.rest.issues.createLabel({ owner, repo, name, color, description });
  } catch (error) {
    if (error.status !== 422) throw error;
  }
}

async function lookupPushPermission(github, { owner, repo, username, core }) {
  try {
    const { data } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner,
      repo,
      username,
    });
    return { permission: data.permission, failed: false };
  } catch (error) {
    core.warning(`Could not look up collaborator permission: ${error.message}`);
    return { permission: null, failed: true };
  }
}

async function syncBlockedLabel(github, { owner, repo, issueNumber, labels, blockedLabel, blocked }) {
  if (blocked && !labels.has(blockedLabel)) {
    await github.rest.issues.addLabels({
      owner,
      repo,
      issue_number: issueNumber,
      labels: [blockedLabel],
    });
    return;
  }
  if (!blocked && labels.has(blockedLabel)) {
    await github.rest.issues.removeLabel({
      owner,
      repo,
      issue_number: issueNumber,
      name: blockedLabel,
    });
  }
}

async function upsertHygieneComment(github, {
  owner,
  repo,
  issueNumber,
  body,
  hygieneMarker,
  gateMarker,
  botLogin,
  writeHygienePayload,
}) {
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: issueNumber,
    per_page: 100,
  });
  const gateComment = comments.find(
    (comment) => comment.user?.login === botLogin && comment.body?.includes(gateMarker),
  );
  if (gateComment) {
    const hygieneLines = body
      .split("\n")
      .map((line) => line.trim())
      .filter((line) => line !== "" && line !== hygieneMarker);
    await github.rest.issues.updateComment({
      owner,
      repo,
      comment_id: gateComment.id,
      body: writeHygienePayload(gateComment.body, hygieneLines),
    });
    return;
  }
  const existing = comments.find(
    (comment) => comment.user?.login === botLogin && comment.body?.includes(hygieneMarker),
  );
  if (existing) {
    await github.rest.issues.updateComment({
      owner,
      repo,
      comment_id: existing.id,
      body,
    });
    return;
  }
  await github.rest.issues.createComment({
    owner,
    repo,
    issue_number: issueNumber,
    body,
  });
}

async function runPrHygieneGate({ github, context, core }) {
  const { evaluateHygiene, HYGIENE_HINTS } = require(script("pr-hygiene.cjs"));
  const { loadGuiTestContentsAtHead, readFileFromContents } = require(script("pr-hygiene-head-files.cjs"));
  const { hasRepoPush } = require(script("pr-quality.cjs"));
  const { GATE_HTML, HYGIENE_HTML, writeHygienePayload } = require(script("pr-quality-reconcile.cjs"));
  const {
    ACTIONS_BOT,
    HYGIENE_BLOCKED_LABEL,
    TEST_EXCEPTION_LABEL,
    SUPPRESSION_LABEL,
    GENERATED_LABEL,
    DEPENDENCY_LABEL,
    SPONSOR_LABEL,
    HEAD_SCOPED_EXCEPTION_LABELS,
  } = require(script("pr-quality-frozen.cjs"));

  const { owner, repo } = context.repo;
  const pullNumber = context.payload.pull_request.number;
  const labelsToEnsure = [
    [HYGIENE_BLOCKED_LABEL, "b60205", "Deterministic PR hygiene checks failed"],
    [TEST_EXCEPTION_LABEL, "5319e7", "Maintainer approved a non-automated regression-test exception"],
    [SUPPRESSION_LABEL, "5319e7", "Maintainer approved a new type or lint suppression"],
    [GENERATED_LABEL, "5319e7", "Maintainer approved committed generated output"],
    [DEPENDENCY_LABEL, "5319e7", "Maintainer approved exceptional dependency or lockfile handling"],
    [SPONSOR_LABEL, "5319e7", "Maintainer sponsors this change to an auth, workflow, release, or dependency surface"],
  ];
  for (const [name, color, description] of labelsToEnsure) {
    await ensureLabel(github, { owner, repo, name, color, description });
  }

  const { data: pr } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: pullNumber,
  });
  const files = await github.paginate(github.rest.pulls.listFiles, {
    owner,
    repo,
    pull_number: pullNumber,
    per_page: 100,
  });
  const labels = new Set(pr.labels.map((label) => label.name));

  if (context.payload.action === "synchronize") {
    for (const name of HEAD_SCOPED_EXCEPTION_LABELS) {
      if (!labels.has(name)) continue;
      await github.rest.issues.removeLabel({
        owner,
        repo,
        issue_number: pullNumber,
        name,
      });
      labels.delete(name);
    }
  }

  const permission = await lookupPushPermission(github, {
    owner,
    repo,
    username: pr.user.login,
    core,
  });
  const guiTestContents = await loadGuiTestContentsAtHead({
    github,
    owner,
    repo,
    files,
    warn: (message) => core.warning(message),
  });
  const failures = evaluateHygiene({
    files,
    labels: [...labels],
    authorHasPushPermission: !permission.failed && hasRepoPush(permission.permission),
    readFile: readFileFromContents(guiTestContents),
  });

  const blocked = failures.length > 0;
  await syncBlockedLabel(github, {
    owner,
    repo,
    issueNumber: pullNumber,
    labels,
    blockedLabel: HYGIENE_BLOCKED_LABEL,
    blocked,
  });

  const body = blocked
    ? [
        HYGIENE_HTML,
        "",
        "⚠️ **Deterministic hygiene checks failed.**",
        "",
        ...failures.map((failure) => {
          const paths = failure.paths?.length
            ? ` Paths: ${failure.paths.map((filePath) => `\`${filePath}\``).join(", ")}.`
            : "";
          return `- **${failure.code}** — ${HYGIENE_HINTS[failure.code] ?? failure.code}${paths}`;
        }),
      ].join("\n")
    : `${HYGIENE_HTML}\n\n✅ **Deterministic PR hygiene checks passed.**`;

  await upsertHygieneComment(github, {
    owner,
    repo,
    issueNumber: pullNumber,
    body,
    hygieneMarker: HYGIENE_HTML,
    gateMarker: GATE_HTML,
    botLogin: ACTIONS_BOT,
    writeHygienePayload,
  });

  if (blocked) {
    core.setFailed(`PR hygiene failed: ${failures.map((failure) => failure.code).join(", ")}`);
  }
}

module.exports = { runPrHygieneGate };
