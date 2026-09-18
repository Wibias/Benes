import assert from "node:assert/strict";
import { describe, test } from "node:test";
import {
  ancestorFromExitCode,
  classifyNpmIdentity,
  executePlan,
  runCut,
  runPublish,
  waitForPublishedIdentity,
} from "./execute.ts";
import { resolveIdentity } from "./identity.ts";
import { buildNotes } from "./notes.ts";
import type { PublicationPlan } from "./plan.ts";
import { ReleaseError } from "./semver.ts";
import type { ReleaseWorld } from "./world.ts";

const SHA = "0123456789abcdef0123456789abcdef01234567";
const identity = resolveIdentity({
  packageName: "benes",
  version: "4.2.0",
  sourceSha: SHA,
  branch: "main",
});

function notes() {
  return buildNotes({
    version: "4.2.0",
    packageName: "benes",
    distTag: "latest",
    repository: "testhost/app",
    tags: ["v4.1.0"],
    commits: [{ sha: SHA, subject: "feat: board", body: "", pulls: [] }],
  });
}

function plan(mutations: PublicationPlan["intent"] extends infer T ? T : never): PublicationPlan {
  return {
    identity,
    repository: "testhost/app",
    notes: notes(),
    intent: mutations,
    ciPushUrl: "https://example.test/ci",
    serviceLifecycleRequired: false,
    serviceLifecycleUrl: null,
  };
}

function matchingRelease() {
  return { tag: "v4.2.0", draft: false, prerelease: false };
}

function fakeWorld(calls: string[], patch: Partial<ReleaseWorld> = {}): ReleaseWorld {
  let createdRelease = false;
  const world: ReleaseWorld = {
    packageName: async () => "benes",
    packageVersion: async () => "4.2.0",
    currentBranch: async () => "main",
    currentSha: async () => SHA,
    workingTreeDirty: async () => false,
    remoteHead: async () => SHA,
    listTags: async () => ["v4.1.0"],
    isAncestor: async () => true,
    firstParentLog: async () => [{ sha: SHA, subject: "feat: board", body: "" }],
    associatedPulls: async () => [],
    generatedNotes: async () => "",
    changedFiles: async () => ["package.json"],
    npmHasVersion: async () => false,
    npmGitHead: async () => null,
    npmDistTags: async () => ({ latest: "4.1.0" }),
    waitUntilPublishedIdentity: async () => {
      calls.push("wait-npm-identity");
    },
    remoteTagSha: async () => null,
    readGithubRelease: async () => (createdRelease ? matchingRelease() : null),
    ciPushUrl: async () => "https://example.test/ci",
    serviceLifecycleUrl: async () => null,
    waitForWorkflow: async (_sha, workflow) => {
      calls.push(`wait:${workflow}`);
      return "https://example.test/run";
    },
    runLocalChecks: async () => {
      calls.push("local-checks");
    },
    bumpPackageVersion: async () => {
      calls.push("bump");
    },
    commitAndPush: async () => {
      calls.push("commit-push");
      return SHA;
    },
    dispatchRelease: async (input) => {
      calls.push(`dispatch:dryRun=${input.dryRun}`);
    },
    watchDispatchedRelease: async () => {
      calls.push("watch");
    },
    preparePackage: async () => {
      calls.push("prepare");
    },
    packDryRun: async () => {
      calls.push("pack");
    },
    stageNpm: async () => {
      calls.push("stage-npm");
    },
    createTag: async () => {
      calls.push("create-tag");
    },
    createGithubRelease: async () => {
      createdRelease = true;
      calls.push("create-github-release");
    },
    log: () => {},
    ...patch,
  };
  return world;
}

describe("executePlan", () => {
  test("dry-run prepares the package and never mutates public metadata", async () => {
    const calls: string[] = [];
    const world = fakeWorld(calls, {
      stageNpm: async () => {
        calls.push("stage-npm");
        throw new Error("stageNpm must not run in dry-run");
      },
      createTag: async () => {
        throw new Error("createTag must not run in dry-run");
      },
      createGithubRelease: async () => {
        throw new Error("createGithubRelease must not run in dry-run");
      },
    });
    const result = await executePlan(
      plan({
        kind: "fresh",
        mutations: ["stage-npm", "create-tag", "create-github-release"],
      }),
      world,
      true,
    );
    assert.equal(result.outcome, "dry-run");
    assert.deepEqual(calls, ["prepare", "pack"]);
  });

  test("a successful stage stops before Git tag and GitHub Release", async () => {
    const calls: string[] = [];
    const result = await executePlan(
      plan({
        kind: "fresh",
        mutations: ["stage-npm", "create-tag", "create-github-release"],
      }),
      fakeWorld(calls),
      false,
    );
    assert.equal(result.outcome, "staged-awaiting-approval");
    assert.deepEqual(calls, ["stage-npm"]);
  });

  test("stage failure never creates GitHub metadata", async () => {
    const calls: string[] = [];
    await assert.rejects(
      () =>
        executePlan(
          plan({
            kind: "fresh",
            mutations: ["stage-npm", "create-tag", "create-github-release"],
          }),
          fakeWorld(calls, {
            stageNpm: async () => {
              calls.push("stage-npm");
              throw new Error("registry down");
            },
          }),
          false,
        ),
      /registry down/,
    );
    assert.deepEqual(calls, ["stage-npm"]);
  });

  test("an already-existing matching GitHub Release after create is success", async () => {
    const calls: string[] = [];
    const result = await executePlan(
      plan({ kind: "recover", mutations: ["create-github-release"] }),
      fakeWorld(calls, {
        createGithubRelease: async () => {
          calls.push("create-github-release");
        },
        readGithubRelease: async () => matchingRelease(),
      }),
      false,
    );
    assert.equal(result.outcome, "recover");
    assert.ok(calls.includes("create-github-release"));
  });

  test("an already-existing mismatched GitHub Release after create is failure", async () => {
    await assert.rejects(
      () =>
        executePlan(
          plan({ kind: "recover", mutations: ["create-github-release"] }),
          fakeWorld([], {
            createGithubRelease: async () => {},
            readGithubRelease: async () => ({ tag: "v4.2.0", draft: true, prerelease: false }),
          }),
          false,
        ),
      (error: unknown) =>
        error instanceof ReleaseError && error.code === "github_release_mismatch",
    );
  });
});

describe("classifyNpmIdentity", () => {
  test("polls when the version is visible but gitHead is not yet available", async () => {
    assert.equal(
      classifyNpmIdentity({ version: "4.2.0", gitHead: null }, { version: "4.2.0", sourceSha: SHA }),
      "pending-gitHead",
    );
    const probes = [
      { version: "4.2.0", gitHead: null },
      { version: "4.2.0", gitHead: SHA },
    ];
    const sleeps: string[] = [];
    await waitForPublishedIdentity({
      version: "4.2.0",
      sourceSha: SHA,
      attempts: 3,
      sleep: async () => {
        sleeps.push("poll");
      },
      read: async () => probes.shift() ?? { version: "4.2.0", gitHead: SHA },
    });
    assert.deepEqual(sleeps, ["poll"]);
  });

  test("treats a visible version with a foreign gitHead as a hard mismatch", () => {
    assert.equal(
      classifyNpmIdentity(
        { version: "4.2.0", gitHead: "ffffffffffffffffffffffffffffffffffffffff" },
        { version: "4.2.0", sourceSha: SHA },
      ),
      "foreign",
    );
  });
});

describe("ancestorFromExitCode", () => {
  test("distinguishes not-ancestor from a git failure", () => {
    assert.equal(ancestorFromExitCode(0), true);
    assert.equal(ancestorFromExitCode(1), false);
    assert.throws(() => ancestorFromExitCode(128), ReleaseError);
  });
});

describe("executePlan recovery", () => {
  test("rerun after staged npm approval creates only remaining GitHub metadata", async () => {
    const calls: string[] = [];
    const result = await executePlan(
      plan({ kind: "recover", mutations: ["create-tag", "create-github-release"] }),
      fakeWorld(calls, {
        stageNpm: async () => {
          throw new Error("stageNpm must not rerun after approval");
        },
      }),
      false,
    );
    assert.equal(result.outcome, "recover");
    assert.deepEqual(calls, ["create-tag", "create-github-release"]);
  });
});

describe("runPublish", () => {
  test("builds a complete plan from history and stops on dry-run", async () => {
    const calls: string[] = [];
    const result = await runPublish({
      world: fakeWorld(calls),
      identity,
      repository: "testhost/app",
      dryRun: true,
    });
    assert.equal(result.outcome, "dry-run");
    assert.equal(result.plan.intent.kind, "fresh");
    assert.ok(calls.includes("prepare"));
    assert.ok(calls.includes("pack"));
    assert.ok(!calls.includes("stage-npm"));
  });

  test("an unexpected HEAD SHA is refused before any mutation", async () => {
    const OTHER = "ffffffffffffffffffffffffffffffffffffffff";
    await assert.rejects(
      () =>
        runPublish({
          world: fakeWorld([], { currentSha: async () => OTHER }),
          identity,
          repository: "testhost/app",
          dryRun: false,
        }),
      (error: unknown) => error instanceof ReleaseError && error.code === "sha_mismatch",
    );
  });

  test("duplicate remote state at this SHA is already complete", async () => {
    const calls: string[] = [];
    const result = await runPublish({
      world: fakeWorld(calls, {
        npmHasVersion: async () => true,
        npmGitHead: async () => SHA,
        remoteTagSha: async () => SHA,
        readGithubRelease: async () => matchingRelease(),
      }),
      identity,
      repository: "testhost/app",
      dryRun: false,
    });
    assert.equal(result.outcome, "already-complete");
    assert.ok(!calls.includes("stage-npm"));
    assert.ok(!calls.includes("create-tag"));
  });

  test("initial-release history uses a null baseline through the audited SHA", async () => {
    const spans: unknown[] = [];
    const earlier = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    const result = await runPublish({
      world: fakeWorld([], {
        listTags: async () => [],
        firstParentLog: async (span) => {
          spans.push({ kind: "log", ...span });
          return [
            { sha: earlier, subject: "feat: native service", body: "" },
            { sha: SHA, subject: "release: v4.2.0", body: "" },
          ];
        },
        changedFiles: async (span) => {
          spans.push({ kind: "files", ...span });
          return ["cmd/benes/service.go", "package.json"];
        },
        serviceLifecycleUrl: async () => "https://example.test/svc",
      }),
      identity,
      repository: "testhost/app",
      dryRun: true,
    });
    assert.equal(result.plan.notes.baseline, null);
    assert.match(result.plan.notes.markdown, /native service/);
    assert.deepEqual(spans, [
      { kind: "log", baseline: null, target: SHA },
      { kind: "files", baseline: null, target: SHA },
    ]);
    assert.equal(result.plan.serviceLifecycleRequired, true);
  });
});

describe("runCut", () => {
  test("plan-only does not bump, push, or dispatch", async () => {
    const calls: string[] = [];
    const result = await runCut({
      world: fakeWorld(calls, {
        packageVersion: async () => "4.1.0",
      }),
      version: "4.2.0",
      distTag: null,
      publish: false,
      planOnly: true,
    });
    assert.equal(result.outcome, "plan-only");
    assert.deepEqual(calls, []);
  });

  test("cut without --publish dispatches a dry-run workflow after local gates", async () => {
    const calls: string[] = [];
    const result = await runCut({
      world: fakeWorld(calls, { packageVersion: async () => "4.1.0" }),
      version: "4.2.0",
      distTag: null,
      publish: false,
      planOnly: false,
    });
    assert.equal(result.outcome, "dispatched-dry-run");
    assert.deepEqual(calls, [
      "local-checks",
      "bump",
      "commit-push",
      "wait:ci.yml",
      "dispatch:dryRun=true",
      "watch",
    ]);
  });

  test("cut dispatches the current SHA when package.json is already at the version", async () => {
    const calls: string[] = [];
    const result = await runCut({
      world: fakeWorld(calls, { packageVersion: async () => "4.2.0" }),
      version: "4.2.0",
      distTag: null,
      publish: true,
      planOnly: false,
    });
    assert.equal(result.outcome, "dispatched-stage-or-finalize");
    assert.ok(!calls.includes("bump"));
    assert.ok(!calls.includes("commit-push"));
    assert.ok(calls.includes("dispatch:dryRun=false"));
  });

  test("cut can finalize an approved staged version on the same branch version", async () => {
    const calls: string[] = [];
    const result = await runCut({
      world: fakeWorld(calls, {
        packageVersion: async () => "4.2.0",
        npmHasVersion: async () => true,
        npmGitHead: async () => SHA,
        npmDistTags: async () => ({ latest: "4.2.0" }),
      }),
      version: "4.2.0",
      distTag: null,
      publish: true,
      planOnly: false,
    });
    assert.equal(result.outcome, "dispatched-stage-or-finalize");
    assert.ok(!calls.includes("bump"));
    assert.ok(!calls.includes("commit-push"));
    assert.ok(calls.includes("dispatch:dryRun=false"));
  });

  test("cut refuses to start when the version is already on npm", async () => {
    await assert.rejects(
      () =>
        runCut({
          world: fakeWorld([], { npmHasVersion: async () => true, remoteTagSha: async () => SHA }),
          version: "4.2.0",
          distTag: null,
          publish: true,
          planOnly: false,
        }),
      ReleaseError,
    );
  });
});
