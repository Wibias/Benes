import assert from "node:assert/strict";
import { describe, test } from "node:test";
import { resolveIdentity } from "./identity.ts";
import {
  assemblePublicationPlan,
  classifyPublic,
  githubReleaseMismatch,
  serviceFilesChanged,
} from "./plan.ts";
import { ReleaseError } from "./semver.ts";
import type { PublicSnapshot } from "./world.ts";

const SHA = "0123456789abcdef0123456789abcdef01234567";
const OTHER = "fedcba9876543210fedcba9876543210fedcba98";

const identity = resolveIdentity({
  packageName: "benes",
  version: "4.2.0",
  sourceSha: SHA,
  branch: "main",
});

function snapshot(patch: Partial<PublicSnapshot> = {}): PublicSnapshot {
  return {
    npmHasVersion: false,
    npmGitHead: null,
    distTags: { latest: "4.1.0" },
    tagSha: null,
    githubRelease: null,
    ciPushUrl: "https://example.test/ci",
    serviceLifecycleUrl: null,
    ...patch,
  };
}

describe("classifyPublic", () => {
  test("vacant public state is a fresh publish", () => {
    const intent = classifyPublic(identity, snapshot());
    assert.equal(intent.kind, "fresh");
    assert.deepEqual(intent.kind === "fresh" ? intent.mutations : [], [
      "publish-npm",
      "create-tag",
      "create-github-release",
    ]);
  });

  test("npm success without GitHub metadata is recoverable", () => {
    const intent = classifyPublic(identity, snapshot({ npmHasVersion: true, npmGitHead: SHA }));
    assert.equal(intent.kind, "recover");
    assert.deepEqual(intent.kind === "recover" ? intent.mutations : [], [
      "create-tag",
      "create-github-release",
    ]);
  });

  test("npm plus tag at the same SHA recovers the GitHub Release only", () => {
    const intent = classifyPublic(identity, snapshot({ npmHasVersion: true, npmGitHead: SHA, tagSha: SHA }));
    assert.equal(intent.kind, "recover");
    assert.deepEqual(intent.kind === "recover" ? intent.mutations : [], ["create-github-release"]);
  });

  test("matching public metadata is already complete", () => {
    const intent = classifyPublic(
      identity,
      snapshot({
        npmHasVersion: true,
        npmGitHead: SHA,
        tagSha: SHA,
        githubRelease: { tag: "v4.2.0", draft: false, prerelease: false },
      }),
    );
    assert.equal(intent.kind, "complete");
  });

  test("matching preview public metadata is already complete", () => {
    const preview = resolveIdentity({
      packageName: "benes",
      version: "4.2.0-preview.1",
      sourceSha: SHA,
      branch: "preview",
    });
    const intent = classifyPublic(
      preview,
      snapshot({
        npmHasVersion: true,
        npmGitHead: SHA,
        tagSha: SHA,
        githubRelease: { tag: "v4.2.0-preview.1", draft: false, prerelease: true },
      }),
    );
    assert.equal(intent.kind, "complete");
  });

  test("a tag at a different SHA is a conflict", () => {
    const intent = classifyPublic(identity, snapshot({ tagSha: OTHER }));
    assert.equal(intent.kind, "conflict");
  });

  test("a GitHub Release without npm or tag is a conflict", () => {
    const intent = classifyPublic(
      identity,
      snapshot({ githubRelease: { tag: "v4.2.0", draft: false, prerelease: false } }),
    );
    assert.equal(intent.kind, "conflict");
  });

  test("an existing draft is never complete", () => {
    const intent = classifyPublic(
      identity,
      snapshot({
        npmHasVersion: true,
        npmGitHead: SHA,
        tagSha: SHA,
        githubRelease: { tag: "v4.2.0", draft: true, prerelease: false },
      }),
    );
    assert.equal(intent.kind, "conflict");
  });

  test("a stable version with an existing prerelease GitHub Release is a conflict", () => {
    const intent = classifyPublic(
      identity,
      snapshot({
        npmHasVersion: true,
        npmGitHead: SHA,
        tagSha: SHA,
        githubRelease: { tag: "v4.2.0", draft: false, prerelease: true },
      }),
    );
    assert.equal(intent.kind, "conflict");
  });

  test("npm at this version from another SHA is a conflict", () => {
    const intent = classifyPublic(
      identity,
      snapshot({ npmHasVersion: true, npmGitHead: OTHER }),
    );
    assert.equal(intent.kind, "conflict");
  });

  test("npm at this version without gitHead is a conflict", () => {
    const intent = classifyPublic(identity, snapshot({ npmHasVersion: true, npmGitHead: null }));
    assert.equal(intent.kind, "conflict");
  });
});

describe("githubReleaseMismatch", () => {
  const preview = resolveIdentity({
    packageName: "benes",
    version: "4.2.0-preview.1",
    sourceSha: SHA,
    branch: "preview",
  });

  test("a matching stable release is acceptable", () => {
    assert.equal(
      githubReleaseMismatch(identity, { tag: "v4.2.0", draft: false, prerelease: false }),
      null,
    );
  });

  test("a matching preview release must be prerelease", () => {
    assert.equal(
      githubReleaseMismatch(preview, { tag: "v4.2.0-preview.1", draft: false, prerelease: true }),
      null,
    );
    assert.match(
      githubReleaseMismatch(preview, { tag: "v4.2.0-preview.1", draft: false, prerelease: false }) ?? "",
      /prerelease/,
    );
  });
});

describe("assemblePublicationPlan", () => {
  test("refuses a dist-tag regression on a fresh npm publish", () => {
    assert.throws(
      () =>
        assemblePublicationPlan({
          identity,
          packageVersion: "4.2.0",
          repository: "testhost/app",
          snapshot: snapshot({ distTags: { latest: "4.3.0" } }),
          tags: ["v4.1.0"],
          commits: [{ sha: SHA, subject: "feat: board", body: "", pulls: [] }],
          generatedNotes: "",
          changedPaths: ["package.json"],
          isAncestor: () => true,
        }),
      ReleaseError,
    );
  });

  test("does not require dist-tag advancement when npm is already published", () => {
    const plan = assemblePublicationPlan({
      identity,
      packageVersion: "4.2.0",
      repository: "testhost/app",
      snapshot: snapshot({ npmHasVersion: true, npmGitHead: SHA, distTags: { latest: "4.2.0" } }),
      tags: ["v4.1.0"],
      commits: [{ sha: SHA, subject: "feat: board", body: "", pulls: [] }],
      generatedNotes: "",
      changedPaths: ["package.json"],
      isAncestor: () => true,
    });
    assert.equal(plan.intent.kind, "recover");
  });

  test("missing push CI is an operational blocker", () => {
    assert.throws(
      () =>
        assemblePublicationPlan({
          identity,
          packageVersion: "4.2.0",
          repository: "testhost/app",
          snapshot: snapshot({ ciPushUrl: null }),
          tags: ["v4.1.0"],
          commits: [{ sha: SHA, subject: "feat: board", body: "", pulls: [] }],
          generatedNotes: "",
          changedPaths: ["package.json"],
          isAncestor: () => true,
        }),
      (error: unknown) => error instanceof ReleaseError && error.code === "ci_evidence_missing",
    );
  });

  test("service lifecycle is required only when service files changed", () => {
    assert.equal(serviceFilesChanged(["package.json"]), false);
    assert.equal(serviceFilesChanged(["internal/servicectl/windows.go"]), true);
    assert.throws(
      () =>
        assemblePublicationPlan({
          identity,
          packageVersion: "4.2.0",
          repository: "testhost/app",
          snapshot: snapshot({ serviceLifecycleUrl: null }),
          tags: ["v4.1.0"],
          commits: [{ sha: SHA, subject: "fix: service stop", body: "", pulls: [] }],
          generatedNotes: "",
          changedPaths: ["cmd/benes/stop.go"],
          isAncestor: () => true,
        }),
      (error: unknown) =>
        error instanceof ReleaseError && error.code === "service_lifecycle_missing",
    );
  });

  test("rejects a package.json version that does not match the request", () => {
    assert.throws(
      () =>
        assemblePublicationPlan({
          identity,
          packageVersion: "4.1.9",
          repository: "testhost/app",
          snapshot: snapshot(),
          tags: ["v4.1.0"],
          commits: [{ sha: SHA, subject: "feat: board", body: "", pulls: [] }],
          generatedNotes: "",
          changedPaths: ["package.json"],
          isAncestor: () => true,
        }),
      ReleaseError,
    );
  });

  test("an initial release still requires service-lifecycle when service files are in the tree", () => {
    assert.throws(
      () =>
        assemblePublicationPlan({
          identity,
          packageVersion: "4.2.0",
          repository: "testhost/app",
          snapshot: snapshot({ serviceLifecycleUrl: null }),
          tags: [],
          commits: [
            { sha: OTHER, subject: "feat: native service", body: "", pulls: [] },
            { sha: SHA, subject: "release: v4.2.0", body: "", pulls: [] },
          ],
          generatedNotes: "",
          changedPaths: ["cmd/benes/service.go", "package.json"],
          isAncestor: () => false,
        }),
      (error: unknown) =>
        error instanceof ReleaseError && error.code === "service_lifecycle_missing",
    );
  });
});
