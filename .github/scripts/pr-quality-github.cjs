"use strict";

const CONVERT_DRAFT = `
  mutation($pullRequestId: ID!) {
    convertPullRequestToDraft(
      input: {
        pullRequestId: $pullRequestId
      }
    ) {
      pullRequest {
        id
        isDraft
      }
    }
  }
`;

const MARK_READY = `
  mutation($pullRequestId: ID!) {
    markPullRequestReadyForReview(
      input: {
        pullRequestId: $pullRequestId
      }
    ) {
      pullRequest {
        id
        isDraft
      }
    }
  }
`;

const REVIEW_THREADS = `
  query($owner: String!, $repo: String!, $number: Int!, $cursor: String) {
    repository(owner: $owner, name: $repo) {
      pullRequest(number: $number) {
        reviewThreads(first: 100, after: $cursor) {
          pageInfo {
            hasNextPage
            endCursor
          }
          nodes {
            isResolved
            comments(first: 1) {
              nodes {
                author { login }
              }
            }
          }
        }
      }
    }
  }
`;

async function convertDraft(github, pullRequestId) {
  await github.graphql(CONVERT_DRAFT, { pullRequestId });
}

async function markReady(github, pullRequestId) {
  await github.graphql(MARK_READY, { pullRequestId });
}

async function syncReviewReady({
  owner,
  repo,
  issue_number,
  shouldHave,
  hasLabel,
  label,
  addLabels,
  removeLabel,
  core,
  wrapInline,
}) {
  try {
    if (shouldHave && !hasLabel) {
      await addLabels({ owner, repo, issue_number, labels: [label] });
    } else if (!shouldHave && hasLabel) {
      await removeLabel({ owner, repo, issue_number, name: label });
    }
  } catch (error) {
    core.warning(
      `Could not ${shouldHave ? "add" : "remove"} the ${wrapInline(label)} label: ${error.message}`,
    );
  }
}

function keepHygieneFromPrior(body, existingBody, { readHygienePayload, HYGIENE_OPEN, HYGIENE_HTML, HYGIENE_CLOSE }) {
  const existingHygiene = readHygienePayload(existingBody);
  if (existingHygiene && !body.includes("pr-hygiene-block")) {
    return body.replace(
      /\s+$/,
      `\n\n## Hygiene\n\n${HYGIENE_OPEN}\n${HYGIENE_HTML}\n\n${existingHygiene}\n\n${HYGIENE_CLOSE}`,
    );
  }
  return body;
}

async function putIssueComment({ github, owner, repo, issue_number, comment_id, body }) {
  if (comment_id) {
    await github.rest.issues.updateComment({ owner, repo, comment_id, body });
    return { id: comment_id, body };
  }
  const created = await github.rest.issues.createComment({
    owner,
    repo,
    issue_number,
    body,
  });
  return { id: created.data.id, body };
}

async function dropStaleBotNotes({
  owner,
  repo,
  ids,
  gateCommentId,
  deleteComment,
  core,
}) {
  const leftover = ids.filter((id) => typeof id === "number" && id !== gateCommentId);
  if (leftover.length === 0) return;
  for (const id of leftover) {
    try {
      await deleteComment({ owner, repo, comment_id: id });
    } catch (error) {
      core.warning(`Could not delete legacy bot comment ${id}: ${error.message}`);
    }
  }
}

async function pageReviewThreads(github, { owner, repo, number, query = REVIEW_THREADS }) {
  const nodes = [];
  let cursor = null;
  let page = null;
  do {
    const threadPage = await github.graphql(query, {
      owner,
      repo,
      number,
      cursor,
    });
    page = threadPage?.repository?.pullRequest?.reviewThreads;
    nodes.push(...(page?.nodes ?? []));
    cursor = page?.pageInfo?.endCursor ?? null;
  } while (page?.pageInfo?.hasNextPage === true && cursor);
  return nodes;
}

async function pageReviews(github, { owner, repo, pull_number }) {
  return github.paginate(github.rest.pulls.listReviews, {
    owner,
    repo,
    pull_number,
    per_page: 100,
  });
}

module.exports = {
  CONVERT_DRAFT,
  MARK_READY,
  REVIEW_THREADS,
  convertDraft,
  markReady,
  syncReviewReady,
  keepHygieneFromPrior,
  putIssueComment,
  dropStaleBotNotes,
  pageReviewThreads,
  pageReviews,
};
