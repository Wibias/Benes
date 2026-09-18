"use strict";

const { planManagedTypeLabels } = require("./pr-labeler.cjs");

const LABEL_COLORS = {
  enhancement: "0075ca",
  bug: "d73a4a",
  documentation: "0075ca",
  chore: "e4e669",
};

async function ensureTypeLabel(github, owner, repo, name, core) {
  try {
    await github.rest.issues.getLabel({ owner, repo, name });
    return;
  } catch (error) {
    if (error.status !== 404) throw error;
  }
  try {
    await github.rest.issues.createLabel({
      owner,
      repo,
      name,
      color: LABEL_COLORS[name] || "ededed",
    });
    core.info(`Created missing label "${name}"`);
  } catch (error) {
    if (error.status !== 422) throw error;
    core.info(`Label "${name}" was created concurrently; continuing.`);
  }
}

async function runPrLabeler({ github, context, core }) {
  const pullNumber = context.payload.pull_request.number;
  const { owner, repo } = context.repo;

  const { data: livePr } = await github.rest.pulls.get({
    owner,
    repo,
    pull_number: pullNumber,
  });
  const { data: currentLabels } = await github.rest.issues.listLabelsOnIssue({
    owner,
    repo,
    issue_number: pullNumber,
  });
  const events = await github.paginate(github.rest.issues.listEvents, {
    owner,
    repo,
    issue_number: pullNumber,
    per_page: 100,
  });
  const commits = await github.paginate(github.rest.pulls.listCommits, {
    owner,
    repo,
    pull_number: pullNumber,
    per_page: 100,
  });

  const plan = planManagedTypeLabels({
    title: livePr.title || context.payload.pull_request.title || "",
    currentLabels: currentLabels.map((label) => label.name),
    events,
    commitMessages: commits.map((commit) => commit.commit?.message || ""),
  });

  if (!plan.apply) {
    core.info(`Skipping type-label sync for PR #${pullNumber}: ${plan.reason}`);
    return;
  }

  await ensureTypeLabel(github, owner, repo, plan.type, core);

  for (const name of plan.remove) {
    await github.rest.issues.removeLabel({
      owner,
      repo,
      issue_number: pullNumber,
      name,
    });
    core.info(`Removed stale type label "${name}" from PR #${pullNumber}`);
  }

  if (plan.add) {
    await github.rest.issues.addLabels({
      owner,
      repo,
      issue_number: pullNumber,
      labels: [plan.add],
    });
    core.info(`Applied label "${plan.add}" to PR #${pullNumber}`);
  }
}

module.exports = { runPrLabeler };
