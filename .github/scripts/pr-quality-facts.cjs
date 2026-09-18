"use strict";

const {
  ACTIONS_BOT,
  LEGACY_ENFORCER_HTML,
  GATE_HTML,
  CODE_RABBIT_BOT,
  CODE_RABBIT_APP_ID,
  CODE_RABBIT_STATUS,
} = require("./pr-quality-frozen.cjs");
const { trustedPolicyRef } = require("./pr-quality.cjs");

function parsePullNumber(raw) {
  const text = raw ?? "";
  const n = /^\d+$/.test(text) ? Number.parseInt(text, 10) : Number.NaN;
  if (!Number.isSafeInteger(n) || n < 1) return null;
  return n;
}

function acceptedWriteEvent(eventName) {
  return ["pull_request_target", "status"].includes(eventName);
}

function botNote(comments, includesText) {
  return comments.find(
    (comment) =>
      comment.user?.login === ACTIONS_BOT && comment.body?.includes(includesText),
  );
}

function legacyEnforcerNote(comments, legacyMarker) {
  return comments.find(
    (comment) =>
      comment.user?.login === ACTIONS_BOT &&
      (comment.body?.includes(LEGACY_ENFORCER_HTML) || comment.body?.includes(legacyMarker)),
  );
}

function pickGateRecord({
  gateComment,
  legacyEnforcerComment,
  legacyReadinessComment,
  decodeGateRecord,
  decodeEnforcerRecord,
  decodeReadinessRecord,
  blendLegacyRecords,
  warn,
}) {
  const stored = decodeGateRecord(gateComment?.body, warn);
  const enforcer = decodeEnforcerRecord(legacyEnforcerComment?.body, warn);
  const readiness = decodeReadinessRecord(legacyReadinessComment?.body, warn);
  const blended = blendLegacyRecords(enforcer, readiness);
  return stored ?? blended;
}

function readTrustedMaintainers({ fs, path, cwd, fileName, readMaintainerLogins, core }) {
  try {
    const text = fs.readFileSync(path.join(cwd, fileName), "utf8");
    return readMaintainerLogins(text);
  } catch (error) {
    core.warning(`Could not read ${fileName}: ${error.message}`);
    return [];
  }
}

async function loadPrBundle({ github, owner, repo, pull_number }) {
  const { data: pr } = await github.rest.pulls.get({ owner, repo, pull_number });
  const comments = await github.paginate(github.rest.issues.listComments, {
    owner,
    repo,
    issue_number: pull_number,
    per_page: 100,
  });
  return { pr, comments };
}

async function lookupPushLevel({ github, owner, repo, username, core }) {
  try {
    const { data } = await github.rest.repos.getCollaboratorPermissionLevel({
      owner,
      repo,
      username,
    });
    return { authorPermission: data.permission, permissionLookupFailed: false };
  } catch (error) {
    core.warning(`Could not look up collaborator permission: ${error.message}`);
    return { authorPermission: null, permissionLookupFailed: true };
  }
}

function stackedOnOpenParent({ openPrs, pull_number, pr, owner, repo }) {
  const baseOwner = pr.base.repo?.owner?.login ?? owner;
  const baseName = pr.base.repo?.name ?? repo;
  return openPrs.some(
    (other) =>
      other.number !== pull_number &&
      other.head?.ref === pr.base.ref &&
      (other.base?.repo?.owner?.login ?? owner) === baseOwner &&
      (other.base?.repo?.name ?? repo) === baseName,
  );
}

async function stackedOnParent({ github, owner, repo, pull_number, pr, core, listOpenPrs }) {
  try {
    const openPrs = await listOpenPrs({ owner, repo, state: "open", per_page: 100 });
    const stacked = stackedOnOpenParent({ openPrs, pull_number, pr, owner, repo });
    if (stacked) {
      core.info(
        `Base ${pr.base.ref} matches an open PR head; treating as stacked (skip wrong_base).`,
      );
    }
    return stacked;
  } catch (error) {
    core.warning(`Could not list open PRs for stacked-base check: ${error.message}`);
    return false;
  }
}

async function compareAncestry({ github, owner, repo, pr, core }) {
  const headSha = pr.head.sha;
  try {
    const { data: mainCompare } = await github.rest.repos.compareCommitsWithBasehead({
      owner,
      repo,
      basehead: `main...${headSha}`,
    });
    const { data: baseCompare } = await github.rest.repos.compareCommitsWithBasehead({
      owner,
      repo,
      basehead: `${pr.base.ref}...${headSha}`,
    });
    return {
      behindMain: mainCompare.behind_by,
      aheadMain: mainCompare.ahead_by,
      behindBase: baseCompare.behind_by,
      ancestryLookupFailed: false,
    };
  } catch (error) {
    core.warning(`Could not compare commits for ancestry check: ${error.message}`);
    return {
      behindMain: 0,
      behindBase: 0,
      aheadMain: 0,
      ancestryLookupFailed: true,
    };
  }
}

async function snapshotChangedFiles({
  github,
  owner,
  repo,
  pull_number,
  core,
  getPull,
  listFiles,
  incompleteFileList,
}) {
  const changedFiles = [];
  const changedFilePaths = [];
  let filesTruncated = true;
  for (let attempt = 0; attempt < 2; attempt += 1) {
    const { data: fileSnapshot } = await getPull({ owner, repo, pull_number });
    const headShaForFiles = fileSnapshot.head?.sha ?? "";
    const listedFiles = await github.paginate(listFiles, {
      owner,
      repo,
      pull_number,
      per_page: 100,
    });
    const { data: fileVerify } = await getPull({ owner, repo, pull_number });
    const headMatches = fileVerify.head?.sha === headShaForFiles;
    if (!headMatches && attempt === 0) {
      core.info("PR head moved while listing changed files; retrying once.");
      continue;
    }
    if (!headMatches) {
      core.warning(
        "PR head moved during changed-file snapshot; treating file list as truncated.",
      );
    }
    changedFiles.length = 0;
    changedFiles.push(...listedFiles);
    changedFilePaths.length = 0;
    changedFilePaths.push(...listedFiles.map((file) => file.filename).filter(Boolean));
    filesTruncated = incompleteFileList(
      fileSnapshot.changed_files,
      listedFiles.length,
      headMatches,
    );
    break;
  }
  return { changedFiles, changedFilePaths, filesTruncated };
}

function eventHead({ payload, eventName, liveHeadSha }) {
  return payload.pull_request?.head?.sha ?? (eventName === "status" ? "" : liveHeadSha);
}

async function latestWaiverActor({ github, owner, repo, pull_number, label, core }) {
  try {
    const issueEvents = await github.paginate(github.rest.issues.listEvents, {
      owner,
      repo,
      issue_number: pull_number,
      per_page: 100,
    });
    const waiverEvents = issueEvents
      .filter(
        (event) =>
          (event.event === "labeled" || event.event === "unlabeled") &&
          event.label?.name === label,
      )
      .sort((left, right) => {
        const leftTime = Date.parse(left.created_at ?? "") || 0;
        const rightTime = Date.parse(right.created_at ?? "") || 0;
        if (leftTime !== rightTime) return leftTime - rightTime;
        return Number(left.id ?? 0) - Number(right.id ?? 0);
      });
    const latest = waiverEvents.at(-1);
    if (latest?.event === "labeled") return latest.actor?.login ?? null;
    return null;
  } catch (error) {
    core.warning(`Could not resolve ${label} label provenance: ${error.message}`);
    return null;
  }
}

function labelWaiverHolds({ present, actorLogin, maintainerLogins }) {
  return (
    present &&
    typeof actorLogin === "string" &&
    maintainerLogins.has(actorLogin.toLowerCase())
  );
}

function publishResolvedPull(core, pull) {
  if (!Number.isInteger(pull?.number)) return false;
  core.setOutput("pull-number", String(pull.number));
  core.setOutput("trusted-ref", trustedPolicyRef(pull.base?.ref));
  return true;
}

async function resolveTrustedPullNumber({ github, context, core }) {
  const { owner, repo } = context.repo;
  let pull = context.payload.pull_request ?? null;

  if (context.eventName === "status") {
    const sender = context.payload.sender;
    const trustedCodeRabbit =
      context.payload.context === CODE_RABBIT_STATUS &&
      context.payload.state === "success" &&
      sender?.login === CODE_RABBIT_BOT &&
      sender?.id === CODE_RABBIT_APP_ID;
    if (!trustedCodeRabbit) {
      core.info("Status producer is not the CodeRabbit GitHub App; skipping.");
      return;
    }

    const statusSha = context.payload.sha;
    let candidates = [];
    try {
      const associatedPrs = await github.paginate(
        github.rest.repos.listPullRequestsAssociatedWithCommit,
        { owner, repo, commit_sha: statusSha, per_page: 100 },
      );
      candidates = associatedPrs.filter(
        (candidate) =>
          candidate.state === "open" && candidate.head?.sha === statusSha,
      );
    } catch (error) {
      core.warning(
        `Could not list PRs associated with commit ${statusSha}: ${error.message}`,
      );
    }

    if (candidates.length !== 1) {
      const priorCount = candidates.length;
      try {
        const openPrs = await github.paginate(github.rest.pulls.list, {
          owner,
          repo,
          state: "open",
          per_page: 100,
        });
        candidates = openPrs.filter(
          (pr) => pr.state === "open" && pr.head?.sha === statusSha,
        );
        core.info(
          `Associated-index fallback: ${priorCount} index match(es), ${candidates.length} open PR(s) match head ${statusSha}.`,
        );
      } catch (error) {
        core.warning(
          `Could not list open PRs for head-${statusSha} fallback: ${error.message}`,
        );
      }
    }

    if (candidates.length !== 1) {
      core.info(
        `CodeRabbit status ${statusSha} maps to ${candidates.length} open current-head PRs; skipping ambiguous/stale revalidation.`,
      );
      return;
    }
    pull = candidates[0];
  }

  publishResolvedPull(core, pull);
}

module.exports = {
  parsePullNumber,
  acceptedWriteEvent,
  botNote,
  legacyEnforcerNote,
  pickGateRecord,
  readTrustedMaintainers,
  loadPrBundle,
  lookupPushLevel,
  stackedOnOpenParent,
  stackedOnParent,
  compareAncestry,
  snapshotChangedFiles,
  eventHead,
  latestWaiverActor,
  labelWaiverHolds,
  publishResolvedPull,
  resolveTrustedPullNumber,
  GATE_HTML,
};
