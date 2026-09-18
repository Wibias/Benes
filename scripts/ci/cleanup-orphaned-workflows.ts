import { isMainModule } from "../node-runtime.ts";

const GITHUB = "https://api.github.com";
const YAML_PREFIX = ".github/workflows/";
const DELETE_CAP = 500;
const PAGE = 100;

function ownerRepo(repository) {
  if (typeof repository !== "string") {
    throw new Error("GITHUB_REPOSITORY is required");
  }
  const parts = repository.split("/");
  if (parts.length !== 2 || !parts[0] || !parts[1]) {
    throw new Error(`GITHUB_REPOSITORY must be owner/repo, got ${JSON.stringify(repository)}`);
  }
  return { owner: parts[0], repo: parts[1] };
}

function encodeSegments(path) {
  return path.split("/").map(encodeURIComponent).join("/");
}

async function github({ token, fetchImpl, apiRoot }, method, pathname) {
  const response = await fetchImpl(`${apiRoot}${pathname}`, {
    method,
    headers: {
      Accept: "application/vnd.github+json",
      Authorization: `Bearer ${token}`,
      "X-GitHub-Api-Version": "2022-11-28",
    },
  });
  const text = await response.text();
  return { status: response.status, ok: response.ok, text };
}

function parseJson(pathname, status, text) {
  try {
    return text ? JSON.parse(text) : undefined;
  } catch {
    throw new Error(`GET ${pathname} -> HTTP ${status}: response was not JSON`);
  }
}

async function requireJson(ctx, pathname) {
  const { status, ok, text } = await github(ctx, "GET", pathname);
  if (!ok) {
    throw new Error(`GET ${pathname} -> HTTP ${status}${text.trim() ? `: ${text.trim()}` : ""}`);
  }
  return parseJson(pathname, status, text);
}

async function collectPages(ctx, pathname, field) {
  const items = [];
  for (let page = 1; ; page += 1) {
    const joiner = pathname.includes("?") ? "&" : "?";
    const payload = await requireJson(ctx, `${pathname}${joiner}per_page=${PAGE}&page=${page}`);
    const batch = payload?.[field];
    if (!Array.isArray(batch)) {
      throw new Error(`GET ${pathname} did not return ${field} as an array`);
    }
    items.push(...batch);
    if (batch.length < PAGE) return items;
  }
}

function otherHeads(runs, origin, defaultBranch) {
  const unique = new Map();
  for (const run of runs) {
    const fullName = run.head_repository?.full_name;
    const branch = run.head_branch;
    if (typeof fullName !== "string" || typeof branch !== "string" || !branch) continue;
    if (fullName === origin && branch === defaultBranch) continue;
    unique.set(JSON.stringify([fullName, branch]), { fullName, branch });
  }
  return [...unique.values()];
}

/**
 * Prefer histories that a remaining deletion budget can finish, so the Actions
 * sidebar loses as many stale workflow identities as possible when the cap hits.
 * Tie-break on workflow id so the order is stable across runs.
 */
export function allocateDeletionBudget(histories, limit) {
  if (!Number.isInteger(limit) || limit < 1) {
    throw new Error(`maxDeletions must be a positive integer, got ${limit}`);
  }
  const ranked = [...histories].sort((left, right) => {
    const byCount = left.runIds.length - right.runIds.length;
    if (byCount !== 0) return byCount;
    return left.workflowId - right.workflowId;
  });
  const picks = [];
  for (const history of ranked) {
    for (const runId of history.runIds) {
      if (picks.length >= limit) {
        return { picks, capped: true };
      }
      picks.push({ path: history.path, runId });
    }
  }
  return { picks, capped: false };
}

export async function removeStaleWorkflowRuns({
  token,
  repository,
  fetchImpl = fetch,
  apiRoot = GITHUB,
  maxDeletions = DELETE_CAP,
  log = console.log,
}) {
  if (!token) throw new Error("GITHUB_TOKEN is required");
  const { owner, repo } = ownerRepo(repository);
  const ctx = { token, fetchImpl, apiRoot };
  const repoPath = `/repos/${encodeURIComponent(owner)}/${encodeURIComponent(repo)}`;
  const origin = `${owner}/${repo}`;

  const repoInfo = await requireJson(ctx, repoPath);
  const defaultBranch = repoInfo.default_branch;
  if (!defaultBranch) throw new Error("repository JSON omitted default_branch");

  const listing = await requireJson(
    ctx,
    `${repoPath}/contents/.github/workflows?ref=${encodeURIComponent(defaultBranch)}`,
  );
  if (!Array.isArray(listing)) {
    throw new Error("default-branch .github/workflows was not a directory listing");
  }
  const liveYaml = new Set(
    listing
      .filter((entry) => entry?.type === "file" && typeof entry.path === "string")
      .map((entry) => entry.path),
  );

  const registered = await collectPages(ctx, `${repoPath}/actions/workflows`, "workflows");
  const stale = registered.filter((workflow) =>
    typeof workflow?.path === "string"
    && workflow.path.startsWith(YAML_PREFIX)
    && !liveYaml.has(workflow.path),
  );
  log(
    `Registry ${registered.length}; YAML on ${defaultBranch}=${liveYaml.size}; missing files=${stale.length}.`,
  );

  const accepted = [];
  for (const workflow of stale) {
    const runs = await collectPages(
      ctx,
      `${repoPath}/actions/workflows/${workflow.id}/runs`,
      "workflow_runs",
    );
    const busy = runs.filter((run) => run.status !== "completed");
    if (busy.length > 0) {
      log(`Skip ${workflow.path}: ${busy.length} run(s) not completed.`);
      continue;
    }

    let yamlElsewhere = false;
    for (const { fullName, branch } of otherHeads(runs, origin, defaultBranch)) {
      const bits = fullName.split("/");
      if (bits.length !== 2 || !bits[0] || !bits[1]) {
        throw new Error(`invalid head_repository.full_name: ${JSON.stringify(fullName)}`);
      }
      const probePath =
        `/repos/${encodeURIComponent(bits[0])}/${encodeURIComponent(bits[1])}`
        + `/contents/${encodeSegments(workflow.path)}?ref=${encodeURIComponent(branch)}`;
      const probe = await github(ctx, "GET", probePath);
      if (probe.status === 404) continue;
      if (!probe.ok) {
        throw new Error(
          `GET ${probePath} -> HTTP ${probe.status}${probe.text.trim() ? `: ${probe.text.trim()}` : ""}`,
        );
      }
      log(`Skip ${workflow.path}: YAML remains on ${fullName}:${branch}.`);
      yamlElsewhere = true;
      break;
    }
    if (yamlElsewhere) continue;

    accepted.push({
      path: workflow.path,
      workflowId: Number(workflow.id),
      runIds: runs.map((run) => run.id),
    });
  }

  const { picks, capped } = allocateDeletionBudget(accepted, maxDeletions);
  log(`Accepted ${accepted.length} stale workflow(s); deleting ${picks.length} run(s).`);

  let deleted = 0;
  const failures = [];
  for (const { path, runId } of picks) {
    const delPath = `${repoPath}/actions/runs/${runId}`;
    const result = await github(ctx, "DELETE", delPath);
    if (result.status === 204 || result.ok) {
      deleted += 1;
      continue;
    }
    if (result.status === 404) {
      log(`Run ${runId} for ${path} already gone; continuing.`);
      continue;
    }
    failures.push(
      `${path} run ${runId}: DELETE ${delPath} -> HTTP ${result.status}${result.text.trim() ? `: ${result.text.trim()}` : ""}`,
    );
  }

  if (capped) {
    log(`Hit the ${maxDeletions}-run cap; leftover histories wait for a later pass.`);
  }
  if (failures.length > 0) {
    throw new Error(`Failed to delete ${failures.length} workflow run(s):\n${failures.join("\n")}`);
  }

  const summary = {
    registered: registered.length,
    liveYaml: liveYaml.size,
    stale: stale.length,
    accepted: accepted.length,
    runCount: accepted.reduce((sum, history) => sum + history.runIds.length, 0),
    deleted,
    hitCap: capped,
  };
  log(`Finished: ${JSON.stringify(summary)}`);
  return summary;
}

if (isMainModule(import.meta.url)) {
  await removeStaleWorkflowRuns({
    token: process.env.GITHUB_TOKEN,
    repository: process.env.GITHUB_REPOSITORY,
  });
}
