import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  allocateDeletionBudget,
  removeStaleWorkflowRuns,
} from "./cleanup-orphaned-workflows.ts";

function json(data, status = 200) {
  return new Response(status === 204 ? null : JSON.stringify(data), {
    status,
    headers: { "content-type": "application/json" },
  });
}

function mockGitHub(handlers) {
  const seen = [];
  const fetchImpl = async (url, init = {}) => {
    const parsed = new URL(url);
    const method = init.method ?? "GET";
    const key = `${method} ${parsed.pathname}${parsed.search}`;
    seen.push(key);
    const handler = handlers[key];
    if (!handler) throw new Error(`unexpected request ${key}`);
    return handler();
  };
  return { fetchImpl, seen };
}

const repo = "acme/loop";
const root = "/repos/acme/loop";

function catalog({
  branch = "dev",
  files = [{ type: "file", path: ".github/workflows/ship.yml" }],
  workflows,
  runs = {},
  deletes = {},
}) {
  const handlers = {
    [`GET ${root}`]: () => json({ default_branch: branch }),
    [`GET ${root}/contents/.github/workflows?ref=${branch}`]: () => json(files),
    [`GET ${root}/actions/workflows?per_page=100&page=1`]: () => json({ workflows }),
  };
  for (const [id, list] of Object.entries(runs)) {
    handlers[`GET ${root}/actions/workflows/${id}/runs?per_page=100&page=1`] = () =>
      json({ workflow_runs: list });
  }
  for (const [id, impl] of Object.entries(deletes)) {
    handlers[`DELETE ${root}/actions/runs/${id}`] = impl;
  }
  return handlers;
}

function doneRun(id) {
  return {
    id,
    status: "completed",
    head_branch: "dev",
    head_repository: { full_name: repo },
  };
}

describe("allocateDeletionBudget", () => {
  test("when the cap cannot finish both histories, the smaller one is taken first", () => {
    const bulky = {
      workflowId: 90,
      path: ".github/workflows/legacy-nightly.yml",
      runIds: [501, 502, 503],
    };
    const brief = {
      workflowId: 4,
      path: ".github/workflows/legacy-lint.yml",
      runIds: [9],
    };
    const { picks, capped } = allocateDeletionBudget([bulky, brief], 2);
    assert.equal(capped, true);
    assert.deepEqual(picks.map((item) => item.runId), [9, 501]);
    assert.equal(picks[0].path, ".github/workflows/legacy-lint.yml");
  });

  test("equal-size histories sort by workflow id", () => {
    const later = { workflowId: 22, path: ".github/workflows/b.yml", runIds: [2] };
    const earlier = { workflowId: 11, path: ".github/workflows/a.yml", runIds: [1] };
    const { picks } = allocateDeletionBudget([later, earlier], 10);
    assert.deepEqual(picks.map((item) => item.runId), [1, 2]);
  });
});

describe("removeStaleWorkflowRuns safety", () => {
  test("rejects a missing token or a malformed repository", async () => {
    await assert.rejects(
      () => removeStaleWorkflowRuns({ token: "", repository: repo }),
      /GITHUB_TOKEN/,
    );
    await assert.rejects(
      () => removeStaleWorkflowRuns({ token: "t", repository: "not-a-repo" }),
      /owner\/repo/,
    );
  });

  test("discovery stays read-only until the deletion plan is complete", async () => {
    const { fetchImpl, seen } = mockGitHub(catalog({
      workflows: [
        { id: 1, path: ".github/workflows/ship.yml" },
        { id: 8, path: ".github/workflows/leftover.yml" },
      ],
      runs: { 8: [doneRun(44)] },
      deletes: { 44: () => json(null, 204) },
    }));
    await removeStaleWorkflowRuns({ token: "t", repository: repo, fetchImpl, log() {} });
    const firstDelete = seen.findIndex((key) => key.startsWith("DELETE "));
    const lastGet = seen.findLastIndex((key) => key.startsWith("GET "));
    assert.ok(firstDelete > lastGet);
  });

  test("does not delete a history that still has an in-progress run", async () => {
    const { fetchImpl, seen } = mockGitHub(catalog({
      workflows: [{ id: 8, path: ".github/workflows/leftover.yml" }],
      runs: {
        8: [{
          id: 44,
          status: "in_progress",
          head_branch: "topic",
          head_repository: { full_name: repo },
        }],
      },
    }));
    const summary = await removeStaleWorkflowRuns({
      token: "t",
      repository: repo,
      fetchImpl,
      log() {},
    });
    assert.equal(summary.deleted, 0);
    assert.equal(summary.accepted, 0);
    assert.equal(seen.some((key) => key.startsWith("DELETE ")), false);
  });

  test("keeps history when the YAML still exists on another live head", async () => {
    const handlers = catalog({
      files: [],
      workflows: [{ id: 8, path: ".github/workflows/leftover.yml" }],
      runs: {
        8: [{
          id: 44,
          status: "completed",
          head_branch: "topic",
          head_repository: { full_name: "mina/loop" },
        }],
      },
    });
    handlers["GET /repos/mina/loop/contents/.github/workflows/leftover.yml?ref=topic"] = () =>
      json({ path: ".github/workflows/leftover.yml" });
    const { fetchImpl, seen } = mockGitHub(handlers);
    const summary = await removeStaleWorkflowRuns({
      token: "t",
      repository: repo,
      fetchImpl,
      log() {},
    });
    assert.equal(summary.deleted, 0);
    assert.equal(seen.some((key) => key.startsWith("DELETE ")), false);
  });

  test("treats a 404 delete as already gone", async () => {
    const { fetchImpl } = mockGitHub(catalog({
      files: [],
      workflows: [{ id: 8, path: ".github/workflows/leftover.yml" }],
      runs: { 8: [doneRun(44)] },
      deletes: { 44: () => new Response("missing", { status: 404 }) },
    }));
    const summary = await removeStaleWorkflowRuns({
      token: "t",
      repository: repo,
      fetchImpl,
      log() {},
    });
    assert.equal(summary.deleted, 0);
  });

  test("under a tight cap, deletes the smaller approved history first", async () => {
    const deleted = [];
    const { fetchImpl } = mockGitHub(catalog({
      files: [],
      workflows: [
        { id: 2, path: ".github/workflows/legacy-nightly.yml" },
        { id: 1, path: ".github/workflows/legacy-lint.yml" },
      ],
      runs: {
        2: [doneRun(100), doneRun(101)],
        1: [doneRun(9)],
      },
      deletes: {
        9: () => {
          deleted.push(9);
          return json(null, 204);
        },
        100: () => {
          deleted.push(100);
          return json(null, 204);
        },
      },
    }));
    const summary = await removeStaleWorkflowRuns({
      token: "t",
      repository: repo,
      fetchImpl,
      maxDeletions: 2,
      log() {},
    });
    assert.deepEqual(deleted, [9, 100]);
    assert.equal(summary.deleted, 2);
    assert.equal(summary.hitCap, true);
  });

  test("fails visibly on an unexpected delete status", async () => {
    const { fetchImpl } = mockGitHub(catalog({
      files: [],
      workflows: [{ id: 8, path: ".github/workflows/leftover.yml" }],
      runs: { 8: [doneRun(44)] },
      deletes: { 44: () => new Response("nope", { status: 500 }) },
    }));
    await assert.rejects(
      () => removeStaleWorkflowRuns({ token: "t", repository: repo, fetchImpl, log() {} }),
      /Failed to delete 1/,
    );
  });
});
